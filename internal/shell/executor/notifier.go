package executor

import "context"

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

// JobFailureNotification contains data for a generic job failure notification
type JobFailureNotification struct {
	JobID               string
	JobName             string
	OrgID               string
	ErrorMsg            string
	ConsecutiveFailures int
	AutoPaused          bool
}

// JobCompletionNotifier defines the interface for sending job completion notifications
type JobCompletionNotifier interface {
	// JobComplete sends a notification when a job completes
	JobComplete(ctx context.Context, notification *ExportCompletionNotification) error

	// JobFailed sends a notification when a job fails
	JobFailed(ctx context.Context, notification *JobFailureNotification) error
}
