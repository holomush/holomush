// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"
	"log/slog"

	"github.com/samber/oops"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/command/commandquery"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

// WithCommandQuerier injects the shared command-query service. The
// list_commands / get_command_help host functions delegate to it exclusively,
// providing the single ABAC-filtered enumeration via commandquery.Querier
// (design spec INV-COMMAND-1: exactly one command-visibility filter). There is no
// second filter implementation in this package.
func WithCommandQuerier(q *commandquery.Querier) Option {
	return func(f *Functions) {
		f.commandQuerier = q
	}
}

// listCommandsFn returns the list_commands host function.
// Args: character_id (string) — IGNORED for authorization (see below); kept for
// Lua-call-signature compatibility only.
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
// Subject derivation (INV-PLUGIN-51): the ABAC subject is the HOST-VOUCHED
// dispatch subject stamped on ls.Context() by the Lua host's stampDispatch
// (eykuh.3.6), NOT the Lua-supplied character_id. A plugin therefore cannot
// enumerate another character's command visibility by passing a foreign id.
// The character_id arg is read for call-signature compatibility and discarded;
// removing it from the Lua surface is a follow-up. When no host-vouched
// dispatch subject is present we fail closed (no command visibility).
//
// This function is a thin shim: it resolves the host-vouched subject, then
// delegates to commandquery.Querier (INV-COMMAND-1). All ABAC filtering, capability
// AND-logic, and the engine-error circuit breaker live in commandquery.
func (f *Functions) listCommandsFn(_ string) lua.LGFunction {
	return func(ls *lua.LState) int {
		// Read (and discard for authz) the character_id arg for call-signature
		// compatibility. It MUST NOT influence the ABAC subject.
		_ = ls.CheckString(1)

		ctx := ls.Context()
		if ctx == nil {
			// Lua VM has no context — fall back to background context.
			// This shouldn't happen when events are delivered via DeliverEvent,
			// which always sets a context. Log a warning for visibility.
			slog.Warn("lua VM context is nil in list_commands, using background context")
			ctx = context.Background()
		}

		if f.commandQuerier == nil {
			ls.Push(lua.LNil)
			ls.Push(lua.LString("command registry not available"))
			return 2
		}

		dc, ok := pluginauthz.DispatchForHost(ctx)
		if !ok || dc.Subject == "" {
			// Fail closed: no host-vouched acting character ⇒ no command visibility.
			ls.Push(lua.LNil)
			ls.Push(lua.LString("no host-vouched dispatch subject"))
			return 2
		}

		return listCommandsViaQuerier(ctx, ls, f.commandQuerier, dc.Subject)
	}
}

// listCommandsViaQuerier delegates to commandquery.Querier and maps its Result
// onto the Lua-table shape the help plugin expects.
func listCommandsViaQuerier(ctx context.Context, ls *lua.LState, q *commandquery.Querier, subject string) int {
	res, qErr := q.Available(ctx, subject)
	if qErr != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString("command listing failed"))
		return 2
	}

	commandsTbl := ls.NewTable()
	for i := range res.Commands {
		cmdTbl := ls.NewTable()
		ls.SetField(cmdTbl, "name", lua.LString(res.Commands[i].Name))
		ls.SetField(cmdTbl, "help", lua.LString(res.Commands[i].Help))
		ls.SetField(cmdTbl, "usage", lua.LString(res.Commands[i].Usage))
		ls.SetField(cmdTbl, "source", lua.LString(res.Commands[i].Source))
		ls.SetTable(commandsTbl, lua.LNumber(i+1), cmdTbl)
	}

	resultTbl := ls.NewTable()
	ls.SetField(resultTbl, "commands", commandsTbl)
	ls.SetField(resultTbl, "incomplete", lua.LBool(res.Incomplete))

	ls.Push(resultTbl)
	if res.Incomplete {
		ls.Push(lua.LString("some commands may be hidden due to a system error; try again or contact an admin if the problem persists"))
	} else {
		ls.Push(lua.LNil)
	}
	return 2
}

