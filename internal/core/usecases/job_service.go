package usecases

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"insights-scheduler/internal/core/domain"
)

type JobRepository interface {
	Save(job domain.Job) error
	FindByID(id string) (domain.Job, error)
	FindAll() ([]domain.Job, error)
	FindByOrgID(orgID string) ([]domain.Job, error)
	FindByUserID(userID string, offset, limit int) ([]domain.Job, int, error)
	Delete(id string) error
}

type JobRunRepository interface {
	Save(run domain.JobRun) error
	FindByID(id string) (domain.JobRun, error)
	FindByJobID(jobID string, offset, limit int) ([]domain.JobRun, int, error)
	FindByJobIDAndOrgID(jobID string, orgID string) ([]domain.JobRun, error)
	FindAll() ([]domain.JobRun, error)
}

type SchedulingService interface {
	ShouldRun(job domain.Job, currentTime time.Time) bool
}

type JobExecutor interface {
	Execute(job domain.Job) error
}

type CronScheduler interface {
	ScheduleJob(job domain.Job) error
	UnscheduleJob(jobID string)
}

type JobService struct {
	repo          JobRepository
	scheduler     SchedulingService
	executor      JobExecutor
	cronScheduler CronScheduler
}

func NewJobService(repo JobRepository, scheduler SchedulingService, executor JobExecutor) *JobService {
	return &JobService{
		repo:      repo,
		scheduler: scheduler,
		executor:  executor,
	}
}

func (s *JobService) SetCronScheduler(cronScheduler CronScheduler) {
	s.cronScheduler = cronScheduler
}

// calculateNextRunAt calculates the next run time for a job based on its cron schedule
// The schedule is interpreted in the specified timezone, but the result is always returned in UTC
func calculateNextRunAt(schedule string, timezone string) (*time.Time, error) {
	// Default to UTC if not specified
	if timezone == "" {
		timezone = "UTC"
	}

	// Load the timezone
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %s: %w", timezone, err)
	}

	// Parse the cron expression
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	cronSchedule, err := parser.Parse(schedule)
	if err != nil {
		return nil, err
	}

	// Get current time in the specified timezone
	nowInTz := time.Now().In(loc)

	// Calculate next run in the specified timezone
	nextInTz := cronSchedule.Next(nowInTz)

	// Convert to UTC for storage
	nextRunAtUTC := nextInTz.UTC()

	log.Printf("[DEBUG] Schedule '%s' in timezone '%s': next run is %s (%s UTC)",
		schedule, timezone, nextInTz.Format(time.RFC3339), nextRunAtUTC.Format(time.RFC3339))

	return &nextRunAtUTC, nil
}

