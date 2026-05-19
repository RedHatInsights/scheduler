package storage

import (
	"fmt"
	"os"
	"testing"
	"time"

	"insights-scheduler/internal/core/domain"
)

func TestSQLiteJobRunRepository_CleanupOldRuns(t *testing.T) {
	dbPath := "./test_cleanup.db"
	defer os.Remove(dbPath)

	// Create job repo first (needed for foreign key)
	jobRepo, err := NewSQLiteJobRepository(dbPath)
	if err != nil {
		t.Fatalf("Failed to create job repository: %v", err)
	}
	defer jobRepo.Close()

	runRepo, err := NewSQLiteJobRunRepository(dbPath)
	if err != nil {
		t.Fatalf("Failed to create job run repository: %v", err)
	}
	defer runRepo.Close()

	// Create two jobs
	job1 := domain.NewJob("Job 1", "org-1", "user-1", "*/5 * * * *", "UTC", domain.PayloadMessage, map[string]interface{}{"msg": "test"})
	job2 := domain.NewJob("Job 2", "org-1", "user-1", "*/10 * * * *", "UTC", domain.PayloadMessage, map[string]interface{}{"msg": "test"})

	if err := jobRepo.Save(job1); err != nil {
		t.Fatalf("Failed to save job1: %v", err)
	}
	if err := jobRepo.Save(job2); err != nil {
		t.Fatalf("Failed to save job2: %v", err)
	}

	// Create 5 runs for job1 with distinct start times
	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		run := domain.JobRun{
			ID:        fmt.Sprintf("run-j1-%d", i),
			JobID:     job1.ID,
			Status:    domain.RunStatusCompleted,
			StartTime: baseTime.Add(time.Duration(i) * time.Hour),
		}
		endTime := run.StartTime.Add(5 * time.Minute)
		run.EndTime = &endTime
		if err := runRepo.Save(run); err != nil {
			t.Fatalf("Failed to save run for job1: %v", err)
		}
	}

	// Create 3 runs for job2
	for i := 0; i < 3; i++ {
		run := domain.JobRun{
			ID:        fmt.Sprintf("run-j2-%d", i),
			JobID:     job2.ID,
			Status:    domain.RunStatusCompleted,
			StartTime: baseTime.Add(time.Duration(i) * time.Hour),
		}
		endTime := run.StartTime.Add(10 * time.Minute)
		run.EndTime = &endTime
		if err := runRepo.Save(run); err != nil {
			t.Fatalf("Failed to save run for job2: %v", err)
		}
	}

	// Verify initial counts
	allRuns, err := runRepo.FindAll()
	if err != nil {
		t.Fatalf("Failed to find all runs: %v", err)
	}
	if len(allRuns) != 8 {
		t.Fatalf("Expected 8 total runs, got %d", len(allRuns))
	}

	// Cleanup keeping 2 per job
	deleted, err := runRepo.CleanupOldRuns(2)
	if err != nil {
		t.Fatalf("CleanupOldRuns failed: %v", err)
	}

	// job1: 5 runs - keep 2 = delete 3
	// job2: 3 runs - keep 2 = delete 1
	// total deleted = 4
	if deleted != 4 {
		t.Errorf("Expected 4 deleted, got %d", deleted)
	}

	// Verify remaining runs
	allRuns, err = runRepo.FindAll()
	if err != nil {
		t.Fatalf("Failed to find all runs after cleanup: %v", err)
	}
	if len(allRuns) != 4 {
		t.Errorf("Expected 4 remaining runs, got %d", len(allRuns))
	}

	// Verify the most recent runs were kept for job1
	job1Runs, _, err := runRepo.FindByJobID(job1.ID, 0, 10)
	if err != nil {
		t.Fatalf("Failed to find job1 runs: %v", err)
	}
	if len(job1Runs) != 2 {
		t.Errorf("Expected 2 runs for job1, got %d", len(job1Runs))
	}
	// Runs are ordered by start_time DESC, so most recent first
	if job1Runs[0].ID != "run-j1-4" {
		t.Errorf("Expected most recent run run-j1-4, got %s", job1Runs[0].ID)
	}
	if job1Runs[1].ID != "run-j1-3" {
		t.Errorf("Expected second most recent run run-j1-3, got %s", job1Runs[1].ID)
	}

	// Verify the most recent runs were kept for job2
	job2Runs, _, err := runRepo.FindByJobID(job2.ID, 0, 10)
	if err != nil {
		t.Fatalf("Failed to find job2 runs: %v", err)
	}
	if len(job2Runs) != 2 {
		t.Errorf("Expected 2 runs for job2, got %d", len(job2Runs))
	}
}

func TestSQLiteJobRunRepository_CleanupOldRuns_NoRunsToDelete(t *testing.T) {
	dbPath := "./test_cleanup_noops.db"
	defer os.Remove(dbPath)

	jobRepo, err := NewSQLiteJobRepository(dbPath)
	if err != nil {
		t.Fatalf("Failed to create job repository: %v", err)
	}
	defer jobRepo.Close()

	runRepo, err := NewSQLiteJobRunRepository(dbPath)
	if err != nil {
		t.Fatalf("Failed to create job run repository: %v", err)
	}
	defer runRepo.Close()

	// Create a job with 2 runs
	job := domain.NewJob("Test Job", "org-1", "user-1", "*/5 * * * *", "UTC", domain.PayloadMessage, map[string]interface{}{"msg": "test"})
	if err := jobRepo.Save(job); err != nil {
		t.Fatalf("Failed to save job: %v", err)
	}

	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		run := domain.JobRun{
			ID:        fmt.Sprintf("run-%d", i),
			JobID:     job.ID,
			Status:    domain.RunStatusCompleted,
			StartTime: baseTime.Add(time.Duration(i) * time.Hour),
		}
		if err := runRepo.Save(run); err != nil {
			t.Fatalf("Failed to save run: %v", err)
		}
	}

	// Cleanup keeping 10 (more than exist)
	deleted, err := runRepo.CleanupOldRuns(10)
	if err != nil {
		t.Fatalf("CleanupOldRuns failed: %v", err)
	}
	if deleted != 0 {
		t.Errorf("Expected 0 deleted, got %d", deleted)
	}

	// Verify all runs still exist
	allRuns, err := runRepo.FindAll()
	if err != nil {
		t.Fatalf("Failed to find all runs: %v", err)
	}
	if len(allRuns) != 2 {
		t.Errorf("Expected 2 runs, got %d", len(allRuns))
	}
}

func TestSQLiteJobRunRepository_CleanupOldRuns_EmptyTable(t *testing.T) {
	dbPath := "./test_cleanup_empty.db"
	defer os.Remove(dbPath)

	runRepo, err := NewSQLiteJobRunRepository(dbPath)
	if err != nil {
		t.Fatalf("Failed to create job run repository: %v", err)
	}
	defer runRepo.Close()

	// Cleanup on empty table
	deleted, err := runRepo.CleanupOldRuns(10)
	if err != nil {
		t.Fatalf("CleanupOldRuns failed: %v", err)
	}
	if deleted != 0 {
		t.Errorf("Expected 0 deleted, got %d", deleted)
	}
}
