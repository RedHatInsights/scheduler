package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"insights-scheduler/internal/core/domain"
)

// RedisJobRepository implements JobRepository using Redis
type RedisJobRepository struct {
	client    *redis.Client
	ctx       context.Context
	keyPrefix string
}

// RedisJobQueue manages scheduled jobs in a ZSET for polling-based scheduling
type RedisJobQueue struct {
	client    *redis.Client
	ctx       context.Context
	keyPrefix string
}

// NewRedisJobQueue creates a new Redis-based job queue
func NewRedisJobQueue(client *redis.Client, keyPrefix string) *RedisJobQueue {
	return &RedisJobQueue{
		client:    client,
		ctx:       context.Background(),
		keyPrefix: keyPrefix,
	}
}

// scheduledJobsKey returns the Redis key for the scheduled jobs ZSET
func (q *RedisJobQueue) scheduledJobsKey() string {
	return fmt.Sprintf("%sscheduled_jobs", q.keyPrefix)
}

// Schedule adds or updates a job in the scheduled jobs ZSET with next run time as score
func (q *RedisJobQueue) Schedule(jobID string, nextRunTime time.Time) error {
	score := float64(nextRunTime.Unix())
	err := q.client.ZAdd(q.ctx, q.scheduledJobsKey(), redis.Z{
		Score:  score,
		Member: jobID,
	}).Err()
	if err != nil {
		return fmt.Errorf("failed to schedule job: %w", err)
	}
	return nil
}

// Unschedule removes a job from the scheduled jobs ZSET
func (q *RedisJobQueue) Unschedule(jobID string) error {
	err := q.client.ZRem(q.ctx, q.scheduledJobsKey(), jobID).Err()
	if err != nil {
		return fmt.Errorf("failed to unschedule job: %w", err)
	}
	return nil
}

// GetJobsDue returns job IDs that are due to run (score <= current time)
func (q *RedisJobQueue) GetJobsDue(currentTime time.Time) ([]string, error) {
	maxScore := float64(currentTime.Unix())

	// Get jobs with score from -inf to current timestamp
	jobIDs, err := q.client.ZRangeByScore(q.ctx, q.scheduledJobsKey(), &redis.ZRangeBy{
		Min: "-inf",
		Max: fmt.Sprintf("%f", maxScore),
	}).Result()

	if err != nil {
		return nil, fmt.Errorf("failed to get due jobs: %w", err)
	}

	return jobIDs, nil
}

// GetNextRunTime returns the next run time for a job
func (q *RedisJobQueue) GetNextRunTime(jobID string) (*time.Time, error) {
	score, err := q.client.ZScore(q.ctx, q.scheduledJobsKey(), jobID).Result()
	if err == redis.Nil {
		return nil, nil // Job not scheduled
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get next run time: %w", err)
	}

	nextRun := time.Unix(int64(score), 0)
	return &nextRun, nil
}

// GetAllScheduledJobs returns all scheduled job IDs with their next run times
func (q *RedisJobQueue) GetAllScheduledJobs() (map[string]time.Time, error) {
	// Get all jobs with scores
	results, err := q.client.ZRangeWithScores(q.ctx, q.scheduledJobsKey(), 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get scheduled jobs: %w", err)
	}

	jobSchedule := make(map[string]time.Time)
	for _, z := range results {
		jobID := z.Member.(string)
		nextRun := time.Unix(int64(z.Score), 0)
		jobSchedule[jobID] = nextRun
	}

	return jobSchedule, nil
}

// Count returns the number of scheduled jobs
func (q *RedisJobQueue) Count() (int64, error) {
	count, err := q.client.ZCard(q.ctx, q.scheduledJobsKey()).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to count scheduled jobs: %w", err)
	}
	return count, nil
}

// NewRedisJobRepository creates a new Redis-based job repository
func NewRedisJobRepository(client *redis.Client, keyPrefix string) *RedisJobRepository {
	return &RedisJobRepository{
		client:    client,
		ctx:       context.Background(),
		keyPrefix: keyPrefix,
	}
}

// jobKey returns the Redis key for a job
func (r *RedisJobRepository) jobKey(id string) string {
	return fmt.Sprintf("%sjob:%s", r.keyPrefix, id)
}

