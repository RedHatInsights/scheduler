package usecases

import (
	"context"
	"testing"
	"time"

	"insights-scheduler/internal/core/domain"
)

func TestCalculateNextRunAt(t *testing.T) {
	tests := []struct {
		name     string
		schedule string
		wantErr  bool
	}{
		{
			name:     "Valid every 10 minutes",
			schedule: "*/10 * * * *",
			wantErr:  false,
		},
		{
			name:     "Valid hourly",
			schedule: "0 * * * *",
			wantErr:  false,
		},
		{
			name:     "Valid daily at midnight",
			schedule: "0 0 * * *",
			wantErr:  false,
		},
		{
			name:     "Valid monthly",
			schedule: "0 0 1 * *",
			wantErr:  false,
		},
		{
			name:     "Valid weekdays at 9am",
			schedule: "0 9 * * MON-FRI",
			wantErr:  false,
		},
		{
			name:     "Invalid cron expression",
			schedule: "invalid",
			wantErr:  true,
		},
		{
			name:     "Invalid 6-field cron (with seconds)",
			schedule: "0 */10 * * * *",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextRunAt, err := calculateNextRunAt(tt.schedule, "UTC")

			if tt.wantErr {
				if err == nil {
					t.Errorf("calculateNextRunAt() expected error, got nil")
				}
				if nextRunAt != nil {
					t.Errorf("calculateNextRunAt() expected nil result on error, got %v", nextRunAt)
				}
			} else {
				if err != nil {
					t.Errorf("calculateNextRunAt() unexpected error: %v", err)
				}
				if nextRunAt == nil {
					t.Error("calculateNextRunAt() expected non-nil result, got nil")
				} else {
					// Verify next run is in the future
					if !nextRunAt.After(time.Now()) {
						t.Errorf("calculateNextRunAt() expected future time, got %v", nextRunAt)
					}
				}
			}
		})
	}
}

func TestCalculateNextRunAtTimings(t *testing.T) {
	// Test specific timing expectations
	tests := []struct {
		name            string
		schedule        string
		maxDuration     time.Duration
		minDuration     time.Duration
		descriptionHint string
	}{
		{
			name:            "Every 10 minutes should be within 10 minutes",
			schedule:        "*/10 * * * *",
			maxDuration:     10 * time.Minute,
			minDuration:     0,
			descriptionHint: "should be less than 10 minutes in the future",
		},
		{
			name:            "Hourly should be within 1 hour",
			schedule:        "0 * * * *",
			maxDuration:     60 * time.Minute,
			minDuration:     0,
			descriptionHint: "should be less than 1 hour in the future",
		},
		{
			name:            "Daily should be within 24 hours",
			schedule:        "0 0 * * *",
			maxDuration:     24 * time.Hour,
			minDuration:     0,
			descriptionHint: "should be less than 24 hours in the future",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now()
			nextRunAt, err := calculateNextRunAt(tt.schedule, "UTC")

			if err != nil {
				t.Fatalf("calculateNextRunAt() unexpected error: %v", err)
			}

			duration := nextRunAt.Sub(now)

			if duration < tt.minDuration {
				t.Errorf("Next run %v too soon (duration: %v, min: %v)", nextRunAt, duration, tt.minDuration)
			}

			if duration > tt.maxDuration {
				t.Errorf("Next run %v too far (duration: %v, max: %v) - %s", nextRunAt, duration, tt.maxDuration, tt.descriptionHint)
			}
		})
	}
}

// Mock implementations for testing

type mockJobRepository struct {
	jobs      map[string]domain.Job
	saveFunc  func(job domain.Job) error
	findFunc  func(id string) (domain.Job, error)
	deleteErr error
}

func newMockJobRepository() *mockJobRepository {
	return &mockJobRepository{
		jobs: make(map[string]domain.Job),
	}
}

func (m *mockJobRepository) Save(job domain.Job) error {
	if m.saveFunc != nil {
		return m.saveFunc(job)
	}
	m.jobs[job.ID] = job
	return nil
}

