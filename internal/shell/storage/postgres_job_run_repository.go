package storage

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
)

type PostgresJobRunRepository struct {
	db *sql.DB
}

func NewPostgresJobRunRepository(cfg *config.Config) (*PostgresJobRunRepository, error) {

	connStr := buildConnectionString(cfg)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	repo := &PostgresJobRunRepository{db: db}
	/*
		if err := repo.initSchema(); err != nil {
			return nil, fmt.Errorf("failed to initialize schema: %w", err)
		}
	*/

	log.Printf("[DEBUG] PostgresJobRunRepository - database initialized successfully")

	return repo, nil
}

/*
func (r *PostgresJobRunRepository) initSchema() error {
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
	`
	_, err := r.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}
	log.Println("[DEBUG] Job runs table schema initialized")
	return nil
}
*/

func (r *PostgresJobRunRepository) Save(run domain.JobRun) error {
	query := `
		INSERT INTO job_runs (id, job_id, status, start_time, end_time, error_message, result, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status, end_time = excluded.end_time,
			error_message = excluded.error_message, result = excluded.result`

	var endTime *string
	if run.EndTime != nil {
		s := run.EndTime.Format(time.RFC3339)
		endTime = &s
	}

	_, err := r.db.Exec(query, run.ID, run.JobID, run.Status, run.StartTime.Format(time.RFC3339),
		endTime, run.ErrorMessage, run.Result, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("failed to save job run: %w", err)
	}
	return nil
}

func (r *PostgresJobRunRepository) FindByID(id string) (domain.JobRun, error) {
	query := `SELECT id, job_id, status, start_time, end_time, error_message, result FROM job_runs WHERE id = $1`
	return r.scanRun(r.db.QueryRow(query, id))
}

func (r *PostgresJobRunRepository) FindByJobID(jobID string) ([]domain.JobRun, error) {
	return r.queryRuns(`SELECT id, job_id, status, start_time, end_time, error_message, result
		FROM job_runs WHERE job_id = $1 ORDER BY start_time DESC`, jobID)
}

func (r *PostgresJobRunRepository) FindByJobIDAndOrgID(jobID, orgID string) ([]domain.JobRun, error) {
	return r.queryRuns(`SELECT jr.id, jr.job_id, jr.status, jr.start_time, jr.end_time, jr.error_message, jr.result
		FROM job_runs jr INNER JOIN jobs j ON jr.job_id = j.id
		WHERE jr.job_id = $1 AND j.org_id = $2 ORDER BY jr.start_time DESC`, jobID, orgID)
}

func (r *PostgresJobRunRepository) FindAll() ([]domain.JobRun, error) {
	return r.queryRuns(`SELECT id, job_id, status, start_time, end_time, error_message, result
		FROM job_runs ORDER BY start_time DESC`)
}

func (r *PostgresJobRunRepository) scanRun(row *sql.Row) (domain.JobRun, error) {
	var run domain.JobRun
	var startTimeStr string
	var endTimeStr, errorMessage, result *string

	err := row.Scan(&run.ID, &run.JobID, &run.Status, &startTimeStr, &endTimeStr, &errorMessage, &result)
	if err == sql.ErrNoRows {
		return domain.JobRun{}, domain.ErrJobRunNotFound
	}
	if err != nil {
		return domain.JobRun{}, fmt.Errorf("failed to find job run: %w", err)
	}

	run.StartTime, _ = time.Parse(time.RFC3339, startTimeStr)
	if endTimeStr != nil {
		if t, err := time.Parse(time.RFC3339, *endTimeStr); err == nil {
			run.EndTime = &t
		}
	}
	run.ErrorMessage, run.Result = errorMessage, result
	return run, nil
}

func (r *PostgresJobRunRepository) queryRuns(query string, args ...interface{}) ([]domain.JobRun, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query job runs: %w", err)
	}
	defer rows.Close()

	var runs []domain.JobRun
	for rows.Next() {
		var run domain.JobRun
		var startTimeStr string
		var endTimeStr, errorMessage, result *string

		if err := rows.Scan(&run.ID, &run.JobID, &run.Status, &startTimeStr, &endTimeStr, &errorMessage, &result); err != nil {
			return nil, fmt.Errorf("failed to scan job run: %w", err)
		}
		run.StartTime, _ = time.Parse(time.RFC3339, startTimeStr)
		if endTimeStr != nil {
			if t, err := time.Parse(time.RFC3339, *endTimeStr); err == nil {
				run.EndTime = &t
			}
		}
		run.ErrorMessage, run.Result = errorMessage, result
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (r *PostgresJobRunRepository) Close() error {
	return r.db.Close()
}
