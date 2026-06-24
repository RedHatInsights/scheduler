package usecases

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"insights-scheduler/internal/core/domain"
)

// Mock executor that can be configured to fail
type failingMockExecutor struct {
	shouldFail bool
	callCount  int
}

func (e *failingMockExecutor) Execute(job domain.Job) error {
	e.callCount++
	if e.shouldFail {
		return errors.New("simulated execution failure")
	}
	return nil
}

func (e *failingMockExecutor) ExecuteWithJobRun(job domain.Job, jobRunID string) error {
	return e.Execute(job)
}

func (e *failingMockExecutor) Wait() {
	// No-op for tests
}

func TestJobFailureCounterIncrementsOnFailure(t *testing.T) {
	repo := newMockJobRepository()
	scheduler := &mockSchedulingService{}
	executor := &failingMockExecutor{shouldFail: true}

	service := NewJobService(repo, scheduler, executor, 3)

	// Create a job
	job, err := service.CreateJob(context.Background(), "Test Job", "org-123", "user-123", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	if err != nil {
		t.Fatalf("CreateJob() failed: %v", err)
	}

	if job.ConsecutiveFailures != 0 {
		t.Errorf("New job should have 0 consecutive failures, got %d", job.ConsecutiveFailures)
	}

	// Execute via scheduler (not manual API run) - it should fail
	err = service.ExecuteScheduledJobWithJobRun(job, "run-1")
	if err != nil {
		t.Fatalf("ExecuteScheduledJobWithJobRun() failed: %v", err)
	}

	// Check the job's failure count increased
	updatedJob, err := service.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("GetJob() failed: %v", err)
	}

	if updatedJob.ConsecutiveFailures != 1 {
		t.Errorf("After 1 failure, expected consecutive_failures=1, got %d", updatedJob.ConsecutiveFailures)
	}

	if updatedJob.LastFailedAt == nil {
		t.Error("After failure, expected last_failed_at to be set, got nil")
	}

	if updatedJob.Status != domain.StatusFailed {
		t.Errorf("After 1 failure (below threshold), expected status=failed, got %s", updatedJob.Status)
	}
}

func TestJobFailureCounterResetsOnSuccess(t *testing.T) {
	repo := newMockJobRepository()
	scheduler := &mockSchedulingService{}
	executor := &failingMockExecutor{shouldFail: true}

	service := NewJobService(repo, scheduler, executor, 3)

	// Create a job
	job, err := service.CreateJob(context.Background(), "Test Job", "org-123", "user-123", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	if err != nil {
		t.Fatalf("CreateJob() failed: %v", err)
	}

	// Fail the job once via scheduler
	err = service.ExecuteScheduledJobWithJobRun(job, "run-1")
	if err != nil {
		t.Fatalf("ExecuteScheduledJobWithJobRun() failed: %v", err)
	}

	updatedJob, _ := service.GetJob(context.Background(), job.ID)
	if updatedJob.ConsecutiveFailures != 1 {
		t.Errorf("After 1 failure, expected consecutive_failures=1, got %d", updatedJob.ConsecutiveFailures)
	}

	// Now make it succeed
	executor.shouldFail = false
	err = service.ExecuteScheduledJobWithJobRun(updatedJob, "run-2")
	if err != nil {
		t.Fatalf("ExecuteScheduledJobWithJobRun() failed: %v", err)
	}

	// Check the failure count reset
	updatedJob, err = service.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("GetJob() failed: %v", err)
	}

	if updatedJob.ConsecutiveFailures != 0 {
		t.Errorf("After success, expected consecutive_failures=0, got %d", updatedJob.ConsecutiveFailures)
	}

	if updatedJob.LastFailedAt != nil {
		t.Error("After success, expected last_failed_at to be cleared, got non-nil")
	}

	if updatedJob.Status != domain.StatusScheduled {
		t.Errorf("After success, expected status=scheduled, got %s", updatedJob.Status)
	}
}

func TestJobAutoPausesAfterThresholdFailures(t *testing.T) {
	repo := newMockJobRepository()
	scheduler := &mockSchedulingService{}
	executor := &failingMockExecutor{shouldFail: true}

	threshold := 3
	service := NewJobService(repo, scheduler, executor, threshold)

	// Create a job
	job, err := service.CreateJob(context.Background(), "Test Job", "org-123", "user-123", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	if err != nil {
		t.Fatalf("CreateJob() failed: %v", err)
	}

	// Fail the job threshold-1 times via scheduler - should NOT pause yet
	currentJob := job
	for i := 1; i < threshold; i++ {
		err = service.ExecuteScheduledJobWithJobRun(currentJob, fmt.Sprintf("run-%d", i))
		if err != nil {
			t.Fatalf("ExecuteScheduledJobWithJobRun() iteration %d failed: %v", i, err)
		}

		currentJob, _ = service.GetJob(context.Background(), job.ID)
		if currentJob.ConsecutiveFailures != i {
			t.Errorf("After %d failures, expected consecutive_failures=%d, got %d", i, i, currentJob.ConsecutiveFailures)
		}

		if currentJob.Status == domain.StatusPaused {
			t.Errorf("After %d failures (below threshold %d), job should not be paused yet", i, threshold)
		}
	}

	// Fail one more time - should trigger auto-pause
	err = service.ExecuteScheduledJobWithJobRun(currentJob, fmt.Sprintf("run-%d", threshold))
	if err != nil {
		t.Fatalf("ExecuteScheduledJobWithJobRun() final failure failed: %v", err)
	}

	finalJob, err := service.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("GetJob() failed: %v", err)
	}

	if finalJob.ConsecutiveFailures != threshold {
		t.Errorf("After %d failures, expected consecutive_failures=%d, got %d", threshold, threshold, finalJob.ConsecutiveFailures)
	}

	if finalJob.Status != domain.StatusPaused {
		t.Errorf("After reaching threshold %d, expected status=paused, got %s", threshold, finalJob.Status)
	}
}

