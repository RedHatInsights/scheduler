package storage

import (
	"context"
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
		INSERT INTO job_runs (id, job_id, status, start_time, end_time, error_message, result_type, result, external_job_id, external_service, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status, end_time = excluded.end_time,
			error_message = excluded.error_message, result_type = excluded.result_type,
			result = excluded.result, external_job_id = excluded.external_job_id,
			external_service = excluded.external_service`

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
		endTime, run.ErrorMessage, resultType, resultJSON, run.ExternalJobID, run.ExternalService,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("failed to save job run: %w", err)
	}
	return nil
}

func (r *PostgresJobRunRepository) FindByID(id string) (domain.JobRun, error) {
	query := `SELECT jr.id, jr.job_id, jr.status, jr.start_time, jr.end_time, jr.error_message, jr.result_type, jr.result, jr.external_job_id, jr.external_service, j.name
		FROM job_runs jr INNER JOIN jobs j ON jr.job_id = j.id WHERE jr.id = $1`
	return r.scanRun(r.db.QueryRow(query, id))
}

func (r *PostgresJobRunRepository) FindByJobID(jobID string, sort domain.SortSpec, offset, limit int) ([]domain.JobRun, int, error) {
	// First get the total count
	var total int
	countQuery := `SELECT COUNT(*) FROM job_runs WHERE job_id = $1`
	err := r.db.QueryRow(countQuery, jobID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count job runs: %w", err)
	}

	// Then get the paginated results. The jobs join carries j.name onto each run;
	// because job_runs and jobs share a "status" column, the query aliases
	// job_runs as "jr" and the ORDER BY column is qualified accordingly. ORDER BY
	// is built from allowlisted column literals only (see buildOrderByClause).
	orderBy := buildOrderByClause(sort, jobRunSortColumns, "start_time", "jr")
	query := fmt.Sprintf(`SELECT jr.id, jr.job_id, jr.status, jr.start_time, jr.end_time, jr.error_message, jr.result_type, jr.result, jr.external_job_id, jr.external_service, j.name
		FROM job_runs jr INNER JOIN jobs j ON jr.job_id = j.id
		WHERE jr.job_id = $1 %s LIMIT $2 OFFSET $3`, orderBy)
	runs, err := r.queryRuns(query, jobID, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	return runs, total, nil
}

func (r *PostgresJobRunRepository) FindByJobIDAndOrgID(jobID, orgID string) ([]domain.JobRun, error) {
	return r.queryRuns(`SELECT jr.id, jr.job_id, jr.status, jr.start_time, jr.end_time, jr.error_message, jr.result_type, jr.result, jr.external_job_id, jr.external_service, j.name
		FROM job_runs jr INNER JOIN jobs j ON jr.job_id = j.id
		WHERE jr.job_id = $1 AND j.org_id = $2 ORDER BY jr.start_time DESC`, jobID, orgID)
}

func (r *PostgresJobRunRepository) FindByUserID(userID string, sort domain.SortSpec, offset, limit int) ([]domain.JobRun, int, error) {
	// First get the total count
	var total int
	countQuery := `SELECT COUNT(*) FROM job_runs jr
		INNER JOIN jobs j ON jr.job_id = j.id
		WHERE j.user_id = $1`
	err := r.db.QueryRow(countQuery, userID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count job runs for user: %w", err)
	}

	// Then get the paginated results. The joined query aliases job_runs as "jr",
	// so the ORDER BY column is qualified with that alias. Columns come from the
	// allowlist only (see buildOrderByClause).
	orderBy := buildOrderByClause(sort, jobRunSortColumns, "start_time", "jr")
	query := fmt.Sprintf(`SELECT jr.id, jr.job_id, jr.status, jr.start_time, jr.end_time, jr.error_message, jr.result_type, jr.result, jr.external_job_id, jr.external_service, j.name
		FROM job_runs jr
		INNER JOIN jobs j ON jr.job_id = j.id
		WHERE j.user_id = $1
		%s
		LIMIT $2 OFFSET $3`, orderBy)
	runs, err := r.queryRuns(query, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	return runs, total, nil
}

func (r *PostgresJobRunRepository) FindAll() ([]domain.JobRun, error) {
	return r.queryRuns(`SELECT jr.id, jr.job_id, jr.status, jr.start_time, jr.end_time, jr.error_message, jr.result_type, jr.result, jr.external_job_id, jr.external_service, j.name
		FROM job_runs jr INNER JOIN jobs j ON jr.job_id = j.id ORDER BY jr.start_time DESC`)
}

// FindInFlightExternalRuns returns running job runs that were handed off to an
// external service (i.e. have an external_job_id) and therefore need polling.
// The WHERE clause matches idx_job_runs_status_external_job_id so the scan is
// index-backed rather than fetching every running run and filtering in Go.
//
// This is a hot path (polled on an interval) and its caller does not use
// JobRun.JobName, so it deliberately avoids the jobs join and uses the
// job_name-free scanner.
func (r *PostgresJobRunRepository) FindInFlightExternalRuns(ctx context.Context) ([]domain.JobRun, error) {
	return r.queryRunsWithoutJobName(`SELECT id, job_id, status, start_time, end_time, error_message, result_type, result, external_job_id, external_service
		FROM job_runs WHERE status = 'running' AND external_job_id IS NOT NULL ORDER BY start_time ASC`)
}

// hydrateRun fills the parsed/derived fields of a run from the raw string
// columns shared by every job_run read query, so the single-row and multi-row
// scanners (with or without the joined jobs.name) don't duplicate this logic.
func hydrateRun(run *domain.JobRun, startTimeStr string, endTimeStr, errorMessage, resultType, result *string) error {
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
			return fmt.Errorf("failed to unmarshal result: %w", err)
		}
		run.Result = resultData
	}

	return nil
}

func (r *PostgresJobRunRepository) scanRun(row *sql.Row) (domain.JobRun, error) {
	var run domain.JobRun
	var startTimeStr, jobName string
	var endTimeStr, errorMessage, resultType, result *string

	err := row.Scan(&run.ID, &run.JobID, &run.Status, &startTimeStr, &endTimeStr, &errorMessage, &resultType, &result, &run.ExternalJobID, &run.ExternalService, &jobName)
	if err == sql.ErrNoRows {
		return domain.JobRun{}, domain.ErrJobRunNotFound
	}
	if err != nil {
		return domain.JobRun{}, fmt.Errorf("failed to find job run: %w", err)
	}

	run.JobName = jobName
	if err := hydrateRun(&run, startTimeStr, endTimeStr, errorMessage, resultType, result); err != nil {
		return domain.JobRun{}, err
	}
	return run, nil
}

// queryRuns scans job_run rows that include the joined jobs.name column (the API
// read paths). The query's SELECT must end with j.name.
func (r *PostgresJobRunRepository) queryRuns(query string, args ...interface{}) ([]domain.JobRun, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query job runs: %w", err)
	}
	defer rows.Close()

	var runs []domain.JobRun
	for rows.Next() {
		var run domain.JobRun
		var startTimeStr, jobName string
		var endTimeStr, errorMessage, resultType, result *string

		if err := rows.Scan(&run.ID, &run.JobID, &run.Status, &startTimeStr, &endTimeStr, &errorMessage, &resultType, &result, &run.ExternalJobID, &run.ExternalService, &jobName); err != nil {
			return nil, fmt.Errorf("failed to scan job run: %w", err)
		}
		run.JobName = jobName
		if err := hydrateRun(&run, startTimeStr, endTimeStr, errorMessage, resultType, result); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// queryRunsWithoutJobName scans job_run rows that do NOT include the joined
// jobs.name column. It lets hot, internal callers (e.g. the export poller) skip
// the jobs join entirely, since they don't use JobRun.JobName.
func (r *PostgresJobRunRepository) queryRunsWithoutJobName(query string, args ...interface{}) ([]domain.JobRun, error) {
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

		if err := rows.Scan(&run.ID, &run.JobID, &run.Status, &startTimeStr, &endTimeStr, &errorMessage, &resultType, &result, &run.ExternalJobID, &run.ExternalService); err != nil {
			return nil, fmt.Errorf("failed to scan job run: %w", err)
		}
		if err := hydrateRun(&run, startTimeStr, endTimeStr, errorMessage, resultType, result); err != nil {
			return nil, err
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
