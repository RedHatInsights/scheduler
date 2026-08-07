package export

import (
	"context"

	"insights-scheduler/internal/clients/polling"
)

// ExportPoller implements the polling.Poller interface for export service
type ExportPoller struct {
	client         *Client
	identityHeader string
}

// NewExportPoller creates a new ExportPoller
func NewExportPoller(client *Client, identityHeader string) *ExportPoller {
	return &ExportPoller{
		client:         client,
		identityHeader: identityHeader,
	}
}

// GetStatus retrieves the current status of an export job
func (p *ExportPoller) GetStatus(ctx context.Context, jobID string) (*polling.StatusResponse, error) {
	status, err := p.client.GetExportStatus(ctx, jobID, p.identityHeader)
	if err != nil {
		return nil, err
	}

	// Map export status to generic job status
	var jobStatus polling.JobStatus
	switch status.Status {
	case StatusPending, StatusRunning, StatusPartial:
		jobStatus = polling.StatusInProgress
	case StatusComplete:
		jobStatus = polling.StatusComplete
	case StatusFailed:
		jobStatus = polling.StatusFailed
	default:
		jobStatus = polling.StatusPending
	}

	// Extract error message if available
	errorMsg := ""
	if status.Status == StatusFailed && len(status.Sources) > 0 {
		if status.Sources[0].Error != nil {
			errorMsg = *status.Sources[0].Error
		}
	}

	return &polling.StatusResponse{
		ID:         status.ID,
		Status:     jobStatus,
		Error:      errorMsg,
		IsTerminal: jobStatus == polling.StatusComplete || jobStatus == polling.StatusFailed,
		Metadata: map[string]interface{}{
			"name":       status.Name,
			"format":     status.Format,
			"sources":    status.Sources,
			"created_at": status.CreatedAt,
		},
	}, nil
}

// IsTerminalStatus determines if a status is final
func (p *ExportPoller) IsTerminalStatus(status polling.JobStatus) bool {
	return status == polling.StatusComplete || status == polling.StatusFailed
}
