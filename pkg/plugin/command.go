// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

// CommandRequest carries context for a plugin command invocation.
// Service access comes through the ServiceProxy, not this struct.
type CommandRequest struct {
	Command       string // parsed command name: "say", "dig"
	Args          string // everything after the command name
	CharacterID   string // invoking character ULID
	CharacterName string // display name
	LocationID    string // character's current location ULID
	SessionID     string // active session ULID
	InvokedAs     string // what the player actually typed (alias support)
}

// CommandResponse carries the result of a plugin command execution.
type CommandResponse struct {
	// Events to append to the event store.
	Events []EmitEvent

	// Output is synchronous text output to the invoking player.
	// The dispatcher emits this as a command_response event on the character stream.
	Output string

	// BootedSessions lists session IDs that were forcibly disconnected.
	// The dispatcher emits leave events and triggers session teardown for each.
	BootedSessions []string

	// EndSession signals that the invoking session should end.
	EndSession bool
}
