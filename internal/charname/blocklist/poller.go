// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package blocklist

import (
	"context"
	"log/slog"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/pkg/errutil"
)

// defaultPollInterval mirrors policy.Poller's own default.
const defaultPollInterval = 10 * time.Second

// VersionQuerier returns a change indicator for one settings row: the PAIR
// (updated_at, hash-of-value).
//
// # Why two signals
//
// updated_at is maintained only by the settings writer's
// `ON CONFLICT … DO UPDATE SET value = $2, updated_at = …` clause
// (store.SetSystemInfo). Migrations forbid triggers, and v0.13 ships no editing
// surface for this key, so the ONLY edit path is direct SQL:
//
//	UPDATE holomush_system_info SET value = '["^admin$"]' WHERE key = '…';
//
// That statement leaves updated_at exactly where it was. A poller watching
// updated_at alone would therefore never observe the only edit that actually
// happens — the operator's list would silently never take effect, on any
// replica, with nothing to see in a log.
//
// The content digest (`md5(value)` in the production query,
// store.SystemInfoVersion) closes that gap; updated_at is kept alongside it so
// that a same-content rewrite is still observed. This mirrors
// policy.VersionQuerier, which returns a timestamp AND a count for the
// structurally identical reason: a deletion may not move MAX(updated_at).
//
// A missing row is NOT an error: it yields the zero indicator (0, "").
type VersionQuerier interface {
	SystemInfoVersion(ctx context.Context, key string) (updatedAt int64, valueHash string, err error)
}

// Reloadable is the subset of Cache the poller needs.
type Reloadable interface {
	Reload(ctx context.Context) error
}

// PollerConfig configures the Poller.
type PollerConfig struct {
	Querier  VersionQuerier
	Reloader Reloadable
	Tracker  *lifecycle.HealthTracker
	Key      string        // default: DefaultKey
	Interval time.Duration // default: 10s
}

// Poller periodically checks the block-list settings row for changes and
// triggers a cache reload when the indicator moves.
type Poller struct {
	cfg         PollerConfig
	lastUpdated int64
	lastHash    string
	initialized bool
}

// NewPoller creates a Poller. It returns an error if Querier, Reloader or
// Tracker is nil. A zero or negative Interval defaults to 10s and an empty Key
// defaults to DefaultKey.
func NewPoller(cfg PollerConfig) (*Poller, error) {
	if cfg.Querier == nil {
		return nil, oops.Code("BLOCKLIST_POLLER_MISCONFIGURED").New("blocklist poller: Querier is required")
	}
	if cfg.Reloader == nil {
		return nil, oops.Code("BLOCKLIST_POLLER_MISCONFIGURED").New("blocklist poller: Reloader is required")
	}
	if cfg.Tracker == nil {
		return nil, oops.Code("BLOCKLIST_POLLER_MISCONFIGURED").New("blocklist poller: Tracker is required")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultPollInterval
	}
	if cfg.Key == "" {
		cfg.Key = DefaultKey
	}
	return &Poller{cfg: cfg}, nil
}

// Interval returns the resolved poll interval, after defaulting.
func (p *Poller) Interval() time.Duration { return p.cfg.Interval }

// Run polls until ctx is cancelled. The first poll is immediate, so a boot
// does not wait a whole interval before the first refresh.
func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.Interval)
	defer ticker.Stop()

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
	updatedAt, hash, err := p.cfg.Querier.SystemInfoVersion(ctx, p.cfg.Key)
	if err != nil {
		errutil.LogErrorContext(ctx, "block-list poller: version query failed", err, "key", p.cfg.Key)
		p.cfg.Tracker.RecordFailure("poll query failed: " + err.Error())
		return
	}

	// First poll: establish the baseline and reload. The baseline is recorded
	// ONLY after the reload succeeds, so a failed first reload (a malformed
	// value written between boot and the first tick, say) is retried on every
	// subsequent tick instead of being frozen out by an already-recorded
	// indicator.
	if !p.initialized {
		if reloadErr := p.cfg.Reloader.Reload(ctx); reloadErr != nil {
			errutil.LogErrorContext(ctx, "block-list poller: initial reload failed", reloadErr, "key", p.cfg.Key)
			p.cfg.Tracker.RecordFailure("initial reload failed: " + reloadErr.Error())
			return
		}
		p.lastUpdated, p.lastHash, p.initialized = updatedAt, hash, true
		p.cfg.Tracker.RecordSuccess()
		slog.InfoContext(ctx, "block-list poller: initial baseline established", "key", p.cfg.Key)
		return
	}

	if updatedAt == p.lastUpdated && hash == p.lastHash {
		p.cfg.Tracker.RecordSuccess()
		return
	}

	slog.InfoContext(
		ctx, "block-list poller: change detected, reloading",
		"key", p.cfg.Key,
		"previous_updated_at", p.lastUpdated,
		"latest_updated_at", updatedAt,
		"hash_changed", hash != p.lastHash,
	)

	if reloadErr := p.cfg.Reloader.Reload(ctx); reloadErr != nil {
		// The last valid list stays in force (Cache.Reload swaps only on
		// success). The indicator is deliberately NOT advanced, so the next
		// tick retries the same edit rather than treating a failed reload as
		// applied.
		errutil.LogErrorContext(ctx, "block-list poller: reload failed; last valid list stays in force",
			reloadErr, "key", p.cfg.Key)
		p.cfg.Tracker.RecordFailure("reload failed: " + reloadErr.Error())
		return
	}

	p.lastUpdated, p.lastHash = updatedAt, hash
	p.cfg.Tracker.RecordSuccess()
}
