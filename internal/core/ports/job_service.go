package ports

import (
	"context"

	"insights-scheduler/internal/core/domain"
)

// JobService defines the contract for job management operations.
// All methods accept a context as the first parameter for cancellation,
// timeouts, and request-scoped values.
type JobService interface {
	// CreateJob creates a new scheduled job
	CreateJob(ctx context.Context, name, orgID, username, userID, schedule, timezone string, payloadType domain.PayloadType, payload interface{}) (domain.Job, error)

	// GetJob retrieves a job by ID (no authorization check)
	GetJob(ctx context.Context, id string) (domain.Job, error)

	// GetJobWithUserCheck retrieves a job by ID with user authorization check
	GetJobWithUserCheck(ctx context.Context, id, userID string) (domain.Job, error)

	// GetJobWithOrgCheck retrieves a job by ID with organization authorization check
	GetJobWithOrgCheck(ctx context.Context, id, orgID string) (domain.Job, error)

	// GetJobsByUserID retrieves all jobs for a specific user with optional filtering
	GetJobsByUserID(ctx context.Context, userID, statusFilter, nameFilter string, offset, limit int) ([]domain.Job, int, error)

	// GetJobsByOrgID retrieves all jobs for a specific organization with optional filtering
	GetJobsByOrgID(ctx context.Context, orgID, statusFilter, nameFilter string, offset, limit int) ([]domain.Job, int, error)

	// UpdateJob updates an existing job (full update)
	UpdateJob(ctx context.Context, id, name, orgID, username, userID, schedule string, payloadType domain.PayloadType, payload interface{}, status string) (domain.Job, error)

	// PatchJobWithUserCheck partially updates a job with user authorization check
	PatchJobWithUserCheck(ctx context.Context, id, userID string, updates map[string]interface{}) (domain.Job, error)

	// PatchJobWithOrgCheck partially updates a job with organization authorization check
	PatchJobWithOrgCheck(ctx context.Context, id, orgID string, updates map[string]interface{}) (domain.Job, error)

	// DeleteJobWithUserCheck deletes a job with user authorization check
	DeleteJobWithUserCheck(ctx context.Context, id, userID string) error

	// DeleteJobWithOrgCheck deletes a job with organization authorization check
	DeleteJobWithOrgCheck(ctx context.Context, id, orgID string) error

	// RunJob executes a job immediately (no authorization check)
	RunJob(ctx context.Context, id string) error

	// RunJobWithUserCheck executes a job immediately with user authorization check
	RunJobWithUserCheck(ctx context.Context, id, userID string) error

	// RunJobWithOrgCheck executes a job immediately with organization authorization check
	RunJobWithOrgCheck(ctx context.Context, id, orgID string) error

	// PauseJobWithUserCheck pauses a job with user authorization check
	PauseJobWithUserCheck(ctx context.Context, id, userID string) (domain.Job, error)

	// PauseJobWithOrgCheck pauses a job with organization authorization check
	PauseJobWithOrgCheck(ctx context.Context, id, orgID string) (domain.Job, error)

	// ResumeJob resumes a paused job (no authorization check)
	ResumeJob(ctx context.Context, id string) (domain.Job, error)

	// ResumeJobWithUserCheck resumes a paused job with user authorization check
	ResumeJobWithUserCheck(ctx context.Context, id, userID string) (domain.Job, error)

	// ResumeJobWithOrgCheck resumes a paused job with organization authorization check
	ResumeJobWithOrgCheck(ctx context.Context, id, orgID string) (domain.Job, error)
}
