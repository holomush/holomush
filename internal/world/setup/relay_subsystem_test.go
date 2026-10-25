// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
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

// fakeListenConn is a listenConn whose WaitForNotification blocks until its
// ctx is done. It records whether Release was ever called while a
// WaitForNotification was still in flight — the exact interleaving that is a
// data race against a real pgconn (Release hands the connection to the pool
// while the waiting goroutine still reads from it).
type fakeListenConn struct {
	inflight             atomic.Int32
	releasedWhileWaiting atomic.Bool
	entered              chan struct{}
	enteredOnce          sync.Once
	released             chan struct{}
	releasedOnce         sync.Once
}

func newFakeListenConn() *fakeListenConn {
	return &fakeListenConn{entered: make(chan struct{}), released: make(chan struct{})}
}

func (f *fakeListenConn) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (f *fakeListenConn) WaitForNotification(ctx context.Context) (*pgconn.Notification, error) {
	f.inflight.Add(1)
	defer f.inflight.Add(-1)
	f.enteredOnce.Do(func() { close(f.entered) })
	<-ctx.Done()
	return nil, ctx.Err()
}

func (f *fakeListenConn) Release() {
	if f.inflight.Load() != 0 {
		f.releasedWhileWaiting.Store(true)
	}
	f.releasedOnce.Do(func() { close(f.released) })
}

func fakeAcquire(conn *fakeListenConn) func(context.Context) (listenConn, error) {
	return func(context.Context) (listenConn, error) { return conn, nil }
}

func TestOutboxWakerCloseUnblocksInFlightWaitBeforeReleasingConn(t *testing.T) {
	t.Parallel()
	conn := newFakeListenConn()
	w := newOutboxWakerWithAcquire(fakeAcquire(conn))

	errCh := make(chan error, 1)
	go func() { errCh <- w.Wait(context.Background()) }()
	awaitClosed(t, conn.entered, "WaitForNotification entry")

	w.Close()

	select {
	case err := <-errCh:
		require.Error(t, err, "Close must unblock the in-flight Wait")
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return after Close")
	}
	awaitClosed(t, conn.released, "release of the pinned connection")
	assert.False(t, conn.releasedWhileWaiting.Load(),
		"Release ran while WaitForNotification was still in flight — that is the pgconn data race")
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

func TestOutboxWakerWaitAfterCloseBlocksOnCallerCtxWithoutAcquiring(t *testing.T) {
	t.Parallel()
	acquired := false
	w := newOutboxWakerWithAcquire(func(context.Context) (listenConn, error) {
		acquired = true
		return newFakeListenConn(), nil
	})
	w.Close()

	// A closed waker must neither spin (return instantly) nor pin a new
	// connection: it blocks until the caller's ctx is done, which is what
	// makes the relay sweep-only instead of busy-looping.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := w.Wait(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.GreaterOrEqual(t, time.Since(start), 50*time.Millisecond, "Wait returned before the caller ctx was done")
	assert.False(t, acquired, "a closed waker must not pin a new connection")
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

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return after caller cancel")
	}
	awaitClosed(t, conn.released, "release of the pinned connection")
	assert.False(t, conn.releasedWhileWaiting.Load())
	// Close after the conn was already released on the error path is a no-op.
	w.Close()
}
