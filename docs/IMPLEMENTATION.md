# Generic Polling Implementation

## Overview

Implemented a flexible, reusable polling mechanism that abstracts the common pattern of checking job status repeatedly until completion. This implementation supports both the export service and can easily be extended to support PDF generation or any other async job service.

## Files Created

### Core Polling Package

**`internal/clients/polling/polling.go`**
- Generic `Poller` interface for any async job service
- `StatusResponse` struct with service-agnostic fields
- `Config` struct for retry/timeout/interval settings
- `Poll()` function implementing the polling loop with:
  - Context-aware timeout handling
  - Configurable retry limits
  - Terminal state detection
  - Detailed error messages with attempt tracking

**`internal/clients/polling/polling_test.go`**
- Comprehensive test coverage (9 test cases)
- Tests for: successful completion, immediate completion, failures, timeouts, cancellation, max retries
- All tests passing ✅

### Export Service Integration

**`internal/clients/export/poller.go`**
- `ExportPoller` implementing the `Poller` interface
- Status mapping: export statuses → generic job statuses
- Error extraction from source-level failures
- Metadata preservation for audit/debugging

**`internal/clients/export/poller_test.go`**
- Unit tests for status mapping logic
- Terminal status detection tests
- Error extraction verification
- All tests passing ✅

### Refactored Executor

**`internal/shell/executor/export_job_executor.go`** (modified)
- Replaced direct `WaitForExportCompletion()` call with generic `polling.Poll()`
- Cleaner error handling via `StatusResponse`
- Deprecated old polling method (marked in `client.go`)

## Key Design Decisions

### 1. Interface-Based Design

```go
type Poller interface {
    GetStatus(ctx context.Context, jobID string) (*StatusResponse, error)
    IsTerminalStatus(status JobStatus) bool
}
```

**Why**: Allows any service to implement polling by providing status mapping logic, without changing the core polling loop.

### 2. Service-Agnostic Status Model

```go
type JobStatus string
const (
    StatusPending    JobStatus = "pending"
    StatusInProgress JobStatus = "in_progress"
    StatusComplete   JobStatus = "complete"
    StatusFailed     JobStatus = "failed"
)
```

**Why**: Different services use different status names (export: "running", PDF: "Generating"). The generic model normalizes these.

### 3. Metadata Preservation

```go
type StatusResponse struct {
    // ... core fields
    Metadata map[string]interface{} // Service-specific data
}
```

**Why**: Services can attach additional context (export format, source details, etc.) without polluting the core interface.

### 4. Context-Aware Timeout

```go
func Poll(ctx context.Context, poller Poller, jobID string, cfg Config) {
    timeoutCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
    defer cancel()
    // ...
}
```

**Why**: Respects both the caller's context AND the configured timeout. Parent cancellation propagates immediately.

## Benefits

### Before (Old Implementation)

```go
// Tightly coupled to export service
finalStatus, err := e.exportClient.WaitForExportCompletion(
    ctx, exportID, identityHeader, maxRetries, pollInterval)

// Different implementations for each service
// No unified error handling
// Hard to test without mocking HTTP
```

### After (New Implementation)

```go
// Generic polling works for any service
poller := export.NewExportPoller(e.exportClient, identityHeader)
pollResult, err := polling.Poll(ctx, poller, exportID, config)

// Same polling loop for all services
// Consistent error messages
// Easy to test with mock pollers
```

### Specific Improvements

1. **Single Source of Truth**: One polling loop implementation, tested once, used everywhere
2. **Easier Testing**: Mock `Poller` interface instead of HTTP client
3. **Better Error Messages**: Includes attempt number and timeout details
4. **Flexible Configuration**: Per-service tuning via `Config` struct
5. **Metadata Support**: Services can attach debug info without breaking the interface
6. **Future-Proof**: Adding PDF polling requires only implementing `Poller` interface

## Usage Example

### Export Service (Already Implemented)

```go
// Create poller
poller := export.NewExportPoller(exportClient, identityHeader)

// Configure polling
config := polling.Config{
    MaxRetries:   60,
    PollInterval: 5 * time.Second,
    Timeout:      9 * time.Minute,
}

// Poll until complete
result, err := polling.Poll(ctx, poller, exportID, config)
if err != nil {
    return fmt.Errorf("export failed: %w", err)
}

// Use result
if result.Status == polling.StatusComplete {
    downloadURL := exportClient.GetDownloadURL(result.ID)
}
```

