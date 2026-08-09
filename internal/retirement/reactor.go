// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package retirement

import (
	"context"
	"errors"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	gamesession "github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/outbox"
	"github.com/holomush/holomush/pkg/errutil"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
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

// jobWrites is the capability class the reactor self-attests at registration
// (D-50). It NARROWS: a seed must still grant the write, and both gates must
// pass, so the declaration alone authorizes nothing (D-51).
//
// SESSION TEARDOWN IS DELIBERATELY ABSENT, and its absence is honest rather
// than an oversight (D-53). The session store has no policy chokepoint — the
// reactor calls DeleteByCharacter directly, as all eight existing fanout sites
// do — so declaring a "session" write kind here would narrow nothing and would
// imply a policy-authorized teardown that no binding proves. The only gated
// effect this job has is the character write.
var jobWrites = []string{"character"}

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

// reactor is the handler behind the durable consumer. It holds no per-message
// state — the only field beyond configuration is the lifecycle context the
// Consume callback runs effects under.
type reactor struct {
	cfg Config
	// ctx is the Activate context, stored so handle (which JetStream invokes
	// on its own goroutine, with no context of its own) can propagate
	// cancellation and trace context into every effect. It MUST be assigned
	// before Consume registers the callback — JetStream may invoke handle the
	// moment Consume returns, and assigning after registration is a data race.
	ctx context.Context //nolint:containedctx // lifecycle ctx, not a request ctx — same shape as audit.projection.workerCtx
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
	// Defaults is idempotent, so re-applying it over a Subsystem config that
	// already ran it costs nothing and keeps a directly-constructed reactor
	// (tests, future callers) from carrying a nil logger into a hot path.
	return &reactor{cfg: cfg.Defaults()}, nil
}

// newDelivery decodes the wire parts of one JetStream message.
//
// The body is the outbox relay's shape and nothing else: a proto-marshaled
// eventbusv1.Event whose Payload is the JSON world envelope
// (internal/world/outbox/wire.go's EnvelopeToEvent). The canonical decoder
// outbox.UnmarshalEnvelope is reused rather than re-derived, so the aggregate
// this handler authorizes against is parsed by the same code that wrote it.
//
// A world envelope is never a sensitive-codec payload — the relay publishes
// already-committed facts with no crypto.emits declaration — so no decrypt
// path is needed here. If one ever were, the proto unmarshal below would still
// succeed and the ENVELOPE unmarshal would fail, which lands on the
// permanently-unhandleable ack path rather than on a silent mis-decode.
func newDelivery(subject string, hdr nats.Header, data []byte) (delivery, error) {
	var wire eventbusv1.Event
	if err := proto.Unmarshal(data, &wire); err != nil {
		return delivery{}, oops.Code("RETIREMENT_EVENT_UNMARSHAL_FAILED").
			With("subject", subject).Wrap(err)
	}
	env, err := outbox.UnmarshalEnvelope(wire.GetPayload())
	if err != nil {
		return delivery{}, oops.Code("RETIREMENT_ENVELOPE_UNMARSHAL_FAILED").
			With("subject", subject).Wrap(err)
	}
	return delivery{
		EventID:   hdr.Get(eventbus.HeaderMsgID),
		EventType: hdr.Get(eventbus.HeaderEventType),
		Aggregate: aggregateFromSubject(subject),
		Character: env.AggregateID,
	}, nil
}

// aggregateFromSubject returns the BARE aggregate ULID from a qualified NATS
// subject (events.<game>.character.<ulid>).
//
// Bare is load-bearing (D-54): a seed binds
// `action.job.trigger_subject == resource.id`, and bags.Resource["id"] is the
// substring after the first ':' of the resource ref. A dotted subject or a
// prefixed "character:<id>" ref compares unequal, and every job write then
// silently default-denies.
// It parses POSITIONALLY and refuses every other shape. The consumer filter
// ends in ">", which admits any depth, so a faceted subject
// (events.<game>.character.<ulid>.<facet>) would yield the FACET name under a
// last-token read. That value is byte-compared against bags.Resource["id"] by
// seed:job-retirement-instance-scoped, so it would not self-authorize — it
// would default-deny, and classifyWorldError acks a deny as terminal, dropping
// the fanout with no retry behind a misleading "policy denied" log. Returning
// "" for an unrecognized shape keeps the deny (which is correct) while making
// the cause legible, and never lets a facet name masquerade as an aggregate id.
//
// Today's world-envelope subjects are exactly four tokens
// (internal/world/outbox/wire.go:159-160), so the faceted case is latent.
func aggregateFromSubject(subject string) string {
	parts := strings.Split(subject, ".")
	if len(parts) != 4 { // events.<game>.character.<ulid>
		return ""
	}
	return parts[3]
}

// handle is the Consume callback. It owns the ack decision; process owns the
// effects.
func (r *reactor) handle(msg jetstream.Msg) {
	if r.handleDecoded(r.workerContext(), msg.Subject(), msg.Headers(), msg.Data()) == ack {
		_ = msg.Ack() //nolint:errcheck // an ack failure is absorbed by redelivery, which is idempotent here
	}
	// retry: deliberately no Nak. Leaving the message unacked lets AckWait
	// pace the redelivery, matching the audit projector — a Nak would produce
	// an instant-redeliver storm against whatever surface is already failing.
}

// handleDecoded gates on the HEADER, then decodes, then processes one message.
//
// THE HEADER GATE MUST COME FIRST. The consumer's filter is the whole character
// aggregate (events.*.character.>), and the world outbox relay is not the only
// publisher on it: internal/presence/session_ended.go:67 publishes session_ended
// on exactly character.<id>, and the reactor's own step-(3) EmitSessionEnded
// lands there too. Those bodies are not world envelopes, so decoding them first
// fails in outbox.UnmarshalEnvelope and misreports routine traffic — every
// logout, guest reap and eviction, plus at least one line per SUCCESSFUL
// retirement — as ERROR-level poison. The ack would still be correct, but the
// diagnostic signal the poison classification exists to carry would be buried
// under noise proportional to session churn, leaving a genuinely undecodable
// message indistinguishable from a routine one.
//
// A body this handler cannot decode IS permanently unhandleable: redelivering it
// forever would occupy a MaxAckPending slot until MaxDeliver, so past this gate
// a decode failure is logged and acked.
func (r *reactor) handleDecoded(ctx context.Context, subject string, hdr nats.Header, data []byte) disposition {
	if hdr.Get(eventbus.HeaderEventType) != eventTypeCharacterRetired {
		// Not ours. Advancing the cursor past it is the point of the fast path,
		// and it costs no unmarshal at all.
		return ack
	}
	d, err := newDelivery(subject, hdr, data)
	if err != nil {
		errutil.LogErrorContext(ctx, "retirement reactor could not decode a delivered event; acking as poison",
			err, "subject", subject)
		return ack
	}
	return r.process(ctx, d)
}

// process runs the guarded fanout for one delivery, in the D-37/D-38 order:
// status guard, session teardown, leave at the OLD location, session_ended,
// then the move.
//
// Every effect is gated on OBSERVED STATE rather than on a durable progress
// record, which is what makes a JetStream redelivery safe: whatever already
// happened is visible as absence (no session row to delete, the character
// already at the starting location) and is skipped.
//
// ACCEPTED CRASH WINDOW. A crash after the session row is deleted but before
// the two emissions loses those notifications permanently — redelivery sees
// (nil, nil) and skips them. That is accepted rather than fixed with a durable
// progress record: it matches the semantics of all eight existing synchronous
// fanout sites, and introducing an outbox for a presence notification would be
// a larger commitment than the loss warrants.
func (r *reactor) process(ctx context.Context, d delivery) disposition {
	if d.EventType != eventTypeCharacterRetired {
		// Defence in depth. handleDecoded already gated on the same header
		// before spending an unmarshal, so this arm is unreachable from the
		// Consume path; it stays because process is also called directly by
		// package tests, and a future second caller must not be able to skip
		// the type check.
		return ack
	}

	caller := world.JobCaller(JobName, world.Provenance{
		EventID:   d.EventID,
		EventType: d.EventType,
		Subject:   d.Aggregate,
	})
	logger := r.cfg.Logger.With(
		"character_id", d.Character.String(),
		"event_id", d.EventID,
		"trigger_subject", d.Aggregate,
	)

	// (1) Status guard. A character un-retired between emit and delivery MUST
	// NOT be evicted, so nothing at all happens before this read succeeds.
	char, err := r.cfg.World.GetCharacter(ctx, caller, d.Character)
	if err != nil {
		return r.classifyWorldError(ctx, err, "retirement reactor could not read the retiring character")
	}
	status, err := world.ParseStatus(string(char.Status))
	if err != nil {
		// INV-WORLD-5's denying default. An unrecognized status will not
		// become recognized on redelivery, so this acks rather than retries.
		errutil.LogErrorContext(ctx, "retirement reactor read an unrecognized lifecycle status; skipping the fanout",
			err, "character_id", d.Character.String())
		return ack
	}
	if status != world.StatusRetired {
		logger.InfoContext(ctx, "retirement reactor skipping a character that is no longer retired",
			"status", string(status))
		return ack
	}

	// (2) Session teardown. (nil, nil) is the absence signal a redelivery
	// relies on: there was no active or detached session to end.
	info, err := r.cfg.Sessions.DeleteByCharacter(ctx, d.Character)
	if err != nil {
		errutil.LogErrorContext(ctx, "retirement reactor could not end the retiree's session; redelivering",
			err, "character_id", d.Character.String())
		return retry
	}

	// (3) Notify the OLD location, before the move, so the leave names the
	// place the character left. Both emissions are best-effort: a fanout
	// failure is operational degradation, exactly as at the eight existing
	// synchronous sites.
	if info != nil {
		// The CHARACTER row is authoritative for where the character is. The
		// session row's LocationID is a copy written when the session was last
		// updated, so a move that landed after that would announce the leave at
		// the place the character had already left. char.LocationID was read
		// two steps ago from the row the move itself writes; the session row is
		// the fallback for the (legitimate) case of a character with no
		// location set.
		leaveLoc := info.LocationID
		if char.LocationID != nil {
			leaveLoc = *char.LocationID
		}
		ref := core.CharacterRef{ID: d.Character, Name: char.Name, LocationID: leaveLoc}
		if emitErr := r.cfg.Presence.EmitLeave(ctx, ref, leaveReasonRetired); emitErr != nil {
			errutil.LogErrorContext(ctx, "retirement reactor could not emit leave for a retired character",
				emitErr, "character_id", d.Character.String(), "location_id", leaveLoc.String())
		}
		if emitErr := r.cfg.Presence.EmitSessionEnded(
			ctx, ref, info.ID, core.SessionEndedCauseRetired, leaveReasonRetired,
		); emitErr != nil {
			errutil.LogErrorContext(ctx, "retirement reactor could not emit session_ended for a retired character",
				emitErr, "character_id", d.Character.String(), "session_id", info.ID)
		}
	}

	// (4) Move to the starting location, skipping when the character is
	// already there — MoveCharacter itself would succeed and emit a second
	// character_moved envelope, so the guard is what makes redelivery silent.
	startLoc := r.cfg.StartLocationID()
	if char.LocationID != nil && *char.LocationID == startLoc {
		logger.DebugContext(ctx, "retirement reactor skipping a move to the starting location the character already occupies")
		return ack
	}
	if err := r.cfg.World.MoveCharacter(ctx, caller, d.Character, startLoc); err != nil {
		return r.classifyWorldError(ctx, err, "retirement reactor could not move the retiree to the starting location")
	}

	logger.InfoContext(ctx, "retirement fanout complete", "start_location_id", startLoc.String())
	return ack
}

// classifyWorldError splits a world.Service failure into the two dispositions.
//
// A policy DENY is terminal: the seed will not change between now and AckWait,
// so redelivering buys nothing but a consumed MaxAckPending slot and a log
// line every five seconds. Everything else — evaluation failures, missing
// rows, persistence errors — is treated as transient and redelivered, which is
// safe precisely because every effect above is gated on observed state.
// A deny observed while the lifecycle context is ALREADY CANCELLED is the one
// exception: during shutdown the job's liveness is being retracted, so a deny
// may mean "the job no longer has attributes" rather than "the seed refuses
// this write". Acking that would permanently abandon a half-applied fanout, so
// it is redelivered instead — on the next boot the job is live again and the
// state guards make the re-run silent.
func (r *reactor) classifyWorldError(ctx context.Context, err error, msg string) disposition {
	if errors.Is(err, world.ErrPermissionDenied) && ctx.Err() == nil {
		errutil.LogErrorContext(ctx, msg+"; policy denied the job and redelivery cannot cure it", err)
		return ack
	}
	errutil.LogErrorContext(ctx, msg+"; redelivering", err)
	return retry
}

// workerContext returns the context the Consume callback runs effects under.
// JetStream invokes handle on its own goroutine with no context of its own, so
// the subsystem's Activate context is stored here at start.
func (r *reactor) workerContext() context.Context {
	if r.ctx == nil {
		return context.Background()
	}
	return r.ctx
}
