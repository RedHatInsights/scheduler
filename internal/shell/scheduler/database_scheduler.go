package scheduler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/ports"
)

// DatabaseScheduler manages job scheduling using PostgreSQL FOR UPDATE SKIP LOCKED
// for distributed coordination without Redis
type DatabaseScheduler struct {
	executor            ports.JobExecutor
	jobRepo             DatabaseJobRepository
	parser              cron.Parser
	ctx                 context.Context
	cancel              context.CancelFunc
	pollInterval        time.Duration
	maxConcurrentJobs   int
	jobExecutionTimeout time.Duration
	workerSemaphore     chan struct{}  // Buffered channel for worker pool
	activeJobsWg        sync.WaitGroup // Tracks in-flight jobs for graceful shutdown
}

// DatabaseJobRepository provides database access with atomic job fetching
type DatabaseJobRepository interface {
	Save(job domain.Job) error
	FindByID(id string) (domain.Job, error)
	// FetchDueJobs atomically claims jobs using FOR UPDATE SKIP LOCKED
	FetchDueJobs(ctx context.Context, limit int) ([]domain.Job, error)
}

// NewDatabaseScheduler creates a new database-based scheduler
func NewDatabaseScheduler(
	executor ports.JobExecutor,
	jobRepo DatabaseJobRepository,
	pollInterval time.Duration,
	maxConcurrentJobs int,
	jobExecutionTimeout time.Duration,
) *DatabaseScheduler {
	ctx, cancel := context.WithCancel(context.Background())

	// Default to 10 seconds if not specified
	if pollInterval == 0 {
		pollInterval = 10 * time.Second
	}

	if maxConcurrentJobs <= 0 {
		maxConcurrentJobs = 10 // sensible default
	}

	if jobExecutionTimeout == 0 {
		jobExecutionTimeout = 2 * time.Minute
	}

	return &DatabaseScheduler{
		executor:            executor,
		jobRepo:             jobRepo,
		parser:              cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
		ctx:                 ctx,
		cancel:              cancel,
		pollInterval:        pollInterval,
		maxConcurrentJobs:   maxConcurrentJobs,
		jobExecutionTimeout: jobExecutionTimeout,
		workerSemaphore:     make(chan struct{}, maxConcurrentJobs),
		activeJobsWg:        sync.WaitGroup{},
	}
}

// Start begins the scheduler loop
func (s *DatabaseScheduler) Start() {
	log.Printf("[DatabaseScheduler] Starting scheduler (poll interval: %s)", s.pollInterval)

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			log.Println("[DatabaseScheduler] Scheduler stopped")
			return
		case <-ticker.C:
			s.processDueJobs()
		}
	}
}

// Stop gracefully stops the scheduler
func (s *DatabaseScheduler) Stop() {
	log.Println("[DatabaseScheduler] Stopping scheduler")
	s.cancel()

	// Wait for in-flight jobs with timeout
	done := make(chan struct{})
	go func() {
		s.activeJobsWg.Wait()
		close(done)
	}()

	// Use a reasonable timeout for graceful shutdown
	shutdownTimeout := 5 * time.Second
	select {
	case <-done:
		log.Println("[DatabaseScheduler] All in-flight jobs completed")
	case <-time.After(shutdownTimeout):
		log.Printf("[DatabaseScheduler] Warning: Graceful shutdown timeout after %s, some jobs may still be running", shutdownTimeout)
	}
}

// processDueJobs finds and executes jobs that are due to run
func (s *DatabaseScheduler) processDueJobs() {
	// Fetch due jobs atomically using FOR UPDATE SKIP LOCKED
	jobs, err := s.jobRepo.FetchDueJobs(s.ctx, 100)
	if err != nil {
		log.Printf("[DatabaseScheduler] Error fetching due jobs: %v", err)
		return
	}

	if len(jobs) == 0 {
		return
	}

	log.Printf("[DatabaseScheduler] Found %d jobs due for execution (concurrent dispatch)", len(jobs))

	// Dispatch jobs concurrently with worker pool limiting
	for _, job := range jobs {
		job := job // Capture loop variable for goroutine

		s.activeJobsWg.Add(1)
		go func() {
			defer s.activeJobsWg.Done()

			// Acquire worker slot (blocks if pool is full)
			s.workerSemaphore <- struct{}{}
			defer func() { <-s.workerSemaphore }()

			// Execute with timeout context to guard against hung services
			ctx, cancel := context.WithTimeout(s.ctx, s.jobExecutionTimeout)
			defer cancel()

			s.executeJobWithContext(ctx, job)
		}()
	}
}

