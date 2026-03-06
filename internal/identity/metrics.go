package identity

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// UserValidationHTTPDuration tracks the duration of HTTP calls to user validation service
	UserValidationHTTPDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "scheduler_user_validation_http_duration_seconds",
			Help:    "Duration of HTTP calls to user validation service in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "status"},
	)

	// UserValidationHTTPRequestsTotal tracks the total number of HTTP requests to user validation service
	UserValidationHTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "scheduler_user_validation_http_requests_total",
			Help: "Total number of HTTP requests to user validation service by method and status",
		},
		[]string{"method", "status"},
	)
)
