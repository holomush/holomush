// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSpecAmendmentsLanded enforces INV-B-AMEND: every master-spec
// amendment listed in the sub-epic B design spec's "Master-spec
// amendments inventory" must leave a detectable fingerprint in the
// master spec text. Catches "code without amendments" and
// "amendments-with-drifted-text" failure modes.
func TestSpecAmendmentsLanded(t *testing.T) {
	masterSpec := readSpec(t,
		"docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md")

	// Each fingerprint is a distinctive substring that MUST appear
	// post-amendment. Keyed by amendment ID for diagnostics.
	fingerprints := map[string]string{
		"A1":  "Compromised in-game admin with crypto.operator capability",
		"A2":  "Single-control break-glass authentication uses two factors",
		"A3":  "audit.<game>.system.crypto_policy",
		"A4":  "policy_hash:        <bytes>",
		"A5":  "DENY_NOT_ENROLLED",
		"A6":  "5.9.1 `crypto.operator` capability",
		"A7":  "admin role + crypto.operator capability + TOTP factor",
		"A8":  "6.3.1 Dual-control protocol",
		"A9":  "admin-creds + TOTP + dual-control",
		"A10": "`policy_set` chain verification failure on startup",
		"A11": "DENY_DUAL_CONTROL_REQUIRED",
		"A12": "`admin_approvals` table (sub-epic D)",
		"A13": "admin accounts who hold the `crypto.operator` capability",
		"A14": "<admin player_id>",
	}

	// Forbidden substrings: pre-amendment text that MUST NOT remain.
	forbiddenAfterAmendment := map[string]string{
		"A1-stale":       "Compromised in-game wizard ", // trailing space avoids matching new text
		"A5-stale-step3": "If TOTP enrolled for this player: prompt for 6-digit code",
		"A13-stale":      "Decide on TOTP enrollment for wizard accounts",
		"A14-stale":      "<wizard player_id>",
	}

	for id, fp := range fingerprints {
		assert.Contains(t, masterSpec, fp,
			"INV-B-AMEND: amendment %s fingerprint missing from master spec", id)
	}
	for id, fp := range forbiddenAfterAmendment {
		assert.NotContains(t, masterSpec, fp,
			"INV-B-AMEND: pre-amendment text %s still present in master spec", id)
	}
}

// TestDecompositionSpecDriftFixesLanded enforces that B's PR also
// carries the drift-fix amendments to the decomposition spec table
// (§11.3 step 5 row + §4.6 line 833 row).
func TestDecompositionSpecDriftFixesLanded(t *testing.T) {
	decomp := readSpec(t,
		"docs/superpowers/specs/2026-05-07-event-payload-crypto-phase5-decomposition.md")

	assert.Contains(t, decomp, "§11.3 step 5 (line 2185)",
		"decomposition spec must point A13 at §11.3, not §12")
	assert.Contains(t, decomp, "§4.6 line 833",
		"decomposition spec must include the §4.6 line 833 amendment row")
	assert.NotContains(t, decomp,
		"Strike \"Decide on TOTP enrollment for wizard accounts\"",
		"decomposition spec must not retain the misattributed §12 strike text")
}

// TestSpecAmendmentsLandedSubEpicD enforces INV-D-AMEND: every master-spec
// amendment listed in the sub-epic D design spec §10 amendments table must
// leave a detectable fingerprint in the master spec text. Also negate-asserts
// that removed text (PromptFunc, RequireDualControl, OSUser string) is gone.
func TestSpecAmendmentsLandedSubEpicD(t *testing.T) {
	masterSpec := readSpec(t,
		"docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md")

	// Each fingerprint is a distinctive substring that MUST appear
	// post-amendment. Keyed by amendment ID for diagnostics.
	fingerprints := map[string]string{
		"D1_AuthenticateSignature":  "Authenticate(ctx context.Context, req AuthRequest) (OperatorIdentity, error)",
		"D2_RoleStorePlayerHasRole": "RoleStore.PlayerHasRole",
		"D3_ChainSubject":           "events.<game>.system.crypto_policy",
		"D4_OpArgsHashAlgorithm":    "SHA-256(proto.MarshalOptions{Deterministic: true}.Marshal(args))",
		"D5_DenyNotAdminRole":       "DENY_NOT_ADMIN_ROLE",
		"D6_DenySessionExpired":     "DENY_SESSION_EXPIRED",
		"D7_DenyDualControlSelf":    "DENY_DUAL_CONTROL_SELF",
	}

	// Forbidden substrings: pre-amendment text that MUST NOT remain.
	forbiddenAfterAmendment := []string{
		"RequireDualControl(ctx context.Context, primary",
		"prompt PromptFunc",
		"OperatorIdentity.OSUser",
		"OSUser                  string",
	}

	for id, fp := range fingerprints {
		assert.Contains(t, masterSpec, fp,
			"INV-D-AMEND: amendment %s fingerprint missing from master spec", id)
	}
	for _, sub := range forbiddenAfterAmendment {
		assert.NotContains(t, masterSpec, sub,
			"INV-D-AMEND: pre-amendment text still present in master spec: %s", sub)
	}
}

