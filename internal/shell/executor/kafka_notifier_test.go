package executor

import (
	"testing"
	"time"
)

func newTestNotifier() *NotificationsBasedJobCompletionNotifier {
	return &NotificationsBasedJobCompletionNotifier{}
}

func TestBuildPlatformNotification_BaseFields(t *testing.T) {
	n := newTestNotifier()
	notification := &ExportCompletionNotification{
		ExportID:    "export-1",
		JobID:       "job-1",
		JobName:     "My Job",
		OrgID:       "org-1",
		Status:      "success",
		DownloadURL: "https://example.com/download",
	}

	msg := n.buildPlatformNotification(notification, "message-1")

	if msg.EventType != "export-complete" {
		t.Errorf("Expected event type 'export-complete', got %q", msg.EventType)
	}
	if msg.OrgID != "org-1" {
		t.Errorf("Expected org_id 'org-1', got %q", msg.OrgID)
	}
	if msg.Context["export_id"] != "export-1" {
		t.Errorf("Expected export_id 'export-1', got %v", msg.Context["export_id"])
	}
	if msg.Context["download_url"] != "https://example.com/download" {
		t.Errorf("Expected download_url to be set, got %v", msg.Context["download_url"])
	}
	if _, ok := msg.Context["run_id"]; ok {
		t.Errorf("Expected no run_id key when RunID is empty, got %v", msg.Context["run_id"])
	}
	if _, ok := msg.Context["next_run_at"]; ok {
		t.Errorf("Expected no next_run_at key when NextRunAt is nil, got %v", msg.Context["next_run_at"])
	}
}

func TestBuildPlatformNotification_FailedStatusSetsEventType(t *testing.T) {
	n := newTestNotifier()
	notification := &ExportCompletionNotification{
		ExportID: "export-2",
		Status:   "failed",
		ErrorMsg: "boom",
	}

	msg := n.buildPlatformNotification(notification, "message-2")

	if msg.EventType != "job-failed" {
		t.Errorf("Expected event type 'job-failed', got %q", msg.EventType)
	}
	if msg.Context["error_message"] != "boom" {
		t.Errorf("Expected error_message 'boom', got %v", msg.Context["error_message"])
	}
}

func TestBuildPlatformNotification_RunIDAndNextRunAt(t *testing.T) {
	n := newTestNotifier()
	nextRun := time.Date(2026, 9, 4, 12, 30, 0, 0, time.UTC)
	notification := &ExportCompletionNotification{
		ExportID:  "export-3",
		Status:    "success",
		RunID:     "run-123",
		NextRunAt: &nextRun,
	}

	msg := n.buildPlatformNotification(notification, "message-3")

	if msg.Context["run_id"] != "run-123" {
		t.Errorf("Expected run_id 'run-123', got %v", msg.Context["run_id"])
	}
	if msg.Context["next_run_at"] != "2026-09-04T12:30:00Z" {
		t.Errorf("Expected next_run_at RFC3339 UTC string, got %v", msg.Context["next_run_at"])
	}
}

func TestBuildPlatformNotification_NextRunAtConvertedToUTC(t *testing.T) {
	n := newTestNotifier()
	loc := time.FixedZone("UTC-5", -5*60*60)
	nextRun := time.Date(2026, 9, 4, 7, 30, 0, 0, loc)
	notification := &ExportCompletionNotification{
		ExportID:  "export-4",
		Status:    "success",
		NextRunAt: &nextRun,
	}

	msg := n.buildPlatformNotification(notification, "message-4")

	if msg.Context["next_run_at"] != "2026-09-04T12:30:00Z" {
		t.Errorf("Expected next_run_at normalized to UTC, got %v", msg.Context["next_run_at"])
	}
}

func TestBuildAutoPausedPlatformNotification_BaseFields(t *testing.T) {
	n := newTestNotifier()
	notification := &JobAutoPausedNotification{
		JobID:               "job-9",
		JobName:             "My Job",
		OrgID:               "org-9",
		UserID:              "user-9",
		ConsecutiveFailures: 3,
		ErrorMsg:            "too many failures",
	}

	msg := n.buildAutoPausedPlatformNotification(notification, "message-9")

	if msg.EventType != "job-auto-paused" {
		t.Errorf("Expected event type 'job-auto-paused', got %q", msg.EventType)
	}
	if msg.Context["consecutive_failures"] != 3 {
		t.Errorf("Expected consecutive_failures 3, got %v", msg.Context["consecutive_failures"])
	}
	if msg.Context["error_message"] != "too many failures" {
		t.Errorf("Expected error_message to be set, got %v", msg.Context["error_message"])
	}
	if _, ok := msg.Context["run_id"]; ok {
		t.Errorf("Expected no run_id key when RunID is empty, got %v", msg.Context["run_id"])
	}
	if _, ok := msg.Context["next_run_at"]; ok {
		t.Errorf("Expected no next_run_at key when NextRunAt is nil, got %v", msg.Context["next_run_at"])
	}
}

func TestBuildAutoPausedPlatformNotification_RunIDAndNextRunAt(t *testing.T) {
	n := newTestNotifier()
	nextRun := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	notification := &JobAutoPausedNotification{
		JobID:     "job-10",
		OrgID:     "org-10",
		RunID:     "run-456",
		NextRunAt: &nextRun,
	}

	msg := n.buildAutoPausedPlatformNotification(notification, "message-10")

	if msg.Context["run_id"] != "run-456" {
		t.Errorf("Expected run_id 'run-456', got %v", msg.Context["run_id"])
	}
	if msg.Context["next_run_at"] != "2026-09-05T00:00:00Z" {
		t.Errorf("Expected next_run_at RFC3339 UTC string, got %v", msg.Context["next_run_at"])
	}
}
