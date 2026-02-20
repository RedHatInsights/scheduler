package storage

import (
	"os"
	"testing"
	"time"

	"insights-scheduler/internal/core/domain"
)

func TestSQLiteJobRepository(t *testing.T) {
	// Create temporary database
	dbPath := "./test_jobs_unit.db"
	defer os.Remove(dbPath) // Clean up

	// Create repository
	repo, err := NewSQLiteJobRepository(dbPath)
	if err != nil {
		t.Fatalf("Failed to create SQLite repository: %v", err)
	}
	defer repo.Close()

	// Test job creation
	payload := map[string]interface{}{
		"message": "Unit test job",
		"count":   42,
	}

	job := domain.NewJob("Unit Test Job", "test-org-123", "testuser", "test-user-id", "*/15 * * * *", "UTC", domain.PayloadMessage, payload)

	// Test Save
	if err := repo.Save(job); err != nil {
		t.Fatalf("Failed to save job: %v", err)
	}

	// Test FindByID
	retrievedJob, err := repo.FindByID(job.ID)
	if err != nil {
		t.Fatalf("Failed to find job by ID: %v", err)
	}

	if retrievedJob.ID != job.ID {
		t.Errorf("Expected job ID %s, got %s", job.ID, retrievedJob.ID)
	}

	if retrievedJob.Name != job.Name {
		t.Errorf("Expected job name %s, got %s", job.Name, retrievedJob.Name)
	}

	if retrievedJob.OrgID != job.OrgID {
		t.Errorf("Expected org_id %s, got %s", job.OrgID, retrievedJob.OrgID)
	}

	if retrievedJob.Username != job.Username {
		t.Errorf("Expected username %s, got %s", job.Username, retrievedJob.Username)
	}

	if retrievedJob.UserID != job.UserID {
		t.Errorf("Expected user_id %s, got %s", job.UserID, retrievedJob.UserID)
	}

	if retrievedJob.Schedule != job.Schedule {
		t.Errorf("Expected schedule %s, got %s", job.Schedule, retrievedJob.Schedule)
	}

	if retrievedJob.Status != job.Status {
		t.Errorf("Expected status %s, got %s", job.Status, retrievedJob.Status)
	}

	// Test type field
	if retrievedJob.Type != job.Type {
		t.Errorf("Expected type %s, got %s", job.Type, retrievedJob.Type)
	}

	// Cast payload to map and verify contents
	payloadMap, ok := retrievedJob.Payload.(map[string]interface{})
	if !ok {
		t.Errorf("Expected payload to be map[string]interface{}, got %T", retrievedJob.Payload)
	}

	if payloadMap["message"] != "Unit test job" {
		t.Errorf("Expected message 'Unit test job', got %v", payloadMap["message"])
	}

	if payloadMap["count"].(float64) != 42 {
		t.Errorf("Expected count 42, got %v", payloadMap["count"])
	}

	// Test FindAll
	jobs, err := repo.FindAll()
	if err != nil {
		t.Fatalf("Failed to find all jobs: %v", err)
	}

	if len(jobs) != 1 {
		t.Errorf("Expected 1 job, found %d", len(jobs))
	}

	// Test Update with LastRun and NextRunAt
	now := time.Now()
	nextRunAt := time.Now().Add(1 * time.Hour)
	updatedJob := job.WithStatus(domain.StatusRunning).WithLastRun(now).WithNextRunAt(nextRunAt)
	if err := repo.Save(updatedJob); err != nil {
		t.Fatalf("Failed to update job: %v", err)
	}

	retrievedUpdated, err := repo.FindByID(job.ID)
	if err != nil {
		t.Fatalf("Failed to find updated job: %v", err)
	}

	if retrievedUpdated.Status != domain.StatusRunning {
		t.Errorf("Expected status %s, got %s", domain.StatusRunning, retrievedUpdated.Status)
	}

	if retrievedUpdated.LastRun == nil {
		t.Error("Expected LastRun to be set")
	} else if retrievedUpdated.LastRun.Unix() != now.Unix() {
		t.Errorf("Expected LastRun %v, got %v", now, retrievedUpdated.LastRun)
	}

	if retrievedUpdated.NextRunAt == nil {
		t.Error("Expected NextRunAt to be set")
	} else if retrievedUpdated.NextRunAt.Unix() != nextRunAt.Unix() {
		t.Errorf("Expected NextRunAt %v, got %v", nextRunAt, retrievedUpdated.NextRunAt)
	}

	// Test Delete
	if err := repo.Delete(job.ID); err != nil {
		t.Fatalf("Failed to delete job: %v", err)
	}

	// Verify deletion
	_, err = repo.FindByID(job.ID)
	if err != domain.ErrJobNotFound {
		t.Errorf("Expected job not found error, got: %v", err)
	}

	// Test FindAll after deletion
	jobs, err = repo.FindAll()
	if err != nil {
		t.Fatalf("Failed to find all jobs after deletion: %v", err)
	}

	if len(jobs) != 0 {
		t.Errorf("Expected 0 jobs after deletion, found %d", len(jobs))
	}
}

