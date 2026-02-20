package main

// Insights Scheduler - Unified Binary with Subcommands
//
// This binary supports multiple operating modes via subcommands:
//
// 1. Legacy Single-Process Server (default):
//    ./scheduler  OR  ./scheduler server
//    - Combines API server and job scheduler in one process
//    - Suitable for local development, testing, small deployments
//    - Supports SQLite or PostgreSQL
//
// 2. Multi-Pod Architecture (production):
//    ./scheduler api     - API server only (handles REST, writes to Postgres + Redis)
//    ./scheduler worker  - Worker only (executes jobs from Redis, writes history to Postgres)
//    - Independent scaling of API and worker components
//    - Redis-based distributed scheduling
//    - See docs/KUBERNETES_DEPLOYMENT.md for details
//
// 3. Database Migrations:
//    ./scheduler db_migration up    - Apply migrations
//    ./scheduler db_migration down  - Rollback migrations

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
	"github.com/spf13/cobra"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/database/sqlite3"
	_ "github.com/golang-migrate/migrate/v4/source/file"

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

var (
	configPath string
)

var rootCmd = &cobra.Command{
	Use:   "scheduler",
	Short: "Insights Scheduler Service",
	Long: `A job scheduling service for Red Hat Insights with support for multiple backends and job execution types.

Operating modes (via subcommands):
  scheduler          - Run legacy single-process server (API + scheduler + executor)
  scheduler api      - Run API server only (for multi-pod deployments)
  scheduler worker   - Run worker only (for multi-pod deployments)
  scheduler db_migration - Manage database migrations`,
	Run: runServer,
}

func init() {
	rootCmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to configuration file")
	rootCmd.AddCommand(dbMigrationCmd)
	rootCmd.AddCommand(apiCmd)
	rootCmd.AddCommand(workerCmd)
}

var apiCmd = &cobra.Command{
	Use:   "api",
	Short: "Run the API server",
	Long:  `Run the API server for handling REST API requests. Writes to Postgres and Redis.`,
	Run:   runAPI,
}

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Run the worker",
	Long:  `Run the worker for executing scheduled jobs. Polls Redis and writes job run history to Postgres.`,
	Run:   runWorker,
}

var dbMigrationCmd = &cobra.Command{
	Use:   "db_migration",
	Short: "Database migration commands",
	Long:  `Manage database migrations for the scheduler service.`,
}

var dbMigrationUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Run database migrations",
	Long:  `Apply all pending database migrations to bring the schema up to date.`,
	RunE:  runDatabaseUp,
}

var dbMigrationDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Rollback database migrations",
	Long:  `Rollback the last database migration.`,
	RunE:  runDatabaseDown,
}

func init() {
	dbMigrationCmd.AddCommand(dbMigrationUpCmd)
	dbMigrationCmd.AddCommand(dbMigrationDownCmd)
}

func runDatabaseUp(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	m, err := createMigration(cfg)
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	if err == migrate.ErrNoChange {
		log.Println("No migrations to apply - database is up to date")
	} else {
		log.Println("Successfully applied database migrations")
	}
	return nil
}

func runDatabaseDown(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	m, err := createMigration(cfg)
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.Steps(-1); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to rollback migration: %w", err)
	}

	if err == migrate.ErrNoChange {
		log.Println("No migrations to rollback")
	} else {
		log.Println("Successfully rolled back last migration")
	}
	return nil
}

type loggerWrapper struct {
	*log.Logger
}

func (lw loggerWrapper) Verbose() bool {
	return true
}

