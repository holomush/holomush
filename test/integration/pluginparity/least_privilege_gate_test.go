// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package pluginparity

import (
	"context"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	plugins "github.com/holomush/holomush/internal/plugin"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// kvCapabilityToken is the controlled-vocabulary capability token whose service
// is KVService (internal/plugin/capability_vocab.go: "kv" -> "KVService"). It is
// registered in BOTH capability sets (register.go:53), so KVService is REACHABLE
// (routes to a handler) on both runtimes regardless of declaration — which is
// exactly why it isolates the DECLARATION gate: any denial of a KV call can only
// come from the declaration gate, never from a missing route. Driving an
// undeclared "world.mutation" instead would let the binary runtime deny via a
// missing route (Unimplemented, BinaryDefaultSet omits WorldMutationService) —
// a different mechanism that would NOT prove the shared declaration gate.
const kvCapabilityToken = "kv"

// declaredDenialMessage is the interceptor's fail-closed denial text for an
// undeclared capability (hostcap/interceptor.go: capDeny("CAPABILITY_NOT_DECLARED",
// "plugin did not declare capability", …)). capDeny wraps a gRPC status, so
// grpc-go surfaces the denial on the wire as codes.PermissionDenied with this
// message preserved (holomush-yc05l). Both runtimes therefore deny IDENTICALLY:
// same code, same message — the structural witness that one shared gate denied both.
const declaredDenialMessage = "plugin did not declare capability"

// undeclaredManifest is a plugin manifest with NO capability requires, so
// hostcap.DeclaredAccessFromManifest yields (_, false) for the "kv" token: the
// declaration gate fails closed. Both runtimes build their interceptor's
// DeclaredAccess from THIS manifest via the SAME DeclaredAccessFromManifest
// constructor, so per-runtime divergence is impossible by construction.
func undeclaredManifest() *plugins.Manifest {
	return &plugins.Manifest{Name: parityPluginName}
}

// declaredKVManifest declares the "kv" capability, so DeclaredAccessFromManifest
// yields (_, true): the declaration gate PASSES and the call reaches the shared
// Unimplemented kvServer base. It is the positive half of the contrast that
// proves the GATE — not the base — denied the undeclared call.
func declaredKVManifest() *plugins.Manifest {
	return &plugins.Manifest{
		Name: parityPluginName,
		Requires: []plugins.Dependency{
			{Kind: plugins.DependencyCapability, Name: kvCapabilityToken},
		},
	}
}

// The gatedBinaryEndpoint / gatedLuaEndpoint builders these specs use live in
// gated_endpoints_test.go (shared with the non-scoped ABAC-denial parity spec).

var _ = Describe("Cross-runtime least-privilege declaration gate", func() {
	// INV-PLUGIN-45: "The declaration gate that enforces least privilege MUST
	// live at the broker/registry common path shared by both runtimes;
	// per-runtime gating that could diverge is forbidden."
	//
	// The single shared gate is hostcap.NewCapabilityInterceptor, whose
	// DeclaredAccess lookup is built by hostcap.DeclaredAccessFromManifest — the
	// ONE constructor both production install sites consume (binary:
	// goplugin/host_service.go:45; Lua: lua/bufconn_endpoint.go:44). Per-runtime
	// divergence is impossible BY CONSTRUCTION because both runtimes pass the
	// SAME manifest through the SAME constructor to the SAME interceptor.
	//
	// This spec drives the SAME undeclared capability ("kv", whose KVService is
	// registered in BOTH sets so it ROUTES to a handler on both runtimes) through
	// each runtime's gated endpoint and asserts BOTH are denied IDENTICALLY:
	//   - same wire code (codes.PermissionDenied — capDeny wraps a gRPC status, so
	//     grpc-go surfaces CAPABILITY_NOT_DECLARED as PermissionDenied, holomush-yc05l),
	//   - same denial message ("plugin did not declare capability"),
	//   - and that code is NOT codes.Unimplemented.
	//
	// The final clause is load-bearing: KVService's base handler returns
	// codes.Unimplemented. So an undeclared call that returned Unimplemented would
	// mean the GATE was bypassed and the BASE answered. The contrast spec below
	// proves a DECLARED kv call DOES reach that Unimplemented base on both
	// runtimes — confirming the undeclared denial was the GATE firing, not the
	// base.
	//
	// Verifies: INV-PLUGIN-45
	It("denies an undeclared capability identically across runtimes through the shared gate", func() {
		manifest := undeclaredManifest()
		binaryEP := gatedBinaryEndpoint(manifest)
		luaEP := gatedLuaEndpoint(manifest)

		binaryKV := hostv1.NewKVServiceClient(binaryEP.conn)
		luaKV := hostv1.NewKVServiceClient(luaEP.conn)

		_, binErr := binaryKV.Get(context.Background(), &hostv1.GetRequest{Key: "least-privilege"})
		_, luaErr := luaKV.Get(context.Background(), &hostv1.GetRequest{Key: "least-privilege"})

		Expect(binErr).To(HaveOccurred(),
			"binary runtime MUST deny an undeclared kv capability at the shared gate")
		Expect(luaErr).To(HaveOccurred(),
			"lua runtime MUST deny an undeclared kv capability at the shared gate")

		binCode := status.Code(binErr)
		luaCode := status.Code(luaErr)

		// IDENTICAL denial: same code on both runtimes, and the codes are EQUAL.
		Expect(binCode).To(Equal(luaCode),
			"both runtimes MUST deny the undeclared capability with the IDENTICAL wire code — one shared gate, no per-runtime divergence (INV-PLUGIN-45)")
		Expect(binCode).To(Equal(codes.PermissionDenied),
			"the shared declaration gate's CAPABILITY_NOT_DECLARED denial surfaces as codes.PermissionDenied on the wire (holomush-yc05l)")

		// IDENTICAL denial reason: the shared gate's CAPABILITY_NOT_DECLARED text.
		Expect(binErr.Error()).To(ContainSubstring(declaredDenialMessage),
			"binary denial MUST be the shared gate's CAPABILITY_NOT_DECLARED reason")
		Expect(luaErr.Error()).To(ContainSubstring(declaredDenialMessage),
			"lua denial MUST be the shared gate's CAPABILITY_NOT_DECLARED reason")

		// NOT the base: an Unimplemented here would mean the gate was bypassed and
		// KVService's Unimplemented base answered instead of the declaration gate.
		Expect(binCode).NotTo(Equal(codes.Unimplemented),
			"the GATE must deny, not the Unimplemented base — Unimplemented would mean the gate was bypassed")
		Expect(luaCode).NotTo(Equal(codes.Unimplemented),
			"the GATE must deny, not the Unimplemented base — Unimplemented would mean the gate was bypassed")
	})

	// Contrast / positive half: with a manifest that DECLARES the "kv" capability
	// — built through the SAME DeclaredAccessFromManifest constructor — the
	// declaration gate PASSES on both runtimes and the SAME kv.Get call reaches
	// the shared Unimplemented kvServer base. Both runtimes return
	// codes.Unimplemented, identically.
	//
	// This is what makes the undeclared assertion above non-vacuous: it proves the
	// undeclared denial (PermissionDenied / "plugin did not declare capability") was the
	// GATE firing, because the only thing that changed is the manifest's
	// declaration — flip it on, and the gate steps aside and the base answers.
	//
	// Verifies: INV-PLUGIN-45
	It("admits a declared capability to the shared base identically across runtimes", func() {
		manifest := declaredKVManifest()
		binaryEP := gatedBinaryEndpoint(manifest)
		luaEP := gatedLuaEndpoint(manifest)

		binaryKV := hostv1.NewKVServiceClient(binaryEP.conn)
		luaKV := hostv1.NewKVServiceClient(luaEP.conn)

		_, binErr := binaryKV.Get(context.Background(), &hostv1.GetRequest{Key: "least-privilege"})
		_, luaErr := luaKV.Get(context.Background(), &hostv1.GetRequest{Key: "least-privilege"})

		Expect(binErr).To(HaveOccurred(), "declared kv.Get reaches the Unimplemented base, which errors")
		Expect(luaErr).To(HaveOccurred(), "declared kv.Get reaches the Unimplemented base, which errors")

		binCode := status.Code(binErr)
		luaCode := status.Code(luaErr)

		Expect(binCode).To(Equal(luaCode),
			"both runtimes reach the IDENTICAL shared base when the capability is declared")
		Expect(binCode).To(Equal(codes.Unimplemented),
			"a DECLARED kv call passes the gate and reaches the Unimplemented base — proving the undeclared denial was the GATE, not the base")

		// The denial reason MUST differ from the undeclared case: passing the gate
		// means the CAPABILITY_NOT_DECLARED text is absent on both runtimes.
		Expect(binErr.Error()).NotTo(ContainSubstring(declaredDenialMessage),
			"a declared capability MUST NOT be denied by the declaration gate")
		Expect(luaErr.Error()).NotTo(ContainSubstring(declaredDenialMessage),
			"a declared capability MUST NOT be denied by the declaration gate")
	})
})
