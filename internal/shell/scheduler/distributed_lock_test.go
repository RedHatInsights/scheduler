package scheduler

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
)

func setupTestRedisClient(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	return mr, client
}

func TestDistributedLock_TryAcquire_Success(t *testing.T) {
	mr, client := setupTestRedisClient(t)
	defer mr.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	lock := NewDistributedLock(client, logger)

	ctx := context.Background()
	acquired, err := lock.TryAcquire(ctx, "test-lock", "pod-1", 10*time.Second)

	if err != nil {
		t.Fatalf("TryAcquire() unexpected error: %v", err)
	}
	if !acquired {
		t.Error("Expected lock to be acquired")
	}

	// Verify lock exists in Redis
	val, err := client.Get(ctx, "test-lock").Result()
	if err != nil {
		t.Fatalf("Lock not found in Redis: %v", err)
	}
	if val != "pod-1" {
		t.Errorf("Lock owner = %q, want %q", val, "pod-1")
	}
}

func TestDistributedLock_TryAcquire_AlreadyHeld(t *testing.T) {
	mr, client := setupTestRedisClient(t)
	defer mr.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	lock := NewDistributedLock(client, logger)

	ctx := context.Background()

	// Pod 1 acquires lock
	acquired, err := lock.TryAcquire(ctx, "test-lock", "pod-1", 10*time.Second)
	if err != nil || !acquired {
		t.Fatalf("First acquire failed: err=%v, acquired=%v", err, acquired)
	}

	// Pod 2 tries to acquire same lock
	acquired, err = lock.TryAcquire(ctx, "test-lock", "pod-2", 10*time.Second)
	if err != nil {
		t.Fatalf("TryAcquire() unexpected error: %v", err)
	}
	if acquired {
		t.Error("Expected lock acquisition to fail (already held)")
	}

	// Verify lock is still owned by pod-1
	val, _ := client.Get(ctx, "test-lock").Result()
	if val != "pod-1" {
		t.Errorf("Lock owner = %q, want %q", val, "pod-1")
	}
}

func TestDistributedLock_Release_Success(t *testing.T) {
	mr, client := setupTestRedisClient(t)
	defer mr.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	lock := NewDistributedLock(client, logger)

	ctx := context.Background()

	// Acquire lock
	lock.TryAcquire(ctx, "test-lock", "pod-1", 10*time.Second)

	// Release lock
	err := lock.Release(ctx, "test-lock", "pod-1")
	if err != nil {
		t.Fatalf("Release() unexpected error: %v", err)
	}

	// Verify lock is gone
	_, err = client.Get(ctx, "test-lock").Result()
	if err != redis.Nil {
		t.Error("Expected lock to be deleted")
	}
}

func TestDistributedLock_Release_NotOwned(t *testing.T) {
	mr, client := setupTestRedisClient(t)
	defer mr.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	lock := NewDistributedLock(client, logger)

	ctx := context.Background()

	// Pod 1 acquires lock
	lock.TryAcquire(ctx, "test-lock", "pod-1", 10*time.Second)

	// Pod 2 tries to release it (should fail silently)
	err := lock.Release(ctx, "test-lock", "pod-2")
	if err != nil {
		t.Fatalf("Release() unexpected error: %v", err)
	}

	// Verify lock is still held by pod-1
	val, err := client.Get(ctx, "test-lock").Result()
	if err != nil {
		t.Fatal("Expected lock to still exist")
	}
	if val != "pod-1" {
		t.Errorf("Lock owner = %q, want %q", val, "pod-1")
	}
}

func TestDistributedLock_Release_Expired(t *testing.T) {
	mr, client := setupTestRedisClient(t)
	defer mr.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	lock := NewDistributedLock(client, logger)

	ctx := context.Background()

	// Acquire lock with 1 second TTL
	lock.TryAcquire(ctx, "test-lock", "pod-1", 1*time.Second)

	// Fast-forward time in miniredis
	mr.FastForward(2 * time.Second)

	// Try to release expired lock (should succeed but do nothing)
	err := lock.Release(ctx, "test-lock", "pod-1")
	if err != nil {
		t.Fatalf("Release() unexpected error: %v", err)
	}

	// Lock should be gone (expired)
	_, err = client.Get(ctx, "test-lock").Result()
	if err != redis.Nil {
		t.Error("Expected lock to be expired")
	}
}

