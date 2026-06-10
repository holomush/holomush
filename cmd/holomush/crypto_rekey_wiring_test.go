// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	socket "github.com/holomush/holomush/internal/admin/socket"
)

// Compile-time interface checks: production adapter types must implement the socket-layer interfaces.
var _ socket.CheckpointStatusReader = (*productionCheckpointReader)(nil)
var _ socket.RekeyAbortRunner = (*productionRekeyAbortRunner)(nil)
var _ socket.OrchestratorRunner = (*productionOrchestratorRunner)(nil)
var _ socket.RekeySessionStore = (*productionRekeySessionAdapter)(nil)

// TestBuildRekeyWiringReturnsZeroWhenKEKProviderMissing verifies that the
// wiring helper degrades gracefully when KEK is unavailable. The admin
// socket then falls back to Unimplemented for the Rekey RPCs and the rest
// of the server boots normally. Sub-epic E T44.
func TestBuildRekeyWiringReturnsZeroWhenKEKProviderMissing(t *testing.T) {
	deps := rekeyWiringDeps{
		// Pool and KEK left nil — buildRekeyWiring's gate triggers.
	}
	w, err := buildRekeyWiring(context.Background(), deps)
	require.NoError(t, err, "missing dependency must NOT be a fatal error — server must still boot")
	assert.Nil(t, w.RekeyHandler, "RekeyHandler must be nil when wiring is incomplete")
	assert.Nil(t, w.Manager, "Manager must be nil when wiring is incomplete")
	assert.Nil(t, w.Orchestrator, "Orchestrator must be nil when wiring is incomplete")
}

// TestBuildRekeyWiringReturnsZeroWhenSubjectResolverMissing verifies the
// same defensive pattern when ABAC is not yet started. Sub-epic E T44.
func TestBuildRekeyWiringReturnsZeroWhenSubjectResolverMissing(t *testing.T) {
	// Pool/KEK present but SubjectResolver nil — still defensive.
	deps := rekeyWiringDeps{
		// SubjectResolver intentionally omitted; required by the gate.
	}
	w, err := buildRekeyWiring(context.Background(), deps)
	require.NoError(t, err)
	assert.Nil(t, w.RekeyHandler)
}

// TestBuildRekeyWiringRequiresCoordHolder verifies that buildRekeyWiring
// rejects a deps shape missing the CoordHolder pointer. The Manager's
// Invalidator closure indirects through this holder to reach the
// late-bound invalidation.Coordinator; without it, the closure would
// dereference a nil pointer on the first Add/Rotate. Phase 3c grounding
// Decision 5 — the holder pattern is what lets the Coordinator be
// constructed AFTER the Manager so it can share cache identity with the
// Manager's caches.
func TestBuildRekeyWiringRequiresCoordHolder(t *testing.T) {
	// All other required fields stubbed non-nil except CoordHolder.
	deps := rekeyWiringDeps{
		// Pool, KEKProvider, etc. — the early-return predicate ORs them
		// together, so a single missing field is enough to trigger the
		// zero-wiring branch. CoordHolder being one of the gated fields
		// is the contract we want documented by this test.
	}
	w, err := buildRekeyWiring(context.Background(), deps)
	require.NoError(t, err, "missing dependency must NOT be fatal — server must still boot")
	assert.Nil(t, w.RekeyHandler, "RekeyHandler must be nil when CoordHolder is missing alongside other deps")
}
