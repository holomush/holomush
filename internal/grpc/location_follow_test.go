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
	"github.com/holomush/holomush/internal/testsupport/sessiontest"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	corecomm "github.com/holomush/holomush/plugins/core-communication"
)

// Compile-time check that *world.Service satisfies WorldQuerier.
var _ WorldQuerier = (*world.Service)(nil)

// testVerbRegistry returns a freshly-bootstrapped registry with the builtin
// verbs (including location_state) registered. Used by the locationFollower
// tests since buildLocationState now requires a non-nil registry to stamp
// RenderingMetadata on synthetic events (per INV-EVENTBUS-6 at internal/web/translate.go:42-60).
func testVerbRegistry(t *testing.T) *core.VerbRegistry {
	t.Helper()
	r, err := core.BootstrapVerbRegistry("test")
	require.NoError(t, err)
	return r
}

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
		verbRegistry: testVerbRegistry(t),
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

	event := core.NewEvent("", core.EventType(corecomm.EventTypeSay), core.Actor{}, nil)

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
	// GridPresent=true is required: ListActiveByLocation filters
	// `status='active' AND grid_present=true` (holomush-rsoe6.12, invariant
	// INV-PRESENCE-1 — a session in the location roster must be grid-present).
	ss := sessiontest.NewStore(t)
	_ = ss.Set(context.Background(), "s1", &session.Info{
		ID:            "s1",
		CharacterID:   charID,
		CharacterName: "Alice",
		LocationID:    locID,
		Status:        session.StatusActive,
		GridPresent:   true,
	})

	lf := &locationFollower{worldQuerier: wq, sessionStore: ss, verbRegistry: testVerbRegistry(t)}
	ev, err := lf.buildLocationState(context.Background(), locID)
	require.NoError(t, err)
	require.NotNil(t, ev)

	ef := ev.GetEvent()
	assert.Equal(t, string(core.EventTypeLocationState), ef.GetType())
	assert.Equal(t, "system", ef.GetActorType())
	assert.Equal(t, world.LocationStream(locID), ef.GetStream())

	// holomush-4wdu: RenderingMetadata MUST be stamped on synthetic
	// location_state events so the gateway's INV-EVENTBUS-6 guard doesn't drop them.
	rendering := ef.GetRendering()
	require.NotNil(t, rendering, "synthetic location_state MUST carry RenderingMetadata (INV-EVENTBUS-6)")
	assert.Equal(t, "state", rendering.GetCategory())
	assert.Equal(t, "snapshot", rendering.GetFormat())
	assert.Equal(t, corev1.EventChannel_EVENT_CHANNEL_STATE, rendering.GetDisplayTarget())
	assert.Equal(t, "builtin", rendering.GetSourcePlugin())

	var payload core.LocationStatePayload
	require.NoError(t, json.Unmarshal(ef.GetPayload(), &payload))
	assert.Equal(t, "Hall", payload.Location.Name)
	assert.Len(t, payload.Exits, 1)
	assert.Len(t, payload.Present, 1)
	assert.Equal(t, "Alice", payload.Present[0].Name)
	// holomush-e4qo: emit site MUST populate CharacterID (ULID) so the web
	// PresenceStore can key entries by ULID rather than display name.
	assert.Equal(t, charID.String(), payload.Present[0].CharacterID)
}

// recordingUpdater captures add/remove stream calls so tests can assert
// that switchLocationSubscription invokes the filter updater correctly.
type recordingUpdater struct {
	added   []string
	removed []string
}

func (r *recordingUpdater) update(_ context.Context, addStream, removeStream string) error {
	if addStream != "" {
		r.added = append(r.added, addStream)
	}
	if removeStream != "" {
		r.removed = append(r.removed, removeStream)
	}
	return nil
}

func TestSwitchLocationSubscriptionAddsNewAndRemovesOldStream(t *testing.T) {
	charID := ulid.Make()
	oldLocID := ulid.Make()
	newLocID := ulid.Make()

	rec := &recordingUpdater{}

	lf := &locationFollower{
		characterID:   charID,
		currentLocID:  oldLocID,
		locStreamName: world.LocationStream(oldLocID),
		updateFilters: rec.update,
	}

	lf.switchLocationSubscription(context.Background(), newLocID)

	assert.Equal(t, world.LocationStream(newLocID), lf.locStreamName)
	require.Len(t, rec.added, 1)
	assert.Equal(t, world.LocationStream(newLocID), rec.added[0])
	require.Len(t, rec.removed, 1)
	assert.Equal(t, world.LocationStream(oldLocID), rec.removed[0])
}

func TestSwitchLocationSubscriptionIsNoOpWhenUpdaterIsNil(t *testing.T) {
	lf := &locationFollower{
		characterID:   ulid.Make(),
		currentLocID:  ulid.Make(),
		updateFilters: nil,
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
		sessionStore: sessiontest.NewStore(t),
		verbRegistry: testVerbRegistry(t),
	}

	stream := &capturingStream{ctx: context.Background()}
	err := lf.sendSynthetic(context.Background(), stream)
	require.NoError(t, err)

	require.Len(t, stream.sent, 1)
	assert.Equal(t, string(core.EventTypeLocationState), stream.sent[0].GetEvent().GetType())
	// holomush-4wdu: synthetic location_state MUST carry RenderingMetadata
	// (gateway drops nil-Rendering events per INV-EVENTBUS-6).
	require.NotNil(t, stream.sent[0].GetEvent().GetRendering(),
		"synthetic location_state MUST carry RenderingMetadata (INV-EVENTBUS-6)")
}

// TestBuildLocationStateRendering_NilRegistryFailsClosed asserts the
// fail-closed branch when verbRegistry is unset. Locks in the
// LOCATION_STATE_NO_REGISTRY error code so external log monitoring can key
// alerts off it. Per holomush-4wdu: the original silent-drop symptom MUST
// be surfaced loudly, not silently dropped a second way.
func TestBuildLocationStateRendering_NilRegistryFailsClosed(t *testing.T) {
	_, err := buildLocationStateRendering(nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "LOCATION_STATE_NO_REGISTRY")
}

// TestBuildLocationStateRendering_UnregisteredVerbFailsClosed asserts the
// fail-closed branch when the location_state verb is missing from an
// otherwise-valid registry. Locks in LOCATION_STATE_UNREGISTERED.
func TestBuildLocationStateRendering_UnregisteredVerbFailsClosed(t *testing.T) {
	// Empty registry — no builtins registered.
	r := core.NewVerbRegistry()
	_, err := buildLocationStateRendering(r)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "LOCATION_STATE_UNREGISTERED")
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
	rec := &recordingUpdater{}

	lf := &locationFollower{
		characterID:   ulid.Make(),
		currentLocID:  ulid.Make(),
		locStreamName: "", // no old stream name set
		updateFilters: rec.update,
	}

	newLocID := ulid.Make()
	lf.switchLocationSubscription(context.Background(), newLocID)

	// Should add new but not remove any (no old stream).
	require.Len(t, rec.added, 1)
	assert.Equal(t, world.LocationStream(newLocID), rec.added[0])
	assert.Empty(t, rec.removed)
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
