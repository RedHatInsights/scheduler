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
	// Cast payload to map[string]interface{}
	payloadMap, ok := job.Payload.(map[string]interface{})
	if !ok {
		log.Printf("Executing HTTP request: unknown (payload is not a map)")
		return nil
	}

	url, _ := payloadMap["url"].(string)
	method, _ := payloadMap["method"].(string)
	if method == "" {
		method = "GET"
	}
	if url == "" {
		url = "unknown"
	}
	log.Printf("Executing HTTP %s request to %s", method, url)
	return nil
}
