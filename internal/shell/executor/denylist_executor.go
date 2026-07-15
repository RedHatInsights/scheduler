package executor

import (
	"log/slog"

	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/ports"
	"insights-scheduler/internal/shell/logging"
)

// DenylistExecutor wraps another executor and checks if job IDs are on a denylist before executing.
// Denied jobs are logged and return nil (success) to avoid triggering failure tracking or auto-pause.
type DenylistExecutor struct {
	wrapped     ports.JobExecutor
	denylistIDs map[string]bool
	baseLogger  *slog.Logger
}

func NewDenylistExecutor(wrapped ports.JobExecutor, denylistJobIDs []string, baseLogger *slog.Logger) *DenylistExecutor {
	// Convert slice to map for O(1) lookups
	denylistMap := make(map[string]bool)
	for _, jobID := range denylistJobIDs {
		denylistMap[jobID] = true
	}

	return &DenylistExecutor{
		wrapped:     wrapped,
		denylistIDs: denylistMap,
		baseLogger:  baseLogger,
	}
}

func (e *DenylistExecutor) Execute(job domain.Job) error {
	if e.isDenied(job.ID) {
		logger := logging.NewJobExecutionLogger(e.baseLogger, job.ID, "", job.OrgID, job.UserID)
		logger.Warn("Job execution skipped - job is on denylist",
			slog.String("name", job.Name),
			slog.String("type", string(job.Type)))
		// Return nil (success) so denied jobs don't count as failures
		// This prevents auto-pause and failure notifications for denied jobs
		return nil
	}

	return e.wrapped.Execute(job)
}

func (e *DenylistExecutor) ExecuteWithJobRun(job domain.Job, jobRunID string) error {
	if e.isDenied(job.ID) {
		logger := logging.NewJobExecutionLogger(e.baseLogger, job.ID, jobRunID, job.OrgID, job.UserID)
		logger.Warn("Job execution skipped - job is on denylist",
			slog.String("name", job.Name),
			slog.String("type", string(job.Type)))
		// Return nil (success) so denied jobs don't count as failures
		// This prevents auto-pause and failure notifications for denied jobs
		return nil
	}

	return e.wrapped.ExecuteWithJobRun(job, jobRunID)
}

func (e *DenylistExecutor) Wait() {
	e.wrapped.Wait()
}

func (e *DenylistExecutor) isDenied(jobID string) bool {
	return e.denylistIDs[jobID]
}
