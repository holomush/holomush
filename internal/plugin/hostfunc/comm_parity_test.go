// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/pkg/plugin/comm"
	commv1 "github.com/holomush/holomush/pkg/proto/holomush/comm/v1"
)

// decodeCommJSON is a test helper: unmarshal a builder's JSON string into a
// CommunicationContent proto (fatal on malformed input — the builders are the
// unit under test, so a decode error is a real failure, not a precondition).
func decodeCommJSON(t *testing.T, jsonStr string) *commv1.CommunicationContent {
	t.Helper()
	var msg commv1.CommunicationContent
	require.NoError(t, protojson.Unmarshal([]byte(jsonStr), &msg))
	return &msg
}

// Verifies: INV-COMM-2
//
// TestGoAndLuaCommBuildersAgree pins the runtime-symmetry guarantee that the Go
// builder (pkg/plugin/comm, used by binary plugins) and the holo.comm.* Lua
// hostfunc (used by Lua plugins) decode to an equal CommunicationContent proto
// for identical inputs. Both delegate to the single Go source, so parity is
// structural; this test guards against a future divergence in EITHER runtime.
// Coverage spans all four builders — pose (no_space via ";"), say (trim), ooc
// (ooc_style), and the actorless emit — so a divergence in any one closure
// (not just pose) fails the binding.
func TestGoAndLuaCommBuildersAgree(t *testing.T) {
	// One Lua state shared across the subtests, which write a global `out`; the
	// subtests MUST stay serial (no t.Parallel) — they race on ls/out otherwise.
	ls := lua.NewState()
	defer ls.Close()
	hostfunc.RegisterStdlib(ls) // production holo.comm

	// assertParity decodes the Go builder's output and the Lua hostfunc's output
	// (captured from the global `out`) and asserts the two protos are equal.
	assertParity := func(t *testing.T, goJSON, luaCall string) {
		t.Helper()
		require.NoError(t, ls.DoString("out = "+luaCall))
		g := decodeCommJSON(t, goJSON)
		l := decodeCommJSON(t, ls.GetGlobal("out").String())
		require.True(t, proto.Equal(g, l), "go=%v lua=%v", g, l)
	}

	// mustBuild adapts a builder's (payload, error) result to the single JSON
	// string assertParity compares, failing the test on a builder error.
	mustBuild := func(jsonStr string, err error) string {
		t.Helper()
		require.NoError(t, err)
		return jsonStr
	}

	a := comm.Author{ID: "01H", Name: "Alaric"}

	t.Run("pose semipose no-space, pose, and plain", func(t *testing.T) {
		for _, c := range []struct{ invoked, raw string }{
			{";", "waves"},
			{":", "smiles"},
			{"", "plain pose"},
		} {
			assertParity(t,
				mustBuild(comm.Pose(a, c.invoked, c.raw)),
				`holo.comm.pose("01H","Alaric","`+c.invoked+`","`+c.raw+`")`)
		}
	})

	t.Run("say trims text", func(t *testing.T) {
		assertParity(t,
			mustBuild(comm.Say(a, "  hello there  ")),
			`holo.comm.say("01H","Alaric","  hello there  ")`)
	})

	t.Run("ooc classifies style", func(t *testing.T) {
		for _, raw := range []string{":laughs", ";'s data is gone", "brb"} {
			assertParity(t,
				mustBuild(comm.OOC(a, raw)),
				`holo.comm.ooc("01H","Alaric","`+raw+`")`)
		}
	})

	t.Run("emit is actorless", func(t *testing.T) {
		assertParity(t,
			mustBuild(comm.Emit("the ground trembles")),
			`holo.comm.emit("the ground trembles")`)
	})
}
