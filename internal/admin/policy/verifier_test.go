// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/admin/policy"
	"github.com/holomush/holomush/internal/eventbus/audit/chain"
)

// Post Phase 5 sub-epic E refactor: these tests drive policy.VerifyChain
// against a fake chain.Repo rather than the deleted internal
// verifyChainEntries / chainEntry surface. The error-code namespace
// migrated from POLICY_CHAIN_* to AUDIT_CHAIN_* (the generalized verifier
// owns the typed-error surface now); policy.VerifyChain wraps with the
// outer POLICY_CHAIN_VERIFY_FAILED code so callers can identify the chain.
//
// The cross-check semantics formerly enforced by POLICY_CHAIN_NAME_MISMATCH
// now surface as AUDIT_CHAIN_SCOPE_MISMATCH via the chain handler's
// ScopeFromPayload extractor (INV-CRYPTO-114).

const testGameID = "testgame"

func testSubjectFor(policyName string) string {
	return "events." + testGameID + ".system.crypto_policy." + policyName
}

// fakeRepo is an in-memory implementation of chain.Repo. The verifier reads
// LoadEntriesByScope on (subjectPrefix, scope) — we store entries keyed by
// the full subject (subjectPrefix + "." + scope).
type fakeRepo struct {
	entries     map[string][]chain.Entry
	initialized map[string]bool // key = chainName + "|" + scope
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		entries:     make(map[string][]chain.Entry),
		initialized: make(map[string]bool),
	}
}

func (r *fakeRepo) LoadEntriesByScope(_ context.Context, subjectPrefix, scope string) ([]chain.Entry, error) {
	key := subjectPrefix + "." + scope
	out := make([]chain.Entry, len(r.entries[key]))
	copy(out, r.entries[key])
	return out, nil
}

func (r *fakeRepo) DiscoverScopes(_ context.Context, _ string) ([]string, error) {
	return nil, errors.New("not used in these tests")
}

func (r *fakeRepo) ChainInitialized(_ context.Context, chainName, scope string) (bool, error) {
	return r.initialized[chainName+"|"+scope], nil
}

func (r *fakeRepo) MarkChainInitialized(_ context.Context, chainName, scope string) error {
	r.initialized[chainName+"|"+scope] = true
	return nil
}

// seed inserts an entry for the given policy_name. PolicyHash is computed
// fresh via ComputePolicyHash unless the caller overrode the payload's
// PolicyHash before calling seed (used by tamper tests).
func (r *fakeRepo) seed(t *testing.T, jsSeq int64, payload policy.PolicySetPayload) {
	t.Helper()
	if payload.PolicyHash == nil {
		h, err := policy.ComputePolicyHash(&payload)
		require.NoError(t, err)
		payload.PolicyHash = h
	}
	body, err := json.Marshal(&payload)
	require.NoError(t, err)
	subject := testSubjectFor(payload.PolicyName)
	r.entries[subject] = append(r.entries[subject], chain.Entry{
		JSSeq:   jsSeq,
		Subject: subject,
		Payload: body,
	})
}

// helperPayload constructs a deterministic PolicySetPayload for chain tests.
// PrevHash and PolicyHash are NOT set by this helper; callers fill them in
// (or let seed() compute the hash).
func helperPayload(name string, prev []byte, ts int64) policy.PolicySetPayload {
	return policy.PolicySetPayload{
		PolicyName:      name,
		PolicySnapshot:  map[string]any{"members": []any{}},
		PrevHash:        prev,
		ServerStartULID: "01HZSTART0000000000000000",
		ServerIdentity:  "holomush@host",
		Timestamp:       time.Unix(ts, 0).UTC(),
	}
}

// runVerify drives the generalized chain verifier through the policy_set
// handler. Mirrors what policy.VerifyChain does internally but lets tests
// inject a fake Repo without standing up Postgres.
func runVerify(t *testing.T, repo chain.Repo, policyName string) error {
	t.Helper()
	v := chain.NewVerifier(repo)
	h := policy.PolicySetHandlerFor(testGameID)
	return v.VerifyScope(context.Background(), h, policyName)
}

