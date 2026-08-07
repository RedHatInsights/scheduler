package export

import (
	"time"
)

// ExportFormat represents the format of the export
type ExportFormat string

const (
	FormatJSON ExportFormat = "json"
	FormatCSV  ExportFormat = "csv"
)

// ExportStatus represents the status of an export
type ExportStatus string

const (
	StatusPartial  ExportStatus = "partial"
	StatusPending  ExportStatus = "pending"
	StatusRunning  ExportStatus = "running"
	StatusComplete ExportStatus = "complete"
	StatusFailed   ExportStatus = "failed"
)

// Application represents the application type
type Application string

const (
	AppAdvisor              Application = "advisor"
	AppCompliance           Application = "compliance"
	AppDriftService         Application = "drift-service"
	AppImageBuilder         Application = "image-builder"
	AppInventory            Application = "inventory"
	AppPatchManager         Application = "patch-manager"
	AppPolicyEngine         Application = "policy-engine"
	AppResourceOptimization Application = "resource-optimization"
	AppSubscriptionsService Application = "subscriptions-service"
	AppSystemBaseline       Application = "system-baseline"
	AppVulnerabilityEngine  Application = "vulnerability-engine"
)

// ExportRequest represents a request to create an export
type ExportRequest struct {
	Name       string       `json:"name"`
	Format     ExportFormat `json:"format"`
	Expiration *time.Time   `json:"expiration,omitempty"`
	Sources    []Source     `json:"sources"`
}

// Source represents a data source for export
type Source struct {
	Application Application            `json:"application"`
	Resource    string                 `json:"resource"`
	Filters     map[string]interface{} `json:"filters,omitempty"`
}

// ExportStatusResponse represents the status of an export request
type ExportStatusResponse struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	CreatedAt   time.Time      `json:"created_at"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
	ExpiresAt   *time.Time     `json:"expires_at,omitempty"`
	Format      ExportFormat   `json:"format"`
	Status      ExportStatus   `json:"status"`
	Sources     []SourceStatus `json:"sources"`
}

// SourceStatus represents the status of a specific data source
type SourceStatus struct {
	Application Application `json:"application"`
	Resource    string      `json:"resource"`
	Status      string      `json:"status"`
	Error       *string     `json:"error,omitempty"`
}

// ExportListResponse represents a list of export statuses
type ExportListResponse struct {
	Data []ExportStatusResponse `json:"data"`
	Meta Metadata               `json:"meta"`
}

// Metadata represents pagination and filtering metadata
type Metadata struct {
	Count  int `json:"count"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// ErrorResponse represents an error response from the API
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// ListParams represents query parameters for listing exports
type ListParams struct {
	Name        *string       `json:"name,omitempty"`
	CreatedAt   *time.Time    `json:"created_at,omitempty"`
	Application *Application  `json:"application,omitempty"`
	Status      *ExportStatus `json:"status,omitempty"`
	Limit       *int          `json:"limit,omitempty"`
	Offset      *int          `json:"offset,omitempty"`
}
