package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"insights-scheduler/internal/shell/messaging"
)

const (
	NOTIFICATIONS_BUNDLE string = "console"
	NOTIFICATIONS_APP    string = "scheduler"
)

// NotificationMessage represents the structure for platform notification events
// Based on the notifications-backend message format
type NotificationMessage struct {
	ID          string                 `json:"id"`
	Version     string                 `json:"version"`
	Bundle      string                 `json:"bundle"`
	Application string                 `json:"application"`
	EventType   string                 `json:"event_type"`
	Timestamp   string                 `json:"timestamp"` // RFC3339 format
	AccountID   string                 `json:"account_id"`
	OrgID       string                 `json:"org_id"`
	Context     map[string]interface{} `json:"context"`
	Events      []interface{}          `json:"events"`
	Recipients  []interface{}          `json:"recipients"`
}

// NotificationsBasedJobCompletionNotifier sends job completion notifications via Kafka
type NotificationsBasedJobCompletionNotifier struct {
	producer *messaging.KafkaProducer
}

// NewNotificationsBasedJobCompletionNotifier creates a new notifications-based notifier
func NewNotificationsBasedJobCompletionNotifier(producer *messaging.KafkaProducer) *NotificationsBasedJobCompletionNotifier {
	return &NotificationsBasedJobCompletionNotifier{
		producer: producer,
	}
}

// JobComplete sends a job completion notification to Kafka
func (n *NotificationsBasedJobCompletionNotifier) JobComplete(ctx context.Context, notification *ExportCompletionNotification, logger *slog.Logger) error {
	// Generate request ID for tracking
	messageID := uuid.New().String()
	logger.Info("Sending platform notification via Kafka",
		slog.String("export_id", notification.ExportID),
		slog.String("message_id", messageID))

	// Build the platform notification message
	platformNotification := n.buildPlatformNotification(notification, messageID)

	// Marshal to JSON
	messageBytes, err := json.Marshal(platformNotification)
	if err != nil {
		logger.Error("Failed to marshal platform notification",
			slog.String("export_id", notification.ExportID),
			slog.String("message_id", messageID),
			slog.Any("error", err))
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	// Build headers for Kafka message
	headers := map[string]string{
		"bundle":      platformNotification.Bundle,
		"application": platformNotification.Application,
		"event-type":  platformNotification.EventType,
		"org-id":      platformNotification.OrgID,
		"account-id":  platformNotification.AccountID,
		"version":     platformNotification.Version,
	}

	// Send via generic Kafka producer
	if err := n.producer.SendMessage(platformNotification.OrgID, messageBytes, headers); err != nil {
		logger.Error("Failed to send platform notification",
			slog.String("export_id", notification.ExportID),
			slog.String("message_id", messageID),
			slog.Any("error", err))
		return err
	}

	logger.Info("Platform notification sent successfully",
		slog.String("export_id", notification.ExportID),
		slog.String("message_id", messageID))
	return nil
}

// JobAutoPaused sends a notification when a job is automatically paused due to consecutive failures
func (n *NotificationsBasedJobCompletionNotifier) JobAutoPaused(ctx context.Context, notification *JobAutoPausedNotification, logger *slog.Logger) error {
	// Generate request ID for tracking
	messageID := uuid.New().String()
	logger.Info("Sending job auto-paused notification via Kafka",
		slog.String("message_id", messageID))

	// Build the platform notification message
	platformNotification := n.buildAutoPausedPlatformNotification(notification, messageID)

	// Marshal to JSON
	messageBytes, err := json.Marshal(platformNotification)
	if err != nil {
		logger.Error("Failed to marshal auto-paused notification",
			slog.String("message_id", messageID),
			slog.Any("error", err))
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	// Build headers for Kafka message
	headers := map[string]string{
		"bundle":      platformNotification.Bundle,
		"application": platformNotification.Application,
		"event-type":  platformNotification.EventType,
		"org-id":      platformNotification.OrgID,
		"version":     platformNotification.Version,
	}

	// Send via generic Kafka producer
	if err := n.producer.SendMessage(platformNotification.OrgID, messageBytes, headers); err != nil {
		logger.Error("Failed to send auto-paused notification",
			slog.String("message_id", messageID),
			slog.Any("error", err))
		return err
	}

	logger.Info("Job auto-paused notification sent successfully",
		slog.String("message_id", messageID))
	return nil
}

// buildPlatformNotification creates a platform notification message from an export completion notification
func (n *NotificationsBasedJobCompletionNotifier) buildPlatformNotification(notification *ExportCompletionNotification, messageID string) *NotificationMessage {
	context := map[string]interface{}{
		"export_id":     notification.ExportID,
		"job_id":        notification.JobID,
		"job_name":      notification.JobName,
		"status":        notification.Status,
		"error_message": notification.ErrorMsg,
		"download_url":  notification.DownloadURL,
	}

	// Add error message if present
	if notification.ErrorMsg != "" {
		context["error_message"] = notification.ErrorMsg
	}

	// Determine event type based on status
	eventType := "export-complete"
	if notification.Status == "failed" {
		eventType = "job-failed"
	}

	return &NotificationMessage{
		ID:          messageID,
		Version:     "v1.0.0",
		Bundle:      NOTIFICATIONS_BUNDLE,
		Application: NOTIFICATIONS_APP,
		EventType:   eventType,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		AccountID:   notification.AccountID,
		OrgID:       notification.OrgID,
		Context:     context,
		Events:      []interface{}{},
		Recipients:  []interface{}{},
	}
}

// buildAutoPausedPlatformNotification creates a platform notification message for auto-paused jobs
func (n *NotificationsBasedJobCompletionNotifier) buildAutoPausedPlatformNotification(notification *JobAutoPausedNotification, messageID string) *NotificationMessage {
	context := map[string]interface{}{
		"job_id":               notification.JobID,
		"job_name":             notification.JobName,
		"consecutive_failures": notification.ConsecutiveFailures,
		"user_id":              notification.UserID,
	}

	// Add error message if present
	if notification.ErrorMsg != "" {
		context["error_message"] = notification.ErrorMsg
	}

	return &NotificationMessage{
		ID:          messageID,
		Version:     "v1.0.0",
		Bundle:      NOTIFICATIONS_BUNDLE,
		Application: NOTIFICATIONS_APP,
		EventType:   "job-auto-paused",
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		AccountID:   "", // Not used for auto-pause notifications
		OrgID:       notification.OrgID,
		Context:     context,
		Events:      []interface{}{},
		Recipients:  []interface{}{},
	}
}
