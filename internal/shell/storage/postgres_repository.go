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
	/*
		if err := repo.initSchema(); err != nil {
			return nil, err
		}
	*/

	log.Printf("[DEBUG] PostgresJobRepository - database initialized successfully")
	return repo, nil
}

/*
func (r *PostgresJobRepository) initSchema() error {
	if err := r.runMigrations(); err != nil {
		return err
	}

	schema := `
CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    org_id TEXT NOT NULL,
    username TEXT NOT NULL,
    user_id TEXT NOT NULL,
    schedule TEXT NOT NULL,
    payload_type TEXT NOT NULL,
    payload_details TEXT NOT NULL,
    status TEXT NOT NULL,
    last_run TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_jobs_org_id ON jobs(org_id);
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_user_id ON jobs(user_id);
CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs(created_at);

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$ BEGIN NEW.updated_at = CURRENT_TIMESTAMP; RETURN NEW; END; $$ language 'plpgsql';

DROP TRIGGER IF EXISTS update_jobs_updated_at ON jobs;
CREATE TRIGGER update_jobs_updated_at BEFORE UPDATE ON jobs FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
`
	_, err := r.db.Exec(schema)
	return err
}

func (r *PostgresJobRepository) runMigrations() error {
	var tableExists bool
	r.db.QueryRow(`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'jobs')`).Scan(&tableExists)
	if !tableExists {
		return nil
	}

	migrations := []struct {
		column, defaultVal string
	}{
		{"org_id", "default-org"},
		{"username", "unknown"},
		{"user_id", "unknown-id"},
	}

	for _, m := range migrations {
		var exists bool
		r.db.QueryRow(`SELECT EXISTS (SELECT FROM information_schema.columns WHERE table_name = 'jobs' AND column_name = $1)`, m.column).Scan(&exists)
		if !exists {
			if _, err := r.db.Exec(`ALTER TABLE jobs ADD COLUMN ` + m.column + ` TEXT DEFAULT '` + m.defaultVal + `'`); err != nil {
				return err
			}
		}
	}
	return nil
}
*/

func (r *PostgresJobRepository) Save(job domain.Job) error {
	payloadJSON, err := json.Marshal(job.Payload)
	if err != nil {
		return err
	}

	var lastRun interface{}
	if job.LastRun != nil {
		lastRun = job.LastRun.Format(time.RFC3339)
	}

	query := `
		INSERT INTO jobs (id, name, org_id, username, user_id, schedule, payload_type, payload_details, status, last_run, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			COALESCE((SELECT created_at FROM jobs WHERE id = $1), CURRENT_TIMESTAMP), CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name, org_id = EXCLUDED.org_id, username = EXCLUDED.username, user_id = EXCLUDED.user_id,
			schedule = EXCLUDED.schedule, payload_type = EXCLUDED.payload_type, payload_details = EXCLUDED.payload_details,
			status = EXCLUDED.status, last_run = EXCLUDED.last_run, updated_at = CURRENT_TIMESTAMP`

	_, err = r.db.Exec(query, job.ID, job.Name, job.OrgID, job.Username, job.UserID,
		string(job.Schedule), string(job.Type), string(payloadJSON), string(job.Status), lastRun)
	return err
}

func (r *PostgresJobRepository) FindByID(id string) (domain.Job, error) {
	query := `SELECT id, name, org_id, username, user_id, schedule, payload_type, payload_details, status, last_run
		FROM jobs WHERE id = $1`

	var job domain.Job
	var payloadJSON string
	var lastRunStr sql.NullString

	err := r.db.QueryRow(query, id).Scan(&job.ID, &job.Name, &job.OrgID, &job.Username, &job.UserID,
		&job.Schedule, &job.Type, &payloadJSON, &job.Status, &lastRunStr)

	if err == sql.ErrNoRows {
		return domain.Job{}, domain.ErrJobNotFound
	}
	if err != nil {
		return domain.Job{}, err
	}

	if err := json.Unmarshal([]byte(payloadJSON), &job.Payload); err != nil {
		return domain.Job{}, err
	}
	if lastRunStr.Valid {
		if t, err := time.Parse(time.RFC3339, lastRunStr.String); err == nil {
			job.LastRun = &t
		}
	}
	return job, nil
}

func (r *PostgresJobRepository) FindAll() ([]domain.Job, error) {
	return r.queryJobs(`SELECT id, name, org_id, username, user_id, schedule, payload_type, payload_details, status, last_run
		FROM jobs ORDER BY created_at DESC`)
}

func (r *PostgresJobRepository) FindByOrgID(orgID string) ([]domain.Job, error) {
	return r.queryJobs(`SELECT id, name, org_id, username, user_id, schedule, payload_type, payload_details, status, last_run
	    FROM jobs WHERE org_id = $1 ORDER BY created_at DESC`, orgID)
}

func (r *PostgresJobRepository) FindByUserID(userID string) ([]domain.Job, error) {
	return r.queryJobs(`SELECT id, name, org_id, username, user_id, schedule, payload_type, payload_details, status, last_run
	    FROM jobs WHERE user_id = $1 ORDER BY created_at DESC`, userID)
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
		var lastRunStr sql.NullString

		if err := rows.Scan(&job.ID, &job.Name, &job.OrgID, &job.Username, &job.UserID,
			&job.Schedule, &job.Type, &payloadJSON, &job.Status, &lastRunStr); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payloadJSON), &job.Payload); err != nil {
			continue
		}
		if lastRunStr.Valid {
			if t, err := time.Parse(time.RFC3339, lastRunStr.String); err == nil {
				job.LastRun = &t
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
