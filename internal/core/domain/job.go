package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

type JobStatus string

const (
	StatusScheduled JobStatus = "scheduled"
	StatusRunning   JobStatus = "running"
	StatusPaused    JobStatus = "paused"
	StatusFailed    JobStatus = "failed"
)

type PayloadType string

const (
	PayloadMessage      PayloadType = "message"
	PayloadHTTPRequest  PayloadType = "http_request"
	PayloadCommand      PayloadType = "command"
	PayloadExport       PayloadType = "export"
	PayloadInventoryPDF PayloadType = "inventory-pdf"
)

type Schedule string

const (
	Schedule10Minutes Schedule = "*/10 * * * *" // Every 10 minutes
	Schedule1Hour     Schedule = "0 * * * *"    // Every hour at minute 0
	Schedule1Day      Schedule = "0 0 * * *"    // Every day at midnight
	Schedule1Month    Schedule = "0 0 1 * *"    // Every month on the 1st at midnight
)

type Job struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	OrgID    string      `json:"org_id"`
	Username string      `json:"username"`
	UserID   string      `json:"user_id"`
	Schedule Schedule    `json:"schedule"`
	Type     PayloadType `json:"type"`
	Payload  interface{} `json:"payload,omitempty"`
	Status   JobStatus   `json:"status"`
	LastRun  *time.Time  `json:"last_run,omitempty"`
}

func NewJob(name string, orgID string, username string, userID string, schedule Schedule, payloadType PayloadType, payload interface{}) Job {
	return Job{
		ID:       uuid.New().String(),
		Name:     name,
		OrgID:    orgID,
		Username: username,
		UserID:   userID,
		Schedule: schedule,
		Type:     payloadType,
		Payload:  payload,
		Status:   StatusScheduled,
		LastRun:  nil,
	}
}

func (j Job) WithStatus(status JobStatus) Job {
	return Job{
		ID:       j.ID,
		Name:     j.Name,
		OrgID:    j.OrgID,
		Username: j.Username,
		UserID:   j.UserID,
		Schedule: j.Schedule,
		Type:     j.Type,
		Payload:  j.Payload,
		Status:   status,
		LastRun:  j.LastRun,
	}
}

func (j Job) WithLastRun(lastRun time.Time) Job {
	return Job{
		ID:       j.ID,
		Name:     j.Name,
		OrgID:    j.OrgID,
		Username: j.Username,
		UserID:   j.UserID,
		Schedule: j.Schedule,
		Type:     j.Type,
		Payload:  j.Payload,
		Status:   j.Status,
		LastRun:  &lastRun,
	}
}

func (j Job) UpdateFields(name *string, orgID *string, username *string, userID *string, schedule *Schedule, payloadType *PayloadType, payload *interface{}, status *JobStatus) Job {
	updated := j

	if name != nil {
		updated.Name = *name
	}
	if orgID != nil {
		updated.OrgID = *orgID
	}
	if username != nil {
		updated.Username = *username
	}
	if userID != nil {
		updated.UserID = *userID
	}
	if schedule != nil {
		updated.Schedule = *schedule
	}
	if payloadType != nil {
		updated.Type = *payloadType
	}
	if payload != nil {
		updated.Payload = *payload
	}
	if status != nil {
		updated.Status = *status
	}

	return updated
}

func (j Job) ToJSON() ([]byte, error) {
	return json.Marshal(j)
}

func JobFromJSON(data []byte) (Job, error) {
	var job Job
	err := json.Unmarshal(data, &job)
	return job, err
}

func IsValidSchedule(s string) bool {
	// Check if it's one of our predefined schedules
	switch Schedule(s) {
	case Schedule10Minutes, Schedule1Hour, Schedule1Day, Schedule1Month:
		return true
	}

	// If not predefined, validate as a standard 5-field cron expression (minute hour dom month dow)
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := parser.Parse(s)
	return err == nil
}

func IsValidPayloadType(pt string) bool {
	switch PayloadType(pt) {
	case PayloadMessage, PayloadHTTPRequest, PayloadCommand, PayloadExport, PayloadInventoryPDF:
		return true
	default:
		return false
	}
}

func IsValidStatus(s string) bool {
	switch JobStatus(s) {
	case StatusScheduled, StatusRunning, StatusPaused, StatusFailed:
		return true
	default:
		return false
	}
}
