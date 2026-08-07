# Database Restart/Upgrade Resilience

## Problem Statement

**What happens when PostgreSQL restarts or is upgraded during active polling operations?**

This is critical because:
1. **Database maintenance windows** - Regular upgrades/restarts
2. **Failover events** - Primary DB fails, replica promoted
3. **Connection loss** - Network issues, connection pool exhaustion
4. **Schema migrations** - Adding the `external_job_id` column requires careful coordination

## Current Code Analysis

### Connection Management (Found Issues)

```go
// internal/shell/storage/postgres_repository.go:21-43
func NewPostgresJobRepository(cfg *config.Config, logger *slog.Logger) (*PostgresJobRepository, error) {
    db, err := sql.Open("postgres", connStr)
    if err != nil {
        return nil, err
    }
    if err := db.Ping(); err != nil {
        return nil, err
    }
    
    // ❌ NO CONNECTION POOL CONFIGURATION
    // ❌ NO RETRY LOGIC
    // ❌ NO HEALTH CHECK MONITORING
    
    return &PostgresJobRepository{db: db, logger: logger}, nil
}
```

**Problems**:
1. No `SetMaxOpenConns` / `SetMaxIdleConns` - defaults may be too low
2. No `SetConnMaxLifetime` - connections never recycled
3. Single `Ping()` at startup - no ongoing health checks
4. No retry logic on connection failure

### What Happens During DB Restart

```
Scenario: PostgreSQL restarts while 50 jobs are polling

09:00:00 - 50 jobs actively polling (making DB queries every 5 seconds)
09:00:05 - DBA runs: systemctl restart postgresql
09:00:06 - PostgreSQL stops accepting connections
09:00:07 - Next poll attempt from Job #1:
           query := "UPDATE job_runs SET status='completed' ..."
           err := db.Exec(query, ...)
           
           ERROR: pq: server closed the connection unexpectedly
           
09:00:07 - Job #1 executor crashes (unhandled error)
09:00:08 - Jobs #2-50 crash one by one as they try to write
09:00:10 - PostgreSQL finishes restarting
09:00:11 - New job executions work fine
09:00:12 - But 50 in-flight jobs are LOST (JobRuns stuck as "running")
```

### Current Error Handling in Executors

```go
// internal/shell/executor/export_job_executor.go:145
result := domain.ExportResult{ExportID: createResult.ID}
if pollResult.Status == polling.StatusComplete {
    result.URL = e.exportClient.GetExportDownloadURL(createResult.ID)
}

return result, domain.ResultTypeExport, nil
// ↑ No error handling if DB is down when returning
```

```go
// internal/shell/executor/job_executor.go:77
if err := e.runRepo.Save(jobRun); err != nil {
    logger.Error("Failed to update job run record", slog.Any("error", err))
}
// ↑ Logs error but doesn't retry or fail the job
```

## Impact Analysis

### Scenario 1: DB Restart During Polling

```
Impact Level: HIGH

What Breaks:
❌ In-flight jobs can't update JobRun status
❌ Completed exports are lost (no notification sent)
❌ JobRuns stuck in "running" status
❌ New job executions may fail to create JobRuns

What Continues Working:
✅ Scheduler cron triggers (in-memory)
✅ Export service (external, unaffected)
✅ Job executions that complete before DB comes back
```

### Scenario 2: DB Failover (Primary → Replica Promotion)

```
Impact Level: MEDIUM

Typical failover: 30-60 seconds

What Happens:
1. Primary DB fails
2. Connection pool has stale connections to old primary
3. All queries fail for 30-60 seconds
4. Replica is promoted to primary
5. Connection pool slowly discovers new primary
6. Some connections succeed, some fail
7. Eventual consistency after ~2 minutes

In-flight jobs during this window: LOST
```

### Scenario 3: Schema Migration (Adding external_job_id)

```
Impact Level: CRITICAL if not handled carefully

Migration Steps:
1. ALTER TABLE job_runs ADD COLUMN external_job_id TEXT;
2. Locks table for writes
3. In-flight jobs trying to INSERT/UPDATE: BLOCKED
4. Migration completes (usually < 1 second for small tables)
5. Jobs unblock and retry

Risk: Old code without external_job_id continues running
```

## Solutions

### 1. Connection Pool Configuration

