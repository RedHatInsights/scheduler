package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-redis/redis/v8"
)

// DistributedLock provides Redis-based distributed locking for multi-pod coordination
// Extracted from RedisScheduler's locking pattern for reusability
type DistributedLock struct {
	client *redis.Client
	logger *slog.Logger
}

// NewDistributedLock creates a new distributed lock manager
func NewDistributedLock(client *redis.Client, logger *slog.Logger) *DistributedLock {
	return &DistributedLock{
		client: client,
		logger: logger,
	}
}

// TryAcquire attempts to acquire a lock
// Returns true if lock was acquired, false if already held by another process
// Uses Redis SET NX (set if not exists) for atomic operation
func (d *DistributedLock) TryAcquire(ctx context.Context, lockKey string, ownerID string, ttl time.Duration) (bool, error) {
	acquired, err := d.client.SetNX(ctx, lockKey, ownerID, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("failed to acquire lock: %w", err)
	}

	if acquired {
		d.logger.Debug("Lock acquired",
			slog.String("lock_key", lockKey),
			slog.String("owner_id", ownerID),
			slog.Duration("ttl", ttl))
	} else {
		d.logger.Debug("Lock already held",
			slog.String("lock_key", lockKey))
	}

	return acquired, nil
}

// Release releases a lock only if we own it
// Uses Lua script to ensure atomic check-and-delete
func (d *DistributedLock) Release(ctx context.Context, lockKey string, ownerID string) error {
	// Lua script ensures we only delete if we own the lock
	// This prevents accidentally releasing a lock that was taken by another process
	// after our lock expired
	script := `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("del", KEYS[1])
		else
			return 0
		end
	`

	result, err := d.client.Eval(ctx, script, []string{lockKey}, ownerID).Result()
	if err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}

	// Safe type assertion with fallback
	released := int64(0)
	switch v := result.(type) {
	case int64:
		released = v
	case int:
		released = int64(v)
	default:
		d.logger.Warn("Unexpected result type from lock release script",
			slog.String("lock_key", lockKey),
			slog.Any("result_type", fmt.Sprintf("%T", result)))
	}

	if released == 1 {
		d.logger.Debug("Lock released",
			slog.String("lock_key", lockKey),
			slog.String("owner_id", ownerID))
	} else {
		d.logger.Debug("Lock was not owned by us (possibly expired)",
			slog.String("lock_key", lockKey),
			slog.String("owner_id", ownerID))
	}

	return nil
}

// Extend extends the TTL of a lock we own
// Useful for long-running operations that need to hold the lock longer
func (d *DistributedLock) Extend(ctx context.Context, lockKey string, ownerID string, ttl time.Duration) (bool, error) {
	// Lua script to extend TTL only if we own the lock
	script := `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("expire", KEYS[1], ARGV[2])
		else
			return 0
		end
	`

	result, err := d.client.Eval(ctx, script, []string{lockKey}, ownerID, int(ttl.Seconds())).Result()
	if err != nil {
		return false, fmt.Errorf("failed to extend lock: %w", err)
	}

	// Safe type assertion with fallback
	extendResult := int64(0)
	switch v := result.(type) {
	case int64:
		extendResult = v
	case int:
		extendResult = int64(v)
	default:
		d.logger.Warn("Unexpected result type from lock extend script",
			slog.String("lock_key", lockKey),
			slog.Any("result_type", fmt.Sprintf("%T", result)))
		return false, nil
	}

	extended := extendResult == 1

	if extended {
		d.logger.Debug("Lock extended",
			slog.String("lock_key", lockKey),
			slog.Duration("ttl", ttl))
	} else {
		d.logger.Warn("Failed to extend lock (not owned or expired)",
			slog.String("lock_key", lockKey),
			slog.String("owner_id", ownerID))
	}

	return extended, nil
}

// WithLock executes a function while holding a lock
// Automatically acquires and releases the lock
// Returns error if lock cannot be acquired or if the function returns an error
func (d *DistributedLock) WithLock(ctx context.Context, lockKey string, ownerID string, ttl time.Duration, fn func() error) error {
	acquired, err := d.TryAcquire(ctx, lockKey, ownerID, ttl)
	if err != nil {
		return err
	}

	if !acquired {
		return fmt.Errorf("failed to acquire lock: already held by another process")
	}

	defer d.Release(ctx, lockKey, ownerID)

	return fn()
}
