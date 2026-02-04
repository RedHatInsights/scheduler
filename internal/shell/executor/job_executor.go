package executor

import (
	"fmt"
	"log"

	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/usecases"
)

type DefaultJobExecutor struct {
	executors map[domain.PayloadType]JobExecutor
	runRepo   usecases.JobRunRepository
}

func NewJobExecutor(executors map[domain.PayloadType]JobExecutor, runRepo usecases.JobRunRepository) *DefaultJobExecutor {
	return &DefaultJobExecutor{
		executors: executors,
		runRepo:   runRepo,
	}
}

func (e *DefaultJobExecutor) Execute(job domain.Job) error {
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

	// Update the job run record
	if e.runRepo != nil && jobRun.ID != "" {
		if execErr != nil {
			jobRun = jobRun.WithFailed(execErr.Error())
		} else {
			result := fmt.Sprintf("Job %s completed successfully", job.Name)
			jobRun = jobRun.WithCompleted(result)
		}

		if err := e.runRepo.Save(jobRun); err != nil {
			log.Printf("Failed to update job run record: %v", err)
		} else {
			log.Printf("Updated job run: %s with status: %s", jobRun.ID, jobRun.Status)
		}
	}

	return execErr
}
