package executor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"insights-scheduler/internal/clients/export"
	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/template"
	"insights-scheduler/internal/identity"
)

func mustCELEvaluator(t *testing.T) *template.Evaluator {
	t.Helper()
	e, err := template.NewEvaluator()
	if err != nil {
		t.Fatalf("NewEvaluator() error: %v", err)
	}
	return e
}

type mockJobRunRepo struct {
	runs      map[string]domain.JobRun
	saveError error
	findError error
}

func (m *mockJobRunRepo) Save(run domain.JobRun) error {
	if m.saveError != nil {
		return m.saveError
	}
	m.runs[run.ID] = run
	return nil
}

func (m *mockJobRunRepo) FindByID(id string) (domain.JobRun, error) {
	if m.findError != nil {
		return domain.JobRun{}, m.findError
	}
	run, ok := m.runs[id]
	if !ok {
		return domain.JobRun{}, domain.ErrJobNotFound
	}
	return run, nil
}

type failingUserValidator struct {
	err error
}

func (f *failingUserValidator) ValidateUser(ctx context.Context, orgID, userID string) (bool, error) {
	return false, f.err
}

func (f *failingUserValidator) GenerateIdentityHeader(ctx context.Context, orgID, userID string) (string, error) {
	return "", f.err
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newTestJob() domain.Job {
	payload := map[string]interface{}{
		"name":   "Test Export",
		"format": "json",
		"sources": []interface{}{
			map[string]interface{}{
				"application": "advisor",
				"resource":    "recommendations",
			},
		},
	}
	return domain.NewJob("Test Export Job", "test-org", "test-user", "0 0 * * *", "UTC", domain.PayloadExport, payload)
}

func TestExportJobExecutor_SuccessfulExportCreation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/exports" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(export.ExportStatusResponse{
				ID:     "exp-123",
				Status: export.StatusPending,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	runRepo := &mockJobRunRepo{
		runs: make(map[string]domain.JobRun),
	}
	runRepo.runs["run-123"] = domain.JobRun{
		ID:     "run-123",
		JobID:  "job-123",
		Status: domain.RunStatusRunning,
	}

	cfg := &config.Config{
		ExportService: config.ExportServiceConfig{
			BaseURL:       server.URL,
			PublicBaseURL: "https://console.example.com/api/export/v1",
		},
	}

	executor := NewExportJobExecutor(cfg, identity.NewFakeUserValidator(), runRepo, mustCELEvaluator(t))
	result, resultType, err := executor.Execute(newTestJob(), "run-123", newTestLogger())

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if resultType != domain.ResultTypePending {
		t.Errorf("Expected ResultTypePending, got: %v", resultType)
	}

	if result != nil {
		t.Errorf("Expected nil result for pending exports, got: %v", result)
	}

	// Verify external job ID was saved for the poller to pick up
	savedRun := runRepo.runs["run-123"]
	if savedRun.ExternalJobID == nil {
		t.Fatal("Expected external_job_id to be saved")
	}
	if *savedRun.ExternalJobID != "exp-123" {
		t.Errorf("External job ID = %q, want %q", *savedRun.ExternalJobID, "exp-123")
	}
	if savedRun.ExternalService == nil || *savedRun.ExternalService != "export" {
		t.Errorf("External service = %v, want 'export'", savedRun.ExternalService)
	}
}

