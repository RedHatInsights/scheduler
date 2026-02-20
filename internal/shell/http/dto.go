package http

import (
	"time"

	"insights-scheduler/internal/core/domain"
)

// JobResponse is the API response model for Job objects.
// It excludes org_id, username, and user_id which are extracted from the identity header.
type JobResponse struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Schedule  string      `json:"schedule"`
	Timezone  string      `json:"timezone"`
	Type      string      `json:"type"`
	Payload   interface{} `json:"payload,omitempty"`
	Status    string      `json:"status"`
	LastRun   *time.Time  `json:"last_run,omitempty"`
	NextRunAt *time.Time  `json:"next_run_at,omitempty"`
}

// ToJobResponse converts a domain.Job to a JobResponse DTO
// Timestamps (LastRun, NextRunAt) are converted from UTC to the job's timezone
func ToJobResponse(job domain.Job) JobResponse {
	// Convert timestamps from UTC to the job's timezone
	var lastRunInTz *time.Time
	var nextRunAtInTz *time.Time

	if job.Timezone != "" {
		// Load the timezone location
		loc, err := time.LoadLocation(job.Timezone)
		if err == nil {
			// Convert LastRun to job's timezone
			if job.LastRun != nil {
				convertedLastRun := job.LastRun.In(loc)
				lastRunInTz = &convertedLastRun
			}

			// Convert NextRunAt to job's timezone
			if job.NextRunAt != nil {
				convertedNextRunAt := job.NextRunAt.In(loc)
				nextRunAtInTz = &convertedNextRunAt
			}
		} else {
			// If timezone is invalid, fall back to UTC (shouldn't happen due to validation)
			lastRunInTz = job.LastRun
			nextRunAtInTz = job.NextRunAt
		}
	} else {
		// No timezone specified, use UTC
		lastRunInTz = job.LastRun
		nextRunAtInTz = job.NextRunAt
	}

	return JobResponse{
		ID:        job.ID,
		Name:      job.Name,
		Schedule:  string(job.Schedule),
		Timezone:  job.Timezone,
		Type:      string(job.Type),
		Payload:   job.Payload,
		Status:    string(job.Status),
		LastRun:   lastRunInTz,
		NextRunAt: nextRunAtInTz,
	}
}

// ToJobResponseList converts a slice of domain.Job to a slice of JobResponse DTOs
func ToJobResponseList(jobs []domain.Job) []JobResponse {
	responses := make([]JobResponse, len(jobs))
	for i, job := range jobs {
		responses[i] = ToJobResponse(job)
	}
	return responses
}
