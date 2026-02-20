package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/redhatinsights/platform-go-middlewares/v2/identity"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/ports"
)

// setupTestRouter creates a test router with the given job service
func setupTestRouter(jobService ports.AuthorizedJobService) *mux.Router {
	router := mux.NewRouter()
	handler := NewJobHandler(jobService)

	api := router.PathPrefix("/api/scheduler/v1").Subrouter()

	// Job CRUD operations
	api.HandleFunc("/jobs", handler.CreateJob).Methods("POST")
	api.HandleFunc("/jobs", handler.GetAllJobs).Methods("GET")
	api.HandleFunc("/jobs/{id}", handler.GetJob).Methods("GET")
	api.HandleFunc("/jobs/{id}", handler.UpdateJob).Methods("PUT")
	api.HandleFunc("/jobs/{id}", handler.PatchJob).Methods("PATCH")
	api.HandleFunc("/jobs/{id}", handler.DeleteJob).Methods("DELETE")

	// Job control operations
	api.HandleFunc("/jobs/{id}/run", handler.RunJob).Methods("POST")
	api.HandleFunc("/jobs/{id}/pause", handler.PauseJob).Methods("POST")
	api.HandleFunc("/jobs/{id}/resume", handler.ResumeJob).Methods("POST")

	return router
}

// TestHTTPAPI_CreateJob_Contract verifies the HTTP API contract for creating a job
func TestHTTPAPI_CreateJob_Contract(t *testing.T) {
	// Create mock service
	nextRunAt := time.Date(2026, 2, 21, 14, 0, 0, 0, time.UTC)
	mockService := &mockAuthorizedJobService{
		createJobFunc: func(ctx context.Context, ident identity.XRHID, name, schedule, timezone string, payloadType domain.PayloadType, payload interface{}) (domain.Job, error) {
			return domain.Job{
				ID:        "job-123",
				Name:      name,
				OrgID:     ident.Identity.OrgID,
				UserID:    ident.Identity.User.UserID,
				Username:  ident.Identity.User.Username,
				Schedule:  domain.Schedule(schedule),
				Timezone:  timezone,
				Type:      payloadType,
				Status:    domain.StatusScheduled,
				NextRunAt: &nextRunAt,
			}, nil
		},
	}

	// Create router with handler
	router := setupTestRouter(mockService)

	// Create test request body
	requestBody := map[string]interface{}{
		"name":     "Test Job",
		"schedule": "0 9 * * *",
		"timezone": "America/New_York",
		"type":     "export",
		"payload":  map[string]interface{}{"key": "value"},
	}
	body, _ := json.Marshal(requestBody)

	// Create HTTP request
	req := httptest.NewRequest("POST", "/api/scheduler/v1/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Add identity to context (simulating middleware)
	testIdent := identity.XRHID{
		Identity: identity.Identity{
			OrgID: "org-123",
			User: &identity.User{
				Username: "testuser",
				UserID:   "user-123",
			},
		},
	}
	ctx := identity.WithIdentity(req.Context(), testIdent)
	req = req.WithContext(ctx)

	// Create response recorder
	rr := httptest.NewRecorder()

	// Call router (which routes to handler)
	router.ServeHTTP(rr, req)

	// Verify HTTP status code
	if rr.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d", http.StatusCreated, rr.Code)
	}

	// Verify Content-Type header
	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
	}

	// Verify Location header
	location := rr.Header().Get("Location")
	expectedLocation := "/api/scheduler/v1/jobs/job-123"
	if location != expectedLocation {
		t.Errorf("Expected Location '%s', got '%s'", expectedLocation, location)
	}

	// Parse response body
	var response JobResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify response fields match API contract
	if response.ID != "job-123" {
		t.Errorf("Expected ID 'job-123', got '%s'", response.ID)
	}
	if response.Name != "Test Job" {
		t.Errorf("Expected name 'Test Job', got '%s'", response.Name)
	}
	if response.Schedule != "0 9 * * *" {
		t.Errorf("Expected schedule '0 9 * * *', got '%s'", response.Schedule)
	}
	if response.Timezone != "America/New_York" {
		t.Errorf("Expected timezone 'America/New_York', got '%s'", response.Timezone)
	}
	if response.Type != "export" {
		t.Errorf("Expected type 'export', got '%s'", response.Type)
	}
	if response.Status != "scheduled" {
		t.Errorf("Expected status 'scheduled', got '%s'", response.Status)
	}

	// Verify required fields are present
	if response.NextRunAt == nil {
		t.Error("Expected next_run_at to be present")
	}

	t.Logf("✓ HTTP API contract verified for POST /api/scheduler/v1/jobs")
}

