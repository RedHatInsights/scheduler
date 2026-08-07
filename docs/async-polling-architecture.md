# Async Polling Architecture - Design Analysis

## Proposed Architecture

Instead of blocking the job execution goroutine during polling, use a **dedicated polling worker pool** that runs throughout the process lifetime.

## Current vs Proposed

### Current Design (Synchronous Polling)

```go
// Job executor goroutine
func (e *ExportJobExecutor) Execute(job domain.Job, logger *slog.Logger) {
    // 1. Create export
    createResult, _ := e.exportClient.CreateExport(...)
    
    // 2. Poll (BLOCKS for 5-10 minutes)
    result, _ := polling.Poll(ctx, poller, createResult.ID, config)
    
    // 3. Update job run
    jobRun = jobRun.WithCompleted(result)
    e.runRepo.Save(jobRun)
    
    return // Goroutine exits after ~10 minutes
}
```

**Timeline**:
```
09:00:00 - Goroutine starts
09:00:01 - Create export
09:00:02 - Start polling (blocks)
09:07:00 - Polling completes
09:07:01 - Save result
09:07:02 - Goroutine exits

Goroutine lifetime: 7 minutes
```

### Proposed Design (Async Polling with Worker Pool)

```go
// Global worker pool started at process init
var pollingPool *PollingWorkerPool

func init() {
    pollingPool = NewPollingWorkerPool(10) // 10 workers
    pollingPool.Start()
}

// Job executor goroutine
func (e *ExportJobExecutor) Execute(job domain.Job, logger *slog.Logger) {
    // 1. Create export
    createResult, _ := e.exportClient.CreateExport(...)
    
    // 2. Submit to worker pool (RETURNS IMMEDIATELY)
    pollingPool.SubmitPollRequest(PollRequest{
        Poller:   poller,
        JobID:    createResult.ID,
        Config:   config,
        JobRunID: jobRun.ID,
        Callback: func(result *polling.StatusResponse, err error) {
            // This runs in a different goroutine
            if err != nil {
                jobRun = jobRun.WithFailed(err.Error())
            } else {
                jobRun = jobRun.WithCompleted(result)
            }
            e.runRepo.Save(jobRun)
        },
    })
    
    return // Goroutine exits immediately!
}
```

**Timeline**:
```
Job Executor Goroutine:
09:00:00 - Goroutine starts
09:00:01 - Create export
09:00:02 - Submit to worker pool
09:00:03 - Goroutine exits
Goroutine lifetime: 3 seconds ✅

Polling Worker Goroutine (from pool):
09:00:03 - Pick up poll request from queue
09:00:04 - Start polling
09:07:00 - Polling completes
09:07:01 - Execute callback
09:07:02 - Return to pool
Worker goroutine reused ✅
```

## Implementation Sketch

### Worker Pool Structure

```go
package polling

type PollRequest struct {
    Poller   Poller
    JobID    string
    Config   Config
    JobRunID string
    Callback func(*StatusResponse, error)
}

type PollingWorkerPool struct {
    workers    int
    requestCh  chan PollRequest
    shutdownCh chan struct{}
    wg         sync.WaitGroup
}

func NewPollingWorkerPool(workers int) *PollingWorkerPool {
    return &PollingWorkerPool{
        workers:    workers,
        requestCh:  make(chan PollRequest, 100), // Buffered queue
        shutdownCh: make(chan struct{}),
    }
}

func (p *PollingWorkerPool) Start() {
    for i := 0; i < p.workers; i++ {
        p.wg.Add(1)
        go p.worker(i)
    }
}

func (p *PollingWorkerPool) worker(id int) {
    defer p.wg.Done()
    
    for {
        select {
        case req := <-p.requestCh:
            // Do the polling (blocks for 5-10 min)
            ctx := context.Background()
            result, err := Poll(ctx, req.Poller, req.JobID, req.Config)
            
            // Execute callback
            req.Callback(result, err)
            
        case <-p.shutdownCh:
            return
        }
    }
}

func (p *PollingWorkerPool) SubmitPollRequest(req PollRequest) error {
    select {
    case p.requestCh <- req:
        return nil
    default:
        return fmt.Errorf("worker pool queue full")
    }
}

func (p *PollingWorkerPool) Shutdown(timeout time.Duration) {
    close(p.shutdownCh)
    
    done := make(chan struct{})
    go func() {
        p.wg.Wait()
        close(done)
    }()
    
    select {
    case <-done:
        // Clean shutdown
    case <-time.After(timeout):
        // Timeout - kill in-flight polls
    }
}
```

