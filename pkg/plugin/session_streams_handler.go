// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import "context"

// SessionStreamsRequest carries the session context the host supplies when it
// asks a plugin which streams it wants subscribed for a session at
// establishment (connect/reconnect). It is the SDK mirror of the
// QuerySessionStreamsRequest proto (character/player/session ids mapped 1:1).
type SessionStreamsRequest struct {
	// CharacterID is the character entering the session.
	CharacterID string
	// PlayerID is the player owning the character.
	PlayerID string
	// SessionID is the active session identifier.
	SessionID string
}

// SessionStreamsHandler is implemented by binary plugins that contribute stream
// subscriptions at session establishment. A plugin that only handles events (or
// commands) need not implement it — the host treats its absence as "no streams
// to contribute" and never routes the RPC to plugin code.
//
// Returned stream names MUST be domain-RELATIVE references in a namespace the
// plugin owns (e.g. "channel.<id>"), never a pre-qualified "events." subject:
// the host qualifies them against its own game id and fences each contribution
// against the plugin's declared emit domains before merging
// (internal/plugin/manager.go::QuerySessionStreams).
//
// This is the binary-runtime half of a Lua-parity feature: Lua plugins already
// contribute via the on_session_subscribe hook.
type SessionStreamsHandler interface {
	// QuerySessionStreams returns the domain-relative stream names the plugin
	// wants subscribed for the session described by req. Returning an error
	// degrades gracefully host-side (the host logs and skips the plugin's
	// contribution); returning an empty slice contributes nothing.
	QuerySessionStreams(ctx context.Context, req SessionStreamsRequest) ([]string, error)
}
