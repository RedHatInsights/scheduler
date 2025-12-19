package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/usecases"
	httpShell "insights-scheduler/internal/shell/http"
	"insights-scheduler/internal/shell/storage"
)

// NoOpExecutor is a no-op executor used by API pod (API doesn't execute jobs)
type NoOpExecutor struct{}

func (e *NoOpExecutor) Execute(job domain.Job) error {
	// API pod doesn't execute jobs
	return fmt.Errorf("API pod cannot execute jobs")
}

// APIScheduler implements CronScheduler interface but only adds jobs to Redis ZSET
// It doesn't actually run a scheduler - that's done by the Scheduler pod
type APIScheduler struct {
	jobQueue           *storage.RedisJobQueue
	scheduleCalculator *usecases.ScheduleCalculator
}

func NewAPIScheduler(jobQueue *storage.RedisJobQueue) *APIScheduler {
	return &APIScheduler{
		jobQueue:           jobQueue,
		scheduleCalculator: usecases.NewScheduleCalculator(),
	}
}

func (s *APIScheduler) ScheduleJob(job domain.Job) error {
	// Calculate next run time
	nextRun := s.scheduleCalculator.GetInitialRunTime(job, time.Now())

	// Add to Redis ZSET
	err := s.jobQueue.Schedule(job.ID, nextRun)
	if err != nil {
		return fmt.Errorf("failed to schedule job in ZSET: %w", err)
	}

	log.Printf("[API] Scheduled job %s (%s) to run at %s", job.ID, job.Name, nextRun.Format(time.RFC3339))
	return nil
}

func (s *APIScheduler) UnscheduleJob(jobID string) {
	// Remove from Redis ZSET
	err := s.jobQueue.Unschedule(jobID)
	if err != nil {
		log.Printf("[API] Error unscheduling job %s: %v", jobID, err)
	} else {
		log.Printf("[API] Unscheduled job %s from ZSET", jobID)
	}
}

func (s *APIScheduler) Start(ctx context.Context) {
	// API pod doesn't run a scheduler
	<-ctx.Done()
}

func (s *APIScheduler) Stop() {
	// Nothing to stop
}

// API Pod - Handles REST API requests only
// Responsibilities:
// - Accept job creation/update/delete requests via REST API
// - Store jobs in database (Redis or SQLite)
// - Add scheduled jobs to Redis ZSET
// - Provide job query endpoints
// - Serve Prometheus metrics
func main() {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Generate unique instance ID
	hostname, _ := os.Hostname()
	instanceID := fmt.Sprintf("api-%s-%d", hostname, os.Getpid())

	log.Printf("Starting Insights Scheduler API Pod")
	log.Printf("  Instance ID: %s", instanceID)
	log.Printf("  Server: %s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("  Storage Backend: %s", cfg.StorageBackend)
	log.Printf("  Redis: enabled=%t, address=%s", cfg.Redis.Enabled, cfg.Redis.Address)
	log.Printf("  Metrics: enabled=%t, port=%d", cfg.Metrics.Enabled, cfg.Metrics.Port)

	// Create storage repositories
	var repo usecases.JobRepository
	var runRepo usecases.JobRunRepository
	var jobQueue *storage.RedisJobQueue

	// Determine which storage backend to use
	storageBackend := cfg.StorageBackend
	if storageBackend == "" {
		storageBackend = cfg.Database.Type
	}

	switch storageBackend {
	case "sqlite":
		sqliteRepo, err := storage.NewSQLiteJobRepository(cfg.Database.Path)
		if err != nil {
			log.Fatalf("Failed to initialize SQLite database: %v", err)
		}
		repo = sqliteRepo
		defer func() {
			if closeErr := sqliteRepo.Close(); closeErr != nil {
				log.Printf("Error closing database: %v", closeErr)
			}
		}()

		sqliteRunRepo, err := storage.NewSQLiteJobRunRepository(cfg.Database.Path)
		if err != nil {
			log.Fatalf("Failed to initialize SQLite job run repository: %v", err)
		}
		runRepo = sqliteRunRepo
		defer func() {
			if closeErr := sqliteRunRepo.Close(); closeErr != nil {
				log.Printf("Error closing job run repository: %v", closeErr)
			}
		}()

		log.Printf("SQLite storage initialized successfully")

		// For SQLite, we still need Redis ZSET for scheduling in distributed mode
		if cfg.Redis.Enabled {
			redisClient, err := storage.NewRedisClient(cfg.Redis)
			if err != nil {
				log.Fatalf("Failed to initialize Redis client: %v", err)
			}
			defer redisClient.Close()

			jobQueue = storage.NewRedisJobQueue(redisClient, cfg.Redis.KeyPrefix)
			log.Printf("Redis job queue initialized for distributed scheduling")
		}

	case "redis":
		if !cfg.Redis.Enabled {
			log.Fatalf("Redis storage backend requires Redis to be enabled")
		}

		redisClient, err := storage.NewRedisClient(cfg.Redis)
		if err != nil {
			log.Fatalf("Failed to initialize Redis client: %v", err)
		}
		defer redisClient.Close()

		repo = storage.NewRedisJobRepository(redisClient, cfg.Redis.KeyPrefix)
		runRepo = storage.NewRedisJobRunRepository(redisClient, cfg.Redis.KeyPrefix)
		jobQueue = storage.NewRedisJobQueue(redisClient, cfg.Redis.KeyPrefix)

		log.Printf("Redis storage and job queue initialized successfully")

	default:
		log.Fatalf("Unsupported storage backend: %s (must be sqlite or redis)", storageBackend)
	}

	// Create a minimal job executor (not used by API pod, but required for JobService)
	// The API pod doesn't execute jobs, but JobService interface requires an executor
	schedulingService := usecases.NewDefaultSchedulingService()
	noOpExecutor := &NoOpExecutor{}

	// Create functional core service
	jobService := usecases.NewJobService(repo, schedulingService, noOpExecutor)
	jobRunService := usecases.NewJobRunService(runRepo, repo)

	// Create API scheduler wrapper that adds jobs to Redis ZSET
	if jobQueue != nil {
		apiScheduler := NewAPIScheduler(jobQueue)
		jobService.SetCronScheduler(apiScheduler)
		log.Printf("API scheduler initialized (adds jobs to Redis ZSET)")
	} else {
		log.Printf("Warning: No job queue available, jobs will not be scheduled")
	}

	// Setup HTTP routes
	router := httpShell.SetupRoutes(jobService, jobRunService)

	// Create HTTP server
	serverAddr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:         serverAddr,
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	// Create metrics server
	var metricsServer *http.Server
	if cfg.Metrics.Enabled {
		metricsAddr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Metrics.Port)
		metricsMux := http.NewServeMux()
		metricsMux.Handle(cfg.Metrics.Path, promhttp.Handler())

		metricsServer = &http.Server{
			Addr:    metricsAddr,
			Handler: metricsMux,
		}

		// Start metrics server
		go func() {
			log.Printf("Starting metrics server on %s%s", metricsAddr, cfg.Metrics.Path)
			if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("Metrics server error: %v", err)
			}
		}()
	}

	// Start HTTP server
	go func() {
		log.Printf("Starting API server on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down API server...")

	// Shutdown HTTP server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	// Shutdown metrics server if enabled
	if cfg.Metrics.Enabled && metricsServer != nil {
		if err := metricsServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("Metrics server forced to shutdown: %v", err)
		}
	}

	log.Println("API server exited")
}
