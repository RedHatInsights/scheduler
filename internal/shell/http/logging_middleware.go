package http

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/redhatinsights/platform-go-middlewares/v2/identity"

	"insights-scheduler/internal/shell/logging"
)

// contextKey is the type for context keys to avoid collisions
type contextKey string

const loggerContextKey contextKey = "logger"

// responseWriter wraps http.ResponseWriter to capture the status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK, // default status code
	}
}

func (rw *responseWriter) WriteHeader(statusCode int) {
	if !rw.written {
		rw.statusCode = statusCode
		rw.written = true
		rw.ResponseWriter.WriteHeader(statusCode)
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

// GetLogger retrieves the request-scoped logger from the request context.
func GetLogger(r *http.Request) *slog.Logger {
	logger, ok := r.Context().Value(loggerContextKey).(*slog.Logger)
	if !ok {
		return slog.Default()
	}
	return logger
}

// LoggingMiddleware logs HTTP requests using structured logging and records Prometheus metrics.
// It creates a request-scoped logger with request_id, job_id, org_id, and user_id fields.
func LoggingMiddleware(baseLogger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Generate unique request ID
			requestID := uuid.New().String()

			// Extract job ID from URL path vars (if present)
			vars := mux.Vars(r)
			jobID := vars["id"]

			// Extract identity from context (set by identity middleware)
			orgID := ""
			userID := ""
			ident := identity.Get(r.Context())
			if ident.Identity.OrgID != "" {
				orgID = ident.Identity.OrgID
				userID = ident.Identity.User.UserID
			}

			// Create request-scoped logger
			logger := logging.NewRequestLogger(baseLogger, requestID, jobID, orgID, userID)

			// Store logger in request context
			ctx := context.WithValue(r.Context(), loggerContextKey, logger)
			r = r.WithContext(ctx)

			// Log request start
			logger.Debug("HTTP request received",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("remote_addr", r.RemoteAddr),
			)

			// Wrap response writer to capture status code
			wrapped := newResponseWriter(w)

			// Call next handler
			next.ServeHTTP(wrapped, r)

			// Calculate duration
			duration := time.Since(start)

			// Get status code
			status := wrapped.statusCode
			statusStr := strconv.Itoa(status)

			// Log completed request
			logger.Info("HTTP request completed",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.Duration("duration", duration),
			)

			// Record metrics
			HTTPRequestDuration.WithLabelValues(r.Method, statusStr).Observe(duration.Seconds())
			HTTPRequestsTotal.WithLabelValues(r.Method, statusStr).Inc()
		})
	}
}
