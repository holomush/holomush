// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lifecycle_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/lifecycle"
)

// phaseCall records one invocation of a lifecycle phase (Prepare, Activate,
// or Stop) against a subsystem id, in global invocation order.
type phaseCall struct {
	phase string
	id    lifecycle.SubsystemID
}

type stubSubsystem struct {
	id         lifecycle.SubsystemID
	deps       []lifecycle.SubsystemID
	prepareFn  func(ctx context.Context) error
	activateFn func(ctx context.Context) error
	stopFn     func(ctx context.Context) error
	prepared   bool
	activated  bool
	stopped    bool
	mu         sync.Mutex
}

func (s *stubSubsystem) ID() lifecycle.SubsystemID          { return s.id }
func (s *stubSubsystem) DependsOn() []lifecycle.SubsystemID { return s.deps }

func (s *stubSubsystem) Prepare(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prepared = true
	if s.prepareFn != nil {
		return s.prepareFn(ctx)
	}
	return nil
}

func (s *stubSubsystem) Activate(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activated = true
	if s.activateFn != nil {
		return s.activateFn(ctx)
	}
	return nil
}

func (s *stubSubsystem) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	if s.stopFn != nil {
		return s.stopFn(ctx)
	}
	return nil
}

// recordingSub wraps a *stubSubsystem and appends a phaseCall to a shared,
// mutex-guarded log every time Prepare, Activate, or Stop is invoked — used
// to assert cross-subsystem call ORDER, not just per-subsystem booleans.
type recordingSub struct {
	*stubSubsystem
	log *[]phaseCall
	mu  *sync.Mutex
}

func newRecordingSub(id lifecycle.SubsystemID, log *[]phaseCall, mu *sync.Mutex, deps ...lifecycle.SubsystemID) *recordingSub {
	return &recordingSub{
		stubSubsystem: &stubSubsystem{id: id, deps: deps},
		log:           log,
		mu:            mu,
	}
}

func (r *recordingSub) Prepare(ctx context.Context) error {
	r.mu.Lock()
	*r.log = append(*r.log, phaseCall{phase: "Prepare", id: r.id})
	r.mu.Unlock()
	return r.stubSubsystem.Prepare(ctx)
}

func (r *recordingSub) Activate(ctx context.Context) error {
	r.mu.Lock()
	*r.log = append(*r.log, phaseCall{phase: "Activate", id: r.id})
	r.mu.Unlock()
	return r.stubSubsystem.Activate(ctx)
}

func (r *recordingSub) Stop(ctx context.Context) error {
	r.mu.Lock()
	*r.log = append(*r.log, phaseCall{phase: "Stop", id: r.id})
	r.mu.Unlock()
	return r.stubSubsystem.Stop(ctx)
}

func TestOrchestratorStartsInDependencyOrder(t *testing.T) {
	var log []phaseCall
	var mu sync.Mutex

	db := newRecordingSub(lifecycle.SubsystemDatabase, &log, &mu)
	abac := newRecordingSub(lifecycle.SubsystemABAC, &log, &mu, lifecycle.SubsystemDatabase)
	bootstrap := newRecordingSub(lifecycle.SubsystemBootstrap, &log, &mu, lifecycle.SubsystemABAC)

	orch := lifecycle.NewOrchestrator()
	orch.Register(bootstrap) // register out of order to prove topo sort works
	orch.Register(db)
	orch.Register(abac)

	err := orch.StartAll(context.Background())
	require.NoError(t, err)

	prepareOrder := prepareIndices(log)

	// Database must come before ABAC, ABAC before Bootstrap (Prepare phase).
	assert.Less(t, prepareOrder[lifecycle.SubsystemDatabase], prepareOrder[lifecycle.SubsystemABAC])
	assert.Less(t, prepareOrder[lifecycle.SubsystemABAC], prepareOrder[lifecycle.SubsystemBootstrap])
}

