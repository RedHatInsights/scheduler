# Resumable Polling with Database Maintenance

## Overview

Adding resumable polling introduces new database maintenance concerns:
1. **Growing job_runs table** - Every execution creates a row
2. **Orphaned external jobs** - External service has completed jobs we never polled
3. **Stuck "running" jobs** - Jobs that will never complete
4. **Query performance** - Finding resumable jobs at scale
5. **Storage costs** - Historical data retention

## Database Growth Analysis

### Without Maintenance

```
Assumptions:
- 100 jobs scheduled
- Each job runs 24 times/day (hourly)
- 365 days/year

Annual growth:
100 jobs × 24 runs/day × 365 days = 876,000 job_runs rows/year

With failures (assume 5% failure rate):
876,000 × 1.05 = 919,800 rows/year

5-year projection:
4,599,000 rows (4.6M rows)

Row size estimate:
- Base row: ~200 bytes
- Result JSON: ~500 bytes (exports with metadata)
- Total: ~700 bytes/row

Storage: 4.6M × 700 bytes = 3.2 GB

With indexes: ~5 GB total
```

**Conclusion**: Not catastrophic, but needs management.

### With External Job ID (Option 1)

```
Additional columns:
- external_job_id: 36 bytes (UUID)
- external_service: 10 bytes (varchar)
- poll_started_at: 8 bytes (timestamp)

Additional per row: ~60 bytes
Total per row: ~760 bytes

5-year storage: ~3.5 GB data + ~5.5 GB with indexes = ~9 GB
```

**Impact**: 80% increase in storage vs. no external job tracking.

### With Redis (Option 2)

```
Redis memory usage (transient):
- Average concurrent jobs: 50
- State size per job: 300 bytes
- Total: 50 × 300 = 15 KB

Redis is negligible, but PostgreSQL still has same growth.
```

## Maintenance Strategy

### 1. Retention Policy

Define how long to keep job run records:

```go
// config.go
type RetentionConfig struct {
    CompletedJobRuns time.Duration // How long to keep successful runs
    FailedJobRuns    time.Duration // How long to keep failed runs
    OrphanedJobRuns  time.Duration // How long before marking orphaned
}

// Recommended defaults
var DefaultRetentionConfig = RetentionConfig{
    CompletedJobRuns: 30 * 24 * time.Hour,  // 30 days
    FailedJobRuns:    90 * 24 * time.Hour,  // 90 days (longer for debugging)
    OrphanedJobRuns:  2 * time.Hour,        // 2 hours (aggressive cleanup)
}
```

### 2. Cleanup Queries

```sql
-- 1. Delete old completed job runs (after 30 days)
DELETE FROM job_runs
WHERE status = 'completed'
  AND end_time < NOW() - INTERVAL '30 days';

-- 2. Delete old failed job runs (after 90 days)
DELETE FROM job_runs
WHERE status = 'failed'
  AND end_time < NOW() - INTERVAL '90 days';

-- 3. Mark truly orphaned runs as failed (older than 2 hours, still "running")
UPDATE job_runs
SET status = 'failed',
    error_message = 'Execution abandoned - exceeded maximum duration',
    end_time = NOW()
WHERE status = 'running'
  AND start_time < NOW() - INTERVAL '2 hours';

-- 4. Clean up runs with no external job ID (crashed before creation)
UPDATE job_runs
SET status = 'failed',
    error_message = 'Lost before external job creation',
    end_time = NOW()
WHERE status = 'running'
  AND external_job_id IS NULL
  AND start_time < NOW() - INTERVAL '15 minutes';
```

### 3. Maintenance Service

