// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/session"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// --- Fakes for runSubscribeLoop / dispatchDelivery ---------------------

// fakeSessionStream implements eventbus.SessionStream. Deliveries are queued
// via push; each Next pulls one or blocks until ctx cancels. Close causes
// subsequent Next calls to return io.EOF.
type fakeSessionStream struct {
	mu            sync.Mutex
	ch            chan eventbus.Delivery
	setFiltersErr error
	setFilters    [][]eventbus.Subject
	closeCount    int
	closed        bool
}

func newFakeSessionStream() *fakeSessionStream {
	return &fakeSessionStream{ch: make(chan eventbus.Delivery, 8)}
}

func (f *fakeSessionStream) push(d eventbus.Delivery) {
	f.ch <- d
}

func (f *fakeSessionStream) Next(ctx context.Context) (eventbus.Delivery, error) {
	select {
	case d, ok := <-f.ch:
		if !ok {
			return nil, io.EOF
		}
		return d, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeSessionStream) SetFilters(_ context.Context, filters []eventbus.Subject) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setFilters = append(f.setFilters, append([]eventbus.Subject(nil), filters...))
	return f.setFiltersErr
}

func (f *fakeSessionStream) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeCount++
	if !f.closed {
		f.closed = true
		close(f.ch)
	}
	return nil
}

var _ eventbus.SessionStream = (*fakeSessionStream)(nil)

// fakeDelivery implements eventbus.Delivery with counters for Ack/Nack.
type fakeDelivery struct {
	ev           eventbus.Event
	metadataOnly bool
	ackErr       error
	nackErr      error
	ackCnt       int
	nackCnt      int
	inProgCnt    int
	mu           sync.Mutex
}

func (d *fakeDelivery) Event() eventbus.Event { return d.ev }
func (d *fakeDelivery) MetadataOnly() bool    { return d.metadataOnly }

func (d *fakeDelivery) Ack() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ackCnt++
	return d.ackErr
}

func (d *fakeDelivery) Nack() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.nackCnt++
	return d.nackErr
}

func (d *fakeDelivery) InProgress() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.inProgCnt++
	return nil
}

func (d *fakeDelivery) acks() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ackCnt
}

func (d *fakeDelivery) nacks() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.nackCnt
}

var _ eventbus.Delivery = (*fakeDelivery)(nil)

// makeDelivery builds a fakeDelivery carrying an event of the given type on
// the given character stream.
func makeDelivery(t *testing.T, evType, characterID string) *fakeDelivery {
	t.Helper()
	id := core.NewULID()
	return &fakeDelivery{
		ev: eventbus.Event{
			ID:        id,
			Subject:   eventbus.Subject("events.main.character." + characterID),
			Type:      eventbus.Type(evType),
			Timestamp: time.Now(),
			Payload:   []byte("{}"),
		},
	}
}

// --- Tests: dispatchDelivery ------------------------------------------

func TestDispatchDeliveryForwardsAndAcks(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	stream := &fakeSubscribeStream{ctx: context.Background()}
	charID := core.NewULID().String()
	d := makeDelivery(t, "say", charID)

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, d.acks())
	assert.Equal(t, 0, d.nacks())
	require.Len(t, stream.sent, 1)
	assert.Equal(t, "say", stream.sent[0].GetEvent().GetType())
}

func TestDispatchDeliveryNacksOnSendError(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	stream := &fakeSubscribeStream{ctx: context.Background(), err: errors.New("send boom")}
	charID := core.NewULID().String()
	d := makeDelivery(t, "say", charID)

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil, nil)
	require.Error(t, err)
	assert.Equal(t, 0, d.acks(), "no ack on send failure — JS must redeliver")
	assert.Equal(t, 1, d.nacks())
}

func TestDispatchDeliveryAckFailureLogsButReturnsNil(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	stream := &fakeSubscribeStream{ctx: context.Background()}
	charID := core.NewULID().String()
	d := makeDelivery(t, "say", charID)
	d.ackErr = errors.New("ack boom")

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil, nil)
	require.NoError(t, err, "ack failure must not propagate — JS will redeliver")
	assert.Equal(t, 1, d.acks())
}

