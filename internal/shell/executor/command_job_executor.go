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
func (e *CommandJobExecutor) Execute(job domain.Job) error {
	command, ok := job.Payload.Details["command"].(string)
	if !ok {
		command = "unknown"
	}
	log.Printf("Executing command: %s", command)
	return nil
}