func (s *JobService) CreateJob(name string, orgID string, username string, userID string, schedule string, timezone string, payloadType domain.PayloadType, payload interface{}) (domain.Job, error) {
	log.Printf("[DEBUG] CreateJob called - name: %s, orgID: %s, username: %s, userID: %s, schedule: %s, timezone: %s, payload type: %s", name, orgID, username, userID, schedule, timezone, payloadType)

	// Validate org_id
	if orgID == "" {
		log.Printf("[DEBUG] CreateJob failed - missing org_id")
		return domain.Job{}, domain.ErrInvalidOrgID
	}
	log.Printf("[DEBUG] CreateJob - org_id validation passed: %s", orgID)

	// Default to UTC if timezone not specified
	if timezone == "" {
		timezone = "UTC"
	}

	// Validate timezone
	if !domain.IsValidTimezone(timezone) {
		log.Printf("[DEBUG] CreateJob failed - invalid timezone: %s", timezone)
		return domain.Job{}, domain.ErrInvalidTimezone
	}
	log.Printf("[DEBUG] CreateJob - timezone validation passed: %s", timezone)

	if !domain.IsValidSchedule(schedule) {
		log.Printf("[DEBUG] CreateJob failed - invalid schedule: %s", schedule)
		return domain.Job{}, domain.ErrInvalidSchedule
	}
	log.Printf("[DEBUG] CreateJob - schedule validation passed: %s", schedule)

	if !domain.IsValidPayloadType(string(payloadType)) {
		log.Printf("[DEBUG] CreateJob failed - invalid payload type: %s", payloadType)
		return domain.Job{}, domain.ErrInvalidPayload
	}
	log.Printf("[DEBUG] CreateJob - payload type validation passed: %s", payloadType)

	job := domain.NewJob(name, orgID, username, userID, domain.Schedule(schedule), timezone, payloadType, payload)
	log.Printf("[DEBUG] CreateJob - created job with ID: %s, status: %s, timezone: %s", job.ID, job.Status, job.Timezone)

	// Calculate next run time using the job's timezone
	nextRunAt, err := calculateNextRunAt(schedule, timezone)
	if err != nil {
		log.Printf("[DEBUG] CreateJob - failed to calculate next run time: %v", err)
		// Continue without next_run if calculation fails
	} else if nextRunAt != nil {
		job = job.WithNextRunAt(*nextRunAt)
		log.Printf("[DEBUG] CreateJob - calculated next run time: %s", nextRunAt.Format(time.RFC3339))
	}

	err = s.repo.Save(job)
	if err != nil {
		log.Printf("[DEBUG] CreateJob failed - repository save error: %v", err)
		return domain.Job{}, err
	}
	log.Printf("[DEBUG] CreateJob - job saved to repository successfully: %s", job.ID)

	// Schedule the job in cron scheduler if it's scheduled
	if s.cronScheduler != nil && job.Status == domain.StatusScheduled {
		log.Printf("[DEBUG] CreateJob - attempting to schedule job in cron scheduler: %s", job.ID)
		if err := s.cronScheduler.ScheduleJob(job); err != nil {
			log.Printf("[DEBUG] CreateJob - cron scheduler error (job still created): %v", err)
			// Log error but don't fail the creation
			// The job is saved, it just won't be scheduled until next restart
		} else {
			log.Printf("[DEBUG] CreateJob - job successfully scheduled in cron scheduler: %s", job.ID)
		}
	} else if s.cronScheduler == nil {
		log.Printf("[DEBUG] CreateJob - cron scheduler not available, job saved but not scheduled: %s", job.ID)
	} else {
		log.Printf("[DEBUG] CreateJob - job not scheduled due to status: %s (status: %s)", job.ID, job.Status)
	}

	log.Printf("[DEBUG] CreateJob completed successfully - job ID: %s", job.ID)
	return job, nil
}

func (s *JobService) GetJob(id string) (domain.Job, error) {
	return s.repo.FindByID(id)
}

func (s *JobService) GetJobWithOrgCheck(id string, orgID string) (domain.Job, error) {
	job, err := s.repo.FindByID(id)
	if err != nil {
		return domain.Job{}, err
	}

	if job.OrgID != orgID {
		return domain.Job{}, domain.ErrJobNotFound // Don't reveal existence of job from other orgs
	}

	return job, nil
}

func (s *JobService) GetJobWithUserCheck(id string, userID string) (domain.Job, error) {
	job, err := s.repo.FindByID(id)
	if err != nil {
		return domain.Job{}, err
	}

	if job.UserID != userID {
		return domain.Job{}, domain.ErrJobNotFound // Don't reveal existence of job from other users
	}

	return job, nil
}

func (s *JobService) ListJobs() ([]domain.Job, error) {
	return s.repo.FindAll()
}

func (s *JobService) GetAllJobs(statusFilter, nameFilter string) ([]domain.Job, error) {
	jobs, err := s.repo.FindAll()
	if err != nil {
		return nil, err
	}

	filtered := make([]domain.Job, 0)
	for _, job := range jobs {
		if statusFilter != "" && string(job.Status) != statusFilter {
			continue
		}
		if nameFilter != "" && !strings.Contains(strings.ToLower(job.Name), strings.ToLower(nameFilter)) {
			continue
		}
		filtered = append(filtered, job)
	}

	return filtered, nil
}

