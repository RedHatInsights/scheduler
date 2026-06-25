package executor

import (
	"log/slog"

	"insights-scheduler/internal/core/domain"
)

// MessageJobExecutor handles message payload type jobs
type MessageJobExecutor struct{}

// NewMessageJobExecutor creates a new MessageJobExecutor
func NewMessageJobExecutor() *MessageJobExecutor {
	return &MessageJobExecutor{}
}

// Execute executes a message job
func (e *MessageJobExecutor) Execute(job domain.Job, logger *slog.Logger) (interface{}, domain.ResultType, error) {
	// Cast payload to map[string]interface{}
	payloadMap, ok := job.Payload.(map[string]interface{})
	if !ok {
		logger.Warn("Processing message with unknown payload (not a map)")
		result := domain.MessageResult{
			Message:        "unknown",
			DeliveryStatus: "sent",
		}
		return result, domain.ResultTypeMessage, nil
	}

	message, ok := payloadMap["message"].(string)
	if !ok {
		message = "unknown"
	}
	logger.Info("Processing message", slog.String("message", message))

	result := domain.MessageResult{
		Message:        message,
		DeliveryStatus: "sent",
	}
	return result, domain.ResultTypeMessage, nil
}
