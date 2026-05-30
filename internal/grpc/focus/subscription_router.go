// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/session"
)

// ConnectionSender delivers per-Connection stream subscription updates.
// Decouples the RPC handler layer from the concrete SessionStreamRegistry
// type in internal/grpc (avoiding an import cycle). Mirrors StreamSender
// for the session-wide path.
type ConnectionSender interface {
	// SendToConnection delivers a stream add/remove to exactly the named
	// connection's control channel. Returns CONNECTION_NOT_REGISTERED if
	// the connection has no active Subscribe goroutine (best-effort;
	// the RPC handler logs and continues).
	SendToConnection(sessionID string, connectionID ulid.ULID, stream string, add bool) error
}

// ComputeFocusManagedStreams returns the focus-managed subset of a
// Connection's stream subscriptions (INV-P5-3 deterministic function
// of FocusKey + character_location + gameID). Always-on streams
// (events.<gid>.notification.<id>) are written once at connection
// creation and not touched by this router.
//
// Grid focus (FocusKey == nil) → location.<character_location_id>
// (dot-relative form; host qualifier adds events.<gameID>. prefix).
//
// Scene focus → events.<gameID>.scene.<sceneID>.{ic,ooc} (dot-style
// from Phase 4 T11).
func ComputeFocusManagedStreams(fk *session.FocusKey, characterLocationID ulid.ULID, gameID string) []string {
	if fk == nil {
		return []string{"location." + characterLocationID.String()}
	}
	if fk.Kind == session.FocusKindScene {
		sceneID := fk.TargetID.String()
		return []string{
			"events." + gameID + ".scene." + sceneID + ".ic",
			"events." + gameID + ".scene." + sceneID + ".ooc",
		}
	}
	// Future kinds (channel, etc.) fall through to grid until plumbed.
	return []string{"location." + characterLocationID.String()}
}

// StreamDeltas computes the (adds, removes) sets between two stream
// lists. Used by the RPC handler layer to derive SendToConnection calls
// on focus change.
func StreamDeltas(old, next []string) (adds, removes []string) {
	oldSet := make(map[string]struct{}, len(old))
	for _, s := range old {
		oldSet[s] = struct{}{}
	}
	nextSet := make(map[string]struct{}, len(next))
	for _, s := range next {
		nextSet[s] = struct{}{}
	}
	for s := range nextSet {
		if _, ok := oldSet[s]; !ok {
			adds = append(adds, s)
		}
	}
	for s := range oldSet {
		if _, ok := nextSet[s]; !ok {
			removes = append(removes, s)
		}
	}
	return adds, removes
}
