package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
	exportClient := export.NewClient(cfg.ExportService.BaseURL, cfg.ExportService.PublicBaseURL)

	return &ExportJobExecutor{
		exportClient:  exportClient,
		notifier:      notifier,
		userValidator: userValidator,
		config:        cfg,
	}
}

// Execute executes an export job
func (e *ExportJobExecutor) Execute(job domain.Job, logger *slog.Logger) (interface{}, domain.ResultType, error) {
	// Use a longer timeout for export operations as they can take time
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Generate identity header for the export request
	identityHeader, err := e.userValidator.GenerateIdentityHeader(ctx, job.OrgID, job.UserID)
	if err != nil {
		logger.Error("Failed to verify user", slog.Any("error", err))
		return nil, domain.ResultTypeExport, fmt.Errorf("failed to verify user: %w", err)
	}

	// Marshal the payload to JSON then unmarshal into ExportRequest
	// This preserves the payload structure exactly as provided
	payloadJSON, err := json.Marshal(job.Payload)
	if err != nil {
		logger.Error("Failed to marshal payload", slog.Any("error", err))
		return nil, domain.ResultTypeExport, fmt.Errorf("failed to marshal payload: %w", err)
	}

	var req export.ExportRequest
	if err := json.Unmarshal(payloadJSON, &req); err != nil {
		logger.Error("Failed to unmarshal payload into ExportRequest", slog.Any("error", err))
		return nil, domain.ResultTypeExport, fmt.Errorf("failed to unmarshal payload into ExportRequest: %w", err)
	}

	logger.Info("Creating export request",
		slog.String("export_name", req.Name),
		slog.String("format", string(req.Format)),
		slog.Int("sources_count", len(req.Sources)))

	// Create the export
	createResult, err := e.exportClient.CreateExport(ctx, req, identityHeader)
	if err != nil {
		logger.Error("Failed to create export", slog.Any("error", err))
		return nil, domain.ResultTypeExport, fmt.Errorf("failed to create export: %w", err)
	}

	logger.Info("Export created successfully",
		slog.String("export_id", createResult.ID),
		slog.String("status", string(createResult.Status)))

	// Wait for completion using configuration values for polling
	maxRetries := e.config.ExportService.PollMaxRetries
	pollInterval := e.config.ExportService.PollInterval

	logger.Debug("Waiting for export to complete",
		slog.String("export_id", createResult.ID),
		slog.Int("max_retries", maxRetries),
		slog.Duration("poll_interval", pollInterval))

	finalStatus, err := e.exportClient.WaitForExportCompletion(ctx, createResult.ID, identityHeader, maxRetries, pollInterval)
	if err != nil {
		logger.Error("Export failed or timed out",
			slog.String("export_id", createResult.ID),
			slog.Any("error", err))
		return nil, domain.ResultTypeExport, fmt.Errorf("export failed or timed out: %w", err)
	}

	logger.Info("Export completed",
		slog.String("export_id", createResult.ID),
		slog.String("status", string(finalStatus.Status)))

	// Send notification
	downloadURL := ""
	errorMsg := ""

	if finalStatus.Status == export.StatusComplete {
		downloadURL = e.exportClient.GetExportDownloadURL(createResult.ID)
	} else if finalStatus.Status == export.StatusFailed {
		// Extract error message if available from the status
		if len(finalStatus.Sources) > 0 && finalStatus.Sources[0].Error != nil {
			errorMsg = *finalStatus.Sources[0].Error
		} else {
			errorMsg = "Export processing failed"
		}
	}

	notification := &ExportCompletionNotification{
		ExportID:    createResult.ID,
		JobID:       job.ID,
		JobName:     job.Name,
		AccountID:   "", // FIXME: account
		OrgID:       job.OrgID,
		Status:      string(finalStatus.Status),
		DownloadURL: downloadURL,
		ErrorMsg:    errorMsg,
	}

	if err := e.notifier.JobComplete(ctx, notification, logger); err != nil {
		// Don't fail the job execution if notification fails
		logger.Warn("Failed to send completion notification",
			slog.String("export_id", createResult.ID),
			slog.Any("error", err))
	}

	logger.Info("Scheduled report has been generated")

	path := e.exportClient.GetExportDownloadURL(createResult.ID)
	logger.Info("Report download URL generated", slog.String("download_url", path))

	// Build typed result
	result := domain.ExportResult{
		ExportID: createResult.ID,
	}

	if finalStatus.Status == export.StatusComplete {
		result.URL = e.exportClient.GetExportDownloadURL(createResult.ID)
	}

	return result, domain.ResultTypeExport, nil
}
