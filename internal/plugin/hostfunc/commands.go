// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"
	"log/slog"

	"github.com/oklog/ulid/v2"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/observability"
)

// CommandRegistry provides read-only access to registered commands.
type CommandRegistry interface {
	// All returns all registered commands.
	All() []command.CommandEntry
	// Get retrieves a command by name.
	Get(name string) (command.CommandEntry, bool)
}

// WithCommandRegistry sets the command registry for command-related host functions.
func WithCommandRegistry(reg CommandRegistry) Option {
	return func(f *Functions) {
		f.commandRegistry = reg
	}
}

// AccessPolicyEngine is a type alias re-exported for integration test convenience.
type AccessPolicyEngine = types.AccessPolicyEngine

// WithEngine sets the access policy engine for capability filtering.
func WithEngine(engine types.AccessPolicyEngine) Option {
	return func(f *Functions) {
		f.engine = engine
	}
}

// listCommandsFn returns the list_commands host function.
// Args: character_id (string) - the character whose capabilities determine visible commands
// Returns: (result table, error string or nil)
//
// Result table structure:
//   - commands: array of {name, help, usage, source} tables
//   - incomplete: bool — true if engine errors prevented some commands from being evaluated
//
// Contract for callers:
//   - When incomplete is false: the command list is authoritative
//   - When incomplete is true: some commands may be hidden due to access evaluation errors.
//     Callers SHOULD display the available commands AND indicate to the user that the list
//     may be incomplete (e.g., show the error string from the second return value).
//   - The error string (second return) is non-nil only when incomplete is true.
//
// Commands are filtered by capability:
//   - Commands with no capabilities (nil or empty slice) are always included
//   - Commands with capabilities require ALL capabilities to be granted (AND logic)
func (f *Functions) listCommandsFn(_ string) lua.LGFunction {
	return func(L *lua.LState) int {
		charIDStr := L.CheckString(1)
		if charIDStr == "" {
			L.RaiseError("character ID cannot be empty")
			return 0
		}

		charID, err := ulid.Parse(charIDStr)
		if err != nil {
			L.RaiseError("invalid character ID: %s", charIDStr)
			return 0
		}

		if f.commandRegistry == nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("command registry not available"))
			return 2
		}

		if f.engine == nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("access engine not available"))
			return 2
		}

		commands := f.commandRegistry.All()
		subject := access.CharacterSubject(charID.String())
		ctx := L.Context()
		if ctx == nil {
			// Lua VM has no context — fall back to background context.
			// This shouldn't happen when events are delivered via DeliverEvent,
			// which always sets a context. Log a warning for visibility.
			slog.Warn("lua VM context is nil in list_commands, using background context")
			ctx = context.Background()
		}

		// Filter commands by character capabilities.
		// Circuit breaker: stop querying the engine after repeated failures to avoid
		// O(n_commands * n_capabilities) calls against a degraded engine.
		// Each command contributes at most 1 to the count regardless of how many capabilities it has.
		// Note: intentionally independent from the identical constant in handlers/who.go —
		// the two circuit breakers protect different code paths and may diverge.
		//
		// Invariant: commands with no capabilities are ALWAYS included, even when the
		// circuit breaker has tripped. The circuit breaker only suppresses engine calls;
		// no-capability commands do not require an engine call, so they are unaffected.
		const maxEngineErrors = 3
		var filtered []command.CommandEntry
		var hadEngineError bool
		var engineErrorCount int
		circuitTripped := false
		for _, cmd := range commands {
			// No-capability commands are always visible — skip engine entirely.
			if len(cmd.GetCapabilities()) == 0 {
				filtered = append(filtered, cmd)
				continue
			}

			// Circuit breaker has tripped: skip capability-gated commands rather than
			// querying a degraded engine. The list will be marked incomplete.
			if circuitTripped {
				continue
			}

			allowed, hadError := f.canExecuteCommand(ctx, subject, cmd)
			if hadError {
				hadEngineError = true
				engineErrorCount++
				if engineErrorCount >= maxEngineErrors {
					slog.WarnContext(ctx, "command list circuit breaker tripped",
						"engine_failures", engineErrorCount,
						"threshold", maxEngineErrors,
					)
					circuitTripped = true
				}
			}
			if allowed {
				filtered = append(filtered, cmd)
			}
		}

		// Create commands array
		commandsTbl := L.NewTable()
		for i, cmd := range filtered {
			cmdTbl := L.NewTable()
			L.SetField(cmdTbl, "name", lua.LString(cmd.Name))
			L.SetField(cmdTbl, "help", lua.LString(cmd.Help))
			L.SetField(cmdTbl, "usage", lua.LString(cmd.Usage))
			L.SetField(cmdTbl, "source", lua.LString(cmd.Source))

			// Add to array (1-indexed for Lua)
			L.SetTable(commandsTbl, lua.LNumber(i+1), cmdTbl)
		}

		// Create result table with commands and incomplete metadata
		resultTbl := L.NewTable()
		L.SetField(resultTbl, "commands", commandsTbl)
		if hadEngineError {
			L.SetField(resultTbl, "incomplete", lua.LTrue)
		} else {
			L.SetField(resultTbl, "incomplete", lua.LFalse)
		}

		L.Push(resultTbl)
		if hadEngineError {
			L.Push(lua.LString("some commands may be hidden due to a system error; try again or contact an admin if the problem persists"))
		} else {
			L.Push(lua.LNil) // no error
		}
		return 2
	}
}