// executeJobWithContext executes a single job and reschedules it
func (s *DatabaseScheduler) executeJobWithContext(ctx context.Context, job domain.Job) {
	// Increment concurrent jobs gauge
	ConcurrentJobsGauge.Inc()
	defer ConcurrentJobsGauge.Dec()

	// Update worker pool utilization
	utilized := float64(len(s.workerSemaphore)) / float64(s.maxConcurrentJobs) * 100
	WorkerPoolUtilization.Set(utilized)

	// Update last_run_at before execution
	now := time.Now()
	job = job.WithLastRunAt(now)

	// Execute the job with timeout awareness
	var execErr error
	executionComplete := make(chan error, 1)

	go func() {
		log.Printf("[DatabaseScheduler] Executing job %s", job.ID)
		executionComplete <- s.executor.Execute(job)
	}()

	// Wait for execution or timeout
	timedOut := false
	select {
	case execErr = <-executionComplete:
		if execErr != nil {
			log.Printf("[DatabaseScheduler] Error executing job %s: %v", job.ID, execErr)
		}
	case <-ctx.Done():
		JobsTimedOutTotal.Inc()
		log.Printf("[DatabaseScheduler] Job %s execution timeout after %s, waiting for executor to finish...", job.ID, s.jobExecutionTimeout)
		timedOut = true
		execErr = fmt.Errorf("job execution timeout after %s", s.jobExecutionTimeout)

		// CRITICAL: Wait for executor goroutine to complete to prevent double execution.
		gracePeriod := 30 * time.Second
		select {
		case <-executionComplete:
			log.Printf("[DatabaseScheduler] Job %s executor completed after timeout", job.ID)
		case <-time.After(gracePeriod):
			log.Printf("[DatabaseScheduler] Job %s executor still running after %s grace period",
				job.ID, gracePeriod)
		}
	}

	// For timed-out jobs, reset status back to scheduled with delayed next_run_at
	if timedOut {
		log.Printf("[DatabaseScheduler] Job %s timed out, resetting status and delaying next run", job.ID)

		// Calculate next run time (delayed by 5 minutes as penalty for timeout)
		schedule, err := s.parser.Parse(string(job.Schedule))
		if err == nil {
			nextRun := schedule.Next(time.Now().Add(5 * time.Minute))
			timeoutJob := job.WithStatus(domain.StatusScheduled).WithLastRunAt(now).WithNextRunAt(nextRun)
			if saveErr := s.jobRepo.Save(timeoutJob); saveErr != nil {
				log.Printf("[DatabaseScheduler] Error saving timed-out job %s: %v", job.ID, saveErr)
			}
		}
		return
	}

	// Reload the job from the database to get updated status/failure tracking
	// The executor may have updated consecutive_failures and auto-paused the job
	reloadedJob, err := s.jobRepo.FindByID(job.ID)
	if err != nil {
		// Check if job was deleted (race condition: deleted while executing)
		if err == domain.ErrJobNotFound {
			log.Printf("[DatabaseScheduler] Job %s was deleted during execution, will not reschedule", job.ID)
			return
		}

		log.Printf("[DatabaseScheduler] Warning: Failed to reload job %s: %v", job.ID, err)
		reloadedJob = job // Fallback to the job we had
	}

	// Check if the job was auto-paused or manually paused during execution
	if reloadedJob.Status == domain.StatusPaused {
		log.Printf("[DatabaseScheduler] Job %s is now paused (consecutive_failures=%d), will not reschedule",
			job.ID, reloadedJob.ConsecutiveFailures)
		return
	}

	// Calculate next run time and reschedule (only if not paused)
	schedule, err := s.parser.Parse(string(job.Schedule))
	if err != nil {
		log.Printf("[DatabaseScheduler] Error parsing schedule for job %s: %v", job.ID, err)
		// Reset status back to scheduled even if we can't parse schedule
		resetJob := reloadedJob.WithStatus(domain.StatusScheduled)
		s.jobRepo.Save(resetJob)
		return
	}

	nextRun := schedule.Next(time.Now())

	// Use the reloaded job (with updated failure tracking) for rescheduling
	// IMPORTANT: Set status back to 'scheduled' (it was 'running' during execution)
	updatedJob := reloadedJob.WithStatus(domain.StatusScheduled).WithLastRunAt(now).WithNextRunAt(nextRun)

	// Persist updated job to PostgreSQL
	if err := s.jobRepo.Save(updatedJob); err != nil {
		log.Printf("[DatabaseScheduler] Error saving job %s: %v", job.ID, err)
	} else {
		lastRunStr := "<nil>"
		if updatedJob.LastRunAt != nil {
			lastRunStr = updatedJob.LastRunAt.Format(time.RFC3339)
		}
		nextRunStr := "<nil>"
		if updatedJob.NextRunAt != nil {
			nextRunStr = updatedJob.NextRunAt.Format(time.RFC3339)
		}
		log.Printf("[DatabaseScheduler] Rescheduled job %s for %s (last_run: %s)",
			job.ID, nextRunStr, lastRunStr)
	}
}