// TestVerifyChainEntriesAcceptsEmptyChain — fresh DB, no init signal.
// First-boot path: empty chain MUST verify cleanly.
func TestVerifyChainEntriesAcceptsEmptyChain(t *testing.T) {
	repo := newFakeRepo()
	require.NoError(t, runVerify(t, repo, "crypto.operators"))
}

// TestVerifyChainEntriesAcceptsValidGenesis — single-row chain whose
// prev_hash is nil and whose policy_hash matches its own payload.
func TestVerifyChainEntriesAcceptsValidGenesis(t *testing.T) {
	repo := newFakeRepo()
	repo.seed(t, 1, helperPayload("crypto.operators", nil, 1700000000))
	require.NoError(t, runVerify(t, repo, "crypto.operators"))
}

// TestVerifyChainEntriesAcceptsValidExtension — two-row chain where the
// second row's prev_hash matches the first row's recomputed policy_hash.
func TestVerifyChainEntriesAcceptsValidExtension(t *testing.T) {
	repo := newFakeRepo()
	gen := helperPayload("crypto.operators", nil, 1700000000)
	genHash, err := policy.ComputePolicyHash(&gen)
	require.NoError(t, err)
	gen.PolicyHash = genHash
	repo.seed(t, 1, gen)

	ext := helperPayload("crypto.operators", genHash, 1700000060)
	repo.seed(t, 2, ext)

	require.NoError(t, runVerify(t, repo, "crypto.operators"))
}

// TestVerifyChainEntriesRejectsBrokenGenesis verifies INV-CRYPTO-77: a genesis
// row with non-nil prev_hash produces AUDIT_CHAIN_BROKEN_GENESIS (post-E
// generalization of POLICY_CHAIN_BROKEN_GENESIS).
func TestVerifyChainEntriesRejectsBrokenGenesis(t *testing.T) {
	repo := newFakeRepo()
	repo.seed(t, 1, helperPayload("crypto.operators", []byte{0xff}, 1700000000))
	err := runVerify(t, repo, "crypto.operators")
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "AUDIT_CHAIN_BROKEN_GENESIS", o.Code())
}

// TestVerifyChainEntriesRejectsBrokenLink verifies INV-CRYPTO-78: a non-genesis
// row whose prev_hash does not match the predecessor produces
// AUDIT_CHAIN_BROKEN_LINK (post-E generalization).
func TestVerifyChainEntriesRejectsBrokenLink(t *testing.T) {
	repo := newFakeRepo()
	repo.seed(t, 1, helperPayload("crypto.operators", nil, 1700000000))
	// Build ext with a wrong prev_hash so the link is broken.
	repo.seed(t, 2, helperPayload("crypto.operators", []byte{0xde, 0xad, 0xbe, 0xef}, 1700000060))
	err := runVerify(t, repo, "crypto.operators")
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "AUDIT_CHAIN_BROKEN_LINK", o.Code())
}

// TestVerifyChainEntriesRejectsHashMismatch verifies INV-CRYPTO-79: a row whose
// stored policy_hash does not match its recomputed hash produces
// AUDIT_CHAIN_HASH_MISMATCH (post-E generalization).
func TestVerifyChainEntriesRejectsHashMismatch(t *testing.T) {
	repo := newFakeRepo()
	gen := helperPayload("crypto.operators", nil, 1700000000)
	genHash, err := policy.ComputePolicyHash(&gen)
	require.NoError(t, err)
	gen.PolicyHash = genHash
	repo.seed(t, 1, gen)

	// Construct a valid ext, then tamper the snapshot AFTER its stored
	// hash was computed — recompute on the tampered payload diverges
	// from the stored hash → AUDIT_CHAIN_HASH_MISMATCH.
	ext := helperPayload("crypto.operators", genHash, 1700000060)
	extHash, err := policy.ComputePolicyHash(&ext)
	require.NoError(t, err)
	ext.PolicyHash = extHash
	ext.PolicySnapshot = map[string]any{"members": []any{"tampered"}}
	repo.seed(t, 2, ext)

	err = runVerify(t, repo, "crypto.operators")
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "AUDIT_CHAIN_HASH_MISMATCH", o.Code())
}

