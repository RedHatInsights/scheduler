package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/usecases"
	"insights-scheduler/internal/shell/executor"
	httpshell "insights-scheduler/internal/shell/http"
	"insights-scheduler/internal/shell/scheduler"
	"insights-scheduler/internal/shell/storage"
)

// API Server - Handles REST API requests only
// Writes to both Postgres (source of truth) and Redis (scheduling)
// Scales horizontally without coordination

func main() {
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
	router := httpshell.SetupRoutes(jobService, jobRunService)

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
