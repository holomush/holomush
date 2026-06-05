// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDecideResumeEntry_NonTerminalStatusCoverage asserts that every
// non-terminal FSM state has a defined resume entry decision. The Resume
// dispatcher (Orchestrator.Run) consumes this decision to pick the first
// RunPhaseN to invoke; missing coverage would manifest as a silent
// fall-through to a default error path (INV-CRYPTO-91 violation: never
// re-enter Phase 1 when a checkpoint exists).
//
// This is a unit-level meta-test: it does NOT touch the database; it
// exercises the pure-functional decision helper.
func TestDecideResumeEntry_NonTerminalStatusCoverage(t *testing.T) {
	cases := []struct {
		name             string
		status           CheckpointStatus
		hasMissing       bool
		forceDestroy     bool
		wantEntry        resumeEntry
		wantManualNeeded bool
	}{
		{"pending", CheckpointStatusPending, false, false, resumeEntryPhase2, false},
		{"phase1_auth", CheckpointStatusPhase1Auth, false, false, resumeEntryPhase2, false},
		{"phase2_mint_dek", CheckpointStatusPhase2MintDEK, false, false, resumeEntryPhase3, false},
		{"phase3_reencrypt_cold", CheckpointStatusPhase3ReencryptCold, false, false, resumeEntryPhase3, false},
		// phase5_invalidate split by missing_members + force_destroy.
		{"phase5_clean_no_missing", CheckpointStatusPhase5Invalidate, false, false, resumeEntryPhase6, false},
		{"phase5_timeout_retry", CheckpointStatusPhase5Invalidate, true, false, resumeEntryPhase5Retry, false},
		{"phase5_timeout_force_destroy", CheckpointStatusPhase5Invalidate, true, true, resumeEntryPhase5ForceDestroy, false},
		{"phase6_destroy_old", CheckpointStatusPhase6DestroyOld, false, false, resumeEntryPhase7, false},
		// phase7_audit: previous run advanced status but emit failed. The
		// FSM does not allow re-entry into RunPhase7 from this state (its
		// precondition is phase6_destroy_old). Resume returns manual-recovery.
		{"phase7_audit", CheckpointStatusPhase7Audit, false, false, resumeEntryManual, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry, manual := decideResumeEntry(tc.status, tc.hasMissing, tc.forceDestroy)
			require.Equal(t, tc.wantEntry, entry, "resumeEntry for status=%s missing=%v force=%v",
				tc.status, tc.hasMissing, tc.forceDestroy)
			require.Equal(t, tc.wantManualNeeded, manual,
				"manual-recovery flag for status=%s", tc.status)
		})
	}
}

// TestDecideResumeEntry_TerminalStates asserts complete and aborted are
// recognised as terminal (no phase dispatch).
func TestDecideResumeEntry_TerminalStates(t *testing.T) {
	entry, _ := decideResumeEntry(CheckpointStatusComplete, false, false)
	require.Equal(t, resumeEntryComplete, entry,
		"INV-CRYPTO-103: complete checkpoint is idempotent no-op")

	entry, _ = decideResumeEntry(CheckpointStatusAborted, false, false)
	require.Equal(t, resumeEntryAborted, entry,
		"aborted checkpoint surfaces terminal error")
}
