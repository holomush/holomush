// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package charactivity

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/core"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakeEntry is a jetstream.KeyValueEntry carrying only what the flusher reads.
type fakeEntry struct {
	key   string
	value []byte
	rev   uint64
}

func (e fakeEntry) Bucket() string                  { return BucketName }
func (e fakeEntry) Key() string                     { return e.key }
func (e fakeEntry) Value() []byte                   { return e.value }
func (e fakeEntry) Revision() uint64                { return e.rev }
func (e fakeEntry) Created() time.Time              { return time.Time{} }
func (e fakeEntry) Delta() uint64                   { return 0 }
func (e fakeEntry) Operation() jetstream.KeyValueOp { return jetstream.KeyValuePut }

// fakeLister replays a fixed key list, exactly as jetstream's KeyLister does —
// including the duplicate keys its own doc warns about under churn.
type fakeLister struct {
	ch      chan string
	stopped bool
}

func (l *fakeLister) Keys() <-chan string { return l.ch }
func (l *fakeLister) Stop() error         { l.stopped = true; return nil }

// fakeKV models the KV bucket, INCLUDING the revision semantics of
// jetstream.LastRevision: DeleteRevision succeeds only while the stored
// revision still matches the one the caller read.
type fakeKV struct {
	mu      sync.Mutex
	entries map[string]fakeEntry
	nextRev uint64

	// listOrder overrides the key list ListKeys reports (used to replay the
	// documented duplicate-key case).
	listOrder []string
	// beforeDelete runs inside DeleteRevision before the revision comparison —
	// the seam that interleaves a concurrent listener Put mid-flush.
	beforeDelete func()
	// beforeGet runs inside Get before the lookup — the seam that interleaves
	// a concurrent write between the listener's read and its guarded write.
	beforeGet func()

	listErr error
	putErr  error

	lastLister *fakeLister
}

func newFakeKV() *fakeKV {
	return &fakeKV{entries: make(map[string]fakeEntry)}
}

func (k *fakeKV) Put(_ context.Context, key string, value []byte) (uint64, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.putErr != nil {
		return 0, k.putErr
	}
	k.nextRev++
	k.entries[key] = fakeEntry{key: key, value: value, rev: k.nextRev}
	return k.nextRev, nil
}

func (k *fakeKV) Get(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
	// Same capture-under-lock/invoke-after-unlock shape as DeleteRevision: the
	// hook re-enters this fake, so the mutex must not be held across it.
	k.mu.Lock()
	hook := k.beforeGet
	k.beforeGet = nil
	k.mu.Unlock()
	if hook != nil {
		hook()
	}

	k.mu.Lock()
	defer k.mu.Unlock()
	e, ok := k.entries[key]
	if !ok {
		return nil, jetstream.ErrKeyNotFound
	}
	return e, nil
}

// Create models jetstream's create-only write: it refuses an existing key with
// ErrKeyExists rather than overwriting it.
func (k *fakeKV) Create(_ context.Context, key string, value []byte) (uint64, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.putErr != nil {
		return 0, k.putErr
	}
	if _, ok := k.entries[key]; ok {
		return 0, jetstream.ErrKeyExists
	}
	k.nextRev++
	k.entries[key] = fakeEntry{key: key, value: value, rev: k.nextRev}
	return k.nextRev, nil
}

// Update models jetstream's revision-conditional write: the server refuses it
// when the stored revision is no longer the one the caller read.
func (k *fakeKV) Update(_ context.Context, key string, value []byte, revision uint64) (uint64, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.putErr != nil {
		return 0, k.putErr
	}
	e, ok := k.entries[key]
	if !ok {
		return 0, jetstream.ErrKeyNotFound
	}
	if e.rev != revision {
		return 0, errors.New("wrong last sequence")
	}
	k.nextRev++
	k.entries[key] = fakeEntry{key: key, value: value, rev: k.nextRev}
	return k.nextRev, nil
}

func (k *fakeKV) Delete(_ context.Context, key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.entries, key)
	return nil
}