func TestDispatchDeliveryTerminatesOnMatchingSessionEnded(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	stream := &fakeSubscribeStream{ctx: context.Background()}
	charID := core.NewULID().String()

	d := makeDelivery(t, string(core.EventTypeSessionEnded), charID)
	payload, _ := json.Marshal(core.SessionEndedPayload{
		SessionID: "s1",
		Cause:     core.SessionEndedCauseQuit,
		Reason:    "bye",
	})
	d.ev.Payload = payload

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil, nil)
	assert.ErrorIs(t, err, errStreamTerminated)
	// One event frame, plus one STREAM_CLOSED control frame.
	require.Len(t, stream.sent, 2)
	assert.Equal(t, corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED, stream.sent[1].GetControl().GetSignal())
	assert.Equal(t, "bye", stream.sent[1].GetControl().GetMessage())
}

func TestDispatchDeliveryIgnoresNonMatchingSessionEnded(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	stream := &fakeSubscribeStream{ctx: context.Background()}
	charID := core.NewULID().String()

	d := makeDelivery(t, string(core.EventTypeSessionEnded), charID)
	payload, _ := json.Marshal(core.SessionEndedPayload{
		SessionID: "SOME_OTHER_SESSION",
		Cause:     core.SessionEndedCauseQuit,
	})
	d.ev.Payload = payload

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil, nil)
	require.NoError(t, err)
	// Forwarded verbatim, no STREAM_CLOSED.
	require.Len(t, stream.sent, 1)
	assert.Nil(t, stream.sent[0].GetControl())
}

func TestDispatchDeliverySessionEndedBadPayloadLogsAndSurvives(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	stream := &fakeSubscribeStream{ctx: context.Background()}
	charID := core.NewULID().String()

	d := makeDelivery(t, string(core.EventTypeSessionEnded), charID)
	d.ev.Payload = []byte("not-json")

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil, nil)
	require.NoError(t, err, "unmarshal failure must not error the stream")
	require.Len(t, stream.sent, 1)
}

// TestDispatchDeliverySkipsAuditOnlyEvents is the dispatch-side regression
// lock for INV-CRYPTO-81 / holomush-jxo8.6.26: events tagged with
// EventChannelAuditOnly (e.g. crypto.totp_*, crypto.policy_set) MUST be
// ack'd and silently dropped before stream.Send. The persist-side
// counterpart lives at test/integration/eventbus_e2e/audit_only_channel_test.go;
// this is the unit-level dispatch filter assertion at
// internal/grpc/server.go (~line 1019).
func TestDispatchDeliverySkipsAuditOnlyEvents(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	stream := &fakeSubscribeStream{ctx: context.Background()}
	charID := core.NewULID().String()

	d := makeDelivery(t, "crypto.totp_locked", charID)
	d.ev.Subject = eventbus.Subject("events.main.system.crypto_totp." + charID + ".locked")
	d.ev.Rendering = &eventbus.RenderingMetadata{
		DisplayTarget: eventbus.EventChannelAuditOnly,
		SourcePlugin:  "builtin",
		Category:      "system",
	}

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, d.acks(), "audit-only event must be ack'd so JS can age it out")
	assert.Equal(t, 0, d.nacks())
	assert.Empty(t, stream.sent,
		"audit-only event must NOT reach client streams (INV-CRYPTO-81)")
}

// makeLocationDelivery builds a fakeDelivery carrying an event on the given
// NATS-form location subject with an explicit timestamp. The Subject is the
// production format (events.<game>.location.<locID>); dispatchDelivery feeds
// that qualified subject directly to the dot-only streamScopeFloor classifier
// (holomush-rops).
// gameID is fixed to "main" — every dispatchDelivery test in this file uses
// the default game; if a multi-game test arrives later it should construct
// its own delivery rather than re-introducing an always-"main" parameter.
func makeLocationDelivery(t *testing.T, locID string, ts time.Time) *fakeDelivery {
	t.Helper()
	return &fakeDelivery{
		ev: eventbus.Event{
			ID:        core.NewULID(),
			Subject:   eventbus.Subject("events.main.location." + locID),
			Type:      eventbus.Type("say"),
			Timestamp: ts,
			Payload:   []byte("{}"),
		},
	}
}

// erroringSessionStore returns a fixed error from Get and panics from any
// other method. Used to exercise the fail-closed branch of
// dispatchDelivery's filter-at-delivery prelude.
type erroringSessionStore struct {
	session.Store // embedded so any unused method panics via nil deref
	getErr        error
}