func TestSQLiteJobRepository_FindByOrgID(t *testing.T) {
	// Create temporary database
	dbPath := "./test_jobs_findbyorg.db"
	defer os.Remove(dbPath) // Clean up

	// Create repository
	repo, err := NewSQLiteJobRepository(dbPath)
	if err != nil {
		t.Fatalf("Failed to create SQLite repository: %v", err)
	}
	defer repo.Close()

	// Create jobs for different orgs
	payload1 := map[string]interface{}{
		"message": "org1 job",
	}

	payload2 := map[string]interface{}{
		"command": "org2 job",
	}

	job1 := domain.NewJob("Org1 Job", "org-1", "user1", "user1-id", "*/30 * * * *", "UTC", domain.PayloadMessage, payload1)
	job2 := domain.NewJob("Org2 Job", "org-2", "user2", "user2-id", "0 * * * *", "UTC", domain.PayloadCommand, payload2)
	job3 := domain.NewJob("Another Org1 Job", "org-1", "user3", "user3-id", "0 12 * * *", "UTC", domain.PayloadMessage, payload1)

	// Save all jobs
	if err := repo.Save(job1); err != nil {
		t.Fatalf("Failed to save job1: %v", err)
	}
	if err := repo.Save(job2); err != nil {
		t.Fatalf("Failed to save job2: %v", err)
	}
	if err := repo.Save(job3); err != nil {
		t.Fatalf("Failed to save job3: %v", err)
	}

	// Test FindByOrgID for org-1
	org1Jobs, err := repo.FindByOrgID("org-1")
	if err != nil {
		t.Fatalf("Failed to find jobs for org-1: %v", err)
	}

	if len(org1Jobs) != 2 {
		t.Errorf("Expected 2 jobs for org-1, found %d", len(org1Jobs))
	}

	for _, job := range org1Jobs {
		if job.OrgID != "org-1" {
			t.Errorf("Expected org_id 'org-1', got %s", job.OrgID)
		}
	}

	// Test FindByOrgID for org-2
	org2Jobs, err := repo.FindByOrgID("org-2")
	if err != nil {
		t.Fatalf("Failed to find jobs for org-2: %v", err)
	}

	if len(org2Jobs) != 1 {
		t.Errorf("Expected 1 job for org-2, found %d", len(org2Jobs))
	}

	if org2Jobs[0].OrgID != "org-2" {
		t.Errorf("Expected org_id 'org-2', got %s", org2Jobs[0].OrgID)
	}

	// Test FindByOrgID for non-existent org
	noJobs, err := repo.FindByOrgID("non-existent-org")
	if err != nil {
		t.Fatalf("Failed to find jobs for non-existent org: %v", err)
	}

	if len(noJobs) != 0 {
		t.Errorf("Expected 0 jobs for non-existent org, found %d", len(noJobs))
	}
}

func TestSQLiteJobRepository_NotFound(t *testing.T) {
	// Create temporary database
	dbPath := "./test_jobs_notfound.db"
	defer os.Remove(dbPath) // Clean up

	// Create repository
	repo, err := NewSQLiteJobRepository(dbPath)
	if err != nil {
		t.Fatalf("Failed to create SQLite repository: %v", err)
	}
	defer repo.Close()

	// Test finding non-existent job
	_, err = repo.FindByID("non-existent-id")
	if err != domain.ErrJobNotFound {
		t.Errorf("Expected job not found error, got: %v", err)
	}

	// Test deleting non-existent job
	err = repo.Delete("non-existent-id")
	if err != domain.ErrJobNotFound {
		t.Errorf("Expected job not found error, got: %v", err)
	}
}
