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
- `inventory_pdf_job_executor.go` - Inventory PDF generator service integration
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

Common predefined schedules (available as constants):
- `*/10 * * * *` - Every 10 minutes (Schedule10Minutes)
- `0 * * * *` - Every hour at minute 0 (Schedule1Hour)
- `0 0 * * *` - Every day at midnight (Schedule1Day)
- `0 0 1 * *` - Every month on the 1st at midnight (Schedule1Month)

The service also accepts any valid 5-field cron expression (e.g., `30 14 * * MON-FRI` for weekdays at 2:30 PM)

## Payload Types

Jobs support five payload types:
- `message` - Simple message processing
- `http_request` - HTTP requests (simulated)
- `command` - Command execution (simulated)
- `export` - Red Hat Insights export service integration (production implementation)
- `inventory-pdf` - Red Hat Insights Inventory PDF generator service integration (production implementation)