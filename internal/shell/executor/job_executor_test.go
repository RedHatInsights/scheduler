package executor

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/template"
	"insights-scheduler/internal/identity"
)

func TestDefaultJobExecutor_ExecuteWithKafka(t *testing.T) {
	// Create test logger
	testLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Create test config
	cfg := &config.Config{
		ExportService: config.ExportServiceConfig{
			BaseURL: "http://localhost:9000/api/export/v1",
		},
	}

	// Create a fake user validator
	userValidator := identity.NewFakeUserValidator()

	// Create payload-specific runners
	runners := map[domain.PayloadType]JobRunner{
		domain.PayloadMessage:     NewMessageJobExecutor(),
		domain.PayloadHTTPRequest: NewHTTPJobExecutor(),
		domain.PayloadCommand:     NewCommandJobExecutor(),
		domain.PayloadExport: func() JobRunner {
			e, _ := template.NewEvaluator()
			return NewExportJobExecutor(cfg, userValidator, nil, e)
		}(),
	}

	// Create executor with map of executors
	executor := NewJobExecutor(runners, nil, testLogger)

	// Create a test job
	payload := map[string]interface{}{
		"message": "test message",
	}

	job := domain.NewJob("Test Job", "test-org-123", "test-user-id", "*/15 * * * *", "UTC", domain.PayloadMessage, payload)

	// Test executing a message job (should not trigger notification)
	err := executor.Execute(job)
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}
}

func TestExportCompletionNotificationStructure(t *testing.T) {
	// Test that we can create the notification structure correctly
	notification := &ExportCompletionNotification{
		ExportID:    "export-123",
		JobID:       "job-456",
		OrgID:       "org-789",
		Status:      "complete",
		DownloadURL: "https://example.com/exports/export-123",
		ErrorMsg:    "",
	}

	if notification.ExportID != "export-123" {
		t.Errorf("Expected ExportID 'export-123', got %s", notification.ExportID)
	}

	if notification.JobID != "job-456" {
		t.Errorf("Expected JobID 'job-456', got %s", notification.JobID)
	}

	if notification.OrgID != "org-789" {
		t.Errorf("Expected OrgID 'org-789', got %s", notification.OrgID)
	}

	if notification.Status != "complete" {
		t.Errorf("Expected Status 'complete', got %s", notification.Status)
	}

	if notification.DownloadURL != "https://example.com/exports/export-123" {
		t.Errorf("Expected DownloadURL 'https://example.com/exports/export-123', got %s", notification.DownloadURL)
	}
}

// capturingRunRepo is a minimal usecases.JobRunRepository that records saved runs
// and lets a test control FindByID.
type capturingRunRepo struct {
	findByID func(id string) (domain.JobRun, error)
	saved    []domain.JobRun
}

func (m *capturingRunRepo) Save(run domain.JobRun) error { m.saved = append(m.saved, run); return nil }
func (m *capturingRunRepo) FindByID(id string) (domain.JobRun, error) {
	return m.findByID(id)
}
func (m *capturingRunRepo) FindByJobID(string, int, int) ([]domain.JobRun, int, error) {
	return nil, 0, nil
}
func (m *capturingRunRepo) FindByJobIDAndOrgID(string, string) ([]domain.JobRun, error) {
	return nil, nil
}
func (m *capturingRunRepo) FindByUserID(string, int, int) ([]domain.JobRun, int, error) {
	return nil, 0, nil
}
func (m *capturingRunRepo) FindAll() ([]domain.JobRun, error) { return nil, nil }
func (m *capturingRunRepo) FindInFlightExternalRuns(context.Context) ([]domain.JobRun, error) {
	return nil, nil
}
func (m *capturingRunRepo) CleanupOldRuns(int) (int64, error) { return 0, nil }

// TestExecuteWithJobRun_RejectsMismatchedRunID verifies the defense-in-depth guard:
// if the supplied pre-created run ID resolves to a run belonging to a DIFFERENT
// job, the executor must not attach this job's execution to that run — it creates
// a fresh run for the executing job instead.
func TestExecuteWithJobRun_RejectsMismatchedRunID(t *testing.T) {
	testLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	runners := map[domain.PayloadType]JobRunner{domain.PayloadMessage: NewMessageJobExecutor()}

	job := domain.NewJob("Job A", "org-a", "user-a", "*/15 * * * *", "UTC", domain.PayloadMessage, map[string]interface{}{"message": "x"})

	// The pre-created run ID resolves to a run owned by a different job.
	foreignRun := domain.NewJobRun("some-other-job-id")
	repo := &capturingRunRepo{
		findByID: func(id string) (domain.JobRun, error) { return foreignRun, nil },
	}

	executor := NewJobExecutor(runners, repo, testLogger)
	if err := executor.ExecuteWithJobRun(job, foreignRun.ID); err != nil {
		t.Fatalf("ExecuteWithJobRun: %v", err)
	}

	freshForJob := false
	for _, r := range repo.saved {
		if r.ID == foreignRun.ID {
			t.Errorf("execution must not reuse the foreign run %s (belongs to another job)", foreignRun.ID)
		}
		if r.JobID == job.ID {
			freshForJob = true
		}
	}
	if !freshForJob {
		t.Error("expected a fresh run to be created for the executing job")
	}
}