// canExecuteCommand checks if subject has all required capabilities for a command.
// Returns (allowed bool, hadError bool) where:
//   - allowed is true if command has no capabilities or subject has ALL required capabilities
//   - hadError is true if any engine evaluation failed (returned error or indicated infrastructure failure)
func (f *Functions) canExecuteCommand(ctx context.Context, subject string, cmd command.CommandEntry) (allowed, hadError bool) {
	caps := cmd.GetCapabilities()
	// Commands with no capabilities are always available
	if len(caps) == 0 {
		return true, false
	}

	// Check ALL capabilities (AND logic) — fail-closed on errors
	for _, cap := range caps {
		req, err := types.NewAccessRequest(subject, "execute", cap)
		if err != nil {
			slog.ErrorContext(ctx, "access request construction failed",
				"error", err, "subject", subject, "action", "execute", "resource", cap)
			observability.RecordEngineFailure("command_capability_check")
			hadError = true
			return false, hadError
		}

		decision, err := f.engine.Evaluate(ctx, req)
		if err != nil {
			slog.ErrorContext(ctx, "access evaluation failed",
				"error", err, "subject", subject, "action", "execute", "resource", cap)
			observability.RecordEngineFailure("command_capability_check")
			hadError = true
			return false, hadError
		}
		if !decision.IsAllowed() {
			if decision.IsInfraFailure() {
				slog.ErrorContext(ctx, "access check infrastructure failure",
					"subject", subject,
					"action", "execute",
					"resource", cap,
					"reason", decision.Reason(),
					"policy_id", decision.PolicyID(),
				)
				observability.RecordEngineFailure("command_capability_check")
				hadError = true
			}
			return false, hadError
		}
	}
	return true, hadError
}

// getCommandHelpFn returns the get_command_help host function.
// Args: command_name (string)
// Returns: (command info table, error string)
func (f *Functions) getCommandHelpFn(_ string) lua.LGFunction {
	return func(L *lua.LState) int {
		name := L.CheckString(1)
		if name == "" {
			L.RaiseError("command name cannot be empty")
			return 0
		}

		if f.commandRegistry == nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("command registry not available"))
			return 2
		}

		cmd, found := f.commandRegistry.Get(name)
		if !found {
			L.Push(lua.LNil)
			L.Push(lua.LString("command not found: " + name))
			return 2
		}

		// Build result table with full command details
		tbl := L.NewTable()
		L.SetField(tbl, "name", lua.LString(cmd.Name))
		L.SetField(tbl, "help", lua.LString(cmd.Help))
		L.SetField(tbl, "usage", lua.LString(cmd.Usage))
		L.SetField(tbl, "help_text", lua.LString(cmd.HelpText))
		L.SetField(tbl, "source", lua.LString(cmd.Source))

		// Add capabilities array (use getter for defensive copy)
		capsTbl := L.NewTable()
		for i, cap := range cmd.GetCapabilities() {
			L.SetTable(capsTbl, lua.LNumber(i+1), lua.LString(cap))
		}
		L.SetField(tbl, "capabilities", capsTbl)

		L.Push(tbl)
		L.Push(lua.LNil) // no error
		return 2
	}
}
