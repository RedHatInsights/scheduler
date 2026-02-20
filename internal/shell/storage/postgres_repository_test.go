//go:build sql
// +build sql

package storage

import (
	"testing"
	"time"

	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
)

// These tests require a running PostgreSQL instance
// Run with: go test -v ./internal/shell/storage -run TestPostgres

func setupPostgresJobRepo(t *testing.T) *PostgresJobRepository {
	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("PostgreSQL test: configuration error: %v", err)
	}

	repo, err := NewPostgresJobRepository(cfg)
	if err != nil {
		t.Fatalf("PostgreSQL test: database not available: %v", err)
	}

	// Clean up any existing test data
	repo.db.Exec("DELETE FROM job_runs WHERE job_id LIKE 'test-%'")
	repo.db.Exec("DELETE FROM jobs WHERE id LIKE 'test-%'")

	return repo
}

func setupPostgresJobRunRepo(t *testing.T) *PostgresJobRunRepository {
	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("PostgreSQL test: configuration error: %v", err)
	}

	repo, err := NewPostgresJobRunRepository(cfg)
	if err != nil {
		t.Fatalf("PostgreSQL test: database not available: %v", err)
	}

	return repo
}

func TestPostgresJobRepository_BasicCRUD(t *testing.T) {
	repo := setupPostgresJobRepo(t)
	defer repo.Close()

	// Create test job
	payload := map[string]interface{}{
		"message": "Postgres test job",
		"count":   42,
	}

	job := domain.NewJob(
		"Postgres Test Job",
		"test-org-123",
		"testuser",
		"test-user-123",
		"*/15 * * * *",
		"UTC",
		domain.PayloadMessage,
		payload,
	)
	job.ID = "test-job-crud-1"

	// Test Save
	err := repo.Save(job)
	if err != nil {
		t.Fatalf("Failed to save job: %v", err)
	}

	// Test FindByID
	retrievedJob, err := repo.FindByID(job.ID)
	if err != nil {
		t.Fatalf("Failed to find job by ID: %v", err)
	}

	// Verify all fields
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
	if retrievedJob.Type != job.Type {
		t.Errorf("Expected type %s, got %s", job.Type, retrievedJob.Type)
	}
	if retrievedJob.Status != job.Status {
		t.Errorf("Expected status %s, got %s", job.Status, retrievedJob.Status)
	}

	// Test Update with LastRunAt and NextRunAt
	now := time.Now().UTC()
	nextRunAt := time.Now().UTC().Add(1 * time.Hour)
	updatedJob := retrievedJob.WithStatus(domain.StatusRunning).WithLastRunAt(now).WithNextRunAt(nextRunAt)

	err = repo.Save(updatedJob)
	if err != nil {
		t.Fatalf("Failed to update job: %v", err)
	}

	retrievedUpdated, err := repo.FindByID(job.ID)
	if err != nil {
		t.Fatalf("Failed to find updated job: %v", err)
	}

	if retrievedUpdated.Status != domain.StatusRunning {
		t.Errorf("Expected status %s, got %s", domain.StatusRunning, retrievedUpdated.Status)
	}

	if retrievedUpdated.LastRunAt == nil {
		t.Error("Expected LastRunAt to be set")
	} else {
		// Allow 1 second difference due to precision
		if retrievedUpdated.LastRunAt.Sub(now).Abs() > time.Second {
			t.Errorf("Expected LastRunAt near %v, got %v", now, *retrievedUpdated.LastRunAt)
		}
	}

	if retrievedUpdated.NextRunAt == nil {
		t.Error("Expected NextRunAt to be set")
	} else {
		// Allow 1 second difference due to precision
		if retrievedUpdated.NextRunAt.Sub(nextRunAt).Abs() > time.Second {
			t.Errorf("Expected NextRunAt near %v, got %v", nextRunAt, *retrievedUpdated.NextRunAt)
		}
	}

	// Test Delete
	err = repo.Delete(job.ID)
	if err != nil {
		t.Fatalf("Failed to delete job: %v", err)
	}

	// Verify deletion
	_, err = repo.FindByID(job.ID)
	if err != domain.ErrJobNotFound {
		t.Errorf("Expected ErrJobNotFound, got %v", err)
	}
}

