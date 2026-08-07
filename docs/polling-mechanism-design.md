# Flexible Polling Mechanism Design

## Overview

Both the export service and pdf-generator follow an asynchronous job pattern that requires polling:

1. **Create** - Submit job, receive status ID
2. **Poll** - Check status until complete/failed
3. **Download** - Retrieve result when ready

## Current Implementations

### Export Service
- **Status Endpoint**: `GET /exports/{id}/status`
- **Statuses**: `pending`, `running`, `partial`, `complete`, `failed`
- **Polling**: `WaitForExportCompletion()` with configurable retries/interval
- **Config**: `EXPORT_SERVICE_POLL_MAX_RETRIES` (60), `EXPORT_SERVICE_POLL_INTERVAL` (5s)

### PDF Generator
- **Status Endpoint**: `GET /api/crc-pdf-generator/v2/status/{statusID}`
- **Statuses**: `Generating`, `Generated`, `Failed`, `NotFound`
- **Polling**: Client-side responsibility (no built-in polling)
- **Download**: `GET /api/crc-pdf-generator/v2/download/{ID}`

## Key Differences

| Aspect | Export Service | PDF Generator |
|--------|---------------|---------------|
| Status granularity | 5 states with source-level status | 4 states, collection-based |
| Polling location | Server-side (in executor) | Client-side expected |
| Completion detection | Status transition | Status + component count |
| Error handling | Per-source errors | Collection-level or component-level |
| Download | Direct URL construction | Endpoint requires status check first |

## Proposed Generic Polling Interface

```go
package polling

import (
	"context"
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
	ID          string
	Status      JobStatus
	Error       string
	Metadata    map[string]interface{} // Service-specific data
	IsTerminal  bool                   // Whether this is a final state
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
```

## Service-Specific Implementations

### Export Service Poller

```go
package export

import (
	"context"
	"insights-scheduler/internal/clients/polling"
)

type ExportPoller struct {
	client         *Client
	identityHeader string
}

func NewExportPoller(client *Client, identityHeader string) *ExportPoller {
	return &ExportPoller{
		client:         client,
		identityHeader: identityHeader,
	}
}

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

func (p *ExportPoller) IsTerminalStatus(status polling.JobStatus) bool {
	return status == polling.StatusComplete || status == polling.StatusFailed
}
```

### PDF Generator Poller

```go
package pdfgen

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"insights-scheduler/internal/clients/polling"
)

type PDFPoller struct {
	client         *http.Client
	baseURL        string
	identityHeader string
}

type PDFStatusResponse struct {
	Status struct {
		Status         string `json:"status"`
		Components     []struct {
			Status  string  `json:"status"`
			Error   *string `json:"error"`
		} `json:"components"`
		ExpectedLength int     `json:"expectedLength"`
		Error          *string `json:"error"`
	} `json:"status"`
	Error *struct {
		Status      int    `json:"status"`
		StatusText  string `json:"statusText"`
		Description string `json:"description"`
	} `json:"error"`
}

func NewPDFPoller(baseURL string, identityHeader string) *PDFPoller {
	return &PDFPoller{
		client:         &http.Client{Timeout: 5 * time.Second},
		baseURL:        baseURL,
		identityHeader: identityHeader,
	}
}

func (p *PDFPoller) GetStatus(ctx context.Context, jobID string) (*polling.StatusResponse, error) {
	url := fmt.Sprintf("%s/api/crc-pdf-generator/v2/status/%s", p.baseURL, jobID)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("x-rh-identity", p.identityHeader)
	req.Header.Set("Content-Type", "application/json")
	
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	
	var pdfStatus PDFStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&pdfStatus); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	
	// Handle error response
	if pdfStatus.Error != nil {
		if pdfStatus.Error.Status == 404 {
			return &polling.StatusResponse{
				ID:         jobID,
				Status:     polling.StatusFailed,
				Error:      pdfStatus.Error.Description,
				IsTerminal: true,
			}, nil
		}
		if pdfStatus.Error.Status >= 400 {
			return &polling.StatusResponse{
				ID:         jobID,
				Status:     polling.StatusFailed,
				Error:      pdfStatus.Error.Description,
				IsTerminal: true,
			}, nil
		}
	}
	
	// Map PDF status to generic job status
	var jobStatus polling.JobStatus
	switch pdfStatus.Status.Status {
	case "Generating":
		jobStatus = polling.StatusInProgress
	case "Generated":
		jobStatus = polling.StatusComplete
	case "Failed", "NotFound":
		jobStatus = polling.StatusFailed
	default:
		jobStatus = polling.StatusPending
	}
	
	errorMsg := ""
	if pdfStatus.Status.Error != nil {
		errorMsg = *pdfStatus.Status.Error
	} else if len(pdfStatus.Status.Components) > 0 {
		for _, comp := range pdfStatus.Status.Components {
			if comp.Error != nil {
				errorMsg = *comp.Error
				break
			}
		}
	}
	
	return &polling.StatusResponse{
		ID:         jobID,
		Status:     jobStatus,
		Error:      errorMsg,
		IsTerminal: jobStatus == polling.StatusComplete || jobStatus == polling.StatusFailed,
		Metadata: map[string]interface{}{
			"status":          pdfStatus.Status.Status,
			"components":      pdfStatus.Status.Components,
			"expected_length": pdfStatus.Status.ExpectedLength,
		},
	}, nil
}

func (p *PDFPoller) IsTerminalStatus(status polling.JobStatus) bool {
	return status == polling.StatusComplete || status == polling.StatusFailed
}
```

## Usage Examples

### Export Job Executor (Refactored)