// TestStartAllRunsAllPreparesBeforeAnyActivate is the whole point of the
// two-sweep design (D-13.1): given subsystems A->B->C, StartAll must call
// Prepare(A), Prepare(B), Prepare(C), THEN Activate(A), Activate(B),
// Activate(C) — recorded call order proves no Activate precedes ANY
// Prepare, not merely that each subsystem's own Activate follows its own
// Prepare (which a per-subsystem Prepare-then-Activate rewrite of Start
// would also satisfy, without buying the global barrier).
func TestStartAllRunsAllPreparesBeforeAnyActivate(t *testing.T) {
	var log []phaseCall
	var mu sync.Mutex

	a := newRecordingSub(lifecycle.SubsystemDatabase, &log, &mu)
	b := newRecordingSub(lifecycle.SubsystemABAC, &log, &mu, lifecycle.SubsystemDatabase)
	c := newRecordingSub(lifecycle.SubsystemBootstrap, &log, &mu, lifecycle.SubsystemABAC)

	orch := lifecycle.NewOrchestrator()
	orch.Register(c)
	orch.Register(a)
	orch.Register(b)

	require.NoError(t, orch.StartAll(context.Background()))

	require.Len(t, log, 6, "3 Prepare + 3 Activate calls expected")

	lastPrepareIdx := -1
	firstActivateIdx := -1
	for i, call := range log {
		switch call.phase {
		case "Prepare":
			lastPrepareIdx = i
		case "Activate":
			if firstActivateIdx == -1 {
				firstActivateIdx = i
			}
		}
	}
	require.NotEqual(t, -1, firstActivateIdx, "at least one Activate call expected")
	assert.Less(t, lastPrepareIdx, firstActivateIdx, "no Activate may precede the last Prepare")

	// The first three calls must all be Prepare (in topo order), the last
	// three must all be Activate (in topo order).
	for i := 0; i < 3; i++ {
		assert.Equal(t, "Prepare", log[i].phase, "call %d expected to be a Prepare", i)
	}
	for i := 3; i < 6; i++ {
		assert.Equal(t, "Activate", log[i].phase, "call %d expected to be an Activate", i)
	}
}

