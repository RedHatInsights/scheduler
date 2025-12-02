package http

import (
	"time"

	"insights-scheduler/internal/core/domain"
)

// JobResponse is the API response model for Job objects.
// It excludes org_id, username, and user_id which are extracted from the identity header.
type JobResponse struct {
	ID       string             `json:"id"`
	Name     string             `json:"name"`
	Schedule string             `json:"schedule"`
	Type     string             `json:"type"`
	Payload  JobPayloadResponse `json:"payload,omitempty"`
	Status   string             `json:"status"`
	LastRun  *time.Time         `json:"last_run,omitempty"`
}

// JobPayloadResponse is the API response model for JobPayload
type JobPayloadResponse struct {
	Details map[string]interface{} `json:"details,omitempty"`
}

// ToJobResponse converts a domain.Job to a JobResponse DTO
func ToJobResponse(job domain.Job) JobResponse {
	return JobResponse{
		ID:       job.ID,
		Name:     job.Name,
		Schedule: string(job.Schedule),
		Type:     string(job.Type),
		Payload: JobPayloadResponse{
			Details: job.Payload.Details,
		},
		Status:  string(job.Status),
		LastRun: job.LastRun,
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
