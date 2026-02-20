package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/shell/executor"
	"insights-scheduler/internal/shell/scheduler"
	"insights-scheduler/internal/shell/storage"
)

// Worker - Executes scheduled jobs from Redis
// Reads from Redis (scheduling state)
// Writes to Postgres (job run history)
// Scales horizontally with distributed locking

func main() {
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
	log.Println("[WORKER] Syncing jobs from Postgres to Redis (one-time on startup)")
	allJobs, err := jobRepo.FindAll()
	if err != nil {
		log.Printf("[WORKER] WARNING: Failed to load jobs from Postgres: %v", err)
		log.Println("[WORKER] Continuing with jobs already in Redis...")
	} else {
		if err := redisScheduler.SyncJobsFromDB(allJobs); err != nil {
			log.Printf("[WORKER] WARNING: Failed to sync jobs to Redis: %v", err)
		} else {
			count, _ := redisScheduler.GetScheduledJobCount()
			log.Printf("[WORKER] Sync complete. %d jobs scheduled in Redis", count)
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
