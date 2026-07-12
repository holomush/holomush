// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package outbox_test

import (
	"context"
	"sync"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/world/outbox"
	"github.com/holomush/holomush/internal/world/wmodel"
)

// --- in-memory fakes (no DB / no NATS): drive the relay + SkipService seams ---

type memRow struct {
	env        *wmodel.Envelope
	published  bool
	skipMarker ulid.ULID
	hasMarker  bool
}

type memStore struct {
	mu              sync.Mutex
	rows            []*memRow
	gen             int64
	acquires        int
	forceStaleMarks int
	failResolveOnce bool
}

func staleErr() error {
	return oops.Code(outbox.CodeStaleLease).Wrap(outbox.ErrStaleLease)
}

func (m *memStore) AcquireLease(_ context.Context, _ string) (outbox.Lease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gen++
	m.acquires++
	return &memLease{store: m, gen: m.gen}, nil
}

type memLease struct {
	store *memStore
	gen   int64
}

func (l *memLease) Generation() int64 { return l.gen }

func (l *memLease) NextUnpublished(_ context.Context) (*wmodel.Envelope, error) {
	l.store.mu.Lock()
	defer l.store.mu.Unlock()
	for _, r := range l.store.rows {
		if !r.published {
			cp := *r.env
			return &cp, nil
		}
	}
	return nil, nil
}

func (l *memLease) MarkPublished(_ context.Context, eventID ulid.ULID, generation int64) error {
	l.store.mu.Lock()
	defer l.store.mu.Unlock()
	if l.store.forceStaleMarks > 0 {
		l.store.forceStaleMarks--
		return staleErr()
	}
	if generation != l.store.gen {
		return staleErr()
	}
	for _, r := range l.store.rows {
		if r.env.EventID == eventID {
			r.published = true
			return nil
		}
	}
	return nil
}

func (l *memLease) lowestUnpublishedAt(position int64) *memRow {
	for _, r := range l.store.rows {
		if !r.published && r.env.FeedPosition == position {
			return r
		}
	}
	return nil
}

func (l *memLease) Prune(_ context.Context) (int64, error) {
	l.store.mu.Lock()
	defer l.store.mu.Unlock()
	kept := l.store.rows[:0]
	var deleted int64
	for _, r := range l.store.rows {
		if r.published {
			deleted++
			continue
		}
		kept = append(kept, r)
	}
	l.store.rows = kept
	return deleted, nil
}

func (l *memLease) MarkSkipResolved(_ context.Context, position int64) error {
	l.store.mu.Lock()
	defer l.store.mu.Unlock()
	if l.store.failResolveOnce {
		l.store.failResolveOnce = false
		return oops.Code("TEST_RESOLVE_FAILED").Errorf("simulated crash before resolve")
	}
	if r := l.lowestUnpublishedAt(position); r != nil {
		r.published = true
	}
	return nil
}

func (l *memLease) CurrentEpoch(_ context.Context) (int64, error) {
	l.store.mu.Lock()
	defer l.store.mu.Unlock()
	if len(l.store.rows) > 0 {
		return l.store.rows[0].env.Epoch, nil
	}
	return 1, nil
}

func (l *memLease) PersistSkipMarkerID(_ context.Context, position int64, eventID ulid.ULID) error {
	l.store.mu.Lock()
	defer l.store.mu.Unlock()
	if r := l.lowestUnpublishedAt(position); r != nil {
		r.skipMarker = eventID
		r.hasMarker = true
	}
	return nil
}

func (l *memLease) SkipMarkerID(_ context.Context, position int64) (ulid.ULID, bool, error) {
	l.store.mu.Lock()
	defer l.store.mu.Unlock()
	if r := l.lowestUnpublishedAt(position); r != nil && r.hasMarker {
		return r.skipMarker, true, nil
	}
	return ulid.ULID{}, false, nil
}

func (l *memLease) Release(_ context.Context) error { return nil }

type fakePublisher struct {
	mu     sync.Mutex
	events []eventbus.Event
	failFn func(ev eventbus.Event) error
}

func (p *fakePublisher) Publish(_ context.Context, ev eventbus.Event) error {
	if p.failFn != nil {
		if err := p.failFn(ev); err != nil {
			return err
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, ev)
	return nil
}

func (p *fakePublisher) snapshot() []eventbus.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]eventbus.Event, len(p.events))
	copy(out, p.events)
	return out
}

