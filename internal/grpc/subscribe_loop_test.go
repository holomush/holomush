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
	"github.com/holomush/holomush/internal/world"
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
	ev       eventbus.Event
	ackErr   error
	nackErr  error
	ackCnt   int
	nackCnt  int
	inProgCnt int
	mu       sync.Mutex
}

func (d *fakeDelivery) Event() eventbus.Event { return d.ev }

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
	s := &CoreServer{}
	info := &session.Info{ID: "s1"}
	stream := &fakeSubscribeStream{ctx: context.Background()}
	charID := core.NewULID().String()
	d := makeDelivery(t, "say", charID)

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, d.acks())
	assert.Equal(t, 0, d.nacks())
	require.Len(t, stream.sent, 1)
	assert.Equal(t, "say", stream.sent[0].GetEvent().GetType())
}

func TestDispatchDeliveryNacksOnSendError(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	info := &session.Info{ID: "s1"}
	stream := &fakeSubscribeStream{ctx: context.Background(), err: errors.New("send boom")}
	charID := core.NewULID().String()
	d := makeDelivery(t, "say", charID)

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil)
	require.Error(t, err)
	assert.Equal(t, 0, d.acks(), "no ack on send failure — JS must redeliver")
	assert.Equal(t, 1, d.nacks())
}

func TestDispatchDeliveryAckFailureLogsButReturnsNil(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	info := &session.Info{ID: "s1"}
	stream := &fakeSubscribeStream{ctx: context.Background()}
	charID := core.NewULID().String()
	d := makeDelivery(t, "say", charID)
	d.ackErr = errors.New("ack boom")

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil)
	require.NoError(t, err, "ack failure must not propagate — JS will redeliver")
	assert.Equal(t, 1, d.acks())
}

func TestDispatchDeliveryTerminatesOnMatchingSessionEnded(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	info := &session.Info{ID: "s1"}
	stream := &fakeSubscribeStream{ctx: context.Background()}
	charID := core.NewULID().String()

	d := makeDelivery(t, string(core.EventTypeSessionEnded), charID)
	payload, _ := json.Marshal(core.SessionEndedPayload{
		SessionID: "s1",
		Cause:     core.SessionEndedCauseQuit,
		Reason:    "bye",
	})
	d.ev.Payload = payload

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil)
	assert.ErrorIs(t, err, errStreamTerminated)
	// One event frame, plus one STREAM_CLOSED control frame.
	require.Len(t, stream.sent, 2)
	assert.Equal(t, corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED, stream.sent[1].GetControl().GetSignal())
	assert.Equal(t, "bye", stream.sent[1].GetControl().GetMessage())
}

func TestDispatchDeliveryIgnoresNonMatchingSessionEnded(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	info := &session.Info{ID: "s1"}
	stream := &fakeSubscribeStream{ctx: context.Background()}
	charID := core.NewULID().String()

	d := makeDelivery(t, string(core.EventTypeSessionEnded), charID)
	payload, _ := json.Marshal(core.SessionEndedPayload{
		SessionID: "SOME_OTHER_SESSION",
		Cause:     core.SessionEndedCauseQuit,
	})
	d.ev.Payload = payload

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil)
	require.NoError(t, err)
	// Forwarded verbatim, no STREAM_CLOSED.
	require.Len(t, stream.sent, 1)
	assert.Nil(t, stream.sent[0].GetControl())
}

func TestDispatchDeliverySessionEndedBadPayloadLogsAndSurvives(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	info := &session.Info{ID: "s1"}
	stream := &fakeSubscribeStream{ctx: context.Background()}
	charID := core.NewULID().String()

	d := makeDelivery(t, string(core.EventTypeSessionEnded), charID)
	d.ev.Payload = []byte("not-json")

	err := s.dispatchDelivery(context.Background(), info, d, stream, nil)
	require.NoError(t, err, "unmarshal failure must not error the stream")
	require.Len(t, stream.sent, 1)
}

// --- Tests: applyFilterCtrl -------------------------------------------

func TestApplyFilterCtrlRejectsLocationStreams(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	info := &session.Info{ID: "s1"}
	bs := newFakeSessionStream()
	filterSet := map[eventbus.Subject]struct{}{}

	ctrl := sessionStreamUpdate{stream: world.StreamPrefixLocation + "01HYXYZ0C0000000000000000C", add: true}
	err := s.applyFilterCtrl(context.Background(), info, bs, filterSet, ctrl)
	require.NoError(t, err)
	assert.Empty(t, filterSet, "location filters must be owned by locationFollower")
	assert.Empty(t, bs.setFilters, "SetFilters must not be called for rejected stream")
}

func TestApplyFilterCtrlAddsAndCallsSetFilters(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	info := &session.Info{ID: "s1"}
	bs := newFakeSessionStream()
	filterSet := map[eventbus.Subject]struct{}{}

	charID := core.NewULID().String()
	ctrl := sessionStreamUpdate{stream: "character:" + charID, add: true}
	err := s.applyFilterCtrl(context.Background(), info, bs, filterSet, ctrl)
	require.NoError(t, err)
	assert.Len(t, filterSet, 1)
	require.Len(t, bs.setFilters, 1)
}

func TestApplyFilterCtrlAddIdempotentWhenExists(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	info := &session.Info{ID: "s1"}
	bs := newFakeSessionStream()
	charID := core.NewULID().String()
	sub := eventbus.Subject("events.main.character." + charID)
	filterSet := map[eventbus.Subject]struct{}{sub: {}}

	ctrl := sessionStreamUpdate{stream: "character:" + charID, add: true}
	err := s.applyFilterCtrl(context.Background(), info, bs, filterSet, ctrl)
	require.NoError(t, err)
	assert.Empty(t, bs.setFilters, "no SetFilters call when already present")
}

