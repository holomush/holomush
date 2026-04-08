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

// AuditEffect is the effect a plugin handler decided for a given audit hint.
// Only "deny" and "allow" are valid — plugin denials never carry
// default_deny or system_bypass semantics.
type AuditEffect string

// AuditEffect constants for audit hint construction.
const (
	AuditEffectDeny  AuditEffect = "deny"
	AuditEffectAllow AuditEffect = "allow"
)

// AuditHint is a partial audit event the plugin handler accumulates during
// command processing. Hints are serialized into CommandResponse.audit_hints
// and harvested by the dispatcher after the handler returns.
//
// Host-stamped fields (subject, source, component, timestamp, duration) are
// filled in by the dispatcher — the plugin provides only decision-specific
// fields. Setting Subject, Source, or Component on this struct is a no-op;
// the dispatcher overwrites them.
type AuditHint struct {
	ID              string            // stable slug, e.g., "not_member"
	Name            string            // human label, e.g., "channels: not a member"
	Message         string            // per-firing description
	Effect          AuditEffect       // deny or allow
	ActionQualifier string            // appended to host base action, e.g., "speak"
	Resource        string            // <type>:<id>, e.g., "channel:01XYZ"
	Attributes      map[string]string // plugin-provided context (namespaced keys)
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

	// AuditHints are plugin-emitted audit entries accumulated during
	// command processing. The dispatcher harvests these after the handler
	// returns and routes them through the audit logger after stamping
	// host-controlled fields. Plugin authors SHOULD NOT construct hints
	// directly; use Audit(ctx).Deny / Allow instead.
	AuditHints []AuditHint
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
