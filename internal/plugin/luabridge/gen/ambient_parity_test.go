// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/plugin/hostfunc"
)

// liveAmbientNames builds the production ambient surface in a real LState (via the
// SAME entrypoint production uses, hostfunc.Register — which installs holomush.* and
// calls RegisterStdlib for holo.fmt/holo.emit) and returns the set of
// "module.name" keys for every function-valued field. It MUST NOT call
// RegisterSessionFuncs (holo.session is capability-gated, out of scope — spec §1).
func liveAmbientNames(t *testing.T) map[string]bool {
	t.Helper()
	L := lua.NewState()
	defer L.Close()

	// nil KVStore is fine: Register installs no kv_* functions (kv is a retired
	// capability, host-brokered now — spec §2 authoritative boundary).
	f := hostfunc.New(nil)
	f.Register(L, "stub-gen")

	got := map[string]bool{}
	for _, global := range []string{"holomush", "holo"} {
		tbl, ok := L.GetGlobal(global).(*lua.LTable)
		require.True(t, ok, "global %q must be a table", global)
		collectFnNames(global, tbl, got)
	}
	return got
}

// collectFnNames recursively records module.name for every *LFunction field; it
// descends into subtables (e.g. holomush.config, holo.fmt) building the dotted path.
func collectFnNames(prefix string, tbl *lua.LTable, out map[string]bool) {
	tbl.ForEach(func(k, v lua.LValue) {
		name, ok := k.(lua.LString)
		if !ok {
			return
		}
		switch vv := v.(type) {
		case *lua.LFunction:
			out[prefix+"."+string(name)] = true
		case *lua.LTable:
			collectFnNames(prefix+"."+string(name), vv, out)
		}
	})
}

func declNames() map[string]bool {
	out := map[string]bool{}
	for _, d := range ambientDecls {
		out[d.Module+"."+d.Name] = true
	}
	return out
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestAmbientDeclTableMatchesRegistrations is the §5.2 drift guard: the decl
// table's (Module, Name) set MUST equal the live ambient registration set. A
// registered ambient fn missing from the table, or a table entry with no live
// registration, fails here.
func TestAmbientDeclTableMatchesRegistrations(t *testing.T) {
	live := liveAmbientNames(t)
	decl := declNames()
	assert.Equal(t, keys(live), keys(decl),
		"ambient decl table must exactly match live holomush.*/holo.* function registrations")
}
