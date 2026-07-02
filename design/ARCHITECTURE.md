# Scheduler Service Architecture

## Table of Contents
1. [Overview](#overview)
2. [Architecture Pattern](#architecture-pattern)
3. [Project Structure](#project-structure)
4. [Core Domain Models](#core-domain-models)
5. [REST API](#rest-api)
6. [Data Persistence](#data-persistence)
7. [Job Scheduler](#job-scheduler)
8. [Job Executor System](#job-executor-system)
9. [External Integrations](#external-integrations)
10. [Configuration Management](#configuration-management)
11. [Security and Multi-Tenancy](#security-and-multi-tenancy)
12. [Deployment](#deployment)

---

## Overview

The Insights Scheduler is a microservice that provides scheduled job execution for the Red Hat Insights platform. It supports multiple job types with cron-based scheduling, tracking execution history, and integrating with platform services.

### Key Features

- Cron-based job scheduling (5-field format)
- Multiple payload types (Export, HTTP, Message, Command)
- Job execution history tracking
- Organization-based multi-tenancy
- Integration with Red Hat Insights Platform Export Service notifications
- Integration with Red Hat Insights Platform Notifications Service
- PostgreSQL and SQLite support
- Kubernetes/OpenShift deployment via Clowder

### Technology Stack

- **Language**: Go 1.21+
- **HTTP Framework**: Gorilla Mux
- **Cron Engine**: robfig/cron/v3
- **Databases**: SQLite, PostgreSQL
- **Messaging**: Kafka (IBM Sarama)
- **Deployment**: Kubernetes, OpenShift (Clowder)
- **Metrics**: Prometheus

---

## Architecture Pattern

The service follows the **Functional Core / Imperative Shell** pattern for clean separation between business logic and side effects.

### Functional Core (`internal/core/`)

Contains pure business logic with no side effects:

- **Domain Layer** (`domain/`): Immutable value objects and validation functions
  - Job and JobRun models with pure transformation methods
  - Domain error definitions
  - Validation logic (schedule, payload type, status)

- **Use Cases Layer** (`usecases/`): Business rules depending only on interfaces
  - `JobService`: Job management operations (CRUD, pause/resume, execution)
  - `JobRunService`: Execution history management
  - `SchedulingService`: Scheduling calculation logic
  - Interfaces: `JobRepository`, `JobRunRepository`, `JobExecutor`, `CronScheduler`

### Imperative Shell (`internal/shell/`)

Handles all side effects and I/O operations:

- **Storage** (`storage/`): Database implementations
  - `SQLiteJobRepository` / `PostgresJobRepository`
  - `SQLiteJobRunRepository` / `PostgresJobRunRepository`
  - `MemoryRepository` (testing)

- **HTTP** (`http/`): REST API handlers and routing
  - Request/response handling
  - DTO marshaling/unmarshaling
  - Identity middleware integration

- **Scheduler** (`scheduler/`): Background job execution engine
  - Cron-based job polling
  - In-memory job scheduling table
  - Job lifecycle management

- **Executor** (`executor/`): Job execution dispatching
  - Type-based executor dispatch
  - Payload-specific executors
  - Kafka notification system

### Dependency Flow

```
main.go
  └─> Creates Shell Components (Storage, HTTP, Scheduler, Executor)
       └─> Injects into Core (JobService, JobRunService)
            └─> Passed back to Shell (HTTP Handlers, Scheduler)
```

---

## Project Structure

```
scheduler/
├── cmd/                          # Application entry points
│   ├── server/main.go           # Main service startup
│   └── test/main.go             # API integration tests
│
├── internal/
│   ├── core/                    # Functional core (pure logic)
│   │   ├── domain/              # Domain models
│   │   │   ├── job.go
│   │   │   ├── job_run.go
│   │   │   └── errors.go
│   │   └── usecases/            # Business logic
│   │       ├── job_service.go
│   │       ├── job_run_service.go
│   │       └── scheduling.go
│   │
│   ├── shell/                   # Imperative shell (I/O)
│   │   ├── storage/            # Data persistence
│   │   │   ├── sqlite_repository.go
│   │   │   ├── postgres_repository.go
│   │   │   └── memory_repository.go
│   │   ├── http/               # REST API
│   │   │   ├── routes.go
│   │   │   ├── handlers.go
│   │   │   └── dto.go
│   │   ├── scheduler/          # Job scheduler
│   │   │   └── scheduler.go
│   │   ├── executor/           # Job execution
│   │   │   ├── job_executor.go
│   │   │   ├── export_job_executor.go
│   │   │   ├── http_job_executor.go
│   │   │   ├── message_job_executor.go
│   │   │   ├── command_job_executor.go
│   │   │   └── kafka_notifier.go
│   │   └── messaging/          # Kafka integration
│   │       └── kafka_producer.go
│   │
│   ├── clients/                # External service clients
│   │   └── export/             # Export service integration
│   │       ├── client.go
│   │       └── types.go
│   │
│   ├── config/                 # Configuration management
│   │   └── config.go
│   │
│   └── identity/               # User validation
│       ├── validator.go
│       └── bop_validator.go
│
├── Dockerfile                   # Container build
├── Makefile                     # Build automation
└── go.mod                       # Dependencies
```

---

## Core Domain Models

### Job

Represents a scheduled task with cron-based execution.

```go
type Job struct {
    ID                  string      // UUID
    Name                string      // User-friendly name
    OrgID               string      // Organization (multi-tenancy, required)
    UserID              string      // Creator user ID (required, used for authorization)
    Schedule            Schedule    // Cron expression (5-field)
    Timezone            string      // Timezone for schedule interpretation (default: UTC)
    Type                PayloadType // Job execution type
    Payload             interface{} // Type-specific configuration
    Status              JobStatus   // Current state
    LastRunAt           *time.Time  // Last execution timestamp (UTC)
    NextRunAt           *time.Time  // Next scheduled run (UTC)
    ConsecutiveFailures int         // Counter for auto-pause feature
    LastFailedAt        *time.Time  // Timestamp of last failure
}
```

**Job Statuses:**
- `scheduled` - Ready for scheduled execution
- `running` - Currently executing
- `paused` - Manually paused by user
- `failed` - Last execution failed

**Payload Types:**
- `message` - Simple message processing
- `http_request` - HTTP request execution
- `command` - Command execution
- `export` - Red Hat Insights export service integration

**Schedule Format:**

5-field cron expression: `minute hour day-of-month month day-of-week`

Schedules are interpreted in the job's specified timezone, but all timestamps are stored in UTC.

Predefined schedules:
- `*/10 * * * *` - Every 10 minutes
- `0 * * * *` - Every hour
- `0 0 * * *` - Daily at midnight (in job's timezone)
- `0 0 1 * *` - Monthly on the 1st at midnight (in job's timezone)

**Timezone Support:**
- Jobs can specify any valid IANA timezone (e.g., "America/New_York", "Europe/London")
- Defaults to "UTC" if not specified
- Cron schedules are interpreted in the specified timezone
- Next run times are calculated in the job's timezone, then converted to UTC for storage
- Example: A job with schedule `0 9 * * *` and timezone `America/New_York` runs at 9am ET daily

**Design Principles:**
- Immutable value objects
- Pure transformation methods: `WithStatus()`, `WithLastRunAt()`, `WithNextRunAt()`, `WithConsecutiveFailures()`, `UpdateFields()`
- Pure validation: `IsValidSchedule()`, `IsValidPayloadType()`, `IsValidStatus()`, `IsValidTimezone()`

### JobRun

Records execution history for audit and debugging.

```go
type JobRun struct {
    ID           string              // UUID
    JobID        string              // Reference to Job
    Status       JobRunStatus        // Execution state
    StartTime    time.Time           // Execution start (UTC)
    EndTime      *time.Time          // Execution end (UTC, nullable)
    ErrorMessage *string             // Failure reason (nullable)
    Result       *domain.JobResult   // Structured execution result (nullable)
}

type JobResult struct {
    Type ResultType  // export, http_response, message, command, etc.
    Data interface{} // Type-specific result data
}
```

**JobRun Statuses:**
- `running` - Execution in progress
- `completed` - Successful execution
- `failed` - Execution error

**Result Types:**
- `export` - Export service results (export ID, download URL, format, status)
- `http_response` - HTTP response (status code, headers, body snippet)
- `message` - Message processing result
- `command` - Command execution result

**Immutable Updates:**
- `WithCompleted(resultType, data)` - Mark as completed with structured result
- `WithFailed(error)` - Mark as failed with error message

---

## REST API

### Base URL
`/api/scheduler/v1`

All endpoints require Red Hat identity headers for authentication.

### Job Management

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/jobs` | Create new job |
| GET | `/jobs` | List user's jobs (filters: status, name) |
| GET | `/jobs/{id}` | Get specific job |
| PUT | `/jobs/{id}` | Replace entire job |
| PATCH | `/jobs/{id}` | Partial job update |
| DELETE | `/jobs/{id}` | Delete job |

### Job Control

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/jobs/{id}/run` | Execute job immediately |
| POST | `/jobs/{id}/pause` | Pause scheduled execution |
| POST | `/jobs/{id}/resume` | Resume paused job |

### Job Execution History

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/jobs/{id}/runs` | List all runs for a job |
| GET | `/jobs/{id}/runs/{run_id}` | Get specific job run |

### Example: Create Job

**Request:**
```bash
POST /api/scheduler/v1/jobs
Content-Type: application/json
x-rh-identity: <base64-encoded-identity>

{
  "name": "Daily Inventory Export",
  "schedule": "0 0 * * *",
  "type": "export",
  "payload": {
    "name": "Inventory Export",
    "format": "csv",
    "sources": [
      {
        "application": "inventory",
        "resource": "hosts",
        "filters": {}
      }
    ]
  }
}
```

**Response:**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "Daily Inventory Export",
  "schedule": "0 0 * * *",
  "type": "export",
  "payload": { ... },
  "status": "scheduled",
  "last_run": null
}
```

### HTTP Error Responses

| Status | Condition |
|--------|-----------|
| 400 | Invalid request (bad schedule, missing fields) |
| 401 | Missing identity header |
| 403 | Unauthorized (org/user mismatch) |
| 404 | Resource not found |
| 409 | Conflict (job already in target state) |
| 500 | Internal server error |

---

## Data Persistence

### Database Schema

**jobs table:**
```sql
CREATE TABLE jobs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    org_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    schedule TEXT NOT NULL,
    timezone TEXT NOT NULL DEFAULT 'UTC',
    payload_type TEXT NOT NULL,
    payload_details TEXT NOT NULL,  -- JSON
    status TEXT NOT NULL,
    last_run_at TIMESTAMP,
    next_run_at TIMESTAMP,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    last_failed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_jobs_org_id ON jobs(org_id);
CREATE INDEX idx_jobs_status ON jobs(status);
CREATE INDEX idx_jobs_user_id ON jobs(user_id);
CREATE INDEX idx_jobs_next_run_at ON jobs(next_run_at);
```

**Schema Changes:**
- Removed `username` field (redundant, user_id is authoritative)
- Added `timezone` field for schedule interpretation
- Renamed `last_run` to `last_run_at` for clarity
- Added `next_run_at` for efficient scheduling queries
- Added `consecutive_failures` for auto-pause feature
- Added `last_failed_at` for failure tracking
- Added index on `next_run_at` for scheduler performance

**job_runs table:**
```sql
CREATE TABLE job_runs (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    status TEXT NOT NULL,
    start_time TIMESTAMP NOT NULL,
    end_time TIMESTAMP,
    error_message TEXT,
    result JSONB,  -- PostgreSQL: JSONB, SQLite: TEXT (JSON string)
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE
);

CREATE INDEX idx_job_runs_job_id ON job_runs(job_id);
CREATE INDEX idx_job_runs_status ON job_runs(status);
CREATE INDEX idx_job_runs_start_time ON job_runs(start_time);
```

**Schema Changes:**
- Changed `result` from TEXT to JSONB (PostgreSQL) for structured results
- Result contains: `{"type": "export", "data": {"export_id": "...", "download_url": "..."}}`
- Timestamps use TIMESTAMP type instead of TEXT
- Added `DEFAULT CURRENT_TIMESTAMP` for `created_at`

### Repository Pattern

**Interface:**
```go
type JobRepository interface {
    Save(job domain.Job) error
    FindByID(id string) (domain.Job, error)
    FindAll() ([]domain.Job, error)
    FindByOrgID(orgID string) ([]domain.Job, error)
    FindByUserID(userID string) ([]domain.Job, error)
    Delete(id string) error
}
```

**Implementations:**
- `SQLiteJobRepository` - SQLite backend (development, testing)
- `PostgresJobRepository` - PostgreSQL backend (production)
- `MemoryRepository` - In-memory (unit tests)

**Features:**
- UPSERT support (INSERT ... ON CONFLICT)
- JSON payload storage for flexibility
- Schema migrations for evolution
- Connection pooling

---

## Job Scheduler

The service supports two scheduler implementations:

### 1. RedisScheduler (Production - Distributed)

**Location:** `internal/shell/scheduler/redis_scheduler.go`

Uses Redis for distributed scheduling across multiple worker pods.

**Architecture:**

```
Redis Data Structures:
├─ scheduler:jobs:scheduled (Sorted Set)
│  └─ Score: next_run_timestamp, Value: job_id
├─ scheduler:job:{id} (String - JSON)
│  └─ ScheduledJob{Job, NextRun, Schedule, LastUpdate, JobRunID}
├─ scheduler:lock:{id} (String)
│  └─ Distributed lock (TTL: 5 minutes)
└─ scheduler:sync:leader (String)
   └─ Leader election for DB→Redis sync (TTL: 5 minutes)
```

**Execution Flow:**

1. **Polling Loop** (every 10 seconds, configurable)
   ```
   Worker polls Redis → ZRANGEBYSCORE scheduler:jobs:scheduled -inf {now}
                     → Get up to 100 overdue jobs
                     → For each job:
                         → Acquire lock: SET scheduler:lock:{id} 1 NX EX 300
                         → If lock acquired:
                             → Fetch job data from scheduler:job:{id}
                             → Execute job via JobExecutor
                             → Update job.LastRunAt, job.NextRunAt
                             → Save to PostgreSQL
                             → Update Redis with new next_run
                             → Delete lock
   ```

2. **Distributed Locking:**
   - Prevents duplicate execution across workers
   - Lock TTL: 5 minutes (prevents stuck locks if worker crashes)
   - Atomic `SET NX` operation ensures only one worker acquires lock

3. **Startup Sync:**
   - Worker checks if Redis has jobs: `ZCARD scheduler:jobs:scheduled`
   - If empty: attempt leader election via `SETNX scheduler:sync:leader`
   - Leader loads all scheduled jobs from PostgreSQL → Redis
   - Non-leaders skip sync (leader already populated Redis)

4. **Periodic Sync** (optional, hourly)
   - Environment: `ENABLE_PERIODIC_SYNC=true`
   - Interval: `SCHEDULER_DB_TO_REDIS_SYNC_INTERVAL` (default: 1h)
   - Syncs PostgreSQL → Redis to catch missed updates
   - Safety mechanism for Redis failures or race conditions

**Benefits:**
- Horizontal scaling (multiple workers)
- High availability (survives worker crashes)
- No duplicate execution (distributed locks)
- Fast lookups (Redis sorted set)

**Graceful Shutdown:**
- Context cancellation stops polling loop
- In-flight jobs complete before shutdown
- Timeout: `SCHEDULER_GRACEFUL_SHUTDOWN_TIMEOUT` (default: 30s)

### 2. CronScheduler (Legacy - Single Process)

**Location:** `internal/shell/scheduler/scheduler.go`

Uses in-memory `robfig/cron/v3` scheduler. Only for local development or single-process deployments.

**Lifecycle:**

1. **Startup** (`Start(ctx)`)
   - Loads all jobs with status `scheduled`
   - Adds each job to in-memory cron scheduler
   - Starts background goroutine for job execution

2. **Job Execution Flow**
   ```
   Cron fires → Fetch latest job state → Verify status = scheduled
              → JobService.ExecuteScheduledJob()
              → Create JobRun record
              → Executor dispatches to payload type
              → Update JobRun with result/error
   ```

3. **Lifecycle Events**
   - **Resume**: Add job back to cron scheduler
   - **Pause**: Remove job from cron scheduler
   - **Update**: Unschedule old, reschedule with new settings
   - **Delete**: Unschedule and remove from database

**Thread Safety:**
- Uses `sync.RWMutex` for concurrent access to job entry map

**Limitations:**
- Single process only (no horizontal scaling)
- No high availability (if process dies, scheduling stops)
- Not suitable for production at scale

**Cron Expression Support (Both Schedulers):**
- Standard 5-field format: `minute hour day month weekday`
- Timezone-aware: schedules interpreted in job's timezone
- Examples:
  - `*/10 * * * *` - Every 10 minutes
  - `0 9 * * MON-FRI` - Weekdays at 9am (in job's timezone)
  - `30 14 * * *` - Daily at 2:30 PM (in job's timezone)

---

## Job Executor System

### Executor Architecture

```
DefaultJobExecutor
  ├─ Execute(job) / ExecuteWithJobRun(job, jobRunID)
  │   ├─ Create JobRun record (status: running)
  │   ├─ Track in-flight job for graceful shutdown (sync.WaitGroup)
  │   ├─ Increment JobsCurrentlyRunning metric
  │   ├─ Dispatch to type-specific runner via map
  │   ├─ Update JobRun with structured result (status: completed/failed)
  │   ├─ Decrement JobsCurrentlyRunning metric
  │   └─ Return execution error (if any)
  │
  └─ runners map[PayloadType]JobRunner
      ├─ PayloadMessage → MessageJobRunner (simulated)
      ├─ PayloadHTTPRequest → HTTPJobRunner (simulated)
      ├─ PayloadCommand → CommandJobRunner (simulated)
      └─ PayloadExport → ExportJobRunner (production)
```

**JobRunner Interface:**
```go
type JobRunner interface {
    Execute(job domain.Job, logger *slog.Logger) (result interface{}, resultType domain.ResultType, err error)
}
```

**Graceful Shutdown:**
- Executor tracks in-flight jobs using `sync.WaitGroup`
- On shutdown, waits for all running jobs to complete
- Timeout controlled by `SCHEDULER_GRACEFUL_SHUTDOWN_TIMEOUT` (default: 30s)
- Long-running export jobs can run up to 10 minutes before forced termination

### Payload Executors

#### 1. MessageJobExecutor
- Simulated message processing
- Extracts `message` field from payload
- Logs execution and returns success

#### 2. HTTPJobExecutor
- Simulated HTTP request execution
- Extracts `url` and `method` fields
- Logs request details (not actually executed)

#### 3. CommandJobExecutor
- Simulated command execution
- Extracts `command` field from payload
- Logs command (not actually executed)

#### 4. ExportJobRunner (Production Integration)

**Execution Flow:**

1. Generate identity header via `UserValidator` (BOP or 3scale)
2. Marshal payload to `ExportRequest` struct
3. Call `export.Client.CreateExport()` to initiate export
4. Poll `export.Client.GetExportStatus()` until complete
   - Max retries: `EXPORT_SERVICE_POLL_MAX_RETRIES` (default: 60)
   - Poll interval: `EXPORT_SERVICE_POLL_INTERVAL` (default: 5s)
   - Total max duration: ~5 minutes
5. Send completion notification via `JobCompletionNotifier`
   - Success: include export ID, download URL, format
   - Failure: include error message
6. Return structured result with `domain.ResultType = "export"`

**Result Structure:**
```go
ExportResult{
    ExportID:    "export-uuid",
    Status:      "complete",
    DownloadURL: "https://console.redhat.com/api/export/v1/exports/export-uuid/download",
    Format:      "csv",
    CreatedAt:   "2026-07-01T10:00:00Z",
}
```

**Export Payload Structure:**
```json
{
  "name": "Inventory Export",
  "format": "csv",
  "sources": [
    {
      "application": "inventory",
      "resource": "hosts",
      "filters": {
        "os_filter": "RHEL"
      }
    }
  ]
}
```

### Job Completion Notification

**Interface:**
```go
type JobCompletionNotifier interface {
    JobComplete(ctx context.Context, notification Notification) error
}
```

**Implementations:**

1. **NotificationsBasedJobCompletionNotifier**
   - Sends to Kafka platform notifications topic
   - Message format: Platform notification v1.2.0
   - Event type: `export-completed`
   - Includes: export_id, job_id, status, download_url

2. **NullJobCompletionNotifier**
   - No-op for testing/disabled notifications

**Platform Notification Formats:**

1. **Export Completion:**
```json
{
  "version": "v1.2.0",
  "bundle": "rhel",
  "application": "insights-scheduler",
  "event_type": "export-completed",
  "timestamp": "2026-07-01T10:30:00Z",
  "account_id": "000202",
  "org_id": "000101",
  "context": {
    "export_id": "export-123",
    "job_id": "job-456",
    "status": "complete",
    "download_url": "https://console.redhat.com/api/export/v1/exports/export-123/download",
    "format": "csv"
  }
}
```

2. **Job Auto-Paused:**
```json
{
  "version": "v1.2.0",
  "bundle": "rhel",
  "application": "insights-scheduler",
  "event_type": "job-auto-paused",
  "timestamp": "2026-07-01T10:35:00Z",
  "account_id": "",
  "org_id": "000101",
  "context": {
    "job_id": "job-456",
    "job_name": "Daily Export",
    "consecutive_failures": 3,
    "last_error": "export service unavailable"
  }
}
```

---

## External Integrations

### Export Service Client

**REST API Operations:**
```go
CreateExport(ctx, request, identityHeader) → ExportStatusResponse
GetExportStatus(ctx, exportID, identityHeader) → ExportStatusResponse
DownloadExport(ctx, exportID, identityHeader) → []byte
ListExports(ctx, params, identityHeader) → ExportListResponse
DeleteExport(ctx, exportID, identityHeader) → error
```

**Export Types:**
- Format: JSON, CSV
- Status: pending, running, partial, complete, failed
- Sources: Multiple Red Hat Insights applications (inventory, advisor, compliance, etc.)

**Request Tracing:**
- Generates unique `x-insights-request-id` per request
- Includes `x-rh-identity` header (base64-encoded identity)
- Timeout: 5 seconds per request

### Identity Validation

**Three Implementations:**

1. **FakeUserValidator** (Development)
   - Generates identity header locally
   - Hard-coded account number and org ID
   - No external service calls
   - Used when `USER_VALIDATOR_IMPL=fake`

2. **BopUserValidator** (Production - Back Office Portal)
   - Calls Back Office Portal (BOP) service
   - Validates users via `/v1/users` endpoint
   - Includes API token and client ID headers
   - Environment: `BOP_URL`, `BOP_API_TOKEN`, `BOP_CLIENT_ID`, `BOP_INSIGHTS_ENV`
   - Used when `USER_VALIDATOR_IMPL=bop`

3. **3ScaleUserValidator** (Production - API Gateway)
   - Calls 3scale API gateway for user validation
   - Environment: `THREESCALE_URL`
   - Used when `USER_VALIDATOR_IMPL=3scale`

**Identity Header Structure:**
```json
{
  "identity": {
    "account_number": "000202",
    "org_id": "000101",
    "type": "User",
    "auth_type": "jwt-auth",
    "user": {
      "username": "jdoe",
      "user_id": "user-123",
      "email": "jdoe@example.com"
    },
    "internal": {
      "org_id": "000101"
    }
  }
}
```

### Kafka Integration

**KafkaProducer Features:**
- Library: IBM/Sarama (Go Kafka client)
- SASL authentication: PLAIN, SCRAM-SHA-256, SCRAM-SHA-512
- TLS encryption with configurable certificates
- Compression: Snappy, gzip, lz4, zstd
- Required ACKs: All replicas (default)

**Configuration:**
```go
type KafkaConfig struct {
    Brokers       []string  // Kafka broker addresses
    Topic         string    // Platform notifications topic
    SASLMechanism string    // SASL mechanism
    SASLUsername  string    // SASL username
    SASLPassword  string    // SASL password
    TLSEnabled    bool      // Enable TLS
    TLSCert       string    // TLS certificate path
    Compression   string    // Compression algorithm
}
```

---

## Configuration Management

### Config Sources (Priority Order)

1. **Clowder** (OpenShift/Kubernetes)
   - Auto-detected via `clowder.IsClowderEnabled()`
   - Provides database, Kafka, metrics configuration
   - Environment variable: `ACG_CONFIG` (JSON file path)

2. **Environment Variables** (Fallback)

**Server Configuration:**
```bash
PORT=8000                    # HTTP server port
HOST=localhost              # HTTP server host
PRIVATE_PORT=8080           # Metrics port
```

**Database Configuration:**
```bash
DB_TYPE=postgres            # Database type (postgres|sqlite)
DB_HOST=localhost           # PostgreSQL host
DB_PORT=5432                # PostgreSQL port
DB_NAME=scheduler           # PostgreSQL database
DB_USERNAME=postgres        # PostgreSQL user
DB_PASSWORD=password        # PostgreSQL password
DB_PATH=./jobs.db           # SQLite path (only if DB_TYPE=sqlite)
```

**Redis Configuration:**
```bash
REDIS_ENABLED=true          # Enable Redis-based distributed scheduling
REDIS_HOST=localhost        # Redis server host
REDIS_PORT=6379             # Redis server port
REDIS_PASSWORD=             # Redis password (optional)
```

**Kafka Configuration:**
```bash
KAFKA_BROKERS=localhost:9092
KAFKA_TOPIC=platform.notifications.ingress
KAFKA_SASL_MECHANISM=SCRAM-SHA-512
KAFKA_SASL_USERNAME=user
KAFKA_SASL_PASSWORD=password
KAFKA_TLS_ENABLED=true
```

**Export Service Configuration:**
```bash
EXPORT_SERVICE_URL=http://export-service-service:8000/api/export/v1
EXPORT_SERVICE_PUBLIC_URL=https://console.redhat.com/api/export/v1  # Public URL for download links
EXPORT_SERVICE_TIMEOUT=5m
EXPORT_SERVICE_POLL_INTERVAL=5s
EXPORT_SERVICE_POLL_MAX_RETRIES=60  # Max ~5 minutes of polling
EXPORT_SERVICE_MAX_RETRIES=3         # API request retries
```

**Identity Configuration:**
```bash
USER_VALIDATOR_IMPL=bop     # bop|3scale|fake
BOP_URL=http://bop-service:8000/v1
BOP_API_TOKEN=secret
BOP_CLIENT_ID=insights-scheduler
BOP_INSIGHTS_ENV=prod
THREESCALE_URL=http://3scale-service:8000
```

**Scheduler Configuration:**
```bash
SCHEDULER_GRACEFUL_SHUTDOWN_TIMEOUT=30s
SCHEDULER_REDIS_POLL_INTERVAL=10s
SCHEDULER_DB_TO_REDIS_SYNC_INTERVAL=1h
MAX_CONSECUTIVE_FAILURES=3  # Set to 0 to disable auto-pause
ENABLE_PERIODIC_SYNC=true   # Enable hourly DB→Redis sync
```

**Logging Configuration:**
```bash
LOG_LEVEL=info              # debug|info|warn|error
LOG_FORMAT=json             # json|text
```

### Config Structure

```go
type Config struct {
    Server                    ServerConfig
    Database                  DatabaseConfig
    Kafka                     KafkaConfig
    Metrics                   MetricsConfig
    ExportService             ExportServiceConfig
    Bop                       BopConfig
    UserValidatorImpl         string
    JobCompletionNotifierImpl string
}
```

---

## Security and Multi-Tenancy

### Authentication

All API requests require the `X-Rh-Identity` header containing base64-encoded identity information:

```json
{
  "identity": {
    "account_number": "000202",
    "org_id": "000101",
    "user": {
      "username": "jdoe",
      "user_id": "user-123",
      "email": "jdoe@example.com"
    },
    "type": "User"
  }
}
```

**Identity Middleware:**
- Package: `github.com/redhatinsights/platform-go-middlewares/v2/identity`
- Applied to all `/api/scheduler/v1/*` routes via `identity.EnforceIdentity`
- Validates header format and extracts identity into request context
- Returns 400 Bad Request if header is missing or invalid
- Extracts: `org_id`, `user_id`, `username`, `email`, `account_number`

### Authorization Model

The service implements **user-scoped authorization** with a two-layer approach:

**Layer 1: AuthorizedJobServiceAdapter** (`internal/core/usecases/authorized_adapter.go`)
- Adapts identity-aware interface to core service interface
- Extracts `org_id` and `user_id` from identity context
- Prevents users from spoofing identity fields via request payloads
- All HTTP handlers use this adapter exclusively

**Layer 2: Core Service Authorization** (`internal/core/usecases/job_service.go`)
- `GetJobWithUserCheck(id, userID)` - Verifies job belongs to user
- `GetJobsByUserID(userID)` - Returns only user's jobs
- `DeleteJobWithUserCheck(id, userID)` - Requires ownership
- `PauseJobWithUserCheck(id, userID)` - Requires ownership
- `ResumeJobWithUserCheck(id, userID)` - Requires ownership
- `PatchJobWithUserCheck(id, userID, updates)` - Requires ownership
- `RunJobWithUserCheck(id, userID)` - Requires ownership

**User Isolation:**
- Jobs are scoped to `user_id` (not just `org_id`)
- Users can only access their own jobs
- Cross-user access attempts return `domain.ErrJobNotFound` (404)
- 404 responses prevent information leakage (instead of 403 Forbidden)

**Database Layer:**
- Repository method: `FindByUserID(userID string, offset, limit int)`
- Database indexes on `user_id` column for performance
- All job queries filtered by authenticated user's ID

**Identity Extraction:**
- `org_id`: `ident.Identity.OrgID`
- `user_id`: `ident.Identity.User.UserID`
- These values are immutable once extracted from the identity header
- Jobs store both for audit purposes, but authorization uses `user_id`

**Authorization Flow:**
```
1. HTTP Request with X-Rh-Identity header
2. Identity middleware validates and extracts identity → request context
3. HTTP handler calls AuthorizedJobService with identity
4. AuthorizedJobServiceAdapter extracts user_id from identity
5. Core JobService enforces user_id-based authorization
6. Repository filters/validates against user_id
7. Response returns only user's data or 404 if not found
```

---

## Deployment

### Build Commands

```bash
# Development
make dev                    # Run server in development mode
make test                   # Run unit tests

# Production
make build                  # Compile binary
make build-prod            # Static binary for containers

# Docker
make docker-build          # Build container image
make docker-compose-up     # Start test environment
```

### Container Support

**Dockerfile:** Multi-stage build for minimal image size

**Docker Compose:**
- `docker-compose.yml` - Development environment
- `docker-compose.test.yml` - Testing with dependencies

**Kubernetes:**
- Manifests in `k8s/` directory
- OpenShift templates in `openshift/` directory
- Clowder integration for platform deployment

### Deployment Commands

**Production Deployment:**

The service supports three deployment modes via subcommands:

1. **API Server** (`./scheduler api`)
   - Runs HTTP API on port 5000
   - Handles job CRUD operations
   - Writes to PostgreSQL and Redis
   - Stateless, horizontally scalable

2. **Worker** (`./scheduler worker`)
   - Polls Redis for due jobs
   - Executes jobs via JobExecutor
   - Writes job runs to PostgreSQL
   - Updates Redis with next run times
   - Stateless, horizontally scalable

3. **Server** (`./scheduler` or `./scheduler server`)
   - Runs both API and Worker in single process
   - For local development or small deployments
   - Not recommended for production at scale

**Kubernetes Deployment:**
```bash
# Build container images
docker build -f Dockerfile.api -t scheduler-api:latest .
docker build -f Dockerfile.worker -t scheduler-worker:latest .

# Deploy via Clowder (Red Hat App-SRE)
bonfire deploy scheduler --ref-env insights-production
```

### Dependency Injection (cmd/server/main.go)

**Initialization Order:**

1. Load configuration (Clowder or environment variables)
2. Initialize logger (structured JSON logger)
3. Initialize storage (PostgreSQL or SQLite)
4. Initialize user validator (BOP, 3scale, or fake)
5. Initialize Kafka producer
6. Initialize job completion notifier
7. Initialize job runners (export, http, message, command)
8. Initialize job executor with runner map
9. Initialize core services (JobService, JobRunService)
10. Initialize scheduler (RedisScheduler or CronScheduler)
11. Create authorized job service adapter
12. Wire services together
13. Setup HTTP routes with identity middleware
14. Start metrics server (private port 8080)
15. Start HTTP server (public port 5000)
16. Start background scheduler (worker mode only)
17. Setup graceful shutdown handlers

**Graceful Shutdown:**
- Captures SIGINT/SIGTERM signals
- Stops background scheduler
- Waits for HTTP server shutdown (with timeout)
- Closes database connections
- Closes Kafka producer

### Metrics and Observability

**Prometheus Metrics:**
- Endpoint: `/metrics` on private port (default: 8080)
- Namespace: `scheduler`

**Executor Metrics:**
- `scheduler_jobs_executed_total{status="success|failure"}` - Counter of job executions
- `scheduler_jobs_currently_running` - Gauge of in-flight jobs
- `scheduler_jobs_auto_paused_total` - Counter of auto-paused jobs
- `scheduler_jobs_consecutive_failures` - Gauge per job

**Identity Validation Metrics:**
- `scheduler_identity_validation_total{validator="bop|3scale|fake", status="success|failure"}` - Counter
- `scheduler_identity_validation_duration_seconds{validator}` - Histogram

**HTTP Metrics:**
- Standard Go HTTP server metrics via Prometheus

**Structured Logging:**
- Package: `log/slog` (Go standard library)
- Format: JSON (CloudWatch-compatible)
- Handler: `slog.NewJSONHandler` with configurable level

**Log Context Fields:**
- `job_id` - Job UUID
- `job_run_id` - Job run UUID
- `org_id` - Organization ID
- `user_id` - User ID
- `request_id` - HTTP request ID (from header)
- `method` - HTTP method
- `path` - HTTP path
- `status` - HTTP status code

**Log Levels:**
- `DEBUG` - Detailed execution flow (disabled in production)
- `INFO` - Job executions, API requests, lifecycle events
- `WARN` - Validation failures, retryable errors
- `ERROR` - Execution failures, unrecoverable errors

**Example Log Entry:**
```json
{
  "time": "2026-07-01T10:30:00.123Z",
  "level": "INFO",
  "msg": "Job execution completed",
  "job_id": "job-uuid",
  "job_run_id": "run-uuid",
  "org_id": "000101",
  "user_id": "user-123",
  "status": "completed",
  "duration_ms": 1234
}
```

---

## Error Handling

### Domain Errors

```go
ErrJobNotFound              // Job does not exist
ErrInvalidSchedule         // Invalid cron expression
ErrInvalidPayload          // Payload validation failed
ErrInvalidStatus           // Invalid status value
ErrInvalidOrgID            // Missing or invalid org_id
ErrJobAlreadyPaused        // Job is already paused
ErrJobNotPaused            // Job is not paused
ErrJobRunNotFound          // Job run does not exist
ErrInvalidRunStatus        // Invalid run status value
```

### Validation Strategy

- **Schedule**: 5-field cron expression validation via `robfig/cron`
- **PayloadType**: Enum validation against defined types
- **Status**: Enum validation with state machine rules
- **Payload**: Type-specific unmarshaling into domain structs
- **Identity**: Required headers validated by middleware

---

## Key Design Decisions

1. **Functional Core / Imperative Shell**: Separates pure business logic from side effects, enabling testability and maintainability

2. **Immutable Domain Objects**: Jobs use value semantics with transformation methods, preventing accidental state corruption

3. **Repository Pattern**: Abstracts storage layer, allowing multiple database implementations without changing business logic

4. **Type-Based Executor Dispatch**: Extensible via map-based dispatch - new payload types can be added without modifying dispatcher

5. **Cron-Based Polling**: Simpler than message queue approach, suitable for scheduled tasks with predictable execution times

6. **Organization-Based Multi-Tenancy**: org_id is a first-class concept enforced at API and database layers

7. **Clowder Configuration**: Kubernetes-native configuration for Red Hat deployment platforms

8. **Kafka for Notifications**: Decouples job completion from notification delivery, supports message queuing and retention

9. **Pure Validation Functions**: All validation is pure and deterministic, enabling easier testing and reasoning

10. **Interface Segregation**: Core depends only on minimal interfaces, allowing flexible implementations

---

## Future Considerations

### Potential Enhancements

- **Distributed Scheduling**: Use leader election for multi-instance deployments
- **Job Retry Logic**: Automatic retry with exponential backoff for failed jobs
- **Job Dependencies**: Allow jobs to depend on other jobs
- **Job Priorities**: Priority-based execution ordering
- **Rate Limiting**: Prevent excessive job execution
- **Structured Logging**: Adopt structured logging framework (e.g., logrus, zap)
- **Tracing**: Distributed tracing with OpenTelemetry
- **Health Checks**: Kubernetes liveness/readiness probes
- **Job Templates**: Reusable job templates with variables
- **Job Chains**: Sequential job execution (workflows)

### Scalability Considerations

- **Database Connection Pooling**: Already implemented for PostgreSQL
- **Horizontal Scaling**: Requires distributed lock for scheduler
- **Job Partitioning**: Shard jobs by org_id for scaling
- **Caching**: Cache frequently accessed jobs in memory
- **Async Job Creation**: Queue job creation for high throughput

---

This architecture provides a solid foundation for a production-ready scheduler service with clean separation of concerns, extensibility, and integration with the Red Hat Insights platform.