func TestDistributedLock_Extend_Success(t *testing.T) {
	mr, client := setupTestRedisClient(t)
	defer mr.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	lock := NewDistributedLock(client, logger)

	ctx := context.Background()

	// Acquire lock with 5 second TTL
	lock.TryAcquire(ctx, "test-lock", "pod-1", 5*time.Second)

	// Extend TTL to 20 seconds
	extended, err := lock.Extend(ctx, "test-lock", "pod-1", 20*time.Second)
	if err != nil {
		t.Fatalf("Extend() unexpected error: %v", err)
	}
	if !extended {
		t.Error("Expected lock extension to succeed")
	}

	// Verify new TTL (approximately 20 seconds)
	ttl := mr.TTL("test-lock")
	if ttl < 15*time.Second || ttl > 21*time.Second {
		t.Errorf("TTL = %v, want ~20s", ttl)
	}
}

func TestDistributedLock_Extend_NotOwned(t *testing.T) {
	mr, client := setupTestRedisClient(t)
	defer mr.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	lock := NewDistributedLock(client, logger)

	ctx := context.Background()

	// Pod 1 acquires lock
	lock.TryAcquire(ctx, "test-lock", "pod-1", 10*time.Second)

	// Pod 2 tries to extend it
	extended, err := lock.Extend(ctx, "test-lock", "pod-2", 20*time.Second)
	if err != nil {
		t.Fatalf("Extend() unexpected error: %v", err)
	}
	if extended {
		t.Error("Expected lock extension to fail (not owned)")
	}

	// Verify TTL unchanged (still ~10 seconds)
	ttl := mr.TTL("test-lock")
	if ttl > 11*time.Second {
		t.Errorf("TTL should not have been extended, got %v", ttl)
	}
}

func TestDistributedLock_WithLock_Success(t *testing.T) {
	mr, client := setupTestRedisClient(t)
	defer mr.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	lock := NewDistributedLock(client, logger)

	ctx := context.Background()
	executed := false

	err := lock.WithLock(ctx, "test-lock", "pod-1", 10*time.Second, func() error {
		executed = true

		// Verify lock is held during execution
		val, _ := client.Get(ctx, "test-lock").Result()
		if val != "pod-1" {
			t.Errorf("Lock not held during execution")
		}

		return nil
	})

	if err != nil {
		t.Fatalf("WithLock() unexpected error: %v", err)
	}
	if !executed {
		t.Error("Function was not executed")
	}

	// Verify lock is released after execution
	_, err = client.Get(ctx, "test-lock").Result()
	if err != redis.Nil {
		t.Error("Expected lock to be released")
	}
}

func TestDistributedLock_WithLock_FunctionError(t *testing.T) {
	mr, client := setupTestRedisClient(t)
	defer mr.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	lock := NewDistributedLock(client, logger)

	ctx := context.Background()

	err := lock.WithLock(ctx, "test-lock", "pod-1", 10*time.Second, func() error {
		return context.DeadlineExceeded
	})

	if err != context.DeadlineExceeded {
		t.Errorf("WithLock() error = %v, want %v", err, context.DeadlineExceeded)
	}

	// Verify lock is released even when function returns error
	_, err = client.Get(ctx, "test-lock").Result()
	if err != redis.Nil {
		t.Error("Expected lock to be released after error")
	}
}

