package polling

import (
	"context"
	"fmt"
	"time"
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

// Config holds polling configuration
type Config struct {
	MaxRetries   int
	PollInterval time.Duration
	Timeout      time.Duration
}

// DefaultConfig provides sensible defaults
func DefaultConfig() Config {
	return Config{
		MaxRetries:   60,
		PollInterval: 5 * time.Second,
		Timeout:      10 * time.Minute,
	}
}

// Poll waits for a job to reach a terminal state
func Poll(ctx context.Context, poller Poller, jobID string, cfg Config) (*StatusResponse, error) {
	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	for attempt := 0; attempt < cfg.MaxRetries; attempt++ {
		// Check if context is cancelled
		select {
		case <-timeoutCtx.Done():
			return nil, fmt.Errorf("polling timed out after %v", cfg.Timeout)
		default:
		}

		// Get current status
		status, err := poller.GetStatus(timeoutCtx, jobID)
		if err != nil {
			return nil, fmt.Errorf("failed to get status (attempt %d/%d): %w",
				attempt+1, cfg.MaxRetries, err)
		}

		// Check if we've reached a terminal state
		if status.IsTerminal || poller.IsTerminalStatus(status.Status) {
			return status, nil
		}

		// Wait before next attempt (unless this is the last attempt)
		if attempt < cfg.MaxRetries-1 {
			select {
			case <-timeoutCtx.Done():
				return nil, fmt.Errorf("polling timed out after %v", cfg.Timeout)
			case <-time.After(cfg.PollInterval):
				// Continue to next iteration
			}
		}
	}

	return nil, fmt.Errorf("job did not complete after %d polling attempts", cfg.MaxRetries)
}
