# Polling State Management

## Question: How is the state of the polling stored?

## Short Answer

**The polling state is NOT persistently stored** - it's ephemeral and lives only in the execution context (goroutine memory) for the duration of a single job run. This is intentional and aligns with the scheduler's architecture.

## Current Architecture

### 1. Job Execution Flow

```
Scheduler (cron) → Execute Job → Create JobRun → Poll External Service → Complete JobRun
     ↓                  ↓              ↓                    ↓                    ↓
  (triggers)      (in goroutine)  (DB: running)      (in-memory loop)     (DB: completed)
```

### 2. What IS Stored

**Job** (persistent in DB):
```go
type Job struct {
    ID                  string      // Permanent job definition
    Schedule            Schedule    // When to run
    Status              JobStatus   // scheduled/running/paused/failed
    LastRunAt           *time.Time  // Last execution time
    NextRunAt           *time.Time  // Next scheduled time
    ConsecutiveFailures int         // Failure tracking
}
```

**JobRun** (persistent in DB):
```go
type JobRun struct {
    ID           string       // Unique run ID
    JobID        string       // Reference to parent Job
    Status       JobRunStatus // running/completed/failed
    StartTime    time.Time    // When execution began
    EndTime      *time.Time   // When execution finished
    ErrorMessage *string      // Error if failed
    Result       interface{}  // Final result (e.g., ExportResult with download URL)
}
```

### 3. What is NOT Stored

**Polling State** (ephemeral, in-memory only):
- Current polling attempt number (1, 2, 3, ..., 60)
- Last status check response ("pending", "running", etc.)
- Time of last poll
- Intermediate status transitions

These exist only as local variables in the `WaitForExportCompletion()` function during execution.

## Current Implementation

### Export Service Executor (export_job_executor.go:88)

```go
func (e *ExportJobExecutor) Execute(job domain.Job, logger *slog.Logger) {
    // ...
    
    // THIS IS IN-MEMORY POLLING - NO STATE PERSISTED
    finalStatus, err := e.exportClient.WaitForExportCompletion(
        ctx, 
        createResult.ID, 
        identityHeader, 
        maxRetries,      // 60 attempts
        pollInterval,    // 5 second sleep
    )
    // Polling state exists only during this function call
    // If the scheduler crashes here, the polling state is lost
}
```

### Polling Loop (clients/export/client.go:263)

```go
func (c *Client) WaitForExportCompletion(...) (*ExportStatusResponse, error) {
    for attempt := 0; attempt < maxRetries; attempt++ {  // ← NOT STORED
        status, err := c.GetExportStatus(ctx, exportID, identityHeader)
        
        switch status.Status {  // ← NOT STORED
        case StatusComplete:
            return status, nil
        case StatusFailed:
            return status, fmt.Errorf("export failed")
        case StatusPending, StatusRunning, StatusPartial:
            time.Sleep(pollInterval)  // ← IN-MEMORY WAIT
        }
    }
}
```

## Implications

### ✅ Advantages of Ephemeral Polling State

1. **Simplicity**: No need for a polling state table or Redis tracking
2. **Stateless Workers**: Any worker can pick up the next scheduled run
3. **No Cleanup**: No orphaned polling records to garbage collect
4. **Idempotent**: Each job execution is independent

### ⚠️ Disadvantages / Edge Cases

1. **Crash Recovery**: If the scheduler crashes during polling:
   - The JobRun remains in `running` status forever (orphaned)
   - The external service (export/PDF) completes but scheduler never records it
   - Next scheduled run creates a NEW JobRun and starts over

2. **No Resumability**: Cannot resume polling from attempt #37 if interrupted

3. **Visibility Gap**: Cannot see "currently on poll attempt 15/60" in UI

4. **Timeout Only**: Relies on context timeout (10 min) to detect stuck executions

## Crash Scenario Example

```
09:00:00 - Scheduler triggers export job #123
09:00:01 - JobRun ABC created (status: running)
09:00:02 - Export created on external service (ID: xyz-789)
09:00:03 - Poll attempt 1: status = "pending"
09:00:08 - Poll attempt 2: status = "running"
09:00:13 - Poll attempt 3: status = "running"
[CRASH - Scheduler process dies]
09:01:00 - External service completes export (status: "complete")
09:05:00 - Scheduler restarts
         - JobRun ABC still shows "running" (orphaned)
         - Export xyz-789 completed but scheduler doesn't know
10:00:00 - Next scheduled run of job #123
         - Creates NEW JobRun DEF
         - Creates NEW export on external service
         - Old export xyz-789 sits unused
```

## Solutions for Production