func (k *fakeKV) DeleteRevision(_ context.Context, key string, revision uint64) error {
	// Captured UNDER the lock and invoked after releasing it: the hook itself
	// re-enters this fake (that is its whole purpose), so holding the mutex
	// across the call would deadlock. Reading the field unguarded is what -race
	// flags the moment a spec drives two drains concurrently.
	k.mu.Lock()
	hook := k.beforeDelete
	k.beforeDelete = nil
	k.mu.Unlock()
	if hook != nil {
		hook()
	}

	k.mu.Lock()
	defer k.mu.Unlock()
	e, ok := k.entries[key]
	if !ok {
		return nil
	}
	if e.rev != revision {
		// What the server answers when Nats-Expected-Last-Subject-Sequence
		// does not match: the delete marker is refused.
		return errors.New("wrong last sequence")
	}
	delete(k.entries, key)
	return nil
}

func (k *fakeKV) ListKeys(context.Context) (jetstream.KeyLister, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.listErr != nil {
		return nil, k.listErr
	}
	keys := k.listOrder
	if keys == nil {
		for key := range k.entries {
			keys = append(keys, key)
		}
	}
	ch := make(chan string, len(keys))
	for _, key := range keys {
		ch <- key
	}
	close(ch)
	l := &fakeLister{ch: ch}
	k.lastLister = l
	return l, nil
}

func (k *fakeKV) snapshot() map[string]fakeEntry {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := make(map[string]fakeEntry, len(k.entries))
	for key, e := range k.entries {
		out[key] = e
	}
	return out
}

// recordingWriter captures every ActivityWriter call.
type recordingWriter struct {
	mu    sync.Mutex
	calls []writeCall
	err   error
}

type writeCall struct {
	id    ulid.ULID
	nanos int64
}

func (w *recordingWriter) write(_ context.Context, id ulid.ULID, nanos int64) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, writeCall{id: id, nanos: nanos})
	return w.err
}

func (w *recordingWriter) recorded() []writeCall {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]writeCall(nil), w.calls...)
}

// eventBytes marshals a wire event with the given actor and timestamp.
func eventBytes(t *testing.T, kind eventbusv1.ActorKind, actorID ulid.ULID, ts time.Time) []byte {
	t.Helper()
	actor := &eventbusv1.Actor{Kind: kind}
	if actorID != (ulid.ULID{}) {
		actor.Id = actorID.Bytes()
	}
	data, err := proto.Marshal(&eventbusv1.Event{
		Id:        core.NewULID().Bytes(),
		Subject:   "events.test.character.x",
		Type:      "say",
		Timestamp: timestamppb.New(ts),
		Actor:     actor,
	})
	require.NoError(t, err)
	return data
}

func newTestListener(kv activityKV) *listener {
	return &listener{cfg: Config{}.Defaults(), kv: kv}
}

func newTestFlusher(kv activityKV, w ActivityWriter) *flusher {
	return &flusher{cfg: Config{Writer: w}.Defaults(), kv: kv}
}

// ---------------------------------------------------------------------------
// Listener
// ---------------------------------------------------------------------------

func TestListenerBuffersOnlyCharacterActorEvents(t *testing.T) {
	ctx := context.Background()
	charID := core.NewULID()
	at := time.Unix(0, 1_700_000_000_000_000_000).UTC()

	tests := []struct {
		name   string
		kind   eventbusv1.ActorKind
		actor  ulid.ULID
		buffer bool
	}{
		{"a character actor is buffered", eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, charID, true},
		{"a system actor is ignored", eventbusv1.ActorKind_ACTOR_KIND_SYSTEM, core.NewULID(), false},
		{"a plugin actor is ignored", eventbusv1.ActorKind_ACTOR_KIND_PLUGIN, core.NewULID(), false},
		{"a player actor is ignored", eventbusv1.ActorKind_ACTOR_KIND_PLAYER, core.NewULID(), false},
		{"a character actor with no id is ignored", eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, ulid.ULID{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kv := newFakeKV()
			newTestListener(kv).record(ctx, eventBytes(t, tt.kind, tt.actor, at))

			got := kv.snapshot()
			if !tt.buffer {
				assert.Empty(t, got, "only character-actor events describe character activity")
				return
			}
			require.Len(t, got, 1)
			e, ok := got[tt.actor.String()]
			require.True(t, ok, "the key MUST be the bare actor ULID")
			assert.Equal(t, strconv.FormatInt(at.UnixNano(), 10), string(e.value),
				"the value MUST be the event timestamp as decimal epoch NANOSECONDS")
		})
	}
}