func (s *JobService) GetJobsByOrgID(orgID string, statusFilter, nameFilter string) ([]domain.Job, error) {
	jobs, err := s.repo.FindByOrgID(orgID)
	if err != nil {
		return nil, err
	}

	filtered := make([]domain.Job, 0)
	for _, job := range jobs {
		if statusFilter != "" && string(job.Status) != statusFilter {
			continue
		}
		if nameFilter != "" && !strings.Contains(strings.ToLower(job.Name), strings.ToLower(nameFilter)) {
			continue
		}
		filtered = append(filtered, job)
	}

	return filtered, nil
}

func (s *JobService) GetJobsByUserID(userID string, statusFilter, nameFilter string, offset, limit int) ([]domain.Job, int, error) {
	jobs, total, err := s.repo.FindByUserID(userID, offset, limit)
	if err != nil {
		return nil, 0, err
	}

	filtered := make([]domain.Job, 0)
	for _, job := range jobs {
		if statusFilter != "" && string(job.Status) != statusFilter {
			continue
		}
		if nameFilter != "" && !strings.Contains(strings.ToLower(job.Name), strings.ToLower(nameFilter)) {
			continue
		}
		filtered = append(filtered, job)
	}

	return filtered, total, nil
}

func (s *JobService) UpdateJob(id string, name string, orgID string, username string, userID string, schedule string, payloadType domain.PayloadType, payload interface{}, status string) (domain.Job, error) {
	job, err := s.repo.FindByID(id)
	if err != nil {
		return domain.Job{}, err
	}

	// Check if job belongs to the same organization
	if job.OrgID != orgID {
		return domain.Job{}, domain.ErrJobNotFound // Don't reveal existence of job from other orgs
	}

	// Validate org_id
	if orgID == "" {
		return domain.Job{}, domain.ErrInvalidOrgID
	}

	if !domain.IsValidSchedule(schedule) {
		return domain.Job{}, domain.ErrInvalidSchedule
	}

	if !domain.IsValidPayloadType(string(payloadType)) {
		return domain.Job{}, domain.ErrInvalidPayload
	}

	if !domain.IsValidStatus(status) {
		return domain.Job{}, domain.ErrInvalidStatus
	}

	scheduleVal := domain.Schedule(schedule)
	statusVal := domain.JobStatus(status)

	updatedJob := job.UpdateFields(&name, &orgID, &username, &userID, &scheduleVal, &payloadType, &payload, &statusVal)

	// Recalculate next run time if schedule changed
	if schedule != string(job.Schedule) {
		nextRunAt, calcErr := calculateNextRunAt(schedule, updatedJob.Timezone)
		if calcErr != nil {
			log.Printf("[DEBUG] UpdateJob - failed to calculate next run time: %v", calcErr)
		} else if nextRunAt != nil {
			updatedJob = updatedJob.WithNextRunAt(*nextRunAt)
			log.Printf("[DEBUG] UpdateJob - calculated next run time: %s", nextRunAt.Format(time.RFC3339))
		}
	}

	err = s.repo.Save(updatedJob)
	if err != nil {
		return domain.Job{}, err
	}

	// Update cron scheduling
	if s.cronScheduler != nil {
		s.cronScheduler.UnscheduleJob(id) // Remove old schedule
		if updatedJob.Status == domain.StatusScheduled {
			if err := s.cronScheduler.ScheduleJob(updatedJob); err != nil {
				// Log error but don't fail the update
			}
		}
	}

	return updatedJob, nil
}

