# Multi-Pod Resumable Polling Design

## Problem Statement

**What happens when multiple scheduler pods try to resume the same in-flight job after a restart?**

This is critical for production deployments where you have:
- 3+ scheduler pods for high availability
- Rolling deployments (pods restart one by one)
- Auto-scaling (pods come and go)

## The Race Condition

### Scenario: 3 Pods, 1 In-Flight Job

```
09:00:00 - Job starts on Pod A
           Creates export (ID: export-xyz-789)
           Saves to DB: job_run (ID: run-abc-123, external_job_id: export-xyz-789, status: "running")
           Starts polling...

09:00:30 - Rolling deployment begins
           Pod A terminates
           
09:00:35 - Pod A' starts up
           Pod B already running
           Pod C already running

09:00:36 - ALL THREE PODS query database:
           SELECT * FROM job_runs WHERE status='running' AND external_job_id IS NOT NULL
           
           Result: All see run-abc-123 with export-xyz-789
           
09:00:37 - ❌ PROBLEM: All three pods try to resume polling!
           Pod A': polling.Poll(ctx, poller, "export-xyz-789", config)
           Pod B:  polling.Poll(ctx, poller, "export-xyz-789", config)
           Pod C:  polling.Poll(ctx, poller, "export-xyz-789", config)
           
           Result: 3x duplicate API calls to export service
                   3x duplicate notifications sent
                   3x duplicate JobRun updates (race condition)
```

### Impact Analysis

**Without distributed locking:**

```
Scenario: 10 in-flight jobs, 3 pods restart

Result:
- 30 polling operations started (3 pods × 10 jobs)
- Export service receives 30 concurrent poll requests (should be 10)
- 3x API quota consumed
- Race conditions updating JobRun status
- Duplicate notifications sent to users (3x)
```

**This is catastrophic in production.**

## Solutions

### Solution 1: Distributed Locks (Redis) ⭐ RECOMMENDED

Use Redis SET NX (set if not exists) to claim jobs atomically.

#### Implementation

