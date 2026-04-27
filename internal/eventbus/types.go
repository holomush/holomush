// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"fmt"
	"regexp"
	"time"

	"github.com/oklog/ulid/v2"
)

// Subject is a typed JetStream subject. Constructed via NewSubject which
// validates against the documented token rules (see spec §1c).
type Subject string

// Type is a typed plugin-declared event type identifier. Constructed via
// NewType which validates against allowed character set.
type Type string

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
type Actor struct {
	Kind ActorKind
	ID   ulid.ULID // zero ULID for ActorKindSystem / Unknown
	// LegacyID carries a non-ULID identifier (e.g. a plugin name) bridged
	// from core.Actor.ID. Set only when ID is zero; propagated through
	// publisher/subscriber headers so plugin-authored host events keep
	// their actor identity across the JetStream boundary.
	LegacyID string
}

// EventChannel mirrors corev1.EventChannel for ergonomic host-side use
// (avoids forcing test fixtures and emit-site struct literals to import
// the proto package). Kept in lockstep with the proto enum by INV-GW-14.
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
	// App-Actor-Legacy-ID, traceparent, tracestate). Keys starting with
	// "Nats-" are reserved unconditionally. Violation panics under
	// testing.Testing(); in production logs a warning and the system
	// value wins.
	//
	// Cold-tier reads: this field is publish-path only. The cold-tier
	// history reader leaves Headers nil. Subscribers MUST NOT depend
	// on Headers being populated at read time; they read Event.Rendering.
	Headers map[string]string
}

// subjectTokenRe permits NATS subject tokens: letters, digits, dashes,
// underscores. Wildcards (* and >) are positional and validated by NewSubject
// directly.
var subjectTokenRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// typeRe permits dot-segmented identifiers like "scene.lifecycle.created".
var typeRe = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)

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
		return "", fmt.Errorf("%w: type %q does not match [a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*", ErrInvalidType, s)
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
