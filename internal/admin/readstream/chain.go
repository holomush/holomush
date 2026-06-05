// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package readstream

import (
	"encoding/hex"
	"encoding/json"
	"strings"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus/audit/chain"
)

// OperatorReadChainFor returns the [chain.Chain] descriptor for the
// crypto.system.operator_read hash chain parameterized by gameID.
//
// Both crypto.system.operator_read and crypto.system.operator_read_completed
// share the same NATS subject (Path C). The chain primitive's
// LoadEntriesByScope returns both rows ordered by js_seq ASC, giving the
// completed event its predecessor naturally.
//
// INV-CRYPTO-113: SubjectPrefix starts with "events.".
// INV-CRYPTO-114: ScopePayloadField is "request_id" (non-empty).
// INV-CRYPTO-115: SelfHashField is "self_hash"; PrevHashField is "prev_hash".
// INV-CRYPTO-59: Both events share the same NATS subject pattern.
func OperatorReadChainFor(gameID string) chain.Chain {
	return chain.Chain{
		SubjectPrefix:     "events." + gameID + ".system.operator_read",
		SelfHashField:     "self_hash",
		PrevHashField:     "prev_hash",
		ScopePayloadField: "request_id",
	}
}

// OperatorReadHandlerFor bundles the [chain.Chain] metadata with all 7
// per-chain extraction / canonicalization callbacks.
//
// Both event types share the same subject so SubjectFor / ScopeFromSubject
// have a single mapping. Differentiation by Event.Type happens at the
// emitter and consumer level, not at the chain-primitive level.
//
// Parallel to policy.PolicySetHandlerFor and dek.RekeyHandlerFor.
func OperatorReadHandlerFor(gameID string) chain.Handler {
	c := OperatorReadChainFor(gameID)
	prefixDot := c.SubjectPrefix + "."
	return chain.Handler{
		Chain: c,
		SubjectFor: func(scope string) string {
			return prefixDot + scope // scope = request_id ULID string
		},
		ScopeFromSubject: func(subject string) (string, error) {
			if !strings.HasPrefix(subject, prefixDot) {
				return "", oops.Code("OPERATOR_READ_SCOPE_FROM_SUBJECT_FAILED").
					With("subject", subject).
					With("expected_prefix", prefixDot).
					Errorf("subject prefix mismatch")
			}
			scope := subject[len(prefixDot):]
			if scope == "" || strings.Contains(scope, ".") {
				return "", oops.Code("OPERATOR_READ_SCOPE_FROM_SUBJECT_FAILED").
					With("subject", subject).
					With("expected_format", prefixDot+"<request_id>").
					Errorf("invalid scope segment: must be a single non-empty segment")
			}
			return scope, nil
		},
		ScopeFromPayload: operatorReadScopeFromPayload,
		Canonicalize:     canonicalizeOperatorReadPayload,
		PrevHashOf:       operatorReadPrevHashOf,
		SelfHashOf:       operatorReadSelfHashOf,
	}
}

// decodeOperatorReadPayloadJSON recovers the JSON payload bytes from whatever
// chain.Repo.LoadEntriesByScope returned (proto-marshaled envelope or raw JSON).
//
// Production rows hold proto-marshaled eventbusv1.Event in events_audit.envelope.
// Test fakes inject raw JSON directly (mirrors policy package convention).
func decodeOperatorReadPayloadJSON(envOrJSON []byte) ([]byte, error) {
	// Try raw JSON first: parse to map[string]any. Both event types share the
	// same payload schema (request_id, self_hash, prev_hash) so a single decode
	// path covers both.
	var probe map[string]any
	if err := json.Unmarshal(envOrJSON, &probe); err == nil {
		return envOrJSON, nil
	}

	// Fall back: proto-unmarshal the eventbusv1.Event envelope and extract Payload.
	// Import cycle prevention: we avoid importing eventbusv1 here by doing a
	// manual proto field extraction using the known field number (4 = payload bytes
	// in the eventbus v1 proto) via a minimal protowire scan.
	payload, err := extractProtoPayloadField(envOrJSON)
	if err != nil || len(payload) == 0 {
		return nil, oops.Code("OPERATOR_READ_PAYLOAD_DECODE_FAILED").
			Errorf("bytes are neither valid JSON nor a recognized proto envelope")
	}
	return payload, nil
}

