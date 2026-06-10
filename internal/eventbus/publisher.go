// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/telemetry"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// Header keys stamped on every published message. Per spec §1d these are
// required and never empty (App-Codec has an explicit "yes — never empty"
// mandate because consumers decode-by-header and silent empties corrupt
// downstream audit).
const (
	// HeaderMsgID is the per-event ULID that drives JetStream dedup.
	HeaderMsgID = "Nats-Msg-Id"
	// HeaderSchemaVersion is the major version of the proto envelope.
	HeaderSchemaVersion = "App-Schema-Version"
	// HeaderEventType is the plugin-declared event type — used to filter
	// without decoding the payload.
	HeaderEventType = "App-Event-Type"
	// HeaderCodec names the codec that produced payload bytes.
	HeaderCodec = "App-Codec"
	// HeaderActorKind is the ActorKind the host stamped on the event.
	HeaderActorKind = "App-Actor-Kind"
	// HeaderActorID is the (optional) actor id, set only when non-zero.
	HeaderActorID = "App-Actor-ID"
	// HeaderDekRef carries the crypto_keys.id (decimal string) for events
	// encrypted with a non-identity codec. Empty for codec=identity. Maps
	// 1:1 to events_audit.dek_ref (BIGINT) via the audit projection.
	HeaderDekRef = "App-Dek-Ref"
	// HeaderDekVersion carries the per-context DEK version (decimal string).
	// Empty for codec=identity. Maps to events_audit.dek_version (INTEGER).
	HeaderDekVersion = "App-Dek-Version"
)

// SchemaVersion is the proto envelope major version advertised in the
// App-Schema-Version header. Incrementing this signals a breaking change
// to the envelope message; subscribers pin to a major version.
const SchemaVersion = "1"

// defaultSafetyMargin is subtracted from Config.DupeWindow when deriving the
// publish deadline. Keeps the effective publish timeout strictly below the
// dedup horizon so a publish that returns DeadlineExceeded cannot race a
// dedup-window rollover on retry.
const defaultSafetyMargin = 30 * time.Second

// PublishOption tunes NewJetStreamPublisher construction.
type PublishOption func(*JetStreamPublisher)

// WithCodecSelector injects a KeySelector that maps subjects to codec names
// and key labels. When unset, NewJetStreamPublisher uses a static selector
// that always returns (codec.NameIdentity, "", nil).
func WithCodecSelector(sel codec.KeySelector) PublishOption {
	return func(p *JetStreamPublisher) { p.selector = sel }
}

// WithKeyProvider injects the KeyProvider used by non-identity codecs.
// Identity codec ignores this; all Phase A deployments use identity.
func WithKeyProvider(kp codec.KeyProvider) PublishOption {
	return func(p *JetStreamPublisher) { p.keys = kp }
}

// WithSafetyMargin overrides the default 30s safety margin used to derive
// the publish deadline from Config.DupeWindow.
func WithSafetyMargin(d time.Duration) PublishOption {
	return func(p *JetStreamPublisher) {
		if d > 0 {
			p.safetyMargin = d
		}
	}
}

// DEKManager is the publisher-facing subset of dek.Manager — Phase 3a uses
// GetOrCreate on the emit path; decrypt-on-fanout (Phase 3b) will use Resolve.
type DEKManager interface {
	GetOrCreate(ctx context.Context, ctxID dek.ContextID, initial []dek.Participant) (codec.Key, error)
}

// WithDEKManager wires a DEK manager. When non-nil, sensitive events
// (event.Sensitive=true) take the crypto branch in Publish; nil keeps
// behavior identical to pre-Phase-3a builds.
func WithDEKManager(m DEKManager) PublishOption {
	return func(p *JetStreamPublisher) { p.dekMgr = m }
}

