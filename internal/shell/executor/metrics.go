package executor

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// JobsCurrentlyRunning tracks the number of jobs currently being executed
	JobsCurrentlyRunning = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "scheduler_jobs_currently_running",
		Help: "The number of jobs currently being executed",
	})

	// JobsAutoPausedTotal tracks the total number of jobs that have been auto-paused due to consecutive failures
	JobsAutoPausedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "scheduler_jobs_auto_paused_total",
		Help: "Total number of jobs auto-paused due to consecutive failures",
	})

	// JobsConsecutiveFailures tracks the distribution of consecutive failure counts
	JobsConsecutiveFailures = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "scheduler_jobs_consecutive_failures",
		Help:    "Distribution of consecutive failure counts for jobs",
		Buckets: []float64{1, 2, 3, 5, 10, 20, 50},
	})
)
