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

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// forbiddenKVResource is the type-level capability resource the interceptor
// evaluates for a NON-scoped kv method: descriptor Resource "kv" + ":*" (the
// non-scoped instance-less form, interceptor.go:199). The operator-forbid engine
// below denies exactly this resource.
const forbiddenKVResource = "kv:*"

// policyDenialMessage is the interceptor's denial text when the dynamic ABAC gate
// forbids a DECLARED non-scoped capability (interceptor.go: capDeny(
// "CAPABILITY_ACCESS_DENIED", "denied by policy", …)). capDeny wraps a gRPC
// status, so grpc-go surfaces it as codes.PermissionDenied with this message
// preserved (holomush-yc05l) — the same wire shape the declaration gate uses,
// which is why the denial reason (not just the code) is the discriminator that
// proves the ABAC gate fired rather than the declaration gate.
const policyDenialMessage = "denied by policy"

// operatorForbidEngine models an operator policy that FORBIDS one specific
// non-scoped capability resource type while permitting everything else — exactly
// the M1 scenario "a default-permit seed overridden by an operator forbid"
// (spec §M1; INV-PLUGIN-50). It is a purpose-built test engine in the sanctioned
// ownLocationEngine idiom (hostcap/interceptor_test.go): it asserts the dynamic
// half end-to-end across the wire without standing up a full DSL-backed engine,
// whose seed DSL is verified separately by the policy package's seed smoke tests.
//
// EffectDeny (not EffectDefaultDeny) is deliberate: an operator forbid is an
// EXPLICIT deny overriding the default-permit, which DenyAllEngine's blanket deny
// would not distinguish from "nothing permits".
type operatorForbidEngine struct {
	forbidResource string
}

// Evaluate forbids the configured resource and default-permits all others.
func (e operatorForbidEngine) Evaluate(_ context.Context, req types.AccessRequest) (types.Decision, error) {
	if req.Resource == e.forbidResource {
		return types.NewDecision(types.EffectDeny, "operator forbid", "test:operator-forbid"), nil
	}
	return types.NewDecision(types.EffectAllow, "default-permit seed", "test:default-permit"), nil
}

// CanPerformAction is not exercised by the capability interceptor (it uses
// Evaluate); it satisfies the AccessPolicyEngine interface permissively.
func (operatorForbidEngine) CanPerformAction(_ context.Context, _, _, _, _ string) (bool, error) {
	return true, nil
}

