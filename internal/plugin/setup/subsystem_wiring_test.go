// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package setup_test

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/commandquery"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
)

// TestPluginSubsystemWiresCommandQuerierIntoLuaHost documents the late-binding
// MECHANISM (holomush-2zjio plan grounding): hostfunc.New is called at
// subsystem.go:193 before cmdRegistry is built at :391, so without the
// SetCommandQuerier late-bind the Lua list_commands always returns "command
// registry not available" in production.
//
// This test exercises the late-binding contract in isolation: create a Functions
// without a querier (simulating the pre-fix state), verify it errors, then call
// SetCommandQuerier (the fix) and verify it succeeds.
//
// The AUTHORITATIVE proof that the production PluginSubsystem.Start() path
// actually performs this late-bind (yielding a non-nil querier over the real
// registry) lives in test/integration/wholesystem/census_test.go
// ("late-binds a non-nil command querier that lists real commands (Start
// wiring)"), which drives integrationtest.Start(WithInTreePlugins()) — the real
// Start() — rather than hand-wiring hostfunc.New here. integrationtest cannot be
// imported from internal/plugin/setup (it constructs a PluginSubsystem, creating
// an import cycle), so that whole-system assertion lives in the wholesystem
// package where the harness is importable.
func TestPluginSubsystemWiresCommandQuerierIntoLuaHost(t *testing.T) {
	charID := ulid.Make()

	// --- Before fix: no querier wired (simulates the production nil-registry bug) ---
	hfUnwired := hostfunc.New(nil)

	L1 := lua.NewState()
	defer L1.Close()
	L1.SetContext(context.Background())
	hfUnwired.Register(L1, "test-plugin")

	err := L1.DoString(`result, errMsg = holomush.list_commands("` + charID.String() + `")`)
	assert.NoError(t, err)
	errVal := L1.GetGlobal("errMsg")
	assert.Contains(t, errVal.String(), "command registry not available",
		"BEFORE fix: list_commands must return 'command registry not available' when no querier is wired")

	// --- After fix: SetCommandQuerier called (simulates the production wiring) ---
	registry := &wiringTestRegistry{
		commands: []command.CommandEntry{
			{Name: "look", Help: "Look around", Usage: "look [target]", Source: "core"},
			{Name: "help", Help: "Get help", Usage: "help [command]", Source: "core"},
		},
	}
	engine := policytest.AllowAllEngine()
	aliasCache := command.NewAliasCache()

	hfWired := hostfunc.New(nil)
	q := commandquery.New(registry, engine, aliasCache)
	hfWired.SetCommandQuerier(q)

	L2 := lua.NewState()
	defer L2.Close()
	L2.SetContext(context.Background())
	hfWired.Register(L2, "test-plugin")

	err = L2.DoString(`result, errMsg = holomush.list_commands("` + charID.String() + `")`)
	assert.NoError(t, err)

	errVal2 := L2.GetGlobal("errMsg")
	assert.Equal(t, lua.LNil, errVal2,
		"AFTER fix: list_commands must NOT return an error when querier is wired")

	result := L2.GetGlobal("result")
	assert.NotEqual(t, lua.LNil, result,
		"AFTER fix: list_commands must return a result table when querier is wired")

	if tbl, ok := result.(*lua.LTable); ok {
		cmds := L2.GetField(tbl, "commands")
		assert.NotEqual(t, lua.LNil, cmds, "result.commands must be present")
		if cmdsTbl, ok2 := cmds.(*lua.LTable); ok2 {
			var names []string
			cmdsTbl.ForEach(func(_, v lua.LValue) {
				if ct, ok3 := v.(*lua.LTable); ok3 {
					names = append(names, L2.GetField(ct, "name").String())
				}
			})
			assert.Contains(t, names, "look",
				"wired list_commands must include 'look'")
			assert.Contains(t, names, "help",
				"wired list_commands must include 'help'")
		}
	}
}

// wiringTestRegistry is a minimal CommandRegistry for the wiring regression test.
type wiringTestRegistry struct {
	commands []command.CommandEntry
}

func (r *wiringTestRegistry) All() []command.CommandEntry { return r.commands }

func (r *wiringTestRegistry) Get(name string) (command.CommandEntry, bool) {
	for _, c := range r.commands {
		if c.Name == name {
			return c, true
		}
	}
	return command.CommandEntry{}, false
}
