# Current Implementation: Pod Restart Behavior

## Question: How does the current implementation handle pod restarts during polling?

**Short Answer: It doesn't. Polling state is completely lost.**

## What Happens During a Pod Restart

### Scenario: Export Job Being Polled

```
09:00:00 - Cron triggers job "daily-export"
09:00:01 - ExportJobExecutor.Execute() starts
09:00:02 - Creates JobRun (ID: run-abc-123, Status: "running")
09:00:03 - POST to export service → Export created (ID: export-xyz-789)
09:00:04 - Enters polling.Poll() function
09:00:05 - Poll attempt 1/60: status = "pending"
09:00:10 - Poll attempt 2/60: status = "running"
09:00:15 - Poll attempt 3/60: status = "running"

09:00:20 - 💥 POD CRASHES / RESTARTS
           ├─ Goroutine dies immediately
           ├─ Polling loop terminates
           ├─ No state saved
           └─ JobRun remains "running" forever

09:01:00 - Pod comes back online
           ├─ Scheduler reinitializes
           ├─ Cron schedule restored
           └─ But the in-flight job is LOST

09:05:00 - Export service completes the export (ID: export-xyz-789)
           └─ Status = "complete"
           └─ Scheduler doesn't know (nobody is polling anymore)

10:00:00 - Next scheduled run of "daily-export"
           ├─ Creates NEW JobRun (ID: run-def-456)
           ├─ Creates NEW export (ID: export-uvw-101)
           └─ Old export (export-xyz-789) is orphaned
```

## Database State After Restart

### Before Crash (09:00:15)

**Jobs Table:**
```
id           | name          | status     | last_run_at
-------------|---------------|------------|-------------
job-1        | daily-export  | scheduled  | 2026-07-13 09:00:00
```

**Job Runs Table:**
```
id           | job_id  | status   | start_time          | end_time | result
-------------|---------|----------|---------------------|----------|--------
run-abc-123  | job-1   | running  | 2026-07-13 09:00:02 | NULL     | NULL
```

### After Restart (09:01:00)

**Jobs Table:**
```
id           | name          | status     | last_run_at
-------------|---------------|------------|-------------
job-1        | daily-export  | scheduled  | 2026-07-13 09:00:00
```

**Job Runs Table:**
```
id           | job_id  | status   | start_time          | end_time | result
-------------|---------|----------|---------------------|----------|--------
run-abc-123  | job-1   | running  | 2026-07-13 09:00:02 | NULL     | NULL  ← ORPHANED!
```

**Status: "running"** but no goroutine is actually running!

## What Gets Lost

### 1. Polling State (In-Memory Only)

```go
// These variables exist ONLY in the goroutine stack
attempt := 3              // Lost
pollInterval := 5s        // Lost
timeoutCtx := ...         // Lost
poller := ExportPoller{} // Lost
```

**None of this is persisted to database.**

### 2. Export Job Reference

The scheduler **created** an export on the external service but has **no record** that:
- Export ID `export-xyz-789` exists
- It was created by job run `run-abc-123`
- It might have completed successfully

### 3. JobRun Completion

The `JobRun` record is stuck in "running" status permanently because:
- No goroutine is polling the export
- No code will ever update it to "completed" or "failed"
- It becomes an orphaned record

## Impact Analysis

### On Job Scheduling

**Good news**: Cron schedule continues normally
```
10:00:00 - Next scheduled run triggers
11:00:00 - Next scheduled run triggers
12:00:00 - Next scheduled run triggers
```

The **schedule is not affected** - only the in-flight execution is lost.

### On Job Runs

**Bad news**: Accumulates orphaned "running" job runs

```sql
SELECT * FROM job_runs WHERE status = 'running';

id           | job_id  | status   | start_time          | age
-------------|---------|----------|---------------------|----------
run-abc-123  | job-1   | running  | 2026-07-13 09:00:02 | 1 hour   ← Orphan
run-ghi-789  | job-2   | running  | 2026-07-13 08:30:00 | 1.5 hours ← Orphan
run-jkl-456  | job-3   | running  | 2026-07-13 07:45:00 | 2+ hours  ← Orphan
```

These will **never complete** without cleanup.

### On External Resources

**Bad news**: Wastes external service resources

```
Export Service:
  export-xyz-789: COMPLETE (nobody will download)
  export-uvw-101: COMPLETE (new duplicate export)
  export-mno-234: COMPLETE (another duplicate)
  
Result: 3 exports for 1 actual need
```

