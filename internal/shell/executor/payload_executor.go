package executor

import (
	"insights-scheduler/internal/core/domain"
)

// JobExecutor defines the interface for executing different job payload types
type JobExecutor interface {
	Execute(job domain.Job) error
}