// jobsSetKey returns the Redis key for the set of all job IDs
func (r *RedisJobRepository) jobsSetKey() string {
	return fmt.Sprintf("%sjobs", r.keyPrefix)
}

// orgJobsSetKey returns the Redis key for the set of job IDs for an org
func (r *RedisJobRepository) orgJobsSetKey(orgID string) string {
	return fmt.Sprintf("%sorg:%s:jobs", r.keyPrefix, orgID)
}

// userJobsSetKey returns the Redis key for the set of job IDs for a user
func (r *RedisJobRepository) userJobsSetKey(userID string) string {
	return fmt.Sprintf("%suser:%s:jobs", r.keyPrefix, userID)
}

// Save stores a job in Redis
func (r *RedisJobRepository) Save(job domain.Job) error {
	// Marshal job to JSON
	jobJSON, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal job: %w", err)
	}

	pipe := r.client.Pipeline()

	// Store job data
	pipe.Set(r.ctx, r.jobKey(job.ID), jobJSON, 0)

	// Add to global jobs set
	pipe.SAdd(r.ctx, r.jobsSetKey(), job.ID)

	// Add to org jobs set
	pipe.SAdd(r.ctx, r.orgJobsSetKey(job.OrgID), job.ID)

	// Add to user jobs set
	pipe.SAdd(r.ctx, r.userJobsSetKey(job.UserID), job.ID)

	// Execute pipeline
	_, err = pipe.Exec(r.ctx)
	if err != nil {
		return fmt.Errorf("failed to save job: %w", err)
	}

	return nil
}

// FindByID retrieves a job by ID
func (r *RedisJobRepository) FindByID(id string) (domain.Job, error) {
	jobJSON, err := r.client.Get(r.ctx, r.jobKey(id)).Result()
	if err == redis.Nil {
		return domain.Job{}, fmt.Errorf("job not found: %s", id)
	}
	if err != nil {
		return domain.Job{}, fmt.Errorf("failed to get job: %w", err)
	}

	var job domain.Job
	if err := json.Unmarshal([]byte(jobJSON), &job); err != nil {
		return domain.Job{}, fmt.Errorf("failed to unmarshal job: %w", err)
	}

	return job, nil
}

