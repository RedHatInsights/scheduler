package export

import (
	"context"
	"fmt"
	"log"
	"time"

	"insights-scheduler/internal/identity"
)

// ExampleUsage demonstrates how to use the export service client
func ExampleUsage() {
	// Create a client
	client := NewClient(
		"https://console.redhat.com/api/export/v1",
	)

	ctx := context.Background()

	// Create an export request
	exportReq := ExportRequest{
		Name:   "Monthly Advisor Report",
		Format: FormatJSON,
		Expiration: func() *time.Time {
			exp := time.Now().Add(7 * 24 * time.Hour) // 7 days
			return &exp
		}(),
		Sources: []Source{
			{
				Application: AppAdvisor,
				Resource:    "recommendations",
				Filters: map[string]interface{}{
					"severity": "high",
					"category": "performance",
				},
			},
			{
				Application: AppCompliance,
				Resource:    "reports",
				Filters: map[string]interface{}{
					"policy_type": "security",
				},
			},
		},
	}

	// Generate identity header using UserValidator
	userValidator := identity.NewFakeUserValidator() // Use client's account number
	identityHeader, err := userValidator.GenerateIdentityHeader("000001", "example-user", "user-123")
	if err != nil {
		log.Fatalf("Failed to generate identity header: %v", err)
	}

	// Create the export
	export, err := client.CreateExport(ctx, exportReq, identityHeader)
	if err != nil {
		log.Fatalf("Failed to create export: %v", err)
	}

	fmt.Printf("Export created with ID: %s\n", export.ID)

	// Poll for completion
	for {
		status, err := client.GetExportStatus(ctx, export.ID, identityHeader)
		if err != nil {
			log.Printf("Failed to get status: %v", err)
			break
		}

		fmt.Printf("Export status: %s\n", status.Status)

		if status.Status == StatusComplete {
			// Download the export
			data, err := client.DownloadExport(ctx, export.ID, identityHeader)
			if err != nil {
				log.Printf("Failed to download: %v", err)
				break
			}

			fmt.Printf("Downloaded %d bytes\n", len(data))
			break
		} else if status.Status == StatusFailed {
			fmt.Println("Export failed")
			break
		}

		// Wait before polling again
		time.Sleep(10 * time.Second)
	}

	// List all exports
	app := AppAdvisor
	status := StatusComplete
	listParams := &ListParams{
		Application: &app,
		Status:      &status,
		Limit:       func() *int { l := 10; return &l }(),
	}

	exports, err := client.ListExports(ctx, listParams, identityHeader)
	if err != nil {
		log.Printf("Failed to list exports: %v", err)
	} else {
		fmt.Printf("Found %d exports\n", exports.Meta.Count)
		for _, exp := range exports.Data {
			fmt.Printf("- %s (%s): %s\n", exp.Name, exp.ID, exp.Status)
		}
	}
}

// CreateAdvisorExport creates an export for Advisor recommendations
func CreateAdvisorExport(client *Client, ctx context.Context) (*ExportStatusResponse, error) {
	req := ExportRequest{
		Name:   fmt.Sprintf("Advisor Export %s", time.Now().Format("2006-01-02")),
		Format: FormatJSON,
		Sources: []Source{
			{
				Application: AppAdvisor,
				Resource:    "recommendations",
				Filters: map[string]interface{}{
					"severity": []string{"high", "critical"},
				},
			},
		},
	}

	// Generate identity header using UserValidator
	userValidator := identity.NewFakeUserValidator() // Use client's account number
	identityHeader, err := userValidator.GenerateIdentityHeader("000001", "example-user", "user-123")
	if err != nil {
		return nil, err
	}
	return client.CreateExport(ctx, req, identityHeader)
}

// CreateComplianceExport creates an export for Compliance reports
func CreateComplianceExport(client *Client, ctx context.Context) (*ExportStatusResponse, error) {
	req := ExportRequest{
		Name:   fmt.Sprintf("Compliance Export %s", time.Now().Format("2006-01-02")),
		Format: FormatCSV,
		Sources: []Source{
			{
				Application: AppCompliance,
				Resource:    "reports",
				Filters: map[string]interface{}{
					"policy_type": "security",
					"status":      "non_compliant",
				},
			},
		},
	}

	// Generate identity header using UserValidator
	userValidator := identity.NewFakeUserValidator() // Use client's account number
	identityHeader, err := userValidator.GenerateIdentityHeader("000001", "example-user", "user-123")
	if err != nil {
		return nil, err
	}
	return client.CreateExport(ctx, req, identityHeader)
}

// CreateInventoryExport creates an export for Inventory systems
func CreateInventoryExport(client *Client, ctx context.Context) (*ExportStatusResponse, error) {
	req := ExportRequest{
		Name:   fmt.Sprintf("Inventory Export %s", time.Now().Format("2006-01-02")),
		Format: FormatJSON,
		Sources: []Source{
			{
				Application: AppInventory,
				Resource:    "systems",
				Filters: map[string]interface{}{
					"status": "active",
					"tags":   map[string]string{"environment": "production"},
				},
			},
		},
	}

	// Generate identity header using UserValidator
	userValidator := identity.NewFakeUserValidator() // Use client's account number
	identityHeader, err := userValidator.GenerateIdentityHeader("000001", "example-user", "user-123")
	if err != nil {
		return nil, err
	}
	return client.CreateExport(ctx, req, identityHeader)
}

// WaitForExportCompletion polls an export until it's complete or failed
func WaitForExportCompletion(client *Client, ctx context.Context, exportID string, identityHeader string, timeout time.Duration) (*ExportStatusResponse, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		status, err := client.GetExportStatus(ctx, exportID, identityHeader)
		if err != nil {
			return nil, fmt.Errorf("failed to get export status: %w", err)
		}

		switch status.Status {
		case StatusComplete:
			return status, nil
		case StatusFailed:
			return status, fmt.Errorf("export failed")
		case StatusPending, StatusRunning, StatusPartial:
			// Continue waiting
			time.Sleep(5 * time.Second)
		default:
			return status, fmt.Errorf("unknown export status: %s", status.Status)
		}
	}

	return nil, fmt.Errorf("export did not complete within timeout")
}
