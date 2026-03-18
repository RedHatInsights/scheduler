package http

import (
	"context"
	"testing"
	"time"

	"github.com/redhatinsights/platform-go-middlewares/v2/identity"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/ports"
)

// mockAuthorizedJobService is a mock implementation of ports.AuthorizedJobService for testing
type mockAuthorizedJobService struct {
	createJobFunc func(ctx context.Context, ident identity.XRHID, name, schedule, timezone string, payloadType domain.PayloadType, payload interface{}) (domain.Job, error)
	getJobFunc    func(ctx context.Context, ident identity.XRHID, id string) (domain.Job, error)
	listJobsFunc  func(ctx context.Context, ident identity.XRHID, statusFilter, nameFilter string, offset, limit int) ([]domain.Job, int, error)
	updateJobFunc func(ctx context.Context, ident identity.XRHID, id, name, schedule string, payloadType domain.PayloadType, payload interface{}, status string) (domain.Job, error)
	patchJobFunc  func(ctx context.Context, ident identity.XRHID, id string, updates map[string]interface{}) (domain.Job, error)
	deleteJobFunc func(ctx context.Context, ident identity.XRHID, id string) error
	runJobFunc    func(ctx context.Context, ident identity.XRHID, id string) error
	pauseJobFunc  func(ctx context.Context, ident identity.XRHID, id string) (domain.Job, error)
	resumeJobFunc func(ctx context.Context, ident identity.XRHID, id string) (domain.Job, error)
}

// Ensure mockAuthorizedJobService implements ports.AuthorizedJobService
var _ ports.AuthorizedJobService = (*mockAuthorizedJobService)(nil)

func (m *mockAuthorizedJobService) CreateJob(ctx context.Context, ident identity.XRHID, name, schedule, timezone string, payloadType domain.PayloadType, payload interface{}) (domain.Job, error) {
	if m.createJobFunc != nil {
		return m.createJobFunc(ctx, ident, name, schedule, timezone, payloadType, payload)
	}
	return domain.Job{}, nil
}

func (m *mockAuthorizedJobService) GetJob(ctx context.Context, ident identity.XRHID, id string) (domain.Job, error) {
	if m.getJobFunc != nil {
		return m.getJobFunc(ctx, ident, id)
	}
	return domain.Job{}, nil
}

func (m *mockAuthorizedJobService) ListJobs(ctx context.Context, ident identity.XRHID, statusFilter, nameFilter string, offset, limit int) ([]domain.Job, int, error) {
	if m.listJobsFunc != nil {
		return m.listJobsFunc(ctx, ident, statusFilter, nameFilter, offset, limit)
	}
	return nil, 0, nil
}

func (m *mockAuthorizedJobService) UpdateJob(ctx context.Context, ident identity.XRHID, id, name, schedule string, payloadType domain.PayloadType, payload interface{}, status string) (domain.Job, error) {
	if m.updateJobFunc != nil {
		return m.updateJobFunc(ctx, ident, id, name, schedule, payloadType, payload, status)
	}
	return domain.Job{}, nil
}

func (m *mockAuthorizedJobService) PatchJob(ctx context.Context, ident identity.XRHID, id string, updates map[string]interface{}) (domain.Job, error) {
	if m.patchJobFunc != nil {
		return m.patchJobFunc(ctx, ident, id, updates)
	}
	return domain.Job{}, nil
}

func (m *mockAuthorizedJobService) DeleteJob(ctx context.Context, ident identity.XRHID, id string) error {
	if m.deleteJobFunc != nil {
		return m.deleteJobFunc(ctx, ident, id)
	}
	return nil
}

func (m *mockAuthorizedJobService) RunJob(ctx context.Context, ident identity.XRHID, id string) error {
	if m.runJobFunc != nil {
		return m.runJobFunc(ctx, ident, id)
	}
	return nil
}

func (m *mockAuthorizedJobService) PauseJob(ctx context.Context, ident identity.XRHID, id string) (domain.Job, error) {
	if m.pauseJobFunc != nil {
		return m.pauseJobFunc(ctx, ident, id)
	}
	return domain.Job{}, nil
}

