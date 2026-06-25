package executor

import (
	"context"
	"log/slog"
	"time"

	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/ports"
	"insights-scheduler/internal/core/usecases"
	"insights-scheduler/internal/shell/logging"
)

// FailureTrackingExecutor wraps a ports.JobExecutor and adds automatic failure tracking
// and auto-pause logic after consecutive failures. Implements ports.JobExecutor.
type FailureTrackingExecutor struct {
	inner                  ports.JobExecutor
	jobRepo                usecases.JobRepository
	notifier               JobCompletionNotifier
	maxConsecutiveFailures int
	baseLogger             *slog.Logger
}

// NewFailureTrackingExecutor creates an executor that tracks failures and auto-pauses jobs
func NewFailureTrackingExecutor(inner ports.JobExecutor, jobRepo usecases.JobRepository, notifier JobCompletionNotifier, maxConsecutiveFailures int, baseLogger *slog.Logger) *FailureTrackingExecutor {
	return &FailureTrackingExecutor{
		inner:                  inner,
		jobRepo:                jobRepo,
		notifier:               notifier,
		maxConsecutiveFailures: maxConsecutiveFailures,
		baseLogger:             baseLogger,
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
	// Create logger for this execution
	logger := logging.NewJobExecutionLogger(e.baseLogger, job.ID, jobRunID, job.OrgID, job.UserID)

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
				logger.Warn("Job exceeded failure threshold, auto-pausing",
					slog.Int("consecutive_failures", updatedJob.ConsecutiveFailures),
					slog.Int("max_failures", e.maxConsecutiveFailures))
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
			logger.Warn("Failed to save job after execution", slog.Any("error", err))
			// Don't fail the execution because of a save error
		} else {
			logger.Debug("Updated job after execution",
				slog.String("status", string(updatedJob.Status)),
				slog.Int("consecutive_failures", updatedJob.ConsecutiveFailures))

			// Send notification if the job was auto-paused
			if wasAutoPaused && e.notifier != nil {
				e.sendAutoPausedNotification(updatedJob, execErr, logger)
			}
		}
	}

	return execErr
}

// sendAutoPausedNotification sends a notification when a job is auto-paused
func (e *FailureTrackingExecutor) sendAutoPausedNotification(job domain.Job, lastError error, logger *slog.Logger) {
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

	if err := e.notifier.JobAutoPaused(ctx, notification, logger); err != nil {
		logger.Warn("Failed to send auto-paused notification", slog.Any("error", err))
		// Don't fail the execution because of a notification error
	} else {
		logger.Info("Successfully sent auto-paused notification")
	}
}