func (e *erroringSessionStore) Get(_ context.Context, _ string) (*session.Info, error) {
	return nil, e.getErr
}

// TestDispatchDeliveryDropsEventEmittedInSameNanosecondAsArrival gates
// INV-STORE-6. The floor comparison MUST operate at nanosecond resolution.
// An event whose Timestamp is one nanosecond BELOW the floor
// (LocationArrivedAt) MUST be filtered out by dispatchDelivery.
//
// Drift fix (holomush-9mxr Task 10): arrivedAt uses microsecond precision
// (no sub-microsecond nanos) so the Postgres round-trip in newTestSessionStore
// does not truncate the stored value. dispatchDelivery re-reads the session
// from the store per event, so precision must survive the PG round-trip.
func TestDispatchDeliveryDropsEventEmittedInSameNanosecondAsArrival(t *testing.T) {
	t.Parallel()
	locID := core.NewULID()
	// Microsecond-precision timestamp (123456us = 123456000ns) — survives
	// Postgres timestamptz microsecond storage without truncation.
	arrivedAt := time.Date(2026, 5, 22, 12, 0, 0, 123456000, time.UTC)
	info := &session.Info{
		ID:                "s1",
		CharacterID:       core.NewULID(),
		LocationID:        locID,
		LocationArrivedAt: arrivedAt,
	}
	store := newTestSessionStore(t, map[string]*session.Info{"s1": info})
	s := &CoreServer{sessionStore: store}
	stream := &fakeSubscribeStream{ctx: context.Background()}

	// Event timestamp one ns BELOW the floor.
	evTs := arrivedAt.Add(-1 * time.Nanosecond)
	d := makeLocationDelivery(t, locID.String(), evTs)

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil, nil)
	require.NoError(t, err)
	require.Len(t, stream.sent, 0,
		"INV-STORE-6: event one ns below floor MUST be filtered at dispatchDelivery")
	assert.Equal(t, 1, d.acks(),
		"filtered event is ack'd (consumed, not forwarded)")
}

// TestDispatchDeliveryIncludesEventAtExactFloorNanosecond gates INV-STORE-7.
// The floor MUST use >= semantics: an event whose Timestamp exactly equals
// LocationArrivedAt MUST be INCLUDED in the visible window.
//
// Drift fix (holomush-9mxr Task 10): same microsecond-precision rationale as
// TestDispatchDeliveryDropsEventEmittedInSameNanosecondAsArrival above.
func TestDispatchDeliveryIncludesEventAtExactFloorNanosecond(t *testing.T) {
	t.Parallel()
	locID := core.NewULID()
	// Microsecond-precision timestamp — survives Postgres timestamptz storage.
	arrivedAt := time.Date(2026, 5, 22, 12, 0, 0, 123456000, time.UTC)
	info := &session.Info{
		ID:                "s1",
		CharacterID:       core.NewULID(),
		LocationID:        locID,
		LocationArrivedAt: arrivedAt,
	}
	store := newTestSessionStore(t, map[string]*session.Info{"s1": info})
	s := &CoreServer{sessionStore: store}
	stream := &fakeSubscribeStream{ctx: context.Background()}

	// Event timestamp exactly equal to the floor.
	d := makeLocationDelivery(t, locID.String(), arrivedAt)

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil, nil)
	require.NoError(t, err)
	require.Len(t, stream.sent, 1,
		"INV-STORE-7: event at exact floor ns MUST be included (>= semantics)")
	assert.Equal(t, 1, d.acks())
}

// TestDispatchDeliveryDropsBelowScopeFloor is the unit-level regression lock
// for holomush-iwzt INV-PRIVACY-1 Tier 2 (load-bearing privacy gate per spec
// §6.2): events with a timestamp strictly before the session's
// streamScopeFloor for the event's subject MUST be ack'd and dropped before
// stream.Send. Ack-and-return (not skip-without-ack) so JetStream does not
// redeliver the dropped event indefinitely.
func TestDispatchDeliveryDropsBelowScopeFloor(t *testing.T) {
	t.Parallel()
	locID := core.NewULID()
	arrivedAt := time.Now()
	info := &session.Info{
		ID:                "s1",
		CharacterID:       core.NewULID(),
		LocationID:        locID,
		LocationArrivedAt: arrivedAt,
	}
	store := newTestSessionStore(t, map[string]*session.Info{"s1": info})
	s := &CoreServer{sessionStore: store}
	stream := &fakeSubscribeStream{ctx: context.Background()}

	// Event timestamp one hour BEFORE LocationArrivedAt → below floor.
	d := makeLocationDelivery(t, locID.String(), arrivedAt.Add(-time.Hour))

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, d.acks(),
		"below-floor event must be ack'd so JS does not redeliver indefinitely")
	assert.Equal(t, 0, d.nacks())
	assert.Empty(t, stream.sent,
		"below-floor event must NOT reach client stream (INV-PRIVACY-1 Tier 2)")
}

