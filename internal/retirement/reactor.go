// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package retirement

import (
	"context"

	"github.com/nats-io/nats.go"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
	gamesession "github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
)

// JobName is the background-job identity the reactor's world writes are
// authorized under. It is the bare name access.JobSubject prefixes into
// "job:retirement", and the literal a seed's principal.job.name conjunct
// matches.
const JobName = "retirement"

// DefaultConsumerName is the durable JetStream consumer name. Durable ⇒ the
// reactor resumes from the last acked sequence across restarts, which is what
// makes a retirement emitted while the host was down still take effect.
const DefaultConsumerName = "character_retirement_reactor"

// ConsumerFilterSubject is the D-36 subscription: every character-aggregate
// event on every game's feed. The handler filters by event type.
const ConsumerFilterSubject = "events.*.character.>"

// eventTypeCharacterRetired mirrors the world service's envelope kind for
// RetireCharacter. It is respelled rather than imported because the world
// constant is unexported; the pairing is pinned by the full-stack proof in
// plan 03-06.
const eventTypeCharacterRetired = "character_retired"

// leaveReasonRetired is the reason string on both presence emissions. It joins
// the eight existing fanout vocabularies ("evicted", "quit", "booted", …).
const leaveReasonRetired = "retired"

// PresenceEmitter is the subset of *presence.Emitter the retirement fanout
// calls. Declared consumer-side (the internal/auth/auth_service.go:26-29
// shape) rather than importing internal/presence, which would pull
// internal/eventbus — whose transitive closure reaches back here.
type PresenceEmitter interface {
	EmitLeave(ctx context.Context, char core.CharacterRef, reason string) error
	EmitSessionEnded(ctx context.Context, char core.CharacterRef, sessionID, cause, reason string) error
}

// SessionEnder is the one session-store method the fanout needs.
//
// Its absence signal is load-bearing: DeleteByCharacter returns (nil, nil)
// when there was no active or detached session to delete, which is exactly the
// idempotency gate a redelivery needs (internal/store/session_store.go:813).
type SessionEnder interface {
	DeleteByCharacter(ctx context.Context, characterID ulid.ULID) (*gamesession.Info, error)
}

// WorldSurface is the world.Service subset the fanout uses. Both methods take
// a world.Caller and cross the ABAC chokepoint — the reactor has no
// unauthorized path to the world, by construction (D-47).
type WorldSurface interface {
	GetCharacter(ctx context.Context, caller world.Caller, id ulid.ULID) (*world.Character, error)
	MoveCharacter(ctx context.Context, caller world.Caller, characterID, toLocationID ulid.ULID) error
}

// JobRegistry is the narrow view of internal/jobs.Registry the reactor needs
// to declare its own liveness and capability class.
type JobRegistry interface {
	Register(name string, writes []string) error
	Unregister(name string)
}

// delivery is the reactor's decoded view of one delivered message. It
// separates the two derivations the authorization check depends on being
// INDEPENDENT (D-55):
//
//   - Aggregate is the bare ULID from the TRANSPORT subject; it becomes the
//     provenance trigger_subject a seed compares against resource.id.
//   - Character is the ULID the handler decoded from the message BODY; it
//     becomes the resource.
//
// A handler that derives the wrong aggregate is therefore DENIED rather than
// self-authorized.
type delivery struct {
	// EventID is the Nats-Msg-Id header — the event ULID, verbatim.
	EventID string
	// EventType is the App-Event-Type header, verbatim.
	EventType string
	// Aggregate is the bare aggregate ULID parsed from the NATS subject. It is
	// a STRING, not a ulid.ULID, because it is carried verbatim into the
	// provenance triple and byte-compared against bags.Resource["id"].
	Aggregate string
	// Character is the character the handler independently decoded from the
	// world envelope in the message body.
	Character ulid.ULID
}

// disposition is what the handler tells JetStream to do with a message.
type disposition int

const (
	// ack means the message is fully handled — or permanently unhandleable,
	// in which case redelivering it buys nothing but a stalled consumer slot.
	ack disposition = iota
	// retry leaves the message unacked so JetStream redelivers after AckWait.
	// Safe by construction: every effect is gated on observed state, so a
	// redelivery re-runs only what did not happen.
	retry
)

// reactor is the stateless handler behind the durable consumer.
type reactor struct {
	cfg Config
}

// newReactor validates that every effect surface the fanout needs is wired.
// A nil surface is a wiring bug at the composition root, not a runtime
// condition, so it is rejected at Prepare rather than nil-dereferenced on the
// first delivered message.
func newReactor(cfg Config) (*reactor, error) {
	switch {
	case cfg.Sessions == nil:
		return nil, oops.Code("RETIREMENT_REACTOR_UNWIRED").Errorf("session store is required")
	case cfg.Presence == nil:
		return nil, oops.Code("RETIREMENT_REACTOR_UNWIRED").Errorf("presence emitter is required")
	case cfg.World == nil:
		return nil, oops.Code("RETIREMENT_REACTOR_UNWIRED").Errorf("world surface is required")
	case cfg.StartLocationID == nil:
		return nil, oops.Code("RETIREMENT_REACTOR_UNWIRED").Errorf("starting-location resolver is required")
	}
	return &reactor{cfg: cfg}, nil
}

// newDelivery decodes the wire parts of one JetStream message.
func newDelivery(_ string, _ nats.Header, _ []byte) (delivery, error) {
	return delivery{}, oops.Errorf("not implemented")
}

// handleDecoded decodes then processes one message.
func (r *reactor) handleDecoded(_ context.Context, _ string, _ nats.Header, _ []byte) disposition {
	return retry
}

// process runs the guarded fanout for one delivery.
func (r *reactor) process(_ context.Context, _ delivery) disposition {
	return retry
}