// ScheduleJob adds or updates a job in the schedule
// For database-only mode, this just updates next_run_at in the database
func (s *DatabaseScheduler) ScheduleJob(job domain.Job) error {
	if job.Status != domain.StatusScheduled {
		log.Printf("[DatabaseScheduler] Skipping job %s - status is %s", job.ID, job.Status)
		return nil
	}

	// Parse schedule to get next run time
	schedule, err := s.parser.Parse(string(job.Schedule))
	if err != nil {
		return fmt.Errorf("invalid schedule: %w", err)
	}

	now := time.Now()
	nextRun := schedule.Next(now)

	// Update job with next run time
	job = job.WithNextRunAt(nextRun)

	// Save to database (workers will pick it up during polling)
	if err := s.jobRepo.Save(job); err != nil {
		return fmt.Errorf("failed to schedule job in database: %w", err)
	}

	log.Printf("[DatabaseScheduler] Scheduled job %s (next run: %s)", job.ID, nextRun.Format(time.RFC3339))
	return nil
}

// ScheduleJobImmediately schedules a job to run immediately
// This is used for manual job runs triggered via the API
func (s *DatabaseScheduler) ScheduleJobImmediately(job domain.Job, jobRunID string) error {
	log.Printf("[DatabaseScheduler] Scheduling job %s for immediate execution (job run: %s)", job.ID, jobRunID)

	// Set next run to current time (or slightly in the past to ensure immediate pickup)
	now := time.Now()
	immediateRun := now.Add(-5 * time.Second) // 5 seconds in the past to ensure it's picked up

	// Update job with immediate run time
	job = job.WithNextRunAt(immediateRun)

	// Save to database (workers will pick it up on next poll)
	if err := s.jobRepo.Save(job); err != nil {
		return fmt.Errorf("failed to schedule job for immediate execution: %w", err)
	}

	log.Printf("[DatabaseScheduler] Job %s scheduled for immediate execution (next run: %s, job run: %s)",
		job.ID, immediateRun.Format(time.RFC3339), jobRunID)
	return nil
}

// UnscheduleJob removes a job from the schedule
// For database-only mode, this updates the job status to prevent execution
func (s *DatabaseScheduler) UnscheduleJob(jobID string) {
	// Load the job
	job, err := s.jobRepo.FindByID(jobID)
	if err != nil {
		log.Printf("[DatabaseScheduler] Error finding job %s to unschedule: %v", jobID, err)
		return
	}

	// Update status to paused (workers will skip it)
	job = job.WithStatus(domain.StatusPaused)

	if err := s.jobRepo.Save(job); err != nil {
		log.Printf("[DatabaseScheduler] Error unscheduling job %s: %v", jobID, err)
		return
	}

	log.Printf("[DatabaseScheduler] Unscheduled job %s", jobID)
}

// Close closes the scheduler
func (s *DatabaseScheduler) Close() error {
	s.Stop()
	return nil
}