func TestPostgresJobRepository_OrgIsolation(t *testing.T) {
	repo := setupPostgresJobRepo(t)
	defer repo.Close()

	// Create jobs for different orgs
	orgAJob1 := domain.NewJob("Org A Job 1", "org-a", "user1", "user-a-1", "0 * * * *", "UTC", domain.PayloadMessage, map[string]interface{}{})
	orgAJob1.ID = "test-job-org-a-1"

	orgAJob2 := domain.NewJob("Org A Job 2", "org-a", "user2", "user-a-2", "0 * * * *", "UTC", domain.PayloadMessage, map[string]interface{}{})
	orgAJob2.ID = "test-job-org-a-2"

	orgBJob1 := domain.NewJob("Org B Job 1", "org-b", "user3", "user-b-1", "0 * * * *", "UTC", domain.PayloadMessage, map[string]interface{}{})
	orgBJob1.ID = "test-job-org-b-1"

	orgBJob2 := domain.NewJob("Org B Job 2", "org-b", "user4", "user-b-2", "0 * * * *", "UTC", domain.PayloadMessage, map[string]interface{}{})
	orgBJob2.ID = "test-job-org-b-2"

	// Save all jobs
	jobs := []domain.Job{orgAJob1, orgAJob2, orgBJob1, orgBJob2}
	for _, job := range jobs {
		if err := repo.Save(job); err != nil {
			t.Fatalf("Failed to save job %s: %v", job.ID, err)
		}
	}

	// Test: Org A should only see its own jobs
	orgAJobs, err := repo.FindByOrgID("org-a")
	if err != nil {
		t.Fatalf("Failed to find jobs for org-a: %v", err)
	}

	if len(orgAJobs) != 2 {
		t.Errorf("Expected 2 jobs for org-a, got %d", len(orgAJobs))
	}

	for _, job := range orgAJobs {
		if job.OrgID != "org-a" {
			t.Errorf("Expected org_id 'org-a', got '%s' for job %s", job.OrgID, job.ID)
		}
	}

	// Test: Org B should only see its own jobs
	orgBJobs, err := repo.FindByOrgID("org-b")
	if err != nil {
		t.Fatalf("Failed to find jobs for org-b: %v", err)
	}

	if len(orgBJobs) != 2 {
		t.Errorf("Expected 2 jobs for org-b, got %d", len(orgBJobs))
	}

	for _, job := range orgBJobs {
		if job.OrgID != "org-b" {
			t.Errorf("Expected org_id 'org-b', got '%s' for job %s", job.OrgID, job.ID)
		}
	}

	// Test: Org C should see no jobs
	orgCJobs, err := repo.FindByOrgID("org-c")
	if err != nil {
		t.Fatalf("Failed to find jobs for org-c: %v", err)
	}

	if len(orgCJobs) != 0 {
		t.Errorf("Expected 0 jobs for org-c, got %d", len(orgCJobs))
	}

	// Cleanup
	for _, job := range jobs {
		repo.Delete(job.ID)
	}
}

