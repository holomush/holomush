// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
)

func TestEvaluateAllowedByEngine(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	charID := core.NewULID()
	L.SetContext(core.WithActor(context.Background(),
		core.Actor{Kind: core.ActorCharacter, ID: charID.String()}))

	hf := hostfunc.New(nil, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "lua-plug")
	hf.RegisterCapabilityFuncsForTest(L, "lua-plug")

	require.NoError(t, L.DoString(`allowed, reason = holomush.evaluate("execute", "command:greet")`))
	assert.True(t, bool(L.GetGlobal("allowed").(lua.LBool)))
	// AllowAllEngine returns reason "test-allow-all" (non-empty) → LString branch fires.
	reason, ok := L.GetGlobal("reason").(lua.LString)
	assert.True(t, ok, "reason should be an LString for non-empty reason")
	assert.Equal(t, lua.LString("test-allow-all"), reason)
}

func TestEvaluateDeniedByEngine(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	L.SetContext(core.WithActor(context.Background(),
		core.Actor{Kind: core.ActorCharacter, ID: core.NewULID().String()}))

	hf := hostfunc.New(nil, hostfunc.WithEngine(policytest.DenyAllEngine()))
	hf.Register(L, "lua-plug")
	hf.RegisterCapabilityFuncsForTest(L, "lua-plug")

	require.NoError(t, L.DoString(`allowed, reason = holomush.evaluate("execute", "command:greet")`))
	assert.False(t, bool(L.GetGlobal("allowed").(lua.LBool)))
}

func TestEvaluateNoActorFailsClosed(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	L.SetContext(context.Background())
	hf := hostfunc.New(nil, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "lua-plug")
	hf.RegisterCapabilityFuncsForTest(L, "lua-plug")

	require.NoError(t, L.DoString(`allowed, err = holomush.evaluate("execute", "command:greet")`))
	assert.False(t, bool(L.GetGlobal("allowed").(lua.LBool)))
	assert.NotEqual(t, lua.LNil, L.GetGlobal("err"))
}

func TestEvaluateNilEngineFailsClosed(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	charID := core.NewULID()
	L.SetContext(core.WithActor(context.Background(),
		core.Actor{Kind: core.ActorCharacter, ID: charID.String()}))

	// No engine configured — WithEngine is intentionally omitted.
	hf := hostfunc.New(nil)
	hf.Register(L, "lua-plug")
	hf.RegisterCapabilityFuncsForTest(L, "lua-plug")

	require.NoError(t, L.DoString(`allowed, err = holomush.evaluate("execute", "command:greet")`))
	assert.False(t, bool(L.GetGlobal("allowed").(lua.LBool)),
		"nil engine MUST deny (fail closed)")
	assert.NotEqual(t, lua.LNil, L.GetGlobal("err"),
		"nil engine MUST return a non-nil error string")
}

func TestEvaluateNilLStateContextFailsClosed(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	// No SetContext at all → L.Context() returns nil → context.Background() fallback fires
	// → no actor in context → pluginauthz.Evaluate fails closed with EVALUATE_NO_SUBJECT.
	hf := hostfunc.New(nil, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "lua-plug")
	hf.RegisterCapabilityFuncsForTest(L, "lua-plug")

	require.NoError(t, L.DoString(`allowed, err = holomush.evaluate("execute", "command:greet")`))
	assert.False(t, bool(L.GetGlobal("allowed").(lua.LBool)))
	assert.NotEqual(t, lua.LNil, L.GetGlobal("err"))
}

func TestEvaluateContextDeadlineExceededFailsClosed(t *testing.T) {
	// Verifies that evaluateFn propagates the context into the engine call so
	// that a deadline-exceeded error surfaces as a Lua-level denial. Uses
	// policytest.NewErrorEngine to inject context.DeadlineExceeded as the
	// engine error, exercising the error-return path of pluginauthz.Evaluate.
	L := lua.NewState()
	defer L.Close()
	charID := core.NewULID()
	L.SetContext(core.WithActor(context.Background(),
		core.Actor{Kind: core.ActorCharacter, ID: charID.String()}))

	hf := hostfunc.New(nil, hostfunc.WithEngine(policytest.NewErrorEngine(context.DeadlineExceeded)))
	hf.Register(L, "lua-plug")
	hf.RegisterCapabilityFuncsForTest(L, "lua-plug")

	require.NoError(t, L.DoString(`allowed, err = holomush.evaluate("execute", "command:greet")`))
	assert.False(t, bool(L.GetGlobal("allowed").(lua.LBool)),
		"engine error MUST deny (fail closed)")
	assert.NotEqual(t, lua.LNil, L.GetGlobal("err"),
		"engine error MUST surface as non-nil Lua error")
}
