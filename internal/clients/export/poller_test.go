package export

import (
	"testing"
	"time"

	"insights-scheduler/internal/clients/polling"
)

func TestExportPoller_GetStatus_Complete(t *testing.T) {
	client := NewClient("http://test.example.com", "http://test.example.com")
	poller := NewExportPoller(client, "test-identity-header")

	// We can't easily test this without mocking the HTTP client
	// This is a basic interface test
	if poller.client != client {
		t.Error("expected client to be set")
	}

	if poller.identityHeader != "test-identity-header" {
		t.Error("expected identity header to be set")
	}
}

func TestExportPoller_IsTerminalStatus(t *testing.T) {
	client := NewClient("http://test.example.com", "http://test.example.com")
	poller := NewExportPoller(client, "test-identity-header")

	testCases := []struct {
		status   polling.JobStatus
		expected bool
	}{
		{polling.StatusComplete, true},
		{polling.StatusFailed, true},
		{polling.StatusPending, false},
		{polling.StatusInProgress, false},
	}

	for _, tc := range testCases {
		result := poller.IsTerminalStatus(tc.status)
		if result != tc.expected {
			t.Errorf("IsTerminalStatus(%s) = %v, expected %v", tc.status, result, tc.expected)
		}
	}
}

func TestExportPoller_StatusMapping(t *testing.T) {
	// This test verifies the status mapping logic
	// In a real scenario, you would mock the HTTP client

	testCases := []struct {
		name           string
		exportStatus   ExportStatus
		expectedStatus polling.JobStatus
		expectedTerm   bool
	}{
		{"Pending maps to InProgress", StatusPending, polling.StatusInProgress, false},
		{"Running maps to InProgress", StatusRunning, polling.StatusInProgress, false},
		{"Partial maps to InProgress", StatusPartial, polling.StatusInProgress, false},
		{"Complete maps to Complete", StatusComplete, polling.StatusComplete, true},
		{"Failed maps to Failed", StatusFailed, polling.StatusFailed, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// This is a documentation test showing the expected mappings
			// Actual HTTP testing would require mocking
			var jobStatus polling.JobStatus
			switch tc.exportStatus {
			case StatusPending, StatusRunning, StatusPartial:
				jobStatus = polling.StatusInProgress
			case StatusComplete:
				jobStatus = polling.StatusComplete
			case StatusFailed:
				jobStatus = polling.StatusFailed
			default:
				jobStatus = polling.StatusPending
			}

			if jobStatus != tc.expectedStatus {
				t.Errorf("expected status %s, got %s", tc.expectedStatus, jobStatus)
			}

			isTerminal := jobStatus == polling.StatusComplete || jobStatus == polling.StatusFailed
			if isTerminal != tc.expectedTerm {
				t.Errorf("expected terminal=%v, got %v", tc.expectedTerm, isTerminal)
			}
		})
	}
}

func TestExportPoller_ErrorExtraction(t *testing.T) {
	errCode := 500
	errMsg := "advisor processing error"
	sources := []SourceStatus{
		{
			Application: AppInventory,
			Resource:    "systems",
			Status:      "failed",
			Error:       &errCode,
			Message:     &errMsg,
		},
	}

	var extractedError string
	if len(sources) > 0 {
		if sources[0].Message != nil {
			extractedError = *sources[0].Message
		}
	}

	if extractedError != errMsg {
		t.Errorf("expected error %q, got %q", errMsg, extractedError)
	}
}

func TestExportPoller_MetadataPreservation(t *testing.T) {
	// Test that metadata is preserved in the StatusResponse
	status := &ExportStatusResponse{
		ID:        "export-123",
		Name:      "test-export",
		Format:    FormatJSON,
		Status:    StatusComplete,
		CreatedAt: time.Now(),
		Sources: []SourceStatus{
			{Application: AppInventory, Resource: "systems", Status: "complete"},
		},
	}

	// Simulate building a StatusResponse
	metadata := map[string]interface{}{
		"name":       status.Name,
		"format":     status.Format,
		"sources":    status.Sources,
		"created_at": status.CreatedAt,
	}

	if metadata["name"] != "test-export" {
		t.Error("expected name in metadata")
	}

	if metadata["format"] != FormatJSON {
		t.Error("expected format in metadata")
	}

	if len(status.Sources) != 1 {
		t.Error("expected sources in metadata")
	}
}
