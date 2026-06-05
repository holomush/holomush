// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package chain_test

import (
	"context"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestVerifyEntries_ScopeMismatch confirms that when ScopeFromPayload returns a
// value different from the query scope, the verifier rejects with
// AUDIT_CHAIN_SCOPE_MISMATCH (INV-CRYPTO-114). This is a focused error-path test
// constructed via a custom handler so that the hash machinery does not need
// to be valid.
func TestVerifyEntries_ScopeMismatch(t *testing.T) {
	h := makeTestHandler(t)
	// Override ScopeFromPayload to always return a wrong scope.
	h.ScopeFromPayload = func(_ []byte) (string, error) {
		return "scopeOther", nil
	}

	// Payload contents are irrelevant because ScopeFromPayload is stubbed.
	repo := &fakeRepo{
		entries: map[string][]chain.Entry{
			"scopeA": {{JSSeq: 1, Subject: "events.g1.system.example.scopeA", Payload: []byte(`{}`)}},
		},
	}
	v := chain.NewVerifier(repo)
	err := v.VerifyScope(context.Background(), h, "scopeA")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_CHAIN_SCOPE_MISMATCH")
}

// TestVerifyEntries_ScopeFromPayloadError confirms that when ScopeFromPayload
// returns an error, the verifier wraps with AUDIT_CHAIN_SCOPE_FROM_PAYLOAD_FAILED.
func TestVerifyEntries_ScopeFromPayloadError(t *testing.T) {
	h := makeTestHandler(t)
	h.ScopeFromPayload = func(_ []byte) (string, error) {
		return "", oops.Errorf("scope extract boom")
	}

	repo := &fakeRepo{
		entries: map[string][]chain.Entry{
			"scopeA": {{JSSeq: 1, Subject: "events.g1.system.example.scopeA", Payload: []byte(`{}`)}},
		},
	}
	v := chain.NewVerifier(repo)
	err := v.VerifyScope(context.Background(), h, "scopeA")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_CHAIN_SCOPE_FROM_PAYLOAD_FAILED")
}

// TestVerifyEntries_PrevHashExtractionError confirms that when PrevHashOf
// returns an error on the genesis entry, the verifier wraps with
// AUDIT_CHAIN_PREV_HASH_EXTRACT_FAILED (verifier.go:194).
func TestVerifyEntries_PrevHashExtractionError(t *testing.T) {
	h := makeTestHandler(t)
	// Scope checks must pass before we reach PrevHashOf.
	h.ScopeFromPayload = func(_ []byte) (string, error) { return "scopeA", nil }
	h.PrevHashOf = func(_ []byte) ([]byte, error) {
		return nil, oops.Errorf("prev hash extract boom")
	}

	repo := &fakeRepo{
		entries: map[string][]chain.Entry{
			"scopeA": {{JSSeq: 1, Subject: "events.g1.system.example.scopeA", Payload: []byte(`{}`)}},
		},
	}
	v := chain.NewVerifier(repo)
	err := v.VerifyScope(context.Background(), h, "scopeA")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_CHAIN_PREV_HASH_EXTRACT_FAILED")
}

// TestVerifyEntries_SelfHashExtractionError confirms that when SelfHashOf
// returns an error on the genesis entry, the verifier wraps with
// AUDIT_CHAIN_SELF_HASH_EXTRACT_FAILED (verifier.go:213).
func TestVerifyEntries_SelfHashExtractionError(t *testing.T) {
	h := makeTestHandler(t)
	// Build a valid genesis so we get past scope, prev_hash, and recompute paths.
	e := buildValidEntry(t, h, "scopeA", nil, 1)

	// Now override SelfHashOf to error out — this fires after the genesis
	// recompute succeeds (verifier.go:211–215).
	h.SelfHashOf = func(_ []byte) ([]byte, error) {
		return nil, oops.Errorf("self hash extract boom")
	}

	repo := &fakeRepo{
		entries: map[string][]chain.Entry{"scopeA": {e}},
	}
	v := chain.NewVerifier(repo)
	err := v.VerifyScope(context.Background(), h, "scopeA")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_CHAIN_SELF_HASH_EXTRACT_FAILED")
}

// TestRecomputeFor_UnmarshalFailure confirms that when Canonicalize returns
// non-JSON bytes, the recompute path wraps with
// AUDIT_CHAIN_PAYLOAD_UNMARSHAL_FAILED (verifier.go:281).
func TestRecomputeFor_UnmarshalFailure(t *testing.T) {
	h := makeTestHandler(t)
	h.ScopeFromPayload = func(_ []byte) (string, error) { return "scopeA", nil }
	h.PrevHashOf = func(_ []byte) ([]byte, error) { return nil, nil }
	h.Canonicalize = func(_ []byte) ([]byte, error) {
		return []byte("not-json"), nil
	}

	repo := &fakeRepo{
		entries: map[string][]chain.Entry{
			"scopeA": {{JSSeq: 1, Subject: "events.g1.system.example.scopeA", Payload: []byte(`{}`)}},
		},
	}
	v := chain.NewVerifier(repo)
	err := v.VerifyScope(context.Background(), h, "scopeA")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_CHAIN_PAYLOAD_UNMARSHAL_FAILED")
}

// TestRecomputeFor_CanonicalizeFailure confirms that when Canonicalize returns
// an error, the recompute path wraps with AUDIT_CHAIN_CANONICALIZE_FAILED
// (verifier.go:276).
func TestRecomputeFor_CanonicalizeFailure(t *testing.T) {
	h := makeTestHandler(t)
	h.ScopeFromPayload = func(_ []byte) (string, error) { return "scopeA", nil }
	h.PrevHashOf = func(_ []byte) ([]byte, error) { return nil, nil }
	h.Canonicalize = func(_ []byte) ([]byte, error) {
		return nil, oops.Errorf("canonicalize boom")
	}

	repo := &fakeRepo{
		entries: map[string][]chain.Entry{
			"scopeA": {{JSSeq: 1, Subject: "events.g1.system.example.scopeA", Payload: []byte(`{}`)}},
		},
	}
	v := chain.NewVerifier(repo)
	err := v.VerifyScope(context.Background(), h, "scopeA")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_CHAIN_CANONICALIZE_FAILED")
}
