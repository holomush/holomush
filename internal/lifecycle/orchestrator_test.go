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

type stubSubsystem struct {
	id      lifecycle.SubsystemID
	deps    []lifecycle.SubsystemID
	startFn func(ctx context.Context) error
	stopFn  func(ctx context.Context) error
	started bool
	stopped bool
	mu      sync.Mutex
}

func (s *stubSubsystem) ID() lifecycle.SubsystemID          { return s.id }
func (s *stubSubsystem) DependsOn() []lifecycle.SubsystemID { return s.deps }
func (s *stubSubsystem) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started = true
	if s.startFn != nil {
		return s.startFn(ctx)
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

func TestOrchestratorStartsInDependencyOrder(t *testing.T) {
	var order []lifecycle.SubsystemID
	var mu sync.Mutex

	makeSub := func(id lifecycle.SubsystemID, deps ...lifecycle.SubsystemID) *stubSubsystem {
		return &stubSubsystem{
			id:   id,
			deps: deps,
			startFn: func(_ context.Context) error {
				mu.Lock()
				order = append(order, id)
				mu.Unlock()
				return nil
			},
		}
	}

	db := makeSub(lifecycle.SubsystemDatabase)
	abac := makeSub(lifecycle.SubsystemABAC, lifecycle.SubsystemDatabase)
	bootstrap := makeSub(lifecycle.SubsystemBootstrap, lifecycle.SubsystemABAC)

	orch := lifecycle.NewOrchestrator()
	orch.Register(bootstrap) // register out of order to prove topo sort works
	orch.Register(db)
	orch.Register(abac)

	err := orch.StartAll(context.Background())
	require.NoError(t, err)

	// Database must come before ABAC, ABAC before Bootstrap
	dbIdx := indexOf(order, lifecycle.SubsystemDatabase)
	abacIdx := indexOf(order, lifecycle.SubsystemABAC)
	bootIdx := indexOf(order, lifecycle.SubsystemBootstrap)
	assert.Less(t, dbIdx, abacIdx)
	assert.Less(t, abacIdx, bootIdx)
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

func TestOrchestratorStartFailureIsFatal(t *testing.T) {
	db := &stubSubsystem{
		id:      lifecycle.SubsystemDatabase,
		startFn: func(_ context.Context) error { return errors.New("connection refused") },
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
	assert.False(t, abac.started, "ABAC should not have started")
}

func TestOrchestratorRollbackOnFailure(t *testing.T) {
	db := &stubSubsystem{id: lifecycle.SubsystemDatabase}
	abac := &stubSubsystem{
		id:      lifecycle.SubsystemABAC,
		deps:    []lifecycle.SubsystemID{lifecycle.SubsystemDatabase},
		startFn: func(_ context.Context) error { return errors.New("abac init failed") },
	}

	orch := lifecycle.NewOrchestrator()
	orch.Register(db)
	orch.Register(abac)

	err := orch.StartAll(context.Background())
	require.Error(t, err)

	// Database was started before ABAC failed, so it should have been rolled back (stopped).
	db.mu.Lock()
	defer db.mu.Unlock()
	assert.True(t, db.stopped, "database should be stopped during rollback")
}

func TestOrchestratorStopsInReverseOrder(t *testing.T) {
	var stopOrder []lifecycle.SubsystemID
	var mu sync.Mutex

	makeSub := func(id lifecycle.SubsystemID, deps ...lifecycle.SubsystemID) *stubSubsystem {
		return &stubSubsystem{
			id:   id,
			deps: deps,
			stopFn: func(_ context.Context) error {
				mu.Lock()
				stopOrder = append(stopOrder, id)
				mu.Unlock()
				return nil
			},
		}
	}

	db := makeSub(lifecycle.SubsystemDatabase)
	abac := makeSub(lifecycle.SubsystemABAC, lifecycle.SubsystemDatabase)
	bootstrap := makeSub(lifecycle.SubsystemBootstrap, lifecycle.SubsystemABAC)

	orch := lifecycle.NewOrchestrator()
	orch.Register(db)
	orch.Register(abac)
	orch.Register(bootstrap)

	require.NoError(t, orch.StartAll(context.Background()))
	orch.StopAll(context.Background())

	// Stop order should be reverse of start: bootstrap, abac, db
	require.Len(t, stopOrder, 3)
	assert.Equal(t, lifecycle.SubsystemBootstrap, stopOrder[0])
	assert.Equal(t, lifecycle.SubsystemABAC, stopOrder[1])
	assert.Equal(t, lifecycle.SubsystemDatabase, stopOrder[2])
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

func indexOf(slice []lifecycle.SubsystemID, id lifecycle.SubsystemID) int {
	for i, v := range slice {
		if v == id {
			return i
		}
	}
	return -1
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
func TestStartAllRollbackStopsSubsystemsWhenStartupCtxAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := &stubSubsystem{id: lifecycle.SubsystemDatabase}
	abac := &stubSubsystem{
		id:   lifecycle.SubsystemABAC,
		deps: []lifecycle.SubsystemID{lifecycle.SubsystemDatabase},
		startFn: func(_ context.Context) error {
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
}