func TestListenerIgnoresAnUndecodableOrUntimestampedEvent(t *testing.T) {
	ctx := context.Background()

	t.Run("garbage bytes", func(t *testing.T) {
		kv := newFakeKV()
		newTestListener(kv).record(ctx, []byte("not a proto"))
		assert.Empty(t, kv.snapshot())
	})

	t.Run("no timestamp", func(t *testing.T) {
		kv := newFakeKV()
		data, err := proto.Marshal(&eventbusv1.Event{
			Actor: &eventbusv1.Actor{
				Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
				Id:   core.NewULID().Bytes(),
			},
		})
		require.NoError(t, err)
		newTestListener(kv).record(ctx, data)
		assert.Empty(t, kv.snapshot(), "a zero timestamp would regress nothing but buffers noise")
	})
}

func TestListenerAbsorbsAPutFailure(t *testing.T) {
	kv := newFakeKV()
	kv.putErr = errors.New("bucket unavailable")

	// A buffered-activity loss is operational degradation; a stalled consumer
	// is not. The listener MUST NOT panic or propagate.
	require.NotPanics(t, func() {
		newTestListener(kv).record(context.Background(),
			eventBytes(t, eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, core.NewULID(), time.Now()))
	})
}

// TestListenerNeverReplacesABufferedTimestampWithAnOlderOne is the buffer's own
// monotonic guard.
//
// Delivery is at-least-once and unordered, so a message whose AckWait expired
// after a later one was already handled comes back with an OLDER timestamp. An
// unconditional Put would store it at a fresh revision; the flusher would then
// write it, the database's own last_active_at < $2 predicate would absorb it as
// a no-op, and the newer value would be gone -- leaving the column behind by
// the delta until the character next acts.
func TestListenerNeverReplacesABufferedTimestampWithAnOlderOne(t *testing.T) {
	ctx := context.Background()
	charID := core.NewULID()
	newer := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	older := newer.Add(-time.Hour)

	kv := newFakeKV()
	l := newTestListener(kv)

	l.record(ctx, eventBytes(t, eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, charID, newer))
	l.record(ctx, eventBytes(t, eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, charID, older))

	e, ok := kv.snapshot()[charID.String()]
	require.True(t, ok)
	assert.Equal(t, strconv.FormatInt(newer.UnixNano(), 10), string(e.value),
		"an older redelivery MUST NOT clobber newer buffered activity")
}

func TestListenerAdvancesABufferedTimestampToANewerOne(t *testing.T) {
	ctx := context.Background()
	charID := core.NewULID()
	older := time.Unix(0, 1_700_000_000_000_000_000).UTC()
	newer := older.Add(time.Hour)

	kv := newFakeKV()
	l := newTestListener(kv)

	l.record(ctx, eventBytes(t, eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, charID, older))
	l.record(ctx, eventBytes(t, eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, charID, newer))

	e, ok := kv.snapshot()[charID.String()]
	require.True(t, ok)
	assert.Equal(t, strconv.FormatInt(newer.UnixNano(), 10), string(e.value),
		"newer activity MUST advance the buffered value")
}

// TestListenerLeavesABufferedValueThatChangedBetweenItsReadAndItsWrite pins the
// revision guard on the buffer write: a concurrent writer that already advanced
// the key wins, and losing that race is harmless because the winner is newer.
func TestListenerLeavesABufferedValueThatChangedBetweenItsReadAndItsWrite(t *testing.T) {
	ctx := context.Background()
	charID := core.NewULID()
	base := time.Unix(0, 1_700_000_000_000_000_000).UTC()

	kv := newFakeKV()
	l := newTestListener(kv)
	l.record(ctx, eventBytes(t, eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, charID, base))

	// A concurrent writer lands between this listener's Get and its Update.
	interposed := strconv.FormatInt(base.Add(2*time.Hour).UnixNano(), 10)
	kv.beforeGet = func() {
		_, err := kv.Put(ctx, charID.String(), []byte(interposed))
		require.NoError(t, err)
	}
	l.record(ctx, eventBytes(t, eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, charID, base.Add(time.Hour)))

	e, ok := kv.snapshot()[charID.String()]
	require.True(t, ok)
	assert.Equal(t, interposed, string(e.value),
		"the write is revision-conditional, so the concurrent writer's value survives")
}

