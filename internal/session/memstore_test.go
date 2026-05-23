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
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestMemStore_GetSet(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	info := &Info{
		ID:            "session-1",
		CharacterID:   ulid.Make(),
		CharacterName: "TestChar",
		Status:        StatusActive,
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

func TestMemStore_ConcurrentAccess(_ *testing.T) {
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
				_ = store.Delete(ctx, id)
			}
		}(i)
	}

	wg.Wait()
	// No assertion needed — the test passes if the race detector finds no issues.
}

func TestMemStore_FindByCharacterName(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		lookup    string
		wantName  string
		wantFound bool
	}{
		{
			name:      "exact match succeeds",
			lookup:    "Artanis",
			wantName:  "Artanis",
			wantFound: true,
		},
		{
			name:      "case-insensitive match succeeds",
			lookup:    "artanis",
			wantName:  "Artanis",
			wantFound: true,
		},
		{
			name:      "upper-case lookup succeeds",
			lookup:    "ARTANIS",
			wantName:  "Artanis",
			wantFound: true,
		},
		{
			name:      "no match returns SESSION_NOT_FOUND",
			lookup:    "Zeratul",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemStore()
			charID := ulid.Make()
			err := store.Set(ctx, "session-1", &Info{
				ID:            "session-1",
				CharacterID:   charID,
				CharacterName: "Artanis",
				Status:        StatusActive,
			})
			require.NoError(t, err)

			got, err := store.FindByCharacterName(ctx, tt.lookup)
			if tt.wantFound {
				require.NoError(t, err)
				assert.Equal(t, tt.wantName, got.CharacterName)
			} else {
				require.Error(t, err)
				assert.Nil(t, got)
			}
		})
	}
}

func TestMemStore_FindByCharacterName_OnlyActiveSession(t *testing.T) {
	ctx := context.Background()
	store := NewMemStore()

	err := store.Set(ctx, "session-detached", &Info{
		ID:            "session-detached",
		CharacterID:   ulid.Make(),
		CharacterName: "Zeratul",
		Status:        StatusDetached,
	})
	require.NoError(t, err)

	got, err := store.FindByCharacterName(ctx, "Zeratul")
	require.Error(t, err)
	assert.Nil(t, got)
}

func TestMemStore_UpdateLastPaged(t *testing.T) {
	ctx := context.Background()
	store := NewMemStore()

	err := store.Set(ctx, "session-1", &Info{
		ID:     "session-1",
		Status: StatusActive,
	})
	require.NoError(t, err)

	require.NoError(t, store.UpdateLastPaged(ctx, "session-1", "Zeratul"))

	got, err := store.Get(ctx, "session-1")
	require.NoError(t, err)
	assert.Equal(t, "Zeratul", got.LastPaged)
}

func TestMemStore_UpdateLastWhispered(t *testing.T) {
	ctx := context.Background()
	store := NewMemStore()

	err := store.Set(ctx, "session-1", &Info{
		ID:     "session-1",
		Status: StatusActive,
	})
	require.NoError(t, err)

	require.NoError(t, store.UpdateLastWhispered(ctx, "session-1", "Artanis"))

	got, err := store.Get(ctx, "session-1")
	require.NoError(t, err)
	assert.Equal(t, "Artanis", got.LastWhispered)
}

func TestFocusMembershipTypesExist(t *testing.T) {
	// Verify the type system is wired correctly. This test serves as
	// a compile-time canary — if the types are removed or renamed,
	// this fails to compile.
	key := FocusKey{
		Kind:     FocusKindScene,
		TargetID: ulid.Make(),
	}
	mem := FocusMembership{
		Kind:     key.Kind,
		TargetID: key.TargetID,
		JoinedAt: time.Now(),
	}
	assert.Equal(t, FocusKindScene, mem.Kind)
	assert.Equal(t, key.TargetID, mem.TargetID)
	assert.NotZero(t, mem.JoinedAt)

	// Verify FocusKey equality semantics.
	key2 := FocusKey{Kind: FocusKindScene, TargetID: key.TargetID}
	assert.Equal(t, key, key2)

	key3 := FocusKey{Kind: FocusKindScene, TargetID: ulid.Make()}
	assert.NotEqual(t, key, key3)
}

