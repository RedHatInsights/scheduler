package executor

import (
	"fmt"
	"log/slog"

	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/shell/logging"
)

// DenylistExecutor wraps another executor and checks if job IDs are on a denylist before executing.
// If a job is denied, it logs the denial and returns an error without executing.
type DenylistExecutor struct {
	wrapped     JobExecutor
	denylistIDs map[string]bool
	baseLogger  *slog.Logger
}

// JobExecutor interface matches the Execute methods we need
type JobExecutor interface {
	Execute(job domain.Job) error
	ExecuteWithJobRun(job domain.Job, jobRunID string) error
	Wait()
}

func NewDenylistExecutor(wrapped JobExecutor, denylistJobIDs []string, baseLogger *slog.Logger) *DenylistExecutor {
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
		logger.Warn("Job execution denied - job is on denylist",
			slog.String("name", job.Name),
			slog.String("type", string(job.Type)))
		return fmt.Errorf("job %s is on the denylist and cannot be executed", job.ID)
	}

	return e.wrapped.Execute(job)
}

func (e *DenylistExecutor) ExecuteWithJobRun(job domain.Job, jobRunID string) error {
	if e.isDenied(job.ID) {
		logger := logging.NewJobExecutionLogger(e.baseLogger, job.ID, jobRunID, job.OrgID, job.UserID)
		logger.Warn("Job execution denied - job is on denylist",
			slog.String("name", job.Name),
			slog.String("type", string(job.Type)))
		return fmt.Errorf("job %s is on the denylist and cannot be executed", job.ID)
	}

	return e.wrapped.ExecuteWithJobRun(job, jobRunID)
}

func (e *DenylistExecutor) Wait() {
	e.wrapped.Wait()
}

func (e *DenylistExecutor) isDenied(jobID string) bool {
	return e.denylistIDs[jobID]
}