// JetStreamPublisher is the production Publisher implementation. It owns the
// invariants a plain JS publish does not:
//
//   - Required headers (Nats-Msg-Id, App-Schema-Version, App-Event-Type,
//     App-Codec, App-Actor-Kind, optional App-Actor-ID) are stamped on every
//     message. Missing or empty headers are a bug.
//   - OTEL trace context is injected via telemetry.InjectHeaders. No-op
//     when the caller's context has no active span.
//   - Publishes run with a deadline derived from Config.DupeWindow. If the
//     deadline expires Publish returns ErrPublishExpired so callers know to
//     mint a new ULID rather than retry.
//   - Codec Encode runs before publish; App-Codec records which codec was
//     used (the default IdentityCodec is a passthrough, but the header is
//     still explicit — never empty).
//
// The emitter in internal/plugin/event_emitter.go calls Publish; it does NOT
// construct nats.Msg values directly. That separation keeps header-stamping
// in one place and makes `*nats.Msg` a non-exported concern of this package.
type JetStreamPublisher struct {
	js           jetstream.JetStream
	cfg          Config
	selector     codec.KeySelector
	keys         codec.KeyProvider
	safetyMargin time.Duration

	// dekMgr provides DEKs for sensitive events (event.Sensitive=true).
	// nil → publisher rejects sensitive events with
	// EVENTBUS_SENSITIVE_EVENT_NO_DEK_MANAGER and takes the legacy
	// identity/selector path for non-sensitive events. Wired via
	// WithDEKManager; bootstrap supplies it when a KEK is configured
	// (RekeyManager present) — not gated on CryptoConfig.Enabled.
	dekMgr DEKManager
}

