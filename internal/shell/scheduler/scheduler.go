package scheduler

import (
	"context"
	"log/slog"
	"sync"

	"github.com/robfig/cron/v3"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/ports"
)

type CronScheduler struct {
	jobService ports.SchedulerJobService
	cron       *cron.Cron
	jobEntries map[string]cron.EntryID // jobID -> cronEntryID mapping
	mu         sync.RWMutex
	logger     *slog.Logger
}

func NewCronScheduler(jobService ports.SchedulerJobService, logger *slog.Logger) *CronScheduler {
	return &CronScheduler{
		jobService: jobService,
		cron:       cron.New(), // Standard 5-field format (minute hour dom month dow)
		jobEntries: make(map[string]cron.EntryID),
		logger:     logger,
	}
}

func (s *CronScheduler) Start(ctx context.Context) {
	s.logger.Info("Starting cron scheduler")

	// Load existing jobs and schedule them
	s.loadAndScheduleAllJobs()

	// Start the cron scheduler
	s.cron.Start()

	// Wait for context cancellation
	<-ctx.Done()
	s.logger.Info("Scheduler context cancelled, stopping")
}

func (s *CronScheduler) Stop() {
	s.logger.Info("Stopping cron scheduler")
	s.mu.Lock()
	ctx := s.cron.Stop()
	s.mu.Unlock()
	<-ctx.Done()
	s.logger.Info("Cron scheduler stopped")
}

// ScheduleJob adds or updates a job in the cron scheduler
func (s *CronScheduler) ScheduleJob(job domain.Job) error {
	s.logger.Debug("ScheduleJob called",
		slog.String("job_id", job.ID),
		slog.String("name", job.Name),
		slog.String("schedule", string(job.Schedule)),
		slog.String("status", string(job.Status)))

	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove existing entry if it exists
	if entryID, exists := s.jobEntries[job.ID]; exists {
		s.logger.Debug("Removing existing cron entry", slog.String("job_id", job.ID))
		s.cron.Remove(entryID)
		delete(s.jobEntries, job.ID)
	}

	// Only schedule jobs that are in scheduled status
	if job.Status != domain.StatusScheduled && job.Status != domain.StatusFailed {
		s.logger.Debug("Job not in scheduled/failed status, skipping",
			slog.String("job_id", job.ID),
			slog.String("status", string(job.Status)))
		return nil
	}

	// Create job execution function
	jobFunc := func() {
		s.logger.Info("Executing cron job",
			slog.String("job_id", job.ID),
			slog.String("name", job.Name))

		// Get the latest job state from repository
		currentJob, err := s.jobService.GetJob(context.Background(), job.ID)
		if err != nil {
			s.logger.Error("Error getting job for execution",
				slog.String("job_id", job.ID),
				slog.Any("error", err))
			return
		}

		// Only execute if job is still scheduled
		if currentJob.Status != domain.StatusScheduled {
			s.logger.Debug("Job no longer scheduled, skipping execution",
				slog.String("job_id", job.ID))
			return
		}

		if err := s.jobService.ExecuteScheduledJob(currentJob); err != nil {
			s.logger.Error("Error executing job",
				slog.String("job_id", job.ID),
				slog.Any("error", err))
		}
	}

	// Schedule the job
	entryID, err := s.cron.AddFunc(string(job.Schedule), jobFunc)
	if err != nil {
		s.logger.Error("Failed to add job to cron",
			slog.String("job_id", job.ID),
			slog.Any("error", err))
		return err
	}

	s.jobEntries[job.ID] = entryID
	s.logger.Info("Job scheduled",
		slog.String("job_id", job.ID),
		slog.String("name", job.Name),
		slog.String("schedule", string(job.Schedule)),
		slog.Int("entry_id", int(entryID)))

	return nil
}

// UnscheduleJob removes a job from the cron scheduler
func (s *CronScheduler) UnscheduleJob(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, exists := s.jobEntries[jobID]; exists {
		s.cron.Remove(entryID)
		delete(s.jobEntries, jobID)
		s.logger.Info("Job unscheduled", slog.String("job_id", jobID))
	}
}

// ScheduleJobImmediately executes a job immediately in the cron scheduler
// For the in-memory cron scheduler, we execute directly since there's no distributed system
func (s *CronScheduler) ScheduleJobImmediately(job domain.Job, jobRunID string) error {
	s.logger.Info("Executing job immediately",
		slog.String("job_id", job.ID),
		slog.String("run_id", jobRunID))

	// Get the latest job state from repository
	currentJob, err := s.jobService.GetJob(context.Background(), job.ID)
	if err != nil {
		s.logger.Error("Error getting job for immediate execution",
			slog.String("job_id", job.ID),
			slog.Any("error", err))
		return err
	}

	// Execute the job immediately with the pre-created job run
	if err := s.jobService.ExecuteScheduledJobWithJobRun(currentJob, jobRunID); err != nil {
		s.logger.Error("Error executing job immediately",
			slog.String("job_id", job.ID),
			slog.String("run_id", jobRunID),
			slog.Any("error", err))
		return err
	}

	s.logger.Info("Job executed immediately",
		slog.String("job_id", job.ID),
		slog.String("run_id", jobRunID))
	return nil
}

// loadAndScheduleAllJobs loads all scheduled jobs from the repository and schedules them
func (s *CronScheduler) loadAndScheduleAllJobs() {
	jobs, err := s.jobService.ListJobs()
	if err != nil {
		s.logger.Error("Error loading jobs", slog.Any("error", err))
		return
	}

	s.logger.Info("Loading jobs for scheduling", slog.Int("count", len(jobs)))

	for _, job := range jobs {
		// Only schedule jobs that are scheduled or failed
		if job.Status == domain.StatusScheduled || job.Status == domain.StatusFailed {
			if err := s.ScheduleJob(job); err != nil {
				s.logger.Error("Error scheduling job",
					slog.String("job_id", job.ID),
					slog.Any("error", err))
			}
		}
	}
}
