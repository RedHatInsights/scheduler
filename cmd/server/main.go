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
	"insights-scheduler/internal/identity"
	"insights-scheduler/internal/shell/executor"
	httpShell "insights-scheduler/internal/shell/http"
	"insights-scheduler/internal/shell/messaging"
	"insights-scheduler/internal/shell/scheduler"
	"insights-scheduler/internal/shell/storage"
)

func main() {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Generate unique instance ID for distributed coordination
	hostname, _ := os.Hostname()
	instanceID := fmt.Sprintf("%s-%d", hostname, os.Getpid())

	log.Printf("Starting Insights Scheduler with configuration:")
	log.Printf("  Instance ID: %s", instanceID)
	log.Printf("  Server: %s:%d (private: %d)", cfg.Server.Host, cfg.Server.Port, cfg.Server.PrivatePort)
	log.Printf("  Storage Backend: %s", cfg.StorageBackend)
	log.Printf("  Database Type: %s", cfg.Database.Type)
	log.Printf("  Redis: enabled=%t, address=%s", cfg.Redis.Enabled, cfg.Redis.Address)
	log.Printf("  Kafka: enabled=%t, brokers=%v", cfg.Kafka.Enabled, cfg.Kafka.Brokers)
	log.Printf("  Metrics: enabled=%t, port=%d", cfg.Metrics.Enabled, cfg.Metrics.Port)

	// Create imperative shell components
	var repo usecases.JobRepository
	var runRepo usecases.JobRunRepository
	var lockManager executor.LockManager

	// Determine which storage backend to use
	storageBackend := cfg.StorageBackend
	if storageBackend == "" {
		storageBackend = cfg.Database.Type // Fallback to database type
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

		// If Redis is enabled, use it for distributed locking even with SQLite storage
		if cfg.Redis.Enabled {
			redisClient, err := storage.NewRedisClient(cfg.Redis)
			if err != nil {
				log.Fatalf("Failed to initialize Redis client: %v", err)
			}
			defer redisClient.Close()

			lockManager = storage.NewRedisLockManager(redisClient, cfg.Redis.KeyPrefix, cfg.Redis.LockTTL, instanceID)
			log.Printf("Redis lock manager initialized for distributed coordination")
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
		lockManager = storage.NewRedisLockManager(redisClient, cfg.Redis.KeyPrefix, cfg.Redis.LockTTL, instanceID)

		log.Printf("Redis storage and lock manager initialized successfully")

	default:
		log.Fatalf("Unsupported storage backend: %s (must be sqlite or redis)", storageBackend)
	}

	if lockManager != nil {
		log.Printf("Distributed locking enabled for instance: %s", instanceID)
	} else {
		log.Printf("Running in single-instance mode (no distributed locking)")
	}

	var userValidator identity.UserValidator
	switch cfg.UserValidatorImpl {
	case "bop":
		fmt.Println("Intializing BOP User Validator")
		userValidator = identity.NewBopUserValidator(
			cfg.Bop.BaseURL,
			cfg.Bop.APIToken,
			cfg.Bop.ClientID,
			cfg.Bop.InsightsEnv,
		)
	case "fake":
		fmt.Println("Intializing FAKE User Validator")
		userValidator = identity.NewFakeUserValidator()
	default:
		log.Fatalf("Unsupported UserValidator type: %s", cfg.UserValidatorImpl)
	}

	schedulingService := usecases.NewDefaultSchedulingService()

	var kafkaProducer *messaging.KafkaProducer
	var notifier executor.JobCompletionNotifier

	// Initialize job completion notifier based on configuration
	switch cfg.JobCompletionNotifierImpl {
	case "notifications":
		log.Printf("Initializing platform notifications job completion notifier")
		log.Printf("Kafka producer config - brokers: %v, topic: %s", cfg.Kafka.Brokers, cfg.Kafka.Topic)

		kafkaProducer, err = messaging.NewKafkaProducer(cfg.Kafka.Brokers, cfg.Kafka.Topic)
		if err != nil {
			log.Fatalf("Failed to initialize Kafka producer: %v", err)
		}

		// Ensure Kafka producer is closed on shutdown
		defer func() {
			if closeErr := kafkaProducer.Close(); closeErr != nil {
				log.Printf("Error closing Kafka producer: %v", closeErr)
			}
		}()

		// Create notifications-based notifier
		notifier = executor.NewNotificationsBasedJobCompletionNotifier(kafkaProducer)
		log.Printf("Job completion notifier initialized (platform notifications)")
	case "null":
		// Use null object pattern - no-op notifier
		notifier = executor.NewNullJobCompletionNotifier()
		log.Printf("Using null notifier (no notifications will be sent)")
	default:
		log.Fatalf("Unsupported JOB_COMPLETION_NOTIFIER_IMPL type: %s", cfg.JobCompletionNotifierImpl)
	}

	// Initialize payload-specific job executors
	executors := map[domain.PayloadType]executor.JobExecutor{
		domain.PayloadMessage:     executor.NewMessageJobExecutor(),
		domain.PayloadHTTPRequest: executor.NewHTTPJobExecutor(),
		domain.PayloadCommand:     executor.NewCommandJobExecutor(),
		domain.PayloadExport:      executor.NewExportJobExecutor(cfg, userValidator, notifier),
	}

	// Initialize job executor with map of executors
	baseExecutor := executor.NewJobExecutor(executors, runRepo)

	// Wrap with distributed executor if lock manager is available
	var jobExecutor usecases.JobExecutor
	if lockManager != nil {
		jobExecutor = executor.NewDistributedJobExecutor(baseExecutor, lockManager, instanceID)
		log.Printf("Job executor wrapped with distributed locking")
	} else {
		jobExecutor = baseExecutor
	}

	// Create functional core service
	jobService := usecases.NewJobService(repo, schedulingService, jobExecutor)
	jobRunService := usecases.NewJobRunService(runRepo, repo)

	// Create scheduler based on storage backend
	var jobScheduler usecases.CronScheduler
	if storageBackend == "redis" && cfg.Redis.Enabled {
		// Use polling scheduler with Redis ZSET for distributed scheduling
		redisClient, err := storage.NewRedisClient(cfg.Redis)
		if err != nil {
			log.Fatalf("Failed to initialize Redis client for scheduler: %v", err)
		}
		defer redisClient.Close()

		jobQueue := storage.NewRedisJobQueue(redisClient, cfg.Redis.KeyPrefix)
		pollInterval := 5 * time.Second // Poll every 5 seconds
		jobScheduler = scheduler.NewPollingScheduler(jobService, jobQueue, pollInterval, instanceID)
		log.Printf("Using polling scheduler with Redis ZSET (poll interval: %v)", pollInterval)
	} else {
		// Use cron scheduler for SQLite (robfig/cron based)
		jobScheduler = scheduler.NewCronScheduler(jobService)
		log.Printf("Using cron scheduler (robfig/cron based)")
	}

	// Connect job service to scheduler
	jobService.SetCronScheduler(jobScheduler)

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

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start scheduler
	go jobScheduler.Start(ctx)

	// Start HTTP server
	go func() {
		log.Printf("Starting server on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Stop scheduler
	jobScheduler.Stop()

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

	log.Println("Server exited")
}
