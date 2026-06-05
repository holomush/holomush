// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"fmt"
	"regexp"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/core"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// Subject is a typed JetStream subject. Constructed via NewSubject which
// validates against the documented token rules (see spec §1c).
type Subject string

// Type is a typed plugin-declared event type identifier. Constructed via
// NewType which validates against allowed character set.
type Type string

// NoPlaintextReason enumerates the causes for metadata_only=true on a
// delivered event so operators and clients can distinguish authorization
// denials, stale-DEK double-misses, and audit-queue backpressure.
// Mirrors corev1.NoPlaintextReason; kept in sync by INV-EVENTBUS-14 convention.
type NoPlaintextReason uint8

const (
	// NoPlaintextReasonUnspecified is the zero value; MUST hold when
	// MetadataOnly=false.
	NoPlaintextReasonUnspecified NoPlaintextReason = 0
	// NoPlaintextReasonAuthGuardDeny indicates the recipient was not in the
	// DEK's participant set or lacked the requisite plugin manifest
	// declaration / ABAC grant. Phase 3b AuthGuard deny.
	NoPlaintextReasonAuthGuardDeny NoPlaintextReason = 1
	// NoPlaintextReasonStaleDEK indicates both hot AND cold tier DEKs are
	// indecipherable. Production-real post sub-epic E rekey + DEK
	// destruction. INV-CRYPTO-108 double miss.
	NoPlaintextReasonStaleDEK NoPlaintextReason = 2
	// NoPlaintextReasonAuditQueueFull indicates plugin audit-emit
	// backpressure (queue full). Host-side TOCTOU defense.
	NoPlaintextReasonAuditQueueFull NoPlaintextReason = 3
	// NoPlaintextReasonDEKMissing indicates the cold-tier audit row has no
	// dek_ref (DEK reference column missing or NULL). Stamped exclusively by
	// F's operator-read classifier (INV-CRYPTO-66).
	NoPlaintextReasonDEKMissing NoPlaintextReason = 4
	// NoPlaintextReasonDEKBadColumns indicates the cold-tier audit row
	// references a DEK whose column set does not match the event's AAD
	// declaration. Stamped exclusively by F's operator-read classifier.
	NoPlaintextReasonDEKBadColumns NoPlaintextReason = 5
	// NoPlaintextReasonInternal is the catch-all for unexpected decrypt
	// failures not covered by the specific cases above. Stamped exclusively
	// by F's operator-read classifier.
	NoPlaintextReasonInternal NoPlaintextReason = 6
	// NoPlaintextReasonDowngradeRefused indicates the PluginDowngradeFence
	// refused the row at layer (1) pre-decrypt — either the manifest-set
	// heuristic (INV-CRYPTO-42: identity codec for an always-sensitive type) or
	// the DEK existence pre-check (INV-CRYPTO-50: unknown / absent dek_ref for
	// a non-identity codec). The original event_id is preserved; payload
	// is empty per master INV-CRYPTO-15.
	NoPlaintextReasonDowngradeRefused NoPlaintextReason = 7
)

// Direction selects the iteration order of HistoryStream.
type Direction uint8

const (
	// DirectionForward iterates events from oldest to newest.
	DirectionForward Direction = 1
	// DirectionBackward iterates events from newest to oldest.
	DirectionBackward Direction = 2
)

// ActorKind identifies what type of entity caused an event. Mirrors the
// existing core.ActorKind so the cutover preserves semantics.
type ActorKind uint8

const (
	// ActorKindUnknown is the zero value; used when the actor cannot be determined.
	ActorKindUnknown ActorKind = 0
	// ActorKindCharacter indicates the event was caused by a character.
	ActorKindCharacter ActorKind = 1
	// ActorKindPlayer indicates the event was caused by a player.
	ActorKindPlayer ActorKind = 2
	// ActorKindSystem indicates the event was caused by internal system logic.
	ActorKindSystem ActorKind = 3
	// ActorKindPlugin indicates the event was caused by a plugin.
	ActorKindPlugin ActorKind = 4
)

// Actor identifies who caused an event. Host-stamped, never plugin-spoofable.
// ID MUST be a real ULID for every ActorKind. Sentinel ULIDs are reserved
// for system actors (SystemActorULID, WorldServiceActorULID); plugin
// actors carry registry-backed ULIDs resolved at stamp time via
// IdentityRegistry.IDByName.
type Actor struct {
	Kind ActorKind
	ID   ulid.ULID
}