func TestPostgresJobRepository_UserIsolation(t *testing.T) {
	repo := setupPostgresJobRepo(t)
	defer repo.Close()

	// Create jobs for different users (same org)
	user1Job1 := domain.NewJob("User 1 Job 1", "shared-org", "alice", "user-1", "0 * * * *", "UTC", domain.PayloadMessage, map[string]interface{}{})
	user1Job1.ID = "test-job-user-1-1"

	user1Job2 := domain.NewJob("User 1 Job 2", "shared-org", "alice", "user-1", "0 * * * *", "UTC", domain.PayloadMessage, map[string]interface{}{})
	user1Job2.ID = "test-job-user-1-2"

	user2Job1 := domain.NewJob("User 2 Job 1", "shared-org", "bob", "user-2", "0 * * * *", "UTC", domain.PayloadMessage, map[string]interface{}{})
	user2Job1.ID = "test-job-user-2-1"

	user2Job2 := domain.NewJob("User 2 Job 2", "shared-org", "bob", "user-2", "0 * * * *", "UTC", domain.PayloadMessage, map[string]interface{}{})
	user2Job2.ID = "test-job-user-2-2"

	// Save all jobs
	jobs := []domain.Job{user1Job1, user1Job2, user2Job1, user2Job2}
	for _, job := range jobs {
		if err := repo.Save(job); err != nil {
			t.Fatalf("Failed to save job %s: %v", job.ID, err)
		}
	}

	// Test: User 1 should only see their own jobs
	user1Jobs, total1, err := repo.FindByUserID("user-1", 0, 100)
	if err != nil {
		t.Fatalf("Failed to find jobs for user-1: %v", err)
	}

	if len(user1Jobs) != 2 {
		t.Errorf("Expected 2 jobs for user-1, got %d", len(user1Jobs))
	}

	if total1 != 2 {
		t.Errorf("Expected total count 2 for user-1, got %d", total1)
	}

	for _, job := range user1Jobs {
		if job.UserID != "user-1" {
			t.Errorf("Expected user_id 'user-1', got '%s' for job %s", job.UserID, job.ID)
		}
	}

	// Test: User 2 should only see their own jobs
	user2Jobs, total2, err := repo.FindByUserID("user-2", 0, 100)
	if err != nil {
		t.Fatalf("Failed to find jobs for user-2: %v", err)
	}

	if len(user2Jobs) != 2 {
		t.Errorf("Expected 2 jobs for user-2, got %d", len(user2Jobs))
	}

	if total2 != 2 {
		t.Errorf("Expected total count 2 for user-2, got %d", total2)
	}

	for _, job := range user2Jobs {
		if job.UserID != "user-2" {
			t.Errorf("Expected user_id 'user-2', got '%s' for job %s", job.UserID, job.ID)
		}
	}

	// Test: User 3 should see no jobs
	user3Jobs, total3, err := repo.FindByUserID("user-3", 0, 100)
	if err != nil {
		t.Fatalf("Failed to find jobs for user-3: %v", err)
	}

	if len(user3Jobs) != 0 {
		t.Errorf("Expected 0 jobs for user-3, got %d", len(user3Jobs))
	}

	if total3 != 0 {
		t.Errorf("Expected total count 0 for user-3, got %d", total3)
	}

	// Cleanup
	for _, job := range jobs {
		repo.Delete(job.ID)
	}
}

func TestPostgresJobRepository_CrossOrgAccess(t *testing.T) {
	repo := setupPostgresJobRepo(t)
	defer repo.Close()

	// Create job for org-sensitive
	sensitiveJob := domain.NewJob(
		"Sensitive Job",
		"org-sensitive",
		"admin",
		"admin-123",
		"0 0 * * *",
		"UTC",
		domain.PayloadExport,
		map[string]interface{}{"data": "confidential"},
	)
	sensitiveJob.ID = "test-job-sensitive-1"

	err := repo.Save(sensitiveJob)
	if err != nil {
		t.Fatalf("Failed to save sensitive job: %v", err)
	}

	// Test: Job can be found by ID (no org check in FindByID)
	foundJob, err := repo.FindByID(sensitiveJob.ID)
	if err != nil {
		t.Fatalf("Failed to find job by ID: %v", err)
	}

	if foundJob.OrgID != "org-sensitive" {
		t.Errorf("Expected org_id 'org-sensitive', got '%s'", foundJob.OrgID)
	}

	// Test: Job should NOT appear in different org's job list
	otherOrgJobs, err := repo.FindByOrgID("org-attacker")
	if err != nil {
		t.Fatalf("Failed to find jobs for org-attacker: %v", err)
	}

	for _, job := range otherOrgJobs {
		if job.ID == sensitiveJob.ID {
			t.Errorf("SECURITY VIOLATION: Sensitive job leaked to different org!")
		}
	}

	// Test: Job should only appear in correct org's job list
	correctOrgJobs, err := repo.FindByOrgID("org-sensitive")
	if err != nil {
		t.Fatalf("Failed to find jobs for org-sensitive: %v", err)
	}

	found := false
	for _, job := range correctOrgJobs {
		if job.ID == sensitiveJob.ID {
			found = true
			break
		}
	}

	if !found {
		t.Error("Sensitive job not found in correct org's job list")
	}

	// Cleanup
	repo.Delete(sensitiveJob.ID)
}

