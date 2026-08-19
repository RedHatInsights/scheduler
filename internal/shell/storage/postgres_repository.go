package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/lib/pq"
	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
)

type PostgresJobRepository struct {
	db     *sql.DB
	logger *slog.Logger
}

func NewPostgresJobRepository(cfg *config.Config, logger *slog.Logger) (*PostgresJobRepository, error) {

	connStr, err := buildConnectionString(cfg)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}

	repo := &PostgresJobRepository{
		db:     db,
		logger: logger,
	}

	logger.Info("PostgreSQL job repository initialized")
	return repo, nil
}

func (r *PostgresJobRepository) Save(job domain.Job) error {
	payloadJSON, err := json.Marshal(job.Payload)
	if err != nil {
		return err
	}

	var lastRunAt interface{}
	if job.LastRunAt != nil {
		lastRunAt = job.LastRunAt.Format(time.RFC3339)
	}

	var nextRunAt interface{}
	if job.NextRunAt != nil {
		nextRunAt = job.NextRunAt.Format(time.RFC3339)
	}

	var lastFailedAt interface{}
	if job.LastFailedAt != nil {
		lastFailedAt = job.LastFailedAt.Format(time.RFC3339)
	}

	query := `
		INSERT INTO jobs (id, name, org_id, user_id, schedule, timezone, payload_type, payload_details, status, last_run_at, next_run_at, consecutive_failures, last_failed_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
			COALESCE((SELECT created_at FROM jobs WHERE id = $1), CURRENT_TIMESTAMP), CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name, org_id = EXCLUDED.org_id, user_id = EXCLUDED.user_id,
			schedule = EXCLUDED.schedule, timezone = EXCLUDED.timezone, payload_type = EXCLUDED.payload_type, payload_details = EXCLUDED.payload_details,
			status = EXCLUDED.status, last_run_at = EXCLUDED.last_run_at, next_run_at = EXCLUDED.next_run_at,
			consecutive_failures = EXCLUDED.consecutive_failures, last_failed_at = EXCLUDED.last_failed_at, updated_at = CURRENT_TIMESTAMP`

	_, err = r.db.Exec(query, job.ID, job.Name, job.OrgID, job.UserID,
		string(job.Schedule), job.Timezone, string(job.Type), string(payloadJSON), string(job.Status), lastRunAt, nextRunAt, job.ConsecutiveFailures, lastFailedAt)
	return err
}

func (r *PostgresJobRepository) FindByID(id string) (domain.Job, error) {
	query := `SELECT id, name, org_id, user_id, schedule, timezone, payload_type, payload_details, status, last_run_at, next_run_at, consecutive_failures, last_failed_at
		FROM jobs WHERE id = $1`

	var job domain.Job
	var payloadJSON string
	var lastRunAtStr, nextRunAtStr, lastFailedAtStr sql.NullString

	err := r.db.QueryRow(query, id).Scan(&job.ID, &job.Name, &job.OrgID, &job.UserID,
		&job.Schedule, &job.Timezone, &job.Type, &payloadJSON, &job.Status, &lastRunAtStr, &nextRunAtStr, &job.ConsecutiveFailures, &lastFailedAtStr)

	if err == sql.ErrNoRows {
		return domain.Job{}, domain.ErrJobNotFound
	}
	if err != nil {
		return domain.Job{}, err
	}

	if err := json.Unmarshal([]byte(payloadJSON), &job.Payload); err != nil {
		return domain.Job{}, err
	}
	if lastRunAtStr.Valid {
		if t, err := time.Parse(time.RFC3339, lastRunAtStr.String); err == nil {
			job.LastRunAt = &t
		}
	}
	if nextRunAtStr.Valid {
		if t, err := time.Parse(time.RFC3339, nextRunAtStr.String); err == nil {
			job.NextRunAt = &t
		}
	}
	if lastFailedAtStr.Valid {
		if t, err := time.Parse(time.RFC3339, lastFailedAtStr.String); err == nil {
			job.LastFailedAt = &t
		}
	}
	return job, nil
}

func (r *PostgresJobRepository) FindAll() ([]domain.Job, error) {
	return r.queryJobs(`SELECT id, name, org_id, user_id, schedule, timezone, payload_type, payload_details, status, last_run_at, next_run_at, consecutive_failures, last_failed_at
		FROM jobs ORDER BY created_at DESC`)
}

func (r *PostgresJobRepository) FindByOrgID(orgID string) ([]domain.Job, error) {
	return r.queryJobs(`SELECT id, name, org_id, user_id, schedule, timezone, payload_type, payload_details, status, last_run_at, next_run_at, consecutive_failures, last_failed_at
	    FROM jobs WHERE org_id = $1 ORDER BY created_at DESC`, orgID)
}