func (s *JobService) PatchJobWithOrgCheck(id string, userOrgID string, updates map[string]interface{}) (domain.Job, error) {
	job, err := s.repo.FindByID(id)
	if err != nil {
		return domain.Job{}, err
	}

	// Check if job belongs to the same organization
	if job.OrgID != userOrgID {
		return domain.Job{}, domain.ErrJobNotFound // Don't reveal existence of job from other orgs
	}

	var name *string
	var orgID *string
	var username *string
	var userID *string
	var schedule *domain.Schedule
	var payloadType *domain.PayloadType
	var payload *interface{}
	var status *domain.JobStatus

	if v, ok := updates["name"]; ok {
		if nameStr, ok := v.(string); ok {
			name = &nameStr
		}
	}

	if v, ok := updates["org_id"]; ok {
		if orgIDStr, ok := v.(string); ok {
			if orgIDStr == "" {
				return domain.Job{}, domain.ErrInvalidOrgID
			}
			// For security, don't allow changing org_id via API
			// Always use the authenticated user's org_id
			orgID = &userOrgID
		}
	}

	if v, ok := updates["username"]; ok {
		if usernameStr, ok := v.(string); ok {
			username = &usernameStr
		}
	}

	if v, ok := updates["user_id"]; ok {
		if userIDStr, ok := v.(string); ok {
			userID = &userIDStr
		}
	}

	if v, ok := updates["schedule"]; ok {
		if schedStr, ok := v.(string); ok {
			if !domain.IsValidSchedule(schedStr) {
				return domain.Job{}, domain.ErrInvalidSchedule
			}
			schedVal := domain.Schedule(schedStr)
			schedule = &schedVal
		}
	}

	if v, ok := updates["type"]; ok {
		if typeStr, ok := v.(string); ok {
			if !domain.IsValidPayloadType(typeStr) {
				return domain.Job{}, domain.ErrInvalidPayload
			}
			pt := domain.PayloadType(typeStr)
			payloadType = &pt
		}
	}

	if v, ok := updates["payload"]; ok {
		// Payload can be any JSON value (object, array, string, number, etc.)
		payload = &v
	}

	if v, ok := updates["status"]; ok {
		if statusStr, ok := v.(string); ok {
			if !domain.IsValidStatus(statusStr) {
				return domain.Job{}, domain.ErrInvalidStatus
			}
			statusVal := domain.JobStatus(statusStr)
			status = &statusVal
		}
	}

	updatedJob := job.UpdateFields(name, orgID, username, userID, schedule, payloadType, payload, status)

	// Recalculate next run time if schedule was updated
	if schedule != nil {
		nextRunAt, calcErr := calculateNextRunAt(string(*schedule), updatedJob.Timezone)
		if calcErr != nil {
			log.Printf("[DEBUG] PatchJobWithOrgCheck - failed to calculate next run time: %v", calcErr)
		} else if nextRunAt != nil {
			updatedJob = updatedJob.WithNextRunAt(*nextRunAt)
			log.Printf("[DEBUG] PatchJobWithOrgCheck - calculated next run time: %s", nextRunAt.Format(time.RFC3339))
		}
	}

	err = s.repo.Save(updatedJob)
	if err != nil {
		return domain.Job{}, err
	}

	// Update cron scheduling
	if s.cronScheduler != nil {
		s.cronScheduler.UnscheduleJob(id) // Remove old schedule
		if updatedJob.Status == domain.StatusScheduled {
			if err := s.cronScheduler.ScheduleJob(updatedJob); err != nil {
				// Log error but don't fail the update
			}
		}
	}

	return updatedJob, nil
}

