package executor

import "log"

// NullJobCompletionNotifier is a no-op implementation of JobCompletionNotifier
// that does nothing when notifications are disabled (null object pattern)
type NullJobCompletionNotifier struct{}

// NewNullJobCompletionNotifier creates a new null notifier
func NewNullJobCompletionNotifier() *NullJobCompletionNotifier {
	return &NullJobCompletionNotifier{}
}

// JobComplete does nothing - this is a no-op implementation
func (n *NullJobCompletionNotifier) JobComplete(notification *ExportCompletionNotification) error {
	log.Printf("No notifier configured - skipping completion notification for export: %s", notification.ExportID)
	return nil
}