```go
package maintenance

import (
    "context"
    "log/slog"
    "time"

    "insights-scheduler/internal/config"
    "insights-scheduler/internal/core/usecases"
)

type JobRunMaintenance struct {
    runRepo  usecases.JobRunRepository
    config   config.RetentionConfig
    logger   *slog.Logger
    stopCh   chan struct{}
}

func NewJobRunMaintenance(
    runRepo usecases.JobRunRepository,
    config config.RetentionConfig,
    logger *slog.Logger,
) *JobRunMaintenance {
    return &JobRunMaintenance{
        runRepo: runRepo,
        config:  config,
        logger:  logger,
        stopCh:  make(chan struct{}),
    }
}

// Start runs maintenance tasks on a schedule
func (m *JobRunMaintenance) Start() {
    // Run immediately on startup
    m.runMaintenance()

    // Then run every hour
    ticker := time.NewTicker(1 * time.Hour)
    go func() {
        for {
            select {
            case <-ticker.C:
                m.runMaintenance()
            case <-m.stopCh:
                ticker.Stop()
                return
            }
        }
    }()
}

func (m *JobRunMaintenance) Stop() {
    close(m.stopCh)
}

func (m *JobRunMaintenance) runMaintenance() {
    ctx := context.Background()
    
    m.logger.Info("Starting job run maintenance")
    
    // 1. Clean up orphaned runs
    orphanedCount, err := m.cleanupOrphanedRuns(ctx)
    if err != nil {
        m.logger.Error("Failed to cleanup orphaned runs", slog.Any("error", err))
    } else {
        m.logger.Info("Cleaned up orphaned runs", slog.Int("count", orphanedCount))
    }
    
    // 2. Clean up runs with no external job ID
    noExternalCount, err := m.cleanupNoExternalJobRuns(ctx)
    if err != nil {
        m.logger.Error("Failed to cleanup no-external-job runs", slog.Any("error", err))
    } else {
        m.logger.Info("Cleaned up no-external-job runs", slog.Int("count", noExternalCount))
    }
    
    // 3. Delete old completed runs
    completedCount, err := m.deleteOldCompletedRuns(ctx)
    if err != nil {
        m.logger.Error("Failed to delete old completed runs", slog.Any("error", err))
    } else {
        m.logger.Info("Deleted old completed runs", slog.Int("count", completedCount))
    }
    
    // 4. Delete old failed runs
    failedCount, err := m.deleteOldFailedRuns(ctx)
    if err != nil {
        m.logger.Error("Failed to delete old failed runs", slog.Any("error", err))
    } else {
        m.logger.Info("Deleted old failed runs", slog.Int("count", failedCount))
    }
    
    m.logger.Info("Job run maintenance completed",
        slog.Int("orphaned", orphanedCount),
        slog.Int("no_external", noExternalCount),
        slog.Int("completed", completedCount),
        slog.Int("failed", failedCount))
}

func (m *JobRunMaintenance) cleanupOrphanedRuns(ctx context.Context) (int, error) {
    cutoff := time.Now().UTC().Add(-m.config.OrphanedJobRuns)
    
    // Find running jobs older than cutoff
    runs, err := m.runRepo.FindByStatusAndOlderThan(ctx, domain.RunStatusRunning, cutoff)
    if err != nil {
        return 0, err
    }
    
    count := 0
    for _, run := range runs {
        m.logger.Warn("Marking orphaned run as failed",
            slog.String("run_id", run.ID),
            slog.String("job_id", run.JobID),
            slog.Time("started", run.StartTime),
            slog.Duration("age", time.Since(run.StartTime)))
        
        run = run.WithFailed("Execution abandoned - exceeded maximum duration")
        if err := m.runRepo.Save(run); err != nil {
            m.logger.Error("Failed to update orphaned run", slog.Any("error", err))
            continue
        }
        count++
    }
    
    return count, nil
}

func (m *JobRunMaintenance) cleanupNoExternalJobRuns(ctx context.Context) (int, error) {
    // Jobs running for more than 15 minutes without external job ID
    // are definitely stuck (should have been created immediately)
    cutoff := time.Now().UTC().Add(-15 * time.Minute)
    
    runs, err := m.runRepo.FindRunningWithoutExternalJob(ctx, cutoff)
    if err != nil {
        return 0, err
    }
    
    count := 0
    for _, run := range runs {
        m.logger.Warn("Marking no-external-job run as failed",
            slog.String("run_id", run.ID),
            slog.String("job_id", run.JobID))
        
        run = run.WithFailed("Lost before external job creation")
        if err := m.runRepo.Save(run); err != nil {
            m.logger.Error("Failed to update no-external-job run", slog.Any("error", err))
            continue
        }
        count++
    }
    
    return count, nil
}

func (m *JobRunMaintenance) deleteOldCompletedRuns(ctx context.Context) (int, error) {
    cutoff := time.Now().UTC().Add(-m.config.CompletedJobRuns)
    return m.runRepo.DeleteCompletedBefore(ctx, cutoff)
}

func (m *JobRunMaintenance) deleteOldFailedRuns(ctx context.Context) (int, error) {
    cutoff := time.Now().UTC().Add(-m.config.FailedJobRuns)
    return m.runRepo.DeleteFailedBefore(ctx, cutoff)
}
```