func TestFocusMutatorHasMutateField(t *testing.T) {
	// FocusMutator is a struct with a Mutate callback. We cannot construct
	// it from outside grpc/focus (the sentinel field is unexported), but we
	// can verify the type exists and document the Mutate signature via
	// reflection. This test exists purely as a compile-time canary.
	var m FocusMutator
	assert.Nil(t, m.Mutate, "zero-value FocusMutator should have nil Mutate")
}

func TestMemStore_FocusMembershipsRoundTrip(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()
	targetID := ulid.Make()
	now := time.Now().Truncate(time.Millisecond)

	presenting := &FocusKey{Kind: FocusKindScene, TargetID: targetID}
	info := &Info{
		ID:            "session-focus-rt",
		CharacterID:   ulid.Make(),
		CharacterName: "FocusChar",
		Status:        StatusActive,

		FocusMemberships: []FocusMembership{
			{Kind: FocusKindScene, TargetID: targetID, JoinedAt: now},
		},
		PresentingFocus: presenting,
	}

	require.NoError(t, store.Set(ctx, info.ID, info))

	got, err := store.Get(ctx, info.ID)
	require.NoError(t, err)
	require.Len(t, got.FocusMemberships, 1)
	assert.Equal(t, FocusKindScene, got.FocusMemberships[0].Kind)
	assert.Equal(t, targetID, got.FocusMemberships[0].TargetID)
	assert.Equal(t, now, got.FocusMemberships[0].JoinedAt)
	require.NotNil(t, got.PresentingFocus)
	assert.Equal(t, *presenting, *got.PresentingFocus)

	// Verify defensive copy — mutating the returned value must not affect the store.
	got.FocusMemberships = nil
	got.PresentingFocus = nil
	got2, err := store.Get(ctx, info.ID)
	require.NoError(t, err)
	require.Len(t, got2.FocusMemberships, 1, "defensive copy must protect store state")
	require.NotNil(t, got2.PresentingFocus, "defensive copy must protect store state")
}

func TestMemStore_UpdateFocusMemberships_AddsAndPresents(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()
	targetID := ulid.Make()

	info := &Info{
		ID:          "session-ufm-add",
		CharacterID: ulid.Make(),
		Status:      StatusActive,
	}
	require.NoError(t, store.Set(ctx, info.ID, info))

	mutator := NewFocusMutator(func(
		current []FocusMembership,
		presenting *FocusKey,
	) ([]FocusMembership, *FocusKey, error) {
		require.Empty(t, current)
		require.Nil(t, presenting)
		newMem := FocusMembership{
			Kind:     FocusKindScene,
			TargetID: targetID,
			JoinedAt: time.Now(),
		}
		newKey := &FocusKey{Kind: FocusKindScene, TargetID: targetID}
		return []FocusMembership{newMem}, newKey, nil
	})

	require.NoError(t, store.UpdateFocusMemberships(ctx, info.ID, mutator))

	got, err := store.Get(ctx, info.ID)
	require.NoError(t, err)
	require.Len(t, got.FocusMemberships, 1)
	assert.Equal(t, FocusKindScene, got.FocusMemberships[0].Kind)
	assert.Equal(t, targetID, got.FocusMemberships[0].TargetID)
	require.NotNil(t, got.PresentingFocus)
	assert.Equal(t, targetID, got.PresentingFocus.TargetID)
}

func TestMemStore_UpdateFocusMemberships_NotFound(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	mutator := NewFocusMutator(func(
		current []FocusMembership,
		presenting *FocusKey,
	) ([]FocusMembership, *FocusKey, error) {
		return current, presenting, nil
	})

	err := store.UpdateFocusMemberships(ctx, "nonexistent", mutator)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session not found")
}