// NewJetStreamPublisher constructs a Publisher backed by the given JetStream
// context. cfg.DupeWindow drives the publish deadline; callers SHOULD pass
// the same Config they gave the Subsystem.
func NewJetStreamPublisher(js jetstream.JetStream, cfg Config, opts ...PublishOption) *JetStreamPublisher {
	p := &JetStreamPublisher{
		js:           js,
		cfg:          cfg.Defaults(),
		selector:     identityKeySelector{},
		safetyMargin: defaultSafetyMargin,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Publish implements Publisher. See JetStreamPublisher for the invariants
// it enforces.
func (p *JetStreamPublisher) Publish(ctx context.Context, event Event) error {
	if p.js == nil {
		return oops.Code("EVENTBUS_PUBLISHER_NOT_READY").Errorf("JetStream context is nil")
	}
	// Reject the zero ULID before we stamp Nats-Msg-Id below; otherwise every
	// malformed event coalesces to the same dedupe key inside the dupe window
	// instead of failing fast.
	if event.ID == (ulid.ULID{}) {
		return oops.Code("EVENTBUS_EVENT_ID_REQUIRED").Errorf("event ID required")
	}
	if _, err := NewSubject(string(event.Subject)); err != nil {
		return err
	}
	if _, err := NewType(string(event.Type)); err != nil {
		return err
	}
	if len(event.Payload) > MaxPayloadSize {
		return oops.Code("EVENTBUS_PAYLOAD_TOO_LARGE").
			With("payload_size", len(event.Payload)).
			With("max_payload_size", MaxPayloadSize).
			Wrap(ErrPayloadTooLarge)
	}

	// Build the envelope with cleartext fields. event.Payload stays as the
	// raw (plugin) bytes for now; it is replaced below with ciphertext after
	// codec selection and key resolution.
	envelope := &eventbusv1.Event{
		Id:      event.ID.Bytes(),
		Subject: string(event.Subject),
		Type:    string(event.Type),
		// Full ns precision — BIGINT-ns column migration (gfo6) makes µs-truncation
		// unnecessary; structural AAD byte-equality holds via INV-STORE-4 / INV-STORE-5
		// (supersedes former INV-CRYPTO-51 discipline-dependent guarantee).
		Timestamp: timestamppb.New(event.Timestamp),
		Actor:     ActorToProto(event.Actor),
		Payload:   event.Payload,
		Rendering: RenderingToProto(event.Rendering),
	}

	var (
		codecName   codec.Name
		keyLabel    codec.KeyLabel
		key         codec.Key
		keyResolved bool
		dekRef      string
		dekVer      string
	)
	switch {
	case event.Sensitive && p.dekMgr != nil:
		ctxID, ctxErr := contextIDFromSubject(event.Subject)
		if ctxErr != nil {
			return oops.Code("EVENTBUS_DEK_CONTEXT_ID_FAILED").
				With("subject", string(event.Subject)).
				Wrap(ctxErr)
		}
		k, dekErr := p.dekMgr.GetOrCreate(ctx, ctxID, initialParticipantsForContext(ctxID))
		if dekErr != nil {
			return oops.Code("EVENTBUS_DEK_GETORCREATE_FAILED").
				With("subject", string(event.Subject)).
				Wrap(dekErr)
		}
		codecName = codec.NameXChaCha20v1
		key = k
		keyResolved = true
		dekRef = strconv.FormatUint(uint64(k.ID), 10)
		dekVer = strconv.FormatUint(uint64(k.Version), 10)
	case event.Sensitive && p.dekMgr == nil:
		return oops.Code("EVENTBUS_SENSITIVE_EVENT_NO_DEK_MANAGER").
			With("subject", string(event.Subject)).
			Errorf("event.Sensitive=true but publisher has no DEK manager wired")
	default:
		cn, kl, selErr := p.selector.SelectForEncrypt(ctx, string(event.Subject))
		if selErr != nil {
			return oops.Code("EVENTBUS_CODEC_SELECT_FAILED").Wrap(selErr)
		}
		codecName = cn
		keyLabel = kl
	}

	c, err := codec.Resolve(codecName)
	if err != nil {
		return oops.Code("EVENTBUS_CODEC_UNKNOWN").With("codec", string(codecName)).Wrap(err)
	}

	// Resolve key for the legacy selector path. Sensitive events already
	// hold a DEK-manager-supplied key (keyResolved=true); the identity
	// codec needs no key.
	if !keyResolved && codecName != codec.NameIdentity {
		if p.keys == nil {
			return oops.Code("EVENTBUS_KEY_PROVIDER_MISSING").
				With("codec", string(codecName)).
				Errorf("non-identity codec requires a KeyProvider")
		}
		k, keyErr := p.keys.Active(ctx, keyLabel)
		if keyErr != nil {
			return oops.Code("EVENTBUS_KEY_FETCH_FAILED").
				With("codec", string(codecName)).
				With("key_label", string(keyLabel)).
				Wrap(keyErr)
		}
		key = k
	}

	// AAD binds (codec, key id/version, envelope identity) into the AEAD
	// authentication tag for sensitive events. Built BEFORE encrypt because
	// aad.Build reads only cleartext envelope fields (id, subject, type,
	// timestamp, actor) — it never reads event.Payload, so the raw payload
	// still being in envelope.Payload at this point is correct.
	var aadBytes []byte
	if event.Sensitive {
		ab, aErr := aad.Build(envelope, string(codecName), uint64(key.ID), key.Version)
		if aErr != nil {
			return oops.Code("EVENTBUS_AAD_BUILD_FAILED").Wrap(aErr)
		}
		aadBytes = ab
	}

	// DECISION 0: encrypt ONLY event.Payload (the bytes), not the marshaled
	// envelope. For identity codec this is a no-op (IdentityCodec.Encode
	// returns input unchanged). For sensitive codec, envelope.Payload is
	// replaced with ciphertext before marshaling.
	ciphertext, err := c.Encode(ctx, event.Payload, key, aadBytes)
	if err != nil {
		return oops.Code("EVENTBUS_CODEC_ENCODE_FAILED").
			With("codec", string(codecName)).
			Wrap(err)
	}
	envelope.Payload = ciphertext

	// Marshal the envelope with cleartext metadata fields and the
	// (possibly encrypted) payload field. These marshaled bytes go on
	// the wire as msg.Data so the proto structure is always visible.
	plainBytes, err := proto.Marshal(envelope)
	if err != nil {
		return oops.Code("EVENTBUS_ENVELOPE_MARSHAL_FAILED").Wrap(err)
	}

	msg := &nats.Msg{
		Subject: string(event.Subject),
		Data:    plainBytes,
		Header:  nats.Header{},
	}
	msg.Header.Set(HeaderMsgID, event.ID.String())
	msg.Header.Set(HeaderSchemaVersion, SchemaVersion)
	msg.Header.Set(HeaderEventType, string(event.Type))
	msg.Header.Set(HeaderCodec, string(codecName))
	if dekRef != "" {
		msg.Header.Set(HeaderDekRef, dekRef)
		msg.Header.Set(HeaderDekVersion, dekVer)
	}
	msg.Header.Set(HeaderActorKind, event.Actor.Kind.String())
	if event.Actor.ID != (ulid.ULID{}) {
		msg.Header.Set(HeaderActorID, event.Actor.ID.String())
	}
	mergeCallerHeaders(msg.Header, event)
	// OTEL trace context; no-op when the caller has no active span.
	telemetry.InjectHeaders(ctx, msg.Header)

	pubCtx, cancel := context.WithTimeout(ctx, p.dupeWindowDeadline())
	defer cancel()
	if _, err := p.js.PublishMsg(pubCtx, msg); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return oops.Code("EVENTBUS_PUBLISH_EXPIRED").
				With("dupe_window", p.cfg.DupeWindow.String()).
				Wrap(ErrPublishExpired)
		}
		return oops.Code("EVENTBUS_PUBLISH_FAILED").
			With("subject", string(event.Subject)).
			Wrap(err)
	}
	return nil
}

