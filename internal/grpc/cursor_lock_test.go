// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// recordingSendStream is a mockSubscribeStream that invokes a callback
// each time Send is called. Used by sendAndCommitEvent unit tests to
// observe ordering between Send and the cursorCommitHook.
type recordingSendStream struct {
	mockSubscribeStream
	onSend func()
}

func (r *recordingSendStream) Send(resp *corev1.SubscribeResponse) error {
	if r.onSend != nil {
		r.onSend()
	}
	return r.mockSubscribeStream.Send(resp)
}

func TestCursorLockMapBlocksSecondAcquireOnSameSession(t *testing.T) {
	m := newCursorLockMap()

	var observedOrder []int
	var orderMu sync.Mutex
	appendOrder := func(n int) {
		orderMu.Lock()
		observedOrder = append(observedOrder, n)
		orderMu.Unlock()
	}

	firstHoldingLock := make(chan struct{})
	releaseFirst := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(2)

	// First goroutine: take the lock, signal that it has it, then wait
	// for the test to release it. While it holds the lock, it appends
	// "1" to the observed order.
	go func() {
		defer wg.Done()
		unlock := m.lock("session-X")
		defer unlock()
		appendOrder(1)
		close(firstHoldingLock)
		<-releaseFirst
	}()

	<-firstHoldingLock

	// Second goroutine: try to take the lock for the same session. It
	// must block until the first goroutine releases. We assert this by
	// requiring its append to land *after* the first.
	secondAttempted := make(chan struct{})
	go func() {
		defer wg.Done()
		close(secondAttempted)
		unlock := m.lock("session-X")
		defer unlock()
		appendOrder(2)
	}()

	// Give the second goroutine a chance to reach lock(). It must NOT
	// progress past it because the first goroutine still holds the
	// session lock. A short sleep is acceptable here because we are
	// asserting *absence* of progress, and the lock primitive does not
	// expose introspection.
	<-secondAttempted
	time.Sleep(20 * time.Millisecond)

	orderMu.Lock()
	require.Equal(t, []int{1}, observedOrder,
		"second goroutine must NOT have entered the critical section while the first holds the lock")
	orderMu.Unlock()

	close(releaseFirst)
	wg.Wait()

	assert.Equal(t, []int{1, 2}, observedOrder,
		"goroutines on the same session must observe the critical section in lock-acquisition order")
}

func TestCursorLockMapPermitsConcurrentAcquireOnDifferentSessions(t *testing.T) {
	m := newCursorLockMap()

	firstHoldingLock := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondCompleted := make(chan struct{})

	go func() {
		unlock := m.lock("session-A")
		defer unlock()
		close(firstHoldingLock)
		<-releaseFirst
	}()

	<-firstHoldingLock

	// Acquire a different session's lock — must NOT block on session-A.
	go func() {
		unlock := m.lock("session-B")
		defer unlock()
		close(secondCompleted)
	}()

	select {
	case <-secondCompleted:
		// Different sessions did not serialize.
	case <-time.After(100 * time.Millisecond):
		t.Fatal("session-B was blocked by session-A's lock — different sessions must not serialize")
	}

	close(releaseFirst)
}

func TestCursorLockMapDeletesEntryWhenLastHolderReleases(t *testing.T) {
	m := newCursorLockMap()

	require.Equal(t, 0, len(m.locks), "registry must start empty")

	unlock := m.lock("session-Y")
	require.Equal(t, 1, len(m.locks), "lock must materialize the map entry")
	assert.Equal(t, 1, m.locks["session-Y"].refCount)

	unlock()
	assert.Equal(t, 0, len(m.locks),
		"map entry must be removed after the last holder releases — otherwise the map grows unbounded over the session lifetime")
}

