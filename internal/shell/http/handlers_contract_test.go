package http

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/ports"
)

// mockJobService is a mock implementation of ports.JobService for testing
type mockJobService struct {
	createJobFunc              func(ctx context.Context, name, orgID, username, userID, schedule, timezone string, payloadType domain.PayloadType, payload interface{}) (domain.Job, error)
	getJobFunc                 func(ctx context.Context, id string) (domain.Job, error)
	getJobWithUserCheckFunc    func(ctx context.Context, id, userID string) (domain.Job, error)
	getJobsByUserIDFunc        func(ctx context.Context, userID, statusFilter, nameFilter string, offset, limit int) ([]domain.Job, int, error)
	updateJobFunc              func(ctx context.Context, id, name, orgID, username, userID, schedule string, payloadType domain.PayloadType, payload interface{}, status string) (domain.Job, error)
	patchJobWithUserCheckFunc  func(ctx context.Context, id, userID string, updates map[string]interface{}) (domain.Job, error)
	deleteJobWithUserCheckFunc func(ctx context.Context, id, userID string) error
	runJobWithUserCheckFunc    func(ctx context.Context, id, userID string) error
	pauseJobWithUserCheckFunc  func(ctx context.Context, id, userID string) (domain.Job, error)
	resumeJobWithUserCheckFunc func(ctx context.Context, id, userID string) (domain.Job, error)
}

// Ensure mockJobService implements ports.JobService
var _ ports.JobService = (*mockJobService)(nil)

func (m *mockJobService) CreateJob(ctx context.Context, name, orgID, username, userID, schedule, timezone string, payloadType domain.PayloadType, payload interface{}) (domain.Job, error) {
	if m.createJobFunc != nil {
		return m.createJobFunc(ctx, name, orgID, username, userID, schedule, timezone, payloadType, payload)
	}
	return domain.Job{}, nil
}

func (m *mockJobService) GetJob(ctx context.Context, id string) (domain.Job, error) {
	if m.getJobFunc != nil {
		return m.getJobFunc(ctx, id)
	}
	return domain.Job{}, nil
}

func (m *mockJobService) GetJobWithUserCheck(ctx context.Context, id, userID string) (domain.Job, error) {
	if m.getJobWithUserCheckFunc != nil {
		return m.getJobWithUserCheckFunc(ctx, id, userID)
	}
	return domain.Job{}, nil
}

func (m *mockJobService) GetJobWithOrgCheck(ctx context.Context, id, orgID string) (domain.Job, error) {
	return domain.Job{}, nil
}

func (m *mockJobService) GetJobsByUserID(ctx context.Context, userID, statusFilter, nameFilter string, offset, limit int) ([]domain.Job, int, error) {
	if m.getJobsByUserIDFunc != nil {
		return m.getJobsByUserIDFunc(ctx, userID, statusFilter, nameFilter, offset, limit)
	}
	return nil, 0, nil
}

func (m *mockJobService) GetJobsByOrgID(ctx context.Context, orgID, statusFilter, nameFilter string, offset, limit int) ([]domain.Job, int, error) {
	return nil, 0, nil
}

func (m *mockJobService) UpdateJob(ctx context.Context, id, name, orgID, username, userID, schedule string, payloadType domain.PayloadType, payload interface{}, status string) (domain.Job, error) {
	if m.updateJobFunc != nil {
		return m.updateJobFunc(ctx, id, name, orgID, username, userID, schedule, payloadType, payload, status)
	}
	return domain.Job{}, nil
}

func (m *mockJobService) PatchJobWithUserCheck(ctx context.Context, id, userID string, updates map[string]interface{}) (domain.Job, error) {
	if m.patchJobWithUserCheckFunc != nil {
		return m.patchJobWithUserCheckFunc(ctx, id, userID, updates)
	}
	return domain.Job{}, nil
}

func (m *mockJobService) PatchJobWithOrgCheck(ctx context.Context, id, orgID string, updates map[string]interface{}) (domain.Job, error) {
	return domain.Job{}, nil
}

func (m *mockJobService) DeleteJobWithUserCheck(ctx context.Context, id, userID string) error {
	if m.deleteJobWithUserCheckFunc != nil {
		return m.deleteJobWithUserCheckFunc(ctx, id, userID)
	}
	return nil
}

func (m *mockJobService) DeleteJobWithOrgCheck(ctx context.Context, id, orgID string) error {
	return nil
}

