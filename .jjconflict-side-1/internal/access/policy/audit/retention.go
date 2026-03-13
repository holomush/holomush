// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// RetentionConfig defines the retention policy for audit logs.
type RetentionConfig struct {
	RetainDenials time.Duration // How long to keep denial records
	RetainAllows  time.Duration // How long to keep allow records
	PurgeInterval time.Duration // How often to run the purge cycle
}

// DefaultRetentionConfig returns the default retention configuration.
func DefaultRetentionConfig() RetentionConfig {
	return RetentionConfig{
		RetainDenials: 90 * 24 * time.Hour,
		RetainAllows:  7 * 24 * time.Hour,
		PurgeInterval: 24 * time.Hour,
	}
}

// PartitionManager manages audit log partitions and purging.
type PartitionManager interface {
	EnsurePartitions(ctx context.Context, months int) error
	PurgeExpiredAllows(ctx context.Context, olderThan time.Time) (int64, error)
	DetachExpiredPartitions(ctx context.Context, olderThan time.Time) ([]string, error)
	DropDetachedPartitions(ctx context.Context, gracePeriod time.Duration) ([]string, error)
	HealthCheck(ctx context.Context) error
}

// RetentionWorker runs periodic retention maintenance on audit logs.
type RetentionWorker struct {
	cfg     RetentionConfig
	manager PartitionManager
	logger  *slog.Logger
	clock   func() time.Time

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewRetentionWorker creates a new retention worker.
func NewRetentionWorker(cfg RetentionConfig, manager PartitionManager) *RetentionWorker {
	return &RetentionWorker{
		cfg:     cfg,
		manager: manager,
		logger:  slog.Default(),
		clock:   time.Now,
	}
}

// RunOnce executes a single retention cycle. All operations are attempted
// even if earlier ones fail; errors are combined.
func (w *RetentionWorker) RunOnce(ctx context.Context) error {
	now := w.clock()
	var errs []error

	// Ensure partitions exist for the next 3 months
	if err := w.manager.EnsurePartitions(ctx, 3); err != nil {
		w.logger.Error("ensure partitions failed", "error", err)
		errs = append(errs, err)
	}

	// Purge expired allow records
	purged, err := w.manager.PurgeExpiredAllows(ctx, now.Add(-w.cfg.RetainAllows))
	if err != nil {
		w.logger.Error("purge expired allows failed", "error", err)
		errs = append(errs, err)
	} else if purged > 0 {
		w.logger.Info("purged expired allow records", "count", purged)
	}

	// Detach expired partitions
	detached, err := w.manager.DetachExpiredPartitions(ctx, now.Add(-w.cfg.RetainDenials))
	if err != nil {
		w.logger.Error("detach expired partitions failed", "error", err)
		errs = append(errs, err)
	} else if len(detached) > 0 {
		w.logger.Info("detached expired partitions", "partitions", detached)
	}

	// Drop detached partitions after grace period
	dropped, err := w.manager.DropDetachedPartitions(ctx, 7*24*time.Hour)
	if err != nil {
		w.logger.Error("drop detached partitions failed", "error", err)
		errs = append(errs, err)
	} else if len(dropped) > 0 {
		w.logger.Info("dropped detached partitions", "partitions", dropped)
	}

	return errors.Join(errs...)
}

// Start begins periodic retention maintenance.
func (w *RetentionWorker) Start(ctx context.Context) error {
	ctx, w.cancel = context.WithCancel(ctx)
	w.wg.Add(1)
	go w.run(ctx)
	return nil
}

// Stop stops the retention worker and waits for completion.
func (w *RetentionWorker) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
}

// HealthCheck delegates to the partition manager.
func (w *RetentionWorker) HealthCheck(ctx context.Context) error {
	if err := w.manager.HealthCheck(ctx); err != nil {
		return fmt.Errorf("partition health check: %w", err)
	}
	return nil
}

func (w *RetentionWorker) run(ctx context.Context) {
	defer w.wg.Done()

	ticker := time.NewTicker(w.cfg.PurgeInterval)
	defer ticker.Stop()

	// Run once immediately
	if err := w.RunOnce(ctx); err != nil {
		w.logger.Error("retention cycle failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.RunOnce(ctx); err != nil {
				w.logger.Error("retention cycle failed", "error", err)
			}
		}
	}
}