// TestDispatchDeliveryForwardsAtOrAboveScopeFloor asserts the converse: an
// event at or after the session's scope floor flows through to the client.
func TestDispatchDeliveryForwardsAtOrAboveScopeFloor(t *testing.T) {
	t.Parallel()
	locID := core.NewULID()
	arrivedAt := time.Now().Add(-time.Hour)
	info := &session.Info{
		ID:                "s1",
		CharacterID:       core.NewULID(),
		LocationID:        locID,
		LocationArrivedAt: arrivedAt,
	}
	store := newTestSessionStore(t, map[string]*session.Info{"s1": info})
	s := &CoreServer{sessionStore: store}
	stream := &fakeSubscribeStream{ctx: context.Background()}

	// Event timestamp one minute AFTER LocationArrivedAt → above floor.
	d := makeLocationDelivery(t, locID.String(), arrivedAt.Add(time.Minute))

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, d.acks())
	assert.Equal(t, 0, d.nacks())
	require.Len(t, stream.sent, 1,
		"above-floor event must reach client stream")
	assert.Equal(t, "say", stream.sent[0].GetEvent().GetType())
}

// TestDispatchDeliveryFallsBackToCachedInfoOnLookupFailure exercises the
// store-lookup-failure branch: when sessionStore.Get returns an error
// (typically because the session was just deleted by quit/evict), the
// filter uses the cached `info` parameter for floor evaluation rather than
// dropping the event. This lets the in-flight session_ended event reach
// the client so the Subscribe stream closes gracefully — otherwise the
// client would be orphaned on /terminal forever waiting for a redirect.
func TestDispatchDeliveryFallsBackToCachedInfoOnLookupFailure(t *testing.T) {
	t.Parallel()
	locID := core.NewULID()
	arrivedAt := time.Now().Add(-time.Hour)
	info := &session.Info{
		ID:                "s1",
		CharacterID:       core.NewULID(),
		LocationID:        locID,
		LocationArrivedAt: arrivedAt,
	}
	store := &erroringSessionStore{getErr: errors.New("session not found")}
	s := &CoreServer{sessionStore: store}
	stream := &fakeSubscribeStream{ctx: context.Background()}

	// Event timestamp AFTER cached LocationArrivedAt → above cached floor.
	// With fallback to cached info, the event passes through.
	d := makeLocationDelivery(t, locID.String(), arrivedAt.Add(time.Minute))

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil, nil)
	require.NoError(t, err,
		"lookup failure must not propagate — JS would redeliver forever")
	assert.Equal(t, 1, d.acks())
	require.Len(t, stream.sent, 1,
		"lookup failure falls back to cached info; above-cached-floor event reaches client")
}

// TestDispatchDeliveryUsesCachedFloorOnLookupFailure asserts the cached-info
// fallback still enforces privacy: an event BELOW the cached floor is still
// dropped even when Get fails. This pins the invariant that the fallback is
// a refresh-failure tolerance, not a privacy escape hatch.
func TestDispatchDeliveryUsesCachedFloorOnLookupFailure(t *testing.T) {
	t.Parallel()
	locID := core.NewULID()
	arrivedAt := time.Now()
	info := &session.Info{
		ID:                "s1",
		CharacterID:       core.NewULID(),
		LocationID:        locID,
		LocationArrivedAt: arrivedAt,
	}
	store := &erroringSessionStore{getErr: errors.New("session not found")}
	s := &CoreServer{sessionStore: store}
	stream := &fakeSubscribeStream{ctx: context.Background()}

	// Event timestamp BEFORE cached LocationArrivedAt → still dropped.
	d := makeLocationDelivery(t, locID.String(), arrivedAt.Add(-time.Hour))

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, d.acks(), "below-cached-floor event must still be ack'd and dropped")
	assert.Empty(t, stream.sent,
		"cached-info fallback still enforces the Subscribe-open floor")
}

