package executor

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"insights-scheduler/internal/core/domain"
)

// MockJobRepository for testing
type MockJobRepository struct {
	jobs         map[string]domain.Job
	saveCount    int
	lastSavedJob *domain.Job
}

func NewMockJobRepository() *MockJobRepository {
	return &MockJobRepository{
		jobs: make(map[string]domain.Job),
	}
}

func (m *MockJobRepository) Save(job domain.Job) error {
	m.jobs[job.ID] = job
	m.saveCount++
	m.lastSavedJob = &job
	return nil
}

func (m *MockJobRepository) FindByID(id string) (domain.Job, error) {
	job, exists := m.jobs[id]
	if !exists {
		return domain.Job{}, domain.ErrJobNotFound
	}
	return job, nil
}

func (m *MockJobRepository) FindAll() ([]domain.Job, error) {
	jobs := make([]domain.Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (m *MockJobRepository) FindByOrgID(orgID string) ([]domain.Job, error) {
	return nil, nil
}

func (m *MockJobRepository) FindByUserID(userID string, offset, limit int) ([]domain.Job, int, error) {
	return nil, 0, nil
}

func (m *MockJobRepository) Delete(id string) error {
	delete(m.jobs, id)
	return nil
}

// MockNotifier for testing
type MockNotifier struct {
	jobCompleteCalls   int
	jobAutoPausedCalls int
	lastAutoPausedJob  *JobAutoPausedNotification
}

func (m *MockNotifier) JobComplete(ctx context.Context, notification *ExportCompletionNotification, logger *slog.Logger) error {
	m.jobCompleteCalls++
	return nil
}

func (m *MockNotifier) JobAutoPaused(ctx context.Context, notification *JobAutoPausedNotification, logger *slog.Logger) error {
	m.jobAutoPausedCalls++
	m.lastAutoPausedJob = notification
	return nil
}

// MockBaseExecutor that can simulate success or failure
type MockBaseExecutor struct {
	shouldFail   bool
	executeCount int
}

var errMockJobFailed = errors.New("mock job failed")

func (m *MockBaseExecutor) Execute(job domain.Job) error {
	m.executeCount++
	if m.shouldFail {
		return errMockJobFailed
	}
	return nil
}

func (m *MockBaseExecutor) ExecuteWithJobRun(job domain.Job, jobRunID string) error {
	m.executeCount++
	if m.shouldFail {
		return errMockJobFailed
	}
	return nil
}

func (m *MockBaseExecutor) Wait() {}

// TestDenylistPreventsFailureTracking verifies that denied jobs don't increment consecutive failures
func TestDenylistPreventsFailureTracking(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	jobRepo := NewMockJobRepository()
	notifier := &MockNotifier{}
	baseExecutor := &MockBaseExecutor{shouldFail: false}

	// Create executor chain: Denylist -> FailureTracking -> Base
	failureTracker := NewFailureTrackingExecutor(baseExecutor, jobRepo, notifier, 3, logger)
	denylist := []string{"denied-job-1"}
	executor := NewDenylistExecutor(failureTracker, denylist, logger)

	// Create a job that will be denied
	job := domain.Job{
		ID:                  "denied-job-1",
		Name:                "Denied Job",
		OrgID:               "org1",
		UserID:              "user1",
		Status:              domain.StatusScheduled,
		ConsecutiveFailures: 0,
	}

	// Save initial job
	jobRepo.Save(job)

	// Execute the denied job 5 times (more than max consecutive failures of 3)
	for i := 0; i < 5; i++ {
		err := executor.Execute(job)
		if err != nil {
			t.Errorf("Denied job should return nil, got error: %v", err)
		}
	}

	// Verify the job was never actually executed
	if baseExecutor.executeCount != 0 {
		t.Errorf("Base executor should not have been called, but was called %d times", baseExecutor.executeCount)
	}

	// Verify consecutive failures was never incremented
	savedJob, _ := jobRepo.FindByID(job.ID)
	if savedJob.ConsecutiveFailures != 0 {
		t.Errorf("Expected ConsecutiveFailures to be 0, got %d", savedJob.ConsecutiveFailures)
	}

	// Verify job status is not paused
	if savedJob.Status == domain.StatusPaused {
		t.Error("Denied job should not be auto-paused")
	}

	// Verify no auto-pause notifications were sent
	if notifier.jobAutoPausedCalls != 0 {
		t.Errorf("Expected no auto-pause notifications, got %d", notifier.jobAutoPausedCalls)
	}
}

// TestDenylistDoesNotPreventRealFailures verifies that allowed jobs still trigger failure tracking
func TestDenylistDoesNotPreventRealFailures(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	jobRepo := NewMockJobRepository()
	notifier := &MockNotifier{}
	baseExecutor := &MockBaseExecutor{shouldFail: true} // This executor will fail

	// Create executor chain: Denylist -> FailureTracking -> Base
	failureTracker := NewFailureTrackingExecutor(baseExecutor, jobRepo, notifier, 3, logger)
	denylist := []string{"denied-job-1"} // Only deny one specific job
	executor := NewDenylistExecutor(failureTracker, denylist, logger)

	// Create a job that is NOT on the denylist
	job := domain.Job{
		ID:                  "allowed-job-1",
		Name:                "Allowed Job",
		OrgID:               "org1",
		UserID:              "user1",
		Status:              domain.StatusScheduled,
		ConsecutiveFailures: 0,
	}

	// Save initial job
	jobRepo.Save(job)

	// Execute the allowed job 3 times (it will fail each time)
	for i := 0; i < 3; i++ {
		err := executor.Execute(job)
		if err == nil {
			t.Error("Expected error from failing job, got nil")
		}
		// Reload job to see updated state
		job, _ = jobRepo.FindByID(job.ID)
	}

	// Verify the job was actually executed
	if baseExecutor.executeCount != 3 {
		t.Errorf("Expected base executor to be called 3 times, got %d", baseExecutor.executeCount)
	}

	// Verify consecutive failures was incremented
	savedJob, _ := jobRepo.FindByID(job.ID)
	if savedJob.ConsecutiveFailures != 3 {
		t.Errorf("Expected ConsecutiveFailures to be 3, got %d", savedJob.ConsecutiveFailures)
	}

	// Verify job status is paused after max failures
	if savedJob.Status != domain.StatusPaused {
		t.Errorf("Expected job to be auto-paused, got status %s", savedJob.Status)
	}

	// Verify auto-pause notification was sent
	if notifier.jobAutoPausedCalls != 1 {
		t.Errorf("Expected 1 auto-pause notification, got %d", notifier.jobAutoPausedCalls)
	}

	if notifier.lastAutoPausedJob == nil {
		t.Error("Expected auto-pause notification to be recorded")
	} else if notifier.lastAutoPausedJob.JobID != "allowed-job-1" {
		t.Errorf("Expected notification for job allowed-job-1, got %s", notifier.lastAutoPausedJob.JobID)
	}
}

// TestDenylistWithZeroMaxFailures verifies denylist works when auto-pause is disabled
func TestDenylistWithZeroMaxFailures(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	jobRepo := NewMockJobRepository()
	notifier := &MockNotifier{}
	baseExecutor := &MockBaseExecutor{shouldFail: false}

	// Create executor chain with maxConsecutiveFailures = 0 (auto-pause disabled)
	failureTracker := NewFailureTrackingExecutor(baseExecutor, jobRepo, notifier, 0, logger)
	denylist := []string{"denied-job-1"}
	executor := NewDenylistExecutor(failureTracker, denylist, logger)

	// Create a denied job
	job := domain.Job{
		ID:                  "denied-job-1",
		Name:                "Denied Job",
		OrgID:               "org1",
		UserID:              "user1",
		Status:              domain.StatusScheduled,
		ConsecutiveFailures: 0,
	}

	jobRepo.Save(job)

	// Execute multiple times
	for i := 0; i < 10; i++ {
		err := executor.Execute(job)
		if err != nil {
			t.Errorf("Denied job should return nil, got error: %v", err)
		}
	}

	// Verify job was never executed
	if baseExecutor.executeCount != 0 {
		t.Errorf("Base executor should not have been called, got %d calls", baseExecutor.executeCount)
	}

	// Verify no state changes
	savedJob, _ := jobRepo.FindByID(job.ID)
	if savedJob.ConsecutiveFailures != 0 {
		t.Errorf("Expected ConsecutiveFailures to be 0, got %d", savedJob.ConsecutiveFailures)
	}
}

// TestDenylistWithMixedJobs verifies denylist correctly handles mix of denied and allowed jobs
func TestDenylistWithMixedJobs(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	jobRepo := NewMockJobRepository()
	notifier := &MockNotifier{}
	baseExecutor := &MockBaseExecutor{shouldFail: false}

	// Create executor chain
	failureTracker := NewFailureTrackingExecutor(baseExecutor, jobRepo, notifier, 3, logger)
	denylist := []string{"denied-1", "denied-2"}
	executor := NewDenylistExecutor(failureTracker, denylist, logger)

	jobs := []domain.Job{
		{ID: "denied-1", Name: "Denied 1", OrgID: "org1", UserID: "user1", Status: domain.StatusScheduled},
		{ID: "allowed-1", Name: "Allowed 1", OrgID: "org1", UserID: "user1", Status: domain.StatusScheduled},
		{ID: "denied-2", Name: "Denied 2", OrgID: "org1", UserID: "user1", Status: domain.StatusScheduled},
		{ID: "allowed-2", Name: "Allowed 2", OrgID: "org1", UserID: "user1", Status: domain.StatusScheduled},
	}

	for _, job := range jobs {
		jobRepo.Save(job)
		executor.Execute(job)
	}

	// Should have executed only the 2 allowed jobs
	if baseExecutor.executeCount != 2 {
		t.Errorf("Expected 2 executions, got %d", baseExecutor.executeCount)
	}

	// Verify denied jobs have 0 failures
	for _, jobID := range []string{"denied-1", "denied-2"} {
		job, _ := jobRepo.FindByID(jobID)
		if job.ConsecutiveFailures != 0 {
			t.Errorf("Job %s should have 0 consecutive failures, got %d", jobID, job.ConsecutiveFailures)
		}
	}

	// Verify allowed jobs had failures reset (successful execution)
	for _, jobID := range []string{"allowed-1", "allowed-2"} {
		job, _ := jobRepo.FindByID(jobID)
		if job.ConsecutiveFailures != 0 {
			t.Errorf("Job %s should have 0 consecutive failures after success, got %d", jobID, job.ConsecutiveFailures)
		}
		if job.Status != domain.StatusScheduled {
			t.Errorf("Job %s should be scheduled, got %s", jobID, job.Status)
		}
	}
}

// TestDenylistWithExecuteWithJobRun verifies denylist works with pre-created job run IDs
func TestDenylistWithExecuteWithJobRun(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	jobRepo := NewMockJobRepository()
	notifier := &MockNotifier{}
	baseExecutor := &MockBaseExecutor{shouldFail: false}

	// Create executor chain
	failureTracker := NewFailureTrackingExecutor(baseExecutor, jobRepo, notifier, 3, logger)
	denylist := []string{"denied-job-1"}
	executor := NewDenylistExecutor(failureTracker, denylist, logger)

	job := domain.Job{
		ID:                  "denied-job-1",
		Name:                "Denied Job",
		OrgID:               "org1",
		UserID:              "user1",
		Status:              domain.StatusScheduled,
		ConsecutiveFailures: 0,
	}

	jobRepo.Save(job)

	// Execute with pre-created job run ID (used by scheduler)
	err := executor.ExecuteWithJobRun(job, "run-123")
	if err != nil {
		t.Errorf("Denied job should return nil, got error: %v", err)
	}

	// Verify not executed
	if baseExecutor.executeCount != 0 {
		t.Errorf("Base executor should not have been called, got %d calls", baseExecutor.executeCount)
	}

	// Verify no failure tracking
	savedJob, _ := jobRepo.FindByID(job.ID)
	if savedJob.ConsecutiveFailures != 0 {
		t.Errorf("Expected ConsecutiveFailures to be 0, got %d", savedJob.ConsecutiveFailures)
	}
}