```go
package scheduler

import (
    "context"
    "fmt"
    "time"
    "github.com/redis/go-redis/v9"
)

type PollingRecovery struct {
    runRepo       usecases.JobRunRepository
    jobRepo       usecases.JobRepository
    redisClient   *redis.Client
    exportClient  *export.Client
    userValidator identity.UserValidator
    config        *config.Config
    logger        *slog.Logger
    podID         string  // Unique ID for this pod
}

func NewPollingRecovery(
    runRepo usecases.JobRunRepository,
    jobRepo usecases.JobRepository,
    redisClient *redis.Client,
    exportClient *export.Client,
    userValidator identity.UserValidator,
    config *config.Config,
    logger *slog.Logger,
) *PollingRecovery {
    // Generate unique pod ID
    podID := fmt.Sprintf("pod-%s-%d", os.Getenv("HOSTNAME"), os.Getpid())
    
    return &PollingRecovery{
        runRepo:       runRepo,
        jobRepo:       jobRepo,
        redisClient:   redisClient,
        exportClient:  exportClient,
        userValidator: userValidator,
        config:        config,
        logger:        logger,
        podID:         podID,
    }
}

func (r *PollingRecovery) RecoverInFlightPolls(ctx context.Context) error {
    // Find all "running" job runs
    runningRuns, err := r.runRepo.FindByStatus(ctx, domain.RunStatusRunning)
    if err != nil {
        return fmt.Errorf("failed to find running jobs: %w", err)
    }

    r.logger.Info("Found in-flight job runs",
        slog.Int("count", len(runningRuns)),
        slog.String("pod_id", r.podID))

    for _, run := range runningRuns {
        if run.ExternalJobID == nil || run.ExternalService == nil {
            // No external job - mark as failed
            r.logger.Warn("Job run has no external job ID",
                slog.String("run_id", run.ID))
            
            run = run.WithFailed("Lost during restart before external job creation")
            r.runRepo.Save(run)
            continue
        }

        // Try to claim this job
        claimed, err := r.tryClaimJob(ctx, run.ID)
        if err != nil {
            r.logger.Error("Failed to claim job",
                slog.String("run_id", run.ID),
                slog.Any("error", err))
            continue
        }

        if !claimed {
            // Another pod already claimed it
            r.logger.Debug("Job already claimed by another pod",
                slog.String("run_id", run.ID))
            continue
        }

        // We claimed it - resume polling
        r.logger.Info("Claimed job for polling recovery",
            slog.String("run_id", run.ID),
            slog.String("external_job_id", *run.ExternalJobID),
            slog.String("pod_id", r.podID))

        go r.resumePollWithLock(ctx, run)
    }

    return nil
}

func (r *PollingRecovery) tryClaimJob(ctx context.Context, jobRunID string) (bool, error) {
    lockKey := fmt.Sprintf("polling:lock:%s", jobRunID)
    lockValue := r.podID
    lockTTL := 15 * time.Minute  // Max polling duration
    
    // Try to acquire lock using SET NX (set if not exists)
    success, err := r.redisClient.SetNX(ctx, lockKey, lockValue, lockTTL).Result()
    if err != nil {
        return false, fmt.Errorf("redis SetNX failed: %w", err)
    }
    
    return success, nil
}

func (r *PollingRecovery) releaseLock(ctx context.Context, jobRunID string) error {
    lockKey := fmt.Sprintf("polling:lock:%s", jobRunID)
    
    // Only delete if we own the lock (check value matches our pod ID)
    script := `
        if redis.call("get", KEYS[1]) == ARGV[1] then
            return redis.call("del", KEYS[1])
        else
            return 0
        end
    `
    
    _, err := r.redisClient.Eval(ctx, script, []string{lockKey}, r.podID).Result()
    return err
}

func (r *PollingRecovery) resumePollWithLock(ctx context.Context, run domain.JobRun) {
    // Ensure lock is released when done
    defer r.releaseLock(context.Background(), run.ID)
    
    logger := r.logger.With(
        slog.String("run_id", run.ID),
        slog.String("external_job_id", *run.ExternalJobID),
        slog.String("pod_id", r.podID),
    )

    // ... rest of polling logic (same as before)
    job, err := r.jobRepo.Get(ctx, run.JobID)
    if err != nil {
        logger.Error("Failed to get job", slog.Any("error", err))
        run = run.WithFailed("Failed to retrieve job details on resume")
        r.runRepo.Save(run)
        return
    }

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

    poller := export.NewExportPoller(r.exportClient, identityHeader)
    pollConfig := polling.Config{
        MaxRetries:   r.config.ExportService.PollMaxRetries,
        PollInterval: r.config.ExportService.PollInterval,
        Timeout:      9 * time.Minute,
    }

    pollResult, err := polling.Poll(ctx, poller, *run.ExternalJobID, pollConfig)
    if err != nil {
        logger.Error("Resumed polling failed", slog.Any("error", err))
        run = run.WithFailed(fmt.Sprintf("Polling failed on resume: %v", err))
        r.runRepo.Save(run)
        return
    }

    logger.Info("Resumed polling completed",
        slog.String("status", string(pollResult.Status)))

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

#### Verification

```go
// Test that only one pod claims the job
func TestMultiPodRecovery(t *testing.T) {
    redis := setupRedis()
    
    // Simulate 3 pods
    recovery1 := NewPollingRecovery(..., "pod-1")
    recovery2 := NewPollingRecovery(..., "pod-2")
    recovery3 := NewPollingRecovery(..., "pod-3")
    
    // All try to recover same job
    var wg sync.WaitGroup
    wg.Add(3)
    
    claimed := make([]bool, 3)
    
    go func() {
        claimed[0], _ = recovery1.tryClaimJob(ctx, "run-abc-123")
        wg.Done()
    }()
    
    go func() {
        claimed[1], _ = recovery2.tryClaimJob(ctx, "run-abc-123")
        wg.Done()
    }()
    
    go func() {
        claimed[2], _ = recovery3.tryClaimJob(ctx, "run-abc-123")
        wg.Done()
    }()
    
    wg.Wait()
    
    // Verify exactly ONE pod claimed it
    claimedCount := 0
    for _, c := range claimed {
        if c {
            claimedCount++
        }
    }
    
    assert.Equal(t, 1, claimedCount, "Exactly one pod should claim the job")
}
```

---

### Solution 2: PostgreSQL Advisory Locks

Use PostgreSQL's built-in advisory locks (no Redis needed).

#### Implementation

```go
func (r *PostgresJobRunRepository) TryClaimForResume(ctx context.Context, jobRunID string, podID string) (bool, error) {
    // PostgreSQL advisory lock using job run ID hash
    lockID := hashToInt64(jobRunID)
    
    query := `SELECT pg_try_advisory_lock($1)`
    
    var acquired bool
    err := r.db.QueryRowContext(ctx, query, lockID).Scan(&acquired)
    if err != nil {
        return false, err
    }
    
    if acquired {
        // Store who owns the lock (for debugging)
        updateQuery := `
            UPDATE job_runs 
            SET poll_owner = $1, poll_claimed_at = NOW()
            WHERE id = $2 AND status = 'running'
        `
        _, err := r.db.ExecContext(ctx, updateQuery, podID, jobRunID)
        if err != nil {
            // Release lock if update fails
            r.db.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, lockID)
            return false, err
        }
    }
    
    return acquired, nil
}