### 4. Repository Methods

```go
// Add to JobRunRepository interface
type JobRunRepository interface {
    // ... existing methods ...
    
    // Maintenance methods
    FindByStatusAndOlderThan(ctx context.Context, status JobRunStatus, cutoff time.Time) ([]JobRun, error)
    FindRunningWithoutExternalJob(ctx context.Context, cutoff time.Time) ([]JobRun, error)
    DeleteCompletedBefore(ctx context.Context, cutoff time.Time) (int, error)
    DeleteFailedBefore(ctx context.Context, cutoff time.Time) (int, error)
}

// PostgreSQL implementation
func (r *PostgresJobRunRepository) FindByStatusAndOlderThan(
    ctx context.Context,
    status domain.JobRunStatus,
    cutoff time.Time,
) ([]domain.JobRun, error) {
    query := `
        SELECT id, job_id, status, start_time, end_time, error_message, 
               result_type, result, external_job_id, external_service, poll_started_at
        FROM job_runs
        WHERE status = $1
          AND start_time < $2
        ORDER BY start_time ASC
    `
    
    rows, err := r.db.QueryContext(ctx, query, status, cutoff)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    
    var runs []domain.JobRun
    for rows.Next() {
        var run domain.JobRun
        // ... scan ...
        runs = append(runs, run)
    }
    
    return runs, nil
}

func (r *PostgresJobRunRepository) FindRunningWithoutExternalJob(
    ctx context.Context,
    cutoff time.Time,
) ([]domain.JobRun, error) {
    query := `
        SELECT id, job_id, status, start_time, end_time, error_message, 
               result_type, result, external_job_id, external_service, poll_started_at
        FROM job_runs
        WHERE status = 'running'
          AND external_job_id IS NULL
          AND start_time < $1
        ORDER BY start_time ASC
    `
    
    rows, err := r.db.QueryContext(ctx, query, cutoff)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    
    var runs []domain.JobRun
    for rows.Next() {
        var run domain.JobRun
        // ... scan ...
        runs = append(runs, run)
    }
    
    return runs, nil
}

func (r *PostgresJobRunRepository) DeleteCompletedBefore(
    ctx context.Context,
    cutoff time.Time,
) (int, error) {
    query := `
        DELETE FROM job_runs
        WHERE status = 'completed'
          AND end_time < $1
    `
    
    result, err := r.db.ExecContext(ctx, query, cutoff)
    if err != nil {
        return 0, err
    }
    
    count, _ := result.RowsAffected()
    return int(count), nil
}

func (r *PostgresJobRunRepository) DeleteFailedBefore(
    ctx context.Context,
    cutoff time.Time,
) (int, error) {
    query := `
        DELETE FROM job_runs
        WHERE status = 'failed'
          AND end_time < $1
    `
    
    result, err := r.db.ExecContext(ctx, query, cutoff)
    if err != nil {
        return 0, err
    }
    
    count, _ := result.RowsAffected()
    return int(count), nil
}
```

### 5. Performance Indexes

```sql
-- Index for recovery queries (startup)
CREATE INDEX idx_job_runs_recovery ON job_runs(status, poll_started_at)
WHERE status = 'running' AND external_job_id IS NOT NULL;

-- Index for orphaned cleanup
CREATE INDEX idx_job_runs_orphaned ON job_runs(status, start_time)
WHERE status = 'running';

-- Index for completed cleanup (delete old records)
CREATE INDEX idx_job_runs_completed_cleanup ON job_runs(status, end_time)
WHERE status = 'completed';

-- Index for failed cleanup
CREATE INDEX idx_job_runs_failed_cleanup ON job_runs(status, end_time)
WHERE status = 'failed';

-- Composite index for no-external-job cleanup
CREATE INDEX idx_job_runs_no_external ON job_runs(status, external_job_id, start_time)
WHERE status = 'running' AND external_job_id IS NULL;
```

**Index sizes**: Each index ~100-200 MB at 1M rows.

### 6. Partitioning Strategy (Optional, for High Volume)

If you exceed 10M rows, consider table partitioning:

```sql
-- Partition by month (range partitioning on start_time)
CREATE TABLE job_runs (
    id TEXT NOT NULL,
    job_id TEXT NOT NULL,
    status TEXT NOT NULL,
    start_time TIMESTAMP NOT NULL,
    -- ... other columns ...
    PRIMARY KEY (id, start_time)
) PARTITION BY RANGE (start_time);

-- Create partitions
CREATE TABLE job_runs_2026_07 PARTITION OF job_runs
    FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');

CREATE TABLE job_runs_2026_08 PARTITION OF job_runs
    FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');

-- Drop old partitions instead of DELETE
DROP TABLE job_runs_2024_01;  -- Much faster than DELETE
```