```go
func (e *ExportJobExecutor) Execute(job domain.Job, logger *slog.Logger) (interface{}, domain.ResultType, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	
	// ... existing identity and request setup ...
	
	// Create the export
	createResult, err := e.exportClient.CreateExport(ctx, req, identityHeader)
	if err != nil {
		return nil, domain.ResultTypeExport, fmt.Errorf("failed to create export: %w", err)
	}
	
	// Use the generic polling mechanism
	poller := export.NewExportPoller(e.exportClient, identityHeader)
	pollConfig := polling.Config{
		MaxRetries:   e.config.ExportService.PollMaxRetries,
		PollInterval: e.config.ExportService.PollInterval,
		Timeout:      9 * time.Minute, // Leave 1 min for cleanup
	}
	
	finalStatus, err := polling.Poll(ctx, poller, createResult.ID, pollConfig)
	if err != nil {
		return nil, domain.ResultTypeExport, fmt.Errorf("export failed or timed out: %w", err)
	}
	
	// ... existing notification and result building ...
}
```

### PDF Job Executor (New)

```go
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"insights-scheduler/internal/clients/pdfgen"
	"insights-scheduler/internal/clients/polling"
	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/identity"
)

type PDFJobExecutor struct {
	pdfClient     *pdfgen.Client
	notifier      JobCompletionNotifier
	userValidator identity.UserValidator
	config        *config.Config
}

func NewPDFJobExecutor(cfg *config.Config, userValidator identity.UserValidator, notifier JobCompletionNotifier) *PDFJobExecutor {
	pdfClient := pdfgen.NewClient(cfg.PDFService.BaseURL)
	
	return &PDFJobExecutor{
		pdfClient:     pdfClient,
		notifier:      notifier,
		userValidator: userValidator,
		config:        cfg,
	}
}

func (e *PDFJobExecutor) Execute(job domain.Job, logger *slog.Logger) (interface{}, domain.ResultType, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	
	// Generate identity header
	identityHeader, err := e.userValidator.GenerateIdentityHeader(ctx, job.OrgID, job.UserID)
	if err != nil {
		return nil, domain.ResultTypePDF, fmt.Errorf("failed to verify user: %w", err)
	}
	
	// Marshal payload to PDF request
	var req pdfgen.CreatePDFRequest
	payloadJSON, _ := json.Marshal(job.Payload)
	if err := json.Unmarshal(payloadJSON, &req); err != nil {
		return nil, domain.ResultTypePDF, fmt.Errorf("invalid PDF payload: %w", err)
	}
	
	// Create the PDF job
	createResult, err := e.pdfClient.CreatePDF(ctx, req, identityHeader)
	if err != nil {
		return nil, domain.ResultTypePDF, fmt.Errorf("failed to create PDF: %w", err)
	}
	
	logger.Info("PDF job created", slog.String("status_id", createResult.StatusID))
	
	// Poll for completion using generic polling
	poller := pdfgen.NewPDFPoller(e.config.PDFService.BaseURL, identityHeader)
	pollConfig := polling.Config{
		MaxRetries:   e.config.PDFService.PollMaxRetries,
		PollInterval: e.config.PDFService.PollInterval,
		Timeout:      9 * time.Minute,
	}
	
	finalStatus, err := polling.Poll(ctx, poller, createResult.StatusID, pollConfig)
	if err != nil {
		return nil, domain.ResultTypePDF, fmt.Errorf("PDF generation failed: %w", err)
	}
	
	// Send notification
	downloadURL := ""
	if finalStatus.Status == polling.StatusComplete {
		downloadURL = e.pdfClient.GetDownloadURL(createResult.StatusID)
	}
	
	notification := &PDFCompletionNotification{
		StatusID:    createResult.StatusID,
		JobID:       job.ID,
		JobName:     job.Name,
		OrgID:       job.OrgID,
		Status:      string(finalStatus.Status),
		DownloadURL: downloadURL,
		ErrorMsg:    finalStatus.Error,
	}
	
	if err := e.notifier.JobComplete(ctx, notification, logger); err != nil {
		logger.Warn("Failed to send completion notification", slog.Any("error", err))
	}
	
	// Build result
	result := domain.PDFResult{
		StatusID: createResult.StatusID,
	}
	
	if finalStatus.Status == polling.StatusComplete {
		result.URL = downloadURL
	}
	
	return result, domain.ResultTypePDF, nil
}
```

## Configuration

Add to `internal/config/config.go`:

```go
type PDFServiceConfig struct {
	BaseURL        string
	PollMaxRetries int
	PollInterval   time.Duration
}

// In loadConfig():
PDFService: PDFServiceConfig{
	BaseURL:        getEnv("PDF_SERVICE_URL", "http://localhost:8080"),
	PollMaxRetries: getEnvInt("PDF_SERVICE_POLL_MAX_RETRIES", 60),
	PollInterval:   getEnvDuration("PDF_SERVICE_POLL_INTERVAL", 5*time.Second),
}
```

## Benefits

1. **Single Polling Implementation**: One well-tested polling loop for all async services
2. **Consistent Error Handling**: Timeouts, retries, and failures handled uniformly
3. **Easy to Extend**: New async services just implement the `Poller` interface
4. **Service-Specific Logic**: Status mapping and metadata extraction isolated per service
5. **Testability**: Mock pollers for unit testing executors
6. **Configuration**: Per-service tuning of retries/intervals via environment variables

## Migration Path

1. Create `internal/clients/polling` package with generic interface
2. Refactor `export.WaitForExportCompletion()` to use `polling.Poll()`
3. Create `internal/clients/pdfgen` package with client and poller
4. Add PDF executor to job executor registry
5. Update configuration and documentation
