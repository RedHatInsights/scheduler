package domain

import (
	"errors"
	"testing"
)

func TestJobFilterValidate(t *testing.T) {
	tests := []struct {
		name    string
		filter  JobFilter
		wantErr bool
	}{
		{"empty filter is valid", JobFilter{}, false},
		{"name-only filter is valid", JobFilter{NameContains: "report"}, false},
		{"valid status", JobFilter{Status: "paused"}, false},
		{"valid status with name", JobFilter{Status: "scheduled", NameContains: "x"}, false},
		{"unknown status rejected", JobFilter{Status: "bogus"}, true},
		{"status is case sensitive", JobFilter{Status: "Paused"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.filter.Validate()
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidStatusFilter) {
					t.Fatalf("expected ErrInvalidStatusFilter, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
