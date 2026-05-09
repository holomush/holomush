// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package main

import (
	"context"
	"testing"

	"github.com/holomush/holomush/internal/lifecycle"
)

// stubSubsystem is a minimal lifecycle.Subsystem for testing the
// productionSubsystems helper. Only ID() is read by the test.
type stubSubsystem struct {
	id lifecycle.SubsystemID
}

func (s stubSubsystem) ID() lifecycle.SubsystemID          { return s.id }
func (s stubSubsystem) DependsOn() []lifecycle.SubsystemID { return nil }
func (s stubSubsystem) Start(_ context.Context) error      { return nil }
func (s stubSubsystem) Stop(_ context.Context) error       { return nil }

// TestProductionSubsystemsIncludesCluster asserts that the production
// subsystem slice (as constructed by productionSubsystems) includes
// SubsystemCluster. If a future refactor drops clusterSub from the
// orchestrator wiring, this test fails — that's the regression catch
// Phase 3c relies on.
func TestProductionSubsystemsIncludesCluster(t *testing.T) {
	subs := productionSubsystems(
		stubSubsystem{id: lifecycle.SubsystemDatabase},
		stubSubsystem{id: lifecycle.SubsystemABAC},
		stubSubsystem{id: lifecycle.SubsystemAuth},
		stubSubsystem{id: lifecycle.SubsystemWorld},
		stubSubsystem{id: lifecycle.SubsystemSessions},
		stubSubsystem{id: lifecycle.SubsystemPlugins},
		stubSubsystem{id: lifecycle.SubsystemBootstrap},
		stubSubsystem{id: lifecycle.SubsystemEventBus},
		stubSubsystem{id: lifecycle.SubsystemCluster},
		stubSubsystem{id: lifecycle.SubsystemAuditProjection},
		stubSubsystem{id: lifecycle.SubsystemGRPC},
	)

	found := false
	for _, sub := range subs {
		if sub.ID() == lifecycle.SubsystemCluster {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("productionSubsystems does not include SubsystemCluster; clusterSub was dropped from the orchestrator wiring")
	}

	if len(subs) != 11 {
		t.Errorf("productionSubsystems returned %d subsystems; want 11 after Phase 3c", len(subs))
	}
}

// TestSubsystemAdminSocketConstantExists verifies that SubsystemAdminSocket
// is defined and distinct from all other SubsystemIDs.
func TestSubsystemAdminSocketConstantExists(t *testing.T) {
	ids := []lifecycle.SubsystemID{
		lifecycle.SubsystemDatabase,
		lifecycle.SubsystemTLS,
		lifecycle.SubsystemABAC,
		lifecycle.SubsystemAuth,
		lifecycle.SubsystemWorld,
		lifecycle.SubsystemPlugins,
		lifecycle.SubsystemSessions,
		lifecycle.SubsystemBootstrap,
		lifecycle.SubsystemGRPC,
		lifecycle.SubsystemEventBus,
		lifecycle.SubsystemAuditProjection,
		lifecycle.SubsystemCluster,
		lifecycle.SubsystemAdminSocket,
	}
	seen := make(map[lifecycle.SubsystemID]bool)
	for _, id := range ids {
		if seen[id] {
			t.Errorf("duplicate SubsystemID value: %v", id)
		}
		seen[id] = true
	}
	if lifecycle.SubsystemAdminSocket.String() == "" {
		t.Error("SubsystemAdminSocket.String() must not be empty")
	}
}
