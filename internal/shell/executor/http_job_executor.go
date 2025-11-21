package executor

import (
	"log"

	"insights-scheduler/internal/core/domain"
)

// HTTPJobExecutor handles http_request payload type jobs
type HTTPJobExecutor struct{}

// NewHTTPJobExecutor creates a new HTTPJobExecutor
func NewHTTPJobExecutor() *HTTPJobExecutor {
	return &HTTPJobExecutor{}
}

// Execute executes an HTTP request job
func (e *HTTPJobExecutor) Execute(job domain.Job) error {
	url, _ := job.Payload.Details["url"].(string)
	method, _ := job.Payload.Details["method"].(string)
	if method == "" {
		method = "GET"
	}
	if url == "" {
		url = "unknown"
	}
	log.Printf("Executing HTTP %s request to %s", method, url)
	return nil
}
