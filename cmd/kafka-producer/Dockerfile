# Multi-stage build for Kafka Producer
FROM registry.access.redhat.com/hi/go:latest-fips-builder@sha256:39b4028b8e2b4565f70f46bb8a09c4a4f192b47ea31bedb0b9f1d079b8cad410 AS builder

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
FROM registry.access.redhat.com/hi/go:latest-fips@sha256:0114ae55412cb9713425fba5439a43ca694832f915d3087562c0eaf335436526

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
