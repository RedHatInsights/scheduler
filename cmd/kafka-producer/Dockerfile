# Multi-stage build for Kafka Producer
FROM golang:1.26-alpine AS builder

# Set working directory
WORKDIR /app

# Install dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the producer
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o kafka-producer ./cmd/kafka-producer/

# Final stage
FROM alpine:latest

# Install ca-certificates for HTTPS connections
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1001 producer && \
    adduser -D -s /bin/sh -u 1001 -G producer producer

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/kafka-producer .

# Copy examples and documentation
COPY --from=builder /app/cmd/kafka-producer/README.md .
COPY --from=builder /app/cmd/kafka-producer/examples.sh .

# Make scripts executable
RUN chmod +x kafka-producer examples.sh

# Change ownership
RUN chown -R producer:producer /app

# Switch to non-root user
USER producer

# Default command
ENTRYPOINT ["./kafka-producer"]

# Default arguments
CMD ["-help"]