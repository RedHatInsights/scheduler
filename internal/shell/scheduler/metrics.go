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
)
