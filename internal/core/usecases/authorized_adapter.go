package usecases

import (
	"context"

	"github.com/redhatinsights/platform-go-middlewares/v2/identity"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/ports"
)

// AuthorizedJobServiceAdapter adapts DefaultJobService to the AuthorizedJobService interface.
// It extracts user/org information from the identity and delegates to the core service.
type AuthorizedJobServiceAdapter struct {
	core *DefaultJobService
}

// NewAuthorizedJobService creates a new AuthorizedJobServiceAdapter
func NewAuthorizedJobService(core *DefaultJobService) *AuthorizedJobServiceAdapter {
	return &AuthorizedJobServiceAdapter{core: core}
}

// Ensure it implements the interface
var _ ports.AuthorizedJobService = (*AuthorizedJobServiceAdapter)(nil)

func (a *AuthorizedJobServiceAdapter) CreateJob(ctx context.Context, ident identity.XRHID, name, schedule, timezone string, payloadType domain.PayloadType, payload interface{}) (domain.Job, error) {
	// Extract identity fields
	orgID := ident.Identity.OrgID
	username := ident.Identity.User.Username
	userID := ident.Identity.User.UserID

	// Delegate to core service
	return a.core.CreateJob(ctx, name, orgID, username, userID, schedule, timezone, payloadType, payload)
}

func (a *AuthorizedJobServiceAdapter) GetJob(ctx context.Context, ident identity.XRHID, id string) (domain.Job, error) {
	// Delegate to core service with user authorization check
	return a.core.GetJobWithUserCheck(ctx, id, ident.Identity.User.UserID)
}

func (a *AuthorizedJobServiceAdapter) ListJobs(ctx context.Context, ident identity.XRHID, statusFilter, nameFilter string, offset, limit int) ([]domain.Job, int, error) {
	// Delegate to core service with user ID from identity
	return a.core.GetJobsByUserID(ctx, ident.Identity.User.UserID, statusFilter, nameFilter, offset, limit)
}

func (a *AuthorizedJobServiceAdapter) UpdateJob(ctx context.Context, ident identity.XRHID, id, name, schedule string, payloadType domain.PayloadType, payload interface{}, status string) (domain.Job, error) {
	// Extract identity fields
	orgID := ident.Identity.OrgID
	username := ident.Identity.User.Username
	userID := ident.Identity.User.UserID

	// Delegate to core service
	return a.core.UpdateJob(ctx, id, name, orgID, username, userID, schedule, payloadType, payload, status)
}

func (a *AuthorizedJobServiceAdapter) PatchJob(ctx context.Context, ident identity.XRHID, id string, updates map[string]interface{}) (domain.Job, error) {
	// Delegate to core service with user authorization check
	return a.core.PatchJobWithUserCheck(ctx, id, ident.Identity.User.UserID, updates)
}

func (a *AuthorizedJobServiceAdapter) DeleteJob(ctx context.Context, ident identity.XRHID, id string) error {
	// Delegate to core service with user authorization check
	return a.core.DeleteJobWithUserCheck(ctx, id, ident.Identity.User.UserID)
}

func (a *AuthorizedJobServiceAdapter) RunJob(ctx context.Context, ident identity.XRHID, id string) error {
	// Delegate to core service with user authorization check
	return a.core.RunJobWithUserCheck(ctx, id, ident.Identity.User.UserID)
}

func (a *AuthorizedJobServiceAdapter) PauseJob(ctx context.Context, ident identity.XRHID, id string) (domain.Job, error) {
	// Delegate to core service with user authorization check
	return a.core.PauseJobWithUserCheck(ctx, id, ident.Identity.User.UserID)
}

func (a *AuthorizedJobServiceAdapter) ResumeJob(ctx context.Context, ident identity.XRHID, id string) (domain.Job, error) {
	// Delegate to core service with user authorization check
	return a.core.ResumeJobWithUserCheck(ctx, id, ident.Identity.User.UserID)
}