### Integration with Executor

```go
// In main.go or server startup
func main() {
    // Start worker pool
    pollingPool := polling.NewPollingWorkerPool(20) // 20 concurrent polls
    pollingPool.Start()
    defer pollingPool.Shutdown(30 * time.Second)
    
    // Create executors with access to pool
    exportExecutor := executor.NewExportJobExecutor(cfg, validator, notifier, pollingPool)
    
    // ... rest of startup
}

// Modified ExportJobExecutor
type ExportJobExecutor struct {
    exportClient  *export.Client
    notifier      JobCompletionNotifier
    userValidator identity.UserValidator
    config        *config.Config
    pollingPool   *polling.PollingWorkerPool  // NEW
    runRepo       usecases.JobRunRepository   // NEW - needed for callback
}

func (e *ExportJobExecutor) Execute(job domain.Job, logger *slog.Logger) {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    // Generate identity
    identityHeader, _ := e.userValidator.GenerateIdentityHeader(ctx, job.OrgID, job.UserID)
    
    // Marshal payload
    var req export.ExportRequest
    // ... unmarshal ...
    
    // Create export
    createResult, err := e.exportClient.CreateExport(ctx, req, identityHeader)
    if err != nil {
        return nil, domain.ResultTypeExport, err
    }
    
    // Create job run record
    jobRun := domain.NewJobRun(job.ID)
    e.runRepo.Save(jobRun)
    
    logger.Info("Export created, submitting to polling worker pool",
        slog.String("export_id", createResult.ID))
    
    // Submit async poll request
    poller := export.NewExportPoller(e.exportClient, identityHeader)
    pollConfig := polling.Config{
        MaxRetries:   e.config.ExportService.PollMaxRetries,
        PollInterval: e.config.ExportService.PollInterval,
        Timeout:      9 * time.Minute,
    }
    
    err = e.pollingPool.SubmitPollRequest(polling.PollRequest{
        Poller:   poller,
        JobID:    createResult.ID,
        Config:   pollConfig,
        JobRunID: jobRun.ID,
        Callback: func(result *polling.StatusResponse, err error) {
            e.handlePollCompletion(job, jobRun, createResult, result, err, logger)
        },
    })
    
    if err != nil {
        return nil, domain.ResultTypeExport, fmt.Errorf("failed to submit poll request: %w", err)
    }
    
    // Return immediately - callback will update job run later
    // Return the export ID so caller knows what was created
    return domain.ExportResult{ExportID: createResult.ID}, domain.ResultTypeExport, nil
}

func (e *ExportJobExecutor) handlePollCompletion(
    job domain.Job,
    jobRun domain.JobRun,
    createResult *export.ExportStatusResponse,
    pollResult *polling.StatusResponse,
    pollErr error,
    logger *slog.Logger,
) {
    ctx := context.Background()
    
    if pollErr != nil {
        logger.Error("Poll failed", slog.Any("error", pollErr))
        jobRun = jobRun.WithFailed(pollErr.Error())
        e.runRepo.Save(jobRun)
        return
    }
    
    // Send notification
    downloadURL := ""
    if pollResult.Status == polling.StatusComplete {
        downloadURL = e.exportClient.GetExportDownloadURL(createResult.ID)
    }
    
    notification := &ExportCompletionNotification{
        ExportID:    createResult.ID,
        JobID:       job.ID,
        JobName:     job.Name,
        OrgID:       job.OrgID,
        Status:      string(pollResult.Status),
        DownloadURL: downloadURL,
        ErrorMsg:    pollResult.Error,
    }
    
    if err := e.notifier.JobComplete(ctx, notification, logger); err != nil {
        logger.Warn("Failed to send notification", slog.Any("error", err))
    }
    
    // Update job run
    result := domain.ExportResult{
        ExportID: createResult.ID,
    }
    if pollResult.Status == polling.StatusComplete {
        result.URL = downloadURL
    }
    
    jobRun = jobRun.WithCompleted(domain.ResultTypeExport, result)
    e.runRepo.Save(jobRun)
    
    logger.Info("Export polling completed", slog.String("status", string(pollResult.Status)))
}
```

