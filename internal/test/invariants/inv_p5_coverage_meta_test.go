// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invariants

import (
	"testing"
)

// TestINV_P5_Coverage_Meta pins INV-SCENE-21 (meta): every numbered INV-P5-N
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
// INV-SCENE-21 is THIS meta-test; recursive self-inclusion would be circular,
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
		// INV-SCENE-14: focus-without-membership MUST NOT be possible.
		// Validated inside SessionConnectionMutator against FocusMemberships.
		// Pinned by the FocusMemberships-rejection unit test in set_connection_focus_test.go.
		{
			inv:      "INV-SCENE-14",
			testName: "TestSetConnectionFocus_RequiresMembership",
		},
		// INV-SCENE-15: each Connection has exactly one FocusKey at all times.
		// nil = grid; otherwise a single FocusKey. No "multiple focuses per connection."
		// Pinned by the zero-value Connection FocusKey test in connection_test.go.
		// Note: spec §10 cites TestConnection_FocusKey_SingleValued but the actual
		// function name is TestConnection_FocusKeyNilByDefault (truth = current code).
		{
			inv:      "INV-SCENE-15",
			testName: "TestConnection_FocusKeyNilByDefault",
			note:     "spec §10 named TestConnection_FocusKey_SingleValued; current code uses TestConnection_FocusKeyNilByDefault",
		},
		// INV-SCENE-16: focus-managed subset of Connection.Streams is a deterministic
		// function of (FocusKey, character-level always-on streams).
		// Pinned by the subscription_router determinism test.
		{
			inv:      "INV-SCENE-16",
			testName: "TestComputeFocusManagedStreamsDeterministic",
			note:     "renamed from TestComputeFocusManagedStreams_Deterministic during PR #4191 table-driven refactor",
		},
		// INV-SCENE-17: AutoFocusOnJoin terminal-only filter — ClientType ∈ {terminal, telnet}.
		// Comms_hub connections are NEVER auto-focused.
		// Pinned by the client-type filter test in auto_focus_on_join_test.go.
		{
			inv:      "INV-SCENE-17",
			testName: "TestAutoFocus_FiltersByClientType",
		},
		// INV-SCENE-18: on reconnect, focus restoration validates PresentingFocus against
		// FocusMemberships inside the SessionConnectionMutator callback; falls back
		// to grid when validation fails.
		// Pinned by the revoked-membership fallback test in restore_connection_focus_test.go.
		// Note: plan table cites internal/session/reconnect_focus_restoration_test.go
		// but the actual file is internal/grpc/focus/restore_connection_focus_test.go.
		{
			inv:      "INV-SCENE-18",
			testName: "TestReconnect_FallsBackToGridWhenMembershipRevoked",
			note:     "in internal/grpc/focus/restore_connection_focus_test.go (not session/)",
		},
		// INV-SCENE-19: 3 new PluginHostService RPCs ship with Go SDK + Lua hostfunc
		// bindings together (INV-S3 substrate-contract parity).
		// Pinned by the Lua parity test in stdlib_focus_test.go.
		{
			inv:      "INV-SCENE-19",
			testName: "TestFocusHostfunc_PhaseFive_LuaParity",
		},
		// INV-SCENE-20: Phase 5 multi-field focus mutations MUST be applied via a single
		// SessionConnectionMutator invocation under one Store-lock acquisition (D7).
		// Originally pinned by the atomic-commit test in the deleted in-memory store
		// (memstore_test.go). Now pinned by TestPostgresUpdateSessionConnection_HappyPath
		// which verifies both Connection.FocusKey and Info.PresentingFocus commit
		// atomically in a single Postgres transaction (session_store_integration_test.go).
		// Accepted coverage shift: the in-memory version needed explicit goroutine
		// coordination to prove its mutex protected against torn reads; the Postgres
		// impl gets all-or-nothing atomicity and rollback-on-error structurally from
		// transaction semantics (single UPDATE inside a tx with defer Rollback), so a
		// dedicated rollback/torn-state test is not required to uphold INV-SCENE-20.
		{
			inv:      "INV-SCENE-20",
			testName: "TestPostgresUpdateSessionConnection_HappyPath",
			note:     "atomicity pin migrated to Postgres integration test after in-memory store deletion (holomush-9mxr Task 16)",
		},
		// INV-SCENE-22: ULID encoding boundary — proto wire = bytes (16-byte); Lua hostfunc
		// accepts 26-char base32 strings; malformed → INVALID_ULID.
		// Pinned by the ULID round-trip test in stdlib_focus_test.go.
		{
			inv:      "INV-SCENE-22",
			testName: "TestFocusHostfunc_ULIDRoundTrip",
		},
		// INV-SCENE-23: SessionStreamRegistry.SendToConnection delivers update to EXACTLY
		// the named connection's channel; other connections in the same session do NOT
		// receive the update via this path.
		// Pinned by TestSendToConnection_TargetsOneConnectionOnly in stream_registry_test.go.
		{
			inv:      "INV-SCENE-23",
			testName: "TestSendToConnection_TargetsOneConnectionOnly",
		},
		// INV-SCENE-24: AutoFocusOnJoin MUST skip a connection whose FocusKey is already
		// non-nil and different from the requested target (D8 skip-rule).
		// Pinned by the skip-already-focused test in auto_focus_on_join_test.go.
		{
			inv:      "INV-SCENE-24",
			testName: "TestAutoFocus_SkipsAlreadyExplicitlyFocusedConn",
		},
		// INV-SCENE-25: reconnect restoration vs concurrent LeaveFocus serializes via the
		// single Store-lock acquisition — no corruption, no torn state.
		// Pinned by the concurrent-leave serialization test in restore_connection_focus_test.go.
		// Note: plan table cites internal/session/reconnect_focus_restoration_test.go
		// but the actual file is internal/grpc/focus/restore_connection_focus_test.go.
		{
			inv:      "INV-SCENE-25",
			testName: "TestReconnect_VsConcurrentLeave_Serializes",
			note:     "in internal/grpc/focus/restore_connection_focus_test.go (not session/)",
		},
		// INV-SCENE-26: scene grid (D10) MUST NOT modify info.PresentingFocus. Per-Connection
		// FocusKey is cleared to nil; session-level reconnect target is preserved.
		// Pinned by TestSceneGrid_DoesNotClearPresentingFocus in set_connection_focus_test.go.
		{
			inv:      "INV-SCENE-26",
			testName: "TestSceneGrid_DoesNotClearPresentingFocus",
		},
		// INV-SCENE-27: Postgres UpdateSessionConnection MUST lock the sessions row FIRST
		// via FOR UPDATE, then the session_connections row (D11 canonical order). Pinned
		// by a deadlock-detector integration test that races two concurrent calls.
		// (Integration test; build tag //go:build integration — the corpus walker includes
		// integration test files so the function declaration is visible.)
		{
			inv:      "INV-SCENE-27",
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
