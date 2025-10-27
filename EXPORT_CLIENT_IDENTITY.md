# Export Client Identity Header Integration

This document explains the changes made to the export client to require identity headers for CreateExport calls.

## Overview

The export client has been modified to require Red Hat Identity headers when making CreateExport requests to the export service. This ensures that export requests are properly authenticated and associated with the correct user and organization.

## Changes Made

### 1. Updated CreateExport Method Signature

**Before:**
```go
func (c *Client) CreateExport(ctx context.Context, req ExportRequest) (*ExportStatusResponse, error)
```

**After:**
```go
func (c *Client) CreateExport(ctx context.Context, req ExportRequest, identityHeader string) (*ExportStatusResponse, error)
```

### 2. Added Identity Header Generation Method

A new method was added to generate identity headers from organization ID and username:

```go
func (c *Client) GenerateIdentityHeader(orgID, username string) (string, error)
```

This method creates a base64-encoded identity header in the format expected by Red Hat services:

```json
{
  "identity": {
    "account_number": "000001",
    "org_id": "user-org-id",
    "type": "User",
    "auth_type": "jwt-auth",
    "internal": {
      "org_id": "user-org-id"
    },
    "user": {
      "username": "user-username",
      "user_id": "user-username-id"
    }
  }
}
```

### 3. Added createRequestWithIdentity Method

A new internal method was added to create HTTP requests with custom identity headers:

```go
func (c *Client) createRequestWithIdentity(ctx context.Context, method, endpoint string, body interface{}, identityHeader string) (*http.Request, error)
```

### 4. Updated Job Executor Integration

The job executor now generates identity headers when creating exports:

```go
// Generate identity header for the export request
identityHeader, err := e.exportClient.GenerateIdentityHeader(job.OrgID, job.Username)
if err != nil {
    return fmt.Errorf("failed to generate identity header: %w", err)
}

// Create the export
result, err := e.exportClient.CreateExport(ctx, req, identityHeader)
```

## Usage Examples

### Direct Usage
```go
client := export.NewClient("https://export-service/api/v1", "000001", "org123")

// Generate identity header
identityHeader, err := client.GenerateIdentityHeader("org123", "john.doe")
if err != nil {
    return err
}

// Create export with identity
req := export.ExportRequest{
    Name:   "My Export",
    Format: export.FormatJSON,
    Sources: []export.Source{
        {
            Application: export.AppAdvisor,
            Resource:    "recommendations",
        },
    },
}

result, err := client.CreateExport(ctx, req, identityHeader)
```

### Job Executor Usage
When jobs are executed, the identity header is automatically generated from the job's organization ID and username:

```go
// This happens automatically in the job executor
identityHeader, err := e.exportClient.GenerateIdentityHeader(job.OrgID, job.Username)
result, err := e.exportClient.CreateExport(ctx, req, identityHeader)
```

## Security Benefits

1. **Authentication**: All export requests now include proper Red Hat identity information
2. **Authorization**: Export service can validate the user's permissions
3. **Audit Trail**: Export requests are associated with specific users and organizations
4. **Multi-Tenancy**: Ensures exports are created within the correct organizational context

## Testing

All tests have been updated to use the new CreateExport signature:

```go
func TestClient_CreateExport(t *testing.T) {
    client := NewClient(server.URL, "123456", "org123")
    
    // Generate identity header for the test
    identityHeader, err := client.GenerateIdentityHeader("org123", "testuser")
    if err != nil {
        t.Fatalf("GenerateIdentityHeader failed: %v", err)
    }

    result, err := client.CreateExport(context.Background(), req, identityHeader)
    // ... rest of test
}
```

A dedicated test was added to verify the GenerateIdentityHeader functionality:

```go
func TestClient_GenerateIdentityHeader(t *testing.T) {
    // Verifies that the identity header is properly formatted and contains correct data
}
```

## Backward Compatibility

- The old `createRequest` method is preserved for other API calls that use default identity
- The `CreateExport` method signature change is intentional and requires updating all callers
- Examples and tests have been updated to demonstrate proper usage

## Migration Notes

Any existing code calling `CreateExport` will need to be updated to:

1. Generate an identity header using `GenerateIdentityHeader(orgID, username)`
2. Pass the identity header as the third parameter to `CreateExport`

This ensures all export creation requests are properly authenticated with user context.