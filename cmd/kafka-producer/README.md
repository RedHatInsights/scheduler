# Kafka Message Producer

A standalone command-line tool for sending messages to Kafka topics, specifically designed for testing platform notifications and export completion messages.

## Features

- **Multiple Message Types**: Support for notification messages, export completion messages, and custom JSON
- **Configurable Parameters**: All message fields can be customized via command-line flags
- **Batch Sending**: Send multiple messages with configurable intervals
- **Verbose Logging**: Optional detailed logging for debugging
- **Connection Testing**: Validates Kafka connectivity before sending messages

## Installation

```bash
# Build the producer
go build -o kafka-producer ./cmd/kafka-producer/

# Or run directly
go run ./cmd/kafka-producer/ [flags]
```

## Usage

### Basic Examples

#### Send a Single Notification Message
```bash
./kafka-producer \
  -brokers "localhost:9092" \
  -topic "platform.notifications.ingress" \
  -type "notification" \
  -account-id "123456" \
  -org-id "org-789"
```

#### Send Export Completion Message
```bash
./kafka-producer \
  -brokers "localhost:9092" \
  -topic "platform.notifications.ingress" \
  -type "export-completion" \
  -export-id "export-123" \
  -job-id "job-456" \
  -org-id "org-789" \
  -status "completed" \
  -download-url "https://example.com/export-123"
```

#### Send Multiple Messages with Interval
```bash
./kafka-producer \
  -brokers "localhost:9092" \
  -topic "platform.notifications.ingress" \
  -type "notification" \
  -count 5 \
  -interval 2s \
  -verbose
```

#### Send Custom JSON Message
```bash
./kafka-producer \
  -brokers "localhost:9092" \
  -topic "platform.notifications.ingress" \
  -type "custom" \
  -custom '{"version":"v1.2.0","bundle":"custom","application":"test-app","event_type":"test-event","account_id":"999","org_id":"test-org"}'
```

### Advanced Examples

#### Test Failed Export Notification
```bash
./kafka-producer \
  -brokers "kafka1:9092,kafka2:9092" \
  -topic "platform.notifications.ingress" \
  -type "export-completion" \
  -export-id "failed-export-123" \
  -status "failed" \
  -error-msg "Export processing failed due to timeout" \
  -org-id "org-789"
```

#### Load Testing
```bash
# Send 100 messages with 100ms interval
./kafka-producer \
  -brokers "localhost:9092" \
  -topic "platform.notifications.ingress" \
  -type "notification" \
  -count 100 \
  -interval 100ms \
  -verbose
```

#### Production-like Message
```bash
./kafka-producer \
  -brokers "kafka.prod.com:9092" \
  -topic "platform.notifications.ingress" \
  -type "notification" \
  -bundle "rhel" \
  -application "insights-scheduler" \
  -event-type "export-completed" \
  -account-id "$(cat /var/secrets/account-id)" \
  -org-id "$(cat /var/secrets/org-id)"
```

## Command-Line Flags

### Connection Options
| Flag | Default | Description |
|------|---------|-------------|
| `-brokers` | `localhost:9092` | Comma-separated list of Kafka brokers |
| `-topic` | `platform.notifications.ingress` | Kafka topic to send messages to |
| `-verbose` | `false` | Enable verbose logging |

### Message Control
| Flag | Default | Description |
|------|---------|-------------|
| `-type` | `notification` | Message type: `notification`, `export-completion`, or `custom` |
| `-count` | `1` | Number of messages to send |
| `-interval` | `1s` | Interval between messages |

### Notification Message Fields
| Flag | Default | Description |
|------|---------|-------------|
| `-bundle` | `rhel` | Bundle name for notifications |
| `-application` | `insights-scheduler` | Application name for notifications |
| `-event-type` | `export-completed` | Event type for notifications |
| `-account-id` | `123456` | Account ID for notifications |
| `-org-id` | `org-789` | Organization ID for notifications |

### Export Completion Message Fields
| Flag | Default | Description |
|------|---------|-------------|
| `-export-id` | `auto-generated` | Export ID for export completion messages |
| `-job-id` | `auto-generated` | Job ID for export completion messages |
| `-status` | `completed` | Status: `completed`, `failed`, `processing` |
| `-download-url` | `""` | Download URL for completed exports |
| `-error-msg` | `""` | Error message for failed exports |

### Custom Message
| Flag | Default | Description |
|------|---------|-------------|
| `-custom` | `""` | Custom JSON message to send |

## Message Formats

### Notification Message
```json
{
  "version": "v1.2.0",
  "bundle": "rhel",
  "application": "insights-scheduler",
  "event_type": "export-completed",
  "timestamp": "2025-01-15T10:30:45Z",
  "account_id": "123456",
  "org_id": "org-789",
  "context": {
    "export_id": "export-123",
    "job_id": "job-456",
    "status": "completed",
    "download_url": "https://example.com/export-123"
  },
  "events": [],
  "recipients": []
}
```

### Export Completion Message
```json
{
  "export_id": "export-123",
  "job_id": "job-456",
  "org_id": "org-789",
  "status": "completed",
  "completed_at": "2025-01-15T10:30:45Z",
  "download_url": "https://example.com/export-123"
}
```

## Environment Variables

You can also set default values using environment variables:

```bash
export KAFKA_BROKERS="kafka1:9092,kafka2:9092"
export KAFKA_TOPIC="platform.notifications.ingress"
export ACCOUNT_ID="123456"
export ORG_ID="org-789"

./kafka-producer -type notification
```

## Error Handling

The producer includes comprehensive error handling:

- **Connection Errors**: Validates Kafka broker connectivity
- **JSON Validation**: Ensures custom JSON messages are valid
- **Message Delivery**: Reports success/failure for each message
- **Graceful Shutdown**: Properly closes Kafka connections

## Troubleshooting

### Common Issues

1. **Connection Refused**
   ```bash
   # Check if Kafka is running
   netstat -tulpn | grep 9092
   
   # Test with telnet
   telnet localhost 9092
   ```

2. **Topic Not Found**
   ```bash
   # Create topic manually
   kafka-topics.sh --create --topic platform.notifications.ingress --bootstrap-server localhost:9092
   ```

3. **Authentication Issues**
   ```bash
   # Check if SASL is required
   ./kafka-producer -verbose -count 1
   ```

### Debug Mode
```bash
# Enable maximum verbosity
./kafka-producer -verbose -count 1 -type notification
```

## Integration with CI/CD

### Test Script Example
```bash
#!/bin/bash
set -e

echo "Testing Kafka producer..."

# Test basic connectivity
./kafka-producer -count 1 -type notification -verbose

# Test export completion
./kafka-producer -count 1 -type export-completion -status completed

# Test failed export
./kafka-producer -count 1 -type export-completion -status failed -error-msg "Test failure"

echo "All tests passed!"
```

### Docker Usage
```bash
# Build container
docker build -t kafka-producer .

# Run producer
docker run --rm kafka-producer \
  -brokers "kafka:9092" \
  -topic "platform.notifications.ingress" \
  -type notification \
  -count 5
```

## Performance Notes

- **Batch Size**: For high-volume testing, use shorter intervals (10ms-100ms)
- **Connection Pooling**: The producer reuses connections efficiently
- **Memory Usage**: Minimal memory footprint for typical usage
- **Throughput**: Can achieve 1000+ messages/second depending on Kafka configuration

## Security Considerations

- **Credentials**: Never pass sensitive data via command-line flags
- **TLS**: Use TLS-enabled brokers in production
- **Access Control**: Ensure proper Kafka ACLs are configured
- **Audit Logging**: Enable verbose mode for security auditing