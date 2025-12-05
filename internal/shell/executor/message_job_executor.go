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
	// Cast payload to map[string]interface{}
	payloadMap, ok := job.Payload.(map[string]interface{})
	if !ok {
		log.Printf("Processing message: unknown (payload is not a map)")
		return nil
	}

	message, ok := payloadMap["message"].(string)
	if !ok {
		message = "unknown"
	}
	log.Printf("Processing message: %s", message)
	return nil
}
