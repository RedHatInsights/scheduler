# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

Start the service:
```bash
go run cmd/server/main.go
```

Install dependencies:
```bash
go mod tidy
```

Run API tests (requires service to be running):
```bash
go run cmd/test/main.go
```

Build for production:
```bash
go build -o bin/scheduler cmd/server/main.go
```

## Architecture Overview (Functional Core / Imperative Shell)

This Go service implements the declarative shell functional core pattern with clear separation between pure business logic and side effects.

### Functional Core (`internal/core/`)
Contains pure functions with no side effects:

**Domain Layer** (`internal/core/domain/`):
- `job.go` - Immutable Job types with pure transformation functions
- `errors.go` - Domain error definitions
- All validation logic is pure (e.g., `IsValidSchedule()`)

**Use Cases Layer** (`internal/core/usecases/`):
- `job_service.go` - Business logic that depends only on interfaces
- `scheduling.go` - Pure scheduling calculation logic
- No concrete dependencies, only interface contracts

### Imperative Shell (`internal/shell/`)
Handles all side effects and I/O:

**Storage** (`internal/shell/storage/`):
- `sqlite_repository.go` - SQLite database implementation for jobs and job runs
- Implements `JobRepository` and `JobRunRepository` interfaces from core

**HTTP** (`internal/shell/http/`):
- `handlers.go` - HTTP request/response handling
- `routes.go` - Route definitions using Gorilla Mux
- All JSON marshaling/unmarshaling happens here

**Scheduler** (`internal/shell/scheduler/`):
- `scheduler.go` - Background goroutine that polls for jobs
- `export_poller_service.go` - Centralized polling for in-flight export jobs (replaces inline polling)
- Uses functional core for scheduling decisions

**Executor** (`internal/shell/executor/`):
- `job_executor.go` - Generic job executor with map-based payload type dispatch
- `export_job_executor.go` - Export service integration (fire-and-forget kick-off; polling handled by ExportPollerService)
- `failure_tracker.go` - Shared consecutive failure tracking logic (used by FailureTrackingExecutor and ExportPollerService)
- `message_job_executor.go`, `http_job_executor.go`, `command_job_executor.go` - Simulated executors
- `kafka_notifier.go` - Platform notifications integration
- Job completion notification system with Kafka support

### Dependency Injection Pattern

The `cmd/server/main.go` wires everything together:
1. Creates imperative shell components (storage, executor, scheduler)
2. Injects them into functional core (usecases.JobService)
3. Passes core service to imperative shells (HTTP handlers, background scheduler)

## Key Design Principles

**Immutability**: Domain objects use value semantics with `WithX()` methods for updates
**Interface Segregation**: Core depends only on minimal interfaces
**Dependency Inversion**: Core defines interfaces, shell implements them
**Pure Functions**: All business logic in core is deterministic and testable

## Schedule Format

The service accepts standard 5-field cron expressions:
- Format: `minute hour day-of-month month day-of-week`
- **All schedules are interpreted in UTC timezone**

Common predefined schedules (available as constants):
- `*/10 * * * *` - Every 10 minutes (Schedule10Minutes)
- `0 * * * *` - Every hour at minute 0 (Schedule1Hour)
- `0 0 * * *` - Every day at midnight UTC (Schedule1Day)
- `0 0 1 * *` - Every month on the 1st at midnight UTC (Schedule1Month)

The service also accepts any valid 5-field cron expression (e.g., `30 14 * * MON-FRI` for weekdays at 2:30 PM UTC)

## Payload Types

Jobs support four payload types:
- `message` - Simple message processing
- `http_request` - HTTP requests (simulated)
- `command` - Command execution (simulated)
- `export` - Red Hat Insights export service integration (production implementation)

## Database to Redis Sync Architecture

The scheduler uses PostgreSQL as the source of truth and Redis as a fast, distributed scheduling index. Workers sync jobs from PostgreSQL to Redis to ensure consistent state across deployments and Redis restarts.

### Startup Sync (On Every Worker Launch)

**When**: Every time a worker pod starts  
**Purpose**: Ensure Redis has current job state from the database  
**Mechanism**: Leader election via Redis to prevent thundering herd

