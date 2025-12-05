package executor

import (
	"context"
	"fmt"
	"log"
	"time"

	"insights-scheduler/internal/clients/export"
	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/identity"
)

// ExportJobExecutor handles export payload type jobs
type ExportJobExecutor struct {
	exportClient  *export.Client
	notifier      JobCompletionNotifier
	userValidator identity.UserValidator
	config        *config.Config
}

// NewExportJobExecutor creates a new ExportJobExecutor
func NewExportJobExecutor(cfg *config.Config, userValidator identity.UserValidator, notifier JobCompletionNotifier) *ExportJobExecutor {
	exportClient := export.NewClient(cfg.ExportService.BaseURL)

	return &ExportJobExecutor{
		exportClient:  exportClient,
		notifier:      notifier,
		userValidator: userValidator,
		config:        cfg,
	}
}

// Execute executes an export job
func (e *ExportJobExecutor) Execute(job domain.Job) error {
	// Use a longer timeout for export operations as they can take time
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Generate identity header for the export request
	identityHeader, err := e.userValidator.GenerateIdentityHeader(ctx, job.OrgID, job.Username, job.UserID)
	if err != nil {
		return fmt.Errorf("failed to generate identity header: %w", err)
	}

	// Cast payload to map[string]interface{}
	details, ok := job.Payload.(map[string]interface{})
	if !ok {
		return fmt.Errorf("payload is not a map[string]interface{}")
	}

	// Extract export configuration from job details
	exportName, _ := details["name"].(string)
	if exportName == "" {
		exportName = fmt.Sprintf("Scheduled Export %s", time.Now().Format("2006-01-02 15:04:05"))
	}

	formatStr, _ := details["format"].(string)
	var format export.ExportFormat
	switch formatStr {
	case "csv":
		format = export.FormatCSV
	default:
		format = export.FormatJSON
	}

	/*
	   subscriptionsFilters := map[string]interface{}{
	           "product_id": "RHEL for x86",
	   }
	*/

	req := export.ExportRequest{
		Name:   exportName,
		Format: export.FormatJSON,
		Sources: []export.Source{
			{
				Application: export.Application("urn:redhat:application:inventory"),
				Resource:    "urn:redhat:application:inventory:export:systems",
			},
			/*
			   {
			           Application: export.Application("subscriptions"),
			           Resource:    "instances",
			           Filters:     subscriptionsFilters,
			   },
			*/
		},
	}

	// Set expiration if specified
	if expirationStr, ok := details["expiration"].(string); ok {
		if expiration, err := time.Parse(time.RFC3339, expirationStr); err == nil {
			req.Expiration = &expiration
		}
	}

	log.Printf("Creating export request: %s (format: %s, sources: %d)", exportName, format, len(req.Sources))

	// Create the export
	result, err := e.exportClient.CreateExport(ctx, req, identityHeader)
	if err != nil {
		return fmt.Errorf("failed to create export: %w", err)
	}

	log.Printf("Export created successfully - ID: %s, Status: %s", result.ID, result.Status)

	// Optionally wait for completion if specified in details
	log.Printf("Waiting for export %s to complete...", result.ID)

	// Use configuration values for polling
	maxRetries := e.config.ExportService.PollMaxRetries
	pollInterval := e.config.ExportService.PollInterval

	// Allow job details to override poll settings
	if maxRetriesVal, ok := details["poll_max_retries"].(float64); ok {
		maxRetries = int(maxRetriesVal)
	}
	if pollIntervalStr, ok := details["poll_interval"].(string); ok {
		if parsed, err := time.ParseDuration(pollIntervalStr); err == nil {
			pollInterval = parsed
		}
	}

	log.Printf("Polling export with maxRetries=%d, pollInterval=%s", maxRetries, pollInterval)

	finalStatus, err := export.WaitForExportCompletion(e.exportClient, ctx, result.ID, identityHeader, maxRetries, pollInterval)
	if err != nil {
		return fmt.Errorf("export failed or timed out: %w", err)
	}

	log.Printf("Export %s completed with status: %s", result.ID, finalStatus.Status)

	// Send notification
	downloadURL := ""
	errorMsg := ""

	if finalStatus.Status == export.StatusComplete {
		downloadURL = e.exportClient.GetExportDownloadURL(result.ID)
	} else if finalStatus.Status == export.StatusFailed {
		// Extract error message if available from the status
		if len(finalStatus.Sources) > 0 && finalStatus.Sources[0].Error != nil {
			errorMsg = *finalStatus.Sources[0].Error
		} else {
			errorMsg = "Export processing failed"
		}
	}

	notification := &ExportCompletionNotification{
		ExportID:    result.ID,
		JobID:       job.ID,
		AccountID:   "", // FIXME: account
		OrgID:       job.OrgID,
		Status:      string(finalStatus.Status),
		DownloadURL: downloadURL,
		ErrorMsg:    errorMsg,
	}

	if err := e.notifier.JobComplete(ctx, notification); err != nil {
		// Don't fail the job execution if notification fails
		log.Printf("Warning: Failed to send completion notification for export %s", result.ID)
	}

	log.Printf("Scheduled report has been generated")

	path := e.exportClient.GetExportDownloadURL(result.ID)

	log.Printf("Sending email to notify customer of generation of scheduled report")
	log.Printf("Hello Valued Customer, your report can be downloaded from here: %s", path)

	return nil
}
