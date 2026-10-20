package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/redhatinsights/platform-go-middlewares/v2/identity"
	"insights-scheduler/internal/core/domain"
)

// TestHTTPAPI_UpdateJob_CannotSetStatusToRunning verifies that PUT endpoint rejects status "running"
func TestHTTPAPI_UpdateJob_CannotSetStatusToRunning(t *testing.T) {
	jobID := "550e8400-e29b-41d4-a716-446655440020"

	mockService := &mockAuthorizedJobService{
		updateJobFunc: func(ctx context.Context, ident identity.XRHID, id, name, schedule string, payloadType domain.PayloadType, payload interface{}, status string) (domain.Job, error) {
			// Service should reject this and return error
			if status == "running" {
				return domain.Job{}, domain.ErrInvalidStatusTransition
			}
			return domain.Job{}, nil
		},
	}

	router := setupTestRouter(mockService)

	// Create request body with status "running"
	requestBody := map[string]interface{}{
		"name":     "Test Job",
		"schedule": "*/10 * * * *",
		"timezone": "UTC",
		"type":     "message",
		"payload":  map[string]interface{}{"message": "test"},
		"status":   "running", // This should be rejected
	}
	body, _ := json.Marshal(requestBody)

	req := httptest.NewRequest("PUT", "/api/scheduler/v1/jobs/"+jobID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

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

	// Verify HTTP status code is 400 Bad Request
	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}

	// Parse error response
	var errResp ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("Failed to parse error response: %v", err)
	}

	// Verify error structure
	if len(errResp.Errors) == 0 {
		t.Fatal("Expected at least one error in response")
	}

	if errResp.Errors[0].Status != "400" {
		t.Errorf("Expected error status '400', got '%s'", errResp.Errors[0].Status)
	}

	t.Logf("✓ PUT /jobs/{id} correctly rejects status 'running' with 400 Bad Request")
}

// TestHTTPAPI_UpdateJob_CannotSetStatusToFailed verifies that PUT endpoint rejects status "failed"
func TestHTTPAPI_UpdateJob_CannotSetStatusToFailed(t *testing.T) {
	jobID := "550e8400-e29b-41d4-a716-446655440021"

	mockService := &mockAuthorizedJobService{
		updateJobFunc: func(ctx context.Context, ident identity.XRHID, id, name, schedule string, payloadType domain.PayloadType, payload interface{}, status string) (domain.Job, error) {
			// Service should reject this and return error
			if status == "failed" {
				return domain.Job{}, domain.ErrInvalidStatusTransition
			}
			return domain.Job{}, nil
		},
	}

	router := setupTestRouter(mockService)

	// Create request body with status "failed"
	requestBody := map[string]interface{}{
		"name":     "Test Job",
		"schedule": "*/10 * * * *",
		"timezone": "UTC",
		"type":     "message",
		"payload":  map[string]interface{}{"message": "test"},
		"status":   "failed", // This should be rejected
	}
	body, _ := json.Marshal(requestBody)

	req := httptest.NewRequest("PUT", "/api/scheduler/v1/jobs/"+jobID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

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

	// Verify HTTP status code is 400 Bad Request
	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}

	// Parse error response
	var errResp ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("Failed to parse error response: %v", err)
	}

	// Verify error structure
	if len(errResp.Errors) == 0 {
		t.Fatal("Expected at least one error in response")
	}

	if errResp.Errors[0].Status != "400" {
		t.Errorf("Expected error status '400', got '%s'", errResp.Errors[0].Status)
	}

	t.Logf("✓ PUT /jobs/{id} correctly rejects status 'failed' with 400 Bad Request")
}

