package executor

import (
	"testing"
	"time"

	"insights-scheduler-part-2/internal/config"
	"insights-scheduler-part-2/internal/core/domain"
	"insights-scheduler-part-2/internal/identity"
	"insights-scheduler-part-2/internal/shell/messaging"
)

func TestDefaultJobExecutor_ExecuteWithKafka(t *testing.T) {
	// Create test config
	cfg := &config.Config{
		ExportService: config.ExportServiceConfig{
			BaseURL:       "http://localhost:9000/api/export/v1",
			AccountNumber: "test-account",
			OrgID:         "test-org",
		},
	}

	// Create a mock Kafka producer (in a real test, you'd use a mock)
	// For this test, we'll use nil to test the nil check
	userValidator := identity.NewDefaultUserValidator(cfg.ExportService.AccountNumber)

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

func TestKafkaProducerInitialization(t *testing.T) {
	// Create test config
	cfg := &config.Config{
		ExportService: config.ExportServiceConfig{
			BaseURL:       "http://localhost:9000/api/export/v1",
			AccountNumber: "test-account",
			OrgID:         "test-org",
		},
	}

	// Test creating executor with Kafka producer
	// This will fail if Kafka is not available, which is expected in unit tests
	brokers := []string{"localhost:9092"}
	topic := "test-export-completions"

	kafkaProducer, err := messaging.NewKafkaProducer(brokers, topic)
	if err != nil {
		t.Logf("Expected error when Kafka is not available: %v", err)
		// Test creating executor without Kafka producer
		executor := NewDefaultJobExecutor(cfg)
		if executor == nil {
			t.Error("Failed to create executor without Kafka")
		}
		return
	}

	// If we somehow connected, test the executor creation
	executor := NewDefaultJobExecutorWithKafka(kafkaProducer, cfg)
	if executor == nil {
		t.Error("Failed to create executor with Kafka producer")
	}

	// Clean up
	defer kafkaProducer.Close()
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
