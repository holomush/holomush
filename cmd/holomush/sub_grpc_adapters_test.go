// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
)

// Round 3 coverage: the adapter types in sub_grpc.go translate between
// core.* and eventbus.* types. They are pure translation logic — no
// JetStream needed — but their patch lines were uncovered because the
// only real call sites are the startup wiring.

// --- busEventAppender ---

// stubPublisher captures the last Publish call so tests can inspect the
// translated event.
type stubPublisher struct {
	gotEvent eventbus.Event
	called   bool
	returnTo error
}

func (s *stubPublisher) Publish(_ context.Context, e eventbus.Event) error {
	s.called = true
	s.gotEvent = e
	return s.returnTo
}

// stubBus returns a Subsystem-compatible GameID method through a tiny
// wrapper; since eventbus.Subsystem.GameID reads bus config, we bypass by
// embedding and using a real zero-value configured Subsystem.
//
// The simplest hook point is to go through eventbus.NewSubsystem with a
// known GameID in Config. Start is not required — GameID reads the cached
// Config field.
func newBusWithGameID(id string) *eventbus.Subsystem {
	cfg := eventbus.Config{GameID: id}
	return eventbus.NewSubsystem(cfg)
}

func TestBusEventAppenderQualifiesRelativeSubjectAndPublishes(t *testing.T) {
	pub := &stubPublisher{}
	bus := newBusWithGameID("main")
	appender := &busEventAppender{publisher: pub, bus: bus}

	evID := core.NewULID()
	charID := core.NewULID().String()
	require.NoError(t, appender.Append(context.Background(), core.Event{
		ID:        evID,
		Stream:    "location.01ABCDEFGHJKMNPQRSTVWXYZ00",
		Type:      core.EventType("pose"),
		Timestamp: time.Unix(42, 0).UTC(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: charID},
		Payload:   []byte(`{"x":1}`),
	}))

	require.True(t, pub.called)
	assert.Equal(t, evID, pub.gotEvent.ID)
	assert.Equal(t, "events.main.location.01ABCDEFGHJKMNPQRSTVWXYZ00", string(pub.gotEvent.Subject))
	assert.Equal(t, eventbus.Type("pose"), pub.gotEvent.Type)
	assert.Equal(t, eventbus.ActorKindCharacter, pub.gotEvent.Actor.Kind)
	// Parseable ULID ID is carried through.
	assert.NotEqual(t, ulid.ULID{}, pub.gotEvent.Actor.ID)
}

func TestBusEventAppenderDefaultsGameIDToMainWhenBusUnset(t *testing.T) {
	pub := &stubPublisher{}
	// Empty GameID → fallback to "main" via Append's own default.
	bus := newBusWithGameID("")
	appender := &busEventAppender{publisher: pub, bus: bus}
	require.NoError(t, appender.Append(context.Background(), core.Event{
		ID:        core.NewULID(),
		Stream:    "scene.01SCENE000000000000000000",
		Type:      core.EventType("system"),
		Timestamp: time.Unix(1, 0).UTC(),
		Actor:     core.Actor{Kind: core.ActorSystem, ID: "system"},
		Payload:   []byte(`{}`),
	}))
	assert.Equal(t, "events.main.scene.01SCENE000000000000000000", string(pub.gotEvent.Subject))
}