// EventChannel mirrors corev1.EventChannel for ergonomic host-side use
// (avoids forcing test fixtures and emit-site struct literals to import
// the proto package). Kept in lockstep with the proto enum by INV-EVENTBUS-14.
type EventChannel uint8

const (
	// EventChannelUnspecified is the zero value; must not be used in production registrations.
	EventChannelUnspecified EventChannel = 0
	// EventChannelTerminal routes events to the terminal/telnet scroll-back only.
	EventChannelTerminal EventChannel = 1
	// EventChannelState routes events to the state sidebar only.
	EventChannelState EventChannel = 2
	// EventChannelBoth routes events to both terminal and state sidebar.
	EventChannelBoth EventChannel = 3
	// EventChannelAuditOnly routes events to events_audit only — the gRPC
	// Subscribe handler drops these before send, and they are NEVER
	// delivered to telnet or web clients. Used by host-emit security
	// audit events (crypto.totp_*, crypto.policy_set).
	EventChannelAuditOnly EventChannel = 4
)

// RenderingMetadata is the host-side representation of corev1.RenderingMetadata.
// Populated by RenderingPublisher.Publish before marshaling to the wire.
type RenderingMetadata struct {
	Category            string
	Format              string
	Label               string
	DisplayTarget       EventChannel
	SourcePlugin        string
	SourcePluginVersion string
}

// Event is the host-side representation of a published event.
//
// Wire format (JetStream): proto-encoded Event in msg.Data, with headers
// `Nats-Msg-Id`, `App-Schema-Version`, `App-Event-Type`, `App-Codec`.
// See spec §1d.
type Event struct {
	ID        ulid.ULID
	Seq       uint64 // JetStream stream sequence; populated by both tier readers and by the subscriber. Host-internal — never serialized in any public proto envelope.
	Subject   Subject
	Type      Type
	Timestamp time.Time
	Actor     Actor
	Payload   []byte // codec.Encode output (ciphertext if encryption is on)

	// Sensitive is a host-internal flag set by the emitter when manifest
	// sensitivity + plugin claim resolve to an encrypted publish. The
	// publisher reads this to choose between the existing identity path
	// and the Phase 3a sensitivity-aware crypto path. NEVER serialized
	// to the wire; never persisted; cold-tier reads return Sensitive=false
	// (the row's codec column is the source of truth on read).
	Sensitive bool

	// Rendering is populated by RenderingPublisher.Publish before
	// marshaling. Callers MUST NOT populate this field directly; the
	// field is reserved for the publisher chain.
	Rendering *RenderingMetadata
	// Headers carries pre-publish NATS headers stamped by the publisher
	// chain (e.g. App-Rendering by RenderingPublisher). JetStreamPublisher
	// merges these into the outgoing nats.Msg headers alongside the
	// system-stamped ones. Callers other than the publisher chain MUST
	// NOT populate this field directly.
	//
	// Reserved-keys rule: caller-written keys MUST start with "App-" and
	// MUST NOT be in the system-reserved set (Nats-Msg-Id, App-Codec,
	// App-Schema-Version, App-Event-Type, App-Actor-Kind, App-Actor-ID,
	// traceparent, tracestate). Keys starting with "Nats-" are reserved
	// unconditionally. Violation panics under testing.Testing(); in
	// production logs a warning and the system value wins.
	//
	// Cold-tier reads: this field is publish-path only. The cold-tier
	// history reader leaves Headers nil. Subscribers MUST NOT depend
	// on Headers being populated at read time; they read Event.Rendering.
	Headers map[string]string

	// MetadataOnly is populated by the hot-tier history reader's
	// decodeAndAuthorizeHistory when AuthGuard denies decryption for this
	// event and this identity. When true, Payload is nil. False for
	// identity-codec events and for events where AuthGuard permitted
	// delivery. The gRPC QueryStreamHistory handler stamps
	// EventFrame.metadata_only from this field (Phase 3b T10).
	// NEVER serialized to the wire event envelope; never persisted.
	MetadataOnly bool

	// NoPlaintextReason classifies why MetadataOnly=true was stamped.
	// Unspecified when MetadataOnly=false. Stamped at the same sites that
	// set MetadataOnly=true (holomush-ojw1.6). Mirrored to the wire via
	// EventFrame.no_plaintext_reason.
	NoPlaintextReason NoPlaintextReason

	// auditRow is the unexported plugin-source-of-truth pointer.
	// Populated by audit.PluginHistoryRouter when converting a plugin's
	// QueryHistoryResponse → Event for the read path. Consumed by
	// history.PluginDowngradeFence (via the package-internal accessor
	// AuditRowOf, see audit_row_access.go) to apply INV-CRYPTO-42 / INV-CRYPTO-50
	// checks against the plugin-supplied original. nil for events not
	// sourced from a plugin (host-owned subjects). Never serialized;
	// never persisted.
	auditRow *pluginauditpb.AuditRow
}

