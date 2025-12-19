package usecases

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"insights-scheduler/internal/core/domain"
)

// ScheduleCalculator calculates next run times for jobs
type ScheduleCalculator struct{}

// NewScheduleCalculator creates a new schedule calculator
func NewScheduleCalculator() *ScheduleCalculator {
	return &ScheduleCalculator{}
}

// CalculateNextRun calculates the next run time for a job based on its schedule and last run
func (sc *ScheduleCalculator) CalculateNextRun(job domain.Job, currentTime time.Time) (time.Time, error) {
	// Use UTC for all calculations
	now := currentTime.UTC()

	specParser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := specParser.Parse(string(job.Schedule))

	if err != nil {
		fmt.Println("Unable to parse schedule: ", job.Schedule)
		return now, err
	}

	nextScheduledRun := sched.Next(now)

	return nextScheduledRun, nil

	/*
		// If job has never run, schedule it to run now
		if job.LastRun == nil {
			return now, nil
		}

		lastRun := job.LastRun.UTC()

		switch job.Schedule {
		case domain.Schedule10Minutes:
			return lastRun.Add(10 * time.Minute), nil

		case domain.Schedule1Hour:
			return lastRun.Add(1 * time.Hour), nil

		case domain.Schedule1Day:
			// Schedule for next day at same time
			return lastRun.Add(24 * time.Hour), nil

		case domain.Schedule1Month:
			// Schedule for same day next month
			return sc.addMonth(lastRun), nil

		default:
			// For custom cron expressions, parse and calculate
			// For now, we'll treat unknown schedules as errors
			return time.Time{}, fmt.Errorf("unsupported schedule format: %s", job.Schedule)
		}
	*/
}

// addMonth adds one month to the given time
func (sc *ScheduleCalculator) addMonth(t time.Time) time.Time {
	year, month, day := t.Date()
	hour, min, sec := t.Clock()
	nsec := t.Nanosecond()
	loc := t.Location()

	// Add one month
	month++
	if month > 12 {
		month = 1
		year++
	}

	// Handle day overflow (e.g., Jan 31 + 1 month = Feb 28/29)
	daysInMonth := time.Date(year, month+1, 0, 0, 0, 0, 0, loc).Day()
	if day > daysInMonth {
		day = daysInMonth
	}

	return time.Date(year, month, day, hour, min, sec, nsec, loc)
}

// GetInitialRunTime gets the time a job should first run
func (sc *ScheduleCalculator) GetInitialRunTime(job domain.Job, currentTime time.Time) time.Time {
	// New jobs run immediately
	if job.LastRun == nil {
		return currentTime.UTC()
	}

	// If job has run before, calculate next run from last run
	nextRun, err := sc.CalculateNextRun(job, currentTime)
	if err != nil {
		// If we can't calculate, run now
		return currentTime.UTC()
	}

	return nextRun
}