// reservedHeaderKeys — keys that event.Headers must never overwrite.
// Note: App-Rendering is NOT listed here because it is written by
// RenderingPublisher before delegating to JetStreamPublisher. Adding it
// would cause RenderingPublisher's own stamp to panic. The single-writer
// invariant for App-Rendering is enforced architecturally: only
// RenderingPublisher holds the proto serialization path.
var reservedHeaderKeys = map[string]struct{}{
	HeaderMsgID:         {},
	HeaderCodec:         {},
	HeaderSchemaVersion: {},
	HeaderEventType:     {},
	HeaderActorKind:     {},
	HeaderActorID:       {},
	HeaderDekRef:        {},
	HeaderDekVersion:    {},
	"traceparent":       {},
	"tracestate":        {},
}

// mergeCallerHeaders copies ev.Headers into msgHeader enforcing the
// reserved-key collision policy. Keys are canonicalized before the
// reserved-key check so casing variants (e.g. "app-event-type") cannot
// bypass the guard — nats.Header.Set canonicalizes via
// textproto.CanonicalMIMEHeaderKey, so without the lookup-time
// canonicalization a casing variant would write to the same canonical
// slot while passing the raw-key check.
func mergeCallerHeaders(msgHeader nats.Header, ev Event) {
	if len(ev.Headers) == 0 {
		return
	}
	for k, v := range ev.Headers {
		canon := textproto.CanonicalMIMEHeaderKey(k)
		if _, reserved := reservedHeaderKeys[canon]; reserved || strings.HasPrefix(canon, "Nats-") {
			if testing.Testing() {
				panic(fmt.Sprintf("eventbus: caller wrote reserved header key %q (canonical %q)", k, canon))
			}
			slog.Warn("eventbus: caller-written header collides with reserved key; system value wins",
				"header", k, "canonical", canon, "event_id", ev.ID.String())
			continue
		}
		msgHeader.Set(k, v)
	}
}

// dupeWindowDeadline returns the effective publish deadline:
// max(DupeWindow − safetyMargin, 1ms). The floor prevents a misconfigured
// DupeWindow from producing a non-positive timeout (which context.WithTimeout
// would treat as "already expired").
func (p *JetStreamPublisher) dupeWindowDeadline() time.Duration {
	d := p.cfg.DupeWindow - p.safetyMargin
	if d <= 0 {
		// Pathological config; surface via publish deadline rather than
		// silently clamping to DupeWindow (which would elide the safety
		// margin the invariant exists to preserve).
		return time.Millisecond
	}
	return d
}

// Publisher returns a Publisher backed by this Subsystem. Nil when the
// Subsystem has not started.
func (s *Subsystem) Publisher(opts ...PublishOption) Publisher {
	if s.js == nil {
		return nil
	}
	return NewJetStreamPublisher(s.js, s.cfg, opts...)
}

// String returns the lowercase name for an ActorKind. Mirrors core.ActorKind's
// String for log/header stability.
func (k ActorKind) String() string {
	switch k {
	case ActorKindCharacter:
		return "character"
	case ActorKindPlayer:
		return "player"
	case ActorKindSystem:
		return "system"
	case ActorKindPlugin:
		return "plugin"
	default:
		return "unknown"
	}
}

// ActorToProto converts an in-process Actor to the proto representation.
// Exported so audit-router and other cross-package callers can reuse the
// single source of truth for Actor mapping.
func ActorToProto(a Actor) *eventbusv1.Actor {
	p := &eventbusv1.Actor{Kind: ActorKindToProto(a.Kind)}
	if a.ID != (ulid.ULID{}) {
		p.Id = a.ID.Bytes()
	}
	return p
}

// ActorKindToProto maps the in-process ActorKind enum to the proto enum.
// ActorKindUnknown (zero) maps to ACTOR_KIND_UNSPECIFIED — there is no
// proto ACTOR_KIND_UNKNOWN.
func ActorKindToProto(k ActorKind) eventbusv1.ActorKind {
	switch k {
	case ActorKindCharacter:
		return eventbusv1.ActorKind_ACTOR_KIND_CHARACTER
	case ActorKindPlayer:
		return eventbusv1.ActorKind_ACTOR_KIND_PLAYER
	case ActorKindSystem:
		return eventbusv1.ActorKind_ACTOR_KIND_SYSTEM
	case ActorKindPlugin:
		return eventbusv1.ActorKind_ACTOR_KIND_PLUGIN
	default:
		return eventbusv1.ActorKind_ACTOR_KIND_UNSPECIFIED
	}
}

