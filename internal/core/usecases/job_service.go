package usecases

import (
	"fmt"
	"log"
	"strings"
	"time"

	"insights-scheduler-part-2/internal/core/domain"
)

type JobRepository interface {
	Save(job domain.Job) error
	FindByID(id string) (domain.Job, error)
	FindAll() ([]domain.Job, error)
	FindByOrgID(orgID string) ([]domain.Job, error)
	Delete(id string) error
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

func (s *JobService) CreateJob(name string, orgID string, username string, userID string, schedule string, payload domain.JobPayload) (domain.Job, error) {
	log.Printf("[DEBUG] CreateJob called - name: %s, orgID: %s, username: %s, userID: %s, schedule: %s, payload type: %s", name, orgID, username, userID, schedule, payload.Type)

	// Validate org_id
	if orgID == "" {
		log.Printf("[DEBUG] CreateJob failed - missing org_id")
		return domain.Job{}, domain.ErrInvalidOrgID
	}
	log.Printf("[DEBUG] CreateJob - org_id validation passed: %s", orgID)

	if !domain.IsValidSchedule(schedule) {
		log.Printf("[DEBUG] CreateJob failed - invalid schedule: %s", schedule)
		return domain.Job{}, domain.ErrInvalidSchedule
	}
	log.Printf("[DEBUG] CreateJob - schedule validation passed: %s", schedule)

	if !domain.IsValidPayloadType(string(payload.Type)) {
		log.Printf("[DEBUG] CreateJob failed - invalid payload type: %s", payload.Type)
		return domain.Job{}, domain.ErrInvalidPayload
	}
	log.Printf("[DEBUG] CreateJob - payload type validation passed: %s", payload.Type)

	job := domain.NewJob(name, orgID, username, userID, domain.Schedule(schedule), payload)
	log.Printf("[DEBUG] CreateJob - created job with ID: %s, status: %s", job.ID, job.Status)

	err := s.repo.Save(job)
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

func (s *JobService) ListJobs() ([]domain.Job, error) {
	return s.repo.FindAll()
}

func (s *JobService) GetAllJobs(statusFilter, nameFilter string) ([]domain.Job, error) {
	jobs, err := s.repo.FindAll()
	if err != nil {
		return nil, err
	}

	var filtered []domain.Job
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

	var filtered []domain.Job
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

func (s *JobService) UpdateJob(id string, name string, orgID string, username string, userID string, schedule string, payload domain.JobPayload, status string) (domain.Job, error) {
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

	if !domain.IsValidPayloadType(string(payload.Type)) {
		return domain.Job{}, domain.ErrInvalidPayload
	}

	if !domain.IsValidStatus(status) {
		return domain.Job{}, domain.ErrInvalidStatus
	}

	scheduleVal := domain.Schedule(schedule)
	statusVal := domain.JobStatus(status)

	updatedJob := job.UpdateFields(&name, &orgID, &username, &userID, &scheduleVal, &payload, &statusVal)

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
	var payload *domain.JobPayload
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

	if v, ok := updates["payload"]; ok {
		if payloadMap, ok := v.(map[string]interface{}); ok {
			typeStr, typeOk := payloadMap["type"].(string)
			details, detailsOk := payloadMap["details"].(map[string]interface{})

			if typeOk && detailsOk {
				if !domain.IsValidPayloadType(typeStr) {
					return domain.Job{}, domain.ErrInvalidPayload
				}
				payloadVal := domain.JobPayload{
					Type:    domain.PayloadType(typeStr),
					Details: details,
				}
				payload = &payloadVal
			}
		}
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

	updatedJob := job.UpdateFields(name, orgID, username, userID, schedule, payload, status)

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

func (s *JobService) RunJob(id string) error {
	job, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}

	runningJob := job.WithStatus(domain.StatusRunning).WithLastRun(time.Now())
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

func (s *JobService) ResumeJob(id string) (domain.Job, error) {
	job, err := s.repo.FindByID(id)
	if err != nil {
		return domain.Job{}, err
	}

	if job.Status != domain.StatusPaused {
		return domain.Job{}, domain.ErrJobNotPaused
	}

	resumedJob := job.WithStatus(domain.StatusScheduled)
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

func (s *JobService) GetScheduledJobs() ([]domain.Job, error) {
	jobs, err := s.repo.FindAll()
	if err != nil {
		return nil, err
	}

	var scheduled []domain.Job
	for _, job := range jobs {
		if job.Status == domain.StatusScheduled && s.scheduler.ShouldRun(job, time.Now()) {
			scheduled = append(scheduled, job)
		}
	}

	return scheduled, nil
}

func (s *JobService) ExecuteScheduledJob(job domain.Job) error {
	return s.RunJob(job.ID)
}