func TestListenerCuresAnUnparsableBufferedValue(t *testing.T) {
	ctx := context.Background()
	charID := core.NewULID()
	at := time.Unix(0, 1_700_000_000_000_000_000).UTC()

	kv := newFakeKV()
	_, err := kv.Put(ctx, charID.String(), []byte("not a number"))
	require.NoError(t, err)

	newTestListener(kv).record(ctx, eventBytes(t, eventbusv1.ActorKind_ACTOR_KIND_CHARACTER, charID, at))

	e, ok := kv.snapshot()[charID.String()]
	require.True(t, ok)
	assert.Equal(t, strconv.FormatInt(at.UnixNano(), 10), string(e.value),
		"an unparsable buffered value is not newer than anything and MUST be cured")
}

// ---------------------------------------------------------------------------
// Flusher
// ---------------------------------------------------------------------------

func TestFlusherWritesEveryBufferedKeyAndDeletesItAtTheRevisionItRead(t *testing.T) {
	ctx := context.Background()
	kv := newFakeKV()
	a, b := core.NewULID(), core.NewULID()
	_, err := kv.Put(ctx, a.String(), []byte("1000"))
	require.NoError(t, err)
	_, err = kv.Put(ctx, b.String(), []byte("2000"))
	require.NoError(t, err)

	w := &recordingWriter{}
	newTestFlusher(kv, w.write).drain(ctx)

	assert.ElementsMatch(t, []writeCall{{id: a, nanos: 1000}, {id: b, nanos: 2000}}, w.recorded())
	assert.Empty(t, kv.snapshot(), "a successfully flushed key is deleted")
	assert.True(t, kv.lastLister.stopped, "the streaming lister MUST be stopped")
}

func TestFlusherLeavesAKeyWhoseWriteFailed(t *testing.T) {
	ctx := context.Background()
	kv := newFakeKV()
	id := core.NewULID()
	_, err := kv.Put(ctx, id.String(), []byte("1000"))
	require.NoError(t, err)

	w := &recordingWriter{err: errors.New("database unavailable")}
	newTestFlusher(kv, w.write).drain(ctx)

	assert.Contains(t, kv.snapshot(), id.String(),
		"an unwritten key MUST survive for the next tick")
}

// TestFlusherLeavesAKeyAConcurrentPutAdvancedMidFlush is R1: every delete is
// revision-conditional, so a listener Put landing between the flusher's Get and
// its Delete is NEVER destroyed.
func TestFlusherLeavesAKeyAConcurrentPutAdvancedMidFlush(t *testing.T) {
	ctx := context.Background()
	kv := newFakeKV()
	id := core.NewULID()
	_, err := kv.Put(ctx, id.String(), []byte("1000"))
	require.NoError(t, err)

	// The interleave: the listener buffers newer activity after the flusher has
	// read (1000, rev 1) but before it deletes.
	kv.beforeDelete = func() {
		_, putErr := kv.Put(ctx, id.String(), []byte("2000"))
		require.NoError(t, putErr)
	}

	w := &recordingWriter{}
	newTestFlusher(kv, w.write).drain(ctx)

	require.Equal(t, []writeCall{{id: id, nanos: 1000}}, w.recorded(),
		"the flush wrote the value it read; the monotonic writer absorbs it being older")
	survivor, ok := kv.snapshot()[id.String()]
	require.True(t, ok, "the revision guard MUST refuse the delete, leaving the key")
	assert.Equal(t, "2000", string(survivor.value), "the NEWER activity survives intact")
}

func TestFlusherAbsorbsDuplicateKeysFromListKeys(t *testing.T) {
	ctx := context.Background()
	kv := newFakeKV()
	id := core.NewULID()
	_, err := kv.Put(ctx, id.String(), []byte("1000"))
	require.NoError(t, err)
	// jetstream's own ListKeys doc warns duplicates may be reported under churn.
	kv.listOrder = []string{id.String(), id.String()}

	w := &recordingWriter{}
	newTestFlusher(kv, w.write).drain(ctx)

	require.Len(t, w.recorded(), 1,
		"the second sighting finds the key already gone; the monotonic writer absorbs it either way")
	assert.Empty(t, kv.snapshot())
}

