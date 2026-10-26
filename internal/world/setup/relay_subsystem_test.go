// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/lifecycle"
)

// Compile-time interface check: *OutboxRelaySubsystem must satisfy lifecycle.Subsystem.
var _ lifecycle.Subsystem = (*OutboxRelaySubsystem)(nil)

// fakeRelayPool is a PoolProvider handing out a REAL (but unreachable)
// *pgxpool.Pool. pgxpool.New parses config and returns a live pool object
// without dialing — Acquire (called by the relay's background goroutine)
// then fails with a connection-refused error rather than panicking on a nil
// receiver, letting Activate's goroutine run and log/retry harmlessly
// during the test window without a real Postgres.
type fakeRelayPool struct{ pool *pgxpool.Pool }

func newFakeRelayPool(t *testing.T) fakeRelayPool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://127.0.0.1:1/holomush_relay_test_unreachable?connect_timeout=1")
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return fakeRelayPool{pool: pool}
}

func (f fakeRelayPool) Pool() *pgxpool.Pool { return f.pool }

// fakePublisher is a no-op eventbus.Publisher for unit tests that never
// actually publish (the relay's Run loop will error against a nil pool
// before it gets this far; Activate itself never blocks on it).
type fakePublisher struct{}

func (fakePublisher) Publish(_ context.Context, _ eventbus.Event) error { return nil }

// fakeRelayEventBus is an EventBusProvider returning fakePublisher.
type fakeRelayEventBus struct{}

func (fakeRelayEventBus) Publisher(_ ...eventbus.PublishOption) eventbus.Publisher {
	return fakePublisher{}
}

// TestOutboxRelaySubsystemIdentity gives the subsystem wiring gating-CI seam
// coverage (no containers): its SubsystemID and declared dependencies. A
// regression in the wiring is caught in the normal `task test` run rather
// than only under integration.
func TestOutboxRelaySubsystemIdentity(t *testing.T) {
	s := NewOutboxRelaySubsystem(OutboxRelaySubsystemConfig{})

	assert.Equal(t, lifecycle.SubsystemOutboxRelay, s.ID())
	require.Equal(t,
		[]lifecycle.SubsystemID{lifecycle.SubsystemDatabase, lifecycle.SubsystemEventBus},
		s.DependsOn(),
		"the relay DependsOn Database + EventBus")
}

// TestOutboxRelayResolvedGameIDDefaultsAtStart proves the "main" default is
// applied at Start-time resolution (07-09 item 7), not at construction — a nil
// GameID provider resolves to defaultRelayGameID.
func TestOutboxRelayResolvedGameIDDefaultsAtStart(t *testing.T) {
	s := NewOutboxRelaySubsystem(OutboxRelaySubsystemConfig{})
	assert.Nil(t, s.cfg.GameID, "constructor must not synthesize a default provider")

	got := s.resolveGameID()
	assert.Equal(t, defaultRelayGameID, got, "resolveGameID defaults to the world default when the provider is nil/empty")
}

// TestOutboxRelayResolvedGameIDOverride verifies an explicit GameID provider
// is honored at Start-time resolution.
func TestOutboxRelayResolvedGameIDOverride(t *testing.T) {
	s := NewOutboxRelaySubsystem(OutboxRelaySubsystemConfig{GameID: func() string { return "alt-game" }})
	assert.Equal(t, "alt-game", s.resolveGameID())
}

// TestOutboxRelayResolvedGameIDEmptyProviderFallsBackToDefault verifies a
// non-nil provider that resolves to "" still falls back to the default.
func TestOutboxRelayResolvedGameIDEmptyProviderFallsBackToDefault(t *testing.T) {
	s := NewOutboxRelaySubsystem(OutboxRelaySubsystemConfig{GameID: func() string { return "" }})
	assert.Equal(t, defaultRelayGameID, s.resolveGameID())
}

