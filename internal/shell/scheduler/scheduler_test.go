package scheduler

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"insights-scheduler/internal/core/domain"
)

// Mock SchedulerJobService for testing
type mockSchedulerJobService struct {
	getJobFunc                        func(ctx context.Context, id string) (domain.Job, error)
	executeScheduledJobFunc           func(job domain.Job) error
	executeScheduledJobWithJobRunFunc func(job domain.Job, jobRunID string) error
	listJobsFunc                      func() ([]domain.Job, error)
}

func (m *mockSchedulerJobService) GetJob(ctx context.Context, id string) (domain.Job, error) {
	if m.getJobFunc != nil {
		return m.getJobFunc(ctx, id)
	}
	return domain.Job{}, domain.ErrJobNotFound
}

func (m *mockSchedulerJobService) ExecuteScheduledJob(job domain.Job) error {
	if m.executeScheduledJobFunc != nil {
		return m.executeScheduledJobFunc(job)
	}
	return nil
}

func (m *mockSchedulerJobService) ExecuteScheduledJobWithJobRun(job domain.Job, jobRunID string) error {
	if m.executeScheduledJobWithJobRunFunc != nil {
		return m.executeScheduledJobWithJobRunFunc(job, jobRunID)
	}
	return nil
}

func (m *mockSchedulerJobService) ListJobs() ([]domain.Job, error) {
	if m.listJobsFunc != nil {
		return m.listJobsFunc()
	}
	return []domain.Job{}, nil
}

func TestCronScheduler_ScheduleJobImmediately(t *testing.T) {
	executedJobs := []domain.Job{}
	executedJobRunIDs := []string{}

	// Create a test job
	job := domain.NewJob("Test Job", "org-123", "user-123", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{
		"format": "json",
	})

	jobService := &mockSchedulerJobService{
		getJobFunc: func(ctx context.Context, id string) (domain.Job, error) {
			// Return the same job that was passed in
			if id == job.ID {
				return job, nil
			}
			return domain.Job{}, domain.ErrJobNotFound
		},
		executeScheduledJobWithJobRunFunc: func(executedJob domain.Job, jobRunID string) error {
			executedJobs = append(executedJobs, executedJob)
			executedJobRunIDs = append(executedJobRunIDs, jobRunID)
			return nil
		},
	}

	// Create a test logger (suppress output during tests)
	testLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	scheduler := NewCronScheduler(jobService, testLogger)

	testJobRunID := "test-job-run-123"

	// Schedule job immediately with job run ID
	err := scheduler.ScheduleJobImmediately(job, testJobRunID)
	if err != nil {
		t.Fatalf("ScheduleJobImmediately() unexpected error: %v", err)
	}

	// Verify ExecuteScheduledJobWithJobRun was called
	if len(executedJobs) != 1 {
		t.Errorf("Expected ExecuteScheduledJobWithJobRun to be called once, got %d calls", len(executedJobs))
	}

	if len(executedJobs) > 0 && executedJobs[0].ID != job.ID {
		t.Errorf("ExecuteScheduledJobWithJobRun called with wrong job ID: got %s, want %s", executedJobs[0].ID, job.ID)
	}

	if len(executedJobRunIDs) > 0 && executedJobRunIDs[0] != testJobRunID {
		t.Errorf("ExecuteScheduledJobWithJobRun called with wrong job run ID: got %s, want %s", executedJobRunIDs[0], testJobRunID)
	}
}

func TestCronScheduler_ScheduleJobImmediately_JobNotFound(t *testing.T) {
	jobService := &mockSchedulerJobService{
		getJobFunc: func(ctx context.Context, id string) (domain.Job, error) {
			return domain.Job{}, domain.ErrJobNotFound
		},
	}

	// Create a test logger (suppress output during tests)
	testLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	scheduler := NewCronScheduler(jobService, testLogger)

	// Create a test job
	job := domain.NewJob("Test Job", "org-123", "user-123", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})

	// Schedule job immediately
	err := scheduler.ScheduleJobImmediately(job, "test-run-id")
	if err != domain.ErrJobNotFound {
		t.Errorf("ScheduleJobImmediately() expected ErrJobNotFound, got %v", err)
	}
}

