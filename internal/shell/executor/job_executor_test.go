package executor

import (
	"context"
	"errors"
	"testing"

	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/identity"
)

// Mock repositories for testing
type mockJobRepository struct {
	savedJobs    []domain.Job
	findByIDFunc func(id string) (domain.Job, error)
}

func (m *mockJobRepository) Save(job domain.Job) error {
	m.savedJobs = append(m.savedJobs, job)
	return nil
}

func (m *mockJobRepository) FindByID(id string) (domain.Job, error) {
	if m.findByIDFunc != nil {
		return m.findByIDFunc(id)
	}
	return domain.Job{}, domain.ErrJobNotFound
}

func (m *mockJobRepository) FindAll() ([]domain.Job, error) {
	return nil, nil
}

func (m *mockJobRepository) FindByOrgID(orgID string) ([]domain.Job, error) {
	return nil, nil
}

func (m *mockJobRepository) FindByUserID(userID string, offset, limit int) ([]domain.Job, int, error) {
	return nil, 0, nil
}

func (m *mockJobRepository) Delete(id string) error {
	return nil
}

type mockJobRunRepository struct {
	savedRuns []domain.JobRun
}

func (m *mockJobRunRepository) Save(run domain.JobRun) error {
	m.savedRuns = append(m.savedRuns, run)
	return nil
}

func (m *mockJobRunRepository) FindByID(id string) (domain.JobRun, error) {
	return domain.JobRun{}, domain.ErrJobRunNotFound
}

func (m *mockJobRunRepository) FindByJobID(jobID string, offset, limit int) ([]domain.JobRun, int, error) {
	return nil, 0, nil
}

func (m *mockJobRunRepository) FindByJobIDAndOrgID(jobID string, orgID string) ([]domain.JobRun, error) {
	return nil, nil
}

func (m *mockJobRunRepository) FindAll() ([]domain.JobRun, error) {
	return nil, nil
}

// Failing executor for testing
type failingExecutor struct{}

func (f *failingExecutor) Execute(job domain.Job) error {
	return errors.New("intentional test failure")
}

// Successful executor for testing
type successfulExecutor struct{}

func (s *successfulExecutor) Execute(job domain.Job) error {
	return nil
}

// Mock notifier for testing
type mockNotifier struct {
	failureNotifications    []*JobFailureNotification
	completionNotifications []*ExportCompletionNotification
}

func (m *mockNotifier) JobFailed(ctx context.Context, notification *JobFailureNotification) error {
	m.failureNotifications = append(m.failureNotifications, notification)
	return nil
}

func (m *mockNotifier) JobComplete(ctx context.Context, notification *ExportCompletionNotification) error {
	m.completionNotifications = append(m.completionNotifications, notification)
	return nil
}

