package storage

import (
	"database/sql"
	"encoding/json"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"insights-scheduler-part-2/internal/core/domain"
)

type SQLiteJobRepository struct {
	db *sql.DB
}

func NewSQLiteJobRepository(dbPath string) (*SQLiteJobRepository, error) {
	log.Printf("[DEBUG] SQLiteJobRepository - opening database: %s", dbPath)

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, err
	}

	repo := &SQLiteJobRepository{db: db}

	// Initialize schema
	if err := repo.initSchema(); err != nil {
		return nil, err
	}

	log.Printf("[DEBUG] SQLiteJobRepository - database initialized successfully")
	return repo, nil
}

func (r *SQLiteJobRepository) initSchema() error {
	log.Printf("[DEBUG] SQLiteJobRepository - initializing schema")

	// First, check if we need to migrate existing data
	if err := r.migrateToOrgID(); err != nil {
		log.Printf("[DEBUG] SQLiteJobRepository - migration failed: %v", err)
		return err
	}

	schema := `
-- Schema for the job scheduler SQLite database

CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    org_id TEXT NOT NULL,
    username TEXT NOT NULL,
    schedule TEXT NOT NULL,
    payload_type TEXT NOT NULL,
    payload_details TEXT NOT NULL, -- JSON string
    status TEXT NOT NULL,
    last_run DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Index for common queries
CREATE INDEX IF NOT EXISTS idx_jobs_org_id ON jobs(org_id);
CREATE INDEX IF NOT EXISTS idx_jobs_org_status ON jobs(org_id, status);
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_name ON jobs(name);
CREATE INDEX IF NOT EXISTS idx_jobs_username ON jobs(username);
CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs(created_at);
CREATE INDEX IF NOT EXISTS idx_jobs_last_run ON jobs(last_run);

-- Trigger to update updated_at timestamp
CREATE TRIGGER IF NOT EXISTS update_jobs_updated_at 
    AFTER UPDATE ON jobs
    FOR EACH ROW
BEGIN
    UPDATE jobs SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;
`

	_, err := r.db.Exec(schema)
	if err != nil {
		log.Printf("[DEBUG] SQLiteJobRepository - schema initialization failed: %v", err)
		return err
	}

	log.Printf("[DEBUG] SQLiteJobRepository - schema initialized successfully")
	return nil
}

func (r *SQLiteJobRepository) migrateToOrgID() error {
	log.Printf("[DEBUG] SQLiteJobRepository - checking for schema migrations")

	// First check if jobs table exists
	var tableExists bool
	query := `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='jobs'`
	row := r.db.QueryRow(query)
	var tableCount int
	if err := row.Scan(&tableCount); err != nil {
		log.Printf("[DEBUG] SQLiteJobRepository - error checking table existence: %v", err)
		return err
	}

	tableExists = tableCount > 0

	if !tableExists {
		// Table doesn't exist yet, no migration needed
		log.Printf("[DEBUG] SQLiteJobRepository - jobs table doesn't exist, skipping migration")
		return nil
	}

	// Check if org_id column exists
	query = `SELECT COUNT(*) FROM pragma_table_info('jobs') WHERE name='org_id'`
	row = r.db.QueryRow(query)
	var orgIDCount int
	if err := row.Scan(&orgIDCount); err != nil {
		log.Printf("[DEBUG] SQLiteJobRepository - error checking org_id column existence: %v", err)
		return err
	}

	orgIDExists := orgIDCount > 0

	if !orgIDExists {
		log.Printf("[DEBUG] SQLiteJobRepository - adding org_id column to existing table")

		// Add org_id column with default value
		_, err := r.db.Exec(`ALTER TABLE jobs ADD COLUMN org_id TEXT DEFAULT 'default-org'`)
		if err != nil {
			log.Printf("[DEBUG] SQLiteJobRepository - error adding org_id column: %v", err)
			return err
		}

		// Update existing records to have org_id = 'default-org'
		_, err = r.db.Exec(`UPDATE jobs SET org_id = 'default-org' WHERE org_id IS NULL`)
		if err != nil {
			log.Printf("[DEBUG] SQLiteJobRepository - error updating existing records with org_id: %v", err)
			return err
		}

		log.Printf("[DEBUG] SQLiteJobRepository - org_id migration completed")
	} else {
		log.Printf("[DEBUG] SQLiteJobRepository - org_id column already exists")
	}

	// Check if username column exists
	query = `SELECT COUNT(*) FROM pragma_table_info('jobs') WHERE name='username'`
	row = r.db.QueryRow(query)
	var usernameCount int
	if err := row.Scan(&usernameCount); err != nil {
		log.Printf("[DEBUG] SQLiteJobRepository - error checking username column existence: %v", err)
		return err
	}

	usernameExists := usernameCount > 0

	if !usernameExists {
		log.Printf("[DEBUG] SQLiteJobRepository - adding username column to existing table")

		// Add username column with default value
		_, err := r.db.Exec(`ALTER TABLE jobs ADD COLUMN username TEXT DEFAULT 'unknown'`)
		if err != nil {
			log.Printf("[DEBUG] SQLiteJobRepository - error adding username column: %v", err)
			return err
		}

		// Update existing records to have username = 'unknown'
		_, err = r.db.Exec(`UPDATE jobs SET username = 'unknown' WHERE username IS NULL`)
		if err != nil {
			log.Printf("[DEBUG] SQLiteJobRepository - error updating existing records with username: %v", err)
			return err
		}

		log.Printf("[DEBUG] SQLiteJobRepository - username migration completed")
	} else {
		log.Printf("[DEBUG] SQLiteJobRepository - username column already exists")
	}

	return nil
}

