package identity

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// UserValidationHTTPDuration tracks the duration of HTTP calls to user validation service
	ThreeScaleUserValidationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "scheduler_3scale_user_validation_duration_seconds",
			Help:    "Duration of calls to 3scale for user validation service in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "status"},
	)

	// UserValidationHTTPRequestsTotal tracks the total number of HTTP requests to user validation service
	ThreeScaleUserValidationRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "scheduler_3scale_user_validation_requests_total",
			Help: "Total number of 3scale user validation service by method and status",
		},
		[]string{"method", "status"},
	)
)
