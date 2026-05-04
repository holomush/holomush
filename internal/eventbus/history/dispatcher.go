// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package history provides the historical-read pathway for both hot
// (JetStream) and cold (PostgreSQL events_audit) tiers.
//
// dispatcher.go: header-free shared logic. Hot path supplies inputs from
// jetstream.Msg headers; cold path supplies inputs from PG row columns.
// Both call decodeAuthorizeAndDispatch.
package history

import (
	"context"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// decodeAuthorizeAndDispatch is the header-free shared dispatcher for
// historical reads. Both the hot tier (JetStream) and cold tier (PG)
// call this with their respective input sources:
//   - hot path: codecName/keyID/keyVersion from msg.Headers()
//   - cold path: codecName/keyID/keyVersion from PG row columns
//
// Returns (event, metadataOnly, error). The metadataOnly flag is true
// when the envelope's payload was redacted by AuthGuard or DEK lookup
// failed (master spec §8.4 terminal branch).
//
// Identity codec short-circuits before any auth/DEK work: payload is
// returned as-is. The hot wrapper currently handles the identity
// branch in its caller (decodeFromEnvelope), but the cold path calls
// this dispatcher uniformly for all codecs, so the early return lives
// here.
func decodeAuthorizeAndDispatch(
	ctx context.Context,
	envelope *eventbusv1.Event,
	codecName codec.Name,
	keyID codec.KeyID,
	keyVersion uint32,
	identity eventbus.SessionIdentity,
	guard eventbus.SessionAuthGuard,
	dekMgr eventbus.SessionDEKManager,
	auditEm eventbus.SessionAuditEmitter,
) (eventbus.Event, bool, error) {
	// Recover event ULID from the pre-stamped bytes.
	var eventID ulid.ULID
	if rawID := envelope.GetId(); len(rawID) == 16 {
		copy(eventID[:], rawID)
	}

	// Identity codec: passthrough. AuthGuard NOT invoked.
	if codecName == codec.NameIdentity {
		return buildHistoryEventFromEnvelope(eventID, envelope, envelope.GetPayload()), false, nil
	}

	// Guard against misconfiguration: cold-tier production wiring
	// currently leaves authGuard nil (pre-Phase-3b passthrough; see
	// Step 5.0a in the Phase 3d grounding doc and the holomush-ojw1
	// follow-up bead "plumb cold-tier auth options through
	// history.NewReader"). A sensitive history read with no guard wired
	// MUST fail in a controlled way rather than panic on guard.Check.
	if guard == nil {
		return eventbus.Event{}, false, oops.Code("EVENTBUS_HISTORY_AUTH_GUARD_NIL").
			With("event_type", envelope.GetType()).
			With("codec", string(codecName)).
			Errorf("encrypted history read requires AuthGuard but got nil — cold-tier wiring is incomplete")
	}

	req := eventbus.SessionCheckRequest{
		Identity:   identity,
		KeyID:      keyID,
		KeyVersion: keyVersion,
		EventType:  envelope.GetType(),
		EventID:    eventID,
	}

	decision, err := guard.Check(ctx, req)
	if err != nil {
		return eventbus.Event{}, false, oops.Code("EVENTBUS_AUTHGUARD_CHECK_FAILED").
			With("event_type", envelope.GetType()).
			Wrap(err)
	}

	if !decision.Permit {
		return buildHistoryEventFromEnvelope(eventID, envelope, nil), true, nil
	}

	// Permit: resolve key, build AAD, decode.
	// Guard against misconfiguration: WithHistoryAuthGuard set without
	// WithHistoryDEKManager. Fail closed rather than panic.
	if dekMgr == nil {
		return eventbus.Event{}, false, oops.Code("EVENTBUS_HISTORY_DEK_MANAGER_NIL").
			Errorf("AuthGuard permitted decrypt but DEKManager is nil — misconfiguration")
	}
	key, err := dekMgr.Resolve(ctx, keyID, keyVersion)
	if err != nil {
		return eventbus.Event{}, false, oops.Code("EVENTBUS_DEK_RESOLVE_FAILED").
			With("key_id", uint64(keyID)).With("key_version", keyVersion).
			Wrap(err)
	}

	aadBytes, err := aad.Build(envelope, string(codecName), uint64(keyID), keyVersion)
	if err != nil {
		return eventbus.Event{}, false, oops.Code("EVENTBUS_AAD_BUILD_FAILED").Wrap(err)
	}

	c, err := codec.Resolve(codecName)
	if err != nil {
		return eventbus.Event{}, false, oops.Code("EVENTBUS_HISTORY_UNKNOWN_CODEC").
			With("codec", string(codecName)).Wrap(err)
	}

	plaintext, err := c.Decode(ctx, envelope.GetPayload(), key, aadBytes)
	if err != nil {
		return eventbus.Event{}, false, oops.Code("EVENTBUS_CODEC_DECODE_FAILED").
			With("codec", string(codecName)).Wrap(err)
	}

	// Plugin recipient: INV-19 — every plugin decrypt MUST produce an audit
	// record. Fail closed if the emitter is absent or fails unexpectedly.
	if identity.Kind == eventbus.IdentityKindPlugin {
		if auditEm == nil {
			// AuthGuard permitted the read but no emitter is wired — configuration
			// error. Fail closed rather than deliver plaintext without audit.
			return eventbus.Event{}, false, oops.Code("EVENTBUS_HISTORY_AUDIT_EMITTER_NIL").
				Errorf("AuthGuard permitted plugin decrypt but no DecryptAuditEmitter configured (INV-19)")
		}
		rec := eventbus.PluginDecryptRecord{
			PluginName:       identity.PluginName,
			PluginInstanceID: identity.InstanceID,
			EventID:          eventID,
			EventSubject:     eventbus.Subject(envelope.GetSubject()),
			EventType:        eventbus.Type(envelope.GetType()),
			DEKRef:           keyID,
			DEKVersion:       keyVersion,
			GrantID:          decision.GrantID,
		}
		if emitErr := auditEm.EmitPluginDecrypt(ctx, rec); emitErr != nil {
			// Narrow: only AUDIT_QUEUE_FULL gets the plaintext-zero +
			// metadata_only fallback (TOCTOU defense per Decision 3).
			// Any other emit error means we cannot confirm the audit
			// landed — fail closed.
			if isHistoryAuditQueueFull(emitErr) {
				for i := range plaintext {
					plaintext[i] = 0
				}
				return buildHistoryEventFromEnvelope(eventID, envelope, nil), true, nil
			}
			return eventbus.Event{}, false, oops.Code("EVENTBUS_HISTORY_AUDIT_EMIT_FAILED").
				With("emit_error", emitErr.Error()).
				Errorf("plugin decrypt audit emit failed — cannot confirm audit landed (INV-19)")
		}
	}

	return buildHistoryEventFromEnvelope(eventID, envelope, plaintext), false, nil
}
