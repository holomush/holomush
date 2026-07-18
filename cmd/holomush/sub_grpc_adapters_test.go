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

	"github.com/holomush/holomush/internal/eventbus"
)

// Round 3 coverage: busHistoryReaderAdapter's translation logic is pure — no
// JetStream needed — but its patch lines were uncovered because the
// only real call sites are the startup wiring.

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

	events, err := adapter.ReplayTail(context.Background(), "scene.01ABC", 10, time.Time{}, 0, ulid.ULID{})
	require.NoError(t, err)
	require.Len(t, events, 2)
	// Reversed: oldest-first.
	assert.Equal(t, e1.ID, events[0].ID)
	assert.Equal(t, e2.ID, events[1].ID)
	// Subject is the qualified subject (holomush-rops).
	assert.Equal(t, eventbus.Subject("events.main.scene.01ABC"), events[0].Subject)
}

func TestBusHistoryReaderReplayTailPassesBeforeIDOnQuery(t *testing.T) {
	before := ulid.Make()
	reader := &stubHistoryReader{stream: &stubHistoryStream{}}
	adapter := &busHistoryReaderAdapter{reader: reader, gameID: func() string { return "main" }}
	notBefore := time.Unix(100, 0).UTC()
	_, err := adapter.ReplayTail(context.Background(), "scene.01ABC", 5, notBefore, 0, before)
	require.NoError(t, err)
	assert.Equal(t, before, reader.gotQuery.BeforeID)
	assert.Equal(t, notBefore, reader.gotQuery.NotBefore)
	assert.Equal(t, eventbus.DirectionBackward, reader.gotQuery.Direction)
	assert.Equal(t, 5, reader.gotQuery.PageSize)
}

// TestBusHistoryReaderReplayTailPassesBeforeSeqOnQuery pins D-07: a nonzero
// beforeSeq must reach HistoryQuery.BeforeSeq, the field the hot and cold
// tiers actually key pagination on (BeforeID alone paginates nothing).
func TestBusHistoryReaderReplayTailPassesBeforeSeqOnQuery(t *testing.T) {
	const beforeSeq = uint64(42)
	before := ulid.Make()
	reader := &stubHistoryReader{stream: &stubHistoryStream{}}
	adapter := &busHistoryReaderAdapter{reader: reader, gameID: func() string { return "main" }}
	_, err := adapter.ReplayTail(context.Background(), "scene.01ABC", 5, time.Time{}, beforeSeq, before)
	require.NoError(t, err)
	assert.Equal(t, beforeSeq, reader.gotQuery.BeforeSeq)
	assert.Equal(t, before, reader.gotQuery.BeforeID)
}

// TestBusHistoryReaderReplayTailZeroBeforeSeqLeavesQueryBeforeSeqUnsetTailRead
// pins the settled D-07 legacy-cursor policy: beforeSeq==0 means "no cursor —
// read the tail", not "fall back to ID-only pagination" (no such fallback
// exists on either tier). HistoryQuery.BeforeSeq must stay at its zero value
// so the reader takes the tail-oriented read path.
func TestBusHistoryReaderReplayTailZeroBeforeSeqLeavesQueryBeforeSeqUnsetTailRead(t *testing.T) {
	reader := &stubHistoryReader{stream: &stubHistoryStream{}}
	adapter := &busHistoryReaderAdapter{reader: reader, gameID: func() string { return "main" }}
	_, err := adapter.ReplayTail(context.Background(), "scene.01ABC", 5, time.Time{}, 0, ulid.ULID{})
	require.NoError(t, err)
	assert.Zero(t, reader.gotQuery.BeforeSeq, "beforeSeq==0 must leave HistoryQuery.BeforeSeq unset (tail read), not synthesize an ID-only fallback")
}

func TestBusHistoryReaderReplayTailZeroCountReturnsNilWithoutQuery(t *testing.T) {
	reader := &stubHistoryReader{stream: &stubHistoryStream{}}
	adapter := &busHistoryReaderAdapter{reader: reader, gameID: func() string { return "main" }}
	events, err := adapter.ReplayTail(context.Background(), "scene.01ABC", 0, time.Time{}, 0, ulid.ULID{})
	require.NoError(t, err)
	assert.Nil(t, events)
}

func TestBusHistoryReaderReplayTailDefaultsEmptyGameIDToMain(t *testing.T) {
	reader := &stubHistoryReader{stream: &stubHistoryStream{}}
	adapter := &busHistoryReaderAdapter{reader: reader, gameID: func() string { return "" }}
	_, err := adapter.ReplayTail(context.Background(), "scene.01ABC", 5, time.Time{}, 0, ulid.ULID{})
	require.NoError(t, err)
	// Subject was translated using the "main" default.
	assert.Equal(t, eventbus.Subject("events.main.scene.01ABC"), reader.gotQuery.Subject)
}

func TestBusHistoryReaderReplayTailWrapsQueryFailure(t *testing.T) {
	sentinel := errors.New("query failed")
	reader := &stubHistoryReader{err: sentinel}
	adapter := &busHistoryReaderAdapter{reader: reader, gameID: func() string { return "main" }}
	_, err := adapter.ReplayTail(context.Background(), "scene.01ABC", 5, time.Time{}, 0, ulid.ULID{})
	require.Error(t, err)
	require.ErrorIs(t, err, sentinel)
}

func TestBusHistoryReaderReplayTailWrapsNextFailure(t *testing.T) {
	sentinel := errors.New("next boom")
	stream := &stubHistoryStream{err: sentinel, errOn: 0}
	reader := &stubHistoryReader{stream: stream}
	adapter := &busHistoryReaderAdapter{reader: reader, gameID: func() string { return "main" }}
	_, err := adapter.ReplayTail(context.Background(), "scene.01ABC", 5, time.Time{}, 0, ulid.ULID{})
	require.Error(t, err)
	require.ErrorIs(t, err, sentinel)
	assert.True(t, stream.closed, "stream is closed via defer even on error")
}

func TestBusHistoryReaderReplayTailRejectsInvalidStream(t *testing.T) {
	reader := &stubHistoryReader{}
	adapter := &busHistoryReaderAdapter{reader: reader, gameID: func() string { return "main" }}
	// Empty stream → Qualify fails.
	_, err := adapter.ReplayTail(context.Background(), "", 5, time.Time{}, 0, ulid.ULID{})
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
		"scene.01HYXSCENE00000000000000CC.ic", 10, time.Time{}, 0, ulid.ULID{})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err),
		"adapter MUST surface the plugin's PermissionDenied for plugin-owned subjects until the plugin-as-caller follow-up lands")
}
