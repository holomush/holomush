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

	"github.com/holomush/holomush/internal/session"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// Compile-time check that *world.Service satisfies WorldQuerier.
var _ WorldQuerier = (*world.Service)(nil)

// mockWorldQuerier implements WorldQuerier for tests.
type mockWorldQuerier struct {
	location *world.Location
	locErr   error
	exits    []*world.Exit
	exitsErr error
}

func (m *mockWorldQuerier) GetLocation(_ context.Context, _ string, _ ulid.ULID) (*world.Location, error) {
	return m.location, m.locErr
}

func (m *mockWorldQuerier) GetExitsByLocation(_ context.Context, _ string, _ ulid.ULID) ([]*world.Exit, error) {
	return m.exits, m.exitsErr
}

// capturingStream captures sent events for assertion.
type capturingStream struct {
	grpc.ServerStream
	sent []*corev1.SubscribeResponse
	ctx  context.Context
}

func (s *capturingStream) Send(ev *corev1.SubscribeResponse) error {
	s.sent = append(s.sent, ev)
	return nil
}

func (s *capturingStream) Context() context.Context {
	return s.ctx
}

func (s *capturingStream) SetHeader(_ metadata.MD) error  { return nil }
func (s *capturingStream) SendHeader(_ metadata.MD) error { return nil }
func (s *capturingStream) SetTrailer(_ metadata.MD)       {}
func (s *capturingStream) SendMsg(_ interface{}) error    { return nil }
func (s *capturingStream) RecvMsg(_ interface{}) error    { return nil }

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
		exits: []*world.Exit{},
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

	event := core.NewEvent(world.CharacterStream(charID), core.EventTypeMove, core.Actor{}, movePayload)

	stream := &capturingStream{ctx: context.Background()}
	handled := lf.handleEvent(context.Background(), event, stream)

	assert.True(t, handled)
	assert.Equal(t, newLocID, lf.currentLocID)
	require.Len(t, stream.sent, 1)
	assert.Equal(t, string(core.EventTypeLocationState), stream.sent[0].GetEvent().GetType())

	// Verify location_state payload
	var locState core.LocationStatePayload
	require.NoError(t, json.Unmarshal(stream.sent[0].GetEvent().GetPayload(), &locState))
	assert.Equal(t, "New Location", locState.Location.Name)
}

func TestLocationFollower_HandleEvent_IgnoresNonMoveEvents(t *testing.T) {
	lf := &locationFollower{
		characterID:  ulid.Make(),
		currentLocID: ulid.Make(),
		worldQuerier: &mockWorldQuerier{},
	}

	event := core.NewEvent("", core.EventTypeSay, core.Actor{}, nil)

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

	event := core.NewEvent("", core.EventTypeMove, core.Actor{}, movePayload)

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

	event := core.NewEvent("", core.EventTypeMove, core.Actor{}, movePayload)

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

	event := core.NewEvent("", core.EventTypeMove, core.Actor{}, nil)

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
	}

	// Presence comes from active sessions at the location, not character repo.
	ss := session.NewMemStore()
	_ = ss.Set(context.Background(), "s1", &session.Info{
		ID:            "s1",
		CharacterID:   charID,
		CharacterName: "Alice",
		LocationID:    locID,
		Status:        session.StatusActive,
	})

	lf := &locationFollower{worldQuerier: wq, sessionStore: ss}
	ev, err := lf.buildLocationState(context.Background(), locID)
	require.NoError(t, err)
	require.NotNil(t, ev)

	ef := ev.GetEvent()
	assert.Equal(t, string(core.EventTypeLocationState), ef.GetType())
	assert.Equal(t, "system", ef.GetActorType())
	assert.Equal(t, world.LocationStream(locID), ef.GetStream())

	var payload core.LocationStatePayload
	require.NoError(t, json.Unmarshal(ef.GetPayload(), &payload))
	assert.Equal(t, "Hall", payload.Location.Name)
	assert.Len(t, payload.Exits, 1)
	assert.Len(t, payload.Present, 1)
	assert.Equal(t, "Alice", payload.Present[0].Name)
}

// mockSubscription implements core.Subscription for tests.
type mockSubscription struct {
	addedStreams   []string
	removedStreams []string
	notifCh       chan core.StreamNotification
	errCh         chan error
}

