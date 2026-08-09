// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package retirement

import (
	"context"
	"sync"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/pkg/errutil"
)

// --- lifecycle fakes ---------------------------------------------------

type fakeConsumeContext struct {
	mu      sync.Mutex
	stopped bool
	closed  chan struct{}
}

func newFakeConsumeContext() *fakeConsumeContext {
	return &fakeConsumeContext{closed: make(chan struct{})}
}

func (f *fakeConsumeContext) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.stopped {
		f.stopped = true
		close(f.closed)
	}
}

func (f *fakeConsumeContext) Drain() { f.Stop() }

func (f *fakeConsumeContext) Closed() <-chan struct{} { return f.closed }

func (f *fakeConsumeContext) wasStopped() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopped
}

type fakeConsumer struct {
	cc         *fakeConsumeContext
	consumeErr error
	handler    jetstream.MessageHandler
	calls      int
}

func (f *fakeConsumer) Consume(handler jetstream.MessageHandler, _ ...jetstream.PullConsumeOpt) (jetstream.ConsumeContext, error) {
	f.calls++
	if f.consumeErr != nil {
		return nil, f.consumeErr
	}
	f.handler = handler
	return f.cc, nil
}

// newLifecycleSubsystem builds a Subsystem with every effect surface wired to
// an inert fake and the consumer-create seam substituted, so the
// Prepare/Activate/Stop contract is exercisable without a live JetStream.
func newLifecycleSubsystem(t *testing.T) (*Subsystem, *fakeConsumer) {
	t.Helper()
	cons := &fakeConsumer{cc: newFakeConsumeContext()}
	j := &journal{}
	s := NewSubsystem(Config{
		Sessions:        &fakeSessions{j: j},
		Presence:        &fakePresence{j: j},
		World:           &fakeWorld{j: j},
		StartLocationID: func() ulid.ULID { return core.NewULID() },
	})
	s.createConsumer = func(context.Context) (consumeStarter, error) { return cons, nil }
	return s, cons
}

func TestNewSubsystemDefaultsANilLoggerToTheProcessDefault(t *testing.T) {
	s := NewSubsystem(Config{})
	require.NotNil(t, s.cfg.Logger, "a nil Logger MUST fall back to slog.Default()")
}

// TestNewSubsystemAcquiresNothing pins the constructor contract
// cmd/holomush's real-constructor graph tests depend on: they build every
// production subsystem with a zero-value config and read DependsOn() live,
// never calling Prepare. A constructor that dialed, opened a file, or
// launched a goroutine would break those tests (and boot ordering).
func TestNewSubsystemAcquiresNoLiveResources(t *testing.T) {
	s := NewSubsystem(Config{})
	require.Nil(t, s.cons, "constructor MUST NOT create a consumer")
	require.Nil(t, s.done, "constructor MUST NOT launch a work loop")
}

func TestIDReturnsSubsystemRetirementReactor(t *testing.T) {
	require.Equal(t, lifecycle.SubsystemRetirementReactor, NewSubsystem(Config{}).ID())
}

// TestDependsOnDeclaresTheFanoutsFullSubstrateAndEffectSurfaces asserts the
// exact edge set. Bootstrap is the one a reader is most likely to think
// spurious: the fanout's destination is StartLocationID(), unresolvable
// before bootstrap's Prepare.
func TestDependsOnDeclaresTheFanoutsFullSubstrateAndEffectSurfaces(t *testing.T) {
	require.ElementsMatch(t, []lifecycle.SubsystemID{
		lifecycle.SubsystemDatabase,
		lifecycle.SubsystemEventBus,
		lifecycle.SubsystemWorld,
		lifecycle.SubsystemSessions,
		lifecycle.SubsystemBootstrap,
	}, NewSubsystem(Config{}).DependsOn())
}

func TestDependsOnExcludesItselfSoTheGraphStaysAcyclic(t *testing.T) {
	s := NewSubsystem(Config{})
	require.NotContains(t, s.DependsOn(), s.ID())
}

func TestPrepareIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s, _ := newLifecycleSubsystem(t)
	require.NoError(t, s.Prepare(ctx))
	first := s.cons
	require.NotNil(t, first)
	require.NoError(t, s.Prepare(ctx), "a repeated Prepare MUST be a no-op, not an error")
	require.Equal(t, first, s.cons, "a repeated Prepare MUST NOT rebuild the consumer")
}

