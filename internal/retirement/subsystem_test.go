// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package retirement

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/pkg/errutil"
)

// --- lifecycle fakes ---------------------------------------------------

// fakeConsumeContext models the REAL jetstream.ConsumeContext teardown
// contract, which the subsystem's join depends on.
//
// Stop() IS NOT A BARRIER: in nats.go it only flags the subscription and
// returns while the dispatch goroutine is still running the handler. A fake
// that closed Closed() synchronously inside Stop would assert the opposite
// contract, and every lifecycle test would then pass with the
// <-cc.Closed() join deleted from Activate outright — which is exactly what
// this fake used to do.
//
// Closed() is the real barrier, signalled only once the in-flight handler has
// returned, so a test can finally observe the ordering Activate's doc block
// claims: the job's ABAC liveness outlives the last process().
type fakeConsumeContext struct {
	mu      sync.Mutex
	stopped bool
	closed  chan struct{}
	// inFlight, when set, stands in for a process() that is STILL MID-FANOUT
	// when Stop is called.
	inFlight func()
}

func newFakeConsumeContext() *fakeConsumeContext {
	return &fakeConsumeContext{closed: make(chan struct{})}
}

func (f *fakeConsumeContext) Stop() {
	f.mu.Lock()
	already := f.stopped
	f.stopped = true
	inFlight := f.inFlight
	f.mu.Unlock()
	if already {
		return
	}
	// Deliberately NOT synchronous, and deliberately not joined by Stop: the
	// real Stop returns while the handler is still running, and only the
	// subscription's closed handler — modelled by close(f.closed) below —
	// fires after it has returned.
	go func() {
		if inFlight != nil {
			inFlight()
		}
		close(f.closed)
	}()
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

// fakeJobs records the liveness declarations the subsystem makes.
//
// It is mutex-guarded because Unregister is reached from Stop while an
// in-flight handler (modelled on the consume context's own goroutine) may
// still be marking the same log — which is the whole ordering this package
// needs to be able to assert.
type fakeJobs struct {
	mu         sync.Mutex
	registered map[string][]string
	registerEr error
	// events records the registration ORDER so it can be checked against
	// consumer startup and against handler completion.
	events []string
}

func newFakeJobs() *fakeJobs { return &fakeJobs{registered: map[string][]string{}} }

func (f *fakeJobs) Register(name string, writes []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, "register:"+name)
	if f.registerEr != nil {
		return f.registerEr
	}
	f.registered[name] = writes
	return nil
}

func (f *fakeJobs) Unregister(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, "unregister:"+name)
	delete(f.registered, name)
}

// mark appends an arbitrary ordering marker to the same log the liveness
// declarations land in, so a handler's completion is orderable against them.
func (f *fakeJobs) mark(event string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
}

func (f *fakeJobs) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.events...)
}

func (f *fakeJobs) declared() map[string][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string][]string, len(f.registered))
	for name, writes := range f.registered {
		out[name] = writes
	}
	return out
}

// newLifecycleSubsystem builds a Subsystem with every effect surface wired to
// an inert fake and the consumer-create seam substituted, so the
// Prepare/Activate/Stop contract is exercisable without a live JetStream.
func newLifecycleSubsystem(t *testing.T) (*Subsystem, *fakeConsumer) {
	t.Helper()
	s, cons, _ := newLifecycleSubsystemWithJobs(t, newFakeJobs())
	return s, cons
}

