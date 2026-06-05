// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

//go:generate go run github.com/holomush/holomush/cmd/internal/fsmdiagram

import "github.com/samber/oops"

// CheckpointStatus is the FSM state for a crypto_rekey_checkpoints row.
// It tracks the Rekey orchestrator's 7-phase progression.
//
// State graph (→ = valid transition):
//
//	Pending → Phase1Auth → Phase2MintDEK → Phase3ReencryptCold
//	        → Phase5Invalidate → Phase6DestroyOld → Phase7Audit → Complete
//
//	Any non-terminal state → Aborted  (operator-abort or sweep-TTL-abort)
//
// INV-CRYPTO-88: Transitions MUST follow validTransitions only (monotone forward).
// INV-CRYPTO-89: Complete and Aborted are absorbing terminal states.
type CheckpointStatus string

const (
	// CheckpointStatusPending is the initial state when a checkpoint row is
	// opened (Phase 1 auth not yet begun).
	CheckpointStatusPending CheckpointStatus = "pending"

	// CheckpointStatusPhase1Auth indicates Phase 1 (authenticate + authorize)
	// is in progress.
	CheckpointStatusPhase1Auth CheckpointStatus = "phase1_auth"

	// CheckpointStatusPhase2MintDEK indicates Phase 2 (mint new DEK) is in
	// progress.
	CheckpointStatusPhase2MintDEK CheckpointStatus = "phase2_mint_dek"

	// CheckpointStatusPhase3ReencryptCold indicates Phase 3 (re-encrypt cold
	// tier / events_audit rows) is in progress. This phase is resumable via
	// the cursor stored in the checkpoint row (INV-CRYPTO-94/INV-CRYPTO-95).
	CheckpointStatusPhase3ReencryptCold CheckpointStatus = "phase3_reencrypt_cold"

	// CheckpointStatusPhase5Invalidate indicates Phase 5 (synchronous
	// cross-replica cache invalidation) is in progress.
	// Note: Phase 4 (hot-tier strategy) carries no separate FSM state because
	// it is a fire-and-forget policy step that does not block progression.
	CheckpointStatusPhase5Invalidate CheckpointStatus = "phase5_invalidate"

	// CheckpointStatusPhase6DestroyOld indicates Phase 6 (soft-delete of the
	// old DEK row) is in progress.
	CheckpointStatusPhase6DestroyOld CheckpointStatus = "phase6_destroy_old"

	// CheckpointStatusPhase7Audit indicates Phase 7 (emit chained rekey audit
	// event) is in progress.
	CheckpointStatusPhase7Audit CheckpointStatus = "phase7_audit"

	// CheckpointStatusComplete is the terminal success state. Absorbing:
	// no transitions out (INV-CRYPTO-89).
	CheckpointStatusComplete CheckpointStatus = "complete"

	// CheckpointStatusAborted is the terminal failure/abort state. Set by
	// operator-initiated abort (INV-CRYPTO-104) or sweep TTL-expiry (INV-CRYPTO-105).
	// Absorbing: no transitions out (INV-CRYPTO-89).
	CheckpointStatusAborted CheckpointStatus = "aborted"
)

// validTransitions is the authoritative transition table. It is the single
// source of truth for AssertTransitionAllowed, AllCheckpointStatuses, and the
// fsmdiagram codegen tool.
//
// Rule: every forward phase transition plus every non-terminal → Aborted edge
// is listed here. Terminal states (Complete, Aborted) have no outgoing edges.
var validTransitions = map[CheckpointStatus][]CheckpointStatus{
	CheckpointStatusPending:             {CheckpointStatusPhase1Auth, CheckpointStatusAborted},
	CheckpointStatusPhase1Auth:          {CheckpointStatusPhase2MintDEK, CheckpointStatusAborted},
	CheckpointStatusPhase2MintDEK:       {CheckpointStatusPhase3ReencryptCold, CheckpointStatusAborted},
	CheckpointStatusPhase3ReencryptCold: {CheckpointStatusPhase5Invalidate, CheckpointStatusAborted},
	CheckpointStatusPhase5Invalidate:    {CheckpointStatusPhase6DestroyOld, CheckpointStatusAborted},
	CheckpointStatusPhase6DestroyOld:    {CheckpointStatusPhase7Audit, CheckpointStatusAborted},
	CheckpointStatusPhase7Audit:         {CheckpointStatusComplete, CheckpointStatusAborted},
	// Terminal states — no outgoing transitions (absorbing per INV-CRYPTO-89).
	CheckpointStatusComplete: {},
	CheckpointStatusAborted:  {},
}