// --- Tests: applyFilterCtrl -------------------------------------------

func TestApplyFilterCtrlRejectsLocationStreams(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	bs := newFakeSessionStream()
	filterSet := map[eventbus.Subject]struct{}{}

	ctrl := sessionStreamUpdate{stream: "location." + "01HYXYZ0C0000000000000000C", add: true}
	err := s.applyFilterCtrl(context.Background(), info, bs, filterSet, ctrl)
	require.NoError(t, err)
	assert.Empty(t, filterSet, "location filters must be owned by locationFollower")
	assert.Empty(t, bs.setFilters, "SetFilters must not be called for rejected stream")
}

func TestApplyFilterCtrlAddsAndCallsSetFilters(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	bs := newFakeSessionStream()
	filterSet := map[eventbus.Subject]struct{}{}

	charID := core.NewULID().String()
	ctrl := sessionStreamUpdate{stream: "character." + charID, add: true}
	err := s.applyFilterCtrl(context.Background(), info, bs, filterSet, ctrl)
	require.NoError(t, err)
	assert.Len(t, filterSet, 1)
	require.Len(t, bs.setFilters, 1)
}

func TestApplyFilterCtrlAddIdempotentWhenExists(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	bs := newFakeSessionStream()
	charID := core.NewULID().String()
	sub := eventbus.Subject("events.main.character." + charID)
	filterSet := map[eventbus.Subject]struct{}{sub: {}}

	ctrl := sessionStreamUpdate{stream: "character." + charID, add: true}
	err := s.applyFilterCtrl(context.Background(), info, bs, filterSet, ctrl)
	require.NoError(t, err)
	assert.Empty(t, bs.setFilters, "no SetFilters call when already present")
}

func TestApplyFilterCtrlRemovesAndCallsSetFilters(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	bs := newFakeSessionStream()
	charID := core.NewULID().String()
	sub := eventbus.Subject("events.main.character." + charID)
	filterSet := map[eventbus.Subject]struct{}{sub: {}}

	ctrl := sessionStreamUpdate{stream: "character." + charID, add: false}
	err := s.applyFilterCtrl(context.Background(), info, bs, filterSet, ctrl)
	require.NoError(t, err)
	assert.Empty(t, filterSet)
	require.Len(t, bs.setFilters, 1)
}

func TestApplyFilterCtrlRemoveIdempotentWhenMissing(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	bs := newFakeSessionStream()
	filterSet := map[eventbus.Subject]struct{}{}

	charID := core.NewULID().String()
	ctrl := sessionStreamUpdate{stream: "character." + charID, add: false}
	err := s.applyFilterCtrl(context.Background(), info, bs, filterSet, ctrl)
	require.NoError(t, err)
	assert.Empty(t, bs.setFilters)
}

func TestApplyFilterCtrlRejectsInvalidStream(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	bs := newFakeSessionStream()
	filterSet := map[eventbus.Subject]struct{}{}

	ctrl := sessionStreamUpdate{stream: "character::bad", add: true}
	err := s.applyFilterCtrl(context.Background(), info, bs, filterSet, ctrl)
	require.Error(t, err)
}

func TestApplyFilterCtrlPropagatesSetFiltersError(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	bs := newFakeSessionStream()
	bs.setFiltersErr = errors.New("js bust")
	filterSet := map[eventbus.Subject]struct{}{}

	charID := core.NewULID().String()
	ctrl := sessionStreamUpdate{stream: "character." + charID, add: true}
	err := s.applyFilterCtrl(context.Background(), info, bs, filterSet, ctrl)
	require.Error(t, err)
}

// --- Tests: makeFilterUpdater -----------------------------------------

func TestMakeFilterUpdaterAddsAndRemovesCorrectly(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	bs := newFakeSessionStream()
	filterSet := map[eventbus.Subject]struct{}{}
	updater := s.makeFilterUpdater(bs, filterSet)

	charA := ulid.MustNew(ulid.Timestamp(time.Now()), ulidEntropy{1})
	charB := ulid.MustNew(ulid.Timestamp(time.Now()), ulidEntropy{2})
	addStream := "character." + charA.String()
	removeStream := "character." + charB.String()

	// Seed charB as present in filterSet so removal actually deletes it.
	bSub, err := s.toSubject("main", removeStream)
	require.NoError(t, err)
	filterSet[bSub] = struct{}{}

	err = updater(context.Background(), addStream, removeStream)
	require.NoError(t, err)
	aSub, _ := s.toSubject("main", addStream)
	assert.Contains(t, filterSet, aSub)
	assert.NotContains(t, filterSet, bSub)
	require.Len(t, bs.setFilters, 1)
}

