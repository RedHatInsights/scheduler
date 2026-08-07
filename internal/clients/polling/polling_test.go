package polling

import (
	"context"
	"errors"
	"testing"
	"time"
)

// mockPoller is a test implementation of the Poller interface
type mockPoller struct {
	responses      []*StatusResponse
	errors         []error
	callCount      int
	delayPerCall   time.Duration
	terminalStatus JobStatus
}

func (m *mockPoller) GetStatus(ctx context.Context, jobID string) (*StatusResponse, error) {
	if m.callCount >= len(m.responses) {
		return nil, errors.New("no more responses configured")
	}

	if m.delayPerCall > 0 {
		time.Sleep(m.delayPerCall)
	}

	response := m.responses[m.callCount]
	var err error
	if m.callCount < len(m.errors) {
		err = m.errors[m.callCount]
	}
	m.callCount++

	return response, err
}

func (m *mockPoller) IsTerminalStatus(status JobStatus) bool {
	return status == StatusComplete || status == StatusFailed
}

func TestPoll_SuccessfulCompletion(t *testing.T) {
	poller := &mockPoller{
		responses: []*StatusResponse{
			{ID: "job1", Status: StatusPending, IsTerminal: false},
			{ID: "job1", Status: StatusInProgress, IsTerminal: false},
			{ID: "job1", Status: StatusComplete, IsTerminal: true},
		},
		errors: []error{nil, nil, nil},
	}

	config := Config{
		MaxRetries:   10,
		PollInterval: 10 * time.Millisecond,
		Timeout:      5 * time.Second,
	}

	result, err := Poll(context.Background(), poller, "job1", config)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result.Status != StatusComplete {
		t.Errorf("expected status Complete, got: %s", result.Status)
	}

	if poller.callCount != 3 {
		t.Errorf("expected 3 calls, got: %d", poller.callCount)
	}
}

func TestPoll_ImmediateCompletion(t *testing.T) {
	poller := &mockPoller{
		responses: []*StatusResponse{
			{ID: "job1", Status: StatusComplete, IsTerminal: true},
		},
		errors: []error{nil},
	}

	config := Config{
		MaxRetries:   10,
		PollInterval: 10 * time.Millisecond,
		Timeout:      5 * time.Second,
	}

	result, err := Poll(context.Background(), poller, "job1", config)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result.Status != StatusComplete {
		t.Errorf("expected status Complete, got: %s", result.Status)
	}

	if poller.callCount != 1 {
		t.Errorf("expected 1 call, got: %d", poller.callCount)
	}
}

func TestPoll_FailureStatus(t *testing.T) {
	poller := &mockPoller{
		responses: []*StatusResponse{
			{ID: "job1", Status: StatusPending, IsTerminal: false},
			{ID: "job1", Status: StatusFailed, Error: "job execution failed", IsTerminal: true},
		},
		errors: []error{nil, nil},
	}

	config := Config{
		MaxRetries:   10,
		PollInterval: 10 * time.Millisecond,
		Timeout:      5 * time.Second,
	}

	result, err := Poll(context.Background(), poller, "job1", config)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result.Status != StatusFailed {
		t.Errorf("expected status Failed, got: %s", result.Status)
	}

	if result.Error != "job execution failed" {
		t.Errorf("expected error message, got: %s", result.Error)
	}

	if poller.callCount != 2 {
		t.Errorf("expected 2 calls, got: %d", poller.callCount)
	}
}

func TestPoll_MaxRetriesExceeded(t *testing.T) {
	poller := &mockPoller{
		responses: []*StatusResponse{
			{ID: "job1", Status: StatusPending, IsTerminal: false},
			{ID: "job1", Status: StatusInProgress, IsTerminal: false},
			{ID: "job1", Status: StatusInProgress, IsTerminal: false},
		},
		errors: []error{nil, nil, nil},
	}

	config := Config{
		MaxRetries:   3,
		PollInterval: 10 * time.Millisecond,
		Timeout:      5 * time.Second,
	}

	result, err := Poll(context.Background(), poller, "job1", config)

	if err == nil {
		t.Fatal("expected error for max retries exceeded")
	}

	if result != nil {
		t.Errorf("expected nil result, got: %v", result)
	}

	if poller.callCount != 3 {
		t.Errorf("expected 3 calls, got: %d", poller.callCount)
	}

	expectedError := "job did not complete after 3 polling attempts"
	if err.Error() != expectedError {
		t.Errorf("expected error: %s, got: %s", expectedError, err.Error())
	}
}

