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
	"insights-scheduler/internal/shell/messaging"
	"insights-scheduler/internal/shell/storage"
)

// Worker Pod - Consumes jobs from work queue and executes them
// Responsibilities:
// - Pop jobs from work queue (Redis LIST) using blocking BRPOP
// - Execute jobs using configured executors
// - Calculate next run time and reschedule to ZSET
// - Handle job failures
// - Remove completed jobs from processing queue
// - Serve Prometheus metrics
func main() {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Generate unique instance ID
	hostname, _ := os.Hostname()
	instanceID := fmt.Sprintf("worker-%s-%d", hostname, os.Getpid())

	log.Printf("Starting Insights Scheduler Worker Pod")
	log.Printf("  Instance ID: %s", instanceID)
	log.Printf("  Storage Backend: %s", cfg.StorageBackend)
	log.Printf("  Redis: enabled=%t, address=%s", cfg.Redis.Enabled, cfg.Redis.Address)
	log.Printf("  Metrics: enabled=%t, port=%d", cfg.Metrics.Enabled, cfg.Metrics.Port)

	// Redis is required for worker pod
	if !cfg.Redis.Enabled {
		log.Fatalf("Worker pod requires Redis to be enabled")
	}

	// Initialize Redis client
	redisClient, err := storage.NewRedisClient(cfg.Redis)
	if err != nil {
		log.Fatalf("Failed to initialize Redis client: %v", err)
	}
	defer redisClient.Close()

	// Create storage repositories
	var repo usecases.JobRepository
	var runRepo usecases.JobRunRepository

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

	case "redis":
		repo = storage.NewRedisJobRepository(redisClient, cfg.Redis.KeyPrefix)
		runRepo = storage.NewRedisJobRunRepository(redisClient, cfg.Redis.KeyPrefix)
		log.Printf("Redis storage initialized successfully")

	default:
		log.Fatalf("Unsupported storage backend: %s (must be sqlite or redis)", storageBackend)
	}

	// Create job queue and work queue
	jobQueue := storage.NewRedisJobQueue(redisClient, cfg.Redis.KeyPrefix)
	workQueue := storage.NewRedisWorkQueue(redisClient, cfg.Redis.KeyPrefix)

	log.Printf("Redis job queue and work queue initialized")

	// Requeue any jobs that were being processed when workers crashed
	requeuedCount, err := workQueue.RequeueProcessingJobs()
	if err != nil {
		log.Printf("Warning: Failed to requeue processing jobs: %v", err)
	} else if requeuedCount > 0 {
		log.Printf("Requeued %d jobs from processing queue", requeuedCount)
	}

	// Initialize user validator
	var userValidator identity.UserValidator
	switch cfg.UserValidatorImpl {
	case "bop":
		log.Println("Initializing BOP User Validator")
		userValidator = identity.NewBopUserValidator(
			cfg.Bop.BaseURL,
			cfg.Bop.APIToken,
			cfg.Bop.ClientID,
			cfg.Bop.InsightsEnv,
		)
	case "fake":
		log.Println("Initializing FAKE User Validator")
		userValidator = identity.NewFakeUserValidator()
	default:
		log.Fatalf("Unsupported UserValidator type: %s", cfg.UserValidatorImpl)
	}

	// Initialize job completion notifier
	var kafkaProducer *messaging.KafkaProducer
	var notifier executor.JobCompletionNotifier

	switch cfg.JobCompletionNotifierImpl {
	case "notifications":
		log.Printf("Initializing platform notifications job completion notifier")
		log.Printf("Kafka producer config - brokers: %v, topic: %s", cfg.Kafka.Brokers, cfg.Kafka.Topic)

		kafkaProducer, err = messaging.NewKafkaProducer(cfg.Kafka.Brokers, cfg.Kafka.Topic)
		if err != nil {
			log.Fatalf("Failed to initialize Kafka producer: %v", err)
		}
		defer func() {
			if closeErr := kafkaProducer.Close(); closeErr != nil {
				log.Printf("Error closing Kafka producer: %v", closeErr)
			}
		}()

		notifier = executor.NewNotificationsBasedJobCompletionNotifier(kafkaProducer)
		log.Printf("Job completion notifier initialized (platform notifications)")
	case "null":
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

	// Initialize job executor
	baseExecutor := executor.NewJobExecutor(executors, runRepo)

	// No distributed locking needed - work queue provides natural locking
	// Each job can only be popped by one worker
	jobExecutor := baseExecutor

	// Create functional core service
	schedulingService := usecases.NewDefaultSchedulingService()
	jobService := usecases.NewJobService(repo, schedulingService, jobExecutor)

	// Create schedule calculator for rescheduling
	scheduleCalculator := usecases.NewScheduleCalculator()

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

	// Start worker loop
	log.Printf("Starting worker loop (blocking on work queue)")

	// Number of concurrent worker goroutines
	numWorkers := 5 // Can be made configurable
	for i := 0; i < numWorkers; i++ {
		workerNum := i + 1
		go func(workerID int) {
			for {
				select {
				case <-ctx.Done():
					log.Printf("[%s] Worker %d stopped", instanceID, workerID)
					return
				default:
					processNextJob(ctx, workQueue, jobQueue, jobService, scheduleCalculator, instanceID, workerID)
				}
			}
		}(workerNum)
	}

	log.Printf("Started %d worker goroutines", numWorkers)

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down worker...")
	log.Println("Waiting up to 5 minutes for current jobs to complete...")
	cancel()

	// Give workers time to finish current jobs (matches terminationGracePeriodSeconds)
	// Workers will stop accepting new jobs but finish current ones
	time.Sleep(290 * time.Second)  // 4m50s - just under the 5 minute grace period

	// Shutdown metrics server if enabled
	if cfg.Metrics.Enabled && metricsServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		if err := metricsServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("Metrics server forced to shutdown: %v", err)
		}
	}

	log.Println("Worker exited")
}