**Benefits**:
- Cleanup = `DROP TABLE` (instant) instead of `DELETE` (slow)
- Query performance on recent data
- Archival = detach partition, backup, drop

**When to use**: > 10M rows or > 100 GB table size.

### 7. Archival Strategy

For compliance or analytics, archive old data before deletion:

```sql
-- Archive table (separate database or schema)
CREATE TABLE job_runs_archive (
    LIKE job_runs INCLUDING ALL
);

-- Archive process (before deletion)
INSERT INTO job_runs_archive
SELECT * FROM job_runs
WHERE status = 'completed'
  AND end_time < NOW() - INTERVAL '30 days';

-- Then delete from main table
DELETE FROM job_runs
WHERE status = 'completed'
  AND end_time < NOW() - INTERVAL '30 days';
```

Or use compressed columnar storage (e.g., parquet files):

```go
func ArchiveOldJobRuns(cutoff time.Time) error {
    // 1. Export to parquet
    runs, _ := repo.FindCompletedBefore(cutoff)
    parquet.Write("job_runs_2026_06.parquet", runs)
    
    // 2. Upload to S3
    s3.Upload("archives/job_runs_2026_06.parquet", file)
    
    // 3. Delete from DB
    repo.DeleteCompletedBefore(cutoff)
}
```

### 8. Monitoring & Alerting

```go
// Prometheus metrics
var (
    OrphanedJobRuns = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "job_runs_orphaned_total",
        Help: "Number of orphaned job runs (running but no activity)",
    })
    
    TableSize = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "job_runs_table_size_bytes",
        Help: "Size of job_runs table in bytes",
    })
    
    MaintenanceDeleted = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "job_runs_maintenance_deleted_total",
        Help: "Number of job runs deleted by maintenance",
    }, []string{"status"})
)

// Collect metrics during maintenance
func (m *JobRunMaintenance) collectMetrics(ctx context.Context) {
    // Count orphaned
    orphaned, _ := m.runRepo.CountByStatusAndOlderThan(ctx, domain.RunStatusRunning, 2*time.Hour)
    OrphanedJobRuns.Set(float64(orphaned))
    
    // Table size
    size, _ := m.runRepo.GetTableSize(ctx)
    TableSize.Set(float64(size))
}
```

**Alerts**:
```yaml
# Prometheus alerts
groups:
  - name: job_runs_maintenance
    rules:
      - alert: TooManyOrphanedJobRuns
        expr: job_runs_orphaned_total > 100
        for: 5m
        annotations:
          summary: "Too many orphaned job runs detected"
          
      - alert: JobRunsTableTooLarge
        expr: job_runs_table_size_bytes > 10e9  # 10 GB
        for: 1h
        annotations:
          summary: "job_runs table exceeding 10 GB"
```

## Redis Maintenance (if using Option 2 or 3)

### TTL-Based Cleanup

```go
// Set TTL on all polling state keys
func SavePollingState(state RedisPollingState) error {
    key := fmt.Sprintf("polling:state:%s", state.JobRunID)
    
    // Set with 2-hour TTL (auto-cleanup)
    return redis.Set(key, state, 2*time.Hour).Err()
}
```

**Advantage**: Automatic cleanup, no manual maintenance needed.

### Manual Cleanup (if not using TTL)

```go
func CleanupExpiredPollingStates() error {
    // Find all polling:state:* keys
    keys, _ := redis.Keys("polling:state:*").Result()
    
    now := time.Now()
    for _, key := range keys {
        state, _ := redis.Get(key).Result()
        
        // Parse state
        var ps RedisPollingState
        json.Unmarshal([]byte(state), &ps)
        
        // If older than 2 hours, delete
        if now.Sub(ps.StartedAt) > 2*time.Hour {
            redis.Del(key)
        }
    }
}
```

### Memory Management

```
# Redis config
maxmemory 1gb
maxmemory-policy allkeys-lru  # Evict least recently used keys

# Or use volatile-lru to only evict keys with TTL
maxmemory-policy volatile-lru
```

## Startup Integration

