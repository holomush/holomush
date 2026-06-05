// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"github.com/holomush/holomush/internal/admin/policy"
	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

// newPolicyHashSourceFromAuditChain builds a dek.PolicyHashSource backed
// by the auditchain Repo and the per-game crypto.policy_set chain handler.
//
// The dek package provides the canonical implementation
// (dek.NewAuditChainPolicyHashSource). This wiring helper exists in
// cmd/holomush so the import-cycle-breaking handler (policy.PolicySetHandlerFor)
// is constructed at the wiring layer where both packages are importable —
// per the wiring contract documented on dek.NewAuditChainPolicyHashSource.
//
// INV-CRYPTO-112 (sub-epic E spec §3.6 / spec §6): Phase 1 calls CurrentPolicyHash
// once and freezes the result on the checkpoint row. Later phases never
// re-query the policy hash.
//
// Sub-epic E T37 (holomush-jxo8.7.34).
func newPolicyHashSourceFromAuditChain(repo chain.Repo, gameID string) dek.PolicyHashSource {
	return dek.NewAuditChainPolicyHashSource(repo, policy.PolicySetHandlerFor(gameID))
}
