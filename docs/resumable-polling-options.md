# Resumable Polling Options (With Redis + PostgreSQL)

## Overview

You have both Redis and PostgreSQL available. Here are your options for making polling resumable after pod restarts.

## Option 1: PostgreSQL-Only (Simple & Durable)

### Architecture

Store the external job ID in the `job_runs` table. On restart, resume polling any in-flight jobs.

### Schema Changes

```sql
-- Add to existing job_runs table
ALTER TABLE job_runs 
ADD COLUMN external_job_id TEXT,
ADD COLUMN external_service TEXT,  -- 'export', 'pdf', etc.
ADD COLUMN poll_started_at TIMESTAMP;

-- Index for startup recovery
CREATE INDEX idx_job_runs_resume ON job_runs(status, poll_started_at) 
WHERE status = 'running';
```

### Code Implementation

```go
// 1. Update domain model
package domain

type JobRun struct {
    ID              string       `json:"id"`
    JobID           string       `json:"job_id"`
    Status          JobRunStatus `json:"status"`
    StartTime       time.Time    `json:"start_time"`
    EndTime         *time.Time   `json:"end_time,omitempty"`
    ErrorMessage    *string      `json:"error_message,omitempty"`
    ResultType      *ResultType  `json:"result_type,omitempty"`
    Result          interface{}  `json:"result,omitempty"`
    
    // New fields for resumable polling
    ExternalJobID   *string      `json:"external_job_id,omitempty"`
    ExternalService *string      `json:"external_service,omitempty"`
    PollStartedAt   *time.Time   `json:"poll_started_at,omitempty"`
}

func (jr JobRun) WithExternalJob(externalJobID, externalService string) JobRun {
    now := time.Now().UTC()
    return JobRun{
        ID:              jr.ID,
        JobID:           jr.JobID,
        Status:          jr.Status,
        StartTime:       jr.StartTime,
        EndTime:         jr.EndTime,
        ErrorMessage:    jr.ErrorMessage,
        ResultType:      jr.ResultType,
        Result:          jr.Result,
        ExternalJobID:   &externalJobID,
        ExternalService: &externalService,
        PollStartedAt:   &now,
    }
}
```

```go
// 2. Update executor to store external job ID immediately
func (e *ExportJobExecutor) Execute(job domain.Job, logger *slog.Logger) (interface{}, domain.ResultType, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
    defer cancel()

    // Generate identity
    identityHeader, err := e.userValidator.GenerateIdentityHeader(ctx, job.OrgID, job.UserID)
    if err != nil {
        return nil, domain.ResultTypeExport, fmt.Errorf("failed to verify user: %w", err)
    }

    // Marshal payload
    var req export.ExportRequest
    payloadJSON, _ := json.Marshal(job.Payload)
    json.Unmarshal(payloadJSON, &req)

    // Create the export
    createResult, err := e.exportClient.CreateExport(ctx, req, identityHeader)
    if err != nil {
        return nil, domain.ResultTypeExport, fmt.Errorf("failed to create export: %w", err)
    }

    // ✅ CRITICAL: Store external job ID IMMEDIATELY
    // This must happen BEFORE polling starts
    jobRun, err := e.getOrCreateJobRun(job.ID)
    if err != nil {
        return nil, domain.ResultTypeExport, err
    }
    
    jobRun = jobRun.WithExternalJob(createResult.ID, "export")
    if err := e.runRepo.Save(jobRun); err != nil {
        logger.Error("Failed to save external job ID", slog.Any("error", err))
        // Continue anyway - worst case we create duplicate on restart
    }

    logger.Info("External job created and stored",
        slog.String("export_id", createResult.ID),
        slog.String("job_run_id", jobRun.ID))

    // Start polling (if restart happens, we can resume from here)
    poller := export.NewExportPoller(e.exportClient, identityHeader)
    pollConfig := polling.Config{
        MaxRetries:   e.config.ExportService.PollMaxRetries,
        PollInterval: e.config.ExportService.PollInterval,
        Timeout:      9 * time.Minute,
    }

    pollResult, err := polling.Poll(ctx, poller, createResult.ID, pollConfig)
    if err != nil {
        logger.Error("Export failed or timed out", slog.Any("error", err))
        return nil, domain.ResultTypeExport, fmt.Errorf("export failed or timed out: %w", err)
    }

    // Build result
    downloadURL := ""
    if pollResult.Status == polling.StatusComplete {
        downloadURL = e.exportClient.GetExportDownloadURL(createResult.ID)
    }

    // Send notification
    notification := &ExportCompletionNotification{
        ExportID:    createResult.ID,
        JobID:       job.ID,
        JobName:     job.Name,
        OrgID:       job.OrgID,
        Status:      string(pollResult.Status),
        DownloadURL: downloadURL,
        ErrorMsg:    pollResult.Error,
    }
    e.notifier.JobComplete(ctx, notification, logger)

    result := domain.ExportResult{
        ExportID: createResult.ID,
    }
    if pollResult.Status == polling.StatusComplete {
        result.URL = downloadURL
    }

    return result, domain.ResultTypeExport, nil
}

func (e *ExportJobExecutor) getOrCreateJobRun(jobID string) (domain.JobRun, error) {
    // This would be called from a higher level with the actual run ID
    // For now, simplified
    return domain.NewJobRun(jobID), nil
}
```

