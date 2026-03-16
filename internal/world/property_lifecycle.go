// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/samber/oops"
)

// OrphanConfig configures the property orphan cleanup goroutine.
type OrphanConfig struct {
	Interval    time.Duration
	GracePeriod time.Duration
	Threshold   int
}

// DefaultOrphanConfig returns production defaults.
func DefaultOrphanConfig() OrphanConfig {
	return OrphanConfig{
		Interval:    24 * time.Hour,
		GracePeriod: 24 * time.Hour,
		Threshold:   100,
	}
}

// OrphanFinder finds orphaned properties in the database.
type OrphanFinder interface {
	CountOrphans(ctx context.Context) (int, error)
	DeleteOrphans(ctx context.Context, olderThan time.Duration) (int, error)
}

// OrphanDetector manages periodic orphan detection and cleanup.
type OrphanDetector struct {
	config    OrphanConfig
	finder    OrphanFinder
	mu        sync.Mutex
	lastSeen  int
	stopCh    chan struct{}
	stopped   chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
}

// NewOrphanDetector creates a new orphan detector.
// Silently defaults a non-positive interval to 24h.
func NewOrphanDetector(config OrphanConfig) *OrphanDetector {
	if config.Interval <= 0 {
		config.Interval = DefaultOrphanConfig().Interval
	}
	return &OrphanDetector{
		config:  config,
		stopCh:  make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// SetFinder sets the OrphanFinder implementation.
// Must be called before Start or StartupCheck.
func (d *OrphanDetector) SetFinder(finder OrphanFinder) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.finder = finder
}

// StartupCheck counts orphans on server startup and logs appropriately.
func (d *OrphanDetector) StartupCheck(ctx context.Context) error {
	d.mu.Lock()
	finder := d.finder
	d.mu.Unlock()

	if finder == nil {
		return nil
	}

	count, err := finder.CountOrphans(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "orphan startup check failed", "error", err)
		return oops.Code("ORPHAN_STARTUP_CHECK_FAILED").Wrap(err)
	}

	if count > d.config.Threshold {
		slog.ErrorContext(ctx, "orphan count exceeds threshold on startup",
			"count", count,
			"threshold", d.config.Threshold,
		)
	} else if count > 0 {
		slog.WarnContext(ctx, "orphaned properties detected on startup",
			"count", count,
		)
	}

	d.mu.Lock()
	d.lastSeen = count
	d.mu.Unlock()

	return nil
}

// Start begins the periodic orphan cleanup goroutine.
// Idempotent: subsequent calls are no-ops.
func (d *OrphanDetector) Start(ctx context.Context) {
	d.startOnce.Do(func() {
		go func() {
			defer close(d.stopped)
			ticker := time.NewTicker(d.config.Interval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-d.stopCh:
					return
				case <-ticker.C:
					d.RunCleanup(ctx)
				}
			}
		}()
	})
}

// Stop stops the cleanup goroutine.
// Idempotent: subsequent calls are no-ops.
func (d *OrphanDetector) Stop() {
	d.stopOnce.Do(func() {
		close(d.stopCh)
		<-d.stopped
	})
}

// RunCleanup performs a single cleanup cycle.
func (d *OrphanDetector) RunCleanup(ctx context.Context) {
	d.mu.Lock()
	finder := d.finder
	d.mu.Unlock()

	if finder == nil {
		return
	}

	count, err := finder.CountOrphans(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "orphan scan failed", "error", err)
		return
	}

	if count == 0 {
		d.mu.Lock()
		d.lastSeen = 0
		d.mu.Unlock()
		return
	}

	d.mu.Lock()
	prevSeen := d.lastSeen
	d.lastSeen = count
	d.mu.Unlock()

	if prevSeen == 0 {
		slog.WarnContext(ctx, "orphaned properties detected",
			"count", count,
		)
		return
	}

	deleted, err := finder.DeleteOrphans(ctx, d.config.GracePeriod)
	if err != nil {
		slog.ErrorContext(ctx, "orphan cleanup failed", "error", err)
		return
	}

	if deleted > 0 {
		slog.InfoContext(ctx, "orphaned properties cleaned up",
			"deleted", deleted,
		)
	}
}
