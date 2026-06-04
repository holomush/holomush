// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

// Structural invariant meta-tests for the plugin Evaluate surface.
//
// These tests are NOT behavioral unit tests (those live in host_service_test.go
// as TestEvaluateForeignResourceTypeRejected etc.) — they are structural locks
// that break loudly if a future change violates the spec invariants:
//
//   INV-PLUGIN-22: EvaluateRequest carries NO subject field (subject is host-derived,
//          never on the wire).
//   INV-PLUGIN-26: Both the binary (gRPC) surface AND the Lua (hostfunc) surface reject
//          a resource type the plugin does not own, proving they both reach the
//          shared pluginauthz.Evaluate entitlement gate and cannot diverge.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/pkg/errutil"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// TestINV1EvaluateRequestHasNoSubjectField reflects over the proto descriptor of
// PluginHostServiceEvaluateRequest and asserts no field named "subject" exists.
// The subject is always host-derived from the dispatch token; placing it on
// the wire would allow plugins to forge authorization subjects (INV-PLUGIN-22).
func TestINV1EvaluateRequestHasNoSubjectField(t *testing.T) {
	md := (&pluginv1.PluginHostServiceEvaluateRequest{}).ProtoReflect().Descriptor()
	fields := md.Fields()
	for i := range fields.Len() {
		name := string(fields.Get(i).Name())
		assert.NotEqual(t, "subject", name,
			"EvaluateRequest MUST NOT carry a subject field (INV-PLUGIN-22): "+
				"subject is host-derived from the dispatch token, never plugin-supplied")
	}
	// Positive assertion: only the expected fields are present.
	// This locks the wire contract so an accidental "subject" field addition
	// breaks this test even if the name check above is somehow weakened.
	assert.Equal(t, 2, fields.Len(),
		"EvaluateRequest MUST have exactly 2 fields (action + resource); "+
			"adding a subject field violates INV-PLUGIN-22")
}

// TestINV5BothSurfacesRejectForeignResourceTypeViaSharedEntitlementGate asserts
// that BOTH the binary (gRPC PluginHostService.Evaluate) surface AND the Lua
// (holomush.evaluate hostfunc) surface reject a resource type the plugin does
// not own. This proves both surfaces share the pluginauthz.Evaluate entitlement
// gate — a divergence (one surface skipping entitlement) MUST break this test.
//
// The foreign type used is "scene" for binary and "scene" for Lua. The binary
// plugin declares ResourceTypes: ["command"] (owns nothing relevant), and the
// Lua plugin passes empty OwnedTypes, so "scene" is unowned by both. The engine
// is AllowAllEngine, meaning the rejection comes from the entitlement check
// (before the engine runs), not from an engine deny. This is the load-bearing
// distinction: entitlement rejects first, so the engine result is irrelevant —
// if either surface skipped the entitlement gate, AllowAllEngine would allow
// the call and the assertion would fail.
func TestINV5BothSurfacesRejectForeignResourceTypeViaSharedEntitlementGate(t *testing.T) {
	t.Run("binary surface rejects foreign type via entitlement", func(t *testing.T) {
		t.Parallel()
		// Binary plugin that owns only "command" — "scene" is foreign.
		manifest := &plugins.Manifest{
			Name:          "core-cmd",
			Type:          plugins.TypeBinary,
			ResourceTypes: []string{"command"},
		}
		// AllowAllEngine: any engine-level decision would be ALLOW. The
		// rejection MUST come from the entitlement gate before engine runs.
		h := newTestHostWithEngine(t, "core-cmd", manifest, policytest.AllowAllEngine())
		defer func() { _ = h.Close(context.Background()) }()
		srv := &pluginHostServiceServer{host: h, pluginName: "core-cmd"}

		ctx, token := contextWithValidToken(t, srv, core.Actor{
			Kind: core.ActorCharacter,
			ID:   core.NewULID().String(),
		})
		defer h.tokenStore.Revoke(token)

		_, err := srv.Evaluate(ctx, &pluginv1.PluginHostServiceEvaluateRequest{
			Action:   "read",
			Resource: "scene:01SCENE0000000000000000000",
		})
		require.Error(t, err, "binary surface MUST reject foreign resource type via entitlement")
		errutil.AssertErrorCode(t, err, "EVALUATE_UNENTITLED_TYPE")
	})

	t.Run("lua surface rejects foreign type via entitlement", func(t *testing.T) {
		t.Parallel()
		// Lua plugins always pass empty OwnedTypes to pluginauthz.Evaluate
		// (see hostfunc/evaluate.go). The only carve-out is the "command:"
		// prefix. A "scene:" resource is therefore always unentitled for Lua.
		// AllowAllEngine: engine would allow if entitlement passed, so a
		// false result proves the entitlement gate fired, not the engine.
		L := lua.NewState()
		defer L.Close()
		L.SetContext(core.WithActor(context.Background(),
			core.Actor{Kind: core.ActorCharacter, ID: core.NewULID().String()}))

		hf := hostfunc.New(nil, hostfunc.WithEngine(policytest.AllowAllEngine()))
		hf.Register(L, "lua-plug")

		// "scene:01SCENE0000000000000000000" is a foreign type for Lua (empty OwnedTypes).
		// pluginauthz.Evaluate must return EVALUATE_UNENTITLED_TYPE before reaching the engine.
		require.NoError(t, L.DoString(
			`allowed, errmsg = holomush.evaluate("read", "scene:01SCENE0000000000000000000")`,
		))
		assert.False(t, bool(L.GetGlobal("allowed").(lua.LBool)),
			"Lua surface MUST reject foreign resource type (scene) via the shared entitlement gate; "+
				"if this passes, Lua is bypassing pluginauthz.Evaluate's OwnedTypes check")
		errmsg := L.GetGlobal("errmsg")
		require.NotEqual(t, lua.LNil, errmsg,
			"Lua evaluate MUST return an error message alongside false on entitlement rejection")
		// The Lua surface returns err.Error() which carries the human-readable
		// oops message. The key proof is: (a) allowed=false and (b) an error is
		// returned. Since AllowAllEngine would return allowed=true if the engine
		// ran, the combination proves the entitlement gate fired before the engine.
		assert.Contains(t, errmsg.String(), "scene",
			"error message MUST reference the rejected resource type to confirm "+
				"rejection came from the entitlement gate (pluginauthz.Evaluate OwnedTypes check), "+
				"not from the engine")
	})
}