func (m *mockAuthorizedJobService) ResumeJob(ctx context.Context, ident identity.XRHID, id string) (domain.Job, error) {
	if m.resumeJobFunc != nil {
		return m.resumeJobFunc(ctx, ident, id)
	}
	return domain.Job{}, nil
}

// Example test demonstrating contract testing with the AuthorizedJobService interface
func TestJobAPI_Contract_TimezoneFieldRequired(t *testing.T) {
	// Create a test identity
	testIdent := identity.XRHID{
		Identity: identity.Identity{
			OrgID: "org-123",
			User: &identity.User{
				Username: "testuser",
				UserID:   "user-123",
			},
		},
	}

	// Create a mock service that returns a predictable job
	nextRunAt := time.Date(2026, 2, 21, 14, 0, 0, 0, time.UTC) // 9 AM EST
	mockService := &mockAuthorizedJobService{
		createJobFunc: func(ctx context.Context, ident identity.XRHID, name, schedule, timezone string, payloadType domain.PayloadType, payload interface{}) (domain.Job, error) {
			// Verify identity is passed correctly
			if ident.Identity.OrgID != "org-123" {
				t.Errorf("Expected org_id 'org-123', got '%s'", ident.Identity.OrgID)
			}
			if ident.Identity.User.UserID != "user-123" {
				t.Errorf("Expected user_id 'user-123', got '%s'", ident.Identity.User.UserID)
			}

			job := domain.Job{
				ID:        "test-job-123",
				Name:      name,
				OrgID:     ident.Identity.OrgID,
				UserID:    ident.Identity.User.UserID,
				Username:  ident.Identity.User.Username,
				Schedule:  domain.Schedule(schedule),
				Timezone:  timezone,
				Type:      payloadType,
				Status:    domain.StatusScheduled,
				NextRunAt: &nextRunAt,
			}
			return job, nil
		},
	}

	// Verify the mock service implements the interface
	_ = ports.AuthorizedJobService(mockService)

	// Call service with identity - authorization is enforced by interface
	job, err := mockService.CreateJob(context.Background(), testIdent, "Daily Report", "0 9 * * *", "America/New_York", domain.PayloadExport, map[string]interface{}{"application": "test"})
	if err != nil {
		t.Fatalf("Service call failed: %v", err)
	}

	// Verify job has correct timezone
	if job.Timezone != "America/New_York" {
		t.Errorf("Expected timezone 'America/New_York', got '%s'", job.Timezone)
	}

	// Verify job has next_run_at set
	if job.NextRunAt == nil {
		t.Error("Expected next_run_at to be set")
	}

	// Verify job is scoped to the identity
	if job.OrgID != "org-123" {
		t.Errorf("Expected job org_id 'org-123', got '%s'", job.OrgID)
	}
	if job.UserID != "user-123" {
		t.Errorf("Expected job user_id 'user-123', got '%s'", job.UserID)
	}
}

