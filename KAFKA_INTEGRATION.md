# Kafka Integration for Export Completion Events

The job scheduler now includes Kafka integration that produces messages when export jobs complete.

## Configuration

Set the following environment variables to enable Kafka:

```bash
export KAFKA_BROKERS="localhost:9092,localhost:9093"
export KAFKA_TOPIC="export-completions"
```

If `KAFKA_BROKERS` is not set, the system will run without Kafka integration.

If `KAFKA_TOPIC` is not set, it defaults to `"export-completions"`.

## Message Format

When an export job completes, a JSON message is sent to the configured Kafka topic:

```json
{
  "export_id": "export-123-456-789",
  "job_id": "job-abc-def-ghi", 
  "org_id": "org-customer-001",
  "status": "complete",
  "completed_at": "2024-01-15T10:30:00Z",
  "download_url": "https://console.redhat.com/api/export/v1/exports/export-123-456-789",
  "error_message": ""
}
```

### Message Fields

- **export_id**: Unique identifier for the export from the export service
- **job_id**: Unique identifier for the scheduled job that triggered the export
- **org_id**: Organization ID that owns the job and export
- **status**: Export completion status (`complete`, `failed`, etc.)
- **completed_at**: Timestamp when the export finished processing
- **download_url**: URL to download the export (only present for successful exports)
- **error_message**: Error description (only present for failed exports)

### Message Headers

Each Kafka message includes the following headers:

- **event_type**: Always set to `"export_completion"`
- **org_id**: Organization ID for message routing/filtering

### Message Key

The Kafka message key is set to the `export_id` to ensure messages for the same export are processed in order.

## Usage Examples

### Starting the Server with Kafka

```bash
# With local Kafka
export KAFKA_BROKERS="localhost:9092"
export KAFKA_TOPIC="export-completions"
./cmd/server/main

# With multiple brokers
export KAFKA_BROKERS="broker1:9092,broker2:9092,broker3:9092"
export KAFKA_TOPIC="prod-export-completions"
./cmd/server/main
```

### Creating an Export Job

```bash
curl -X POST http://localhost:5000/api/v1/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Daily Customer Report",
    "org_id": "customer-001",
    "schedule": "0 0 8 * * *",
    "payload": {
      "type": "export",
      "details": {
        "name": "Daily Customer Export",
        "format": "json",
        "timeout": "10m"
      }
    }
  }'
```

When this job runs and the export completes, a Kafka message will be produced.

## Consumer Example

Here's a simple Kafka consumer example in Go:

```go
package main

import (
    "encoding/json"
    "log"
    
    "github.com/IBM/sarama"
)

type ExportCompletionMessage struct {
    ExportID    string `json:"export_id"`
    JobID       string `json:"job_id"`
    OrgID       string `json:"org_id"`
    Status      string `json:"status"`
    CompletedAt string `json:"completed_at"`
    DownloadURL string `json:"download_url,omitempty"`
    ErrorMsg    string `json:"error_message,omitempty"`
}

func main() {
    consumer, err := sarama.NewConsumer([]string{"localhost:9092"}, nil)
    if err != nil {
        log.Fatal(err)
    }
    defer consumer.Close()

    partitionConsumer, err := consumer.ConsumePartition("export-completions", 0, sarama.OffsetNewest)
    if err != nil {
        log.Fatal(err)
    }
    defer partitionConsumer.Close()

    for message := range partitionConsumer.Messages() {
        var completion ExportCompletionMessage
        if err := json.Unmarshal(message.Value, &completion); err != nil {
            log.Printf("Failed to unmarshal message: %v", err)
            continue
        }
        
        log.Printf("Export %s for org %s completed with status: %s", 
            completion.ExportID, completion.OrgID, completion.Status)
            
        if completion.Status == "complete" {
            log.Printf("Download URL: %s", completion.DownloadURL)
        } else if completion.Status == "failed" {
            log.Printf("Error: %s", completion.ErrorMsg)
        }
    }
}
```

## Error Handling

- If Kafka is unavailable during startup, the server will log an error and continue running without Kafka integration
- If Kafka becomes unavailable during runtime, export job execution will continue but Kafka messages will not be sent
- Kafka message failures do not cause job execution to fail

## Monitoring

Monitor the following logs for Kafka integration:

- `"Kafka producer initialized successfully"` - Kafka is working
- `"No Kafka configuration found"` - Running without Kafka (expected if KAFKA_BROKERS not set)
- `"Failed to initialize Kafka producer"` - Kafka configuration error
- `"Kafka message sent successfully"` - Message sent for export completion
- `"Failed to send Kafka message"` - Message sending failed (job still succeeds)

## Production Considerations

1. **Message Retention**: Configure appropriate retention policies on the Kafka topic
2. **Partitioning**: Consider partitioning strategy based on org_id for better distribution
3. **Monitoring**: Set up monitoring for message production and consumption
4. **Security**: Configure SASL/SSL for production Kafka clusters
5. **Schema Evolution**: Plan for message schema changes using schema registry if needed