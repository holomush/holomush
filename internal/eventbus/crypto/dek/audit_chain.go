// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package dek — RekeyAuditPayload type, helpers, and RekeyChain registration.
//
// RekeyAuditPayload is the JSON shape for the crypto.system.rekey audit event.
// Codec is identity (cleartext) per master spec §8.5; the event rides the
// system.rekey hash chain via the auditchain primitive.
//
// RekeyChainFor(gameID) returns the [chain.Chain] descriptor for the
// system.rekey chain registered in the generalized auditchain verifier.
// The subject prefix is "events.<gameID>.system.rekey" (INV-CRYPTO-113).
// ScopePayloadField is "context" (INV-CRYPTO-114).
// SelfHashField is "rekey_chain.self_hash" (INV-CRYPTO-115).
package dek

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus/audit/chain"
)

// RekeyAuditPayload is the JSON shape for the rekey audit event payload.
// Codec is identity (cleartext) per master spec §8.5; rides the
// system.rekey chain via auditchain primitive.
type RekeyAuditPayload struct {
	RequestID            string            `json:"request_id"`
	Context              RekeyAuditContext `json:"context"`
	OldDEK               RekeyAuditDEK     `json:"old_dek"`
	NewDEK               RekeyAuditDEK     `json:"new_dek"`
	PrimaryOperator      RekeyAuditOp      `json:"primary_operator"`
	DualControlPartner   *RekeyAuditPart   `json:"dual_control_partner,omitempty"`
	Justification        string            `json:"justification"`
	PolicyHash           string            `json:"policy_hash"`
	PolicyChainGenesisID string            `json:"policy_chain_genesis_id"`
	Phases               RekeyAuditPhases  `json:"phases"`
	ForceDestroy         bool              `json:"force_destroy"`
	StartedAt            time.Time         `json:"started_at"`
	CompletedAt          time.Time         `json:"completed_at"`
	ServerIdentity       string            `json:"server_identity"`
	SpecVersion          string            `json:"spec_version"`
	RekeyChainField      RekeyChainBlock   `json:"rekey_chain"`
}

// RekeyAuditContext is the context (type+id) embedded in the rekey audit payload.
type RekeyAuditContext struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// RekeyAuditDEK holds the DEK identity embedded in the rekey audit payload.
type RekeyAuditDEK struct {
	ID      int64  `json:"id"`
	Version uint32 `json:"version"`
}

// RekeyAuditOp holds the primary operator's identity in the rekey audit payload.
type RekeyAuditOp struct {
	PlayerID         string `json:"player_id"`
	OSUser           string `json:"os_user"`
	TOTPVerified     bool   `json:"totp_verified"`
	AuthProviderName string `json:"auth_provider_name"`
}

// RekeyAuditPart holds the dual-control partner's identity (null when single-control).
type RekeyAuditPart struct {
	PlayerID          string `json:"player_id"`
	ApprovalRequestID string `json:"approval_request_id"`
}

// RekeyAuditPhases holds per-phase metrics embedded in the rekey audit payload.
type RekeyAuditPhases struct {
	Phase3RowsRewritten       int       `json:"phase3_rows_rewritten"`
	Phase5Attempts            int       `json:"phase5_attempts"`
	Phase5FinalMissingMembers []string  `json:"phase5_final_missing_members"`
	Phase6DestroyedAt         time.Time `json:"phase6_destroyed_at"`
}

// RekeyChainBlock holds the chain linkage fields inside the rekey audit payload.
// PrevHash and SelfHash are stored as "sha256:..." hex strings so JSON
// round-trips are byte-stable without base64 encoding.
type RekeyChainBlock struct {
	Scope       string  `json:"scope"`
	PrevHash    *string `json:"prev_hash"` // null at genesis
	PrevEventID string  `json:"prev_event_id"`
	SelfHash    string  `json:"self_hash"`
}

// canonicalizeRekeyPayload uses plain JCS (RFC 8785). Rekey payloads have no
// nullable byte-slice fields that require empty-form normalization (unlike
// PolicySetChain). Spec §3.7.
//
// Unexported — INV-CRYPTO-16 prohibits exported []byte returns in the dek package.
// Used by RekeyChain registration; tested via audit_chain_internal_test.go.
func canonicalizeRekeyPayload(payload []byte) ([]byte, error) {
	// Parse to map[string]any so JCS key-sorting is applied over the full
	// nested structure, then re-marshal + transform.
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		return nil, oops.Code("DEK_REKEY_CANONICALIZE_FAILED").Wrap(err)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, oops.Code("DEK_REKEY_CANONICALIZE_FAILED").Wrap(err)
	}
	canonical, err := jsoncanonicalizer.Transform(raw)
	if err != nil {
		return nil, oops.Code("DEK_REKEY_CANONICALIZE_FAILED").
			Errorf("JCS transform failed: %w", err)
	}
	return canonical, nil
}

