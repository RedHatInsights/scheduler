package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"insights-scheduler/internal/clients/polling"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/shell/executor"
)

// --- mocks ---

type mockJobRunRepo struct {
	mu   sync.Mutex
	runs map[string]domain.JobRun
}

func newMockJobRunRepo() *mockJobRunRepo {
	return &mockJobRunRepo{runs: make(map[string]domain.JobRun)}
}

func (r *mockJobRunRepo) Save(run domain.JobRun) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs[run.ID] = run
	return nil
}

func (r *mockJobRunRepo) FindByID(id string) (domain.JobRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[id]
	if !ok {
		return domain.JobRun{}, domain.ErrJobRunNotFound
	}
	return run, nil
}

func (r *mockJobRunRepo) FindByJobID(jobID string, _ domain.SortSpec, offset, limit int) ([]domain.JobRun, int, error) {
	return nil, 0, nil
}

func (r *mockJobRunRepo) FindByJobIDAndOrgID(jobID, orgID string) ([]domain.JobRun, error) {
	return nil, nil
}

func (r *mockJobRunRepo) FindByUserID(userID string, _ domain.SortSpec, offset, limit int) ([]domain.JobRun, int, error) {
	return nil, 0, nil
}

func (r *mockJobRunRepo) FindAll() ([]domain.JobRun, error) { return nil, nil }

func (r *mockJobRunRepo) FindInFlightExternalRuns(ctx context.Context) ([]domain.JobRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []domain.JobRun
	for _, run := range r.runs {
		if run.Status == domain.RunStatusRunning && run.ExternalJobID != nil {
			result = append(result, run)
		}
	}
	return result, nil
}

func (r *mockJobRunRepo) CleanupOldRuns(keepPerJob int) (int64, error) { return 0, nil }

type mockJobRepo struct {
	mu   sync.Mutex
	jobs map[string]domain.Job
}

func newMockJobRepo() *mockJobRepo {
	return &mockJobRepo{jobs: make(map[string]domain.Job)}
}

func (r *mockJobRepo) Save(job domain.Job) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobs[job.ID] = job
	return nil
}

func (r *mockJobRepo) FindByID(id string) (domain.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok {
		return domain.Job{}, domain.ErrJobNotFound
	}
	return job, nil
}

func (r *mockJobRepo) FindAll() ([]domain.Job, error) { return nil, nil }

func (r *mockJobRepo) FindByOrgID(orgID string) ([]domain.Job, error) { return nil, nil }

func (r *mockJobRepo) FindByUserID(userID string, _ domain.JobFilter, _ domain.SortSpec, offset, limit int) ([]domain.Job, int, error) {
	return nil, 0, nil
}

func (r *mockJobRepo) Delete(id string) error { return nil }

type mockExportClient struct {
	statusFunc func(exportID string) (string, string, error)
}

func (c *mockExportClient) getStatus(exportID string) (string, string, error) {
	return c.statusFunc(exportID)
}

type mockUserValidator struct {
	header string
	err    error
}

func (v *mockUserValidator) ValidateUser(ctx context.Context, orgID, userID string) (bool, error) {
	return true, nil
}

func (v *mockUserValidator) GenerateIdentityHeader(ctx context.Context, orgID, userID string) (string, error) {
	return v.header, v.err
}

type testNotifier struct {
	mu             sync.Mutex
	completeCalls  []*executor.ExportCompletionNotification
	autoPauseCalls []*executor.JobAutoPausedNotification
}

func (n *testNotifier) JobComplete(ctx context.Context, notification *executor.ExportCompletionNotification, logger *slog.Logger) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.completeCalls = append(n.completeCalls, notification)
	return nil
}

func (n *testNotifier) JobAutoPaused(ctx context.Context, notification *executor.JobAutoPausedNotification, logger *slog.Logger) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.autoPauseCalls = append(n.autoPauseCalls, notification)
	return nil
}

// mockPoller implements polling.Poller for testing the service's processRun path
type mockPoller struct {
	status *polling.StatusResponse
	err    error
}

func (p *mockPoller) GetStatus(ctx context.Context, jobID string) (*polling.StatusResponse, error) {
	return p.status, p.err
}

func (p *mockPoller) IsTerminalStatus(status polling.JobStatus) bool {
	return status == polling.StatusComplete || status == polling.StatusFailed
}

// --- helpers ---

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func createRunningExportRun(jobID string) domain.JobRun {
	run := domain.NewJobRun(jobID)
	return run.WithExternalJob("export-abc-123", "export")
}

// --- tests ---

