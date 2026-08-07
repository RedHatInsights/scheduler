# PDF Generator Cache Loss Issue

## Problem Statement

**YES - A pod restart in pdf-generator WILL flush the in-memory cache and cause the service to lose track of generated PDFs.**

## The Architecture Flaw

### What Gets Lost on Pod Restart

```
Before Restart:
┌─────────────────────────────────────┐
│  Pod A (In-Memory PdfCache)         │
│  ┌───────────────────────────────┐  │
│  │ Collection: abc-123            │  │
│  │ Status: "Generated"            │  │
│  │ Components: [comp1, comp2]     │  │
│  │ ExpectedLength: 2              │  │
│  └───────────────────────────────┘  │
└─────────────────────────────────────┘

After Restart:
┌─────────────────────────────────────┐
│  Pod A (EMPTY PdfCache)             │
│  ┌───────────────────────────────┐  │
│  │ (no data)                      │  │
│  └───────────────────────────────┘  │
└─────────────────────────────────────┘
```

**The PDF file still exists in S3**, but the metadata mapping `collectionId → {status, components, filepath}` is **gone**.

## Evidence from Code

### No Persistent Storage

```bash
$ grep -r "database\|postgres\|mysql\|redis" package.json
# (no results)
```

**No database dependency** - all state is in-memory only.

### Consumer Doesn't Read History (kafka.ts:106-108)

```typescript
export async function consumeMessages(topic: string) {
  const consumer = kafka.consumer({ groupId: `pdf-gen-${os.hostname()}` });
  await consumer.connect();
  
  // Don't read from the beginning. Messages from not-yet-expired objects on the topic
  // will contain paths to PDFs that are not on the new pod
  await consumer.subscribe({ topic: topic });  // ← fromBeginning: false (default)
  
  // Only receives NEW messages after subscription
  // Old completed PDFs are LOST
}
```

**Key Issue**: The comment says "not on the new pod" - they're aware that file paths might not exist, but this creates a worse problem: **status metadata is lost entirely**.

### Cache is Just a Singleton (pdfCache.ts:103-109)

```typescript
class PdfCache {
  private static instance: PdfCache;
  private data: PdfCollection;  // ← In-memory object

  private constructor() {
    this.data = {};  // ← Starts empty on pod restart
  }
}
```

No `loadFromS3()`, no `restoreFromKafka()`, no persistence layer at all.

### Startup Sequence (server/index.ts:30-37)

```typescript
PdfCache.getInstance();  // ← Empty cache
store.intialize(StoreType.S3);  // ← S3 connection only

const server = http.createServer({}, app).listen(PORT, () => {
  apiLogger.info(`Listening on port ${PORT}`);
  consumeMessages(UPDATE_TOPIC).catch(...);  // ← Only NEW messages
});
```

No restoration logic. The pod comes up with an empty cache and only learns about new PDFs created **after** it started.

## Failure Scenarios

### Scenario 1: Single Pod Restart During PDF Generation

```
10:00:00 - Client → POST /v2/create to Pod A
         - Pod A creates collection abc-123
         - Starts generating PDF components

10:00:30 - Pod A generates component 1/3
         - Kafka message sent (all pods update cache)
         
10:00:45 - Pod A CRASHES or is restarted (OOM, deploy, eviction)

10:01:00 - Pod A comes back up
         - Cache is EMPTY
         - Consumer subscribes to Kafka (fromBeginning: false)
         - Only receives NEW messages
         
10:01:30 - Client → GET /v2/status/abc-123 to Pod A
         - Pod A: 404 "No PDF status found for abc-123"
         - BUT the partial PDF component is sitting in S3!
```

### Scenario 2: Rolling Deployment During Active Jobs

```
10:00 - 100 PDF jobs in progress across 3 pods
      - Pod A: 40 jobs (20 complete, 20 generating)
      - Pod B: 35 jobs (30 complete, 5 generating)  
      - Pod C: 25 jobs (all generating)

10:05 - Rolling deployment starts
      - Pod A terminates → cache flushed
      - Pod A' starts → empty cache
      - Pod A' consumer subscribes (fromBeginning: false)
      
10:10 - Pod B terminates → cache flushed
      - Pod B' starts → empty cache
      
10:15 - Pod C terminates → cache flushed
      - Pod C' starts → empty cache

Result: ALL in-progress jobs are now orphaned
        - PDFs exist in S3
        - Status API returns 404
        - Clients have no way to download completed PDFs
```

