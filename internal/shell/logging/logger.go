package logging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"insights-scheduler/internal/config"
)

// InitializeLogger creates the root logger based on configuration.
// Returns the logger, a cleanup function, and any error.
func InitializeLogger(cfg *config.Config) (*slog.Logger, func(), error) {
	level := parseLogLevel(cfg.LogLevel)

	// If CloudWatch is disabled, use console JSON handler
	if !cfg.CloudWatch.Enabled {
		handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: level,
		})
		logger := slog.New(handler)
		return logger, func() {}, nil
	}

	// Create AWS config
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(cfg.CloudWatch.Region),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create CloudWatch client
	cwClient := cloudwatchlogs.NewFromConfig(awsCfg)

	// Ensure log group and stream exist
	if err := ensureLogGroupAndStream(cwClient, cfg.CloudWatch.LogGroupName, cfg.CloudWatch.LogStreamName); err != nil {
		return nil, nil, fmt.Errorf("failed to ensure CloudWatch log group/stream: %w", err)
	}

	// Create CloudWatch handler
	cwHandler := NewCloudWatchHandler(
		cwClient,
		cfg.CloudWatch.LogGroupName,
		cfg.CloudWatch.LogStreamName,
		cfg.CloudWatch.BufferSize,
		cfg.CloudWatch.FlushInterval,
		level,
	)

	var handler slog.Handler = cwHandler

	// If dual output is enabled, create multi-handler
	if cfg.CloudWatch.ConsoleOutput {
		consoleHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: level,
		})
		handler = NewMultiHandler(consoleHandler, cwHandler)
	}

	logger := slog.New(handler)

	// Cleanup function flushes CloudWatch handler
	cleanup := func() {
		if err := cwHandler.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing CloudWatch handler: %v\n", err)
		}
	}

	return logger, cleanup, nil
}

// NewRequestLogger creates an HTTP request-scoped logger with context fields.
func NewRequestLogger(base *slog.Logger, requestID, jobID, orgID, userID string) *slog.Logger {
	logger := base
	if requestID != "" {
		logger = logger.With(slog.String("request_id", requestID))
	}
	if jobID != "" {
		logger = logger.With(slog.String("job_id", jobID))
	}
	if orgID != "" {
		logger = logger.With(slog.String("org_id", orgID))
	}
	if userID != "" {
		logger = logger.With(slog.String("user_id", userID))
	}
	return logger
}

// NewJobExecutionLogger creates a job execution-scoped logger with context fields.
func NewJobExecutionLogger(base *slog.Logger, jobID, runID, orgID, userID string) *slog.Logger {
	logger := base
	if jobID != "" {
		logger = logger.With(slog.String("job_id", jobID))
	}
	if runID != "" {
		logger = logger.With(slog.String("run_id", runID))
	}
	if orgID != "" {
		logger = logger.With(slog.String("org_id", orgID))
	}
	if userID != "" {
		logger = logger.With(slog.String("user_id", userID))
	}
	return logger
}

// parseLogLevel converts a string log level to slog.Level.
func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// ensureLogGroupAndStream creates CloudWatch log group and stream if they don't exist.
func ensureLogGroupAndStream(client *cloudwatchlogs.Client, logGroupName, logStreamName string) error {
	ctx := context.Background()

	// Check if log group exists
	describeGroupsOutput, err := client.DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{
		LogGroupNamePrefix: aws.String(logGroupName),
	})
	if err != nil {
		return fmt.Errorf("failed to describe log groups: %w", err)
	}

	// Create log group if it doesn't exist
	groupExists := false
	for _, group := range describeGroupsOutput.LogGroups {
		if group.LogGroupName != nil && *group.LogGroupName == logGroupName {
			groupExists = true
			break
		}
	}

	if !groupExists {
		_, err = client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{
			LogGroupName: aws.String(logGroupName),
		})
		if err != nil {
			return fmt.Errorf("failed to create log group: %w", err)
		}
	}

	// Check if log stream exists
	describeStreamsOutput, err := client.DescribeLogStreams(ctx, &cloudwatchlogs.DescribeLogStreamsInput{
		LogGroupName:        aws.String(logGroupName),
		LogStreamNamePrefix: aws.String(logStreamName),
	})
	if err != nil {
		return fmt.Errorf("failed to describe log streams: %w", err)
	}

	// Create log stream if it doesn't exist
	streamExists := false
	for _, stream := range describeStreamsOutput.LogStreams {
		if stream.LogStreamName != nil && *stream.LogStreamName == logStreamName {
			streamExists = true
			break
		}
	}

	if !streamExists {
		_, err = client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{
			LogGroupName:  aws.String(logGroupName),
			LogStreamName: aws.String(logStreamName),
		})
		if err != nil {
			// ResourceAlreadyExistsException is OK (race condition)
			var alreadyExists *types.ResourceAlreadyExistsException
			if !errors.As(err, &alreadyExists) {
				return fmt.Errorf("failed to create log stream: %w", err)
			}
		}
	}

	return nil
}
