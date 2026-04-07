package executor

import (
	"context"
	"fmt"
	"log"
	"sync"

	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/usecases"
)

type DefaultJobExecutor struct {
	executors map[domain.PayloadType]JobExecutor
	runRepo   usecases.JobRunRepository
	jobRepo   usecases.JobRepository
	config    *config.Config
	notifier  JobCompletionNotifier
	wg        sync.WaitGroup // Tracks in-flight jobs for graceful shutdown
}

func NewJobExecutor(executors map[domain.PayloadType]JobExecutor, runRepo usecases.JobRunRepository, jobRepo usecases.JobRepository, cfg *config.Config, notifier JobCompletionNotifier) *DefaultJobExecutor {
	return &DefaultJobExecutor{
		executors: executors,
		runRepo:   runRepo,
		jobRepo:   jobRepo,
		config:    cfg,
		notifier:  notifier,
	}
}

func (e *DefaultJobExecutor) Execute(job domain.Job) error {
	// Track this job for graceful shutdown
	e.wg.Add(1)
	defer e.wg.Done()

	log.Printf("Executing job: %s (%s)", job.Name, job.ID)

	// Increment the currently running jobs metric
	JobsCurrentlyRunning.Inc()
	defer JobsCurrentlyRunning.Dec()

	// Create a job run record
	var jobRun domain.JobRun
	if e.runRepo != nil {
		jobRun = domain.NewJobRun(job.ID)
		if err := e.runRepo.Save(jobRun); err != nil {
			log.Printf("Failed to create job run record: %v", err)
			// Continue with execution even if we can't save the run
		} else {
			log.Printf("Created job run: %s for job: %s", jobRun.ID, job.ID)
		}
	}

	// Execute the job using the appropriate executor
	var execErr error
	executor, ok := e.executors[job.Type]
	if !ok {
		execErr = fmt.Errorf("no executor found for payload type: %s", job.Type)
	} else {
		execErr = executor.Execute(job)
	}

	// Update the job run record and handle failure tracking
	if e.runRepo != nil && jobRun.ID != "" {
		if execErr != nil {
			jobRun = jobRun.WithFailed(execErr.Error())

			// Increment consecutive failure counter and check threshold
			if e.jobRepo != nil && e.config != nil {
				job = e.handleJobFailure(job, execErr.Error())
			}
		} else {
			result := fmt.Sprintf("Job %s completed successfully", job.Name)
			jobRun = jobRun.WithCompleted(result)

			// Reset consecutive failure counter on success
			if e.jobRepo != nil && job.ConsecutiveFailures > 0 {
				job = job.WithResetFailures()
				if err := e.jobRepo.Save(job); err != nil {
					log.Printf("Failed to reset failure count for job %s: %v", job.ID, err)
				} else {
					log.Printf("Reset failure count for job %s", job.ID)
				}
			}
		}

		if err := e.runRepo.Save(jobRun); err != nil {
			log.Printf("Failed to update job run record: %v", err)
		} else {
			log.Printf("Updated job run: %s with status: %s", jobRun.ID, jobRun.Status)
		}
	}

	return execErr
}

// Wait blocks until all in-flight jobs complete
// This is used for graceful shutdown
func (e *DefaultJobExecutor) Wait() {
	e.wg.Wait()
}

// handleJobFailure increments the consecutive failure counter and pauses the job if threshold is exceeded
// Returns the updated job (which should be used for subsequent operations)
func (e *DefaultJobExecutor) handleJobFailure(job domain.Job, errorMsg string) domain.Job {
	maxFailures := e.config.Scheduler.MaxConsecutiveFailures
	if maxFailures <= 0 {
		return job // Feature disabled
	}

	// Increment consecutive failures counter
	updatedJob := job.WithIncrementedFailures()

	log.Printf("Job %s (%s) has %d consecutive failures (threshold: %d)",
		updatedJob.Name, updatedJob.ID, updatedJob.ConsecutiveFailures, maxFailures)

	autoPaused := false

	// If threshold exceeded, pause the job
	if updatedJob.ConsecutiveFailures >= maxFailures {
		log.Printf("WARNING: Job %s (%s) has failed %d consecutive times, pausing job",
			updatedJob.Name, updatedJob.ID, updatedJob.ConsecutiveFailures)

		updatedJob = updatedJob.WithStatus(domain.StatusPaused)
		autoPaused = true
	}

	// Save the updated job (with incremented counter and possibly paused status)
	if err := e.jobRepo.Save(updatedJob); err != nil {
		log.Printf("ERROR: Failed to update job %s after failure: %v", updatedJob.ID, err)
	} else {
		if updatedJob.Status == domain.StatusPaused {
			log.Printf("Job %s (%s) has been paused due to repeated failures", updatedJob.Name, updatedJob.ID)
		}
	}

	// Send failure notification
	if e.notifier != nil {
		notification := &JobFailureNotification{
			JobID:               updatedJob.ID,
			JobName:             updatedJob.Name,
			OrgID:               updatedJob.OrgID,
			ErrorMsg:            errorMsg,
			ConsecutiveFailures: updatedJob.ConsecutiveFailures,
			AutoPaused:          autoPaused,
		}

		if err := e.notifier.JobFailed(context.Background(), notification); err != nil {
			// Don't fail the job execution if notification fails
			log.Printf("WARNING: Failed to send failure notification for job %s: %v", updatedJob.ID, err)
		}
	}

	return updatedJob
}
