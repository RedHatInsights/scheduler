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
	"insights-scheduler/internal/shell/messaging"
)

type DefaultJobExecutor struct {
	exportClient  *export.Client
	kafkaProducer *messaging.KafkaProducer
	userValidator identity.UserValidator
	config        *config.Config
}

func NewDefaultJobExecutor(cfg *config.Config) *DefaultJobExecutor {
	exportClient := export.NewClient(
		cfg.ExportService.BaseURL,
		cfg.ExportService.AccountNumber,
		cfg.ExportService.OrgID,
	)

	var userValidator identity.UserValidator

	if cfg.Bop.Enabled == true {
		fmt.Println("Intializing BOP User Validator")
		userValidator = identity.NewBopUserValidator(
			cfg.Bop.BaseURL,
			cfg.Bop.APIToken,
			cfg.Bop.ClientID,
			cfg.Bop.InsightsEnv,
		)
	} else {
		fmt.Println("Intializing FAKE User Validator")
		userValidator = identity.NewDefaultUserValidator(cfg.ExportService.AccountNumber)
	}

	return &DefaultJobExecutor{
		exportClient:  exportClient,
		kafkaProducer: nil, // No Kafka producer by default
		userValidator: userValidator,
		config:        cfg,
	}
}

func NewDefaultJobExecutorWithExportClient(exportClient *export.Client, cfg *config.Config) *DefaultJobExecutor {
	userValidator := identity.NewDefaultUserValidator(cfg.ExportService.AccountNumber)

	return &DefaultJobExecutor{
		exportClient:  exportClient,
		kafkaProducer: nil, // No Kafka producer by default
		userValidator: userValidator,
		config:        cfg,
	}
}

func NewDefaultJobExecutorWithKafka(kafkaProducer *messaging.KafkaProducer, cfg *config.Config) *DefaultJobExecutor {

	jobExecutor := NewDefaultJobExecutor(cfg)

	jobExecutor.kafkaProducer = kafkaProducer

	return jobExecutor
}

func NewDefaultJobExecutorWithClients(exportClient *export.Client, kafkaProducer *messaging.KafkaProducer, cfg *config.Config) *DefaultJobExecutor {
	userValidator := identity.NewDefaultUserValidator(cfg.ExportService.AccountNumber)

	return &DefaultJobExecutor{
		exportClient:  exportClient,
		kafkaProducer: kafkaProducer,
		userValidator: userValidator,
		config:        cfg,
	}
}

func (e *DefaultJobExecutor) Execute(job domain.Job) error {
	log.Printf("Executing job: %s (%s)", job.Name, job.ID)

	switch job.Payload.Type {
	case domain.PayloadMessage:
		return e.executeMessage(job.Payload.Details)
	case domain.PayloadHTTPRequest:
		return e.executeHTTPRequest(job.Payload.Details)
	case domain.PayloadCommand:
		return e.executeCommand(job.Payload.Details)
	case domain.PayloadExport:
		return e.executeExportReport(job, job.Payload.Details)
	default:
		return fmt.Errorf("unknown payload type: %s", job.Payload.Type)
	}
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

	timeout := 5 * time.Minute // Default timeout
	if timeoutStr, ok := details["timeout"].(string); ok {
		if parsed, err := time.ParseDuration(timeoutStr); err == nil {
			timeout = parsed
		}
	}

	finalStatus, err := export.WaitForExportCompletion(e.exportClient, ctx, result.ID, timeout)
	if err != nil {
		return fmt.Errorf("export failed or timed out: %w", err)
	}

	log.Printf("Export %s completed with status: %s", result.ID, finalStatus.Status)

	// Send Kafka message if producer is available
	if e.kafkaProducer != nil {
		log.Printf("Sending platform notification for export completion: %s", result.ID)

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

		// Create platform notification message
		notification := messaging.NewExportCompletionNotification(
			result.ID,
			job.ID,
			e.config.ExportService.AccountNumber,
			job.OrgID,
			string(finalStatus.Status),
			downloadURL,
			errorMsg,
		)

		if err := e.kafkaProducer.SendNotificationMessage(notification); err != nil {
			log.Printf("Failed to send platform notification for export %s: %v", result.ID, err)
			// Don't fail the job execution if Kafka message fails
		} else {
			log.Printf("Platform notification sent successfully for export %s", result.ID)
		}
	} else {
		log.Printf("No Kafka producer configured, skipping notification send")
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
