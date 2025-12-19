package executor

import (
	"fmt"
	"log"
	"time"

	"insights-scheduler/internal/core/domain"
)

// LockManager defines the interface for distributed locking
type LockManager interface {
	TryAcquire(jobID string) (bool, error)
	Release(jobID string) error
	Extend(jobID string, additionalTTL time.Duration) error
	IsLocked(jobID string) (bool, error)
}

// DistributedJobExecutor wraps a JobExecutor with distributed locking
type DistributedJobExecutor struct {
	executor    *DefaultJobExecutor
	lockManager LockManager
	instanceID  string
}

// NewDistributedJobExecutor creates a new distributed job executor
func NewDistributedJobExecutor(executor *DefaultJobExecutor, lockManager LockManager, instanceID string) *DistributedJobExecutor {
	return &DistributedJobExecutor{
		executor:    executor,
		lockManager: lockManager,
		instanceID:  instanceID,
	}
}

// Execute executes a job with distributed locking to prevent duplicate execution
func (e *DistributedJobExecutor) Execute(job domain.Job) error {
	// Try to acquire lock for this job
	acquired, err := e.lockManager.TryAcquire(job.ID)
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	if !acquired {
		log.Printf("[%s] Job %s is already being executed by another instance, skipping", e.instanceID, job.ID)
		return nil // Not an error - another instance is handling it
	}

	log.Printf("[%s] Acquired lock for job %s", e.instanceID, job.ID)

	// Ensure lock is released even if execution panics
	defer func() {
		if err := e.lockManager.Release(job.ID); err != nil {
			log.Printf("[%s] Failed to release lock for job %s: %v", e.instanceID, job.ID, err)
		} else {
			log.Printf("[%s] Released lock for job %s", e.instanceID, job.ID)
		}
	}()

	// Execute the job
	return e.executor.Execute(job)
}
