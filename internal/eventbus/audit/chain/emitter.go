// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package chain

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// Emitter helps a domain emitter compute the prev_hash to embed in an outgoing
// audit event. It takes a [Handler] so it has access to the chain's
// Canonicalize and SelfHashOf extractors.
//
// The prevEventID return value is *ulid.ULID — since eventbus.EventID is a type
// alias for ulid.ULID, callers that hold eventbus.EventID variables can use it
// directly. The chain package uses ulid.ULID here to avoid an import cycle:
// internal/eventbus imports internal/eventbus/crypto/dek which imports this package.
//
// See docs/superpowers/specs/2026-05-10-event-payload-crypto-phase5-sub-epic-e-design.md §3.6 (R6 amendment).
type Emitter interface {
	// ComputePrevHashFor loads the entries for the given scope, finds the latest
	// entry by JSSeq, and returns:
	//   - prevHash: the recomputed self-hash of the tail entry (via
	//     h.Canonicalize → json.Unmarshal → RecomputeSelfHash).
	//   - prevEventID: always nil for now; chain authors who need it can wire a
	//     SelfEventIDOf extractor later.
	//   - err: non-nil if the repo or recompute fails.
	//
	// If no entries exist (genesis-eligible chain) all three return values are nil.
	ComputePrevHashFor(ctx context.Context, h Handler, scope string) (prevHash []byte, prevEventID *ulid.ULID, err error)
}

// NewEmitter constructs an Emitter backed by repo.
func NewEmitter(repo Repo) Emitter {
	return &emitter{repo: repo}
}

type emitter struct {
	repo Repo
}

// ComputePrevHashFor implements [Emitter].
//
// The scope parameter is the canonical domain scope (e.g. "scene:01ABC").
// LoadEntriesByScope appends scope to the subject prefix to build the DB query
// subject. For chains where the canonical scope uses a different separator than
// the subject suffix (e.g. rekey uses "scene:01ABC" but the subject has
// "scene.01ABC"), we derive the raw suffix via h.SubjectFor(scope), stripping
// the prefix, to match the stored subject value.
func (e *emitter) ComputePrevHashFor(ctx context.Context, h Handler, scope string) ([]byte, *ulid.ULID, error) {
	// Convert canonical scope to the raw suffix for the Repo query.
	// For policy_set, scope == rawSuffix (simple string, no separator difference).
	// For rekey, scope = "scene:01ABC" but subject is "…scene.01ABC";
	// SubjectFor("scene:01ABC") = "events.g1.system.rekey.scene.01ABC";
	// strip prefix → rawSuffix = "scene.01ABC".
	fullSubject := h.SubjectFor(scope)
	prefixDot := h.Chain.SubjectPrefix + "."
	rawSuffix := scope
	if strings.HasPrefix(fullSubject, prefixDot) {
		rawSuffix = fullSubject[len(prefixDot):]
	}

	entries, err := e.repo.LoadEntriesByScope(ctx, h.Chain.SubjectPrefix, rawSuffix)
	if err != nil {
		return nil, nil, oops.Code("AUDIT_CHAIN_LOAD_FAILED").
			With("chain", h.Chain.SubjectPrefix).With("scope", scope).Wrap(err)
	}
	if len(entries) == 0 {
		// Genesis: no predecessor hash.
		return nil, nil, nil
	}
	last := entries[len(entries)-1]

	// Recompute self-hash: canonicalize first (chain-specific normalization),
	// then unmarshal into map[string]any, then call RecomputeSelfHash.
	// This is the same path the Verifier uses (recomputeFor), ensuring the
	// prev_hash the emitter embeds is consistent with what the verifier checks.
	canonical, err := h.Canonicalize(last.Payload)
	if err != nil {
		return nil, nil, oops.Code("AUDIT_CHAIN_CANONICALIZE_FAILED").
			With("chain", h.Chain.SubjectPrefix).With("scope", scope).Wrap(err)
	}
	var m map[string]any
	if unmarshalErr := json.Unmarshal(canonical, &m); unmarshalErr != nil {
		return nil, nil, oops.Code("AUDIT_CHAIN_PAYLOAD_UNMARSHAL_FAILED").
			With("chain", h.Chain.SubjectPrefix).With("scope", scope).Wrap(unmarshalErr)
	}
	hash, err := RecomputeSelfHash(m, h.Chain.SelfHashField)
	if err != nil {
		return nil, nil, oops.Code("AUDIT_CHAIN_HASH_RECOMPUTE_FAILED").
			With("chain", h.Chain.SubjectPrefix).With("scope", scope).Wrap(err)
	}
	// prevEventID: not yet implemented (no SelfEventIDOf extractor in Handler).
	// Domain emitters that need the predecessor event ID can wire it directly
	// from the audit-chain entry once a SelfEventIDOf callback is added.
	return hash, nil, nil
}
