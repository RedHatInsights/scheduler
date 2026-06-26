package executor

import (
	"fmt"
	"log/slog"
	"sync"

	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/usecases"
	"insights-scheduler/internal/shell/logging"
)

// DefaultJobExecutor implements ports.JobExecutor by orchestrating JobRunners.
// It handles job run record creation, dispatches to payload-specific runners,
// and manages graceful shutdown.
type DefaultJobExecutor struct {
	runners    map[domain.PayloadType]JobRunner
	runRepo    usecases.JobRunRepository
	baseLogger *slog.Logger
	wg         sync.WaitGroup // Tracks in-flight jobs for graceful shutdown
}

func NewJobExecutor(runners map[domain.PayloadType]JobRunner, runRepo usecases.JobRunRepository, baseLogger *slog.Logger) *DefaultJobExecutor {
	return &DefaultJobExecutor{
		runners:    runners,
		runRepo:    runRepo,
		baseLogger: baseLogger,
	}
}

func (e *DefaultJobExecutor) Execute(job domain.Job) error {
	// Track this job for graceful shutdown
	e.wg.Add(1)
	defer e.wg.Done()

	// Create job run record to get run ID
	var jobRun domain.JobRun
	if e.runRepo != nil {
		jobRun = domain.NewJobRun(job.ID)
		if err := e.runRepo.Save(jobRun); err != nil {
			// Create logger without run_id if we can't create the run
			logger := logging.NewJobExecutionLogger(e.baseLogger, job.ID, "", job.OrgID, job.UserID)
			logger.Error("Failed to create job run record", slog.Any("error", err))
			// Continue with execution even if we can't save the run
		}
	}

	// Create logger with all context fields
	logger := logging.NewJobExecutionLogger(e.baseLogger, job.ID, jobRun.ID, job.OrgID, job.UserID)
	logger.Info("Executing job", slog.String("name", job.Name), slog.String("type", string(job.Type)))

	// Increment the currently running jobs metric
	JobsCurrentlyRunning.Inc()
	defer JobsCurrentlyRunning.Dec()

	// Execute the job using the appropriate runner
	var execErr error
	var result interface{}
	var resultType domain.ResultType
	runner, ok := e.runners[job.Type]
	if !ok {
		execErr = fmt.Errorf("no runner found for payload type: %s", job.Type)
		logger.Error("No runner found for payload type", slog.String("type", string(job.Type)))
	} else {
		result, resultType, execErr = runner.Execute(job, logger)
	}

	// Update the job run record
	if e.runRepo != nil && jobRun.ID != "" {
		if execErr != nil {
			jobRun = jobRun.WithFailed(execErr.Error())
			logger.Error("Job execution failed", slog.Any("error", execErr))
		} else {
			jobRun = jobRun.WithCompleted(resultType, result)
			logger.Info("Job execution completed", slog.String("status", string(jobRun.Status)))
		}

		if err := e.runRepo.Save(jobRun); err != nil {
			logger.Error("Failed to update job run record", slog.Any("error", err))
		} else {
			logger.Debug("Updated job run", slog.String("status", string(jobRun.Status)))
		}
	}

	return execErr
}

// ExecuteWithJobRun executes a job with a pre-created job run ID
func (e *DefaultJobExecutor) ExecuteWithJobRun(job domain.Job, jobRunID string) error {
	// Track this job for graceful shutdown
	e.wg.Add(1)
	defer e.wg.Done()

	// Create logger with all context fields
	logger := logging.NewJobExecutionLogger(e.baseLogger, job.ID, jobRunID, job.OrgID, job.UserID)
	logger.Info("Executing job with pre-created run", slog.String("name", job.Name), slog.String("type", string(job.Type)))

	// Increment the currently running jobs metric
	JobsCurrentlyRunning.Inc()
	defer JobsCurrentlyRunning.Dec()

	// Load the existing job run record
	var jobRun domain.JobRun
	if e.runRepo != nil && jobRunID != "" {
		var err error
		jobRun, err = e.runRepo.FindByID(jobRunID)
		if err != nil {
			logger.Error("Failed to load job run, creating fallback", slog.Any("error", err))
			// Continue with execution but create a new run as fallback
			jobRun = domain.NewJobRun(job.ID)
			if saveErr := e.runRepo.Save(jobRun); saveErr != nil {
				logger.Error("Failed to create fallback job run", slog.Any("error", saveErr))
			}
		} else {
			logger.Debug("Using pre-created job run")
		}
	}

	// Execute the job using the appropriate runner
	var execErr error
	var result interface{}
	var resultType domain.ResultType
	runner, ok := e.runners[job.Type]
	if !ok {
		execErr = fmt.Errorf("no runner found for payload type: %s", job.Type)
		logger.Error("No runner found for payload type", slog.String("type", string(job.Type)))
	} else {
		result, resultType, execErr = runner.Execute(job, logger)
	}

	// Update the job run record
	if e.runRepo != nil && jobRun.ID != "" {
		if execErr != nil {
			jobRun = jobRun.WithFailed(execErr.Error())
			logger.Error("Job execution failed", slog.Any("error", execErr))
		} else {
			jobRun = jobRun.WithCompleted(resultType, result)
			logger.Info("Job execution completed", slog.String("status", string(jobRun.Status)))
		}

		if err := e.runRepo.Save(jobRun); err != nil {
			logger.Error("Failed to update job run record", slog.Any("error", err))
		} else {
			logger.Debug("Updated job run", slog.String("status", string(jobRun.Status)))
		}
	}

	return execErr
}

// Wait blocks until all in-flight jobs complete
// This is used for graceful shutdown
func (e *DefaultJobExecutor) Wait() {
	e.wg.Wait()
}
