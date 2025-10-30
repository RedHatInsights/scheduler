package usecases

import (
	"time"

	"insights-scheduler/internal/core/domain"
)

type DefaultSchedulingService struct{}

func NewDefaultSchedulingService() *DefaultSchedulingService {
	return &DefaultSchedulingService{}
}

func (s *DefaultSchedulingService) ShouldRun(job domain.Job, currentTime time.Time) bool {
	if job.LastRun == nil {
		return true
	}

	switch job.Schedule {
	case domain.Schedule10Minutes:
		return currentTime.Sub(*job.LastRun) >= 10*time.Minute
	case domain.Schedule1Hour:
		return currentTime.Sub(*job.LastRun) >= time.Hour
	case domain.Schedule1Day:
		return currentTime.Sub(*job.LastRun) >= 24*time.Hour
	case domain.Schedule1Month:
		return s.shouldRunMonthly(job.LastRun, &currentTime)
	default:
		return false
	}
}

func (s *DefaultSchedulingService) shouldRunMonthly(lastRun *time.Time, currentTime *time.Time) bool {
	if lastRun == nil {
		return true
	}

	lastYear, lastMonth, _ := lastRun.Date()
	currentYear, currentMonth, _ := currentTime.Date()

	if currentYear > lastYear {
		return true
	}

	if currentYear == lastYear && currentMonth > lastMonth {
		return true
	}

	return false
}