// TestHTTPAPI_UpdateJob_CanSetStatusToScheduled verifies that PUT endpoint accepts status "scheduled"
func TestHTTPAPI_UpdateJob_CanSetStatusToScheduled(t *testing.T) {
	jobID := "550e8400-e29b-41d4-a716-446655440022"
	nextRunAt := time.Now().Add(10 * time.Minute)

	mockService := &mockAuthorizedJobService{
		updateJobFunc: func(ctx context.Context, ident identity.XRHID, id, name, schedule string, payloadType domain.PayloadType, payload interface{}, status string) (domain.Job, error) {
			// This should succeed
			return domain.Job{
				ID:        jobID,
				Name:      name,
				OrgID:     ident.Identity.OrgID,
				UserID:    ident.Identity.User.UserID,
				Schedule:  domain.Schedule(schedule),
				Timezone:  "UTC",
				Type:      payloadType,
				Status:    domain.StatusScheduled,
				NextRunAt: &nextRunAt,
			}, nil
		},
	}

	router := setupTestRouter(mockService)

	// Create request body with status "scheduled"
	requestBody := map[string]interface{}{
		"name":     "Test Job",
		"schedule": "*/10 * * * *",
		"timezone": "UTC",
		"type":     "message",
		"payload":  map[string]interface{}{"message": "test"},
		"status":   "scheduled", // This should be accepted
	}
	body, _ := json.Marshal(requestBody)

	req := httptest.NewRequest("PUT", "/api/scheduler/v1/jobs/"+jobID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

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

	// Verify HTTP status code is 200 OK
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	// Parse response
	var response JobResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify status is scheduled
	if response.Status != "scheduled" {
		t.Errorf("Expected status 'scheduled', got '%s'", response.Status)
	}

	t.Logf("✓ PUT /jobs/{id} correctly accepts status 'scheduled' with 200 OK")
}

// TestHTTPAPI_UpdateJob_CanSetStatusToPaused verifies that PUT endpoint accepts status "paused"
func TestHTTPAPI_UpdateJob_CanSetStatusToPaused(t *testing.T) {
	jobID := "550e8400-e29b-41d4-a716-446655440023"
	nextRunAt := time.Now().Add(10 * time.Minute)

	mockService := &mockAuthorizedJobService{
		updateJobFunc: func(ctx context.Context, ident identity.XRHID, id, name, schedule string, payloadType domain.PayloadType, payload interface{}, status string) (domain.Job, error) {
			// This should succeed
			return domain.Job{
				ID:        jobID,
				Name:      name,
				OrgID:     ident.Identity.OrgID,
				UserID:    ident.Identity.User.UserID,
				Schedule:  domain.Schedule(schedule),
				Timezone:  "UTC",
				Type:      payloadType,
				Status:    domain.StatusPaused,
				NextRunAt: &nextRunAt,
			}, nil
		},
	}

	router := setupTestRouter(mockService)

	// Create request body with status "paused"
	requestBody := map[string]interface{}{
		"name":     "Test Job",
		"schedule": "*/10 * * * *",
		"timezone": "UTC",
		"type":     "message",
		"payload":  map[string]interface{}{"message": "test"},
		"status":   "paused", // This should be accepted
	}
	body, _ := json.Marshal(requestBody)

	req := httptest.NewRequest("PUT", "/api/scheduler/v1/jobs/"+jobID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

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

	// Verify HTTP status code is 200 OK
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	// Parse response
	var response JobResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify status is paused
	if response.Status != "paused" {
		t.Errorf("Expected status 'paused', got '%s'", response.Status)
	}

	t.Logf("✓ PUT /jobs/{id} correctly accepts status 'paused' with 200 OK")
}

// TestHTTPAPI_PatchJob_CannotSetStatusToRunning verifies that PATCH endpoint rejects status "running"
func TestHTTPAPI_PatchJob_CannotSetStatusToRunning(t *testing.T) {
	jobID := "550e8400-e29b-41d4-a716-446655440024"

	mockService := &mockAuthorizedJobService{
		patchJobFunc: func(ctx context.Context, ident identity.XRHID, id string, updates map[string]interface{}) (domain.Job, error) {
			// Service should reject this and return error
			if status, ok := updates["status"].(string); ok && status == "running" {
				return domain.Job{}, domain.ErrInvalidStatusTransition
			}
			return domain.Job{}, nil
		},
	}

	router := setupTestRouter(mockService)

	// Create request body with status "running"
	requestBody := map[string]interface{}{
		"status": "running", // This should be rejected
	}
	body, _ := json.Marshal(requestBody)

	req := httptest.NewRequest("PATCH", "/api/scheduler/v1/jobs/"+jobID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

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

	// Verify HTTP status code is 400 Bad Request
	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}

	// Parse error response
	var errResp ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("Failed to parse error response: %v", err)
	}

	// Verify error structure
	if len(errResp.Errors) == 0 {
		t.Fatal("Expected at least one error in response")
	}

	if errResp.Errors[0].Status != "400" {
		t.Errorf("Expected error status '400', got '%s'", errResp.Errors[0].Status)
	}

	t.Logf("✓ PATCH /jobs/{id} correctly rejects status 'running' with 400 Bad Request")
}

// TestHTTPAPI_PatchJob_CannotSetStatusToFailed verifies that PATCH endpoint rejects status "failed"
func TestHTTPAPI_PatchJob_CannotSetStatusToFailed(t *testing.T) {
	jobID := "550e8400-e29b-41d4-a716-446655440025"

	mockService := &mockAuthorizedJobService{
		patchJobFunc: func(ctx context.Context, ident identity.XRHID, id string, updates map[string]interface{}) (domain.Job, error) {
			// Service should reject this and return error
			if status, ok := updates["status"].(string); ok && status == "failed" {
				return domain.Job{}, domain.ErrInvalidStatusTransition
			}
			return domain.Job{}, nil
		},
	}

	router := setupTestRouter(mockService)

	// Create request body with status "failed"
	requestBody := map[string]interface{}{
		"status": "failed", // This should be rejected
	}
	body, _ := json.Marshal(requestBody)

	req := httptest.NewRequest("PATCH", "/api/scheduler/v1/jobs/"+jobID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

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

	// Verify HTTP status code is 400 Bad Request
	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}

	// Parse error response
	var errResp ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("Failed to parse error response: %v", err)
	}

	// Verify error structure
	if len(errResp.Errors) == 0 {
		t.Fatal("Expected at least one error in response")
	}

	if errResp.Errors[0].Status != "400" {
		t.Errorf("Expected error status '400', got '%s'", errResp.Errors[0].Status)
	}

	t.Logf("✓ PATCH /jobs/{id} correctly rejects status 'failed' with 400 Bad Request")
}