```go
package storage

import (
    "database/sql"
    "time"
)

func NewPostgresJobRepository(cfg *config.Config, logger *slog.Logger) (*PostgresJobRepository, error) {
    connStr, err := buildConnectionString(cfg)
    if err != nil {
        return nil, err
    }

    db, err := sql.Open("postgres", connStr)
    if err != nil {
        return nil, err
    }

    // ✅ Configure connection pool
    db.SetMaxOpenConns(25)                 // Max concurrent connections
    db.SetMaxIdleConns(10)                 // Keep 10 idle connections ready
    db.SetConnMaxLifetime(5 * time.Minute) // Recycle connections every 5 min
    db.SetConnMaxIdleTime(2 * time.Minute) // Close idle connections after 2 min

    // ✅ Retry initial ping
    if err := pingWithRetry(db, 3, 1*time.Second); err != nil {
        return nil, fmt.Errorf("failed to connect to database: %w", err)
    }

    logger.Info("PostgreSQL job repository initialized",
        slog.Int("max_open_conns", 25),
        slog.Int("max_idle_conns", 10),
        slog.Duration("conn_max_lifetime", 5*time.Minute))

    return &PostgresJobRepository{
        db:     db,
        logger: logger,
    }, nil
}

func pingWithRetry(db *sql.DB, maxRetries int, delay time.Duration) error {
    for i := 0; i < maxRetries; i++ {
        if err := db.Ping(); err == nil {
            return nil
        }
        if i < maxRetries-1 {
            time.Sleep(delay)
        }
    }
    return fmt.Errorf("failed to ping database after %d attempts", maxRetries)
}
```

**Why this helps**:
- `SetConnMaxLifetime`: Forces connection recycling, discovers new primary after failover
- `SetMaxOpenConns`: Prevents connection exhaustion
- `SetMaxIdleConns`: Keeps warm connections ready
- Retry logic: Handles transient failures

### 2. Retry Logic for Database Operations

```go
package storage

import (
    "context"
    "database/sql"
    "errors"
    "time"
    "github.com/lib/pq"
)

// RetryableError checks if an error is retryable
func isRetryableError(err error) bool {
    if err == nil {
        return false
    }

    // Check for PostgreSQL-specific errors
    var pqErr *pq.Error
    if errors.As(err, &pqErr) {
        // Connection errors (Class 08)
        if pqErr.Code.Class() == "08" {
            return true
        }
        // Serialization failures (Class 40)
        if pqErr.Code.Class() == "40" {
            return true
        }
    }

    // Connection-related errors
    if errors.Is(err, sql.ErrConnDone) ||
       errors.Is(err, context.DeadlineExceeded) {
        return true
    }

    // String matching for common errors
    errStr := err.Error()
    return contains(errStr, "connection refused") ||
           contains(errStr, "connection reset") ||
           contains(errStr, "broken pipe") ||
           contains(errStr, "no such host") ||
           contains(errStr, "server closed the connection")
}

// RetryConfig defines retry behavior
type RetryConfig struct {
    MaxAttempts int
    InitialDelay time.Duration
    MaxDelay     time.Duration
    Multiplier   float64
}

func DefaultRetryConfig() RetryConfig {
    return RetryConfig{
        MaxAttempts:  3,
        InitialDelay: 100 * time.Millisecond,
        MaxDelay:     5 * time.Second,
        Multiplier:   2.0,
    }
}

// RetryOperation retries a database operation with exponential backoff
func RetryOperation(ctx context.Context, cfg RetryConfig, operation func() error) error {
    var lastErr error
    delay := cfg.InitialDelay

    for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
        // Try the operation
        err := operation()
        if err == nil {
            return nil // Success!
        }

        lastErr = err

        // Don't retry if error is not retryable
        if !isRetryableError(err) {
            return err
        }

        // Don't sleep on last attempt
        if attempt < cfg.MaxAttempts-1 {
            // Check context before sleeping
            select {
            case <-ctx.Done():
                return ctx.Err()
            case <-time.After(delay):
                // Continue to next attempt
            }

            // Exponential backoff
            delay = time.Duration(float64(delay) * cfg.Multiplier)
            if delay > cfg.MaxDelay {
                delay = cfg.MaxDelay
            }
        }
    }

    return fmt.Errorf("operation failed after %d attempts: %w", cfg.MaxAttempts, lastErr)
}

// Update Save method to use retry logic
func (r *PostgresJobRunRepository) Save(jobRun domain.JobRun) error {
    ctx := context.Background()
    cfg := DefaultRetryConfig()

    return RetryOperation(ctx, cfg, func() error {
        return r.saveInternal(jobRun)
    })
}

func (r *PostgresJobRunRepository) saveInternal(jobRun domain.JobRun) error {
    // Existing save logic here
    query := `INSERT INTO job_runs (...) VALUES (...) ON CONFLICT (id) DO UPDATE ...`
    _, err := r.db.Exec(query, jobRun.ID, jobRun.JobID, ...)
    return err
}
```

