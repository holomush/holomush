// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import "fmt"

// CommandStatus indicates the outcome category of a command execution.
type CommandStatus int

const (
	// CommandOK indicates successful execution. Output is normal text.
	CommandOK CommandStatus = iota
	// CommandError indicates a user-facing error (bad input, not found, no permission).
	// This is expected behavior — does not count as service degradation.
	CommandError
	// CommandFailure indicates a service failure (DB down, proxy error).
	// Player sees a specific message. Handler should have logged the underlying error.
	CommandFailure
	// CommandFatal indicates an unrecoverable error. The dispatcher surfaces
	// a generic message. Handler returns this when it cannot proceed at all.
	CommandFatal
)

// CommandRequest carries context for a plugin command invocation.
type CommandRequest struct {
	Command       string // parsed command name: "say", "dig"
	Args          string // everything after the command name
	CharacterID   string // invoking character ULID
	CharacterName string // display name
	LocationID    string // character's current location ULID
	SessionID     string // active session ULID
	PlayerID      string // player account ULID
	InvokedAs     string // what the player actually typed (alias support)
}

// CommandResponse carries the result of a plugin command execution.
type CommandResponse struct {
	// Status indicates the outcome category (OK, Error, Failure, Fatal).
	Status CommandStatus

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

// OK returns a successful command response with output text.
func OK(output string) *CommandResponse {
	return &CommandResponse{Status: CommandOK, Output: output}
}

// Errorf returns a user-facing error response (bad input, not found, no permission).
func Errorf(format string, args ...any) *CommandResponse {
	return &CommandResponse{Status: CommandError, Output: fmt.Sprintf(format, args...)}
}

// Failuref returns a service failure response. The handler should log the
// underlying error via proxy.Log before calling this.
func Failuref(format string, args ...any) *CommandResponse {
	return &CommandResponse{Status: CommandFailure, Output: fmt.Sprintf(format, args...)}
}
