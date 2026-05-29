// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package corehelp_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

// blanketMessage is the user-facing string the handler MUST reserve for a
// genuinely unavailable result (registry/engine nil → host returns nil result).
// It MUST NOT appear when the host returns a populated-but-incomplete list.
const blanketMessage = "Help is temporarily unavailable. Please try again later."

// listCommandsResult configures the stubbed holomush.list_commands return for
// a single test case, mirroring the host contract in
// internal/plugin/hostfunc/commands.go:
//   - hard failure (registry/engine nil): nilResult=true, errString!=""
//   - soft failure (incomplete): nilResult=false, populated commands,
//     incomplete=true, errString!=""
//   - clean: nilResult=false, populated commands, incomplete=false, errString=""
type listCommandsResult struct {
	commands   []map[string]string // each row: name, help, usage, source
	incomplete bool
	nilResult  bool
	errString  string // "" => Lua nil (no error)
}

// runHelp loads the real plugins/core-help/main.lua into a gopher-lua state
// with stubbed holo.fmt and holomush host tables, then invokes on_command with
// the given args. It returns the rendered output text regardless of whether the
// handler returned a bare string (success path) or a {status, output} table
// (error path).
func runHelp(t *testing.T, args string, stub listCommandsResult) string {
	t.Helper()

	L := lua.NewState()
	defer L.Close()

	registerFmtStub(L)
	registerHolomushStub(L, stub)

	require.NoError(t, L.DoFile("main.lua"), "load core-help main.lua")

	ctx := L.NewTable()
	L.SetField(ctx, "args", lua.LString(args))
	L.SetField(ctx, "character_id", lua.LString("01HZX0000000000000000TURQ"))

	require.NoError(t, L.CallByParam(lua.P{
		Fn:      L.GetGlobal("on_command"),
		NRet:    1,
		Protect: true,
	}, ctx), "call on_command")

	ret := L.Get(-1)
	L.Pop(1)

	switch v := ret.(type) {
	case lua.LString:
		return string(v)
	case *lua.LTable:
		return lua.LVAsString(L.GetField(v, "output"))
	default:
		t.Fatalf("on_command returned unexpected type %T", ret)
		return ""
	}
}

// registerFmtStub installs a minimal `holo.fmt` table. The formatters are
// identity-ish so that command names, the trailing hint, and any incomplete
// indicator survive into the asserted output.
func registerFmtStub(L *lua.LState) {
	identity := func(s *lua.LState) int {
		s.Push(lua.LString(s.CheckString(1)))
		return 1
	}
	fmtMod := L.NewTable()
	L.SetField(fmtMod, "header", L.NewFunction(identity))
	L.SetField(fmtMod, "bold", L.NewFunction(identity))
	L.SetField(fmtMod, "dim", L.NewFunction(identity))
	// table stub: flatten rows so cell contents (command names) appear in output.
	L.SetField(fmtMod, "table", L.NewFunction(func(s *lua.LState) int {
		arg := s.CheckTable(1)
		rows, ok := L.GetField(arg, "rows").(*lua.LTable)
		var b strings.Builder
		if ok {
			rows.ForEach(func(_, row lua.LValue) {
				rt, isTbl := row.(*lua.LTable)
				if !isTbl {
					return
				}
				rt.ForEach(func(_, cell lua.LValue) {
					b.WriteString(lua.LVAsString(cell))
					b.WriteString(" ")
				})
				b.WriteString("\n")
			})
		}
		s.Push(lua.LString(b.String()))
		return 1
	}))
	holo := L.NewTable()
	L.SetField(holo, "fmt", fmtMod)
	L.SetGlobal("holo", holo)
}

// registerHolomushStub installs a `holomush` table with a no-op log and a
// list_commands that returns the configured (result, err) pair.
func registerHolomushStub(L *lua.LState, stub listCommandsResult) {
	holomush := L.NewTable()
	L.SetField(holomush, "log", L.NewFunction(func(_ *lua.LState) int { return 0 }))
	L.SetField(holomush, "list_commands", L.NewFunction(func(s *lua.LState) int {
		if stub.nilResult {
			s.Push(lua.LNil)
		} else {
			result := s.NewTable()
			cmds := s.NewTable()
			for i, c := range stub.commands {
				ct := s.NewTable()
				s.SetField(ct, "name", lua.LString(c["name"]))
				s.SetField(ct, "help", lua.LString(c["help"]))
				s.SetField(ct, "usage", lua.LString(c["usage"]))
				s.SetField(ct, "source", lua.LString(c["source"]))
				s.RawSetInt(cmds, i+1, ct)
			}
			s.SetField(result, "commands", cmds)
			s.SetField(result, "incomplete", lua.LBool(stub.incomplete))
			s.Push(result)
		}
		if stub.errString != "" {
			s.Push(lua.LString(stub.errString))
		} else {
			s.Push(lua.LNil)
		}
		return 2
	}))
	L.SetGlobal("holomush", holomush)
}

func twoCommands() []map[string]string {
	return []map[string]string{
		{"name": "help", "help": "Show help", "source": "core-help"},
		{"name": "look", "help": "Look around", "source": "core-world"},
	}
}

// TestHelpHandlerRendersAccordingToListCommandsContract exercises the no-args
// (list_all_commands) and "search <term>" (search_commands) paths against every
// tier of the holomush.list_commands contract:
//   - soft failure (populated result + incomplete=true + err): render the usable
//     commands with an incompleteness indicator, never the blanket message;
//   - hard failure (nil result + err): show the blanket message;
//   - clean (populated result, no err): render the full list, no indicator.
//
// The "incomplete" substring is the lowercase token from the dim indicator the
// handler appends; its presence/absence distinguishes soft from clean/hard.
func TestHelpHandlerRendersAccordingToListCommandsContract(t *testing.T) {
	const searchBlanket = "Search is temporarily unavailable"

	tests := []struct {
		name            string
		args            string
		result          listCommandsResult
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "list renders partial list with indicator when incomplete with error",
			args: "",
			result: listCommandsResult{
				commands:   twoCommands(),
				incomplete: true,
				errString:  "some commands may be hidden due to a system error; try again or contact an admin if the problem persists",
			},
			wantContains:    []string{"help", "look", "incomplete"},
			wantNotContains: []string{blanketMessage},
		},
		{
			name:         "list shows blanket message when result unavailable",
			args:         "",
			result:       listCommandsResult{nilResult: true, errString: "command registry not available"},
			wantContains: []string{blanketMessage},
		},
		{
			name:            "list renders full list without indicator when complete",
			args:            "",
			result:          listCommandsResult{commands: twoCommands()},
			wantContains:    []string{"help", "look"},
			wantNotContains: []string{blanketMessage, "incomplete"},
		},
		{
			name: "search renders partial matches with indicator when incomplete with error",
			args: "search look",
			result: listCommandsResult{
				commands:   twoCommands(),
				incomplete: true,
				errString:  "some commands may be hidden due to a system error",
			},
			wantContains:    []string{"look", "incomplete"},
			wantNotContains: []string{searchBlanket},
		},
		{
			name:         "search shows blanket message when result unavailable",
			args:         "search look",
			result:       listCommandsResult{nilResult: true, errString: "access engine not available"},
			wantContains: []string{searchBlanket},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := runHelp(t, tt.args, tt.result)
			for _, want := range tt.wantContains {
				assert.Contains(t, out, want)
			}
			for _, notWant := range tt.wantNotContains {
				assert.NotContains(t, out, notWant)
			}
		})
	}
}