func TestExportJobExecutor_CreateExportFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "Internal server error"}`))
	}))
	defer server.Close()

	runRepo := &mockJobRunRepo{runs: make(map[string]domain.JobRun)}
	cfg := &config.Config{
		ExportService: config.ExportServiceConfig{
			BaseURL:       server.URL,
			PublicBaseURL: server.URL,
		},
	}

	executor := NewExportJobExecutor(cfg, identity.NewFakeUserValidator(), runRepo, mustCELEvaluator(t))
	_, resultType, err := executor.Execute(newTestJob(), "run-123", newTestLogger())

	if err == nil {
		t.Fatal("Expected error when CreateExport fails")
	}

	if resultType != domain.ResultTypeExport {
		t.Errorf("Expected ResultTypeExport on failure, got: %v", resultType)
	}
}

func TestExportJobExecutor_IdentityValidationFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("Should not reach export service when identity validation fails")
	}))
	defer server.Close()

	runRepo := &mockJobRunRepo{runs: make(map[string]domain.JobRun)}
	cfg := &config.Config{
		ExportService: config.ExportServiceConfig{
			BaseURL:       server.URL,
			PublicBaseURL: server.URL,
		},
	}

	validator := &failingUserValidator{err: errors.New("user not found")}
	executor := NewExportJobExecutor(cfg, validator, runRepo, mustCELEvaluator(t))
	_, resultType, err := executor.Execute(newTestJob(), "run-123", newTestLogger())

	if err == nil {
		t.Fatal("Expected error when identity validation fails")
	}

	if resultType != domain.ResultTypeExport {
		t.Errorf("Expected ResultTypeExport on failure, got: %v", resultType)
	}
}

func TestExportJobExecutor_InvalidPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Server will be called but with invalid data
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "Invalid request"}`))
	}))
	defer server.Close()

	runRepo := &mockJobRunRepo{runs: make(map[string]domain.JobRun)}
	cfg := &config.Config{
		ExportService: config.ExportServiceConfig{
			BaseURL:       server.URL,
			PublicBaseURL: server.URL,
		},
	}

	// Invalid payload - missing required fields for ExportRequest
	invalidJob := domain.NewJob("Bad Job", "org", "user", "0 0 * * *", "UTC", domain.PayloadExport, map[string]interface{}{
		"name": "test",
		// Missing format and sources
	})

	executor := NewExportJobExecutor(cfg, identity.NewFakeUserValidator(), runRepo, mustCELEvaluator(t))
	_, resultType, err := executor.Execute(invalidJob, "run-123", newTestLogger())

	if err == nil {
		t.Fatal("Expected error with invalid payload")
	}

	if resultType != domain.ResultTypeExport {
		t.Errorf("Expected ResultTypeExport on failure, got: %v", resultType)
	}
}

func TestExportJobExecutor_ExternalJobIDSaveFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/exports" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(export.ExportStatusResponse{
				ID:     "exp-456",
				Status: export.StatusPending,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	runRepo := &mockJobRunRepo{
		runs:      make(map[string]domain.JobRun),
		saveError: errors.New("database connection lost"),
	}
	runRepo.runs["run-123"] = domain.JobRun{
		ID:     "run-123",
		JobID:  "job-123",
		Status: domain.RunStatusRunning,
	}

	cfg := &config.Config{
		ExportService: config.ExportServiceConfig{
			BaseURL:       server.URL,
			PublicBaseURL: server.URL,
		},
	}

	executor := NewExportJobExecutor(cfg, identity.NewFakeUserValidator(), runRepo, mustCELEvaluator(t))
	result, resultType, err := executor.Execute(newTestJob(), "run-123", newTestLogger())

	// Returns an error when save fails (prevents orphaned runs)
	if err == nil {
		t.Fatal("Expected error when external_job_id save fails")
	}

	if resultType != domain.ResultTypeExport {
		t.Errorf("Expected ResultTypeExport on failure, got: %v", resultType)
	}

	if result != nil {
		t.Errorf("Expected nil result, got: %v", result)
	}
}

func TestExportJobExecutor_NoJobRunID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/exports" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(export.ExportStatusResponse{
				ID:     "exp-789",
				Status: export.StatusPending,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	runRepo := &mockJobRunRepo{runs: make(map[string]domain.JobRun)}
	cfg := &config.Config{
		ExportService: config.ExportServiceConfig{
			BaseURL:       server.URL,
			PublicBaseURL: server.URL,
		},
	}

	executor := NewExportJobExecutor(cfg, identity.NewFakeUserValidator(), runRepo, mustCELEvaluator(t))
	result, resultType, err := executor.Execute(newTestJob(), "", newTestLogger())

	// Should succeed even without job run ID (for backwards compatibility)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if resultType != domain.ResultTypePending {
		t.Errorf("Expected ResultTypePending, got: %v", resultType)
	}

	if result != nil {
		t.Errorf("Expected nil result, got: %v", result)
	}
}

func TestExportJobExecutor_JobRunNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/exports" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(export.ExportStatusResponse{
				ID:     "exp-999",
				Status: export.StatusPending,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	runRepo := &mockJobRunRepo{
		runs:      make(map[string]domain.JobRun),
		findError: domain.ErrJobNotFound,
	}

	cfg := &config.Config{
		ExportService: config.ExportServiceConfig{
			BaseURL:       server.URL,
			PublicBaseURL: server.URL,
		},
	}

	executor := NewExportJobExecutor(cfg, identity.NewFakeUserValidator(), runRepo, mustCELEvaluator(t))
	result, resultType, err := executor.Execute(newTestJob(), "nonexistent-run", newTestLogger())

	// Should fail (cannot find job run to save external_job_id)
	if err == nil {
		t.Fatal("Expected error when job run not found")
	}

	if resultType != domain.ResultTypeExport {
		t.Errorf("Expected ResultTypeExport on failure, got: %v", resultType)
	}

	if result != nil {
		t.Errorf("Expected nil result, got: %v", result)
	}
}

