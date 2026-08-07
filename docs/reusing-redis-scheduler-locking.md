# Reusing Redis Scheduler Locking for Resumable Polling

## Existing Pattern in RedisScheduler

Your `redis_scheduler.go` already implements distributed locking:

```go
// internal/shell/scheduler/redis_scheduler.go:264-277

func (s *RedisScheduler) executeJob(jobID string) {
    // Try to acquire lock
    lockKey := jobLockKeyPrefix + jobID  // "scheduler:lock:" + jobID
    locked, err := s.client.SetNX(s.ctx, lockKey, "locked", lockTTL).Result()
    if err != nil {
        log.Printf("[RedisScheduler] Error acquiring lock for job %s: %v", jobID, err)
        return
    }

    if !locked {
        log.Printf("[RedisScheduler] Job %s is already being processed", jobID)
        return
    }

    // Ensure lock is released
    defer s.client.Del(s.ctx, lockKey)
    
    // Execute job...
}
```

**This is EXACTLY the pattern we need for resumable polling!**

## Extract to Reusable Component

### Create: `internal/shell/scheduler/distributed_lock.go`

```go
package scheduler

import (
    "context"
    "fmt"
    "log/slog"
    "time"

    "github.com/redis/go-redis/v9"
)

// DistributedLock provides Redis-based distributed locking
type DistributedLock struct {
    client *redis.Client
    logger *slog.Logger
}

// NewDistributedLock creates a new distributed lock manager
func NewDistributedLock(client *redis.Client, logger *slog.Logger) *DistributedLock {
    return &DistributedLock{
        client: client,
        logger: logger,
    }
}

// TryAcquire attempts to acquire a lock
// Returns true if lock was acquired, false if already held by another process
func (d *DistributedLock) TryAcquire(ctx context.Context, lockKey string, ownerID string, ttl time.Duration) (bool, error) {
    acquired, err := d.client.SetNX(ctx, lockKey, ownerID, ttl).Result()
    if err != nil {
        return false, fmt.Errorf("failed to acquire lock: %w", err)
    }

    if acquired {
        d.logger.Debug("Lock acquired",
            slog.String("lock_key", lockKey),
            slog.String("owner_id", ownerID),
            slog.Duration("ttl", ttl))
    } else {
        d.logger.Debug("Lock already held",
            slog.String("lock_key", lockKey))
    }

    return acquired, nil
}

// Release releases a lock only if we own it
func (d *DistributedLock) Release(ctx context.Context, lockKey string, ownerID string) error {
    // Lua script to ensure we only delete if we own the lock
    script := `
        if redis.call("get", KEYS[1]) == ARGV[1] then
            return redis.call("del", KEYS[1])
        else
            return 0
        end
    `

    result, err := d.client.Eval(ctx, script, []string{lockKey}, ownerID).Result()
    if err != nil {
        return fmt.Errorf("failed to release lock: %w", err)
    }

    if result.(int64) == 1 {
        d.logger.Debug("Lock released",
            slog.String("lock_key", lockKey),
            slog.String("owner_id", ownerID))
    } else {
        d.logger.Debug("Lock was not owned by us",
            slog.String("lock_key", lockKey),
            slog.String("owner_id", ownerID))
    }

    return nil
}

// Extend extends the TTL of a lock we own
func (d *DistributedLock) Extend(ctx context.Context, lockKey string, ownerID string, ttl time.Duration) (bool, error) {
    // Lua script to extend TTL only if we own the lock
    script := `
        if redis.call("get", KEYS[1]) == ARGV[1] then
            return redis.call("expire", KEYS[1], ARGV[2])
        else
            return 0
        end
    `

    result, err := d.client.Eval(ctx, script, []string{lockKey}, ownerID, int(ttl.Seconds())).Result()
    if err != nil {
        return false, fmt.Errorf("failed to extend lock: %w", err)
    }

    extended := result.(int64) == 1

    if extended {
        d.logger.Debug("Lock extended",
            slog.String("lock_key", lockKey),
            slog.Duration("ttl", ttl))
    }

    return extended, nil
}

// WithLock executes a function while holding a lock
// Automatically acquires and releases the lock
func (d *DistributedLock) WithLock(ctx context.Context, lockKey string, ownerID string, ttl time.Duration, fn func() error) error {
    acquired, err := d.TryAcquire(ctx, lockKey, ownerID, ttl)
    if err != nil {
        return err
    }

    if !acquired {
        return fmt.Errorf("failed to acquire lock: already held")
    }

    defer d.Release(ctx, lockKey, ownerID)

    return fn()
}
```