### Future: PDF Service

```go
// Create PDF poller (not yet implemented)
poller := pdfgen.NewPDFPoller(pdfClient, identityHeader)

// Same polling interface!
config := polling.Config{
    MaxRetries:   120,  // PDFs might take longer
    PollInterval: 3 * time.Second,
    Timeout:      15 * time.Minute,
}

result, err := polling.Poll(ctx, poller, statusID, config)
// Same error handling, same result structure
```

## Migration Notes

### Old Method Still Available

The original `WaitForExportCompletion()` method is still available but marked deprecated:

```go
// Deprecated: Use polling.Poll with ExportPoller instead
func (c *Client) WaitForExportCompletion(...) (*ExportStatusResponse, error)
```

**Why Keep It**: Backward compatibility for any external code that might be using it directly.

### Safe to Remove When

- Confirm no other packages call `WaitForExportCompletion()` directly
- Migration is complete and stable in production
- All tests updated to new pattern

## Test Coverage

### Polling Package Tests
```
TestPoll_SuccessfulCompletion      ✅
TestPoll_ImmediateCompletion       ✅
TestPoll_FailureStatus             ✅
TestPoll_MaxRetriesExceeded        ✅
TestPoll_ContextTimeout            ✅
TestPoll_GetStatusError            ✅
TestPoll_CancelledContext          ✅
TestPoll_DefaultConfig             ✅
TestPoll_StatusResponseMetadata    ✅
```

### Export Poller Tests
```
TestExportPoller_GetStatus_Complete     ✅
TestExportPoller_IsTerminalStatus       ✅
TestExportPoller_StatusMapping          ✅
TestExportPoller_ErrorExtraction        ✅
TestExportPoller_MetadataPreservation   ✅
```

**All tests passing**: `go test ./internal/clients/...` ✅

## Performance Characteristics

### Memory
- Minimal overhead: Only status response in memory
- No buffering of intermediate statuses
- Metadata map is service-controlled

### Latency
- Same as before: `MaxRetries × PollInterval`
- Context cancellation is immediate (no busy-wait)
- Timeout enforced via context, not loop counting

### Concurrency
- Thread-safe: Each goroutine gets its own poller instance
- No shared state between concurrent polls
- Context cancellation propagates correctly

## Next Steps

### To Add PDF Support

1. Create `internal/clients/pdfgen/client.go` - HTTP client for PDF service
2. Create `internal/clients/pdfgen/poller.go` - Implement `Poller` interface
3. Map PDF statuses to generic statuses:
   - `"Generating"` → `StatusInProgress`
   - `"Generated"` → `StatusComplete`
   - `"Failed"` → `StatusFailed`
4. Create `internal/shell/executor/pdf_job_executor.go`
5. Register in executor map with payload type `PayloadPDF`

### To Add Defensive Retry Logic (for PDF cache loss issue)

```go
// In PDF poller
func (p *PDFPoller) GetStatus(ctx context.Context, jobID string) (*StatusResponse, error) {
    status, err := p.client.GetPDFStatus(ctx, jobID)
    
    if isNotFoundError(err) && p.createdRecently(jobID, 30*time.Minute) {
        // Pod likely restarted - return in-progress to continue polling
        return &StatusResponse{
            ID:         jobID,
            Status:     StatusInProgress,
            IsTerminal: false,
        }, nil
    }
    
    // ... normal status handling
}
```

### To Add Metrics

```go
// In polling.go
var (
    PollAttempts = prometheus.NewHistogram(...)
    PollDuration = prometheus.NewHistogram(...)
    PollErrors   = prometheus.NewCounter(...)
)

func Poll(...) {
    start := time.Now()
    defer func() {
        PollDuration.Observe(time.Since(start).Seconds())
    }()
    
    for attempt := 0; attempt < cfg.MaxRetries; attempt++ {
        PollAttempts.Observe(float64(attempt + 1))
        // ...
    }
}
```

## References

- Design Document: [`docs/polling-mechanism-design.md`](./polling-mechanism-design.md)
- State Management: [`docs/polling-state-management.md`](./polling-state-management.md)
- PDF Cache Issue: [`docs/pdf-generator-cache-loss-issue.md`](./pdf-generator-cache-loss-issue.md)
- Schedule Maintenance: [`docs/polling-schedule-maintenance.md`](./polling-schedule-maintenance.md)
