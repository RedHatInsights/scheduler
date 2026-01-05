package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

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

	log.Printf("Starting Insights Scheduler with configuration:")
	log.Printf("  Server: %s:%d (private: %d)", cfg.Server.Host, cfg.Server.Port, cfg.Server.PrivatePort)
	log.Printf("  Database Type: %s", cfg.Database.Type)
	log.Printf("  Kafka: enabled=%t, brokers=%v", cfg.Kafka.Enabled, cfg.Kafka.Brokers)
	log.Printf("  Metrics: enabled=%t, port=%d", cfg.Metrics.Enabled, cfg.Metrics.Port)

	// Create imperative shell components
	var repo usecases.JobRepository
	var runRepo usecases.JobRunRepository
	switch cfg.Database.Type {
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
	case "postgres", "postgresql":
		connStr := cfg.Database.ConnectionString()
		postgresRepo, err := storage.NewPostgresJobRepository(connStr)
		if err != nil {
			log.Fatalf("Failed to initialize PostgreSQL database: %v", err)
		}
		repo = postgresRepo
		defer func() {
			if closeErr := postgresRepo.Close(); closeErr != nil {
				log.Printf("Error closing database: %v", closeErr)
			}
		}()

		postgresRunRepo, err := storage.NewPostgresJobRunRepository(connStr)
		if err != nil {
			log.Fatalf("Failed to initialize PostgreSQL job run repository: %v", err)
		}
		runRepo = postgresRunRepo
		defer func() {
			if closeErr := postgresRunRepo.Close(); closeErr != nil {
				log.Printf("Error closing job run repository: %v", closeErr)
			}
		}()
	default:
		log.Fatalf("Unsupported database type: %s", cfg.Database.Type)
	}
	log.Printf("Database initialized successfully")

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
	jobExecutor := executor.NewJobExecutor(executors, runRepo)

	// Create functional core service
	jobService := usecases.NewJobService(repo, schedulingService, jobExecutor)
	jobRunService := usecases.NewJobRunService(runRepo, repo)

	// Create cron scheduler
	cronScheduler := scheduler.NewCronScheduler(jobService)

	// Connect job service to cron scheduler
	jobService.SetCronScheduler(cronScheduler)

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

	// Start cron scheduler
	go cronScheduler.Start(ctx)

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
	cronScheduler.Stop()

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
