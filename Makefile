# Go parameters
GOCMD=/home/dehort/dev/go/distros/go1.24.5/bin/go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=$(GOCMD) fmt

# Binary names
BINARY_NAME=scheduler
BINARY_PATH=bin/$(BINARY_NAME)
TEST_BINARY=test-client

# Build targets
.PHONY: all build clean test deps fmt vet run test-api help

# Default target
all: clean deps fmt vet test build

# Build the server binary
build:
	@echo "Building server..."
	@mkdir -p bin
	$(GOBUILD) -o $(BINARY_PATH) cmd/server/main.go

# Build test client
build-test:
	@echo "Building test client..."
	@mkdir -p bin
	$(GOBUILD) -o bin/$(TEST_BINARY) cmd/test/main.go

# Build export CLI client
build-export-cli:
	@echo "Building export CLI client..."
	@mkdir -p bin
	$(GOBUILD) -o bin/export-cli cmd/export-cli/main.go

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	@rm -rf bin/

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

# Install dependencies
deps:
	@echo "Installing dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) ./...

# Run go vet
vet:
	@echo "Running go vet..."
	$(GOCMD) vet ./...

# Run the server
run: build
	@echo "Starting server..."
	./$(BINARY_PATH)

# Run the server with custom database path
run-db: build
	@echo "Starting server with custom database..."
	DB_PATH=./custom_jobs.db ./$(BINARY_PATH)

# Run API tests (requires server to be running)
test-api: build-test
	@echo "Running API tests..."
	@echo "Make sure the server is running on localhost:5000"
	./bin/$(TEST_BINARY)

# Run server in development mode
dev:
	@echo "Starting server in development mode..."
	$(GOCMD) run cmd/server/main.go

# Run test client in development mode
test-dev:
	@echo "Running test client in development mode..."
	$(GOCMD) run cmd/test/main.go

# Run both server and tests (in separate terminals)
demo: build build-test
	@echo "Demo setup complete!"
	@echo "Run 'make run' in one terminal and 'make test-api' in another"

# Check code quality
check: fmt vet test
	@echo "Code quality checks passed!"

# Production build with optimizations
build-prod:
	@echo "Building for production..."
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=linux $(GOBUILD) -a -installsuffix cgo -ldflags '-extldflags "-static"' -o $(BINARY_PATH) cmd/server/main.go

# Docker targets
docker-build:
	@echo "Building Docker image..."
	docker build -t insights-scheduler .

docker-run: docker-build
	@echo "Running Docker container..."
	docker run -p 5000:5000 --name scheduler-container insights-scheduler

docker-compose-up:
	@echo "Starting with docker-compose..."
	docker-compose up --build

docker-compose-down:
	@echo "Stopping docker-compose services..."
	docker-compose down

docker-clean:
	@echo "Cleaning Docker artifacts..."
	docker container rm -f scheduler-container 2>/dev/null || true
	docker image rm insights-scheduler 2>/dev/null || true
	docker-compose down --rmi all --volumes --remove-orphans 2>/dev/null || true

# Show help
help:
	@echo "Available targets:"
	@echo "  all                 - Run full build pipeline (clean, deps, fmt, vet, test, build)"
	@echo "  build               - Build server binary"
	@echo "  build-test          - Build test client binary"
	@echo "  build-export-cli    - Build export CLI client binary"
	@echo "  clean               - Clean build artifacts"
	@echo "  test                - Run unit tests"
	@echo "  deps                - Install/update dependencies"
	@echo "  fmt                 - Format Go code"
	@echo "  vet                 - Run go vet"
	@echo "  run                 - Build and run server"
	@echo "  run-db              - Build and run server with custom database"
	@echo "  test-api            - Build and run API tests"
	@echo "  dev                 - Run server in development mode"
	@echo "  test-dev            - Run test client in development mode"
	@echo "  demo                - Build both binaries for demo"
	@echo "  check               - Run code quality checks"
	@echo "  build-prod          - Build optimized production binary"
	@echo "  docker-build        - Build Docker image"
	@echo "  docker-run          - Build and run Docker container"
	@echo "  docker-compose-up   - Start with docker-compose"
	@echo "  docker-compose-down - Stop docker-compose services"
	@echo "  docker-clean        - Clean Docker artifacts"
	@echo "  help                - Show this help message"

ugh:
	cd cmd/worker/ && go build -o worker
	cd cmd/api/ && go build -o api
	cd cmd/scheduler/ && go build -o scheduler

