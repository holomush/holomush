// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package readstream implements the AdminReadStream gRPC handler: operator
// audit-log read access with hash-chained audit events, dual-control support,
// and policy-hash binding. See docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md.
package readstream

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

// ContextRef is the Go mirror of adminv1.ContextRef.
type ContextRef struct {
	Type string   `json:"type"`
	IDs  []string `json:"ids"`
}

// encodeHash mirrors internal/eventbus/crypto/dek/audit.go::encodeHash
// (package-private there). MUST produce byte-identical output to maintain
// cross-chain JCS canonical-form parity (INV-CRYPTO-57).
func encodeHash(b []byte) string {
	return fmt.Sprintf("sha256:%s", hex.EncodeToString(b))
}

// encodeHashPtr returns nil for genesis entries (b == nil), or a pointer to
// the encoded hash string otherwise.
func encodeHashPtr(b []byte) *string {
	if b == nil {
		return nil
	}
	s := encodeHash(b)
	return &s
}

// OperatorReadStartPayload is the JSON-shaped payload of the
// crypto.system.operator_read audit event.
//
// INV-CRYPTO-57: both Requested-* (nullable, capturing defaulting) and Resolved-*
// (always populated) fields for since/until/contexts MUST be present.
type OperatorReadStartPayload struct {
	// Operator identity
	OperatorPlayerID       ulid.ULID `json:"operator_player_id"`
	OperatorSessionTokenID string    `json:"operator_session_token_id"`
	PeerCredUID            uint32    `json:"peercred_uid"`
	PeerCredPID            int32     `json:"peercred_pid,omitempty"`

	// Dual-control (zero values when not used)
	DualControl      bool       `json:"dual_control"`
	ApproverPlayerID *ulid.ULID `json:"approver_player_id,omitempty"`
	ApprovalID       *ulid.ULID `json:"approval_id,omitempty"`

	// Justification (validated: 1..4096 bytes after trim)
	Justification string `json:"justification"`

	// Request: what the operator typed (nullable = defaulted)
	RequestedContexts []ContextRef `json:"requested_contexts"`
	RequestedSince    *time.Time   `json:"requested_since,omitempty"`
	RequestedUntil    *time.Time   `json:"requested_until,omitempty"`

	// Resolved: what the server actually queried
	ResolvedContexts []ContextRef `json:"resolved_contexts"`
	ResolvedSince    time.Time    `json:"resolved_since"`
	ResolvedUntil    time.Time    `json:"resolved_until"`

	// Chain binding — encoded as "sha256:<hex>" string per rekey precedent.
	// RequestID is encoded as the 26-char ULID Base32 string (NOT ulid.ULID)
	// to mirror the rekey-chain precedent (RekeyAuditPayload.RequestID is
	// string); this makes the chain primitive's ScopeFromPayload extraction
	// resilient to ULID-type changes and keeps the audit JSON shape stable.
	PolicyHash string `json:"policy_hash"`
	RequestID  string `json:"request_id"` // 26-char ULID Base32

	// Chain bookkeeping
	SelfHash string  `json:"self_hash"`           // "sha256:<hex>"; emitter populates
	PrevHash *string `json:"prev_hash,omitempty"` // nil at genesis

	// Timing
	StartedAt time.Time `json:"started_at"`
}

// OperatorReadCompletedPayload is the JSON-shaped payload of the
// crypto.system.operator_read_completed audit event.
type OperatorReadCompletedPayload struct {
	RequestID        string    `json:"request_id"`    // 26-char ULID Base32; chain ScopePayloadField; links to start
	TerminatedBy     string    `json:"terminated_by"` //nolint:tagliatelle // canonical field name from spec §3.5
	EventsScanned    int64     `json:"events_scanned"`
	DecryptFailCount int64     `json:"decrypt_fail_count"`
	PolicyHash       string    `json:"policy_hash"` // "sha256:<hex>"
	SelfHash         string    `json:"self_hash"`   // "sha256:<hex>"
	PrevHash         string    `json:"prev_hash"`   // "sha256:<hex>"; non-empty (links to start)
	FinishedAt       time.Time `json:"finished_at"`
}