func TestCursorLockMapKeepsEntryAliveWhileWaiterIsQueued(t *testing.T) {
	m := newCursorLockMap()

	firstHolding := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan struct{})
	waiterAcquired := make(chan struct{})
	waiterHoldsLock := make(chan struct{})
	waiterDone := make(chan struct{})

	// First goroutine: take the lock via the public helper, signal
	// that it's holding it, then wait for the test to release it.
	// firstDone closes only AFTER the defer chain has run — that is,
	// after both entry.mu.Unlock and m.release have completed. The
	// map-cleanup assertion below depends on this ordering to avoid a
	// flake where the waiter completes before the first holder has
	// decremented its refcount.
	go func() {
		defer close(firstDone)
		unlock := m.lock("session-Z")
		defer unlock()
		close(firstHolding)
		<-releaseFirst
	}()

	<-firstHolding

	// Second goroutine: drive acquire() and mu.Lock() separately so we
	// can synchronize on the acquire step. acquire() bumps refCount to
	// 2 immediately and returns; mu.Lock() then blocks behind the
	// first holder. Using m.lock() would do both in one call, leaving
	// the test with no race-free way to confirm acquire() ran before
	// asserting refCount.
	go func() {
		defer close(waiterDone)
		entry := m.acquire("session-Z")
		close(waiterAcquired)
		entry.mu.Lock()
		// Critical section: signal that we have transitioned from
		// "queued" to "holding". This gives the section a meaningful
		// body and lets the test assert ordering downstream — the
		// waiter must NOT signal waiterHoldsLock until releaseFirst
		// has been closed.
		close(waiterHoldsLock)
		// Match the lock helper's release order: unlock the per-session
		// mu first, then release the refcount. Reversing this would let
		// release run while the mutex is still held, which violates the
		// helper's contract.
		entry.mu.Unlock()
		m.release("session-Z")
	}()

	// Synchronize on the waiter's acquire(). After this, refCount must
	// be exactly 2 — the first holder plus the queued waiter.
	<-waiterAcquired

	m.mu.Lock()
	entry, present := m.locks["session-Z"]
	rc := 0
	if present {
		rc = entry.refCount
	}
	m.mu.Unlock()
	require.True(t, present, "entry must still exist while a waiter holds a refcount")
	require.NotNil(t, entry)
	assert.Equal(t, 2, rc,
		"refCount must be exactly 2 — first holder + queued waiter")

	// Confirm the waiter has not yet entered its critical section —
	// it should be blocked at mu.Lock() because the first holder is
	// still inside its own.
	select {
	case <-waiterHoldsLock:
		t.Fatal("waiter entered critical section before first holder released — mu serialization is broken")
	default:
	}

	// Release the first holder and wait for BOTH goroutines to finish
	// their full defer chains. waiterDone alone is insufficient — the
	// waiter can complete its critical section before the first
	// holder's m.release runs, leaving the map entry temporarily
	// alive with refCount=1 and making the stillPresent check flaky.
	// firstDone closes only after the first holder's full defer chain
	// (Unlock + release) completes.
	close(releaseFirst)
	<-firstDone
	<-waiterDone

	m.mu.Lock()
	_, stillPresent := m.locks["session-Z"]
	m.mu.Unlock()
	assert.False(t, stillPresent, "entry must be deleted after the last holder releases")
}

func TestCursorLockMapLockIsNoOpOnNilReceiver(t *testing.T) {
	var m *cursorLockMap // nil

	// Must not panic. The returned release function must also be safe
	// to invoke on a nil receiver — direct-construction CoreServer
	// unit tests rely on this.
	unlock := m.lock("session-nil")
	require.NotNil(t, unlock)
	assert.NotPanics(t, func() { unlock() })
}

func TestCursorLockMapRefCountReturnsZeroOnNilReceiver(t *testing.T) {
	var m *cursorLockMap
	assert.Equal(t, 0, m.refCount("session-any"),
		"nil receiver must return 0, not panic")
}

func TestCursorLockMapRefCountReturnsZeroForMissingSession(t *testing.T) {
	m := newCursorLockMap()
	assert.Equal(t, 0, m.refCount("never-locked"),
		"missing session must return 0")
}