### Option 1: Accept Ephemeral State (Current Approach)

**Keep polling in-memory**, add monitoring:

```go
// Add orphaned run cleanup job
func CleanupOrphanedRuns() {
    // Find JobRuns in "running" status older than max execution time (15 min)
    orphans := jobRunRepo.FindRunning(olderThan: 15 * time.Minute)
    for _, run := range orphans {
        run = run.WithFailed("Execution interrupted - scheduler restarted")
        jobRunRepo.Update(run)
    }
}
```

**Pros**: Simple, stateless, works for 99% of cases  
**Cons**: Wasted API calls on crash/restart, orphaned runs need cleanup

### Option 2: Store External Job ID Only (Minimal State)

Store the external service's job ID to enable checking if a job already exists:

```go
type JobRun struct {
    // ... existing fields ...
    ExternalJobID *string `json:"external_job_id,omitempty"` // "xyz-789" for export, "status-uuid" for PDF
}

// Before creating export, check if we already have one running
func (e *ExportJobExecutor) Execute(job domain.Job, logger *slog.Logger) {
    // Check if there's an in-progress run with an external ID
    lastRun, _ := e.jobRunRepo.GetLastRunForJob(job.ID)
    if lastRun != nil && lastRun.Status == RunStatusRunning && lastRun.ExternalJobID != nil {
        // Resume polling the existing export instead of creating a new one
        logger.Info("Resuming existing export", slog.String("export_id", *lastRun.ExternalJobID))
        finalStatus, err := polling.Poll(ctx, poller, *lastRun.ExternalJobID, pollConfig)
        // ...
    } else {
        // Create new export as usual
        createResult, err := e.exportClient.CreateExport(...)
        
        // Store the external ID immediately
        run = run.WithExternalJobID(createResult.ID)
        e.jobRunRepo.Update(run)
    }
}
```

**Pros**: Idempotent on restart, no duplicate exports created  
**Cons**: Requires schema change, adds complexity

### Option 3: Persistent Polling State (Full Solution)

Create a polling state table for resumability:

```sql
CREATE TABLE polling_state (
    job_run_id TEXT PRIMARY KEY,
    external_job_id TEXT NOT NULL,
    service_type TEXT NOT NULL,  -- 'export', 'pdf'
    current_attempt INT DEFAULT 0,
    last_status TEXT,
    last_poll_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

```go
type PollingState struct {
    JobRunID      string
    ExternalJobID string
    ServiceType   string
    CurrentAttempt int
    LastStatus    string
    LastPollAt    time.Time
}

// Resume polling from saved state
func (p *Poller) ResumePolling(state PollingState, cfg Config) (*StatusResponse, error) {
    remaining := cfg.MaxRetries - state.CurrentAttempt
    // Continue polling from where we left off
}
```

**Pros**: Fully resumable, no duplicate calls, visibility into progress  
**Cons**: Complex, requires state management, cleanup logic

## Recommended Approach

### For Current Scale: **Option 1** (Ephemeral + Cleanup)

```go
// Add to scheduler startup
go func() {
    ticker := time.NewTicker(5 * time.Minute)
    for range ticker.C {
        CleanupOrphanedRuns(15 * time.Minute)
    }
}()

func CleanupOrphanedRuns(maxAge time.Duration) {
    cutoff := time.Now().UTC().Add(-maxAge)
    orphans, _ := jobRunRepo.FindByStatusAndOlderThan(RunStatusRunning, cutoff)
    
    for _, run := range orphans {
        logger.Warn("Cleaning up orphaned run",
            slog.String("run_id", run.ID),
            slog.String("job_id", run.JobID),
            slog.Time("started", run.StartTime))
        
        run = run.WithFailed("Execution timeout - likely interrupted by scheduler restart")
        jobRunRepo.Update(run)
    }
}
```

### For High-Reliability Production: **Option 2** (Store External ID)

Minimal state to prevent duplicate job creation while keeping polling ephemeral.

### For Enterprise Scale: **Option 3** (Full State Management)

Only if you need:
- Multi-hour polling operations
- Guaranteed exactly-once execution
- Detailed progress tracking
- Horizontal scaling with multiple scheduler instances

## Summary

**Current State**: Polling is **100% ephemeral** - no state is stored beyond the initial JobRun record. The polling loop lives entirely in goroutine memory.

**This is acceptable** for:
- Jobs that complete in minutes (not hours)
- Rare scheduler restarts
- Acceptable to create duplicate external jobs on crash

**Consider persistent state** if:
- External services charge per request
- Jobs take 30+ minutes to complete
- Scheduler restarts are frequent
- Exactly-once guarantees are critical
