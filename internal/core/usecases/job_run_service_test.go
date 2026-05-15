package usecases

import (
	"testing"

	"github.com/redhatinsights/platform-go-middlewares/v2/identity"

	"insights-scheduler/internal/core/domain"
)

// Mock repositories for testing
type mockJobRunRepository struct {
	findByIDFunc        func(id string) (domain.JobRun, error)
	findByJobIDFunc     func(jobID string, offset, limit int) ([]domain.JobRun, int, error)
	findByJobIDAndOrgID func(jobID string, orgID string) ([]domain.JobRun, error)
	findAllFunc         func() ([]domain.JobRun, error)
	saveFunc            func(run domain.JobRun) error
}

func (m *mockJobRunRepository) FindByID(id string) (domain.JobRun, error) {
	if m.findByIDFunc != nil {
		return m.findByIDFunc(id)
	}
	return domain.JobRun{}, domain.ErrJobRunNotFound
}

func (m *mockJobRunRepository) FindByJobID(jobID string, offset, limit int) ([]domain.JobRun, int, error) {
	if m.findByJobIDFunc != nil {
		return m.findByJobIDFunc(jobID, offset, limit)
	}
	return nil, 0, nil
}

func (m *mockJobRunRepository) FindByJobIDAndOrgID(jobID string, orgID string) ([]domain.JobRun, error) {
	if m.findByJobIDAndOrgID != nil {
		return m.findByJobIDAndOrgID(jobID, orgID)
	}
	return nil, nil
}

func (m *mockJobRunRepository) FindAll() ([]domain.JobRun, error) {
	if m.findAllFunc != nil {
		return m.findAllFunc()
	}
	return nil, nil
}

func (m *mockJobRunRepository) Save(run domain.JobRun) error {
	if m.saveFunc != nil {
		return m.saveFunc(run)
	}
	return nil
}

type mockJobRepositoryForRuns struct {
	findByIDFunc func(id string) (domain.Job, error)
}

func (m *mockJobRepositoryForRuns) FindByID(id string) (domain.Job, error) {
	if m.findByIDFunc != nil {
		return m.findByIDFunc(id)
	}
	return domain.Job{}, domain.ErrJobNotFound
}

func (m *mockJobRepositoryForRuns) FindAll() ([]domain.Job, error) {
	return nil, nil
}

func (m *mockJobRepositoryForRuns) FindByOrgID(orgID string) ([]domain.Job, error) {
	return nil, nil
}

func (m *mockJobRepositoryForRuns) FindByUserID(userID string, offset, limit int) ([]domain.Job, int, error) {
	return nil, 0, nil
}

func (m *mockJobRepositoryForRuns) Save(job domain.Job) error {
	return nil
}

func (m *mockJobRepositoryForRuns) Delete(id string) error {
	return nil
}

// Test helper to create a test identity
func testIdentity(userID string) identity.XRHID {
	return identity.XRHID{
		Identity: identity.Identity{
			OrgID: "org-123",
			User: &identity.User{
				Username: "testuser",
				UserID:   userID,
			},
		},
	}
}

func TestGetJobRunWithUserCheck_Success(t *testing.T) {
	runID := "run-123"
	jobID := "job-456"
	userID := "user-789"

	mockRunRepo := &mockJobRunRepository{
		findByIDFunc: func(id string) (domain.JobRun, error) {
			if id != runID {
				t.Errorf("Expected run ID %s, got %s", runID, id)
			}
			return domain.JobRun{
				ID:    runID,
				JobID: jobID,
			}, nil
		},
	}

	mockJobRepo := &mockJobRepositoryForRuns{
		findByIDFunc: func(id string) (domain.Job, error) {
			if id != jobID {
				t.Errorf("Expected job ID %s, got %s", jobID, id)
			}
			return domain.Job{
				ID:     jobID,
				UserID: userID,
			}, nil
		},
	}

	service := NewJobRunService(mockRunRepo, mockJobRepo)
	ident := testIdentity(userID)

	run, err := service.GetJobRunWithUserCheck(runID, ident)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if run.ID != runID {
		t.Errorf("Expected run ID %s, got %s", runID, run.ID)
	}
}