**Benefits**:
- Automatically retries on transient connection errors
- Exponential backoff prevents overwhelming recovering DB
- Context-aware (respects cancellation)
- Distinguishes retryable vs non-retryable errors

### 3. Health Check Monitoring

```go
package storage

import (
    "context"
    "sync"
    "time"
    "github.com/prometheus/client_golang/prometheus"
)

var (
    dbHealthy = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "database_healthy",
        Help: "1 if database is healthy, 0 otherwise",
    })

    dbConnectionPoolSize = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "database_connection_pool_open",
        Help: "Number of open database connections",
    })
)

type HealthMonitor struct {
    db       *sql.DB
    logger   *slog.Logger
    stopCh   chan struct{}
    isHealthy bool
    mu       sync.RWMutex
}

func NewHealthMonitor(db *sql.DB, logger *slog.Logger) *HealthMonitor {
    return &HealthMonitor{
        db:        db,
        logger:    logger,
        stopCh:    make(chan struct{}),
        isHealthy: true,
    }
}

func (h *HealthMonitor) Start() {
    // Check health every 10 seconds
    ticker := time.NewTicker(10 * time.Second)
    
    go func() {
        for {
            select {
            case <-ticker.C:
                h.checkHealth()
            case <-h.stopCh:
                ticker.Stop()
                return
            }
        }
    }()
}

func (h *HealthMonitor) Stop() {
    close(h.stopCh)
}

func (h *HealthMonitor) checkHealth() {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    err := h.db.PingContext(ctx)
    
    h.mu.Lock()
    wasHealthy := h.isHealthy
    h.isHealthy = (err == nil)
    h.mu.Unlock()

    if err != nil {
        h.logger.Error("Database health check failed", slog.Any("error", err))
        dbHealthy.Set(0)
        
        if wasHealthy {
            h.logger.Warn("Database became unhealthy")
            // Could trigger alert here
        }
    } else {
        dbHealthy.Set(1)
        
        if !wasHealthy {
            h.logger.Info("Database recovered")
        }
        
        // Update connection pool metrics
        stats := h.db.Stats()
        dbConnectionPoolSize.Set(float64(stats.OpenConnections))
    }
}

func (h *HealthMonitor) IsHealthy() bool {
    h.mu.RLock()
    defer h.mu.RUnlock()
    return h.isHealthy
}
```

**Add to main.go**:
```go
func main() {
    // ... existing setup ...
    
    // Start health monitoring
    healthMonitor := storage.NewHealthMonitor(db, logger)
    healthMonitor.Start()
    defer healthMonitor.Stop()
    
    // ... continue ...
}
```

### 4. Graceful Degradation in Executors

```go
// internal/shell/executor/job_executor.go
func (e *DefaultJobExecutor) Execute(job domain.Job) error {
    // ... existing execution logic ...
    
    // Update the job run record with retry
    if e.runRepo != nil && jobRun.ID != "" {
        if execErr != nil {
            jobRun = jobRun.WithFailed(execErr.Error())
        } else {
            jobRun = jobRun.WithCompleted(resultType, result)
        }

        // ✅ RETRY DATABASE SAVES
        saveErr := e.saveWithRetry(jobRun, logger)
        if saveErr != nil {
            logger.Error("Failed to save job run after retries", 
                slog.Any("error", saveErr))
            
            // ✅ QUEUE FOR LATER PERSISTENCE
            e.queueForRetry(jobRun)
            
            // Don't fail the job - external work succeeded
            // Just log that we couldn't persist
        }
    }

    return execErr
}

func (e *DefaultJobExecutor) saveWithRetry(jobRun domain.JobRun, logger *slog.Logger) error {
    ctx := context.Background()
    cfg := storage.DefaultRetryConfig()
    
    return storage.RetryOperation(ctx, cfg, func() error {
        return e.runRepo.Save(jobRun)
    })
}

// Persist job run updates that failed due to DB issues
func (e *DefaultJobExecutor) queueForRetry(jobRun domain.JobRun) {
    // Option 1: Write to local file
    file, _ := os.OpenFile("/tmp/failed_job_runs.jsonl", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    defer file.Close()
    
    data, _ := json.Marshal(jobRun)
    file.Write(append(data, '\n'))
    
    // Option 2: Send to dead letter queue (if using messaging)
    // e.dlq.Publish("failed_job_runs", jobRun)
}

// Background process to retry failed saves
func (e *DefaultJobExecutor) retryFailedSaves() {
    ticker := time.NewTicker(1 * time.Minute)
    
    for range ticker.C {
        // Read from /tmp/failed_job_runs.jsonl
        // Try to save each one
        // Remove from file if successful
    }
}
```

