package domain

import "time"

type ResultType string

const (
	ResultTypeExport  ResultType = "export"
	ResultTypeCommand ResultType = "command"
	ResultTypeHTTP    ResultType = "http_request"
	ResultTypeMessage ResultType = "message"
)

// ExportResult represents the result of an export job execution
type ExportResult struct {
	Type        ResultType           `json:"type"`
	ExportID    string               `json:"export_id"`
	Status      string               `json:"status"`
	DownloadURL string               `json:"download_url,omitempty"`
	Format      string               `json:"format"`
	CreatedAt   time.Time            `json:"created_at"`
	CompletedAt *time.Time           `json:"completed_at,omitempty"`
	ExpiresAt   *time.Time           `json:"expires_at,omitempty"`
	Sources     []ExportSourceStatus `json:"sources,omitempty"`
}

// ExportSourceStatus represents the status of a specific data source in an export
type ExportSourceStatus struct {
	Application string  `json:"application"`
	Resource    string  `json:"resource"`
	Status      string  `json:"status"`
	Error       *string `json:"error,omitempty"`
}

// CommandResult represents the result of a command execution
type CommandResult struct {
	Type     ResultType `json:"type"`
	Command  string     `json:"command"`
	ExitCode int        `json:"exit_code"`
	Stdout   string     `json:"stdout,omitempty"`
	Stderr   string     `json:"stderr,omitempty"`
	Duration float64    `json:"duration_ms"`
}

// HTTPResult represents the result of an HTTP request execution
type HTTPResult struct {
	Type       ResultType `json:"type"`
	URL        string     `json:"url"`
	Method     string     `json:"method"`
	StatusCode int        `json:"status_code"`
	Latency    float64    `json:"latency_ms"`
}

// MessageResult represents the result of a message delivery
type MessageResult struct {
	Type           ResultType `json:"type"`
	Message        string     `json:"message"`
	DeliveryStatus string     `json:"delivery_status"`
}

// IsValidResultType validates result type strings
func IsValidResultType(rt string) bool {
	switch ResultType(rt) {
	case ResultTypeExport, ResultTypeCommand, ResultTypeHTTP, ResultTypeMessage:
		return true
	default:
		return false
	}
}
