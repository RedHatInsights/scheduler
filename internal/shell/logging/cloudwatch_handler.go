package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

// CloudWatchHandler is a custom slog.Handler that sends logs to AWS CloudWatch Logs.
// It buffers log events and flushes them periodically or when the buffer is full.
type CloudWatchHandler struct {
	client        *cloudwatchlogs.Client
	logGroupName  string
	logStreamName string
	bufferSize    int
	flushInterval time.Duration

	mu            sync.Mutex
	buffer        []types.InputLogEvent
	sequenceToken *string
	stopChan      chan struct{}
	attrs         []slog.Attr
	groups        []string
	level         slog.Level
}

// NewCloudWatchHandler creates a new CloudWatch log handler.
func NewCloudWatchHandler(
	client *cloudwatchlogs.Client,
	logGroupName string,
	logStreamName string,
	bufferSize int,
	flushInterval time.Duration,
	level slog.Level,
) *CloudWatchHandler {
	h := &CloudWatchHandler{
		client:        client,
		logGroupName:  logGroupName,
		logStreamName: logStreamName,
		bufferSize:    bufferSize,
		flushInterval: flushInterval,
		buffer:        make([]types.InputLogEvent, 0, bufferSize),
		stopChan:      make(chan struct{}),
		level:         level,
	}

	// Start background flush goroutine
	go h.periodicFlush()

	return h
}

// Enabled reports whether the handler handles records at the given level.
func (h *CloudWatchHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

// Handle processes a log record and adds it to the buffer.
func (h *CloudWatchHandler) Handle(_ context.Context, r slog.Record) error {
	// Build log message as JSON
	fields := make(map[string]interface{})
	fields["time"] = r.Time.Format(time.RFC3339Nano)
	fields["level"] = r.Level.String()
	fields["msg"] = r.Message

	// Add attributes from record
	r.Attrs(func(a slog.Attr) bool {
		fields[a.Key] = a.Value.Any()
		return true
	})

	// Add attributes from handler (from WithAttrs)
	for _, attr := range h.attrs {
		fields[attr.Key] = attr.Value.Any()
	}

	// Add group prefixes if any
	if len(h.groups) > 0 {
		// Nest fields under group names
		for i := len(h.groups) - 1; i >= 0; i-- {
			fields = map[string]interface{}{h.groups[i]: fields}
		}
	}

	// Marshal to JSON
	message, err := json.Marshal(fields)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal log record to JSON: %v\n", err)
		return err
	}

	// Add to buffer
	h.mu.Lock()
	defer h.mu.Unlock()

	h.buffer = append(h.buffer, types.InputLogEvent{
		Message:   aws.String(string(message)),
		Timestamp: aws.Int64(r.Time.UnixMilli()),
	})

	// Flush if buffer is full
	if len(h.buffer) >= h.bufferSize {
		go h.flush()
	}

	return nil
}

// WithAttrs returns a new Handler whose attributes consist of h's attributes followed by attrs.
func (h *CloudWatchHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandler := &CloudWatchHandler{
		client:        h.client,
		logGroupName:  h.logGroupName,
		logStreamName: h.logStreamName,
		bufferSize:    h.bufferSize,
		flushInterval: h.flushInterval,
		buffer:        h.buffer,
		sequenceToken: h.sequenceToken,
		stopChan:      h.stopChan,
		level:         h.level,
	}
	newHandler.attrs = append(make([]slog.Attr, 0, len(h.attrs)+len(attrs)), h.attrs...)
	newHandler.attrs = append(newHandler.attrs, attrs...)
	newHandler.groups = h.groups
	return newHandler
}

// WithGroup returns a new Handler with the given group appended to the receiver's existing groups.
func (h *CloudWatchHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	newHandler := &CloudWatchHandler{
		client:        h.client,
		logGroupName:  h.logGroupName,
		logStreamName: h.logStreamName,
		bufferSize:    h.bufferSize,
		flushInterval: h.flushInterval,
		buffer:        h.buffer,
		sequenceToken: h.sequenceToken,
		stopChan:      h.stopChan,
		level:         h.level,
		attrs:         h.attrs,
	}
	newHandler.groups = append(make([]string, 0, len(h.groups)+1), h.groups...)
	newHandler.groups = append(newHandler.groups, name)
	return newHandler
}

// flush sends buffered logs to CloudWatch.
// Must be called with h.mu held OR in a goroutine.
func (h *CloudWatchHandler) flush() {
	h.mu.Lock()
	if len(h.buffer) == 0 {
		h.mu.Unlock()
		return
	}

	// Copy buffer and clear it
	events := make([]types.InputLogEvent, len(h.buffer))
	copy(events, h.buffer)
	h.buffer = h.buffer[:0]
	h.mu.Unlock()

	// Send to CloudWatch
	input := &cloudwatchlogs.PutLogEventsInput{
		LogGroupName:  aws.String(h.logGroupName),
		LogStreamName: aws.String(h.logStreamName),
		LogEvents:     events,
		SequenceToken: h.sequenceToken,
	}

	output, err := h.client.PutLogEvents(context.Background(), input)
	if err != nil {
		// Best effort: log to stderr and continue
		fmt.Fprintf(os.Stderr, "Failed to send logs to CloudWatch: %v\n", err)
		return
	}

	// Update sequence token for next batch
	h.mu.Lock()
	h.sequenceToken = output.NextSequenceToken
	h.mu.Unlock()
}

// periodicFlush runs in a background goroutine and flushes logs at the configured interval.
func (h *CloudWatchHandler) periodicFlush() {
	ticker := time.NewTicker(h.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			h.flush()
		case <-h.stopChan:
			return
		}
	}
}

// Close flushes any remaining logs and stops the background flush goroutine.
func (h *CloudWatchHandler) Close() error {
	close(h.stopChan)
	h.flush()
	return nil
}