**How it works**:
1. Worker attempts to acquire a distributed lock via `TryAcquireLeader(5 * time.Minute)`
2. Only one worker becomes the "sync leader" (others skip and start polling immediately)
3. Sync leader loads near-due jobs: `FindScheduledNearDue(SCHEDULER_SYNC_LOOKAHEAD_WINDOW)`
   - Default: Jobs due within next 2 hours
   - Filters: `status = 'scheduled' AND next_run_at <= NOW() + lookahead`
   - Sorted by `next_run_at ASC` (earliest due first)
4. Syncs to Redis via `SyncJobsFromDB()` which calls `ScheduleJob()` for each job
5. Records metrics: `scheduler_db_sync_duration_seconds`, `scheduler_db_sync_jobs_loaded`, `scheduler_db_sync_operations_total{operation="startup"}`

**Why sync on every startup** (not just when Redis is empty):
- Refreshes jobs that may have been updated in the database
- Recovers from partial sync failures
- Ensures new workers have fresh data even if Redis has stale jobs
- Safe because `SyncJobsFromDB` is idempotent (uses Redis `SET`/`ZADD` which overwrite)

**Performance**: With default 2h lookahead, a system with 10,000 jobs typically syncs only ~100 near-due jobs in <1 second.

### Periodic Sync (Optional Background Maintenance)

**When**: On a configurable interval (default: 1 hour) if `ENABLE_PERIODIC_SYNC=true`  
**Purpose**: Catch jobs that become near-due between worker restarts, or recover if API pods fail to update Redis  
**Mechanism**: Each worker independently performs sync (no leader election)

**How it works**:
1. Timer fires every `SCHEDULER_DB_TO_REDIS_SYNC_INTERVAL`
2. Loads near-due jobs: `FindScheduledNearDue(SCHEDULER_SYNC_LOOKAHEAD_WINDOW)`
3. Syncs to Redis via `SyncJobsFromDB()`
4. Records same metrics with `operation="periodic"` label

**When to enable**: If workers restart infrequently (e.g., weekly deploys) or you want extra resilience against missed Redis updates.

**Tuning recommendation**: Set `SCHEDULER_SYNC_LOOKAHEAD_WINDOW >= 2x SCHEDULER_DB_TO_REDIS_SYNC_INTERVAL` to avoid gaps where jobs enter the lookahead window between syncs.

### Metrics for Monitoring Sync Health

- `scheduler_db_sync_duration_seconds` (histogram) - How long each sync takes
- `scheduler_db_sync_jobs_loaded` (histogram) - Number of jobs loaded per sync
- `scheduler_db_sync_operations_total{operation,status}` (counter) - Count of syncs by type (startup/periodic) and outcome (success/error)

**Alert if**: p95 duration > 5s, error rate > 5%, or jobs_loaded unexpectedly high (indicates misconfigured lookahead window).

See [Database Sync Metrics Documentation](docs/db-sync-metrics.md) for detailed monitoring guidance.

## Environment Variables

### Scheduler Timing Configuration

**Graceful Shutdown Timeout**:
- Variable: `SCHEDULER_GRACEFUL_SHUTDOWN_TIMEOUT`
- Default: `30s`
- Description: Maximum time to wait for in-flight jobs during shutdown
- Example: `SCHEDULER_GRACEFUL_SHUTDOWN_TIMEOUT=60s`

**Redis Poll Interval**:
- Variable: `SCHEDULER_REDIS_POLL_INTERVAL`
- Default: `10s`
- Description: How often workers check Redis for due jobs
- Example: `SCHEDULER_REDIS_POLL_INTERVAL=5s`

**Database to Redis Sync Interval**:
- Variable: `SCHEDULER_DB_TO_REDIS_SYNC_INTERVAL`
- Default: `1h`
- Description: How often workers sync jobs from PostgreSQL to Redis (requires `ENABLE_PERIODIC_SYNC=true`)
- Example: `SCHEDULER_DB_TO_REDIS_SYNC_INTERVAL=30m`