func testEnv(gameID string, epoch, pos int64, kind string) *wmodel.Envelope {
	return &wmodel.Envelope{
		EventID:       ulid.Make(),
		GameID:        gameID,
		Kind:          kind,
		SchemaVersion: 1,
		Actor:         "system",
		AggregateType: wmodel.AggregateLocation,
		AggregateID:   ulid.Make(),
		Epoch:         epoch,
		FeedPosition:  pos,
		Payload:       []byte(`{"name":"Atrium"}`),
	}
}

func newRelay(store outbox.OutboxStore, pub eventbus.Publisher, game string) *outbox.Relay {
	return outbox.NewRelay(outbox.RelayConfig{Store: store, Publisher: pub, GameID: game})
}

// TestRelayPublishesInPositionOrderWithDedupKey proves ordered publish and that
// each publish carries Nats-Msg-Id = the envelope's event ULID (Event.ID).
func TestRelayPublishesInPositionOrderWithDedupKey(t *testing.T) {
	game := ulid.Make().String()
	rows := []*memRow{
		{env: testEnv(game, 1, 1, "location_updated")},
		{env: testEnv(game, 1, 2, "location_updated")},
		{env: testEnv(game, 1, 3, "location_updated")},
	}
	store := &memStore{rows: rows}
	pub := &fakePublisher{}
	relay := newRelay(store, pub, game)

	published, err := relay.Drain(context.Background())
	require.NoError(t, err)
	require.Equal(t, 3, published)

	got := pub.snapshot()
	require.Len(t, got, 3)
	for i, ev := range got {
		assert.Equal(t, rows[i].env.EventID, ev.ID, "publish %d carries the envelope's event ULID (Nats-Msg-Id)", i)
	}
	halted, _ := relay.Halted()
	assert.False(t, halted)
}

// TestRelayHaltsOnPoisonEnvelopeAtPosition proves a permanently-unpublishable row
// HALTS the relay at its position; rows after it are NOT published, and the
// halt-position is exposed.
func TestRelayHaltsOnPoisonEnvelopeAtPosition(t *testing.T) {
	game := ulid.Make().String()
	// A row with an invalid kind can never build a wire event → poison.
	poison := testEnv(game, 1, 2, "BAD KIND WITH SPACES")
	rows := []*memRow{
		{env: testEnv(game, 1, 1, "location_updated")},
		{env: poison},
		{env: testEnv(game, 1, 3, "location_updated")},
	}
	store := &memStore{rows: rows}
	pub := &fakePublisher{}
	relay := newRelay(store, pub, game)

	_, err := relay.Drain(context.Background())
	require.NoError(t, err)

	halted, pos := relay.Halted()
	require.True(t, halted, "relay must halt on poison")
	assert.Equal(t, int64(2), pos, "halt position is the poison row's feed_position")
	// Only position 1 published; the poison and everything after are withheld.
	require.Len(t, pub.snapshot(), 1)
}

// TestRelayResumesAfterTransientOutage proves a transient broker outage does NOT
// halt the relay; it resumes in order on the next drain.
func TestRelayResumesAfterTransientOutage(t *testing.T) {
	game := ulid.Make().String()
	rows := []*memRow{{env: testEnv(game, 1, 1, "location_updated")}}
	store := &memStore{rows: rows}
	var attempts int
	pub := &fakePublisher{failFn: func(eventbus.Event) error {
		attempts++
		if attempts <= 3 {
			return oops.Code("EVENTBUS_PUBLISH_FAILED").Errorf("broker down")
		}
		return nil
	}}
	relay := newRelay(store, pub, game)

	published, err := relay.Drain(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, published, "transient outage publishes nothing this pass")
	halted, _ := relay.Halted()
	assert.False(t, halted, "a transient outage MUST NOT halt")

	published, err = relay.Drain(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, published, "relay resumes and publishes after the outage clears")
}

// TestRelayReacquiresLeaseWithNewGenerationOnStaleAck is the drop→re-acquire
// lifecycle seam: a stale DB ack (connection loss) makes the relay drop the lease
// and re-AcquireLease with a NEW generation before publishing again (round-6
// R6-5). At-least-once: the wire message may already be out; the re-mark is
// absorbed by dedup.
func TestRelayReacquiresLeaseWithNewGenerationOnStaleAck(t *testing.T) {
	game := ulid.Make().String()
	rows := []*memRow{{env: testEnv(game, 1, 1, "location_updated")}}
	store := &memStore{rows: rows, forceStaleMarks: 1}
	pub := &fakePublisher{}
	relay := newRelay(store, pub, game)

	published, err := relay.Drain(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, published)
	assert.GreaterOrEqual(t, store.acquires, 2, "a stale ack forces a lease re-acquire (new generation)")
	require.True(t, rows[0].published, "the row is marked published under the new generation")
}