// RenderingToProto converts the host-side RenderingMetadata to its proto
// form. Returns nil if input is nil. INV-EVENTBUS-14 ensures parity.
func RenderingToProto(r *RenderingMetadata) *corev1.RenderingMetadata {
	if r == nil {
		return nil
	}
	return &corev1.RenderingMetadata{
		Category:            r.Category,
		Format:              r.Format,
		Label:               r.Label,
		DisplayTarget:       corev1.EventChannel(r.DisplayTarget),
		SourcePlugin:        r.SourcePlugin,
		SourcePluginVersion: r.SourcePluginVersion,
	}
}

// RenderingFromProto converts the proto form to the host-side struct.
// Returns nil if input is nil.
func RenderingFromProto(p *corev1.RenderingMetadata) *RenderingMetadata {
	if p == nil {
		return nil
	}
	return &RenderingMetadata{
		Category:            p.GetCategory(),
		Format:              p.GetFormat(),
		Label:               p.GetLabel(),
		DisplayTarget:       EventChannel(p.GetDisplayTarget()), //nolint:gosec // G115: EventChannel values are bounded 0-3 by proto enum; no overflow possible
		SourcePlugin:        p.GetSourcePlugin(),
		SourcePluginVersion: p.GetSourcePluginVersion(),
	}
}

// identityKeySelector is the zero-policy selector used when the caller does
// not inject one. Every subject resolves to the identity codec with no key
// label. Production deployments that want at-rest encryption override via
// WithCodecSelector.
type identityKeySelector struct{}

func (identityKeySelector) SelectForEncrypt(_ context.Context, _ string) (codec.Name, codec.KeyLabel, error) {
	return codec.NameIdentity, "", nil
}

func (identityKeySelector) SelectForDecrypt(_ context.Context, _ codec.Name, _ codec.KeyID) (codec.Key, error) {
	return codec.NoKey, nil
}

// initialParticipantsForContext returns the DEK participant set to seed at
// genesis for a context, derived from the subject. A character.<id> context is
// a personal stream whose owner (the recipient of a private message —
// page/whisper/pemit) must be able to decrypt, so seed that character. The
// BindingID and PlayerID are left empty; GetOrCreate resolves both from the
// recipient's active binding row via the BindingResolver on its create branch
// (manager.go). The PlayerID lets the AuthGuard player-history branch match
// after a later binding rotation — symmetric with the scene-focus seed
// (holomush-5rh.8.29.11). Scene and other contexts seed nothing here (scene
// readers are seeded at SetSceneFocus).
func initialParticipantsForContext(ctxID dek.ContextID) []dek.Participant {
	if ctxID.Type != "character" {
		return nil
	}
	return []dek.Participant{{
		CharacterID: ctxID.ID,
		JoinedAt:    time.Now().UTC(),
		AddedVia:    "publisher.genesis",
	}}
}

// contextIDFromSubject derives a dek.ContextID from a NATS-native subject
// like "events.<game>.<namespace>.<id>[.<facet>...]". All producers emit
// dot-style subjects directly (e.g. "events.main.scene.01ABC"); the host
// qualifies domain-relative references via eventbus.Qualify before they
// reach the publisher, so parts[2]=namespace, parts[3]=id is the canonical form.
func contextIDFromSubject(subject Subject) (dek.ContextID, error) {
	s := string(subject)
	if !strings.HasPrefix(s, "events.") {
		return dek.ContextID{}, oops.With("subject", s).
			New("subject is not in events.<game>.<namespace>.<id>... form")
	}
	parts := strings.SplitN(s, ".", 5)
	if len(parts) < 4 {
		return dek.ContextID{}, oops.With("subject", s).
			Errorf("subject must have at least events.<game>.<namespace>.<id>")
	}
	namespace := parts[2]
	id := parts[3]
	if namespace == "" {
		return dek.ContextID{}, oops.With("subject", s).
			New("subject namespace token must not be empty")
	}
	if id == "" {
		return dek.ContextID{}, oops.With("subject", s).
			New("subject context id token must not be empty")
	}
	return dek.ContextID{Type: namespace, ID: id}, nil
}
