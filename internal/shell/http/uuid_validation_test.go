package http

import (
	"testing"

	"github.com/google/uuid"
)

func TestValidateUUID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "Valid UUID v4",
			input:    "550e8400-e29b-41d4-a716-446655440000",
			expected: true,
		},
		{
			name:     "Valid UUID from uuid.New()",
			input:    uuid.New().String(),
			expected: true,
		},
		{
			name:     "Invalid - not a UUID",
			input:    "not-a-uuid",
			expected: false,
		},
		{
			name:     "Invalid - empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "Invalid - numeric only",
			input:    "12345",
			expected: false,
		},
		{
			name:     "Invalid - wrong format",
			input:    "550e8400-e29b-41d4-a716",
			expected: false,
		},
		{
			name:     "Invalid - too many segments",
			input:    "550e8400-e29b-41d4-a716-446655440000-extra",
			expected: false,
		},
		{
			name:     "Valid - lowercase",
			input:    "550e8400-e29b-41d4-a716-446655440000",
			expected: true,
		},
		{
			name:     "Valid - uppercase",
			input:    "550E8400-E29B-41D4-A716-446655440000",
			expected: true,
		},
		{
			name:     "Valid - mixed case",
			input:    "550e8400-E29B-41d4-A716-446655440000",
			expected: true,
		},
		{
			name:     "Invalid - contains spaces",
			input:    "550e8400 e29b 41d4 a716 446655440000",
			expected: false,
		},
		{
			name:     "Invalid - SQL injection attempt",
			input:    "1' OR '1'='1",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validateUUID(tt.input)
			if result != tt.expected {
				t.Errorf("validateUUID(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestErrorInvalidUUID(t *testing.T) {
	tests := []struct {
		name      string
		paramName string
		value     string
	}{
		{
			name:      "Job ID",
			paramName: "job ID",
			value:     "not-a-uuid",
		},
		{
			name:      "Run ID",
			paramName: "run ID",
			value:     "12345",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := errorInvalidUUID(tt.paramName, tt.value)

			if err.Status != "400" {
				t.Errorf("errorInvalidUUID().Status = %q, want %q", err.Status, "400")
			}

			if err.Title != "Invalid UUID Format" {
				t.Errorf("errorInvalidUUID().Title = %q, want %q", err.Title, "Invalid UUID Format")
			}

			expectedDetail := "The " + tt.paramName + " parameter '" + tt.value + "' is not a valid UUID"
			if err.Detail != expectedDetail {
				t.Errorf("errorInvalidUUID().Detail = %q, want %q", err.Detail, expectedDetail)
			}
		})
	}
}