var _ = Describe("Cross-runtime least-privilege ABAC denial (non-scoped capability)", func() {
	// INV-PLUGIN-50: "A plugin's consumption of a host capability ... MUST be
	// authorized by a default-deny ABAC decision keyed on the host-stamped
	// plugin:<name> subject. Manifest declaration is necessary but not sufficient;
	// an operator policy MAY deny a declared capability."
	//
	// The declaration-gate parity spec (least_privilege_gate_test.go) covers the
	// STATIC half — a capability denied for being UNDECLARED. This spec covers the
	// DYNAMIC half at the same cross-runtime tier: a capability that IS declared
	// (the declaration gate passes) but is forbidden by operator policy. Both
	// runtimes wire the SAME operatorForbidEngine into the SAME shared
	// NewCapabilityInterceptor, so an identical denial is the structural witness
	// that one shared ABAC gate — not two per-runtime gates that happen to agree —
	// denies both (the parity dimension of the spec's M1 testing strategy).
	//
	// kv is the subject capability: its KVService is registered in BOTH runtime
	// sets (register.go) so kv.Get ROUTES to a handler on both runtimes, and all
	// its methods are NON-scoped (descriptor.go: empty Scopes), so the interceptor
	// evaluates the type-level resource "kv:*" with no dispatch context required.
	//
	// Verifies: INV-PLUGIN-50
	It("denies a declared but operator-forbidden non-scoped capability identically across runtimes", func() {
		manifest := declaredKVManifest() // declares kv → declaration gate PASSES
		engine := operatorForbidEngine{forbidResource: forbiddenKVResource}

		binaryEP := gatedBinaryEndpointWithEngine(manifest, engine)
		luaEP := gatedLuaEndpointWithEngine(manifest, engine)

		binaryKV := hostv1.NewKVServiceClient(binaryEP.conn)
		luaKV := hostv1.NewKVServiceClient(luaEP.conn)

		_, binErr := binaryKV.Get(context.Background(), &hostv1.GetRequest{Key: "least-privilege"})
		_, luaErr := luaKV.Get(context.Background(), &hostv1.GetRequest{Key: "least-privilege"})

		Expect(binErr).To(HaveOccurred(),
			"binary runtime MUST deny a declared kv capability forbidden by operator policy")
		Expect(luaErr).To(HaveOccurred(),
			"lua runtime MUST deny a declared kv capability forbidden by operator policy")

		binCode := status.Code(binErr)
		luaCode := status.Code(luaErr)

		// IDENTICAL denial: same wire code on both runtimes.
		Expect(binCode).To(Equal(luaCode),
			"both runtimes MUST deny the forbidden capability with the IDENTICAL wire code — one shared ABAC gate, no per-runtime divergence (INV-PLUGIN-50)")
		Expect(binCode).To(Equal(codes.PermissionDenied),
			"the shared ABAC gate's CAPABILITY_ACCESS_DENIED denial surfaces as codes.PermissionDenied on the wire (holomush-yc05l)")

		// IDENTICAL denial reason: the dynamic gate's "denied by policy" text.
		Expect(binErr.Error()).To(ContainSubstring(policyDenialMessage),
			"binary denial MUST be the ABAC gate's CAPABILITY_ACCESS_DENIED reason")
		Expect(luaErr.Error()).To(ContainSubstring(policyDenialMessage),
			"lua denial MUST be the ABAC gate's CAPABILITY_ACCESS_DENIED reason")

		// NOT the declaration gate: the manifest DECLARES kv, so a
		// "plugin did not declare capability" denial would mean the wrong gate
		// fired and this spec would not prove the ABAC (dynamic) half.
		Expect(binErr.Error()).NotTo(ContainSubstring(declaredDenialMessage),
			"the ABAC gate must deny, not the declaration gate — kv IS declared")
		Expect(luaErr.Error()).NotTo(ContainSubstring(declaredDenialMessage),
			"the ABAC gate must deny, not the declaration gate — kv IS declared")

		// NOT the base: an Unimplemented here would mean the gate was bypassed and
		// KVService's Unimplemented base answered instead of the ABAC gate.
		Expect(binCode).NotTo(Equal(codes.Unimplemented),
			"the GATE must deny, not the Unimplemented base — Unimplemented would mean the gate was bypassed")
		Expect(luaCode).NotTo(Equal(codes.Unimplemented),
			"the GATE must deny, not the Unimplemented base — Unimplemented would mean the gate was bypassed")
	})

	// Contrast / positive half: with the SAME declared-kv manifest but a policy
	// that PERMITS the capability, the ABAC gate passes on both runtimes and the
	// SAME kv.Get reaches the shared Unimplemented kvServer base, identically.
	// AllowAllEngine stands in for the default-permit seed.
	//
	// This is what makes the denial assertion above non-vacuous: it proves the
	// manifest's kv declaration is genuine (otherwise the permitted call would
	// still be denied by the declaration gate, not reach the base), AND that the
	// denial above was the OPERATOR POLICY firing — flip the policy to permit and
	// the gate steps aside and the base answers.
	//
	// Verifies: INV-PLUGIN-50
	It("admits the same declared non-scoped capability to the shared base when policy permits, identically across runtimes", func() {
		manifest := declaredKVManifest()

		binaryEP := gatedBinaryEndpointWithEngine(manifest, policytest.AllowAllEngine())
		luaEP := gatedLuaEndpointWithEngine(manifest, policytest.AllowAllEngine())

		binaryKV := hostv1.NewKVServiceClient(binaryEP.conn)
		luaKV := hostv1.NewKVServiceClient(luaEP.conn)

		_, binErr := binaryKV.Get(context.Background(), &hostv1.GetRequest{Key: "least-privilege"})
		_, luaErr := luaKV.Get(context.Background(), &hostv1.GetRequest{Key: "least-privilege"})

		Expect(binErr).To(HaveOccurred(), "permitted kv.Get reaches the Unimplemented base, which errors")
		Expect(luaErr).To(HaveOccurred(), "permitted kv.Get reaches the Unimplemented base, which errors")

		binCode := status.Code(binErr)
		luaCode := status.Code(luaErr)

		Expect(binCode).To(Equal(luaCode),
			"both runtimes reach the IDENTICAL shared base when the capability is permitted")
		Expect(binCode).To(Equal(codes.Unimplemented),
			"a PERMITTED kv call passes the ABAC gate and reaches the Unimplemented base — proving the denial above was the operator policy, not the gate-bypass or declaration gate")

		// Passing the ABAC gate means neither denial reason is present on either runtime.
		Expect(binErr.Error()).NotTo(ContainSubstring(policyDenialMessage),
			"a permitted capability MUST NOT be denied by the ABAC gate")
		Expect(luaErr.Error()).NotTo(ContainSubstring(policyDenialMessage),
			"a permitted capability MUST NOT be denied by the ABAC gate")
	})
})
