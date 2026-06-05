// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// PolicySetPayload is the body of a crypto.policy_set audit event. It is
// stored as JSON inside Event.Payload (the inner field of the marshaled
// eventbusv1.Event envelope written to events_audit.envelope) per spec §6.
//
// The "policy" prefix in the package name causes a stutter (policy.PolicySetPayload),
// but the name is canonical per the master crypto spec (§6) — do not rename.
//
//nolint:revive // name is canonical per master crypto spec §6 and must not be changed
type PolicySetPayload struct {
	PolicyName      string         `json:"policy_name"`
	PolicySnapshot  map[string]any `json:"policy_snapshot"`
	PolicyHash      []byte         `json:"policy_hash"` // computed; excluded from canon-input
	PrevHash        []byte         `json:"prev_hash"`   // null at genesis
	ServerStartULID string         `json:"server_start_ulid"`
	ServerIdentity  string         `json:"server_identity"`
	Timestamp       time.Time      `json:"timestamp"`
}

// ComputePolicyHash returns SHA-256 over RFC 8785 JCS-canonicalized JSON
// of payload with the policy_hash field zeroed out. INV-CRYPTO-79, INV-CRYPTO-80.
//
// Caller pattern: build the payload with PolicyHash empty, call
// ComputePolicyHash, set the result onto payload.PolicyHash, then marshal
// the populated payload for storage.
//
// nil is the canonical absent form for both PolicyHash and PrevHash. An
// empty (length-zero) []byte is normalized to nil before canonicalization
// so callers cannot accidentally produce different hashes by passing
// `[]byte{}` vs `nil` — `json.Marshal` emits `null` for nil and `""` for
// `[]byte{}`, which canonicalize to different bytes. Genesis rows MUST
// have PrevHash == nil (per INV-CRYPTO-77); this normalization makes the
// genesis hash stable regardless of which empty form the caller used.
//
// Retained post-Phase-5-sub-epic-E refactor as the legacy reference: it
// is byte-equivalent to the generalized
// chain.RecomputeSelfHash(PolicySetHandlerFor(_).Canonicalize(payload), "policy_hash")
// path. The reduction is locked by TestPolicySetChain_ReducibleToDComputePolicyHash.
func ComputePolicyHash(payload *PolicySetPayload) ([]byte, error) {
	canon := *payload
	canon.PolicyHash = nil
	if len(canon.PrevHash) == 0 {
		canon.PrevHash = nil
	}
	raw, err := json.Marshal(&canon)
	if err != nil {
		return nil, oops.Code("POLICY_HASH_JSON_MARSHAL_FAILED").Wrap(err)
	}
	canonical, err := jsoncanonicalizer.Transform(raw)
	if err != nil {
		return nil, oops.Code("POLICY_HASH_JCS_FAILED").Wrap(err)
	}
	sum := sha256.Sum256(canonical)
	return sum[:], nil
}

// PolicySetChainName is the canonical chain name registered with the
// auditchain primitive. Also used as the bootstrap_metadata.chain_name key.
const PolicySetChainName = "crypto.policy_set"

// PolicySetChainFor returns the [chain.Chain] descriptor for the
// crypto.policy_set hash chain parameterized by gameID. The subject prefix
// embeds gameID so the verifier can scope its SQL LIKE query.
//
// Parallels [dek.RekeyChainFor] (Phase 5 sub-epic E sub-epic .16). The
// PolicySet prefix is canonical per the master crypto spec §6 and matches
// the chain's existing event-type name; the stutter against the package
// name is unavoidable without renaming the chain.
//
// INV-CRYPTO-113: SubjectPrefix starts with "events.".
// INV-CRYPTO-114: ScopePayloadField is "policy_name" (non-empty).
// INV-CRYPTO-115: SelfHashField is "policy_hash"; PrevHashField is "prev_hash".
//
//nolint:revive // name is canonical per master crypto spec §6 and matches the chain's event type
func PolicySetChainFor(gameID string) chain.Chain {
	return chain.Chain{
		SubjectPrefix:     fmt.Sprintf("events.%s.system.crypto_policy", gameID),
		SelfHashField:     "policy_hash",
		PrevHashField:     "prev_hash",
		ScopePayloadField: "policy_name",
	}
}

// PolicySetHandlerFor bundles the [chain.Chain] metadata with the per-chain
// extraction / canonicalization callbacks. Registered at wiring time with
// chain.VerifierSubsystem.
//
// Standalone functions back each callback rather than embedding behavior on
// chain.Chain (post-R6 amendment — see spec §3.6).
//
//nolint:revive // name is canonical per master crypto spec §6 and matches the chain's event type
func PolicySetHandlerFor(gameID string) chain.Handler {
	c := PolicySetChainFor(gameID)
	prefixWithDot := c.SubjectPrefix + "."
	return chain.Handler{
		Chain: c,
		SubjectFor: func(scope string) string {
			return prefixWithDot + scope
		},
		ScopeFromSubject: func(subject string) (string, error) {
			if !strings.HasPrefix(subject, prefixWithDot) {
				return "", oops.Code("POLICY_SET_SCOPE_FROM_SUBJECT_FAILED").
					With("subject", subject).
					With("expected_prefix", prefixWithDot).
					Errorf("subject prefix mismatch")
			}
			return subject[len(prefixWithDot):], nil
		},
		ScopeFromPayload: policyScopeFromPayload,
		Canonicalize:     canonicalizePolicySetPayload,
		PrevHashOf:       policyPrevHashOf,
		SelfHashOf:       policySelfHashOf,
	}
}