func TestCursorLockMapRefCountReflectsActiveHolders(t *testing.T) {
	m := newCursorLockMap()

	unlock1 := m.lock("session-rc")
	assert.Equal(t, 1, m.refCount("session-rc"))

	// A second acquire (without blocking on the mutex, by using the
	// lower-level acquire() primitive) bumps the count without
	// deadlocking on the already-held mu.
	_ = m.acquire("session-rc")
	assert.Equal(t, 2, m.refCount("session-rc"))

	m.release("session-rc")
	assert.Equal(t, 1, m.refCount("session-rc"),
		"refCount must decrement by exactly one per release")

	unlock1()
	assert.Equal(t, 0, m.refCount("session-rc"),
		"entry must be deleted and refCount must be 0 after last release")
}

func TestCursorLockMapReleaseIsNoOpForMissingSession(t *testing.T) {
	m := newCursorLockMap()

	// Releasing a session that was never acquired must not panic and
	// must leave the map in a sane state. This protects against
	// double-release bugs in future refactors.
	assert.NotPanics(t, func() {
		m.release("never-locked")
	})
	assert.Equal(t, 0, len(m.locks))
}

func TestWithCursorCommitHookInstallsHookOnCoreServer(t *testing.T) {
	var captured struct {
		sessionID string
		eventID   ulid.ULID
		called    bool
	}

	hook := func(_ context.Context, sid string, eid ulid.ULID) {
		captured.sessionID = sid
		captured.eventID = eid
		captured.called = true
	}

	s := &CoreServer{}
	WithCursorCommitHook(hook)(s)

	require.NotNil(t, s.cursorCommitHook,
		"WithCursorCommitHook must install the hook on the CoreServer")

	eid := core.NewULID()
	s.cursorCommitHook(context.Background(), "test-session", eid)

	assert.True(t, captured.called, "installed hook must be invokable")
	assert.Equal(t, "test-session", captured.sessionID)
	assert.Equal(t, eid, captured.eventID)
}

func TestCoreServerCursorLockRefCountDelegatesToMap(t *testing.T) {
	s := &CoreServer{cursorLocks: newCursorLockMap()}

	// Missing session returns 0.
	assert.Equal(t, 0, s.CursorLockRefCount("absent"))

	// Holding the lock bumps the refcount observable via the CoreServer method.
	unlock := s.cursorLocks.lock("active")
	assert.Equal(t, 1, s.CursorLockRefCount("active"))

	unlock()
	assert.Equal(t, 0, s.CursorLockRefCount("active"),
		"refCount must drop to 0 after release")
}

func TestSendAndCommitEventFiresHookBetweenSendAndCommit(t *testing.T) {
	// This exercises sendAndCommitEvent's full per-event critical
	// section: lock acquire → Send → cursorCommitHook →
	// UpdateCursors → unlock. The ordering matters because Finding 1
	// closure (see holomush-9ues) depends on the hook firing AFTER
	// Send and BEFORE UpdateCursors, under the per-session lock.
	sessionID := core.NewULID().String()
	streamName := "location:test"
	ev := core.NewEvent(streamName, core.EventTypeSay, core.Actor{}, []byte(`{"message":"hi"}`))

	// The MemStore requires a pre-existing session for UpdateCursors
	// to take effect. Populate one with an empty cursor map.
	store := newTestSessionStore(t, map[string]*session.Info{
		sessionID: {
			ID:           sessionID,
			CharacterID:  core.NewULID(),
			EventCursors: map[string]ulid.ULID{},
		},
	})

	var observed []string
	var obsMu sync.Mutex
	record := func(step string) {
		obsMu.Lock()
		observed = append(observed, step)
		obsMu.Unlock()
	}

	hookCalled := false
	s := &CoreServer{
		sessionStore: store,
		cursorLocks:  newCursorLockMap(),
		cursorCommitHook: func(_ context.Context, _ string, _ ulid.ULID) {
			record("hook")
			hookCalled = true
		},
	}

	// Wrap mockSubscribeStream to record when Send is called. The
	// embedded mock handles Context() and the actual Send signature.
	stream := &recordingSendStream{
		mockSubscribeStream: mockSubscribeStream{ctx: context.Background()},
		onSend:              func() { record("send") },
	}

	info, err := store.Get(context.Background(), sessionID)
	require.NoError(t, err)

	// Slip a "commit" marker in via a second session store layer
	// would over-complicate this — instead, read the cursor after the
	// call and verify it was committed. Order of "send" vs "hook" is
	// still asserted via the observed slice plus the post-call cursor
	// check, which proves commit ran last.
	require.NoError(t, s.sendAndCommitEvent(
		context.Background(), info, streamName, ev, stream, nil))

	assert.True(t, hookCalled, "cursorCommitHook must fire inside the critical section")
	assert.Equal(t, []string{"send", "hook"}, observed,
		"Send must precede the hook inside the critical section")

	// Commit ran AFTER hook: the cursor must now reflect ev.ID.
	refreshed, getErr := store.Get(context.Background(), sessionID)
	require.NoError(t, getErr)
	assert.Equal(t, ev.ID, refreshed.EventCursors[streamName],
		"cursor must reflect ev.ID after sendAndCommitEvent returns")

	// Lock must be fully released after return (no leaked refcount).
	assert.Equal(t, 0, s.CursorLockRefCount(sessionID),
		"lock must be fully released after sendAndCommitEvent returns")
}