func TestGetJobRunWithUserCheck_UserMismatch(t *testing.T) {
	runID := "run-123"
	jobID := "job-456"
	jobUserID := "user-789"
	requestUserID := "user-999"

	mockRunRepo := &mockJobRunRepository{
		findByIDFunc: func(id string) (domain.JobRun, error) {
			return domain.JobRun{
				ID:    runID,
				JobID: jobID,
			}, nil
		},
	}

	mockJobRepo := &mockJobRepositoryForRuns{
		findByIDFunc: func(id string) (domain.Job, error) {
			return domain.Job{
				ID:     jobID,
				UserID: jobUserID,
			}, nil
		},
	}

	service := NewJobRunService(mockRunRepo, mockJobRepo)
	ident := testIdentity(requestUserID)

	_, err := service.GetJobRunWithUserCheck(runID, ident)
	if err != domain.ErrJobRunNotFound {
		t.Errorf("Expected ErrJobRunNotFound, got %v", err)
	}
}

func TestGetJobRunsWithUserCheck_Success(t *testing.T) {
	jobID := "job-456"
	userID := "user-789"
	expectedRuns := []domain.JobRun{
		{ID: "run-1", JobID: jobID},
		{ID: "run-2", JobID: jobID},
	}

	mockRunRepo := &mockJobRunRepository{
		findByJobIDFunc: func(id string, offset, limit int) ([]domain.JobRun, int, error) {
			if id != jobID {
				t.Errorf("Expected job ID %s, got %s", jobID, id)
			}
			return expectedRuns, len(expectedRuns), nil
		},
	}

	mockJobRepo := &mockJobRepositoryForRuns{
		findByIDFunc: func(id string) (domain.Job, error) {
			if id != jobID {
				t.Errorf("Expected job ID %s, got %s", jobID, id)
			}
			return domain.Job{
				ID:     jobID,
				UserID: userID,
			}, nil
		},
	}

	service := NewJobRunService(mockRunRepo, mockJobRepo)
	ident := testIdentity(userID)

	runs, total, err := service.GetJobRunsWithUserCheck(jobID, ident, 0, 10)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if total != len(expectedRuns) {
		t.Errorf("Expected total %d, got %d", len(expectedRuns), total)
	}

	if len(runs) != len(expectedRuns) {
		t.Errorf("Expected %d runs, got %d", len(expectedRuns), len(runs))
	}
}

func TestGetJobRunsWithUserCheck_UserMismatch(t *testing.T) {
	jobID := "job-456"
	jobUserID := "user-789"
	requestUserID := "user-999"

	mockRunRepo := &mockJobRunRepository{}

	mockJobRepo := &mockJobRepositoryForRuns{
		findByIDFunc: func(id string) (domain.Job, error) {
			return domain.Job{
				ID:     jobID,
				UserID: jobUserID,
			}, nil
		},
	}

	service := NewJobRunService(mockRunRepo, mockJobRepo)
	ident := testIdentity(requestUserID)

	_, _, err := service.GetJobRunsWithUserCheck(jobID, ident, 0, 10)
	if err != domain.ErrJobNotFound {
		t.Errorf("Expected ErrJobNotFound, got %v", err)
	}
}

func TestGetJobRunsWithUserCheck_JobNotFound(t *testing.T) {
	jobID := "nonexistent-job"
	userID := "user-789"

	mockRunRepo := &mockJobRunRepository{}

	mockJobRepo := &mockJobRepositoryForRuns{
		findByIDFunc: func(id string) (domain.Job, error) {
			return domain.Job{}, domain.ErrJobNotFound
		},
	}

	service := NewJobRunService(mockRunRepo, mockJobRepo)
	ident := testIdentity(userID)

	_, _, err := service.GetJobRunsWithUserCheck(jobID, ident, 0, 10)
	if err != domain.ErrJobNotFound {
		t.Errorf("Expected ErrJobNotFound, got %v", err)
	}
}

func TestGetJobRunWithUserCheck_RunNotFound(t *testing.T) {
	runID := "nonexistent-run"
	userID := "user-789"

	mockRunRepo := &mockJobRunRepository{
		findByIDFunc: func(id string) (domain.JobRun, error) {
			return domain.JobRun{}, domain.ErrJobRunNotFound
		},
	}

	mockJobRepo := &mockJobRepositoryForRuns{}

	service := NewJobRunService(mockRunRepo, mockJobRepo)
	ident := testIdentity(userID)

	_, err := service.GetJobRunWithUserCheck(runID, ident)
	if err != domain.ErrJobRunNotFound {
		t.Errorf("Expected ErrJobRunNotFound, got %v", err)
	}
}
