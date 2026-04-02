package scheduler

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// DBSyncFailures tracks the number of database sync failures
	// Labels: type (postgres_load, redis_sync)
	DBSyncFailures = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "scheduler_db_sync_failures_total",
			Help: "Total number of database sync failures by type",
		},
		[]string{"type"},
	)
)
