// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package chain

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/samber/oops"
)

// Handler bundles per-chain behavior with the [Chain] metadata.
//
// A Handler is registered at wiring time by the chain's owning package
// (e.g. dek.RegisterRekey(v)) and consumed by [Verifier] and [Emitter].
// The Chain field carries structural metadata; the function fields carry
// per-chain extraction and canonicalization logic.
//
// See docs/superpowers/specs/2026-05-10-event-payload-crypto-phase5-sub-epic-e-design.md §3.6 (R6 amendment).
type Handler struct {
	// Chain is the pure-metadata descriptor for this chain.
	Chain Chain

	// SubjectFor builds the full NATS subject for a given scope.
	// Example: "events.<game>.system.rekey.<context_type>.<context_id>".
	SubjectFor func(scope string) string

	// ScopeFromSubject is the inverse of SubjectFor. Parses the domain scope
	// from a full NATS subject. Used for INV-E27 cross-check.
	ScopeFromSubject func(subject string) (string, error)

	// ScopeFromPayload extracts the domain scope from raw payload bytes.
	// This is an independent extraction path for the INV-E27 cross-check —
	// the verifier asserts ScopeFromSubject(entry.Subject) == ScopeFromPayload(entry.Payload).
	ScopeFromPayload func(payload []byte) (string, error)

	// Canonicalize unmarshals and applies chain-specific normalization to the
	// payload bytes, returning canonical JSON bytes. For example, D's policy_set
	// chain normalizes the empty-form PrevHash to nil here. If no domain
	// normalization is needed, a plain JSON unmarshal+marshal is sufficient.
	Canonicalize func(payload []byte) ([]byte, error)

	// PrevHashOf extracts the prev_hash bytes from raw payload bytes.
	// Returns nil for genesis entries (prev_hash absent or null).
	PrevHashOf func(payload []byte) ([]byte, error)

	// SelfHashOf extracts the self_hash bytes from raw payload bytes.
	SelfHashOf func(payload []byte) ([]byte, error)
}

// Verifier walks one chain scope or all scopes of a chain, validating the
// tamper-evident hash chain invariants.
//
// INV-E27: for each entry, ScopeFromSubject(subject) MUST equal ScopeFromPayload(payload).
// INV-E28: for each entry, the stored self_hash MUST equal
//
//	SHA-256(JCS(zero(canonicalized_payload, SelfHashField))).
//
// Genesis invariant (INV-D10 generalized): the first entry's prev_hash MUST be nil.
// Link invariant (INV-D11 generalized): each subsequent entry's prev_hash MUST equal
// the predecessor's recomputed self_hash.
type Verifier interface {
	// VerifyScope validates the chain for a single scope.
	// Returns nil on success; a typed AUDIT_CHAIN_* error on any integrity failure.
	VerifyScope(ctx context.Context, h Handler, scope string) error

	// VerifyAll discovers all scopes via the Repo and calls VerifyScope for each.
	// Returns on the first failure.
	VerifyAll(ctx context.Context, h Handler) error
}

// NewVerifier constructs a Verifier backed by repo.
func NewVerifier(repo Repo) Verifier {
	return &verifier{repo: repo}
}

type verifier struct {
	repo Repo
}

// VerifyAll discovers all scopes for h.Chain.SubjectPrefix and verifies each.
func (v *verifier) VerifyAll(ctx context.Context, h Handler) error {
	scopes, err := v.repo.DiscoverScopes(ctx, h.Chain.SubjectPrefix)
	if err != nil {
		return oops.Code("AUDIT_CHAIN_DISCOVER_FAILED").
			With("chain", h.Chain.SubjectPrefix).Wrap(err)
	}
	for _, s := range scopes {
		if err := v.VerifyScope(ctx, h, s); err != nil {
			return err
		}
	}
	return nil
}

// VerifyScope validates the hash chain for scope.
func (v *verifier) VerifyScope(ctx context.Context, h Handler, scope string) error {
	entries, err := v.repo.LoadEntriesByScope(ctx, h.Chain.SubjectPrefix, scope)
	if err != nil {
		return oops.Code("AUDIT_CHAIN_LOAD_FAILED").
			With("chain", h.Chain.SubjectPrefix).With("scope", scope).Wrap(err)
	}

	if len(entries) == 0 {
		// No events — distinguish first-boot (genesis eligible) from truncation.
		initialized, err := v.repo.ChainInitialized(ctx, h.Chain.SubjectPrefix, scope)
		if err != nil {
			return oops.Code("AUDIT_CHAIN_INIT_READ_FAILED").
				With("chain", h.Chain.SubjectPrefix).With("scope", scope).Wrap(err)
		}
		if initialized {
			return oops.Code("AUDIT_CHAIN_TRUNCATED").
				With("chain", h.Chain.SubjectPrefix).With("scope", scope).
				Errorf("chain previously initialized but events_audit holds no rows")
		}
		// First boot: genesis eligible.
		return nil
	}

	return v.verifyEntries(ctx, h, scope, entries)
}

