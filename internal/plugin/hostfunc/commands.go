// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
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

// WithCommandRegistry sets the command registry for command-related host functions.
func WithCommandRegistry(reg CommandRegistry) Option {
	return func(f *Functions) {
		f.commandRegistry = reg
	}
}

// listCommandsFn returns the list_commands host function.
// Returns: (commands table, error string)
func (f *Functions) listCommandsFn(_ string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.commandRegistry == nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("command registry not available"))
			return 2
		}

		commands := f.commandRegistry.All()

		// Create result table
		tbl := L.NewTable()
		for i, cmd := range commands {
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
