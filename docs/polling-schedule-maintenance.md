# How Polling Schedule is Maintained

## Short Answer

**The cron schedule is NOT affected by long-running polling operations.** Each cron trigger executes the job **asynchronously in a goroutine**, then immediately returns. The next scheduled run will fire on time regardless of whether previous executions are still polling.

## Execution Flow

```
┌─────────────────────────────────────────────────────────────────────────┐
│ Cron Scheduler (scheduler.go:80)                                        │
│                                                                          │
│ 09:00:00 - Cron triggers job "daily-export"                             │
│            └─> jobFunc() is called                                      │
│                └─> jobService.ExecuteScheduledJob(job)                  │
│                    └─> executor.Execute(job)  ← ASYNC (in goroutine)   │
│                        └─> Starts polling (5-10 minutes)                │
│                                                                          │
│ 09:00:00.001 - jobFunc() RETURNS immediately                            │
│                Cron scheduler is FREE for next trigger                  │
│                                                                          │
│ [Meanwhile: Export executor still polling in background goroutine...]   │
│                                                                          │
│ 10:00:00 - Cron triggers AGAIN (next scheduled run)                     │
│            └─> New goroutine spawned                                    │
│            └─> Previous execution might still be polling!               │
└─────────────────────────────────────────────────────────────────────────┘
```

## Code Walkthrough

### 1. Cron Scheduler Registers Job Function (scheduler.go:80-106)

```go
func (s *CronScheduler) ScheduleJob(job domain.Job) error {
    // Create job execution function
    jobFunc := func() {
        s.logger.Info("Executing cron job", slog.String("job_id", job.ID))
        
        // Get latest job state
        currentJob, err := s.jobService.GetJob(context.Background(), job.ID)
        if err != nil {
            s.logger.Error("Error getting job", slog.Any("error", err))
            return  // ← Returns to cron immediately
        }
        
        // Execute the job (ASYNC - doesn't block)
        if err := s.jobService.ExecuteScheduledJob(currentJob); err != nil {
            s.logger.Error("Error executing job", slog.Any("error", err))
        }
        // ← Function returns here, cron is free
    }
    
    // Schedule the job - cron will call jobFunc() on schedule
    entryID, err := s.cron.AddFunc(string(job.Schedule), jobFunc)
    
    return nil
}
```

**Key Point**: `jobFunc()` returns immediately after calling `ExecuteScheduledJob()`. It does NOT wait for polling to complete.

### 2. Job Service Dispatches to Executor (job_service.go:973-976)

```go
func (s *DefaultJobService) ExecuteScheduledJob(job domain.Job) error {
    // Use background context for scheduled job execution
    _, err := s.RunJob(context.Background(), job.ID)
    return err  // Returns quickly - polling happens inside RunJob
}
```

### 3. Executor Runs in Goroutine (job_executor.go:30-85)

```go
func (e *DefaultJobExecutor) Execute(job domain.Job) error {
    // Track this job for graceful shutdown
    e.wg.Add(1)          // ← Goroutine tracking
    defer e.wg.Done()
    
    // Create job run record
    jobRun := domain.NewJobRun(job.ID)
    e.runRepo.Save(jobRun)
    
    // Create logger
    logger := logging.NewJobExecutionLogger(...)
    
    // Execute the job using the appropriate runner
    runner, ok := e.runners[job.Type]
    result, resultType, execErr = runner.Execute(job, logger)
    // ↑ THIS is where polling happens (blocks for 5-10 minutes)
    
    // Update job run with result
    if execErr != nil {
        jobRun = jobRun.WithFailed(execErr.Error())
    } else {
        jobRun = jobRun.WithCompleted(resultType, result)
    }
    e.runRepo.Save(jobRun)
    
    return execErr
    // ← Returns after polling completes, but cron already moved on
}
```

**Key Point**: `runner.Execute()` is where the 5-10 minute polling happens, but this runs in a **separate goroutine** from the cron scheduler.

### 4. Export Runner Polls Synchronously (export_job_executor.go:88)