func (s *JobService) PatchJobWithUserCheck(id string, userUserID string, updates map[string]interface{}) (domain.Job, error) {
	job, err := s.repo.FindByID(id)
	if err != nil {
		return domain.Job{}, err
	}

	// Check if job belongs to the same user
	if job.UserID != userUserID {
		return domain.Job{}, domain.ErrJobNotFound // Don't reveal existence of job from other users
	}

	var name *string
	var orgID *string
	var username *string
	var userID *string
	var schedule *domain.Schedule
	var payloadType *domain.PayloadType
	var payload *interface{}
	var status *domain.JobStatus

	if v, ok := updates["name"]; ok {
		if nameStr, ok := v.(string); ok {
			name = &nameStr
		}
	}

	if v, ok := updates["org_id"]; ok {
		if orgIDStr, ok := v.(string); ok {
			if orgIDStr == "" {
				return domain.Job{}, domain.ErrInvalidOrgID
			}
			// For security, don't allow changing org_id via API
			// Keep the job's current org_id
			currentOrgID := job.OrgID
			orgID = &currentOrgID
		}
	}

	if v, ok := updates["username"]; ok {
		if usernameStr, ok := v.(string); ok {
			username = &usernameStr
		}
	}

	if v, ok := updates["user_id"]; ok {
		if _, ok := v.(string); ok {
			// For security, don't allow changing user_id via API
			// Always use the authenticated user's user_id
			userID = &userUserID
		}
	}

	if v, ok := updates["schedule"]; ok {
		if schedStr, ok := v.(string); ok {
			if !domain.IsValidSchedule(schedStr) {
				return domain.Job{}, domain.ErrInvalidSchedule
			}
			schedVal := domain.Schedule(schedStr)
			schedule = &schedVal
		}
	}

	if v, ok := updates["type"]; ok {
		if typeStr, ok := v.(string); ok {
			if !domain.IsValidPayloadType(typeStr) {
				return domain.Job{}, domain.ErrInvalidPayload
			}
			pt := domain.PayloadType(typeStr)
			payloadType = &pt
		}
	}

	if v, ok := updates["payload"]; ok {
		// Payload can be any JSON value (object, array, string, number, etc.)
		payload = &v
	}

	if v, ok := updates["status"]; ok {
		if statusStr, ok := v.(string); ok {
			if !domain.IsValidStatus(statusStr) {
				return domain.Job{}, domain.ErrInvalidStatus
			}
			statusVal := domain.JobStatus(statusStr)
			status = &statusVal
		}
	}

	updatedJob := job.UpdateFields(name, orgID, username, userID, schedule, payloadType, payload, status)

	// Recalculate next run time if schedule was updated
	if schedule != nil {
		nextRunAt, calcErr := calculateNextRunAt(string(*schedule), updatedJob.Timezone)
		if calcErr != nil {
			log.Printf("[DEBUG] PatchJobWithUserCheck - failed to calculate next run time: %v", calcErr)
		} else if nextRunAt != nil {
			updatedJob = updatedJob.WithNextRunAt(*nextRunAt)
			log.Printf("[DEBUG] PatchJobWithUserCheck - calculated next run time: %s", nextRunAt.Format(time.RFC3339))
		}
	}

	err = s.repo.Save(updatedJob)
	if err != nil {
		return domain.Job{}, err
	}

	// Update cron scheduling
	if s.cronScheduler != nil {
		s.cronScheduler.UnscheduleJob(id) // Remove old schedule
		if updatedJob.Status == domain.StatusScheduled {
			if err := s.cronScheduler.ScheduleJob(updatedJob); err != nil {
				// Log error but don't fail the update
			}
		}
	}

	return updatedJob, nil
}

func (s *JobService) DeleteJob(id string) error {
	_, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}

	// Unschedule from cron scheduler
	if s.cronScheduler != nil {
		s.cronScheduler.UnscheduleJob(id)
	}

	return s.repo.Delete(id)
}

func (s *JobService) DeleteJobWithOrgCheck(id string, orgID string) error {
	job, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}

	// Check if job belongs to the same organization
	if job.OrgID != orgID {
		return domain.ErrJobNotFound // Don't reveal existence of job from other orgs
	}

	// Unschedule from cron scheduler
	if s.cronScheduler != nil {
		s.cronScheduler.UnscheduleJob(id)
	}

	return s.repo.Delete(id)
}

func (s *JobService) DeleteJobWithUserCheck(id string, userID string) error {
	job, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}

	// Check if job belongs to the same user
	if job.UserID != userID {
		return domain.ErrJobNotFound // Don't reveal existence of job from other users
	}

	// Unschedule from cron scheduler
	if s.cronScheduler != nil {
		s.cronScheduler.UnscheduleJob(id)
	}

	return s.repo.Delete(id)
}

