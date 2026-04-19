// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

func TestEndSessionEmitsCorrectEventShapeOnCharacterStream(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEventStore()
	engine := NewEngine(store)

	charID := NewULID()
	sessionID := NewULID().String()
	char := CharacterRef{ID: charID, Name: "Testy", LocationID: NewULID()}

	err := engine.EndSession(ctx, char, sessionID, SessionEndedCauseQuit, "Goodbye!")
	require.NoError(t, err)

	stream := "character:" + charID.String()
	events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)

	ev := events[0]
	assert.Equal(t, stream, ev.Stream, "stream must be character:{ID}")
	assert.Equal(t, EventTypeSessionEnded, ev.Type)
	assert.Equal(t, ActorCharacter, ev.Actor.Kind, "cause=quit uses ActorCharacter")
	assert.Equal(t, charID.String(), ev.Actor.ID)
	assert.NotZero(t, ev.ID, "event MUST have a ULID (monotonic per I-16)")

	var payload SessionEndedPayload
	require.NoError(t, json.Unmarshal(ev.Payload, &payload))
	assert.Equal(t, sessionID, payload.SessionID)
	assert.Equal(t, charID.String(), payload.CharacterID)
	assert.Equal(t, SessionEndedCauseQuit, payload.Cause)
	assert.Equal(t, "Goodbye!", payload.Reason)
}

func TestEndSessionUsesActorSystemForNonQuitCauses(t *testing.T) {
	ctx := context.Background()

	charID := NewULID()
	char := CharacterRef{ID: charID, Name: "Testy", LocationID: NewULID()}

	cases := []string{
		SessionEndedCauseLogout,
		SessionEndedCauseGuestEnd,
		SessionEndedCauseKicked,
		SessionEndedCauseReaped,
		SessionEndedCauseEvicted,
	}

	for _, cause := range cases {
		t.Run("uses ActorSystem actor when cause is "+cause, func(t *testing.T) {
			store := NewMemoryEventStore()
			engine := NewEngine(store)

			err := engine.EndSession(ctx, char, NewULID().String(), cause, "reason")
			require.NoError(t, err)

			stream := "character:" + charID.String()
			events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
			require.NoError(t, err)
			require.Len(t, events, 1)
			assert.Equal(t, ActorSystem, events[0].Actor.Kind)
			assert.Equal(t, ActorSystemID, events[0].Actor.ID)
		})
	}
}

// appendFailStore is a minimal EventStore that always fails on Append.
type appendFailStore struct {
	err error
}

func (s *appendFailStore) Append(_ context.Context, _ Event) error { return s.err }
func (s *appendFailStore) Replay(_ context.Context, _ string, _ ulid.ULID, _ int) ([]Event, error) {
	return nil, nil
}
func (s *appendFailStore) LastEventID(_ context.Context, _ string) (ulid.ULID, error) {
	return ulid.ULID{}, nil
}
func (s *appendFailStore) ReplayTail(_ context.Context, _ string, _ int, _ time.Time, _ ulid.ULID) ([]Event, error) {
	return nil, nil
}
func (s *appendFailStore) SubscribeSession(_ context.Context) (Subscription, error) {
	return nil, nil
}

func TestEndSessionReturnsErrorWhenStoreFails(t *testing.T) {
	store := &appendFailStore{err: errors.New("disk full")}
	engine := NewEngine(store)

	char := CharacterRef{ID: NewULID(), Name: "Testy", LocationID: NewULID()}
	err := engine.EndSession(context.Background(), char, NewULID().String(), SessionEndedCauseQuit, "bye")

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_ENDED_APPEND_FAILED")
}
