# Multi-stage build for smaller final image
FROM registry.access.redhat.com/ubi9/go-toolset:1.25.7-1771417345 AS builder

# Set working directory
WORKDIR /opt/app-root/src

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o scheduler cmd/server/main.go
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o kafka-producer cmd/kafka-producer/main.go

# Final stage - minimal runtime image
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

# Install ca-certificates for HTTPS calls
RUN microdnf update -y && \
    microdnf install -y ca-certificates sqlite && \
    microdnf clean all

# Create non-root user
RUN groupadd -r scheduler && useradd -r -g scheduler -u 1001 scheduler

# Create data directory
RUN mkdir -p /data && chown scheduler:scheduler /data

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /opt/app-root/src/scheduler .
COPY --from=builder /opt/app-root/src/kafka-producer .
COPY --from=builder /opt/app-root/src/trigger_swatch_report_gen.sh .
COPY --from=builder /opt/app-root/src/db/migrations db/migrations/

# Change ownership of the binary
RUN chown scheduler:scheduler scheduler

# Switch to non-root user
USER 1001

# Expose port
EXPOSE 5000

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 \
  CMD curl -f http://localhost:5000/api/v1/jobs || exit 1

# Set environment variables
ENV DB_PATH=/data/jobs.db
ENV PORT=5000

# Run the application
CMD ["./scheduler"]
