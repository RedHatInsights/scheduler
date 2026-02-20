package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/robfig/cron/v3"

	"insights-scheduler/internal/core/domain"
)

// RedisScheduler manages job scheduling using Redis for persistence
type RedisScheduler struct {
	client   *redis.Client
	executor JobExecutor
	parser   cron.Parser
	ctx      context.Context
	cancel   context.CancelFunc
}

// JobExecutor executes jobs
type JobExecutor interface {
	Execute(job domain.Job) error
}

// ScheduledJob represents a job stored in Redis
type ScheduledJob struct {
	Job        domain.Job `json:"job"`
	NextRun    time.Time  `json:"next_run"`
	Schedule   string     `json:"schedule"`
	LastUpdate time.Time  `json:"last_update"`
}

const (
	// Redis key for sorted set of scheduled jobs (score = next run timestamp)
	scheduledJobsKey = "scheduler:jobs:scheduled"

	// Redis key prefix for job data
	jobDataKeyPrefix = "scheduler:job:"

	// Redis key for job execution locks
	jobLockKeyPrefix = "scheduler:lock:"

	// Redis key for sync leader election
	syncLeaderKey = "scheduler:sync:leader"

	// Lock TTL - prevents stuck locks if worker crashes
	lockTTL = 5 * time.Minute

	// Leader TTL - prevents stuck leader lock if worker crashes during sync
	leaderTTL = 5 * time.Minute
)

// NewRedisScheduler creates a new Redis-based scheduler
func NewRedisScheduler(redisAddr string, executor JobExecutor) (*RedisScheduler, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	// Test connection
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &RedisScheduler{
		client:   client,
		executor: executor,
		parser:   cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
		ctx:      ctx,
		cancel:   cancel,
	}, nil
}

// Start begins the scheduler loop
func (s *RedisScheduler) Start() {
	log.Println("[RedisScheduler] Starting scheduler")

	ticker := time.NewTicker(10 * time.Second) // Check every 10 seconds
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			log.Println("[RedisScheduler] Scheduler stopped")
			return
		case <-ticker.C:
			s.processDueJobs()
		}
	}
}

// Stop gracefully stops the scheduler
func (s *RedisScheduler) Stop() {
	log.Println("[RedisScheduler] Stopping scheduler")
	s.cancel()
}

// ScheduleJob adds or updates a job in the Redis schedule
func (s *RedisScheduler) ScheduleJob(job domain.Job) error {
	if job.Status != domain.StatusScheduled {
		log.Printf("[RedisScheduler] Skipping job %s - status is %s", job.ID, job.Status)
		return nil
	}

	// Parse schedule to get next run time
	schedule, err := s.parser.Parse(string(job.Schedule))
	if err != nil {
		return fmt.Errorf("invalid schedule: %w", err)
	}

	now := time.Now()
	nextRun := schedule.Next(now)

	// Store job data
	scheduledJob := ScheduledJob{
		Job:        job,
		NextRun:    nextRun,
		Schedule:   string(job.Schedule),
		LastUpdate: now,
	}

	jobData, err := json.Marshal(scheduledJob)
	if err != nil {
		return fmt.Errorf("failed to marshal job: %w", err)
	}

	pipe := s.client.Pipeline()

	// Store job data in Redis hash
	jobKey := jobDataKeyPrefix + job.ID
	pipe.Set(s.ctx, jobKey, jobData, 0)

	// Add to sorted set with next run time as score
	pipe.ZAdd(s.ctx, scheduledJobsKey, &redis.Z{
		Score:  float64(nextRun.Unix()),
		Member: job.ID,
	})

	_, err = pipe.Exec(s.ctx)
	if err != nil {
		return fmt.Errorf("failed to schedule job in Redis: %w", err)
	}

	log.Printf("[RedisScheduler] Scheduled job %s (next run: %s)", job.ID, nextRun.Format(time.RFC3339))
	return nil
}

// UnscheduleJob removes a job from the Redis schedule
func (s *RedisScheduler) UnscheduleJob(jobID string) {
	pipe := s.client.Pipeline()

	// Remove from sorted set
	pipe.ZRem(s.ctx, scheduledJobsKey, jobID)

	// Remove job data
	jobKey := jobDataKeyPrefix + jobID
	pipe.Del(s.ctx, jobKey)

	_, err := pipe.Exec(s.ctx)
	if err != nil {
		log.Printf("[RedisScheduler] Error unscheduling job %s: %v", jobID, err)
		return
	}

	log.Printf("[RedisScheduler] Unscheduled job %s", jobID)
}