func TestPostgresJobRunRepository_BasicCRUD(t *testing.T) {
	jobRepo := setupPostgresJobRepo(t)
	defer jobRepo.Close()

	runRepo := setupPostgresJobRunRepo(t)
	defer runRepo.Close()

	// Create a job first
	job := domain.NewJob("Test Job for Runs", "test-org", "testuser", "test-user", "0 * * * *", "UTC", domain.PayloadMessage, map[string]interface{}{})
	job.ID = "test-job-runs-1"

	err := jobRepo.Save(job)
	if err != nil {
		t.Fatalf("Failed to save job: %v", err)
	}

	// Create job run
	startTime := time.Now().UTC()
	run := domain.JobRun{
		ID:        "test-run-1",
		JobID:     job.ID,
		Status:    domain.RunStatusRunning,
		StartTime: startTime,
	}

	// Test Save
	err = runRepo.Save(run)
	if err != nil {
		t.Fatalf("Failed to save job run: %v", err)
	}

	// Test FindByID
	retrievedRun, err := runRepo.FindByID(run.ID)
	if err != nil {
		t.Fatalf("Failed to find job run by ID: %v", err)
	}

	if retrievedRun.ID != run.ID {
		t.Errorf("Expected run ID %s, got %s", run.ID, retrievedRun.ID)
	}
	if retrievedRun.JobID != run.JobID {
		t.Errorf("Expected job ID %s, got %s", run.JobID, retrievedRun.JobID)
	}
	if retrievedRun.Status != run.Status {
		t.Errorf("Expected status %s, got %s", run.Status, retrievedRun.Status)
	}

	// Test Update (complete the run)
	endTime := time.Now().UTC()
	run.Status = domain.RunStatusCompleted
	run.EndTime = &endTime
	result := "Success"
	run.Result = &result

	err = runRepo.Save(run)
	if err != nil {
		t.Fatalf("Failed to update job run: %v", err)
	}

	retrievedUpdated, err := runRepo.FindByID(run.ID)
	if err != nil {
		t.Fatalf("Failed to find updated job run: %v", err)
	}

	if retrievedUpdated.Status != domain.RunStatusCompleted {
		t.Errorf("Expected status %s, got %s", domain.RunStatusCompleted, retrievedUpdated.Status)
	}
	if retrievedUpdated.EndTime == nil {
		t.Error("Expected EndTime to be set")
	}
	if retrievedUpdated.Result == nil || *retrievedUpdated.Result != "Success" {
		t.Error("Expected Result to be 'Success'")
	}

	// Cleanup
	jobRepo.Delete(job.ID)
}

