// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// Compile-time check that *world.Service satisfies WorldQuerier.
var _ WorldQuerier = (*world.Service)(nil)

// mockWorldQuerier implements WorldQuerier for tests.
type mockWorldQuerier struct {
	location   *world.Location
	locErr     error
	exits      []*world.Exit
	exitsErr   error
	characters []*world.Character
	charsErr   error
}

func (m *mockWorldQuerier) GetLocation(_ context.Context, _ string, _ ulid.ULID) (*world.Location, error) {
	return m.location, m.locErr
}

func (m *mockWorldQuerier) GetExitsByLocation(_ context.Context, _ string, _ ulid.ULID) ([]*world.Exit, error) {
	return m.exits, m.exitsErr
}

func (m *mockWorldQuerier) GetCharactersByLocation(_ context.Context, _ string, _ ulid.ULID, _ world.ListOptions) ([]*world.Character, error) {
	return m.characters, m.charsErr
}

// capturingStream captures sent events for assertion.
type capturingStream struct {
	grpc.ServerStream
	sent []*corev1.Event
	ctx  context.Context
}

func (s *capturingStream) Send(ev *corev1.Event) error {
	s.sent = append(s.sent, ev)
	return nil
}

func (s *capturingStream) Context() context.Context {
	return s.ctx
}

func (s *capturingStream) SetHeader(_ metadata.MD) error  { return nil }
func (s *capturingStream) SendHeader(_ metadata.MD) error { return nil }
func (s *capturingStream) SetTrailer(_ metadata.MD)       {}
func (s *capturingStream) SendMsg(_ interface{}) error     { return nil }
func (s *capturingStream) RecvMsg(_ interface{}) error     { return nil }

func TestLocationFollower_HandleEvent_DetectsCharacterMove(t *testing.T) {
	charID := ulid.Make()
	oldLocID := ulid.Make()
	newLocID := ulid.Make()

	wq := &mockWorldQuerier{
		location: &world.Location{
			ID:          newLocID,
			Name:        "New Location",
			Description: "A shiny new location.",
		},
		exits:      []*world.Exit{},
		characters: []*world.Character{},
	}

	lf := &locationFollower{
		characterID:  charID,
		currentLocID: oldLocID,
		worldQuerier: wq,
	}

	movePayload, err := json.Marshal(world.MovePayload{
		EntityType: world.EntityTypeCharacter,
		EntityID:   charID,
		FromType:   world.ContainmentTypeLocation,
		FromID:     &oldLocID,
		ToType:     world.ContainmentTypeLocation,
		ToID:       newLocID,
	})
	require.NoError(t, err)

	event := core.Event{
		ID:      ulid.Make(),
		Stream:  world.CharacterStream(charID),
		Type:    core.EventTypeMove,
		Payload: movePayload,
	}

	stream := &capturingStream{ctx: context.Background()}
	handled := lf.handleEvent(context.Background(), event, stream)

	assert.True(t, handled)
	assert.Equal(t, newLocID, lf.currentLocID)
	require.Len(t, stream.sent, 1)
	assert.Equal(t, string(core.EventTypeLocationState), stream.sent[0].GetType())

	// Verify location_state payload
	var locState core.LocationStatePayload
	require.NoError(t, json.Unmarshal(stream.sent[0].GetPayload(), &locState))
	assert.Equal(t, "New Location", locState.Location.Name)
}

func TestLocationFollower_HandleEvent_IgnoresNonMoveEvents(t *testing.T) {
	lf := &locationFollower{
		characterID:  ulid.Make(),
		currentLocID: ulid.Make(),
		worldQuerier: &mockWorldQuerier{},
	}

	event := core.Event{
		ID:   ulid.Make(),
		Type: core.EventTypeSay,
	}

	stream := &capturingStream{ctx: context.Background()}
	handled := lf.handleEvent(context.Background(), event, stream)

	assert.False(t, handled)
	assert.Empty(t, stream.sent)
}

