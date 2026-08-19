package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"insights-scheduler/internal/clients/export"
	"insights-scheduler/internal/clients/polling"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/usecases"
	"insights-scheduler/internal/identity"
	"insights-scheduler/internal/shell/executor"
)

const (
	exportPollLockPrefix = "scheduler:export-poll:"
	exportPollLockTTL    = 30 * time.Second
)

// ExportPollerService runs a continuous background loop that scans for
// in-flight export jobs and checks their status. When an export reaches
// a terminal state, it completes the job run, sends notifications,
// and tracks failures.
//
// This replaces both the inline polling in ExportJobExecutor and the
// startup-only PollingRecovery, providing a single code path for all
// export completion handling.
type ExportPollerService struct {
	runRepo            usecases.JobRunRepository
	jobRepo            usecases.JobRepository
	exportClient       *export.Client
	userValidator      identity.UserValidator
	notifier           executor.JobCompletionNotifier
	failureTracker     *executor.FailureTracker
	lock               *DistributedLock // nil = no locking (single-pod mode)
	logger             *slog.Logger
	podID              string
	scanInterval       time.Duration
	maxAge             time.Duration
	maxConcurrentPolls int
	pollSemaphore      chan struct{} // Buffered channel for concurrent poll limiting
	activePolls        sync.WaitGroup
}

func NewExportPollerService(
	runRepo usecases.JobRunRepository,
	jobRepo usecases.JobRepository,
	exportClient *export.Client,
	userValidator identity.UserValidator,
	notifier executor.JobCompletionNotifier,
	failureTracker *executor.FailureTracker,
	lock *DistributedLock,
	scanInterval time.Duration,
	maxAge time.Duration,
	maxConcurrentPolls int,
	logger *slog.Logger,
) *ExportPollerService {
	if maxConcurrentPolls <= 0 {
		maxConcurrentPolls = 20 // Default
	}

	return &ExportPollerService{
		runRepo:            runRepo,
		jobRepo:            jobRepo,
		exportClient:       exportClient,
		userValidator:      userValidator,
		notifier:           notifier,
		failureTracker:     failureTracker,
		lock:               lock,
		logger:             logger,
		podID:              GetPodID(),
		scanInterval:       scanInterval,
		maxAge:             maxAge,
		maxConcurrentPolls: maxConcurrentPolls,
		pollSemaphore:      make(chan struct{}, maxConcurrentPolls),
		activePolls:        sync.WaitGroup{},
	}
}

// Start runs the polling loop until ctx is cancelled.
// Blocks the caller; run in a goroutine.
func (s *ExportPollerService) Start(ctx context.Context) {
	s.logger.Info("Starting export poller service",
		slog.String("pod_id", s.podID),
		slog.Duration("scan_interval", s.scanInterval),
		slog.Duration("max_age", s.maxAge),
		slog.Int("max_concurrent_polls", s.maxConcurrentPolls))

	s.scanAndProcess(ctx)

	ticker := time.NewTicker(s.scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Export poller service shutting down, waiting for active polls...")
			s.activePolls.Wait()
			s.logger.Info("Export poller service shut down gracefully")
			return
		case <-ticker.C:
			s.scanAndProcess(ctx)
		}
	}
}

func (s *ExportPollerService) scanAndProcess(ctx context.Context) {
	runs, err := s.runRepo.FindByStatus(ctx, domain.RunStatusRunning)
	if err != nil {
		s.logger.Error("Failed to scan for running exports", slog.Any("error", err))
		return
	}

	// Filter to export runs only
	var exportRuns []domain.JobRun
	for _, run := range runs {
		if run.ExternalJobID == nil || run.ExternalService == nil {
			continue
		}
		if *run.ExternalService != "export" {
			continue
		}
		exportRuns = append(exportRuns, run)
	}

	if len(exportRuns) == 0 {
		return
	}

	s.logger.Debug("Processing export runs concurrently",
		slog.Int("count", len(exportRuns)),
		slog.Int("max_concurrent", s.maxConcurrentPolls))

	// Dispatch runs concurrently with worker pool limiting
	for _, run := range exportRuns {
		if ctx.Err() != nil {
			return
		}

		run := run // Capture loop variable for goroutine

		s.activePolls.Add(1)
		go func() {
			defer s.activePolls.Done()

			// Acquire worker slot (blocks if pool is full)
			s.pollSemaphore <- struct{}{}
			defer func() { <-s.pollSemaphore }()

			if time.Since(run.StartTime) > s.maxAge {
				s.markAsTimedOut(ctx, run)
			} else {
				s.processRun(ctx, run)
			}
		}()
	}
}

