package executor

import (
	"context"
	"log"

	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/usecases"
)

// NullJobCompletionNotifier is a no-op implementation of JobCompletionNotifier
// that does nothing when notifications are disabled (null object pattern)
type NullJobCompletionNotifier struct{}

// NewNullJobCompletionNotifier creates a new null notifier
func NewNullJobCompletionNotifier() *NullJobCompletionNotifier {
	return &NullJobCompletionNotifier{}
}

// JobComplete does nothing - this is a no-op implementation
func (n *NullJobCompletionNotifier) JobComplete(ctx context.Context, notification *ExportCompletionNotification) error {
	log.Printf("No notifier configured - skipping completion notification for export: %s", notification.ExportID)
	return nil
}

// NotifyJobPausedDueToFailures does nothing - no-op for failure-paused notifications
func (n *NullJobCompletionNotifier) NotifyJobPausedDueToFailures(ctx context.Context, job domain.Job, consecutiveFailures int) error {
	log.Printf("No notifier configured - skipping failure-paused notification for job: %s", job.ID)
	return nil
}

// Ensure NullJobCompletionNotifier implements JobPausedDueToFailuresNotifier
var _ usecases.JobPausedDueToFailuresNotifier = (*NullJobCompletionNotifier)(nil)