// TestHTTPAPI_GetJob_Contract verifies the HTTP API contract for getting a job
func TestHTTPAPI_GetJob_Contract(t *testing.T) {
	lastRunAt := time.Date(2026, 2, 20, 14, 0, 0, 0, time.UTC)
	nextRunAt := time.Date(2026, 2, 21, 14, 0, 0, 0, time.UTC)

	mockService := &mockAuthorizedJobService{
		getJobFunc: func(ctx context.Context, ident identity.XRHID, id string) (domain.Job, error) {
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

	// Create router with handler (this properly sets up mux vars)
	router := setupTestRouter(mockService)

	// Create HTTP request
	req := httptest.NewRequest("GET", "/api/scheduler/v1/jobs/job-123", nil)

	// Add identity to context
	testIdent := identity.XRHID{
		Identity: identity.Identity{
			OrgID: "org-123",
			User: &identity.User{
				Username: "testuser",
				UserID:   "user-123",
			},
		},
	}
	ctx := identity.WithIdentity(req.Context(), testIdent)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Verify HTTP status code
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	// Verify Content-Type header
	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
	}

	// Parse response
	var response JobResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify all required fields are present
	requiredFields := map[string]bool{
		"id":          response.ID != "",
		"name":        response.Name != "",
		"schedule":    response.Schedule != "",
		"timezone":    response.Timezone != "",
		"type":        response.Type != "",
		"status":      response.Status != "",
		"last_run_at": response.LastRunAt != nil,
		"next_run_at": response.NextRunAt != nil,
	}

	for field, present := range requiredFields {
		if !present {
			t.Errorf("Required field '%s' is missing or empty", field)
		}
	}

	t.Logf("✓ HTTP API contract verified for GET /api/scheduler/v1/jobs/{id}")
}

// TestHTTPAPI_ListJobs_Contract verifies the HTTP API contract for listing jobs
func TestHTTPAPI_ListJobs_Contract(t *testing.T) {
	mockService := &mockAuthorizedJobService{
		listJobsFunc: func(ctx context.Context, ident identity.XRHID, statusFilter, nameFilter string, offset, limit int) ([]domain.Job, int, error) {
			jobs := []domain.Job{
				{
					ID:       "job-1",
					Name:     "Job 1",
					OrgID:    ident.Identity.OrgID,
					UserID:   ident.Identity.User.UserID,
					Username: ident.Identity.User.Username,
					Schedule: "0 9 * * *",
					Timezone: "UTC",
					Type:     domain.PayloadExport,
					Status:   domain.StatusScheduled,
				},
				{
					ID:       "job-2",
					Name:     "Job 2",
					OrgID:    ident.Identity.OrgID,
					UserID:   ident.Identity.User.UserID,
					Username: ident.Identity.User.Username,
					Schedule: "0 10 * * *",
					Timezone: "UTC",
					Type:     domain.PayloadExport,
					Status:   domain.StatusScheduled,
				},
			}
			return jobs, 2, nil
		},
	}

	// Create router with handler
	router := setupTestRouter(mockService)

	// Create HTTP request with pagination params
	req := httptest.NewRequest("GET", "/api/scheduler/v1/jobs?offset=0&limit=10", nil)

	// Add identity to context
	testIdent := identity.XRHID{
		Identity: identity.Identity{
			OrgID: "org-123",
			User: &identity.User{
				Username: "testuser",
				UserID:   "user-123",
			},
		},
	}
	ctx := identity.WithIdentity(req.Context(), testIdent)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Verify HTTP status code
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	// Parse response
	var response paginatedResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify pagination structure
	if response.Meta.Count != 2 {
		t.Errorf("Expected count 2, got %d", response.Meta.Count)
	}

	// Verify links structure
	if response.Links.First == "" {
		t.Error("Expected 'first' link to be present")
	}
	if response.Links.Last == "" {
		t.Error("Expected 'last' link to be present")
	}

	// Verify data is an array
	if response.Data == nil {
		t.Error("Expected 'data' array to be present")
	}

	t.Logf("✓ HTTP API contract verified for GET /api/scheduler/v1/jobs")
}

// TestHTTPAPI_DeleteJob_Contract verifies the HTTP API contract for deleting a job
func TestHTTPAPI_DeleteJob_Contract(t *testing.T) {
	mockService := &mockAuthorizedJobService{
		deleteJobFunc: func(ctx context.Context, ident identity.XRHID, id string) error {
			return nil
		},
	}

	// Create router with handler
	router := setupTestRouter(mockService)

	// Create HTTP request
	req := httptest.NewRequest("DELETE", "/api/scheduler/v1/jobs/job-123", nil)

	// Add identity to context
	testIdent := identity.XRHID{
		Identity: identity.Identity{
			OrgID: "org-123",
			User: &identity.User{
				Username: "testuser",
				UserID:   "user-123",
			},
		},
	}
	ctx := identity.WithIdentity(req.Context(), testIdent)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Verify HTTP status code (204 No Content)
	if rr.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d", http.StatusNoContent, rr.Code)
	}

	// Verify no response body
	if rr.Body.Len() > 0 {
		t.Errorf("Expected empty body for 204 No Content, got %d bytes", rr.Body.Len())
	}

	t.Logf("✓ HTTP API contract verified for DELETE /api/scheduler/v1/jobs/{id}")
}

