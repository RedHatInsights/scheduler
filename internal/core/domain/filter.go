package domain

import "fmt"

// JobFilter describes optional server-side filters for listing jobs. A zero-value
// field means "no filter on this dimension", so the zero JobFilter matches all
// jobs. It is a pure value with no knowledge of storage.
type JobFilter struct {
	// Status, when non-empty, restricts results to jobs with an exact status match.
	Status string
	// NameContains, when non-empty, restricts results to jobs whose name contains
	// this substring (case-insensitive).
	NameContains string
}

// Validate checks the filter's field values. An empty Status is allowed (no
// filter); a non-empty Status must be a recognized job status. NameContains is
// treated as an opaque substring and needs no validation.
func (f JobFilter) Validate() error {
	if f.Status != "" && !IsValidStatus(f.Status) {
		return fmt.Errorf("%w: %q", ErrInvalidStatusFilter, f.Status)
	}
	return nil
}
