package scheduler

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	JobsTimedOutTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "scheduler",
		Subsystem: "redis",
		Name:      "jobs_timed_out_total",
		Help:      "Total number of jobs that exceeded execution timeout",
	})

	ConcurrentJobsGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "scheduler",
		Subsystem: "redis",
		Name:      "concurrent_jobs",
		Help:      "Current number of jobs executing concurrently",
	})

	WorkerPoolUtilization = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "scheduler",
		Subsystem: "redis",
		Name:      "worker_pool_utilization_percent",
		Help:      "Percentage of worker pool slots currently in use (0-100)",
	})

	// ExportPollTimeoutsTotal counts in-flight export runs that exceeded the
	// configured max-age (SCHEDULER_EXPORT_POLL_MAX_AGE) and were marked failed.
	// Distinct from redis.jobs_timed_out_total, which tracks the job kick-off
	// (create-phase) execution timeout.
	ExportPollTimeoutsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "scheduler",
		Subsystem: "export",
		Name:      "poll_timeouts_total",
		Help:      "Total number of in-flight export runs that exceeded the max-age timeout and were marked failed",
	})

	// ExportInFlightRuns reflects the number of in-flight export runs observed in
	// the most recent poll scan, so operators can watch how close volume gets to
	// the concurrency limit and how many runs are aging toward the timeout.
	ExportInFlightRuns = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "scheduler",
		Subsystem: "export",
		Name:      "in_flight_runs",
		Help:      "Number of in-flight export runs observed in the most recent poll scan",
	})

	// DBSyncDuration tracks how long database to Redis sync operations take.
	// Use to measure performance improvement from lookahead window optimization
	// and alert on slow syncs that could delay job execution.
	DBSyncDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "scheduler",
		Subsystem: "db_sync",
		Name:      "duration_seconds",
		Help:      "Duration of database to Redis sync operations",
		Buckets:   prometheus.ExponentialBuckets(0.001, 2, 15), // 1ms to ~16s
	})

	// DBSyncJobsLoaded tracks the number of jobs loaded from the database during sync.
	// Use to track correlation between job count and sync duration and monitor
	// the effectiveness of the lookahead window filter.
	DBSyncJobsLoaded = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "scheduler",
		Subsystem: "db_sync",
		Name:      "jobs_loaded",
		Help:      "Number of jobs loaded from database during sync",
		Buckets:   prometheus.ExponentialBuckets(1, 2, 15), // 1 to ~16k jobs
	})

	// DBSyncTotal counts database sync operations by type and outcome.
	// Labels: operation (startup, periodic), status (success, error)
	DBSyncTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "scheduler",
		Subsystem: "db_sync",
		Name:      "operations_total",
		Help:      "Total number of database sync operations",
	}, []string{"operation", "status"})
)