func TestPostgresJobRunRepository_OrgIsolation(t *testing.T) {
	jobRepo := setupPostgresJobRepo(t)
	defer jobRepo.Close()

	runRepo := setupPostgresJobRunRepo(t)
	defer runRepo.Close()

	// Create jobs for different orgs
	orgAJob := domain.NewJob("Org A Job", "org-a", "user1", "user-a-1", "0 * * * *", "UTC", domain.PayloadMessage, map[string]interface{}{})
	orgAJob.ID = "test-job-run-org-a"

	orgBJob := domain.NewJob("Org B Job", "org-b", "user2", "user-b-1", "0 * * * *", "UTC", domain.PayloadMessage, map[string]interface{}{})
	orgBJob.ID = "test-job-run-org-b"

	jobRepo.Save(orgAJob)
	jobRepo.Save(orgBJob)

	// Create runs for each job
	orgARun := domain.JobRun{
		ID:        "test-run-org-a-1",
		JobID:     orgAJob.ID,
		Status:    domain.RunStatusCompleted,
		StartTime: time.Now().UTC(),
	}

	orgBRun := domain.JobRun{
		ID:        "test-run-org-b-1",
		JobID:     orgBJob.ID,
		Status:    domain.RunStatusCompleted,
		StartTime: time.Now().UTC(),
	}

	runRepo.Save(orgARun)
	runRepo.Save(orgBRun)

	// Test: Org A should only see runs for their jobs
	orgARuns, err := runRepo.FindByJobIDAndOrgID(orgAJob.ID, "org-a")
	if err != nil {
		t.Fatalf("Failed to find runs for org-a: %v", err)
	}

	if len(orgARuns) != 1 {
		t.Errorf("Expected 1 run for org-a, got %d", len(orgARuns))
	}

	if len(orgARuns) > 0 && orgARuns[0].JobID != orgAJob.ID {
		t.Errorf("Expected job ID %s, got %s", orgAJob.ID, orgARuns[0].JobID)
	}

	// Test: Org A should NOT see Org B's runs
	orgACantSeeBRuns, err := runRepo.FindByJobIDAndOrgID(orgBJob.ID, "org-a")
	if err != nil {
		t.Fatalf("Failed to query runs: %v", err)
	}

	if len(orgACantSeeBRuns) != 0 {
		t.Errorf("SECURITY VIOLATION: Org A can see Org B's runs! Got %d runs", len(orgACantSeeBRuns))
	}

	// Test: Org B should only see runs for their jobs
	orgBRuns, err := runRepo.FindByJobIDAndOrgID(orgBJob.ID, "org-b")
	if err != nil {
		t.Fatalf("Failed to find runs for org-b: %v", err)
	}

	if len(orgBRuns) != 1 {
		t.Errorf("Expected 1 run for org-b, got %d", len(orgBRuns))
	}

	// Cleanup
	jobRepo.Delete(orgAJob.ID)
	jobRepo.Delete(orgBJob.ID)
}

