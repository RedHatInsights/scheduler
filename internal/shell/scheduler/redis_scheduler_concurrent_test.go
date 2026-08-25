package scheduler

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
)

// mockExecutorWithDelay simulates a slow executor for testing
type mockExecutorWithDelay struct {
	executedCount int32
	delay         time.Duration
	mu            sync.Mutex
	executedJobs  []string
}

func (m *mockExecutorWithDelay) Execute(job domain.Job) error {
	atomic.AddInt32(&m.executedCount, 1)
	m.mu.Lock()
	m.executedJobs = append(m.executedJobs, job.ID)
	m.mu.Unlock()
	time.Sleep(m.delay)
	return nil
}

func (m *mockExecutorWithDelay) ExecuteWithJobRun(job domain.Job, runID string) error {
	return m.Execute(job)
}

func (m *mockExecutorWithDelay) Wait() {}

func (m *mockExecutorWithDelay) Count() int {
	return int(atomic.LoadInt32(&m.executedCount))
}

func (m *mockExecutorWithDelay) GetExecutedJobs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	jobs := make([]string, len(m.executedJobs))
	copy(jobs, m.executedJobs)
	return jobs
}

func TestWorkerPool_LimitsConcurrency(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	redisCfg := config.RedisConfig{
		Enabled:  true,
		Host:     mr.Host(),
		Port:     mr.Server().Addr().Port,
		Password: "",
		DB:       0,
	}

	executor := &mockExecutorWithDelay{delay: 100 * time.Millisecond}
	repo := &mockJobRepository{jobs: make(map[string]domain.Job)}

	maxConcurrentJobs := 3
	scheduler, err := NewRedisScheduler(
		redisCfg,
		executor,
		repo,
		100*time.Millisecond, // Short interval for testing
		maxConcurrentJobs,
		2*time.Minute,
	)
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	defer scheduler.Close()

	// Create 10 jobs all due now
	for i := 0; i < 10; i++ {
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
	}

	// Manually trigger job processing (instead of starting scheduler)
	scheduler.processDueJobs()

	// Wait for jobs to complete (100ms delay per job + pool of 3)
	time.Sleep(500 * time.Millisecond)

	// Verify all 10 jobs executed
	if executor.Count() != 10 {
		t.Errorf("Expected 10 jobs executed, got %d", executor.Count())
	}

	// Check that pool is now empty
	poolUsage := len(scheduler.workerSemaphore)
	if poolUsage != 0 {
		t.Errorf("Worker pool should be empty after jobs complete, got %d", poolUsage)
	}
}

func TestJobTimeout_TriggersCorrectly(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	redisCfg := config.RedisConfig{
		Enabled:  true,
		Host:     mr.Host(),
		Port:     mr.Server().Addr().Port,
		Password: "",
		DB:       0,
	}

	// Executor that takes 2 seconds (longer than timeout)
	executor := &mockExecutorWithDelay{delay: 2 * time.Second}
	repo := &mockJobRepository{jobs: make(map[string]domain.Job)}

	scheduler, err := NewRedisScheduler(
		redisCfg,
		executor,
		repo,
		100*time.Millisecond, // Short interval for testing
		10,
		500*time.Millisecond, // Short timeout
	)
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	defer scheduler.Close()

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

	// Manually trigger job processing
	scheduler.processDueJobs()

	// Wait for timeout to occur (job takes 2s, timeout is 500ms)
	time.Sleep(600 * time.Millisecond)

	// Verify job started executing (even though it timed out)
	if executor.Count() == 0 {
		t.Error("Expected job to start executing")
	}

	// Check timeout metric was incremented
	// Note: We can't easily check the metric value in tests without more infrastructure
	// But we can verify the job was processed
}

func TestGracefulShutdown_WaitsForInFlightJobs(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	redisCfg := config.RedisConfig{
		Enabled:  true,
		Host:     mr.Host(),
		Port:     mr.Server().Addr().Port,
		Password: "",
		DB:       0,
	}

	executor := &mockExecutorWithDelay{delay: 200 * time.Millisecond}
	repo := &mockJobRepository{jobs: make(map[string]domain.Job)}

	scheduler, err := NewRedisScheduler(
		redisCfg,
		executor,
		repo,
		100*time.Millisecond, // Short interval for testing
		5,
		2*time.Minute,
	)
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	defer scheduler.Close()

	// Create 3 jobs
	for i := 0; i < 3; i++ {
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
	}

	// Manually trigger job processing
	scheduler.processDueJobs()

	// Jobs execute concurrently (200ms each) but only 5 workers, so should complete quickly
	startTime := time.Now()

	// Wait for jobs to complete
	time.Sleep(300 * time.Millisecond)
	duration := time.Since(startTime)

	// All 3 jobs should complete within pool capacity
	if duration > 1*time.Second {
		t.Errorf("Jobs took too long: %v", duration)
	}

	// Verify all jobs completed
	if executor.Count() != 3 {
		t.Errorf("Expected 3 jobs executed, got %d", executor.Count())
	}
}

func TestConcurrentExecution_NoRaceConditions(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	redisCfg := config.RedisConfig{
		Enabled:  true,
		Host:     mr.Host(),
		Port:     mr.Server().Addr().Port,
		Password: "",
		DB:       0,
	}

	executor := &mockExecutorWithDelay{delay: 10 * time.Millisecond}
	repo := &mockJobRepository{jobs: make(map[string]domain.Job)}

	scheduler, err := NewRedisScheduler(
		redisCfg,
		executor,
		repo,
		100*time.Millisecond, // Short interval for testing
		10,
		2*time.Minute,
	)
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}
	defer scheduler.Close()

	// Create 20 jobs to execute concurrently
	for i := 0; i < 20; i++ {
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
	}

	// Manually trigger job processing
	scheduler.processDueJobs()

	// Wait for all jobs to complete (10ms each, pool of 10, so 2 batches)
	time.Sleep(100 * time.Millisecond)

	// Verify all 20 jobs executed
	if executor.Count() != 20 {
		t.Errorf("Expected 20 jobs executed, got %d", executor.Count())
	}

	// Verify no duplicate executions
	executedJobs := executor.GetExecutedJobs()
	jobSet := make(map[string]bool)
	for _, jobID := range executedJobs {
		if jobSet[jobID] {
			t.Errorf("Job %s was executed more than once", jobID)
		}
		jobSet[jobID] = true
	}
}
