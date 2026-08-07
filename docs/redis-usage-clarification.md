# Redis Usage Clarification

## Question: Does the resumable polling design use Redis?

**Short answer**: My recommended design (Option 1) does **NOT** use Redis. But you already have Redis in your system for a different purpose.

## Current Redis Usage in Your Codebase

### Existing Use: Distributed Scheduler (Worker Mode)

Redis is **already configured** but used for **job scheduling**, NOT for polling state:

```go
// internal/config/config.go
type RedisConfig struct {
    Enabled  bool   // Default: false
    Host     string
    Port     int
    Password string
}

// internal/shell/scheduler/redis_scheduler.go
type RedisScheduler struct {
    // Uses Redis as a distributed job queue
    // Multiple worker pods pull jobs from Redis
}
```

**What Redis does NOW**:
```
┌──────────┐                ┌───────────┐
│ API Pod  │                │  Redis    │
│          │ ─Schedule Job─>│  Queue    │
└──────────┘                │           │
                            │ job:123   │
                            │ job:456   │
┌──────────┐                │ job:789   │
│ Worker 1 │ <──Pull Job────│           │
└──────────┘                └───────────┘
┌──────────┐                     
│ Worker 2 │ <──Pull Job────┘
└──────────┘
```

**What Redis does NOT do** (currently):
- ❌ Store polling state
- ❌ Store external job IDs
- ❌ Track poll progress

## Three Design Options (from docs/resumable-polling-options.md)

### Option 1: PostgreSQL-Only ⭐ **MY RECOMMENDATION**

**Does NOT use Redis for polling state.**

```
State Storage: PostgreSQL only
└─ external_job_id: TEXT column in job_runs table
└─ poll_started_at: TIMESTAMP column

Recovery:
1. Scheduler restarts
2. Query: SELECT * FROM job_runs WHERE status='running' AND external_job_id IS NOT NULL
3. Resume polling those exports
```

**Why I recommended this**:
- ✅ Simpler (one system to maintain)
- ✅ PostgreSQL is already your source of truth
- ✅ Transactional consistency
- ✅ Works even if Redis is down
- ✅ No additional infrastructure

**Redis usage**: NONE (for polling state)

---

### Option 2: Redis-Only

**Uses Redis for polling state, NOT PostgreSQL.**

```
State Storage: Redis only
└─ Key: polling:state:{job_run_id}
└─ Value: {external_job_id, current_attempt: 42, last_status: "running"}
└─ TTL: 1 hour (auto-cleanup)

Recovery:
1. Scheduler restarts
2. Redis SCAN polling:state:*
3. Resume polling from last attempt
```

**Advantages**:
- ✅ Fast writes (optimized for high frequency)
- ✅ Resume from exact attempt (not just job ID)
- ✅ Auto-cleanup via TTL

**Disadvantages**:
- ⚠️ Depends on Redis persistence config
- ⚠️ Data loss if Redis fails before fsync
- ⚠️ Can't query with SQL (harder to debug)

**Redis usage**: HIGH (every poll attempt writes to Redis)

---

### Option 3: Hybrid (PostgreSQL + Redis)

**Uses BOTH systems.**

```
PostgreSQL (durable):
└─ external_job_id: TEXT (written once at job creation)

Redis (transient):
└─ poll:progress:{job_run_id} → {current_attempt: 42, last_status}
└─ TTL: 1 hour

Recovery:
1. Scheduler restarts
2. Query PostgreSQL for external_job_id
3. Check Redis for progress (if available)
4. Resume from attempt N (Redis) or attempt 1 (if Redis empty)
```

**Advantages**:
- ✅ Best of both worlds
- ✅ Survives Redis failure (degrades to Option 1)
- ✅ Fast progress tracking when Redis available

**Disadvantages**:
- ⚠️ Most complex
- ⚠️ Two systems to keep in sync

**Redis usage**: MEDIUM (progress updates only)

## Comparison Table

| Aspect | Option 1 (PostgreSQL) | Option 2 (Redis) | Option 3 (Hybrid) |
|--------|----------------------|------------------|-------------------|
| **Uses Redis** | ❌ No | ✅ Yes (only) | ✅ Yes (partial) |
| **Uses PostgreSQL** | ✅ Yes (only) | ❌ No | ✅ Yes (partial) |
| **Resume from** | Attempt 1 | Attempt N | Attempt N (or 1) |
| **Complexity** | Low | Medium | High |
| **Durability** | High | Medium* | High |
| **Write frequency** | Once per job | Every poll (60x) | Once + 60x |
| **Failover behavior** | Always works | Fails if Redis down | Degrades gracefully |

*Depends on Redis persistence config (AOF/RDB)

## My Recommendation: Option 1 (PostgreSQL-Only)

**Why NOT use Redis for polling state:**

1. **PostgreSQL is already your source of truth**
   - Jobs are in PostgreSQL
   - JobRuns are in PostgreSQL
   - Adding external_job_id to PostgreSQL is natural

2. **Simpler architecture**
   - One system to backup
   - One system to query
   - One failure mode to handle

