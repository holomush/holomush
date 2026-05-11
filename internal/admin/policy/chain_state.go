// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"
)

// Chain-init signal — addresses CodeRabbit #11 / Phase 5 sub-epic D follow-up,
// post Phase 5 sub-epic E refactor onto the auditchain (chain_name, scope_key) schema.
//
// The verifier MUST distinguish "first boot, no chain yet" from "chain
// existed and was truncated". Without a persistent signal, deleting every
// crypto.policy_set row for a subject would let VerifyChain succeed and
// the next startup would emit a fresh genesis — full-chain truncation
// invisible to the fail-closed verifier.
//
// Storage shape — bootstrap_metadata keyed on (chain_name, scope_key):
//
//	chain_name = Handler.Chain.SubjectPrefix
//	             (e.g. "events.<gameID>.system.crypto_policy")
//	scope_key  = the policy_name (e.g. "dual_control_required")
//
// chain_name MUST equal Handler.Chain.SubjectPrefix because the
// generalized chain.Verifier reads bootstrap_metadata via
// chain.Repo.ChainInitialized passing SubjectPrefix as the chain_name
// argument — keeping the read and write keyed on the same field
// guarantees the truncation-detection signal lands at the same row.
//
// Migration 000030 (Phase 5 sub-epic E) replaced D's earlier (key, value)
// shape with this generalized (chain_name, scope_key) schema. The new
// shape is shared with E's rekey chain and any future audit chain
// registered via the auditchain primitive.

// markChainInitialized inserts the chain-init signal idempotently against
// the (chain_name, scope_key) primary key. Called from EmitCurrentSnapshot
// after a successful Publish; safe to call repeatedly.
//
// The gameID parameter determines the chain_name key — bootstrap_metadata
// rows are per-gameID because [PolicySetChainFor] embeds gameID in the
// SubjectPrefix.
func markChainInitialized(ctx context.Context, pool *pgxpool.Pool, gameID, policyName string) error {
	chainName := PolicySetChainFor(gameID).SubjectPrefix
	_, err := pool.Exec(ctx, `
		INSERT INTO bootstrap_metadata (chain_name, scope_key)
		VALUES ($1, $2)
		ON CONFLICT (chain_name, scope_key) DO NOTHING
	`, chainName, policyName)
	if err != nil {
		return oops.Code("POLICY_CHAIN_STATE_WRITE_FAILED").
			With("game_id", gameID).
			With("policy_name", policyName).Wrap(err)
	}
	return nil
}
