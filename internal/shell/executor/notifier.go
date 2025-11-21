package executor

import "context"

// ExportCompletionNotification contains the data for an export completion notification
type ExportCompletionNotification struct {
	ExportID    string
	JobID       string
	AccountID   string
	OrgID       string
	Status      string
	DownloadURL string
	ErrorMsg    string
}

// JobCompletionNotifier defines the interface for sending job completion notifications
type JobCompletionNotifier interface {
	// JobComplete sends a notification when a job completes
	JobComplete(ctx context.Context, notification *ExportCompletionNotification) error
}
