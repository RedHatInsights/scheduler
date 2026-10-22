# Multi-stage build for Kafka Producer
FROM registry.access.redhat.com/hi/go:latest-fips-builder@sha256:0a9d37491af574f846513100a1a50c4f98fdebc315333d79fa48206d680a6138 AS builder

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
FROM registry.access.redhat.com/hi/go:latest-fips@sha256:e296272755a1e72503619fddc84cae3b3990f87f1cf34fccf5b2cfc9d591834d

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