// parseRekeyScopeFromPayload extracts the scope key "<context.type>:<context.id>"
// from the raw JSON payload. Satisfies INV-CRYPTO-114 (independent payload-derived scope).
//
// Unexported — called from RekeyChain registration and tested internally.
func parseRekeyScopeFromPayload(payload []byte) (string, error) {
	var p struct {
		Context RekeyAuditContext `json:"context"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return "", oops.Code("DEK_REKEY_SCOPE_FROM_PAYLOAD_FAILED").Wrap(err)
	}
	if p.Context.Type == "" || p.Context.ID == "" {
		return "", oops.Code("DEK_REKEY_SCOPE_FROM_PAYLOAD_FAILED").
			Errorf("context.type or context.id empty")
	}
	return p.Context.Type + ":" + p.Context.ID, nil
}

// ParseRekeyScopeFromSubject extracts the scope key "<ct>:<cid>" from a
// rekey audit event subject of the form
// "events.<game>.system.rekey.<ct>.<cid>".
//
// Exported for use by bead .17 (RekeyAuditEmitter) which constructs
// subjects dynamically. Does not return []byte so INV-CRYPTO-16 is satisfied.
func ParseRekeyScopeFromSubject(subject string) (string, error) {
	prefix := "events." + currentGameIDForRekey + ".system.rekey."
	if !strings.HasPrefix(subject, prefix) {
		return "", oops.Code("DEK_REKEY_SCOPE_FROM_SUBJECT_FAILED").
			With("subject", subject).
			With("expected_prefix", prefix).
			Errorf("subject prefix mismatch")
	}
	rest := subject[len(prefix):]
	parts := strings.SplitN(rest, ".", 2)
	if len(parts) != 2 {
		return "", oops.Code("DEK_REKEY_SCOPE_FROM_SUBJECT_FAILED").
			With("subject", subject).
			Errorf("expected <ct>.<cid> after prefix")
	}
	return parts[0] + ":" + parts[1], nil
}

// decodeHashString decodes a "sha256:<hex>" encoded hash back to raw bytes.
// Returns nil when s is empty (genesis prev_hash absent). This is the inverse
// of audit.go's encodeHash / encodeHashPtr — the chain verifier and emitter
// call SelfHashOf / PrevHashOf to extract stored bytes, then compare them to
// raw bytes returned by chain.RecomputeSelfHash (which returns a 32-byte
// SHA-256 digest). Both must be in the same format for bytes.Equal to hold.
func decodeHashString(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	const prefix = "sha256:"
	if !strings.HasPrefix(s, prefix) {
		return nil, oops.Code("DEK_REKEY_HASH_DECODE_FAILED").
			With("value", s).
			Errorf("hash string must start with %q", prefix)
	}
	b, err := hex.DecodeString(s[len(prefix):])
	if err != nil {
		return nil, oops.Code("DEK_REKEY_HASH_DECODE_FAILED").
			With("value", s).Wrap(err)
	}
	return b, nil
}

// extractRekeyPrevHash extracts the prev_hash raw bytes from the rekey_chain
// block. Returns nil for a genesis entry (prev_hash is null or absent).
// Decodes from the "sha256:<hex>" string format stored in JSON so the returned
// bytes match the 32-byte format produced by chain.RecomputeSelfHash.
func extractRekeyPrevHash(payload []byte) ([]byte, error) {
	var p struct {
		RekeyChain struct {
			PrevHash *string `json:"prev_hash"`
		} `json:"rekey_chain"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, oops.Code("DEK_REKEY_EXTRACT_PREV_HASH_FAILED").Wrap(err)
	}
	if p.RekeyChain.PrevHash == nil {
		return nil, nil
	}
	raw, err := decodeHashString(*p.RekeyChain.PrevHash)
	if err != nil {
		return nil, oops.Code("DEK_REKEY_EXTRACT_PREV_HASH_FAILED").Wrap(err)
	}
	return raw, nil
}

// extractRekeySelfHash extracts the self_hash raw bytes from the rekey_chain
// block. Decodes from the "sha256:<hex>" string format stored in JSON so the
// returned bytes match the 32-byte format produced by chain.RecomputeSelfHash.
func extractRekeySelfHash(payload []byte) ([]byte, error) {
	var p struct {
		RekeyChain struct {
			SelfHash string `json:"self_hash"`
		} `json:"rekey_chain"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, oops.Code("DEK_REKEY_EXTRACT_SELF_HASH_FAILED").Wrap(err)
	}
	raw, err := decodeHashString(p.RekeyChain.SelfHash)
	if err != nil {
		return nil, oops.Code("DEK_REKEY_EXTRACT_SELF_HASH_FAILED").Wrap(err)
	}
	return raw, nil
}

// currentGameIDForRekey is set at boot from cfg.Game.ID via SetGameIDForRekey.
var currentGameIDForRekey string

// SetGameIDForRekey configures the game ID used in rekey audit event subjects.
// Call once at startup before registering RekeyChain.
func SetGameIDForRekey(g string) { currentGameIDForRekey = g }

// SetGameIDForTest is a test-only shim. Production code uses SetGameIDForRekey.
func SetGameIDForTest(g string) { currentGameIDForRekey = g }

// GameIDForTest returns the current rekey game ID; tests use it with
// SetGameIDForTest + t.Cleanup to restore prior state.
func GameIDForTest() string { return currentGameIDForRekey }

// RekeyChainFor returns the [chain.Chain] descriptor for the system.rekey
// hash chain parameterized by gameID. The subject prefix embeds gameID so
// the verifier can scope its SQL LIKE query.
//
// INV-CRYPTO-113: SubjectPrefix starts with "events.".
// INV-CRYPTO-114: ScopePayloadField is "context" (non-empty).
// INV-CRYPTO-115: SelfHashField is "rekey_chain.self_hash".
func RekeyChainFor(gameID string) chain.Chain {
	return chain.Chain{
		SubjectPrefix:     fmt.Sprintf("events.%s.system.rekey", gameID),
		SelfHashField:     "rekey_chain.self_hash",
		PrevHashField:     "rekey_chain.prev_hash",
		ScopePayloadField: "context",
	}
}