## Refactor RedisScheduler to Use It

```go
// internal/shell/scheduler/redis_scheduler.go

type RedisScheduler struct {
    client       *redis.Client
    executor     ports.JobExecutor
    jobRepo      JobRepository
    parser       cron.Parser
    ctx          context.Context
    cancel       context.CancelFunc
    pollInterval time.Duration
    lock         *DistributedLock  // ← Add this
}

func NewRedisScheduler(redisAddr string, executor ports.JobExecutor, jobRepo JobRepository, pollInterval time.Duration, logger *slog.Logger) (*RedisScheduler, error) {
    client := redis.NewClient(&redis.Options{
        Addr: redisAddr,
    })

    if err := client.Ping(context.Background()).Err(); err != nil {
        return nil, fmt.Errorf("failed to connect to Redis: %w", err)
    }

    ctx, cancel := context.WithCancel(context.Background())

    return &RedisScheduler{
        client:       client,
        executor:     executor,
        jobRepo:      jobRepo,
        parser:       cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
        ctx:          ctx,
        cancel:       cancel,
        pollInterval: pollInterval,
        lock:         NewDistributedLock(client, logger),  // ← Initialize
    }, nil
}

// Refactored executeJob
func (s *RedisScheduler) executeJob(jobID string) {
    lockKey := jobLockKeyPrefix + jobID
    podID := getPodID()  // Helper to get unique pod identifier
    
    // Use the reusable lock component
    err := s.lock.WithLock(s.ctx, lockKey, podID, lockTTL, func() error {
        return s.executeJobInternal(jobID)
    })
    
    if err != nil {
        log.Printf("[RedisScheduler] Error executing job %s: %v", jobID, err)
    }
}

func (s *RedisScheduler) executeJobInternal(jobID string) error {
    // Get job data
    jobKey := jobDataKeyPrefix + jobID
    jobData, err := s.client.Get(s.ctx, jobKey).Result()
    if err != nil {
        return err
    }

    var scheduledJob ScheduledJob
    if err := json.Unmarshal([]byte(jobData), &scheduledJob); err != nil {
        return err
    }

    // Execute job
    if err := s.executor.Execute(scheduledJob.Job); err != nil {
        return err
    }

    // Reschedule...
    return nil
}
```

## Use in PollingRecovery