// TestOutboxRelaySubsystemRepeatedActivateLaunchesOnlyOneDrainGoroutine pins
// D-13.2 row 6's ADD guard: a second Activate must return nil without
// launching a second drain goroutine — the phase-owned s.done channel is
// the guard.
func TestOutboxRelaySubsystemRepeatedActivateLaunchesOnlyOneDrainGoroutine(t *testing.T) {
	s := NewOutboxRelaySubsystem(OutboxRelaySubsystemConfig{
		DB:       newFakeRelayPool(t),
		EventBus: fakeRelayEventBus{},
	})

	require.NoError(t, s.Prepare(context.Background()))
	require.NoError(t, s.Activate(context.Background()))
	firstDone := s.done

	require.NoError(t, s.Activate(context.Background()))
	assert.True(t, firstDone == s.done, "second Activate must not launch a second drain goroutine")

	require.NoError(t, s.Stop(context.Background()))
}

// TestOutboxRelaySubsystemRepeatedPrepareDoesNotRebuildRelay pins the
// Prepare-side guard: a second Prepare returns nil without rebuilding the
// waker/relay.
func TestOutboxRelaySubsystemRepeatedPrepareDoesNotRebuildRelay(t *testing.T) {
	s := NewOutboxRelaySubsystem(OutboxRelaySubsystemConfig{
		DB:       newFakeRelayPool(t),
		EventBus: fakeRelayEventBus{},
	})

	require.NoError(t, s.Prepare(context.Background()))
	firstRelay := s.relay

	require.NoError(t, s.Prepare(context.Background()))
	assert.Same(t, firstRelay, s.relay, "second Prepare must not rebuild the relay")
}

// fakeListenConn is a listenConn whose WaitForNotification returns nil when
// notify receives and ctx.Err() when its ctx is done. It counts releases and
// records whether Release was ever called while a WaitForNotification was
// still in flight — the exact interleaving that is a data race against a real
// pgconn (Release closes or recycles the connection while the waiting
// goroutine still reads from it).
type fakeListenConn struct {
	notify               chan struct{}
	inflight             atomic.Int32
	releases             atomic.Int32
	releasedWhileWaiting atomic.Bool
	entered              chan struct{}
	enteredOnce          sync.Once
	released             chan struct{}
	releasedOnce         sync.Once
}

func newFakeListenConn() *fakeListenConn {
	return &fakeListenConn{
		notify:   make(chan struct{}, 1),
		entered:  make(chan struct{}),
		released: make(chan struct{}),
	}
}

func (f *fakeListenConn) WaitForNotification(ctx context.Context) error {
	f.inflight.Add(1)
	defer f.inflight.Add(-1)
	f.enteredOnce.Do(func() { close(f.entered) })
	select {
	case <-f.notify:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *fakeListenConn) Release() {
	if f.inflight.Load() != 0 {
		f.releasedWhileWaiting.Store(true)
	}
	f.releases.Add(1)
	f.releasedOnce.Do(func() { close(f.released) })
}

func fakeAcquire(conn *fakeListenConn) func(context.Context) (listenConn, error) {
	return func(context.Context) (listenConn, error) { return conn, nil }
}

// closeAsync runs w.Close on its own goroutine and returns a channel that
// closes when Close returns, so a Close that never returns fails the test
// through awaitClosed instead of hanging the package.
func closeAsync(w *outboxWaker) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Close()
	}()
	return done
}

// awaitClosed fails the test with a message instead of hanging the package
// when a channel the test expects to close never does.
func awaitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not happen within 5s", what)
	}
}

// awaitErr returns the error received on errCh, failing the test with a
// message instead of hanging the package when nothing arrives.
func awaitErr(t *testing.T, errCh <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-errCh:
		return err
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not happen within 5s", what)
		return nil
	}
}

