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
	"insights-scheduler/internal/shell/storage"
)

// Scheduler Pod - Polls ZSET and pushes jobs to work queue
// Responsibilities:
// - Poll Redis ZSET for jobs that are due to run
// - Move jobs from ZSET to work queue (Redis LIST) using atomic Lua script
// - Multiple scheduler pods can run for HA (idempotent queue push)
// - Serve Prometheus metrics
func main() {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Generate unique instance ID
	hostname, _ := os.Hostname()
	instanceID := fmt.Sprintf("scheduler-%s-%d", hostname, os.Getpid())

	log.Printf("Starting Insights Scheduler Pod")
	log.Printf("  Instance ID: %s", instanceID)
	log.Printf("  Redis: enabled=%t, address=%s", cfg.Redis.Enabled, cfg.Redis.Address)
	log.Printf("  Metrics: enabled=%t, port=%d", cfg.Metrics.Enabled, cfg.Metrics.Port)

	// Redis is required for scheduler pod
	if !cfg.Redis.Enabled {
		log.Fatalf("Scheduler pod requires Redis to be enabled")
	}

	// Initialize Redis client
	redisClient, err := storage.NewRedisClient(cfg.Redis)
	if err != nil {
		log.Fatalf("Failed to initialize Redis client: %v", err)
	}
	defer redisClient.Close()

	// Create job queue and work queue
	jobQueue := storage.NewRedisJobQueue(redisClient, cfg.Redis.KeyPrefix)
	workQueue := storage.NewRedisWorkQueue(redisClient, cfg.Redis.KeyPrefix)

	log.Printf("Redis job queue and work queue initialized")

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

	// Poll interval for checking ZSET
	pollInterval := 5 * time.Second
	log.Printf("Starting scheduler polling loop (interval: %v)", pollInterval)

	// Start polling loop
	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				pollAndEnqueue(jobQueue, workQueue, cfg.Redis.KeyPrefix, instanceID)
			case <-ctx.Done():
				log.Println("Scheduler polling loop stopped")
				return
			}
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down scheduler...")
	cancel()

	// Shutdown metrics server if enabled
	if cfg.Metrics.Enabled && metricsServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		if err := metricsServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("Metrics server forced to shutdown: %v", err)
		}
	}

	log.Println("Scheduler exited")
}

// pollAndEnqueue checks ZSET for due jobs and moves them to work queue
func pollAndEnqueue(jobQueue *storage.RedisJobQueue, workQueue *storage.RedisWorkQueue, keyPrefix string, instanceID string) {
	now := time.Now().UTC()

	// Get jobs that are due to run
	jobIDs, err := jobQueue.GetJobsDue(now)
	if err != nil {
		log.Printf("[%s] Error getting due jobs: %v", instanceID, err)
		return
	}

	if len(jobIDs) == 0 {
		return // No jobs due
	}

	log.Printf("[%s] Found %d jobs due for execution at %s", instanceID, len(jobIDs), now.Format(time.RFC3339))

	// Construct the scheduled jobs ZSET key
	scheduledJobsKey := fmt.Sprintf("%sscheduled_jobs", keyPrefix)

	// Move each job from ZSET to work queue atomically
	movedCount := 0
	for _, jobID := range jobIDs {
		// Use atomic Lua script to move from ZSET to LIST
		// This ensures idempotent operations when multiple scheduler pods run
		moved, err := workQueue.MoveJobToQueue(jobID, scheduledJobsKey)
		if err != nil {
			log.Printf("[%s] Error moving job %s to work queue: %v", instanceID, jobID, err)
			continue
		}

		if moved {
			movedCount++
			log.Printf("[%s] Moved job %s to work queue", instanceID, jobID)
		} else {
			// Job was already moved by another scheduler instance
			log.Printf("[%s] Job %s already in work queue (another scheduler moved it)", instanceID, jobID)
		}
	}

	if movedCount > 0 {
		log.Printf("[%s] Successfully moved %d/%d jobs to work queue", instanceID, movedCount, len(jobIDs))
	}
}