// NewEvent constructs an Event with a monotonic ULID (from core.NewULID()),
// the current timestamp, and the provided fields. This is the canonical
// construction path for eventbus.Event values that will be published — it
// mirrors the core.NewEvent() convention and prevents accidental omission of
// the ID stamp (holomush-jxo8.7.53).
//
// Callers that need to override specific fields after construction (e.g.
// Sensitive, Headers, Rendering) MUST still use NewEvent for the base value
// rather than a raw Event{} literal.
func NewEvent(subject Subject, typ Type, actor Actor, payload []byte) Event {
	return Event{
		ID:        core.NewULID(),
		Subject:   subject,
		Type:      typ,
		Timestamp: time.Now(),
		Actor:     actor,
		Payload:   payload,
	}
}

// subjectTokenRe permits NATS subject tokens: letters, digits, dashes,
// underscores. Wildcards (* and >) are positional and validated by NewSubject
// directly.
var subjectTokenRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// typeRe permits two mutually exclusive forms. Mixing separators in one
// string is rejected. Hyphens are allowed in each segment so plugin names
// that use them (e.g. "core-communication") round-trip unchanged.
//
//   - Dot-only: "say", "scene.pose", "scene.lifecycle.created"
//   - Colon (plugin:verb): "core-communication:say"  (exactly one colon, per spec §7.1)
var typeRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*(\.[a-z][a-z0-9_-]*)*$|^[a-z][a-z0-9_-]*:[a-z][a-z0-9_-]*$`)

// NewSubject validates and constructs a Subject. Returns ErrInvalidSubject
// on failure.
//
// Rules (per spec §1c):
//   - dot-delimited tokens
//   - * matches one token (positional)
//   - > matches the remainder and MUST be the last token
//   - depth SHOULD be ≤ 16
//   - non-wildcard tokens match [A-Za-z0-9_-]+
//   - leading "events." prefix is required (host enforces by convention)
func NewSubject(s string) (Subject, error) {
	if s == "" {
		return "", fmt.Errorf("%w: empty subject", ErrInvalidSubject)
	}
	tokens := splitDots(s)
	if len(tokens) > 16 {
		return "", fmt.Errorf("%w: token depth %d exceeds 16", ErrInvalidSubject, len(tokens))
	}
	if tokens[0] != "events" {
		return "", fmt.Errorf("%w: must start with 'events.'", ErrInvalidSubject)
	}
	for i, tok := range tokens {
		if tok == "" {
			return "", fmt.Errorf("%w: empty token at position %d", ErrInvalidSubject, i)
		}
		if tok == ">" {
			if i != len(tokens)-1 {
				return "", fmt.Errorf("%w: '>' must be the last token", ErrInvalidSubject)
			}
			continue
		}
		if tok == "*" {
			continue
		}
		if !subjectTokenRe.MatchString(tok) {
			return "", fmt.Errorf("%w: token %q has invalid characters", ErrInvalidSubject, tok)
		}
	}
	return Subject(s), nil
}

// MustSubject panics on validation failure. Use only for compile-time
// constants in plugin code (e.g., var sceneICPattern = MustSubject("events.*.scene.*.ic")).
func MustSubject(s string) Subject {
	sub, err := NewSubject(s)
	if err != nil {
		panic(err)
	}
	return sub
}

// NewType validates and constructs a Type.
func NewType(s string) (Type, error) {
	if s == "" {
		return "", fmt.Errorf("%w: empty type", ErrInvalidType)
	}
	if !typeRe.MatchString(s) {
		return "", fmt.Errorf("%w: type %q must match [a-z][a-z0-9_-]*(\\.[a-z][a-z0-9_-]*)* (dot-segmented) or plugin:verb", ErrInvalidType, s)
	}
	return Type(s), nil
}

func splitDots(s string) []string {
	out := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