func createMigration(cfg *config.Config) (*migrate.Migrate, error) {
	var databaseURL string

	switch cfg.Database.Type {
	case "sqlite":
		databaseURL = fmt.Sprintf("sqlite3://%s", cfg.Database.Path)
	case "postgres", "postgresql":
		databaseURL = fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
			cfg.Database.Username,
			cfg.Database.Password,
			cfg.Database.Host,
			cfg.Database.Port,
			cfg.Database.Name,
			cfg.Database.SSLMode,
		)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", cfg.Database.Type)
	}

	migrationsPath := "file://db/migrations"
	fmt.Println("databaseURL", databaseURL)
	m, err := migrate.New(migrationsPath, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize migration: %w", err)
	}

	m.Log = loggerWrapper{log.Default()}

	return m, nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runServer(cmd *cobra.Command, args []string) {
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
		postgresRepo, err := storage.NewPostgresJobRepository(cfg)
		if err != nil {
			log.Fatalf("Failed to initialize PostgreSQL database: %v", err)
		}
		repo = postgresRepo
		defer func() {
			if closeErr := postgresRepo.Close(); closeErr != nil {
				log.Printf("Error closing database: %v", closeErr)
			}
		}()

		postgresRunRepo, err := storage.NewPostgresJobRunRepository(cfg)
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

// runAPI runs the API server
// API Server - Handles REST API requests only
// Writes to both Postgres (source of truth) and Redis (scheduling)
// Scales horizontally without coordination
func runAPI(cmd *cobra.Command, args []string) {
	log.Println("[API] Starting Scheduler API server")

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("[API] Failed to load configuration: %v", err)
	}

	// Initialize PostgreSQL repository (source of truth)
	jobRepo, err := storage.NewPostgresJobRepository(cfg)
	if err != nil {
		log.Fatalf("[API] Failed to initialize Postgres repository: %v", err)
	}

	jobRunRepo, err := storage.NewPostgresJobRunRepository(cfg)
	if err != nil {
		log.Fatalf("[API] Failed to initialize Postgres job run repository: %v", err)
	}

	// Initialize dummy executors (API doesn't actually execute jobs)
	dummyExecutors := map[domain.PayloadType]executor.JobExecutor{
		domain.PayloadMessage:     executor.NewMessageJobExecutor(),
		domain.PayloadHTTPRequest: executor.NewHTTPJobExecutor(),
		domain.PayloadCommand:     executor.NewCommandJobExecutor(),
		domain.PayloadExport:      executor.NewMessageJobExecutor(), // Use message executor as dummy
	}
	dummyExecutor := executor.NewJobExecutor(dummyExecutors, jobRunRepo)

	// Initialize scheduling service
	schedService := usecases.NewDefaultSchedulingService()
	jobService := usecases.NewJobService(jobRepo, schedService, dummyExecutor)

	// Initialize Redis client for scheduling coordination
	var redisScheduler *scheduler.RedisScheduler
	if cfg.Redis.Enabled {
		redisAddr := fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)
		log.Printf("[API] Connecting to Redis at %s", redisAddr)

		redisScheduler, err = scheduler.NewRedisScheduler(redisAddr, dummyExecutor)
		if err != nil {
			log.Fatalf("[API] Failed to connect to Redis: %v", err)
		}
		defer redisScheduler.Close()

		log.Println("[API] Connected to Redis successfully")

		// Set Redis scheduler for job service (so Create/Update/Delete sync to Redis)
		jobService.SetCronScheduler(redisScheduler)
	} else {
		log.Println("[API] WARNING: Redis is disabled. Scheduling will not work!")
	}

	jobRunService := usecases.NewJobRunService(jobRunRepo, jobRepo)

	// Setup HTTP routes
	router := httpShell.SetupRoutes(jobService, jobRunService)

	// Create HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	// Start server in background
	go func() {
		log.Printf("[API] Server listening on port %d", cfg.Server.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[API] Server error: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println("[API] Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("[API] Server forced to shutdown: %v", err)
	}

	log.Println("[API] Server exited")
}

// runWorker runs the worker
// Worker - Executes scheduled jobs from Redis
// Reads from Redis (scheduling state)
// Writes to Postgres (job run history)
// Scales horizontally with distributed locking
func runWorker(cmd *cobra.Command, args []string) {
	log.Println("[WORKER] Starting Scheduler Worker")

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("[WORKER] Failed to load configuration: %v", err)
	}

	// Initialize PostgreSQL repository for job run history
	jobRepo, err := storage.NewPostgresJobRepository(cfg)
	if err != nil {
		log.Fatalf("[WORKER] Failed to initialize Postgres job repository: %v", err)
	}

	jobRunRepo, err := storage.NewPostgresJobRunRepository(cfg)
	if err != nil {
		log.Fatalf("[WORKER] Failed to initialize Postgres job run repository: %v", err)
	}

	// Initialize job executors (worker actually executes jobs)
	executors := map[domain.PayloadType]executor.JobExecutor{
		domain.PayloadMessage:     executor.NewMessageJobExecutor(),
		domain.PayloadHTTPRequest: executor.NewHTTPJobExecutor(),
		domain.PayloadCommand:     executor.NewCommandJobExecutor(),
		domain.PayloadExport:      executor.NewMessageJobExecutor(), // Simplified for now
	}
	jobExecutor := executor.NewJobExecutor(executors, jobRunRepo)

	// Initialize Redis scheduler
	if !cfg.Redis.Enabled {
		log.Fatalf("[WORKER] Redis must be enabled for worker pods. Set REDIS_ENABLED=true")
	}

	redisAddr := fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)
	log.Printf("[WORKER] Connecting to Redis at %s", redisAddr)

	redisScheduler, err := scheduler.NewRedisScheduler(redisAddr, jobExecutor)
	if err != nil {
		log.Fatalf("[WORKER] Failed to connect to Redis: %v", err)
	}
	defer redisScheduler.Close()

	log.Println("[WORKER] Connected to Redis successfully")

	// On startup, sync jobs from Postgres to Redis (for resilience)
	// This ensures Redis has all scheduled jobs even after Redis restart
	// Use leader election to prevent thundering herd (only one worker syncs)
	log.Println("[WORKER] Checking if database sync is needed...")

	// First, check if Redis already has jobs
	jobCount, err := redisScheduler.GetScheduledJobCount()
	if err != nil {
		log.Printf("[WORKER] WARNING: Failed to check Redis job count: %v", err)
	} else if jobCount > 0 {
		log.Printf("[WORKER] Redis already has %d jobs, skipping sync", jobCount)
	} else {
		// Redis is empty, try to become sync leader
		log.Println("[WORKER] Redis is empty, attempting to acquire sync leader lock...")

		isLeader, err := redisScheduler.TryAcquireLeader(5 * time.Minute)
		if err != nil {
			log.Printf("[WORKER] WARNING: Failed to acquire leader lock: %v", err)
			log.Println("[WORKER] Continuing without sync...")
		} else if !isLeader {
			log.Println("[WORKER] Another worker is syncing, skipping...")
		} else {
			// This worker is the leader, perform sync
			log.Println("[WORKER] Elected as sync leader, performing database sync")

			allJobs, err := jobRepo.FindAll()
			if err != nil {
				log.Printf("[WORKER] WARNING: Failed to load jobs from Postgres: %v", err)
			} else {
				log.Printf("[WORKER] Loaded %d jobs from Postgres, syncing to Redis...", len(allJobs))
				if err := redisScheduler.SyncJobsFromDB(allJobs); err != nil {
					log.Printf("[WORKER] WARNING: Failed to sync jobs to Redis: %v", err)
				} else {
					count, _ := redisScheduler.GetScheduledJobCount()
					log.Printf("[WORKER] Sync complete. %d jobs scheduled in Redis", count)
				}
			}
		}
	}

	// Start Redis scheduler (blocking)
	log.Println("[WORKER] Starting job execution loop...")

	// Run scheduler in background
	go redisScheduler.Start()

	// Optional: Periodic re-sync from Postgres to catch any missed updates
	// This is a safety mechanism in case API pods fail to update Redis
	if os.Getenv("ENABLE_PERIODIC_SYNC") == "true" {
		syncInterval := 1 * time.Hour // Sync every hour
		go func() {
			ticker := time.NewTicker(syncInterval)
			defer ticker.Stop()

			for range ticker.C {
				log.Println("[WORKER] Performing periodic sync from Postgres to Redis")
				jobs, err := jobRepo.FindAll()
				if err != nil {
					log.Printf("[WORKER] Periodic sync failed to load jobs: %v", err)
					continue
				}

				if err := redisScheduler.SyncJobsFromDB(jobs); err != nil {
					log.Printf("[WORKER] Periodic sync failed: %v", err)
				} else {
					count, _ := redisScheduler.GetScheduledJobCount()
					log.Printf("[WORKER] Periodic sync complete. %d jobs in Redis", count)
				}
			}
		}()
	}

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println("[WORKER] Shutting down worker...")
	redisScheduler.Stop()

	// Give in-flight jobs a chance to complete (up to 30 seconds)
	log.Println("[WORKER] Waiting for in-flight jobs to complete (max 30s)...")
	time.Sleep(30 * time.Second)

	log.Println("[WORKER] Worker exited")
}
