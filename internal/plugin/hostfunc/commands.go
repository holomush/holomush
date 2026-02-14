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
)

// CommandRegistry provides read-only access to registered commands.
type CommandRegistry interface {
	// All returns all registered commands.
	All() []command.CommandEntry
	// Get retrieves a command by name.
	Get(name string) (command.CommandEntry, bool)
}

// AccessPolicyEngine evaluates access requests against loaded policies.
// This mirrors internal/access/policy.AccessPolicyEngine to avoid coupling hostfunc to the access package.
// Used for command capability filtering in list_commands.
type AccessPolicyEngine interface {
	Evaluate(ctx context.Context, req types.AccessRequest) (types.Decision, error)
}

// WithCommandRegistry sets the command registry for command-related host functions.
func WithCommandRegistry(reg CommandRegistry) Option {
	return func(f *Functions) {
		f.commandRegistry = reg
	}
}

// WithEngine sets the access policy engine for capability filtering.
func WithEngine(engine AccessPolicyEngine) Option {
	return func(f *Functions) {
		f.engine = engine
	}
}

// listCommandsFn returns the list_commands host function.
// Args: character_id (string) - the character whose capabilities determine visible commands
// Returns: (commands table, error string)
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
		subject := access.SubjectCharacter + charID.String()
		ctx := context.Background()

		// Filter commands by character capabilities
		var filtered []command.CommandEntry
		for _, cmd := range commands {
			if f.canExecuteCommand(ctx, subject, cmd) {
				filtered = append(filtered, cmd)
			}
		}

		// Create result table
		tbl := L.NewTable()
		for i, cmd := range filtered {
			cmdTbl := L.NewTable()
			L.SetField(cmdTbl, "name", lua.LString(cmd.Name))
			L.SetField(cmdTbl, "help", lua.LString(cmd.Help))
			L.SetField(cmdTbl, "usage", lua.LString(cmd.Usage))
			L.SetField(cmdTbl, "source", lua.LString(cmd.Source))

			// Add to array (1-indexed for Lua)
			L.SetTable(tbl, lua.LNumber(i+1), cmdTbl)
		}

		L.Push(tbl)
		L.Push(lua.LNil) // no error
		return 2
	}
}

// canExecuteCommand checks if subject has all required capabilities for a command.
// Returns true if command has no capabilities or subject has ALL required capabilities.
func (f *Functions) canExecuteCommand(ctx context.Context, subject string, cmd command.CommandEntry) bool {
	caps := cmd.GetCapabilities()
	// Commands with no capabilities are always available
	if len(caps) == 0 {
		return true
	}

	// Check ALL capabilities (AND logic) — fail-closed on errors
	for _, cap := range caps {
		decision, err := f.engine.Evaluate(ctx, types.AccessRequest{
			Subject: subject, Action: "execute", Resource: cap,
		})
		if err != nil {
			slog.ErrorContext(ctx, "access evaluation failed",
				"error", err, "subject", subject, "action", "execute", "resource", cap)
			return false
		}
		if !decision.IsAllowed() {
			return false
		}
	}
	return true
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
