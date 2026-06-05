// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package chain provides the generalized audit-chain primitive used by
// hash-chained crypto audit events (crypto.policy_set, crypto.rekey, …).
//
// Each chain is described by a [Chain] registration that names the NATS
// subject prefix, the JSON field carrying the self-hash, the JSON field
// carrying the previous event's hash, and the JSON field carrying the scope
// identifier. [ValidateRegistration] enforces the structural invariants before
// a chain is wired into a subsystem.
//
// Hash algorithm (INV-E28):
//
//	self_hash = SHA-256(JCS_canonicalize(zero(payload, SelfHashFieldName)))
//
// where JCS is RFC 8785 JSON Canonicalization Scheme implemented by
// github.com/cyberphone/json-canonicalization (version pinned in go.mod per
// INV-CRYPTO-80 — changing the library is a chain-breaking master-spec amendment).
package chain

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

// Chain describes a single hash-chained audit-event family.
//
// All registered chains MUST satisfy [ValidateRegistration] before use.
//
// Field names refer to JSON object keys in the flat or nested map[string]any
// payload decoded from Event.Payload. Dot-notation is supported for nested
// fields (e.g. "meta.self_hash" addresses payload["meta"]["self_hash"]).
type Chain struct {
	// SubjectPrefix is the NATS subject prefix for this chain family.
	// MUST start with "events." (INV-E26).
	// Example: "events.game.system.rekey"
	SubjectPrefix string

	// SelfHashField is the dot-path name of the payload field that holds
	// this event's own hash. This field is zeroed before canonicalization
	// to prevent the hash from being its own input (INV-E28).
	SelfHashField string

	// PrevHashField is the dot-path name of the payload field that holds
	// the predecessor event's hash. nil at genesis.
	PrevHashField string

	// ScopePayloadField is the dot-path name of the payload field that
	// identifies the chain's scope (e.g. policy name, context ID).
	// MUST be non-empty (INV-E27).
	ScopePayloadField string
}

// ValidateRegistration returns an error if c does not satisfy the structural
// invariants required of every chain registration.
//
// INV-E26: SubjectPrefix MUST start with "events.".
// INV-E27: ScopePayloadField MUST be non-empty.
// Additionally: SelfHashField and PrevHashField MUST be non-empty.
func ValidateRegistration(c Chain) error {
	if !strings.HasPrefix(c.SubjectPrefix, "events.") {
		return fmt.Errorf(
			"auditchain: SubjectPrefix %q must start with \"events.\" (INV-E26)",
			c.SubjectPrefix,
		)
	}
	if c.ScopePayloadField == "" {
		return fmt.Errorf(
			"auditchain: ScopePayloadField must not be empty for chain %q (INV-E27)",
			c.SubjectPrefix,
		)
	}
	if c.SelfHashField == "" {
		return fmt.Errorf(
			"auditchain: SelfHashField must not be empty for chain %q",
			c.SubjectPrefix,
		)
	}
	if c.PrevHashField == "" {
		return fmt.Errorf(
			"auditchain: PrevHashField must not be empty for chain %q",
			c.SubjectPrefix,
		)
	}
	return nil
}

// ZeroField returns a shallow copy of payload with the field identified by
// fieldPath set to nil. fieldPath supports dot-notation for one level of
// nesting (e.g. "meta.self_hash").
//
// The original map is never mutated. If fieldPath does not exist in payload
// the function returns payload unchanged (no-op).
//
// Nested support is limited to a single dot — deeply nested paths beyond one
// level (e.g. "a.b.c") are treated as a two-segment path where only the first
// dot is used as the separator. Callers that need deeper nesting should model
// it as a flat field.
func ZeroField(payload map[string]any, fieldPath string) map[string]any {
	dotIdx := strings.Index(fieldPath, ".")
	if dotIdx < 0 {
		// Top-level field.
		if _, ok := payload[fieldPath]; !ok {
			return payload
		}
		out := shallowCopy(payload)
		out[fieldPath] = nil
		return out
	}

	// Nested: split into parent key + remainder.
	parent := fieldPath[:dotIdx]
	rest := fieldPath[dotIdx+1:]

	nested, ok := payload[parent].(map[string]any)
	if !ok {
		// Parent key absent or not a map — no-op.
		return payload
	}

	// Recurse into the nested map.
	zeroed := ZeroField(nested, rest)
	out := shallowCopy(payload)
	out[parent] = zeroed
	return out
}

// RecomputeSelfHash computes SHA-256 over the RFC 8785 JCS-canonicalized JSON
// of payload with selfHashField zeroed out. This implements INV-E28:
//
//	self_hash = SHA-256(JCS_canonicalize(zero(payload, SelfHashFieldName)))
//
// nil and []byte{} values in selfHashField are both normalized to nil before
// canonicalization so callers cannot accidentally produce different hashes by
// representing "absent" differently — json.Marshal emits "null" for nil and
// a base64 string for []byte{} (length-zero slice), which JCS-canonicalize
// to different bytes.
func RecomputeSelfHash(payload map[string]any, selfHashField string) ([]byte, error) {
	// Zero the self-hash field so it does not participate in its own hash.
	canon := ZeroField(payload, selfHashField)

	raw, err := json.Marshal(canon)
	if err != nil {
		return nil, fmt.Errorf("auditchain: json.Marshal failed: %w", err)
	}
	canonical, err := jsoncanonicalizer.Transform(raw)
	if err != nil {
		return nil, fmt.Errorf("auditchain: JCS transform failed: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return sum[:], nil
}

// shallowCopy returns a new map with the same top-level key-value pairs as src.
func shallowCopy(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
