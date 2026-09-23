package scheduler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
)

// TestRedisScheduler_TimezoneAwareness verifies that jobs are scheduled
// with correct timezone awareness
func TestRedisScheduler_TimezoneAwareness(t *testing.T) {
	// Start miniredis
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	// Create Redis scheduler
	redisCfg := config.RedisConfig{
		Host: mr.Host(),
		Port: mr.Server().Addr().Port,
	}

	scheduler, err := NewRedisScheduler(
		redisCfg,
		&mockJobExecutor{},
		&mockJobRepository{jobs: make(map[string]domain.Job)},
		10*time.Second,
		10,
		2*time.Minute,
	)
	require.NoError(t, err)
	defer scheduler.Close()

	t.Run("schedule with America/New_York timezone", func(t *testing.T) {
		// Create a job scheduled for 7:30 PM every day in America/New_York
		// At the time of this test (approximately 2026-09-22 23:07 UTC),
		// which is 2026-09-22 19:07 EDT, the next run should be
		// 2026-09-22 19:30 EDT = 2026-09-22 23:30 UTC (same day, 23 minutes later)
		job := domain.Job{
			ID:       "test-job-ny",
			Name:     "NY Timezone Job",
			OrgID:    "org-123",
			UserID:   "user-123",
			Schedule: "30 19 */1 * *", // 7:30 PM every day
			Timezone: "America/New_York",
			Type:     domain.PayloadMessage,
			Payload:  map[string]interface{}{"test": "data"},
			Status:   domain.StatusScheduled,
		}

		// Calculate expected next run using the same logic as job_service
		loc, err := time.LoadLocation("America/New_York")
		require.NoError(t, err)

		// Simulate a time close to the log example: 2026-09-22 23:07 UTC
		testTime := time.Date(2026, 9, 22, 23, 7, 0, 0, time.UTC)
		nowInTz := testTime.In(loc) // Should be 19:07 EDT

		// The next 19:30 should be on the same day (23 minutes later)
		expectedNextInTz := time.Date(2026, 9, 22, 19, 30, 0, 0, loc)
		expectedNextUTC := expectedNextInTz.UTC()

		t.Logf("Test time UTC: %s", testTime.Format(time.RFC3339))
		t.Logf("Test time in NY: %s", nowInTz.Format(time.RFC3339))
		t.Logf("Expected next run in NY: %s", expectedNextInTz.Format(time.RFC3339))
		t.Logf("Expected next run UTC: %s", expectedNextUTC.Format(time.RFC3339))

		// Calculate using the scheduler's helper function
		actualNextRun, err := scheduler.calculateNextRunWithTimezone(
			string(job.Schedule),
			job.Timezone,
			testTime,
		)
		require.NoError(t, err)

		t.Logf("Actual next run UTC: %s", actualNextRun.Format(time.RFC3339))

		// Verify the calculation matches expected
		assert.Equal(t, expectedNextUTC.Unix(), actualNextRun.Unix(),
			"Next run should be 2026-09-22 23:30 UTC (19:30 EDT)")

		// Now test ScheduleJob with pre-calculated NextRunAt
		job.NextRunAt = &actualNextRun
		err = scheduler.ScheduleJob(job)
		require.NoError(t, err)

		// Verify the job was scheduled in Redis with correct time
		jobData, err := scheduler.client.Get(scheduler.ctx, jobDataKeyPrefix+job.ID).Result()
		require.NoError(t, err)

		var scheduledJob ScheduledJob
		err = scheduledJob.UnmarshalJSON([]byte(jobData))
		require.NoError(t, err)

		assert.Equal(t, actualNextRun.Unix(), scheduledJob.NextRun.Unix(),
			"Job should be scheduled with timezone-aware next run")
	})

	t.Run("schedule with UTC timezone", func(t *testing.T) {
		// For UTC, the behavior should be straightforward
		job := domain.Job{
			ID:       "test-job-utc",
			Name:     "UTC Timezone Job",
			OrgID:    "org-123",
			UserID:   "user-123",
			Schedule: "0 12 */1 * *", // Noon every day
			Timezone: "UTC",
			Type:     domain.PayloadMessage,
			Payload:  map[string]interface{}{"test": "data"},
			Status:   domain.StatusScheduled,
		}

		// Simulate a time: 2026-09-23 11:06 UTC (before noon)
		testTime := time.Date(2026, 9, 23, 11, 6, 0, 0, time.UTC)
		expectedNext := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

		actualNextRun, err := scheduler.calculateNextRunWithTimezone(
			string(job.Schedule),
			job.Timezone,
			testTime,
		)
		require.NoError(t, err)

		assert.Equal(t, expectedNext.Unix(), actualNextRun.Unix(),
			"Next run should be today at noon UTC")
	})

	t.Run("schedule with Asia/Tokyo timezone", func(t *testing.T) {
		// Test with a timezone ahead of UTC
		job := domain.Job{
			ID:       "test-job-tokyo",
			Name:     "Tokyo Timezone Job",
			OrgID:    "org-123",
			UserID:   "user-123",
			Schedule: "0 9 */1 * *", // 9 AM every day in Tokyo
			Timezone: "Asia/Tokyo",
			Type:     domain.PayloadMessage,
			Payload:  map[string]interface{}{"test": "data"},
			Status:   domain.StatusScheduled,
		}

		loc, err := time.LoadLocation("Asia/Tokyo")
		require.NoError(t, err)

		// Current time: 2026-09-23 08:30 JST (before 9 AM)
		testTime := time.Date(2026, 9, 23, 8, 30, 0, 0, loc).UTC()
		expectedNext := time.Date(2026, 9, 23, 9, 0, 0, 0, loc).UTC()

		actualNextRun, err := scheduler.calculateNextRunWithTimezone(
			string(job.Schedule),
			job.Timezone,
			testTime,
		)
		require.NoError(t, err)

		assert.Equal(t, expectedNext.Unix(), actualNextRun.Unix(),
			"Next run should be today at 9 AM JST")
	})
}

// Helper to unmarshal ScheduledJob from JSON
func (sj *ScheduledJob) UnmarshalJSON(data []byte) error {
	type Alias ScheduledJob
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(sj),
	}
	return json.Unmarshal(data, &aux)
}