API quota wasted, storage consumed, duplicate processing.

## Current Code - No Persistence

### What We DON'T Store

```go
func (e *ExportJobExecutor) Execute(job domain.Job, logger *slog.Logger) {
    // ... create export ...
    
    // Create job run (status: "running")
    jobRun := domain.NewJobRun(job.ID)
    e.runRepo.Save(jobRun)
    
    // ❌ NOT STORED: Export ID
    // ❌ NOT STORED: Current poll attempt
    // ❌ NOT STORED: Last poll time
    // ❌ NOT STORED: Polling config
    
    // Start polling (ALL STATE IN MEMORY)
    pollResult, err := polling.Poll(ctx, poller, exportID, config)
    //                                 ↑
    //                    Everything inside this function is ephemeral
    //                    Pod restart = LOST
    
    // Only if we get here do we update JobRun
    if err != nil {
        jobRun = jobRun.WithFailed(err.Error())
    } else {
        jobRun = jobRun.WithCompleted(result)
    }
    e.runRepo.Save(jobRun)
}
```

### What IS Stored (Before Polling)

```go
type JobRun struct {
    ID           string       // ✅ Stored
    JobID        string       // ✅ Stored
    Status       JobRunStatus // ✅ Stored ("running")
    StartTime    time.Time    // ✅ Stored
    EndTime      *time.Time   // ❌ NULL (will be set later)
    ErrorMessage *string      // ❌ NULL
    Result       interface{}  // ❌ NULL
}
```

**The export ID is NOT stored in JobRun.**

## Comparison with Other Designs

### Our Implementation (Current)

```
State Persistence: ❌ None
Recovery on Restart: ❌ No
Orphaned JobRuns: ✅ Yes
Duplicate Exports: ✅ Yes (on retry)
```

### Option 1: Store External Job ID (Proposed in Design Docs)

```go
type JobRun struct {
    // ... existing fields ...
    ExternalJobID *string `json:"external_job_id,omitempty"`  // ← NEW
}

func (e *ExportJobExecutor) Execute(job domain.Job) {
    createResult, _ := e.exportClient.CreateExport(...)
    
    // Store external ID immediately
    jobRun.ExternalJobID = &createResult.ID
    e.runRepo.Save(jobRun)
    
    // Now if pod restarts, we can resume polling this export
}
```

**Recovery**: On startup, check for JobRuns with `status="running"` and `ExternalJobID != null`, resume polling them.

```
State Persistence: ✅ External job ID
Recovery on Restart: ✅ Can resume polling
Orphaned JobRuns: ❌ No (cleanup resumes them)
Duplicate Exports: ❌ No (reuse existing)
```

### Option 2: Full Polling State Persistence

```go
type PollingState struct {
    JobRunID       string
    ExternalJobID  string
    ServiceType    string
    CurrentAttempt int        // ← Track progress
    LastPollAt     time.Time
}
```

**Recovery**: Resume from exact attempt number.

```
State Persistence: ✅ Full state
Recovery on Restart: ✅ Resume from attempt N
Orphaned JobRuns: ❌ No
Duplicate Exports: ❌ No
```

## Real-World Impact Scenarios

### Scenario 1: Rolling Deployment (Kubernetes)

```
3 scheduler pods, rolling update every week

During deployment:
  - 50 jobs in progress across pods
  - Pods restart one by one
  - Each restart orphans ~15-20 jobs
  
Result after deployment:
  - 50 orphaned JobRuns
  - 50 completed exports nobody knows about
  - Users don't receive notifications
  - Next runs create duplicate exports
```

### Scenario 2: OOM Kill

```
Scheduler pod hits memory limit during high load
  - Kubernetes kills pod
  - 100 jobs in progress
  - All 100 orphaned immediately
  
Recovery:
  - Pod restarts
  - Cron continues scheduling
  - But 100 completed exports are lost
  - Users never notified
```

### Scenario 3: Cluster Node Failure

```
Node failure takes down scheduler pod
  - All in-flight jobs lost
  - Pod reschedules to different node
  - Starts fresh with no memory of old jobs
  
External service perspective:
  - Exports completed successfully
  - Nobody ever downloaded them
  - Wasted processing
```

## Cleanup Strategies (Not Currently Implemented)