3. **Resume from attempt 1 is fine**
   - Export service polling takes 5-10 minutes
   - Restarting from beginning adds ~30 seconds
   - Not worth the complexity of tracking attempt number

4. **Redis is already busy**
   - Currently used for distributed job queue
   - Don't overload it with polling state too

5. **Easier debugging**
   ```sql
   -- See all in-flight polls
   SELECT * FROM job_runs 
   WHERE status='running' AND external_job_id IS NOT NULL;
   
   -- Can't do this with Redis SCAN
   ```

## When to Consider Using Redis

**Use Option 2 (Redis-only) if:**
- ✅ Polling takes hours (not minutes)
- ✅ Resume from exact attempt is critical
- ✅ Very high job volume (1000+ concurrent)
- ✅ Redis has AOF persistence enabled

**Use Option 3 (Hybrid) if:**
- ✅ You want progress tracking but can't risk data loss
- ✅ High volume + need resilience
- ✅ Team comfortable managing dual-write systems

**Stick with Option 1 (PostgreSQL) if:**
- ✅ Polling takes 5-10 minutes
- ✅ Restart from beginning is acceptable
- ✅ You want simplicity over optimization
- ✅ You're already using PostgreSQL for everything else

## Implementation Comparison

### Option 1 (PostgreSQL-Only) - What I Documented

```go
// 1. Add column to job_runs
ALTER TABLE job_runs ADD COLUMN external_job_id TEXT;

// 2. Save immediately after creating export
jobRun.ExternalJobID = &exportID
runRepo.Save(jobRun)

// 3. On restart, query and resume
runs := runRepo.FindByStatus("running")
for _, run := range runs {
    if run.ExternalJobID != nil {
        resumePoll(run)
    }
}
```

**Lines of code**: ~100  
**New dependencies**: 0  
**Redis writes**: 0

---

### Option 2 (Redis-Only) - NOT in My Docs

```go
// 1. Create Redis client
redisClient := redis.NewClient(&redis.Options{
    Addr: "localhost:6379",
})

// 2. Save state on every poll attempt
func Poll(...) {
    for attempt := 0; attempt < 60; attempt++ {
        state := RedisPollingState{
            CurrentAttempt: attempt,
            ExternalJobID: exportID,
        }
        redis.Set("polling:state:"+jobRunID, state, 1*time.Hour)
        
        // Poll external service
        // ...
    }
}

// 3. On restart, scan Redis
keys := redis.Keys("polling:state:*")
for _, key := range keys {
    state := redis.Get(key)
    resumePoll(state, fromAttempt: state.CurrentAttempt)
}
```

**Lines of code**: ~300  
**New dependencies**: github.com/redis/go-redis  
**Redis writes**: 60 per job (every poll attempt)

---

### Option 3 (Hybrid) - Documented as Alternative

```go
// 1. Add PostgreSQL column (durable)
ALTER TABLE job_runs ADD COLUMN external_job_id TEXT;

// 2. Save to PostgreSQL + Redis
jobRun.ExternalJobID = &exportID
runRepo.Save(jobRun)  // Durable

redis.Set("poll:progress:"+jobRunID, {attempt: 0}, 1*time.Hour)  // Transient

// 3. On restart, try both
runs := runRepo.FindByStatus("running")  // PostgreSQL
for _, run := range runs {
    progress := redis.Get("poll:progress:"+run.ID)  // Redis
    
    if progress != nil {
        resumePoll(run, fromAttempt: progress.Attempt)  // Resume from N
    } else {
        resumePoll(run, fromAttempt: 0)  // Start over
    }
}
```

**Lines of code**: ~200  
**New dependencies**: github.com/redis/go-redis  
**Redis writes**: 60 per job

## Current Design in My Docs

**All my documents (db-restart-resilience.md, resumable-polling-options.md) recommend Option 1 (PostgreSQL-only).**

The only mentions of Redis are:
1. In `resumable-polling-options.md` - as Options 2 and 3 (alternatives)
2. In `db-restart-resilience.md` - NOT mentioned at all (PostgreSQL-focused)

**I did NOT design the implementation to use Redis.**

## Should You Use Redis?

**My answer: No, stick with PostgreSQL (Option 1).**

**But if you insist on using Redis**, I can:
1. Implement Option 2 (Redis-only)
2. Implement Option 3 (Hybrid)
3. Show you the trade-offs with working code

**However**, based on:
- Your polling duration (5-10 minutes, not hours)
- Your existing PostgreSQL usage
- Simplicity preferences
- The fact that restarting from attempt 1 is acceptable

**Option 1 (PostgreSQL-only) is the right choice.**

## Summary

| Question | Answer |
|----------|--------|
| Does my recommended design use Redis? | ❌ No |
| Is Redis available in your system? | ✅ Yes (for job queue) |
| Did I document Redis options? | ✅ Yes (as alternatives) |
| Should you use Redis for polling state? | ❌ No (unless you have specific needs) |
| What should you implement? | ✅ Option 1 (PostgreSQL-only) |

Want me to proceed with **Option 1 (PostgreSQL-only)** implementation, or would you prefer I design **Option 3 (Hybrid)** instead?
