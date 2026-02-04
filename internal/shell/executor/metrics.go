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
)