// TestVerifyChainEntriesDecodesEnvelopeAndJSON documents the JSON decode
// shape (the inner stage that the chain Handler's extractors all parse)
// by round-tripping a payload and asserting field equality after the
// round-trip.
func TestVerifyChainEntriesDecodesEnvelopeAndJSON(t *testing.T) {
	original := helperPayload("crypto.operators", nil, 1700000000)
	h, err := policy.ComputePolicyHash(&original)
	require.NoError(t, err)
	original.PolicyHash = h

	bodyJSON, err := json.Marshal(&original)
	require.NoError(t, err)
	var decoded policy.PolicySetPayload
	require.NoError(t, json.Unmarshal(bodyJSON, &decoded))
	assert.Equal(t, original.PolicyName, decoded.PolicyName)
	assert.Equal(t, original.PolicyHash, decoded.PolicyHash)
	assert.Equal(t, original.PrevHash, decoded.PrevHash)
	assert.Equal(t, original.ServerStartULID, decoded.ServerStartULID)
}

// TestVerifyChainEntriesAcceptsMultiplePolicyNames verifies that each
// policy_name is verified independently — a chain whose every entry's
// payload.policy_name matches the expected name verifies cleanly across
// the namespace.
func TestVerifyChainEntriesAcceptsMultiplePolicyNames(t *testing.T) {
	for _, name := range []string{"crypto.operators", "crypto.admins", "crypto.auditors"} {
		repo := newFakeRepo()
		repo.seed(t, 1, helperPayload(name, nil, 1700000000))
		require.NoError(t, runVerify(t, repo, name),
			"expected valid genesis chain for policy %s", name)
	}
}

// TestVerifyChainEntriesRejectsPolicyNameMismatchAtGenesis verifies the
// cross-check between Payload.PolicyName and the expected policy_name arg
// (now via Handler.ScopeFromPayload — INV-CRYPTO-114). Post-E refactor surfaces
// as AUDIT_CHAIN_SCOPE_MISMATCH (was POLICY_CHAIN_NAME_MISMATCH).
func TestVerifyChainEntriesRejectsPolicyNameMismatchAtGenesis(t *testing.T) {
	repo := newFakeRepo()
	// Manually inject an entry under one subject whose payload carries a
	// different policy_name. The chain's Handler.ScopeFromPayload will see
	// "crypto.admins" while the verifier was asked to verify "crypto.operators".
	wrong := helperPayload("crypto.admins", nil, 1700000000)
	wrongHash, err := policy.ComputePolicyHash(&wrong)
	require.NoError(t, err)
	wrong.PolicyHash = wrongHash
	body, err := json.Marshal(&wrong)
	require.NoError(t, err)
	// Subject is keyed on the expected scope ("crypto.operators") so the
	// Repo returns this row when asked for that scope.
	subject := testSubjectFor("crypto.operators")
	repo.entries[subject] = append(repo.entries[subject], chain.Entry{
		JSSeq: 1, Subject: subject, Payload: body,
	})

	err = runVerify(t, repo, "crypto.operators")
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "AUDIT_CHAIN_SCOPE_MISMATCH", o.Code())
}

// TestVerifyChainEntriesRejectsPolicyNameMismatchOnExtension — extension
// row's payload encodes a different policy_name. Surfaces as
// AUDIT_CHAIN_SCOPE_MISMATCH.
func TestVerifyChainEntriesRejectsPolicyNameMismatchOnExtension(t *testing.T) {
	repo := newFakeRepo()
	// Genesis: clean operator policy.
	gen := helperPayload("crypto.operators", nil, 1700000000)
	genHash, err := policy.ComputePolicyHash(&gen)
	require.NoError(t, err)
	gen.PolicyHash = genHash
	repo.seed(t, 1, gen)

	// Inject extension with mismatched payload.policy_name under the
	// operator subject.
	wrong := helperPayload("crypto.admins", genHash, 1700000060)
	wrongHash, err := policy.ComputePolicyHash(&wrong)
	require.NoError(t, err)
	wrong.PolicyHash = wrongHash
	body, err := json.Marshal(&wrong)
	require.NoError(t, err)
	subject := testSubjectFor("crypto.operators")
	repo.entries[subject] = append(repo.entries[subject], chain.Entry{
		JSSeq: 2, Subject: subject, Payload: body,
	})

	err = runVerify(t, repo, "crypto.operators")
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "AUDIT_CHAIN_SCOPE_MISMATCH", o.Code())
}
