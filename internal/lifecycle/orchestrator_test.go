// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lifecycle_test

import (
	"context"
	"errors"
	"sync"
	"testing"

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