func TestMakeFilterUpdaterRejectsInvalidAdd(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	bs := newFakeSessionStream()
	filterSet := map[eventbus.Subject]struct{}{}
	updater := s.makeFilterUpdater(bs, filterSet)

	err := updater(context.Background(), "character::bad", "")
	require.Error(t, err)
}

func TestMakeFilterUpdaterRejectsInvalidRemove(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	bs := newFakeSessionStream()
	filterSet := map[eventbus.Subject]struct{}{}
	updater := s.makeFilterUpdater(bs, filterSet)

	err := updater(context.Background(), "", "character::bad")
	require.Error(t, err)
}

func TestMakeFilterUpdaterNoopForEmptyStrings(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	bs := newFakeSessionStream()
	filterSet := map[eventbus.Subject]struct{}{}
	updater := s.makeFilterUpdater(bs, filterSet)

	err := updater(context.Background(), "", "")
	require.NoError(t, err)
	// SetFilters still gets called once with empty slice — exercises the path.
	assert.Len(t, bs.setFilters, 1)
}

// --- Tests: runSubscribeLoop ------------------------------------------

func TestRunSubscribeLoopDeliversEventsThenReturnsOnCtxCancel(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	bs := newFakeSessionStream()
	charID := core.NewULID().String()

	d1 := makeDelivery(t, "say", charID)
	d2 := makeDelivery(t, "pose", charID)
	bs.push(d1)
	bs.push(d2)

	stream := &fakeSubscribeStream{ctx: context.Background()}
	ctrlCh := make(chan sessionStreamUpdate, 1)
	filterSet := map[eventbus.Subject]struct{}{}

	ctx, cancel := context.WithCancel(context.Background())
	stream.ctx = ctx

	// Run loop in goroutine; wait for acks then cancel.
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.runSubscribeLoop(ctx, info, bs, filterSet, stream, nil, ctrlCh, nil)
	}()

	// Wait for both deliveries to be acked.
	require.Eventually(t, func() bool {
		return d1.acks() == 1 && d2.acks() == 1
	}, 2*time.Second, 10*time.Millisecond)

	cancel()
	select {
	case err := <-errCh:
		// context.Canceled → nil return (ctx.Err() == context.Canceled branch).
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("runSubscribeLoop did not return after ctx cancel")
	}
	assert.GreaterOrEqual(t, len(stream.sent), 2)
}

func TestRunSubscribeLoopReturnsOnSendError(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	bs := newFakeSessionStream()
	charID := core.NewULID().String()

	d1 := makeDelivery(t, "say", charID)
	bs.push(d1)

	ctx := context.Background()
	stream := &fakeSubscribeStream{ctx: ctx, err: errors.New("send fail")}
	ctrlCh := make(chan sessionStreamUpdate, 1)
	filterSet := map[eventbus.Subject]struct{}{}

	err := s.runSubscribeLoop(ctx, info, bs, filterSet, stream, nil, ctrlCh, nil)
	require.Error(t, err)
	assert.Equal(t, 0, d1.acks(), "must not ack on send failure")
	assert.Equal(t, 1, d1.nacks())
}

func TestRunSubscribeLoopReturnsNilOnSessionEnded(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	bs := newFakeSessionStream()
	charID := core.NewULID().String()

	d := makeDelivery(t, string(core.EventTypeSessionEnded), charID)
	payload, _ := json.Marshal(core.SessionEndedPayload{SessionID: "s1", Reason: "goodbye"})
	d.ev.Payload = payload
	bs.push(d)

	ctx := context.Background()
	stream := &fakeSubscribeStream{ctx: ctx}
	ctrlCh := make(chan sessionStreamUpdate, 1)
	filterSet := map[eventbus.Subject]struct{}{}

	err := s.runSubscribeLoop(ctx, info, bs, filterSet, stream, nil, ctrlCh, nil)
	assert.NoError(t, err, "errStreamTerminated collapses to nil at caller boundary")
}

