// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus/audit/chain"
)

// AuditPublisher is the narrow publish seam for RekeyAuditEmitter.
//
// It avoids importing internal/eventbus directly (that package imports dek,
// creating a cycle). Production callers adapt eventbus.Publisher via
// NewEventBusAuditPublisher or similar; tests supply a simple struct.
//
// subject is the full NATS subject (e.g. "events.g1.system.rekey.scene.01ABC").
// evType is the EventBus event type (e.g. "crypto.system.rekey").
// payload is the JSON-serialized RekeyAuditPayload.
// Returns the minted event ULID so callers can correlate the audit row.
type AuditPublisher interface {
	PublishAudit(ctx context.Context, subject, evType string, payload []byte) (ulid.ULID, error)
}

// RekeyAuditEmitter fills the rekey_chain block (scope, prev_hash, self_hash)
// on outgoing rekey audit events and publishes them via AuditPublisher.
//
// The caller MUST populate all payload fields (RequestID, Context, OldDEK,
// NewDEK, PolicyHash, StartedAt, CompletedAt, …) before calling Emit.
// Emit adds the chain linkage fields and then publishes.
//
// Thread-safe: no mutable state; chain.Emitter and AuditPublisher must be
// safe for concurrent use (both are in production).
type RekeyAuditEmitter struct {
	chainEmitter chain.Emitter
	publisher    AuditPublisher
}

// NewRekeyAuditEmitter constructs a RekeyAuditEmitter.
// ce provides prev_hash computation from events_audit; pub is the publish
// surface. Both MUST be non-nil.
func NewRekeyAuditEmitter(ce chain.Emitter, pub AuditPublisher) *RekeyAuditEmitter {
	return &RekeyAuditEmitter{chainEmitter: ce, publisher: pub}
}

// Emit fills in the rekey_chain block and publishes the rekey audit event.
// The payload's Context field determines the chain scope ("type:id") and
// the NATS subject ("events.<game>.system.rekey.<ct>.<cid>").
//
// Returns the published eventID, the finalized payload (with scope,
// prev_hash, self_hash filled in), and any error. The caller MUST persist
// the finalized payload to the audit-fallback log on error so the recorded
// chain-link fields match what would have been published (INV-CRYPTO-100).
//
// INV-CRYPTO-101: prev_hash is the recomputed self-hash of the tail entry, or nil
// for genesis (empty chain for this scope).
// INV-CRYPTO-115: self_hash is SHA-256(JCS(zero(payload, "rekey_chain.self_hash"))).
func (e *RekeyAuditEmitter) Emit(ctx context.Context, payload RekeyAuditPayload) (ulid.ULID, RekeyAuditPayload, error) {
	h := RekeyHandlerFor(currentGameIDForRekey)
	scope := payload.Context.Type + ":" + payload.Context.ID

	// Step 1: compute prev_hash from the current chain head.
	prevHashBytes, _, err := e.chainEmitter.ComputePrevHashFor(ctx, h, scope)
	if err != nil {
		return ulid.ULID{}, payload, oops.Code("DEK_REKEY_AUDIT_PREV_HASH_FAILED").Wrap(err)
	}

	// Encode prev_hash as "sha256:<hex>" string, or nil for genesis.
	payload.RekeyChainField.Scope = scope
	payload.RekeyChainField.PrevHash = encodeHashPtr(prevHashBytes)
	payload.RekeyChainField.SelfHash = "" // zeroed before self-hash computation

	// Step 2: marshal with zeroed self_hash; compute self_hash via
	// chain.RecomputeSelfHash (INV-CRYPTO-115 pinned composition).
	raw, err := json.Marshal(&payload)
	if err != nil {
		return ulid.ULID{}, payload, oops.Code("DEK_REKEY_AUDIT_MARSHAL_FAILED").Wrap(err)
	}
	var m map[string]any
	if err = json.Unmarshal(raw, &m); err != nil {
		return ulid.ULID{}, payload, oops.Code("DEK_REKEY_AUDIT_UNMARSHAL_FAILED").Wrap(err)
	}
	selfHashBytes, err := chain.RecomputeSelfHash(m, h.Chain.SelfHashField)
	if err != nil {
		return ulid.ULID{}, payload, oops.Code("DEK_REKEY_AUDIT_SELF_HASH_FAILED").Wrap(err)
	}
	payload.RekeyChainField.SelfHash = encodeHash(selfHashBytes)

	// Step 3: re-marshal with self_hash populated.
	raw, err = json.Marshal(&payload)
	if err != nil {
		return ulid.ULID{}, payload, oops.Code("DEK_REKEY_AUDIT_REMARSHAL_FAILED").Wrap(err)
	}

	// Step 4: publish via the narrow AuditPublisher seam. On failure, the
	// finalized payload (with chain-link fields populated) is returned so
	// the caller's fallback log persists the exact record that would have
	// been emitted (INV-CRYPTO-100).
	subject := h.SubjectFor(scope)
	eventID, err := e.publisher.PublishAudit(ctx, subject, rekeyEventType, raw)
	if err != nil {
		return ulid.ULID{}, payload, oops.Code("DEK_REKEY_AUDIT_PUBLISH_FAILED").
			With("subject", subject).Wrap(err)
	}
	return eventID, payload, nil
}

