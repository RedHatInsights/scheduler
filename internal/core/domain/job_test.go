package domain

import (
	"testing"
)

func TestIsValidSchedule(t *testing.T) {
	tests := []struct {
		name     string
		schedule string
		expected bool
	}{
		{
			name:     "Valid predefined 10 minutes",
			schedule: string(Schedule10Minutes),
			expected: true,
		},
		{
			name:     "Valid predefined 1 hour",
			schedule: string(Schedule1Hour),
			expected: true,
		},
		{
			name:     "Valid predefined 1 day",
			schedule: string(Schedule1Day),
			expected: true,
		},
		{
			name:     "Valid predefined 1 month",
			schedule: string(Schedule1Month),
			expected: true,
		},
		{
			name:     "Valid custom cron expression",
			schedule: "30 0 * * *", // Every day at 30 minutes past midnight (minute hour dom month dow)
			expected: true,
		},
		{
			name:     "Invalid 6-field cron expression (seconds not supported)",
			schedule: "0 0 12 * * *", // 6-field format should fail
			expected: false,
		},
		{
			name:     "Invalid cron expression",
			schedule: "invalid cron",
			expected: false,
		},
		{
			name:     "Empty schedule",
			schedule: "",
			expected: false,
		},
		{
			name:     "Legacy format should fail",
			schedule: "10m",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidSchedule(tt.schedule)
			if result != tt.expected {
				t.Errorf("IsValidSchedule(%q) = %v; expected %v", tt.schedule, result, tt.expected)
			}
		})
	}
}

func TestScheduleConstants(t *testing.T) {
	// Verify our predefined schedules are valid cron expressions
	schedules := []Schedule{
		Schedule10Minutes,
		Schedule1Hour,
		Schedule1Day,
		Schedule1Month,
	}

	for _, schedule := range schedules {
		if !IsValidSchedule(string(schedule)) {
			t.Errorf("Predefined schedule %q should be valid", schedule)
		}
	}
}

func TestNewJob(t *testing.T) {
	payload := map[string]interface{}{
		"message": "test message",
	}

	job := NewJob("Test Job", "org-123", "testuser", "user-123", Schedule1Hour, "UTC", PayloadMessage, payload)

	if job.ID == "" {
		t.Error("Job ID should not be empty")
	}

	if job.Name != "Test Job" {
		t.Errorf("Expected name 'Test Job', got %s", job.Name)
	}

	if job.OrgID != "org-123" {
		t.Errorf("Expected org_id 'org-123', got %s", job.OrgID)
	}

	if job.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got %s", job.Username)
	}

	if job.UserID != "user-123" {
		t.Errorf("Expected user_id 'user-123', got %s", job.UserID)
	}

	if job.Schedule != Schedule1Hour {
		t.Errorf("Expected schedule %s, got %s", Schedule1Hour, job.Schedule)
	}

	if job.Type != PayloadMessage {
		t.Errorf("Expected type %s, got %s", PayloadMessage, job.Type)
	}

	if job.Status != StatusScheduled {
		t.Errorf("Expected status %s, got %s", StatusScheduled, job.Status)
	}

	if job.LastRunAt != nil {
		t.Error("LastRunAt should be nil for new job")
	}
}

func TestJobUpdateFields(t *testing.T) {
	job := NewJob("Original Job", "org-123", "originaluser", "user-123", Schedule1Hour, "UTC", PayloadMessage, map[string]interface{}{
		"msg": "original",
	})

	newName := "Updated Job"
	newOrgID := "org-456"
	newUsername := "updateduser"
	newUserID := "user-456"
	newSchedule := Schedule1Day
	newPayloadType := PayloadCommand
	var newPayload interface{} = map[string]interface{}{"cmd": "ls"}
	newStatus := StatusPaused

	updatedJob := job.UpdateFields(&newName, &newOrgID, &newUsername, &newUserID, &newSchedule, &newPayloadType, &newPayload, &newStatus)

	if updatedJob.ID != job.ID {
		t.Error("Job ID should not change")
	}

	if updatedJob.Name != newName {
		t.Errorf("Expected name %s, got %s", newName, updatedJob.Name)
	}

	if updatedJob.OrgID != newOrgID {
		t.Errorf("Expected org_id %s, got %s", newOrgID, updatedJob.OrgID)
	}

	if updatedJob.Username != newUsername {
		t.Errorf("Expected username %s, got %s", newUsername, updatedJob.Username)
	}

	if updatedJob.UserID != newUserID {
		t.Errorf("Expected user_id %s, got %s", newUserID, updatedJob.UserID)
	}

	if updatedJob.Schedule != newSchedule {
		t.Errorf("Expected schedule %s, got %s", newSchedule, updatedJob.Schedule)
	}

	if updatedJob.Type != newPayloadType {
		t.Errorf("Expected type %s, got %s", newPayloadType, updatedJob.Type)
	}

	if updatedJob.Status != newStatus {
		t.Errorf("Expected status %s, got %s", newStatus, updatedJob.Status)
	}
}
