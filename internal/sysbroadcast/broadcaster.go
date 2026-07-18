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

	"github.com/holomush/holomush/internal/eventbus"
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
// TODO(RED): does not yet enforce the nil-guard / construction-time failure
// discipline — this is the RED-phase stub, filled in by the GREEN commit.
func NewBroadcaster(pub eventbus.Publisher, gameID func() string) *Broadcaster {
	return &Broadcaster{pub: pub, gameID: gameID}
}

// Broadcast is the RED-phase stub — it does not yet marshal the payload,
// qualify the subject, or publish anything.
func (b *Broadcaster) Broadcast(_ context.Context, _, _ string) error {
	return nil
}