// AllCheckpointStatuses returns every declared CheckpointStatus value in a
// deterministic order (Pending first, then phases 1–7 in order, then
// Complete, then Aborted). Used by tests and the fsmdiagram codegen tool.
func AllCheckpointStatuses() []CheckpointStatus {
	return []CheckpointStatus{
		CheckpointStatusPending,
		CheckpointStatusPhase1Auth,
		CheckpointStatusPhase2MintDEK,
		CheckpointStatusPhase3ReencryptCold,
		CheckpointStatusPhase5Invalidate,
		CheckpointStatusPhase6DestroyOld,
		CheckpointStatusPhase7Audit,
		CheckpointStatusComplete,
		CheckpointStatusAborted,
	}
}

// ValidTransitionPairs returns every valid (from, to) pair as a [][2]CheckpointStatus
// slice. Used by tests and the fsmdiagram codegen tool.
func ValidTransitionPairs() [][2]CheckpointStatus {
	var pairs [][2]CheckpointStatus
	for _, from := range AllCheckpointStatuses() {
		for _, to := range validTransitions[from] {
			pairs = append(pairs, [2]CheckpointStatus{from, to})
		}
	}
	return pairs
}

// AssertTransitionAllowed returns nil if the (from → to) transition is declared
// in validTransitions, or an oops error with code
// DEK_REKEY_FSM_INVALID_TRANSITION otherwise.
//
// INV-CRYPTO-88: CheckpointRepo.UpdateStatus MUST call AssertTransitionAllowed before
// issuing the CAS UPDATE; the CAS provides the concurrent-write guard but the
// FSM guard is the semantic correctness gate.
func AssertTransitionAllowed(from, to CheckpointStatus) error {
	targets, ok := validTransitions[from]
	if !ok {
		return oops.Code("DEK_REKEY_FSM_INVALID_TRANSITION").
			With("from", string(from)).
			With("to", string(to)).
			Errorf("CheckpointStatus %q is not a declared state", from)
	}
	for _, t := range targets {
		if t == to {
			return nil
		}
	}
	return oops.Code("DEK_REKEY_FSM_INVALID_TRANSITION").
		With("from", string(from)).
		With("to", string(to)).
		Errorf("transition %s → %s is not allowed by the CheckpointStatus FSM (INV-CRYPTO-88)", from, to)
}

// IsTerminal reports whether s is an absorbing terminal state (INV-CRYPTO-89).
// Terminal states have no outgoing transitions: Complete and Aborted.
func (s CheckpointStatus) IsTerminal() bool {
	return s == CheckpointStatusComplete || s == CheckpointStatusAborted
}

// String implements fmt.Stringer for readable log and error output.
func (s CheckpointStatus) String() string { return string(s) }

// Diagram returns a Mermaid stateDiagram-v2 string derived from
// validTransitions. It is the canonical source for docs. The fsmdiagram
// codegen tool calls this at build time and writes the output to a
// markdown file.
func Diagram() string {
	var b []byte
	b = append(b, "stateDiagram-v2\n"...)
	b = append(b, "    [*] --> pending\n"...)
	for _, from := range AllCheckpointStatuses() {
		for _, to := range validTransitions[from] {
			b = append(b, "    "...)
			b = append(b, string(from)...)
			b = append(b, " --> "...)
			b = append(b, string(to)...)
			b = append(b, '\n')
		}
	}
	// Mark terminal states.
	b = append(b, "    complete --> [*]\n"...)
	b = append(b, "    aborted --> [*]\n"...)
	return string(b)
}
