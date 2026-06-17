package executor

import (
	"fmt"
	"log"
	"sync"

	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/usecases"
)

type DefaultJobExecutor struct {
	executors map[domain.PayloadType]JobExecutor
	runRepo   usecases.JobRunRepository
	wg        sync.WaitGroup // Tracks in-flight jobs for graceful shutdown
}

func NewJobExecutor(executors map[domain.PayloadType]JobExecutor, runRepo usecases.JobRunRepository) *DefaultJobExecutor {
	return &DefaultJobExecutor{
		executors: executors,
		runRepo:   runRepo,
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
	var result interface{}
	var resultType domain.ResultType
	executor, ok := e.executors[job.Type]
	if !ok {
		execErr = fmt.Errorf("no executor found for payload type: %s", job.Type)
	} else {
		result, resultType, execErr = executor.Execute(job)
	}

	// Update the job run record
	if e.runRepo != nil && jobRun.ID != "" {
		if execErr != nil {
			jobRun = jobRun.WithFailed(execErr.Error())
		} else {
			jobRun = jobRun.WithCompleted(resultType, result)
		}

		if err := e.runRepo.Save(jobRun); err != nil {
			log.Printf("Failed to update job run record: %v", err)
		} else {
			log.Printf("Updated job run: %s with status: %s", jobRun.ID, jobRun.Status)
		}
	}

	return execErr
}

// ExecuteWithJobRun executes a job with a pre-created job run ID
func (e *DefaultJobExecutor) ExecuteWithJobRun(job domain.Job, jobRunID string) error {
	// Track this job for graceful shutdown
	e.wg.Add(1)
	defer e.wg.Done()

	log.Printf("Executing job: %s (%s) with job run: %s", job.Name, job.ID, jobRunID)

	// Increment the currently running jobs metric
	JobsCurrentlyRunning.Inc()
	defer JobsCurrentlyRunning.Dec()

	// Load the existing job run record
	var jobRun domain.JobRun
	if e.runRepo != nil && jobRunID != "" {
		var err error
		jobRun, err = e.runRepo.FindByID(jobRunID)
		if err != nil {
			log.Printf("Failed to load job run %s: %v", jobRunID, err)
			// Continue with execution but create a new run as fallback
			jobRun = domain.NewJobRun(job.ID)
			if saveErr := e.runRepo.Save(jobRun); saveErr != nil {
				log.Printf("Failed to create fallback job run: %v", saveErr)
			}
		} else {
			log.Printf("Using pre-created job run: %s for job: %s", jobRun.ID, job.ID)
		}
	}

	// Execute the job using the appropriate executor
	var execErr error
	var result interface{}
	var resultType domain.ResultType
	executor, ok := e.executors[job.Type]
	if !ok {
		execErr = fmt.Errorf("no executor found for payload type: %s", job.Type)
	} else {
		result, resultType, execErr = executor.Execute(job)
	}

	// Update the job run record
	if e.runRepo != nil && jobRun.ID != "" {
		if execErr != nil {
			jobRun = jobRun.WithFailed(execErr.Error())
		} else {
			jobRun = jobRun.WithCompleted(resultType, result)
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