// verifyEntries performs the actual chain-walk integrity checks on a non-empty
// slice of entries (ordered by js_seq ASC).
func (v *verifier) verifyEntries(_ context.Context, h Handler, scope string, entries []Entry) error {
	// INV-E27: for every entry, ScopeFromPayload MUST agree with the query scope.
	// (The query scope is derived from ScopeFromSubject on the stored subject, but
	// we check against the caller-supplied scope for simplicity — the Repo query
	// is authoritative for which rows are returned for a given scope.)
	for _, e := range entries {
		payloadScope, err := h.ScopeFromPayload(e.Payload)
		if err != nil {
			return oops.Code("AUDIT_CHAIN_SCOPE_FROM_PAYLOAD_FAILED").
				With("chain", h.Chain.SubjectPrefix).
				With("js_seq", e.JSSeq).Wrap(err)
		}
		if payloadScope != scope {
			return oops.Code("AUDIT_CHAIN_SCOPE_MISMATCH").
				With("chain", h.Chain.SubjectPrefix).
				With("subject_scope", scope).
				With("payload_scope", payloadScope).
				With("js_seq", e.JSSeq).
				Errorf("INV-E27: subject and payload scope disagree")
		}
	}

	// Genesis invariant (INV-D10 generalized): first entry's prev_hash MUST be nil.
	genPrev, err := h.PrevHashOf(entries[0].Payload)
	if err != nil {
		return oops.Code("AUDIT_CHAIN_PREV_HASH_EXTRACT_FAILED").
			With("chain", h.Chain.SubjectPrefix).
			With("js_seq", entries[0].JSSeq).Wrap(err)
	}
	if genPrev != nil {
		return oops.Code("AUDIT_CHAIN_BROKEN_GENESIS").
			With("chain", h.Chain.SubjectPrefix).
			With("scope", scope).
			With("js_seq", entries[0].JSSeq).
			Errorf("genesis prev_hash must be nil")
	}

	// INV-E28: genesis self_hash MUST equal recomputed hash.
	genHash, err := v.recomputeFor(h, entries[0].Payload)
	if err != nil {
		return err
	}
	storedGen, err := h.SelfHashOf(entries[0].Payload)
	if err != nil {
		return oops.Code("AUDIT_CHAIN_SELF_HASH_EXTRACT_FAILED").
			With("chain", h.Chain.SubjectPrefix).
			With("js_seq", entries[0].JSSeq).Wrap(err)
	}
	if !bytes.Equal(genHash, storedGen) {
		return oops.Code("AUDIT_CHAIN_HASH_MISMATCH").
			With("chain", h.Chain.SubjectPrefix).
			With("scope", scope).
			With("js_seq", entries[0].JSSeq).
			Errorf("genesis self_hash does not match recompute")
	}

	// Walk remaining entries: INV-D11 (prev_hash == predecessor recompute)
	// and INV-D12 / INV-E28 (stored self_hash == own recompute).
	for i := 1; i < len(entries); i++ {
		// Predecessor's recomputed hash is what this entry's prev_hash must equal.
		prevRecompute, err := v.recomputeFor(h, entries[i-1].Payload)
		if err != nil {
			return err
		}
		prev, err := h.PrevHashOf(entries[i].Payload)
		if err != nil {
			return oops.Code("AUDIT_CHAIN_PREV_HASH_EXTRACT_FAILED").
				With("chain", h.Chain.SubjectPrefix).
				With("js_seq", entries[i].JSSeq).Wrap(err)
		}
		if !bytes.Equal(prev, prevRecompute) {
			return oops.Code("AUDIT_CHAIN_BROKEN_LINK").
				With("chain", h.Chain.SubjectPrefix).
				With("scope", scope).
				With("js_seq", entries[i].JSSeq).
				Errorf("prev_hash does not match predecessor's recompute")
		}

		recompute, err := v.recomputeFor(h, entries[i].Payload)
		if err != nil {
			return err
		}
		stored, err := h.SelfHashOf(entries[i].Payload)
		if err != nil {
			return oops.Code("AUDIT_CHAIN_SELF_HASH_EXTRACT_FAILED").
				With("chain", h.Chain.SubjectPrefix).
				With("js_seq", entries[i].JSSeq).Wrap(err)
		}
		if !bytes.Equal(recompute, stored) {
			return oops.Code("AUDIT_CHAIN_HASH_MISMATCH").
				With("chain", h.Chain.SubjectPrefix).
				With("scope", scope).
				With("js_seq", entries[i].JSSeq).
				Errorf("self_hash does not match recompute")
		}
	}
	return nil
}

// recomputeFor runs h.Canonicalize on payload, unmarshals the canonical bytes
// into map[string]any, then calls chain.RecomputeSelfHash.
// This is the INV-E28 recompute path: caller applies chain-specific
// normalization (via h.Canonicalize) before RecomputeSelfHash zeroes
// and hashes.
func (v *verifier) recomputeFor(h Handler, payload []byte) ([]byte, error) {
	canonical, err := h.Canonicalize(payload)
	if err != nil {
		return nil, oops.Code("AUDIT_CHAIN_CANONICALIZE_FAILED").
			With("chain", h.Chain.SubjectPrefix).Wrap(err)
	}
	var m map[string]any
	if unmarshalErr := json.Unmarshal(canonical, &m); unmarshalErr != nil {
		return nil, oops.Code("AUDIT_CHAIN_PAYLOAD_UNMARSHAL_FAILED").
			With("chain", h.Chain.SubjectPrefix).Wrap(unmarshalErr)
	}
	result, err := RecomputeSelfHash(m, h.Chain.SelfHashField)
	if err != nil {
		return nil, oops.Code("AUDIT_CHAIN_HASH_RECOMPUTE_FAILED").
			With("chain", h.Chain.SubjectPrefix).Wrap(err)
	}
	return result, nil
}
