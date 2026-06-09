package scheduler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/robfig/cron/v3"
	"insights-scheduler/internal/core/domain"
)

// getDefaultParser returns the standard 5-field cron parser
func getDefaultParser() cron.Parser {
	return cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
}

// Mock JobExecutor for testing
type mockJobExecutor struct {
	executedJobs          []domain.Job
	executeFunc           func(job domain.Job) error
	executeWithJobRunFunc func(job domain.Job, jobRunID string) error
}

func (m *mockJobExecutor) Execute(job domain.Job) error {
	if m.executeFunc != nil {
		return m.executeFunc(job)
	}
	m.executedJobs = append(m.executedJobs, job)
	return nil
}

func (m *mockJobExecutor) ExecuteWithJobRun(job domain.Job, jobRunID string) error {
	if m.executeWithJobRunFunc != nil {
		return m.executeWithJobRunFunc(job, jobRunID)
	}
	m.executedJobs = append(m.executedJobs, job)
	return nil
}

// Mock JobRepository for testing
type mockJobRepository struct {
	jobs map[string]domain.Job
}

func (m *mockJobRepository) Save(job domain.Job) error {
	m.jobs[job.ID] = job
	return nil
}

func (m *mockJobRepository) FindByID(id string) (domain.Job, error) {
	job, ok := m.jobs[id]
	if !ok {
		return domain.Job{}, domain.ErrJobNotFound
	}
	return job, nil
}

// setupTestRedis creates a test Redis instance using miniredis
func setupTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	return mr, client
}

func TestRedisScheduler_ScheduleJobImmediately(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()

	executor := &mockJobExecutor{}
	repo := &mockJobRepository{jobs: make(map[string]domain.Job)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler := &RedisScheduler{
		client:       client,
		executor:     executor,
		jobRepo:      repo,
		parser:       getDefaultParser(),
		ctx:          ctx,
		cancel:       cancel,
		pollInterval: 10 * time.Second,
	}

	// Create a test job
	job := domain.NewJob("Test Job", "org-123", "user-123", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{
		"format": "json",
	})

	testJobRunID := "test-job-run-123"

	// Schedule job immediately
	err := scheduler.ScheduleJobImmediately(job, testJobRunID)
	if err != nil {
		t.Fatalf("ScheduleJobImmediately() unexpected error: %v", err)
	}

	// Verify job was added to Redis sorted set
	score, err := client.ZScore(ctx, scheduledJobsKey, job.ID).Result()
	if err != nil {
		t.Fatalf("Job not found in Redis sorted set: %v", err)
	}

	// Verify the score (timestamp) is in the past (for immediate execution)
	jobTimestamp := int64(score)
	now := time.Now().Unix()
	if jobTimestamp > now {
		t.Errorf("Job scheduled for future (%d), expected past or current time (%d)", jobTimestamp, now)
	}

	// Verify timestamp is within last 10 seconds (5 seconds in the past + small buffer)
	timeDiff := now - jobTimestamp
	if timeDiff > 10 {
		t.Errorf("Job timestamp too far in the past: %d seconds ago", timeDiff)
	}

	// Verify job data was stored in Redis
	jobKey := jobDataKeyPrefix + job.ID
	jobData, err := client.Get(ctx, jobKey).Result()
	if err != nil {
		t.Fatalf("Job data not found in Redis: %v", err)
	}

	var scheduledJob ScheduledJob
	err = json.Unmarshal([]byte(jobData), &scheduledJob)
	if err != nil {
		t.Fatalf("Failed to unmarshal job data: %v", err)
	}

	// Verify the job content
	if scheduledJob.Job.ID != job.ID {
		t.Errorf("Job ID mismatch: got %s, want %s", scheduledJob.Job.ID, job.ID)
	}

	if scheduledJob.Job.Name != job.Name {
		t.Errorf("Job Name mismatch: got %s, want %s", scheduledJob.Job.Name, job.Name)
	}

	// Verify NextRun is in the past (for immediate execution)
	if scheduledJob.NextRun.After(time.Now()) {
		t.Errorf("NextRun should be in the past for immediate execution, got %s", scheduledJob.NextRun)
	}
}

func TestRedisScheduler_ScheduleJobImmediately_MultipleCalls(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()

	executor := &mockJobExecutor{}
	repo := &mockJobRepository{jobs: make(map[string]domain.Job)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler := &RedisScheduler{
		client:       client,
		executor:     executor,
		jobRepo:      repo,
		parser:       getDefaultParser(),
		ctx:          ctx,
		cancel:       cancel,
		pollInterval: 10 * time.Second,
	}

	job := domain.NewJob("Test Job", "org-123", "user-123", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})

	// Schedule job immediately multiple times
	err := scheduler.ScheduleJobImmediately(job, "run-1")
	if err != nil {
		t.Fatalf("First ScheduleJobImmediately() unexpected error: %v", err)
	}

	time.Sleep(10 * time.Millisecond) // Small delay to ensure different timestamps

	err = scheduler.ScheduleJobImmediately(job, "run-2")
	if err != nil {
		t.Fatalf("Second ScheduleJobImmediately() unexpected error: %v", err)
	}

	// Verify job still exists in sorted set (should be updated, not duplicated)
	count, err := client.ZCount(ctx, scheduledJobsKey, "-inf", "+inf").Result()
	if err != nil {
		t.Fatalf("Failed to count jobs in sorted set: %v", err)
	}

	if count != 1 {
		t.Errorf("Expected 1 job in sorted set after multiple ScheduleJobImmediately calls, got %d", count)
	}
}

func TestRedisScheduler_ScheduleJob_RegularScheduling(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()

	executor := &mockJobExecutor{}
	repo := &mockJobRepository{jobs: make(map[string]domain.Job)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler := &RedisScheduler{
		client:       client,
		executor:     executor,
		jobRepo:      repo,
		parser:       getDefaultParser(),
		ctx:          ctx,
		cancel:       cancel,
		pollInterval: 10 * time.Second,
	}

	job := domain.NewJob("Test Job", "org-123", "user-123", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})

	// Regular schedule (not immediate)
	err := scheduler.ScheduleJob(job)
	if err != nil {
		t.Fatalf("ScheduleJob() unexpected error: %v", err)
	}

	// Verify job was added to Redis sorted set
	score, err := client.ZScore(ctx, scheduledJobsKey, job.ID).Result()
	if err != nil {
		t.Fatalf("Job not found in Redis sorted set: %v", err)
	}

	// Verify the score (timestamp) is in the FUTURE (for regular scheduling)
	jobTimestamp := int64(score)
	now := time.Now().Unix()
	if jobTimestamp <= now {
		t.Errorf("Regular scheduled job should be in the future, got timestamp %d (now: %d)", jobTimestamp, now)
	}
}
