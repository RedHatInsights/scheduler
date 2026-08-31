package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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

// FindScheduledNearDue returns scheduled jobs with next_run_at within the lookahead window,
// sorted by next_run_at ascending (earliest due first).
func (r *PostgresJobRepository) FindScheduledNearDue(lookahead time.Duration) ([]domain.Job, error) {
	query := `
		SELECT id, name, org_id, user_id, schedule, timezone,
		       payload_type, payload_details, status, last_run_at,
		       next_run_at, consecutive_failures, last_failed_at
		FROM jobs
		WHERE status = 'scheduled'
		  AND next_run_at IS NOT NULL
		  AND next_run_at <= NOW() + ($1 || ' seconds')::INTERVAL
		ORDER BY next_run_at ASC`

	lookaheadSeconds := int(lookahead.Seconds())
	return r.queryJobs(query, lookaheadSeconds)
}

func (r *PostgresJobRepository) FindByOrgID(orgID string) ([]domain.Job, error) {
	return r.queryJobs(`SELECT id, name, org_id, user_id, schedule, timezone, payload_type, payload_details, status, last_run_at, next_run_at, consecutive_failures, last_failed_at
	    FROM jobs WHERE org_id = $1 ORDER BY created_at DESC`, orgID)
}

func (r *PostgresJobRepository) FindByUserID(userID string, filter domain.JobFilter, sort domain.SortSpec, offset, limit int) ([]domain.Job, int, error) {
	// Build the shared WHERE clause. All filter values are bound as parameters
	// ($N placeholders), never concatenated, so there is no injection surface;
	// the same clause is used for the count and the page so the total matches the
	// filtered result set.
	where, args := buildJobFilterWhere(userID, filter)

	// First get the total count of the filtered set.
	var total int
	countQuery := "SELECT COUNT(*) FROM jobs " + where
	if err := r.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Then get the paginated results. The ORDER BY clause is built exclusively
	// from allowlisted column literals (see buildOrderByClause), so the sort
	// input is not an injection vector.
	orderBy := buildOrderByClause(sort, jobSortColumns, "created_at", "")
	args = append(args, limit, offset)
	query := fmt.Sprintf(`SELECT id, name, org_id, user_id, schedule, timezone, payload_type, payload_details, status, last_run_at, next_run_at, consecutive_failures, last_failed_at
	    FROM jobs %s %s LIMIT $%d OFFSET $%d`, where, orderBy, len(args)-1, len(args))
	jobs, err := r.queryJobs(query, args...)
	if err != nil {
		return nil, 0, err
	}

	return jobs, total, nil
}

// buildJobFilterWhere renders the WHERE clause for a user-scoped job query and
// the ordered argument list backing its $N placeholders. Every dynamic value is
// a bound parameter; only fixed column names appear in the SQL text.
func buildJobFilterWhere(userID string, filter domain.JobFilter) (string, []interface{}) {
	conditions := []string{"user_id = $1"}
	args := []interface{}{userID}

	if filter.Status != "" {
		args = append(args, filter.Status)
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}
	if filter.NameContains != "" {
		args = append(args, "%"+escapeLikePattern(filter.NameContains)+"%")
		conditions = append(conditions, fmt.Sprintf("name ILIKE $%d ESCAPE '\\'", len(args)))
	}

	return "WHERE " + strings.Join(conditions, " AND "), args
}

// escapeLikePattern escapes the LIKE/ILIKE wildcard metacharacters so a user's
// substring is matched literally (a '%' or '_' in the search term is treated as
// text, not a wildcard). Used with an explicit ESCAPE '\' clause.
func escapeLikePattern(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(s)
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