### 5. Schema Migration Safety

**Migration file**: `migrations/000X_add_external_job_id.up.sql`

```sql
-- Add columns with default NULL (non-blocking)
ALTER TABLE job_runs 
ADD COLUMN IF NOT EXISTS external_job_id TEXT,
ADD COLUMN IF NOT EXISTS external_service TEXT,
ADD COLUMN IF NOT EXISTS poll_started_at TIMESTAMP;

-- Add indexes (may take a few seconds, but non-blocking with CONCURRENTLY)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_job_runs_external_job 
ON job_runs(external_job_id) 
WHERE external_job_id IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_job_runs_recovery 
ON job_runs(status, poll_started_at) 
WHERE status = 'running' AND external_job_id IS NOT NULL;

-- No data migration needed - new column is nullable
```

**Down migration**: `migrations/000X_add_external_job_id.down.sql`

```sql
DROP INDEX CONCURRENTLY IF EXISTS idx_job_runs_external_job;
DROP INDEX CONCURRENTLY IF EXISTS idx_job_runs_recovery;

ALTER TABLE job_runs 
DROP COLUMN IF EXISTS external_job_id,
DROP COLUMN IF EXISTS external_service,
DROP COLUMN IF EXISTS poll_started_at;
```

**Deployment strategy**:
```bash
# 1. Deploy new code (but don't use new columns yet)
kubectl rollout restart deployment scheduler

# 2. Run migration (adds columns)
./scheduler db_migration up

# 3. Enable feature flag to start using external_job_id
kubectl set env deployment/scheduler USE_RESUMABLE_POLLING=true

# 4. Monitor for issues
kubectl logs -f deployment/scheduler | grep "external_job_id"
```

### 6. Circuit Breaker Pattern (Advanced)

For high-reliability scenarios:

```go
package storage

import (
    "errors"
    "sync"
    "time"
)

type CircuitBreaker struct {
    maxFailures  int
    resetTimeout time.Duration
    
    mu           sync.Mutex
    failures     int
    lastFailTime time.Time
    state        string // "closed", "open", "half-open"
}

func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
    return &CircuitBreaker{
        maxFailures:  maxFailures,
        resetTimeout: resetTimeout,
        state:        "closed",
    }
}

func (cb *CircuitBreaker) Call(operation func() error) error {
    cb.mu.Lock()
    
    // Check if we should reset
    if cb.state == "open" && time.Since(cb.lastFailTime) > cb.resetTimeout {
        cb.state = "half-open"
        cb.failures = 0
    }
    
    // Reject if circuit is open
    if cb.state == "open" {
        cb.mu.Unlock()
        return errors.New("circuit breaker is open")
    }
    
    cb.mu.Unlock()
    
    // Try the operation
    err := operation()
    
    cb.mu.Lock()
    defer cb.mu.Unlock()
    
    if err != nil {
        cb.failures++
        cb.lastFailTime = time.Now()
        
        if cb.failures >= cb.maxFailures {
            cb.state = "open"
        }
        
        return err
    }
    
    // Success - reset
    if cb.state == "half-open" {
        cb.state = "closed"
    }
    cb.failures = 0
    
    return nil
}
```

**Usage**:
```go
type PostgresJobRunRepository struct {
    db             *sql.DB
    logger         *slog.Logger
    circuitBreaker *CircuitBreaker
}

func (r *PostgresJobRunRepository) Save(jobRun domain.JobRun) error {
    return r.circuitBreaker.Call(func() error {
        return r.saveInternal(jobRun)
    })
}
```

