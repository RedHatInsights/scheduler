package polling

import (
	"context"
)

// JobStatus represents the state of an asynchronous job
type JobStatus string

const (
	StatusPending    JobStatus = "pending"
	StatusInProgress JobStatus = "in_progress"
	StatusComplete   JobStatus = "complete"
	StatusFailed     JobStatus = "failed"
)

// StatusResponse contains the result of a status check
type StatusResponse struct {
	ID         string
	Status     JobStatus
	Error      string
	Metadata   map[string]interface{} // Service-specific data
	IsTerminal bool                   // Whether this is a final state
}

// Poller defines the interface for checking job status
type Poller interface {
	// GetStatus retrieves the current status of a job
	GetStatus(ctx context.Context, jobID string) (*StatusResponse, error)

	// IsTerminalStatus determines if a status is final (complete/failed)
	IsTerminalStatus(status JobStatus) bool
}
