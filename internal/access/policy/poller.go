// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/pkg/errutil"
)

// VersionQuerier queries the database for the latest policy version indicator.
// It returns both the latest timestamp and the total policy count so that
// deletions (which may not change MAX(updated_at)) are also detected.
type VersionQuerier interface {
	LatestPolicyVersion(ctx context.Context) (time.Time, int64, error)
}

// Reloadable is the subset of Cache that the poller needs.
type Reloadable interface {
	Reload(ctx context.Context) error
}

// PollerConfig configures the Poller.
type PollerConfig struct {
	Querier  VersionQuerier
	Reloader Reloadable
	Tracker  *lifecycle.HealthTracker
	Interval time.Duration // default: 10s
}

var (
	pollerPollsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "abac_policy_poller_polls_total",
		Help: "Total poll attempts",
	})
	pollerChangesDetected = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "abac_policy_poller_changes_detected",
		Help: "Polls that found changes",
	})
	pollerErrorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "abac_policy_poller_errors_total",
		Help: "Poll failures",
	})
)

// RegisterPollerMetrics registers poller Prometheus collectors with reg.
// Duplicate registrations are silently ignored; other registration errors panic.
func RegisterPollerMetrics(reg prometheus.Registerer) {
	for _, c := range []prometheus.Collector{pollerPollsTotal, pollerChangesDetected, pollerErrorsTotal} {
		if err := reg.Register(c); err != nil {
			// Ignore AlreadyRegisteredError; re-raise anything else.
			if !isAlreadyRegistered(err) {
				panic(err)
			}
		}
	}
}

// isAlreadyRegistered reports whether err is a prometheus.AlreadyRegisteredError.
func isAlreadyRegistered(err error) bool {
	var are prometheus.AlreadyRegisteredError
	return errors.As(err, &are)
}

// Poller periodically checks the database for policy changes
// and triggers cache reloads when changes are detected.
type Poller struct {
	cfg         PollerConfig
	lastUpdated time.Time
	lastCount   int64
	initialized bool
}

// NewPoller creates a Poller configured with the provided PollerConfig.
// Returns an error if Querier, Reloader, or Tracker is nil. If Interval is
// zero or negative it defaults to 10 seconds.
func NewPoller(cfg PollerConfig) (*Poller, error) {
	if cfg.Querier == nil {
		return nil, fmt.Errorf("policy poller: Querier is required")
	}
	if cfg.Reloader == nil {
		return nil, fmt.Errorf("policy poller: Reloader is required")
	}
	if cfg.Tracker == nil {
		return nil, fmt.Errorf("policy poller: Tracker is required")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 10 * time.Second
	}
	return &Poller{cfg: cfg}, nil
}

// Run starts the polling loop. It blocks until the context is cancelled.
func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.Interval)
	defer ticker.Stop()

	// Immediate first poll
	p.poll(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

func (p *Poller) poll(ctx context.Context) {
	pollerPollsTotal.Inc()

	latest, count, err := p.cfg.Querier.LatestPolicyVersion(ctx)
	if err != nil {
		pollerErrorsTotal.Inc()
		errutil.LogErrorContext(ctx, "policy poller: query failed", err)
		p.cfg.Tracker.RecordFailure("poll query failed: " + err.Error())
		return
	}

	// First poll: establish baseline and reload.
	// IMPORTANT: only mark initialized AFTER reload succeeds to ensure retry on failure.
	if !p.initialized {
		if reloadErr := p.cfg.Reloader.Reload(ctx); reloadErr != nil {
			pollerErrorsTotal.Inc()
			errutil.LogErrorContext(ctx, "policy poller: initial reload failed", reloadErr)
			p.cfg.Tracker.RecordFailure("initial reload failed: " + reloadErr.Error())
			return
		}
		p.lastUpdated = latest
		p.lastCount = count
		p.initialized = true
		p.cfg.Tracker.RecordSuccess()
		slog.InfoContext(ctx, "policy poller: initial baseline established")
		return
	}

	// Detect changes via timestamp OR count (count catches deletes that
	// don't change MAX(updated_at)).
	changed := latest.After(p.lastUpdated) || count != p.lastCount
	if !changed {
		p.cfg.Tracker.RecordSuccess()
		return
	}

	// Change detected — reload
	pollerChangesDetected.Inc()
	slog.InfoContext(
		ctx, "policy poller: change detected, reloading cache",
		"previous_ts", p.lastUpdated,
		"latest_ts", latest,
		"previous_count", p.lastCount,
		"latest_count", count,
	)

	if reloadErr := p.cfg.Reloader.Reload(ctx); reloadErr != nil {
		pollerErrorsTotal.Inc()
		errutil.LogErrorContext(ctx, "policy poller: reload failed", reloadErr)
		p.cfg.Tracker.RecordFailure("reload failed: " + reloadErr.Error())
		return
	}

	p.lastUpdated = latest
	p.lastCount = count
	p.cfg.Tracker.RecordSuccess()
}