func TestRunSubscribeLoopAppliesFilterCtrl(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	bs := newFakeSessionStream()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := &fakeSubscribeStream{ctx: ctx}
	ctrlCh := make(chan sessionStreamUpdate, 2)
	filterSet := map[eventbus.Subject]struct{}{}

	charID := core.NewULID().String()
	ctrlCh <- sessionStreamUpdate{stream: "character." + charID, add: true}
	// Location stream: rejected path (logged warning).
	ctrlCh <- sessionStreamUpdate{stream: "location." + "01HYXYZ0C0000000000000000C", add: true}

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.runSubscribeLoop(ctx, info, bs, filterSet, stream, nil, ctrlCh, nil)
	}()

	// Wait for SetFilters to be called once (only the character:add succeeds).
	require.Eventually(t, func() bool {
		bs.mu.Lock()
		defer bs.mu.Unlock()
		return len(bs.setFilters) == 1
	}, 2*time.Second, 10*time.Millisecond)

	cancel()
	<-errCh
	assert.Len(t, filterSet, 1)
}

func TestRunSubscribeLoopReturnsNilOnCtrlChClose(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	bs := newFakeSessionStream()
	ctx := context.Background()
	stream := &fakeSubscribeStream{ctx: ctx}
	ctrlCh := make(chan sessionStreamUpdate)
	close(ctrlCh)

	err := s.runSubscribeLoop(ctx, info, bs, map[eventbus.Subject]struct{}{}, stream, nil, ctrlCh, nil)
	assert.NoError(t, err)
}

func TestRunSubscribeLoopReturnsNilOnDeliveriesClose(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	bs := newFakeSessionStream()
	// Close immediately → Next returns io.EOF → loop returns nil.
	_ = bs.Close()
	ctx := context.Background()
	stream := &fakeSubscribeStream{ctx: ctx}
	ctrlCh := make(chan sessionStreamUpdate, 1)

	err := s.runSubscribeLoop(ctx, info, bs, map[eventbus.Subject]struct{}{}, stream, nil, ctrlCh, nil)
	assert.NoError(t, err)
}

func TestRunSubscribeLoopPropagatesNextError(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}

	// Use a custom fake whose Next returns a non-EOF non-canceled error.
	bs := &errorNextStream{err: errors.New("js bust")}
	ctx := context.Background()
	stream := &fakeSubscribeStream{ctx: ctx}
	ctrlCh := make(chan sessionStreamUpdate, 1)

	err := s.runSubscribeLoop(ctx, info, bs, map[eventbus.Subject]struct{}{}, stream, nil, ctrlCh, nil)
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SUBSCRIPTION_ERROR", o.Code())
}

// errorNextStream is a SessionStream whose Next returns a fixed error.
type errorNextStream struct {
	err error
}

func (e *errorNextStream) Next(_ context.Context) (eventbus.Delivery, error) { return nil, e.err }
func (e *errorNextStream) SetFilters(_ context.Context, _ []eventbus.Subject) error {
	return nil
}
func (e *errorNextStream) Close() error { return nil }

var _ eventbus.SessionStream = (*errorNextStream)(nil)

// ulidEntropy is a deterministic io.Reader returning the same byte repeatedly.
// Used in tests to produce distinct but reproducible ULIDs without relying on
// time-based entropy (which collides at sub-ms resolution).
type ulidEntropy struct{ seed byte }

func (u ulidEntropy) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = u.seed
	}
	return len(p), nil
}

func TestDispatchDeliveryStampsMetadataOnlyWhenDeliveryReportsTrue(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	stream := &fakeSubscribeStream{ctx: context.Background()}
	charID := core.NewULID().String()

	d := makeDelivery(t, "say", charID)
	d.metadataOnly = true

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil, nil)
	require.NoError(t, err)
	require.Len(t, stream.sent, 1)
	assert.True(t, stream.sent[0].GetEvent().GetMetadataOnly(),
		"EventFrame.metadata_only must be true when delivery.MetadataOnly() returns true")
}