// processDueJobs finds and executes jobs that are due to run
func (s *RedisScheduler) processDueJobs() {
	now := time.Now()

	// Get all jobs with score (next run time) <= now
	results, err := s.client.ZRangeByScore(s.ctx, scheduledJobsKey, &redis.ZRangeBy{
		Min:    "0",
		Max:    fmt.Sprintf("%d", now.Unix()),
		Offset: 0,
		Count:  100, // Process up to 100 jobs at a time
	}).Result()

	if err != nil {
		log.Printf("[RedisScheduler] Error fetching due jobs: %v", err)
		return
	}

	if len(results) == 0 {
		return
	}

	log.Printf("[RedisScheduler] Found %d jobs due for execution", len(results))

	for _, jobID := range results {
		s.executeJob(jobID)
	}
}

// executeJob executes a single job and reschedules it
func (s *RedisScheduler) executeJob(jobID string) {
	// Try to acquire lock
	lockKey := jobLockKeyPrefix + jobID
	locked, err := s.client.SetNX(s.ctx, lockKey, "locked", lockTTL).Result()
	if err != nil {
		log.Printf("[RedisScheduler] Error acquiring lock for job %s: %v", jobID, err)
		return
	}

	if !locked {
		log.Printf("[RedisScheduler] Job %s is already being processed", jobID)
		return
	}

	// Ensure lock is released
	defer s.client.Del(s.ctx, lockKey)

	// Get job data
	jobKey := jobDataKeyPrefix + jobID
	jobData, err := s.client.Get(s.ctx, jobKey).Result()
	if err == redis.Nil {
		// Job was deleted, remove from schedule
		log.Printf("[RedisScheduler] Job %s not found, removing from schedule", jobID)
		s.client.ZRem(s.ctx, scheduledJobsKey, jobID)
		return
	} else if err != nil {
		log.Printf("[RedisScheduler] Error fetching job %s: %v", jobID, err)
		return
	}

	var scheduledJob ScheduledJob
	if err := json.Unmarshal([]byte(jobData), &scheduledJob); err != nil {
		log.Printf("[RedisScheduler] Error unmarshaling job %s: %v", jobID, err)
		return
	}

	// Execute the job
	log.Printf("[RedisScheduler] Executing job %s", jobID)
	if err := s.executor.Execute(scheduledJob.Job); err != nil {
		log.Printf("[RedisScheduler] Error executing job %s: %v", jobID, err)
	}

	// Calculate next run time and reschedule
	schedule, err := s.parser.Parse(scheduledJob.Schedule)
	if err != nil {
		log.Printf("[RedisScheduler] Error parsing schedule for job %s: %v", jobID, err)
		return
	}

	nextRun := schedule.Next(time.Now())
	scheduledJob.NextRun = nextRun
	scheduledJob.LastUpdate = time.Now()

	// Update job data and sorted set score
	updatedJobData, err := json.Marshal(scheduledJob)
	if err != nil {
		log.Printf("[RedisScheduler] Error marshaling job %s: %v", jobID, err)
		return
	}

	pipe := s.client.Pipeline()
	pipe.Set(s.ctx, jobKey, updatedJobData, 0)
	pipe.ZAdd(s.ctx, scheduledJobsKey, &redis.Z{
		Score:  float64(nextRun.Unix()),
		Member: jobID,
	})

	if _, err := pipe.Exec(s.ctx); err != nil {
		log.Printf("[RedisScheduler] Error rescheduling job %s: %v", jobID, err)
		return
	}

	log.Printf("[RedisScheduler] Rescheduled job %s for %s", jobID, nextRun.Format(time.RFC3339))
}

// SyncJobsFromDB loads jobs from the database into Redis
// This should be called on startup to populate Redis
func (s *RedisScheduler) SyncJobsFromDB(jobs []domain.Job) error {
	log.Printf("[RedisScheduler] Syncing %d jobs from database to Redis", len(jobs))

	for _, job := range jobs {
		if job.Status == domain.StatusScheduled {
			if err := s.ScheduleJob(job); err != nil {
				log.Printf("[RedisScheduler] Error syncing job %s: %v", job.ID, err)
				// Continue with other jobs
			}
		}
	}

	log.Printf("[RedisScheduler] Sync completed")
	return nil
}

// GetScheduledJobCount returns the number of scheduled jobs in Redis
func (s *RedisScheduler) GetScheduledJobCount() (int64, error) {
	return s.client.ZCard(s.ctx, scheduledJobsKey).Result()
}

// TryAcquireLeader attempts to become the sync leader
// Returns true if this worker should perform the database sync
// Uses Redis SetNX to ensure only one worker syncs at a time
func (s *RedisScheduler) TryAcquireLeader(ttl time.Duration) (bool, error) {
	if ttl == 0 {
		ttl = leaderTTL
	}

	result, err := s.client.SetNX(s.ctx, syncLeaderKey, "1", ttl).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check leader status: %w", err)
	}

	return result, nil
}

// Close closes the Redis connection
func (s *RedisScheduler) Close() error {
	s.Stop()
	return s.client.Close()
}