func newMockSubscription() *mockSubscription {
	return &mockSubscription{
		notifCh: make(chan core.StreamNotification, 10),
		errCh:   make(chan error, 1),
	}
}

func (m *mockSubscription) AddStream(_ context.Context, stream string) error {
	m.addedStreams = append(m.addedStreams, stream)
	return nil
}

func (m *mockSubscription) RemoveStream(_ context.Context, stream string) error {
	m.removedStreams = append(m.removedStreams, stream)
	return nil
}

func (m *mockSubscription) Notifications() <-chan core.StreamNotification { return m.notifCh }
func (m *mockSubscription) Errors() <-chan error                         { return m.errCh }
func (m *mockSubscription) Close() error                                 { return nil }

func TestSwitchLocationSubscriptionAddsNewAndRemovesOldStream(t *testing.T) {
	charID := ulid.Make()
	oldLocID := ulid.Make()
	newLocID := ulid.Make()

	mockSub := newMockSubscription()

	lf := &locationFollower{
		characterID:   charID,
		currentLocID:  oldLocID,
		locStreamName: world.LocationStream(oldLocID),
		sub:           mockSub,
	}

	lf.switchLocationSubscription(context.Background(), newLocID)

	assert.Equal(t, world.LocationStream(newLocID), lf.locStreamName)
	require.Len(t, mockSub.addedStreams, 1)
	assert.Equal(t, world.LocationStream(newLocID), mockSub.addedStreams[0])
	require.Len(t, mockSub.removedStreams, 1)
	assert.Equal(t, world.LocationStream(oldLocID), mockSub.removedStreams[0])
}

func TestSwitchLocationSubscriptionIsNoOpWhenSubIsNil(t *testing.T) {
	lf := &locationFollower{
		characterID:  ulid.Make(),
		currentLocID: ulid.Make(),
		sub:          nil,
	}

	require.NotPanics(t, func() {
		lf.switchLocationSubscription(context.Background(), ulid.Make())
	})
}

func TestSendSyntheticSendsLocationStateForCurrentLocation(t *testing.T) {
	locID := ulid.Make()
	wq := &mockWorldQuerier{
		location: &world.Location{
			ID:          locID,
			Name:        "Library",
			Description: "A quiet library.",
		},
		exits: []*world.Exit{},
	}
	lf := &locationFollower{
		currentLocID: locID,
		worldQuerier: wq,
		sessionStore: session.NewMemStore(),
	}

	stream := &capturingStream{ctx: context.Background()}
	err := lf.sendSynthetic(context.Background(), stream)
	require.NoError(t, err)

	require.Len(t, stream.sent, 1)
	assert.Equal(t, string(core.EventTypeLocationState), stream.sent[0].GetEvent().GetType())
}

func TestSendSyntheticReturnsNilWhenWorldQuerierNil(t *testing.T) {
	lf := &locationFollower{
		currentLocID: ulid.Make(),
		worldQuerier: nil,
	}

	stream := &capturingStream{ctx: context.Background()}
	err := lf.sendSynthetic(context.Background(), stream)
	assert.NoError(t, err)
	assert.Empty(t, stream.sent)
}

func TestSendSyntheticReturnsNilWhenLocationIDZero(t *testing.T) {
	lf := &locationFollower{
		worldQuerier: &mockWorldQuerier{},
	}

	stream := &capturingStream{ctx: context.Background()}
	err := lf.sendSynthetic(context.Background(), stream)
	assert.NoError(t, err)
	assert.Empty(t, stream.sent)
}

func TestSwitchLocationSubscriptionSkipsRemoveWhenNoOldStream(t *testing.T) {
	mockSub := newMockSubscription()

	lf := &locationFollower{
		characterID:   ulid.Make(),
		currentLocID:  ulid.Make(),
		locStreamName: "", // no old stream name set
		sub:           mockSub,
	}

	newLocID := ulid.Make()
	lf.switchLocationSubscription(context.Background(), newLocID)

	// Should add new but not remove any (no old stream).
	require.Len(t, mockSub.addedStreams, 1)
	assert.Equal(t, world.LocationStream(newLocID), mockSub.addedStreams[0])
	assert.Empty(t, mockSub.removedStreams)
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
