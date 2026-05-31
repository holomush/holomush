// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session

import (
	"context"
	"log/slog"
	"time"

	"github.com/samber/oops"
)

// ReaperConfig configures the session reaper.
type ReaperConfig struct {
	Interval  time.Duration    // how often to check for expired sessions
	OnExpired func(info *Info) // callback for each expired session (emit leave events, etc.)

	// LeaseTTL: a connection whose last_seen_at is older than now-LeaseTTL is
	// swept (holomush-rsoe6, I-LIVE-2). Zero disables the lease sweep.
	LeaseTTL time.Duration
	// BootGrace suppresses the lease sweep for this long after Run starts, so a
	// surviving gateway re-asserts its leases before any reaping (I-LIVE-4).
	BootGrace time.Duration
	// Now is injectable for tests; defaults to time.Now.
	Now func() time.Time
	// OnSessionDetached fires when the sweep detaches a session (last live
	// connection removed). Leave events stay deferred to OnExpired at TTL.
	OnSessionDetached func(info *Info)
	// OnGridPhaseOut fires when grid_present falls true→false but the session
	// stays active (a non-grid connection remains).
	OnGridPhaseOut func(info *Info)
}

// Reaper periodically checks for and cleans up expired detached sessions.
type Reaper struct {
	store  Store
	config ReaperConfig
	bootAt time.Time
}

// NewReaper creates a new session reaper.
func NewReaper(store Store, config ReaperConfig) *Reaper {
	if config.Interval <= 0 {
		config.Interval = 30 * time.Second
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	r := &Reaper{store: store, config: config}
	r.bootAt = config.Now()
	return r
}

// Run starts the reaper loop. Blocks until context is cancelled.
func (r *Reaper) Run(ctx context.Context) {
	ticker := time.NewTicker(r.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reapExpired(ctx)
			if r.config.LeaseTTL > 0 && r.config.Now().Sub(r.bootAt) >= r.config.BootGrace {
				r.reapLapsedConnections(ctx)
			}
		}
	}
}

func (r *Reaper) reapExpired(ctx context.Context) {
	expired, err := r.store.ListExpired(ctx)
	if err != nil {
		slog.WarnContext(ctx, "reaper: failed to list expired sessions", "error", err)
		return
	}

	for _, info := range expired {
		// Notify callback (emit leave events, release guest characters).
		// Wrapped in a closure with panic recovery so a misbehaving callback
		// cannot abort reaping of remaining sessions.
		if r.config.OnExpired != nil {
			func() {
				defer func() {
					if p := recover(); p != nil {
						slog.WarnContext(ctx, "reaper: OnExpired callback panicked",
							"session_id", info.ID, "panic", p)
					}
				}()
				r.config.OnExpired(info)
			}()
		}

		// Mark as expired and delete
		if err := r.store.UpdateStatus(ctx, info.ID, StatusExpired, nil, nil); err != nil {
			slog.WarnContext(
				ctx, "reaper: failed to expire session",
				"session_id", info.ID,
				"error", err,
			)
			continue
		}

		if err := r.store.Delete(ctx, info.ID); err != nil {
			slog.WarnContext(
				ctx, "reaper: failed to delete expired session",
				"session_id", info.ID,
				"error", err,
			)
		}

		slog.InfoContext(
			ctx, "reaper: expired session cleaned up",
			"session_id", info.ID,
			"character_name", info.CharacterName,
			"is_guest", info.IsGuest,
		)
	}
}

func (r *Reaper) reapLapsedConnections(ctx context.Context) {
	cutoff := r.config.Now().Add(-r.config.LeaseTTL)
	lapsed, err := r.store.ListLapsedConnections(ctx, cutoff)
	if err != nil {
		slog.WarnContext(ctx, "reaper: list lapsed connections failed", "error", err)
		return
	}
	affected := make(map[string]struct{})
	for _, lc := range lapsed {
		if rmErr := r.store.RemoveConnection(ctx, lc.ID); rmErr != nil {
			slog.WarnContext(ctx, "reaper: remove lapsed connection failed",
				"connection_id", lc.ID.String(), "error", rmErr)
			continue
		}
		affected[lc.SessionID] = struct{}{}
	}
	for sessionID := range affected {
		r.recomputeAfterSweep(ctx, sessionID)
	}
}

func (r *Reaper) recomputeAfterSweep(ctx context.Context, sessionID string) {
	info, err := r.store.Get(ctx, sessionID)
	if err != nil {
		if oopsErr, ok := oops.AsOops(err); !ok || oopsErr.Code() != "SESSION_NOT_FOUND" {
			slog.WarnContext(ctx, "reaper: get session for recompute failed", "session_id", sessionID, "error", err)
		}
		return // session already gone (not-found) or transient error logged above
	}
	if info.Status != StatusActive {
		return
	}
	total, err := r.store.CountConnections(ctx, sessionID)
	if err != nil {
		slog.WarnContext(ctx, "reaper: count connections failed", "session_id", sessionID, "error", err)
		return
	}
	if total == 0 {
		ttl := time.Duration(info.TTLSeconds) * time.Second
		if ttl <= 0 {
			ttl = 30 * time.Minute // default reattach window; matches recomputeSessionLiveness (server.go) 1800s fallback
		}
		now := r.config.Now()
		expiresAt := now.Add(ttl)
		if updErr := r.store.UpdateStatus(ctx, sessionID, StatusDetached, &now, &expiresAt); updErr != nil {
			slog.WarnContext(ctx, "reaper: detach after sweep failed", "session_id", sessionID, "error", updErr)
			return
		}
		// The last connection is gone, so the session is no longer grid-present.
		// Roster queries (ListActiveByLocation) filter on grid_present, so leaving
		// it set would keep a lease-swept session visible until TTL expiry.
		if info.GridPresent {
			if updErr := r.store.UpdateGridPresent(ctx, sessionID, false); updErr != nil {
				slog.WarnContext(ctx, "reaper: clear grid_present after detach failed", "session_id", sessionID, "error", updErr)
				return
			}
		}
		r.safeCallback(ctx, "OnSessionDetached", r.config.OnSessionDetached, info)
		return
	}
	term, err := r.store.CountConnectionsByType(ctx, sessionID, "terminal")
	if err != nil {
		slog.WarnContext(ctx, "reaper: count terminal connections failed", "session_id", sessionID, "error", err)
		return
	}
	tel, err := r.store.CountConnectionsByType(ctx, sessionID, "telnet")
	if err != nil {
		slog.WarnContext(ctx, "reaper: count telnet connections failed", "session_id", sessionID, "error", err)
		return
	}
	gridPresent := term+tel > 0
	if !gridPresent && info.GridPresent {
		if updErr := r.store.UpdateGridPresent(ctx, sessionID, false); updErr != nil {
			slog.WarnContext(ctx, "reaper: clear grid_present after sweep failed", "session_id", sessionID, "error", updErr)
			return
		}
		r.safeCallback(ctx, "OnGridPhaseOut", r.config.OnGridPhaseOut, info)
	}
}

// safeCallback invokes fn (if non-nil) with panic recovery, so a misbehaving
// reaper callback cannot abort the sweep/reap loop.
func (r *Reaper) safeCallback(ctx context.Context, name string, fn func(info *Info), info *Info) {
	if fn == nil {
		return
	}
	defer func() {
		if p := recover(); p != nil {
			slog.WarnContext(ctx, "reaper: callback panicked", "callback", name, "session_id", info.ID, "panic", p)
		}
	}()
	fn(info)
}