func TestCronScheduler_ScheduleJobImmediately_ExecutionError(t *testing.T) {
	expectedError := domain.ErrInvalidPayload

	jobService := &mockSchedulerJobService{
		getJobFunc: func(ctx context.Context, id string) (domain.Job, error) {
			return domain.NewJob("Test Job", "org-123", "user-123", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{}), nil
		},
		executeScheduledJobWithJobRunFunc: func(job domain.Job, jobRunID string) error {
			return expectedError
		},
	}

	// Create a test logger (suppress output during tests)
	testLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	scheduler := NewCronScheduler(jobService, testLogger)

	job := domain.NewJob("Test Job", "org-123", "user-123", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})

	// Schedule job immediately
	err := scheduler.ScheduleJobImmediately(job, "test-run-id")
	if err != expectedError {
		t.Errorf("ScheduleJobImmediately() expected error %v, got %v", expectedError, err)
	}
}

func TestCronScheduler_ScheduleJob(t *testing.T) {
	jobService := &mockSchedulerJobService{}
	// Create a test logger (suppress output during tests)
	testLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	scheduler := NewCronScheduler(jobService, testLogger)

	// Create a scheduled job
	job := domain.NewJob("Test Job", "org-123", "user-123", "*/5 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})

	// Schedule the job
	err := scheduler.ScheduleJob(job)
	if err != nil {
		t.Fatalf("ScheduleJob() unexpected error: %v", err)
	}

	// Verify job was added to the scheduler
	scheduler.mu.RLock()
	_, exists := scheduler.jobEntries[job.ID]
	scheduler.mu.RUnlock()

	if !exists {
		t.Error("ScheduleJob() job was not added to scheduler entries")
	}
}

func TestCronScheduler_ScheduleJob_SkipsPausedJobs(t *testing.T) {
	jobService := &mockSchedulerJobService{}
	// Create a test logger (suppress output during tests)
	testLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	scheduler := NewCronScheduler(jobService, testLogger)

	// Create a paused job
	job := domain.NewJob("Test Job", "org-123", "user-123", "*/5 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	job = job.WithStatus(domain.StatusPaused)

	// Try to schedule the paused job
	err := scheduler.ScheduleJob(job)
	if err != nil {
		t.Fatalf("ScheduleJob() unexpected error: %v", err)
	}

	// Verify job was NOT added to the scheduler
	scheduler.mu.RLock()
	_, exists := scheduler.jobEntries[job.ID]
	scheduler.mu.RUnlock()

	if exists {
		t.Error("ScheduleJob() paused job should not be added to scheduler entries")
	}
}

func TestCronScheduler_UnscheduleJob(t *testing.T) {
	jobService := &mockSchedulerJobService{}
	// Create a test logger (suppress output during tests)
	testLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	scheduler := NewCronScheduler(jobService, testLogger)

	// Create and schedule a job
	job := domain.NewJob("Test Job", "org-123", "user-123", "*/5 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	err := scheduler.ScheduleJob(job)
	if err != nil {
		t.Fatalf("ScheduleJob() unexpected error: %v", err)
	}

	// Verify job exists
	scheduler.mu.RLock()
	_, exists := scheduler.jobEntries[job.ID]
	scheduler.mu.RUnlock()
	if !exists {
		t.Fatal("Job should exist before unscheduling")
	}

	// Unschedule the job
	scheduler.UnscheduleJob(job.ID)

	// Verify job was removed
	scheduler.mu.RLock()
	_, exists = scheduler.jobEntries[job.ID]
	scheduler.mu.RUnlock()

	if exists {
		t.Error("UnscheduleJob() job should be removed from scheduler entries")
	}
}

func TestCronScheduler_ScheduleJob_UpdatesExistingJob(t *testing.T) {
	jobService := &mockSchedulerJobService{}
	// Create a test logger (suppress output during tests)
	testLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	scheduler := NewCronScheduler(jobService, testLogger)

	// Create and schedule a job
	job := domain.NewJob("Test Job", "org-123", "user-123", "*/5 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	err := scheduler.ScheduleJob(job)
	if err != nil {
		t.Fatalf("ScheduleJob() first call unexpected error: %v", err)
	}

	scheduler.mu.RLock()
	firstEntryID := scheduler.jobEntries[job.ID]
	scheduler.mu.RUnlock()

	// Schedule the same job again with a different schedule
	updatedJob := domain.NewJob("Test Job", "org-123", "user-123", "*/10 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	// Use the same ID as the original job to simulate an update
	updatedJob.ID = job.ID
	err = scheduler.ScheduleJob(updatedJob)
	if err != nil {
		t.Fatalf("ScheduleJob() second call unexpected error: %v", err)
	}

	scheduler.mu.RLock()
	secondEntryID := scheduler.jobEntries[job.ID]
	scheduler.mu.RUnlock()

	// Verify the entry ID changed (old entry was removed, new one added)
	if firstEntryID == secondEntryID {
		t.Error("ScheduleJob() should create a new entry when updating an existing job")
	}
}
