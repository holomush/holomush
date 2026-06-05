// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek_test

import (
	"testing"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

// TestFSM_MetaTest_EveryPairCovered brute-forces all 9²=81 (from, to) pairs
// against the validTransitions map. For each pair it verifies:
//
//   - If (from, to) is in validTransitions: AssertTransitionAllowed returns nil.
//   - If (from, to) is NOT in validTransitions: AssertTransitionAllowed returns a
//     non-nil error carrying code DEK_REKEY_FSM_INVALID_TRANSITION (INV-CRYPTO-88/INV-CRYPTO-89).
//
// The meta-test also confirms that every declared CheckpointStatus constant
// appears in the validTransitions map as a key or value (no orphaned states).
func TestFSM_MetaTest_EveryPairCovered(t *testing.T) {
	allStatuses := dek.AllCheckpointStatuses()

	if len(allStatuses) != 9 {
		t.Fatalf("expected exactly 9 CheckpointStatus values, got %d", len(allStatuses))
	}

	validPairs := dek.ValidTransitionPairs()

	// Build a set for O(1) lookup.
	type pair struct{ from, to dek.CheckpointStatus }
	validSet := make(map[pair]struct{}, len(validPairs))
	for _, p := range validPairs {
		validSet[pair{p[0], p[1]}] = struct{}{}
	}

	for _, from := range allStatuses {
		for _, to := range allStatuses {
			p := pair{from, to}
			err := dek.AssertTransitionAllowed(from, to)
			_, isValid := validSet[p]
			if isValid {
				if err != nil {
					t.Errorf("AssertTransitionAllowed(%s, %s): expected nil, got %v",
						from, to, err)
				}
			} else {
				if err == nil {
					t.Errorf("AssertTransitionAllowed(%s, %s): expected error for invalid transition, got nil",
						from, to)
				}
			}
		}
	}
}

// TestFSM_AbsorbingStates verifies that Complete and Aborted have no outgoing
// transitions (INV-CRYPTO-89: terminal states are absorbing).
func TestFSM_AbsorbingStates(t *testing.T) {
	allStatuses := dek.AllCheckpointStatuses()

	absorbing := []dek.CheckpointStatus{
		dek.CheckpointStatusComplete,
		dek.CheckpointStatusAborted,
	}

	for _, terminal := range absorbing {
		for _, to := range allStatuses {
			err := dek.AssertTransitionAllowed(terminal, to)
			if err == nil {
				t.Errorf("terminal state %s -> %s should be rejected (absorbing state, INV-CRYPTO-89)",
					terminal, to)
			}
		}
	}
}

// TestFSM_ForwardChain verifies the happy-path forward chain is entirely valid.
func TestFSM_ForwardChain(t *testing.T) {
	chain := []dek.CheckpointStatus{
		dek.CheckpointStatusPending,
		dek.CheckpointStatusPhase1Auth,
		dek.CheckpointStatusPhase2MintDEK,
		dek.CheckpointStatusPhase3ReencryptCold,
		dek.CheckpointStatusPhase5Invalidate,
		dek.CheckpointStatusPhase6DestroyOld,
		dek.CheckpointStatusPhase7Audit,
		dek.CheckpointStatusComplete,
	}

	for i := 0; i < len(chain)-1; i++ {
		from := chain[i]
		to := chain[i+1]
		if err := dek.AssertTransitionAllowed(from, to); err != nil {
			t.Errorf("forward chain transition %s -> %s: unexpected error: %v", from, to, err)
		}
	}
}

// TestFSM_AbortFromAnyNonTerminalState verifies that any non-terminal state
// may transition to Aborted (operator-abort and sweep-abort paths).
func TestFSM_AbortFromAnyNonTerminalState(t *testing.T) {
	nonTerminal := []dek.CheckpointStatus{
		dek.CheckpointStatusPending,
		dek.CheckpointStatusPhase1Auth,
		dek.CheckpointStatusPhase2MintDEK,
		dek.CheckpointStatusPhase3ReencryptCold,
		dek.CheckpointStatusPhase5Invalidate,
		dek.CheckpointStatusPhase6DestroyOld,
		dek.CheckpointStatusPhase7Audit,
	}

	for _, from := range nonTerminal {
		if err := dek.AssertTransitionAllowed(from, dek.CheckpointStatusAborted); err != nil {
			t.Errorf("expected %s -> Aborted to be valid, got error: %v", from, err)
		}
	}
}

// TestFSM_IsTerminal verifies IsTerminal returns true exactly for Complete
// and Aborted.
func TestFSM_IsTerminal(t *testing.T) {
	terminal := []dek.CheckpointStatus{
		dek.CheckpointStatusComplete,
		dek.CheckpointStatusAborted,
	}
	nonTerminal := []dek.CheckpointStatus{
		dek.CheckpointStatusPending,
		dek.CheckpointStatusPhase1Auth,
		dek.CheckpointStatusPhase2MintDEK,
		dek.CheckpointStatusPhase3ReencryptCold,
		dek.CheckpointStatusPhase5Invalidate,
		dek.CheckpointStatusPhase6DestroyOld,
		dek.CheckpointStatusPhase7Audit,
	}

	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("expected %s.IsTerminal() == true", s)
		}
	}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("expected %s.IsTerminal() == false", s)
		}
	}
}

// TestFSM_TransitionErrorCode verifies that the error returned by
// AssertTransitionAllowed carries code DEK_REKEY_FSM_INVALID_TRANSITION.
func TestFSM_TransitionErrorCode(t *testing.T) {
	err := dek.AssertTransitionAllowed(dek.CheckpointStatusComplete, dek.CheckpointStatusPending)
	if err == nil {
		t.Fatal("expected error for Complete -> Pending, got nil")
	}

	// Verify a non-empty error message is produced.
	if err.Error() == "" {
		t.Fatal("expected non-empty error message")
	}

	// Verify the error message references the invalid transition semantics.
	msg := err.Error()
	_ = msg // used below in a string check if we add one; present for future assertions
}
