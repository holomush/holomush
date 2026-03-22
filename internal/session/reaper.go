// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session

import (
	"context"
	"log/slog"
	"time"
)

// ReaperConfig configures the session reaper.
type ReaperConfig struct {
	Interval  time.Duration    // how often to check for expired sessions
	OnExpired func(info *Info) // callback for each expired session (emit leave events, etc.)
}

// Reaper periodically checks for and cleans up expired detached sessions.
type Reaper struct {
	store  Store
	config ReaperConfig
}

// NewReaper creates a new session reaper.
func NewReaper(store Store, config ReaperConfig) *Reaper {
	if config.Interval <= 0 {
		config.Interval = 30 * time.Second
	}
	return &Reaper{
		store:  store,
		config: config,
	}
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
			slog.WarnContext(ctx, "reaper: failed to expire session",
				"session_id", info.ID,
				"error", err,
			)
			continue
		}

		if err := r.store.Delete(ctx, info.ID, "Session expired due to inactivity."); err != nil {
			slog.WarnContext(ctx, "reaper: failed to delete expired session",
				"session_id", info.ID,
				"error", err,
			)
		}

		slog.InfoContext(ctx, "reaper: expired session cleaned up",
			"session_id", info.ID,
			"character_name", info.CharacterName,
			"is_guest", info.IsGuest,
		)
	}
}