func TestJobAutoPauseDisabledWhenThresholdIsZero(t *testing.T) {
	repo := newMockJobRepository()
	scheduler := &mockSchedulingService{}
	executor := &failingMockExecutor{shouldFail: true}

	service := NewJobService(repo, scheduler, executor, 0) // 0 = disabled

	// Create a job
	job, err := service.CreateJob(context.Background(), "Test Job", "org-123", "user-123", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	if err != nil {
		t.Fatalf("CreateJob() failed: %v", err)
	}

	// Fail the job 10 times via scheduler - should NEVER pause when threshold is 0
	currentJob := job
	for i := 1; i <= 10; i++ {
		err = service.ExecuteScheduledJobWithJobRun(currentJob, fmt.Sprintf("run-%d", i))
		if err != nil {
			t.Fatalf("ExecuteScheduledJobWithJobRun() iteration %d failed: %v", i, err)
		}

		currentJob, _ = service.GetJob(context.Background(), job.ID)
		if currentJob.ConsecutiveFailures != i {
			t.Errorf("After %d failures, expected consecutive_failures=%d, got %d", i, i, currentJob.ConsecutiveFailures)
		}

		if currentJob.Status == domain.StatusPaused {
			t.Errorf("With threshold=0 (disabled), job should never auto-pause, but it did after %d failures", i)
		}
	}
}

func TestResumeJobResetsFailureCounter(t *testing.T) {
	repo := newMockJobRepository()
	scheduler := &mockSchedulingService{}
	executor := &failingMockExecutor{shouldFail: true}

	service := NewJobService(repo, scheduler, executor, 3)

	// Create a job
	job, err := service.CreateJob(context.Background(), "Test Job", "org-123", "user-123", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	if err != nil {
		t.Fatalf("CreateJob() failed: %v", err)
	}

	// Fail it twice via scheduler
	service.ExecuteScheduledJobWithJobRun(job, "run-1")
	job, _ = service.GetJob(context.Background(), job.ID)
	service.ExecuteScheduledJobWithJobRun(job, "run-2")

	updatedJob, _ := service.GetJob(context.Background(), job.ID)
	if updatedJob.ConsecutiveFailures != 2 {
		t.Errorf("After 2 failures, expected consecutive_failures=2, got %d", updatedJob.ConsecutiveFailures)
	}

	// Manually pause the job
	pausedJob, err := service.PauseJob(job.ID)
	if err != nil {
		t.Fatalf("PauseJob() failed: %v", err)
	}

	if pausedJob.Status != domain.StatusPaused {
		t.Errorf("After pause, expected status=paused, got %s", pausedJob.Status)
	}

	// Resume the job - should reset failure counter
	resumedJob, err := service.ResumeJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("ResumeJob() failed: %v", err)
	}

	if resumedJob.ConsecutiveFailures != 0 {
		t.Errorf("After resume, expected consecutive_failures=0, got %d", resumedJob.ConsecutiveFailures)
	}

	if resumedJob.LastFailedAt != nil {
		t.Error("After resume, expected last_failed_at to be cleared, got non-nil")
	}

	if resumedJob.Status != domain.StatusScheduled {
		t.Errorf("After resume, expected status=scheduled, got %s", resumedJob.Status)
	}
}

func TestExecuteScheduledJobWithJobRunAutoPauseLogic(t *testing.T) {
	repo := newMockJobRepository()
	scheduler := &mockSchedulingService{}
	executor := &failingMockExecutor{shouldFail: true}

	threshold := 2
	service := NewJobService(repo, scheduler, executor, threshold)

	// Create a job
	job, err := service.CreateJob(context.Background(), "Test Job", "org-123", "user-123", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	if err != nil {
		t.Fatalf("CreateJob() failed: %v", err)
	}

	// Fail using ExecuteScheduledJobWithJobRun (used by Redis scheduler)
	err = service.ExecuteScheduledJobWithJobRun(job, "run-1")
	if err != nil {
		t.Fatalf("ExecuteScheduledJobWithJobRun() run 1 failed: %v", err)
	}

	updatedJob, _ := service.GetJob(context.Background(), job.ID)
	if updatedJob.ConsecutiveFailures != 1 {
		t.Errorf("After 1 failure, expected consecutive_failures=1, got %d", updatedJob.ConsecutiveFailures)
	}

	// Fail again - should trigger auto-pause
	err = service.ExecuteScheduledJobWithJobRun(updatedJob, "run-2")
	if err != nil {
		t.Fatalf("ExecuteScheduledJobWithJobRun() run 2 failed: %v", err)
	}

	finalJob, _ := service.GetJob(context.Background(), job.ID)
	if finalJob.ConsecutiveFailures != 2 {
		t.Errorf("After 2 failures, expected consecutive_failures=2, got %d", finalJob.ConsecutiveFailures)
	}

	if finalJob.Status != domain.StatusPaused {
		t.Errorf("After reaching threshold, expected status=paused, got %s", finalJob.Status)
	}
}

func TestAutoPauseClearsNextRunAt(t *testing.T) {
	repo := newMockJobRepository()
	scheduler := &mockSchedulingService{}
	executor := &failingMockExecutor{shouldFail: true}

	threshold := 2
	service := NewJobService(repo, scheduler, executor, threshold)

	// Create a job (will have next_run_at set)
	job, err := service.CreateJob(context.Background(), "Test Job", "org-123", "user-123", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	if err != nil {
		t.Fatalf("CreateJob() failed: %v", err)
	}

	if job.NextRunAt == nil {
		t.Fatal("New job should have next_run_at set")
	}

	// Fail the job twice via scheduler to trigger auto-pause
	service.ExecuteScheduledJobWithJobRun(job, "run-1")
	job, _ = service.GetJob(context.Background(), job.ID)
	service.ExecuteScheduledJobWithJobRun(job, "run-2")

	pausedJob, _ := service.GetJob(context.Background(), job.ID)

	if pausedJob.Status != domain.StatusPaused {
		t.Errorf("Expected job to be paused, got status=%s", pausedJob.Status)
	}

	if pausedJob.NextRunAt != nil {
		t.Errorf("Auto-paused job should have next_run_at=nil, got %v", pausedJob.NextRunAt)
	}
}

func TestManualPauseClearsNextRunAt(t *testing.T) {
	repo := newMockJobRepository()
	scheduler := &mockSchedulingService{}
	executor := &failingMockExecutor{shouldFail: false}

	service := NewJobService(repo, scheduler, executor, 3)

	// Create a job (will have next_run_at set)
	job, err := service.CreateJob(context.Background(), "Test Job", "org-123", "user-123", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	if err != nil {
		t.Fatalf("CreateJob() failed: %v", err)
	}

	if job.NextRunAt == nil {
		t.Fatal("New job should have next_run_at set")
	}

	// Manually pause the job
	pausedJob, err := service.PauseJob(job.ID)
	if err != nil {
		t.Fatalf("PauseJob() failed: %v", err)
	}

	if pausedJob.NextRunAt != nil {
		t.Errorf("Manually paused job should have next_run_at=nil, got %v", pausedJob.NextRunAt)
	}
}

func TestManualAPIRunDoesNotTrackFailures(t *testing.T) {
	repo := newMockJobRepository()
	scheduler := &mockSchedulingService{}
	executor := &failingMockExecutor{shouldFail: true}

	service := NewJobService(repo, scheduler, executor, 3)

	// Create a job
	job, err := service.CreateJob(context.Background(), "Test Job", "org-123", "user-123", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	if err != nil {
		t.Fatalf("CreateJob() failed: %v", err)
	}

	if job.ConsecutiveFailures != 0 {
		t.Errorf("New job should have 0 consecutive failures, got %d", job.ConsecutiveFailures)
	}

	// Manually run via API (should NOT track failures)
	_, err = service.RunJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("RunJob() failed: %v", err)
	}

	// Verify failure was NOT tracked
	updatedJob, err := service.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("GetJob() failed: %v", err)
	}

	if updatedJob.ConsecutiveFailures != 0 {
		t.Errorf("Manual API run should NOT track failures, expected consecutive_failures=0, got %d", updatedJob.ConsecutiveFailures)
	}

	if updatedJob.LastFailedAt != nil {
		t.Error("Manual API run should NOT set last_failed_at, got non-nil")
	}

	// Status should still be failed (execution failed)
	if updatedJob.Status != domain.StatusFailed {
		t.Errorf("After manual run failure, expected status=failed, got %s", updatedJob.Status)
	}

	// Run it 10 more times manually - should NEVER increment consecutive_failures or auto-pause
	for i := 0; i < 10; i++ {
		_, err = service.RunJob(context.Background(), updatedJob.ID)
		if err != nil {
			t.Fatalf("RunJob() iteration %d failed: %v", i, err)
		}

		updatedJob, _ = service.GetJob(context.Background(), updatedJob.ID)
		if updatedJob.ConsecutiveFailures != 0 {
			t.Errorf("After %d manual runs, consecutive_failures should still be 0, got %d", i+2, updatedJob.ConsecutiveFailures)
		}

		if updatedJob.Status == domain.StatusPaused {
			t.Errorf("Manual API runs should NEVER trigger auto-pause, but job was paused after %d manual runs", i+2)
		}
	}
}
