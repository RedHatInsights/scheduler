package ports

import (
	"context"

	"insights-scheduler/internal/core/domain"
)

// SchedulerJobService provides system-level job operations.
// Used by the background scheduler which has no user identity.
// All operations are internal system operations without authorization checks.
type SchedulerJobService interface {
	// GetJob retrieves a job by ID without authorization checks
	GetJob(ctx context.Context, id string) (domain.Job, error)

	// ListJobs returns all jobs (used by scheduler to load jobs on startup)
	ListJobs() ([]domain.Job, error)

	// ExecuteScheduledJob executes a job (called when cron fires)
	ExecuteScheduledJob(job domain.Job) error
}