func (r *PostgresJobRepository) FindByUserID(userID string, offset, limit int) ([]domain.Job, int, error) {
	// First get the total count
	var total int
	countQuery := `SELECT COUNT(*) FROM jobs WHERE user_id = $1`
	err := r.db.QueryRow(countQuery, userID).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Then get the paginated results
	query := `SELECT id, name, org_id, user_id, schedule, timezone, payload_type, payload_details, status, last_run_at, next_run_at, consecutive_failures, last_failed_at
	    FROM jobs WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`
	jobs, err := r.queryJobs(query, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	return jobs, total, nil
}

func (r *PostgresJobRepository) queryJobs(query string, args ...interface{}) ([]domain.Job, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []domain.Job
	for rows.Next() {
		var job domain.Job
		var payloadJSON string
		var lastRunAtStr, nextRunAtStr, lastFailedAtStr sql.NullString

		if err := rows.Scan(&job.ID, &job.Name, &job.OrgID, &job.UserID,
			&job.Schedule, &job.Timezone, &job.Type, &payloadJSON, &job.Status, &lastRunAtStr, &nextRunAtStr, &job.ConsecutiveFailures, &lastFailedAtStr); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payloadJSON), &job.Payload); err != nil {
			r.logger.Error("Failed to unmarshal job payload",
				slog.String("job_id", job.ID),
				slog.Any("error", err))
			job.Payload = nil // Include job with nil payload rather than silently dropping it
		}
		if lastRunAtStr.Valid {
			if t, err := time.Parse(time.RFC3339, lastRunAtStr.String); err == nil {
				job.LastRunAt = &t
			}
		}
		if nextRunAtStr.Valid {
			if t, err := time.Parse(time.RFC3339, nextRunAtStr.String); err == nil {
				job.NextRunAt = &t
			}
		}
		if lastFailedAtStr.Valid {
			if t, err := time.Parse(time.RFC3339, lastFailedAtStr.String); err == nil {
				job.LastFailedAt = &t
			}
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// FetchDueJobs atomically claims and returns jobs ready for execution
// Uses FOR UPDATE SKIP LOCKED to prevent duplicate execution across workers
// Calculates next_run_at from cron schedule BEFORE committing transaction
// This prevents race condition where another pod claims same jobs
func (r *PostgresJobRepository) FetchDueJobs(ctx context.Context, limit int, calculateNextRun func(schedule string, now time.Time) (time.Time, error)) ([]domain.Job, error) {
	query := `
		SELECT id, name, org_id, user_id, schedule, timezone,
		       payload_type, payload_details, status,
		       last_run_at, next_run_at, consecutive_failures, last_failed_at
		FROM jobs
		WHERE next_run_at <= NOW()
		  AND status = 'scheduled'
		ORDER BY next_run_at
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`

	// Start transaction to atomically fetch and claim jobs
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := time.Now()

	rows, err := tx.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type jobUpdate struct {
		job     domain.Job
		nextRun time.Time
	}

	var jobs []domain.Job
	var updates []jobUpdate

	for rows.Next() {
		var job domain.Job
		var payloadJSON string
		var lastRunAtStr, nextRunAtStr, lastFailedAtStr sql.NullString

		if err := rows.Scan(&job.ID, &job.Name, &job.OrgID, &job.UserID,
			&job.Schedule, &job.Timezone, &job.Type, &payloadJSON,
			&job.Status, &lastRunAtStr, &nextRunAtStr,
			&job.ConsecutiveFailures, &lastFailedAtStr); err != nil {
			return nil, err
		}

		if err := json.Unmarshal([]byte(payloadJSON), &job.Payload); err != nil {
			r.logger.Error("Failed to unmarshal job payload",
				slog.String("job_id", job.ID),
				slog.Any("error", err))
			job.Payload = nil
		}

		if lastRunAtStr.Valid {
			if t, err := time.Parse(time.RFC3339, lastRunAtStr.String); err == nil {
				job.LastRunAt = &t
			}
		}
		if nextRunAtStr.Valid {
			if t, err := time.Parse(time.RFC3339, nextRunAtStr.String); err == nil {
				job.NextRunAt = &t
			}
		}
		if lastFailedAtStr.Valid {
			if t, err := time.Parse(time.RFC3339, lastFailedAtStr.String); err == nil {
				job.LastFailedAt = &t
			}
		}

		// Calculate next_run_at from cron schedule
		nextRun, err := calculateNextRun(string(job.Schedule), now)
		if err != nil {
			r.logger.Error("Failed to calculate next run time",
				slog.String("job_id", job.ID),
				slog.String("schedule", string(job.Schedule)),
				slog.Any("error", err))
			continue // Skip jobs with invalid schedules
		}

		updates = append(updates, jobUpdate{job: job, nextRun: nextRun})
		jobs = append(jobs, job)
	}
	rows.Close()

	if len(updates) == 0 {
		return jobs, nil
	}

	// CRITICAL: Update next_run_at BEFORE committing transaction
	// This prevents another pod from claiming the same jobs
	updateQuery := `UPDATE jobs SET last_run_at = $1, next_run_at = $2 WHERE id = $3`
	for _, u := range updates {
		_, err := tx.Exec(updateQuery, now, u.nextRun, u.job.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to update job %s: %w", u.job.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return jobs, nil
}

func (r *PostgresJobRepository) Delete(id string) error {
	if _, err := r.FindByID(id); err != nil {
		return err
	}
	result, err := r.db.Exec(`DELETE FROM jobs WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return domain.ErrJobNotFound
	}
	return nil
}

func (r *PostgresJobRepository) Close() error {
	return r.db.Close()
}

func buildConnectionString(cfg *config.Config) (string, error) {
	sslSettings, err := buildPostgresSslConfigString(cfg)
	if err != nil {
		return "", err
	}

	databaseURL := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?%s&options=-ctimezone=UTC",
		cfg.Database.Username,
		cfg.Database.Password,
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.Name,
		sslSettings,
	)

	return databaseURL, nil
}

func buildPostgresSslConfigString(cfg *config.Config) (string, error) {
	if cfg.Database.SSLMode == "disable" {
		return "sslmode=disable", nil
	} else if cfg.Database.SSLMode == "verify-full" {
		return "sslmode=verify-full&sslrootcert=" + cfg.Database.SSLRootCert, nil
	} else {
		return "", errors.New("Invalid SSL configuration for database connection: " + cfg.Database.SSLMode)
	}
}