func (r *SQLiteJobRepository) Save(job domain.Job) error {
	log.Printf("[DEBUG] SQLiteJobRepository.Save - saving job: %s", job.ID)

	// Marshal payload details to JSON
	payloadJSON, err := json.Marshal(job.Payload.Details)
	if err != nil {
		log.Printf("[DEBUG] SQLiteJobRepository.Save - failed to marshal payload: %v", err)
		return err
	}

	// Convert last_run to SQLite format
	var lastRun interface{}
	if job.LastRun != nil {
		lastRun = job.LastRun.Format(time.RFC3339)
	}

	// Use UPSERT (INSERT OR REPLACE)
	query := `
		INSERT OR REPLACE INTO jobs (id, name, org_id, username, schedule, payload_type, payload_details, status, last_run, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 
			COALESCE((SELECT created_at FROM jobs WHERE id = ?), CURRENT_TIMESTAMP),
			CURRENT_TIMESTAMP
		)
	`

	_, err = r.db.Exec(query, job.ID, job.Name, job.OrgID, job.Username, string(job.Schedule), string(job.Payload.Type),
		string(payloadJSON), string(job.Status), lastRun, job.ID)

	if err != nil {
		log.Printf("[DEBUG] SQLiteJobRepository.Save - database error: %v", err)
		return err
	}

	log.Printf("[DEBUG] SQLiteJobRepository.Save - job saved successfully: %s", job.ID)
	return nil
}

func (r *SQLiteJobRepository) FindByID(id string) (domain.Job, error) {
	log.Printf("[DEBUG] SQLiteJobRepository.FindByID - searching for job: %s", id)

	query := `
		SELECT id, name, org_id, username, schedule, payload_type, payload_details, status, last_run
		FROM jobs 
		WHERE id = ?
	`

	row := r.db.QueryRow(query, id)

	var job domain.Job
	var payloadJSON string
	var lastRunStr sql.NullString

	err := row.Scan(&job.ID, &job.Name, &job.OrgID, &job.Username, &job.Schedule, &job.Payload.Type,
		&payloadJSON, &job.Status, &lastRunStr)

	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("[DEBUG] SQLiteJobRepository.FindByID - job not found: %s", id)
			return domain.Job{}, domain.ErrJobNotFound
		}
		log.Printf("[DEBUG] SQLiteJobRepository.FindByID - database error: %v", err)
		return domain.Job{}, err
	}

	// Unmarshal payload details
	if err := json.Unmarshal([]byte(payloadJSON), &job.Payload.Details); err != nil {
		log.Printf("[DEBUG] SQLiteJobRepository.FindByID - failed to unmarshal payload: %v", err)
		return domain.Job{}, err
	}

	// Parse last_run time
	if lastRunStr.Valid {
		if parsedTime, err := time.Parse(time.RFC3339, lastRunStr.String); err == nil {
			job.LastRun = &parsedTime
		}
	}

	log.Printf("[DEBUG] SQLiteJobRepository.FindByID - job found: %s", id)
	return job, nil
}