```go
// 3. Add startup recovery
package scheduler

type PollingRecovery struct {
    runRepo       usecases.JobRunRepository
    jobRepo       usecases.JobRepository
    exportClient  *export.Client
    userValidator identity.UserValidator
    config        *config.Config
    logger        *slog.Logger
}

func NewPollingRecovery(
    runRepo usecases.JobRunRepository,
    jobRepo usecases.JobRepository,
    exportClient *export.Client,
    userValidator identity.UserValidator,
    config *config.Config,
    logger *slog.Logger,
) *PollingRecovery {
    return &PollingRecovery{
        runRepo:       runRepo,
        jobRepo:       jobRepo,
        exportClient:  exportClient,
        userValidator: userValidator,
        config:        config,
        logger:        logger,
    }
}

func (r *PollingRecovery) RecoverInFlightPolls(ctx context.Context) error {
    // Find all "running" job runs
    runningRuns, err := r.runRepo.FindByStatus(ctx, domain.RunStatusRunning)
    if err != nil {
        return fmt.Errorf("failed to find running jobs: %w", err)
    }

    r.logger.Info("Found in-flight job runs", slog.Int("count", len(runningRuns)))

    for _, run := range runningRuns {
        if run.ExternalJobID == nil || run.ExternalService == nil {
            // No external job ID - must have crashed before creating external job
            // Mark as failed
            r.logger.Warn("Job run has no external job ID, marking as failed",
                slog.String("run_id", run.ID))
            
            run = run.WithFailed("Lost during restart before external job creation")
            r.runRepo.Save(run)
            continue
        }

        age := time.Since(run.StartTime)
        if age > 30*time.Minute {
            // Too old - mark as failed
            r.logger.Warn("Job run too old, marking as failed",
                slog.String("run_id", run.ID),
                slog.Duration("age", age))
            
            run = run.WithFailed("Execution timeout - exceeded maximum duration")
            r.runRepo.Save(run)
            continue
        }

        // Resume polling in background
        r.logger.Info("Resuming polling for job run",
            slog.String("run_id", run.ID),
            slog.String("external_job_id", *run.ExternalJobID),
            slog.String("service", *run.ExternalService))

        go r.resumePoll(ctx, run)
    }

    return nil
}

func (r *PollingRecovery) resumePoll(ctx context.Context, run domain.JobRun) {
    logger := r.logger.With(
        slog.String("run_id", run.ID),
        slog.String("external_job_id", *run.ExternalJobID),
    )

    // Get job details for identity
    job, err := r.jobRepo.Get(ctx, run.JobID)
    if err != nil {
        logger.Error("Failed to get job", slog.Any("error", err))
        run = run.WithFailed("Failed to retrieve job details on resume")
        r.runRepo.Save(run)
        return
    }

    // Generate identity header
    identityHeader, err := r.userValidator.GenerateIdentityHeader(ctx, job.OrgID, job.UserID)
    if err != nil {
        logger.Error("Failed to generate identity", slog.Any("error", err))
        run = run.WithFailed("Failed to generate identity on resume")
        r.runRepo.Save(run)
        return
    }

    switch *run.ExternalService {
    case "export":
        r.resumeExportPoll(ctx, run, job, identityHeader, logger)
    case "pdf":
        // r.resumePDFPoll(ctx, run, job, identityHeader, logger)
        logger.Warn("PDF polling not yet implemented")
    default:
        logger.Error("Unknown external service", slog.String("service", *run.ExternalService))
        run = run.WithFailed(fmt.Sprintf("Unknown service type: %s", *run.ExternalService))
        r.runRepo.Save(run)
    }
}

func (r *PollingRecovery) resumeExportPoll(
    ctx context.Context,
    run domain.JobRun,
    job domain.Job,
    identityHeader string,
    logger *slog.Logger,
) {
    logger.Info("Resuming export polling")

    // Create poller
    poller := export.NewExportPoller(r.exportClient, identityHeader)
    pollConfig := polling.Config{
        MaxRetries:   r.config.ExportService.PollMaxRetries,
        PollInterval: r.config.ExportService.PollInterval,
        Timeout:      9 * time.Minute,
    }

    // Resume polling from the beginning (we don't track attempt number)
    pollResult, err := polling.Poll(ctx, poller, *run.ExternalJobID, pollConfig)
    if err != nil {
        logger.Error("Resumed polling failed", slog.Any("error", err))
        run = run.WithFailed(fmt.Sprintf("Polling failed on resume: %v", err))
        r.runRepo.Save(run)
        return
    }

    logger.Info("Resumed polling completed",
        slog.String("status", string(pollResult.Status)))

    // Update job run
    downloadURL := ""
    if pollResult.Status == polling.StatusComplete {
        downloadURL = r.exportClient.GetExportDownloadURL(*run.ExternalJobID)
    }

    result := domain.ExportResult{
        ExportID: *run.ExternalJobID,
        URL:      downloadURL,
    }

    run = run.WithCompleted(domain.ResultTypeExport, result)
    r.runRepo.Save(run)

    logger.Info("Job run completed after resume")
}
```

