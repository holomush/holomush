// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package history provides the historical-read pathway for both hot
// (JetStream) and cold (PostgreSQL events_audit) tiers.
//
// dispatcher.go: header-free shared logic. Hot path supplies inputs from
// jetstream.Msg headers; cold path supplies inputs from PG row columns.
// Both call decodeAuthorizeAndDispatch.
//
// Task 12 (holomush-jxo8.7.12) adds the dispatcher struct + WithSourceResolver
// option, replacing inline dekMgr.Resolve with resolver.Resolve for INV-CRYPTO-22
// fallback support.
package history

import (
	"context"
	"errors"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	"github.com/holomush/holomush/internal/eventbus/history/source"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// dispatcher wraps the per-call source resolution via a SourceResolver.
// Construct via newDispatcher; configure with WithSourceResolver.
//
// DispatchFor replaces the inline dekMgr.Resolve call in
// decodeAuthorizeAndDispatch with resolver.Resolve, enabling the INV-CRYPTO-22
// hot→cold-tier fallback (holomush-jxo8.7.12).
type dispatcher struct {
	resolver source.SourceResolver
}

// DispatcherOption configures a dispatcher.
type DispatcherOption func(*dispatcher)

// WithSourceResolver injects the SourceResolver used for DEK resolution and
// optional cold-tier fallback (INV-CRYPTO-22). Required for sensitive codec events.
func WithSourceResolver(r source.SourceResolver) DispatcherOption {
	return func(d *dispatcher) { d.resolver = r }
}

// newDispatcher constructs a dispatcher and applies opts.
func newDispatcher(opts ...DispatcherOption) *dispatcher {
	d := &dispatcher{}
	for _, o := range opts {
		o(d)
	}
	return d
}

// DispatchFor is the resolver-aware dispatcher for historical reads. It
// replaces the inline dekMgr.Resolve block in decodeAuthorizeAndDispatch
// with d.resolver.Resolve, enabling the INV-CRYPTO-22 hot→cold-tier fallback.
//
// Auth flow is identical to decodeAuthorizeAndDispatch: identity codec
// short-circuits; AuthGuard decision gates decryption; plugin decrypts
// produce an audit record.
//
// INV-E20: AAD is constructed from resolved.Envelope's fields, not the
// original envelope parameter. For TierColdFallback, resolved.Envelope
// carries the cold-tier substitute proto bytes; its keyID/keyVersion supply
// the DEK reference that was used to re-encrypt during Rekey.
//
// INV-E21: when resolver returns ErrMetadataOnly (double miss), the event is
// delivered with MetadataOnly=true and empty payload.
func (d *dispatcher) DispatchFor(
	ctx context.Context,
	envelope *eventbusv1.Event,
	codecName codec.Name,
	keyID codec.KeyID,
	keyVersion uint32,
	identity eventbus.SessionIdentity,
	guard eventbus.SessionAuthGuard,
	auditEm eventbus.SessionAuditEmitter,
) (eventbus.Event, bool, error) {
	var eventID ulid.ULID
	if rawID := envelope.GetId(); len(rawID) == 16 {
		copy(eventID[:], rawID)
	}

	// Identity codec: passthrough. AuthGuard NOT invoked.
	if codecName == codec.NameIdentity {
		return buildHistoryEventFromEnvelope(eventID, envelope, envelope.GetPayload()), false, nil
	}

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
		ev := buildHistoryEventFromEnvelope(eventID, envelope, nil)
		ev.NoPlaintextReason = eventbus.NoPlaintextReasonAuthGuardDeny
		return ev, true, nil
	}

	// Permit: resolve via SourceResolver.
	if d.resolver == nil {
		return eventbus.Event{}, false, oops.Code("EVENTBUS_HISTORY_RESOLVER_NIL").
			Errorf("AuthGuard permitted decrypt but SourceResolver is nil — misconfiguration")
	}

	// Build the hot envelope to pass to the resolver. Payload carries the
	// ciphertext bytes from the hot proto; subject/type/timestamp are not
	// needed by SimpleResolver or FallbackResolver (only keyID/keyVersion are
	// consulted for DEK lookup, and eventID for cold-tier lookup-by-id).
	hotEnv := eventbus.NewEnvelopeFromFields(eventbus.EnvelopeFields{
		EventID:    eventID,
		Codec:      codecName,
		KeyID:      keyID,
		KeyVersion: keyVersion,
		Payload:    envelope.GetPayload(),
	})

	resolved, err := d.resolver.Resolve(ctx, hotEnv)
	if errors.Is(err, source.ErrMetadataOnly) {
		// INV-E21: double miss — deliver metadata-only, no error.
		ev := buildHistoryEventFromEnvelope(eventID, envelope, nil)
		ev.NoPlaintextReason = eventbus.NoPlaintextReasonStaleDEK
		return ev, true, nil
	}
	if err != nil {
		return eventbus.Event{}, false, oops.Code("EVENTBUS_SOURCE_RESOLVE_FAILED").
			With("event_id", eventID.String()).
			Wrap(err)
	}

	// INV-E20: AAD and ciphertext come from resolved.Envelope, not the
	// original envelope. For TierColdFallback, resolved.Envelope carries
	// the cold-tier proto bytes (marshaled eventbusv1.Event); we unmarshal
	// them to obtain the cold envelope's Subject/Type/Actor/Timestamp
	// needed by aad.Build. For TierHot, the original proto is used directly.
	var activeProto *eventbusv1.Event
	var ciphertext []byte
	switch resolved.SourceTier {
	case source.TierColdFallback:
		// Cold-tier payload bytes are the full marshaled eventbusv1.Event proto.
		var coldProto eventbusv1.Event
		if unmarshalErr := proto.Unmarshal(resolved.Envelope.Payload(), &coldProto); unmarshalErr != nil {
			return eventbus.Event{}, false, oops.Code("EVENTBUS_SOURCE_COLD_UNMARSHAL_FAILED").
				With("event_id", eventID.String()).
				Wrap(unmarshalErr)
		}
		activeProto = &coldProto
		ciphertext = coldProto.GetPayload()
	default:
		// TierHot: use the original proto envelope for metadata; ciphertext is
		// the hot payload (same as resolved.Envelope.Payload() for hot tier).
		activeProto = envelope
		ciphertext = resolved.Envelope.Payload()
	}

	// INV-E20: AAD is built from activeProto (resolved envelope's fields).
	aadBytes, err := aad.Build(activeProto, string(codecName), uint64(resolved.KeyID), resolved.KeyVersion)
	if err != nil {
		return eventbus.Event{}, false, oops.Code("EVENTBUS_AAD_BUILD_FAILED").Wrap(err)
	}

	c, err := codec.Resolve(codecName)
	if err != nil {
		return eventbus.Event{}, false, oops.Code("EVENTBUS_HISTORY_UNKNOWN_CODEC").
			With("codec", string(codecName)).Wrap(err)
	}

	plaintext, err := c.Decode(ctx, ciphertext, resolved.Key, aadBytes)
	if err != nil {
		return eventbus.Event{}, false, oops.Code("EVENTBUS_CODEC_DECODE_FAILED").
			With("codec", string(codecName)).Wrap(err)
	}

	// Plugin recipient: INV-CRYPTO-11 — every plugin decrypt MUST produce an audit
	// record. Fail closed if the emitter is absent or fails unexpectedly.
	if identity.Kind == eventbus.IdentityKindPlugin {
		if auditEm == nil {
			return eventbus.Event{}, false, oops.Code("EVENTBUS_HISTORY_AUDIT_EMITTER_NIL").
				Errorf("AuthGuard permitted plugin decrypt but no DecryptAuditEmitter configured (INV-CRYPTO-11)")
		}
		rec := eventbus.PluginDecryptRecord{
			PluginName:       identity.PluginName,
			PluginInstanceID: identity.InstanceID,
			EventID:          eventID,
			EventSubject:     eventbus.Subject(activeProto.GetSubject()),
			EventType:        eventbus.Type(activeProto.GetType()),
			DEKRef:           resolved.KeyID,
			DEKVersion:       resolved.KeyVersion,
			GrantID:          decision.GrantID,
		}
		if emitErr := auditEm.EmitPluginDecrypt(ctx, rec); emitErr != nil {
			if isHistoryAuditQueueFull(emitErr) {
				for i := range plaintext {
					plaintext[i] = 0
				}
				// Use activeProto (resolved hot/cold source), not envelope
				// (hot proto), so the metadata-only event returned on
				// TierColdFallback carries the cold record's subject/type
				// rather than potentially-stale hot envelope metadata.
				// Consistent with line 228 success path and lines 207-208
				// audit-record fields.
				ev := buildHistoryEventFromEnvelope(eventID, activeProto, nil)
				ev.NoPlaintextReason = eventbus.NoPlaintextReasonAuditQueueFull
				return ev, true, nil
			}
			return eventbus.Event{}, false, oops.Code("EVENTBUS_HISTORY_AUDIT_EMIT_FAILED").
				With("emit_error", emitErr.Error()).
				Errorf("plugin decrypt audit emit failed — cannot confirm audit landed (INV-CRYPTO-11)")
		}
	}

	return buildHistoryEventFromEnvelope(eventID, activeProto, plaintext), false, nil
}

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
//
// readBack selects the read-back authorization path on the AuthGuard
// (manifest crypto.emits[].readback) over the live-delivery path. It is
// only meaningful for IdentityKindPlugin; the live hot/cold tier callers
// pass false. The read-back primitive (decryptPluginRow, INV-CRYPTO-26) passes
// true for plugin principals.
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
	readBack bool,
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
		ReadBack:   readBack,
	}

	decision, err := guard.Check(ctx, req)
	if err != nil {
		return eventbus.Event{}, false, oops.Code("EVENTBUS_AUTHGUARD_CHECK_FAILED").
			With("event_type", envelope.GetType()).
			Wrap(err)
	}

	if !decision.Permit {
		ev := buildHistoryEventFromEnvelope(eventID, envelope, nil)
		ev.NoPlaintextReason = eventbus.NoPlaintextReasonAuthGuardDeny
		return ev, true, nil
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

	// Plugin recipient: INV-CRYPTO-11 — every plugin decrypt MUST produce an audit
	// record. Fail closed if the emitter is absent or fails unexpectedly.
	if identity.Kind == eventbus.IdentityKindPlugin {
		if auditEm == nil {
			// AuthGuard permitted the read but no emitter is wired — configuration
			// error. Fail closed rather than deliver plaintext without audit.
			return eventbus.Event{}, false, oops.Code("EVENTBUS_HISTORY_AUDIT_EMITTER_NIL").
				Errorf("AuthGuard permitted plugin decrypt but no DecryptAuditEmitter configured (INV-CRYPTO-11)")
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
				ev := buildHistoryEventFromEnvelope(eventID, envelope, nil)
				ev.NoPlaintextReason = eventbus.NoPlaintextReasonAuditQueueFull
				return ev, true, nil
			}
			return eventbus.Event{}, false, oops.Code("EVENTBUS_HISTORY_AUDIT_EMIT_FAILED").
				With("emit_error", emitErr.Error()).
				Errorf("plugin decrypt audit emit failed — cannot confirm audit landed (INV-CRYPTO-11)")
		}
	}

	return buildHistoryEventFromEnvelope(eventID, envelope, plaintext), false, nil
}