// decodePolicyPayloadJSON is a per-callback helper that recovers the JSON
// payload bytes from whatever chain.Repo.LoadEntriesByScope returned.
//
// events_audit.envelope holds proto-marshaled eventbusv1.Event for
// production rows (written by audit/projection.go), so the callback first
// tries proto.Unmarshal. If that fails, the payload is assumed to already
// be raw JSON (test fakes inject raw JSON directly, mirroring the chain
// primitive's own unit-test convention; see verifier_test.go in this
// package and internal/eventbus/audit/chain/verifier_test.go for the
// fake-repo pattern).
//
// This is the boundary between the chain primitive's "Entry.Payload is the
// envelope column verbatim" contract and the policy_set chain's
// JSON-shaped payload. It is internal to the policy package.
func decodePolicyPayloadJSON(envOrJSON []byte) ([]byte, error) {
	var ev eventbusv1.Event
	if err := proto.Unmarshal(envOrJSON, &ev); err == nil && ev.Type == "crypto.policy_set" && len(ev.Payload) > 0 {
		return ev.Payload, nil
	}
	// Fall back: assume the bytes are already JSON. Validate by attempting a
	// JSON unmarshal into a generic map — if that fails too, return a typed
	// error so callers can distinguish a malformed envelope from a downstream
	// JSON-shape problem.
	var probe map[string]any
	if err := json.Unmarshal(envOrJSON, &probe); err != nil {
		return nil, oops.Code("POLICY_SET_PAYLOAD_DECODE_FAILED").
			Wrap(err)
	}
	return envOrJSON, nil
}

// canonicalizePolicySetPayload preserves D's INV-CRYPTO-77 PrevHash empty-form → nil
// normalization. Proto-unmarshals the envelope (or accepts raw JSON for test
// fakes), parses the JSON payload bytes into PolicySetPayload, normalizes
// PrevHash, re-marshals, then applies RFC 8785 JCS. This makes the new
// chain.RecomputeSelfHash path byte-equivalent to legacy ComputePolicyHash
// for all D fixtures including PrevHash: []byte{} cases.
//
// Used by [PolicySetHandlerFor].Canonicalize; locked by
// TestPolicySetChain_ReducibleToDComputePolicyHash.
func canonicalizePolicySetPayload(envOrJSON []byte) ([]byte, error) {
	payload, err := decodePolicyPayloadJSON(envOrJSON)
	if err != nil {
		return nil, oops.Code("POLICY_SET_CANON_PAYLOAD_DECODE_FAILED").Wrap(err)
	}
	var p PolicySetPayload
	if uerr := json.Unmarshal(payload, &p); uerr != nil {
		return nil, oops.Code("POLICY_SET_CANON_UNMARSHAL_FAILED").Wrap(uerr)
	}
	if len(p.PrevHash) == 0 {
		p.PrevHash = nil
	}
	raw, err := json.Marshal(&p)
	if err != nil {
		return nil, oops.Code("POLICY_SET_CANON_MARSHAL_FAILED").Wrap(err)
	}
	canonical, err := jsoncanonicalizer.Transform(raw)
	if err != nil {
		return nil, oops.Code("POLICY_SET_CANON_JCS_FAILED").Wrap(err)
	}
	return canonical, nil
}

// policyScopeFromPayload extracts policy_name from the envelope (or raw JSON
// for test fakes). Satisfies INV-CRYPTO-114 (independent payload-derived scope).
func policyScopeFromPayload(envOrJSON []byte) (string, error) {
	payload, err := decodePolicyPayloadJSON(envOrJSON)
	if err != nil {
		return "", oops.Code("POLICY_SET_SCOPE_FROM_PAYLOAD_FAILED").Wrap(err)
	}
	var p struct {
		PolicyName string `json:"policy_name"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return "", oops.Code("POLICY_SET_SCOPE_FROM_PAYLOAD_FAILED").Wrap(err)
	}
	if p.PolicyName == "" {
		return "", oops.Code("POLICY_SET_SCOPE_FROM_PAYLOAD_FAILED").
			Errorf("policy_name empty")
	}
	return p.PolicyName, nil
}

// policyPrevHashOf returns prev_hash bytes from the envelope (or raw JSON
// for test fakes). Returns nil for genesis (prev_hash absent or null).
// []byte fields in JSON unmarshal to nil when the JSON value is null, so
// this naturally produces nil at genesis.
func policyPrevHashOf(envOrJSON []byte) ([]byte, error) {
	payload, err := decodePolicyPayloadJSON(envOrJSON)
	if err != nil {
		return nil, oops.Code("POLICY_SET_PREV_HASH_EXTRACT_FAILED").Wrap(err)
	}
	var p struct {
		PrevHash []byte `json:"prev_hash"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, oops.Code("POLICY_SET_PREV_HASH_EXTRACT_FAILED").Wrap(err)
	}
	if len(p.PrevHash) == 0 {
		return nil, nil
	}
	return p.PrevHash, nil
}

// policySelfHashOf returns policy_hash bytes from the envelope (or raw JSON
// for test fakes).
func policySelfHashOf(envOrJSON []byte) ([]byte, error) {
	payload, err := decodePolicyPayloadJSON(envOrJSON)
	if err != nil {
		return nil, oops.Code("POLICY_SET_SELF_HASH_EXTRACT_FAILED").Wrap(err)
	}
	var p struct {
		PolicyHash []byte `json:"policy_hash"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, oops.Code("POLICY_SET_SELF_HASH_EXTRACT_FAILED").Wrap(err)
	}
	return p.PolicyHash, nil
}