## Trade-offs Analysis

### ✅ Advantages

1. **Resource Efficiency**
   - Job executor goroutines complete in seconds (not minutes)
   - Fixed number of polling goroutines (e.g., 20) regardless of job count
   - Can handle 1000s of jobs with only 20 polling workers
   - Memory: 1000 pending jobs = ~100KB (vs 1-2MB with current design)

2. **Better Concurrency Control**
   - Explicitly control max concurrent polls (worker pool size)
   - Queue automatically handles backpressure
   - Can prioritize poll requests (add priority queue)

3. **Resilience**
   - Worker crashes don't affect job creation
   - Can restart worker pool without restarting scheduler
   - Easier to add retry logic at the pool level

4. **Observability**
   - Clear metrics: queue depth, worker utilization, poll duration
   - Can see "10 workers, 50 queued polls, avg wait time 2 min"

5. **Separation of Concerns**
   - Job execution = create remote job + queue local poll
   - Polling = separate concern handled by dedicated workers

### ⚠️ Disadvantages

1. **Complexity**
   - Callback-based code is harder to follow than linear code
   - State management: need to pass context through callbacks
   - More moving parts: queue, workers, callbacks
   - Harder to debug (spans multiple goroutines)

2. **Testing**
   - Async tests are harder to write
   - Need to handle timing/synchronization in tests
   - Mocking callbacks is awkward

3. **Error Handling**
   - Can't return errors from Execute() - they happen later in callback
   - Harder to distinguish "creation failed" vs "polling failed"
   - Caller of Execute() doesn't know final outcome

4. **Job Run Semantics Change**
   - Currently: JobRun completes when Execute() returns
   - With async: JobRun starts as "running", updates later
   - Need to handle: "what if callback never fires?"

5. **Shutdown Complexity**
   - Need to drain queue gracefully
   - What about in-flight polls? (still need timeout)
   - More complex than current sync model

6. **Loss of Execution Context**
   - Current: logger, metrics, trace all in one goroutine
   - Async: need to thread context through callback
   - Distributed tracing becomes harder

## Detailed Comparison

### Resource Usage

**Current (Sync Polling)**:
```
100 concurrent jobs with 5-minute avg poll time:
- Goroutines: 100 (one per job)
- Memory: ~100-200 KB
- HTTP connections: 100 concurrent
```

**Proposed (Async Polling)**:
```
100 concurrent jobs with 5-minute avg poll time:
- Job executor goroutines: 100 (but exit immediately)
- Polling worker goroutines: 20 (fixed pool)
- Queue length: 80 jobs waiting
- Memory: ~100 KB (goroutines) + ~40 KB (queue)
- HTTP connections: 20 concurrent (vs 100)

Benefit: 80% fewer concurrent HTTP connections
```

### Execution Flow Comparison

**Current**:
```go
// Simple, linear flow
result, err := executor.Execute(job)
if err != nil {
    return err
}
// Done - result is complete
```

**Proposed**:
```go
// Async - result is partial
result, err := executor.Execute(job)
if err != nil {
    return err
}
// NOT done - polling happens later
// Callback will fire sometime in next 5-10 minutes
```

## When to Use Each Approach

