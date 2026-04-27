// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"context"
	"errors"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/eventbus/codec"
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
	// HeaderActorLegacyID carries a non-ULID actor identifier (e.g. a plugin
	// name) that comes from a legacy core.Actor. Stamped only when Actor.ID
	// is zero but Actor.LegacyID is set, so downstream decoders can restore
	// the original string identity without corrupting the ULID contract of
	// HeaderActorID.
	HeaderActorLegacyID = "App-Actor-Legacy-ID"
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

	// Proto-marshal the envelope. The payload inside is raw (plugin) bytes;
	// the codec below operates on the serialized envelope so subscribers can
	// decrypt the whole unit.
	envelope := &eventbusv1.Event{
		Id:        event.ID.Bytes(),
		Subject:   string(event.Subject),
		Type:      string(event.Type),
		Timestamp: timestamppb.New(event.Timestamp),
		Actor:     ActorToProto(event.Actor),
		Payload:   event.Payload,
	}
	plainBytes, err := proto.Marshal(envelope)
	if err != nil {
		return oops.Code("EVENTBUS_ENVELOPE_MARSHAL_FAILED").Wrap(err)
	}

	codecName, keyLabel, err := p.selector.SelectForEncrypt(ctx, string(event.Subject))
	if err != nil {
		return oops.Code("EVENTBUS_CODEC_SELECT_FAILED").Wrap(err)
	}
	c, err := codec.Resolve(codecName)
	if err != nil {
		return oops.Code("EVENTBUS_CODEC_UNKNOWN").With("codec", string(codecName)).Wrap(err)
	}
	var key codec.Key
	if codecName != codec.NameIdentity {
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
	encoded, err := c.Encode(ctx, plainBytes, key)
	if err != nil {
		return oops.Code("EVENTBUS_CODEC_ENCODE_FAILED").
			With("codec", string(codecName)).
			Wrap(err)
	}

	msg := &nats.Msg{
		Subject: string(event.Subject),
		Data:    encoded,
		Header:  nats.Header{},
	}
	msg.Header.Set(HeaderMsgID, event.ID.String())
	msg.Header.Set(HeaderSchemaVersion, SchemaVersion)
	msg.Header.Set(HeaderEventType, string(event.Type))
	msg.Header.Set(HeaderCodec, string(codecName))
	msg.Header.Set(HeaderActorKind, event.Actor.Kind.String())
	if event.Actor.ID != (ulid.ULID{}) {
		msg.Header.Set(HeaderActorID, event.Actor.ID.String())
	} else if event.Actor.LegacyID != "" {
		msg.Header.Set(HeaderActorLegacyID, event.Actor.LegacyID)
	}
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
	} else if a.LegacyID != "" {
		p.LegacyId = a.LegacyID
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
// form. Returns nil if input is nil. INV-GW-14 ensures parity.
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
		DisplayTarget:       EventChannel(p.GetDisplayTarget()),
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
