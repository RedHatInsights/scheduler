package executor

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"insights-scheduler/internal/clients/export"
	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/identity"
)

type recordingNotifier struct {
	mu            sync.Mutex
	notifications []*ExportCompletionNotification
}

func (n *recordingNotifier) JobComplete(_ context.Context, notification *ExportCompletionNotification, _ *slog.Logger) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.notifications = append(n.notifications, notification)
	return nil
}

func (n *recordingNotifier) JobAutoPaused(_ context.Context, _ *JobAutoPausedNotification, _ *slog.Logger) error {
	return nil
}

func (n *recordingNotifier) getNotifications() []*ExportCompletionNotification {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.notifications
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

func TestExportJobExecutor_SuccessfulExport(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" && r.URL.Path == "/exports" {
			json.NewEncoder(w).Encode(export.ExportStatusResponse{
				ID:     "exp-success-123",
				Status: export.StatusPending,
			})
			return
		}
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/status") {
			callCount++
			json.NewEncoder(w).Encode(export.ExportStatusResponse{
				ID:     "exp-success-123",
				Status: export.StatusComplete,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	notifier := &recordingNotifier{}
	cfg := &config.Config{
		ExportService: config.ExportServiceConfig{
			BaseURL:        server.URL,
			PublicBaseURL:  "https://console.example.com/api/export/v1",
			PollMaxRetries: 5,
			PollInterval:   1 * time.Millisecond,
		},
	}

	executor := NewExportJobExecutor(cfg, identity.NewFakeUserValidator(), notifier)
	result, resultType, err := executor.Execute(newTestJob(), newTestLogger())

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if resultType != domain.ResultTypeExport {
		t.Errorf("Expected result type %s, got %s", domain.ResultTypeExport, resultType)
	}

	exportResult, ok := result.(domain.ExportResult)
	if !ok {
		t.Fatalf("Expected ExportResult, got %T", result)
	}
	if exportResult.ExportID != "exp-success-123" {
		t.Errorf("Expected export ID 'exp-success-123', got %s", exportResult.ExportID)
	}
	if !strings.Contains(exportResult.URL, "exp-success-123") {
		t.Errorf("Expected URL to contain export ID, got %s", exportResult.URL)
	}

	notifications := notifier.getNotifications()
	if len(notifications) != 1 {
		t.Fatalf("Expected 1 notification, got %d", len(notifications))
	}
	if notifications[0].Status != "complete" {
		t.Errorf("Expected notification status 'complete', got %s", notifications[0].Status)
	}
	if notifications[0].DownloadURL == "" {
		t.Error("Expected non-empty download URL in notification")
	}
}

func TestExportJobExecutor_ExportFailed_IncludesExportIDAndSendsNotification(t *testing.T) {
	sourceErr := "advisor service unavailable"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" && r.URL.Path == "/exports" {
			json.NewEncoder(w).Encode(export.ExportStatusResponse{
				ID:     "exp-fail-456",
				Status: export.StatusPending,
			})
			return
		}
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/status") {
			json.NewEncoder(w).Encode(export.ExportStatusResponse{
				ID:     "exp-fail-456",
				Status: export.StatusFailed,
				Sources: []export.SourceStatus{
					{
						Application: export.AppAdvisor,
						Resource:    "recommendations",
						Status:      "failed",
						Error:       &sourceErr,
					},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	notifier := &recordingNotifier{}
	cfg := &config.Config{
		ExportService: config.ExportServiceConfig{
			BaseURL:        server.URL,
			PublicBaseURL:  server.URL,
			PollMaxRetries: 3,
			PollInterval:   1 * time.Millisecond,
		},
	}

	executor := NewExportJobExecutor(cfg, identity.NewFakeUserValidator(), notifier)
	_, _, err := executor.Execute(newTestJob(), newTestLogger())

	if err == nil {
		t.Fatal("Expected error for failed export")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "exp-fail-456") {
		t.Errorf("Error should contain export ID, got: %s", errMsg)
	}

	// Should have sent a failure notification
	notifications := notifier.getNotifications()
	if len(notifications) != 1 {
		t.Fatalf("Expected 1 failure notification, got %d", len(notifications))
	}
	if notifications[0].Status != "failed" {
		t.Errorf("Expected notification status 'failed', got %s", notifications[0].Status)
	}
	if notifications[0].ExportID != "exp-fail-456" {
		t.Errorf("Expected notification export ID 'exp-fail-456', got %s", notifications[0].ExportID)
	}
	if !strings.Contains(notifications[0].ErrorMsg, "advisor service unavailable") {
		t.Errorf("Expected notification error message to contain source error, got: %s", notifications[0].ErrorMsg)
	}
}

func TestExportJobExecutor_PollTimeout_IncludesExportID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" && r.URL.Path == "/exports" {
			json.NewEncoder(w).Encode(export.ExportStatusResponse{
				ID:     "exp-timeout-789",
				Status: export.StatusPending,
			})
			return
		}
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/status") {
			json.NewEncoder(w).Encode(export.ExportStatusResponse{
				ID:     "exp-timeout-789",
				Status: export.StatusRunning,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	notifier := &recordingNotifier{}
	cfg := &config.Config{
		ExportService: config.ExportServiceConfig{
			BaseURL:        server.URL,
			PublicBaseURL:  server.URL,
			PollMaxRetries: 2,
			PollInterval:   1 * time.Millisecond,
		},
	}

	executor := NewExportJobExecutor(cfg, identity.NewFakeUserValidator(), notifier)
	_, _, err := executor.Execute(newTestJob(), newTestLogger())

	if err == nil {
		t.Fatal("Expected error for timed-out export")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "exp-timeout-789") {
		t.Errorf("Error should contain export ID, got: %s", errMsg)
	}

	// No notification should be sent for timeout (no status response returned)
	notifications := notifier.getNotifications()
	if len(notifications) != 0 {
		t.Errorf("Expected 0 notifications for timeout (no status), got %d", len(notifications))
	}
}

func TestExportJobExecutor_CreateExportFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(export.ErrorResponse{
			Error:   "invalid_request",
			Message: "name is required",
		})
	}))
	defer server.Close()

	notifier := &recordingNotifier{}
	cfg := &config.Config{
		ExportService: config.ExportServiceConfig{
			BaseURL:        server.URL,
			PublicBaseURL:  server.URL,
			PollMaxRetries: 3,
			PollInterval:   1 * time.Millisecond,
		},
	}

	executor := NewExportJobExecutor(cfg, identity.NewFakeUserValidator(), notifier)
	_, _, err := executor.Execute(newTestJob(), newTestLogger())

	if err == nil {
		t.Fatal("Expected error for failed creation")
	}

	if !strings.Contains(err.Error(), "failed to create export") {
		t.Errorf("Error should mention creation failure, got: %s", err.Error())
	}

	// No notification for creation failures
	notifications := notifier.getNotifications()
	if len(notifications) != 0 {
		t.Errorf("Expected 0 notifications for creation failure, got %d", len(notifications))
	}
}

func TestExtractExportSourceErrors(t *testing.T) {
	err1 := "source error message"

	tests := []struct {
		name     string
		sources  []export.SourceStatus
		expected string
	}{
		{
			name:     "no sources returns default",
			sources:  nil,
			expected: "Export processing failed",
		},
		{
			name: "nil error pointer returns default",
			sources: []export.SourceStatus{
				{Application: export.AppAdvisor, Resource: "recs", Error: nil},
			},
			expected: "Export processing failed",
		},
		{
			name: "returns first source error",
			sources: []export.SourceStatus{
				{Application: export.AppAdvisor, Resource: "recs", Error: &err1},
			},
			expected: "source error message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractExportSourceErrors(tt.sources)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}
