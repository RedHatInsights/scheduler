package http

import (
	"encoding/json"
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
	LastRunAt *time.Time  `json:"last_run_at,omitempty"`
	NextRunAt *time.Time  `json:"next_run_at,omitempty"`
}

// ToJobResponse converts a domain.Job to a JobResponse DTO
// Timestamps (LastRunAt, NextRunAt) are converted from UTC to the job's timezone
func ToJobResponse(job domain.Job) JobResponse {
	// Convert timestamps from UTC to the job's timezone
	var lastRunAtInTz *time.Time
	var nextRunAtInTz *time.Time

	if job.Timezone != "" {
		// Load the timezone location
		loc, err := time.LoadLocation(job.Timezone)
		if err == nil {
			// Convert LastRunAt to job's timezone
			if job.LastRunAt != nil {
				convertedLastRunAt := job.LastRunAt.In(loc)
				lastRunAtInTz = &convertedLastRunAt
			}

			// Convert NextRunAt to job's timezone
			if job.NextRunAt != nil {
				convertedNextRunAt := job.NextRunAt.In(loc)
				nextRunAtInTz = &convertedNextRunAt
			}
		} else {
			// If timezone is invalid, fall back to UTC (shouldn't happen due to validation)
			lastRunAtInTz = job.LastRunAt
			nextRunAtInTz = job.NextRunAt
		}
	} else {
		// No timezone specified, use UTC
		lastRunAtInTz = job.LastRunAt
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
		LastRunAt: lastRunAtInTz,
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

// JobRunResponse is the API response model for JobRun objects.
type JobRunResponse struct {
	ID           string      `json:"id"`
	JobID        string      `json:"job_id"`
	Status       string      `json:"status"`
	StartTime    time.Time   `json:"start_time"`
	EndTime      *time.Time  `json:"end_time,omitempty"`
	ErrorMessage *string     `json:"error_message,omitempty"`
	ResultType   *string     `json:"result_type,omitempty"`
	Result       interface{} `json:"result,omitempty"`
}

// ToJobRunResponse converts a domain.JobRun to a JobRunResponse DTO
func ToJobRunResponse(run domain.JobRun) JobRunResponse {
	var resultType *string
	if run.ResultType != nil {
		rt := string(*run.ResultType)
		resultType = &rt
	}

	// Process the result field: try to ensure it's a proper JSON object or string
	var processedResult interface{}
	if run.Result != nil {
		// If result is a string, try to unmarshal it as JSON
		if resultStr, ok := run.Result.(string); ok {
			var jsonObj interface{}
			if err := json.Unmarshal([]byte(resultStr), &jsonObj); err == nil {
				// Successfully unmarshaled as JSON - use the object
				processedResult = jsonObj
			} else {
				// Not valid JSON - keep as string (legacy result)
				processedResult = resultStr
			}
		} else {
			// Already an object (map, struct, etc.) - keep as is
			processedResult = run.Result
		}
	}

	return JobRunResponse{
		ID:           run.ID,
		JobID:        run.JobID,
		Status:       string(run.Status),
		StartTime:    run.StartTime,
		EndTime:      run.EndTime,
		ErrorMessage: run.ErrorMessage,
		ResultType:   resultType,
		Result:       processedResult,
	}
}

// ToJobRunResponseList converts a slice of domain.JobRun to a slice of JobRunResponse DTOs
func ToJobRunResponseList(runs []domain.JobRun) []JobRunResponse {
	responses := make([]JobRunResponse, len(runs))
	for i, run := range runs {
		responses[i] = ToJobRunResponse(run)
	}
	return responses
}
