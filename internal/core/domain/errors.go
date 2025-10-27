package domain

import "errors"

var (
	ErrJobNotFound      = errors.New("job not found")
	ErrInvalidSchedule  = errors.New("invalid schedule format")
	ErrInvalidPayload   = errors.New("invalid payload type")
	ErrInvalidStatus    = errors.New("invalid job status")
	ErrInvalidOrgID     = errors.New("invalid or missing org_id")
	ErrJobAlreadyPaused = errors.New("job is already paused")
	ErrJobNotPaused     = errors.New("job is not paused")
)