func TestLocationFollower_HandleEvent_IgnoresOtherCharacterMoves(t *testing.T) {
	charID := ulid.Make()
	otherCharID := ulid.Make()
	locID := ulid.Make()
	newLocID := ulid.Make()

	lf := &locationFollower{
		characterID:  charID,
		currentLocID: locID,
		worldQuerier: &mockWorldQuerier{},
	}

	movePayload, err := json.Marshal(world.MovePayload{
		EntityType: world.EntityTypeCharacter,
		EntityID:   otherCharID,
		FromType:   world.ContainmentTypeLocation,
		FromID:     &locID,
		ToType:     world.ContainmentTypeLocation,
		ToID:       newLocID,
	})
	require.NoError(t, err)

	event := core.Event{
		ID:      ulid.Make(),
		Type:    core.EventTypeMove,
		Payload: movePayload,
	}

	stream := &capturingStream{ctx: context.Background()}
	handled := lf.handleEvent(context.Background(), event, stream)

	assert.False(t, handled)
	assert.Empty(t, stream.sent)
	assert.Equal(t, locID, lf.currentLocID, "location should not change")
}

func TestLocationFollower_HandleEvent_IgnoresObjectMoves(t *testing.T) {
	charID := ulid.Make()
	locID := ulid.Make()
	newLocID := ulid.Make()
	objID := ulid.Make()

	lf := &locationFollower{
		characterID:  charID,
		currentLocID: locID,
		worldQuerier: &mockWorldQuerier{},
	}

	movePayload, err := json.Marshal(world.MovePayload{
		EntityType: world.EntityTypeObject,
		EntityID:   objID,
		FromType:   world.ContainmentTypeLocation,
		FromID:     &locID,
		ToType:     world.ContainmentTypeLocation,
		ToID:       newLocID,
	})
	require.NoError(t, err)

	event := core.Event{
		ID:      ulid.Make(),
		Type:    core.EventTypeMove,
		Payload: movePayload,
	}

	stream := &capturingStream{ctx: context.Background()}
	handled := lf.handleEvent(context.Background(), event, stream)

	assert.False(t, handled)
	assert.Empty(t, stream.sent)
}

func TestLocationFollower_HandleEvent_NilWorldQuerier(t *testing.T) {
	lf := &locationFollower{
		characterID:  ulid.Make(),
		currentLocID: ulid.Make(),
		worldQuerier: nil,
	}

	event := core.Event{
		ID:   ulid.Make(),
		Type: core.EventTypeMove,
	}

	stream := &capturingStream{ctx: context.Background()}
	handled := lf.handleEvent(context.Background(), event, stream)

	assert.False(t, handled)
}

func TestLocationFollower_BuildLocationState(t *testing.T) {
	locID := ulid.Make()
	charID := ulid.Make()

	wq := &mockWorldQuerier{
		location: &world.Location{
			ID:          locID,
			Name:        "Hall",
			Description: "A grand hall.",
		},
		exits: []*world.Exit{
			{Name: "north", Locked: false},
		},
		characters: []*world.Character{
			{ID: charID, Name: "Alice"},
		},
	}

	lf := &locationFollower{worldQuerier: wq}
	ev, err := lf.buildLocationState(context.Background(), locID)
	require.NoError(t, err)
	require.NotNil(t, ev)

	assert.Equal(t, string(core.EventTypeLocationState), ev.GetType())
	assert.Equal(t, "system", ev.GetActorType())
	assert.Equal(t, world.LocationStream(locID), ev.GetStream())

	var payload core.LocationStatePayload
	require.NoError(t, json.Unmarshal(ev.GetPayload(), &payload))
	assert.Equal(t, "Hall", payload.Location.Name)
	assert.Len(t, payload.Exits, 1)
	assert.Len(t, payload.Present, 1)
}

func TestConvertExits_GRPCPackage(t *testing.T) {
	exits := []*world.Exit{
		{Name: "north", Locked: false},
		{Name: "south", Locked: true},
	}
	result := convertExits(exits)
	require.Len(t, result, 2)
	assert.Equal(t, "north", result[0].Direction)
	assert.False(t, result[0].Locked)
	assert.Equal(t, "south", result[1].Direction)
	assert.True(t, result[1].Locked)
}

func TestConvertCharacters_GRPCPackage(t *testing.T) {
	chars := []*world.Character{
		{Name: "Alice"},
		{Name: "Bob"},
	}
	result := convertCharacters(chars)
	require.Len(t, result, 2)
	assert.Equal(t, "Alice", result[0].Name)
	assert.Equal(t, "Bob", result[1].Name)
}
