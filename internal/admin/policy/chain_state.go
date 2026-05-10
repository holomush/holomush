// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"
)

// Chain-init signal — addresses CodeRabbit #11 / Phase 5 sub-epic D follow-up:
//
// The verifier MUST distinguish "first boot, no chain yet" from "chain
// existed and was truncated". Without a persistent signal, deleting every
// crypto.policy_set row for a subject would let VerifyChain succeed and
// the next startup would emit a fresh genesis — full-chain truncation
// invisible to the fail-closed verifier.
//
// Storage shape — re-uses bootstrap_metadata (key/value), per-policy:
//
//	key   = "crypto.policy_chain_initialized.<policy_name>"
//	value = "true"
//
// This avoids a dedicated migration: bootstrap_metadata is the existing
// "this was set up once" table (000001_baseline) and already has the
// idempotent INSERT ... ON CONFLICT DO NOTHING shape used elsewhere.
//
// Lifecycle:
//   1. Genesis emit (first successful Publish for a policy_name) writes
//      the row via markChainInitialized — idempotent on conflict.
//   2. Subsequent boots: VerifyChain reads the row.
//      - len(entries) == 0 + initialized == false → first-boot allowed,
//        return nil (emitter will write genesis).
//      - len(entries) == 0 + initialized == true  → POLICY_CHAIN_TRUNCATED.
//      - len(entries) > 0                          → existing walk.
//
// INV-D11 / INV-D17 are extended: the verifier no longer trusts an empty
// audit row-set as proof of "fresh DB" — it cross-checks against the
// persisted init signal.

func chainStateKey(policyName string) string {
	return "crypto.policy_chain_initialized." + policyName
}

// chainInitialized returns true iff the bootstrap_metadata row for
// policyName has been recorded (i.e., the chain genesis has been emitted
// at least once in this database's history).
//
// A missing row is treated as "not initialized" (first-boot path). An
// underlying SQL error is wrapped — failing closed is the caller's
// responsibility (VerifyChain). An unexpected stored value (anything
// other than "true") fails closed here with POLICY_CHAIN_STATE_INVALID:
// silently treating a corrupted row as first-boot would re-open exactly
// the truncation gap INV-D20 closes.
func chainInitialized(ctx context.Context, pool *pgxpool.Pool, policyName string) (bool, error) {
	var value string
	err := pool.QueryRow(ctx, `
		SELECT value FROM bootstrap_metadata WHERE key = $1
	`, chainStateKey(policyName)).Scan(&value)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, oops.Code("POLICY_CHAIN_STATE_READ_FAILED").
			With("policy_name", policyName).Wrap(err)
	}
	if value != "true" {
		return false, oops.Code("POLICY_CHAIN_STATE_INVALID").
			With("policy_name", policyName).
			With("value", value).
			Errorf("unexpected chain-init value")
	}
	return true, nil
}

// markChainInitialized inserts the chain-init signal idempotently.
// Called from EmitCurrentSnapshot after a successful Publish; safe to
// call repeatedly.
func markChainInitialized(ctx context.Context, pool *pgxpool.Pool, policyName string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO bootstrap_metadata (key, value)
		VALUES ($1, 'true')
		ON CONFLICT (key) DO NOTHING
	`, chainStateKey(policyName))
	if err != nil {
		return oops.Code("POLICY_CHAIN_STATE_WRITE_FAILED").
			With("policy_name", policyName).Wrap(err)
	}
	return nil
}
