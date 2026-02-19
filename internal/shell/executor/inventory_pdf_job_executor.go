package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"insights-scheduler/internal/clients/inventory_pdf"
	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/identity"
)

// InventoryPdfJobExecutor handles inventory-pdf payload type jobs
type InventoryPdfJobExecutor struct {
	pdfClient     *inventory_pdf.Client
	notifier      JobCompletionNotifier
	userValidator identity.UserValidator
	config        *config.Config
}

// NewInventoryPdfJobExecutor creates a new InventoryPdfJobExecutor
func NewInventoryPdfJobExecutor(cfg *config.Config, userValidator identity.UserValidator, notifier JobCompletionNotifier) *InventoryPdfJobExecutor {
	pdfClient := inventory_pdf.NewClient(cfg.InventoryPdfService.BaseURL)

	return &InventoryPdfJobExecutor{
		pdfClient:     pdfClient,
		notifier:      notifier,
		userValidator: userValidator,
		config:        cfg,
	}
}

// Execute executes an inventory PDF generation job
func (e *InventoryPdfJobExecutor) Execute(job domain.Job) error {
	// Use a longer timeout for PDF operations as they can take time
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Generate identity header for the PDF request
	identityHeader, err := e.userValidator.GenerateIdentityHeader(ctx, job.OrgID, job.Username, job.UserID)
	if err != nil {
		return fmt.Errorf("failed to generate identity header: %w", err)
	}

	// Marshal the payload to JSON then unmarshal into PDF request
	// This preserves the payload structure exactly as provided
	payloadJSON, err := json.Marshal(job.Payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(payloadJSON, &req); err != nil {
		return fmt.Errorf("failed to unmarshal payload into PDF request: %w", err)
	}

	log.Printf("Creating Inventory PDF request for job: %s", job.Name)

	// Create the PDF
	statusID, err := e.pdfClient.CreatePdf(ctx, req, identityHeader)
	if err != nil {
		return fmt.Errorf("failed to create Inventory PDF: %w", err)
	}

	log.Printf("Inventory PDF created successfully - StatusID: %s", statusID)

	// Wait for completion using configuration values for polling
	log.Printf("Waiting for Inventory PDF %s to complete...", statusID)

	maxRetries := e.config.InventoryPdfService.PollMaxRetries
	pollInterval := e.config.InventoryPdfService.PollInterval

	log.Printf("Polling Inventory PDF with maxRetries=%d, pollInterval=%s", maxRetries, pollInterval)

	finalStatus, _, err := inventory_pdf.WaitForPdfCompletion(e.pdfClient, ctx, statusID, identityHeader, maxRetries, pollInterval)
	if err != nil {
		return fmt.Errorf("Inventory PDF generation failed or timed out: %w", err)
	}

	log.Printf("Inventory PDF %s completed with status: %s", statusID, finalStatus)

	// Send notification
	downloadURL := ""
	errorMsg := ""

	if finalStatus == "Generated" {
		downloadURL = e.pdfClient.GetPdfDownloadURL(statusID)
	} else if finalStatus == "Failed" {
		errorMsg = "Inventory PDF generation failed"
	}

	notification := &ExportCompletionNotification{
		ExportID:    statusID,
		JobID:       job.ID,
		AccountID:   "", // FIXME: account
		OrgID:       job.OrgID,
		Status:      finalStatus,
		DownloadURL: downloadURL,
		ErrorMsg:    errorMsg,
	}

	if err := e.notifier.JobComplete(ctx, notification); err != nil {
		// Don't fail the job execution if notification fails
		log.Printf("Warning: Failed to send completion notification for Inventory PDF %s", statusID)
	}

	log.Printf("Scheduled Inventory PDF has been generated")

	path := e.pdfClient.GetPdfDownloadURL(statusID)

	log.Printf("Sending email to notify customer of generation of scheduled Inventory PDF")
	log.Printf("Hello Valued Customer, your Inventory PDF can be downloaded from here: %s", path)

	return nil
}