func (r *PostgresJobRunRepository) ReleaseLock(ctx context.Context, jobRunID string) error {
    lockID := hashToInt64(jobRunID)
    
    query := `SELECT pg_advisory_unlock($1)`
    _, err := r.db.ExecContext(ctx, query, lockID)
    
    return err
}

func hashToInt64(s string) int64 {
    h := fnv.New64a()
    h.Write([]byte(s))
    return int64(h.Sum64())
}
```

#### Schema Changes

```sql
-- Add fields to track lock ownership
ALTER TABLE job_runs
ADD COLUMN poll_owner TEXT,          -- Which pod is polling
ADD COLUMN poll_claimed_at TIMESTAMP; -- When claimed

-- Index for debugging
CREATE INDEX idx_job_runs_poll_owner ON job_runs(poll_owner) 
WHERE status = 'running';
```

**Pros**:
- ✅ No Redis dependency
- ✅ Transactional with JobRun updates
- ✅ Automatic cleanup on connection loss

**Cons**:
- ⚠️ Locks tied to DB connection
- ⚠️ Released on connection pool recycling (SetConnMaxLifetime)
- ⚠️ Complex if using connection pooling

---

### Solution 3: Optimistic Locking (PostgreSQL Only)

Use a version column to detect concurrent updates.

#### Implementation

```sql
-- Add version column
ALTER TABLE job_runs
ADD COLUMN poll_version INTEGER DEFAULT 0,
ADD COLUMN poll_owner TEXT;
```

```go
func (r *PostgresJobRunRepository) TryClaimForResume(ctx context.Context, jobRunID string, podID string) (bool, error) {
    // Try to claim by incrementing version
    query := `
        UPDATE job_runs
        SET poll_owner = $1,
            poll_version = poll_version + 1,
            poll_claimed_at = NOW()
        WHERE id = $2 
          AND status = 'running'
          AND (poll_owner IS NULL OR poll_claimed_at < NOW() - INTERVAL '15 minutes')
        RETURNING poll_version
    `
    
    var newVersion int
    err := r.db.QueryRowContext(ctx, query, podID, jobRunID).Scan(&newVersion)
    
    if err == sql.ErrNoRows {
        // Another pod already claimed it (or not running)
        return false, nil
    }
    
    if err != nil {
        return false, err
    }
    
    return true, nil
}
```

**Pros**:
- ✅ No Redis dependency
- ✅ Simpler than advisory locks
- ✅ No connection pooling issues

**Cons**:
- ⚠️ Stale claims if pod crashes (need TTL logic)
- ⚠️ Race window between SELECT and UPDATE

---

### Solution 4: SELECT FOR UPDATE SKIP LOCKED (PostgreSQL 9.5+)

Modern PostgreSQL feature for work queue pattern.

#### Implementation

```go
func (r *PostgresJobRunRepository) ClaimNextResumableJob(ctx context.Context, podID string) (*domain.JobRun, error) {
    tx, err := r.db.BeginTx(ctx, nil)
    if err != nil {
        return nil, err
    }
    defer tx.Rollback()
    
    // Get one job that's not locked by another transaction
    query := `
        UPDATE job_runs
        SET poll_owner = $1,
            poll_claimed_at = NOW()
        WHERE id = (
            SELECT id FROM job_runs
            WHERE status = 'running'
              AND external_job_id IS NOT NULL
              AND (poll_owner IS NULL OR poll_claimed_at < NOW() - INTERVAL '15 minutes')
            ORDER BY start_time ASC
            LIMIT 1
            FOR UPDATE SKIP LOCKED
        )
        RETURNING id, job_id, status, start_time, external_job_id, external_service
    `
    
    var run domain.JobRun
    err = tx.QueryRowContext(ctx, query, podID).Scan(
        &run.ID, &run.JobID, &run.Status, &run.StartTime, &run.ExternalJobID, &run.ExternalService,
    )
    
    if err == sql.ErrNoRows {
        // No jobs available
        return nil, nil
    }
    
    if err != nil {
        return nil, err
    }
    
    if err := tx.Commit(); err != nil {
        return nil, err
    }
    
    return &run, nil
}

