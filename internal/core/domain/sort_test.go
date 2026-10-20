package domain

import (
	"errors"
	"testing"
)

func TestParseSortSpec(t *testing.T) {
	def := SortSpec{Field: "created_at", Direction: SortDesc}

	tests := []struct {
		name      string
		raw       string
		want      SortSpec
		wantErr   bool
		errTarget error
	}{
		{
			name: "empty returns default",
			raw:  "",
			want: def,
		},
		{
			name: "whitespace returns default",
			raw:  "   ",
			want: def,
		},
		{
			name: "field only defaults to ascending",
			raw:  "name",
			want: SortSpec{Field: "name", Direction: SortAsc},
		},
		{
			name: "field and desc",
			raw:  "name:desc",
			want: SortSpec{Field: "name", Direction: SortDesc},
		},
		{
			name: "field and asc",
			raw:  "status:asc",
			want: SortSpec{Field: "status", Direction: SortAsc},
		},
		{
			name: "direction is case insensitive",
			raw:  "name:DESC",
			want: SortSpec{Field: "name", Direction: SortDesc},
		},
		{
			name: "surrounding whitespace trimmed",
			raw:  "  name : desc  ",
			want: SortSpec{Field: "name", Direction: SortDesc},
		},
		{
			name:      "unknown field rejected",
			raw:       "org_id:asc",
			wantErr:   true,
			errTarget: ErrInvalidSort,
		},
		{
			name:      "invalid direction rejected",
			raw:       "name:sideways",
			wantErr:   true,
			errTarget: ErrInvalidSort,
		},
		{
			name:      "injection attempt rejected as unknown field",
			raw:       "name; DROP TABLE jobs--:asc",
			wantErr:   true,
			errTarget: ErrInvalidSort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSortSpec(tt.raw, JobSortableFields, def)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (result %+v)", got)
				}
				if tt.errTarget != nil && !errors.Is(err, tt.errTarget) {
					t.Fatalf("expected error wrapping %v, got %v", tt.errTarget, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseSortSpecUsesResourceAllowlist(t *testing.T) {
	// A field valid for jobs but not for job runs must be rejected against the
	// job-run allowlist.
	_, err := ParseSortSpec("name:asc", JobRunSortableFields, DefaultJobRunSort)
	if !errors.Is(err, ErrInvalidSort) {
		t.Fatalf("expected ErrInvalidSort for job field against run allowlist, got %v", err)
	}

	got, err := ParseSortSpec("start_time:asc", JobRunSortableFields, DefaultJobRunSort)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != (SortSpec{Field: "start_time", Direction: SortAsc}) {
		t.Fatalf("unexpected spec: %+v", got)
	}
}