### Scenario 3: All Pods Restart (Cluster Reboot)

```
10:00 - Complete cluster restart (node maintenance, K8s upgrade)
      - All pods terminate simultaneously
      - All in-memory caches lost

10:05 - All pods come back up
      - All caches start empty
      - No restoration logic runs
      
Result: Every PDF generated in the last 8 hours is now "lost"
        (files in S3, but no metadata to find them)
```

## What About Kafka Message Retention?

**Kafka messages are retained**, but the consumer doesn't read them!

```typescript
await consumer.subscribe({ topic: topic });  
// Defaults to fromBeginning: false
// Only reads NEW messages from subscription point forward
```

The comment in the code says:
```typescript
// Don't read from the beginning. Messages from not-yet-expired objects on the topic
// will contain paths to PDFs that are not on the new pod
```

**This is a misguided optimization** - they're worried about file paths not existing, but the actual problem is:
1. The file paths are S3 keys (global, not pod-local)
2. Losing the status metadata is worse than handling missing files

## Impact on Scheduler Integration

If the scheduler creates a PDF job:

```
09:00 - Scheduler creates PDF (statusID: xyz-789)
09:01 - PDF starts generating
09:05 - pdf-generator pod restarts
09:10 - Scheduler polls GET /v2/status/xyz-789
      - Response: 404 "No PDF status found"
      
Scheduler thinks: Job failed?
Reality: PDF might have completed, file is in S3, but metadata is lost
```

The scheduler has no way to distinguish:
- Invalid statusID (never existed)
- Expired statusID (>8 hours old)
- Orphaned statusID (pod restarted)

## Solutions

### Option 1: Make pdf-generator Stateful (Fix Upstream)

**Add a database** to persist status metadata:

```sql
CREATE TABLE pdf_collections (
    collection_id TEXT PRIMARY KEY,
    status TEXT NOT NULL,
    expected_length INT,
    created_at TIMESTAMP,
    updated_at TIMESTAMP
);

CREATE TABLE pdf_components (
    component_id TEXT PRIMARY KEY,
    collection_id TEXT REFERENCES pdf_collections(collection_id),
    status TEXT,
    filepath TEXT,
    num_pages INT,
    component_order INT,
    error TEXT
);
```

On startup, restore cache from DB:
```typescript
async function restoreCacheFromDB() {
    const collections = await db.query('SELECT * FROM pdf_collections WHERE created_at > NOW() - INTERVAL 8 HOURS');
    for (const col of collections) {
        const components = await db.query('SELECT * FROM pdf_components WHERE collection_id = ?', col.collection_id);
        pdfCache.restoreCollection(col, components);
    }
}
```

**Pros**: Proper fix, handles all edge cases  
**Cons**: Requires upstream changes, adds DB dependency

### Option 2: Consume Kafka from Beginning (Quick Fix Upstream)

Change kafka.ts to replay recent messages:

```typescript
await consumer.subscribe({ 
    topic: topic,
    fromBeginning: true  // ← Change this
});

await consumer.run({
    eachMessage: async ({ message, timestamp }) => {
        // Only process messages from last 8 hours (match cache TTL)
        const messageAge = Date.now() - timestamp;
        if (messageAge > 8 * 60 * 60 * 1000) {
            return; // Skip old messages
        }
        
        // Validate S3 object exists before adding to cache
        const exists = await store.objectExists(cacheObject.filepath);
        if (!exists) {
            apiLogger.debug(`Skipping message for missing S3 object: ${cacheObject.filepath}`);
            return;
        }
        
        pdfCache.addToCollection(cacheObject.collectionId, cacheObject);
    }
});
```

**Pros**: Minimal code change, uses existing Kafka retention  
**Cons**: Slow startup on large topic, still has race conditions

### Option 3: Scheduler Defensive Polling (Workaround)

**Accept that pdf-generator is unreliable**, implement retry logic:

