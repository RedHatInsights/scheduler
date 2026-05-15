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
func (e *MessageJobExecutor) Execute(job domain.Job) (interface{}, domain.ResultType, error) {
	// Cast payload to map[string]interface{}
	payloadMap, ok := job.Payload.(map[string]interface{})
	if !ok {
		log.Printf("Processing message: unknown (payload is not a map)")
		result := domain.MessageResult{
			Type:           domain.ResultTypeMessage,
			Message:        "unknown",
			DeliveryStatus: "sent",
		}
		return result, domain.ResultTypeMessage, nil
	}

	message, ok := payloadMap["message"].(string)
	if !ok {
		message = "unknown"
	}
	log.Printf("Processing message: %s", message)

	result := domain.MessageResult{
		Type:           domain.ResultTypeMessage,
		Message:        message,
		DeliveryStatus: "sent",
	}
	return result, domain.ResultTypeMessage, nil
}
