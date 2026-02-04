// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"

	"github.com/oklog/ulid/v2"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/command"
)

// CommandRegistry provides read-only access to registered commands.
type CommandRegistry interface {
	// All returns all registered commands.
	All() []command.CommandEntry
	// Get retrieves a command by name.
	Get(name string) (command.CommandEntry, bool)
}

// AccessControl checks if a subject can perform an action on a resource.
// This is used to filter commands based on character capabilities.
type AccessControl interface {
	// Check returns true if subject can perform action on resource.
	Check(ctx context.Context, subject, action, resource string) bool
}

// WithCommandRegistry sets the command registry for command-related host functions.
func WithCommandRegistry(reg CommandRegistry) Option {
	return func(f *Functions) {
		f.commandRegistry = reg
	}
}

// WithAccessControl sets the access control for capability filtering.
func WithAccessControl(ac AccessControl) Option {
	return func(f *Functions) {
		f.access = ac
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

		if f.access == nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("access control not available"))
			return 2
		}

		commands := f.commandRegistry.All()
		subject := "char:" + charID.String()
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
	// Commands with no capabilities are always available
	if len(cmd.Capabilities) == 0 {
		return true
	}

	// Check ALL capabilities (AND logic)
	for _, cap := range cmd.Capabilities {
		if !f.access.Check(ctx, subject, "execute", cap) {
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

		// Add capabilities array
		capsTbl := L.NewTable()
		for i, cap := range cmd.Capabilities {
			L.SetTable(capsTbl, lua.LNumber(i+1), lua.LString(cap))
		}
		L.SetField(tbl, "capabilities", capsTbl)

		L.Push(tbl)
		L.Push(lua.LNil) // no error
		return 2
	}
}