func TestExportPollerService_SkipsRunsWithoutExternalID(t *testing.T) {
	runRepo := newMockJobRunRepo()
	jobRepo := newMockJobRepo()
	notifier := &testNotifier{}

	run := domain.NewJobRun("job-1")
	runRepo.Save(run)

	job := domain.NewJob("Test", "org-1", "user-1", "0 * * * *", "UTC", domain.PayloadExport, nil)
	job.ID = "job-1"
	jobRepo.Save(job)

	svc := NewExportPollerService(
		runRepo, jobRepo, nil, &mockUserValidator{header: "hdr"},
		notifier, nil, nil, 100*time.Millisecond, 30*time.Minute, 10, testLogger(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	svc.Start(ctx)

	// Run should still be running (not touched by poller)
	updated, _ := runRepo.FindByID(run.ID)
	if updated.Status != domain.RunStatusRunning {
		t.Errorf("Expected run to remain running, got %s", updated.Status)
	}
}

func TestExportPollerService_SkipsNonExportServices(t *testing.T) {
	runRepo := newMockJobRunRepo()
	jobRepo := newMockJobRepo()
	notifier := &testNotifier{}

	run := domain.NewJobRun("job-1")
	pdfService := "pdf"
	pdfID := "pdf-123"
	run.ExternalService = &pdfService
	run.ExternalJobID = &pdfID
	runRepo.Save(run)

	svc := NewExportPollerService(
		runRepo, jobRepo, nil, &mockUserValidator{header: "hdr"},
		notifier, nil, nil, 100*time.Millisecond, 30*time.Minute, 10, testLogger(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	svc.Start(ctx)

	updated, _ := runRepo.FindByID(run.ID)
	if updated.Status != domain.RunStatusRunning {
		t.Errorf("Expected non-export run to remain running, got %s", updated.Status)
	}
}

func TestExportPollerService_TimesOutOldRuns(t *testing.T) {
	runRepo := newMockJobRunRepo()
	jobRepo := newMockJobRepo()
	notifier := &testNotifier{}
	tracker := executor.NewFailureTracker(jobRepo, notifier, 3)

	job := domain.NewJob("Test", "org-1", "user-1", "0 * * * *", "UTC", domain.PayloadExport, nil)
	jobRepo.Save(job)

	run := domain.NewJobRun(job.ID)
	run = run.WithExternalJob("export-old", "export")
	run.StartTime = time.Now().Add(-31 * time.Minute) // older than 30 min threshold
	runRepo.Save(run)

	svc := NewExportPollerService(
		runRepo, jobRepo, nil, &mockUserValidator{header: "hdr"},
		notifier, tracker, nil, 100*time.Millisecond, 30*time.Minute, 10, testLogger(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	svc.Start(ctx)

	updated, _ := runRepo.FindByID(run.ID)
	if updated.Status != domain.RunStatusFailed {
		t.Errorf("Expected timed-out run to be failed, got %s", updated.Status)
	}
	if updated.ErrorMessage == nil || *updated.ErrorMessage != "Execution timeout - exceeded maximum duration" {
		t.Errorf("Expected timeout error message, got %v", updated.ErrorMessage)
	}
}

func TestExportPollerService_TimeoutIncrementsMetric(t *testing.T) {
	runRepo := newMockJobRunRepo()
	jobRepo := newMockJobRepo()
	notifier := &testNotifier{}
	tracker := executor.NewFailureTracker(jobRepo, notifier, 3)

	job := domain.NewJob("Test", "org-1", "user-1", "0 * * * *", "UTC", domain.PayloadExport, nil)
	jobRepo.Save(job)

	run := domain.NewJobRun(job.ID)
	run = run.WithExternalJob("export-old", "export")
	run.StartTime = time.Now().Add(-31 * time.Minute)
	runRepo.Save(run)

	svc := NewExportPollerService(
		runRepo, jobRepo, nil, &mockUserValidator{header: "hdr"},
		notifier, tracker, nil, 100*time.Millisecond, 30*time.Minute, 10, testLogger(),
	)

	before := testutil.ToFloat64(ExportPollTimeoutsTotal)

	svc.scanAndProcess(context.Background())
	svc.activePolls.Wait()

	after := testutil.ToFloat64(ExportPollTimeoutsTotal)
	if delta := after - before; delta != 1 {
		t.Errorf("Expected poll_timeouts_total to increment by 1, got delta %v", delta)
	}
}

func TestExportPollerService_InFlightGauge(t *testing.T) {
	runRepo := newMockJobRunRepo()
	jobRepo := newMockJobRepo()

	job := domain.NewJob("Test", "org-1", "user-1", "0 * * * *", "UTC", domain.PayloadExport, nil)
	jobRepo.Save(job)

	svc := NewExportPollerService(
		runRepo, jobRepo, nil, &mockUserValidator{header: "hdr"},
		&testNotifier{}, nil, nil, 100*time.Millisecond, 30*time.Minute, 10, testLogger(),
	)

	// No in-flight runs -> gauge should read 0.
	svc.scanAndProcess(context.Background())
	svc.activePolls.Wait()
	if v := testutil.ToFloat64(ExportInFlightRuns); v != 0 {
		t.Errorf("Expected in-flight gauge 0 with no runs, got %v", v)
	}

	// One in-flight (timed-out) run -> gauge is set to 1 synchronously during the scan.
	run := domain.NewJobRun(job.ID)
	run = run.WithExternalJob("export-1", "export")
	run.StartTime = time.Now().Add(-31 * time.Minute)
	runRepo.Save(run)

	svc.scanAndProcess(context.Background())
	svc.activePolls.Wait()
	if v := testutil.ToFloat64(ExportInFlightRuns); v != 1 {
		t.Errorf("Expected in-flight gauge 1 with one run, got %v", v)
	}
}

func TestExportErrorMessage(t *testing.T) {
	if got := exportErrorMessage(""); got != defaultExportErrorMsg {
		t.Errorf("empty error should map to default %q, got %q", defaultExportErrorMsg, got)
	}
	if got := exportErrorMessage("real failure"); got != "real failure" {
		t.Errorf("non-empty error should pass through, got %q", got)
	}
}

func TestExportPollerService_TimeoutSendsNotification(t *testing.T) {
	runRepo := newMockJobRunRepo()
	jobRepo := newMockJobRepo()
	notifier := &testNotifier{}
	tracker := executor.NewFailureTracker(jobRepo, notifier, 3)

	job := domain.NewJob("Test", "org-1", "user-1", "0 * * * *", "UTC", domain.PayloadExport, nil)
	nextRun := time.Now().Add(1 * time.Hour)
	job = job.WithNextRunAt(nextRun)
	jobRepo.Save(job)

	run := domain.NewJobRun(job.ID)
	run = run.WithExternalJob("export-old", "export")
	run.StartTime = time.Now().Add(-31 * time.Minute) // older than 30 min threshold
	runRepo.Save(run)

	svc := NewExportPollerService(
		runRepo, jobRepo, nil, &mockUserValidator{header: "hdr"},
		notifier, tracker, nil, 100*time.Millisecond, 30*time.Minute, 10, testLogger(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	svc.Start(ctx)

	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.completeCalls) != 1 {
		t.Fatalf("Expected 1 completion notification on timeout, got %d", len(notifier.completeCalls))
	}
	n := notifier.completeCalls[0]
	if n.Status != string(polling.StatusFailed) {
		t.Errorf("Expected notification status=failed, got %s", n.Status)
	}
	if n.ErrorMsg != exportTimeoutErrorMsg {
		t.Errorf("Expected timeout error message in notification, got %q", n.ErrorMsg)
	}
	if n.ExportID != "export-old" {
		t.Errorf("Expected export ID in notification, got %q", n.ExportID)
	}
	if n.RunID != run.ID {
		t.Errorf("Expected run_id=%s in notification, got %s", run.ID, n.RunID)
	}
	if n.NextRunAt == nil {
		t.Error("Expected next_run_at to be set for scheduled job")
	}
}

func TestExportPollerService_ScanAndProcess_ContextCancellation(t *testing.T) {
	runRepo := newMockJobRunRepo()
	jobRepo := newMockJobRepo()

	svc := NewExportPollerService(
		runRepo, jobRepo, nil, &mockUserValidator{header: "hdr"},
		nil, nil, nil, 50*time.Millisecond, 30*time.Minute, 10, testLogger(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Start(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// Good, it exited
	case <-time.After(2 * time.Second):
		t.Fatal("ExportPollerService did not shut down after context cancellation")
	}
}

func TestExportPollerService_FailureTrackerCalledOnTimeout(t *testing.T) {
	runRepo := newMockJobRunRepo()
	jobRepo := newMockJobRepo()
	notifier := &testNotifier{}
	tracker := executor.NewFailureTracker(jobRepo, notifier, 2)

	job := domain.NewJob("Test", "org-1", "user-1", "0 * * * *", "UTC", domain.PayloadExport, nil)
	jobRepo.Save(job)

	run := domain.NewJobRun(job.ID)
	run = run.WithExternalJob("export-old", "export")
	run.StartTime = time.Now().Add(-31 * time.Minute)
	runRepo.Save(run)

	svc := NewExportPollerService(
		runRepo, jobRepo, nil, &mockUserValidator{header: "hdr"},
		notifier, tracker, nil, 100*time.Millisecond, 30*time.Minute, 10, testLogger(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	svc.Start(ctx)

	updatedJob, _ := jobRepo.FindByID(job.ID)
	if updatedJob.ConsecutiveFailures != 1 {
		t.Errorf("Expected 1 consecutive failure after timeout, got %d", updatedJob.ConsecutiveFailures)
	}
}

func TestExportPollerService_IdentityFailureRetries(t *testing.T) {
	runRepo := newMockJobRunRepo()
	jobRepo := newMockJobRepo()

	job := domain.NewJob("Test", "org-1", "user-1", "0 * * * *", "UTC", domain.PayloadExport, nil)
	jobRepo.Save(job)

	run := createRunningExportRun(job.ID)
	runRepo.Save(run)

	svc := NewExportPollerService(
		runRepo, jobRepo, nil,
		&mockUserValidator{header: "", err: errors.New("identity service down")},
		nil, nil, nil, 100*time.Millisecond, 30*time.Minute, 10, testLogger(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	svc.Start(ctx)

	// Run should still be running — identity failure is retryable
	updated, _ := runRepo.FindByID(run.ID)
	if updated.Status != domain.RunStatusRunning {
		t.Errorf("Expected run to remain running after identity failure, got %s", updated.Status)
	}
}
