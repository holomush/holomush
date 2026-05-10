// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"

	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// chainEntry is one decoded events_audit row for the chain subject.
type chainEntry struct {
	Seq     int64
	Payload PolicySetPayload
}

// VerifyChain validates the integrity of the policy_set chain for one
// policy_name (identified by subject). Per INV-D10/D11/D12. Reads
// events_audit ORDER BY js_seq, two-step decodes envelope -> JSON payload,
// walks the chain, and recomputes each event's policy_hash to catch
// payload tampering.
//
// Returns nil on empty chain (fresh DB; the emitter writes the genesis row).
// Returns a typed POLICY_CHAIN_* error on any integrity failure.
func VerifyChain(ctx context.Context, pool *pgxpool.Pool, subject, policyName string) error {
	entries, err := loadChainEntries(ctx, pool, subject)
	if err != nil {
		return oops.Code("POLICY_CHAIN_LOAD_FAILED").
			With("subject", subject).Wrap(err)
	}
	return verifyChainEntries(entries, policyName)
}

// verifyChainEntries is the integrity check separated from the data source
// so unit tests can drive canned chainEntry slices without a database.
// INV-D10: genesis prev_hash is nil. INV-D11: each entry's prev_hash equals
// the predecessor's recomputed policy_hash. INV-D12: each entry's stored
// policy_hash equals the recomputed hash over its own canonicalized payload.
//
// Cross-checks each entry's Payload.PolicyName against the expected
// policyName argument. The loader queries by subject, but the payload's
// PolicyName is independent JSON — a row whose subject and PolicyName
// disagree is a chain-breaking corruption (or a misconfigured emitter)
// and surfaces as POLICY_CHAIN_NAME_MISMATCH.
func verifyChainEntries(entries []chainEntry, policyName string) error {
	if len(entries) == 0 {
		return nil
	}
	if entries[0].Payload.PolicyName != policyName {
		return oops.Code("POLICY_CHAIN_NAME_MISMATCH").
			With("policy_name", policyName).
			With("payload_policy_name", entries[0].Payload.PolicyName).
			With("js_seq", entries[0].Seq).
			Errorf("payload.policy_name does not match expected policy_name")
	}
	if entries[0].Payload.PrevHash != nil {
		return oops.Code("POLICY_CHAIN_BROKEN_GENESIS").
			With("policy_name", policyName).
			With("js_seq", entries[0].Seq).
			Errorf("first event has non-null prev_hash")
	}
	// Genesis row: policy_hash MUST match its own canonicalized payload.
	genHash, err := ComputePolicyHash(&entries[0].Payload)
	if err != nil {
		return oops.Code("POLICY_CHAIN_HASH_RECOMPUTE_FAILED").
			With("policy_name", policyName).Wrap(err)
	}
	if !bytes.Equal(entries[0].Payload.PolicyHash, genHash) {
		return oops.Code("POLICY_CHAIN_HASH_MISMATCH").
			With("policy_name", policyName).
			With("js_seq", entries[0].Seq).
			Errorf("genesis policy_hash does not match canonicalized payload")
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].Payload.PolicyName != policyName {
			return oops.Code("POLICY_CHAIN_NAME_MISMATCH").
				With("policy_name", policyName).
				With("payload_policy_name", entries[i].Payload.PolicyName).
				With("js_seq", entries[i].Seq).
				Errorf("payload.policy_name does not match expected policy_name")
		}
		prevHash, err := ComputePolicyHash(&entries[i-1].Payload)
		if err != nil {
			return oops.Code("POLICY_CHAIN_HASH_RECOMPUTE_FAILED").
				With("policy_name", policyName).Wrap(err)
		}
		if !bytes.Equal(entries[i].Payload.PrevHash, prevHash) {
			return oops.Code("POLICY_CHAIN_BROKEN_LINK").
				With("policy_name", policyName).
				With("js_seq", entries[i].Seq).
				Errorf("prev_hash does not match predecessor's policy_hash")
		}
		recomputed, err := ComputePolicyHash(&entries[i].Payload)
		if err != nil {
			return oops.Code("POLICY_CHAIN_HASH_RECOMPUTE_FAILED").
				With("policy_name", policyName).Wrap(err)
		}
		if !bytes.Equal(entries[i].Payload.PolicyHash, recomputed) {
			return oops.Code("POLICY_CHAIN_HASH_MISMATCH").
				With("policy_name", policyName).
				With("js_seq", entries[i].Seq).
				Errorf("policy_hash does not match canonicalized payload")
		}
	}
	return nil
}

// loadChainEntries reads events_audit rows for the given subject ordered by
// js_seq and two-step decodes each row: proto unmarshal (envelope) -> JSON
// unmarshal (PolicySetPayload).
//
// The query is scoped to type='crypto.policy_set' so foreign rows that
// happen to share the subject (e.g., misrouted plugin emits) cannot be
// folded into the policy chain. After decoding, the envelope's Subject
// and Type are re-checked against the expected values: a mismatch
// surfaces as POLICY_CHAIN_ENVELOPE_MISMATCH (defense-in-depth against
// envelope tampering that left the SQL filter satisfied but corrupted
// the encoded subject/type fields).
func loadChainEntries(ctx context.Context, pool *pgxpool.Pool, subject string) ([]chainEntry, error) {
	rows, err := pool.Query(ctx, `
		SELECT envelope, js_seq
		  FROM events_audit
		 WHERE subject = $1
		   AND type = 'crypto.policy_set'
		 ORDER BY js_seq ASC
	`, subject)
	if err != nil {
		return nil, oops.Code("POLICY_CHAIN_QUERY_FAILED").
			With("subject", subject).Wrap(err)
	}
	defer rows.Close()

	var out []chainEntry
	for rows.Next() {
		var envelopeBytes []byte
		var seq int64
		if err := rows.Scan(&envelopeBytes, &seq); err != nil {
			return nil, oops.Code("POLICY_CHAIN_SCAN_FAILED").
				With("subject", subject).Wrap(err)
		}
		var ev eventbusv1.Event
		if err := proto.Unmarshal(envelopeBytes, &ev); err != nil {
			return nil, oops.Code("POLICY_CHAIN_ENVELOPE_DECODE_FAILED").
				With("js_seq", seq).Wrap(err)
		}
		if ev.Subject != subject || ev.Type != "crypto.policy_set" {
			return nil, oops.Code("POLICY_CHAIN_ENVELOPE_MISMATCH").
				With("subject", subject).
				With("js_seq", seq).
				With("envelope_subject", ev.Subject).
				With("envelope_type", ev.Type).
				Errorf("unexpected envelope subject/type in policy chain row")
		}
		var payload PolicySetPayload
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			return nil, oops.Code("POLICY_CHAIN_PAYLOAD_DECODE_FAILED").
				With("js_seq", seq).Wrap(err)
		}
		out = append(out, chainEntry{Seq: seq, Payload: payload})
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("POLICY_CHAIN_ROWS_ERR").
			With("subject", subject).Wrap(err)
	}
	return out, nil
}