func TestMemStore_UpdateFocusMemberships_RejectsExpiredSession(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	info := &Info{
		ID:          "session-ufm-expired",
		CharacterID: ulid.Make(),
		Status:      StatusExpired,
	}
	require.NoError(t, store.Set(ctx, info.ID, info))

	mutator := NewFocusMutator(func(
		current []FocusMembership,
		presenting *FocusKey,
	) ([]FocusMembership, *FocusKey, error) {
		return current, presenting, nil
	})

	err := store.UpdateFocusMemberships(ctx, info.ID, mutator)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

func TestMemStore_UpdateFocusMemberships_MutatorErrorRollsBack(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	info := &Info{
		ID:          "session-ufm-err",
		CharacterID: ulid.Make(),
		Status:      StatusActive,
	}
	require.NoError(t, store.Set(ctx, info.ID, info))

	mutator := NewFocusMutator(func(
		_ []FocusMembership,
		_ *FocusKey,
	) ([]FocusMembership, *FocusKey, error) {
		return nil, nil, fmt.Errorf("intentional mutator error")
	})

	err := store.UpdateFocusMemberships(ctx, info.ID, mutator)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "intentional mutator error")

	// State unchanged after error.
	got, err := store.Get(ctx, info.ID)
	require.NoError(t, err)
	assert.Empty(t, got.FocusMemberships)
	assert.Nil(t, got.PresentingFocus)
}