```go
func (e *ExportJobExecutor) Execute(job domain.Job, logger *slog.Logger) {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
    defer cancel()
    
    // Create export
    createResult, err := e.exportClient.CreateExport(ctx, req, identityHeader)
    
    // POLL FOR COMPLETION (blocks for 5-10 minutes)
    finalStatus, err := e.exportClient.WaitForExportCompletion(
        ctx, 
        createResult.ID, 
        identityHeader, 
        maxRetries,      // 60
        pollInterval,    // 5 seconds
    )
    // ↑ THIS is synchronous polling in the goroutine
    // Cron scheduler doesn't wait for this
    
    // Build result
    result := domain.ExportResult{ExportID: createResult.ID, URL: downloadURL}
    return result, domain.ResultTypeExport, nil
}
```

**Key Point**: Polling is **synchronous within the executor goroutine**, but **asynchronous relative to the cron scheduler**.

## Timeline Example: Hourly Job That Takes 7 Minutes to Poll

```
Job Schedule: "0 * * * *" (every hour on the hour)
Poll Duration: ~7 minutes average

09:00:00.000 - Cron triggers
09:00:00.001 - Spawns goroutine A for JobRun #1
09:00:00.002 - jobFunc() returns ← CRON IS FREE
09:00:00.100 - Goroutine A creates export on external service
09:00:05.000 - Goroutine A polls: status = "pending"
09:00:10.000 - Goroutine A polls: status = "running"
09:00:15.000 - Goroutine A polls: status = "running"
...
09:07:00.000 - Goroutine A polls: status = "complete"
09:07:00.100 - Goroutine A saves JobRun #1 as "completed"
09:07:00.200 - Goroutine A exits

10:00:00.000 - Cron triggers AGAIN ← On time!
10:00:00.001 - Spawns goroutine B for JobRun #2
10:00:00.002 - jobFunc() returns ← CRON IS FREE
10:00:00.100 - Goroutine B creates export on external service
...
```

**No interference** - each execution is independent.

## What Happens If Polling Takes Longer Than the Schedule Interval?

### Scenario: 10-minute job with 5-minute schedule

```
Job Schedule: "*/5 * * * *" (every 5 minutes)
Poll Duration: 10 minutes

09:00:00 - Trigger #1 → Goroutine A starts
           ├─ Polling: 09:00 - 09:10
           
09:05:00 - Trigger #2 → Goroutine B starts (A still running!)
           ├─ Polling: 09:05 - 09:15
           
09:10:00 - Trigger #3 → Goroutine C starts (A & B still running!)
           ├─ Polling: 09:10 - 09:20
           └─ Goroutine A completes

09:15:00 - Trigger #4 → Goroutine D starts (B & C still running!)
           ├─ Polling: 09:15 - 09:25
           └─ Goroutine B completes

Result: MULTIPLE CONCURRENT EXECUTIONS of the same job
```

### Is This a Problem?

**Depends on the job:**

| Job Type | Multiple Concurrent Executions OK? |
|----------|-----------------------------------|
| **Export job** | ⚠️ **Maybe** - Creates duplicate exports on external service |
| **PDF job** | ⚠️ **Maybe** - Creates duplicate PDFs |
| **HTTP webhook** | ❌ **NO** - Sends duplicate notifications |
| **Message job** | ✅ **YES** - Idempotent processing |

### Protection Mechanisms

#### Current Protection: Job Status Check (scheduler.go:94-99)

```go
jobFunc := func() {
    currentJob, err := s.jobService.GetJob(context.Background(), job.ID)
    
    // Only execute if job is still scheduled
    if currentJob.Status != domain.StatusScheduled {
        s.logger.Debug("Job no longer scheduled, skipping execution")
        return  // ← Skip if status changed
    }
    
    s.jobService.ExecuteScheduledJob(currentJob)
}
```

**Issue**: This only prevents execution if the job is paused/failed, NOT if another execution is in progress.

#### Missing Protection: Concurrency Lock

**Current behavior**: Multiple concurrent executions CAN happen if polling duration > schedule interval.

