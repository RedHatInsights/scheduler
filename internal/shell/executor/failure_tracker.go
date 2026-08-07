package executor

import (
	"context"
	"log/slog"
	"time"

	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/usecases"
)

// FailureTracker encapsulates consecutive failure tracking and auto-pause logic.
// Used by both FailureTrackingExecutor (for synchronous job outcomes) and
// ExportPollerService (for deferred export outcomes).
type FailureTracker struct {
	jobRepo                usecases.JobRepository
	notifier               JobCompletionNotifier
	maxConsecutiveFailures int
}

func NewFailureTracker(jobRepo usecases.JobRepository, notifier JobCompletionNotifier, maxConsecutiveFailures int) *FailureTracker {
	return &FailureTracker{
		jobRepo:                jobRepo,
		notifier:               notifier,
		maxConsecutiveFailures: maxConsecutiveFailures,
	}
}

func (t *FailureTracker) TrackSuccess(job domain.Job, logger *slog.Logger) {
	updatedJob := job.WithFailuresReset().WithStatus(domain.StatusScheduled)
	if err := t.jobRepo.Save(updatedJob); err != nil {
		logger.Warn("Failed to save job after success tracking", slog.Any("error", err))
	} else {
		logger.Debug("Tracked job success",
			slog.String("status", string(updatedJob.Status)),
			slog.Int("consecutive_failures", updatedJob.ConsecutiveFailures))
	}
}

func (t *FailureTracker) TrackFailure(job domain.Job, execErr error, logger *slog.Logger) {
	updatedJob := job.WithFailuresIncremented(time.Now().UTC())

	JobsConsecutiveFailures.Observe(float64(updatedJob.ConsecutiveFailures))

	wasAutoPaused := false
	if t.maxConsecutiveFailures > 0 && updatedJob.ConsecutiveFailures >= t.maxConsecutiveFailures {
		logger.Warn("Job exceeded failure threshold, auto-pausing",
			slog.Int("consecutive_failures", updatedJob.ConsecutiveFailures),
			slog.Int("max_failures", t.maxConsecutiveFailures))
		updatedJob = updatedJob.WithStatus(domain.StatusPaused).WithNextRunAtCleared()
		wasAutoPaused = true

		JobsAutoPausedTotal.Inc()
	} else {
		updatedJob = updatedJob.WithStatus(domain.StatusFailed)
	}

	if err := t.jobRepo.Save(updatedJob); err != nil {
		logger.Warn("Failed to save job after failure tracking", slog.Any("error", err))
	} else {
		logger.Debug("Tracked job failure",
			slog.String("status", string(updatedJob.Status)),
			slog.Int("consecutive_failures", updatedJob.ConsecutiveFailures))

		if wasAutoPaused && t.notifier != nil {
			t.sendAutoPausedNotification(updatedJob, execErr, logger)
		}
	}
}

func (t *FailureTracker) sendAutoPausedNotification(job domain.Job, lastError error, logger *slog.Logger) {
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

	if err := t.notifier.JobAutoPaused(ctx, notification, logger); err != nil {
		logger.Warn("Failed to send auto-paused notification", slog.Any("error", err))
	} else {
		logger.Info("Successfully sent auto-paused notification")
	}
}
