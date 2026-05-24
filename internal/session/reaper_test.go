// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/sessiontest"
)

func TestReaper_ReapsExpiredSessions(t *testing.T) {
	store := sessiontest.NewStore(t)
	ctx := context.Background()

	past := time.Now().Add(-1 * time.Hour)
	info := &session.Info{
		ID:            "expired-session",
		CharacterID:   ulid.Make(),
		CharacterName: "Ghost",
		Status:        session.StatusDetached,
		ExpiresAt:     &past,
		IsGuest:       false,
	}
	require.NoError(t, store.Set(ctx, "expired-session", info))

	var reaped []session.Info
	reaper := session.NewReaper(store, session.ReaperConfig{
		Interval:  50 * time.Millisecond,
		OnExpired: func(info *session.Info) { reaped = append(reaped, *info) },
	})

	reaperCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	reaper.Run(reaperCtx)

	assert.Len(t, reaped, 1)
	assert.Equal(t, "Ghost", reaped[0].CharacterName)

	// Session should be deleted
	_, err := store.Get(ctx, "expired-session")
	assert.Error(t, err)
}

func TestReaper_SkipsActiveAndFutureSessions(t *testing.T) {
	store := sessiontest.NewStore(t)
	ctx := context.Background()

	future := time.Now().Add(1 * time.Hour)
	require.NoError(t, store.Set(ctx, "active", &session.Info{
		ID:          "active",
		CharacterID: ulid.Make(),
		Status:      session.StatusActive,
	}))
	require.NoError(t, store.Set(ctx, "future", &session.Info{
		ID:          "future",
		CharacterID: ulid.Make(),
		Status:      session.StatusDetached,
		ExpiresAt:   &future,
	}))

	var reaped []session.Info
	reaper := session.NewReaper(store, session.ReaperConfig{
		Interval:  50 * time.Millisecond,
		OnExpired: func(info *session.Info) { reaped = append(reaped, *info) },
	})

	reaperCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	reaper.Run(reaperCtx)

	assert.Empty(t, reaped)
}