func TestPostgresJobRunRepository_MultipleRunsPerJob(t *testing.T) {
	jobRepo := setupPostgresJobRepo(t)
	defer jobRepo.Close()

	runRepo := setupPostgresJobRunRepo(t)
	defer runRepo.Close()

	// Create a job
	job := domain.NewJob("Multi-Run Job", "test-org", "testuser", "test-user", "0 * * * *", "UTC", domain.PayloadMessage, map[string]interface{}{})
	job.ID = "test-job-multi-runs"

	err := jobRepo.Save(job)
	if err != nil {
		t.Fatalf("Failed to save job: %v", err)
	}

	// Create multiple runs
	runs := []domain.JobRun{
		{
			ID:        "test-run-multi-1",
			JobID:     job.ID,
			Status:    domain.RunStatusCompleted,
			StartTime: time.Now().UTC().Add(-2 * time.Hour),
		},
		{
			ID:        "test-run-multi-2",
			JobID:     job.ID,
			Status:    domain.RunStatusCompleted,
			StartTime: time.Now().UTC().Add(-1 * time.Hour),
		},
		{
			ID:        "test-run-multi-3",
			JobID:     job.ID,
			Status:    domain.RunStatusCompleted,
			StartTime: time.Now().UTC(),
		},
	}

	for _, run := range runs {
		if err := runRepo.Save(run); err != nil {
			t.Fatalf("Failed to save run %s: %v", run.ID, err)
		}
	}

	// Test: FindByJobID should return all runs for the job
	jobRuns, total, err := runRepo.FindByJobID(job.ID, 0, 100)
	if err != nil {
		t.Fatalf("Failed to find runs by job ID: %v", err)
	}

	if total != 3 {
		t.Errorf("Expected total count 3, got %d", total)
	}

	if len(jobRuns) != 3 {
		t.Errorf("Expected 3 runs, got %d", len(jobRuns))
	}

	// Verify runs are sorted by start_time DESC (newest first)
	if len(jobRuns) == 3 {
		if jobRuns[0].ID != "test-run-multi-3" {
			t.Errorf("Expected newest run first, got %s", jobRuns[0].ID)
		}
		if jobRuns[2].ID != "test-run-multi-1" {
			t.Errorf("Expected oldest run last, got %s", jobRuns[2].ID)
		}
	}

	// Cleanup
	jobRepo.Delete(job.ID)
}
func TestPostgresJobRepository_UpdatedAtColumn(t *testing.T) {
	repo := setupPostgresJobRepo(t)
	defer repo.Close()

	// Create a test job
	payload := map[string]interface{}{
		"message": "Test updated_at tracking",
	}

	job := domain.NewJob(
		"Updated At Test Job",
		"test-org-updated-at",
		"testuser",
		"test-user-updated-at",
		"*/30 * * * *",
		"UTC",
		domain.PayloadMessage,
		payload,
	)
	job.ID = "test-job-updated-at"

	// Save the job initially
	err := repo.Save(job)
	if err != nil {
		t.Fatalf("Failed to save job: %v", err)
	}

	// Query the initial created_at and updated_at timestamps
	var initialCreatedAt, initialUpdatedAt time.Time
	query := `SELECT created_at, updated_at FROM jobs WHERE id = $1`
	err = repo.db.QueryRow(query, job.ID).Scan(&initialCreatedAt, &initialUpdatedAt)
	if err != nil {
		t.Fatalf("Failed to query timestamps: %v", err)
	}

	// Verify created_at and updated_at are set
	if initialCreatedAt.IsZero() {
		t.Error("created_at should not be zero")
	}
	if initialUpdatedAt.IsZero() {
		t.Error("updated_at should not be zero")
	}

	// On initial insert, created_at and updated_at should be approximately equal
	timeDiff := initialUpdatedAt.Sub(initialCreatedAt).Abs()
	if timeDiff > time.Second {
		t.Errorf("created_at and updated_at should be nearly equal on insert, diff: %v", timeDiff)
	}

	// Wait to ensure timestamp difference
	time.Sleep(2 * time.Second)

	// Update the job
	updatedJob := job.WithStatus(domain.StatusPaused)
	err = repo.Save(updatedJob)
	if err != nil {
		t.Fatalf("Failed to update job: %v", err)
	}

	// Query the timestamps again
	var finalCreatedAt, finalUpdatedAt time.Time
	err = repo.db.QueryRow(query, job.ID).Scan(&finalCreatedAt, &finalUpdatedAt)
	if err != nil {
		t.Fatalf("Failed to query updated timestamps: %v", err)
	}

	// Verify created_at hasn't changed
	if !finalCreatedAt.Equal(initialCreatedAt) {
		t.Errorf("created_at should not change on update. Initial: %v, Final: %v",
			initialCreatedAt, finalCreatedAt)
	}

	// Verify updated_at has changed and is after the initial value
	if !finalUpdatedAt.After(initialUpdatedAt) {
		t.Errorf("updated_at should be after initial value. Initial: %v, Final: %v",
			initialUpdatedAt, finalUpdatedAt)
	}

	// Verify updated_at changed by at least 1 second (we waited 2 seconds)
	updateDiff := finalUpdatedAt.Sub(initialUpdatedAt)
	if updateDiff < time.Second {
		t.Errorf("updated_at should have changed by at least 1 second, got: %v", updateDiff)
	}

	// Cleanup
	repo.Delete(job.ID)
}