// TestPrepareRefusesAnUnwiredSubsystem is the fail-loud contract: a nil effect
// surface is a composition-root bug, and discovering it at boot beats
// nil-dereferencing on the first retirement the server ever sees.
func TestPrepareRefusesAnUnwiredSubsystem(t *testing.T) {
	err := NewSubsystem(Config{}).Prepare(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "RETIREMENT_REACTOR_UNWIRED")
}

func TestActivateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s, cons := newLifecycleSubsystem(t)
	require.NoError(t, s.Prepare(ctx))
	require.NoError(t, s.Activate(ctx))
	first := s.done
	require.NotNil(t, first)
	require.NoError(t, s.Activate(ctx), "a repeated Activate MUST be a no-op, not an error")
	require.Equal(t, first, s.done, "a repeated Activate MUST NOT replace the done channel")
	require.Equal(t, 1, cons.calls, "a repeated Activate MUST NOT register a second callback")
	require.NoError(t, s.Stop(ctx))
}

// TestActivateRefusesToRunWithoutAPreparedConsumer replaces the skeleton's
// "Activate without Prepare still succeeds" contract. The orchestrator always
// runs the whole Prepare sweep first, so this never fires in production; if it
// ever did, a silent success would leave retirement permanently ineffective
// with nothing in the logs to point at.
func TestActivateRefusesToRunWithoutAPreparedConsumer(t *testing.T) {
	s, _ := newLifecycleSubsystem(t)
	err := s.Activate(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "RETIREMENT_REACTOR_NOT_PREPARED")
}

func TestActivateSurfacesAConsumeFailure(t *testing.T) {
	ctx := context.Background()
	s, cons := newLifecycleSubsystem(t)
	cons.consumeErr = oops.Errorf("subscription refused")
	require.NoError(t, s.Prepare(ctx))

	err := s.Activate(ctx)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "RETIREMENT_CONSUME_FAILED")
	require.Nil(t, s.done, "a failed Activate MUST NOT leave the guard set")
}

// TestStopStopsTheConsumeContext pins that teardown actually unsubscribes —
// the reason Activate parks a goroutine on the run context at all.
func TestStopStopsTheConsumeContext(t *testing.T) {
	ctx := context.Background()
	s, cons := newLifecycleSubsystem(t)
	require.NoError(t, s.Prepare(ctx))
	require.NoError(t, s.Activate(ctx))

	require.NoError(t, s.Stop(ctx))
	require.True(t, cons.cc.wasStopped(), "Stop MUST unsubscribe the durable consumer")
}

// TestStopResetsBothGuardsSoRetryWorks is the guard-reset contract: Stop is
// the single teardown path, and a Prepare/Activate retry after it MUST
// rebuild rather than short-circuit on torn-down state.
func TestStopResetsBothGuardsSoRetryWorks(t *testing.T) {
	ctx := context.Background()
	s, _ := newLifecycleSubsystem(t)
	require.NoError(t, s.Prepare(ctx))
	require.NoError(t, s.Activate(ctx))

	require.NoError(t, s.Stop(ctx))
	require.Nil(t, s.cons, "Stop MUST reset the Prepare guard")
	require.Nil(t, s.done, "Stop MUST reset the Activate guard")

	require.NoError(t, s.Prepare(ctx))
	require.NoError(t, s.Activate(ctx))
	require.NotNil(t, s.cons)
	require.NotNil(t, s.done)
	require.NoError(t, s.Stop(ctx))
}

func TestStopIsSafeInEveryLifecycleState(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		reach func(t *testing.T, s *Subsystem)
	}{
		{"never prepared", func(*testing.T, *Subsystem) {}},
		{"prepared but never activated", func(t *testing.T, s *Subsystem) {
			t.Helper()
			require.NoError(t, s.Prepare(ctx))
		}},
		{"fully activated", func(t *testing.T, s *Subsystem) {
			t.Helper()
			require.NoError(t, s.Prepare(ctx))
			require.NoError(t, s.Activate(ctx))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newLifecycleSubsystem(t)
			tt.reach(t, s)
			require.NoError(t, s.Stop(ctx))
			require.NoError(t, s.Stop(ctx), "Stop MUST be idempotent")
		})
	}
}

// TestSubsystemSatisfiesTheLifecycleInterface is the compile-time proof the
// skeleton is registerable today, before 03-04 fills the bodies.
func TestSubsystemSatisfiesTheLifecycleInterface(t *testing.T) {
	var s lifecycle.Subsystem = NewSubsystem(Config{})
	require.Equal(t, lifecycle.SubsystemRetirementReactor, s.ID())
}