// rekeyEventType is the EventBus event type for rekey audit events.
// Matches RekeyChain.EventType declared in audit_chain.go and spec §3.7.
const rekeyEventType = "crypto.system.rekey"

// encodeHash encodes SHA-256 bytes as a "sha256:<hex>" string.
// Stored in rekey_chain.self_hash and rekey_chain.prev_hash so that
// JSON round-trips are byte-stable without base64 padding surprises.
func encodeHash(b []byte) string {
	return fmt.Sprintf("sha256:%s", hex.EncodeToString(b))
}

// encodeHashPtr returns nil when b is nil (genesis), or a pointer to the
// encoded hash string otherwise.
func encodeHashPtr(b []byte) *string {
	if b == nil {
		return nil
	}
	s := encodeHash(b)
	return &s
}

// RekeyHandlerFor bundles the [chain.Chain] metadata with per-chain
// extraction and canonicalization callbacks for the system.rekey audit chain.
// Registered at wiring time with [chain.VerifierSubsystem] alongside
// [policy.PolicySetHandlerFor].
//
// SubjectFor converts scope "ct:cid" → "events.<game>.system.rekey.<ct>.<cid>".
// ScopeFromSubject is the inverse (INV-CRYPTO-114).
// ScopeFromPayload extracts scope from context.type:context.id (INV-CRYPTO-114).
// Canonicalize applies plain JCS (no empty-form normalization needed, spec §3.7).
// PrevHashOf and SelfHashOf extract the respective chain-link fields.
//
// Parallel to policy.PolicySetHandlerFor (post-R6 amendment, spec §3.6).
func RekeyHandlerFor(gameID string) chain.Handler {
	c := RekeyChainFor(gameID)
	prefixWithDot := c.SubjectPrefix + "."
	return chain.Handler{
		Chain: c,
		SubjectFor: func(scope string) string {
			// scope is "ct:cid"; convert to "events.<game>.system.rekey.<ct>.<cid>"
			ct, cid, _ := strings.Cut(scope, ":")
			return prefixWithDot + ct + "." + cid
		},
		ScopeFromSubject: func(subject string) (string, error) {
			if !strings.HasPrefix(subject, prefixWithDot) {
				return "", oops.Code("DEK_REKEY_SCOPE_FROM_SUBJECT_FAILED").
					With("subject", subject).
					With("expected_prefix", prefixWithDot).
					Errorf("subject prefix mismatch")
			}
			rest := subject[len(prefixWithDot):]
			parts := strings.SplitN(rest, ".", 2)
			if len(parts) != 2 {
				return "", oops.Code("DEK_REKEY_SCOPE_FROM_SUBJECT_FAILED").
					With("subject", subject).
					Errorf("expected <ct>.<cid> after prefix")
			}
			return parts[0] + ":" + parts[1], nil
		},
		ScopeFromPayload: parseRekeyScopeFromPayload,
		Canonicalize:     canonicalizeRekeyPayload,
		PrevHashOf:       extractRekeyPrevHash,
		SelfHashOf:       extractRekeySelfHash,
	}
}

// Compile-time assertion: core package is imported for ULID generation.
var _ = core.NewULID