func (s *JobService) RunJob(id string) error {
	job, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}

	runningJob := job.WithStatus(domain.StatusRunning).WithLastRunAt(time.Now().UTC())
	err = s.repo.Save(runningJob)
	if err != nil {
		return err
	}

	err = s.executor.Execute(runningJob)
	fmt.Println("err: ", err)

	var finalStatus domain.JobStatus
	if err != nil {
		finalStatus = domain.StatusFailed
	} else {
		finalStatus = domain.StatusScheduled
	}

	finalJob := runningJob.WithStatus(finalStatus)

	// Calculate next run time after execution
	nextRunAt, calcErr := calculateNextRunAt(string(job.Schedule), job.Timezone)
	if calcErr != nil {
		log.Printf("[DEBUG] RunJob - failed to calculate next run time for job %s: %v", id, calcErr)
	} else if nextRunAt != nil {
		finalJob = finalJob.WithNextRunAt(*nextRunAt)
		log.Printf("[DEBUG] RunJob - calculated next run time for job %s: %s", id, nextRunAt.Format(time.RFC3339))
	}

	return s.repo.Save(finalJob)
}

func (s *JobService) PauseJob(id string) (domain.Job, error) {
	job, err := s.repo.FindByID(id)
	if err != nil {
		return domain.Job{}, err
	}

	if job.Status == domain.StatusPaused {
		return domain.Job{}, domain.ErrJobAlreadyPaused
	}

	pausedJob := job.WithStatus(domain.StatusPaused)
	err = s.repo.Save(pausedJob)
	if err != nil {
		return domain.Job{}, err
	}

	// Unschedule from cron scheduler
	if s.cronScheduler != nil {
		s.cronScheduler.UnscheduleJob(id)
	}

	return pausedJob, nil
}

func (s *JobService) PauseJobWithOrgCheck(id string, orgID string) (domain.Job, error) {
	job, err := s.repo.FindByID(id)
	if err != nil {
		return domain.Job{}, err
	}

	// Check if job belongs to the same organization
	if job.OrgID != orgID {
		return domain.Job{}, domain.ErrJobNotFound // Don't reveal existence of job from other orgs
	}

	if job.Status == domain.StatusPaused {
		return domain.Job{}, domain.ErrJobAlreadyPaused
	}

	pausedJob := job.WithStatus(domain.StatusPaused)
	err = s.repo.Save(pausedJob)
	if err != nil {
		return domain.Job{}, err
	}

	// Unschedule from cron scheduler
	if s.cronScheduler != nil {
		s.cronScheduler.UnscheduleJob(id)
	}

	return pausedJob, nil
}

func (s *JobService) PauseJobWithUserCheck(id string, userID string) (domain.Job, error) {
	job, err := s.repo.FindByID(id)
	if err != nil {
		return domain.Job{}, err
	}

	// Check if job belongs to the same user
	if job.UserID != userID {
		return domain.Job{}, domain.ErrJobNotFound // Don't reveal existence of job from other users
	}

	if job.Status == domain.StatusPaused {
		return domain.Job{}, domain.ErrJobAlreadyPaused
	}

	pausedJob := job.WithStatus(domain.StatusPaused)
	err = s.repo.Save(pausedJob)
	if err != nil {
		return domain.Job{}, err
	}

	// Unschedule from cron scheduler
	if s.cronScheduler != nil {
		s.cronScheduler.UnscheduleJob(id)
	}

	return pausedJob, nil
}

func (s *JobService) ResumeJob(id string) (domain.Job, error) {
	job, err := s.repo.FindByID(id)
	if err != nil {
		return domain.Job{}, err
	}

	if job.Status != domain.StatusPaused {
		return domain.Job{}, domain.ErrJobNotPaused
	}

	resumedJob := job.WithStatus(domain.StatusScheduled)

	// Recalculate next run time when resuming
	nextRunAt, calcErr := calculateNextRunAt(string(job.Schedule), job.Timezone)
	if calcErr != nil {
		log.Printf("[DEBUG] ResumeJob - failed to calculate next run time: %v", calcErr)
	} else if nextRunAt != nil {
		resumedJob = resumedJob.WithNextRunAt(*nextRunAt)
		log.Printf("[DEBUG] ResumeJob - calculated next run time: %s", nextRunAt.Format(time.RFC3339))
	}

	err = s.repo.Save(resumedJob)
	if err != nil {
		return domain.Job{}, err
	}

	// Reschedule in cron scheduler
	if s.cronScheduler != nil {
		if err := s.cronScheduler.ScheduleJob(resumedJob); err != nil {
			// Log error but don't fail the resume
		}
	}

	return resumedJob, nil
}

