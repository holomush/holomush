// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package sysbroadcast is the single construction site for the host's
// system-broadcast event. The {"message": ...} payload shape, the system
// actor stamp, and the reserved-subject qualification MUST exist nowhere
// else in the tree (D-02) — internal/plugin/hostcap and internal/command
// both delegate to Broadcaster rather than building the event themselves.
package sysbroadcast

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventvocab"
)

// Broadcaster publishes system-actor system events over an
// eventbus.Publisher. gameID supplies the game id used to qualify the
// domain-relative subject a caller passes to Broadcast — a Publisher alone
// cannot qualify a subject (FINDING-5), mirroring internal/presence.Emitter's
// shape.
type Broadcaster struct {
	pub    eventbus.Publisher
	gameID func() string
}

// NewBroadcaster constructs a Broadcaster over pub, qualifying subjects with
// the game id returned by gameID.
//
// Panics when pub or gameID is nil so a misconfiguration surfaces at
// construction time rather than deferring to the first Broadcast call,
// mirroring presence.NewEmitter's construction-time failure discipline.
// Detects both untyped nil and typed-nil interface values (e.g. a typed-nil
// concrete pointer) so callers truly fail fast at construction.
func NewBroadcaster(pub eventbus.Publisher, gameID func() string) *Broadcaster {
	if pub == nil || isNilPublisher(pub) {
		panic("sysbroadcast.NewBroadcaster: nil Publisher")
	}
	if gameID == nil {
		panic("sysbroadcast.NewBroadcaster: nil gameID")
	}
	return &Broadcaster{pub: pub, gameID: gameID}
}

// isNilPublisher detects typed-nil interface values whose underlying
// concrete kind is nilable (pointer, slice, map, chan, func, interface).
// Returns false for non-nilable kinds (struct, value-receiver fakes).
// Mirrors internal/presence/emitter.go::isNilPublisher and
// internal/cluster/registry.go::isNilConn (WR-02) so all three
// eventbus.Publisher-shaped nil-checking constructors introduced in this
// phase detect the same failure mode.
func isNilPublisher(pub eventbus.Publisher) bool {
	v := reflect.ValueOf(pub)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// Broadcast publishes a single system-actor system event carrying message to
// subject. subject is a domain-relative reference (e.g.
// core.SystemBroadcastSubject, or a caller-chosen stream) qualified with the
// game id before publish — a relative subject never reaches Publish.
//
// A publish failure surfaces oops code SYSTEM_BROADCAST_FAILED, the code
// hostcap/system_broadcaster.go used before this collapse, preserved
// verbatim so existing errutil.AssertErrorCode assertions keep passing.
func (b *Broadcaster) Broadcast(ctx context.Context, subject, message string) error {
	//nolint:errcheck // json.Marshal cannot fail for map[string]string
	payload, _ := json.Marshal(map[string]string{"message": message})

	gameID := b.gameID()
	if gameID == "" {
		gameID = "main"
	}

	sub, err := eventbus.Qualify(gameID, subject)
	if err != nil {
		return oops.With("subject", subject).Wrap(err)
	}

	typ, err := eventbus.NewType(string(eventvocab.EventTypeSystem))
	if err != nil {
		return oops.With("type", string(eventvocab.EventTypeSystem)).Wrap(err)
	}

	systemActor := eventbus.Actor{Kind: eventbus.ActorKindSystem, ID: core.SystemActorULID}
	ev := eventbus.NewEvent(sub, typ, systemActor, payload)

	if err := b.pub.Publish(ctx, ev); err != nil {
		return oops.Code("SYSTEM_BROADCAST_FAILED").Wrap(err)
	}
	return nil
}
