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
)
