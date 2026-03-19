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

func TestMemStore_GetSet(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	info := &Info{
		ID:            "session-1",
		CharacterID:   ulid.Make(),
		CharacterName: "TestChar",
		Status:        StatusActive,
		EventCursors:  map[string]ulid.ULID{},
	}

	require.NoError(t, store.Set(ctx, "session-1", info))

	got, err := store.Get(ctx, "session-1")
	require.NoError(t, err)
	assert.Equal(t, "TestChar", got.CharacterName)
}

func TestMemStore_Get_NotFound(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	_, err := store.Get(ctx, "nonexistent")
	assert.Error(t, err)
}

func TestMemStore_Delete(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	info := &Info{ID: "session-1", Status: StatusActive}
	require.NoError(t, store.Set(ctx, "session-1", info))
	require.NoError(t, store.Delete(ctx, "session-1"))

	_, err := store.Get(ctx, "session-1")
	assert.Error(t, err)
}

func TestMemStore_FindByCharacter(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()
	charID := ulid.Make()

	info := &Info{
		ID:          "session-1",
		CharacterID: charID,
		Status:      StatusDetached,
	}
	require.NoError(t, store.Set(ctx, "session-1", info))

	got, err := store.FindByCharacter(ctx, charID)
	require.NoError(t, err)
	assert.Equal(t, "session-1", got.ID)
}

func TestMemStore_FindByCharacter_SkipsExpired(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()
	charID := ulid.Make()

	info := &Info{
		ID:          "session-1",
		CharacterID: charID,
		Status:      StatusExpired,
	}
	require.NoError(t, store.Set(ctx, "session-1", info))

	_, err := store.FindByCharacter(ctx, charID)
	assert.Error(t, err)
}

func TestMemStore_ReattachCAS(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	info := &Info{ID: "session-1", Status: StatusDetached}
	require.NoError(t, store.Set(ctx, "session-1", info))

	ok, err := store.ReattachCAS(ctx, "session-1")
	require.NoError(t, err)
	assert.True(t, ok)

	got, err := store.Get(ctx, "session-1")
	require.NoError(t, err)
	assert.Equal(t, StatusActive, got.Status)

	// Second CAS fails — already active
	ok, err = store.ReattachCAS(ctx, "session-1")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestMemStore_ConnectionTracking(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	info := &Info{ID: "session-1", Status: StatusActive}
	require.NoError(t, store.Set(ctx, "session-1", info))

	connID := ulid.Make()
	conn := &Connection{
		ID:          connID,
		SessionID:   "session-1",
		ClientType:  "terminal",
		Streams:     []string{"location:abc"},
		ConnectedAt: time.Now(),
	}
	require.NoError(t, store.AddConnection(ctx, conn))

	count, err := store.CountConnections(ctx, "session-1")
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	require.NoError(t, store.RemoveConnection(ctx, connID))

	count, err = store.CountConnections(ctx, "session-1")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestMemStore_AppendCommand(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	info := &Info{ID: "session-1", Status: StatusActive, CommandHistory: []string{}}
	require.NoError(t, store.Set(ctx, "session-1", info))

	require.NoError(t, store.AppendCommand(ctx, "session-1", "say hello", 3))
	require.NoError(t, store.AppendCommand(ctx, "session-1", "pose waves", 3))
	require.NoError(t, store.AppendCommand(ctx, "session-1", "look", 3))
	require.NoError(t, store.AppendCommand(ctx, "session-1", "say bye", 3))

	history, err := store.GetCommandHistory(ctx, "session-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"pose waves", "look", "say bye"}, history)
}

func TestMemStore_ListByPlayer_ReturnsAllNonExpired(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	active := &Info{ID: "session-1", Status: StatusActive}
	detached := &Info{ID: "session-2", Status: StatusDetached}
	expired := &Info{ID: "session-3", Status: StatusExpired}

	require.NoError(t, store.Set(ctx, "session-1", active))
	require.NoError(t, store.Set(ctx, "session-2", detached))
	require.NoError(t, store.Set(ctx, "session-3", expired))

	// MemStore stub: ignores playerID, returns all non-expired sessions
	results, err := store.ListByPlayer(ctx, ulid.Make())
	require.NoError(t, err)
	assert.Len(t, results, 2) // active + detached, not expired
}

func TestMemStore_ListExpired(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	past := time.Now().Add(-1 * time.Hour)
	info := &Info{
		ID:        "session-1",
		Status:    StatusDetached,
		ExpiresAt: &past,
	}
	require.NoError(t, store.Set(ctx, "session-1", info))

	expired, err := store.ListExpired(ctx)
	require.NoError(t, err)
	assert.Len(t, expired, 1)
}