**Potential solution** (not currently implemented):

```go
// Add to Job model
type Job struct {
    // ... existing fields
    CurrentlyExecuting bool      `json:"currently_executing"`
    LastExecutionStart *time.Time `json:"last_execution_start,omitempty"`
}

// In scheduler
jobFunc := func() {
    currentJob, err := s.jobService.GetJob(context.Background(), job.ID)
    
    // Skip if already executing
    if currentJob.CurrentlyExecuting {
        s.logger.Warn("Job already executing, skipping this trigger",
            slog.String("job_id", job.ID))
        return
    }
    
    // Mark as executing
    currentJob = currentJob.WithCurrentlyExecuting(true)
    s.jobService.UpdateJob(currentJob)
    
    // Execute
    s.jobService.ExecuteScheduledJob(currentJob)
    
    // Mark as not executing (done in ExecuteScheduledJob cleanup)
}
```

## Distributed Scheduler Considerations (Redis Mode)

When using Redis-based distributed scheduling:

```go
// redis_scheduler.go (worker mode)
func (s *RedisScheduler) Start(ctx context.Context) {
    ticker := time.NewTicker(s.pollInterval)  // Default: 10 seconds
    
    for {
        select {
        case <-ticker.C:
            s.checkAndExecuteDueJobs()  // ← Polls Redis for due jobs
        case <-ctx.Done():
            return
        }
    }
}
```

**Redis-based polling is DIFFERENT from job execution polling:**
- **Scheduler polling**: Checks Redis every 10 seconds for due jobs
- **Job execution polling**: Checks export/PDF service status every 5 seconds

**These are independent:**
```
Worker polls Redis every 10s  → Finds due job → Executes in goroutine
                                                   └─> Polls export service every 5s
Worker polls Redis every 10s  → No due jobs
Worker polls Redis every 10s  → Finds due job → Executes in goroutine
                                                   └─> Polls PDF service every 5s
```

## Graceful Shutdown

The scheduler waits for in-flight polling operations before shutting down:

```go
func (e *DefaultJobExecutor) Execute(job domain.Job) error {
    e.wg.Add(1)          // ← Track this execution
    defer e.wg.Done()    // ← Untrack when done
    
    // ... polling happens here ...
}

func (e *DefaultJobExecutor) WaitForInFlightJobs(timeout time.Duration) {
    done := make(chan struct{})
    go func() {
        e.wg.Wait()  // ← Wait for all tracked jobs
        close(done)
    }()
    
    select {
    case <-done:
        // All jobs completed
    case <-time.After(timeout):
        // Timeout - jobs killed
    }
}
```

## Summary

### How Polling Schedule is Maintained

1. **Cron triggers are independent** - Each trigger spawns a goroutine and returns immediately
2. **Polling happens in goroutines** - Long-running polling does NOT block the scheduler
3. **Next trigger fires on time** - Regardless of previous execution status
4. **Multiple executions possible** - If polling > schedule interval, concurrent executions happen
5. **No built-in concurrency lock** - Scheduler doesn't prevent overlapping executions
6. **Graceful shutdown waits** - In-flight polling operations complete before process exits

### Recommendations

For jobs with long polling (>5 minutes):

1. **Schedule conservatively** - Use longer intervals than max polling duration
   ```
   Poll duration: 10 minutes max
   Schedule: */30 * * * * (every 30 minutes) ← Safe
   Schedule: */5 * * * *  (every 5 minutes)  ← Overlaps!
   ```

2. **Add concurrency protection** if overlapping executions are problematic:
   - Add `currently_executing` flag to Job model
   - Check flag before execution
   - Clear flag in cleanup

3. **Monitor execution duration** - Track how long jobs actually take:
   ```go
   JobExecutionDuration.Observe(time.Since(startTime).Seconds())
   ```

4. **Consider async notification** instead of polling for very long jobs:
   - Export service calls webhook when complete
   - Scheduler doesn't poll, just waits for callback
   - Avoids long-running goroutines entirely
