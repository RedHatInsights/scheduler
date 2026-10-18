# Multi-stage build for smaller final image
FROM registry.access.redhat.com/hi/go:latest-fips-builder@sha256:2a5ae43ae03303cd0858de802ae61f424a3aa5baa482fa013274a36a505a95c2 AS builder

USER 0

# Set working directory
WORKDIR /opt/app-root/src

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN if [ -f /cachi2/cachi2.env ]; then source /cachi2/cachi2.env; fi && \
    go mod download

# Copy source code
COPY . .

# Build the application binaries
# Main scheduler binary (supports: server, api, worker, db_migration subcommands)
# Kafka producer utility
RUN if [ -f /cachi2/cachi2.env ]; then source /cachi2/cachi2.env; fi && \
    CGO_ENABLED=1 GOOS=linux go build -o scheduler cmd/server/main.go && \
    CGO_ENABLED=1 GOOS=linux go build -o kafka-producer cmd/kafka-producer/main.go

# Install runtime dependencies in builder (runtime image has no package manager)
RUN dnf5 install -y ca-certificates sqlite && \
    dnf5 clean all

# Final stage - minimal runtime image
FROM registry.access.redhat.com/hi/go:latest-fips@sha256:53c5db665d6a0b23ad5316abe8cdc29000a48c544143ccb5634c62dbed7400ef

# Copy runtime dependencies from builder
COPY --from=builder /usr/lib64/libsqlite3* /usr/lib64/

# Create data directory
RUN mkdir -p /data && chown 1001:0 /data

# Set working directory
WORKDIR /app

# Copy binaries from builder stage
COPY --from=builder /opt/app-root/src/scheduler .
COPY --from=builder /opt/app-root/src/kafka-producer .
COPY --from=builder /opt/app-root/src/trigger_swatch_report_gen.sh .
COPY --from=builder /opt/app-root/src/db/migrations db/migrations/

# Switch to non-root user
USER 1001

# Expose port
EXPOSE 5000

# Set environment variables
ENV DB_PATH=/data/jobs.db
ENV PORT=5000

# Run the application
CMD ["./scheduler"]
