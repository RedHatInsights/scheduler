package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/redhatinsights/platform-go-middlewares/v2/identity"

	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/usecases"
)

// Mock job run repository for handler tests
type mockJobRunRepositoryForHandler struct {
	findByUserIDFunc func(userID string, offset, limit int) ([]domain.JobRun, int, error)
}

func (m *mockJobRunRepositoryForHandler) Save(run domain.JobRun) error {
	return nil
}

func (m *mockJobRunRepositoryForHandler) FindByID(id string) (domain.JobRun, error) {
	return domain.JobRun{}, domain.ErrJobRunNotFound
}

func (m *mockJobRunRepositoryForHandler) FindByJobID(jobID string, offset, limit int) ([]domain.JobRun, int, error) {
	return nil, 0, nil
}

func (m *mockJobRunRepositoryForHandler) FindByJobIDAndOrgID(jobID string, orgID string) ([]domain.JobRun, error) {
	return nil, nil
}

func (m *mockJobRunRepositoryForHandler) FindByUserID(userID string, offset, limit int) ([]domain.JobRun, int, error) {
	if m.findByUserIDFunc != nil {
		return m.findByUserIDFunc(userID, offset, limit)
	}
	return nil, 0, nil
}

func (m *mockJobRunRepositoryForHandler) FindAll() ([]domain.JobRun, error) {
	return nil, nil
}

func (m *mockJobRunRepositoryForHandler) CleanupOldRuns(keepPerJob int) (int64, error) {
	return 0, nil
}

// Mock job repository for handler tests
type mockJobRepositoryForHandler struct{}

func (m *mockJobRepositoryForHandler) FindByID(id string) (domain.Job, error) {
	return domain.Job{}, domain.ErrJobNotFound
}

func (m *mockJobRepositoryForHandler) FindAll() ([]domain.Job, error) {
	return nil, nil
}

func (m *mockJobRepositoryForHandler) FindByOrgID(orgID string) ([]domain.Job, error) {
	return nil, nil
}

func (m *mockJobRepositoryForHandler) FindByUserID(userID string, offset, limit int) ([]domain.Job, int, error) {
	return nil, 0, nil
}

func (m *mockJobRepositoryForHandler) Save(job domain.Job) error {
	return nil
}

func (m *mockJobRepositoryForHandler) Delete(id string) error {
	return nil
}

// testIdentityForHandler creates a test identity for handler tests
func testIdentityForHandler(userID string) identity.XRHID {
	return identity.XRHID{
		Identity: identity.Identity{
			OrgID: "org-123",
			User: &identity.User{
				Username: "testuser",
				UserID:   userID,
			},
		},
	}
}

func TestGetAllRuns_Success(t *testing.T) {
	userID := "user-123"
	expectedRuns := []domain.JobRun{
		{
			ID:     "run-1",
			JobID:  "job-1",
			Status: domain.RunStatusCompleted,
		},
		{
			ID:     "run-2",
			JobID:  "job-2",
			Status: domain.RunStatusFailed,
		},
	}

	mockRunRepo := &mockJobRunRepositoryForHandler{
		findByUserIDFunc: func(uid string, offset, limit int) ([]domain.JobRun, int, error) {
			if uid != userID {
				t.Errorf("Expected user ID %s, got %s", userID, uid)
			}
			return expectedRuns, len(expectedRuns), nil
		},
	}
	mockJobRepo := &mockJobRepositoryForHandler{}
	service := usecases.NewJobRunService(mockRunRepo, mockJobRepo)
	handler := NewJobRunHandler(service)

	router := mux.NewRouter()
	api := router.PathPrefix("/api/scheduler/v1").Subrouter()
	api.HandleFunc("/runs", handler.GetAllRuns).Methods("GET")

	req := httptest.NewRequest("GET", "/api/scheduler/v1/runs", nil)
	testIdent := testIdentityForHandler(userID)
	ctx := identity.WithIdentity(req.Context(), testIdent)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, status)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	meta, ok := response["meta"].(map[string]interface{})
	if !ok {
		t.Fatal("Response missing 'meta' field")
	}

	count, ok := meta["count"].(float64)
	if !ok {
		t.Fatal("Meta missing 'count' field")
	}

	if int(count) != len(expectedRuns) {
		t.Errorf("Expected count %d, got %d", len(expectedRuns), int(count))
	}

	data, ok := response["data"].([]interface{})
	if !ok {
		t.Fatal("Response missing 'data' field")
	}

	if len(data) != len(expectedRuns) {
		t.Errorf("Expected %d runs in data, got %d", len(expectedRuns), len(data))
	}
}

