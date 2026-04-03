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
- Uses functional core for scheduling decisions

**Executor** (`internal/shell/executor/`):
- `job_executor.go` - Generic job executor with map-based payload type dispatch
- `export_job_executor.go` - Export service integration
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