func (s *JobService) ResumeJobWithOrgCheck(id string, orgID string) (domain.Job, error) {
	job, err := s.repo.FindByID(id)
	if err != nil {
		return domain.Job{}, err
	}

	// Check if job belongs to the same organization
	if job.OrgID != orgID {
		return domain.Job{}, domain.ErrJobNotFound // Don't reveal existence of job from other orgs
	}

	if job.Status != domain.StatusPaused {
		return domain.Job{}, domain.ErrJobNotPaused
	}

	resumedJob := job.WithStatus(domain.StatusScheduled)

	// Recalculate next run time when resuming
	nextRunAt, calcErr := calculateNextRunAt(string(job.Schedule), job.Timezone)
	if calcErr != nil {
		log.Printf("[DEBUG] ResumeJobWithOrgCheck - failed to calculate next run time: %v", calcErr)
	} else if nextRunAt != nil {
		resumedJob = resumedJob.WithNextRunAt(*nextRunAt)
		log.Printf("[DEBUG] ResumeJobWithOrgCheck - calculated next run time: %s", nextRunAt.Format(time.RFC3339))
	}

	err = s.repo.Save(resumedJob)
	if err != nil {
		return domain.Job{}, err
	}

	// Reschedule in cron scheduler
	if s.cronScheduler != nil {
		if err := s.cronScheduler.ScheduleJob(resumedJob); err != nil {
			// Log error but don't fail the resume
		}
	}

	return resumedJob, nil
}

func (s *JobService) ResumeJobWithUserCheck(id string, userID string) (domain.Job, error) {
	job, err := s.repo.FindByID(id)
	if err != nil {
		return domain.Job{}, err
	}

	// Check if job belongs to the same user
	if job.UserID != userID {
		return domain.Job{}, domain.ErrJobNotFound // Don't reveal existence of job from other users
	}

	if job.Status != domain.StatusPaused {
		return domain.Job{}, domain.ErrJobNotPaused
	}

	resumedJob := job.WithStatus(domain.StatusScheduled)

	// Recalculate next run time when resuming
	nextRunAt, calcErr := calculateNextRunAt(string(job.Schedule), job.Timezone)
	if calcErr != nil {
		log.Printf("[DEBUG] ResumeJobWithUserCheck - failed to calculate next run time: %v", calcErr)
	} else if nextRunAt != nil {
		resumedJob = resumedJob.WithNextRunAt(*nextRunAt)
		log.Printf("[DEBUG] ResumeJobWithUserCheck - calculated next run time: %s", nextRunAt.Format(time.RFC3339))
	}

	err = s.repo.Save(resumedJob)
	if err != nil {
		return domain.Job{}, err
	}

	// Reschedule in cron scheduler
	if s.cronScheduler != nil {
		if err := s.cronScheduler.ScheduleJob(resumedJob); err != nil {
			// Log error but don't fail the resume
		}
	}

	return resumedJob, nil
}

func (s *JobService) RunJobWithOrgCheck(id string, orgID string) error {
	job, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}

	// Check if job belongs to the same organization
	if job.OrgID != orgID {
		return domain.ErrJobNotFound // Don't reveal existence of job from other orgs
	}

	return s.RunJob(id)
}

func (s *JobService) RunJobWithUserCheck(id string, userID string) error {
	job, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}

	// Check if job belongs to the same user
	if job.UserID != userID {
		return domain.ErrJobNotFound // Don't reveal existence of job from other users
	}

	return s.RunJob(id)
}

func (s *JobService) GetScheduledJobs() ([]domain.Job, error) {
	jobs, err := s.repo.FindAll()
	if err != nil {
		return nil, err
	}

	scheduled := make([]domain.Job, 0)
	for _, job := range jobs {
		if job.Status == domain.StatusScheduled && s.scheduler.ShouldRun(job, time.Now().UTC()) {
			scheduled = append(scheduled, job)
		}
	}

	return scheduled, nil
}

func (s *JobService) ExecuteScheduledJob(job domain.Job) error {
	return s.RunJob(job.ID)
}
