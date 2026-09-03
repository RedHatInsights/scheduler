package executor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"insights-scheduler/internal/core/domain"
)

func TestFailureTracker_TrackSuccess(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := newMockJobRepo()
	notifier := &mockNotifier{}
	tracker := NewFailureTracker(repo, notifier, 3)

	job := domain.NewJob("Test Job", "org-123", "user-456", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	job = job.WithFailuresIncremented(time.Now().UTC())
	repo.Save(job)

	tracker.TrackSuccess(job, logger)

	updated, _ := repo.FindByID(job.ID)
	if updated.ConsecutiveFailures != 0 {
		t.Errorf("Expected failures reset to 0, got %d", updated.ConsecutiveFailures)
	}
	if updated.Status != domain.StatusScheduled {
		t.Errorf("Expected status scheduled, got %s", updated.Status)
	}
}

func TestFailureTracker_TrackFailure_BelowThreshold(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := newMockJobRepo()
	notifier := &mockNotifier{}
	tracker := NewFailureTracker(repo, notifier, 3)

	job := domain.NewJob("Test Job", "org-123", "user-456", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	repo.Save(job)

	tracker.TrackFailure(job, errors.New("something broke"), "run-123", logger)

	updated, _ := repo.FindByID(job.ID)
	if updated.ConsecutiveFailures != 1 {
		t.Errorf("Expected failures=1, got %d", updated.ConsecutiveFailures)
	}
	if updated.Status != domain.StatusScheduled {
		t.Errorf("Expected status scheduled (job stays active below threshold), got %s", updated.Status)
	}
	if updated.LastFailedAt == nil {
		t.Error("Expected last_failed_at to be set after a failure, got nil")
	}
	if len(notifier.jobAutoPausedCalls) != 0 {
		t.Errorf("Expected no auto-pause notification, got %d", len(notifier.jobAutoPausedCalls))
	}
}

func TestFailureTracker_TrackFailure_ReachesThreshold(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := newMockJobRepo()
	notifier := &mockNotifier{}
	tracker := NewFailureTracker(repo, notifier, 2)

	job := domain.NewJob("Test Job", "org-123", "user-456", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	repo.Save(job)

	tracker.TrackFailure(job, errors.New("fail 1"), "run-1", logger)
	job, _ = repo.FindByID(job.ID)
	tracker.TrackFailure(job, errors.New("fail 2"), "run-2", logger)

	updated, _ := repo.FindByID(job.ID)
	if updated.ConsecutiveFailures != 2 {
		t.Errorf("Expected failures=2, got %d", updated.ConsecutiveFailures)
	}
	if updated.Status != domain.StatusPaused {
		t.Errorf("Expected status paused, got %s", updated.Status)
	}
	if len(notifier.jobAutoPausedCalls) != 1 {
		t.Errorf("Expected 1 auto-pause notification, got %d", len(notifier.jobAutoPausedCalls))
	}
	if notifier.jobAutoPausedCalls[0].ConsecutiveFailures != 2 {
		t.Errorf("Expected 2 consecutive failures in notification, got %d", notifier.jobAutoPausedCalls[0].ConsecutiveFailures)
	}
	if notifier.jobAutoPausedCalls[0].RunID != "run-2" {
		t.Errorf("Expected run_id 'run-2' in notification, got %s", notifier.jobAutoPausedCalls[0].RunID)
	}
	if notifier.jobAutoPausedCalls[0].NextRunAt != nil {
		t.Errorf("Expected next_run_at to be nil for paused job, got %v", notifier.jobAutoPausedCalls[0].NextRunAt)
	}
}

func TestFailureTracker_TrackFailure_ThresholdDisabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	repo := newMockJobRepo()
	notifier := &mockNotifier{}
	tracker := NewFailureTracker(repo, notifier, 0)

	job := domain.NewJob("Test Job", "org-123", "user-456", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	repo.Save(job)

	for i := 0; i < 10; i++ {
		tracker.TrackFailure(job, errors.New("fail"), fmt.Sprintf("run-%d", i), logger)
		job, _ = repo.FindByID(job.ID)
	}

	if job.Status == domain.StatusPaused {
		t.Error("Job should not be paused when threshold is disabled")
	}
	if len(notifier.jobAutoPausedCalls) != 0 {
		t.Errorf("Expected no auto-pause notifications, got %d", len(notifier.jobAutoPausedCalls))
	}
}

// mockNotifierForTracker implements JobCompletionNotifier for failure tracker tests
type mockNotifierForTracker struct {
	completeCalls  []*ExportCompletionNotification
	autoPauseCalls []*JobAutoPausedNotification
}

func (m *mockNotifierForTracker) JobComplete(ctx context.Context, notification *ExportCompletionNotification, logger *slog.Logger) error {
	m.completeCalls = append(m.completeCalls, notification)
	return nil
}

func (m *mockNotifierForTracker) JobAutoPaused(ctx context.Context, notification *JobAutoPausedNotification, logger *slog.Logger) error {
	m.autoPauseCalls = append(m.autoPauseCalls, notification)
	return nil
}