// canonicalizeOperatorReadPayload applies plain JCS (RFC 8785). Both event
// types carry the same chain fields (self_hash, prev_hash, request_id) so a
// single canonicalizer covers both. No empty-form normalization is needed
// (fields are sha256:<hex> strings, not []byte).
func canonicalizeOperatorReadPayload(envOrJSON []byte) ([]byte, error) {
	payload, err := decodeOperatorReadPayloadJSON(envOrJSON)
	if err != nil {
		return nil, oops.Code("OPERATOR_READ_CANON_DECODE_FAILED").Wrap(err)
	}
	var m map[string]any
	if uerr := json.Unmarshal(payload, &m); uerr != nil {
		return nil, oops.Code("OPERATOR_READ_CANON_UNMARSHAL_FAILED").Wrap(uerr)
	}
	raw, merr := json.Marshal(m)
	if merr != nil {
		return nil, oops.Code("OPERATOR_READ_CANON_MARSHAL_FAILED").Wrap(merr)
	}
	canonical, jerr := jsoncanonicalizer.Transform(raw)
	if jerr != nil {
		return nil, oops.Code("OPERATOR_READ_CANON_JCS_FAILED").
			Errorf("JCS transform failed: %w", jerr)
	}
	return canonical, nil
}

// operatorReadScopeFromPayload extracts request_id from the envelope (or raw
// JSON for test fakes). Satisfies INV-CRYPTO-114 (independent payload-derived scope).
func operatorReadScopeFromPayload(envOrJSON []byte) (string, error) {
	payload, err := decodeOperatorReadPayloadJSON(envOrJSON)
	if err != nil {
		return "", oops.Code("OPERATOR_READ_SCOPE_FROM_PAYLOAD_FAILED").Wrap(err)
	}
	var p struct {
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return "", oops.Code("OPERATOR_READ_SCOPE_FROM_PAYLOAD_FAILED").Wrap(err)
	}
	if p.RequestID == "" {
		return "", oops.Code("OPERATOR_READ_SCOPE_FROM_PAYLOAD_FAILED").
			Errorf("request_id empty")
	}
	return p.RequestID, nil
}

// operatorReadPrevHashOf returns prev_hash bytes from the envelope (or raw
// JSON for test fakes). Returns nil for genesis (prev_hash absent or null).
// Decodes from the "sha256:<hex>" string format.
func operatorReadPrevHashOf(envOrJSON []byte) ([]byte, error) {
	payload, err := decodeOperatorReadPayloadJSON(envOrJSON)
	if err != nil {
		return nil, oops.Code("OPERATOR_READ_PREV_HASH_EXTRACT_FAILED").Wrap(err)
	}
	var p struct {
		PrevHash *string `json:"prev_hash"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, oops.Code("OPERATOR_READ_PREV_HASH_EXTRACT_FAILED").Wrap(err)
	}
	if p.PrevHash == nil {
		return nil, nil
	}
	return decodeHashString(*p.PrevHash)
}

// operatorReadSelfHashOf returns self_hash bytes from the envelope (or raw
// JSON for test fakes). Decodes from the "sha256:<hex>" string format.
func operatorReadSelfHashOf(envOrJSON []byte) ([]byte, error) {
	payload, err := decodeOperatorReadPayloadJSON(envOrJSON)
	if err != nil {
		return nil, oops.Code("OPERATOR_READ_SELF_HASH_EXTRACT_FAILED").Wrap(err)
	}
	var p struct {
		SelfHash string `json:"self_hash"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, oops.Code("OPERATOR_READ_SELF_HASH_EXTRACT_FAILED").Wrap(err)
	}
	return decodeHashString(p.SelfHash)
}

