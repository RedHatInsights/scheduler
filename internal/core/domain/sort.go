package domain

import (
	"fmt"
	"strings"
)

// SortDirection represents the ordering direction of a sort.
type SortDirection string

const (
	SortAsc  SortDirection = "asc"
	SortDesc SortDirection = "desc"
)

// SortSpec describes a validated sort request: a field name that has already
// been checked against an allowlist, plus a direction. It is a pure value with
// no knowledge of storage columns.
type SortSpec struct {
	Field     string
	Direction SortDirection
}

// JobSortableFields is the allowlist of user-facing field names that jobs may be
// sorted by. Keys are the names accepted in the API's `sort_by` parameter. Only
// fields exposed on the job API response are sortable (plus created_at, the
// historical default order); internal-only fields such as consecutive_failures
// and last_failed_at are intentionally excluded.
var JobSortableFields = map[string]struct{}{
	"name":        {},
	"status":      {},
	"created_at":  {},
	"next_run_at": {},
	"last_run_at": {},
}

// JobRunSortableFields is the allowlist of user-facing field names that job runs
// may be sorted by.
var JobRunSortableFields = map[string]struct{}{
	"start_time": {},
	"end_time":   {},
	"status":     {},
	"created_at": {},
}

// DefaultJobSort preserves the historical ordering of the jobs list endpoint.
var DefaultJobSort = SortSpec{Field: "created_at", Direction: SortDesc}

// DefaultJobRunSort preserves the historical ordering of the job runs endpoints.
var DefaultJobRunSort = SortSpec{Field: "start_time", Direction: SortDesc}

// ParseSortSpec parses a `field:direction` sort expression and validates the
// field against the provided allowlist. It is a pure function suitable for the
// functional core.
//
//   - An empty (or whitespace-only) expression returns def with no error.
//   - The direction is optional and defaults to ascending; when present it must
//     be exactly "asc" or "desc" (case-insensitive).
//   - The field must be a member of allowed; otherwise ErrInvalidSort is returned.
//
// Because callers only ever forward the validated Field to storage, and storage
// maps that field to a fixed column literal, no untrusted input reaches SQL.
func ParseSortSpec(raw string, allowed map[string]struct{}, def SortSpec) (SortSpec, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}

	field := raw
	direction := SortAsc

	if idx := strings.Index(raw, ":"); idx >= 0 {
		field = strings.TrimSpace(raw[:idx])
		dirStr := strings.ToLower(strings.TrimSpace(raw[idx+1:]))
		switch SortDirection(dirStr) {
		case SortAsc:
			direction = SortAsc
		case SortDesc:
			direction = SortDesc
		default:
			return SortSpec{}, fmt.Errorf("%w: invalid direction %q (expected 'asc' or 'desc')", ErrInvalidSort, dirStr)
		}
	}

	if _, ok := allowed[field]; !ok {
		return SortSpec{}, fmt.Errorf("%w: unknown sort field %q", ErrInvalidSort, field)
	}

	return SortSpec{Field: field, Direction: direction}, nil
}
