package usecases

import (
	"fmt"
	"log"

	"github.com/redhatinsights/platform-go-middlewares/v2/identity"

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

// GetJobRunWithUserCheck retrieves a job run only if it belongs to the specified user
func (s *JobRunService) GetJobRunWithUserCheck(runID string, ident identity.XRHID) (domain.JobRun, error) {
	run, err := s.runRepo.FindByID(runID)
	if err != nil {
		return domain.JobRun{}, err
	}

	// Get the parent job to check user_id
	job, err := s.jobRepo.FindByID(run.JobID)
	if err != nil {
		return domain.JobRun{}, err
	}

	userID := ident.Identity.User.UserID
	if job.UserID != userID {
		log.Printf("[DEBUG] JobRunService - user_id mismatch: run belongs to job with user_id=%s, requested user_id=%s", job.UserID, userID)
		return domain.JobRun{}, domain.ErrJobRunNotFound
	}

	return run, nil
}

// GetJobRuns retrieves all runs for a specific job
func (s *JobRunService) GetJobRuns(jobID string, offset, limit int) ([]domain.JobRun, int, error) {
	runs, total, err := s.runRepo.FindByJobID(jobID, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	return runs, total, nil
}

// GetJobRunsWithOrgCheck retrieves all runs for a job only if it belongs to the specified organization
func (s *JobRunService) GetJobRunsWithOrgCheck(jobID string, orgID string, offset, limit int) ([]domain.JobRun, int, error) {
	// First verify the job exists and belongs to this org
	job, err := s.jobRepo.FindByID(jobID)
	if err != nil {
		return nil, 0, err
	}

	if job.OrgID != orgID {
		log.Printf("[DEBUG] JobRunService - org_id mismatch: job belongs to org_id=%s, requested org_id=%s", job.OrgID, orgID)
		return nil, 0, domain.ErrJobNotFound
	}

	runs, total, err := s.runRepo.FindByJobID(jobID, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	return runs, total, nil
}

// GetJobRunsWithUserCheck retrieves all runs for a job only if it belongs to the specified user
func (s *JobRunService) GetJobRunsWithUserCheck(jobID string, ident identity.XRHID, offset, limit int) ([]domain.JobRun, int, error) {
	// First verify the job exists and belongs to this user
	job, err := s.jobRepo.FindByID(jobID)
	if err != nil {
		return nil, 0, err
	}

	userID := ident.Identity.User.UserID
	if job.UserID != userID {
		log.Printf("[DEBUG] JobRunService - user_id mismatch: job belongs to user_id=%s, requested user_id=%s", job.UserID, userID)
		return nil, 0, domain.ErrJobNotFound
	}

	runs, total, err := s.runRepo.FindByJobID(jobID, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	return runs, total, nil
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
