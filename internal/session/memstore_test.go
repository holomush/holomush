// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session

import (
	"context"
	"fmt"
	"sync"
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
	require.NoError(t, store.Delete(ctx, "session-1", "test"))

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

func TestMemStore_UpdateGridPresent(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	info := &Info{ID: "session-1", Status: StatusActive, GridPresent: false}
	require.NoError(t, store.Set(ctx, "session-1", info))

	require.NoError(t, store.UpdateGridPresent(ctx, "session-1", true))

	got, err := store.Get(ctx, "session-1")
	require.NoError(t, err)
	assert.True(t, got.GridPresent)

	require.NoError(t, store.UpdateGridPresent(ctx, "session-1", false))

	got, err = store.Get(ctx, "session-1")
	require.NoError(t, err)
	assert.False(t, got.GridPresent)
}

func TestMemStore_UpdateGridPresent_NotFound(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	err := store.UpdateGridPresent(ctx, "nonexistent", true)
	assert.Error(t, err)
}

func TestMemStore_AddConnection_InvalidClientType(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	conn := &Connection{
		ID:         ulid.Make(),
		SessionID:  "session-1",
		ClientType: "unknown_type",
		Streams:    []string{},
	}
	err := store.AddConnection(ctx, conn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown_type")
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

func TestMemStore_WatchSession_NotifiesOnDelete(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()
	require.NoError(t, store.Set(ctx, "sess-1", &Info{ID: "sess-1"}))

	ch, err := store.WatchSession(ctx, "sess-1")
	require.NoError(t, err)

	require.NoError(t, store.Delete(ctx, "sess-1", "Goodbye!"))

	ev, ok := <-ch
	require.True(t, ok, "expected event on channel")
	assert.Equal(t, SessionDestroyed, ev.Type)
	assert.Equal(t, "Goodbye!", ev.Message)
}

func TestMemStore_WatchSession_ChannelClosedOnDelete(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()
	require.NoError(t, store.Set(ctx, "sess-1", &Info{ID: "sess-1"}))

	ch, err := store.WatchSession(ctx, "sess-1")
	require.NoError(t, err)

	require.NoError(t, store.Delete(ctx, "sess-1", "test"))

	// Drain the event
	<-ch
	// Channel should be closed after event
	_, ok := <-ch
	assert.False(t, ok, "channel should be closed")
}

func TestMemStore_ConcurrentAccess(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	const goroutines = 10
	const opsPerGoroutine = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(n int) {
			defer wg.Done()
			for j := range opsPerGoroutine {
				id := fmt.Sprintf("session-%d-%d", n, j)
				info := &Info{ID: id, Status: StatusActive}

				// interleave Set, Get, and Delete without caring about errors
				// (sessions may not exist yet); the goal is to detect data races.
				_ = store.Set(ctx, id, info)
				_, _ = store.Get(ctx, id)
				_ = store.Delete(ctx, id, "test")
			}
		}(i)
	}

	wg.Wait()
	// No assertion needed — the test passes if the race detector finds no issues.
	assert.True(t, true)
}
