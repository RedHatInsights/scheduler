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
	"insights-scheduler/internal/core/ports"
)

// RedisScheduler manages job scheduling using Redis for persistence
type RedisScheduler struct {
	client       *redis.Client
	executor     ports.JobExecutor
	jobRepo      JobRepository
	parser       cron.Parser
	ctx          context.Context
	cancel       context.CancelFunc
	pollInterval time.Duration
}

// JobRepository provides access to job storage
type JobRepository interface {
	Save(job domain.Job) error
	FindByID(id string) (domain.Job, error)
}

// ScheduledJob represents a job stored in Redis
type ScheduledJob struct {
	Job        domain.Job `json:"job"`
	NextRun    time.Time  `json:"next_run"`
	Schedule   string     `json:"schedule"`
	LastUpdate time.Time  `json:"last_update"`
	JobRunID   string     `json:"job_run_id,omitempty"` // Pre-created job run ID for immediate execution
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
func NewRedisScheduler(redisAddr string, executor ports.JobExecutor, jobRepo JobRepository, pollInterval time.Duration) (*RedisScheduler, error) {
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

	// Default to 10 seconds if not specified
	if pollInterval == 0 {
		pollInterval = 10 * time.Second
	}

	return &RedisScheduler{
		client:       client,
		executor:     executor,
		jobRepo:      jobRepo,
		parser:       cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
		ctx:          ctx,
		cancel:       cancel,
		pollInterval: pollInterval,
	}, nil
}

// Start begins the scheduler loop
func (s *RedisScheduler) Start() {
	log.Printf("[RedisScheduler] Starting scheduler (poll interval: %s)", s.pollInterval)

	ticker := time.NewTicker(s.pollInterval)
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

// ScheduleJobImmediately schedules a job to run immediately (bypassing the regular schedule)
// This is used for manual job runs triggered via the API
func (s *RedisScheduler) ScheduleJobImmediately(job domain.Job, jobRunID string) error {
	log.Printf("[RedisScheduler] Scheduling job %s for immediate execution (job run: %s)", job.ID, jobRunID)

	// Set next run to current time (or slightly in the past to ensure immediate pickup)
	now := time.Now()
	immediateRun := now.Add(-5 * time.Second) // 5 seconds in the past to ensure it's picked up

	// Store job data with immediate run time and job run ID
	scheduledJob := ScheduledJob{
		Job:        job,
		NextRun:    immediateRun,
		Schedule:   string(job.Schedule),
		LastUpdate: now,
		JobRunID:   jobRunID, // Store the pre-created job run ID
	}

	jobData, err := json.Marshal(scheduledJob)
	if err != nil {
		return fmt.Errorf("failed to marshal job: %w", err)
	}

	pipe := s.client.Pipeline()

	// Store job data in Redis hash
	jobKey := jobDataKeyPrefix + job.ID
	pipe.Set(s.ctx, jobKey, jobData, 0)

	// Add to sorted set with immediate run time as score
	pipe.ZAdd(s.ctx, scheduledJobsKey, &redis.Z{
		Score:  float64(immediateRun.Unix()),
		Member: job.ID,
	})

	_, err = pipe.Exec(s.ctx)
	if err != nil {
		return fmt.Errorf("failed to schedule job for immediate execution in Redis: %w", err)
	}

	log.Printf("[RedisScheduler] Job %s scheduled for immediate execution (next run: %s, job run: %s)", job.ID, immediateRun.Format(time.RFC3339), jobRunID)
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

	log.Printf("[RedisScheduler] scheduledJob %+v", scheduledJob)

	// Update last_run_at before execution
	now := time.Now()
	scheduledJob.Job = scheduledJob.Job.WithLastRunAt(now)

	// Execute the job (with job run ID if this is an immediate execution)
	if scheduledJob.JobRunID != "" {
		log.Printf("[RedisScheduler] Executing job %s with pre-created job run %s", jobID, scheduledJob.JobRunID)
		if err := s.executor.ExecuteWithJobRun(scheduledJob.Job, scheduledJob.JobRunID); err != nil {
			log.Printf("[RedisScheduler] Error executing job %s: %v", jobID, err)
		}
	} else {
		log.Printf("[RedisScheduler] Executing job %s", jobID)
		if err := s.executor.Execute(scheduledJob.Job); err != nil {
			log.Printf("[RedisScheduler] Error executing job %s: %v", jobID, err)
		}
	}

	// Reload the job from the database to get updated status/failure tracking
	// The executor may have updated consecutive_failures and auto-paused the job
	var updatedJob domain.Job
	if s.jobRepo != nil {
		reloadedJob, err := s.jobRepo.FindByID(jobID)
		if err != nil {
			log.Printf("[RedisScheduler] Warning: Failed to reload job %s from database: %v", jobID, err)
			updatedJob = scheduledJob.Job // Fallback to the job we had
		} else {
			updatedJob = reloadedJob
			log.Printf("[RedisScheduler] Reloaded job %s: status=%s, consecutive_failures=%d",
				jobID, updatedJob.Status, updatedJob.ConsecutiveFailures)
		}
	} else {
		updatedJob = scheduledJob.Job
	}

	// Check if the job was auto-paused or manually paused during execution
	if updatedJob.Status == domain.StatusPaused {
		log.Printf("[RedisScheduler] Job %s is now paused (consecutive_failures=%d), removing from schedule",
			jobID, updatedJob.ConsecutiveFailures)

		// Remove from Redis sorted set (do not reschedule)
		if err := s.client.ZRem(s.ctx, scheduledJobsKey, jobID).Err(); err != nil {
			log.Printf("[RedisScheduler] Warning: Failed to remove paused job %s from schedule: %v", jobID, err)
		}

		// Remove job data from Redis
		if err := s.client.Del(s.ctx, jobKey).Err(); err != nil {
			log.Printf("[RedisScheduler] Warning: Failed to remove paused job data for %s: %v", jobID, err)
		}

		return // Do not reschedule
	}

	// Calculate next run time and reschedule (only if not paused)
	schedule, err := s.parser.Parse(scheduledJob.Schedule)
	if err != nil {
		log.Printf("[RedisScheduler] Error parsing schedule for job %s: %v", jobID, err)
		return
	}

	nextRun := schedule.Next(time.Now())
	scheduledJob.NextRun = nextRun
	scheduledJob.LastUpdate = time.Now()
	scheduledJob.JobRunID = "" // Clear the job run ID after execution (it was for immediate execution only)

	// Use the reloaded job (with updated failure tracking) for rescheduling
	scheduledJob.Job = updatedJob.WithNextRunAt(nextRun)

	// Persist updated job to PostgreSQL (if repository is available)
	if s.jobRepo != nil {
		if err := s.jobRepo.Save(scheduledJob.Job); err != nil {
			log.Printf("[RedisScheduler] Warning: Failed to save job %s to database: %v", jobID, err)
			// Continue with Redis update even if database save fails
		} else {
			log.Printf("[RedisScheduler] Saved job %s to database with last_run_at=%s, next_run_at=%s",
				jobID, scheduledJob.Job.LastRunAt.Format(time.RFC3339), scheduledJob.Job.NextRunAt.Format(time.RFC3339))
		}
	}

	// Update job data and sorted set score in Redis
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
