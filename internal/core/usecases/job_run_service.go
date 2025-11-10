package usecases

import (
	"fmt"
	"log"

	"insights-scheduler/internal/core/domain"
)

type JobRunService struct {
	runRepo JobRunRepository
	jobRepo JobRepository
}

func NewJobRunService(runRepo JobRunRepository, jobRepo JobRepository) *JobRunService {
	return &JobRunService{
		runRepo: runRepo,
		jobRepo: jobRepo,
	}
}

// GetJobRun retrieves a specific job run by ID
func (s *JobRunService) GetJobRun(runID string) (domain.JobRun, error) {
	run, err := s.runRepo.FindByID(runID)
	if err != nil {
		return domain.JobRun{}, err
	}
	return run, nil
}

// GetJobRunWithOrgCheck retrieves a job run only if it belongs to the specified organization
func (s *JobRunService) GetJobRunWithOrgCheck(runID string, orgID string) (domain.JobRun, error) {
	run, err := s.runRepo.FindByID(runID)
	if err != nil {
		return domain.JobRun{}, err
	}

	// Get the parent job to check org_id
	job, err := s.jobRepo.FindByID(run.JobID)
	if err != nil {
		return domain.JobRun{}, err
	}

	if job.OrgID != orgID {
		log.Printf("[DEBUG] JobRunService - org_id mismatch: run belongs to job with org_id=%s, requested org_id=%s", job.OrgID, orgID)
		return domain.JobRun{}, domain.ErrJobRunNotFound
	}

	return run, nil
}

// GetJobRuns retrieves all runs for a specific job
func (s *JobRunService) GetJobRuns(jobID string) ([]domain.JobRun, error) {
	runs, err := s.runRepo.FindByJobID(jobID)
	if err != nil {
		return nil, err
	}
	return runs, nil
}

// GetJobRunsWithOrgCheck retrieves all runs for a job only if it belongs to the specified organization
func (s *JobRunService) GetJobRunsWithOrgCheck(jobID string, orgID string) ([]domain.JobRun, error) {
	// First verify the job exists and belongs to this org
	job, err := s.jobRepo.FindByID(jobID)
	if err != nil {
		return nil, err
	}

	if job.OrgID != orgID {
		log.Printf("[DEBUG] JobRunService - org_id mismatch: job belongs to org_id=%s, requested org_id=%s", job.OrgID, orgID)
		return nil, domain.ErrJobNotFound
	}

	runs, err := s.runRepo.FindByJobID(jobID)
	if err != nil {
		return nil, err
	}
	return runs, nil
}

// CreateJobRun creates a new job run record
func (s *JobRunService) CreateJobRun(jobID string) (domain.JobRun, error) {
	// Verify the job exists
	_, err := s.jobRepo.FindByID(jobID)
	if err != nil {
		return domain.JobRun{}, fmt.Errorf("cannot create run for non-existent job: %w", err)
	}

	run := domain.NewJobRun(jobID)
	if err := s.runRepo.Save(run); err != nil {
		return domain.JobRun{}, err
	}

	log.Printf("[DEBUG] JobRunService - created job run: run_id=%s, job_id=%s", run.ID, run.JobID)
	return run, nil
}

// UpdateJobRun updates a job run record
func (s *JobRunService) UpdateJobRun(run domain.JobRun) error {
	// Verify the run exists
	_, err := s.runRepo.FindByID(run.ID)
	if err != nil {
		return err
	}

	if err := s.runRepo.Save(run); err != nil {
		return err
	}

	log.Printf("[DEBUG] JobRunService - updated job run: run_id=%s, status=%s", run.ID, run.Status)
	return nil
}
