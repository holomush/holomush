// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/core/coretest"
	"github.com/holomush/holomush/internal/eventvocab"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestEndSessionEmitsCorrectEventShapeOnCharacterStream(t *testing.T) {
	ctx := context.Background()
	store := coretest.NewMemoryEventStore()
	engine := core.NewEngine(store)

	charID := core.NewULID()
	sessionID := core.NewULID().String()
	char := core.CharacterRef{ID: charID, Name: "Testy", LocationID: core.NewULID()}

	err := engine.EndSession(ctx, char, sessionID, core.SessionEndedCauseQuit, "Goodbye!")
	require.NoError(t, err)

	stream := "character." + charID.String()
	events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)

	ev := events[0]
	assert.Equal(t, stream, ev.Stream, "stream must be character:{ID}")
	assert.Equal(t, eventvocab.EventTypeSessionEnded, ev.Type)
	assert.Equal(t, core.ActorCharacter, ev.Actor.Kind, "cause=quit uses ActorCharacter")
	assert.Equal(t, charID.String(), ev.Actor.ID)
	assert.NotZero(t, ev.ID, "event MUST have a ULID (monotonic per I-16)")

	var payload core.SessionEndedPayload
	require.NoError(t, json.Unmarshal(ev.Payload, &payload))
	assert.Equal(t, sessionID, payload.SessionID)
	assert.Equal(t, charID.String(), payload.CharacterID)
	assert.Equal(t, core.SessionEndedCauseQuit, payload.Cause)
	assert.Equal(t, "Goodbye!", payload.Reason)
}

func TestEndSessionUsesActorSystemForNonQuitCauses(t *testing.T) {
	ctx := context.Background()

	charID := core.NewULID()
	char := core.CharacterRef{ID: charID, Name: "Testy", LocationID: core.NewULID()}

	cases := []string{
		core.SessionEndedCauseLogout,
		core.SessionEndedCauseGuestEnd,
		core.SessionEndedCauseKicked,
		core.SessionEndedCauseReaped,
		core.SessionEndedCauseEvicted,
	}

	for _, cause := range cases {
		t.Run("uses ActorSystem actor when cause is "+cause, func(t *testing.T) {
			store := coretest.NewMemoryEventStore()
			engine := core.NewEngine(store)

			err := engine.EndSession(ctx, char, core.NewULID().String(), cause, "reason")
			require.NoError(t, err)

			stream := "character." + charID.String()
			events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
			require.NoError(t, err)
			require.Len(t, events, 1)
			assert.Equal(t, core.ActorSystem, events[0].Actor.Kind)
			assert.Equal(t, core.ActorSystemID, events[0].Actor.ID)
		})
	}
}

// appendFailStore is a minimal EventAppender that always fails on Append.
type appendFailStore struct {
	err error
}

func (s *appendFailStore) Append(_ context.Context, _ core.Event) error { return s.err }

var _ core.EventAppender = (*appendFailStore)(nil)

func TestEndSessionReturnsErrorWhenStoreFails(t *testing.T) {
	store := &appendFailStore{err: errors.New("disk full")}
	engine := core.NewEngine(store)

	char := core.CharacterRef{ID: core.NewULID(), Name: "Testy", LocationID: core.NewULID()}
	err := engine.EndSession(context.Background(), char, core.NewULID().String(), core.SessionEndedCauseQuit, "bye")

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_ENDED_APPEND_FAILED")
}
