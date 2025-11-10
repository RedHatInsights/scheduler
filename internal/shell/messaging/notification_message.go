package messaging

import (
	"encoding/json"
	"time"
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

// NewExportCompletionNotification creates a notification message for export completion
func NewExportCompletionNotification(exportID, jobID, accountID, orgID, status, downloadURL, errorMsg string) *NotificationMessage {
	context := map[string]interface{}{
		"export_id":    exportID,
		"job_id":       jobID,
		"status":       status,
		"download_url": downloadURL,
	}

	// Add error message if present
	if errorMsg != "" {
		context["error_message"] = errorMsg
	}

	eventType := "export-completed"
	if status == "failed" {
		eventType = "export-failed"
	}

	eventType = "my-event-type"

	return &NotificationMessage{
		Version:     "v1.2.0",
		Bundle:      "my-bundle",
		Application: "my-app",
		EventType:   eventType,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		AccountID:   accountID,
		OrgID:       orgID,
		Context:     context,
		Events:      []interface{}{},
		Recipients:  []interface{}{},
	}
}

// ToJSON converts the notification message to JSON bytes
func (n *NotificationMessage) ToJSON() ([]byte, error) {
	return json.Marshal(n)
}

// ToJSONString converts the notification message to a JSON string
func (n *NotificationMessage) ToJSONString() (string, error) {
	bytes, err := n.ToJSON()
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}