```go
// cmd/server/main.go
func main() {
    // ... existing setup ...
    
    // Create maintenance service
    retentionConfig := config.RetentionConfig{
        CompletedJobRuns: 30 * 24 * time.Hour,
        FailedJobRuns:    90 * 24 * time.Hour,
        OrphanedJobRuns:  2 * time.Hour,
    }
    
    maintenance := maintenance.NewJobRunMaintenance(
        jobRunRepo,
        retentionConfig,
        logger,
    )
    
    // Start maintenance (runs every hour)
    maintenance.Start()
    defer maintenance.Stop()
    
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
    if err := recovery.RecoverInFlightPolls(ctx); err != nil {
        logger.Error("Failed to recover in-flight polls", slog.Any("error", err))
    }
    
    // ... continue with scheduler startup ...
}
```

## Maintenance Schedule

| Task | Frequency | Duration | Impact |
|------|-----------|----------|--------|
| Orphaned run cleanup | Every 1 hour | < 1 second | Low |
| No-external-job cleanup | Every 1 hour | < 1 second | Low |
| Delete old completed | Every 1 hour | 1-5 seconds | Low |
| Delete old failed | Every 1 hour | 1-5 seconds | Low |
| Vacuum analyze | Weekly | 1-10 minutes | Medium |
| Archive old data | Monthly | 5-30 minutes | Low |

## Operational Runbook

### Daily Operations

```bash
# Check for orphaned runs
psql -c "SELECT COUNT(*) FROM job_runs WHERE status='running' AND start_time < NOW() - INTERVAL '2 hours';"

# Check table size
psql -c "SELECT pg_size_pretty(pg_total_relation_size('job_runs'));"

# Check oldest record
psql -c "SELECT MIN(start_time) FROM job_runs;"
```

### Monthly Tasks

```bash
# Archive old data
./scripts/archive-job-runs.sh 2026-06

# Analyze table
psql -c "ANALYZE job_runs;"

# Check index bloat
psql -c "SELECT schemaname, tablename, pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) 
         FROM pg_tables WHERE tablename = 'job_runs';"
```

### Troubleshooting

**Slow maintenance queries:**
```sql
-- Check if indexes are being used
EXPLAIN ANALYZE
DELETE FROM job_runs
WHERE status = 'completed'
  AND end_time < NOW() - INTERVAL '30 days';

-- If seq scan, add missing index
CREATE INDEX idx_job_runs_end_time ON job_runs(end_time) WHERE status = 'completed';
```

**Too many orphaned runs:**
```sql
-- Investigate pattern
SELECT job_id, COUNT(*) 
FROM job_runs 
WHERE status = 'running' 
  AND start_time < NOW() - INTERVAL '2 hours'
GROUP BY job_id
ORDER BY COUNT(*) DESC;

-- Check if specific jobs are failing
```

## Cost Analysis

### Storage Costs (5-year projection)

**PostgreSQL**:
- Data: 3.5 GB
- Indexes: 2 GB
- Total: 5.5 GB
- Cost (AWS RDS): ~$1-2/month

**With archival to S3**:
- Hot data (30 days): 300 MB
- S3 archive: 3 GB compressed
- Cost: ~$0.50/month

**Savings**: 75% cost reduction with archival.

### Query Performance

**Without maintenance** (4.6M rows):
```
SELECT * FROM job_runs WHERE status='running';
Duration: 500-1000ms (seq scan)
```

**With maintenance + indexes** (50K rows):
```
SELECT * FROM job_runs WHERE status='running';
Duration: 5-10ms (index scan)
```

**Performance gain**: 100x faster queries.

## Summary

### Recommended Maintenance Strategy

1. **Immediate** (Day 1):
   - Add orphaned run cleanup (hourly)
   - Add indexes for recovery queries
   
2. **Short-term** (Week 1):
   - Implement retention policy (30/90 days)
   - Add monitoring metrics
   
3. **Medium-term** (Month 1):
   - Add archival process
   - Tune retention based on actual usage
   
4. **Long-term** (as needed):
   - Consider partitioning at 10M+ rows
   - Optimize based on metrics

### Key Metrics to Monitor

- Orphaned run count
- Table size growth rate
- Maintenance duration
- Query performance (p95, p99)
- Deleted rows per maintenance cycle

### Estimated Maintenance Overhead

- Development: 1-2 days
- Runtime CPU: < 0.1% (hourly cleanup)
- Storage savings: 75% with retention policy
- Query performance: 100x improvement

This design ensures your resumable polling system remains performant and cost-effective long-term.