### Option A: Periodic Cleanup Job

```go
// Run every 5 minutes
func CleanupOrphanedRuns() {
    cutoff := time.Now().Add(-15 * time.Minute)
    
    orphans, _ := jobRunRepo.FindByStatusAndOlderThan("running", cutoff)
    
    for _, run := range orphans {
        logger.Warn("Cleaning up orphaned run",
            slog.String("run_id", run.ID),
            slog.Duration("age", time.Since(run.StartTime)))
        
        run = run.WithFailed("Execution interrupted - scheduler restarted")
        jobRunRepo.Update(run)
    }
}
```

**Add to scheduler startup**:
```go
func main() {
    // ... existing initialization ...
    
    // Start cleanup goroutine
    go func() {
        ticker := time.NewTicker(5 * time.Minute)
        for range ticker.C {
            CleanupOrphanedRuns()
        }
    }()
}
```

### Option B: Startup Recovery

```go
// On scheduler startup
func RecoverInFlightJobs() {
    runningJobs, _ := jobRunRepo.FindByStatus("running")
    
    for _, run := range runningJobs {
        age := time.Since(run.StartTime)
        
        if age > 15*time.Minute {
            // Too old - mark as failed
            run = run.WithFailed("Execution timed out during restart")
            jobRunRepo.Update(run)
        } else {
            // Recent - could retry if we stored external ID
            logger.Warn("Found recent orphaned run", slog.String("run_id", run.ID))
            // TODO: Resume polling if ExternalJobID is stored
        }
    }
}
```

## Mitigation in Current Implementation

### What We CAN Do Now (Without Code Changes)

1. **Monitor orphaned runs**:
```sql
-- Alert if too many "running" jobs older than 15 minutes
SELECT COUNT(*) 
FROM job_runs 
WHERE status = 'running' 
  AND start_time < NOW() - INTERVAL '15 minutes';
```

2. **Manual cleanup**:
```sql
-- Mark old "running" jobs as failed
UPDATE job_runs
SET status = 'failed',
    error_message = 'Execution interrupted - likely pod restart',
    end_time = NOW()
WHERE status = 'running'
  AND start_time < NOW() - INTERVAL '15 minutes';
```

3. **Accept the limitation**:
   - Document that pod restarts will orphan in-flight jobs
   - Next scheduled run will create a new export
   - Not ideal but acceptable for non-critical jobs

### What We SHOULD Do (Requires Code Changes)

**Minimal fix**: Store external job ID

```go
// 1. Add field to JobRun
type JobRun struct {
    // ... existing fields ...
    ExternalJobID *string `json:"external_job_id,omitempty"`
}

// 2. Save immediately after creating export
func (e *ExportJobExecutor) Execute(job domain.Job) {
    createResult, _ := e.exportClient.CreateExport(...)
    
    // Store external ID
    jobRun.ExternalJobID = &createResult.ID
    e.runRepo.Save(jobRun)
    
    // Now poll (if restart, we can resume)
    polling.Poll(...)
}

// 3. On startup, resume orphaned runs
func RecoverInFlightJobs() {
    running, _ := jobRunRepo.FindByStatus("running")
    
    for _, run := range running {
        if run.ExternalJobID != nil {
            // Resume polling this export
            go resumePolling(run)
        } else {
            // No external ID - mark as failed
            run.WithFailed("Lost during restart")
        }
    }
}
```

## Summary

### Current Behavior on Pod Restart

| What Happens | Impact |
|--------------|--------|
| ❌ Polling goroutine dies | Immediate termination |
| ❌ Poll state lost | Can't resume from attempt N |
| ❌ Export ID not stored | Can't find the export later |
| ❌ JobRun stuck "running" | Orphaned record |
| ❌ Export completes unnoticed | Wasted resources |
| ✅ Cron schedule continues | Next runs work normally |

### Recommended Fix Priority

1. **Immediate** (< 1 hour): Add cleanup job for orphaned runs
2. **Short term** (< 1 day): Store external job ID in JobRun
3. **Medium term** (< 1 week): Add startup recovery to resume polling
4. **Long term** (optional): Full polling state persistence

### Code Example: Minimal Fix

See `docs/polling-state-management.md` Option 2 for implementation details.

The **current implementation has the same limitation as before** - no polling state persistence. The generic polling interface doesn't change this fundamental behavior, it just makes the code cleaner.
