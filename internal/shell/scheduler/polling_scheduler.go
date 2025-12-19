package scheduler

import (
	"context"
	"log"
	"time"

	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/usecases"
)

// JobQueue defines the interface for managing scheduled jobs
type JobQueue interface {
	Schedule(jobID string, nextRunTime time.Time) error
	Unschedule(jobID string) error
	GetJobsDue(currentTime time.Time) ([]string, error)
	GetNextRunTime(jobID string) (*time.Time, error)
	GetAllScheduledJobs() (map[string]time.Time, error)
	Count() (int64, error)
}

// PollingScheduler polls the job queue for jobs that need to be executed
type PollingScheduler struct {
	jobService         *usecases.JobService
	jobQueue           JobQueue
	scheduleCalculator *usecases.ScheduleCalculator
	pollInterval       time.Duration
	instanceID         string
}

// NewPollingScheduler creates a new polling-based scheduler
func NewPollingScheduler(
	jobService *usecases.JobService,
	jobQueue JobQueue,
	pollInterval time.Duration,
	instanceID string,
) *PollingScheduler {
	return &PollingScheduler{
		jobService:         jobService,
		jobQueue:           jobQueue,
		scheduleCalculator: usecases.NewScheduleCalculator(),
		pollInterval:       pollInterval,
		instanceID:         instanceID,
	}
}

// Start begins the polling loop
func (s *PollingScheduler) Start(ctx context.Context) {
	log.Printf("[%s] Starting polling scheduler (interval: %v)", s.instanceID, s.pollInterval)

	// Load existing jobs and schedule them
	s.loadAndScheduleAllJobs()

	// Start polling loop
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[%s] Polling scheduler context cancelled, stopping", s.instanceID)
			return
		case <-ticker.C:
			s.pollAndExecute()
		}
	}
}

// Stop stops the polling scheduler
func (s *PollingScheduler) Stop() {
	log.Printf("[%s] Stopping polling scheduler", s.instanceID)
	// Nothing to stop - context cancellation handles cleanup
}

// ScheduleJob adds or updates a job in the schedule queue
func (s *PollingScheduler) ScheduleJob(job domain.Job) error {
	log.Printf("[%s] ScheduleJob called - job ID: %s, name: %s, schedule: %s, status: %s",
		s.instanceID, job.ID, job.Name, job.Schedule, job.Status)

	// Only schedule jobs that are in scheduled status
	if job.Status != domain.StatusScheduled && job.Status != domain.StatusFailed {
		log.Printf("[%s] Job not in scheduled/failed status, unscheduling: %s (status: %s)",
			s.instanceID, job.ID, job.Status)
		return s.jobQueue.Unschedule(job.ID)
	}

	// Calculate next run time
	nextRun := s.scheduleCalculator.GetInitialRunTime(job, time.Now().UTC())

	// Add to queue with next run time as score
	if err := s.jobQueue.Schedule(job.ID, nextRun); err != nil {
		return err
	}

	log.Printf("[%s] Scheduled job %s (%s) to run at %s",
		s.instanceID, job.Name, job.ID, nextRun.Format(time.RFC3339))

	return nil
}

// UnscheduleJob removes a job from the schedule queue
func (s *PollingScheduler) UnscheduleJob(jobID string) {
	log.Printf("[%s] Unscheduling job: %s", s.instanceID, jobID)
	if err := s.jobQueue.Unschedule(jobID); err != nil {
		log.Printf("[%s] Error unscheduling job %s: %v", s.instanceID, jobID, err)
	}
}

// pollAndExecute polls for jobs that are due and executes them
func (s *PollingScheduler) pollAndExecute() {
	now := time.Now().UTC()

	// Get jobs that are due to run
	jobIDs, err := s.jobQueue.GetJobsDue(now)
	if err != nil {
		log.Printf("[%s] Error getting due jobs: %v", s.instanceID, err)
		return
	}

	if len(jobIDs) == 0 {
		return
	}

	log.Printf("[%s] Found %d jobs due for execution at %s", s.instanceID, len(jobIDs), now.Format(time.RFC3339))

	// Execute each job
	for _, jobID := range jobIDs {
		// Get job details
		job, err := s.jobService.GetJob(jobID)
		if err != nil {
			log.Printf("[%s] Error getting job %s: %v", s.instanceID, jobID, err)
			// Remove from queue if job doesn't exist
			s.jobQueue.Unschedule(jobID)
			continue
		}

		// Double-check job status
		if job.Status != domain.StatusScheduled && job.Status != domain.StatusFailed {
			log.Printf("[%s] Job %s is no longer scheduled (status: %s), unscheduling",
				s.instanceID, job.ID, job.Status)
			s.jobQueue.Unschedule(job.ID)
			continue
		}

		// Execute the job (distributed lock is handled by executor)
		log.Printf("[%s] Executing job: %s (%s)", s.instanceID, job.Name, job.ID)
		if err := s.jobService.ExecuteScheduledJob(job); err != nil {
			log.Printf("[%s] Error executing job %s: %v", s.instanceID, job.ID, err)
		}

		// Get updated job state after execution
		updatedJob, err := s.jobService.GetJob(jobID)
		if err != nil {
			log.Printf("[%s] Error getting updated job %s: %v", s.instanceID, jobID, err)
			continue
		}

		// Calculate next run time and update queue
		if updatedJob.Status == domain.StatusScheduled || updatedJob.Status == domain.StatusFailed {
			nextRun, err := s.scheduleCalculator.CalculateNextRun(updatedJob, now)
			if err != nil {
				log.Printf("[%s] Error calculating next run for job %s: %v", s.instanceID, jobID, err)
				s.jobQueue.Unschedule(jobID)
				continue
			}

			// Update the score in the ZSET with the new next run time
			if err := s.jobQueue.Schedule(jobID, nextRun); err != nil {
				log.Printf("[%s] Error rescheduling job %s: %v", s.instanceID, jobID, err)
			} else {
				log.Printf("[%s] Rescheduled job %s to run at %s", s.instanceID, job.Name, nextRun.Format(time.RFC3339))
			}
		} else {
			// Job is paused or in another state, remove from queue
			log.Printf("[%s] Job %s status changed to %s, unscheduling", s.instanceID, job.ID, updatedJob.Status)
			s.jobQueue.Unschedule(jobID)
		}
	}
}

// loadAndScheduleAllJobs loads all scheduled jobs from the repository and schedules them
func (s *PollingScheduler) loadAndScheduleAllJobs() {
	jobs, err := s.jobService.ListJobs()
	if err != nil {
		log.Printf("[%s] Error loading jobs: %v", s.instanceID, err)
		return
	}

	scheduled := 0
	for _, job := range jobs {
		// Only schedule jobs that are scheduled or failed
		if job.Status == domain.StatusScheduled || job.Status == domain.StatusFailed {
			if err := s.ScheduleJob(job); err != nil {
				log.Printf("[%s] Error scheduling job %s: %v", s.instanceID, job.ID, err)
			} else {
				scheduled++
			}
		}
	}

	log.Printf("[%s] Loaded and scheduled %d jobs", s.instanceID, scheduled)
}
