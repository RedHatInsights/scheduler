package executor

import (
	"log/slog"

	"insights-scheduler/internal/core/domain"
)

// CommandJobExecutor handles command payload type jobs
type CommandJobExecutor struct{}

// NewCommandJobExecutor creates a new CommandJobExecutor
func NewCommandJobExecutor() *CommandJobExecutor {
	return &CommandJobExecutor{}
}

// Execute executes a command job
func (e *CommandJobExecutor) Execute(job domain.Job, logger *slog.Logger) (interface{}, domain.ResultType, error) {
	// Cast payload to map[string]interface{}
	payloadMap, ok := job.Payload.(map[string]interface{})
	if !ok {
		logger.Warn("Executing command with unknown payload (not a map)")
		result := domain.CommandResult{
			Command:  "unknown",
			ExitCode: 0,
			Duration: 0,
		}
		return result, domain.ResultTypeCommand, nil
	}

	command, ok := payloadMap["command"].(string)
	if !ok {
		command = "unknown"
	}
	logger.Info("Executing command", slog.String("command", command))

	result := domain.CommandResult{
		Command:  command,
		ExitCode: 0,
		Duration: 0,
	}
	return result, domain.ResultTypeCommand, nil
}
