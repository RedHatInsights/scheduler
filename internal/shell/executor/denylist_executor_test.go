package executor

import (
	"log/slog"
	"os"
	"testing"

	"insights-scheduler/internal/core/domain"
)

// MockExecutor is a simple mock for testing
type MockExecutor struct {
	executeCallCount           int
	executeWithJobRunCallCount int
	lastJobID                  string
}

func (m *MockExecutor) Execute(job domain.Job) error {
	m.executeCallCount++
	m.lastJobID = job.ID
	return nil
}

func (m *MockExecutor) ExecuteWithJobRun(job domain.Job, jobRunID string) error {
	m.executeWithJobRunCallCount++
	m.lastJobID = job.ID
	return nil
}

func (m *MockExecutor) Wait() {}

func TestDenylistExecutor_Execute_AllowsNonDeniedJobs(t *testing.T) {
	mockExecutor := &MockExecutor{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	denylist := []string{"denied-job-1", "denied-job-2"}
	executor := NewDenylistExecutor(mockExecutor, denylist, logger)

	job := domain.Job{
		ID:     "allowed-job",
		Name:   "Allowed Job",
		OrgID:  "org1",
		UserID: "user1",
	}

	err := executor.Execute(job)

	if err != nil {
		t.Errorf("Expected no error for allowed job, got: %v", err)
	}

	if mockExecutor.executeCallCount != 1 {
		t.Errorf("Expected wrapped executor to be called once, got %d", mockExecutor.executeCallCount)
	}

	if mockExecutor.lastJobID != "allowed-job" {
		t.Errorf("Expected job ID 'allowed-job', got %s", mockExecutor.lastJobID)
	}
}

func TestDenylistExecutor_Execute_DeniesListedJobs(t *testing.T) {
	mockExecutor := &MockExecutor{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	denylist := []string{"denied-job-1", "denied-job-2"}
	executor := NewDenylistExecutor(mockExecutor, denylist, logger)

	job := domain.Job{
		ID:     "denied-job-1",
		Name:   "Denied Job",
		OrgID:  "org1",
		UserID: "user1",
	}

	err := executor.Execute(job)

	// Denied jobs return nil (success) to avoid counting as failures
	if err != nil {
		t.Errorf("Expected nil for denied job (to avoid failure tracking), got: %v", err)
	}

	if mockExecutor.executeCallCount != 0 {
		t.Errorf("Expected wrapped executor not to be called, but it was called %d times", mockExecutor.executeCallCount)
	}
}

func TestDenylistExecutor_ExecuteWithJobRun_DeniesListedJobs(t *testing.T) {
	mockExecutor := &MockExecutor{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	denylist := []string{"denied-job-1", "denied-job-2"}
	executor := NewDenylistExecutor(mockExecutor, denylist, logger)

	job := domain.Job{
		ID:     "denied-job-2",
		Name:   "Denied Job",
		OrgID:  "org1",
		UserID: "user1",
	}

	err := executor.ExecuteWithJobRun(job, "run-123")

	// Denied jobs return nil (success) to avoid counting as failures
	if err != nil {
		t.Errorf("Expected nil for denied job (to avoid failure tracking), got: %v", err)
	}

	if mockExecutor.executeWithJobRunCallCount != 0 {
		t.Errorf("Expected wrapped executor not to be called, but it was called %d times", mockExecutor.executeWithJobRunCallCount)
	}
}

func TestDenylistExecutor_EmptyDenylist_AllowsAllJobs(t *testing.T) {
	mockExecutor := &MockExecutor{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	denylist := []string{}
	executor := NewDenylistExecutor(mockExecutor, denylist, logger)

	job := domain.Job{
		ID:     "any-job",
		Name:   "Any Job",
		OrgID:  "org1",
		UserID: "user1",
	}

	err := executor.Execute(job)

	if err != nil {
		t.Errorf("Expected no error with empty denylist, got: %v", err)
	}

	if mockExecutor.executeCallCount != 1 {
		t.Errorf("Expected wrapped executor to be called once, got %d", mockExecutor.executeCallCount)
	}
}

func TestDenylistExecutor_TrimsWhitespace(t *testing.T) {
	mockExecutor := &MockExecutor{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Denylist with spaces around job IDs (simulates SCHEDULER_DENYLIST_JOB_IDS="job-1, job-2, job-3")
	denylist := []string{" denied-job-1 ", "denied-job-2 ", " denied-job-3"}
	executor := NewDenylistExecutor(mockExecutor, denylist, logger)

	tests := []struct {
		jobID          string
		shouldBeDenied bool
		description    string
	}{
		{"denied-job-1", true, "leading and trailing spaces"},
		{"denied-job-2", true, "trailing space"},
		{"denied-job-3", true, "leading space"},
		{"allowed-job", false, "not on denylist"},
	}

	for _, tt := range tests {
		mockExecutor.executeCallCount = 0

		job := domain.Job{
			ID:     tt.jobID,
			Name:   "Test Job",
			OrgID:  "org1",
			UserID: "user1",
		}

		err := executor.Execute(job)

		if err != nil {
			t.Errorf("[%s] Expected no error, got: %v", tt.description, err)
		}

		if tt.shouldBeDenied && mockExecutor.executeCallCount != 0 {
			t.Errorf("[%s] Job %s should be denied, but was executed", tt.description, tt.jobID)
		}

		if !tt.shouldBeDenied && mockExecutor.executeCallCount != 1 {
			t.Errorf("[%s] Job %s should be allowed, but was denied", tt.description, tt.jobID)
		}
	}
}

func TestDenylistExecutor_IgnoresEmptyStrings(t *testing.T) {
	mockExecutor := &MockExecutor{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Denylist with empty strings (could happen with trailing commas: "job-1,job-2,")
	denylist := []string{"denied-job-1", "", "  ", "denied-job-2"}
	executor := NewDenylistExecutor(mockExecutor, denylist, logger)

	job := domain.Job{
		ID:     "denied-job-1",
		Name:   "Denied Job",
		OrgID:  "org1",
		UserID: "user1",
	}

	err := executor.Execute(job)

	// Should be denied
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if mockExecutor.executeCallCount != 0 {
		t.Error("Job should be denied")
	}
}
