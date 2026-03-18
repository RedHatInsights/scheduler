package ports

import (
	"context"

	"github.com/redhatinsights/platform-go-middlewares/v2/identity"
	"insights-scheduler/internal/core/domain"
)

// AuthorizedJobService enforces identity-based authorization.
// Used by HTTP handlers where identity is always available from middleware.
// All operations are automatically scoped to the provided identity.
type AuthorizedJobService interface {
	// CreateJob creates a new scheduled job scoped to the provided identity
	CreateJob(ctx context.Context, ident identity.XRHID, name, schedule, timezone string, payloadType domain.PayloadType, payload interface{}) (domain.Job, error)

	// GetJob retrieves a job by ID with authorization check
	GetJob(ctx context.Context, ident identity.XRHID, id string) (domain.Job, error)

	// ListJobs retrieves all jobs for the identity with optional filtering
	ListJobs(ctx context.Context, ident identity.XRHID, statusFilter, nameFilter string, offset, limit int) ([]domain.Job, int, error)

	// UpdateJob updates an existing job with authorization check
	UpdateJob(ctx context.Context, ident identity.XRHID, id, name, schedule string, payloadType domain.PayloadType, payload interface{}, status string) (domain.Job, error)

	// PatchJob partially updates a job with authorization check
	PatchJob(ctx context.Context, ident identity.XRHID, id string, updates map[string]interface{}) (domain.Job, error)

	// DeleteJob deletes a job with authorization check
	DeleteJob(ctx context.Context, ident identity.XRHID, id string) error

	// RunJob executes a job immediately with authorization check
	RunJob(ctx context.Context, ident identity.XRHID, id string) error

	// PauseJob pauses a job with authorization check
	PauseJob(ctx context.Context, ident identity.XRHID, id string) (domain.Job, error)

	// ResumeJob resumes a paused job with authorization check
	ResumeJob(ctx context.Context, ident identity.XRHID, id string) (domain.Job, error)
}
