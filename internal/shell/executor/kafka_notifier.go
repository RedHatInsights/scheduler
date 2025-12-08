package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"insights-scheduler/internal/shell/messaging"
)

// NotificationMessage represents the structure for platform notification events
// Based on the notifications-backend message format
type NotificationMessage struct {
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
	log.Printf("Sending platform notification via Kafka for export: %s", notification.ExportID)

	// Build the platform notification message
	platformNotification := n.buildPlatformNotification(notification)

	// Marshal to JSON
	messageBytes, err := json.Marshal(platformNotification)
	if err != nil {
		log.Printf("Failed to marshal platform notification for export %s: %v", notification.ExportID, err)
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	// Build headers for Kafka message
	headers := map[string]string{
		"message-type": "platform-notification",
		"bundle":       platformNotification.Bundle,
		"application":  platformNotification.Application,
		"event-type":   platformNotification.EventType,
		"org-id":       platformNotification.OrgID,
		"account-id":   platformNotification.AccountID,
		"version":      platformNotification.Version,
	}

	// Send via generic Kafka producer
	if err := n.producer.SendMessage(platformNotification.OrgID, messageBytes, headers); err != nil {
		log.Printf("Failed to send platform notification for export %s: %v", notification.ExportID, err)
		return err
	}

	log.Printf("Platform notification sent successfully for export %s", notification.ExportID)
	return nil
}

// buildPlatformNotification creates a platform notification message from an export completion notification
func (n *NotificationsBasedJobCompletionNotifier) buildPlatformNotification(notification *ExportCompletionNotification) *NotificationMessage {
	context := map[string]interface{}{
		"export_id":    notification.ExportID,
		"job_id":       notification.JobID,
		"status":       notification.Status,
		"download_url": notification.DownloadURL,
		"name":         "ima-test-from-scheduler",
	}

	// Add error message if present
	if notification.ErrorMsg != "" {
		context["error_message"] = notification.ErrorMsg
	}

	/*
		// Determine event type based on status
		eventType := "export-completed"
		if notification.Status == "failed" {
			eventType = "export-failed"
		}
	*/

	return &NotificationMessage{
		Version:     "v1.2.0",
		Bundle:      "console",
		Application: "integrations",     //"insights-scheduler",
		EventType:   "integration-test", // eventType,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		AccountID:   notification.AccountID,
		OrgID:       notification.OrgID,
		Context:     context,
		Events:      []interface{}{},
		Recipients:  []interface{}{},
	}
}
