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

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/pkg/errutil"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// TestINV1EvaluateRequestHasNoSubjectField reflects over the proto descriptor of
// host.v1 EvaluateRequest and asserts no field named "subject" exists.
// The subject is always host-derived from the dispatch token; placing it on
// the wire would allow plugins to forge authorization subjects (INV-PLUGIN-22).
//
// This is the spec-canonical binding mechanism for INV-PLUGIN-22 (the design's
// stated meta-test: "proto descriptor has no subject field").
//
// Verifies: INV-PLUGIN-22
func TestINV1EvaluateRequestHasNoSubjectField(t *testing.T) {
	md := (&hostv1.EvaluateRequest{}).ProtoReflect().Descriptor()
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

// TestINV5BinarySurfaceRejectsForeignResourceTypeViaSharedEntitlementGate
// asserts that the binary (gRPC PluginHostService.Evaluate) surface rejects a
// resource type the plugin does not own, proving it reaches the shared
// pluginauthz.Evaluate entitlement gate (INV-PLUGIN-26).
//
// The Lua half of the same parity proof lives in package lua as
// TestLuaBrokeredEvalRejectsForeignResourceTypeViaSharedEntitlementGate — it
// drives the host-brokered eval surface (_G["eval"].Evaluate) over the real Lua
// bufconn endpoint. After the atomic capability cutover (holomush-eykuh.4) the
// legacy holomush.evaluate hostfunc injection is retired, so the Lua surface is
// the brokered EvalService, not an in-VM global; the two subtests cannot share a
// package because the binary server and the Lua endpoint live in different
// packages. Together they still prove both runtimes reach the same gate.
//
// The foreign type used is "scene". The binary plugin declares
// ResourceTypes: ["command"] (owns nothing relevant), so "scene" is unowned. The
// engine is AllowAllEngine, meaning the rejection comes from the entitlement
// check (before the engine runs), not from an engine deny. This is the
// load-bearing distinction: entitlement rejects first, so the engine result is
// irrelevant — if the surface skipped the entitlement gate, AllowAllEngine would
// allow the call and the assertion would fail.
//
// Verifies: INV-PLUGIN-26
func TestINV5BinarySurfaceRejectsForeignResourceTypeViaSharedEntitlementGate(t *testing.T) {
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

	_, err := srv.Evaluate(ctx, &hostv1.EvaluateRequest{
		Action:   "read",
		Resource: "scene:01SCENE0000000000000000000",
	})
	require.Error(t, err, "binary surface MUST reject foreign resource type via entitlement")
	errutil.AssertErrorCode(t, err, "EVALUATE_UNENTITLED_TYPE")
}