func newLifecycleSubsystemWithJobs(t *testing.T, jobsReg *fakeJobs) (*Subsystem, *fakeConsumer, *fakeJobs) {
	t.Helper()
	cons := &fakeConsumer{cc: newFakeConsumeContext()}
	j := &journal{}
	cfg := Config{
		Sessions:        &fakeSessions{j: j},
		Presence:        &fakePresence{j: j},
		World:           &fakeWorld{j: j},
		StartLocationID: func() ulid.ULID { return core.NewULID() },
	}
	// Assigned conditionally: `Jobs: (*fakeJobs)(nil)` would be a NON-nil
	// interface holding a nil pointer, so the nil-registry path would never be
	// what the caller asked for.
	if jobsReg != nil {
		cfg.Jobs = jobsReg
	}
	s := NewSubsystem(cfg)
	s.createConsumer = func(context.Context) (consumeStarter, error) { return cons, nil }
	return s, cons, jobsReg
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

// TestTheJoinBudgetStrictlyOutlastsTheBarrierBudget pins the ONE relationship
// that makes Stop's join deterministic.
//
// Stop's <-s.done wait and the shutdown goroutine's <-cc.Closed() wait start at
// essentially the same instant: Stop cancels runCtx, and the goroutine's
// <-runCtx.Done() returns immediately. Give the two the same budget and their
// timers expire together, so the join can never observe the barrier resolving.
//
// The margin is what makes the join deterministic — and it is ALSO what makes
// the outer arm a last resort rather than the wedged-fanout report: the
// goroutine's only blocking wait is the barrier, so it always exits first. The
// wedged case is reported off s.barrierOK instead, which is what
// TestStopReportsTheLivenessRetractionWhenTheBarrierWasAbandoned covers.
func TestTheJoinBudgetStrictlyOutlastsTheBarrierBudget(t *testing.T) {
	require.Greater(t, stopTimeout, barrierTimeout,
		"the outer join MUST outlast the inner barrier, or it can never observe it resolving")
}

// TestStopReportsTheLivenessRetractionWhenTheBarrierWasAbandoned covers the
// case that the outer stopTimeout arm was written for and cannot reach.
//
// Because the shutdown goroutine's only blocking wait IS the barrier, s.done
// closes within barrierTimeout + ε on every path. So under a genuinely wedged
// fanout Stop's join SUCCEEDS, and inferring the barrier's outcome from the
// join can only ever conclude "fine". unregisterJob() then retracts the job's
// ABAC liveness one line later, under a live MoveCharacter, silently.
//
// s.barrierOK is the signal that distinguishes the two: closed only on the arm
// where cc.Closed() resolved. This test wedges the handler past a shortened
// barrier and asserts Stop says so.
func TestStopReportsTheLivenessRetractionWhenTheBarrierWasAbandoned(t *testing.T) {
	// slog.SetDefault is process-global: this test MUST NOT call t.Parallel().
	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctx := context.Background()
	s, cons := newLifecycleSubsystem(t)
	s.barrier = 10 * time.Millisecond
	require.NoError(t, s.Prepare(ctx))
	require.NoError(t, s.Activate(ctx))

	handlerDone := make(chan struct{})
	cons.cc.mu.Lock()
	cons.cc.inFlight = func() {
		// A fanout still mid-flight well past the barrier's budget.
		time.Sleep(200 * time.Millisecond)
		close(handlerDone)
	}
	cons.cc.mu.Unlock()

	require.NoError(t, s.Stop(ctx))

	require.Contains(t, logBuf.String(),
		"retirement reactor retracting job liveness under a possible in-flight fanout",
		"a join that succeeded while the barrier was abandoned MUST still report the retraction")

	<-handlerDone // keep the fake's goroutine from outliving the log capture
}

// TestStopStaysSilentAboutLivenessWhenTheBarrierResolved is the other half:
// the retraction warning MUST NOT fire on the ordinary path, or it is noise an
// operator learns to ignore.
func TestStopStaysSilentAboutLivenessWhenTheBarrierResolved(t *testing.T) {
	// slog.SetDefault is process-global: this test MUST NOT call t.Parallel().
	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctx := context.Background()
	s, _ := newLifecycleSubsystem(t)
	require.NoError(t, s.Prepare(ctx))
	require.NoError(t, s.Activate(ctx))
	require.NoError(t, s.Stop(ctx))

	require.NotContains(t, logBuf.String(), "retracting job liveness")
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

// --- job liveness ------------------------------------------------------

// TestActivateDeclaresTheJobLiveBeforeAnyMessageCanBeDelivered is the D-49
// ordering proof: authority is tied to liveness, so registering AFTER Consume
// would leave the first delivered retirement denied for no diagnosable reason.
func TestActivateDeclaresTheJobLiveBeforeAnyMessageCanBeDelivered(t *testing.T) {
	ctx := context.Background()
	s, cons, jobsReg := newLifecycleSubsystemWithJobs(t, newFakeJobs())
	require.NoError(t, s.Prepare(ctx))
	require.Equal(t, 0, cons.calls, "Prepare MUST NOT start consuming")

	require.NoError(t, s.Activate(ctx))
	require.Equal(t, []string{"register:" + JobName}, jobsReg.recorded())
	require.Equal(t, 1, cons.calls)
	require.NoError(t, s.Stop(ctx))
}

// TestActivateDeclaresOnlyTheCharacterWriteCapabilityClass pins D-53: the
// declaration claims no more than the bindings prove. Session teardown has no
// policy chokepoint, so declaring it would be a claim nothing enforces.
func TestActivateDeclaresOnlyTheCharacterWriteCapabilityClass(t *testing.T) {
	ctx := context.Background()
	s, _, jobsReg := newLifecycleSubsystemWithJobs(t, newFakeJobs())
	require.NoError(t, s.Prepare(ctx))
	require.NoError(t, s.Activate(ctx))

	require.Equal(t, map[string][]string{JobName: {"character"}}, jobsReg.declared())
	require.NoError(t, s.Stop(ctx))
}

// TestStopJoinsAnInFlightHandlerBeforeRetractingTheJobsLiveness is the
// ordering Activate's <-cc.Closed() barrier exists for, and the one nothing in
// this package could previously falsify.
//
// Stop() alone is not a barrier: a process() can still be mid-fanout when it
// returns. If Stop went straight on to unregisterJob(), that still-running
// MoveCharacter would lose its ABAC attributes mid-flight, and classifyWorldError
// would ack the resulting deny as terminal — permanently abandoning a
// half-applied fanout (session already deleted, character never moved).
//
// The in-flight marker MUST therefore land before the unregister, not after.
func TestStopJoinsAnInFlightHandlerBeforeRetractingTheJobsLiveness(t *testing.T) {
	ctx := context.Background()
	jobsReg := newFakeJobs()
	s, cons, _ := newLifecycleSubsystemWithJobs(t, jobsReg)
	require.NoError(t, s.Prepare(ctx))
	require.NoError(t, s.Activate(ctx))

	cons.cc.mu.Lock()
	cons.cc.inFlight = func() {
		// A process() that was already mid-fanout when Stop was called. The
		// sleep is what makes the ordering claim falsifiable rather than
		// accidentally satisfied by goroutine scheduling.
		time.Sleep(20 * time.Millisecond)
		jobsReg.mark("handler-finished")
	}
	cons.cc.mu.Unlock()

	require.NoError(t, s.Stop(ctx))

	require.Equal(t,
		[]string{"register:" + JobName, "handler-finished", "unregister:" + JobName},
		jobsReg.recorded(),
		"the job's ABAC liveness MUST outlive the last in-flight process()")
}

func TestStopRetractsTheJobsLivenessDeclaration(t *testing.T) {
	ctx := context.Background()
	s, _, jobsReg := newLifecycleSubsystemWithJobs(t, newFakeJobs())
	require.NoError(t, s.Prepare(ctx))
	require.NoError(t, s.Activate(ctx))
	require.NoError(t, s.Stop(ctx))

	require.Empty(t, jobsReg.declared(), "a stopped reactor MUST resolve to no job attributes")
}

// TestActivateRetractsTheJobDeclarationWhenConsumeFails keeps a failed boot
// from leaving a job declared live that will never process a message.
func TestActivateRetractsTheJobDeclarationWhenConsumeFails(t *testing.T) {
	ctx := context.Background()
	s, cons, jobsReg := newLifecycleSubsystemWithJobs(t, newFakeJobs())
	cons.consumeErr = oops.Errorf("subscription refused")
	require.NoError(t, s.Prepare(ctx))

	require.Error(t, s.Activate(ctx))
	require.Empty(t, jobsReg.declared())
}

func TestActivateFailsWhenTheJobRegistrationIsRejected(t *testing.T) {
	ctx := context.Background()
	jobsReg := newFakeJobs()
	jobsReg.registerEr = oops.Code("JOB_REGISTRATION_INVALID").Errorf("nope")
	s, cons, _ := newLifecycleSubsystemWithJobs(t, jobsReg)
	require.NoError(t, s.Prepare(ctx))

	err := s.Activate(ctx)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "JOB_REGISTRATION_INVALID")
	require.Equal(t, 0, cons.calls, "a rejected declaration MUST NOT be followed by consumption")
}

// TestActivateToleratesANilJobRegistry pins the fail-CLOSED shape: an
// entrypoint that wires no registry runs a reactor whose every world write
// default-denies, which is safe — it is not a bypass.
func TestActivateToleratesANilJobRegistry(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newLifecycleSubsystemWithJobs(t, nil)
	require.Nil(t, s.cfg.Jobs)
	require.NoError(t, s.Prepare(ctx))
	require.NoError(t, s.Activate(ctx))
	require.NoError(t, s.Stop(ctx))
}

// TestSubsystemSatisfiesTheLifecycleInterface is the compile-time proof the
// skeleton is registerable today, before 03-04 fills the bodies.
func TestSubsystemSatisfiesTheLifecycleInterface(t *testing.T) {
	var s lifecycle.Subsystem = NewSubsystem(Config{})
	require.Equal(t, lifecycle.SubsystemRetirementReactor, s.ID())
}
