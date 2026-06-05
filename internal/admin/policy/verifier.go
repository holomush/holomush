// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus/audit/chain"
)

// VerifyChain validates the integrity of the policy_set chain for one
// policy_name (identified by subject). Per INV-CRYPTO-77/INV-CRYPTO-78/INV-CRYPTO-79. Reads
// events_audit ORDER BY js_seq via the auditchain primitive, walks the
// chain, and recomputes each event's policy_hash via the chain handler's
// canonicalize step.
//
// Post Phase 5 sub-epic E refactor: this is a thin shim over
// [chain.Verifier.VerifyScope] backed by [chain.NewPostgresRepo]. The
// per-chain canonicalize / scope / hash extractors live as standalone
// functions registered via [PolicySetHandlerFor] (chain.go). D's INV-CRYPTO-77
// PrevHash empty-form → nil normalization is preserved in
// canonicalizePolicySetPayload.
//
// Empty-chain handling cross-checks against the persistent chain-init
// signal in bootstrap_metadata (chain_state.go) via
// [chain.Repo.ChainInitialized]. On first boot the signal is absent and
// an empty chain is permitted (the emitter will write the genesis row and
// mark the signal). On any subsequent boot the signal is present, so an
// empty audit row-set is treated as full-chain truncation and surfaces as
// AUDIT_CHAIN_TRUNCATED.
//
// Returns a typed AUDIT_CHAIN_* error on any integrity failure, wrapped in
// POLICY_CHAIN_VERIFY_FAILED to identify the policy-set chain at the
// caller boundary. INV-CRYPTO-77/INV-CRYPTO-78/INV-CRYPTO-79 invariants are generalized to
// AUDIT_CHAIN_BROKEN_GENESIS / AUDIT_CHAIN_BROKEN_LINK /
// AUDIT_CHAIN_HASH_MISMATCH respectively.
//
// The subject and policyName parameters are both retained for D
// backward-compat. The scope walked by the auditchain primitive is
// derived from the subject (the suffix after the chain's subject prefix);
// the policyName parameter is used to construct the chain handler's
// gameID-parameterized prefix. When subject and policyName disagree at
// the suffix level, the test fixtures' "loose" subject is honored — the
// chain handler's INV-CRYPTO-114 ScopeFromPayload cross-check still rejects
// payload-level mismatches.
func VerifyChain(ctx context.Context, pool *pgxpool.Pool, subject, policyName string) error {
	gameID, scope, err := parsePolicySubject(subject)
	if err != nil {
		return oops.Code("POLICY_CHAIN_VERIFY_FAILED").
			With("subject", subject).
			With("policy_name", policyName).Wrap(err)
	}
	// If the subject's tail differs from policyName, prefer the tail (the
	// suffix the chain primitive will walk) — D's pre-refactor verifier
	// queried by subject directly, so any divergence between policyName and
	// the subject suffix was effectively ignored. We preserve that behavior.
	_ = policyName // retained for backward-compat in the API surface
	h := PolicySetHandlerFor(gameID)
	v := chain.NewVerifier(chain.NewPostgresRepo(pool))
	if err := v.VerifyScope(ctx, h, scope); err != nil {
		return oops.Code("POLICY_CHAIN_VERIFY_FAILED").
			With("subject", subject).
			With("policy_name", policyName).
			With("scope", scope).Wrap(err)
	}
	return nil
}

// parsePolicySubject extracts (gameID, scopeSuffix) from a subject of the
// form "events.<gameID>.system.crypto_policy.<scopeSuffix>". The scope
// suffix is whatever follows the literal middle segment, allowing test
// fixtures with policy_name-divergent suffixes to be walked as a chain.
func parsePolicySubject(subject string) (gameID, scope string, err error) {
	const (
		prefix = "events."
		mid    = ".system.crypto_policy."
	)
	if len(subject) < len(prefix) || subject[:len(prefix)] != prefix {
		return "", "", oops.Code("POLICY_CHAIN_SUBJECT_INVALID").
			With("subject", subject).
			Errorf("subject does not start with %q", prefix)
	}
	// After stripping the "events." prefix, rest has the shape
	// "<gameID>.system.crypto_policy.<scope>".
	rest := subject[len(prefix):]
	midIdx := indexOf(rest, mid)
	if midIdx < 0 {
		return "", "", oops.Code("POLICY_CHAIN_SUBJECT_INVALID").
			With("subject", subject).
			Errorf("subject does not contain %q", mid)
	}
	gameID = rest[:midIdx]
	scope = rest[midIdx+len(mid):]
	if gameID == "" {
		return "", "", oops.Code("POLICY_CHAIN_SUBJECT_INVALID").
			With("subject", subject).
			Errorf("gameID segment empty")
	}
	if scope == "" {
		return "", "", oops.Code("POLICY_CHAIN_SUBJECT_INVALID").
			With("subject", subject).
			Errorf("scope segment empty")
	}
	return gameID, scope, nil
}

// indexOf is a thin strings.Index wrapper kept local to avoid an import
// purely for one call site.
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