// FindAll retrieves all jobs
func (r *RedisJobRepository) FindAll() ([]domain.Job, error) {
	// Get all job IDs from the set
	jobIDs, err := r.client.SMembers(r.ctx, r.jobsSetKey()).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get job IDs: %w", err)
	}

	if len(jobIDs) == 0 {
		return []domain.Job{}, nil
	}

	// Get all jobs in a pipeline
	jobs := make([]domain.Job, 0, len(jobIDs))
	for _, jobID := range jobIDs {
		job, err := r.FindByID(jobID)
		if err != nil {
			// Skip jobs that fail to load (may have been deleted)
			continue
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// FindByOrgID retrieves all jobs for an organization
func (r *RedisJobRepository) FindByOrgID(orgID string) ([]domain.Job, error) {
	// Get all job IDs for this org
	jobIDs, err := r.client.SMembers(r.ctx, r.orgJobsSetKey(orgID)).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get job IDs for org: %w", err)
	}

	if len(jobIDs) == 0 {
		return []domain.Job{}, nil
	}

	// Get all jobs
	jobs := make([]domain.Job, 0, len(jobIDs))
	for _, jobID := range jobIDs {
		job, err := r.FindByID(jobID)
		if err != nil {
			// Skip jobs that fail to load
			continue
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// FindByUserID retrieves all jobs for a user
func (r *RedisJobRepository) FindByUserID(userID string) ([]domain.Job, error) {
	// Get all job IDs for this user
	jobIDs, err := r.client.SMembers(r.ctx, r.userJobsSetKey(userID)).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get job IDs for user: %w", err)
	}

	if len(jobIDs) == 0 {
		return []domain.Job{}, nil
	}

	// Get all jobs
	jobs := make([]domain.Job, 0, len(jobIDs))
	for _, jobID := range jobIDs {
		job, err := r.FindByID(jobID)
		if err != nil {
			// Skip jobs that fail to load
			continue
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// Delete removes a job from Redis
func (r *RedisJobRepository) Delete(id string) error {
	// First, get the job to find org and user IDs
	job, err := r.FindByID(id)
	if err != nil {
		return err
	}

	pipe := r.client.Pipeline()

	// Delete job data
	pipe.Del(r.ctx, r.jobKey(id))

	// Remove from global jobs set
	pipe.SRem(r.ctx, r.jobsSetKey(), id)

	// Remove from org jobs set
	pipe.SRem(r.ctx, r.orgJobsSetKey(job.OrgID), id)

	// Remove from user jobs set
	pipe.SRem(r.ctx, r.userJobsSetKey(job.UserID), id)

	// Execute pipeline
	_, err = pipe.Exec(r.ctx)
	if err != nil {
		return fmt.Errorf("failed to delete job: %w", err)
	}

	return nil
}

// RedisJobRunRepository implements JobRunRepository using Redis
type RedisJobRunRepository struct {
	client    *redis.Client
	ctx       context.Context
	keyPrefix string
}

// NewRedisJobRunRepository creates a new Redis-based job run repository
func NewRedisJobRunRepository(client *redis.Client, keyPrefix string) *RedisJobRunRepository {
	return &RedisJobRunRepository{
		client:    client,
		ctx:       context.Background(),
		keyPrefix: keyPrefix,
	}
}

// runKey returns the Redis key for a job run
func (r *RedisJobRunRepository) runKey(id string) string {
	return fmt.Sprintf("%srun:%s", r.keyPrefix, id)
}

// jobRunsListKey returns the Redis key for the list of runs for a job
func (r *RedisJobRunRepository) jobRunsListKey(jobID string) string {
	return fmt.Sprintf("%sjob:%s:runs", r.keyPrefix, jobID)
}

// runsSetKey returns the Redis key for the set of all run IDs
func (r *RedisJobRunRepository) runsSetKey() string {
	return fmt.Sprintf("%sruns", r.keyPrefix)
}

// Save stores a job run in Redis
func (r *RedisJobRunRepository) Save(run domain.JobRun) error {
	// Marshal run to JSON
	runJSON, err := json.Marshal(run)
	if err != nil {
		return fmt.Errorf("failed to marshal job run: %w", err)
	}

	pipe := r.client.Pipeline()

	// Store run data
	pipe.Set(r.ctx, r.runKey(run.ID), runJSON, 0)

	// Add to global runs set
	pipe.SAdd(r.ctx, r.runsSetKey(), run.ID)

	// Add to job's runs list (sorted by start time)
	pipe.ZAdd(r.ctx, r.jobRunsListKey(run.JobID), redis.Z{
		Score:  float64(run.StartTime.Unix()),
		Member: run.ID,
	})

	// Execute pipeline
	_, err = pipe.Exec(r.ctx)
	if err != nil {
		return fmt.Errorf("failed to save job run: %w", err)
	}

	return nil
}

// FindByID retrieves a job run by ID
func (r *RedisJobRunRepository) FindByID(id string) (domain.JobRun, error) {
	runJSON, err := r.client.Get(r.ctx, r.runKey(id)).Result()
	if err == redis.Nil {
		return domain.JobRun{}, fmt.Errorf("job run not found: %s", id)
	}
	if err != nil {
		return domain.JobRun{}, fmt.Errorf("failed to get job run: %w", err)
	}

	var run domain.JobRun
	if err := json.Unmarshal([]byte(runJSON), &run); err != nil {
		return domain.JobRun{}, fmt.Errorf("failed to unmarshal job run: %w", err)
	}

	return run, nil
}

// FindByJobID retrieves all runs for a job (sorted by start time, newest first)
func (r *RedisJobRunRepository) FindByJobID(jobID string) ([]domain.JobRun, error) {
	// Get all run IDs for this job (sorted by start time, descending)
	runIDs, err := r.client.ZRevRange(r.ctx, r.jobRunsListKey(jobID), 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get run IDs for job: %w", err)
	}

	if len(runIDs) == 0 {
		return []domain.JobRun{}, nil
	}

	// Get all runs
	runs := make([]domain.JobRun, 0, len(runIDs))
	for _, runID := range runIDs {
		run, err := r.FindByID(runID)
		if err != nil {
			// Skip runs that fail to load
			continue
		}
		runs = append(runs, run)
	}

	return runs, nil
}

// FindByJobIDAndOrgID retrieves all runs for a job belonging to an org
// Note: JobRun doesn't store OrgID, so we just verify the job belongs to the org
func (r *RedisJobRunRepository) FindByJobIDAndOrgID(jobID string, orgID string) ([]domain.JobRun, error) {
	// Since JobRun doesn't have OrgID, we just return all runs for the job
	// The caller should validate that the job belongs to the org
	// TODO: Consider adding OrgID to JobRun for better filtering
	return r.FindByJobID(jobID)
}

// FindAll retrieves all job runs
func (r *RedisJobRunRepository) FindAll() ([]domain.JobRun, error) {
	// Get all run IDs from the set
	runIDs, err := r.client.SMembers(r.ctx, r.runsSetKey()).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get run IDs: %w", err)
	}

	if len(runIDs) == 0 {
		return []domain.JobRun{}, nil
	}

	// Get all runs
	runs := make([]domain.JobRun, 0, len(runIDs))
	for _, runID := range runIDs {
		run, err := r.FindByID(runID)
		if err != nil {
			// Skip runs that fail to load
			continue
		}
		runs = append(runs, run)
	}

	return runs, nil
}

// RedisLockManager implements distributed locks using Redis
type RedisLockManager struct {
	client     *redis.Client
	ctx        context.Context
	keyPrefix  string
	lockTTL    time.Duration
	instanceID string
}

// NewRedisLockManager creates a new Redis-based lock manager
func NewRedisLockManager(client *redis.Client, keyPrefix string, lockTTL time.Duration, instanceID string) *RedisLockManager {
	return &RedisLockManager{
		client:     client,
		ctx:        context.Background(),
		keyPrefix:  keyPrefix,
		lockTTL:    lockTTL,
		instanceID: instanceID,
	}
}

// lockKey returns the Redis key for a lock
func (l *RedisLockManager) lockKey(jobID string) string {
	return fmt.Sprintf("%slock:job:%s", l.keyPrefix, jobID)
}

// TryAcquire attempts to acquire a lock for a job (non-blocking)
func (l *RedisLockManager) TryAcquire(jobID string) (bool, error) {
	key := l.lockKey(jobID)

	// Try to set the key with NX (only if not exists) and PX (expiration)
	acquired, err := l.client.SetNX(l.ctx, key, l.instanceID, l.lockTTL).Result()
	if err != nil {
		return false, fmt.Errorf("failed to acquire lock: %w", err)
	}

	return acquired, nil
}

// Release releases a lock for a job
func (l *RedisLockManager) Release(jobID string) error {
	key := l.lockKey(jobID)

	// Use Lua script to ensure we only delete our own lock
	script := `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("del", KEYS[1])
		else
			return 0
		end
	`

	result, err := l.client.Eval(l.ctx, script, []string{key}, l.instanceID).Result()
	if err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}

	if result == int64(0) {
		return fmt.Errorf("lock not owned by this instance")
	}

	return nil
}

// Extend extends the TTL of a lock (useful for long-running jobs)
func (l *RedisLockManager) Extend(jobID string, additionalTTL time.Duration) error {
	key := l.lockKey(jobID)

	// Use Lua script to extend TTL only if we own the lock
	script := `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("pexpire", KEYS[1], ARGV[2])
		else
			return 0
		end
	`

	result, err := l.client.Eval(l.ctx, script, []string{key}, l.instanceID, additionalTTL.Milliseconds()).Result()
	if err != nil {
		return fmt.Errorf("failed to extend lock: %w", err)
	}

	if result == int64(0) {
		return fmt.Errorf("lock not owned by this instance")
	}

	return nil
}

// IsLocked checks if a job is currently locked
func (l *RedisLockManager) IsLocked(jobID string) (bool, error) {
	key := l.lockKey(jobID)

	exists, err := l.client.Exists(l.ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check lock: %w", err)
	}

	return exists > 0, nil
}