// Recovery process
func (r *PollingRecovery) RecoverInFlightPolls(ctx context.Context) error {
    for {
        // Each pod claims one job at a time
        run, err := r.runRepo.ClaimNextResumableJob(ctx, r.podID)
        if err != nil {
            return err
        }
        
        if run == nil {
            // No more jobs to claim
            break
        }
        
        // Resume this job in background
        go r.resumePoll(ctx, *run)
    }
    
    return nil
}
```

**Pros**:
- ✅ No Redis dependency
- ✅ Atomic claim (no race conditions)
- ✅ Built-in PostgreSQL feature
- ✅ Simple and elegant

**Cons**:
- ⚠️ Requires PostgreSQL 9.5+
- ⚠️ Need cleanup for stale claims

---

## Comparison Matrix

| Solution | Pros | Cons | Best For |
|----------|------|------|----------|
| **Redis Locks (SET NX)** | ✅ Distributed<br>✅ TTL auto-cleanup<br>✅ Well-tested pattern | ⚠️ Redis dependency<br>⚠️ Two systems to coordinate | Already using Redis |
| **PostgreSQL Advisory Locks** | ✅ No Redis<br>✅ Transactional | ⚠️ Connection-tied<br>⚠️ Pool complications | Single DB system |
| **Optimistic Locking** | ✅ Simple<br>✅ No Redis | ⚠️ Manual TTL<br>⚠️ Race window | Low concurrency |
| **SELECT FOR UPDATE SKIP LOCKED** | ✅ Atomic<br>✅ No Redis<br>✅ Elegant | ⚠️ PostgreSQL 9.5+<br>⚠️ Manual TTL | Modern PostgreSQL |

## Recommended Approach

### If You're Using Redis Already (RedisScheduler): Solution 1 ⭐

You already have `RedisScheduler` with locking patterns - reuse them:

```go
// Your existing pattern (redis_scheduler.go:48-61)
const (
    jobLockKeyPrefix = "scheduler:lock:"
    lockTTL = 5 * time.Minute
)

// Add polling locks
const (
    pollingLockKeyPrefix = "scheduler:polling:lock:"
    pollingLockTTL = 15 * time.Minute
)

// Reuse your existing Redis client
func (r *PollingRecovery) tryClaimJob(ctx context.Context, jobRunID string) (bool, error) {
    lockKey := pollingLockKeyPrefix + jobRunID
    return r.redisClient.SetNX(ctx, lockKey, r.podID, pollingLockTTL).Result()
}
```

**Advantages**:
- Consistent with existing architecture
- Already managing Redis
- Proven pattern in your codebase

### If NOT Using Redis: Solution 4

Use PostgreSQL SELECT FOR UPDATE SKIP LOCKED:

```go
func (r *PollingRecovery) RecoverInFlightPolls(ctx context.Context) error {
    for {
        run, _ := r.runRepo.ClaimNextResumableJob(ctx, r.podID)
        if run == nil {
            break
        }
        go r.resumePoll(ctx, *run)
    }
}
```

**Advantages**:
- Single system (PostgreSQL)
- Atomic operations
- No additional infrastructure

## Implementation for Multi-Pod Safety

### Complete Flow with Redis Locks

```
Pod Startup Sequence:

