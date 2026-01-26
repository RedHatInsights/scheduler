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
- Integration with Red Hat Export Service
- Kafka-based notifications
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
    ID       string      // UUID
    Name     string      // User-friendly name
    OrgID    string      // Organization (multi-tenancy)
    Username string      // Creator username
    UserID   string      // Creator user ID
    Schedule Schedule    // Cron expression (5-field)
    Type     PayloadType // Job execution type
    Payload  interface{} // Type-specific configuration
    Status   JobStatus   // Current state
    LastRun  *time.Time  // Last execution timestamp
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

Predefined schedules:
- `*/10 * * * *` - Every 10 minutes
- `0 * * * *` - Every hour
- `0 0 * * *` - Daily at midnight
- `0 0 1 * *` - Monthly on the 1st

**Design Principles:**
- Immutable value objects
- Pure transformation methods: `WithStatus()`, `WithLastRun()`, `UpdateFields()`
- Pure validation: `IsValidSchedule()`, `IsValidPayloadType()`, `IsValidStatus()`

### JobRun

Records execution history for audit and debugging.

```go
type JobRun struct {
    ID           string       // UUID
    JobID        string       // Reference to Job
    Status       JobRunStatus // Execution state
    StartTime    time.Time    // Execution start
    EndTime      *time.Time   // Execution end (nullable)
    ErrorMessage *string      // Failure reason (nullable)
    Result       *string      // Execution result (nullable)
}
```

**JobRun Statuses:**
- `running` - Execution in progress
- `completed` - Successful execution
- `failed` - Execution error

**Immutable Updates:**
- `WithCompleted(result)` - Mark as completed
- `WithFailed(error)` - Mark as failed

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
    username TEXT NOT NULL,
    user_id TEXT NOT NULL,
    schedule TEXT NOT NULL,
    payload_type TEXT NOT NULL,
    payload_details TEXT NOT NULL,  -- JSON
    status TEXT NOT NULL,
    last_run DATETIME,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_jobs_org_id ON jobs(org_id);
CREATE INDEX idx_jobs_status ON jobs(status);
CREATE INDEX idx_jobs_user_id ON jobs(user_id);
```

**job_runs table:**
```sql
CREATE TABLE job_runs (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    status TEXT NOT NULL,
    start_time TEXT NOT NULL,
    end_time TEXT,
    error_message TEXT,
    result TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE
);

CREATE INDEX idx_job_runs_job_id ON job_runs(job_id);
CREATE INDEX idx_job_runs_status ON job_runs(status);
CREATE INDEX idx_job_runs_start_time ON job_runs(start_time);
```

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

### CronScheduler Architecture

The scheduler uses `robfig/cron/v3` for cron-based job execution.

**Lifecycle:**

1. **Startup** (`Start(ctx)`)
   - Loads all jobs with status `scheduled` or `failed`
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
- Safe for multi-goroutine execution

**Cron Expression Support:**
- Standard 5-field format: `minute hour day month weekday`
- Examples:
  - `*/10 * * * *` - Every 10 minutes
  - `0 9-17 * * MON-FRI` - Weekdays 9am-5pm
  - `30 14 * * MON-FRI` - Weekdays at 2:30 PM

---

## Job Executor System

### Executor Architecture

```
DefaultJobExecutor
  ├─ Execute(job)
  │   ├─ Create JobRun record (status: running)
  │   ├─ Dispatch to type-specific executor via map
  │   ├─ Update JobRun with result (status: completed/failed)
  │   └─ Update Job.LastRun timestamp
  │
  └─ executors map[PayloadType]PayloadExecutor
      ├─ PayloadMessage → MessageJobExecutor
      ├─ PayloadHTTPRequest → HTTPJobExecutor
      ├─ PayloadCommand → CommandJobExecutor
      └─ PayloadExport → ExportJobExecutor
