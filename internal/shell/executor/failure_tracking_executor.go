package executor

import (
	"log/slog"

	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/ports"
	"insights-scheduler/internal/core/usecases"
	"insights-scheduler/internal/shell/logging"
)

// FailureTrackingExecutor wraps a ports.JobExecutor and adds automatic failure tracking
// and auto-pause logic after consecutive failures. Implements ports.JobExecutor.
type FailureTrackingExecutor struct {
	inner      ports.JobExecutor
	tracker    *FailureTracker
	baseLogger *slog.Logger
}

// NewFailureTrackingExecutor creates an executor that tracks failures and auto-pauses jobs
func NewFailureTrackingExecutor(inner ports.JobExecutor, jobRepo usecases.JobRepository, notifier JobCompletionNotifier, maxConsecutiveFailures int, baseLogger *slog.Logger) *FailureTrackingExecutor {
	return &FailureTrackingExecutor{
		inner:      inner,
		tracker:    NewFailureTracker(jobRepo, notifier, maxConsecutiveFailures),
		baseLogger: baseLogger,
	}
}

func (e *FailureTrackingExecutor) Execute(job domain.Job) error {
	return e.executeWithTracking(job, "")
}

func (e *FailureTrackingExecutor) ExecuteWithJobRun(job domain.Job, jobRunID string) error {
	return e.executeWithTracking(job, jobRunID)
}

func (e *FailureTrackingExecutor) Wait() {
	e.inner.Wait()
}

func (e *FailureTrackingExecutor) executeWithTracking(job domain.Job, jobRunID string) error {
	logger := logging.NewJobExecutionLogger(e.baseLogger, job.ID, jobRunID, job.OrgID, job.UserID)

	var execErr error
	if jobRunID != "" {
		execErr = e.inner.ExecuteWithJobRun(job, jobRunID)
	} else {
		execErr = e.inner.Execute(job)
	}

	if e.tracker != nil {
		if execErr != nil {
			e.tracker.TrackFailure(job, execErr, jobRunID, logger)
		} else if job.Type != domain.PayloadExport {
			// Export success tracking is handled by ExportPollerService
			// when the export actually completes.
			e.tracker.TrackSuccess(job, logger)
		}
	}

	return execErr
}
