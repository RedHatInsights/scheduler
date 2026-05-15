package domain

import (
	"time"

	"github.com/google/uuid"
)

type JobRunStatus string

const (
	RunStatusRunning   JobRunStatus = "running"
	RunStatusCompleted JobRunStatus = "completed"
	RunStatusFailed    JobRunStatus = "failed"
)

type JobRun struct {
	ID           string       `json:"id"`
	JobID        string       `json:"job_id"`
	Status       JobRunStatus `json:"status"`
	StartTime    time.Time    `json:"start_time"`
	EndTime      *time.Time   `json:"end_time,omitempty"`
	ErrorMessage *string      `json:"error_message,omitempty"`
	ResultType   *ResultType  `json:"result_type,omitempty"`
	Result       interface{}  `json:"result,omitempty"`
}

func NewJobRun(jobID string) JobRun {
	return JobRun{
		ID:           uuid.New().String(),
		JobID:        jobID,
		Status:       RunStatusRunning,
		StartTime:    time.Now().UTC(),
		EndTime:      nil,
		ErrorMessage: nil,
		ResultType:   nil,
		Result:       nil,
	}
}

func (jr JobRun) WithCompleted(resultType ResultType, result interface{}) JobRun {
	now := time.Now().UTC()
	return JobRun{
		ID:           jr.ID,
		JobID:        jr.JobID,
		Status:       RunStatusCompleted,
		StartTime:    jr.StartTime,
		EndTime:      &now,
		ErrorMessage: nil,
		ResultType:   &resultType,
		Result:       result,
	}
}

func (jr JobRun) WithFailed(errorMessage string) JobRun {
	now := time.Now().UTC()
	return JobRun{
		ID:           jr.ID,
		JobID:        jr.JobID,
		Status:       RunStatusFailed,
		StartTime:    jr.StartTime,
		EndTime:      &now,
		ErrorMessage: &errorMessage,
		ResultType:   nil,
		Result:       nil,
	}
}

func IsValidRunStatus(s string) bool {
	switch JobRunStatus(s) {
	case RunStatusRunning, RunStatusCompleted, RunStatusFailed:
		return true
	default:
		return false
	}
}
