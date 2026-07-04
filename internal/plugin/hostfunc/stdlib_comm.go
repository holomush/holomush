// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/pkg/plugin/comm"
)

// registerComm sets up the holo.comm.* namespace: pose/say/ooc/emit each
// return the CommunicationContent JSON built by pkg/plugin/comm (the single
// source shared with binary plugins). Called from RegisterStdlib alongside
// registerFmt/registerEmit. A builder error (a marshal failure the builders
// cannot sanitize away) is surfaced as a Lua error via RaiseError rather than
// pushing a partial payload — fail-closed, matching the Go binary path.
func registerComm(ls *lua.LState, holoTable *lua.LTable) {
	mod := ls.NewTable()

	ls.SetField(mod, "pose", ls.NewFunction(func(l *lua.LState) int {
		a := comm.Author{ID: l.CheckString(1), Name: l.CheckString(2)}
		payload, err := comm.Pose(a, l.CheckString(3), l.CheckString(4))
		if err != nil {
			l.RaiseError("holo.comm.pose: %v", err)
			return 0
		}
		l.Push(lua.LString(payload))
		return 1
	}))
	ls.SetField(mod, "say", ls.NewFunction(func(l *lua.LState) int {
		a := comm.Author{ID: l.CheckString(1), Name: l.CheckString(2)}
		payload, err := comm.Say(a, l.CheckString(3))
		if err != nil {
			l.RaiseError("holo.comm.say: %v", err)
			return 0
		}
		l.Push(lua.LString(payload))
		return 1
	}))
	ls.SetField(mod, "ooc", ls.NewFunction(func(l *lua.LState) int {
		a := comm.Author{ID: l.CheckString(1), Name: l.CheckString(2)}
		payload, err := comm.OOC(a, l.CheckString(3))
		if err != nil {
			l.RaiseError("holo.comm.ooc: %v", err)
			return 0
		}
		l.Push(lua.LString(payload))
		return 1
	}))
	ls.SetField(mod, "emit", ls.NewFunction(func(l *lua.LState) int {
		payload, err := comm.Emit(l.CheckString(1))
		if err != nil {
			l.RaiseError("holo.comm.emit: %v", err)
			return 0
		}
		l.Push(lua.LString(payload))
		return 1
	}))

	ls.SetField(holoTable, "comm", mod)
}
