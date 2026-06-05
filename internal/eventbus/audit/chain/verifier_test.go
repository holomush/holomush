// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package chain_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/pkg/errutil"
)

// ---------------------------------------------------------------------------
// fakeRepo — in-memory Repo for unit tests.
// LoadEntriesByScope / DiscoverScopes ignore subjectPrefix (tests use a single
// fake scope per instance); ChainInitialized / MarkChainInitialized track
// "chainName|scope" keys.
// ---------------------------------------------------------------------------

type fakeRepo struct {
	entries     map[string][]chain.Entry // scope → entries
	scopes      []string
	initialized map[string]bool
}

func (r *fakeRepo) LoadEntriesByScope(_ context.Context, _ string, scope string) ([]chain.Entry, error) {
	return r.entries[scope], nil
}

func (r *fakeRepo) DiscoverScopes(_ context.Context, _ string) ([]string, error) {
	return r.scopes, nil
}

func (r *fakeRepo) ChainInitialized(_ context.Context, chainName, scope string) (bool, error) {
	return r.initialized[chainName+"|"+scope], nil
}

func (r *fakeRepo) MarkChainInitialized(_ context.Context, chainName, scope string) error {
	if r.initialized == nil {
		r.initialized = map[string]bool{}
	}
	r.initialized[chainName+"|"+scope] = true
	return nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// testPayload is the JSON shape used by all verifier tests.
type testPayload struct {
	Scope    string `json:"scope"`
	PrevHash []byte `json:"prev_hash,omitempty"`
	SelfHash []byte `json:"self_hash,omitempty"`
	Note     string `json:"note"`
}

// buildPayload serialises a testPayload JSON blob. selfHash is not set here —
// callers that need a valid self_hash first call buildPayload, then
// mustRecomputeSelfHash, then setPayloadSelfHash.
func buildPayload(t *testing.T, scope string, prevHash []byte) []byte {
	t.Helper()
	b, err := json.Marshal(testPayload{Scope: scope, PrevHash: prevHash, Note: "test"})
	require.NoError(t, err)
	return b
}

// setPayloadSelfHash returns a new JSON blob with the self_hash field set to h.
func setPayloadSelfHash(t *testing.T, payload []byte, h []byte) []byte {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(payload, &m))
	m["self_hash"] = h
	b, err := json.Marshal(m)
	require.NoError(t, err)
	return b
}

// mustRecomputeSelfHash mirrors the verifier's recomputeFor path:
// canonicalize (unmarshal + re-marshal), then unmarshal into map[string]any,
// then call chain.RecomputeSelfHash with "self_hash" as the field name.
// This ensures the test's expected hash uses the same intermediate form as
// the verifier, so ZeroField operates on the same map shape in both paths.
func mustRecomputeSelfHash(t *testing.T, h chain.Handler, payload []byte) []byte {
	t.Helper()
	canonical, err := h.Canonicalize(payload)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(canonical, &m))
	result, err := chain.RecomputeSelfHash(m, "self_hash")
	require.NoError(t, err)
	return result
}