func TestDispatchDeliveryDoesNotStampMetadataOnlyWhenFalse(t *testing.T) {
	t.Parallel()
	info := &session.Info{ID: "s1"}
	s := &CoreServer{sessionStore: newTestSessionStore(t, map[string]*session.Info{"s1": info})}
	stream := &fakeSubscribeStream{ctx: context.Background()}
	charID := core.NewULID().String()

	d := makeDelivery(t, "say", charID)
	// metadataOnly defaults to false

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil, nil)
	require.NoError(t, err)
	require.Len(t, stream.sent, 1)
	assert.False(t, stream.sent[0].GetEvent().GetMetadataOnly(),
		"EventFrame.metadata_only must be false when delivery.MetadataOnly() returns false")
}

// --- Tests: scene_activity badge downgrade (INV-SCENE-62) ---------------

// makeSceneDelivery builds a fakeDelivery carrying an event on the given
// scene IC subject (events.main.scene.<sceneID>.ic).
func makeSceneDelivery(t *testing.T, evType, sceneID string) *fakeDelivery {
	t.Helper()
	id := core.NewULID()
	return &fakeDelivery{
		ev: eventbus.Event{
			ID:        id,
			Subject:   eventbus.Subject("events.main.scene." + sceneID + ".ic"),
			Type:      eventbus.Type(evType),
			Timestamp: time.Now(),
			Payload:   []byte("{}"),
		},
	}
}

// TestDispatchDeliveryDowngradesSceneEventForNonFocusedMemberConnection
// verifies that a scene event delivered to a member connection NOT focused on
// that scene becomes a CONTROL_SIGNAL_SCENE_ACTIVITY badge frame — no
// EventFrame is sent, the delivery is acked, and SceneId is set.
func TestDispatchDeliveryDowngradesSceneEventForNonFocusedMemberConnection(t *testing.T) {
	t.Parallel()
	sceneID := ulid.Make()
	connID := ulid.Make()
	info := &session.Info{
		ID: "s1",
		FocusMemberships: []session.FocusMembership{
			{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: time.Now().Add(-time.Hour)},
		},
	}
	store := newTestSessionStore(t, map[string]*session.Info{"s1": info})
	// Add connection with NO focus (nil FocusKey = grid / scene-grid, not focused on any scene).
	require.NoError(t, store.AddConnection(context.Background(), &session.Connection{
		ID:         connID,
		SessionID:  "s1",
		ClientType: "terminal",
		Streams:    []string{},
		FocusKey:   nil, // not focused on any scene
	}))

	s := &CoreServer{sessionStore: store}
	stream := &fakeSubscribeStream{ctx: context.Background()}
	d := makeSceneDelivery(t, "core-scenes:scene_pose", sceneID.String())

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil, &connID)
	require.NoError(t, err)
	assert.Equal(t, 1, d.acks(), "badge downgrade must ack the delivery")
	assert.Equal(t, 0, d.nacks())
	require.Len(t, stream.sent, 1, "must send exactly one control frame (the badge)")
	ctrl := stream.sent[0].GetControl()
	require.NotNil(t, ctrl, "frame must be a control frame, not an event frame")
	assert.Equal(t, corev1.ControlSignal_CONTROL_SIGNAL_SCENE_ACTIVITY, ctrl.GetSignal())
	assert.Equal(t, sceneID.String(), ctrl.GetSceneId())
}

// TestDispatchDeliveryForwardsFocusedSceneEventNormally verifies that when the
// connection IS focused on the scene from which the event arrives, the event
// frame is forwarded normally (no downgrade).
func TestDispatchDeliveryForwardsFocusedSceneEventNormally(t *testing.T) {
	t.Parallel()
	sceneID := ulid.Make()
	connID := ulid.Make()
	info := &session.Info{
		ID: "s1",
		FocusMemberships: []session.FocusMembership{
			{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: time.Now().Add(-time.Hour)},
		},
	}
	store := newTestSessionStore(t, map[string]*session.Info{"s1": info})
	fk := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}
	require.NoError(t, store.AddConnection(context.Background(), &session.Connection{
		ID:         connID,
		SessionID:  "s1",
		ClientType: "terminal",
		Streams:    []string{},
		FocusKey:   &fk, // focused on this scene
	}))

	s := &CoreServer{sessionStore: store}
	stream := &fakeSubscribeStream{ctx: context.Background()}
	d := makeSceneDelivery(t, "core-scenes:scene_pose", sceneID.String())

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil, &connID)
	require.NoError(t, err)
	require.Len(t, stream.sent, 1, "focused connection must receive the EventFrame")
	assert.NotNil(t, stream.sent[0].GetEvent(), "frame must be an EventFrame, not a control frame")
}