### Stick with Current (Sync) If:
- ✅ Job count is moderate (< 500 concurrent jobs)
- ✅ Poll duration is short (< 10 minutes)
- ✅ Code simplicity is priority
- ✅ Immediate error feedback is important
- ✅ Team prefers linear, easy-to-debug code

### Switch to Async If:
- ✅ High concurrency (1000+ concurrent jobs)
- ✅ Long poll duration (30+ minutes)
- ✅ Need fine-grained control over polling resources
- ✅ Want to limit HTTP connections to external services
- ✅ Need to prioritize certain polls over others
- ✅ Team is comfortable with async patterns

## Hybrid Approach

You could combine both:

```go
type PollingMode string

const (
    PollingModeSync  PollingMode = "sync"
    PollingModeAsync PollingMode = "async"
)

type Config struct {
    PollingMode       PollingMode
    AsyncWorkerCount  int
    // ...
}

func (e *ExportJobExecutor) Execute(job domain.Job, logger *slog.Logger) {
    createResult, _ := e.exportClient.CreateExport(...)
    
    switch e.config.PollingMode {
    case PollingModeSync:
        // Current implementation
        result, err := polling.Poll(ctx, poller, createResult.ID, config)
        // ...
        
    case PollingModeAsync:
        // Worker pool implementation
        e.pollingPool.SubmitPollRequest(...)
        // ...
    }
}
```

Start with sync, switch to async if you hit scale issues.

## Implementation Effort

### To Implement Async Polling

**Medium complexity** (~1-2 days):

1. Create `PollingWorkerPool` (~2 hours)
   - Worker pool logic
   - Queue management
   - Graceful shutdown

2. Refactor `ExportJobExecutor` (~2 hours)
   - Move completion logic to callback
   - Handle async semantics
   - Error propagation

3. Testing (~3 hours)
   - Mock worker pool
   - Async test utilities
   - Integration tests

4. Update JobRun semantics (~1 hour)
   - Handle "pending poll completion" state
   - Timeout detection for stalled polls

5. Observability (~1 hour)
   - Queue depth metrics
   - Worker utilization metrics
   - Poll duration histograms

## Recommendation

**For your current use case, stick with synchronous polling** because:

1. **Simplicity wins**: Linear code is easier to understand/debug/maintain
2. **Scale is adequate**: Goroutines are cheap enough for moderate concurrency
3. **Error handling is cleaner**: Caller knows immediately if job failed
4. **Testing is simpler**: No async coordination needed

**Consider async polling when**:
- You exceed 500 concurrent jobs regularly
- External services complain about too many concurrent connections
- You need fine-grained control over polling resources

## Middle Ground: Semaphore Pattern

If you just want to limit concurrent polls without full async complexity:

```go
// Limit concurrent polls to 50
var pollingSemaphore = make(chan struct{}, 50)

func (e *ExportJobExecutor) Execute(job domain.Job, logger *slog.Logger) {
    createResult, _ := e.exportClient.CreateExport(...)
    
    // Acquire semaphore (blocks if 50 polls in progress)
    pollingSemaphore <- struct{}{}
    defer func() { <-pollingSemaphore }()
    
    // Poll (same as current)
    result, err := polling.Poll(ctx, poller, createResult.ID, config)
    // ...
}
```

This gives you concurrency control without callback complexity.

## Summary

| Aspect | Sync (Current) | Async (Proposed) |
|--------|---------------|------------------|
| **Complexity** | Low | Medium |
| **Max Concurrent Jobs** | ~500 | ~5000+ |
| **Goroutine Count** | 1 per job | Fixed pool |
| **Code Flow** | Linear | Callback-based |
| **Error Handling** | Simple | Complex |
| **Testing** | Easy | Harder |
| **Resource Efficiency** | Good | Excellent |
| **Debugging** | Easy | Harder |
| **Recommended For** | Most cases | High scale |

**My recommendation**: **Don't change now**. The current sync design is appropriate for your scale. Revisit async polling if/when you hit resource limits.
