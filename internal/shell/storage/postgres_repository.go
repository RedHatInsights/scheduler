package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
)

type PostgresJobRepository struct {
	db *sql.DB
}

func NewPostgresJobRepository(cfg *config.Config) (*PostgresJobRepository, error) {

	connStr := buildConnectionString(cfg)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}

	repo := &PostgresJobRepository{db: db}

	log.Printf("[DEBUG] PostgresJobRepository - database initialized successfully")
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

	query := `
		INSERT INTO jobs (id, name, org_id, username, user_id, schedule, timezone, payload_type, payload_details, status, last_run_at, next_run_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
			COALESCE((SELECT created_at FROM jobs WHERE id = $1), CURRENT_TIMESTAMP), CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name, org_id = EXCLUDED.org_id, username = EXCLUDED.username, user_id = EXCLUDED.user_id,
			schedule = EXCLUDED.schedule, timezone = EXCLUDED.timezone, payload_type = EXCLUDED.payload_type, payload_details = EXCLUDED.payload_details,
			status = EXCLUDED.status, last_run_at = EXCLUDED.last_run_at, next_run_at = EXCLUDED.next_run_at, updated_at = CURRENT_TIMESTAMP`

	_, err = r.db.Exec(query, job.ID, job.Name, job.OrgID, job.Username, job.UserID,
		string(job.Schedule), job.Timezone, string(job.Type), string(payloadJSON), string(job.Status), lastRunAt, nextRunAt)
	return err
}

func (r *PostgresJobRepository) FindByID(id string) (domain.Job, error) {
	query := `SELECT id, name, org_id, username, user_id, schedule, timezone, payload_type, payload_details, status, last_run_at, next_run_at
		FROM jobs WHERE id = $1`

	var job domain.Job
	var payloadJSON string
	var lastRunAtStr, nextRunAtStr sql.NullString

	err := r.db.QueryRow(query, id).Scan(&job.ID, &job.Name, &job.OrgID, &job.Username, &job.UserID,
		&job.Schedule, &job.Timezone, &job.Type, &payloadJSON, &job.Status, &lastRunAtStr, &nextRunAtStr)

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
	return job, nil
}

func (r *PostgresJobRepository) FindAll() ([]domain.Job, error) {
	return r.queryJobs(`SELECT id, name, org_id, username, user_id, schedule, timezone, payload_type, payload_details, status, last_run_at, next_run_at
		FROM jobs ORDER BY created_at DESC`)
}

func (r *PostgresJobRepository) FindByOrgID(orgID string) ([]domain.Job, error) {
	return r.queryJobs(`SELECT id, name, org_id, username, user_id, schedule, timezone, payload_type, payload_details, status, last_run_at, next_run_at
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
	query := `SELECT id, name, org_id, username, user_id, schedule, timezone, payload_type, payload_details, status, last_run_at, next_run_at
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
		var lastRunAtStr, nextRunAtStr sql.NullString

		if err := rows.Scan(&job.ID, &job.Name, &job.OrgID, &job.Username, &job.UserID,
			&job.Schedule, &job.Timezone, &job.Type, &payloadJSON, &job.Status, &lastRunAtStr, &nextRunAtStr); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payloadJSON), &job.Payload); err != nil {
			log.Printf("ERROR: Failed to unmarshal payload for job %s: %v", job.ID, err)
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
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
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

func buildConnectionString(cfg *config.Config) string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Database.Host, cfg.Database.Port, cfg.Database.Username, cfg.Database.Password, cfg.Database.Name, cfg.Database.SSLMode)
}
