package executor

import (
	"testing"

	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/identity"
)

func TestDefaultJobExecutor_ExecuteWithKafka(t *testing.T) {
	// Create test config
	cfg := &config.Config{
		ExportService: config.ExportServiceConfig{
			BaseURL: "http://localhost:9000/api/export/v1",
		},
	}

	// Create a fake user validator
	userValidator := identity.NewFakeUserValidator()

	// Create null notifier (test null object pattern)
	notifier := NewNullJobCompletionNotifier()

	// Create payload-specific executors
	executors := map[domain.PayloadType]JobExecutor{
		domain.PayloadMessage:     NewMessageJobExecutor(),
		domain.PayloadHTTPRequest: NewHTTPJobExecutor(),
		domain.PayloadCommand:     NewCommandJobExecutor(),
		domain.PayloadExport:      NewExportJobExecutor(cfg, userValidator, notifier),
	}

	// Create executor with map of executors
	executor := NewJobExecutor(executors, nil)

	// Create a test job
	payload := map[string]interface{}{
		"message": "test message",
	}

	job := domain.NewJob("Test Job", "test-org-123", "testuser", "test-user-id", "*/15 * * * *", domain.PayloadMessage, payload)

	// Test executing a message job (should not trigger notification)
	err := executor.Execute(job)
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}
}

func TestExportCompletionNotificationStructure(t *testing.T) {
	// Test that we can create the notification structure correctly
	notification := &ExportCompletionNotification{
		ExportID:    "export-123",
		JobID:       "job-456",
		OrgID:       "org-789",
		AccountID:   "account-123",
		Username:    "testuser",
		UserID:      "user-123",
		Status:      "complete",
		DownloadURL: "https://example.com/exports/export-123",
		ErrorMsg:    "",
	}

	if notification.ExportID != "export-123" {
		t.Errorf("Expected ExportID 'export-123', got %s", notification.ExportID)
	}

	if notification.JobID != "job-456" {
		t.Errorf("Expected JobID 'job-456', got %s", notification.JobID)
	}

	if notification.OrgID != "org-789" {
		t.Errorf("Expected OrgID 'org-789', got %s", notification.OrgID)
	}

	if notification.Username != "testuser" {
		t.Errorf("Expected Username 'testuser', got %s", notification.Username)
	}

	if notification.UserID != "user-123" {
		t.Errorf("Expected UserID 'user-123', got %s", notification.UserID)
	}

	if notification.Status != "complete" {
		t.Errorf("Expected Status 'complete', got %s", notification.Status)
	}

	if notification.DownloadURL != "https://example.com/exports/export-123" {
		t.Errorf("Expected DownloadURL 'https://example.com/exports/export-123', got %s", notification.DownloadURL)
	}
}