// makeTestHandler builds a Handler bundle backed by the simple
// "events.g1.system.example" subject prefix.  Per-chain functions are minimal
// but correct:
//   - SubjectFor:       "events.g1.system.example.<scope>"
//   - ScopeFromSubject: suffix after the last "."
//   - ScopeFromPayload: reads the "scope" JSON field
//   - Canonicalize:     plain JSON unmarshal + re-marshal (no domain normalization)
//   - PrevHashOf:       reads "prev_hash"
//   - SelfHashOf:       reads "self_hash"
func makeTestHandler(t *testing.T) chain.Handler {
	t.Helper()
	c := chain.Chain{
		SubjectPrefix:     "events.g1.system.example",
		SelfHashField:     "self_hash",
		PrevHashField:     "prev_hash",
		ScopePayloadField: "scope",
	}
	return chain.Handler{
		Chain: c,
		SubjectFor: func(scope string) string {
			return "events.g1.system.example." + scope
		},
		ScopeFromSubject: func(subject string) (string, error) {
			idx := strings.LastIndex(subject, ".")
			if idx < 0 {
				return "", nil
			}
			return subject[idx+1:], nil
		},
		ScopeFromPayload: func(payload []byte) (string, error) {
			var p testPayload
			if err := json.Unmarshal(payload, &p); err != nil {
				return "", err
			}
			return p.Scope, nil
		},
		Canonicalize: func(payload []byte) ([]byte, error) {
			var m map[string]any
			if err := json.Unmarshal(payload, &m); err != nil {
				return nil, err
			}
			return json.Marshal(m)
		},
		PrevHashOf: func(payload []byte) ([]byte, error) {
			var m map[string]any
			if err := json.Unmarshal(payload, &m); err != nil {
				return nil, err
			}
			v, ok := m["prev_hash"]
			if !ok || v == nil {
				return nil, nil
			}
			// json.Unmarshal decodes base64 bytes as []byte automatically
			// when the target is map[string]any and the JSON value is a base64 string.
			// However, for []byte-typed fields json stores them as base64 strings, so
			// we re-marshal and unmarshal via a typed struct to get the raw bytes.
			raw, err := json.Marshal(map[string]any{"v": v})
			if err != nil {
				return nil, err
			}
			var typed struct {
				V []byte `json:"v"`
			}
			if err := json.Unmarshal(raw, &typed); err != nil {
				return nil, err
			}
			return typed.V, nil
		},
		SelfHashOf: func(payload []byte) ([]byte, error) {
			var m map[string]any
			if err := json.Unmarshal(payload, &m); err != nil {
				return nil, err
			}
			v, ok := m["self_hash"]
			if !ok || v == nil {
				return nil, nil
			}
			raw, err := json.Marshal(map[string]any{"v": v})
			if err != nil {
				return nil, err
			}
			var typed struct {
				V []byte `json:"v"`
			}
			if err := json.Unmarshal(raw, &typed); err != nil {
				return nil, err
			}
			return typed.V, nil
		},
	}
}

// ---------------------------------------------------------------------------
// Tests — canonical names from bead TDD acceptance criteria.
// ---------------------------------------------------------------------------

// TestVerifier_GenesisPrevHashNil: a single-entry chain with nil prev_hash and
// a correctly computed self_hash passes verification.
func TestVerifier_GenesisPrevHashNil(t *testing.T) {
	h := makeTestHandler(t)
	// Build payload with a placeholder self_hash first so the field is present
	// in the JSON (ZeroField sets it to null, not removes it; the hash must be
	// computed over the same shape the verifier sees).
	payload := setPayloadSelfHash(t, buildPayload(t, "scopeA", nil), []byte{0x00})
	selfHash := mustRecomputeSelfHash(t, h, payload)
	payload = setPayloadSelfHash(t, payload, selfHash)

	repo := &fakeRepo{
		entries: map[string][]chain.Entry{
			"scopeA": {{JSSeq: 1, Subject: "events.g1.system.example.scopeA", Payload: payload}},
		},
	}
	v := chain.NewVerifier(repo)
	require.NoError(t, v.VerifyScope(context.Background(), h, "scopeA"))
}

// TestVerifier_BrokenGenesis_NonNilPrev: a genesis entry with a non-nil
// prev_hash must be rejected with AUDIT_CHAIN_BROKEN_GENESIS.
func TestVerifier_BrokenGenesis_NonNilPrev(t *testing.T) {
	h := makeTestHandler(t)
	// Add placeholder self_hash so the field is present when we compute the hash.
	payload := setPayloadSelfHash(t, buildPayload(t, "scopeA", []byte{0x01, 0x02}), []byte{0x00})
	selfHash := mustRecomputeSelfHash(t, h, payload)
	payload = setPayloadSelfHash(t, payload, selfHash)

	repo := &fakeRepo{
		entries: map[string][]chain.Entry{
			"scopeA": {{JSSeq: 1, Subject: "events.g1.system.example.scopeA", Payload: payload}},
		},
	}
	v := chain.NewVerifier(repo)
	err := v.VerifyScope(context.Background(), h, "scopeA")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_CHAIN_BROKEN_GENESIS")
}