func TestPoll_ContextTimeout(t *testing.T) {
	poller := &mockPoller{
		responses: []*StatusResponse{
			{ID: "job1", Status: StatusPending, IsTerminal: false},
			{ID: "job1", Status: StatusInProgress, IsTerminal: false},
		},
		errors:       []error{nil, nil},
		delayPerCall: 100 * time.Millisecond,
	}

	config := Config{
		MaxRetries:   10,
		PollInterval: 10 * time.Millisecond,
		Timeout:      150 * time.Millisecond, // Will timeout after first call + one interval
	}

	result, err := Poll(context.Background(), poller, "job1", config)

	if err == nil {
		t.Fatal("expected timeout error")
	}

	if result != nil {
		t.Errorf("expected nil result, got: %v", result)
	}

	if !errors.Is(err, context.DeadlineExceeded) && err.Error() != "polling timed out after 150ms" {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

func TestPoll_GetStatusError(t *testing.T) {
	poller := &mockPoller{
		responses: []*StatusResponse{
			{ID: "job1", Status: StatusPending, IsTerminal: false},
		},
		errors: []error{errors.New("network error")},
	}

	config := Config{
		MaxRetries:   3,
		PollInterval: 10 * time.Millisecond,
		Timeout:      5 * time.Second,
	}

	result, err := Poll(context.Background(), poller, "job1", config)

	if err == nil {
		t.Fatal("expected error from GetStatus")
	}

	if result != nil {
		t.Errorf("expected nil result, got: %v", result)
	}

	expectedError := "failed to get status (attempt 1/3): network error"
	if err.Error() != expectedError {
		t.Errorf("expected error: %s, got: %s", expectedError, err.Error())
	}
}

func TestPoll_CancelledContext(t *testing.T) {
	poller := &mockPoller{
		responses: []*StatusResponse{
			{ID: "job1", Status: StatusPending, IsTerminal: false},
			{ID: "job1", Status: StatusInProgress, IsTerminal: false},
		},
		errors: []error{nil, nil},
	}

	config := Config{
		MaxRetries:   10,
		PollInterval: 100 * time.Millisecond,
		Timeout:      5 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after first call
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	result, err := Poll(ctx, poller, "job1", config)

	if err == nil {
		t.Fatal("expected context cancellation error")
	}

	if result != nil {
		t.Errorf("expected nil result, got: %v", result)
	}
}

func TestPoll_DefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.MaxRetries != 60 {
		t.Errorf("expected MaxRetries 60, got: %d", config.MaxRetries)
	}

	if config.PollInterval != 5*time.Second {
		t.Errorf("expected PollInterval 5s, got: %v", config.PollInterval)
	}

	if config.Timeout != 10*time.Minute {
		t.Errorf("expected Timeout 10m, got: %v", config.Timeout)
	}
}

func TestPoll_StatusResponseMetadata(t *testing.T) {
	poller := &mockPoller{
		responses: []*StatusResponse{
			{
				ID:         "job1",
				Status:     StatusComplete,
				IsTerminal: true,
				Metadata: map[string]interface{}{
					"foo": "bar",
					"baz": 123,
				},
			},
		},
		errors: []error{nil},
	}

	config := Config{
		MaxRetries:   10,
		PollInterval: 10 * time.Millisecond,
		Timeout:      5 * time.Second,
	}

	result, err := Poll(context.Background(), poller, "job1", config)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result.Metadata["foo"] != "bar" {
		t.Errorf("expected metadata foo=bar, got: %v", result.Metadata["foo"])
	}

	if result.Metadata["baz"] != 123 {
		t.Errorf("expected metadata baz=123, got: %v", result.Metadata["baz"])
	}
}
