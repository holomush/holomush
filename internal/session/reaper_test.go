// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReaper_ReapsExpiredSessions(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	past := time.Now().Add(-1 * time.Hour)
	info := &Info{
		ID:            "expired-session",
		CharacterID:   ulid.Make(),
		CharacterName: "Ghost",
		Status:        StatusDetached,
		ExpiresAt:     &past,
		IsGuest:       false,
	}
	require.NoError(t, store.Set(ctx, "expired-session", info))

	var reaped []Info
	reaper := NewReaper(store, ReaperConfig{
		Interval:  100 * time.Millisecond,
		OnExpired: func(info *Info) { reaped = append(reaped, *info) },
	})

	reaperCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	reaper.Run(reaperCtx)

	assert.Len(t, reaped, 1)
	assert.Equal(t, "Ghost", reaped[0].CharacterName)

	// Session should be deleted
	_, err := store.Get(ctx, "expired-session")
	assert.Error(t, err)
}

func TestReaper_SkipsActiveAndFutureSessions(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	future := time.Now().Add(1 * time.Hour)
	require.NoError(t, store.Set(ctx, "active", &Info{
		ID:     "active",
		Status: StatusActive,
	}))
	require.NoError(t, store.Set(ctx, "future", &Info{
		ID:        "future",
		Status:    StatusDetached,
		ExpiresAt: &future,
	}))

	var reaped []Info
	reaper := NewReaper(store, ReaperConfig{
		Interval:  100 * time.Millisecond,
		OnExpired: func(info *Info) { reaped = append(reaped, *info) },
	})

	reaperCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	reaper.Run(reaperCtx)

	assert.Empty(t, reaped)
}