```go
// 4. Add to server startup
func main() {
    // ... existing initialization ...

    // Create recovery service
    recovery := scheduler.NewPollingRecovery(
        jobRunRepo,
        jobRepo,
        exportClient,
        userValidator,
        cfg,
        logger,
    )

    // Recover in-flight polls on startup
    ctx := context.Background()
    if err := recovery.RecoverInFlightPolls(ctx); err != nil {
        logger.Error("Failed to recover in-flight polls", slog.Any("error", err))
        // Don't fail startup - just log the error
    }

    // ... continue with normal startup ...
}
```

### Pros & Cons

✅ **Pros**:
- Simple implementation
- Uses existing PostgreSQL infrastructure
- Transactional consistency
- No new dependencies
- Durable (survives database restarts too)
- Easy to query/debug (SQL)

⚠️ **Cons**:
- Restarts polling from scratch (attempt 1), not from last attempt
- DB writes on every job creation (minimal overhead)
- No progress tracking during long polls

## Option 2: Redis-Only (Fast & Lightweight)

### Architecture

Store polling state in Redis. On restart, check Redis for in-flight polls.

### Redis Schema

```
Key Pattern: polling:state:{job_run_id}
Value: JSON object with polling state
TTL: 1 hour (auto-cleanup)

{
  "job_run_id": "run-abc-123",
  "external_job_id": "export-xyz-789",
  "external_service": "export",
  "job_id": "job-1",
  "org_id": "org-1",
  "user_id": "user-1",
  "started_at": "2026-07-13T09:00:00Z",
  "current_attempt": 15,
  "last_poll_at": "2026-07-13T09:01:15Z",
  "last_status": "running"
}
```

### Code Implementation