// processNextJob blocks waiting for a job from the work queue and processes it
func processNextJob(
	ctx context.Context,
	workQueue *storage.RedisWorkQueue,
	jobQueue *storage.RedisJobQueue,
	jobService *usecases.JobService,
	scheduleCalculator *usecases.ScheduleCalculator,
	instanceID string,
	workerID int,
) {
	// Block for up to 5 seconds waiting for a job
	jobID, err := workQueue.PopJob(5 * time.Second)
	if err != nil {
		log.Printf("[%s-W%d] Error popping job from work queue: %v", instanceID, workerID, err)
		return
	}

	if jobID == "" {
		// Timeout - no job available
		return
	}

	log.Printf("[%s-W%d] Processing job %s", instanceID, workerID, jobID)

	// Get job details
	job, err := jobService.GetJob(jobID)
	if err != nil {
		log.Printf("[%s-W%d] Error getting job %s: %v", instanceID, workerID, jobID, err)
		// Remove from processing queue since we can't process it
		if err := workQueue.CompleteJob(jobID); err != nil {
			log.Printf("[%s-W%d] Error removing failed job from processing queue: %v", instanceID, workerID, err)
		}
		return
	}

	// Check if job should still run (might have been paused/deleted)
	if job.Status != domain.StatusScheduled {
		log.Printf("[%s-W%d] Job %s status is %s, skipping execution", instanceID, workerID, jobID, job.Status)
		// Remove from processing queue
		if err := workQueue.CompleteJob(jobID); err != nil {
			log.Printf("[%s-W%d] Error completing job: %v", instanceID, workerID, err)
		}
		return
	}

	// Execute the job
	startTime := time.Now()
	err = jobService.ExecuteScheduledJob(job)
	duration := time.Since(startTime)

	if err != nil {
		log.Printf("[%s-W%d] Job %s (%s) failed after %v: %v", instanceID, workerID, jobID, job.Name, duration, err)
	} else {
		log.Printf("[%s-W%d] Job %s (%s) completed successfully in %v", instanceID, workerID, jobID, job.Name, duration)
	}

	// Calculate next run time and reschedule
	now := time.Now()
	nextRun, err := scheduleCalculator.CalculateNextRun(job, now)
	if err != nil {
		log.Printf("[%s-W%d] Error calculating next run time for job %s: %v", instanceID, workerID, jobID, err)
		log.Printf("[%s-W%d] Leaving job %s in processing queue for retry", instanceID, workerID, jobID)
		// CRITICAL: Don't remove from processing queue if we can't calculate next run
		// Job will be requeued on worker restart
		return
	}

	// Add back to scheduled jobs ZSET
	if err := jobQueue.Schedule(jobID, nextRun); err != nil {
		log.Printf("[%s-W%d] Error rescheduling job %s: %v", instanceID, workerID, jobID, err)
		log.Printf("[%s-W%d] Leaving job %s in processing queue for retry", instanceID, workerID, jobID)
		// CRITICAL: Don't remove from processing queue if rescheduling failed
		// Job will be requeued on worker restart
		return
	}

	log.Printf("[%s-W%d] Rescheduled job %s to run at %s", instanceID, workerID, job.Name, nextRun.Format(time.RFC3339))

	// Remove from processing queue ONLY if rescheduling succeeded
	if err := workQueue.CompleteJob(jobID); err != nil {
		log.Printf("[%s-W%d] Error removing job %s from processing queue: %v", instanceID, workerID, jobID, err)
		// Job is already in ZSET, so if this fails it will be in both places temporarily
		// On restart, it will be requeued and re-executed, but ZSET already has correct next run
		// This is acceptable (idempotent if job handlers are idempotent)
	}
}
