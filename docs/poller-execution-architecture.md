# Poller Execution Architecture

## Where Does Polling Run?

**The poller runs IN-PROCESS within the scheduler worker**, NOT as a separate pod.

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│  Scheduler Pod (Your Go Application)                            │
│                                                                  │
│  ┌────────────────────────────────────────────────────────┐    │
│  │ Cron Scheduler / Redis Worker                          │    │
│  │ - Triggers jobs on schedule                            │    │
│  │ - Spawns goroutines for execution                      │    │
│  └─────────────────┬──────────────────────────────────────┘    │
│                    │                                            │
│                    ↓ (spawns goroutine)                        │
│  ┌────────────────────────────────────────────────────────┐    │
│  │ Job Executor Goroutine                                 │    │
│  │                                                         │    │
│  │  ┌──────────────────────────────────────────────┐     │    │
│  │  │ ExportJobExecutor.Execute()                  │     │    │
│  │  │                                               │     │    │
│  │  │  1. Create export on external service        │     │    │
│  │  │  2. Create ExportPoller                      │     │    │
│  │  │  3. Call polling.Poll()  ← RUNS HERE         │     │    │
│  │  │     ↓                                         │     │    │
│  │  │     Loop 60 times:                           │     │    │
│  │  │       - HTTP GET to export service           │     │    │
│  │  │       - Check status                         │     │    │
│  │  │       - Sleep 5 seconds                      │     │    │
│  │  │       - Repeat until complete/failed         │     │    │
│  │  │  4. Return result                            │     │    │
│  │  │                                               │     │    │
│  │  └──────────────────────────────────────────────┘     │    │
│  │                                                         │    │
│  └────────────────────────────────────────────────────────┘    │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
                            │
                            │ HTTP Requests (polling)
                            ↓
    ┌────────────────────────────────────────┐
    │  Export Service (External Pod)         │
    │  - Processes export asynchronously     │
    │  - Returns status when polled          │
    └────────────────────────────────────────┘
```

## What is a "Poller"?

A poller is **NOT** a separate service/pod/process. It's just:

1. **An interface** defining how to check job status
2. **A pattern** for organizing status-checking code
3. **Code that runs IN-PROCESS** within the scheduler

Think of it like this:

```go
// This is NOT a separate service - it's just an object
poller := export.NewExportPoller(client, identityHeader)

