// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package sessiontest_test

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/sessiontest"
)

// TestNewStore_ReturnsUsableSessionStore verifies the helper returns a
// session.Store backed by real Postgres and that basic Set/Get round-trips
// work (proves the pool wiring + schema migrations applied).
func TestNewStore_ReturnsUsableSessionStore(t *testing.T) {
	store := sessiontest.NewStore(t)
	ctx := context.Background()

	info := &session.Info{
		ID:            "test-session-1",
		CharacterID:   ulid.Make(),
		CharacterName: "TestChar",
		Status:        session.StatusActive,
	}
	require.NoError(t, store.Set(ctx, info.ID, info))

	got, err := store.Get(ctx, info.ID)
	require.NoError(t, err)
	assert.Equal(t, info.CharacterName, got.CharacterName)
}

// TestNewStore_IsolatedBetweenCalls verifies INV-SESSION-2: each NewStore call
// returns a store backed by a fresh database. State from a prior call MUST
// NOT be visible in a subsequent call.
func TestNewStore_IsolatedBetweenCalls(t *testing.T) {
	ctx := context.Background()

	storeA := sessiontest.NewStore(t)
	info := &session.Info{
		ID:            "iso-test",
		CharacterID:   ulid.Make(),
		CharacterName: "InA",
		Status:        session.StatusActive,
	}
	require.NoError(t, storeA.Set(ctx, info.ID, info))

	storeB := sessiontest.NewStore(t)
	_, err := storeB.Get(ctx, info.ID)
	require.Error(t, err, "INV-SESSION-2: storeB MUST NOT see state from storeA")
}
