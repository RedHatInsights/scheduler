package ports

import "insights-scheduler/internal/core/domain"

// JobExecutor executes jobs with cross-cutting concerns (tracking, metrics, lifecycle management).
// This is the interface used by schedulers and the job service to execute jobs.
//
// Implementations may wrap other executors to add additional behavior (decorator pattern),
// such as failure tracking, retry logic, or notifications.
type JobExecutor interface {
	// Execute runs a job immediately
	Execute(job domain.Job) error

	// ExecuteWithJobRun runs a job with a pre-created job run ID
	ExecuteWithJobRun(job domain.Job, jobRunID string) error

	// Wait blocks until all in-flight jobs complete (for graceful shutdown)
	Wait()
}
