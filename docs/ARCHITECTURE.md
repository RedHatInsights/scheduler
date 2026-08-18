# Insights Scheduler Architecture

This document describes the architecture of the Insights Scheduler, covering both the legacy single-process implementation and the modern distributed, scalable Kubernetes deployment.

## Table of Contents

1. [Overview](#overview)
2. [Architecture Patterns](#architecture-patterns)
3. [Deployment Models](#deployment-models)
4. [Payload Templating](#payload-templating)
5. [Data Flow](#data-flow)
6. [Scaling Strategy](#scaling-strategy)
7. [Reliability and Resilience](#reliability-and-resilience)
8. [Zero-Downtime Deployments](#zero-downtime-deployments)

## Overview

The Insights Scheduler is a job scheduling service built using Go and following clean architecture principles (Functional Core / Imperative Shell pattern). It supports multiple deployment models from local development to large-scale Kubernetes deployments.

### Core Capabilities

- **Cron-based scheduling**: Standard 5-field cron expressions with timezone support
- **Multiple job types**: Message processing, HTTP requests, command execution, export service integration
- **Persistence**: PostgreSQL (production) or SQLite (local development)
- **Distributed scheduling**: Redis-based coordination for multi-worker deployments
- **Horizontal scaling**: Stateless API and Worker pods with rolling updates
- **Zero-downtime deployments**: No missed jobs during rolling updates
- **Job run history**: Complete audit trail of all job executions with structured results
- **User-based authorization**: User-scoped access control via X-Rh-Identity header
- **Dual persistence**: Redis + PostgreSQL ensure job state survives failures
- **Auto-pause on failures**: Jobs automatically pause after N consecutive failures (configurable)
- **Payload templating**: CEL-based dynamic expressions in job payloads (see [Payload Templating](payload_templating.md))
- **Structured logging**: CloudWatch-compatible JSON logging with context fields
- **Metrics**: Prometheus metrics for monitoring job execution and system health

## Architecture Patterns

### Functional Core / Imperative Shell

The codebase follows a strict separation between pure business logic and side effects:

```
internal/
├── core/                           # Functional Core (Pure)
│   ├── domain/                     # Domain models and validation
│   │   ├── job.go                  # Immutable Job types with timezone support
│   │   ├── job_run.go              # Job execution history
│   │   ├── result.go               # Structured job results
│   │   └── errors.go               # Domain errors
│   ├── ports/                      # Interface definitions
│   │   ├── job_service.go          # Core job operations
│   │   ├── authorized_job_service.go  # Identity-aware operations
│   │   ├── scheduler_job_service.go   # Scheduler-specific operations
│   │   ├── executor.go             # Job execution interface
│   │   └── template.go             # PayloadValidator / PayloadResolver interfaces
│   ├── template/                   # CEL payload templating engine
│   │   ├── evaluator.go            # CEL environment, date functions, expression eval
│   │   └── evaluator_test.go       # Comprehensive template tests
│   └── usecases/                   # Business logic
│       ├── job_service.go          # Job CRUD operations
│       ├── job_run_service.go      # Job run management
│       ├── authorized_adapter.go   # Identity extraction adapter
│       ├── scheduler_adapter.go    # Scheduler operations adapter
│       └── scheduling.go           # Scheduling calculations
│
├── clients/                        # External service clients
│   └── export/                     # Export service integration
│       ├── client.go               # REST client for export service
│       └── types.go                # Export request/response types
│
├── identity/                       # User validation
│   ├── validator.go                # Identity header generation
│   ├── bop_validator.go            # Back Office Portal integration
│   ├── 3scale_validator.go         # 3scale API gateway integration
│   └── metrics.go                  # Identity validation metrics
│
├── config/                         # Configuration management
│   └── config.go                   # Clowder and env var configuration
│
└── shell/                          # Imperative Shell (Side Effects)
    ├── http/                       # REST API
    │   ├── routes.go               # Route definitions
    │   ├── handlers.go             # HTTP handlers
    │   ├── dto.go                  # Request/response DTOs
    │   └── middleware.go           # Logging middleware
    ├── storage/                    # Persistence
    │   ├── postgres_repository.go  # PostgreSQL job repository
    │   ├── postgres_job_run_repository.go  # Job run persistence
    │   ├── migrations.go           # Schema migrations
    │   └── memory_repository.go    # In-memory (testing)
    ├── scheduler/                  # Background schedulers
    │   ├── redis_scheduler.go      # Redis-based distributed scheduler
    │   └── scheduler.go            # In-memory cron scheduler (legacy)
    ├── executor/                   # Job execution
    │   ├── job_executor.go         # Executor orchestration
    │   ├── export_job_executor.go  # Export service integration
    │   ├── http_job_executor.go    # HTTP request execution (simulated)
    │   ├── message_job_executor.go # Message processing (simulated)
    │   ├── command_job_executor.go # Command execution (simulated)
    │   ├── kafka_notifier.go       # Job completion notifications
    │   └── metrics.go              # Executor metrics
    ├── messaging/                  # Kafka integration
    │   └── kafka_producer.go       # Kafka message producer
    └── logging/                    # Structured logging
        └── logger.go               # CloudWatch-compatible JSON logger
```

**Benefits**:
- Pure functions in core are easily testable
- Business logic isolated from infrastructure
- Shell components can be swapped (e.g., SQLite → Postgres)
- Clear dependency direction (shell → core, never core → shell)

### Dependency Injection

All dependencies are injected at the composition root (`cmd/*/main.go`):

```go
// 1. Create infrastructure (shell)
jobRepo := storage.NewPostgresJobRepository(cfg)
executor := executor.NewJobExecutor()
scheduler := scheduler.NewRedisScheduler(redisAddr, executor)

// 2. Inject into business logic (core)
jobService := usecases.NewJobService(jobRepo, jobRunRepo)

// 3. Inject into HTTP handlers (shell)
handler := http.NewHandler(jobService)
```

## Deployment Models

### 1. Single-Process (Legacy)

**Location**: `cmd/server/main.go` (run with `./scheduler` or `./scheduler server`)

**Architecture**:
```
┌─────────────────────────────┐
│     Single Process          │
│                             │
│  ┌─────────┐  ┌──────────┐ │
│  │ HTTP    │  │Scheduler │ │
│  │ Handler │  │(polling) │ │
│  └────┬────┘  └────┬─────┘ │
│       │            │        │
│       └──────┬─────┘        │
│              │              │
│         ┌────▼────┐         │
│         │Database │         │
│         │(SQLite/ │         │
│         │Postgres)│         │
│         └─────────┘         │
└─────────────────────────────┘
```

**Use Cases**:
- Local development
- Testing
- Small deployments
- Single-tenant scenarios

**Limitations**:
- No horizontal scaling
- Single point of failure
- Scheduler and API compete for resources

**Start Command**:
```bash
go run cmd/server/main.go
```

### 2. Multi-Pod Distributed (Production)

**Location**: `cmd/server/main.go` with subcommands:
- `./scheduler api` - Run API server
- `./scheduler worker` - Run worker

**Architecture**:
```
                    ┌─────────────────┐
                    │   Load Balancer │
                    └────────┬────────┘
                             │
              ┌──────────────┴──────────────┐
              │                             │
    ┌─────────▼────────┐         ┌─────────▼────────┐
    │   API Pod 1      │         │   API Pod N      │
    │  (Stateless)     │   ...   │  (Stateless)     │
    │                  │         │                  │
    │ - REST API       │         │ - REST API       │
    │ - CRUD ops       │         │ - CRUD ops       │
    └─────────┬────────┘         └─────────┬────────┘
              │                             │
              │         PostgreSQL          │
              │      (Source of Truth)      │
              └──────────┬──────────────────┘
                         │
              ┌──────────▼──────────┐
              │                     │
    ┌─────────▼────────┐  ┌────────▼─────────┐
    │  Worker Pod 1    │  │  Worker Pod M    │
    │  (Stateless)     │  │  (Stateless)     │
    │                  │  │                  │
    │ - Job Execution  │  │ - Job Execution  │
    │ - Redis polling  │  │ - Redis polling  │
    └─────────┬────────┘  └────────┬─────────┘
              │                     │
              └──────────┬──────────┘
                         │
                    ┌────▼────┐
                    │  Redis  │
                    │(Sorted  │
                    │ Sets)   │
                    └─────────┘
```

**Components**:

1. **API Pods** (2-10 replicas)
   - Handle REST API requests
   - Write job metadata to Postgres
   - Update Redis sorted sets with next run time
   - Stateless, scale based on request volume
   - Ports: 5000 (HTTP), 8080 (metrics), 9090 (private)

2. **Worker Pods** (3-50 replicas)
   - Poll Redis for jobs due to run
   - Acquire distributed locks to prevent duplicates
   - Execute jobs via job executor framework
   - Write job run history to Postgres
   - Periodic sync from Postgres → Redis (hourly)
   - Stateless, scale based on job execution volume
   - Port: 8080 (metrics)

3. **PostgreSQL** (StatefulSet)
   - Source of truth for job definitions
   - Stores job run history
   - Queried by API for all reads
   - Updated by API (metadata) and Workers (run history)

4. **Redis** (StatefulSet)
   - Distributed scheduling coordinator
   - Sorted set: `scheduler:jobs:scheduled` (score = timestamp)
   - Job data: `scheduler:job:{id}` (hash with job details)
   - Distributed locks: `scheduler:lock:{id}` (SET NX with TTL)
   - Polled by Workers for due jobs

**Use Cases**:
- Production Kubernetes deployments
- High-availability requirements
- Large-scale job processing (1000s of jobs)
- Multi-tenant scenarios
- Traffic spikes (autoscaling)

**Deployment**:
```bash
# Build images
docker build -f Dockerfile.api -t scheduler-api:latest .
docker build -f Dockerfile.worker -t scheduler-worker:latest .

# Deploy to Kubernetes
kubectl apply -k k8s/
```

## Payload Templating

Job payloads support dynamic values via [CEL (Common Expression Language)](https://cel.dev/) expressions. Any string value prefixed with `cel:` is evaluated at execution time; all other values pass through unchanged.

**Architecture:**

```
┌─────────────────────────────────────────────────────────┐
│                    CEL Evaluator                        │
│                (internal/core/template/)                │
│                                                         │
│  Implements:  ports.PayloadValidator                    │
│               ports.PayloadResolver                     │
│                                                         │
│  Environment: now (timestamp), job_id (string)          │
│  Functions:   add_days, add_months, start_of_day,       │
│               end_of_day, first_of_month, last_of_month,│
│               first_of_last_month, last_of_last_month,  │
│               first_of_week, last_of_week,              │
│               first_of_quarter, last_of_quarter,        │
│               format_date                               │
│  Constants:   ISO_DATE, ISO_DATETIME, ISO_8601,         │
│               US_DATE, EU_DATE, YEAR_MONTH, etc.        │
└─────────────────────────────────────────────────────────┘
```

**Two-phase design:**

1. **API time (validation)** — When a job is created or updated, all `cel:` expressions are compiled but not evaluated. Syntax errors return `400 Bad Request` immediately.

2. **Execution time (resolution)** — When the job runs, `cel:` expressions are evaluated with the current UTC time as `now` and the job's UUID as `job_id`. The resolved payload is then passed to the executor.

**Integration points:**

- `DefaultJobService` receives a `PayloadValidator` and validates on create/update/patch
- `ExportJobExecutor` receives a `PayloadResolver` and resolves before marshaling the export request
- Both are the same `Evaluator` instance, created once at startup and injected via DI

**Security:** Expression length, evaluation cost, nesting depth, and expression count are all capped. The CEL sandbox has no access to I/O or system resources.

For the full function reference, format constants, and usage examples, see **[docs/payload_templating.md](payload_templating.md)**.

## Data Flow

### Job Creation (POST /api/v1/jobs)

```
User Request
    │
    ▼
┌───────────────┐
│   API Pod     │
│               │
│ 1. Validate   │
│ 2. Save to    │────────▶ PostgreSQL (jobs table)
│    Postgres   │
│               │
│ 3. Schedule   │────────▶ Redis ZADD scheduler:jobs:scheduled
│    in Redis   │         Redis HSET scheduler:job:{id}
└───────────────┘
    │
    ▼
Success Response
```

### Auto-Pause on Consecutive Failures

The scheduler automatically pauses jobs that fail repeatedly:

**Configuration:**
- Environment variable: `MAX_CONSECUTIVE_FAILURES` (default: 3)
- Set to `0` to disable auto-pause feature

**Behavior:**
1. Each job tracks `consecutive_failures` counter
2. Counter increments on execution failure
3. Counter resets to 0 on successful execution
4. When `consecutive_failures >= MAX_CONSECUTIVE_FAILURES`, job status changes to `paused`
5. Paused jobs must be manually resumed via `/jobs/{id}/resume` endpoint

**Notifications:**
- Auto-paused jobs trigger Kafka notification to `platform.notifications.ingress` topic
- Event type: `job-auto-paused`
- Context includes: `job_id`, `consecutive_failures`, `last_error`

**Metrics:**
- `scheduler_jobs_auto_paused_total` - Counter of auto-paused jobs
- `scheduler_jobs_consecutive_failures` - Gauge of current failure count per job

### Job Execution

```
Worker Pod (polling every 10 seconds, configurable via SCHEDULER_REDIS_POLL_INTERVAL)
    │
    ▼
┌───────────────┐
│  Redis Query  │  ZRANGEBYSCORE scheduler:jobs:scheduled
│  (get due     │  -inf {now} LIMIT 0 100
│   jobs)       │
└───────┬───────┘
        │
        ▼
┌───────────────┐
│  Acquire Lock │  SET scheduler:lock:{id} 1 NX EX 300
│  (distributed)│
└───────┬───────┘
        │
        ▼ (if lock acquired)
┌───────────────┐
│  Execute Job  │  jobExecutor.Execute(job)
└───────┬───────┘
        │
        ▼
┌───────────────┐
│  Save Result  │────────▶ PostgreSQL (job_runs table)
└───────┬───────┘
        │
        ▼
┌───────────────┐
│  Update Next  │────────▶ Redis ZADD (new timestamp)
│  Run Time     │
└───────────────┘
```

### Job Query (GET /api/v1/jobs/{id}/runs)

```
User Request
    │
    ▼
┌───────────────┐
│   API Pod     │
│               │
│ 1. Validate   │
│    identity   │
│               │
│ 2. Query runs │────────▶ PostgreSQL
│    with       │         (job_runs table)
│    pagination │
└───────┬───────┘
        │
        ▼
JSON Response with job runs
```

## Scaling Strategy

### Horizontal Pod Autoscaler (HPA)

**API Pods**:
```yaml
minReplicas: 2
maxReplicas: 10
targetCPUUtilizationPercentage: 70
targetMemoryUtilizationPercentage: 80
```

**Triggers for scale-up**:
- Increased API request volume
- High CPU usage from request processing
- High memory usage from database queries

**Worker Pods**:
```yaml
minReplicas: 3
maxReplicas: 50
targetCPUUtilizationPercentage: 70
targetMemoryUtilizationPercentage: 80
scaleUp:
  policies:
    - type: Percent, value: 50%    # +50% pods
    - type: Pods, value: 5         # or +5 pods
  stabilizationWindowSeconds: 60
scaleDown:
  policies:
    - type: Percent, value: 25%    # -25% pods
  stabilizationWindowSeconds: 300  # Wait 5min before scaling down
```

**Triggers for scale-up**:
- Large number of jobs due to execute
- Long-running job execution
- High CPU from job processing

### Database Scaling

**PostgreSQL**:
- Vertical scaling (increase instance size)
- Read replicas for read-heavy workloads
- Connection pooling (25 max open, 5 idle per pod)
- In production: Use managed database (RDS, Cloud SQL)

**Redis**:
- Vertical scaling for in-memory dataset
- Redis Sentinel for high availability
- Redis Cluster for horizontal scaling (advanced)
- In production: Use managed Redis (ElastiCache, Cloud Memorystore)

### Capacity Planning

**API Pods**:
- Each pod: 100m CPU, 128Mi memory (request)
- Each pod handles ~100 req/sec
- For 1000 req/sec: need ~10 pods

**Worker Pods**:
- Each pod: 200m CPU, 256Mi memory (request)
- Each pod executes ~10 concurrent jobs
- For 500 concurrent jobs: need ~50 pods

## Reliability and Resilience

### Database Downtime Handling

**Scenario**: PostgreSQL is down for maintenance

**Impact**:
- ✅ **Workers continue executing jobs** (read from Redis)
- ✅ **Scheduled jobs still run on time** (Redis has schedule)
- ❌ **API requests fail** (can't read/write to Postgres)
- ❌ **Job run history not saved** (will accumulate when DB returns)

**Mitigation**:
1. Use managed database with minimal downtime
2. Schedule maintenance during low-traffic periods
3. Implement retry logic in workers for job run saves
4. Use circuit breaker pattern for database connections

### Redis Downtime Handling

**Scenario**: Redis is down or restarted

**Impact**:
- ✅ **API CRUD operations work** (Postgres is source of truth)
- ❌ **Workers can't find jobs to execute** (no schedule)
- ❌ **New job schedules not updated** (API writes fail)

**Recovery**:
1. Workers perform hourly sync: Postgres → Redis
2. On startup, all workers sync from Postgres
3. Redis persistence (RDB + AOF) restores schedule after restart

**Mitigation**:
1. Use Redis Sentinel for automatic failover
2. Enable AOF persistence for durability
3. Monitor Redis health closely

### Worker Pod Failures

**Scenario**: Worker pod crashes during job execution

**Impact**:
- ✅ **Job lock expires** (5 minute TTL)
- ✅ **Another worker picks up job** (after lock expiry)
- ✅ **No duplicate execution** (lock prevents it)
- ⚠️  **Delayed execution** (5 minute delay maximum)

**Mitigation**:
1. Graceful shutdown: 5 minute termination grace period
2. Job timeout handling
3. Idempotent job execution (safe to retry)
4. Multiple worker replicas (≥3) ensure coverage

**See Also**: [Zero-Downtime Deployments](#zero-downtime-deployments) for details on rolling update strategy

### API Pod Failures

**Scenario**: API pod crashes

**Impact**:
- ✅ **Other API pods handle requests** (stateless)
- ✅ **No data loss** (Postgres and Redis persist)
- ✅ **Kubernetes restarts pod** (liveness probe)

**Mitigation**:
1. Always run ≥2 API pods (HPA minimum)
2. Load balancer distributes traffic
3. Health checks detect failures quickly

### Split-Brain Prevention

**Problem**: Two workers executing the same job

**Solution**: Distributed locking with Redis

```go
// Worker 1 and Worker 2 both see job is due
lockKey := fmt.Sprintf("scheduler:lock:%s", jobID)

// Only one succeeds
success := redis.SetNX(lockKey, "1", 5*time.Minute)
if !success {
    // Another worker has the lock, skip this job
    return
}

// Execute job knowing we have exclusive access
executeJob()
```

**Properties**:
- Atomic operation (SET NX)
- TTL ensures lock release even if worker crashes
- Lock key deleted after successful execution

## Zero-Downtime Deployments

### Overview

The scheduler service is designed to prevent missed jobs during deployments through a combination of rolling updates, graceful shutdown, and dual-persistence architecture.

### Deployment Strategy

**ClowdApp Configuration** (`deploy/clowdapp.yml`):

```yaml
deployments:
  - name: api
    minReplicas: 2
    deploymentStrategy:
      rollingParams:
        maxSurge: 25%
        maxUnavailable: 25%

  - name: worker
    minReplicas: 3
    deploymentStrategy:
      rollingParams:
        maxSurge: 1
        maxUnavailable: 1
```

**Key Settings**:
- **API Pods**: 2+ replicas, 25% rolling update (fast deployment)
- **Worker Pods**: 3+ replicas, 1-at-a-time rolling update (conservative, ensures coverage)
- **Termination Grace Period**: 300s (5 minutes) for workers to complete jobs
- **PreStop Hook**: 15-second sleep before SIGTERM (allows polling loop to exit gracefully)

### How Jobs Remain Scheduled During Deployment

**Dual Persistence Architecture**:

1. **PostgreSQL** (Source of Truth)
   - All job definitions persisted
   - Job run history stored
   - Survives pod restarts and Redis failures

2. **Redis** (Scheduling Coordinator)
   - Jobs stored in sorted set by next run time
   - Distributed locks prevent duplicate execution
   - Persisted to disk (RDB + AOF)
   - External service - survives pod restarts

**Startup Sync Process** (`cmd/server/main.go:575-612`):

```go
// Worker startup sequence
1. Check if Redis has jobs (ZCARD scheduler:jobs:scheduled)
2. If Redis is empty:
   - Attempt leader election (SETNX scheduler:sync:leader)
   - If elected: Load all jobs from PostgreSQL
   - Sync PostgreSQL → Redis (ZADD for each scheduled job)
3. Start polling loop
```

**Periodic Sync** (enabled via `ENABLE_PERIODIC_SYNC=true`):
- Hourly sync from PostgreSQL → Redis
- Safety mechanism for Redis failures or missed updates
- Runs in background goroutine

### Rolling Deployment Flow

**Example: 3 worker pods updating from v1.0 to v1.1**

```
Time    Pod 1      Pod 2      Pod 3      Pod 4      Active Workers
------  ---------  ---------  ---------  ---------  --------------
T+0     v1.0 ✓     v1.0 ✓     v1.0 ✓     -          3 (all v1.0)
T+30    v1.0 ✓     v1.0 ✓     v1.0 ✓     v1.1 ⏳     3
T+45    v1.0 ✓     v1.0 ✓     v1.0 ✓     v1.1 ✓     4
T+60    v1.0 ⏬     v1.0 ✓     v1.0 ✓     v1.1 ✓     3
        (preStop)
T+75    -          v1.0 ✓     v1.0 ✓     v1.1 ✓     3 (2×v1.0, 1×v1.1)
T+90    -          v1.0 ✓     v1.0 ✓     v1.1 ✓     v1.1 ⏳     3
T+105   -          v1.0 ✓     v1.0 ✓     v1.1 ✓     v1.1 ✓     4
T+120   -          v1.0 ⏬     v1.0 ✓     v1.1 ✓     v1.1 ✓     3
T+135   -          -          v1.0 ✓     v1.1 ✓     v1.1 ✓     3
T+150   -          -          v1.0 ✓     v1.1 ✓     v1.1 ✓     v1.1 ⏳  3
T+165   -          -          v1.0 ✓     v1.1 ✓     v1.1 ✓     v1.1 ✓  4
T+180   -          -          v1.0 ⏬     v1.1 ✓     v1.1 ✓     v1.1 ✓  3
T+195   -          -          -          v1.1 ✓     v1.1 ✓     v1.1 ✓  3 (all v1.1)
```

**Legend**:
- ✓ = Running and polling
- ⏳ = Starting up
- ⏬ = Graceful shutdown (preStop + terminationGracePeriod)

**Result**: At least 3 workers are ALWAYS actively polling Redis throughout the deployment.

### Graceful Shutdown Sequence

**Worker Pod Shutdown** (`cmd/server/main.go:648-658`):

```
1. Kubernetes sends SIGTERM to pod
2. PreStop hook executes: sleep 15 seconds
   - Gives time for load balancer to remove pod from rotation
   - Allows current polling iteration to complete
3. Application receives SIGTERM
4. redisScheduler.Stop() called
   - Cancels context
   - Stops polling loop
   - No new jobs acquired
5. Wait up to 300 seconds (terminationGracePeriodSeconds)
   - In-flight jobs continue executing
   - Export jobs can run up to 10 minutes (polling for completion)
6. If jobs still running after 300s:
   - Kubernetes sends SIGKILL
   - Job locks expire after 5 minutes (TTL)
   - Another worker picks up the job
```

### Job Execution Guarantees

**During Normal Operation**:
- ✅ **No duplicates**: Distributed locks (Redis SETNX) prevent multiple workers from executing same job
- ✅ **At-least-once execution**: Jobs in Redis sorted set are processed when due
- ✅ **Timestamp accuracy**: Jobs updated with `last_run_at` and `next_run_at` in PostgreSQL

**During Deployment**:
- ✅ **No missed jobs**: Multiple workers always polling (minUnavailable: 1)
- ✅ **No duplicates**: Locks remain active during rolling update
- ✅ **Bounded delay**: Maximum 10-second delay (polling interval) + deployment transition time
- ✅ **State preserved**: Redis and PostgreSQL persist across pod restarts

**Worst-Case Scenarios**:

1. **All workers killed simultaneously** (NOT recommended)
   - Jobs remain in Redis sorted set
   - New workers start within ~30 seconds
   - First poll happens within 10 seconds of startup
   - Maximum delay: ~40 seconds
   - Result: Jobs delayed but NOT lost

2. **Redis failure during deployment**
   - Workers sync from PostgreSQL on startup
   - Periodic sync restores Redis state
   - Jobs execute once Redis recovers
   - Result: Delayed until Redis returns

3. **Worker crashes during job execution**
   - Lock expires after 5 minutes (lockTTL)
   - Another worker picks up job after lock expiry
   - Job marked as failed in job_runs table
   - Result: Delayed by up to 5 minutes, then retried

### Polling Configuration

**Worker Poll Interval** (`redis_scheduler.go:95`):
```go
ticker := time.NewTicker(10 * time.Second)
```

**Job Selection** (`redis_scheduler.go:190-194`):
```go
// Get all overdue jobs (not just current tick)
results := redis.ZRangeByScore("scheduler:jobs:scheduled",
    Min: "0",
    Max: now.Unix(),
    Count: 100  // Process up to 100 jobs per tick
)
```

**Properties**:
- Processes ALL overdue jobs, not just current interval
- Prevents accumulation during temporary downtime
- 100-job batch limit prevents memory issues
- Each job gets distributed lock before execution

### Job Timestamp Management

**Updated Fields** (`redis_scheduler.go:252-274`):

```go
// Before execution
scheduledJob.Job = scheduledJob.Job.WithLastRunAt(now)

// Execute job
executor.Execute(scheduledJob.Job)

// After execution
nextRun := schedule.Next(time.Now())
scheduledJob.Job = scheduledJob.Job.WithNextRunAt(nextRun)

// Persist to PostgreSQL
jobRepo.Save(scheduledJob.Job)

// Persist to Redis
redis.Set(jobKey, scheduledJob)
redis.ZAdd("scheduler:jobs:scheduled", nextRun.Unix(), jobID)
```

**Result**: Both PostgreSQL and Redis stay in sync with current execution state.

### Monitoring Deployment Health

**Metrics to Watch**:
```
# Number of workers actively polling
scheduler_worker_pods_active{version="v1.1"}

# Job execution latency
scheduler_job_execution_delay_seconds
  - Histogram of (execution_time - scheduled_time)
  - Should remain under 15 seconds during deployment

# Lock acquisition failures
scheduler_lock_acquisition_failures_total
  - Should remain at 0 during healthy deployment
  - Spikes indicate split-brain or timing issues

# Jobs in Redis
scheduler_redis_jobs_scheduled_count
  - Should remain constant or grow during deployment
  - Drop indicates Redis sync issue
```

**Health Checks**:
- Worker liveness: `/metrics` endpoint (8080)
- Worker readiness: Redis connectivity check
- API liveness: `/health` endpoint
- API readiness: PostgreSQL connectivity check

### Deployment Best Practices

**Recommended Settings**:
```yaml
# API Deployment
minReplicas: 2
maxReplicas: 10
maxSurge: 25%
maxUnavailable: 25%
terminationGracePeriodSeconds: 30

# Worker Deployment
minReplicas: 3
maxReplicas: 50
maxSurge: 1
maxUnavailable: 1
terminationGracePeriodSeconds: 300
```

**Environment Variables**:
```bash
# Enable periodic PostgreSQL → Redis sync (hourly)
ENABLE_PERIODIC_SYNC=true

# Shutdown timeout for workers (5 minutes)
SHUTDOWN_TIMEOUT=300s

# Export job polling configuration
EXPORT_SERVICE_POLL_INTERVAL=5s
EXPORT_SERVICE_POLL_MAX_RETRIES=60  # Up to 5 minutes
```

**Pre-Deployment Checklist**:
1. ✅ Verify Redis is healthy and accessible
2. ✅ Verify PostgreSQL is healthy with recent backup
3. ✅ Check current job count: `redis-cli ZCARD scheduler:jobs:scheduled`
4. ✅ Verify at least 3 worker pods running
5. ✅ Monitor job execution metrics for baseline
6. ✅ Review deployment strategy (rolling update configured)

**Post-Deployment Verification**:
1. ✅ Verify all worker pods running new version
2. ✅ Check Redis job count unchanged
3. ✅ Monitor job execution latency (should be < 15s)
4. ✅ Check PostgreSQL for recent job runs
5. ✅ Verify no error spikes in logs
6. ✅ Review lock acquisition metrics (should be 0 failures)

### Redis Configuration

**ClowdApp Settings** (`deploy/clowdapp.yml:22`):
```yaml
inMemoryDb: true  # Enables Redis via Clowder
```

**Clowder Provisions**:
- Redis StatefulSet with persistence (RDB + AOF)
- Service discovery (hostname from `clowderConfig.InMemoryDb`)
- Automatic password management
- Connection pooling configuration

**Config Loading** (`internal/config/config.go:357-405`):
```go
if clowderConfig != nil && clowderConfig.InMemoryDb != nil {
    // Automatic configuration from Clowder
    host = clowderConfig.InMemoryDb.Hostname
    port = clowderConfig.InMemoryDb.Port
    password = *clowderConfig.InMemoryDb.Password
}
```

**Benefits**:
- Zero configuration in deployed environments
- Automatic failover with Redis Sentinel
- Persistence survives Redis pod restarts
- Credentials managed by platform

### Summary

The scheduler service prevents missed jobs during deployments through:

1. **Rolling updates**: Only 1 worker updated at a time (maxUnavailable: 1)
2. **Multiple workers**: Always ≥2 workers active (minReplicas: 3, maxUnavailable: 1)
3. **Graceful shutdown**: 5-minute grace period for job completion
4. **PreStop hooks**: 15-second buffer before SIGTERM
5. **Dual persistence**: Redis + PostgreSQL ensure job state survival
6. **Startup sync**: New workers sync from PostgreSQL on startup
7. **Periodic sync**: Hourly PostgreSQL → Redis safety sync
8. **Overdue job processing**: Workers process ALL overdue jobs, not just current interval
9. **Distributed locking**: Prevents duplicate execution across workers
10. **Timestamp tracking**: Jobs track `last_run_at` and `next_run_at` in both stores

**Result**: Zero missed jobs during normal rolling deployments with <15 second execution delay.

## Monitoring and Observability

### Metrics (Prometheus)

All pods expose `/metrics` on port 8080:

```
# Job execution metrics
scheduler_jobs_executed_total{status="success|failure"}
scheduler_job_execution_duration_seconds

# Scheduler metrics
scheduler_jobs_scheduled_count
scheduler_lock_acquisition_failures_total

# Database metrics
scheduler_db_queries_total{operation="select|insert|update"}
scheduler_db_query_duration_seconds
```

### Logging

Structured logging with context:

```
[API] POST /api/v1/jobs org_id=12345 user_id=67890 job_id=abc-123
[WORKER] Executing job job_id=abc-123 type=export
[WORKER] Job completed job_id=abc-123 status=success duration=2.5s
```

### Health Checks

**API Pods**:
- Liveness: GET /api/v1/jobs (verifies HTTP server)
- Readiness: GET /api/v1/jobs (verifies database connection)

**Worker Pods**:
- Liveness: GET /metrics (verifies process alive)
- Readiness: GET /metrics (verifies Redis connection)

## Security

### Authentication

All API requests require the `X-Rh-Identity` header, which is enforced by the `identity.EnforceIdentity` middleware on all `/api/scheduler/v1/*` routes.

**Identity Header Format:**
```
X-Rh-Identity: <base64-encoded-json>
{
  "identity": {
    "org_id": "000101",
    "user": {
      "user_id": "user-123",
      "username": "jdoe",
      "email": "jdoe@example.com"
    },
    "type": "User",
    "account_number": "000202"
  }
}
```

The middleware validates the header and extracts identity information into the request context. Missing or invalid identity headers result in 400 Bad Request responses.

### Authorization

The service implements **user-based authorization** with multi-tenant isolation:

**Two-Layer Authorization Model:**

1. **AuthorizedJobServiceAdapter** (`internal/core/usecases/authorized_adapter.go`)
   - Extracts `org_id` and `user_id` from the identity context
   - Passes these values to the core service layer
   - Users cannot spoof org_id or user_id via request payloads

2. **Core Service Authorization Checks** (`internal/core/usecases/job_service.go`)
   - `GetJobWithUserCheck()` - Verifies `job.UserID == userID`
   - `GetJobsByUserID()` - Filters jobs by user ID
   - Authorization failures return `ErrJobNotFound` (404) instead of 403 to prevent information leakage

**User Isolation:**
- Jobs are scoped to `user_id` (not just `org_id`)
- Users can only view, modify, and delete their own jobs
- Cross-user access attempts return "job not found" to prevent enumeration attacks

**Database Filtering:**
- Repository methods filter by `user_id`: `FindByUserID(userID string)`
- Indexes exist on `user_id` and `org_id` columns for performance

**Request Flow:**
```
1. HTTP Request → Identity Middleware validates X-Rh-Identity header
2. AuthorizedJobServiceAdapter extracts user_id from identity
3. Core JobService enforces user_id authorization checks
4. Repository filters jobs by user_id
```

### Network Security

**Kubernetes NetworkPolicy** (recommended):
```yaml
# API pods can receive from ingress
# API pods can connect to postgres, redis

# Worker pods cannot receive external traffic
# Worker pods can connect to postgres, redis
```

### Secrets Management

- Passwords stored in Kubernetes Secrets
- Production: Use external secret management (Vault, etc.)
- Secrets injected as environment variables
- Never logged or exposed in metrics

## Future Enhancements

1. **Multi-region deployment**
   - Redis Cluster for cross-region coordination
   - PostgreSQL replication for disaster recovery

2. **Job priority and queuing**
   - Multiple Redis sorted sets by priority
   - Workers process high-priority jobs first

3. **Job dependencies**
   - DAG-based job execution
   - Wait for dependent jobs before execution

4. **Advanced scheduling**
   - Time windows (execute only between 9am-5pm)
   - Retry policies with exponential backoff
   - Job cancellation and pausing

5. **Observability improvements**
   - Distributed tracing (OpenTelemetry)
   - Job execution timeline visualization
   - Anomaly detection for job failures

## References

- [KUBERNETES_DEPLOYMENT.md](KUBERNETES_DEPLOYMENT.md) - Deployment guide
- [BUILD.md](../BUILD.md) - Build instructions
- [REDIS_SCHEDULER.md](REDIS_SCHEDULER.md) - Redis implementation details
- [CLAUDE.md](../CLAUDE.md) - Development guide