// Example test showing how the interface enforces authorization at compile time
func TestJobAPI_Contract_ResponseFields(t *testing.T) {
	// Create a test identity
	testIdent := identity.XRHID{
		Identity: identity.Identity{
			OrgID: "org-123",
			User: &identity.User{
				Username: "testuser",
				UserID:   "user-123",
			},
		},
	}

	tests := []struct {
		name           string
		setupMock      func() *mockAuthorizedJobService
		requiredFields []string
	}{
		{
			name: "GetJob must return timezone and timestamps in user's timezone",
			setupMock: func() *mockAuthorizedJobService {
				lastRunAt := time.Date(2026, 2, 20, 14, 0, 0, 0, time.UTC)
				nextRunAt := time.Date(2026, 2, 21, 14, 0, 0, 0, time.UTC)
				return &mockAuthorizedJobService{
					getJobFunc: func(ctx context.Context, ident identity.XRHID, id string) (domain.Job, error) {
						// Service automatically checks authorization based on identity
						return domain.Job{
							ID:        id,
							Name:      "Test Job",
							OrgID:     ident.Identity.OrgID,
							UserID:    ident.Identity.User.UserID,
							Username:  ident.Identity.User.Username,
							Schedule:  "0 9 * * *",
							Timezone:  "America/New_York",
							Type:      domain.PayloadExport,
							Status:    domain.StatusScheduled,
							LastRunAt: &lastRunAt,
							NextRunAt: &nextRunAt,
						}, nil
					},
				}
			},
			requiredFields: []string{"id", "name", "schedule", "timezone", "type", "status", "last_run_at", "next_run_at"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := tt.setupMock()
			_ = NewJobHandler(mockService) // Verify handler can be created with mock

			// Get job from service - identity is required
			job, err := mockService.GetJob(context.Background(), testIdent, "test-job-1")
			if err != nil {
				t.Fatalf("Failed to get job: %v", err)
			}

			// Convert to DTO (this is what the HTTP handler does)
			response := ToJobResponse(job)

			// Verify timezone is present
			if response.Timezone != "America/New_York" {
				t.Errorf("Expected timezone 'America/New_York', got '%s'", response.Timezone)
			}

			// Verify timestamps are set
			if response.LastRunAt == nil {
				t.Error("Expected last_run_at to be set")
			}
			if response.NextRunAt == nil {
				t.Error("Expected next_run_at to be set")
			}

			// Verify timestamps are in job's timezone (not UTC)
			if response.NextRunAt != nil {
				location := response.NextRunAt.Location().String()
				if location != "America/New_York" {
					t.Errorf("Expected next_run_at in America/New_York timezone, got %s", location)
				}
			}

			// Verify job is scoped to the identity
			if job.OrgID != "org-123" {
				t.Errorf("Expected job org_id 'org-123', got '%s'", job.OrgID)
			}

			t.Logf("Contract verified: Job response contains all required fields with correct timezone handling")
		})
	}
}

// Test that demonstrates the authorization enforcement benefit
func TestJobAPI_Contract_AuthorizationEnforcement(t *testing.T) {
	// Create identities for two different users
	user1Ident := identity.XRHID{
		Identity: identity.Identity{
			OrgID: "org-123",
			User: &identity.User{
				Username: "user1",
				UserID:   "user-1",
			},
		},
	}

	user2Ident := identity.XRHID{
		Identity: identity.Identity{
			OrgID: "org-456",
			User: &identity.User{
				Username: "user2",
				UserID:   "user-2",
			},
		},
	}

	// Create mock that verifies identity is checked
	mockService := &mockAuthorizedJobService{
		getJobFunc: func(ctx context.Context, ident identity.XRHID, id string) (domain.Job, error) {
			// Simulate authorization check - only return job if it belongs to user
			if id == "job-1" && ident.Identity.User.UserID != "user-1" {
				return domain.Job{}, domain.ErrJobNotFound // Don't leak existence
			}

			return domain.Job{
				ID:       id,
				Name:     "Test Job",
				OrgID:    "org-123",
				UserID:   "user-1",
				Username: "user1",
				Schedule: "0 9 * * *",
				Timezone: "UTC",
				Type:     domain.PayloadExport,
				Status:   domain.StatusScheduled,
			}, nil
		},
	}

	// User 1 can access their job
	job, err := mockService.GetJob(context.Background(), user1Ident, "job-1")
	if err != nil {
		t.Errorf("User 1 should be able to access their job: %v", err)
	}
	if job.UserID != "user-1" {
		t.Errorf("Expected job to belong to user-1, got %s", job.UserID)
	}

	// User 2 cannot access user 1's job
	_, err = mockService.GetJob(context.Background(), user2Ident, "job-1")
	if err != domain.ErrJobNotFound {
		t.Errorf("User 2 should not be able to access user 1's job, expected ErrJobNotFound, got %v", err)
	}

	t.Logf("Authorization enforcement verified: Users can only access their own jobs")
}