func TestApplyFilterCtrlRemovesAndCallsSetFilters(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	info := &session.Info{ID: "s1"}
	bs := newFakeSessionStream()
	charID := core.NewULID().String()
	sub := eventbus.Subject("events.main.character." + charID)
	filterSet := map[eventbus.Subject]struct{}{sub: {}}

	ctrl := sessionStreamUpdate{stream: "character:" + charID, add: false}
	err := s.applyFilterCtrl(context.Background(), info, bs, filterSet, ctrl)
	require.NoError(t, err)
	assert.Empty(t, filterSet)
	require.Len(t, bs.setFilters, 1)
}

func TestApplyFilterCtrlRemoveIdempotentWhenMissing(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	info := &session.Info{ID: "s1"}
	bs := newFakeSessionStream()
	filterSet := map[eventbus.Subject]struct{}{}

	charID := core.NewULID().String()
	ctrl := sessionStreamUpdate{stream: "character:" + charID, add: false}
	err := s.applyFilterCtrl(context.Background(), info, bs, filterSet, ctrl)
	require.NoError(t, err)
	assert.Empty(t, bs.setFilters)
}

func TestApplyFilterCtrlRejectsInvalidStream(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	info := &session.Info{ID: "s1"}
	bs := newFakeSessionStream()
	filterSet := map[eventbus.Subject]struct{}{}

	ctrl := sessionStreamUpdate{stream: "character::bad", add: true}
	err := s.applyFilterCtrl(context.Background(), info, bs, filterSet, ctrl)
	require.Error(t, err)
}

func TestApplyFilterCtrlPropagatesSetFiltersError(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	info := &session.Info{ID: "s1"}
	bs := newFakeSessionStream()
	bs.setFiltersErr = errors.New("js bust")
	filterSet := map[eventbus.Subject]struct{}{}

	charID := core.NewULID().String()
	ctrl := sessionStreamUpdate{stream: "character:" + charID, add: true}
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
	addStream := "character:" + charA.String()
	removeStream := "character:" + charB.String()

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
	s := &CoreServer{}
	info := &session.Info{ID: "s1"}
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
		errCh <- s.runSubscribeLoop(ctx, info, bs, filterSet, stream, nil, ctrlCh)
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
	s := &CoreServer{}
	info := &session.Info{ID: "s1"}
	bs := newFakeSessionStream()
	charID := core.NewULID().String()

	d1 := makeDelivery(t, "say", charID)
	bs.push(d1)

	ctx := context.Background()
	stream := &fakeSubscribeStream{ctx: ctx, err: errors.New("send fail")}
	ctrlCh := make(chan sessionStreamUpdate, 1)
	filterSet := map[eventbus.Subject]struct{}{}

	err := s.runSubscribeLoop(ctx, info, bs, filterSet, stream, nil, ctrlCh)
	require.Error(t, err)
	assert.Equal(t, 0, d1.acks(), "must not ack on send failure")
	assert.Equal(t, 1, d1.nacks())
}

func TestRunSubscribeLoopReturnsNilOnSessionEnded(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	info := &session.Info{ID: "s1"}
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

	err := s.runSubscribeLoop(ctx, info, bs, filterSet, stream, nil, ctrlCh)
	assert.NoError(t, err, "errStreamTerminated collapses to nil at caller boundary")
}

func TestRunSubscribeLoopAppliesFilterCtrl(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	info := &session.Info{ID: "s1"}
	bs := newFakeSessionStream()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := &fakeSubscribeStream{ctx: ctx}
	ctrlCh := make(chan sessionStreamUpdate, 2)
	filterSet := map[eventbus.Subject]struct{}{}

	charID := core.NewULID().String()
	ctrlCh <- sessionStreamUpdate{stream: "character:" + charID, add: true}
	// Location stream: rejected path (logged warning).
	ctrlCh <- sessionStreamUpdate{stream: world.StreamPrefixLocation + "01HYXYZ0C0000000000000000C", add: true}

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.runSubscribeLoop(ctx, info, bs, filterSet, stream, nil, ctrlCh)
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
	s := &CoreServer{}
	info := &session.Info{ID: "s1"}
	bs := newFakeSessionStream()
	ctx := context.Background()
	stream := &fakeSubscribeStream{ctx: ctx}
	ctrlCh := make(chan sessionStreamUpdate)
	close(ctrlCh)

	err := s.runSubscribeLoop(ctx, info, bs, map[eventbus.Subject]struct{}{}, stream, nil, ctrlCh)
	assert.NoError(t, err)
}

func TestRunSubscribeLoopReturnsNilOnDeliveriesClose(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	info := &session.Info{ID: "s1"}
	bs := newFakeSessionStream()
	// Close immediately → Next returns io.EOF → loop returns nil.
	_ = bs.Close()
	ctx := context.Background()
	stream := &fakeSubscribeStream{ctx: ctx}
	ctrlCh := make(chan sessionStreamUpdate, 1)

	err := s.runSubscribeLoop(ctx, info, bs, map[eventbus.Subject]struct{}{}, stream, nil, ctrlCh)
	assert.NoError(t, err)
}

func TestRunSubscribeLoopPropagatesNextError(t *testing.T) {
	t.Parallel()
	s := &CoreServer{}
	info := &session.Info{ID: "s1"}

	// Use a custom fake whose Next returns a non-EOF non-canceled error.
	bs := &errorNextStream{err: errors.New("js bust")}
	ctx := context.Background()
	stream := &fakeSubscribeStream{ctx: ctx}
	ctrlCh := make(chan sessionStreamUpdate, 1)

	err := s.runSubscribeLoop(ctx, info, bs, map[eventbus.Subject]struct{}{}, stream, nil, ctrlCh)
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
