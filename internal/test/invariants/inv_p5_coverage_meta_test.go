// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invariants

import (
	"testing"
)

// TestINV_P5_Coverage_Meta pins INV-P5-8 (meta): every numbered INV-P5-N
// declaration in the Phase 5 design spec MUST have at least one named
// test in the Go corpus.
//
// For each INV-P5-N this test verifies that the named test referenced in
// the coverage table (spec §10) exists somewhere under the repo root as a
// top-level Test* function in a *_test.go file.
//
// If this test FAILS:
//   - Either the test was renamed/deleted without updating this table, OR
//   - A new INV-P5-N invariant was declared without a corresponding test.
//
// Fix by updating the cases slice AND the spec's §10 invariant table in
// lockstep — the two MUST agree at all times.
//
// INV-P5-8 is THIS meta-test; recursive self-inclusion would be circular,
// so it is excluded from the cases table and is self-evidently covered by
// its own execution.
//
// Ginkgo integration tests are registered under a top-level TestXxx suite
// entry; this meta-test maps invariants to suite entry names rather than
// Ginkgo Describe labels. The Describe label remains greppable in the spec
// for invariant traceability.
//
// Same shape as internal/test/invariants/inv_p4_coverage_meta_test.go
// (Phase 4 T28). Uses pure-Go go/parser walk — no external rg dependency —
// so the test runs in any environment with a Go toolchain.
func TestINV_P5_Coverage_Meta(t *testing.T) {
	t.Parallel()

	cases := []struct {
		inv      string
		testName string
		note     string
	}{
		// INV-P5-1: focus-without-membership MUST NOT be possible.
		// Validated inside SessionConnectionMutator against FocusMemberships.
		// Pinned by the FocusMemberships-rejection unit test in set_connection_focus_test.go.
		{
			inv:      "INV-P5-1",
			testName: "TestSetConnectionFocus_RequiresMembership",
		},
		// INV-P5-2: each Connection has exactly one FocusKey at all times.
		// nil = grid; otherwise a single FocusKey. No "multiple focuses per connection."
		// Pinned by the zero-value Connection FocusKey test in connection_test.go.
		// Note: spec §10 cites TestConnection_FocusKey_SingleValued but the actual
		// function name is TestConnection_FocusKeyNilByDefault (truth = current code).
		{
			inv:      "INV-P5-2",
			testName: "TestConnection_FocusKeyNilByDefault",
			note:     "spec §10 named TestConnection_FocusKey_SingleValued; current code uses TestConnection_FocusKeyNilByDefault",
		},
		// INV-P5-3: focus-managed subset of Connection.Streams is a deterministic
		// function of (FocusKey, character-level always-on streams).
		// Pinned by the subscription_router determinism test.
		{
			inv:      "INV-P5-3",
			testName: "TestComputeFocusManagedStreamsDeterministic",
			note:     "renamed from TestComputeFocusManagedStreams_Deterministic during PR #4191 table-driven refactor",
		},
		// INV-P5-4: AutoFocusOnJoin terminal-only filter — ClientType ∈ {terminal, telnet}.
		// Comms_hub connections are NEVER auto-focused.
		// Pinned by the client-type filter test in auto_focus_on_join_test.go.
		{
			inv:      "INV-P5-4",
			testName: "TestAutoFocus_FiltersByClientType",
		},
		// INV-P5-5: on reconnect, focus restoration validates PresentingFocus against
		// FocusMemberships inside the SessionConnectionMutator callback; falls back
		// to grid when validation fails.
		// Pinned by the revoked-membership fallback test in restore_connection_focus_test.go.
		// Note: plan table cites internal/session/reconnect_focus_restoration_test.go
		// but the actual file is internal/grpc/focus/restore_connection_focus_test.go.
		{
			inv:      "INV-P5-5",
			testName: "TestReconnect_FallsBackToGridWhenMembershipRevoked",
			note:     "in internal/grpc/focus/restore_connection_focus_test.go (not session/)",
		},
		// INV-P5-6: 3 new PluginHostService RPCs ship with Go SDK + Lua hostfunc
		// bindings together (INV-S3 substrate-contract parity).
		// Pinned by the Lua parity test in stdlib_focus_test.go.
		{
			inv:      "INV-P5-6",
			testName: "TestFocusHostfunc_PhaseFive_LuaParity",
		},
		// INV-P5-7: Phase 5 multi-field focus mutations MUST be applied via a single
		// SessionConnectionMutator invocation under one Store-lock acquisition (D7).
		// Pinned by the atomic-commit test in memstore_test.go.
		// Note: spec §10 also cites TestSetConnectionFocus_RoutesViaCoordinator and
		// TestSessionConnectionMutator_OnlyConstructibleInGrpcFocus but the
		// corresponding test is TestUpdateSessionConnection_AtomicCommit (truth = current code
		// per plan table; RoutesViaCoordinator was not implemented in coordinator_test.go).
		{
			inv:      "INV-P5-7",
			testName: "TestUpdateSessionConnection_AtomicCommit",
			note:     "atomicity pin in memstore_test.go; spec §10 also cited RoutesViaCoordinator (not present in corpus)",
		},
		// INV-P5-9: ULID encoding boundary — proto wire = bytes (16-byte); Lua hostfunc
		// accepts 26-char base32 strings; malformed → INVALID_ULID.
		// Pinned by the ULID round-trip test in stdlib_focus_test.go.
		{
			inv:      "INV-P5-9",
			testName: "TestFocusHostfunc_ULIDRoundTrip",
		},
		// INV-P5-10: SessionStreamRegistry.SendToConnection delivers update to EXACTLY
		// the named connection's channel; other connections in the same session do NOT
		// receive the update via this path.
		// Pinned by TestSendToConnection_TargetsOneConnectionOnly in stream_registry_test.go.
		{
			inv:      "INV-P5-10",
			testName: "TestSendToConnection_TargetsOneConnectionOnly",
		},
		// INV-P5-11: AutoFocusOnJoin MUST skip a connection whose FocusKey is already
		// non-nil and different from the requested target (D8 skip-rule).
		// Pinned by the skip-already-focused test in auto_focus_on_join_test.go.
		{
			inv:      "INV-P5-11",
			testName: "TestAutoFocus_SkipsAlreadyExplicitlyFocusedConn",
		},
		// INV-P5-12: reconnect restoration vs concurrent LeaveFocus serializes via the
		// single Store-lock acquisition — no corruption, no torn state.
		// Pinned by the concurrent-leave serialization test in restore_connection_focus_test.go.
		// Note: plan table cites internal/session/reconnect_focus_restoration_test.go
		// but the actual file is internal/grpc/focus/restore_connection_focus_test.go.
		{
			inv:      "INV-P5-12",
			testName: "TestReconnect_VsConcurrentLeave_Serializes",
			note:     "in internal/grpc/focus/restore_connection_focus_test.go (not session/)",
		},
		// INV-P5-13: scene grid (D10) MUST NOT modify info.PresentingFocus. Per-Connection
		// FocusKey is cleared to nil; session-level reconnect target is preserved.
		// Pinned by TestSceneGrid_DoesNotClearPresentingFocus in set_connection_focus_test.go.
		{
			inv:      "INV-P5-13",
			testName: "TestSceneGrid_DoesNotClearPresentingFocus",
		},
		// INV-P5-14: Postgres UpdateSessionConnection MUST lock the sessions row FIRST
		// via FOR UPDATE, then the session_connections row (D11 canonical order). Pinned
		// by a deadlock-detector integration test that races two concurrent calls.
		// (Integration test; build tag //go:build integration — the corpus walker includes
		// integration test files so the function declaration is visible.)
		{
			inv:      "INV-P5-14",
			testName: "TestPostgresUpdateSessionConnection_LockAcquisitionOrder_NoDeadlock",
			note:     "integration test in internal/store/session_store_integration_test.go; build tag integration",
		},
	}

	repoRoot := findRepoRootFromInvariants(t)
	testNames := collectTestFuncNamesFromInvariants(t, repoRoot)

	for _, tc := range cases {
		tc := tc
		name := tc.inv
		if tc.note != "" {
			name = tc.inv + "/" + tc.testName
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, ok := testNames[tc.testName]; !ok {
				t.Fatalf("%s: named test %q NOT FOUND under %s\n  note: %s",
					tc.inv, tc.testName, repoRoot, tc.note)
			}
		})
	}
}

// Note: findRepoRootFromInvariants, collectTestFuncNamesFromInvariants, and
// isRunnableGoTestInvariants are defined in inv_p4_coverage_meta_test.go in
// this package and are shared by both the Phase 4 and Phase 5 meta-tests.
// The helpers are named with the "Invariants" suffix to avoid collisions with
// identically-structured helpers in other packages (Phase 7 meta-test etc.).