**Database to Redis Sync Lookahead Window**:
- Variable: `SCHEDULER_SYNC_LOOKAHEAD_WINDOW`
- Default: `2h`
- Description: Time window for syncing near-due jobs from PostgreSQL to Redis. Only jobs with `next_run_at` within this window are loaded during sync operations.
- Example: `SCHEDULER_SYNC_LOOKAHEAD_WINDOW=4h`
- Tuning: Should be >= 2x `SCHEDULER_DB_TO_REDIS_SYNC_INTERVAL` when periodic sync is enabled to avoid gaps. Larger values increase sync time but provide more buffer for worker restarts.
- Performance: With default 2h window, a 10,000-job system might only sync 100 near-due jobs, reducing startup time from 30s to <1s.

**Auto-Pause on Consecutive Failures**:
- Variable: `MAX_CONSECUTIVE_FAILURES`
- Default: `3`
- Description: Number of consecutive failures before a job is automatically paused. Set to `0` to disable auto-pause.
- Example: `MAX_CONSECUTIVE_FAILURES=5`
- Note: When a job fails N consecutive times, it will be automatically paused and will not run again until manually resumed via the `/jobs/{id}/resume` endpoint. The failure counter resets to 0 after any successful execution or when the job is manually resumed.
- Job status while retrying: A failed run does **not** flip the job-level status to `failed`. The job stays `scheduled` (i.e. active and retrying) until it reaches the auto-pause threshold, at which point it becomes `paused`. Failure state is tracked via the `consecutive_failures` and `last_failed_at` fields on the job, and the outcome of each individual run is recorded in that run's `JobRun` record. To detect a job that is failing, check `consecutive_failures > 0` / `last_failed_at` or the run history rather than the job status. (The `failed` job status is retained only for backward compatibility with rows written by older versions; such jobs are still treated as active and heal back to `scheduled` on their next success.)

**Export Poll Scan Interval**:
- Variable: `SCHEDULER_EXPORT_POLL_SCAN_INTERVAL`
- Default: `10s`
- Description: How often the ExportPollerService checks for in-flight export runs that need status polling
- Example: `SCHEDULER_EXPORT_POLL_SCAN_INTERVAL=15s`

**Export Poll Max Age**:
- Variable: `SCHEDULER_EXPORT_POLL_MAX_AGE`
- Default: `30m`
- Description: Maximum time an export run can remain in-flight before it is timed out and marked as failed. Increase this for long-running exports.
- Example: `SCHEDULER_EXPORT_POLL_MAX_AGE=2h`
- Note: Timeout detection is polled every `SCHEDULER_EXPORT_POLL_SCAN_INTERVAL`, so a run is actually timed out up to one scan interval after it exceeds the max age.
- Monitoring: `scheduler_export_poll_timeouts_total` (counter) counts export runs that exceeded the max age and were marked failed — alert on a rising rate. `scheduler_export_in_flight_runs` (gauge) reports the number of in-flight export runs seen in the most recent scan; watch it against `SCHEDULER_MAX_CONCURRENT_EXPORT_POLLS`. Timeouts are also logged at WARN with `job_id`, `org_id`, `user_id`, `export_id`, `age`, and `max_age`. (Distinct from `scheduler_redis_jobs_timed_out_total`, which tracks the separate job kick-off execution timeout.)

**Maximum Concurrent Export Polls**:
- Variable: `SCHEDULER_MAX_CONCURRENT_EXPORT_POLLS`
- Default: `20`
- Description: Maximum number of export status checks that can run concurrently. Controls scalability of export polling - higher values allow more parallel status checks but increase resource usage.
- Example: `SCHEDULER_MAX_CONCURRENT_EXPORT_POLLS=50`
- Tuning: Set based on expected concurrent export volume. Each poll makes identity validation + HTTP request to export service (~200ms total). Default of 20 supports ~100 concurrent exports with 10s scan interval.

**Maximum Concurrent Jobs**:
- Variable: `SCHEDULER_MAX_CONCURRENT_JOBS`
- Default: `10`
- Description: Maximum number of jobs that can execute simultaneously. Prevents resource exhaustion when many jobs are due at the same time.
- Example: `SCHEDULER_MAX_CONCURRENT_JOBS=20`
- Tuning: Consider database connection limits (default max: 25) and downstream service capacity. Monitor `scheduler_concurrent_jobs` and `scheduler_worker_pool_utilization` metrics.

