package scheduler

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/robfig/cron/v3"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/usecases"
)

type CronScheduler struct {
	jobService *usecases.JobService
	cron       *cron.Cron
	jobEntries map[string]cron.EntryID // jobID -> cronEntryID mapping
	mu         sync.RWMutex
}

func NewCronScheduler(jobService *usecases.JobService) *CronScheduler {
	return &CronScheduler{
		jobService: jobService,
		cron:       cron.New(), // Standard 5-field format (minute hour dom month dow)
		jobEntries: make(map[string]cron.EntryID),
	}
}

func (s *CronScheduler) Start(ctx context.Context) {
	log.Println("Starting cron scheduler")
	log.Println("HERE!!")

	// Load existing jobs and schedule them
	s.loadAndScheduleAllJobs()

	// Start the cron scheduler
	s.cron.Start()

	// Wait for context cancellation
	<-ctx.Done()
	log.Println("Scheduler context cancelled, stopping")
}

func (s *CronScheduler) Stop() {
	log.Println("Stopping cron scheduler")
	ctx := s.cron.Stop()
	<-ctx.Done()
	log.Println("Cron scheduler stopped")
}

// ScheduleJob adds or updates a job in the cron scheduler
func (s *CronScheduler) ScheduleJob(job domain.Job) error {
	log.Printf("[DEBUG] CronScheduler.ScheduleJob called - job ID: %s, name: %s, schedule: %s, status: %s", job.ID, job.Name, job.Schedule, job.Status)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove existing entry if it exists
	if entryID, exists := s.jobEntries[job.ID]; exists {
		log.Printf("[DEBUG] CronScheduler.ScheduleJob - removing existing entry for job: %s", job.ID)
		s.cron.Remove(entryID)
		delete(s.jobEntries, job.ID)
	}

	// Only schedule jobs that are in scheduled status
	if job.Status != domain.StatusScheduled && job.Status != domain.StatusFailed {
		log.Printf("[DEBUG] CronScheduler.ScheduleJob - job not in scheduled/failed status, skipping: %s (status: %s)", job.ID, job.Status)
		return nil
	}

	log.Printf("[DEBUG] CronScheduler.ScheduleJob - creating job execution function for: %s", job.ID)

	// Create job execution function
	jobFunc := func() {
		log.Printf("Executing cron job: %s (%s)", job.Name, job.ID)

		// Get the latest job state from repository
		currentJob, err := s.jobService.GetJob(job.ID)
		if err != nil {
			log.Printf("Error getting job %s for execution: %v", job.ID, err)
			return
		}

		// Only execute if job is still scheduled
		if currentJob.Status != domain.StatusScheduled {
			log.Printf("Job %s is no longer scheduled, skipping execution", job.ID)
			return
		}

		if err := s.jobService.ExecuteScheduledJob(currentJob); err != nil {
			log.Printf("Error executing job %s: %v", job.ID, err)
		}
	}

	log.Printf("[DEBUG] CronScheduler.ScheduleJob - adding job to cron with expression: %s", job.Schedule)

	// Schedule the job
	entryID, err := s.cron.AddFunc(string(job.Schedule), jobFunc)
	if err != nil {
		log.Printf("[DEBUG] CronScheduler.ScheduleJob failed - cron.AddFunc error: %v", err)
		return err
	}

	s.jobEntries[job.ID] = entryID
	log.Printf("[DEBUG] CronScheduler.ScheduleJob - job successfully added to cron scheduler: %s (entry ID: %d)", job.ID, entryID)
	log.Printf("Scheduled job %s (%s) with cron expression: %s", job.Name, job.ID, job.Schedule)

	return nil
}

// UnscheduleJob removes a job from the cron scheduler
func (s *CronScheduler) UnscheduleJob(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, exists := s.jobEntries[jobID]; exists {
		s.cron.Remove(entryID)
		delete(s.jobEntries, jobID)
		log.Printf("Unscheduled job: %s", jobID)
	}
}

// loadAndScheduleAllJobs loads all scheduled jobs from the repository and schedules them
func (s *CronScheduler) loadAndScheduleAllJobs() {
	jobs, err := s.jobService.ListJobs()
	fmt.Println("inside loadAndScheduleAllJobs - jobs: ", jobs)
	if err != nil {
		log.Printf("Error loading jobs: %v", err)
		return
	}

	for _, job := range jobs {
		fmt.Printf("Scheduling job: %v\n", job)
		fmt.Printf("job.Status: %v\n", job.Status)
		// Only schedule jobs that are scheduled or failed
		if job.Status == domain.StatusScheduled || job.Status == domain.StatusFailed {
			if err := s.ScheduleJob(job); err != nil {
				log.Printf("Error scheduling job %s: %v", job.ID, err)
			}
		}
	}
}