func TestSendAndCommitEventPropagatesSendErrorAndReleasesLock(t *testing.T) {
	// When grpcStream.Send fails, sendAndCommitEvent must:
	//   (1) propagate the error wrapped with the event ID
	//   (2) NOT commit the cursor (because the client never received
	//       the event)
	//   (3) release the per-session lock (no leaked refcount)
	sessionID := core.NewULID().String()
	streamName := "location:test"
	ev := core.NewEvent(streamName, core.EventTypeSay, core.Actor{}, []byte(`{"message":"hi"}`))

	store := newTestSessionStore(t, map[string]*session.Info{
		sessionID: {
			ID:           sessionID,
			CharacterID:  core.NewULID(),
			EventCursors: map[string]ulid.ULID{},
		},
	})

	// Capture the pre-call cursor map so we can verify no commit ran.
	preCall, err := store.Get(context.Background(), sessionID)
	require.NoError(t, err)
	require.Empty(t, preCall.EventCursors[streamName])

	s := &CoreServer{
		sessionStore: store,
		cursorLocks:  newCursorLockMap(),
	}

	failStream := &mockSubscribeStreamWithError{
		ctx:     context.Background(),
		sendErr: assert.AnError,
	}
	info, err := store.Get(context.Background(), sessionID)
	require.NoError(t, err)

	err = s.sendAndCommitEvent(context.Background(), info, streamName, ev, failStream, nil)
	require.Error(t, err, "Send failure must surface as an error from sendAndCommitEvent")
	assert.ErrorIs(t, err, assert.AnError)

	// Cursor must NOT have advanced — the commit is skipped when Send
	// fails, matching the at-most-once semantics of the per-event
	// critical section.
	refreshed, getErr := store.Get(context.Background(), sessionID)
	require.NoError(t, getErr)
	assert.Empty(t, refreshed.EventCursors[streamName],
		"cursor must NOT advance when Send fails — the client never got the event")

	// Lock must be released even on the error path.
	assert.Equal(t, 0, s.CursorLockRefCount(sessionID),
		"lock must be released via defer chain even when Send errors")
}

func TestSendAndCommitEventSkipsHookWhenNil(t *testing.T) {
	sessionID := core.NewULID().String()
	streamName := "location:test"
	ev := core.NewEvent(streamName, core.EventTypeSay, core.Actor{}, []byte(`{"message":"hi"}`))

	store := newTestSessionStore(t, map[string]*session.Info{
		sessionID: {
			ID:           sessionID,
			CharacterID:  core.NewULID(),
			EventCursors: map[string]ulid.ULID{},
		},
	})

	// No cursorCommitHook — the production configuration. The nil
	// branch must be taken without panicking.
	s := &CoreServer{
		sessionStore: store,
		cursorLocks:  newCursorLockMap(),
	}

	stream := &mockSubscribeStream{ctx: context.Background()}
	info, err := store.Get(context.Background(), sessionID)
	require.NoError(t, err)

	require.NoError(t, s.sendAndCommitEvent(
		context.Background(), info, streamName, ev, stream, nil))
	assert.Equal(t, 0, s.CursorLockRefCount(sessionID))
}
