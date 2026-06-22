package executor

import (
	"log"
	"time"

	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/usecases"
)

// FailureTrackingExecutor wraps a JobExecutor and adds automatic failure tracking
// and auto-pause logic after consecutive failures.
type FailureTrackingExecutor struct {
	inner                  JobExecutor
	jobRepo                usecases.JobRepository
	maxConsecutiveFailures int
}

// NewFailureTrackingExecutor creates an executor that tracks failures and auto-pauses jobs
func NewFailureTrackingExecutor(inner JobExecutor, jobRepo usecases.JobRepository, maxConsecutiveFailures int) *FailureTrackingExecutor {
	return &FailureTrackingExecutor{
		inner:                  inner,
		jobRepo:                jobRepo,
		maxConsecutiveFailures: maxConsecutiveFailures,
	}
}

func (e *FailureTrackingExecutor) Execute(job domain.Job) error {
	return e.executeWithTracking(job, "")
}

func (e *FailureTrackingExecutor) ExecuteWithJobRun(job domain.Job, jobRunID string) error {
	return e.executeWithTracking(job, jobRunID)
}

func (e *FailureTrackingExecutor) executeWithTracking(job domain.Job, jobRunID string) error {
	// Execute the job using the inner executor
	var execErr error
	if jobRunID != "" {
		execErr = e.inner.ExecuteWithJobRun(job, jobRunID)
	} else {
		execErr = e.inner.Execute(job)
	}

	// Track failure/success and potentially auto-pause
	if e.jobRepo != nil {
		updatedJob := job

		if execErr != nil {
			// Job failed - increment failure counter
			updatedJob = updatedJob.WithFailureIncremented(time.Now().UTC())

			// Check if we should auto-pause
			if e.maxConsecutiveFailures > 0 && updatedJob.ConsecutiveFailures >= e.maxConsecutiveFailures {
				log.Printf("[FailureTrackingExecutor] Job %s exceeded failure threshold (%d consecutive failures), auto-pausing",
					job.ID, e.maxConsecutiveFailures)
				updatedJob = updatedJob.WithStatus(domain.StatusPaused)
			} else {
				updatedJob = updatedJob.WithStatus(domain.StatusFailed)
			}
		} else {
			// Job succeeded - reset failure counter
			updatedJob = updatedJob.WithFailuresReset()
			updatedJob = updatedJob.WithStatus(domain.StatusScheduled)
		}

		// Save the updated job to persist failure tracking
		if err := e.jobRepo.Save(updatedJob); err != nil {
			log.Printf("[FailureTrackingExecutor] Warning: Failed to save job %s after execution: %v", job.ID, err)
			// Don't fail the execution because of a save error
		} else {
			log.Printf("[FailureTrackingExecutor] Updated job %s: status=%s, consecutive_failures=%d",
				job.ID, updatedJob.Status, updatedJob.ConsecutiveFailures)
		}
	}

	return execErr
}