func TestBusEventAppenderWrapsPublishFailure(t *testing.T) {
	sentinel := errors.New("publish kaboom")
	pub := &stubPublisher{returnTo: sentinel}
	bus := newBusWithGameID("main")
	appender := &busEventAppender{publisher: pub, bus: bus}
	err := appender.Append(context.Background(), core.Event{
		ID:        core.NewULID(),
		Stream:    "location.01XYZ000000000000000000000",
		Type:      core.EventType("pose"),
		Timestamp: time.Unix(1, 0).UTC(),
		Actor:     core.Actor{Kind: core.ActorSystem, ID: "system"},
		Payload:   []byte(`{}`),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, sentinel)
}

func TestBusEventAppenderRejectsInvalidSubject(t *testing.T) {
	pub := &stubPublisher{}
	bus := newBusWithGameID("main")
	appender := &busEventAppender{publisher: pub, bus: bus}
	err := appender.Append(context.Background(), core.Event{
		ID:        core.NewULID(),
		Stream:    "", // empty stream reference → Qualify error
		Type:      core.EventType("pose"),
		Timestamp: time.Unix(1, 0).UTC(),
		Actor:     core.Actor{Kind: core.ActorSystem, ID: "system"},
		Payload:   []byte(`{}`),
	})
	require.Error(t, err)
	assert.False(t, pub.called, "must not publish when subject qualification fails")
}

func TestBusEventAppenderRejectsInvalidType(t *testing.T) {
	pub := &stubPublisher{}
	bus := newBusWithGameID("main")
	appender := &busEventAppender{publisher: pub, bus: bus}
	// Empty type → eventbus.NewType returns an error.
	err := appender.Append(context.Background(), core.Event{
		ID:        core.NewULID(),
		Stream:    "location.01ABC000000000000000000000",
		Type:      core.EventType(""),
		Timestamp: time.Unix(1, 0).UTC(),
		Actor:     core.Actor{Kind: core.ActorSystem, ID: "system"},
		Payload:   []byte(`{}`),
	})
	require.Error(t, err)
	assert.False(t, pub.called)
}

// --- coreToBusActor ---

func TestCoreToBusActorPreservesULIDID(t *testing.T) {
	id := ulid.Make()
	a := coreToBusActor(core.Actor{Kind: core.ActorCharacter, ID: id.String()})
	assert.Equal(t, eventbus.ActorKindCharacter, a.Kind)
	assert.Equal(t, id, a.ID)
}

// TestCoreToBusActorIgnoresNonULIDIDPostW9ml asserts the new boundary
// behavior post-w9ml: non-ULID IDs are silently dropped here (the
// strict gate at coreActorToEventbusActor in event_emitter.go surfaces
// ACTOR_ID_NOT_ULID before this boundary is reached for emit paths).
func TestCoreToBusActorIgnoresNonULIDIDPostW9ml(t *testing.T) {
	a := coreToBusActor(core.Actor{Kind: core.ActorPlugin, ID: "core-scenes"})
	assert.Equal(t, eventbus.ActorKindPlugin, a.Kind)
	assert.Equal(t, ulid.ULID{}, a.ID)
}

func TestCoreToBusActorEmptyIDLeavesIDZero(t *testing.T) {
	a := coreToBusActor(core.Actor{Kind: core.ActorSystem})
	assert.Equal(t, eventbus.ActorKindSystem, a.Kind)
	assert.Equal(t, ulid.ULID{}, a.ID)
}

func TestCoreActorKindToBusEveryKind(t *testing.T) {
	tests := []struct {
		name string
		in   core.ActorKind
		want eventbus.ActorKind
	}{
		{"character", core.ActorCharacter, eventbus.ActorKindCharacter},
		{"system", core.ActorSystem, eventbus.ActorKindSystem},
		{"plugin", core.ActorPlugin, eventbus.ActorKindPlugin},
		{"unknown falls through", core.ActorKind(99), eventbus.ActorKindUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, coreActorKindToBus(tc.in))
		})
	}
}

// --- busActorKindToCore ---

func TestBusActorKindToCoreEveryKind(t *testing.T) {
	tests := []struct {
		name string
		in   eventbus.ActorKind
		want core.ActorKind
	}{
		{"character", eventbus.ActorKindCharacter, core.ActorCharacter},
		{"system", eventbus.ActorKindSystem, core.ActorSystem},
		{"plugin", eventbus.ActorKindPlugin, core.ActorPlugin},
		// Unknown / Player fall back to ActorSystem per translation choice.
		{"unknown defaults to system", eventbus.ActorKindUnknown, core.ActorSystem},
		{"player defaults to system", eventbus.ActorKindPlayer, core.ActorSystem},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, busActorKindToCore(tc.in))
		})
	}
}

// --- busEventToCoreEvent ---