func TestMemStoreListByFocusReturnsNonExpiredSessionsWithMatchingMembership(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()
	sceneID := ulid.Make()
	otherSceneID := ulid.Make()
	target := FocusKey{Kind: FocusKindScene, TargetID: sceneID}

	match1 := &Info{
		ID:          "sess-match-1",
		CharacterID: ulid.Make(),
		Status:      StatusActive,
		FocusMemberships: []FocusMembership{
			{Kind: FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
		},
	}
	match2Detached := &Info{
		ID:          "sess-match-2",
		CharacterID: ulid.Make(),
		Status:      StatusDetached,
		FocusMemberships: []FocusMembership{
			{Kind: FocusKindScene, TargetID: otherSceneID, JoinedAt: time.Now()},
			{Kind: FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
		},
	}
	nonMatch := &Info{
		ID:          "sess-nomatch",
		CharacterID: ulid.Make(),
		Status:      StatusActive,
		FocusMemberships: []FocusMembership{
			{Kind: FocusKindScene, TargetID: otherSceneID, JoinedAt: time.Now()},
		},
	}
	expiredMatch := &Info{
		ID:          "sess-expired",
		CharacterID: ulid.Make(),
		Status:      StatusExpired,
		FocusMemberships: []FocusMembership{
			{Kind: FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
		},
	}
	noMemberships := &Info{
		ID:          "sess-empty",
		CharacterID: ulid.Make(),
		Status:      StatusActive,
	}
	require.NoError(t, store.Set(ctx, match1.ID, match1))
	require.NoError(t, store.Set(ctx, match2Detached.ID, match2Detached))
	require.NoError(t, store.Set(ctx, nonMatch.ID, nonMatch))
	require.NoError(t, store.Set(ctx, expiredMatch.ID, expiredMatch))
	require.NoError(t, store.Set(ctx, noMemberships.ID, noMemberships))

	results, err := store.ListByFocus(ctx, target)
	require.NoError(t, err)

	ids := make([]string, 0, len(results))
	for _, r := range results {
		ids = append(ids, r.ID)
	}
	assert.ElementsMatch(t, []string{"sess-match-1", "sess-match-2"}, ids)
}

func TestMemStoreListByFocusReturnsEmptySliceWhenNoMatches(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()

	info := &Info{
		ID:          "sess-alone",
		CharacterID: ulid.Make(),
		Status:      StatusActive,
		FocusMemberships: []FocusMembership{
			{Kind: FocusKindScene, TargetID: ulid.Make(), JoinedAt: time.Now()},
		},
	}
	require.NoError(t, store.Set(ctx, info.ID, info))

	results, err := store.ListByFocus(ctx, FocusKey{Kind: FocusKindScene, TargetID: ulid.Make()})
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestMemStoreListByPlayerSessionReturnsOnlyMatchingSessions(t *testing.T) {
	ctx := context.Background()
	store := NewMemStore()

	ps1 := ulid.Make()
	ps2 := ulid.Make()
	ps3 := ulid.Make()

	require.NoError(t, store.Set(ctx, "s1", &Info{
		ID: "s1", CharacterID: ulid.Make(), PlayerSessionID: ps1, Status: StatusActive,
	}))
	require.NoError(t, store.Set(ctx, "s2", &Info{
		ID: "s2", CharacterID: ulid.Make(), PlayerSessionID: ps2, Status: StatusActive,
	}))
	require.NoError(t, store.Set(ctx, "s3", &Info{
		ID: "s3", CharacterID: ulid.Make(), PlayerSessionID: ps1, Status: StatusActive,
	}))
	require.NoError(t, store.Set(ctx, "s4", &Info{
		ID: "s4", CharacterID: ulid.Make(), PlayerSessionID: ps3, Status: StatusActive,
	}))

	got, err := store.ListByPlayerSession(ctx, []ulid.ULID{ps1, ps2})
	require.NoError(t, err)

	gotIDs := make(map[string]bool)
	for _, info := range got {
		gotIDs[info.ID] = true
	}
	assert.True(t, gotIDs["s1"])
	assert.True(t, gotIDs["s2"])
	assert.True(t, gotIDs["s3"])
	assert.False(t, gotIDs["s4"])
	assert.Len(t, got, 3)
}

func TestMemStoreListByPlayerSessionReturnsEmptyForNoMatches(t *testing.T) {
	ctx := context.Background()
	store := NewMemStore()
	got, err := store.ListByPlayerSession(ctx, []ulid.ULID{ulid.Make()})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestMemStoreListByPlayerSessionReturnsEmptyForEmptyInput(t *testing.T) {
	ctx := context.Background()
	store := NewMemStore()
	// Seed a session so it would match if input weren't empty
	require.NoError(t, store.Set(ctx, "s1", &Info{
		ID: "s1", CharacterID: ulid.Make(), PlayerSessionID: ulid.Make(), Status: StatusActive,
	}))
	got, err := store.ListByPlayerSession(ctx, []ulid.ULID{})
	require.NoError(t, err)
	assert.Empty(t, got, "empty input must return empty result, not scan all sessions")
}

// TestMemStoreListByPlayerSessionSkipsExpiredSessions verifies that sessions
// with StatusExpired are excluded from ListByPlayerSession results even when
// their PlayerSessionID matches the query.
func TestMemStoreListByPlayerSessionSkipsExpiredSessions(t *testing.T) {
	ctx := context.Background()
	store := NewMemStore()

	psID := ulid.Make()

	// Active session — should appear in results.
	require.NoError(t, store.Set(ctx, "active", &Info{
		ID: "active", CharacterID: ulid.Make(), PlayerSessionID: psID, Status: StatusActive,
	}))
	// Expired session — must be excluded.
	require.NoError(t, store.Set(ctx, "expired", &Info{
		ID: "expired", CharacterID: ulid.Make(), PlayerSessionID: psID, Status: StatusExpired,
	}))

	got, err := store.ListByPlayerSession(ctx, []ulid.ULID{psID})
	require.NoError(t, err)
	require.Len(t, got, 1, "only the active session must be returned")
	assert.Equal(t, "active", got[0].ID)
}

func TestMemStoreUpdateLocationOnMove(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()
	charID := ulid.Make()
	origLoc := ulid.Make()
	newLoc := ulid.Make()
	origArrival := time.Now().UTC().Add(-time.Hour)
	newArrival := time.Now().UTC()

	info := &Info{
		ID: ulid.Make().String(), CharacterID: charID,
		LocationID: origLoc, LocationArrivedAt: origArrival,
		Status: StatusActive, CreatedAt: origArrival, UpdatedAt: origArrival,
	}
	require.NoError(t, store.Set(ctx, info.ID, info))

	require.NoError(t, store.UpdateLocationOnMove(ctx, charID, newLoc, newArrival))

	got, err := store.Get(ctx, info.ID)
	require.NoError(t, err)
	assert.Equal(t, newLoc, got.LocationID)
	assert.True(t, got.LocationArrivedAt.Equal(newArrival))
}

func TestMemStoreUpdateLocationOnMove_SkipsDetached(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()
	charID := ulid.Make()
	origLoc := ulid.Make()
	newLoc := ulid.Make()
	arrival := time.Now().UTC()

	// Detached session — must NOT be updated.
	detached := &Info{
		ID: ulid.Make().String(), CharacterID: charID,
		LocationID: origLoc, LocationArrivedAt: arrival.Add(-time.Hour),
		Status: StatusDetached, CreatedAt: arrival, UpdatedAt: arrival,
	}
	require.NoError(t, store.Set(ctx, detached.ID, detached))

	require.NoError(t, store.UpdateLocationOnMove(ctx, charID, newLoc, arrival))

	got, err := store.Get(ctx, detached.ID)
	require.NoError(t, err)
	// Location must remain unchanged for detached sessions.
	assert.Equal(t, origLoc, got.LocationID)
}

func TestMemStoreBumpLocationArrivedAt(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()
	origArrival := time.Now().UTC().Add(-time.Hour)
	newArrival := time.Now().UTC()

	info := &Info{
		ID: ulid.Make().String(), CharacterID: ulid.Make(),
		LocationID: ulid.Make(), LocationArrivedAt: origArrival,
		Status: StatusDetached, CreatedAt: origArrival, UpdatedAt: origArrival,
	}
	require.NoError(t, store.Set(ctx, info.ID, info))

	require.NoError(t, store.BumpLocationArrivedAt(ctx, info.ID, newArrival))

	got, err := store.Get(ctx, info.ID)
	require.NoError(t, err)
	assert.True(t, got.LocationArrivedAt.Equal(newArrival), "LocationArrivedAt must be bumped to newArrival")
	assert.True(t, got.UpdatedAt.Equal(newArrival), "UpdatedAt must also be bumped")
	// Status-agnostic: detached session is also updated.
	assert.Equal(t, StatusDetached, got.Status)
}

func TestMemStoreBumpLocationArrivedAt_NotFound(t *testing.T) {
	store := NewMemStore()
	ctx := context.Background()
	err := store.BumpLocationArrivedAt(ctx, "no-such-session", time.Now().UTC())
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SESSION_NOT_FOUND", oopsErr.Code())
}

// Verifies: I-PRES-1
// MemStore S-1: ListActiveByLocation returns only Status=Active sessions;
// Detached and Expired are excluded by the Status filter.
func TestMemStoreListActiveByLocationReturnsActiveSessionsOnly(t *testing.T) {
	m := NewMemStore()
	ctx := context.Background()
	loc := idgen.New()
	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)

	activeA := &Info{ID: "a", Status: StatusActive, LocationID: loc, ExpiresAt: &future, CharacterID: idgen.New()}
	activeB := &Info{ID: "b", Status: StatusActive, LocationID: loc, ExpiresAt: &future, CharacterID: idgen.New()}
	detached := &Info{ID: "c", Status: StatusDetached, LocationID: loc, ExpiresAt: &future, CharacterID: idgen.New()}
	// Expired status: in production the reaper transitions expired sessions
	// to StatusExpired. ListActiveByLocation filters on Status only (not
	// ExpiresAt) — expiry-at-handler is enforced separately by info.IsExpired().
	expiredStatus := &Info{ID: "d", Status: StatusExpired, LocationID: loc, ExpiresAt: &past, CharacterID: idgen.New()}

	for _, s := range []*Info{activeA, activeB, detached, expiredStatus} {
		require.NoError(t, m.Set(ctx, s.ID, s))
	}

	got, err := m.ListActiveByLocation(ctx, loc)
	require.NoError(t, err)
	ids := make([]string, 0, len(got))
	for _, s := range got {
		ids = append(ids, s.ID)
	}
	assert.ElementsMatch(t, []string{"a", "b"}, ids, "Detached and Expired must be excluded by Status filter")
}

// Verifies: I-PRES-1
// ListActiveByLocation returns no matching sessions for a location with no
// active sessions, and reports no error. The Go-level return value MAY be
// nil OR an empty slice — both are zero-length and acceptable; the wire-
// level entries=[] contract (handler-side U-2) is locked separately by
// internal/grpc/list_focus_presence_test.go via the proto response shape.
func TestMemStoreListActiveByLocationReturnsNoSessionsForEmptyLocation(t *testing.T) {
	m := NewMemStore()
	got, err := m.ListActiveByLocation(context.Background(), idgen.New())
	require.NoError(t, err)
	assert.Empty(t, got)
}

// Verifies: I-PRES-1
// MemStore S-3: ListActiveByLocation filters by LocationID — sessions at
// other locations MUST be excluded (cross-location isolation).
func TestMemStoreListActiveByLocationFiltersByLocation(t *testing.T) {
	m := NewMemStore()
	ctx := context.Background()
	loc1 := idgen.New()
	loc2 := idgen.New()
	future := time.Now().Add(time.Hour)
	s1 := &Info{ID: "1", Status: StatusActive, LocationID: loc1, ExpiresAt: &future, CharacterID: idgen.New()}
	s2 := &Info{ID: "2", Status: StatusActive, LocationID: loc2, ExpiresAt: &future, CharacterID: idgen.New()}
	require.NoError(t, m.Set(ctx, s1.ID, s1))
	require.NoError(t, m.Set(ctx, s2.ID, s2))

	got, err := m.ListActiveByLocation(ctx, loc1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "1", got[0].ID)
}

func TestListConnectionsBySession_Empty(t *testing.T) {
	t.Parallel()
	s := NewMemStore()
	ctx := context.Background()
	require.NoError(t, s.Set(ctx, "sess-list-empty", &Info{ID: "sess-list-empty", Status: StatusActive}))

	conns, err := s.ListConnectionsBySession(ctx, "sess-list-empty")
	require.NoError(t, err)
	assert.Empty(t, conns)
}

func TestListConnectionsBySession_Multi(t *testing.T) {
	t.Parallel()
	s := NewMemStore()
	ctx := context.Background()
	sessionID := "sess-list-multi"
	require.NoError(t, s.Set(ctx, sessionID, &Info{ID: sessionID, Status: StatusActive}))

	for _, ct := range []string{"terminal", "telnet", "comms_hub"} {
		require.NoError(t, s.AddConnection(ctx, &Connection{ID: ulid.Make(), SessionID: sessionID, ClientType: ct}))
	}

	conns, err := s.ListConnectionsBySession(ctx, sessionID)
	require.NoError(t, err)
	assert.Len(t, conns, 3)

	seen := map[string]bool{}
	for _, c := range conns {
		seen[c.ClientType] = true
	}
	assert.True(t, seen["terminal"])
	assert.True(t, seen["telnet"])
	assert.True(t, seen["comms_hub"])
}

func TestListConnectionsBySession_SessionNotFound(t *testing.T) {
	t.Parallel()
	s := NewMemStore()
	_, err := s.ListConnectionsBySession(context.Background(), "nope")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_NOT_FOUND")
}

func TestUpdateSessionConnection_HappyPath(t *testing.T) {
	t.Parallel()
	s := NewMemStore()
	ctx := context.Background()

	sessionID := "sess-uc-happy"
	require.NoError(t, s.Set(ctx, sessionID, &Info{ID: sessionID, Status: StatusActive}))

	connID := ulid.Make()
	require.NoError(t, s.AddConnection(ctx, &Connection{
		ID: connID, SessionID: sessionID, ClientType: "terminal",
	}))

	sceneID := ulid.Make()
	target := &FocusKey{Kind: FocusKindScene, TargetID: sceneID}

	m := NewSessionConnectionMutator(func(info Info, conn Connection) (Info, Connection, error) {
		conn.FocusKey = target
		info.PresentingFocus = target
		return info, conn, nil
	})

	require.NoError(t, s.UpdateSessionConnection(ctx, sessionID, connID, m))

	// Verify Connection.FocusKey written.
	conn, err := s.GetConnection(ctx, connID)
	require.NoError(t, err)
	require.NotNil(t, conn.FocusKey)
	assert.Equal(t, target.TargetID, conn.FocusKey.TargetID)

	// Verify Info.PresentingFocus written.
	info, err := s.Get(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, info.PresentingFocus)
	assert.Equal(t, target.TargetID, info.PresentingFocus.TargetID)
}

func TestUpdateSessionConnection_ConnectionNotFound(t *testing.T) {
	t.Parallel()
	s := NewMemStore()
	ctx := context.Background()

	require.NoError(t, s.Set(ctx, "sess-uc-404", &Info{ID: "sess-uc-404", Status: StatusActive}))

	m := NewSessionConnectionMutator(func(info Info, conn Connection) (Info, Connection, error) {
		return info, conn, nil
	})
	err := s.UpdateSessionConnection(ctx, "sess-uc-404", ulid.Make(), m)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CONNECTION_NOT_FOUND")
}

func TestUpdateSessionConnection_MutatorErrorPropagates(t *testing.T) {
	t.Parallel()
	s := NewMemStore()
	ctx := context.Background()

	sessionID := "sess-uc-err"
	require.NoError(t, s.Set(ctx, sessionID, &Info{ID: sessionID, Status: StatusActive}))
	connID := ulid.Make()
	require.NoError(t, s.AddConnection(ctx, &Connection{ID: connID, SessionID: sessionID, ClientType: "telnet"}))

	sentinel := oops.Code("FOCUS_WITHOUT_MEMBERSHIP").Errorf("test")
	m := NewSessionConnectionMutator(func(info Info, conn Connection) (Info, Connection, error) {
		return info, conn, sentinel
	})
	err := s.UpdateSessionConnection(context.Background(), sessionID, connID, m)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "FOCUS_WITHOUT_MEMBERSHIP")

	// Verify NO write happened despite mutator returning (info, conn, err).
	info, err2 := s.Get(ctx, sessionID)
	require.NoError(t, err2)
	assert.Nil(t, info.PresentingFocus, "mutator error MUST abort the write")
}

func TestUpdateSessionConnection_AtomicCommit(t *testing.T) {
	t.Parallel()
	// INV-P5-7: an external observer between the mutator call and
	// its commit cannot see one field updated while the other lags.
	// MemStore implements this by holding m.mu.Lock() for the whole
	// mutator callback, so any concurrent Get() blocks until commit.
	s := NewMemStore()
	ctx := context.Background()

	sessionID := "sess-uc-atomic"
	require.NoError(t, s.Set(ctx, sessionID, &Info{ID: sessionID, Status: StatusActive}))
	connID := ulid.Make()
	require.NoError(t, s.AddConnection(ctx, &Connection{ID: connID, SessionID: sessionID, ClientType: "terminal"}))

	target := &FocusKey{Kind: FocusKindScene, TargetID: ulid.Make()}

	// Launch the mutation, blocking the mutator mid-flight via a channel.
	blockCh := make(chan struct{})
	enteredCh := make(chan struct{}) // signal the mutator has acquired the lock
	doneCh := make(chan error)
	go func() {
		m := NewSessionConnectionMutator(func(info Info, conn Connection) (Info, Connection, error) {
			close(enteredCh) // lock acquired; reader is now guaranteed to block
			<-blockCh        // hold the lock until released
			conn.FocusKey = target
			info.PresentingFocus = target
			return info, conn, nil
		})
		doneCh <- s.UpdateSessionConnection(ctx, sessionID, connID, m)
	}()

	// Wait deterministically for the mutator to be holding the lock,
	// instead of a sleep-based timing race.
	<-enteredCh

	// External read attempted in a goroutine; should observe POST-commit
	// state once we unblock. require.NoError is unsafe from a goroutine
	// (it calls t.FailNow which is only safe in the test goroutine), so
	// errors get bubbled back via readErrCh for the main goroutine to
	// assert. readStarted gates the "reader is blocked" timeout check —
	// without it, the 20ms could false-pass if the reader goroutine
	// hadn't reached s.Get yet (scheduler lag, not blocking-behavior).
	// (CodeRabbit PR #4191 — round 5)
	readDone := make(chan struct{})
	readStarted := make(chan struct{})
	readErrCh := make(chan error, 2)
	var info *Info
	var conn *Connection
	go func() {
		defer close(readDone)
		close(readStarted) // I'm about to call s.Get; the next line will block on m.mu.
		var err error
		info, err = s.Get(ctx, sessionID)
		readErrCh <- err
		conn, err = s.GetConnection(ctx, connID)
		readErrCh <- err
	}()
	<-readStarted // confirm reader reached the call boundary before timing.

	// Verify the reader is blocked while mutator holds the lock.
	select {
	case <-readDone:
		t.Fatal("INV-P5-7 violated: external read returned before mutator committed (torn state observable)")
	case <-time.After(20 * time.Millisecond):
		// good — reader is blocked
	}

	// Release the mutator; commit happens; reader unblocks.
	close(blockCh)
	require.NoError(t, <-doneCh)
	<-readDone
	// Now safe to assert any goroutine-side errors from the test goroutine.
	require.NoError(t, <-readErrCh, "s.Get from reader goroutine")
	require.NoError(t, <-readErrCh, "s.GetConnection from reader goroutine")

	// Both fields MUST be observed post-commit (no torn state).
	require.NotNil(t, info.PresentingFocus, "PresentingFocus visible post-commit")
	require.NotNil(t, conn.FocusKey, "FocusKey visible post-commit")
	assert.Equal(t, target.TargetID, info.PresentingFocus.TargetID)
	assert.Equal(t, target.TargetID, conn.FocusKey.TargetID)
}