// buildValidEntry constructs a chain.Entry with a correctly computed self_hash.
// The placeholder pattern (add dummy self_hash → compute → set correct) ensures
// the self_hash field is present in the JSON for both compute and verify paths.
func buildValidEntry(t *testing.T, h chain.Handler, scope string, prevHash []byte, jsSeq int64) chain.Entry {
	t.Helper()
	payload := setPayloadSelfHash(t, buildPayload(t, scope, prevHash), []byte{0x00})
	selfHash := mustRecomputeSelfHash(t, h, payload)
	payload = setPayloadSelfHash(t, payload, selfHash)
	return chain.Entry{
		JSSeq:   jsSeq,
		Subject: h.SubjectFor(scope),
		Payload: payload,
	}
}

// TestVerifier_PrevHashLinkMismatch: a second entry whose prev_hash does not
// match the predecessor's recomputed self_hash must be rejected with
// AUDIT_CHAIN_BROKEN_LINK.
func TestVerifier_PrevHashLinkMismatch(t *testing.T) {
	h := makeTestHandler(t)

	// Build a valid genesis.
	e1 := buildValidEntry(t, h, "scopeA", nil, 1)

	// Build a second entry with a wrong prev_hash (does not match e1's self_hash).
	e2 := buildValidEntry(t, h, "scopeA", []byte{0xff, 0xff}, 2)

	repo := &fakeRepo{
		entries: map[string][]chain.Entry{
			"scopeA": {e1, e2},
		},
	}
	v := chain.NewVerifier(repo)
	err := v.VerifyScope(context.Background(), h, "scopeA")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_CHAIN_BROKEN_LINK")
}

// TestVerifier_SelfHashTamperDetected: an entry with a self_hash that does not
// match the recomputed hash must be rejected with AUDIT_CHAIN_HASH_MISMATCH.
func TestVerifier_SelfHashTamperDetected(t *testing.T) {
	h := makeTestHandler(t)
	payload := buildPayload(t, "scopeA", nil)
	payload = setPayloadSelfHash(t, payload, []byte{0xde, 0xad}) // wrong self_hash

	repo := &fakeRepo{
		entries: map[string][]chain.Entry{
			"scopeA": {{JSSeq: 1, Subject: "events.g1.system.example.scopeA", Payload: payload}},
		},
	}
	v := chain.NewVerifier(repo)
	err := v.VerifyScope(context.Background(), h, "scopeA")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_CHAIN_HASH_MISMATCH")
}

// TestVerifier_ScopeMismatchBetweenSubjectAndPayload_RejectsRow: an entry
// whose subject says scopeA but payload.scope says scopeB must be rejected
// with AUDIT_CHAIN_SCOPE_MISMATCH (INV-CRYPTO-114).
func TestVerifier_ScopeMismatchBetweenSubjectAndPayload_RejectsRow(t *testing.T) {
	h := makeTestHandler(t)

	// Payload says "scopeB" but it's stored under the "scopeA" query key.
	// The scope check runs before the hash check, so hash correctness doesn't matter.
	e := buildValidEntry(t, h, "scopeB", nil, 1)
	// Overwrite the subject to say scopeA so the Repo returns it for the scopeA query,
	// while the payload still says scopeB — triggering INV-CRYPTO-114.
	e.Subject = "events.g1.system.example.scopeA"

	repo := &fakeRepo{
		entries: map[string][]chain.Entry{
			"scopeA": {e},
		},
	}
	v := chain.NewVerifier(repo)
	err := v.VerifyScope(context.Background(), h, "scopeA")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_CHAIN_SCOPE_MISMATCH")
}

// TestVerifier_EmptyChain_NotInitialized_OK: an empty entries slice for a
// chain that has never been initialized should pass (first boot, genesis
// eligible).
func TestVerifier_EmptyChain_NotInitialized_OK(t *testing.T) {
	h := makeTestHandler(t)
	repo := &fakeRepo{entries: nil}
	v := chain.NewVerifier(repo)
	require.NoError(t, v.VerifyScope(context.Background(), h, "scopeA"),
		"first boot: empty chain is genesis-eligible")
}