func TestBusEventToCoreEventPropagatesULIDActorID(t *testing.T) {
	id := ulid.Make()
	e := eventbus.Event{
		ID:        core.NewULID(),
		Subject:   eventbus.Subject("events.main.scene.01ABC"),
		Type:      eventbus.Type("system"),
		Timestamp: time.Unix(1, 0).UTC(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindCharacter, ID: id},
		Payload:   []byte(`{}`),
	}
	got := busEventToCoreEvent(e, "events.main.scene.01ABC")
	assert.Equal(t, id.String(), got.Actor.ID, "ULID actor id propagates through busEventToCoreEvent")
	assert.Equal(t, core.ActorCharacter, got.Actor.Kind)
	assert.Equal(t, e.ID, got.ID)
	assert.Equal(t, e.Timestamp, got.Timestamp, "read-back reconstruction preserves the persisted timestamp; core.NewEvent() would overwrite it with time.Now()")
	assert.Equal(t, "events.main.scene.01ABC", got.Stream)
}

func TestBusEventToCoreEventEmptyActorIDYieldsEmpty(t *testing.T) {
	e := eventbus.Event{
		ID:      core.NewULID(),
		Subject: eventbus.Subject("events.main.scene.01ABC"),
		Type:    eventbus.Type("system"),
		Actor:   eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload: []byte(`{}`),
	}
	got := busEventToCoreEvent(e, "events.main.scene.01ABC")
	assert.Empty(t, got.Actor.ID)
}

// --- busHistoryReaderAdapter ---

// stubHistoryStream feeds a pre-baked slice through Next() then returns io.EOF.
type stubHistoryStream struct {
	events []eventbus.Event
	i      int
	closed bool
	errOn  int // index at which to return a synthetic error instead of an event
	err    error
}

func (s *stubHistoryStream) Next(_ context.Context) (eventbus.Event, error) {
	if s.err != nil && s.i == s.errOn {
		return eventbus.Event{}, s.err
	}
	if s.i >= len(s.events) {
		return eventbus.Event{}, io.EOF
	}
	e := s.events[s.i]
	s.i++
	return e, nil
}

func (s *stubHistoryStream) Close() error { s.closed = true; return nil }

type stubHistoryReader struct {
	gotQuery eventbus.HistoryQuery
	stream   eventbus.HistoryStream
	err      error
}

func (s *stubHistoryReader) QueryHistory(_ context.Context, q eventbus.HistoryQuery) (eventbus.HistoryStream, error) {
	s.gotQuery = q
	if s.err != nil {
		return nil, s.err
	}
	return s.stream, nil
}