func TestDistributedLock_WithLock_AlreadyHeld(t *testing.T) {
	mr, client := setupTestRedisClient(t)
	defer mr.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	lock := NewDistributedLock(client, logger)

	ctx := context.Background()

	// Pod 1 acquires lock
	lock.TryAcquire(ctx, "test-lock", "pod-1", 10*time.Second)

	// Pod 2 tries to run WithLock
	executed := false
	err := lock.WithLock(ctx, "test-lock", "pod-2", 10*time.Second, func() error {
		executed = true
		return nil
	})

	if err == nil {
		t.Error("WithLock() should return error when lock is held")
	}
	if executed {
		t.Error("Function should not be executed when lock cannot be acquired")
	}
}

func TestDistributedLock_ConcurrentAccess(t *testing.T) {
	mr, client := setupTestRedisClient(t)
	defer mr.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	lock := NewDistributedLock(client, logger)

	ctx := context.Background()
	var counter int
	var mu sync.Mutex
	var wg sync.WaitGroup

	// 10 goroutines try to increment counter with retry
	for i := 0; i < 10; i++ {
		wg.Add(1)
		podID := "pod-" + string(rune('A'+i))
		go func(id string) {
			defer wg.Done()

			// Retry until we acquire the lock
			for {
				err := lock.WithLock(ctx, "test-lock", id, 1*time.Second, func() error {
					mu.Lock()
					counter++
					mu.Unlock()
					time.Sleep(5 * time.Millisecond) // Hold lock briefly
					return nil
				})
				if err == nil {
					break // Success
				}
				// Lock held by another goroutine, retry after brief delay
				time.Sleep(1 * time.Millisecond)
			}
		}(podID)
	}

	wg.Wait()

	// All 10 should have executed sequentially (with retries)
	if counter != 10 {
		t.Errorf("counter = %d, want 10", counter)
	}
}

func TestDistributedLock_TTLExpiry(t *testing.T) {
	mr, client := setupTestRedisClient(t)
	defer mr.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	lock := NewDistributedLock(client, logger)

	ctx := context.Background()

	// Acquire lock with 1 second TTL
	acquired, _ := lock.TryAcquire(ctx, "test-lock", "pod-1", 1*time.Second)
	if !acquired {
		t.Fatal("Failed to acquire lock")
	}

	// Lock should exist
	val, err := client.Get(ctx, "test-lock").Result()
	if err != nil || val != "pod-1" {
		t.Fatal("Lock not found immediately after acquisition")
	}

	// Fast-forward time
	mr.FastForward(2 * time.Second)

	// Lock should be expired
	_, err = client.Get(ctx, "test-lock").Result()
	if err != redis.Nil {
		t.Error("Expected lock to expire after TTL")
	}

	// Another pod should be able to acquire it
	acquired, _ = lock.TryAcquire(ctx, "test-lock", "pod-2", 10*time.Second)
	if !acquired {
		t.Error("Expected lock to be acquirable after expiry")
	}

	val, _ = client.Get(ctx, "test-lock").Result()
	if val != "pod-2" {
		t.Errorf("Lock owner = %q, want pod-2", val)
	}
}

func TestDistributedLock_Release_AfterAcquireByAnother(t *testing.T) {
	mr, client := setupTestRedisClient(t)
	defer mr.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	lock := NewDistributedLock(client, logger)

	ctx := context.Background()

	// Pod 1 acquires lock with short TTL
	lock.TryAcquire(ctx, "test-lock", "pod-1", 1*time.Second)

	// Expire the lock
	mr.FastForward(2 * time.Second)

	// Pod 2 acquires the same lock
	lock.TryAcquire(ctx, "test-lock", "pod-2", 10*time.Second)

	// Pod 1 tries to release (should fail silently - doesn't own it anymore)
	err := lock.Release(ctx, "test-lock", "pod-1")
	if err != nil {
		t.Fatalf("Release() unexpected error: %v", err)
	}

	// Lock should still be held by pod-2
	val, err := client.Get(ctx, "test-lock").Result()
	if err != nil {
		t.Fatal("Lock should still exist (owned by pod-2)")
	}
	if val != "pod-2" {
		t.Errorf("Lock owner = %q, want pod-2 (pod-1 should not have released it)", val)
	}
}
