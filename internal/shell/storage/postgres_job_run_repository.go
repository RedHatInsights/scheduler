package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/lib/pq"
	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
)

type PostgresJobRunRepository struct {
	db     *sql.DB
	logger *slog.Logger
}

func NewPostgresJobRunRepository(cfg *config.Config, logger *slog.Logger) (*PostgresJobRunRepository, error) {

	connStr, err := buildConnectionString(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build database connection string: %w", err)
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	repo := &PostgresJobRunRepository{
		db:     db,
		logger: logger,
	}

	logger.Info("PostgreSQL job run repository initialized")

	return repo, nil
}

func (r *PostgresJobRunRepository) Save(run domain.JobRun) error {
	query := `
		INSERT INTO job_runs (id, job_id, status, start_time, end_time, error_message, result_type, result_json, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status, end_time = excluded.end_time,
			error_message = excluded.error_message, result_type = excluded.result_type,
			result_json = excluded.result_json`

	var endTime *string
	if run.EndTime != nil {
		s := run.EndTime.Format(time.RFC3339)
		endTime = &s
	}

	// Marshal Result to JSON if present
	var resultJSON *string
	var resultType *string
	if run.Result != nil {
		data, err := json.Marshal(run.Result)
		if err != nil {
			return fmt.Errorf("failed to marshal result: %w", err)
		}
		str := string(data)
		resultJSON = &str

		if run.ResultType != nil {
			rt := string(*run.ResultType)
			resultType = &rt
		}
	}

	_, err := r.db.Exec(query, run.ID, run.JobID, run.Status, run.StartTime.Format(time.RFC3339),
		endTime, run.ErrorMessage, resultType, resultJSON, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("failed to save job run: %w", err)
	}
	return nil
}

func (r *PostgresJobRunRepository) FindByID(id string) (domain.JobRun, error) {
	query := `SELECT id, job_id, status, start_time, end_time, error_message, result_type, result_json FROM job_runs WHERE id = $1`
	return r.scanRun(r.db.QueryRow(query, id))
}

func (r *PostgresJobRunRepository) FindByJobID(jobID string, offset, limit int) ([]domain.JobRun, int, error) {
	// First get the total count
	var total int
	countQuery := `SELECT COUNT(*) FROM job_runs WHERE job_id = $1`
	err := r.db.QueryRow(countQuery, jobID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count job runs: %w", err)
	}

	// Then get the paginated results
	query := `SELECT id, job_id, status, start_time, end_time, error_message, result_type, result_json
		FROM job_runs WHERE job_id = $1 ORDER BY start_time DESC LIMIT $2 OFFSET $3`
	runs, err := r.queryRuns(query, jobID, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	return runs, total, nil
}

func (r *PostgresJobRunRepository) FindByJobIDAndOrgID(jobID, orgID string) ([]domain.JobRun, error) {
	return r.queryRuns(`SELECT jr.id, jr.job_id, jr.status, jr.start_time, jr.end_time, jr.error_message, jr.result_type, jr.result_json
		FROM job_runs jr INNER JOIN jobs j ON jr.job_id = j.id
		WHERE jr.job_id = $1 AND j.org_id = $2 ORDER BY jr.start_time DESC`, jobID, orgID)
}

func (r *PostgresJobRunRepository) FindByUserID(userID string, offset, limit int) ([]domain.JobRun, int, error) {
	// First get the total count
	var total int
	countQuery := `SELECT COUNT(*) FROM job_runs jr
		INNER JOIN jobs j ON jr.job_id = j.id
		WHERE j.user_id = $1`
	err := r.db.QueryRow(countQuery, userID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count job runs for user: %w", err)
	}

	// Then get the paginated results
	query := `SELECT jr.id, jr.job_id, jr.status, jr.start_time, jr.end_time, jr.error_message, jr.result_type, jr.result_json
		FROM job_runs jr
		INNER JOIN jobs j ON jr.job_id = j.id
		WHERE j.user_id = $1
		ORDER BY jr.start_time DESC
		LIMIT $2 OFFSET $3`
	runs, err := r.queryRuns(query, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	return runs, total, nil
}

func (r *PostgresJobRunRepository) FindAll() ([]domain.JobRun, error) {
	return r.queryRuns(`SELECT id, job_id, status, start_time, end_time, error_message, result_type, result_json
		FROM job_runs ORDER BY start_time DESC`)
}

func (r *PostgresJobRunRepository) scanRun(row *sql.Row) (domain.JobRun, error) {
	var run domain.JobRun
	var startTimeStr string
	var endTimeStr, errorMessage, resultType, result *string

	err := row.Scan(&run.ID, &run.JobID, &run.Status, &startTimeStr, &endTimeStr, &errorMessage, &resultType, &result)
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
	run.ErrorMessage = errorMessage

	// Parse result_type
	if resultType != nil {
		rt := domain.ResultType(*resultType)
		run.ResultType = &rt
	}

	// Unmarshal Result from JSON if present
	if result != nil {
		var resultData interface{}
		if err := json.Unmarshal([]byte(*result), &resultData); err != nil {
			return domain.JobRun{}, fmt.Errorf("failed to unmarshal result: %w", err)
		}
		run.Result = resultData
	}

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
		var endTimeStr, errorMessage, resultType, result *string

		if err := rows.Scan(&run.ID, &run.JobID, &run.Status, &startTimeStr, &endTimeStr, &errorMessage, &resultType, &result); err != nil {
			return nil, fmt.Errorf("failed to scan job run: %w", err)
		}
		run.StartTime, _ = time.Parse(time.RFC3339, startTimeStr)
		if endTimeStr != nil {
			if t, err := time.Parse(time.RFC3339, *endTimeStr); err == nil {
				run.EndTime = &t
			}
		}
		run.ErrorMessage = errorMessage

		// Parse result_type
		if resultType != nil {
			rt := domain.ResultType(*resultType)
			run.ResultType = &rt
		}

		// Unmarshal Result from JSON if present
		if result != nil {
			var resultData interface{}
			if err := json.Unmarshal([]byte(*result), &resultData); err != nil {
				return nil, fmt.Errorf("failed to unmarshal result: %w", err)
			}
			run.Result = resultData
		}

		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (r *PostgresJobRunRepository) CleanupOldRuns(keepPerJob int) (int64, error) {
	query := `
		DELETE FROM job_runs
		WHERE id IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (PARTITION BY job_id ORDER BY start_time DESC) AS rn
				FROM job_runs
			) ranked
			WHERE rn > $1
		)`

	result, err := r.db.Exec(query, keepPerJob)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup old job runs: %w", err)
	}

	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return deleted, nil
}

func (r *PostgresJobRunRepository) Close() error {
	return r.db.Close()
}
