package usecases

import (
	"context"

	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/ports"
)

// SchedulerJobServiceAdapter adapts DefaultJobService to the SchedulerJobService interface.
// It provides system-level operations without authorization checks.
type SchedulerJobServiceAdapter struct {
	core *DefaultJobService
}

// NewSchedulerJobService creates a new SchedulerJobServiceAdapter
func NewSchedulerJobService(core *DefaultJobService) *SchedulerJobServiceAdapter {
	return &SchedulerJobServiceAdapter{core: core}
}

// Ensure it implements the interface
var _ ports.SchedulerJobService = (*SchedulerJobServiceAdapter)(nil)

func (s *SchedulerJobServiceAdapter) GetJob(ctx context.Context, id string) (domain.Job, error) {
	// Delegate to core service without authorization check
	return s.core.GetJob(ctx, id)
}

func (s *SchedulerJobServiceAdapter) ListJobs() ([]domain.Job, error) {
	// Delegate to core service to get all jobs
	return s.core.ListJobs()
}

func (s *SchedulerJobServiceAdapter) ExecuteScheduledJob(job domain.Job) error {
	// Delegate to core service to execute the job
	return s.core.ExecuteScheduledJob(job)
}
