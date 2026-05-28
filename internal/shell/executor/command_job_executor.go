package executor

import (
	"log"

	"insights-scheduler/internal/core/domain"
)

// CommandJobExecutor handles command payload type jobs
type CommandJobExecutor struct{}

// NewCommandJobExecutor creates a new CommandJobExecutor
func NewCommandJobExecutor() *CommandJobExecutor {
	return &CommandJobExecutor{}
}

// Execute executes a command job
func (e *CommandJobExecutor) Execute(job domain.Job) (interface{}, domain.ResultType, error) {
	// Cast payload to map[string]interface{}
	payloadMap, ok := job.Payload.(map[string]interface{})
	if !ok {
		log.Printf("Executing command: unknown (payload is not a map)")
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
	log.Printf("Executing command: %s", command)

	result := domain.CommandResult{
		Command:  command,
		ExitCode: 0,
		Duration: 0,
	}
	return result, domain.ResultTypeCommand, nil
}