func TestPostgresJobRepository_UpdatedAtMultipleUpdates(t *testing.T) {
	repo := setupPostgresJobRepo(t)
	defer repo.Close()

	// Create a test job
	job := domain.NewJob(
		"Multi Update Test",
		"test-org",
		"testuser",
		"test-user",
		"0 * * * *",
		"UTC",
		domain.PayloadMessage,
		map[string]interface{}{"test": "data"},
	)
	job.ID = "test-job-multi-update"

	// Save initial job
	if err := repo.Save(job); err != nil {
		t.Fatalf("Failed to save job: %v", err)
	}

	query := `SELECT updated_at FROM jobs WHERE id = $1`
	timestamps := make([]time.Time, 0, 3)

	// Capture initial updated_at
	var ts time.Time
	repo.db.QueryRow(query, job.ID).Scan(&ts)
	timestamps = append(timestamps, ts)

	// Perform multiple updates with delays
	for i := 0; i < 2; i++ {
		time.Sleep(time.Second)

		// Update job with different status
		var newStatus domain.JobStatus
		if i == 0 {
			newStatus = domain.StatusRunning
		} else {
			newStatus = domain.StatusScheduled
		}

		updatedJob := job.WithStatus(newStatus)
		if err := repo.Save(updatedJob); err != nil {
			t.Fatalf("Failed to update job on iteration %d: %v", i, err)
		}

		// Capture updated_at
		repo.db.QueryRow(query, job.ID).Scan(&ts)
		timestamps = append(timestamps, ts)
	}

	// Verify all timestamps are different and monotonically increasing
	for i := 1; i < len(timestamps); i++ {
		if !timestamps[i].After(timestamps[i-1]) {
			t.Errorf("Timestamp %d should be after timestamp %d. Got %v <= %v",
				i, i-1, timestamps[i], timestamps[i-1])
		}
	}

	// Cleanup
	repo.Delete(job.ID)
}

func TestPostgresJobRepository_TimezoneAndTimestamps(t *testing.T) {
	repo := setupPostgresJobRepo(t)
	defer repo.Close()

	// Create a job with a non-UTC timezone
	payload := map[string]interface{}{
		"message": "Test timezone and timestamps",
	}

	job := domain.NewJob(
		"Timezone Test Job",
		"test-org-tz",
		"testuser",
		"test-user-tz",
		"0 9 * * *",
		"America/New_York",
		domain.PayloadMessage,
		payload,
	)
	job.ID = "test-job-timezone-1"

	// Set last_run_at and next_run_at
	lastRunAt := time.Date(2026, 2, 20, 14, 0, 0, 0, time.UTC) // 9 AM EST
	nextRunAt := time.Date(2026, 2, 21, 14, 0, 0, 0, time.UTC) // 9 AM EST next day
	job = job.WithLastRunAt(lastRunAt).WithNextRunAt(nextRunAt)

	// Save the job
	err := repo.Save(job)
	if err != nil {
		t.Fatalf("Failed to save job: %v", err)
	}

	// Retrieve the job
	retrievedJob, err := repo.FindByID(job.ID)
	if err != nil {
		t.Fatalf("Failed to find job by ID: %v", err)
	}

	// Verify timezone is preserved
	if retrievedJob.Timezone != "America/New_York" {
		t.Errorf("Expected timezone 'America/New_York', got '%s'", retrievedJob.Timezone)
	}

	// Verify last_run_at is preserved
	if retrievedJob.LastRunAt == nil {
		t.Error("Expected LastRunAt to be set, got nil")
	} else {
		// Allow 1 second difference due to precision
		if retrievedJob.LastRunAt.Sub(lastRunAt).Abs() > time.Second {
			t.Errorf("Expected LastRunAt %v, got %v", lastRunAt, *retrievedJob.LastRunAt)
		}
	}

	// Verify next_run_at is preserved
	if retrievedJob.NextRunAt == nil {
		t.Error("Expected NextRunAt to be set, got nil")
	} else {
		// Allow 1 second difference due to precision
		if retrievedJob.NextRunAt.Sub(nextRunAt).Abs() > time.Second {
			t.Errorf("Expected NextRunAt %v, got %v", nextRunAt, *retrievedJob.NextRunAt)
		}
	}

	// Update only next_run_at (simulating job execution scheduling)
	newNextRunAt := time.Date(2026, 2, 22, 14, 0, 0, 0, time.UTC)
	updatedJob := retrievedJob.WithNextRunAt(newNextRunAt)

	err = repo.Save(updatedJob)
	if err != nil {
		t.Fatalf("Failed to update job: %v", err)
	}

	// Retrieve and verify update
	retrievedUpdated, err := repo.FindByID(job.ID)
	if err != nil {
		t.Fatalf("Failed to find updated job: %v", err)
	}

	// Verify timezone unchanged
	if retrievedUpdated.Timezone != "America/New_York" {
		t.Errorf("Timezone should not change on update, got '%s'", retrievedUpdated.Timezone)
	}

	// Verify last_run_at unchanged
	if retrievedUpdated.LastRunAt == nil {
		t.Error("LastRunAt should still be set after update")
	} else {
		if retrievedUpdated.LastRunAt.Sub(lastRunAt).Abs() > time.Second {
			t.Errorf("LastRunAt should not change, expected %v, got %v", lastRunAt, *retrievedUpdated.LastRunAt)
		}
	}

	// Verify next_run_at updated
	if retrievedUpdated.NextRunAt == nil {
		t.Error("NextRunAt should be set after update")
	} else {
		if retrievedUpdated.NextRunAt.Sub(newNextRunAt).Abs() > time.Second {
			t.Errorf("Expected NextRunAt %v, got %v", newNextRunAt, *retrievedUpdated.NextRunAt)
		}
	}

	// Cleanup
	repo.Delete(job.ID)
}