// decodeHashString decodes a "sha256:<hex>" encoded hash back to raw bytes.
// Returns nil when s is empty (genesis prev_hash absent). Mirrors
// internal/eventbus/crypto/dek/audit_chain.go::decodeHashString.
func decodeHashString(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	const prefix = "sha256:"
	if !strings.HasPrefix(s, prefix) {
		return nil, oops.Code("OPERATOR_READ_HASH_DECODE_FAILED").
			With("value", s).
			Errorf("hash string must start with %q", prefix)
	}
	b, err := hex.DecodeString(s[len(prefix):])
	if err != nil {
		return nil, oops.Code("OPERATOR_READ_HASH_DECODE_FAILED").
			With("value", s).Wrap(err)
	}
	return b, nil
}

// extractProtoPayloadField performs a minimal protowire scan to extract
// field 6 (payload bytes) from a serialized eventbusv1.Event envelope.
// Field 6 is the payload field per the eventbus v1 proto schema (field 4
// is Timestamp — a common transcription error caught by F-E1's audit-row
// assertion against real events_audit envelopes, sub-epic F r8 R.17).
// This avoids importing the proto package (which would create a cycle) while
// still supporting production envelopes stored in events_audit.envelope.
func extractProtoPayloadField(b []byte) ([]byte, error) {
	// Protobuf wire format: each field is (tag << 3 | wire_type), value.
	// Field 6, wire type 2 (length-delimited) → tag = (6 << 3) | 2 = 0x32.
	for len(b) > 0 {
		tag, n := decodeVarint(b)
		if n <= 0 {
			break
		}
		b = b[n:]
		wireType := tag & 0x7
		fieldNum := tag >> 3
		switch wireType {
		case 0: // varint
			_, n2 := decodeVarint(b)
			if n2 <= 0 {
				return nil, oops.Code("OPERATOR_READ_PROTO_SCAN_FAILED").Errorf("varint decode failed")
			}
			b = b[n2:]
		case 2: // length-delimited
			length, n2 := decodeVarint(b)
			if n2 <= 0 {
				return nil, oops.Code("OPERATOR_READ_PROTO_SCAN_FAILED").Errorf("length varint decode failed")
			}
			b = b[n2:]
			if length > uint64(len(b)) {
				return nil, oops.Code("OPERATOR_READ_PROTO_SCAN_FAILED").Errorf("truncated field")
			}
			if fieldNum == 6 {
				// Field 6 is the payload bytes in eventbusv1.Event
				// (per api/proto/holomush/eventbus/v1/eventbus.proto:41).
				result := make([]byte, length)
				copy(result, b[:length])
				return result, nil
			}
			b = b[length:]
		case 5: // 32-bit
			if len(b) < 4 {
				return nil, oops.Code("OPERATOR_READ_PROTO_SCAN_FAILED").Errorf("truncated 32-bit field")
			}
			b = b[4:]
		case 1: // 64-bit
			if len(b) < 8 {
				return nil, oops.Code("OPERATOR_READ_PROTO_SCAN_FAILED").Errorf("truncated 64-bit field")
			}
			b = b[8:]
		default:
			return nil, oops.Code("OPERATOR_READ_PROTO_SCAN_FAILED").
				With("wire_type", wireType).Errorf("unknown wire type")
		}
	}
	return nil, nil
}

// decodeVarint decodes a protobuf varint from b.
// Returns (value, bytesConsumed). bytesConsumed <= 0 indicates failure.
func decodeVarint(b []byte) (value uint64, bytesConsumed int) {
	var x uint64
	var s uint
	for i, c := range b {
		if i == 10 {
			return 0, -1 // overflow
		}
		x |= uint64(c&0x7f) << s
		s += 7
		if c < 0x80 {
			return x, i + 1
		}
	}
	return 0, 0
}