```go
package polling

import (
    "context"
    "encoding/json"
    "fmt"
    "time"
    "github.com/redis/go-redis/v9"
)

type RedisPollingState struct {
    JobRunID       string    `json:"job_run_id"`
    ExternalJobID  string    `json:"external_job_id"`
    ExternalService string   `json:"external_service"`
    JobID          string    `json:"job_id"`
    OrgID          string    `json:"org_id"`
    UserID         string    `json:"user_id"`
    StartedAt      time.Time `json:"started_at"`
    CurrentAttempt int       `json:"current_attempt"`
    LastPollAt     time.Time `json:"last_poll_at"`
    LastStatus     string    `json:"last_status"`
}

type RedisPollingStore struct {
    client *redis.Client
    ttl    time.Duration
}

func NewRedisPollingStore(client *redis.Client) *RedisPollingStore {
    return &RedisPollingStore{
        client: client,
        ttl:    1 * time.Hour,
    }
}

func (s *RedisPollingStore) SaveState(ctx context.Context, state RedisPollingState) error {
    key := fmt.Sprintf("polling:state:%s", state.JobRunID)
    
    data, err := json.Marshal(state)
    if err != nil {
        return fmt.Errorf("failed to marshal state: %w", err)
    }
    
    return s.client.Set(ctx, key, data, s.ttl).Err()
}

func (s *RedisPollingStore) GetState(ctx context.Context, jobRunID string) (*RedisPollingState, error) {
    key := fmt.Sprintf("polling:state:%s", jobRunID)
    
    data, err := s.client.Get(ctx, key).Bytes()
    if err == redis.Nil {
        return nil, nil // Not found
    }
    if err != nil {
        return nil, fmt.Errorf("failed to get state: %w", err)
    }
    
    var state RedisPollingState
    if err := json.Unmarshal(data, &state); err != nil {
        return nil, fmt.Errorf("failed to unmarshal state: %w", err)
    }
    
    return &state, nil
}

func (s *RedisPollingStore) DeleteState(ctx context.Context, jobRunID string) error {
    key := fmt.Sprintf("polling:state:%s", jobRunID)
    return s.client.Del(ctx, key).Err()
}

func (s *RedisPollingStore) GetAllInFlightStates(ctx context.Context) ([]RedisPollingState, error) {
    // Scan for all polling:state:* keys
    var cursor uint64
    var states []RedisPollingState
    
    for {
        keys, newCursor, err := s.client.Scan(ctx, cursor, "polling:state:*", 100).Result()
        if err != nil {
            return nil, err
        }
        
        for _, key := range keys {
            data, err := s.client.Get(ctx, key).Bytes()
            if err != nil {
                continue // Skip errors
            }
            
            var state RedisPollingState
            if err := json.Unmarshal(data, &state); err != nil {
                continue
            }
            
            states = append(states, state)
        }
        
        cursor = newCursor
        if cursor == 0 {
            break
        }
    }
    
    return states, nil
}

// Stateful poller that updates Redis on each attempt
type StatefulPoller struct {
    basePoller Poller
    store      *RedisPollingStore
    state      RedisPollingState
}

func NewStatefulPoller(basePoller Poller, store *RedisPollingStore, state RedisPollingState) *StatefulPoller {
    return &StatefulPoller{
        basePoller: basePoller,
        store:      store,
        state:      state,
    }
}

func (p *StatefulPoller) GetStatus(ctx context.Context, jobID string) (*StatusResponse, error) {
    // Increment attempt
    p.state.CurrentAttempt++
    p.state.LastPollAt = time.Now().UTC()
    
    // Get status
    status, err := p.basePoller.GetStatus(ctx, jobID)
    if err != nil {
        return nil, err
    }
    
    // Update state
    p.state.LastStatus = string(status.Status)
    
    // Save to Redis (fire and forget - don't fail poll if Redis fails)
    go p.store.SaveState(context.Background(), p.state)
    
    return status, nil
}

func (p *StatefulPoller) IsTerminalStatus(status JobStatus) bool {
    return p.basePoller.IsTerminalStatus(status)
}

// Poll with state tracking
func PollWithState(
    ctx context.Context,
    poller Poller,
    store *RedisPollingStore,
    state RedisPollingState,
    jobID string,
    cfg Config,
) (*StatusResponse, error) {
    // Wrap poller with state tracking
    statefulPoller := NewStatefulPoller(poller, store, state)
    
    // Save initial state
    if err := store.SaveState(ctx, state); err != nil {
        // Log but don't fail
        fmt.Printf("Failed to save initial state: %v\n", err)
    }
    
    // Use regular Poll with stateful poller
    result, err := Poll(ctx, statefulPoller, jobID, cfg)
    
    // Clean up state on completion
    if err == nil && result.IsTerminal {
        store.DeleteState(context.Background(), state.JobRunID)
    }
    
    return result, err
}
```

### Pros & Cons

✅ **Pros**:
- Fast writes (Redis optimized for this)
- Progress tracking (current attempt number)
- Auto-cleanup via TTL
- No DB schema changes
- Can resume from last attempt (not just restart)

⚠️ **Cons**:
- Depends on Redis availability
- Depends on Redis persistence config
- Data loss if Redis fails before sync to disk
- More complex recovery logic
- Adds Redis operations on every poll attempt

## Option 3: Hybrid (PostgreSQL + Redis)

### Architecture