func TestPostgresJobRepository_NullTimestamps(t *testing.T) {
	repo := setupPostgresJobRepo(t)
	defer repo.Close()

	// Create a job without setting last_run_at or next_run_at
	job := domain.NewJob(
		"Null Timestamps Test",
		"test-org",
		"testuser",
		"test-user",
		"0 * * * *",
		"UTC",
		domain.PayloadMessage,
		map[string]interface{}{"test": "data"},
	)
	job.ID = "test-job-null-timestamps"

	// Save the job
	err := repo.Save(job)
	if err != nil {
		t.Fatalf("Failed to save job: %v", err)
	}

	// Retrieve the job
	retrievedJob, err := repo.FindByID(job.ID)
	if err != nil {
		t.Fatalf("Failed to find job by ID: %v", err)
	}

	// Verify both timestamps are nil
	if retrievedJob.LastRunAt != nil {
		t.Errorf("Expected LastRunAt to be nil, got %v", *retrievedJob.LastRunAt)
	}

	if retrievedJob.NextRunAt != nil {
		t.Errorf("Expected NextRunAt to be nil, got %v", *retrievedJob.NextRunAt)
	}

	// Now set only next_run_at
	nextRunAt := time.Now().UTC().Add(1 * time.Hour)
	updatedJob := retrievedJob.WithNextRunAt(nextRunAt)

	err = repo.Save(updatedJob)
	if err != nil {
		t.Fatalf("Failed to update job: %v", err)
	}

	// Retrieve and verify
	retrievedUpdated, err := repo.FindByID(job.ID)
	if err != nil {
		t.Fatalf("Failed to find updated job: %v", err)
	}

	// Verify last_run_at is still nil
	if retrievedUpdated.LastRunAt != nil {
		t.Errorf("Expected LastRunAt to still be nil, got %v", *retrievedUpdated.LastRunAt)
	}

	// Verify next_run_at is now set
	if retrievedUpdated.NextRunAt == nil {
		t.Error("Expected NextRunAt to be set")
	} else {
		if retrievedUpdated.NextRunAt.Sub(nextRunAt).Abs() > time.Second {
			t.Errorf("Expected NextRunAt %v, got %v", nextRunAt, *retrievedUpdated.NextRunAt)
		}
	}

	// Cleanup
	repo.Delete(job.ID)
}