func (r *SQLiteJobRepository) FindAll() ([]domain.Job, error) {
	log.Printf("[DEBUG] SQLiteJobRepository.FindAll - retrieving all jobs")

	query := `
		SELECT id, name, org_id, username, schedule, payload_type, payload_details, status, last_run
		FROM jobs 
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(query)
	if err != nil {
		log.Printf("[DEBUG] SQLiteJobRepository.FindAll - database error: %v", err)
		return nil, err
	}
	defer rows.Close()

	var jobs []domain.Job

	for rows.Next() {
		var job domain.Job
		var payloadJSON string
		var lastRunStr sql.NullString

		err := rows.Scan(&job.ID, &job.Name, &job.OrgID, &job.Username, &job.Schedule, &job.Payload.Type,
			&payloadJSON, &job.Status, &lastRunStr)

		if err != nil {
			log.Printf("[DEBUG] SQLiteJobRepository.FindAll - scan error: %v", err)
			return nil, err
		}

		// Unmarshal payload details
		if err := json.Unmarshal([]byte(payloadJSON), &job.Payload.Details); err != nil {
			log.Printf("[DEBUG] SQLiteJobRepository.FindAll - failed to unmarshal payload for job %s: %v", job.ID, err)
			continue
		}

		// Parse last_run time
		if lastRunStr.Valid {
			if parsedTime, err := time.Parse(time.RFC3339, lastRunStr.String); err == nil {
				job.LastRun = &parsedTime
			}
		}

		jobs = append(jobs, job)
	}

	if err := rows.Err(); err != nil {
		log.Printf("[DEBUG] SQLiteJobRepository.FindAll - rows error: %v", err)
		return nil, err
	}

	log.Printf("[DEBUG] SQLiteJobRepository.FindAll - found %d jobs", len(jobs))
	return jobs, nil
}

func (r *SQLiteJobRepository) FindByOrgID(orgID string) ([]domain.Job, error) {
	log.Printf("[DEBUG] SQLiteJobRepository.FindByOrgID - retrieving jobs for org: %s", orgID)

	query := `
		SELECT id, name, org_id, username, schedule, payload_type, payload_details, status, last_run
		FROM jobs 
		WHERE org_id = ?
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(query, orgID)
	if err != nil {
		log.Printf("[DEBUG] SQLiteJobRepository.FindByOrgID - database error: %v", err)
		return nil, err
	}
	defer rows.Close()

	var jobs []domain.Job

	for rows.Next() {
		var job domain.Job
		var payloadJSON string
		var lastRunStr sql.NullString

		err := rows.Scan(&job.ID, &job.Name, &job.OrgID, &job.Username, &job.Schedule, &job.Payload.Type,
			&payloadJSON, &job.Status, &lastRunStr)

		if err != nil {
			log.Printf("[DEBUG] SQLiteJobRepository.FindByOrgID - scan error: %v", err)
			return nil, err
		}

		// Unmarshal payload details
		if err := json.Unmarshal([]byte(payloadJSON), &job.Payload.Details); err != nil {
			log.Printf("[DEBUG] SQLiteJobRepository.FindByOrgID - failed to unmarshal payload for job %s: %v", job.ID, err)
			continue
		}

		// Parse last_run time
		if lastRunStr.Valid {
			if parsedTime, err := time.Parse(time.RFC3339, lastRunStr.String); err == nil {
				job.LastRun = &parsedTime
			}
		}

		jobs = append(jobs, job)
	}

	if err := rows.Err(); err != nil {
		log.Printf("[DEBUG] SQLiteJobRepository.FindByOrgID - rows error: %v", err)
		return nil, err
	}

	log.Printf("[DEBUG] SQLiteJobRepository.FindByOrgID - found %d jobs for org %s", len(jobs), orgID)
	return jobs, nil
}

func (r *SQLiteJobRepository) Delete(id string) error {
	log.Printf("[DEBUG] SQLiteJobRepository.Delete - deleting job: %s", id)

	// First check if job exists
	_, err := r.FindByID(id)
	if err != nil {
		return err
	}

	query := `DELETE FROM jobs WHERE id = ?`

	result, err := r.db.Exec(query, id)
	if err != nil {
		log.Printf("[DEBUG] SQLiteJobRepository.Delete - database error: %v", err)
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Printf("[DEBUG] SQLiteJobRepository.Delete - failed to get rows affected: %v", err)
		return err
	}

	if rowsAffected == 0 {
		log.Printf("[DEBUG] SQLiteJobRepository.Delete - no rows affected for job: %s", id)
		return domain.ErrJobNotFound
	}

	log.Printf("[DEBUG] SQLiteJobRepository.Delete - job deleted successfully: %s", id)
	return nil
}

func (r *SQLiteJobRepository) Close() error {
	log.Printf("[DEBUG] SQLiteJobRepository.Close - closing database connection")
	return r.db.Close()
}