func TestOrchestratorDetectsCycle(t *testing.T) {
	a := &stubSubsystem{id: lifecycle.SubsystemABAC, deps: []lifecycle.SubsystemID{lifecycle.SubsystemWorld}}
	b := &stubSubsystem{id: lifecycle.SubsystemWorld, deps: []lifecycle.SubsystemID{lifecycle.SubsystemABAC}}

	orch := lifecycle.NewOrchestrator()
	orch.Register(a)
	orch.Register(b)

	err := orch.StartAll(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestOrchestratorPrepareFailureIsFatal(t *testing.T) {
	db := &stubSubsystem{
		id:        lifecycle.SubsystemDatabase,
		prepareFn: func(_ context.Context) error { return errors.New("connection refused") },
	}
	abac := &stubSubsystem{
		id:   lifecycle.SubsystemABAC,
		deps: []lifecycle.SubsystemID{lifecycle.SubsystemDatabase},
	}

	orch := lifecycle.NewOrchestrator()
	orch.Register(db)
	orch.Register(abac)

	err := orch.StartAll(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
	assert.False(t, abac.prepared, "ABAC should never have been prepared")
	assert.False(t, abac.activated, "ABAC should never have been activated")
}

// TestOrchestratorRollbackStopsFailingSubsystemToo pins rollback Bug 1
// (cross-AI review): when Prepare(B) fails, Stop must be called on B
// itself, not only on subsystems prepared before it — a failed Prepare may
// have partially acquired resources.
func TestOrchestratorRollbackStopsFailingSubsystemToo(t *testing.T) {
	db := &stubSubsystem{id: lifecycle.SubsystemDatabase}
	abac := &stubSubsystem{
		id:        lifecycle.SubsystemABAC,
		deps:      []lifecycle.SubsystemID{lifecycle.SubsystemDatabase},
		prepareFn: func(_ context.Context) error { return errors.New("abac init failed") },
	}

	orch := lifecycle.NewOrchestrator()
	orch.Register(db)
	orch.Register(abac)

	err := orch.StartAll(context.Background())
	require.Error(t, err)

	db.mu.Lock()
	assert.True(t, db.stopped, "database should be stopped during rollback")
	db.mu.Unlock()

	abac.mu.Lock()
	assert.True(t, abac.stopped, "the failing subsystem itself must also be stopped during rollback")
	abac.mu.Unlock()
}

// TestOrchestratorActivateFailureRollsBackEverythingPrepared covers the
// Activate-fails-at-j rollback path (D-13.1): Stop is called on every
// prepared subsystem (a superset of the activated set), in reverse order.
func TestOrchestratorActivateFailureRollsBackEverythingPrepared(t *testing.T) {
	var log []phaseCall
	var mu sync.Mutex

	db := newRecordingSub(lifecycle.SubsystemDatabase, &log, &mu)
	abac := newRecordingSub(lifecycle.SubsystemABAC, &log, &mu, lifecycle.SubsystemDatabase)
	abac.activateFn = func(_ context.Context) error { return errors.New("abac activate failed") }
	bootstrap := newRecordingSub(lifecycle.SubsystemBootstrap, &log, &mu, lifecycle.SubsystemABAC)

	orch := lifecycle.NewOrchestrator()
	orch.Register(db)
	orch.Register(abac)
	orch.Register(bootstrap)

	err := orch.StartAll(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "abac activate failed")

	// bootstrap was prepared (topo order: db, abac, bootstrap) but its
	// Activate is never reached because abac's Activate fails first.
	bootstrap.mu.Lock()
	assert.True(t, bootstrap.prepared, "bootstrap must have been prepared before the activate sweep failed")
	assert.False(t, bootstrap.activated, "bootstrap must never activate once abac's activate fails")
	bootstrap.mu.Unlock()

	db.mu.Lock()
	assert.True(t, db.stopped, "db must be stopped during rollback")
	db.mu.Unlock()
	abac.mu.Lock()
	assert.True(t, abac.stopped, "abac must be stopped during rollback")
	abac.mu.Unlock()
	bootstrap.mu.Lock()
	assert.True(t, bootstrap.stopped, "bootstrap (prepared but never activated) must be stopped during rollback")
	bootstrap.mu.Unlock()

	// Reverse order: bootstrap, abac, db.
	stopOrder := stopIndices(log)
	assert.Less(t, stopOrder[lifecycle.SubsystemBootstrap], stopOrder[lifecycle.SubsystemABAC])
	assert.Less(t, stopOrder[lifecycle.SubsystemABAC], stopOrder[lifecycle.SubsystemDatabase])
}

// TestOrchestratorActivateFailureAtLastSubsystemRollsBackAll covers the
// specific "Activate(C) fails" case named in the plan's behavior spec.
func TestOrchestratorActivateFailureAtLastSubsystemRollsBackAll(t *testing.T) {
	var log []phaseCall
	var mu sync.Mutex

	db := newRecordingSub(lifecycle.SubsystemDatabase, &log, &mu)
	abac := newRecordingSub(lifecycle.SubsystemABAC, &log, &mu, lifecycle.SubsystemDatabase)
	bootstrap := newRecordingSub(lifecycle.SubsystemBootstrap, &log, &mu, lifecycle.SubsystemABAC)
	bootstrap.activateFn = func(_ context.Context) error { return errors.New("bootstrap activate failed") }

	orch := lifecycle.NewOrchestrator()
	orch.Register(db)
	orch.Register(abac)
	orch.Register(bootstrap)

	err := orch.StartAll(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bootstrap activate failed")

	db.mu.Lock()
	assert.True(t, db.activated, "db activates before bootstrap fails")
	assert.True(t, db.stopped)
	db.mu.Unlock()
	abac.mu.Lock()
	assert.True(t, abac.activated, "abac activates before bootstrap fails")
	assert.True(t, abac.stopped)
	abac.mu.Unlock()
	bootstrap.mu.Lock()
	assert.True(t, bootstrap.stopped, "bootstrap itself must also be stopped")
	bootstrap.mu.Unlock()

	stopOrder := stopIndices(log)
	assert.Less(t, stopOrder[lifecycle.SubsystemBootstrap], stopOrder[lifecycle.SubsystemABAC])
	assert.Less(t, stopOrder[lifecycle.SubsystemABAC], stopOrder[lifecycle.SubsystemDatabase])
}

func TestOrchestratorStopsInReverseOrder(t *testing.T) {
	var log []phaseCall
	var mu sync.Mutex

	db := newRecordingSub(lifecycle.SubsystemDatabase, &log, &mu)
	abac := newRecordingSub(lifecycle.SubsystemABAC, &log, &mu, lifecycle.SubsystemDatabase)
	bootstrap := newRecordingSub(lifecycle.SubsystemBootstrap, &log, &mu, lifecycle.SubsystemABAC)

	orch := lifecycle.NewOrchestrator()
	orch.Register(db)
	orch.Register(abac)
	orch.Register(bootstrap)

	require.NoError(t, orch.StartAll(context.Background()))
	orch.StopAll(context.Background())

	stopOrder := stopIndices(log)
	assert.Less(t, stopOrder[lifecycle.SubsystemBootstrap], stopOrder[lifecycle.SubsystemABAC])
	assert.Less(t, stopOrder[lifecycle.SubsystemABAC], stopOrder[lifecycle.SubsystemDatabase])
}

// TestStopAllOnPreparedButNeverActivatedSubsystemDoesNotError covers the
// D-13.1 contract extension: Stop must be safe to call on a subsystem that
// was prepared but never got to Activate (e.g. a sibling's Activate failed
// first).
func TestStopAllOnPreparedButNeverActivatedSubsystemDoesNotError(t *testing.T) {
	db := &stubSubsystem{id: lifecycle.SubsystemDatabase}
	abac := &stubSubsystem{
		id:         lifecycle.SubsystemABAC,
		deps:       []lifecycle.SubsystemID{lifecycle.SubsystemDatabase},
		activateFn: func(_ context.Context) error { return errors.New("abac activate failed") },
	}

	orch := lifecycle.NewOrchestrator()
	orch.Register(db)
	orch.Register(abac)

	err := orch.StartAll(context.Background())
	require.Error(t, err)

	// db was prepared and activated before abac's activate failed; it must
	// have been stopped without error during rollback.
	db.mu.Lock()
	assert.True(t, db.stopped)
	db.mu.Unlock()
}

func TestOrchestratorMissingDependency(t *testing.T) {
	abac := &stubSubsystem{
		id:   lifecycle.SubsystemABAC,
		deps: []lifecycle.SubsystemID{lifecycle.SubsystemDatabase}, // not registered
	}

	orch := lifecycle.NewOrchestrator()
	orch.Register(abac)

	err := orch.StartAll(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing dependency")
}

func prepareIndices(log []phaseCall) map[lifecycle.SubsystemID]int {
	out := make(map[lifecycle.SubsystemID]int)
	for i, call := range log {
		if call.phase == "Prepare" {
			out[call.id] = i
		}
	}
	return out
}

func stopIndices(log []phaseCall) map[lifecycle.SubsystemID]int {
	out := make(map[lifecycle.SubsystemID]int)
	stopSeq := 0
	for _, call := range log {
		if call.phase == "Stop" {
			out[call.id] = stopSeq
			stopSeq++
		}
	}
	return out
}

// TestStopAllReturnsWithinDeadlineWhenMiddleSubsystemStopBlocksOnCtxDone
// covers LOW-7's core requirement: a well-behaved Stop that blocks until its
// OWN ctx is done must not prevent StopAll from returning once the deadline
// elapses. Three subsystems are started; the middle one's Stop blocks on
// <-ctx.Done() (a subsystem correctly honoring cancellation, just slow to
// react to it in practice). StopAll, given a short-deadline ctx, must return
// at roughly that deadline rather than waiting indefinitely.
func TestStopAllReturnsWithinDeadlineWhenMiddleSubsystemStopBlocksOnCtxDone(t *testing.T) {
	db := &stubSubsystem{id: lifecycle.SubsystemDatabase}
	blocked := &stubSubsystem{
		id:   lifecycle.SubsystemABAC,
		deps: []lifecycle.SubsystemID{lifecycle.SubsystemDatabase},
		stopFn: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	top := &stubSubsystem{id: lifecycle.SubsystemBootstrap, deps: []lifecycle.SubsystemID{lifecycle.SubsystemABAC}}

	orch := lifecycle.NewOrchestrator()
	orch.Register(db)
	orch.Register(blocked)
	orch.Register(top)
	require.NoError(t, orch.StartAll(context.Background()))

	deadline := 100 * time.Millisecond
	stopCtx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()

	start := time.Now()
	orch.StopAll(stopCtx)
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 2*time.Second, "StopAll must return at roughly the deadline, not block indefinitely")

	// "top" stops first in reverse order and has no special Stop, so it
	// completes immediately.
	top.mu.Lock()
	assert.True(t, top.stopped, "top must have been stopped before the deadline consumed by the blocked middle subsystem")
	top.mu.Unlock()
}

// TestStopAllReturnsWithinDeadlineWhenSubsystemStopIgnoresCtxAndSleepsPastDeadline
// covers the misbehaving case: a Stop that IGNORES its ctx entirely and
// sleeps well past the deadline must still not hold up StopAll — the
// orchestrator stops trusting the Subsystem interface's "MUST NOT block
// indefinitely" contract and forcibly abandons it via the goroutine+select.
func TestStopAllReturnsWithinDeadlineWhenSubsystemStopIgnoresCtxAndSleepsPastDeadline(t *testing.T) {
	db := &stubSubsystem{id: lifecycle.SubsystemDatabase}
	rude := &stubSubsystem{
		id:   lifecycle.SubsystemABAC,
		deps: []lifecycle.SubsystemID{lifecycle.SubsystemDatabase},
		stopFn: func(_ context.Context) error {
			time.Sleep(2 * time.Second) // ignores its ctx entirely
			return nil
		},
	}

	orch := lifecycle.NewOrchestrator()
	orch.Register(db)
	orch.Register(rude)
	require.NoError(t, orch.StartAll(context.Background()))

	deadline := 100 * time.Millisecond
	stopCtx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()

	start := time.Now()
	orch.StopAll(stopCtx)
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 1*time.Second, "StopAll must return near the deadline even though rude's Stop ignores its ctx and sleeps for 2s")
}

// TestStopAllLogsAbandonedSubsystemsAtErrorLevelOnDeadline covers T-07-53:
// silent abandonment is worse than a slow shutdown, so StopAll must log
// (at error level) which subsystems it did not get to stop.
func TestStopAllLogsAbandonedSubsystemsAtErrorLevelOnDeadline(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, nil)
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	db := &stubSubsystem{id: lifecycle.SubsystemDatabase}
	rude := &stubSubsystem{
		id:   lifecycle.SubsystemABAC,
		deps: []lifecycle.SubsystemID{lifecycle.SubsystemDatabase},
		stopFn: func(_ context.Context) error {
			time.Sleep(2 * time.Second)
			return nil
		},
	}

	orch := lifecycle.NewOrchestrator()
	orch.Register(db)
	orch.Register(rude)
	require.NoError(t, orch.StartAll(context.Background()))

	stopCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	orch.StopAll(stopCtx)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "abandon", "abandoned-subsystem log line must mention abandonment")
	assert.Contains(t, logOutput, `"level":"ERROR"`, "abandoned-subsystem log must be at error level")
}

// TestStartAllRollbackStopsSubsystemsWhenStartupCtxAlreadyCancelled covers
// the rollback-path fix (cross-AI round 4, MEDIUM): once StopAll is
// deadline-aware, StartAll's rollback branch must NOT hand it the startup
// ctx, because that ctx can already be cancelled by the very failure that
// triggered rollback (e.g. a SIGINT mid-boot). Passing the cancelled ctx
// straight through would make the new ctx.Err() check abandon every
// rollback Stop instantly. Rollback must run on a fresh bounded context.
//
// This is pinned against the Prepare-fails path specifically (rollback Bug
// 2): Prepare(abac) both cancels the startup ctx AND fails, simulating a
// SIGINT arriving at the exact moment Prepare errors.
func TestStartAllRollbackStopsSubsystemsWhenStartupCtxAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := &stubSubsystem{id: lifecycle.SubsystemDatabase}
	abac := &stubSubsystem{
		id:   lifecycle.SubsystemABAC,
		deps: []lifecycle.SubsystemID{lifecycle.SubsystemDatabase},
		prepareFn: func(_ context.Context) error {
			// Simulate the startup ctx being cancelled (e.g. SIGINT) at the
			// exact moment the failure that triggers rollback occurs.
			cancel()
			return errors.New("abac init failed")
		},
	}

	orch := lifecycle.NewOrchestrator()
	orch.Register(db)
	orch.Register(abac)

	err := orch.StartAll(ctx)
	require.Error(t, err)

	db.mu.Lock()
	defer db.mu.Unlock()
	assert.True(t, db.stopped, "database must be stopped during rollback even though the startup ctx is already cancelled")

	abac.mu.Lock()
	defer abac.mu.Unlock()
	assert.True(t, abac.stopped, "the failing subsystem itself must also be stopped even though the startup ctx is already cancelled")
}

// TestOrchestratorTopoSortOrderUnchanged pins that the topological order
// produced for a fixed graph is unchanged from the single-phase
// implementation this plan replaces.
func TestOrchestratorTopoSortOrderUnchanged(t *testing.T) {
	var log []phaseCall
	var mu sync.Mutex

	db := newRecordingSub(lifecycle.SubsystemDatabase, &log, &mu)
	abac := newRecordingSub(lifecycle.SubsystemABAC, &log, &mu, lifecycle.SubsystemDatabase)
	auth := newRecordingSub(lifecycle.SubsystemAuth, &log, &mu, lifecycle.SubsystemDatabase)
	bootstrap := newRecordingSub(lifecycle.SubsystemBootstrap, &log, &mu, lifecycle.SubsystemABAC, lifecycle.SubsystemAuth)

	orch := lifecycle.NewOrchestrator()
	orch.Register(bootstrap)
	orch.Register(auth)
	orch.Register(db)
	orch.Register(abac)

	require.NoError(t, orch.StartAll(context.Background()))

	prepareOrder := prepareIndices(log)
	assert.Less(t, prepareOrder[lifecycle.SubsystemDatabase], prepareOrder[lifecycle.SubsystemABAC])
	assert.Less(t, prepareOrder[lifecycle.SubsystemDatabase], prepareOrder[lifecycle.SubsystemAuth])
	assert.Less(t, prepareOrder[lifecycle.SubsystemABAC], prepareOrder[lifecycle.SubsystemBootstrap])
	assert.Less(t, prepareOrder[lifecycle.SubsystemAuth], prepareOrder[lifecycle.SubsystemBootstrap])
}
