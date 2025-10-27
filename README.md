# Insights Scheduler Service

A Go REST API service for programmatic job scheduling using the declarative shell functional core design pattern.

## Features

- **Functional Core** with pure business logic
- **Imperative Shell** for side effects (HTTP, storage, scheduling)
- **CRUD operations** for scheduled jobs
- **Job control endpoints** (run, pause, resume)
- **Strict scheduling intervals** (10m, 1h, 1d, 1mon)
- **Thread-safe in-memory storage**
- **Background job execution**

## Installation

1. Install dependencies:
```bash
go mod tidy
```

2. Build and run the service:
```bash
go run cmd/server/main.go
```

The service will start on `http://localhost:5000` with the scheduler running in the background.

## API Endpoints

### Job Management

- `POST /api/v1/jobs` - Create a new job
- `GET /api/v1/jobs` - Get all jobs (supports ?status= and ?name= filters)
- `GET /api/v1/jobs/{id}` - Get specific job
- `PUT /api/v1/jobs/{id}` - Update job (full replacement)
- `PATCH /api/v1/jobs/{id}` - Partial update job
- `DELETE /api/v1/jobs/{id}` - Delete job

### Job Control

- `POST /api/v1/jobs/{id}/run` - Run job immediately
- `POST /api/v1/jobs/{id}/pause` - Pause job
- `POST /api/v1/jobs/{id}/resume` - Resume paused job

## Job Schema

```json
{
  "id": "string (UUID)",
  "name": "string",
  "schedule": "string (10m|1h|1d|1mon)",
  "payload": {
    "type": "string (message|http_request|command)",
    "details": {}
  },
  "status": "string (scheduled|running|paused|failed)",
  "last_run": "string (ISO timestamp)"
}
```

## Schedule Formats

Supported intervals:
- `10m` - Every 10 minutes
- `1h` - Every 1 hour  
- `1d` - Every 1 day
- `1mon` - Every 1 month

## Testing

Run the test suite:
```bash
go run cmd/test/main.go
```

Make sure the service is running before executing tests.

## Architecture (Functional Core / Imperative Shell)

### Functional Core (`internal/core/`)
- `domain/` - Pure domain models and validation logic
- `usecases/` - Business logic with dependency interfaces

### Imperative Shell (`internal/shell/`)
- `http/` - HTTP handlers and routing
- `storage/` - In-memory repository implementation
- `scheduler/` - Background scheduling
- `executor/` - Job execution logic

### Entry Points (`cmd/`)
- `server/` - Main application server
- `test/` - API test client