# Red Hat Insights Export Service Client

A Go REST client for the [Red Hat Insights Export Service](https://github.com/RedHatInsights/export-service-go) API.

## Features

- **Complete API Coverage** - All export service endpoints (create, list, status, download, delete)
- **Type-Safe** - Strongly typed Go structs for all API entities
- **Authentication** - Built-in Red Hat identity header handling
- **Error Handling** - Proper error parsing and reporting
- **Context Support** - All operations support context for timeouts/cancellation
- **Testing** - Comprehensive unit tests with mock server
- **CLI Tool** - Command-line interface for interactive use

## Installation

```bash
# Build the export CLI tool
make build-export-cli

# Run tests
go test ./internal/clients/export/...
```

## Usage

### Go Library

```go
import "insights-scheduler/internal/clients/export"

// Create client
client := export.NewClient(
    "https://console.redhat.com/api/export/v1",
    "your-account-number",
    "your-org-id",
)

// Create an export
req := export.ExportRequest{
    Name:   "My Export",
    Format: export.FormatJSON,
    Sources: map[string]export.Source{
        "advisor_data": {
            Application: export.AppAdvisor,
            Resource:    "recommendations",
            Filters: map[string]interface{}{
                "severity": "high",
            },
        },
    },
}

result, err := client.CreateExport(context.Background(), req)
if err != nil {
    log.Fatal(err)
}

// Wait for completion
status, err := export.WaitForExportCompletion(
    client, 
    context.Background(), 
    result.ID, 
    5*time.Minute,
)
if err != nil {
    log.Fatal(err)
}

// Download the export
data, err := client.DownloadExport(context.Background(), result.ID)
if err != nil {
    log.Fatal(err)
}

// Save to file
os.WriteFile("export.zip", data, 0644)
```

### CLI Tool

```bash
# List all exports
./bin/export-cli -account=123456 -org=org123 -cmd=list

# Create a new export
./bin/export-cli -account=123456 -org=org123 -cmd=create -name="My Export" -format=json

# Check export status
./bin/export-cli -account=123456 -org=org123 -cmd=status -id=export-id

# Download completed export
./bin/export-cli -account=123456 -org=org123 -cmd=download -id=export-id -output=export.zip

# Delete an export
./bin/export-cli -account=123456 -org=org123 -cmd=delete -id=export-id
```

## API Reference

### Client Methods

- `CreateExport(ctx, req)` - Create a new export request
- `ListExports(ctx, params)` - List export requests with optional filters
- `GetExportStatus(ctx, exportID)` - Get detailed status of an export
- `DownloadExport(ctx, exportID)` - Download completed export as zip file
- `DeleteExport(ctx, exportID)` - Delete an export request

### Supported Applications

- `advisor` - Red Hat Insights Advisor
- `compliance` - Compliance scanning
- `drift-service` - Configuration drift detection
- `image-builder` - Image building service
- `inventory` - System inventory
- `patch-manager` - Patch management
- `policy-engine` - Policy evaluation
- `resource-optimization` - Resource optimization recommendations
- `subscriptions-service` - Subscription management
- `system-baseline` - System baseline comparison
- `vulnerability-engine` - Vulnerability scanning

### Export Formats

- `json` - JSON format
- `csv` - CSV format

### Export Statuses

- `pending` - Export request received, waiting to start
- `running` - Export in progress
- `partial` - Some data sources completed
- `complete` - All data exported successfully
- `failed` - Export failed

## Authentication

The client uses Red Hat's 3Scale identity authentication. You need:

1. **Account Number** - Your Red Hat account number
2. **Organization ID** - Your organization ID

The client automatically constructs the required `x-rh-identity` header with base64-encoded JSON containing your identity information.

## Error Handling

The client provides detailed error information including:

- HTTP status codes
- API error messages
- Request/response parsing errors
- Network timeouts

```go
result, err := client.CreateExport(ctx, req)
if err != nil {
    // Error contains full context
    log.Printf("Export creation failed: %v", err)
    // Example: "API error (status 400): invalid_request - Export name is required"
}
```

## Testing

Run the test suite:

```bash
go test ./internal/clients/export/... -v
```

Tests include:
- All API endpoint operations
- Authentication header generation
- Error response handling
- Request/response serialization

## Examples

See `internal/clients/export/examples.go` for additional usage patterns:

- Creating application-specific exports (Advisor, Compliance, Inventory)
- Polling for export completion
- Handling different export formats
- Working with filters and data sources