# Multi-stage build for Kafka Producer
FROM registry.access.redhat.com/hi/go:latest-fips-builder@sha256:44b74e7349b73b68f30a17dcdc52842f14ff3fa1293827a408d8d051049ec08d AS builder

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
FROM registry.access.redhat.com/hi/go:latest-fips@sha256:b793b36bc6129d842d28f051681deb7907aae8b9b40e894fcd62e0fb2c1eaad5

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