### 7. Startup Recovery with DB Check

```go
// cmd/server/main.go
func runServer(cmd *cobra.Command, args []string) {
    // ... load config ...
    
    // Wait for database to be ready
    if err := waitForDatabase(cfg, logger, 30*time.Second); err != nil {
        logger.Error("Database not ready", slog.Any("error", err))
        os.Exit(1)
    }
    
    // ... create repositories ...
    
    // Recover in-flight polls
    recovery := scheduler.NewPollingRecovery(...)
    if err := recovery.RecoverInFlightPolls(ctx); err != nil {
        logger.Error("Failed to recover in-flight polls", slog.Any("error", err))
        // Don't exit - continue with fresh state
    }
    
    // ... start scheduler ...
}

func waitForDatabase(cfg *config.Config, logger *slog.Logger, timeout time.Duration) error {
    deadline := time.Now().Add(timeout)
    
    for time.Now().Before(deadline) {
        connStr, _ := buildConnectionString(cfg)
        db, err := sql.Open("postgres", connStr)
        if err == nil {
            if err := db.Ping(); err == nil {
                db.Close()
                logger.Info("Database connection established")
                return nil
            }
            db.Close()
        }
        
        logger.Debug("Waiting for database...", slog.Any("error", err))
        time.Sleep(2 * time.Second)
    }
    
    return fmt.Errorf("database not ready after %v", timeout)
}
```

## Testing Database Resilience

### Test Scenarios

```bash
# 1. Restart PostgreSQL during active polling
# Terminal 1: Start scheduler with active jobs
./scheduler

# Terminal 2: Restart PostgreSQL
docker restart postgres
# OR
systemctl restart postgresql

# Expected: Jobs should retry and complete successfully

# 2. Failover test (if using replication)
# Promote replica to primary
pg_ctl promote -D /var/lib/postgresql/replica

# Expected: Connections refresh within 5 minutes

# 3. Connection pool exhaustion
# Create 100 concurrent jobs (exceeds pool size)
for i in {1..100}; do
    curl -X POST http://localhost:8080/api/jobs/run/job-$i &
done

# Expected: Some queue, all complete eventually

# 4. Schema migration during execution
# Terminal 1: Run jobs
./scheduler

# Terminal 2: Apply migration
./scheduler db_migration up

# Expected: Jobs continue, use new schema after restart
```

### Metrics to Monitor

```
# Database health
database_healthy 1

# Connection pool
database_connection_pool_open 15
database_connection_pool_idle 5
database_connection_pool_wait_duration_seconds{quantile="0.99"} 0.001

# Retry statistics
database_operation_retries_total{operation="save_job_run"} 5
database_operation_retry_success_total 4
database_operation_retry_failure_total 1

# Circuit breaker
database_circuit_breaker_state{state="closed"} 1
database_circuit_breaker_state{state="open"} 0
```

## Summary

### Current Issues

| Issue | Impact | Severity |
|-------|--------|----------|
| No connection pool config | Stale connections after failover | HIGH |
| No retry logic | DB restart kills in-flight jobs | HIGH |
| No health monitoring | Can't detect DB issues proactively | MEDIUM |
| No graceful degradation | Lost work if DB unavailable | HIGH |

### Recommended Implementation Order

1. **Day 1** (Critical):
   - ✅ Add connection pool configuration
   - ✅ Add retry logic to Save operations
   - ✅ Add waitForDatabase on startup

2. **Week 1** (Important):
   - ✅ Add health monitoring
   - ✅ Add metrics for connection pool
   - ✅ Test DB restart scenarios

3. **Month 1** (Nice to have):
   - ✅ Add circuit breaker
   - ✅ Add dead letter queue for failed saves
   - ✅ Add automated recovery tests

### Code Changes Required

- `internal/shell/storage/postgres_repository.go` - Add connection pool config
- `internal/shell/storage/retry.go` (new) - Retry logic
- `internal/shell/storage/health.go` (new) - Health monitoring
- `internal/shell/executor/job_executor.go` - Retry saves
- `cmd/server/main.go` - Wait for DB on startup
- `migrations/000X_add_external_job_id.up.sql` - Safe migration

This ensures resumable polling survives database restarts, upgrades, and failovers.
