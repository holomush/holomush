// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
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
	config   OrphanConfig
	finder   OrphanFinder
	mu       sync.Mutex
	lastSeen int
	stopCh   chan struct{}
	stopped  chan struct{}
}

// NewOrphanDetector creates a new orphan detector.
func NewOrphanDetector(config OrphanConfig) *OrphanDetector {
	return &OrphanDetector{
		config:  config,
		stopCh:  make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// SetFinder sets the OrphanFinder implementation.
func (d *OrphanDetector) SetFinder(finder OrphanFinder) {
	d.finder = finder
}

// StartupCheck counts orphans on server startup and logs appropriately.
func (d *OrphanDetector) StartupCheck(ctx context.Context) error {
	if d.finder == nil {
		return nil
	}

	count, err := d.finder.CountOrphans(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "orphan startup check failed", "error", err)
		return fmt.Errorf("orphan startup check: %w", err)
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
func (d *OrphanDetector) Start(ctx context.Context) {
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
}

// Stop stops the cleanup goroutine.
func (d *OrphanDetector) Stop() {
	close(d.stopCh)
	<-d.stopped
}

// RunCleanup performs a single cleanup cycle.
func (d *OrphanDetector) RunCleanup(ctx context.Context) {
	if d.finder == nil {
		return
	}

	count, err := d.finder.CountOrphans(ctx)
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

	deleted, err := d.finder.DeleteOrphans(ctx, d.config.GracePeriod)
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
