# Multi-stage build for Kafka Producer
FROM registry.access.redhat.com/hi/go:latest-fips-builder@sha256:56051f722eacb34ce7e9c8cec4f5ff2eef10e5f0424a464468b0b35ee76c3c60 AS builder

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

# Build the producer
RUN if [ -f /cachi2/cachi2.env ]; then source /cachi2/cachi2.env; fi && \
    CGO_ENABLED=1 GOOS=linux go build -o kafka-producer ./cmd/kafka-producer/

# Final stage
FROM registry.access.redhat.com/hi/go:latest-fips@sha256:6529d1b9f359d6804d9b069518529a48848d458291ab68da0ea75fa73cce0eb3

WORKDIR /app

# Copy binary from builder
COPY --from=builder /opt/app-root/src/kafka-producer .

# Copy examples and documentation
COPY --from=builder /opt/app-root/src/cmd/kafka-producer/README.md .
COPY --from=builder /opt/app-root/src/cmd/kafka-producer/examples.sh .

# Make scripts executable
RUN chmod +x kafka-producer examples.sh

# Switch to non-root user
USER 1001

# Default command
ENTRYPOINT ["./kafka-producer"]

# Default arguments
CMD ["-help"]