// TestHTTPAPI_ErrorResponses_Contract verifies error response format
func TestHTTPAPI_ErrorResponses_Contract(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func() *mockAuthorizedJobService
		expectedStatus int
		checkError     func(t *testing.T, body []byte)
	}{
		{
			name: "404 Not Found",
			setupMock: func() *mockAuthorizedJobService {
				return &mockAuthorizedJobService{
					getJobFunc: func(ctx context.Context, ident identity.XRHID, id string) (domain.Job, error) {
						return domain.Job{}, domain.ErrJobNotFound
					},
				}
			},
			expectedStatus: http.StatusNotFound,
			checkError: func(t *testing.T, body []byte) {
				var errResp ErrorResponse
				if err := json.Unmarshal(body, &errResp); err != nil {
					t.Fatalf("Failed to parse error response: %v", err)
				}
				if len(errResp.Errors) == 0 {
					t.Error("Expected errors array to have at least one error")
				}
			},
		},
		{
			name: "400 Bad Request - Invalid Identity",
			setupMock: func() *mockAuthorizedJobService {
				return &mockAuthorizedJobService{}
			},
			expectedStatus: http.StatusBadRequest,
			checkError: func(t *testing.T, body []byte) {
				var errResp ErrorResponse
				if err := json.Unmarshal(body, &errResp); err != nil {
					t.Fatalf("Failed to parse error response: %v", err)
				}
				if len(errResp.Errors) == 0 {
					t.Error("Expected errors array to have at least one error")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := tt.setupMock()
			router := setupTestRouter(mockService)

			req := httptest.NewRequest("GET", "/api/scheduler/v1/jobs/job-123", nil)

			// For invalid identity test, don't add identity to context
			if tt.name != "400 Bad Request - Invalid Identity" {
				testIdent := identity.XRHID{
					Identity: identity.Identity{
						OrgID: "org-123",
						User: &identity.User{
							Username: "testuser",
							UserID:   "user-123",
						},
					},
				}
				ctx := identity.WithIdentity(req.Context(), testIdent)
				req = req.WithContext(ctx)
			}

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rr.Code)
			}

			tt.checkError(t, rr.Body.Bytes())
		})
	}

	t.Logf("✓ HTTP API error response contract verified")
}