func TestBusHistoryReaderReplayTailReturnsReversedEvents(t *testing.T) {
	// QueryHistory returns newest-first; ReplayTail must reverse to oldest-first.
	e1 := eventbus.Event{
		ID:        ulid.Make(),
		Subject:   eventbus.Subject("events.main.scene.01ABC"),
		Type:      eventbus.Type("system"),
		Timestamp: time.Unix(1, 0).UTC(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload:   []byte(`{"n":1}`),
	}
	e2 := e1
	e2.ID = ulid.Make()
	e2.Payload = []byte(`{"n":2}`)
	reader := &stubHistoryReader{stream: &stubHistoryStream{events: []eventbus.Event{e2, e1}}}
	adapter := &busHistoryReaderAdapter{reader: reader, gameID: func() string { return "main" }}

	events, err := adapter.ReplayTail(context.Background(), "scene.01ABC", 10, time.Time{}, ulid.ULID{})
	require.NoError(t, err)
	require.Len(t, events, 2)
	// Reversed: oldest-first.
	assert.Equal(t, e1.ID, events[0].ID)
	assert.Equal(t, e2.ID, events[1].ID)
	// Stream name is the qualified subject (holomush-rops).
	assert.Equal(t, "events.main.scene.01ABC", events[0].Stream)
}

func TestBusHistoryReaderReplayTailPassesBeforeIDOnQuery(t *testing.T) {
	before := ulid.Make()
	reader := &stubHistoryReader{stream: &stubHistoryStream{}}
	adapter := &busHistoryReaderAdapter{reader: reader, gameID: func() string { return "main" }}
	notBefore := time.Unix(100, 0).UTC()
	_, err := adapter.ReplayTail(context.Background(), "scene.01ABC", 5, notBefore, before)
	require.NoError(t, err)
	assert.Equal(t, before, reader.gotQuery.BeforeID)
	assert.Equal(t, notBefore, reader.gotQuery.NotBefore)
	assert.Equal(t, eventbus.DirectionBackward, reader.gotQuery.Direction)
	assert.Equal(t, 5, reader.gotQuery.PageSize)
}

func TestBusHistoryReaderReplayTailZeroCountReturnsNilWithoutQuery(t *testing.T) {
	reader := &stubHistoryReader{stream: &stubHistoryStream{}}
	adapter := &busHistoryReaderAdapter{reader: reader, gameID: func() string { return "main" }}
	events, err := adapter.ReplayTail(context.Background(), "scene.01ABC", 0, time.Time{}, ulid.ULID{})
	require.NoError(t, err)
	assert.Nil(t, events)
}

func TestBusHistoryReaderReplayTailDefaultsEmptyGameIDToMain(t *testing.T) {
	reader := &stubHistoryReader{stream: &stubHistoryStream{}}
	adapter := &busHistoryReaderAdapter{reader: reader, gameID: func() string { return "" }}
	_, err := adapter.ReplayTail(context.Background(), "scene.01ABC", 5, time.Time{}, ulid.ULID{})
	require.NoError(t, err)
	// Subject was translated using the "main" default.
	assert.Equal(t, eventbus.Subject("events.main.scene.01ABC"), reader.gotQuery.Subject)
}

func TestBusHistoryReaderReplayTailWrapsQueryFailure(t *testing.T) {
	sentinel := errors.New("query failed")
	reader := &stubHistoryReader{err: sentinel}
	adapter := &busHistoryReaderAdapter{reader: reader, gameID: func() string { return "main" }}
	_, err := adapter.ReplayTail(context.Background(), "scene.01ABC", 5, time.Time{}, ulid.ULID{})
	require.Error(t, err)
	require.ErrorIs(t, err, sentinel)
}

func TestBusHistoryReaderReplayTailWrapsNextFailure(t *testing.T) {
	sentinel := errors.New("next boom")
	stream := &stubHistoryStream{err: sentinel, errOn: 0}
	reader := &stubHistoryReader{stream: stream}
	adapter := &busHistoryReaderAdapter{reader: reader, gameID: func() string { return "main" }}
	_, err := adapter.ReplayTail(context.Background(), "scene.01ABC", 5, time.Time{}, ulid.ULID{})
	require.Error(t, err)
	require.ErrorIs(t, err, sentinel)
	assert.True(t, stream.closed, "stream is closed via defer even on error")
}

func TestBusHistoryReaderReplayTailRejectsInvalidStream(t *testing.T) {
	reader := &stubHistoryReader{}
	adapter := &busHistoryReaderAdapter{reader: reader, gameID: func() string { return "main" }}
	// Empty stream → Qualify fails.
	_, err := adapter.ReplayTail(context.Background(), "", 5, time.Time{}, ulid.ULID{})
	require.Error(t, err)
}

// TestBusHistoryReaderAdapterFailsClosedOnPluginOwnedSubjects covers spec
// §6.5 case 6: when the plugin's PluginAuditService.QueryHistory returns
// PermissionDenied (e.g., because the plugin-as-caller invariant cannot yet
// be satisfied — this adapter passes a zero Caller — or because the plugin
// fails its own membership check), the adapter MUST surface the
// PermissionDenied to the plugin caller. It MUST NOT swallow or downgrade
// that into an opaque or generic error: the plugin needs to see the precise
// gRPC code so it can react (e.g., return an empty page rather than spin).
//
// This test pins the §4.4 fail-closed contract for the deferred
// plugin-as-caller follow-up. If the adapter is later changed to pass a
// real plugin Caller, this test should still pass — the adapter MUST
// propagate PermissionDenied unchanged for plugin-owned subjects.
func TestBusHistoryReaderAdapterFailsClosedOnPluginOwnedSubjects(t *testing.T) {
	reader := &stubHistoryReader{
		err: status.Error(codes.PermissionDenied, "caller required"),
	}
	adapter := &busHistoryReaderAdapter{
		reader: reader,
		gameID: func() string { return "main" },
	}

	_, err := adapter.ReplayTail(context.Background(),
		"scene.01HYXSCENE00000000000000CC.ic", 10, time.Time{}, ulid.ULID{})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err),
		"adapter MUST surface the plugin's PermissionDenied for plugin-owned subjects until the plugin-as-caller follow-up lands")
}