// TestHTTPAPI_PatchJob_CanSetStatusToScheduled verifies that PATCH endpoint accepts status "scheduled"
func TestHTTPAPI_PatchJob_CanSetStatusToScheduled(t *testing.T) {
	jobID := "550e8400-e29b-41d4-a716-446655440026"
	nextRunAt := time.Now().Add(10 * time.Minute)

	mockService := &mockAuthorizedJobService{
		patchJobFunc: func(ctx context.Context, ident identity.XRHID, id string, updates map[string]interface{}) (domain.Job, error) {
			// This should succeed
			return domain.Job{
				ID:        jobID,
				Name:      "Test Job",
				OrgID:     ident.Identity.OrgID,
				UserID:    ident.Identity.User.UserID,
				Schedule:  "*/10 * * * *",
				Timezone:  "UTC",
				Type:      domain.PayloadMessage,
				Status:    domain.StatusScheduled,
				NextRunAt: &nextRunAt,
			}, nil
		},
	}

	router := setupTestRouter(mockService)

	// Create request body with status "scheduled"
	requestBody := map[string]interface{}{
		"status": "scheduled", // This should be accepted
	}
	body, _ := json.Marshal(requestBody)

	req := httptest.NewRequest("PATCH", "/api/scheduler/v1/jobs/"+jobID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

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

	// Verify HTTP status code is 200 OK
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	// Parse response
	var response JobResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify status is scheduled
	if response.Status != "scheduled" {
		t.Errorf("Expected status 'scheduled', got '%s'", response.Status)
	}

	t.Logf("✓ PATCH /jobs/{id} correctly accepts status 'scheduled' with 200 OK")
}

// TestHTTPAPI_PatchJob_CanSetStatusToPaused verifies that PATCH endpoint accepts status "paused"
func TestHTTPAPI_PatchJob_CanSetStatusToPaused(t *testing.T) {
	jobID := "550e8400-e29b-41d4-a716-446655440027"
	nextRunAt := time.Now().Add(10 * time.Minute)

	mockService := &mockAuthorizedJobService{
		patchJobFunc: func(ctx context.Context, ident identity.XRHID, id string, updates map[string]interface{}) (domain.Job, error) {
			// This should succeed
			return domain.Job{
				ID:        jobID,
				Name:      "Test Job",
				OrgID:     ident.Identity.OrgID,
				UserID:    ident.Identity.User.UserID,
				Schedule:  "*/10 * * * *",
				Timezone:  "UTC",
				Type:      domain.PayloadMessage,
				Status:    domain.StatusPaused,
				NextRunAt: &nextRunAt,
			}, nil
		},
	}

	router := setupTestRouter(mockService)

	// Create request body with status "paused"
	requestBody := map[string]interface{}{
		"status": "paused", // This should be accepted
	}
	body, _ := json.Marshal(requestBody)

	req := httptest.NewRequest("PATCH", "/api/scheduler/v1/jobs/"+jobID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

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

	// Verify HTTP status code is 200 OK
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	// Parse response
	var response JobResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify status is paused
	if response.Status != "paused" {
		t.Errorf("Expected status 'paused', got '%s'", response.Status)
	}

	t.Logf("✓ PATCH /jobs/{id} correctly accepts status 'paused' with 200 OK")
}
