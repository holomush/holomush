// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package presence owns the host's arrive/leave/session-ended emissions —
// the observable record of active sessions at a location (per
// .claude/rules/terminology.md's "presence" definition: "active sessions at
// a location"). Emitter is the sole type: it publishes these three event
// shapes through an injected eventbus.Publisher, replacing the former
// core.Engine + busEventAppender pair (D-03/D-04).
package presence

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventvocab"
)

// ArrivePayload is the JSON payload for arrive events.
type ArrivePayload struct {
	CharacterName string `json:"character_name"`
}

// LeavePayload is the JSON payload for leave events.
type LeavePayload struct {
	CharacterName string `json:"character_name"`
	Reason        string `json:"reason"`
}

// Emitter publishes arrive/leave/session_ended events through an
// eventbus.Publisher. gameID supplies the game id used to qualify the
// domain-relative subject references built internally (e.g.
// "location.<id>") — a Publisher alone cannot qualify a subject, since
// qualification needs the game id, which the one-method Publisher interface
// does not expose (FINDING-5).
type Emitter struct {
	pub    eventbus.Publisher
	gameID func() string
}

// NewEmitter constructs an Emitter over pub, qualifying subjects with the
// game id returned by gameID.
//
// Panics when pub or gameID is nil so a misconfiguration surfaces at
// construction time rather than deferring to the first Emit* call. Detects
// both untyped nil and typed-nil interface values (e.g. a typed-nil concrete
// pointer) so callers truly fail fast at construction.
func NewEmitter(pub eventbus.Publisher, gameID func() string) *Emitter {
	if pub == nil || isNilPublisher(pub) {
		panic("presence.NewEmitter: nil Publisher")
	}
	if gameID == nil {
		panic("presence.NewEmitter: nil gameID")
	}
	return &Emitter{pub: pub, gameID: gameID}
}

// isNilPublisher detects typed-nil interface values whose underlying
// concrete kind is nilable (pointer, slice, map, chan, func, interface).
// Returns false for non-nilable kinds (struct, value-receiver fakes).
func isNilPublisher(pub eventbus.Publisher) bool {
	v := reflect.ValueOf(pub)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// buildEvent resolves the game id (falling back to "main" when gameID()
// returns empty, the same fallback busEventAppender.Append had), qualifies
// relRef into a fully-qualified Subject via eventbus.Qualify, converts typ
// into an eventbus.Type via eventbus.NewType, and constructs the Event via
// the canonical eventbus.NewEvent constructor. Mirrors
// busEventAppender.Append's four steps (cmd/holomush/sub_grpc.go) so the
// Emitter reproduces exactly what the retired Engine+adapter pair produced.
func (e *Emitter) buildEvent(relRef string, typ eventvocab.EventType, actor core.Actor, payload []byte) (eventbus.Event, error) {
	var zero eventbus.Event

	gameID := e.gameID()
	if gameID == "" {
		gameID = "main"
	}

	sub, err := eventbus.Qualify(gameID, relRef)
	if err != nil {
		return zero, oops.With("stream", relRef).Wrap(err)
	}

	t, err := eventbus.NewType(string(typ))
	if err != nil {
		return zero, oops.With("type", string(typ)).Wrap(err)
	}

	return eventbus.NewEvent(sub, t, mapActor(actor), payload), nil
}

// mapActor bridges the legacy core.Actor (ID is a string, expected to be a
// ULID post-w9ml) to the JetStream-side Actor (ID is a ULID; zero for
// anonymous/system). Mirrors coreToBusActor (cmd/holomush/sub_grpc.go).
//
// Note: ULID parse failure for non-empty IDs is silently ignored at this
// boundary, matching coreToBusActor's documented behavior — post-w9ml every
// stamp site stamps a valid ULID, and a failure here indicates a contract
// violation upstream that the structured emit-side gate
// (coreActorToEventbusActor in internal/plugin/event_emitter.go) already
// surfaces with full context.
func mapActor(a core.Actor) eventbus.Actor {
	out := eventbus.Actor{Kind: mapActorKind(a.Kind)}
	if a.ID == "" {
		return out
	}
	if parsed, err := ulid.Parse(a.ID); err == nil {
		out.ID = parsed
	}
	return out
}

func mapActorKind(k core.ActorKind) eventbus.ActorKind {
	switch k {
	case core.ActorCharacter:
		return eventbus.ActorKindCharacter
	case core.ActorSystem:
		return eventbus.ActorKindSystem
	case core.ActorPlugin:
		return eventbus.ActorKindPlugin
	default:
		return eventbus.ActorKindUnknown
	}
}

// EmitArrive publishes an arrive event on the character's location stream.
func (e *Emitter) EmitArrive(ctx context.Context, char core.CharacterRef) error {
	payload, err := json.Marshal(ArrivePayload{CharacterName: char.Name})
	if err != nil {
		return oops.With("operation", "marshal_arrive_payload").Wrap(err)
	}

	ev, err := e.buildEvent(
		"location."+char.LocationID.String(),
		eventvocab.EventTypeArrive,
		core.Actor{Kind: core.ActorCharacter, ID: char.ID.String()},
		payload,
	)
	if err != nil {
		return err
	}

	if err := e.pub.Publish(ctx, ev); err != nil {
		return oops.With("operation", "publish_arrive_event").Wrap(err)
	}

	return nil
}

// EmitLeave publishes a leave event on the character's location stream.
func (e *Emitter) EmitLeave(ctx context.Context, char core.CharacterRef, reason string) error {
	payload, err := json.Marshal(LeavePayload{CharacterName: char.Name, Reason: reason})
	if err != nil {
		return oops.With("operation", "marshal_leave_payload").Wrap(err)
	}

	ev, err := e.buildEvent(
		"location."+char.LocationID.String(),
		eventvocab.EventTypeLeave,
		core.Actor{Kind: core.ActorCharacter, ID: char.ID.String()},
		payload,
	)
	if err != nil {
		return err
	}

	if err := e.pub.Publish(ctx, ev); err != nil {
		return oops.With("operation", "publish_leave_event").Wrap(err)
	}

	return nil
}
