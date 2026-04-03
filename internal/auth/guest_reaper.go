// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
	"context"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
)

// GuestPlayerLister lists guest players eligible for reaping.
type GuestPlayerLister interface {
	ListIdleGuests(ctx context.Context, idleSince time.Time) ([]*Player, error)
}

// GuestCleaner deletes a guest player and all associated data.
type GuestCleaner interface {
	DeleteGuestPlayer(ctx context.Context, playerID ulid.ULID) error
}

// GuestReaperConfig configures the guest reaper.
type GuestReaperConfig struct {
	Interval time.Duration            // how often to scan (default: 1m)
	IdleTTL  time.Duration            // delete after this idle duration (default: 10m)
	OnReaped func(playerID ulid.ULID) // optional callback for each reaped guest
}

// GuestReaper periodically cleans up idle guest players and their data.
type GuestReaper struct {
	config  GuestReaperConfig
	lister  GuestPlayerLister
	cleaner GuestCleaner
}

// NewGuestReaper creates a new guest reaper with the given config and dependencies.
func NewGuestReaper(config GuestReaperConfig, lister GuestPlayerLister, cleaner GuestCleaner) *GuestReaper {
	if config.Interval <= 0 {
		config.Interval = 1 * time.Minute
	}
	if config.IdleTTL <= 0 {
		config.IdleTTL = 10 * time.Minute
	}
	return &GuestReaper{
		config:  config,
		lister:  lister,
		cleaner: cleaner,
	}
}

// Run starts the reaper loop. Blocks until context is cancelled.
func (r *GuestReaper) Run(ctx context.Context) {
	ticker := time.NewTicker(r.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reap(ctx)
		}
	}
}

func (r *GuestReaper) reap(ctx context.Context) {
	idleSince := time.Now().Add(-r.config.IdleTTL)

	guests, err := r.lister.ListIdleGuests(ctx, idleSince)
	if err != nil {
		slog.WarnContext(ctx, "guest_reaper: failed to list idle guests", "error", err)
		return
	}

	for _, guest := range guests {
		func() {
			defer func() {
				if p := recover(); p != nil {
					slog.WarnContext(ctx, "guest_reaper: panic during reap",
						"player_id", guest.ID,
						"username", guest.Username,
						"panic", p,
					)
				}
			}()

			if err := r.cleaner.DeleteGuestPlayer(ctx, guest.ID); err != nil {
				slog.WarnContext(ctx, "guest_reaper: failed to delete guest player",
					"player_id", guest.ID,
					"username", guest.Username,
					"error", err,
				)
				return
			}

			slog.InfoContext(ctx, "guest_reaper: reaped idle guest player",
				"player_id", guest.ID,
				"username", guest.Username,
			)

			if r.config.OnReaped != nil {
				r.config.OnReaped(guest.ID)
			}
		}()
	}
}
