package storage

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"insights-scheduler/internal/core/domain"
)

type SQLiteJobRunRepository struct {
	db *sql.DB
}

func NewSQLiteJobRunRepository(dbPath string) (*SQLiteJobRunRepository, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	repo := &SQLiteJobRunRepository{db: db}

	// Initialize schema
	if err := repo.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return repo, nil
}

func (r *SQLiteJobRunRepository) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS job_runs (
		id TEXT PRIMARY KEY,
		job_id TEXT NOT NULL,
		status TEXT NOT NULL,
		start_time TEXT NOT NULL,
		end_time TEXT,
		error_message TEXT,
		result TEXT,
		created_at TEXT NOT NULL,
		FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_job_runs_job_id ON job_runs(job_id);
	CREATE INDEX IF NOT EXISTS idx_job_runs_status ON job_runs(status);
	CREATE INDEX IF NOT EXISTS idx_job_runs_start_time ON job_runs(start_time);
	CREATE INDEX IF NOT EXISTS idx_job_runs_created_at ON job_runs(created_at);
	`

	_, err := r.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	log.Println("[DEBUG] Job runs table schema initialized")
	return nil
}

func (r *SQLiteJobRunRepository) Save(run domain.JobRun) error {
	query := `
		INSERT INTO job_runs (id, job_id, status, start_time, end_time, error_message, result, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			end_time = excluded.end_time,
			error_message = excluded.error_message,
			result = excluded.result
	`

	var endTime *string
	if run.EndTime != nil {
		endTimeStr := run.EndTime.Format(time.RFC3339)
		endTime = &endTimeStr
	}

	_, err := r.db.Exec(
		query,
		run.ID,
		run.JobID,
		run.Status,
		run.StartTime.Format(time.RFC3339),
		endTime,
		run.ErrorMessage,
		run.Result,
		time.Now().UTC().Format(time.RFC3339),
	)

	if err != nil {
		return fmt.Errorf("failed to save job run: %w", err)
	}

	log.Printf("[DEBUG] SQLiteJobRunRepository - saved job run: id=%s, job_id=%s, status=%s", run.ID, run.JobID, run.Status)
	return nil
}

func (r *SQLiteJobRunRepository) FindByID(id string) (domain.JobRun, error) {
	query := `
		SELECT id, job_id, status, start_time, end_time, error_message, result
		FROM job_runs
		WHERE id = ?
	`

	var run domain.JobRun
	var startTimeStr string
	var endTimeStr *string
	var errorMessage *string
	var result *string

	err := r.db.QueryRow(query, id).Scan(
		&run.ID,
		&run.JobID,
		&run.Status,
		&startTimeStr,
		&endTimeStr,
		&errorMessage,
		&result,
	)

	if err == sql.ErrNoRows {
		return domain.JobRun{}, domain.ErrJobRunNotFound
	}
	if err != nil {
		return domain.JobRun{}, fmt.Errorf("failed to find job run: %w", err)
	}

	// Parse start time
	startTime, err := time.Parse(time.RFC3339, startTimeStr)
	if err != nil {
		return domain.JobRun{}, fmt.Errorf("failed to parse start time: %w", err)
	}
	run.StartTime = startTime

	// Parse end time if present
	if endTimeStr != nil {
		endTime, err := time.Parse(time.RFC3339, *endTimeStr)
		if err != nil {
			return domain.JobRun{}, fmt.Errorf("failed to parse end time: %w", err)
		}
		run.EndTime = &endTime
	}

	run.ErrorMessage = errorMessage
	run.Result = result

	return run, nil
}

func (r *SQLiteJobRunRepository) FindByJobID(jobID string) ([]domain.JobRun, error) {
	query := `
		SELECT id, job_id, status, start_time, end_time, error_message, result
		FROM job_runs
		WHERE job_id = ?
		ORDER BY start_time DESC
	`

	rows, err := r.db.Query(query, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to query job runs: %w", err)
	}
	defer rows.Close()

	runs := make([]domain.JobRun, 0)
	for rows.Next() {
		var run domain.JobRun
		var startTimeStr string
		var endTimeStr *string
		var errorMessage *string
		var result *string

		err := rows.Scan(
			&run.ID,
			&run.JobID,
			&run.Status,
			&startTimeStr,
			&endTimeStr,
			&errorMessage,
			&result,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job run: %w", err)
		}

		// Parse start time
		startTime, err := time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse start time: %w", err)
		}
		run.StartTime = startTime

		// Parse end time if present
		if endTimeStr != nil {
			endTime, err := time.Parse(time.RFC3339, *endTimeStr)
			if err != nil {
				return nil, fmt.Errorf("failed to parse end time: %w", err)
			}
			run.EndTime = &endTime
		}

		run.ErrorMessage = errorMessage
		run.Result = result

		runs = append(runs, run)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating job runs: %w", err)
	}

	return runs, nil
}

func (r *SQLiteJobRunRepository) FindByJobIDAndOrgID(jobID string, orgID string) ([]domain.JobRun, error) {
	query := `
		SELECT jr.id, jr.job_id, jr.status, jr.start_time, jr.end_time, jr.error_message, jr.result
		FROM job_runs jr
		INNER JOIN jobs j ON jr.job_id = j.id
		WHERE jr.job_id = ? AND j.org_id = ?
		ORDER BY jr.start_time DESC
	`

	rows, err := r.db.Query(query, jobID, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to query job runs: %w", err)
	}
	defer rows.Close()

	runs := make([]domain.JobRun, 0)
	for rows.Next() {
		var run domain.JobRun
		var startTimeStr string
		var endTimeStr *string
		var errorMessage *string
		var result *string

		err := rows.Scan(
			&run.ID,
			&run.JobID,
			&run.Status,
			&startTimeStr,
			&endTimeStr,
			&errorMessage,
			&result,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job run: %w", err)
		}

		// Parse start time
		startTime, err := time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse start time: %w", err)
		}
		run.StartTime = startTime

		// Parse end time if present
		if endTimeStr != nil {
			endTime, err := time.Parse(time.RFC3339, *endTimeStr)
			if err != nil {
				return nil, fmt.Errorf("failed to parse end time: %w", err)
			}
			run.EndTime = &endTime
		}

		run.ErrorMessage = errorMessage
		run.Result = result

		runs = append(runs, run)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating job runs: %w", err)
	}

	return runs, nil
}

func (r *SQLiteJobRunRepository) FindAll() ([]domain.JobRun, error) {
	query := `
		SELECT id, job_id, status, start_time, end_time, error_message, result
		FROM job_runs
		ORDER BY start_time DESC
	`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query job runs: %w", err)
	}
	defer rows.Close()

	var runs []domain.JobRun
	for rows.Next() {
		var run domain.JobRun
		var startTimeStr string
		var endTimeStr *string
		var errorMessage *string
		var result *string

		err := rows.Scan(
			&run.ID,
			&run.JobID,
			&run.Status,
			&startTimeStr,
			&endTimeStr,
			&errorMessage,
			&result,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job run: %w", err)
		}

		// Parse start time
		startTime, err := time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse start time: %w", err)
		}
		run.StartTime = startTime

		// Parse end time if present
		if endTimeStr != nil {
			endTime, err := time.Parse(time.RFC3339, *endTimeStr)
			if err != nil {
				return nil, fmt.Errorf("failed to parse end time: %w", err)
			}
			run.EndTime = &endTime
		}

		run.ErrorMessage = errorMessage
		run.Result = result

		runs = append(runs, run)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating job runs: %w", err)
	}

	return runs, nil
}

func (r *SQLiteJobRunRepository) Close() error {
	return r.db.Close()
}
