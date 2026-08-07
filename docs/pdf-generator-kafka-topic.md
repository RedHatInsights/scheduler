# PDF Generator Kafka Topic: `pdf-generator.updated.report`

## Yes, it is a Kafka topic

`pdf-generator.updated.report` is a Kafka topic used for **multi-pod synchronization** in the pdf-generator service.

## Purpose

The pdf-generator service runs multiple replicas (3 by default in production). When one pod generates a PDF component, it needs to inform other pods about the status update so they can all serve consistent status information.

## How It Works

### Producer Side (src/server/utils.ts:8)

```typescript
export const UpdateStatus = async (updateMessage: PDFComponent) => {
  // Update local cache
  pdfCache.addToCollection(updateMessage.collectionId, updateMessage);
  
  // Broadcast to other pods via Kafka
  await produceMessage(UPDATE_TOPIC, updateMessage)  // ← Publishes to kafka
    .then(() => {
      apiLogger.debug('Generating message sent');
    })
    .catch((error: unknown) => {
      apiLogger.error(`Kafka message not sent: ${error}`);
    });
  
  await pdfCache.verifyCollection(updateMessage.collectionId);
};
```

### Consumer Side (src/common/kafka.ts:103)

```typescript
export async function consumeMessages(topic: string) {
  // Each pod has its own consumer group based on hostname
  const consumer = kafka.consumer({ groupId: `pdf-gen-${os.hostname()}` });
  await consumer.connect();
  await consumer.subscribe({ topic: topic });

  await consumer.run({
    eachMessage: async ({ message }) => {
      const cacheObject = JSON.parse(message.value?.toString() as string);
      
      // Update this pod's local PdfCache with the status from other pods
      pdfCache.addToCollection(updateMessage.collectionId, {
        status: updateMessage.status,
        filepath: updateMessage.filepath,
        collectionId: updateMessage.collectionId,
        componentId: updateMessage.componentId,
        numPages: updateMessage?.numPages || 0,
        error: updateMessage?.error || '',
        order: updateMessage?.order,
      });
    },
  });
}
```

## Message Format

```typescript
type PDFComponent = {
  status: PdfStatus;           // "Generating" | "Generated" | "Failed" | "NotFound"
  filepath: string;            // S3 object key
  collectionId: string;        // UUID of the PDF collection
  componentId: string;         // UUID of this component
  error?: string;              // Error message if failed
  numPages?: number;           // Number of pages in this component
  order?: number;              // Order in the collection
}
```

## Topology Configuration (deploy/clowdapp.yml:15-18)

```yaml
kafkaTopics:
- replicas: 1
  partitions: 1
  topicName: pdf-generator.updated.report
```

## Why This Pattern?

### Problem Without Kafka
```
┌─────────┐   POST /create   ┌─────────┐
│ Client  │ ───────────────> │  Pod A  │ ← Generates PDF, updates local cache
└─────────┘                  └─────────┘
     │                             
     │ GET /status                   
     │ ────────────> Load Balancer
     │                     │
     │                     └──────> ┌─────────┐
     └────────────────────────────> │  Pod B  │ ← Doesn't know about the PDF!
                                    └─────────┘
                                    (404 or stale status)
```

### Solution With Kafka
```
┌─────────┐   POST /create   ┌─────────┐
│ Client  │ ───────────────> │  Pod A  │ ─┐
└─────────┘                  └─────────┘  │
     │                             │       │ Publishes status
     │                             │       │ to Kafka
     │                             │       ↓
     │                       ┌─────────────────────────┐
     │                       │  pdf-generator.updated  │
     │                       │        .report          │
     │                       └─────────────────────────┘
     │                             │       ↑
     │                             │       │ Subscribes
     │ GET /status                 ↓       │
     │ ────────────> Load Balancer         │
     │                     │               │
     │                     └──────> ┌─────────┐
     └────────────────────────────> │  Pod B  │ ← Has status in cache!
                                    └─────────┘
                                    (200 with correct status)
```

## Consumer Group Strategy

Each pod subscribes with a unique consumer group based on hostname:
```typescript
groupId: `pdf-gen-${os.hostname()}`  // e.g., "pdf-gen-pod-abc123"
```

This means:
- **Every pod receives every message** (different consumer groups = all get messages)
- All pods maintain synchronized `PdfCache` state
- Client can hit any pod and get consistent status

## Key Differences from Export Service

| Aspect | PDF Generator | Export Service |
|--------|--------------|----------------|
| **State storage** | In-memory `PdfCache` (8 hour TTL) | External service manages state |
| **Multi-pod sync** | Kafka `pdf-generator.updated.report` | Not needed (stateless polling) |
| **Status endpoint** | Serves from local cache | Proxies to external service |
| **Download** | Serves from S3 via local cache lookup | Returns URL to external service |

## Implications for Scheduler Integration

When the scheduler integrates with pdf-generator, it should:

1. **Not expect Kafka messages** - The scheduler doesn't need to subscribe to this topic
2. **Poll the status endpoint** - Use `GET /v2/status/{statusID}` which queries the pod's cache
3. **Hit any pod** - Load balancer can route to any pod (all have synced state)
4. **Cache may expire** - After 8 hours, the status will return 404 (implement cleanup logic)

## Example: How a PDF Job Flows

```
1. Scheduler → POST /v2/create to Pod A
              ← Receives statusID: "abc-123"

2. Pod A → Generates PDF component 1
         → UpdateStatus() → Kafka publish
         → Kafka → All pods (A, B, C) update their caches

3. Scheduler → GET /v2/status/abc-123 to Pod B (via load balancer)
              ← Pod B returns status from its synced cache
              ← Status: "Generating" (1/3 components ready)

4. Pod A → Generates components 2 & 3
         → Each triggers Kafka publish
         → All pods update caches

5. Scheduler → GET /v2/status/abc-123 to Pod C
              ← Status: "Generated" (3/3 components merged)

6. Scheduler → GET /v2/download/abc-123 to Pod A
              ← Pod A retrieves merged PDF from S3
              ← Returns PDF blob
```

## Summary

**`pdf-generator.updated.report`** is a Kafka topic for **horizontal pod synchronization**, NOT for external consumers. The scheduler should treat pdf-generator as a black box and interact only via its HTTP API (create/status/download), relying on the service's internal Kafka plumbing to keep all pods synchronized.
