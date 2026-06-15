// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/plugin/luabridge"
)

// TestLuaBrokeredEvalRejectsForeignResourceTypeViaSharedEntitlementGate is the
// Lua half of the INV-PLUGIN-26 runtime-parity proof, re-pointed onto the
// host-brokered eval surface after the atomic capability cutover
// (holomush-eykuh.4) retired the legacy holomush.evaluate hostfunc injection.
//
// It stands up the REAL Lua per-plugin bufconn endpoint (newPluginEndpoint:
// actor stamp + dispatch stamp + capability interceptor + LuaDefaultSet servers,
// the exact production wiring) and injects the generated host-capability tables
// via luabridge.RegisterHostCaps — the same path Host.DeliverEvent uses. It then
// drives _G["eval"].Evaluate{...} through actual Lua with a "scene:" resource.
//
// "scene" is foreign for any Lua plugin: luaHostCapAdapter.OwnedResourceTypes
// returns an empty map (Lua plugins own no resource types), so evalServer.Evaluate
// → pluginauthz.Evaluate rejects it with EVALUATE_UNENTITLED_TYPE BEFORE the
// engine is consulted. The engine is AllowAllEngine: if the brokered path skipped
// the entitlement gate, the engine would ALLOW and the call would return
// allowed=true — so a non-nil error here proves the gate fired, not the engine.
// This is the same load-bearing distinction the binary test
// (goplugin.TestINV5BinarySurfaceRejectsForeignResourceTypeViaSharedEntitlementGate)
// asserts; together they prove both runtimes reach the shared pluginauthz.Evaluate
// entitlement gate and cannot diverge.
//
// Verifies: INV-PLUGIN-26
func TestLuaBrokeredEvalRejectsForeignResourceTypeViaSharedEntitlementGate(t *testing.T) {
	const pluginName = "lua-plug"

	// AllowAllEngine: the engine would allow if the entitlement gate were skipped.
	// The rejection MUST come from the owned-types entitlement check first.
	adapter := newLuaHostCapAdapter(
		hostfunc.New(nil, hostfunc.WithEngine(policytest.AllowAllEngine())),
	)

	// Declare the "eval" capability so the capability interceptor permits the call.
	ep, err := newPluginEndpoint(adapter, &plugins.Manifest{
		Name: pluginName,
		Requires: []plugins.Dependency{
			{Kind: plugins.DependencyCapability, Name: "eval"},
		},
	})
	if err != nil {
		t.Fatalf("newPluginEndpoint: %v", err)
	}
	defer ep.Close()

	L := lua.NewState()
	defer L.Close()
	// A character actor on the context is what the server-side actor stamp +
	// LookupActor recover so the call reaches the entitlement gate (a missing
	// actor would fail earlier at ACTOR_NOT_FOUND, masking the gate).
	L.SetContext(core.WithActor(context.Background(),
		core.Actor{Kind: core.ActorCharacter, ID: core.NewULID().String()}))

	// Inject the host-cap Lua tables (including _G["eval"]) over the real endpoint
	// conn — the production RegisterHostCaps path. Only the declared "eval" token
	// is injected.
	luabridge.RegisterHostCaps(L, ep.Conn(), pluginName, []string{"eval"})

	// Drive the brokered eval surface through actual Lua. resp is nil and errmsg
	// is the bridge-surfaced status message on the entitlement rejection.
	if err := L.DoString(
		`resp, errmsg = eval.Evaluate({action = "read", resource = "scene:01SCENE0000000000000000000"})`,
	); err != nil {
		t.Fatalf("Lua eval.Evaluate dispatch errored: %v", err)
	}

	resp := L.GetGlobal("resp")
	if resp != lua.LNil {
		t.Fatalf("brokered eval MUST reject a foreign resource type; got non-nil response %v — "+
			"the Lua surface is bypassing the pluginauthz.Evaluate OwnedTypes entitlement gate", resp)
	}

	errmsg := L.GetGlobal("errmsg")
	if errmsg == lua.LNil {
		t.Fatal("brokered eval MUST return an error message alongside nil on entitlement rejection")
	}
	// The bridge surfaces status.Convert(err).Message(); the entitlement error
	// references the rejected resource type "scene", confirming the rejection came
	// from the owned-types gate and not from a generic engine deny.
	if !strings.Contains(errmsg.String(), "scene") {
		t.Fatalf("entitlement error message MUST reference the rejected resource type \"scene\" "+
			"(proving the OwnedTypes gate fired, not the engine); got %q", errmsg.String())
	}
}
