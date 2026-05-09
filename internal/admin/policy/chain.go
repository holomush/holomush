// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"crypto/sha256"
	"encoding/json"
	"time"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/samber/oops"
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
	PolicyHash      []byte         `json:"policy_hash"`      // computed; excluded from canon-input
	PrevHash        []byte         `json:"prev_hash"`        // null at genesis
	ServerStartULID string         `json:"server_start_ulid"`
	ServerIdentity  string         `json:"server_identity"`
	Timestamp       time.Time      `json:"timestamp"`
}

// ComputePolicyHash returns SHA-256 over RFC 8785 JCS-canonicalized JSON
// of payload with the policy_hash field zeroed out. INV-D12, INV-D13.
//
// Caller pattern: build the payload with PolicyHash empty, call
// ComputePolicyHash, set the result onto payload.PolicyHash, then marshal
// the populated payload for storage.
func ComputePolicyHash(payload *PolicySetPayload) ([]byte, error) {
	canon := *payload
	canon.PolicyHash = nil
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