```

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

#### 4. ExportJobExecutor (Production Integration)

**Execution Flow:**

1. Generate identity header via `UserValidator`
2. Marshal payload to `ExportRequest` struct
3. Call `export.Client.CreateExport()` to initiate export
4. Poll `export.Client.GetExportStatus()` until complete
   - Max retries: configurable (default: 120)
   - Poll interval: configurable (default: 5s)
5. Send completion notification via `JobCompletionNotifier`
   - Success: include download URL
   - Failure: include error message
6. Return result to executor

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

**Platform Notification Format:**
```json
{
  "version": "v1.2.0",
  "bundle": "rhel",
  "application": "insights-scheduler",
  "event_type": "export-completed",
  "timestamp": "2026-01-26T10:30:00Z",
  "account_id": "000202",
  "org_id": "000101",
  "context": {
    "export_id": "export-123",
    "job_id": "job-456",
    "status": "complete",
    "download_url": "http://export-service/exports/export-123"
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

**Two Implementations:**

1. **FakeUserValidator** (Development)
   - Generates identity header locally
   - Hard-coded account number
   - No external service calls
   - Used when `USER_VALIDATOR_IMPL=fake`

2. **BopUserValidator** (Production)
   - Calls Back Office Portal (BOP) service
   - Validates users via `/v1/users` endpoint
   - Includes API token and client ID headers
   - Used when `USER_VALIDATOR_IMPL=bop`

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
DB_TYPE=sqlite              # Database type (sqlite|postgres)
DB_PATH=./jobs.db          # SQLite path
DB_HOST=localhost          # PostgreSQL host
DB_PORT=5432               # PostgreSQL port
DB_NAME=scheduler          # PostgreSQL database
DB_USER=postgres           # PostgreSQL user
DB_PASSWORD=password       # PostgreSQL password
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
EXPORT_SERVICE_URL=http://export-service:8000
EXPORT_SERVICE_TIMEOUT=300s
EXPORT_SERVICE_POLL_INTERVAL=5s
EXPORT_SERVICE_POLL_MAX_RETRIES=120
```

**Identity Configuration:**
```bash
USER_VALIDATOR_IMPL=bop     # bop|fake
BOP_URL=http://bop-service:8000
BOP_API_TOKEN=secret
BOP_CLIENT_ID=insights-scheduler
BOP_INSIGHTS_ENV=prod
```

**Notification Configuration:**
```bash
JOB_COMPLETION_NOTIFIER_IMPL=notifications  # notifications|null
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

### Organization Isolation

- **org_id** required for all job operations
- API endpoints enforce org_id verification
- Jobs filtered by authenticated user's org_id
- Prevents cross-organization information leakage

### User Identity Tracking

- **username**: Who created/updated the job
- **user_id**: Unique user identifier
- Extracted from Red Hat identity headers
- Used for permission checks and audit trails

### Identity Middleware

- From `platform-go-middlewares/v2/identity`
- Validates Red Hat identity headers on all API endpoints
- Extracts: org_id, user_id, username, email
- Returns 400 Bad Request if missing
- Injects identity context into request

### Authorization Model

- Job creation: Requires valid org_id and user_id
- Job listing: Returns only jobs for authenticated org
- Job updates: Verifies job belongs to authenticated org
- Job deletion: Verifies job ownership

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

### Dependency Injection (main.go)

**Initialization Order:**

1. Load configuration (Clowder or environment)
2. Initialize storage (SQLite or PostgreSQL)
3. Initialize user validator (BOP or fake)
4. Initialize scheduling service
5. Initialize Kafka producer
6. Initialize job completion notifier
7. Initialize payload executors
8. Initialize job executor with executor map
9. Initialize core services (JobService, JobRunService)
10. Initialize cron scheduler
11. Wire services together
12. Setup HTTP routes
13. Start HTTP server and metrics server
14. Start background scheduler
15. Setup graceful shutdown

**Graceful Shutdown:**
- Captures SIGINT/SIGTERM signals
- Stops background scheduler
- Waits for HTTP server shutdown (with timeout)
- Closes database connections
- Closes Kafka producer

### Metrics and Observability

**Prometheus Metrics:**
- Endpoint: `/metrics` (configurable port)
- Namespace: `insights`
- Subsystem: `scheduler`
- Standard Go runtime metrics

**Logging:**
- Standard Go `log` package
- Debug logs: Prefixed with `[DEBUG]`
- Context: Function name, operation, status
- No structured logging framework (simple text logs)

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