// This function runs IN THIS GOROUTINE, blocking for 5-10 minutes
result, err := polling.Poll(ctx, poller, jobID, config)
//                           ↑
//                  This is a synchronous loop that:
//                  - Makes HTTP requests
//                  - Sleeps between attempts
//                  - Returns when done
```

## Execution Flow

### Single Scheduler Pod Perspective

```
09:00:00.000 - Cron triggers export job
09:00:00.001 - Scheduler spawns goroutine
09:00:00.010 - Goroutine: POST to export service (create export)
09:00:00.100 - Goroutine: Enters polling.Poll() function
09:00:00.101 - Goroutine: GET to export service (status check #1)
09:00:00.102 - Goroutine: status = "pending", sleep 5s
09:00:05.102 - Goroutine: GET to export service (status check #2)
09:00:05.103 - Goroutine: status = "running", sleep 5s
09:00:10.103 - Goroutine: GET to export service (status check #3)
09:00:10.104 - Goroutine: status = "running", sleep 5s
...
09:07:00.000 - Goroutine: GET to export service (status check #84)
09:07:00.001 - Goroutine: status = "complete"
09:07:00.002 - Goroutine: polling.Poll() returns
09:07:00.003 - Goroutine: Save JobRun as completed
09:07:00.004 - Goroutine: Exit
```

**Key Point**: The entire polling loop from 09:00 to 09:07 happens in ONE goroutine in the scheduler pod.

## Why This Design?

### ✅ Advantages

1. **Simple Architecture**: No additional services to deploy/manage
2. **No Message Queue Needed**: No need for callbacks or webhooks
3. **Stateless**: Each job execution is independent
4. **Resource Efficient**: Goroutines are cheap (thousands per pod)
5. **Easy Debugging**: All logs in one place (scheduler pod)

### ⚠️ Disadvantages

1. **Goroutine Overhead**: Long-running jobs hold goroutines
2. **Pod Restart Impact**: In-flight polls are lost (see polling-state-management.md)
3. **Resource Contention**: Many concurrent jobs = many HTTP clients
4. **No Horizontal Scaling of Polls**: Tied to job execution

## Alternative Architecture (NOT Implemented)

If polling were a separate service (which it's not):

```
┌─────────────────┐         ┌─────────────────┐         ┌─────────────────┐
│  Scheduler Pod  │         │  Poller Pod     │         │  Export Service │
│                 │         │  (Hypothetical) │         │   (External)    │
│  1. Create job  │────────>│  2. Poll loop   │────────>│  3. Process     │
│  2. Enqueue msg │         │  3. Check status│         │  4. Return      │
│                 │         │  4. Send result │         │     status      │
│                 │<────────┤     via queue   │         │                 │
│  5. Mark done   │         │                 │         │                 │
└─────────────────┘         └─────────────────┘         └─────────────────┘
```

**We did NOT implement this** because:
- More complexity (queue, poller service, message routing)
- More infrastructure (separate deployment, networking, monitoring)
- Harder debugging (distributed logs)
- Not needed for current scale

## Comparison: Poller vs Webhook

### Current Design (In-Process Polling)

```
Scheduler Pod:
  - Goroutine blocks for 5-10 minutes
  - Makes HTTP requests every 5 seconds
  - Returns when complete

Pros: Simple, no infrastructure
Cons: Holds goroutine, wasted CPU on sleep
```

### Alternative: Webhook Callback (NOT Implemented)

```
Scheduler Pod:
  - Goroutine completes immediately after creating job
  - Export service calls webhook when done
  - Scheduler receives callback, marks job complete

Pros: No wasted goroutines/CPU
Cons: Requires webhook endpoint, callback routing, retry logic
```

## Resource Usage (Current Design)

### Per Job Execution

```
Memory:  ~1-2 KB (goroutine stack)
CPU:     ~0.1% during HTTP request, 0% during sleep
Network: 1 HTTP POST + 60-120 HTTP GETs (depending on duration)
Time:    Holds goroutine for entire poll duration (5-10 min)
```

### Scheduler Pod with 100 Concurrent Jobs

```
Memory:  ~100-200 KB (100 goroutines)
CPU:     ~1-2% (assuming staggered HTTP requests)
Network: ~200 req/sec average (100 jobs × 1 req/5s)
```

**This is acceptable** for moderate concurrency (< 500 concurrent jobs).

### When to Consider Separate Poller Service

If you have:
- 1000+ concurrent long-running jobs
- Jobs that poll for hours (not minutes)
- Need to survive scheduler restarts without losing polls
- Want to scale polling independently of job scheduling

Then consider:
- Webhook callbacks (export service notifies scheduler)
- Or separate poller service with persistent queue

## What "Poller" Actually Is

```go
// Poller is just an interface - a contract
type Poller interface {
    GetStatus(ctx, jobID) (*StatusResponse, error)
    IsTerminalStatus(status) bool
}

// ExportPoller is just a struct with methods
type ExportPoller struct {
    client         *Client        // HTTP client
    identityHeader string
}

// GetStatus makes an HTTP request (in-process)
func (p *ExportPoller) GetStatus(ctx, jobID) (*StatusResponse, error) {
    // This runs in the SAME goroutine as the job executor
    status, err := p.client.GetExportStatus(ctx, jobID, p.identityHeader)
    // Map to generic status and return
    return &StatusResponse{...}, nil
}
```

**It's just organizing code**, not creating a separate execution context.

## Summary

| Question | Answer |
|----------|--------|
| Is poller a separate pod? | ❌ No |
| Does it run in a separate process? | ❌ No |
| Does it run in a separate goroutine? | ❌ No (same goroutine as job executor) |
| Is it just code that runs in-line? | ✅ Yes |
| Does it make HTTP calls to external service? | ✅ Yes |
| Does it block the job executor goroutine? | ✅ Yes (intentionally) |

**The poller is just a design pattern for organizing status-checking code.** It runs in the same goroutine that executes the job, inside the scheduler pod.
