package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"insights-scheduler-part-2/internal/config"
	"insights-scheduler-part-2/internal/core/usecases"
	"insights-scheduler-part-2/internal/shell/executor"
	httpShell "insights-scheduler-part-2/internal/shell/http"
	"insights-scheduler-part-2/internal/shell/messaging"
	"insights-scheduler-part-2/internal/shell/scheduler"
	"insights-scheduler-part-2/internal/shell/storage"
)

func main() {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Starting Insights Scheduler with configuration:")
	log.Printf("  Server: %s:%d (private: %d)", cfg.Server.Host, cfg.Server.Port, cfg.Server.PrivatePort)
	log.Printf("  Database: %s (%s)", cfg.Database.Type, cfg.GetDatabaseConnectionString())
	log.Printf("  Kafka: enabled=%t, brokers=%v", cfg.Kafka.Enabled, cfg.Kafka.Brokers)
	log.Printf("  Metrics: enabled=%t, port=%d", cfg.Metrics.Enabled, cfg.Metrics.Port)

	// Create imperative shell components
	var repo usecases.JobRepository
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
	default:
		log.Fatalf("Unsupported database type: %s", cfg.Database.Type)
	}
	log.Printf("Database initialized successfully")

	schedulingService := usecases.NewDefaultSchedulingService()

	// Initialize job executor with optional Kafka producer
	var jobExecutor usecases.JobExecutor
	if cfg.Kafka.Enabled {
		log.Printf("Initializing Kafka producer with brokers: %v, topic: %s", cfg.Kafka.Brokers, cfg.Kafka.Topic)

		kafkaProducer, err := messaging.NewKafkaProducer(cfg.Kafka.Brokers, cfg.Kafka.Topic)
		if err != nil {
			log.Printf("Failed to initialize Kafka producer: %v", err)
			log.Printf("Continuing without Kafka producer")
			jobExecutor = executor.NewDefaultJobExecutor(cfg)
		} else {
			log.Printf("Kafka producer initialized successfully")
			jobExecutor = executor.NewDefaultJobExecutorWithKafka(kafkaProducer, cfg)

			// Ensure Kafka producer is closed on shutdown
			defer func() {
				if closeErr := kafkaProducer.Close(); closeErr != nil {
					log.Printf("Error closing Kafka producer: %v", closeErr)
				}
			}()
		}
	} else {
		log.Printf("Kafka disabled, running without Kafka producer")
		jobExecutor = executor.NewDefaultJobExecutor(cfg)
	}

	// Create functional core service
	jobService := usecases.NewJobService(repo, schedulingService, jobExecutor)

	// Create cron scheduler
	cronScheduler := scheduler.NewCronScheduler(jobService)

	// Connect job service to cron scheduler
	jobService.SetCronScheduler(cronScheduler)

	// Setup HTTP routes
	router := httpShell.SetupRoutes(jobService)

	// Create HTTP server
	serverAddr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:         serverAddr,
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
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

	log.Println("Server exited")
}