// TestSpecAmendmentsLandedSubEpicE enforces INV-E-AMEND: every master-spec
// amendment listed in the sub-epic E design spec §9 amendments table must
// leave a detectable fingerprint in the master spec text. Also negate-asserts
// that superseded text (audit.<game>.system.rekey subject prefix) is gone.
func TestSpecAmendmentsLandedSubEpicE(t *testing.T) {
	masterSpec := readSpec(t,
		"docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md")

	// Each fingerprint is a distinctive substring that MUST appear
	// post-amendment. Keyed by amendment ID for diagnostics.
	fingerprints := map[string]string{
		// A1: §3 Rekey flow — status/list/abort/resume subcommands + --purge-hot deferred to holomush-ujuv
		"E_A1_rekey_status_cmd":   "holomush crypto rekey status",
		"E_A1_purge_hot_deferred": "holomush-ujuv",
		// A2: §4.6 audit shapes — rekey_chain field added
		"E_A2_rekey_chain_field": "rekey_chain:",
		// A3: §4.6.X "Audit-chain integrity" new subsection
		"E_A3_auditchain_subsection": "Audit-chain integrity",
		// A4: §4.6 line 830 subject prefix reconciliation
		"E_A4_events_rekey_subject": "events.<game>.system.rekey.<context_type>.<context_id>",
		// A5: §6.3 mechanics — Phase 4 no-op clarification
		"E_A5_phase4_noop": "Phase 4 introduces no status transitions",
		// A6: §6.3.2 Resume, abort, force-destroy new subsection
		"E_A6_resume_abort_subsection": "6.3.2",
		// A7: §6.3.3 Operator UX commitments new subsection
		"E_A7_operator_ux_subsection": "6.3.3",
		// A8: §6.3.1 INV-E16 resume bypass note
		"E_A8_resume_bypass_inv_e16": "INV-E16",
		// A9: §8.4 SourceResolver reference replaces pseudocode
		"E_A9_source_resolver_ref": "SourceResolver",
		// A10: §8.5 rekey chain note
		"E_A10_rekey_chain_in_8_5": "events.<game>.system.rekey",
		// A11: §10 DENY codes — new rekey codes
		"E_A11_deny_rekey_in_progress": "DEK_REKEY_ALREADY_IN_PROGRESS",
		// A12: §10 lifecycle failures — force-destroy row
		"E_A12_force_destroy_lifecycle": "Force-destroy used",
		// A13: §10 audit emission failures — boot verifier row
		"E_A13_audit_chain_broken_boot": "AUDIT_CHAIN_BROKEN",
		// A14: §11.1 Phase 5 row — crypto_rekey_checkpoints reference
		"E_A14_phase5_rekey_checkpoints": "crypto_rekey_checkpoints",
		// A15: §12 open questions — holomush-ojw1.6 permanent reference
		"E_A15_ojw1_6_ref": "holomush-ojw1.6",
		// A16: §10 line 900-901 INV-ACCESS-7 ABAC denial extended to events.*.system.*
		"E_A16_abac_events_system_denial": "events.*.system.*",
	}

	// Forbidden substrings: pre-amendment text that MUST NOT remain.
	forbiddenAfterAmendment := map[string]string{
		// §4.6 line 830: old audit.* prefix for rekey subject superseded by events.*
		"E_A4_stale_audit_rekey_subject": "`audit.<game>.system.rekey.<context_type>.<context_id>`",
	}

	for id, fp := range fingerprints {
		assert.Contains(t, masterSpec, fp,
			"INV-E-AMEND: amendment %s fingerprint missing from master spec", id)
	}
	for id, fp := range forbiddenAfterAmendment {
		assert.NotContains(t, masterSpec, fp,
			"INV-E-AMEND: pre-amendment text %s still present in master spec", id)
	}
}

// readSpec resolves the path relative to repo root by walking up from
// the current test file location. Required because Go tests run with
// CWD set to the package directory, not the repo root.
func readSpec(t *testing.T, relPath string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "could not determine caller location")
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	fullPath := filepath.Join(repoRoot, relPath)
	bytes, err := os.ReadFile(fullPath)
	require.NoError(t, err, "could not read spec at %s", fullPath)
	return string(bytes)
}
