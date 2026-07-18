// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup

import (
	"context"
	"testing"

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
