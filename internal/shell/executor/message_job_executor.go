package executor

import (
	"log"

	"insights-scheduler/internal/core/domain"
)

// MessageJobExecutor handles message payload type jobs
type MessageJobExecutor struct{}

// NewMessageJobExecutor creates a new MessageJobExecutor
func NewMessageJobExecutor() *MessageJobExecutor {
	return &MessageJobExecutor{}
}

// Execute executes a message job
func (e *MessageJobExecutor) Execute(job domain.Job) error {
	message, ok := job.Payload.Details["message"].(string)
	if !ok {
		message = "unknown"
	}
	log.Printf("Processing message: %s", message)
	return nil
}
