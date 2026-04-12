// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core_test

import (
	"testing"
	"time"

	"github.com/holomush/holomush/internal/core"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEventAssignsMonotonicID(t *testing.T) {
	before := time.Now()
	ev := core.NewEvent("location:test", core.EventTypeSay, core.Actor{
		Kind: core.ActorCharacter,
		ID:   "char-1",
	}, []byte(`{"message":"hello"}`))
	after := time.Now()

	require.NotEqual(t, ulid.ULID{}, ev.ID, "ID must be non-zero")
	assert.Equal(t, "location:test", ev.Stream)
	assert.Equal(t, core.EventTypeSay, ev.Type)
	assert.Equal(t, core.ActorCharacter, ev.Actor.Kind)
	assert.Equal(t, "char-1", ev.Actor.ID)
	assert.Equal(t, []byte(`{"message":"hello"}`), ev.Payload)
	assert.False(t, ev.Timestamp.Before(before), "Timestamp must be >= before")
	assert.False(t, ev.Timestamp.After(after), "Timestamp must be <= after")
}

func TestNewEventProducesUniqueIDs(t *testing.T) {
	seen := make(map[ulid.ULID]struct{}, 100)
	for range 100 {
		ev := core.NewEvent("location:test", core.EventTypeSay, core.Actor{
			Kind: core.ActorSystem,
			ID:   "system",
		}, []byte(`{}`))
		_, dup := seen[ev.ID]
		require.False(t, dup, "NewEvent must produce unique IDs")
		seen[ev.ID] = struct{}{}
	}
}

func TestNewEventIDsAreMonotonicWithinGoroutine(t *testing.T) {
	var prev ulid.ULID
	for range 1000 {
		ev := core.NewEvent("location:test", core.EventTypeSay, core.Actor{
			Kind: core.ActorSystem,
			ID:   "system",
		}, []byte(`{}`))
		if prev != (ulid.ULID{}) {
			assert.True(t, ev.ID.Compare(prev) > 0,
				"IDs must be strictly monotonic: prev=%s cur=%s", prev, ev.ID)
		}
		prev = ev.ID
	}
}
