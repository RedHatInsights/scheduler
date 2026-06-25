package executor

import (
	"context"
	"log"
	"time"

	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/ports"
	"insights-scheduler/internal/core/usecases"
)

// FailureTrackingExecutor wraps a ports.JobExecutor and adds automatic failure tracking
// and auto-pause logic after consecutive failures. Implements ports.JobExecutor.
type FailureTrackingExecutor struct {
	inner                  ports.JobExecutor
	jobRepo                usecases.JobRepository
	notifier               JobCompletionNotifier
	maxConsecutiveFailures int
}

// NewFailureTrackingExecutor creates an executor that tracks failures and auto-pauses jobs
func NewFailureTrackingExecutor(inner ports.JobExecutor, jobRepo usecases.JobRepository, notifier JobCompletionNotifier, maxConsecutiveFailures int) *FailureTrackingExecutor {
	return &FailureTrackingExecutor{
		inner:                  inner,
		jobRepo:                jobRepo,
		notifier:               notifier,
		maxConsecutiveFailures: maxConsecutiveFailures,
	}
}

func (e *FailureTrackingExecutor) Execute(job domain.Job) error {
	return e.executeWithTracking(job, "")
}

func (e *FailureTrackingExecutor) ExecuteWithJobRun(job domain.Job, jobRunID string) error {
	return e.executeWithTracking(job, jobRunID)
}

func (e *FailureTrackingExecutor) Wait() {
	// Delegate to the inner executor's Wait method for graceful shutdown
	e.inner.Wait()
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
		wasAutoPaused := false

		if execErr != nil {
			// Job failed - increment failure counter
			updatedJob = updatedJob.WithFailuresIncremented(time.Now().UTC())

			// Record failure count metric
			JobsConsecutiveFailures.Observe(float64(updatedJob.ConsecutiveFailures))

			// Check if we should auto-pause
			if e.maxConsecutiveFailures > 0 && updatedJob.ConsecutiveFailures >= e.maxConsecutiveFailures {
				log.Printf("[FailureTrackingExecutor] Job %s exceeded failure threshold (%d consecutive failures), auto-pausing",
					job.ID, e.maxConsecutiveFailures)
				updatedJob = updatedJob.WithStatus(domain.StatusPaused).WithNextRunAtCleared()
				wasAutoPaused = true

				// Record auto-pause metric
				JobsAutoPausedTotal.Inc()
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

			// Send notification if the job was auto-paused
			if wasAutoPaused && e.notifier != nil {
				e.sendAutoPausedNotification(updatedJob, execErr)
			}
		}
	}

	return execErr
}

// sendAutoPausedNotification sends a notification when a job is auto-paused
func (e *FailureTrackingExecutor) sendAutoPausedNotification(job domain.Job, lastError error) {
	errorMsg := ""
	if lastError != nil {
		errorMsg = lastError.Error()
	}

	notification := &JobAutoPausedNotification{
		JobID:               job.ID,
		JobName:             job.Name,
		OrgID:               job.OrgID,
		UserID:              job.UserID,
		ConsecutiveFailures: job.ConsecutiveFailures,
		ErrorMsg:            errorMsg,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := e.notifier.JobAutoPaused(ctx, notification); err != nil {
		log.Printf("[FailureTrackingExecutor] Warning: Failed to send auto-paused notification for job %s: %v", job.ID, err)
		// Don't fail the execution because of a notification error
	} else {
		log.Printf("[FailureTrackingExecutor] Successfully sent auto-paused notification for job %s", job.ID)
	}
}
