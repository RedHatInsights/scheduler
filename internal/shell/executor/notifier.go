package executor

import (
	"context"
	"log/slog"
)

// ExportCompletionNotification contains the data for an export completion notification
type ExportCompletionNotification struct {
	ExportID    string
	JobID       string
	JobName     string
	AccountID   string
	OrgID       string
	Status      string
	DownloadURL string
	ErrorMsg    string
}

// JobAutoPausedNotification contains the data for a job auto-pause notification
type JobAutoPausedNotification struct {
	JobID               string
	JobName             string
	OrgID               string
	UserID              string
	ConsecutiveFailures int
	ErrorMsg            string
}

// JobCompletionNotifier defines the interface for sending job completion notifications
type JobCompletionNotifier interface {
	// JobComplete sends a notification when a job completes
	JobComplete(ctx context.Context, notification *ExportCompletionNotification, logger *slog.Logger) error

	// JobAutoPaused sends a notification when a job is automatically paused due to consecutive failures
	JobAutoPaused(ctx context.Context, notification *JobAutoPausedNotification, logger *slog.Logger) error
}
