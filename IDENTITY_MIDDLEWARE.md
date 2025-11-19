# Identity Middleware Integration

This document explains how the Red Hat Insights Identity Middleware has been integrated into the Job Scheduler service.

## Overview

The service now uses the `github.com/redhatinsights/platform-go-middlewares/identity` package to enforce authentication and authorization for all API endpoints.

## What Changed

### 1. Middleware Integration
- All `/api/v1/*` routes now require valid Red Hat Identity headers
- The `identity.EnforceIdentity` middleware is applied to all API routes
- User identity information is extracted from the `X-Rh-Identity` header

### 2. Automatic Identity Extraction
- **Organization ID**: Automatically extracted from identity context (`ident.Identity.OrgID`)
- **Username**: Extracted from identity context (`ident.Identity.User.Username`) with fallback to email
- Users can only access jobs within their own organization

### 3. API Changes
**Request Changes:**
- Removed `org_id` and `username` fields from request bodies
- These are now automatically populated from the authenticated user's identity

**Before:**
```json
{
  "name": "My Job",
  "org_id": "12345",
  "username": "john.doe",
  "schedule": "0 */10 * * * *",
  "payload": {
    "type": "message",
    "details": {"message": "Hello"}
  }
}
```

**After:**
```json
{
  "name": "My Job",
  "schedule": "0 */10 * * * *",
  "payload": {
    "type": "message",
    "details": {"message": "Hello"}
  }
}
```

### 4. Security Enhancements
- **Organization Isolation**: Users can only see/manage jobs from their organization
- **Authorization Checks**: All operations verify job ownership before execution
- **No Cross-Org Access**: Jobs from other organizations return "not found" errors

## Required Headers

All API requests must include a valid `X-Rh-Identity` header containing base64-encoded identity information:

```bash
curl -H "X-Rh-Identity: <base64-encoded-identity>" \
     -H "Content-Type: application/json" \
     -d '{"name": "Test Job", "schedule": "0 */10 * * * *", "payload": {"type": "message", "details": {"message": "Hello"}}}' \
     http://localhost:5000/api/v1/jobs
```

## Identity Header Format

The `X-Rh-Identity` header should contain base64-encoded JSON:

```json
{
  "identity": {
    "account_number": "000001",
    "org_id": "000001",
    "user": {
      "username": "john.doe",
      "email": "john.doe@example.com",
      "user_id": "john.doe-id"
    },
    "type": "User"
  }
}
```

## Testing

For testing purposes, the `cmd/test/main.go` includes a mock identity header:

```go
// Base64 encoded test identity
req.Header.Set("X-Rh-Identity", "eyJpZGVudGl0eSI6eyJhY2NvdW50X251bWJlciI6IjAwMDAwMSIsIm9yZ19pZCI6IjAwMDAwMSIsInVzZXIiOnsidXNlcm5hbWUiOiJ0ZXN0dXNlciIsImVtYWlsIjoidGVzdEBleGFtcGxlLmNvbSIsInVzZXJfaWQiOiJ0ZXN0dXNlci1pZCJ9LCJ0eXBlIjoiVXNlciJ9fQ==")
```

## Database Migration

The username field is still stored in the database and populated from the identity context. Existing data will have default values ("unknown") for username until updated.

## API Endpoints Affected

All endpoints under `/api/v1/` now require authentication:

- `POST /api/v1/jobs` - Create job
- `GET /api/v1/jobs` - List jobs (filtered by user's org)
- `GET /api/v1/jobs/{id}` - Get job (org ownership check)
- `PUT /api/v1/jobs/{id}` - Update job (org ownership check)
- `PATCH /api/v1/jobs/{id}` - Patch job (org ownership check)
- `DELETE /api/v1/jobs/{id}` - Delete job (org ownership check)
- `POST /api/v1/jobs/{id}/run` - Run job (org ownership check)
- `POST /api/v1/jobs/{id}/pause` - Pause job (org ownership check)
- `POST /api/v1/jobs/{id}/resume` - Resume job (org ownership check)

## Error Responses

### Missing Identity
```json
HTTP 400 Bad Request
{
  "error": "Missing organization ID in identity"
}
```

### Job Not Found (Cross-Org Access)
```json
HTTP 404 Not Found
{
  "error": "Job not found"
}
```

This ensures users cannot determine if jobs exist in other organizations.