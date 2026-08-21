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
	"insights-scheduler/internal/core/ports"
	"insights-scheduler/internal/identity"
)

// ExportJobExecutor handles export payload type jobs.
// It creates the export and saves the external job ID, then returns immediately.
// Polling for completion is handled by ExportPollerService.
type ExportJobExecutor struct {
	exportClient    *export.Client
	userValidator   identity.UserValidator
	config          *config.Config
	runRepo         JobRunRepository
	payloadResolver ports.PayloadResolver
}

// JobRunRepository defines the minimal interface needed for saving external job IDs
type JobRunRepository interface {
	Save(run domain.JobRun) error
	FindByID(id string) (domain.JobRun, error)
}

// NewExportJobExecutor creates a new ExportJobExecutor.
// payloadResolver resolves CEL templates in the job payload before kick-off;
// polling, notifications, and failure tracking are handled by ExportPollerService.
func NewExportJobExecutor(cfg *config.Config, userValidator identity.UserValidator, runRepo JobRunRepository, payloadResolver ports.PayloadResolver) *ExportJobExecutor {
	exportClient := export.NewClient(cfg.ExportService.BaseURL, cfg.ExportService.PublicBaseURL)

	return &ExportJobExecutor{
		exportClient:    exportClient,
		userValidator:   userValidator,
		config:          cfg,
		runRepo:         runRepo,
		payloadResolver: payloadResolver,
	}
}

// Execute kicks off an export and returns immediately.
// Returns ResultTypePending so the executor skips the completion save.
// The ExportPollerService handles polling, notifications, and failure tracking.
func (e *ExportJobExecutor) Execute(job domain.Job, jobRunID string, logger *slog.Logger) (interface{}, domain.ResultType, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	identityHeader, err := e.userValidator.GenerateIdentityHeader(ctx, job.OrgID, job.UserID)
	if err != nil {
		logger.Error("Failed to verify user", slog.Any("error", err))
		return nil, domain.ResultTypeExport, fmt.Errorf("failed to verify user: %w", err)
	}

	// Resolve CEL expressions in the payload before kick-off
	resolvedPayload := job.Payload
	if e.payloadResolver != nil {
		evalCtx := map[string]any{
			"now":    time.Now().UTC(),
			"job_id": job.ID,
		}
		resolved, err := e.payloadResolver.ProcessPayload(job.Payload, evalCtx)
		if err != nil {
			logger.Error("Failed to resolve payload templates", slog.Any("error", err))
			return nil, domain.ResultTypeExport, fmt.Errorf("failed to resolve payload templates: %w", err)
		}
		resolvedPayload = resolved
	}

	// Marshal the payload to JSON then unmarshal into ExportRequest
	// This preserves the payload structure exactly as provided
	payloadJSON, err := json.Marshal(resolvedPayload)
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

	createResult, err := e.exportClient.CreateExport(ctx, req, identityHeader)
	if err != nil {
		logger.Error("Failed to create export", slog.Any("error", err))
		return nil, domain.ResultTypeExport, fmt.Errorf("failed to create export: %w", err)
	}

	logger.Info("Export created successfully",
		slog.String("export_id", createResult.ID),
		slog.String("status", string(createResult.Status)))

	if jobRunID != "" && e.runRepo != nil {
		jobRun, err := e.runRepo.FindByID(jobRunID)
		if err != nil {
			logger.Error("Failed to load job run for external ID save",
				slog.String("job_run_id", jobRunID),
				slog.Any("error", err))
			return nil, domain.ResultTypeExport, fmt.Errorf("failed to load job run for external ID save: %w", err)
		}

		jobRun = jobRun.WithExternalJob(createResult.ID, "export")
		if err := e.runRepo.Save(jobRun); err != nil {
			logger.Error("Failed to save external job ID - job run will be orphaned",
				slog.String("export_id", createResult.ID),
				slog.Any("error", err))
			return nil, domain.ResultTypeExport, fmt.Errorf("failed to save external job ID: %w", err)
		}

		logger.Info("Saved external job ID for polling",
			slog.String("export_id", createResult.ID),
			slog.String("job_run_id", jobRunID))
	}

	return nil, domain.ResultTypePending, nil
}