func TestOutboxWakerCloseUnblocksInFlightWaitBeforeReleasingConn(t *testing.T) {
	t.Parallel()
	conn := newFakeListenConn()
	w := newOutboxWakerWithAcquire(fakeAcquire(conn))

	errCh := make(chan error, 1)
	go func() { errCh <- w.Wait(context.Background()) }()
	awaitClosed(t, conn.entered, "WaitForNotification entry")

	awaitClosed(t, closeAsync(w), "Close return")
	assert.False(t, conn.releasedWhileWaiting.Load(),
		"Release ran while WaitForNotification was still in flight — that is the pgconn data race")
	require.Error(t, awaitErr(t, errCh, "Wait return after Close"), "Close must unblock the in-flight Wait")
	assert.Equal(t, int32(1), conn.releases.Load(), "the pinned connection must be released exactly once")
}

func TestOutboxWakerCloseUnblocksWaitBlockedInAcquire(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{})
	w := newOutboxWakerWithAcquire(func(ctx context.Context) (listenConn, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})

	errCh := make(chan error, 1)
	go func() { errCh <- w.Wait(context.Background()) }()
	awaitClosed(t, entered, "acquire entry")

	awaitClosed(t, closeAsync(w), "Close return")
	require.ErrorIs(t, awaitErr(t, errCh, "Wait return after Close"), context.Canceled,
		"Close must cancel the ctx an in-flight acquire runs under")
}

func TestOutboxWakerKeepsConnPinnedAcrossNotifications(t *testing.T) {
	t.Parallel()
	conn := newFakeListenConn()
	var acquires atomic.Int32
	w := newOutboxWakerWithAcquire(func(context.Context) (listenConn, error) {
		acquires.Add(1)
		return conn, nil
	})
	t.Cleanup(w.Close)

	for range 2 {
		conn.notify <- struct{}{}
		require.NoError(t, w.Wait(context.Background()))
	}
	assert.Equal(t, int32(1), acquires.Load(), "a notification must not drop the pinned connection")
	assert.Equal(t, int32(0), conn.releases.Load(), "a notification must not release the pinned connection")
}

func TestOutboxWakerCloseReleasesIdlePinnedConnOnce(t *testing.T) {
	t.Parallel()
	conn := newFakeListenConn()
	w := newOutboxWakerWithAcquire(fakeAcquire(conn))
	conn.notify <- struct{}{}
	require.NoError(t, w.Wait(context.Background()))

	w.Close()
	w.Close()
	assert.Equal(t, int32(1), conn.releases.Load(), "Close must release the idle pinned connection exactly once")
}

func TestOutboxWakerWaitAfterCloseBlocksOnCallerCtxWithoutAcquiring(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var acquired atomic.Bool
		w := newOutboxWakerWithAcquire(func(context.Context) (listenConn, error) {
			acquired.Store(true)
			return newFakeListenConn(), nil
		})
		w.Close()

		// A closed waker must neither spin (return instantly) nor pin a new
		// connection: it blocks until the caller's ctx is done, which is what
		// makes the relay sweep-only instead of busy-looping.
		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() { errCh <- w.Wait(ctx) }()
		synctest.Wait()
		select {
		case err := <-errCh:
			t.Fatalf("closed waker returned before the caller ctx was done: %v", err)
		default:
		}

		cancel()
		require.ErrorIs(t, <-errCh, context.Canceled)
		assert.False(t, acquired.Load(), "a closed waker must not pin a new connection")
	})
}

func TestOutboxWakerCallerCancelReleasesConnAfterWaitReturns(t *testing.T) {
	t.Parallel()
	conn := newFakeListenConn()
	w := newOutboxWakerWithAcquire(fakeAcquire(conn))

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- w.Wait(ctx) }()
	awaitClosed(t, conn.entered, "WaitForNotification entry")
	cancel()

	require.ErrorIs(t, awaitErr(t, errCh, "Wait return after caller cancel"), context.Canceled)
	awaitClosed(t, conn.released, "release of the pinned connection")
	assert.False(t, conn.releasedWhileWaiting.Load())

	w.Close()
	assert.Equal(t, int32(1), conn.releases.Load(), "Close after the error-path release must not release again")
}
