package main

import (
	"context"
	//	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"insights-scheduler/internal/clients/export"
)

func main() {
	var (
		baseURL       = flag.String("url", "https://console.redhat.com/api/export/v1", "Export service base URL")
		accountNumber = flag.String("account", "", "Red Hat account number")
		orgID         = flag.String("org", "", "Organization ID")
		command       = flag.String("cmd", "list", "Command to execute (list, create, status, download, delete)")
		exportID      = flag.String("id", "", "Export ID for status, download, or delete commands")
		name          = flag.String("name", "", "Export name for create command")
		format        = flag.String("format", "json", "Export format (json, csv)")
		application   = flag.String("app", "", "Filter by application")
		output        = flag.String("output", "", "Output file for download command")
	)
	flag.Parse()

	if *accountNumber == "" || *orgID == "" {
		log.Fatal("Account number and organization ID are required")
	}

	client := export.NewClient(*baseURL, *accountNumber, *orgID)
	ctx := context.Background()

	switch *command {
	case "list":
		listExports(client, ctx, *application)
	case "create":
		createExport(client, ctx, *name, *format)
	case "status":
		getStatus(client, ctx, *exportID)
	case "download":
		downloadExport(client, ctx, *exportID, *output)
	case "delete":
		deleteExport(client, ctx, *exportID)
	default:
		fmt.Printf("Unknown command: %s\n", *command)
		flag.Usage()
		os.Exit(1)
	}
}

func listExports(client *export.Client, ctx context.Context, appFilter string) {
	params := &export.ListParams{}

	if appFilter != "" {
		app := export.Application(appFilter)
		params.Application = &app
	}

	result, err := client.ListExports(ctx, params)
	if err != nil {
		log.Fatalf("Failed to list exports: %v", err)
	}

	fmt.Printf("Found %d exports:\n", result.Meta.Count)
	for _, exp := range result.Data {
		fmt.Printf("- %s (%s): %s [%s]\n", exp.Name, exp.ID, exp.Status, exp.Format)
		if exp.CompletedAt != nil {
			fmt.Printf("  Completed: %s\n", exp.CompletedAt.Format(time.RFC3339))
		}
		if exp.ExpiresAt != nil {
			fmt.Printf("  Expires: %s\n", exp.ExpiresAt.Format(time.RFC3339))
		}
		fmt.Println()
	}
}

func createExport(client *export.Client, ctx context.Context, name, format string) {
	if name == "" {
		name = fmt.Sprintf("CLI Export %s", time.Now().Format("2006-01-02 15:04:05"))
	}

	var exportFormat export.ExportFormat
	switch format {
	case "json":
		exportFormat = export.FormatJSON
	case "csv":
		exportFormat = export.FormatCSV
	default:
		log.Fatalf("Invalid format: %s (use json or csv)", format)
	}

	req := export.ExportRequest{
		Name:   name,
		Format: exportFormat,
		Sources: []export.Source{
			{
				Application: export.AppAdvisor,
				Resource:    "recommendations",
				Filters: map[string]interface{}{
					"severity": "high",
				},
			},
		},
	}

	result, err := client.CreateExport(ctx, req, "FIXME")
	if err != nil {
		log.Fatalf("Failed to create export: %v", err)
	}

	fmt.Printf("Export created successfully:\n")
	printExportStatus(result)
}

func getStatus(client *export.Client, ctx context.Context, exportID string) {
	if exportID == "" {
		log.Fatal("Export ID is required for status command")
	}

	status, err := client.GetExportStatus(ctx, exportID)
	if err != nil {
		log.Fatalf("Failed to get export status: %v", err)
	}

	printExportStatus(status)
}

func downloadExport(client *export.Client, ctx context.Context, exportID, output string) {
	if exportID == "" {
		log.Fatal("Export ID is required for download command")
	}

	// Check if export is ready
	status, err := client.GetExportStatus(ctx, exportID)
	if err != nil {
		log.Fatalf("Failed to get export status: %v", err)
	}

	if status.Status != export.StatusComplete {
		log.Fatalf("Export is not ready for download. Status: %s", status.Status)
	}

	data, err := client.DownloadExport(ctx, exportID)
	if err != nil {
		log.Fatalf("Failed to download export: %v", err)
	}

	if output == "" {
		output = fmt.Sprintf("export_%s.zip", exportID)
	}

	if err := os.WriteFile(output, data, 0644); err != nil {
		log.Fatalf("Failed to write file: %v", err)
	}

	fmt.Printf("Export downloaded to %s (%d bytes)\n", output, len(data))
}

func deleteExport(client *export.Client, ctx context.Context, exportID string) {
	if exportID == "" {
		log.Fatal("Export ID is required for delete command")
	}

	if err := client.DeleteExport(ctx, exportID); err != nil {
		log.Fatalf("Failed to delete export: %v", err)
	}

	fmt.Printf("Export %s deleted successfully\n", exportID)
}

func printExportStatus(status *export.ExportStatusResponse) {
	fmt.Printf("Export Details:\n")
	fmt.Printf("  ID: %s\n", status.ID)
	fmt.Printf("  Name: %s\n", status.Name)
	fmt.Printf("  Format: %s\n", status.Format)
	fmt.Printf("  Status: %s\n", status.Status)
	fmt.Printf("  Created: %s\n", status.CreatedAt.Format(time.RFC3339))

	if status.CompletedAt != nil {
		fmt.Printf("  Completed: %s\n", status.CompletedAt.Format(time.RFC3339))
	}

	if status.ExpiresAt != nil {
		fmt.Printf("  Expires: %s\n", status.ExpiresAt.Format(time.RFC3339))
	}

	if len(status.Sources) > 0 {
		fmt.Printf("  Sources:\n")
		for i, source := range status.Sources {
			fmt.Printf("    Source %d (%s/%s): %s\n", i, source.Application, source.Resource, source.Status)
			if source.Error != nil {
				fmt.Printf("      Error: %s\n", *source.Error)
			}
		}
	}
}