// TestSkipServicePublishesSamePositionMarkerThenResolves proves the SkipService
// publishes an operator marker at the poison row's OWN feed_position, then (after
// PubAck) resolves the row so the relay resumes — no wire gap.
func TestSkipServicePublishesSamePositionMarkerThenResolves(t *testing.T) {
	game := ulid.Make().String()
	poison := testEnv(game, 1, 2, "BAD KIND WITH SPACES")
	rows := []*memRow{
		{env: testEnv(game, 1, 1, "location_updated")},
		{env: poison},
		{env: testEnv(game, 1, 3, "location_updated")},
	}
	store := &memStore{rows: rows}
	pub := &fakePublisher{}
	relay := newRelay(store, pub, game)

	_, err := relay.Drain(context.Background())
	require.NoError(t, err)
	halted, pos := relay.Halted()
	require.True(t, halted)
	require.Equal(t, int64(2), pos)

	skip := outbox.NewSkipService(store, pub, game, nil)
	require.NoError(t, skip.Skip(context.Background(), 2))

	// The marker published at position 2 with SkipMarkerKind.
	var markerCount int
	for _, ev := range pub.snapshot() {
		if string(ev.Type) == outbox.SkipMarkerKind {
			markerCount++
			env, derr := outbox.UnmarshalEnvelope(ev.Payload)
			require.NoError(t, derr)
			assert.Equal(t, int64(2), env.FeedPosition, "marker fills the poison position")
		}
	}
	assert.Equal(t, 1, markerCount, "exactly one skip marker published")

	// The relay resumes: position 3 now publishes.
	published, err := relay.Drain(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, published, "relay resumes past the resolved poison row")
	halted, _ = relay.Halted()
	assert.False(t, halted, "halt cleared after skip")
}

// TestSkipRetryReusesStableMarkerID proves a crash after PubAck but before
// MarkSkipResolved retries the skip with the SAME Nats-Msg-Id (round-4 A1), so
// JetStream dedup collapses the two attempts to exactly one event at the position.
func TestSkipRetryReusesStableMarkerID(t *testing.T) {
	game := ulid.Make().String()
	poison := testEnv(game, 1, 1, "BAD KIND WITH SPACES")
	rows := []*memRow{{env: poison}}
	store := &memStore{rows: rows, failResolveOnce: true}
	pub := &fakePublisher{}
	skip := outbox.NewSkipService(store, pub, game, nil)

	// First attempt: marker PubAcks, then MarkSkipResolved fails (crash).
	err := skip.Skip(context.Background(), 1)
	require.Error(t, err, "first attempt fails at resolve (simulated crash)")

	// Second attempt: reuses the persisted stable marker id, resolves.
	require.NoError(t, skip.Skip(context.Background(), 1))

	events := pub.snapshot()
	require.Len(t, events, 2, "two publish attempts (retry republishes)")
	assert.Equal(t, events[0].ID, events[1].ID,
		"both attempts carry the SAME stable Nats-Msg-Id — JetStream dedup collapses them to one event")
}

// TestWireRoundTripPreservesGameID proves the whole envelope survives outbox row
// → decoded envelope → qualified subject, with game_id preserved (round-9 R6-5).
func TestWireRoundTripPreservesGameID(t *testing.T) {
	game := ulid.Make().String()
	env := testEnv(game, 7, 42, "location_updated")

	ev, err := outbox.EnvelopeToEvent(*env)
	require.NoError(t, err)
	assert.Contains(t, string(ev.Subject), game, "qualified subject carries the game id")
	assert.Equal(t, env.EventID, ev.ID, "Event.ID = event ULID (drives Nats-Msg-Id)")

	decoded, err := outbox.UnmarshalEnvelope(ev.Payload)
	require.NoError(t, err)
	assert.Equal(t, game, decoded.GameID)
	assert.Equal(t, int64(7), decoded.Epoch)
	assert.Equal(t, int64(42), decoded.FeedPosition)
	assert.Equal(t, env.EventID, decoded.EventID)
}