// getCommandHelpFn returns the get_command_help host function.
// Args: command_name (string), character_id (string) — character_id is IGNORED
// for authorization (see below); kept for Lua-call-signature compatibility only.
// Returns: (command info table, error string)
//
// This function is a thin shim over commandquery.Querier.Help (INV-COMMAND-1). It maps
// the querier's typed errors back to the legacy error strings the Lua help
// plugin matches on:
//   - NOT_FOUND          → "command not found: <name>"
//   - PERMISSION_DENIED  → "access denied"
//   - UNAVAILABLE / other → "access check failed"
//
// Subject derivation (INV-PLUGIN-51): the capability check uses the HOST-VOUCHED
// dispatch subject stamped on ls.Context() (eykuh.3.6), NOT the Lua-supplied
// character_id. A plugin cannot probe another character's gated-command help by
// passing a foreign id. When no host-vouched dispatch subject is present we
// fail closed before consulting the querier — a missing dispatch is abnormal
// for an in-VM hostfunc call.
//
// Precedence note: for the with-dispatch case, command lookup (NOT_FOUND) still
// happens inside Querier.Help BEFORE any capability/character evaluation, so a
// non-existent command yields "command not found" regardless of caller. We
// preserve that by calling Help with the host-vouched subject (no pre-empting
// of the lookup).
func (f *Functions) getCommandHelpFn(_ string) lua.LGFunction {
	return func(ls *lua.LState) int {
		name := ls.CheckString(1)
		// Read (and discard for authz) the character_id arg for call-signature
		// compatibility. It MUST NOT influence the ABAC subject.
		_ = ls.CheckString(2)
		if name == "" {
			ls.RaiseError("command name cannot be empty")
			return 0
		}

		ctx := ls.Context()
		if ctx == nil {
			ctx = context.Background()
			slog.WarnContext(ctx, "lua VM context is nil in get_command_help, using background context")
		}

		if f.commandQuerier == nil {
			ls.Push(lua.LNil)
			ls.Push(lua.LString("command registry not available"))
			return 2
		}

		dc, ok := pluginauthz.DispatchForHost(ctx)
		if !ok || dc.Subject == "" {
			// Fail closed: no host-vouched acting character ⇒ no command help.
			ls.Push(lua.LNil)
			ls.Push(lua.LString("no host-vouched dispatch subject"))
			return 2
		}

		detail, helpErr := f.commandQuerier.Help(ctx, dc.Subject, name)
		if helpErr != nil {
			if oopsErr, ok := oops.AsOops(helpErr); ok {
				switch oopsErr.Code() {
				case "NOT_FOUND":
					ls.Push(lua.LNil)
					ls.Push(lua.LString("command not found: " + name))
					return 2
				case "PERMISSION_DENIED":
					ls.Push(lua.LNil)
					ls.Push(lua.LString("access denied"))
					return 2
				}
			}
			ls.Push(lua.LNil)
			ls.Push(lua.LString("access check failed"))
			return 2
		}
		return buildHelpTable(ls, detail)
	}
}

// buildHelpTable pushes a command help result table onto the Lua stack.
func buildHelpTable(ls *lua.LState, d commandquery.Detail) int {
	tbl := ls.NewTable()
	ls.SetField(tbl, "name", lua.LString(d.Name))
	ls.SetField(tbl, "help", lua.LString(d.Help))
	ls.SetField(tbl, "usage", lua.LString(d.Usage))
	ls.SetField(tbl, "help_text", lua.LString(d.HelpText))
	ls.SetField(tbl, "source", lua.LString(d.Source))

	capsTbl := ls.NewTable()
	for i, cap := range d.Capabilities {
		capTbl := ls.NewTable()
		ls.SetField(capTbl, "action", lua.LString(cap.Action))
		ls.SetField(capTbl, "resource", lua.LString(cap.Resource))
		if cap.Scope != "" {
			ls.SetField(capTbl, "scope", lua.LString(cap.Scope))
		}
		ls.SetTable(capsTbl, lua.LNumber(i+1), capTbl)
	}
	ls.SetField(tbl, "capabilities", capsTbl)

	ls.Push(tbl)
	ls.Push(lua.LNil)
	return 2
}
