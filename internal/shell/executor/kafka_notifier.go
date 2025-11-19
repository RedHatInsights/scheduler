package executor

import (
	"log"

	"insights-scheduler/internal/shell/messaging"
)

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
func (n *NotificationsBasedJobCompletionNotifier) JobComplete(notification *ExportCompletionNotification) error {
	log.Printf("Sending platform notification via Kafka for export: %s", notification.ExportID)

	// Create the platform notification message
	platformNotification := messaging.NewExportCompletionNotification(
		notification.ExportID,
		notification.JobID,
		notification.AccountID,
		notification.OrgID,
		notification.Status,
		notification.DownloadURL,
		notification.ErrorMsg,
	)

	if err := n.producer.SendNotificationMessage(platformNotification); err != nil {
		log.Printf("Failed to send platform notification for export %s: %v", notification.ExportID, err)
		return err
	}

	log.Printf("Platform notification sent successfully for export %s", notification.ExportID)
	return nil
}