1. Pod A starts
   └─> RecoverInFlightPolls()
       └─> Query: SELECT * FROM job_runs WHERE status='running' AND external_job_id IS NOT NULL
           Result: [run-1, run-2, run-3]
       
       └─> For each run:
           ├─> Redis SET NX polling:lock:run-1 pod-a EX 900
           │   Success! (claimed)
           │   └─> go resumePoll(run-1)
           │
           ├─> Redis SET NX polling:lock:run-2 pod-a EX 900
           │   Success! (claimed)
           │   └─> go resumePoll(run-2)
           │
           └─> Redis SET NX polling:lock:run-3 pod-a EX 900
               Success! (claimed)
               └─> go resumePoll(run-3)

2. Pod B starts (2 seconds later)
   └─> RecoverInFlightPolls()
       └─> Query: SELECT * FROM job_runs WHERE status='running' ...
           Result: [run-1, run-2, run-3] (same jobs!)
       
       └─> For each run:
           ├─> Redis SET NX polling:lock:run-1 pod-b EX 900
           │   FAIL (already locked by pod-a)
           │   └─> Skip
           │
           ├─> Redis SET NX polling:lock:run-2 pod-b EX 900
           │   FAIL (already locked by pod-a)
           │   └─> Skip
           │
           └─> Redis SET NX polling:lock:run-3 pod-b EX 900
               FAIL (already locked by pod-a)
               └─> Skip

3. Pod C starts (2 seconds later)
   └─> Same as Pod B - all jobs already claimed
   └─> Nothing to do

Result: Only Pod A polls the 3 jobs (exactly once each)
```

### Stale Lock Handling

```go
// Lock TTL should be longer than max polling duration
const pollingLockTTL = 15 * time.Minute

// On startup, clean up stale locks from crashed pods
func (r *PollingRecovery) cleanupStaleLocks(ctx context.Context) error {
    // Find jobs that have been "running" for too long
    staleRuns, err := r.runRepo.FindByStatusAndOlderThan(
        ctx,
        domain.RunStatusRunning,
        time.Now().Add(-30*time.Minute),
    )
    
    for _, run := range staleRuns {
        lockKey := pollingLockKeyPrefix + run.ID
        
        // Delete lock to allow re-claiming
        r.redisClient.Del(ctx, lockKey)
        
        r.logger.Warn("Cleaned up stale polling lock",
            slog.String("run_id", run.ID))
    }
    
    return nil
}
```

## Testing Multi-Pod Scenarios

```bash
# Test 1: Simulate 3 pods starting simultaneously
for i in 1 2 3; do
    POD_ID=pod-$i ./scheduler &
    sleep 0.1  # Slight stagger
done

# Verify only one pod claimed each job
redis-cli KEYS "scheduler:polling:lock:*"

# Test 2: Kill pod mid-polling, verify another can claim after TTL
POD_ID=pod-1 ./scheduler &
PID=$!
sleep 5
kill $PID

# Wait for lock TTL
sleep 900

# Start new pod - should be able to claim
POD_ID=pod-2 ./scheduler
```

## Summary

### Without Multi-Pod Support (Current)

```
3 pods, 10 in-flight jobs:
❌ 30 polling operations (3 × 10)
❌ 3× API calls to export service
❌ Race conditions on updates
❌ Duplicate notifications
```

### With Multi-Pod Support (Recommended)

```
3 pods, 10 in-flight jobs:
✅ 10 polling operations (1 per job)
✅ Normal API call volume
✅ No race conditions
✅ One notification per job
✅ Automatic claim on pod crash (after TTL)
```

### Implementation Choice

**If using Redis already**: Use Solution 1 (Redis locks)  
**If PostgreSQL only**: Use Solution 4 (SELECT FOR UPDATE SKIP LOCKED)

Both ensure exactly-once resume semantics in multi-pod deployments.
