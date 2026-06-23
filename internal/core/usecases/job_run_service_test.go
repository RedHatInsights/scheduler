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
	findByUserIDFunc    func(userID string, offset, limit int) ([]domain.JobRun, int, error)
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

func (m *mockJobRunRepository) FindByUserID(userID string, offset, limit int) ([]domain.JobRun, int, error) {
	if m.findByUserIDFunc != nil {
		return m.findByUserIDFunc(userID, offset, limit)
	}
	return nil, 0, nil
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

func (m *mockJobRunRepository) CleanupOldRuns(keepPerJob int) (int64, error) {
	return 0, nil
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

func TestGetAllRunsForUser_Success(t *testing.T) {
	userID := "user-123"
	expectedRuns := []domain.JobRun{
		{ID: "run-1", JobID: "job-1"},
		{ID: "run-2", JobID: "job-1"},
		{ID: "run-3", JobID: "job-2"},
	}

	mockRunRepo := &mockJobRunRepository{
		findByUserIDFunc: func(uid string, offset, limit int) ([]domain.JobRun, int, error) {
			if uid != userID {
				t.Errorf("Expected user ID %s, got %s", userID, uid)
			}
			if offset != 0 {
				t.Errorf("Expected offset 0, got %d", offset)
			}
			if limit != 10 {
				t.Errorf("Expected limit 10, got %d", limit)
			}
			return expectedRuns, len(expectedRuns), nil
		},
	}

	mockJobRepo := &mockJobRepositoryForRuns{}

	service := NewJobRunService(mockRunRepo, mockJobRepo)
	ident := testIdentity(userID)

	runs, total, err := service.GetAllRunsForUser(ident, 0, 10)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if total != len(expectedRuns) {
		t.Errorf("Expected total %d, got %d", len(expectedRuns), total)
	}

	if len(runs) != len(expectedRuns) {
		t.Errorf("Expected %d runs, got %d", len(expectedRuns), len(runs))
	}

	for i, run := range runs {
		if run.ID != expectedRuns[i].ID {
			t.Errorf("Expected run ID %s at index %d, got %s", expectedRuns[i].ID, i, run.ID)
		}
	}
}

func TestGetAllRunsForUser_EmptyResult(t *testing.T) {
	userID := "user-with-no-runs"

	mockRunRepo := &mockJobRunRepository{
		findByUserIDFunc: func(uid string, offset, limit int) ([]domain.JobRun, int, error) {
			if uid != userID {
				t.Errorf("Expected user ID %s, got %s", userID, uid)
			}
			return []domain.JobRun{}, 0, nil
		},
	}

	mockJobRepo := &mockJobRepositoryForRuns{}

	service := NewJobRunService(mockRunRepo, mockJobRepo)
	ident := testIdentity(userID)

	runs, total, err := service.GetAllRunsForUser(ident, 0, 10)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if total != 0 {
		t.Errorf("Expected total 0, got %d", total)
	}

	if len(runs) != 0 {
		t.Errorf("Expected 0 runs, got %d", len(runs))
	}
}

func TestGetAllRunsForUser_Pagination(t *testing.T) {
	userID := "user-456"
	offset := 20
	limit := 5

	mockRunRepo := &mockJobRunRepository{
		findByUserIDFunc: func(uid string, off, lim int) ([]domain.JobRun, int, error) {
			if uid != userID {
				t.Errorf("Expected user ID %s, got %s", userID, uid)
			}
			if off != offset {
				t.Errorf("Expected offset %d, got %d", offset, off)
			}
			if lim != limit {
				t.Errorf("Expected limit %d, got %d", limit, lim)
			}
			// Simulate paginated results
			return []domain.JobRun{
				{ID: "run-21", JobID: "job-1"},
				{ID: "run-22", JobID: "job-2"},
			}, 50, nil // Total of 50 runs
		},
	}

	mockJobRepo := &mockJobRepositoryForRuns{}

	service := NewJobRunService(mockRunRepo, mockJobRepo)
	ident := testIdentity(userID)

	runs, total, err := service.GetAllRunsForUser(ident, offset, limit)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if total != 50 {
		t.Errorf("Expected total 50, got %d", total)
	}

	if len(runs) != 2 {
		t.Errorf("Expected 2 runs in page, got %d", len(runs))
	}
}