func TestGetAllRuns_WithPagination(t *testing.T) {
	userID := "user-456"
	offset := 10
	limit := 5

	mockRunRepo := &mockJobRunRepositoryForHandler{
		findByUserIDFunc: func(uid string, off, lim int) ([]domain.JobRun, int, error) {
			if off != offset {
				t.Errorf("Expected offset %d, got %d", offset, off)
			}
			if lim != limit {
				t.Errorf("Expected limit %d, got %d", limit, lim)
			}
			return []domain.JobRun{
				{ID: "run-11", JobID: "job-1"},
			}, 50, nil
		},
	}
	mockJobRepo := &mockJobRepositoryForHandler{}
	service := usecases.NewJobRunService(mockRunRepo, mockJobRepo)
	handler := NewJobRunHandler(service)

	router := mux.NewRouter()
	api := router.PathPrefix("/api/scheduler/v1").Subrouter()
	api.HandleFunc("/runs", handler.GetAllRuns).Methods("GET")

	req := httptest.NewRequest("GET", "/api/scheduler/v1/runs?offset=10&limit=5", nil)
	testIdent := testIdentityForHandler(userID)
	ctx := identity.WithIdentity(req.Context(), testIdent)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, status)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	meta := response["meta"].(map[string]interface{})
	count := int(meta["count"].(float64))
	if count != 50 {
		t.Errorf("Expected total count 50, got %d", count)
	}
}

func TestGetAllRuns_InvalidIdentity(t *testing.T) {
	mockRunRepo := &mockJobRunRepositoryForHandler{}
	mockJobRepo := &mockJobRepositoryForHandler{}
	service := usecases.NewJobRunService(mockRunRepo, mockJobRepo)
	handler := NewJobRunHandler(service)

	router := mux.NewRouter()
	api := router.PathPrefix("/api/scheduler/v1").Subrouter()
	api.HandleFunc("/runs", handler.GetAllRuns).Methods("GET")

	req := httptest.NewRequest("GET", "/api/scheduler/v1/runs", nil)

	// Add identity with missing user info
	invalidIdent := identity.XRHID{
		Identity: identity.Identity{
			OrgID: "org-123",
			User:  nil, // Missing user
		},
	}
	ctx := identity.WithIdentity(req.Context(), invalidIdent)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Should return 400 Bad Request
	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, status)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	errors, ok := response["errors"].([]interface{})
	if !ok || len(errors) == 0 {
		t.Fatal("Expected errors in response")
	}
}

func TestGetAllRuns_EmptyResult(t *testing.T) {
	userID := "user-no-runs"

	mockRunRepo := &mockJobRunRepositoryForHandler{
		findByUserIDFunc: func(uid string, offset, limit int) ([]domain.JobRun, int, error) {
			return []domain.JobRun{}, 0, nil
		},
	}
	mockJobRepo := &mockJobRepositoryForHandler{}
	service := usecases.NewJobRunService(mockRunRepo, mockJobRepo)
	handler := NewJobRunHandler(service)

	router := mux.NewRouter()
	api := router.PathPrefix("/api/scheduler/v1").Subrouter()
	api.HandleFunc("/runs", handler.GetAllRuns).Methods("GET")

	req := httptest.NewRequest("GET", "/api/scheduler/v1/runs", nil)
	testIdent := testIdentityForHandler(userID)
	ctx := identity.WithIdentity(req.Context(), testIdent)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, status)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	meta := response["meta"].(map[string]interface{})
	count := int(meta["count"].(float64))
	if count != 0 {
		t.Errorf("Expected count 0, got %d", count)
	}

	data := response["data"].([]interface{})
	if len(data) != 0 {
		t.Errorf("Expected empty data array, got %d items", len(data))
	}
}