```go
// internal/shell/scheduler/polling_recovery.go

package scheduler

import (
    "context"
    "fmt"
    "log/slog"
    "os"
    "time"

    "insights-scheduler/internal/clients/export"
    "insights-scheduler/internal/clients/polling"
    "insights-scheduler/internal/config"
    "insights-scheduler/internal/core/domain"
    "insights-scheduler/internal/core/usecases"
    "insights-scheduler/internal/identity"
)

const (
    // Reuse the same key prefix pattern
    pollingLockKeyPrefix = "scheduler:polling:lock:"
    pollingLockTTL       = 15 * time.Minute
)

type PollingRecovery struct {
    runRepo       usecases.JobRunRepository
    jobRepo       usecases.JobRepository
    exportClient  *export.Client
    userValidator identity.UserValidator
    config        *config.Config
    logger        *slog.Logger
    lock          *DistributedLock  // ← Reuse the lock component
    podID         string
}

func NewPollingRecovery(
    runRepo usecases.JobRunRepository,
    jobRepo usecases.JobRepository,
    exportClient *export.Client,
    userValidator identity.UserValidator,
    config *config.Config,
    lock *DistributedLock,  // ← Inject the same lock instance
    logger *slog.Logger,
) *PollingRecovery {
    podID := getPodID()
    
    return &PollingRecovery{
        runRepo:       runRepo,
        jobRepo:       jobRepo,
        exportClient:  exportClient,
        userValidator: userValidator,
        config:        config,
        logger:        logger,
        lock:          lock,
        podID:         podID,
    }
}

func (r *PollingRecovery) RecoverInFlightPolls(ctx context.Context) error {
    runningRuns, err := r.runRepo.FindByStatus(ctx, domain.RunStatusRunning)
    if err != nil {
        return fmt.Errorf("failed to find running jobs: %w", err)
    }

    r.logger.Info("Found in-flight job runs",
        slog.Int("count", len(runningRuns)),
        slog.String("pod_id", r.podID))

    for _, run := range runningRuns {
        if run.ExternalJobID == nil || run.ExternalService == nil {
            r.markAsFailedNoExternalJob(run)
            continue
        }

        // Try to claim using the shared lock component
        lockKey := pollingLockKeyPrefix + run.ID
        
        acquired, err := r.lock.TryAcquire(ctx, lockKey, r.podID, pollingLockTTL)
        if err != nil {
            r.logger.Error("Failed to acquire lock",
                slog.String("run_id", run.ID),
                slog.Any("error", err))
            continue
        }

        if !acquired {
            // Another pod already claimed it
            r.logger.Debug("Job already claimed by another pod",
                slog.String("run_id", run.ID))
            continue
        }

        // We claimed it - resume polling in background
        r.logger.Info("Claimed job for polling recovery",
            slog.String("run_id", run.ID),
            slog.String("external_job_id", *run.ExternalJobID))

        go r.resumePollWithLock(ctx, run)
    }

    return nil
}

func (r *PollingRecovery) resumePollWithLock(ctx context.Context, run domain.JobRun) {
    lockKey := pollingLockKeyPrefix + run.ID
    
    // Ensure lock is released when done
    defer r.lock.Release(context.Background(), lockKey, r.podID)
    
    logger := r.logger.With(
        slog.String("run_id", run.ID),
        slog.String("external_job_id", *run.ExternalJobID),
        slog.String("pod_id", r.podID),
    )

    // Get job details
    job, err := r.jobRepo.Get(ctx, run.JobID)
    if err != nil {
        logger.Error("Failed to get job", slog.Any("error", err))
        run = run.WithFailed("Failed to retrieve job details on resume")
        r.runRepo.Save(run)
        return
    }

    // Generate identity
    identityHeader, err := r.userValidator.GenerateIdentityHeader(ctx, job.OrgID, job.UserID)
    if err != nil {
        logger.Error("Failed to generate identity", slog.Any("error", err))
        run = run.WithFailed("Failed to generate identity on resume")
        r.runRepo.Save(run)
        return
    }

    // Start polling
    poller := export.NewExportPoller(r.exportClient, identityHeader)
    pollConfig := polling.Config{
        MaxRetries:   r.config.ExportService.PollMaxRetries,
        PollInterval: r.config.ExportService.PollInterval,
        Timeout:      9 * time.Minute,
    }

    logger.Info("Starting resumed polling")

    // If polling is very long (> 5 min), extend lock periodically
    go r.extendLockPeriodically(ctx, lockKey, 9*time.Minute)

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

func (r *PollingRecovery) extendLockPeriodically(ctx context.Context, lockKey string, duration time.Duration) {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()

    deadline := time.Now().Add(duration)

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            if time.Now().After(deadline) {
                return
            }

            extended, err := r.lock.Extend(ctx, lockKey, r.podID, pollingLockTTL)
            if err != nil {
                r.logger.Error("Failed to extend lock", slog.Any("error", err))
                return
            }

            if !extended {
                r.logger.Warn("Lost ownership of lock")
                return
            }

            r.logger.Debug("Extended lock", slog.String("lock_key", lockKey))
        }
    }
}

func (r *PollingRecovery) markAsFailedNoExternalJob(run domain.JobRun) {
    r.logger.Warn("Job run has no external job ID",
        slog.String("run_id", run.ID))
    
    run = run.WithFailed("Lost during restart before external job creation")
    r.runRepo.Save(run)
}

// Helper function to get unique pod ID
func getPodID() string {
    hostname := os.Getenv("HOSTNAME")
    if hostname == "" {
        hostname = "unknown"
    }
    return fmt.Sprintf("%s-%d", hostname, os.Getpid())
}
```