func (m *mockJobRepository) FindByID(id string) (domain.Job, error) {
	if m.findFunc != nil {
		return m.findFunc(id)
	}
	job, ok := m.jobs[id]
	if !ok {
		return domain.Job{}, domain.ErrJobNotFound
	}
	return job, nil
}

func (m *mockJobRepository) FindAll() ([]domain.Job, error) {
	jobs := make([]domain.Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (m *mockJobRepository) FindByOrgID(orgID string) ([]domain.Job, error) {
	jobs := make([]domain.Job, 0)
	for _, job := range m.jobs {
		if job.OrgID == orgID {
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

func (m *mockJobRepository) FindByUserID(userID string, offset, limit int) ([]domain.Job, int, error) {
	jobs := make([]domain.Job, 0)
	for _, job := range m.jobs {
		if job.UserID == userID {
			jobs = append(jobs, job)
		}
	}
	return jobs, len(jobs), nil
}

func (m *mockJobRepository) Delete(id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.jobs, id)
	return nil
}

type mockSchedulingService struct{}

func (m *mockSchedulingService) ShouldRun(job domain.Job, currentTime time.Time) bool {
	return false
}

type mockJobExecutor struct {
	executeFunc func(job domain.Job) error
}

func (m *mockJobExecutor) Execute(job domain.Job) error {
	if m.executeFunc != nil {
		return m.executeFunc(job)
	}
	return nil
}

func TestCreateJobSetsNextRunAt(t *testing.T) {
	repo := newMockJobRepository()
	scheduler := &mockSchedulingService{}
	executor := &mockJobExecutor{}

	service := NewJobService(repo, scheduler, executor)

	schedule := "0 * * * *" // Every hour
	job, err := service.CreateJob(context.Background(), "Test Job", "org-123", "testuser", "user-123", schedule, "UTC", domain.PayloadExport, map[string]interface{}{})

	if err != nil {
		t.Fatalf("CreateJob() unexpected error: %v", err)
	}

	if job.NextRunAt == nil {
		t.Error("CreateJob() expected NextRunAt to be set, got nil")
	} else {
		// Verify next run is in the future
		if !job.NextRunAt.After(time.Now()) {
			t.Errorf("CreateJob() NextRunAt should be in the future, got %v", job.NextRunAt)
		}

		// Verify next run is within 1 hour (for hourly schedule)
		if job.NextRunAt.Sub(time.Now()) > time.Hour {
			t.Errorf("CreateJob() NextRunAt too far in future for hourly schedule: %v", job.NextRunAt)
		}
	}

	// Verify it was saved to repository
	savedJob, err := repo.FindByID(job.ID)
	if err != nil {
		t.Fatalf("FindByID() unexpected error: %v", err)
	}

	if savedJob.NextRunAt == nil {
		t.Error("Saved job should have NextRunAt set")
	}
}

func TestRunJobUpdatesLastRunAndNextRunAt(t *testing.T) {
	repo := newMockJobRepository()
	scheduler := &mockSchedulingService{}
	executor := &mockJobExecutor{}

	service := NewJobService(repo, scheduler, executor)

	// Create a job
	job := domain.NewJob("Test Job", "org-123", "testuser", "user-123", "*/10 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	repo.Save(job)

	timeBefore := time.Now()

	// Run the job
	err := service.RunJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("RunJob() unexpected error: %v", err)
	}

	timeAfter := time.Now()

	// Retrieve the updated job
	updatedJob, err := repo.FindByID(job.ID)
	if err != nil {
		t.Fatalf("FindByID() unexpected error: %v", err)
	}

	// Verify LastRunAt was set
	if updatedJob.LastRunAt == nil {
		t.Error("RunJob() expected LastRunAt to be set, got nil")
	} else {
		if updatedJob.LastRunAt.Before(timeBefore) || updatedJob.LastRunAt.After(timeAfter) {
			t.Errorf("RunJob() LastRunAt %v not within expected range [%v, %v]", updatedJob.LastRunAt, timeBefore, timeAfter)
		}
	}

	// Verify NextRunAt was recalculated
	if updatedJob.NextRunAt == nil {
		t.Error("RunJob() expected NextRunAt to be recalculated, got nil")
	} else {
		// For */10 schedule, next run should be within 10 minutes
		if updatedJob.NextRunAt.Sub(timeAfter) > 10*time.Minute {
			t.Errorf("RunJob() NextRunAt too far in future for 10-minute schedule: %v", updatedJob.NextRunAt)
		}
	}
}

func TestUpdateJobRecalculatesNextRunAtWhenScheduleChanges(t *testing.T) {
	repo := newMockJobRepository()
	scheduler := &mockSchedulingService{}
	executor := &mockJobExecutor{}

	service := NewJobService(repo, scheduler, executor)

	// Create a job with hourly schedule
	job := domain.NewJob("Test Job", "org-123", "testuser", "user-123", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})

	// Set a next_run
	now := time.Now()
	nextRunAtBefore := now.Add(30 * time.Minute)
	job = job.WithNextRunAt(nextRunAtBefore)
	repo.Save(job)

	// Update to daily schedule
	newSchedule := "0 0 * * *" // Daily at midnight
	updatedJob, err := service.UpdateJob(context.Background(), job.ID, "Test Job", "org-123", "testuser", "user-123", newSchedule, domain.PayloadExport, map[string]interface{}{}, "scheduled")

	if err != nil {
		t.Fatalf("UpdateJob() unexpected error: %v", err)
	}

	// Verify NextRunAt was recalculated
	if updatedJob.NextRunAt == nil {
		t.Error("UpdateJob() expected NextRunAt to be recalculated, got nil")
	} else {
		// NextRunAt should have changed (daily schedule will have different next run than hourly)
		// For daily schedule at midnight, next run should be within 24 hours
		if updatedJob.NextRunAt.Sub(now) > 24*time.Hour {
			t.Errorf("UpdateJob() NextRunAt too far in future for daily schedule: %v", updatedJob.NextRunAt)
		}
	}
}

func TestResumeJobRecalculatesNextRunAt(t *testing.T) {
	repo := newMockJobRepository()
	scheduler := &mockSchedulingService{}
	executor := &mockJobExecutor{}

	service := NewJobService(repo, scheduler, executor)

	// Create a paused job
	job := domain.NewJob("Test Job", "org-123", "testuser", "user-123", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	job = job.WithStatus(domain.StatusPaused)

	// Set an old next_run
	oldNextRunAt := time.Now().Add(-1 * time.Hour)
	job = job.WithNextRunAt(oldNextRunAt)
	repo.Save(job)

	// Resume the job
	resumedJob, err := service.ResumeJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("ResumeJob() unexpected error: %v", err)
	}

	// Verify NextRunAt was recalculated
	if resumedJob.NextRunAt == nil {
		t.Error("ResumeJob() expected NextRunAt to be recalculated, got nil")
	} else {
		// NextRunAt should be in the future (not the old past value)
		if !resumedJob.NextRunAt.After(time.Now()) {
			t.Errorf("ResumeJob() NextRunAt should be in future, got %v", resumedJob.NextRunAt)
		}

		// For hourly schedule, should be within 1 hour
		if resumedJob.NextRunAt.Sub(time.Now()) > time.Hour {
			t.Errorf("ResumeJob() NextRunAt too far in future for hourly schedule: %v", resumedJob.NextRunAt)
		}
	}
}

func TestCreateJobWithInvalidScheduleDoesNotSetNextRunAt(t *testing.T) {
	repo := newMockJobRepository()
	scheduler := &mockSchedulingService{}
	executor := &mockJobExecutor{}

	service := NewJobService(repo, scheduler, executor)

	// Try to create a job with invalid schedule
	_, err := service.CreateJob(context.Background(), "Test Job", "org-123", "testuser", "user-123", "invalid", "UTC", domain.PayloadExport, map[string]interface{}{})

	if err != domain.ErrInvalidSchedule {
		t.Errorf("CreateJob() expected ErrInvalidSchedule, got %v", err)
	}
}