Store durable state (external job ID) in PostgreSQL, transient state (poll progress) in Redis.

### Implementation

```go
// PostgreSQL: External job ID (durable)
type JobRun struct {
    ExternalJobID   *string `json:"external_job_id,omitempty"`
    ExternalService *string `json:"external_service,omitempty"`
}

// Redis: Poll progress (transient)
type RedisPollingProgress struct {
    CurrentAttempt int       `json:"current_attempt"`
    LastPollAt     time.Time `json:"last_poll_at"`
    LastStatus     string    `json:"last_status"`
}

// On job creation
func Execute(job) {
    createResult := createExport()
    
    // Save to PostgreSQL (durable)
    jobRun.ExternalJobID = &createResult.ID
    db.Save(jobRun)
    
    // Save to Redis (transient progress)
    redis.Set("poll:progress:" + jobRun.ID, progress, 1*time.Hour)
    
    // Poll
    Poll(...)
}

// On restart
func Recover() {
    // Find from PostgreSQL
    runs := db.FindRunning()
    
    for _, run := range runs {
        // Check Redis for progress
        progress := redis.Get("poll:progress:" + run.ID)
        
        if progress != nil {
            // Resume from attempt N
            resumeFromAttempt(run, progress.CurrentAttempt)
        } else {
            // No progress - start from beginning
            resumeFromAttempt(run, 0)
        }
    }
}
```

### Pros & Cons

✅ **Pros**:
- Best of both worlds
- Durable external job ID (survives everything)
- Fast progress tracking (Redis)
- Graceful degradation (works without Redis progress)

⚠️ **Cons**:
- Most complex option
- Two systems to maintain
- Need to handle inconsistency

## Option 4: PostgreSQL with Periodic State Updates

### Architecture

Store external job ID immediately, update progress periodically (every 10 attempts or 1 minute).

### Implementation

```sql
ALTER TABLE job_runs
ADD COLUMN external_job_id TEXT,
ADD COLUMN external_service TEXT,
ADD COLUMN poll_started_at TIMESTAMP,
ADD COLUMN poll_last_attempt INT DEFAULT 0,
ADD COLUMN poll_last_update_at TIMESTAMP;
```

```go
func Poll(ctx, poller, jobID, cfg) {
    for attempt := 0; attempt < cfg.MaxRetries; attempt++ {
        status := poller.GetStatus(ctx, jobID)
        
        // Update DB every 10 attempts or every minute
        if attempt % 10 == 0 || time.Since(lastUpdate) > 1*time.Minute {
            updatePollProgress(jobRunID, attempt)
            lastUpdate = time.Now()
        }
        
        if status.IsTerminal {
            return status
        }
        
        time.Sleep(cfg.PollInterval)
    }
}
```

### Pros & Cons

✅ **Pros**:
- Simple (PostgreSQL only)
- Progress tracking
- Lower DB load (batched updates)

⚠️ **Cons**:
- Still loses some progress on crash (up to 10 attempts)
- DB writes during polling
- More complex than Option 1

## Recommendation Matrix

| Scenario | Recommended Option | Why |
|----------|-------------------|-----|
| **Simple & Reliable** | Option 1 (PostgreSQL only) | Easiest to implement, maintain, debug |
| **High Volume** | Option 3 (Hybrid) | Minimize DB load while maintaining durability |
| **Mission Critical** | Option 3 (Hybrid) | Maximum recoverability |
| **Already using Redis heavily** | Option 2 (Redis only) | Leverage existing infrastructure |
| **Quick Win** | Option 1 (PostgreSQL only) | Can implement in < 4 hours |

## My Recommendation: Start with Option 1

**Why**: 
1. **Simplest** - Just add 3 fields to existing table
2. **Most Reliable** - PostgreSQL is already your source of truth
3. **Easiest to Debug** - `SELECT * FROM job_runs WHERE external_job_id = '...'`
4. **Sufficient** - Restarting from attempt 1 is fine for 5-minute polls
5. **Fast to Implement** - Can ship today

**Upgrade Path**:
- If you outgrow it (high volume, need progress tracking), add Redis layer later
- Schema supports both Option 1 and Option 3

## Next Steps

1. Implement Option 1 (PostgreSQL-only)
2. Test recovery with intentional pod kills
3. Monitor metrics (orphaned runs, recovery success rate)
4. If needed, add Redis progress tracking (upgrade to Option 3)

Want me to implement Option 1 now?