func TestExportJobExecutor_WithNilRunRepo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/exports" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(export.ExportStatusResponse{
				ID:     "exp-nil",
				Status: export.StatusPending,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := &config.Config{
		ExportService: config.ExportServiceConfig{
			BaseURL:       server.URL,
			PublicBaseURL: server.URL,
		},
	}

	executor := NewExportJobExecutor(cfg, identity.NewFakeUserValidator(), nil, mustCELEvaluator(t))
	result, resultType, err := executor.Execute(newTestJob(), "run-123", newTestLogger())

	// Should succeed even with nil runRepo
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if resultType != domain.ResultTypePending {
		t.Errorf("Expected ResultTypePending, got: %v", resultType)
	}

	if result != nil {
		t.Errorf("Expected nil result, got: %v", result)
	}
}

// TestExportJobExecutor_TemplatedPayload verifies CEL expressions in the payload
// are resolved before the export is created (kick-off is fire-and-forget).
func TestExportJobExecutor_TemplatedPayload(t *testing.T) {
	// Test server that captures the request body sent to the export service
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" && r.URL.Path == "/exports" {
			capturedBody, _ = io.ReadAll(r.Body)
			json.NewEncoder(w).Encode(export.ExportStatusResponse{
				ID:     "exp-template-123",
				Status: export.StatusPending,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Job with a CEL-templated payload
	payload := map[string]interface{}{
		"name":   "Templated Export",
		"format": "json",
		"sources": []interface{}{
			map[string]interface{}{
				"application": "advisor",
				"resource":    "recommendations",
				"filters": map[string]interface{}{
					"start_date": "scheduler_cel:now.first_of_last_month().format_date('2006-01-02')",
					"end_date":   "scheduler_cel:now.last_of_last_month().format_date('2006-01-02')",
				},
			},
		},
	}
	job := domain.NewJob("Templated Export Job", "test-org", "test-user", "0 0 * * *", "UTC", domain.PayloadExport, payload)

	cfg := &config.Config{
		ExportService: config.ExportServiceConfig{
			BaseURL:       server.URL,
			PublicBaseURL: "https://console.example.com/api/export/v1",
		},
	}

	executor := NewExportJobExecutor(cfg, identity.NewFakeUserValidator(), nil, mustCELEvaluator(t))
	_, resultType, err := executor.Execute(job, "", newTestLogger())

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if resultType != domain.ResultTypePending {
		t.Errorf("Expected ResultTypePending, got: %v", resultType)
	}

	// The captured request body should have resolved dates, not scheduler_cel: prefixes
	if capturedBody == nil {
		t.Fatal("Expected captured request body")
	}

	var sentReq map[string]interface{}
	if err := json.Unmarshal(capturedBody, &sentReq); err != nil {
		t.Fatalf("Failed to parse captured body: %v", err)
	}

	sources := sentReq["sources"].([]interface{})
	source := sources[0].(map[string]interface{})
	filters := source["filters"].(map[string]interface{})

	startDate := filters["start_date"].(string)
	endDate := filters["end_date"].(string)

	if strings.HasPrefix(startDate, "scheduler_cel:") {
		t.Errorf("start_date was not resolved: %s", startDate)
	}
	if strings.HasPrefix(endDate, "scheduler_cel:") {
		t.Errorf("end_date was not resolved: %s", endDate)
	}
	if len(startDate) != 10 || startDate[4] != '-' || startDate[7] != '-' {
		t.Errorf("start_date not in YYYY-MM-DD format: %s", startDate)
	}
}

// TestExportJobExecutor_InvalidTemplate verifies an invalid CEL expression fails
// the kick-off with a clear error before any export is created.
func TestExportJobExecutor_InvalidTemplate(t *testing.T) {
	payload := map[string]interface{}{
		"name":   "Bad Template",
		"format": "json",
		"sources": []interface{}{
			map[string]interface{}{
				"application": "advisor",
				"resource":    "recommendations",
				"filters": map[string]interface{}{
					"start_date": "scheduler_cel:invalid_func()",
				},
			},
		},
	}
	job := domain.NewJob("Bad Template Job", "test-org", "test-user", "0 0 * * *", "UTC", domain.PayloadExport, payload)

	cfg := &config.Config{
		ExportService: config.ExportServiceConfig{
			BaseURL:       "http://unused",
			PublicBaseURL: "http://unused",
		},
	}

	executor := NewExportJobExecutor(cfg, identity.NewFakeUserValidator(), nil, mustCELEvaluator(t))
	_, _, err := executor.Execute(job, "", newTestLogger())

	if err == nil {
		t.Fatal("Expected error for invalid CEL expression")
	}
	if !strings.Contains(err.Error(), "payload templates") {
		t.Errorf("Error should mention payload templates, got: %s", err.Error())
	}
}
