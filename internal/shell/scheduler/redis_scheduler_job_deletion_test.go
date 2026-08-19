package scheduler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
)

// mockJobRepoWithDeletion simulates a job being deleted mid-execution
type mockJobRepoWithDeletion struct {
	jobs        map[string]domain.Job
	deleteAfter int // Delete job after N calls to FindByID
	findCount   int
}

func (m *mockJobRepoWithDeletion) Save(job domain.Job) error {
	m.jobs[job.ID] = job
	return nil
}

func (m *mockJobRepoWithDeletion) FindByID(id string) (domain.Job, error) {
	m.findCount++

	// Simulate deletion after N calls
	// deleteAfter=0 means always return deleted
	// deleteAfter=1 means delete after first call, etc.
	if m.deleteAfter >= 0 && m.findCount > m.deleteAfter {
		return domain.Job{}, domain.ErrJobNotFound
	}

	job, ok := m.jobs[id]
	if !ok {
		return domain.Job{}, domain.ErrJobNotFound
	}
	return job, nil
}

func TestRedisScheduler_JobDeletedDuringExecution(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	redisCfg := config.RedisConfig{
		Enabled:  true,
		Host:     mr.Host(),
		Port:     mr.Server().Addr().Port,
		Password: "",
		DB:       0,
	}

	executor := &mockJobExecutor{}

	// Repo that simulates job deletion - returns ErrJobNotFound on first call
	// This simulates the job being deleted just before execution tries to reload it
	repo := &mockJobRepoWithDeletion{
		jobs:        make(map[string]domain.Job),
		deleteAfter: 0, // Delete immediately (job deleted during execution)
	}

	scheduler, err := NewRedisScheduler(
		redisCfg,
		executor,
		repo,
		100*time.Millisecond,
		10,
		2*time.Minute,
	)
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	defer scheduler.Close()

	// Create and schedule a job
	job := domain.NewJob(
		"Test Job",
		"org-123",
		"user-123",
		"* * * * *",
		"UTC",
		domain.PayloadMessage,
		map[string]interface{}{"test": "data"},
	)
	repo.jobs[job.ID] = job
	scheduler.ScheduleJobImmediately(job, "")

	// Verify job is in Redis
	ctx := context.Background()
	jobKey := jobDataKeyPrefix + job.ID
	exists, err := scheduler.client.Exists(ctx, jobKey).Result()
	if err != nil {
		t.Fatalf("Failed to check if job exists in Redis: %v", err)
	}
	if exists != 1 {
		t.Fatal("Job should exist in Redis before execution")
	}

	// Execute the job (will trigger reload which simulates deletion)
	scheduler.processDueJobs()

	// Wait for job processing to complete
	time.Sleep(100 * time.Millisecond)

	// Verify job was removed from Redis (not rescheduled)
	exists, err = scheduler.client.Exists(ctx, jobKey).Result()
	if err != nil {
		t.Fatalf("Failed to check if job exists in Redis: %v", err)
	}
	if exists != 0 {
		t.Error("Job should have been removed from Redis after deletion detected")
	}

	// Verify job was removed from sorted set
	score, err := scheduler.client.ZScore(ctx, scheduledJobsKey, job.ID).Result()
	if err != redis.Nil {
		t.Errorf("Job should have been removed from sorted set, got score: %v, err: %v", score, err)
	}
}

func TestRedisScheduler_JobDeletedBeforeExecution(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	redisCfg := config.RedisConfig{
		Enabled:  true,
		Host:     mr.Host(),
		Port:     mr.Server().Addr().Port,
		Password: "",
		DB:       0,
	}

	executor := &mockJobExecutor{}
	repo := &mockJobRepoWithDeletion{
		jobs:        make(map[string]domain.Job),
		deleteAfter: 0, // Job already deleted (returns ErrJobNotFound immediately)
	}

	scheduler, err := NewRedisScheduler(
		redisCfg,
		executor,
		repo,
		100*time.Millisecond,
		10,
		2*time.Minute,
	)
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	defer scheduler.Close()

	// Create job in Redis only (simulating stale Redis entry)
	job := domain.NewJob(
		"Deleted Job",
		"org-123",
		"user-123",
		"* * * * *",
		"UTC",
		domain.PayloadMessage,
		map[string]interface{}{"test": "data"},
	)

	// Add directly to Redis (bypassing database)
	ctx := context.Background()
	scheduledJob := ScheduledJob{
		Job:        job,
		NextRun:    time.Now().Add(-1 * time.Minute),
		Schedule:   string(job.Schedule),
		LastUpdate: time.Now(),
	}
	jobData, _ := json.Marshal(scheduledJob)
	jobKey := jobDataKeyPrefix + job.ID

	scheduler.client.Set(ctx, jobKey, jobData, 0)
	scheduler.client.ZAdd(ctx, scheduledJobsKey, &redis.Z{
		Score:  float64(time.Now().Add(-1 * time.Minute).Unix()),
		Member: job.ID,
	})

	// Execute - should handle missing job gracefully
	scheduler.processDueJobs()

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Verify job was removed from Redis
	exists, err := scheduler.client.Exists(ctx, jobKey).Result()
	if err != nil {
		t.Fatalf("Failed to check if job exists in Redis: %v", err)
	}
	if exists != 0 {
		t.Error("Deleted job should have been removed from Redis")
	}
}
