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
	now := time.Now()

	// Fetch and claim due jobs atomically using FOR UPDATE SKIP LOCKED
	// This sets last_run_at = NOW() to mark them as claimed
	jobs, err := s.jobRepo.FetchDueJobs(s.ctx, 100)
	if err != nil {
		log.Printf("[DatabaseScheduler] Error fetching due jobs: %v", err)
		return
	}

	if len(jobs) == 0 {
		return
	}

	log.Printf("[DatabaseScheduler] Found %d jobs due for execution (concurrent dispatch)", len(jobs))

	// Immediately update next_run_at for all claimed jobs to prevent re-claiming
	// Calculate next run time from cron schedule and save it
	for _, job := range jobs {
		schedule, err := s.parser.Parse(string(job.Schedule))
		if err != nil {
			log.Printf("[DatabaseScheduler] Error parsing schedule for job %s: %v", job.ID, err)
			continue
		}

		nextRun := schedule.Next(now)
		job = job.WithNextRunAt(nextRun)

		// Save updated next_run_at immediately to prevent duplicate execution
		if err := s.jobRepo.Save(job); err != nil {
			log.Printf("[DatabaseScheduler] Error updating next_run_at for job %s: %v", job.ID, err)
		}
	}

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

	// For timed-out jobs, optionally delay next run
	if timedOut {
		log.Printf("[DatabaseScheduler] Job %s timed out (next_run_at already set correctly)", job.ID)
		// next_run_at was already calculated and saved before execution
		// No need to update it unless we want to add a penalty delay
		return
	}

	// Check if job was deleted or paused during execution
	// (next_run_at is already correct, but we need to know if we should log differently)
	if reloadedJob, err := s.jobRepo.FindByID(job.ID); err == nil {
		if reloadedJob.Status == domain.StatusPaused {
			log.Printf("[DatabaseScheduler] Job %s was auto-paused during execution (consecutive_failures=%d)",
				job.ID, reloadedJob.ConsecutiveFailures)
		} else if err == domain.ErrJobNotFound {
			log.Printf("[DatabaseScheduler] Job %s was deleted during execution", job.ID)
		}
	}

	// Job execution complete, next_run_at was already set at claim time
	// Status remains 'scheduled' (no status overloading)
	log.Printf("[DatabaseScheduler] Job %s execution completed", job.ID)
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
