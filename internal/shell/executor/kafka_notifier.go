package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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
func (n *NotificationsBasedJobCompletionNotifier) JobComplete(ctx context.Context, notification *ExportCompletionNotification) error {
	// Generate request ID for tracking
	messageID := uuid.New().String()
	log.Printf("Sending platform notification via Kafka for export: %s (message_id: %s)", notification.ExportID, messageID)

	// Build the platform notification message
	platformNotification := n.buildPlatformNotification(notification, messageID)

	// Marshal to JSON
	messageBytes, err := json.Marshal(platformNotification)
	if err != nil {
		log.Printf("Failed to marshal platform notification for export %s (message_id: %s): %v", notification.ExportID, messageID, err)
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
		log.Printf("Failed to send platform notification for export %s (message_id: %s): %v", notification.ExportID, messageID, err)
		return err
	}

	log.Printf("Platform notification sent successfully for export %s (message_id: %s)", notification.ExportID, messageID)
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