**Job Execution Timeout**:
- Variable: `SCHEDULER_JOB_EXECUTION_TIMEOUT`
- Default: `2m`
- Description: Maximum time allowed for a single job's create/execute phase before timing out. Guards against hung identity validation or export service calls.
- Example: `SCHEDULER_JOB_EXECUTION_TIMEOUT=3m`
- Note: This timeout applies to the create phase (identity validation + CreateExport call). Export completion polling is handled separately by ExportPollerService with its own timeout.
- Covers: Identity validation (~1-30s) + CreateExport (~1-30s) + safety margin

**Job Denylist**:
- Variable: `SCHEDULER_DENYLIST_JOB_IDS`
- Default: Empty (no jobs denied)
- Description: Comma-separated list of job IDs that should not be executed. Denied jobs will be logged but not run.
- Example: `SCHEDULER_DENYLIST_JOB_IDS=job-id-1,job-id-2,job-id-3`
- Whitespace handling: Spaces around job IDs are automatically trimmed (e.g., `job-1, job-2, job-3` works correctly)
- Note: When the scheduler attempts to execute a denied job, it will log a warning message and skip execution (returning success to avoid triggering failure tracking). The job remains in the database and scheduled, but will silently skip execution while on the denylist. Denied jobs do not count as failures and will not auto-pause.

### Database Configuration

- `DB_TYPE`: Database type (`sqlite`, `postgres`)
- `DB_HOST`: Database host (for postgres)
- `DB_PORT`: Database port (default: `5432`)
- `DB_NAME`: Database name (default: `scheduler`)
- `DB_USERNAME`: Database username
- `DB_PASSWORD`: Database password

### Redis Configuration

- `REDIS_ENABLED`: Enable Redis for distributed scheduling (`true`/`false`)
- `REDIS_HOST`: Redis server host
- `REDIS_PORT`: Redis server port (default: `6379`)
- `REDIS_PASSWORD`: Redis authentication password (optional)
- `REDIS_TLS_ENABLED`: Enable TLS for Redis connections (`true`/`false`, default: `false`)
- `REDIS_TLS_CA_FILE`: Path to CA certificate file for verifying the Redis server (optional)
- `REDIS_TLS_CERT_FILE`: Path to client certificate file for mTLS (optional, requires `REDIS_TLS_KEY_FILE`)
- `REDIS_TLS_KEY_FILE`: Path to client private key file for mTLS (optional, requires `REDIS_TLS_CERT_FILE`)
- `REDIS_TLS_INSECURE_SKIP_VERIFY`: Skip TLS certificate verification (`true`/`false`, default: `false`)

### Kafka Configuration

- `KAFKA_BROKERS`: Comma-separated list of Kafka broker addresses
- `KAFKA_TOPIC`: Topic for notifications (default: `platform.notifications.ingress`)
- `KAFKA_SASL_ENABLED`: Enable SASL authentication (`true`/`false`)

### Export Service Configuration

- `EXPORT_SERVICE_URL`: Internal export service API URL (default: `http://export-service-service:8000/api/export/v1`)
- `EXPORT_SERVICE_PUBLIC_URL`: Public-facing export service URL for download links (default: same as `EXPORT_SERVICE_URL`)
  - In production, this should be set to the publicly accessible endpoint (e.g., `https://console.redhat.com/api/export/v1`)
  - Used to generate download URLs sent to users in notifications
- `EXPORT_SERVICE_TIMEOUT`: Timeout for export requests (default: `5m`)
- `EXPORT_SERVICE_MAX_RETRIES`: Maximum retries for failed requests (default: `3`)
- `EXPORT_SERVICE_POLL_MAX_RETRIES`: Maximum polling attempts for export completion (default: `60`)
- `EXPORT_SERVICE_POLL_INTERVAL`: Time between polling attempts (default: `5s`)

### User Validation

- `USER_VALIDATOR_IMPL`: Implementation to use (`fake`, `bop`, `3scale`)
- `BOP_URL`: Back Office Portal API URL
- `BOP_API_TOKEN`: BOP authentication token
- `THREESCALE_URL`: 3scale validation service URL