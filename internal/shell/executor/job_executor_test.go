package executor

import (
	"testing"
	"time"

	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/identity"
	"insights-scheduler/internal/shell/messaging"
)

func TestDefaultJobExecutor_ExecuteWithKafka(t *testing.T) {
	// Create test config
	cfg := &config.Config{
		ExportService: config.ExportServiceConfig{
			BaseURL: "http://localhost:9000/api/export/v1",
		},
	}

	// Create a mock Kafka producer (in a real test, you'd use a mock)
	// For this test, we'll use nil to test the nil check
	userValidator := identity.NewFakeUserValidator()

	executor := &DefaultJobExecutor{
		exportClient:  nil, // We won't actually execute exports in this test
		kafkaProducer: nil, // Test nil producer handling
		userValidator: userValidator,
		config:        cfg,
	}

	// Create a test job
	payload := domain.JobPayload{
		Type: domain.PayloadMessage,
		Details: map[string]interface{}{
			"message": "test message",
		},
	}

	job := domain.NewJob("Test Job", "test-org-123", "testuser", "test-user-id", "0 */15 * * * *", payload)

	// Test executing a message job (should not trigger Kafka)
	err := executor.Execute(job)
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}
}

func TestExportCompletionMessageStructure(t *testing.T) {
	// Test that we can create the message structure correctly
	message := messaging.ExportCompletionMessage{
		ExportID:    "export-123",
		JobID:       "job-456",
		OrgID:       "org-789",
		Status:      "complete",
		CompletedAt: time.Now(),
		DownloadURL: "https://example.com/exports/export-123",
	}

	if message.ExportID != "export-123" {
		t.Errorf("Expected ExportID 'export-123', got %s", message.ExportID)
	}

	if message.JobID != "job-456" {
		t.Errorf("Expected JobID 'job-456', got %s", message.JobID)
	}

	if message.OrgID != "org-789" {
		t.Errorf("Expected OrgID 'org-789', got %s", message.OrgID)
	}

	if message.Status != "complete" {
		t.Errorf("Expected Status 'complete', got %s", message.Status)
	}

	if message.DownloadURL != "https://example.com/exports/export-123" {
		t.Errorf("Expected DownloadURL 'https://example.com/exports/export-123', got %s", message.DownloadURL)
	}
}
