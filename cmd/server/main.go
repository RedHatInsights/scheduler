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
	Long:  `A job scheduling service for Red Hat Insights with support for multiple backends and job execution types.`,
	Run:   runServer,
}

func init() {
	rootCmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to configuration file")
	rootCmd.AddCommand(dbMigrationCmd)
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