func (s *ExportPollerService) processRun(ctx context.Context, run domain.JobRun) {
	lockKey := exportPollLockPrefix + run.ID

	if s.lock != nil {
		acquired, err := s.lock.TryAcquire(ctx, lockKey, s.podID, exportPollLockTTL)
		if err != nil {
			s.logger.Error("Failed to acquire poll lock",
				slog.String("run_id", run.ID),
				slog.Any("error", err))
			return
		}
		if !acquired {
			return
		}
		defer s.lock.Release(ctx, lockKey, s.podID)
	}

	logger := s.logger.With(
		slog.String("run_id", run.ID),
		slog.String("job_id", run.JobID),
		slog.String("export_id", *run.ExternalJobID),
	)

	job, err := s.jobRepo.FindByID(run.JobID)
	if err != nil {
		logger.Error("Failed to load job for polling", slog.Any("error", err))
		return
	}

	identityHeader, err := s.userValidator.GenerateIdentityHeader(ctx, job.OrgID, job.UserID)
	if err != nil {
		logger.Warn("Failed to generate identity for polling, will retry", slog.Any("error", err))
		return
	}

	poller := export.NewExportPoller(s.exportClient, identityHeader)
	status, err := poller.GetStatus(ctx, *run.ExternalJobID)
	if err != nil {
		logger.Warn("Failed to check export status, will retry", slog.Any("error", err))
		return
	}

	if !status.IsTerminal && !poller.IsTerminalStatus(status.Status) {
		return
	}

	s.completeRun(ctx, run, job, status, logger)
}

func (s *ExportPollerService) completeRun(
	ctx context.Context,
	run domain.JobRun,
	job domain.Job,
	status *polling.StatusResponse,
	logger *slog.Logger,
) {
	externalJobID := *run.ExternalJobID

	if status.Status == polling.StatusComplete {
		downloadURL := s.exportClient.GetExportDownloadURL(externalJobID)
		result := domain.ExportResult{ExportID: externalJobID, URL: downloadURL}
		run = run.WithCompleted(domain.ResultTypeExport, result)
		logger.Info("Export completed", slog.String("download_url", downloadURL))
	} else {
		errorMsg := status.Error
		if errorMsg == "" {
			errorMsg = "Export processing failed"
		}
		run = run.WithFailed(errorMsg)
		logger.Warn("Export failed", slog.String("error", errorMsg))
	}

	if err := s.runRepo.Save(run); err != nil {
		logger.Error("Failed to save completed job run", slog.Any("error", err))
		return
	}

	s.sendNotification(ctx, externalJobID, job, status, logger)

	if s.failureTracker != nil {
		if status.Status == polling.StatusComplete {
			s.failureTracker.TrackSuccess(job, logger)
		} else {
			s.failureTracker.TrackFailure(job, fmt.Errorf("%s", status.Error), logger)
		}
	}
}

func (s *ExportPollerService) sendNotification(
	ctx context.Context,
	exportID string,
	job domain.Job,
	status *polling.StatusResponse,
	logger *slog.Logger,
) {
	if s.notifier == nil {
		return
	}

	downloadURL := ""
	errorMsg := status.Error

	if status.Status == polling.StatusComplete {
		downloadURL = s.exportClient.GetExportDownloadURL(exportID)
	} else if errorMsg == "" {
		errorMsg = "Export processing failed"
	}

	notification := &executor.ExportCompletionNotification{
		ExportID:    exportID,
		JobID:       job.ID,
		JobName:     job.Name,
		AccountID:   "",
		OrgID:       job.OrgID,
		Status:      string(status.Status),
		DownloadURL: downloadURL,
		ErrorMsg:    errorMsg,
	}

	if err := s.notifier.JobComplete(ctx, notification, logger); err != nil {
		logger.Warn("Failed to send completion notification",
			slog.String("export_id", exportID),
			slog.Any("error", err))
	}
}

func (s *ExportPollerService) markAsTimedOut(ctx context.Context, run domain.JobRun) {
	logger := s.logger.With(
		slog.String("run_id", run.ID),
		slog.String("job_id", run.JobID),
		slog.Duration("age", time.Since(run.StartTime)),
	)

	if s.lock != nil {
		lockKey := exportPollLockPrefix + run.ID
		acquired, err := s.lock.TryAcquire(ctx, lockKey, s.podID, exportPollLockTTL)
		if err != nil || !acquired {
			return
		}
		defer s.lock.Release(ctx, lockKey, s.podID)
	}

	// Reload run from database after acquiring lock to check if another pod already completed it
	currentRun, err := s.runRepo.FindByID(run.ID)
	if err != nil {
		logger.Error("Failed to reload run after acquiring lock", slog.Any("error", err))
		return
	}

	// Check if run is still in-flight
	if currentRun.Status != domain.RunStatusRunning {
		logger.Debug("Run already completed by another pod, skipping timeout",
			slog.String("current_status", string(currentRun.Status)))
		return
	}

	logger.Warn("Export job run timed out, marking as failed")

	currentRun = currentRun.WithFailed("Execution timeout - exceeded maximum duration")
	if err := s.runRepo.Save(currentRun); err != nil {
		logger.Error("Failed to save timed out job run", slog.Any("error", err))
		return
	}

	if s.failureTracker != nil {
		job, err := s.jobRepo.FindByID(currentRun.JobID)
		if err != nil {
			logger.Error("Failed to load job for timeout failure tracking", slog.Any("error", err))
			return
		}
		s.failureTracker.TrackFailure(job, fmt.Errorf("execution timeout - exceeded maximum duration"), logger)
	}
}