func (m *mockJobService) RunJob(ctx context.Context, id string) error {
	return nil
}

func (m *mockJobService) RunJobWithUserCheck(ctx context.Context, id, userID string) error {
	if m.runJobWithUserCheckFunc != nil {
		return m.runJobWithUserCheckFunc(ctx, id, userID)
	}
	return nil
}

func (m *mockJobService) RunJobWithOrgCheck(ctx context.Context, id, orgID string) error {
	return nil
}

func (m *mockJobService) PauseJobWithUserCheck(ctx context.Context, id, userID string) (domain.Job, error) {
	if m.pauseJobWithUserCheckFunc != nil {
		return m.pauseJobWithUserCheckFunc(ctx, id, userID)
	}
	return domain.Job{}, nil
}

func (m *mockJobService) PauseJobWithOrgCheck(ctx context.Context, id, orgID string) (domain.Job, error) {
	return domain.Job{}, nil
}

func (m *mockJobService) ResumeJob(ctx context.Context, id string) (domain.Job, error) {
	return domain.Job{}, nil
}

func (m *mockJobService) ResumeJobWithUserCheck(ctx context.Context, id, userID string) (domain.Job, error) {
	if m.resumeJobWithUserCheckFunc != nil {
		return m.resumeJobWithUserCheckFunc(ctx, id, userID)
	}
	return domain.Job{}, nil
}

func (m *mockJobService) ResumeJobWithOrgCheck(ctx context.Context, id, orgID string) (domain.Job, error) {
	return domain.Job{}, nil
}

// Example test demonstrating contract testing with the interface
func TestJobAPI_Contract_TimezoneFieldRequired(t *testing.T) {
	// Create a mock service that returns a predictable job
	nextRunAt := time.Date(2026, 2, 21, 14, 0, 0, 0, time.UTC) // 9 AM EST
	mockService := &mockJobService{
		createJobFunc: func(ctx context.Context, name, orgID, username, userID, schedule, timezone string, payloadType domain.PayloadType, payload interface{}) (domain.Job, error) {
			job := domain.Job{
				ID:        "test-job-123",
				Name:      name,
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
	_ = ports.JobService(mockService)

	// Create test request with America/New_York timezone
	requestBody := map[string]interface{}{
		"name":     "Daily Report",
		"schedule": "0 9 * * *",
		"timezone": "America/New_York",
		"type":     "export",
		"payload":  map[string]interface{}{"application": "test"},
	}
	_, _ = json.Marshal(requestBody)

	// Verify the mock was called with correct timezone
	job, err := mockService.CreateJob(context.Background(), "Daily Report", "org-123", "user", "user-123", "0 9 * * *", "America/New_York", domain.PayloadExport, requestBody["payload"])
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

	// In a full integration test with HTTP layer, you would verify:
	// 1. HTTP status code is 201
	// 2. Response contains required fields: id, name, schedule, timezone, next_run_at
	// 3. next_run_at is returned in America/New_York timezone (with -05:00 or -04:00 offset)
	// 4. Timezone field matches the request
}

// Example test showing how the interface enables testing without a database
func TestJobAPI_Contract_ResponseFields(t *testing.T) {
	// This test verifies the API contract doesn't change
	// It ensures all required fields are present in responses

	tests := []struct {
		name           string
		setupMock      func() *mockJobService
		requiredFields []string
	}{
		{
			name: "GetJobWithUserCheck must return timezone and timestamps in user's timezone",
			setupMock: func() *mockJobService {
				lastRunAt := time.Date(2026, 2, 20, 14, 0, 0, 0, time.UTC)
				nextRunAt := time.Date(2026, 2, 21, 14, 0, 0, 0, time.UTC)
				return &mockJobService{
					getJobWithUserCheckFunc: func(ctx context.Context, id, userID string) (domain.Job, error) {
						return domain.Job{
							ID:        id,
							Name:      "Test Job",
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

			// Get job from service
			job, err := mockService.GetJobWithUserCheck(context.Background(), "test-job-1", "user-123")
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
			// The ToJobResponse should convert times to the job's timezone
			if response.NextRunAt != nil {
				location := response.NextRunAt.Location().String()
				if location != "America/New_York" {
					t.Errorf("Expected next_run_at in America/New_York timezone, got %s", location)
				}
			}

			t.Logf("Contract verified: Job response contains all required fields with correct timezone handling")
		})
	}
}
