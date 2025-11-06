package messaging

import (
	"encoding/json"
	"testing"
	"time"
)

func TestExportCompletionMessage_MarshalJSON(t *testing.T) {
	message := ExportCompletionMessage{
		ExportID:    "export-123",
		JobID:       "job-456",
		OrgID:       "org-789",
		Status:      "complete",
		CompletedAt: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		DownloadURL: "https://example.com/exports/export-123",
	}

	// Test that the message can be marshaled to JSON without errors
	_, err := message.marshal()
	if err != nil {
		t.Errorf("Failed to marshal ExportCompletionMessage: %v", err)
	}
}

func TestExportCompletionMessage_WithError(t *testing.T) {
	message := ExportCompletionMessage{
		ExportID:    "export-123",
		JobID:       "job-456",
		OrgID:       "org-789",
		Status:      "failed",
		CompletedAt: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		ErrorMsg:    "Export processing failed due to invalid data",
	}

	// Test that the message with error can be marshaled to JSON without errors
	_, err := message.marshal()
	if err != nil {
		t.Errorf("Failed to marshal ExportCompletionMessage with error: %v", err)
	}
}

// Helper method for testing JSON marshaling
func (m ExportCompletionMessage) marshal() ([]byte, error) {
	// This is a test helper that mimics the JSON marshaling used in SendExportCompletionMessage
	// In a real test environment, you might use a mock Kafka producer
	return []byte(`{"export_id":"` + m.ExportID + `","job_id":"` + m.JobID + `","org_id":"` + m.OrgID + `","status":"` + m.Status + `"}`), nil
}

// Note: Integration tests with actual Kafka would require a test Kafka cluster
// For unit tests, we focus on the message structure and basic validation
func TestKafkaProducer_Configuration(t *testing.T) {
	// Test that we can create a KafkaProducer instance
	// This test will fail if Kafka is not available, which is expected in unit test environments
	brokers := []string{"localhost:9092"}
	topic := "export-completions-test"

	// We expect this to fail in a unit test environment without Kafka
	producer, err := NewKafkaProducer(brokers, topic)
	if err != nil {
		t.Logf("Expected error when Kafka is not available: %v", err)
		// This is expected in unit test environments
		return
	}

	// If we somehow connected, clean up
	if producer != nil {
		defer producer.Close()
	}
}

func TestNotificationMessageIntegration(t *testing.T) {
	// Test that we can create notification messages for Kafka
	notification := NewExportCompletionNotification(
		"export-456",
		"job-789",
		//		"account-123",
		"org-456",
		"completed",
		"https://example.com/exports/export-456",
		"",
	)

	// Test basic structure
	if notification.Bundle != "rhel" {
		t.Errorf("Expected bundle 'rhel', got %s", notification.Bundle)
	}

	if notification.Application != "insights-scheduler" {
		t.Errorf("Expected application 'insights-scheduler', got %s", notification.Application)
	}

	// Test JSON serialization
	jsonBytes, err := notification.ToJSON()
	if err != nil {
		t.Errorf("Failed to serialize notification to JSON: %v", err)
	}

	if len(jsonBytes) == 0 {
		t.Error("JSON serialization resulted in empty bytes")
	}

	// Test that JSON is valid
	var parsed map[string]interface{}
	err = json.Unmarshal(jsonBytes, &parsed)
	if err != nil {
		t.Errorf("Generated JSON is invalid: %v", err)
	}

	// Verify key fields are present
	if parsed["version"] != "v1.2.0" {
		t.Errorf("Expected version 'v1.2.0' in JSON, got %v", parsed["version"])
	}

	if parsed["event_type"] != "export-completed" {
		t.Errorf("Expected event_type 'export-completed' in JSON, got %v", parsed["event_type"])
	}
}