// TestVerifier_EmptyChain_PreviouslyInitialized_TruncationDetected: if the
// chain has been initialized before but now has no rows, truncation is
// detected and AUDIT_CHAIN_TRUNCATED must be returned.
func TestVerifier_EmptyChain_PreviouslyInitialized_TruncationDetected(t *testing.T) {
	h := makeTestHandler(t)
	repo := &fakeRepo{
		entries:     map[string][]chain.Entry{"scopeA": nil},
		initialized: map[string]bool{"events.g1.system.example|scopeA": true},
	}
	v := chain.NewVerifier(repo)
	err := v.VerifyScope(context.Background(), h, "scopeA")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_CHAIN_TRUNCATED")
}

// ---------------------------------------------------------------------------
// Emitter tests — canonical names from bead holomush-jxo8.7.5 TDD criteria.
// ---------------------------------------------------------------------------

// TestEmitter_ComputePrevHashFor_GenesisReturnsNil: with no entries in the repo,
// ComputePrevHashFor returns nil, nil, nil (genesis-eligible chain).
func TestEmitter_ComputePrevHashFor_GenesisReturnsNil(t *testing.T) {
	h := makeTestHandler(t)
	repo := &fakeRepo{entries: nil}
	em := chain.NewEmitter(repo)

	prev, prevID, err := em.ComputePrevHashFor(context.Background(), h, "scopeA")
	require.NoError(t, err)
	require.Nil(t, prev)
	require.Nil(t, prevID)
}

// TestEmitter_ComputePrevHashFor_ReturnsHashOfLastEntry: with one entry in the
// repo, ComputePrevHashFor returns the recomputed self-hash of that entry.
func TestEmitter_ComputePrevHashFor_ReturnsHashOfLastEntry(t *testing.T) {
	h := makeTestHandler(t)
	// Build an entry: placeholder self_hash so field is present, then compute real hash.
	p1 := setPayloadSelfHash(t, buildPayload(t, "scopeA", nil), []byte{0x00})
	p1 = setPayloadSelfHash(t, p1, mustRecomputeSelfHash(t, h, p1))
	repo := &fakeRepo{
		entries: map[string][]chain.Entry{"scopeA": {{JSSeq: 1, Payload: p1}}},
	}
	em := chain.NewEmitter(repo)

	prev, _, err := em.ComputePrevHashFor(context.Background(), h, "scopeA")
	require.NoError(t, err)

	// Expected: same path as verifier — canonicalize, unmarshal to map, RecomputeSelfHash.
	expected := mustRecomputeSelfHash(t, h, p1)
	require.Equal(t, expected, prev)
}

// TestEmitter_ComputePrevHashFor_MultiEntry_ReturnsLatestByJSSeq: with multiple
// entries, ComputePrevHashFor returns the self-hash of the entry with the
// highest JSSeq (the tail of the chain).
func TestEmitter_ComputePrevHashFor_MultiEntry_ReturnsLatestByJSSeq(t *testing.T) {
	h := makeTestHandler(t)

	// Build two valid chained entries.
	e1 := buildValidEntry(t, h, "scopeA", nil, 1)
	e1SelfHash := mustRecomputeSelfHash(t, h, e1.Payload)
	e2 := buildValidEntry(t, h, "scopeA", e1SelfHash, 2)

	repo := &fakeRepo{
		entries: map[string][]chain.Entry{"scopeA": {e1, e2}},
	}
	em := chain.NewEmitter(repo)

	prev, _, err := em.ComputePrevHashFor(context.Background(), h, "scopeA")
	require.NoError(t, err)

	// Emitter should return hash of the last entry (e2), not e1.
	expectedE2Hash := mustRecomputeSelfHash(t, h, e2.Payload)
	require.Equal(t, expectedE2Hash, prev)
}