func TestDefaultJobExecutor_ExecuteWithKafka(t *testing.T) {
	// Create test config
	cfg := &config.Config{
		ExportService: config.ExportServiceConfig{
			BaseURL: "http://localhost:9000/api/export/v1",
		},
		Scheduler: config.SchedulerConfig{
			MaxConsecutiveFailures: 3,
		},
	}

	// Create a fake user validator
	userValidator := identity.NewFakeUserValidator()

	// Create null notifier (test null object pattern)
	notifier := NewNullJobCompletionNotifier()

	// Create payload-specific executors
	executors := map[domain.PayloadType]JobExecutor{
		domain.PayloadMessage:     NewMessageJobExecutor(),
		domain.PayloadHTTPRequest: NewHTTPJobExecutor(),
		domain.PayloadCommand:     NewCommandJobExecutor(),
		domain.PayloadExport:      NewExportJobExecutor(cfg, userValidator, notifier),
	}

	// Create executor with map of executors (nil for runRepo and jobRepo in test)
	executor := NewJobExecutor(executors, nil, nil, cfg, nil)

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
		AccountID:   "account-123",
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

// TestConsecutiveFailures_IncrementOnFailure verifies that consecutive_failures is incremented when a job fails
func TestConsecutiveFailures_IncrementOnFailure(t *testing.T) {
	jobRepo := &mockJobRepository{}
	runRepo := &mockJobRunRepository{}

	cfg := &config.Config{
		Scheduler: config.SchedulerConfig{
			MaxConsecutiveFailures: 3,
		},
	}

	executors := map[domain.PayloadType]JobExecutor{
		domain.PayloadMessage: &failingExecutor{},
	}

	executor := NewJobExecutor(executors, runRepo, jobRepo, cfg, nil)

	job := domain.NewJob("Test Job", "org-123", "user-123", "*/15 * * * *", "UTC", domain.PayloadMessage, nil)

	// Verify initial state
	if job.ConsecutiveFailures != 0 {
		t.Errorf("Expected initial consecutive_failures = 0, got %d", job.ConsecutiveFailures)
	}

	// Execute the job (which will fail)
	err := executor.Execute(job)
	if err == nil {
		t.Error("Expected job to fail, but it succeeded")
	}

	// Verify that the job was saved with incremented failure count
	if len(jobRepo.savedJobs) != 1 {
		t.Fatalf("Expected 1 job to be saved, got %d", len(jobRepo.savedJobs))
	}

	savedJob := jobRepo.savedJobs[0]
	if savedJob.ConsecutiveFailures != 1 {
		t.Errorf("Expected consecutive_failures = 1, got %d", savedJob.ConsecutiveFailures)
	}

	if savedJob.Status != domain.StatusScheduled {
		t.Errorf("Expected status to remain 'scheduled', got %s", savedJob.Status)
	}
}

// TestConsecutiveFailures_ResetOnSuccess verifies that consecutive_failures is reset when a job succeeds
func TestConsecutiveFailures_ResetOnSuccess(t *testing.T) {
	jobRepo := &mockJobRepository{}
	runRepo := &mockJobRunRepository{}

	cfg := &config.Config{
		Scheduler: config.SchedulerConfig{
			MaxConsecutiveFailures: 3,
		},
	}

	executors := map[domain.PayloadType]JobExecutor{
		domain.PayloadMessage: &successfulExecutor{},
	}

	executor := NewJobExecutor(executors, runRepo, jobRepo, cfg, nil)

	// Create a job that has already failed 2 times
	job := domain.NewJob("Test Job", "org-123", "user-123", "*/15 * * * *", "UTC", domain.PayloadMessage, nil)
	job = job.WithIncrementedFailures()
	job = job.WithIncrementedFailures()

	if job.ConsecutiveFailures != 2 {
		t.Fatalf("Expected consecutive_failures = 2, got %d", job.ConsecutiveFailures)
	}

	// Execute the job (which will succeed)
	err := executor.Execute(job)
	if err != nil {
		t.Errorf("Expected job to succeed, but got error: %v", err)
	}

	// Verify that the job was saved with reset failure count
	if len(jobRepo.savedJobs) != 1 {
		t.Fatalf("Expected 1 job to be saved, got %d", len(jobRepo.savedJobs))
	}

	savedJob := jobRepo.savedJobs[0]
	if savedJob.ConsecutiveFailures != 0 {
		t.Errorf("Expected consecutive_failures to be reset to 0, got %d", savedJob.ConsecutiveFailures)
	}
}

// TestConsecutiveFailures_PauseAtThreshold verifies that a job is paused when it reaches the failure threshold
func TestConsecutiveFailures_PauseAtThreshold(t *testing.T) {
	jobRepo := &mockJobRepository{}
	runRepo := &mockJobRunRepository{}

	cfg := &config.Config{
		Scheduler: config.SchedulerConfig{
			MaxConsecutiveFailures: 3,
		},
	}

	executors := map[domain.PayloadType]JobExecutor{
		domain.PayloadMessage: &failingExecutor{},
	}

	executor := NewJobExecutor(executors, runRepo, jobRepo, cfg, nil)

	// Create a job that has already failed 2 times (one more will hit threshold)
	job := domain.NewJob("Test Job", "org-123", "user-123", "*/15 * * * *", "UTC", domain.PayloadMessage, nil)
	job = job.WithIncrementedFailures()
	job = job.WithIncrementedFailures()

	if job.ConsecutiveFailures != 2 {
		t.Fatalf("Expected consecutive_failures = 2, got %d", job.ConsecutiveFailures)
	}

	// Execute the job (3rd failure - should trigger pause)
	err := executor.Execute(job)
	if err == nil {
		t.Error("Expected job to fail, but it succeeded")
	}

	// Verify that the job was saved with failure count = 3 AND paused status
	if len(jobRepo.savedJobs) != 1 {
		t.Fatalf("Expected 1 job to be saved, got %d", len(jobRepo.savedJobs))
	}

	savedJob := jobRepo.savedJobs[0]
	if savedJob.ConsecutiveFailures != 3 {
		t.Errorf("Expected consecutive_failures = 3, got %d", savedJob.ConsecutiveFailures)
	}

	if savedJob.Status != domain.StatusPaused {
		t.Errorf("Expected status to be 'paused', got %s", savedJob.Status)
	}
}

// TestConsecutiveFailures_BelowThresholdNoPause verifies that job is NOT paused when below threshold
func TestConsecutiveFailures_BelowThresholdNoPause(t *testing.T) {
	jobRepo := &mockJobRepository{}
	runRepo := &mockJobRunRepository{}

	cfg := &config.Config{
		Scheduler: config.SchedulerConfig{
			MaxConsecutiveFailures: 5, // Higher threshold
		},
	}

	executors := map[domain.PayloadType]JobExecutor{
		domain.PayloadMessage: &failingExecutor{},
	}

	executor := NewJobExecutor(executors, runRepo, jobRepo, cfg, nil)

	// Create a job that has failed 2 times
	job := domain.NewJob("Test Job", "org-123", "user-123", "*/15 * * * *", "UTC", domain.PayloadMessage, nil)
	job = job.WithIncrementedFailures()
	job = job.WithIncrementedFailures()

	// Execute the job (3rd failure - below threshold of 5)
	err := executor.Execute(job)
	if err == nil {
		t.Error("Expected job to fail, but it succeeded")
	}

	// Verify that the job was saved with failure count = 3 but NOT paused
	if len(jobRepo.savedJobs) != 1 {
		t.Fatalf("Expected 1 job to be saved, got %d", len(jobRepo.savedJobs))
	}

	savedJob := jobRepo.savedJobs[0]
	if savedJob.ConsecutiveFailures != 3 {
		t.Errorf("Expected consecutive_failures = 3, got %d", savedJob.ConsecutiveFailures)
	}

	if savedJob.Status == domain.StatusPaused {
		t.Error("Expected status to NOT be 'paused' when below threshold")
	}
}

// TestConsecutiveFailures_DisabledWhenZero verifies that feature is disabled when MaxConsecutiveFailures = 0
func TestConsecutiveFailures_DisabledWhenZero(t *testing.T) {
	jobRepo := &mockJobRepository{}
	runRepo := &mockJobRunRepository{}

	cfg := &config.Config{
		Scheduler: config.SchedulerConfig{
			MaxConsecutiveFailures: 0, // Feature disabled
		},
	}

	executors := map[domain.PayloadType]JobExecutor{
		domain.PayloadMessage: &failingExecutor{},
	}

	executor := NewJobExecutor(executors, runRepo, jobRepo, cfg, nil)

	// Create a job with high failure count
	job := domain.NewJob("Test Job", "org-123", "user-123", "*/15 * * * *", "UTC", domain.PayloadMessage, nil)
	for i := 0; i < 100; i++ {
		job = job.WithIncrementedFailures()
	}

	// Execute the job (which will fail)
	err := executor.Execute(job)
	if err == nil {
		t.Error("Expected job to fail, but it succeeded")
	}

	// Verify that no job was saved (feature disabled, so no tracking)
	if len(jobRepo.savedJobs) != 0 {
		t.Errorf("Expected 0 jobs to be saved when feature disabled, got %d", len(jobRepo.savedJobs))
	}
}

// TestConsecutiveFailures_NoRepoNoCrash verifies that execution doesn't crash when jobRepo is nil
func TestConsecutiveFailures_NoRepoNoCrash(t *testing.T) {
	runRepo := &mockJobRunRepository{}

	cfg := &config.Config{
		Scheduler: config.SchedulerConfig{
			MaxConsecutiveFailures: 3,
		},
	}

	executors := map[domain.PayloadType]JobExecutor{
		domain.PayloadMessage: &failingExecutor{},
	}

	// Pass nil for jobRepo
	executor := NewJobExecutor(executors, runRepo, nil, cfg, nil)

	job := domain.NewJob("Test Job", "org-123", "user-123", "*/15 * * * *", "UTC", domain.PayloadMessage, nil)

	// Should not crash even though jobRepo is nil
	err := executor.Execute(job)
	if err == nil {
		t.Error("Expected job to fail, but it succeeded")
	}

	// Verify job run was still created
	if len(runRepo.savedRuns) == 0 {
		t.Error("Expected job run to be created even when jobRepo is nil")
	}
}

// TestConsecutiveFailures_MultipleFailures verifies correct behavior across multiple failures
func TestConsecutiveFailures_MultipleFailures(t *testing.T) {
	jobRepo := &mockJobRepository{}
	runRepo := &mockJobRunRepository{}

	cfg := &config.Config{
		Scheduler: config.SchedulerConfig{
			MaxConsecutiveFailures: 3,
		},
	}

	executors := map[domain.PayloadType]JobExecutor{
		domain.PayloadMessage: &failingExecutor{},
	}

	executor := NewJobExecutor(executors, runRepo, jobRepo, cfg, nil)

	job := domain.NewJob("Test Job", "org-123", "user-123", "*/15 * * * *", "UTC", domain.PayloadMessage, nil)

	// First failure
	executor.Execute(job)
	if len(jobRepo.savedJobs) != 1 {
		t.Fatalf("Expected 1 saved job after first failure")
	}
	if jobRepo.savedJobs[0].ConsecutiveFailures != 1 {
		t.Errorf("After 1st failure: expected consecutive_failures = 1, got %d", jobRepo.savedJobs[0].ConsecutiveFailures)
	}
	if jobRepo.savedJobs[0].Status != domain.StatusScheduled {
		t.Errorf("After 1st failure: expected status 'scheduled', got %s", jobRepo.savedJobs[0].Status)
	}

	// Second failure (use the updated job from first save)
	job = jobRepo.savedJobs[0]
	jobRepo.savedJobs = nil // Reset
	executor.Execute(job)
	if len(jobRepo.savedJobs) != 1 {
		t.Fatalf("Expected 1 saved job after second failure")
	}
	if jobRepo.savedJobs[0].ConsecutiveFailures != 2 {
		t.Errorf("After 2nd failure: expected consecutive_failures = 2, got %d", jobRepo.savedJobs[0].ConsecutiveFailures)
	}
	if jobRepo.savedJobs[0].Status != domain.StatusScheduled {
		t.Errorf("After 2nd failure: expected status 'scheduled', got %s", jobRepo.savedJobs[0].Status)
	}

	// Third failure (should trigger pause)
	job = jobRepo.savedJobs[0]
	jobRepo.savedJobs = nil // Reset
	executor.Execute(job)
	if len(jobRepo.savedJobs) != 1 {
		t.Fatalf("Expected 1 saved job after third failure")
	}
	if jobRepo.savedJobs[0].ConsecutiveFailures != 3 {
		t.Errorf("After 3rd failure: expected consecutive_failures = 3, got %d", jobRepo.savedJobs[0].ConsecutiveFailures)
	}
	if jobRepo.savedJobs[0].Status != domain.StatusPaused {
		t.Errorf("After 3rd failure: expected status 'paused', got %s", jobRepo.savedJobs[0].Status)
	}
}

// TestConsecutiveFailures_SuccessResetsCount verifies that success between failures resets the count
func TestConsecutiveFailures_SuccessResetsCount(t *testing.T) {
	jobRepo := &mockJobRepository{}
	runRepo := &mockJobRunRepository{}

	cfg := &config.Config{
		Scheduler: config.SchedulerConfig{
			MaxConsecutiveFailures: 3,
		},
	}

	job := domain.NewJob("Test Job", "org-123", "user-123", "*/15 * * * *", "UTC", domain.PayloadMessage, nil)

	// Fail twice
	failExecutor := NewJobExecutor(
		map[domain.PayloadType]JobExecutor{domain.PayloadMessage: &failingExecutor{}},
		runRepo, jobRepo, cfg, nil,
	)

	failExecutor.Execute(job)
	job = jobRepo.savedJobs[0]
	jobRepo.savedJobs = nil

	failExecutor.Execute(job)
	job = jobRepo.savedJobs[0]
	if job.ConsecutiveFailures != 2 {
		t.Fatalf("After 2 failures: expected consecutive_failures = 2, got %d", job.ConsecutiveFailures)
	}
	jobRepo.savedJobs = nil

	// Succeed once (should reset)
	successExecutor := NewJobExecutor(
		map[domain.PayloadType]JobExecutor{domain.PayloadMessage: &successfulExecutor{}},
		runRepo, jobRepo, cfg, nil,
	)

	successExecutor.Execute(job)
	job = jobRepo.savedJobs[0]
	if job.ConsecutiveFailures != 0 {
		t.Errorf("After success: expected consecutive_failures = 0, got %d", job.ConsecutiveFailures)
	}
	jobRepo.savedJobs = nil

	// Fail again (should be count 1, not 3)
	failExecutor.Execute(job)
	job = jobRepo.savedJobs[0]
	if job.ConsecutiveFailures != 1 {
		t.Errorf("After failure following success: expected consecutive_failures = 1, got %d", job.ConsecutiveFailures)
	}
	if job.Status == domain.StatusPaused {
		t.Error("Job should not be paused after only 1 failure following a success")
	}
}

// TestNotification_SentOnFailure verifies that a failure notification is sent when a job fails
func TestNotification_SentOnFailure(t *testing.T) {
	jobRepo := &mockJobRepository{}
	runRepo := &mockJobRunRepository{}
	notifier := &mockNotifier{}

	cfg := &config.Config{
		Scheduler: config.SchedulerConfig{
			MaxConsecutiveFailures: 3,
		},
	}

	executors := map[domain.PayloadType]JobExecutor{
		domain.PayloadMessage: &failingExecutor{},
	}

	executor := NewJobExecutor(executors, runRepo, jobRepo, cfg, notifier)

	job := domain.NewJob("Test Job", "org-123", "user-123", "*/15 * * * *", "UTC", domain.PayloadMessage, nil)

	// Execute the job (which will fail)
	err := executor.Execute(job)
	if err == nil {
		t.Error("Expected job to fail, but it succeeded")
	}

	// Verify notification was sent
	if len(notifier.failureNotifications) != 1 {
		t.Fatalf("Expected 1 failure notification, got %d", len(notifier.failureNotifications))
	}

	notification := notifier.failureNotifications[0]
	if notification.JobID != job.ID {
		t.Errorf("Expected JobID %s, got %s", job.ID, notification.JobID)
	}
	if notification.JobName != job.Name {
		t.Errorf("Expected JobName %s, got %s", job.Name, notification.JobName)
	}
	if notification.OrgID != job.OrgID {
		t.Errorf("Expected OrgID %s, got %s", job.OrgID, notification.OrgID)
	}
	if notification.ConsecutiveFailures != 1 {
		t.Errorf("Expected ConsecutiveFailures 1, got %d", notification.ConsecutiveFailures)
	}
	if notification.AutoPaused {
		t.Error("Expected AutoPaused to be false for first failure")
	}
	if notification.ErrorMsg == "" {
		t.Error("Expected error message to be set")
	}
}

// TestNotification_AutoPausedFlagSet verifies that AutoPaused flag is set when job is paused
func TestNotification_AutoPausedFlagSet(t *testing.T) {
	jobRepo := &mockJobRepository{}
	runRepo := &mockJobRunRepository{}
	notifier := &mockNotifier{}

	cfg := &config.Config{
		Scheduler: config.SchedulerConfig{
			MaxConsecutiveFailures: 3,
		},
	}

	executors := map[domain.PayloadType]JobExecutor{
		domain.PayloadMessage: &failingExecutor{},
	}

	executor := NewJobExecutor(executors, runRepo, jobRepo, cfg, notifier)

	// Create a job that has already failed 2 times
	job := domain.NewJob("Test Job", "org-123", "user-123", "*/15 * * * *", "UTC", domain.PayloadMessage, nil)
	job = job.WithIncrementedFailures()
	job = job.WithIncrementedFailures()

	// Execute the job (3rd failure - should trigger pause)
	err := executor.Execute(job)
	if err == nil {
		t.Error("Expected job to fail, but it succeeded")
	}

	// Verify notification was sent with AutoPaused flag
	if len(notifier.failureNotifications) != 1 {
		t.Fatalf("Expected 1 failure notification, got %d", len(notifier.failureNotifications))
	}

	notification := notifier.failureNotifications[0]
	if notification.ConsecutiveFailures != 3 {
		t.Errorf("Expected ConsecutiveFailures 3, got %d", notification.ConsecutiveFailures)
	}
	if !notification.AutoPaused {
		t.Error("Expected AutoPaused to be true when threshold reached")
	}
}

// TestNotification_NoNotificationOnSuccess verifies that no notification is sent when a job succeeds
func TestNotification_NoNotificationOnSuccess(t *testing.T) {
	jobRepo := &mockJobRepository{}
	runRepo := &mockJobRunRepository{}
	notifier := &mockNotifier{}

	cfg := &config.Config{
		Scheduler: config.SchedulerConfig{
			MaxConsecutiveFailures: 3,
		},
	}

	executors := map[domain.PayloadType]JobExecutor{
		domain.PayloadMessage: &successfulExecutor{},
	}

	executor := NewJobExecutor(executors, runRepo, jobRepo, cfg, notifier)

	job := domain.NewJob("Test Job", "org-123", "user-123", "*/15 * * * *", "UTC", domain.PayloadMessage, nil)

	// Execute the job (which will succeed)
	err := executor.Execute(job)
	if err != nil {
		t.Errorf("Expected job to succeed, but got error: %v", err)
	}

	// Verify NO notification was sent
	if len(notifier.failureNotifications) != 0 {
		t.Errorf("Expected 0 failure notifications on success, got %d", len(notifier.failureNotifications))
	}
}

// TestNotification_NoNotificationWhenDisabled verifies that no notification is sent when feature is disabled
func TestNotification_NoNotificationWhenDisabled(t *testing.T) {
	jobRepo := &mockJobRepository{}
	runRepo := &mockJobRunRepository{}
	notifier := &mockNotifier{}

	cfg := &config.Config{
		Scheduler: config.SchedulerConfig{
			MaxConsecutiveFailures: 0, // Feature disabled
		},
	}

	executors := map[domain.PayloadType]JobExecutor{
		domain.PayloadMessage: &failingExecutor{},
	}

	executor := NewJobExecutor(executors, runRepo, jobRepo, cfg, notifier)

	job := domain.NewJob("Test Job", "org-123", "user-123", "*/15 * * * *", "UTC", domain.PayloadMessage, nil)

	// Execute the job (which will fail)
	err := executor.Execute(job)
	if err == nil {
		t.Error("Expected job to fail, but it succeeded")
	}

	// Verify NO notification was sent (feature disabled)
	if len(notifier.failureNotifications) != 0 {
		t.Errorf("Expected 0 failure notifications when feature disabled, got %d", len(notifier.failureNotifications))
	}
}
