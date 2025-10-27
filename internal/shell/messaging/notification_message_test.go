package messaging

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewExportCompletionNotification(t *testing.T) {
	exportID := "export-123"
	jobID := "job-456"
	accountID := "account-789"
	orgID := "org-012"
	status := "completed"
	downloadURL := "https://example.com/exports/export-123"
	errorMsg := ""

	notification := NewExportCompletionNotification(
		exportID, jobID, accountID, orgID, status, downloadURL, errorMsg,
	)

	// Test basic fields
	if notification.Version != "v1.2.0" {
		t.Errorf("Expected version 'v1.2.0', got %s", notification.Version)
	}

	if notification.Bundle != "rhel" {
		t.Errorf("Expected bundle 'rhel', got %s", notification.Bundle)
	}

	if notification.Application != "insights-scheduler" {
		t.Errorf("Expected application 'insights-scheduler', got %s", notification.Application)
	}

	if notification.EventType != "export-completed" {
		t.Errorf("Expected event type 'export-completed', got %s", notification.EventType)
	}

	if notification.AccountID != accountID {
		t.Errorf("Expected account ID '%s', got %s", accountID, notification.AccountID)
	}

	if notification.OrgID != orgID {
		t.Errorf("Expected org ID '%s', got %s", orgID, notification.OrgID)
	}

	// Test timestamp format
	_, err := time.Parse(time.RFC3339, notification.Timestamp)
	if err != nil {
		t.Errorf("Invalid timestamp format: %s", notification.Timestamp)
	}

	// Test context
	if notification.Context["export_id"] != exportID {
		t.Errorf("Expected context export_id '%s', got %v", exportID, notification.Context["export_id"])
	}

	if notification.Context["job_id"] != jobID {
		t.Errorf("Expected context job_id '%s', got %v", jobID, notification.Context["job_id"])
	}

	if notification.Context["status"] != status {
		t.Errorf("Expected context status '%s', got %v", status, notification.Context["status"])
	}

	if notification.Context["download_url"] != downloadURL {
		t.Errorf("Expected context download_url '%s', got %v", downloadURL, notification.Context["download_url"])
	}

	// Test that error_message is not present for successful export
	if _, exists := notification.Context["error_message"]; exists {
		t.Error("Expected error_message to not be present for successful export")
	}

	// Test empty arrays
	if len(notification.Events) != 0 {
		t.Errorf("Expected empty events array, got length %d", len(notification.Events))
	}

	if len(notification.Recipients) != 0 {
		t.Errorf("Expected empty recipients array, got length %d", len(notification.Recipients))
	}
}

func TestNewExportCompletionNotificationWithError(t *testing.T) {
	exportID := "export-123"
	jobID := "job-456"
	accountID := "account-789"
	orgID := "org-012"
	status := "failed"
	downloadURL := ""
	errorMsg := "Export processing failed due to timeout"

	notification := NewExportCompletionNotification(
		exportID, jobID, accountID, orgID, status, downloadURL, errorMsg,
	)

	// Test event type for failed export
	if notification.EventType != "export-failed" {
		t.Errorf("Expected event type 'export-failed', got %s", notification.EventType)
	}

	// Test that error_message is present
	if notification.Context["error_message"] != errorMsg {
		t.Errorf("Expected context error_message '%s', got %v", errorMsg, notification.Context["error_message"])
	}
}

func TestNotificationMessageJSON(t *testing.T) {
	notification := NewExportCompletionNotification(
		"export-123", "job-456", "account-789", "org-012", "completed",
		"https://example.com/export", "",
	)

	// Test ToJSON
	jsonBytes, err := notification.ToJSON()
	if err != nil {
		t.Errorf("Failed to convert to JSON: %v", err)
	}

	// Test that we can unmarshal it back
	var unmarshaled NotificationMessage
	err = json.Unmarshal(jsonBytes, &unmarshaled)
	if err != nil {
		t.Errorf("Failed to unmarshal JSON: %v", err)
	}

	// Verify key fields
	if unmarshaled.Version != notification.Version {
		t.Errorf("Version mismatch after JSON round-trip")
	}

	if unmarshaled.EventType != notification.EventType {
		t.Errorf("EventType mismatch after JSON round-trip")
	}

	if unmarshaled.OrgID != notification.OrgID {
		t.Errorf("OrgID mismatch after JSON round-trip")
	}

	// Test ToJSONString
	jsonString, err := notification.ToJSONString()
	if err != nil {
		t.Errorf("Failed to convert to JSON string: %v", err)
	}

	if len(jsonString) == 0 {
		t.Error("JSON string is empty")
	}
}

func TestNotificationMessageStructure(t *testing.T) {
	notification := NewExportCompletionNotification(
		"export-123", "job-456", "account-789", "org-012", "completed",
		"https://example.com/export", "",
	)

	jsonBytes, err := notification.ToJSON()
	if err != nil {
		t.Fatalf("Failed to convert to JSON: %v", err)
	}

	// Parse as generic map to verify structure
	var data map[string]interface{}
	err = json.Unmarshal(jsonBytes, &data)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	// Verify required top-level fields exist
	requiredFields := []string{"version", "bundle", "application", "event_type", "timestamp", "account_id", "org_id", "context", "events", "recipients"}
	for _, field := range requiredFields {
		if _, exists := data[field]; !exists {
			t.Errorf("Required field '%s' is missing from JSON", field)
		}
	}

	// Verify context structure
	context, ok := data["context"].(map[string]interface{})
	if !ok {
		t.Fatal("Context is not a map")
	}

	contextFields := []string{"export_id", "job_id", "status", "download_url"}
	for _, field := range contextFields {
		if _, exists := context[field]; !exists {
			t.Errorf("Required context field '%s' is missing", field)
		}
	}

	// Verify arrays are present and empty
	events, ok := data["events"].([]interface{})
	if !ok {
		t.Error("Events is not an array")
	} else if len(events) != 0 {
		t.Error("Events array should be empty")
	}

	recipients, ok := data["recipients"].([]interface{})
	if !ok {
		t.Error("Recipients is not an array")
	} else if len(recipients) != 0 {
		t.Error("Recipients array should be empty")
	}
}
