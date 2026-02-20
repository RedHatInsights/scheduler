package http

import (
	"encoding/json"
	"testing"
	"time"

	"insights-scheduler/internal/core/domain"
)

func TestToJobResponse(t *testing.T) {
	// Create a domain job with all fields including org_id, username, user_id
	payload := map[string]interface{}{
		"message": "test message",
	}

	job := domain.NewJob("Test Job", "org-123", "testuser", "user-123", "*/15 * * * *", "UTC", domain.PayloadMessage, payload)

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
	payload1 := map[string]interface{}{
		"message": "test message 1",
	}

	payload2 := map[string]interface{}{
		"message": "test message 2",
	}

	job1 := domain.NewJob("Test Job 1", "org-123", "testuser", "user-123", "*/15 * * * *", "UTC", domain.PayloadMessage, payload1)
	job2 := domain.NewJob("Test Job 2", "org-456", "testuser2", "user-456", "0 * * * *", "UTC", domain.PayloadCommand, payload2)

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

func TestJobResponseWithLastRunAt(t *testing.T) {
	// Create a job with last_run_at set
	payload := map[string]interface{}{
		"message": "test message",
	}

	job := domain.NewJob("Test Job", "org-123", "testuser", "user-123", "*/15 * * * *", "UTC", domain.PayloadMessage, payload)
	now := time.Now()
	job = job.WithLastRunAt(now)

	// Convert to response DTO
	response := ToJobResponse(job)

	// Verify LastRunAt is set
	if response.LastRunAt == nil {
		t.Error("Expected LastRunAt to be set")
	} else if response.LastRunAt.Unix() != now.Unix() {
		t.Errorf("Expected LastRunAt %v, got %v", now, response.LastRunAt)
	}
}

func TestJobResponseTimezoneConversion(t *testing.T) {
	// Create a job with America/New_York timezone
	payload := map[string]interface{}{
		"message": "test message",
	}

	job := domain.NewJob("Test Job", "org-123", "testuser", "user-123", "*/15 * * * *", "America/New_York", domain.PayloadMessage, payload)

	// Set next_run_at to a known UTC time: 2026-02-21 14:00:00 UTC
	// This should be 2026-02-21 09:00:00 EST (America/New_York is UTC-5 in winter)
	utcTime := time.Date(2026, 2, 21, 14, 0, 0, 0, time.UTC)
	job = job.WithNextRunAt(utcTime)

	// Convert to response DTO
	response := ToJobResponse(job)

	// Verify NextRunAt is set
	if response.NextRunAt == nil {
		t.Fatal("Expected NextRunAt to be set")
	}

	// Verify the time is in America/New_York timezone
	expectedLoc, _ := time.LoadLocation("America/New_York")
	expectedTime := utcTime.In(expectedLoc)

	if response.NextRunAt.Location().String() != "America/New_York" {
		t.Errorf("Expected timezone America/New_York, got %s", response.NextRunAt.Location().String())
	}

	// Verify the hour matches (should be 9 AM in NY, not 2 PM UTC)
	if response.NextRunAt.Hour() != 9 {
		t.Errorf("Expected hour 9 (EST), got %d", response.NextRunAt.Hour())
	}

	// Verify the times are equal (same absolute time, different representation)
	if !response.NextRunAt.Equal(utcTime) {
		t.Errorf("Expected times to be equal: got %v, want %v", response.NextRunAt, expectedTime)
	}

	// Marshal to JSON and verify it includes timezone offset
	jsonBytes, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal response to JSON: %v", err)
	}

	jsonString := string(jsonBytes)

	// The JSON should contain a timestamp with timezone offset (e.g., -05:00 for EST)
	// Not just Z for UTC
	if contains(jsonString, "\"next_run_at\":\"2026-02-21T14:00:00Z\"") {
		t.Error("NextRunAt should not be in UTC (Z suffix), should be in America/New_York timezone")
	}

	// Should contain the EST time (9 AM) with offset
	if !contains(jsonString, "2026-02-21T09:00:00-05:00") {
		t.Logf("JSON: %s", jsonString)
		t.Error("NextRunAt should be in America/New_York timezone (09:00:00-05:00)")
	}
}

func TestJobResponseUTCTimezone(t *testing.T) {
	// Create a job with UTC timezone
	payload := map[string]interface{}{
		"message": "test message",
	}

	job := domain.NewJob("Test Job", "org-123", "testuser", "user-123", "*/15 * * * *", "UTC", domain.PayloadMessage, payload)

	// Set next_run_at to a known UTC time
	utcTime := time.Date(2026, 2, 21, 14, 0, 0, 0, time.UTC)
	job = job.WithNextRunAt(utcTime)

	// Convert to response DTO
	response := ToJobResponse(job)

	// Verify NextRunAt is set and remains in UTC
	if response.NextRunAt == nil {
		t.Fatal("Expected NextRunAt to be set")
	}

	// Marshal to JSON
	jsonBytes, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal response to JSON: %v", err)
	}

	jsonString := string(jsonBytes)

	// Should contain the UTC time with Z suffix
	if !contains(jsonString, "2026-02-21T14:00:00Z") {
		t.Logf("JSON: %s", jsonString)
		t.Error("NextRunAt should be in UTC timezone (Z suffix)")
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || contains(s[1:], substr)))
}
