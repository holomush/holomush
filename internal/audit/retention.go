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

	// skipFirstRun defers the FIRST destructive RunOnce until the first
	// ticker tick instead of firing it immediately on Start. See
	// WithSkipFirstRun.
	skipFirstRun bool

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Option configures a RetentionWorker at construction.
type Option func(*RetentionWorker)

// WithSkipFirstRun makes the worker SKIP the immediate RunOnce that Start
// otherwise fires before the ticker, so the first destructive Detach/Drop
// cycle only runs after the first PurgeInterval tick (round-4 MEDIUM). The
// events_audit worker wires this so a subsystem that fails after the
// synchronous boot gate cannot trigger DETACH/DROP during a red deploy. The
// default (no option) preserves the immediate-run behavior the ABAC
// access_audit worker relies on.
func WithSkipFirstRun() Option {
	return func(w *RetentionWorker) { w.skipFirstRun = true }
}

// NewRetentionWorker creates a new retention worker.
func NewRetentionWorker(cfg RetentionConfig, manager PartitionManager, opts ...Option) *RetentionWorker {
	w := &RetentionWorker{
		cfg:     cfg,
		manager: manager,
		logger:  slog.Default(),
		clock:   time.Now,
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// RunOnce executes a single retention cycle. All operations are attempted
// even if earlier ones fail; errors are combined.
func (w *RetentionWorker) RunOnce(ctx context.Context) error {
	now := w.clock()
	var errs []error

	// Ensure partitions exist for the next 3 months
	if err := w.manager.EnsurePartitions(ctx, 3); err != nil {
		w.logger.ErrorContext(ctx, "ensure partitions failed", "error", err)
		errs = append(errs, err)
	}

	// Purge expired allow records
	purged, err := w.manager.PurgeExpiredAllows(ctx, now.Add(-w.cfg.RetainAllows))
	if err != nil {
		w.logger.ErrorContext(ctx, "purge expired allows failed", "error", err)
		errs = append(errs, err)
	} else if purged > 0 {
		w.logger.InfoContext(ctx, "purged expired allow records", "count", purged)
	}

	// Detach expired partitions
	detached, err := w.manager.DetachExpiredPartitions(ctx, now.Add(-w.cfg.RetainDenials))
	if err != nil {
		w.logger.ErrorContext(ctx, "detach expired partitions failed", "error", err)
		errs = append(errs, err)
	} else if len(detached) > 0 {
		w.logger.InfoContext(ctx, "detached expired partitions", "partitions", detached)
	}

	// Drop detached partitions after grace period
	dropped, err := w.manager.DropDetachedPartitions(ctx, 7*24*time.Hour)
	if err != nil {
		w.logger.ErrorContext(ctx, "drop detached partitions failed", "error", err)
		errs = append(errs, err)
	} else if len(dropped) > 0 {
		w.logger.InfoContext(ctx, "dropped detached partitions", "partitions", dropped)
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

	// Run once immediately unless the caller deferred the first destructive
	// cycle to the first tick (WithSkipFirstRun — round-4 MEDIUM: no prune on
	// a red deploy).
	if !w.skipFirstRun {
		if err := w.RunOnce(ctx); err != nil {
			w.logger.ErrorContext(ctx, "retention cycle failed", "error", err)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.RunOnce(ctx); err != nil {
				w.logger.ErrorContext(ctx, "retention cycle failed", "error", err)
			}
		}
	}
}
