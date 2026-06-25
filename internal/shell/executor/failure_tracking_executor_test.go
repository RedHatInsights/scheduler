package executor

import (
	"context"
	"errors"
	"testing"

	"insights-scheduler/internal/core/domain"
)

// Mock notifier for testing
type mockNotifier struct {
	jobCompleteCalls   []*ExportCompletionNotification
	jobAutoPausedCalls []*JobAutoPausedNotification
}

func (m *mockNotifier) JobComplete(ctx context.Context, notification *ExportCompletionNotification) error {
	m.jobCompleteCalls = append(m.jobCompleteCalls, notification)
	return nil
}

func (m *mockNotifier) JobAutoPaused(ctx context.Context, notification *JobAutoPausedNotification) error {
	m.jobAutoPausedCalls = append(m.jobAutoPausedCalls, notification)
	return nil
}

// Mock repository for testing
type mockJobRepo struct {
	jobs map[string]domain.Job
}

func newMockJobRepo() *mockJobRepo {
	return &mockJobRepo{
		jobs: make(map[string]domain.Job),
	}
}

func (r *mockJobRepo) Save(job domain.Job) error {
	r.jobs[job.ID] = job
	return nil
}

func (r *mockJobRepo) FindByID(id string) (domain.Job, error) {
	job, ok := r.jobs[id]
	if !ok {
		return domain.Job{}, domain.ErrJobNotFound
	}
	return job, nil
}

func (r *mockJobRepo) FindAll() ([]domain.Job, error) {
	var jobs []domain.Job
	for _, job := range r.jobs {
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (r *mockJobRepo) FindByOrgID(orgID string) ([]domain.Job, error) {
	return nil, nil
}

func (r *mockJobRepo) FindByUserID(userID string, offset, limit int) ([]domain.Job, int, error) {
	return nil, 0, nil
}

func (r *mockJobRepo) Delete(id string) error {
	delete(r.jobs, id)
	return nil
}

// Mock executor for testing
type mockSchedulerExecutor struct {
	shouldFail bool
	callCount  int
}

func (e *mockSchedulerExecutor) Execute(job domain.Job) error {
	e.callCount++
	if e.shouldFail {
		return errors.New("execution failed")
	}
	return nil
}

func (e *mockSchedulerExecutor) ExecuteWithJobRun(job domain.Job, jobRunID string) error {
	return e.Execute(job)
}

func (e *mockSchedulerExecutor) Wait() {
	// No-op for testing
}

func TestFailureTrackingExecutor_SendsNotificationOnAutoPause(t *testing.T) {
	repo := newMockJobRepo()
	notifier := &mockNotifier{}
	innerExecutor := &mockSchedulerExecutor{shouldFail: true}

	// Set threshold to 2 for quick testing
	executor := NewFailureTrackingExecutor(innerExecutor, repo, notifier, 2)

	// Create a job
	job := domain.NewJob("Test Job", "org-123", "user-456", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	repo.Save(job)

	// Fail the job once - should NOT send notification yet
	executor.Execute(job)

	if len(notifier.jobAutoPausedCalls) != 0 {
		t.Errorf("Expected 0 auto-paused notifications after 1 failure, got %d", len(notifier.jobAutoPausedCalls))
	}

	// Reload job to get updated failure count
	job, _ = repo.FindByID(job.ID)

	// Fail the job again - should trigger auto-pause and notification
	executor.Execute(job)

	if len(notifier.jobAutoPausedCalls) != 1 {
		t.Errorf("Expected 1 auto-paused notification after reaching threshold, got %d", len(notifier.jobAutoPausedCalls))
	}

	if len(notifier.jobAutoPausedCalls) > 0 {
		notification := notifier.jobAutoPausedCalls[0]
		if notification.JobID != job.ID {
			t.Errorf("Expected notification for job %s, got %s", job.ID, notification.JobID)
		}
		if notification.JobName != job.Name {
			t.Errorf("Expected notification for job name '%s', got '%s'", job.Name, notification.JobName)
		}
		if notification.OrgID != job.OrgID {
			t.Errorf("Expected notification for org %s, got %s", job.OrgID, notification.OrgID)
		}
		if notification.UserID != job.UserID {
			t.Errorf("Expected notification for user %s, got %s", job.UserID, notification.UserID)
		}
		if notification.ConsecutiveFailures != 2 {
			t.Errorf("Expected consecutive_failures=2 in notification, got %d", notification.ConsecutiveFailures)
		}
		if notification.ErrorMsg == "" {
			t.Error("Expected error message in notification, got empty string")
		}
	}

	// Verify job is paused
	job, _ = repo.FindByID(job.ID)
	if job.Status != domain.StatusPaused {
		t.Errorf("Expected job status to be paused, got %s", job.Status)
	}
}

func TestFailureTrackingExecutor_NoNotificationWhenBelowThreshold(t *testing.T) {
	repo := newMockJobRepo()
	notifier := &mockNotifier{}
	innerExecutor := &mockSchedulerExecutor{shouldFail: true}

	executor := NewFailureTrackingExecutor(innerExecutor, repo, notifier, 5)

	job := domain.NewJob("Test Job", "org-123", "user-456", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	repo.Save(job)

	// Fail 3 times - below threshold of 5
	for i := 0; i < 3; i++ {
		executor.Execute(job)
		job, _ = repo.FindByID(job.ID)
	}

	if len(notifier.jobAutoPausedCalls) != 0 {
		t.Errorf("Expected 0 auto-paused notifications when below threshold, got %d", len(notifier.jobAutoPausedCalls))
	}

	// Verify job is NOT paused
	if job.Status == domain.StatusPaused {
		t.Error("Job should not be paused when below failure threshold")
	}
}

func TestFailureTrackingExecutor_NoNotificationOnSuccess(t *testing.T) {
	repo := newMockJobRepo()
	notifier := &mockNotifier{}
	innerExecutor := &mockSchedulerExecutor{shouldFail: false}

	executor := NewFailureTrackingExecutor(innerExecutor, repo, notifier, 2)

	job := domain.NewJob("Test Job", "org-123", "user-456", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	repo.Save(job)

	// Execute successfully multiple times
	for i := 0; i < 5; i++ {
		executor.Execute(job)
		job, _ = repo.FindByID(job.ID)
	}

	if len(notifier.jobAutoPausedCalls) != 0 {
		t.Errorf("Expected 0 auto-paused notifications on successful executions, got %d", len(notifier.jobAutoPausedCalls))
	}

	// Verify job is still scheduled
	if job.Status != domain.StatusScheduled {
		t.Errorf("Expected job status to be scheduled, got %s", job.Status)
	}
}

func TestFailureTrackingExecutor_NoNotificationWhenThresholdDisabled(t *testing.T) {
	repo := newMockJobRepo()
	notifier := &mockNotifier{}
	innerExecutor := &mockSchedulerExecutor{shouldFail: true}

	// Threshold 0 = disabled
	executor := NewFailureTrackingExecutor(innerExecutor, repo, notifier, 0)

	job := domain.NewJob("Test Job", "org-123", "user-456", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	repo.Save(job)

	// Fail 10 times - should never auto-pause when threshold is 0
	for i := 0; i < 10; i++ {
		executor.Execute(job)
		job, _ = repo.FindByID(job.ID)
	}

	if len(notifier.jobAutoPausedCalls) != 0 {
		t.Errorf("Expected 0 auto-paused notifications when threshold disabled, got %d", len(notifier.jobAutoPausedCalls))
	}

	// Verify job is NOT paused
	if job.Status == domain.StatusPaused {
		t.Error("Job should not be auto-paused when threshold is disabled (0)")
	}
}