```go
func (p *PDFPoller) GetStatus(ctx context.Context, jobID string) (*polling.StatusResponse, error) {
    status, err := p.client.GetPDFStatus(ctx, jobID)
    
    if err != nil && isNotFoundError(err) {
        // Could be: never existed, expired, or pod restarted
        // Cannot distinguish - treat as "still generating" if within reasonable time
        
        // Check if we recently created this job (within last 30 minutes)
        if p.createdRecently(jobID, 30*time.Minute) {
            logger.Warn("PDF status not found but job was recently created - assuming pod restart",
                slog.String("status_id", jobID))
            
            // Return "in progress" to continue polling
            return &polling.StatusResponse{
                ID:         jobID,
                Status:     polling.StatusInProgress,
                Error:      "",
                IsTerminal: false,
            }, nil
        }
        
        // Old job or truly doesn't exist - fail
        return nil, fmt.Errorf("PDF status not found: %w", err)
    }
    
    // ... normal status handling
}

// Track job creation times in scheduler DB
type PDFJobTracking struct {
    StatusID   string
    JobRunID   string
    CreatedAt  time.Time
}
```

**Pros**: Scheduler-side fix, no upstream dependency  
**Cons**: Hacky, can't detect truly failed jobs vs. restart, wastes polling attempts

### Option 4: Always Check S3 as Fallback

Enhance the status endpoint to check S3 if cache misses:

```typescript
router.get(`${config?.APIPrefix}/v2/status/:statusID`, async (req, res) => {
  const ID = req.params.statusID;
  
  // First check cache
  let status = pdfCache.getCollection(ID);
  
  // If not in cache, try to reconstruct from S3
  if (!status) {
    apiLogger.warn(`Status not in cache for ${ID}, checking S3`);
    
    const s3Object = await store.getObjectMetadata(ID);
    if (s3Object) {
      // Reconstruct minimal status from S3 metadata
      status = {
        status: PdfStatus.Generated,  // If it's in S3, it must be complete
        components: [{
          status: PdfStatus.Generated,
          filepath: ID,
          collectionId: ID,
          componentId: ID,
        }],
        expectedLength: 1,
      };
      
      // Restore to cache for future requests
      pdfCache.restoreCollection(ID, status);
    }
  }
  
  // ... rest of endpoint
});
```

**Pros**: Transparent fallback, S3 is source of truth  
**Cons**: Slower (S3 API call), requires S3 metadata tagging, still can't track in-progress jobs

## Recommended Approach

### Short Term (Scheduler Integration)
**Option 3** - Implement defensive polling with job creation tracking:
```go
// Store when we create PDF jobs
type JobRun struct {
    // ... existing fields
    ExternalJobID     *string    `json:"external_job_id,omitempty"`
    ExternalCreatedAt *time.Time `json:"external_created_at,omitempty"`
}

// On 404, check if job is recent enough to retry
if isNotFound(err) && time.Since(*run.ExternalCreatedAt) < 30*time.Minute {
    return StatusInProgress  // Assume pod restart, keep polling
}
```

### Medium Term (Request Upstream)
**Option 2** - Ask pdf-generator team to consume from beginning:
- File a GitHub issue explaining the pod restart problem
- Propose the Kafka replay + S3 validation approach
- Minimal code change, reasonable performance impact

### Long Term (Proper Fix)
**Option 1** - Request database backend for status:
- Persistent status metadata
- Fast cache warm-up on pod start
- Survives complete cluster restarts
- Required for true production reliability

## Workaround: Download URL Storage

One more option - **don't rely on status API at all**:

```go
// After creating PDF, immediately store the download URL pattern
type PDFResult struct {
    StatusID    string
    DownloadURL string  // Predictable URL: {baseURL}/v2/download/{statusID}
}

// Skip status polling, just try to download after reasonable delay
func (e *PDFJobExecutor) Execute(job domain.Job) {
    createResult, _ := e.pdfClient.CreatePDF(...)
    
    // Wait maximum time for PDF generation (5 minutes)
    time.Sleep(5 * time.Minute)
    
    // Try download directly (download endpoint might work even if status is lost)
    downloadURL := e.pdfClient.GetDownloadURL(createResult.StatusID)
    
    // If download succeeds, job succeeded (even if status was lost)
    // If download 404s, job failed
}
```

This works because:
- S3 storage survives pod restarts
- Download endpoint can fetch from S3 even if cache is empty
- Simpler than polling, but requires fixed timeout

## Summary

**The pdf-generator has a critical architectural flaw**: all status metadata lives in in-memory cache with no persistence and no recovery on pod restart. This makes it unsuitable for long-running or critical jobs.

**For scheduler integration**, you must either:
1. Implement defensive retry logic (assume 404 = pod restart if recent)
2. Request upstream fixes (Kafka replay or database)
3. Use fixed-timeout + direct download (skip status polling)

**The export service does not have this problem** because it stores state in its own database, and the scheduler just polls an external stateless API.
