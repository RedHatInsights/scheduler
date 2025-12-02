package http

import (
	"encoding/json"
	"testing"
	"time"

	"insights-scheduler/internal/core/domain"
)

func TestToJobResponse(t *testing.T) {
	// Create a domain job with all fields including org_id, username, user_id
	payload := domain.JobPayload{
		Details: map[string]interface{}{
			"message": "test message",
		},
	}

	job := domain.NewJob("Test Job", "org-123", "testuser", "user-123", "*/15 * * * *", domain.PayloadMessage, payload)

	// Convert to response DTO
	response := ToJobResponse(job)

	// Verify that the response has the expected fields
	if response.ID != job.ID {
		t.Errorf("Expected ID %s, got %s", job.ID, response.ID)
	}

	if response.Name != job.Name {
		t.Errorf("Expected Name %s, got %s", job.Name, response.Name)
	}

	if response.Schedule != string(job.Schedule) {
		t.Errorf("Expected Schedule %s, got %s", job.Schedule, response.Schedule)
	}

	if response.Type != string(job.Type) {
		t.Errorf("Expected Type %s, got %s", job.Type, response.Type)
	}

	if response.Status != string(job.Status) {
		t.Errorf("Expected Status %s, got %s", job.Status, response.Status)
	}

	// Marshal to JSON and verify org_id, username, user_id are NOT present
	jsonBytes, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal response to JSON: %v", err)
	}

	jsonString := string(jsonBytes)

	// Check that sensitive fields are NOT in the JSON
	if contains(jsonString, "org_id") {
		t.Error("JSON should not contain 'org_id' field")
	}

	if contains(jsonString, "username") {
		t.Error("JSON should not contain 'username' field")
	}

	if contains(jsonString, "user_id") {
		t.Error("JSON should not contain 'user_id' field")
	}

	// Check that expected fields ARE in the JSON
	if !contains(jsonString, "\"id\"") {
		t.Error("JSON should contain 'id' field")
	}

	if !contains(jsonString, "\"name\"") {
		t.Error("JSON should contain 'name' field")
	}

	if !contains(jsonString, "\"schedule\"") {
		t.Error("JSON should contain 'schedule' field")
	}

	if !contains(jsonString, "\"type\"") {
		t.Error("JSON should contain 'type' field")
	}

	if !contains(jsonString, "\"status\"") {
		t.Error("JSON should contain 'status' field")
	}
}

func TestToJobResponseList(t *testing.T) {
	// Create multiple domain jobs
	payload1 := domain.JobPayload{
		Details: map[string]interface{}{
			"message": "test message 1",
		},
	}

	payload2 := domain.JobPayload{
		Details: map[string]interface{}{
			"message": "test message 2",
		},
	}

	job1 := domain.NewJob("Test Job 1", "org-123", "testuser", "user-123", "*/15 * * * *", domain.PayloadMessage, payload1)
	job2 := domain.NewJob("Test Job 2", "org-456", "testuser2", "user-456", "0 * * * *", domain.PayloadCommand, payload2)

	jobs := []domain.Job{job1, job2}

	// Convert to response DTOs
	responses := ToJobResponseList(jobs)

	if len(responses) != 2 {
		t.Errorf("Expected 2 responses, got %d", len(responses))
	}

	// Verify first job
	if responses[0].ID != job1.ID {
		t.Errorf("Expected first job ID %s, got %s", job1.ID, responses[0].ID)
	}

	// Verify second job
	if responses[1].ID != job2.ID {
		t.Errorf("Expected second job ID %s, got %s", job2.ID, responses[1].ID)
	}

	// Marshal to JSON and verify org_id, username, user_id are NOT present
	jsonBytes, err := json.Marshal(responses)
	if err != nil {
		t.Fatalf("Failed to marshal responses to JSON: %v", err)
	}

	jsonString := string(jsonBytes)

	// Check that sensitive fields are NOT in the JSON
	if contains(jsonString, "org_id") {
		t.Error("JSON should not contain 'org_id' field")
	}

	if contains(jsonString, "username") {
		t.Error("JSON should not contain 'username' field")
	}

	if contains(jsonString, "user_id") {
		t.Error("JSON should not contain 'user_id' field")
	}
}

func TestJobResponseWithLastRun(t *testing.T) {
	// Create a job with last_run set
	payload := domain.JobPayload{
		Details: map[string]interface{}{
			"message": "test message",
		},
	}

	job := domain.NewJob("Test Job", "org-123", "testuser", "user-123", "*/15 * * * *", domain.PayloadMessage, payload)
	now := time.Now()
	job = job.WithLastRun(now)

	// Convert to response DTO
	response := ToJobResponse(job)

	// Verify LastRun is set
	if response.LastRun == nil {
		t.Error("Expected LastRun to be set")
	} else if response.LastRun.Unix() != now.Unix() {
		t.Errorf("Expected LastRun %v, got %v", now, response.LastRun)
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || contains(s[1:], substr)))
}
