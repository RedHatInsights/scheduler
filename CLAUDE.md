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
- `memory_repository.go` - Thread-safe in-memory storage implementation
- Implements `JobRepository` interface from core

**HTTP** (`internal/shell/http/`):
- `handlers.go` - HTTP request/response handling
- `routes.go` - Route definitions using Gorilla Mux
- All JSON marshaling/unmarshaling happens here

**Scheduler** (`internal/shell/scheduler/`):
- `scheduler.go` - Background goroutine that polls for jobs
- Uses functional core for scheduling decisions

**Executor** (`internal/shell/executor/`):
- `job_executor.go` - Simulated job execution with logging

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

## Schedule Format Restrictions

The service only accepts these exact schedule strings:
- `10m` - Every 10 minutes
- `1h` - Every 1 hour  
- `1d` - Every 1 day
- `1mon` - Every 1 month

## Payload Types

Jobs support three payload types:
- `message` - Simple message processing
- `http_request` - HTTP requests (simulated)
- `command` - Command execution (simulated)