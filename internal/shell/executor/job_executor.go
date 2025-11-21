package executor

import (
	"context"
	"fmt"
	"log"
	"time"

	"insights-scheduler/internal/clients/export"
	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/usecases"
	"insights-scheduler/internal/identity"
)

type DefaultJobExecutor struct {
	exportClient  *export.Client
	notifier      JobCompletionNotifier
	userValidator identity.UserValidator
	config        *config.Config
	runRepo       usecases.JobRunRepository
}

func NewJobExecutor(cfg *config.Config, userValidator identity.UserValidator, notifier JobCompletionNotifier, runRepo usecases.JobRunRepository) *DefaultJobExecutor {
	exportClient := export.NewClient(
		cfg.ExportService.BaseURL,
	)

	return &DefaultJobExecutor{
		exportClient:  exportClient,
		notifier:      notifier,
		userValidator: userValidator,
		config:        cfg,
		runRepo:       runRepo,
	}
}

func (e *DefaultJobExecutor) Execute(job domain.Job) error {
	log.Printf("Executing job: %s (%s)", job.Name, job.ID)

	// Create a job run record
	var jobRun domain.JobRun
	if e.runRepo != nil {
		jobRun = domain.NewJobRun(job.ID)
		if err := e.runRepo.Save(jobRun); err != nil {
			log.Printf("Failed to create job run record: %v", err)
			// Continue with execution even if we can't save the run
		} else {
			log.Printf("Created job run: %s for job: %s", jobRun.ID, job.ID)
		}
	}

	// Execute the job
	var execErr error
	switch job.Payload.Type {
	case domain.PayloadMessage:
		execErr = e.executeMessage(job.Payload.Details)
	case domain.PayloadHTTPRequest:
		execErr = e.executeHTTPRequest(job.Payload.Details)
	case domain.PayloadCommand:
		execErr = e.executeCommand(job.Payload.Details)
	case domain.PayloadExport:
		execErr = e.executeExportReport(job, job.Payload.Details)
	default:
		execErr = fmt.Errorf("unknown payload type: %s", job.Payload.Type)
	}

	// Update the job run record
	if e.runRepo != nil && jobRun.ID != "" {
		if execErr != nil {
			jobRun = jobRun.WithFailed(execErr.Error())
		} else {
			result := fmt.Sprintf("Job %s completed successfully", job.Name)
			jobRun = jobRun.WithCompleted(result)
		}

		if err := e.runRepo.Save(jobRun); err != nil {
			log.Printf("Failed to update job run record: %v", err)
		} else {
			log.Printf("Updated job run: %s with status: %s", jobRun.ID, jobRun.Status)
		}
	}

	return execErr
}

func (e *DefaultJobExecutor) executeMessage(details map[string]interface{}) error {
	message, ok := details["message"].(string)
	if !ok {
		message = "unknown"
	}
	log.Printf("Processing message: %s", message)
	return nil
}

func (e *DefaultJobExecutor) executeExportReport(job domain.Job, details map[string]interface{}) error {
	// Use a longer timeout for export operations as they can take time
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Generate identity header for the export request
	identityHeader, err := e.userValidator.GenerateIdentityHeader(job.OrgID, job.Username, job.UserID)
	if err != nil {
		return fmt.Errorf("failed to generate identity header: %w", err)
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
			/*
				{
					Application: export.Application("subscriptions"),
					Resource:    "instances",
					Filters:     subscriptionsFilters,
				},
			*/
			{
				Application: export.Application("urn:redhat:application:inventory"),
				Resource:    "urn:redhat:application:inventory:export:systems",
			},
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

func (e *DefaultJobExecutor) executeHTTPRequest(details map[string]interface{}) error {
	url, _ := details["url"].(string)
	method, _ := details["method"].(string)
	if method == "" {
		method = "GET"
	}
	if url == "" {
		url = "unknown"
	}
	log.Printf("Executing HTTP %s request to %s", method, url)
	return nil
}

func (e *DefaultJobExecutor) executeCommand(details map[string]interface{}) error {
	command, ok := details["command"].(string)
	if !ok {
		command = "unknown"
	}
	log.Printf("Executing command: %s", command)
	return nil
}
