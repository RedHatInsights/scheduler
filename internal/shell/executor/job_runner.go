package executor

import (
	"log/slog"

	"insights-scheduler/internal/core/domain"
)

// JobRunner runs a specific job type (message, HTTP, export, etc.) and returns typed results.
// This is internal to the executor package - runners do the actual work for each payload type.
//
// JobRunner is different from ports.JobExecutor:
//   - JobRunner: Runs the job and returns typed results (the athlete)
//   - JobExecutor: Orchestrates execution with cross-cutting concerns (the coach)
//
// The jobRunID parameter allows runners to save intermediate state (e.g., external job IDs)
// for resumable operations after pod restarts. It may be empty for legacy callers.
type JobRunner interface {
	Execute(job domain.Job, jobRunID string, logger *slog.Logger) (result interface{}, resultType domain.ResultType, err error)
}
