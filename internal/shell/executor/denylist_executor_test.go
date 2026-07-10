package executor

import (
	"log/slog"
	"os"
	"strings"
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

	if err == nil {
		t.Error("Expected error for denied job, got nil")
	}

	if !strings.Contains(err.Error(), "denylist") {
		t.Errorf("Expected error to mention denylist, got: %v", err)
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

	if err == nil {
		t.Error("Expected error for denied job, got nil")
	}

	if !strings.Contains(err.Error(), "denylist") {
		t.Errorf("Expected error to mention denylist, got: %v", err)
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
