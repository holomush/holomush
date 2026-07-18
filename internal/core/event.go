// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package core contains the core game engine types and logic.
package core

import (
	"github.com/oklog/ulid/v2"
)

// ActorKind identifies what type of entity caused an event.
type ActorKind uint8

// Actor kinds for event sources.
const (
	ActorCharacter ActorKind = iota
	ActorSystem
	ActorPlugin
)

func (a ActorKind) String() string {
	switch a {
	case ActorCharacter:
		return "character"
	case ActorSystem:
		return "system"
	case ActorPlugin:
		return "plugin"
	default:
		return "unknown"
	}
}

// SystemActorULID is the canonical identity for the host's "system" actor —
// the categorical bucket for events emitted by the host itself rather than
// by a character, player, or plugin. Defined as a fixed byte pattern (not
// entropy-generated) so audit rows and history queries reliably round-trip
// the same identity. The all-zero leading 15 bytes plus a low-numbered tag
// byte make sentinels visually distinguishable from real entropy ULIDs in
// logs.
var SystemActorULID = ulid.ULID{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x01}

// WorldServiceActorULID is the identity for events emitted by the world
// service subsystem (location/object/exit lifecycle).
var WorldServiceActorULID = ulid.ULID{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x02}

// ActorSystemID is the canonical Actor.ID value for the host's system
// actor. Pre-w9ml this was the literal "system"; post-w9ml it is the
// canonical ULID-string form of SystemActorULID. The 1 production + 4 test
// call sites compile unchanged — only the value flowing through them
// changes (string → ULID-string).
var ActorSystemID = SystemActorULID.String()

// SystemBroadcastSubject is the reserved stream (the `subject` argument to
// eventbus.NewEvent) for grid-wide system broadcasts — server announcements
// and the admin `wall` command. internal/sysbroadcast.Broadcaster qualifies
// it to events.<game_id>.system at the emit boundary. Both the command-layer
// broadcast path (command.Services.BroadcastSystemMessage / the shutdown
// command) and the plugin-host SessionAdmin broadcast backing
// (hostcap.NewSystemBroadcaster, decision holomush-t019a) MUST agree on this
// value so a plugin `wall` and a host announcement land on the same subject.
const SystemBroadcastSubject = "system"

// IsSentinelULID returns true iff id is a system actor sentinel ULID:
// first 15 bytes zero, last byte in [0x01, 0xFF]. Used by IdentityRegistry
// bootstrap (sentinel-collision detection on plugin row load) and by
// TestSentinelTagsUnique. Tag 0x00 is reserved as "no sentinel" — the
// all-zero ULID is the proto3 zero-value and would be wire-indistinguishable
// from absence-of-id.
//
// Tag-byte allocation policy: tags MUST be unique across the codebase and
// MUST be allocated via PR review of this file (single source of truth).
// Existing allocations: 0x01 = SystemActorULID, 0x02 = WorldServiceActorULID.
func IsSentinelULID(id ulid.ULID) bool {
	if id[15] == 0x00 {
		return false
	}
	for i := 0; i < 15; i++ {
		if id[i] != 0 {
			return false
		}
	}
	return true
}

// Actor represents who or what caused an event.
type Actor struct {
	Kind ActorKind
	// ID is the canonical ULID-string identity:
	//   Character: ULID from the user store (already in place).
	//   Plugin:    ULID from the plugin registry (resolved at stamp time
	//              via plugins.IdentityRegistry.IDByName, post-w9ml).
	//   System:    one of the sentinel ULID-strings above (SystemActorULID,
	//              WorldServiceActorULID, …) accessed via ActorSystemID or
	//              the typed sentinel constants.
	// core.Actor has no Unknown kind — empty ID is undefined behavior at
	// the core layer; the bus translation maps to ActorKindUnknown.
	ID string
}
