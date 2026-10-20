package executor

import (
	"context"
	"log/slog"
)

// NullJobCompletionNotifier is a no-op implementation of JobCompletionNotifier
// that does nothing when notifications are disabled (null object pattern)
type NullJobCompletionNotifier struct{}

// NewNullJobCompletionNotifier creates a new null notifier
func NewNullJobCompletionNotifier() *NullJobCompletionNotifier {
	return &NullJobCompletionNotifier{}
}

// JobComplete does nothing - this is a no-op implementation
func (n *NullJobCompletionNotifier) JobComplete(ctx context.Context, notification *ExportCompletionNotification, logger *slog.Logger) error {
	logger.Debug("No notifier configured - skipping completion notification",
		slog.String("export_id", notification.ExportID))
	return nil
}

// JobAutoPaused does nothing - this is a no-op implementation
func (n *NullJobCompletionNotifier) JobAutoPaused(ctx context.Context, notification *JobAutoPausedNotification, logger *slog.Logger) error {
	logger.Debug("No notifier configured - skipping auto-paused notification")
	return nil
}