func TestFlusherPurgesAKeyThatCanNeverBeACharacterID(t *testing.T) {
	ctx := context.Background()
	kv := newFakeKV()
	_, err := kv.Put(ctx, "not-a-ulid", []byte("1000"))
	require.NoError(t, err)

	w := &recordingWriter{}
	newTestFlusher(kv, w.write).drain(ctx)

	assert.Empty(t, w.recorded(), "there is no character to write")
	assert.Empty(t, kv.snapshot(),
		"a key NAME that is not a ULID can never become flushable, so it is purged unconditionally")
}

func TestFlusherDropsAMalformedValueAtItsReadRevision(t *testing.T) {
	ctx := context.Background()
	kv := newFakeKV()
	id := core.NewULID()
	_, err := kv.Put(ctx, id.String(), []byte("not-a-number"))
	require.NoError(t, err)

	w := &recordingWriter{}
	newTestFlusher(kv, w.write).drain(ctx)

	assert.Empty(t, w.recorded())
	assert.Empty(t, kv.snapshot(), "the garbage revision is gone, so the bucket stays bounded")
}

func TestFlusherSurvivesAListKeysFailure(t *testing.T) {
	kv := newFakeKV()
	kv.listErr = errors.New("bucket unavailable")

	require.NotPanics(t, func() {
		newTestFlusher(kv, (&recordingWriter{}).write).drain(context.Background())
	})
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// TestStopJoinsBothProducersBeforeTheFinalDrain is R2b: no listener and no
// ticker may be live while the shutdown drain runs.
//
// The join is exercised, not merely asserted. inFlight stands in for a
// listener handler that is still running when Stop is called — the real Stop
// flags the subscription and returns without joining the NATS dispatch
// goroutine, so its Put lands DURING shutdown. The final drain must observe
// that Put, which it can only do if Stop waited on Closed() first. Relying on
// Stop() alone, the drain races the Put and flushes the stale value.
func TestStopJoinsBothProducersBeforeTheFinalDrain(t *testing.T) {
	ctx := context.Background()
	kv := newFakeKV()
	id := core.NewULID()
	_, err := kv.Put(ctx, id.String(), []byte("1000"))
	require.NoError(t, err)

	w := &recordingWriter{}
	s := newFakeWiredSubsystem(t, kv, w.write)
	require.NoError(t, s.Prepare(ctx))
	require.NoError(t, s.Activate(ctx))

	cons, ok := s.cons.(*fakeConsumer)
	require.True(t, ok, "the fake wiring installs a fakeConsumer")
	cons.cc.mu.Lock()
	cons.cc.inFlight = func() {
		// A listener Put that was already in flight when Stop was called.
		time.Sleep(20 * time.Millisecond)
		_, putErr := kv.Put(context.Background(), id.String(), []byte("2000"))
		assert.NoError(t, putErr)
	}
	cons.cc.mu.Unlock()

	require.NoError(t, s.Stop(ctx))

	assert.Equal(t, []writeCall{{id: id, nanos: 2000}}, w.recorded(),
		"the final drain runs only after the in-flight listener handler has been joined, "+
			"so it flushes that handler's value and not the stale one")
	assert.Empty(t, kv.snapshot())
}

// TestStopAfterPrepareOnlyPerformsNoDatabaseWrite pins the activated/joined
// split. The orchestrator's rollback path calls Stop on every subsystem it has
// already PREPARED when some other subsystem fails, so a never-activated
// charactivity must not flush: a failed boot has no business issuing one
// UPDATE characters per buffered key.
func TestStopAfterPrepareOnlyPerformsNoDatabaseWrite(t *testing.T) {
	ctx := context.Background()
	kv := newFakeKV()
	id := core.NewULID()
	_, err := kv.Put(ctx, id.String(), []byte("1000"))
	require.NoError(t, err)

	w := &recordingWriter{}
	s := newFakeWiredSubsystem(t, kv, w.write)
	require.NoError(t, s.Prepare(ctx))
	// Activate deliberately NOT called — this is the rollback shape.
	require.NoError(t, s.Stop(ctx))

	assert.Empty(t, w.recorded(), "a subsystem that never activated MUST NOT write to characters")
	assert.Len(t, kv.snapshot(), 1, "and MUST leave the durable buffer intact for the next boot")
}

func TestActivateRegistersTheJobAndStopUnregistersIt(t *testing.T) {
	ctx := context.Background()
	reg := &fakeJobRegistry{}
	s := newFakeWiredSubsystem(t, newFakeKV(), (&recordingWriter{}).write)
	s.cfg.Jobs = reg

	require.NoError(t, s.Prepare(ctx))
	require.NoError(t, s.Activate(ctx))
	assert.Equal(t, map[string][]string{JobName: {"character"}}, reg.registered,
		"D-68 option A+D: every background consumer declares its identity and capability class")

	require.NoError(t, s.Stop(ctx))
	assert.Empty(t, reg.registered, "a stopped job MUST resolve to no attributes")
}

type fakeJobRegistry struct {
	registered map[string][]string
}

// fakeConsumer stands in for the durable JetStream consumer. Consume returns a
// context whose Stop is observable, which is what lets the lifecycle tests
// assert the producers really were joined.
type fakeConsumer struct {
	handler jetstream.MessageHandler
	cc      *fakeConsumeContext
}

func (c *fakeConsumer) Consume(h jetstream.MessageHandler, _ ...jetstream.PullConsumeOpt) (jetstream.ConsumeContext, error) {
	c.handler = h
	c.cc = newFakeConsumeContext()
	return c.cc, nil
}

// fakeConsumeContext models the REAL jetstream.ConsumeContext teardown
// contract, which the subsystem's join now depends on.
//
// Closed() must be a channel that closes only once no handler is in flight —
// in nats.go that is the subscription's closed handler firing at the end of
// waitForMsgs. A fake returning a nil channel (blocks forever) or a
// pre-closed one would make the join untestable in opposite directions: the
// first hangs the shutdown to its timeout, the second lets a Stop that joins
// nothing still pass. Here Stop closes it, and onStop lets a test interpose a
// still-running "handler" so the ordering claim can actually fail.
type fakeConsumeContext struct {
	mu      sync.Mutex
	stopped bool
	closed  chan struct{}
	// inFlight, when set, stands in for a handler that is STILL RUNNING when
	// Stop is called. Stop returns without waiting for it — as the real Stop
	// does — and Closed() is signalled only once it returns, which is the
	// contract nats.go provides and the one the subsystem's join relies on.
	inFlight func()
}

func newFakeConsumeContext() *fakeConsumeContext {
	return &fakeConsumeContext{closed: make(chan struct{})}
}

func (c *fakeConsumeContext) Stop() {
	c.mu.Lock()
	already := c.stopped
	c.stopped = true
	inFlight := c.inFlight
	c.mu.Unlock()
	if already {
		return
	}
	// Deliberately NOT synchronous: the real Stop flags the subscription and
	// returns while the dispatch goroutine is still running the handler.
	go func() {
		if inFlight != nil {
			inFlight()
		}
		close(c.closed)
	}()
}

func (c *fakeConsumeContext) Drain()                  { c.Stop() }
func (c *fakeConsumeContext) Closed() <-chan struct{} { return c.closed }

// newFakeWiredSubsystem builds a Subsystem whose two acquisition seams yield
// fakes, so the whole Prepare/Activate/Stop contract runs without a live
// JetStream.
func newFakeWiredSubsystem(t *testing.T, kv activityKV, w ActivityWriter) *Subsystem {
	t.Helper()
	s := NewSubsystemWithStorage(Config{Writer: w}, jetstream.MemoryStorage)
	s.createKV = func(context.Context) (activityKV, error) { return kv, nil }
	s.createConsumer = func(context.Context) (consumeStarter, error) { return &fakeConsumer{}, nil }
	return s
}

func (r *fakeJobRegistry) Register(name string, writes []string) error {
	if r.registered == nil {
		r.registered = make(map[string][]string)
	}
	r.registered[name] = writes
	return nil
}

func (r *fakeJobRegistry) Unregister(name string) { delete(r.registered, name) }