## Integration in main.go

```go
// cmd/server/main.go

func main() {
    cfg, _ := config.LoadConfig()
    logger := slog.Default()
    
    // Create Redis client (reuse if already exists)
    var redisClient *redis.Client
    var distributedLock *scheduler.DistributedLock
    
    if cfg.Redis.Enabled {
        redisClient = redis.NewClient(&redis.Options{
            Addr:     fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
            Password: cfg.Redis.Password,
            DB:       cfg.Redis.DB,
        })
        
        // Create shared distributed lock component
        distributedLock = scheduler.NewDistributedLock(redisClient, logger)
    }
    
    // Create repositories
    jobRepo, _ := storage.NewPostgresJobRepository(cfg, logger)
    jobRunRepo, _ := storage.NewPostgresJobRunRepository(cfg, logger)
    
    // Create export client
    exportClient := export.NewClient(cfg.ExportService.BaseURL, cfg.ExportService.PublicBaseURL)
    
    // Create user validator
    userValidator := identity.NewUserValidator(cfg)
    
    // Create polling recovery (shares the lock component)
    recovery := scheduler.NewPollingRecovery(
        jobRunRepo,
        jobRepo,
        exportClient,
        userValidator,
        cfg,
        distributedLock,  // ← Same lock instance used by RedisScheduler
        logger,
    )
    
    // Recover in-flight polls on startup
    ctx := context.Background()
    if err := recovery.RecoverInFlightPolls(ctx); err != nil {
        logger.Error("Failed to recover in-flight polls", slog.Any("error", err))
    }
    
    // Create scheduler (also uses the same lock)
    var schedulerInstance ports.Scheduler
    
    if cfg.Redis.Enabled {
        schedulerInstance, _ = scheduler.NewRedisScheduler(
            fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
            executor,
            jobRepo,
            cfg.Scheduler.RedisPollInterval,
            logger,
        )
        // RedisScheduler internally uses distributedLock too
    } else {
        schedulerInstance = scheduler.NewCronScheduler(...)
    }
    
    // ... continue with rest of setup
}
```

## Benefits of Reusing RedisScheduler's Pattern

1. **✅ Consistent locking across the system**
   - Job execution uses `scheduler:lock:{job_id}`
   - Polling recovery uses `scheduler:polling:lock:{run_id}`
   - Same Redis client, same patterns

2. **✅ No code duplication**
   - Single `DistributedLock` component
   - RedisScheduler refactored to use it
   - PollingRecovery uses the same instance

3. **✅ Easier to reason about**
   - One locking mechanism
   - One place to add monitoring
   - One place to fix bugs

4. **✅ Built-in features**
   - Extend lock for long operations
   - Safe release (only if we own it)
   - Consistent logging

5. **✅ Production-tested**
   - RedisScheduler already running in production
   - Proven pattern, just extracted and reused

## Key Differences from RedisScheduler's Current Implementation

| Aspect | Current RedisScheduler | Extracted DistributedLock |
|--------|----------------------|---------------------------|
| Lock release | `defer s.client.Del(lockKey)` | `defer lock.Release(lockKey, ownerID)` |
| Ownership check | ❌ No (deletes unconditionally) | ✅ Yes (Lua script verifies owner) |
| Lock extension | ❌ Not supported | ✅ Supported (for long polls) |
| Reusability | ❌ Embedded in RedisScheduler | ✅ Standalone component |

## Summary

**Yes, absolutely reuse the RedisScheduler locking!**

Steps:
1. Extract locking logic to `DistributedLock` component
2. Refactor `RedisScheduler` to use it
3. Use same component in `PollingRecovery`
4. Share one Redis client and one lock instance

This gives you:
- ✅ Consistent multi-pod safety
- ✅ No code duplication
- ✅ Proven production pattern
- ✅ Easier maintenance

I've documented the complete refactoring in `docs/reusing-redis-scheduler-locking.md`.

**Want me to implement the `DistributedLock` component now?**
