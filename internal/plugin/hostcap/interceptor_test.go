// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/pkg/errutil"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

func okHandler(_ context.Context, _ any) (any, error) { return struct{}{}, nil }

// ctxWithDispatch returns a ctx carrying a host-vouched dispatch context. The
// static half does not read it; the dynamic (M3) scope half reads dc.Subject
// and dc.Attributes["location"] to build the scope attributes.
func ctxWithDispatch(t *testing.T) context.Context {
	t.Helper()
	return pluginauthz.WithDispatch(context.Background(), pluginauthz.DispatchContext{Subject: "character:01TEST"})
}

// ownLocationEngine is a test AccessPolicyEngine that models the own-location
// scope condition the way the seed policy does: it permits a plugin write to
// "location:<id>" only when the caller-overlaid action attribute
// "dispatch_location" equals <id>. It lets the interceptor unit tests assert the
// dynamic half end-to-end (extract resource → build scope attrs from dispatch →
// EvaluateCapabilityAccess → deny on mismatch) without standing up a full
// DSL-backed engine; the seed DSL itself is verified by the seed smoke tests in
// the policy package (TestSeedSmokePluginWorldMutationOwnLocation*).
type ownLocationEngine struct{}

func (ownLocationEngine) Evaluate(_ context.Context, req types.AccessRequest) (types.Decision, error) {
	resType, resID, ok := splitTypeID(req.Resource)
	if !ok || resType != "location" || req.Action != "write" {
		return types.NewDecision(types.EffectDefaultDeny, "no policy", ""), nil
	}
	dispatchLoc, _ := req.Attributes["dispatch_location"].(string)
	if dispatchLoc != "" && dispatchLoc == resID {
		return types.NewDecision(types.EffectAllow, "own-location", "test:own-location"), nil
	}
	return types.NewDecision(types.EffectDefaultDeny, "not own-location", ""), nil
}

func (ownLocationEngine) CanPerformAction(_ context.Context, _, _, _, _ string) (bool, error) {
	return true, nil
}

func splitTypeID(ref string) (typ, id string, ok bool) {
	for i := 0; i < len(ref); i++ {
		if ref[i] == ':' {
			if i == 0 || i == len(ref)-1 {
				return "", "", false
			}
			return ref[:i], ref[i+1:], true
		}
	}
	return "", "", false
}

const testLocID = "01LOCAAAAAAAAAAAAAAAAAAAAAA"

// scopedDispatchCtx carries a dispatch context whose acting-character location
// is loc, mirroring what eykuh.3.15 populates at delivery time.
func scopedDispatchCtx(loc string) context.Context {
	return pluginauthz.WithDispatch(context.Background(), pluginauthz.DispatchContext{
		Subject:    "character:01TEST",
		Attributes: map[string]string{"location": loc},
	})
}

// createExitInLocation builds a CreateExit gRPC call whose source location is
// fromID; the descriptor's Extract pulls from_id as the scoped resource.
func createExitInfo() *grpc.UnaryServerInfo {
	return &grpc.UnaryServerInfo{FullMethod: "/holomush.plugin.host.v1.WorldMutationService/CreateExit"}
}

// Verifies: INV-PLUGIN-50
func TestInterceptorScopeOwnLocationPermitsMatch(t *testing.T) {
	ic := NewCapabilityInterceptor(InterceptorDeps{
		Engine:         ownLocationEngine{},
		PluginName:     "builder-bot",
		DeclaredAccess: func(_, _ string) (string, bool) { return "write", true },
	})
	resp, err := ic(scopedDispatchCtx(testLocID), &hostv1.CreateExitRequest{FromId: testLocID},
		createExitInfo(), okHandler)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// Verifies: INV-PLUGIN-50
func TestInterceptorScopeOwnLocationDeniesMismatch(t *testing.T) {
	ic := NewCapabilityInterceptor(InterceptorDeps{
		Engine:         ownLocationEngine{},
		PluginName:     "builder-bot",
		DeclaredAccess: func(_, _ string) (string, bool) { return "write", true },
	})
	// Dispatch location differs from the exit's source location => own-location fails.
	otherLoc := "01LOCBBBBBBBBBBBBBBBBBBBBBB"
	_, err := ic(scopedDispatchCtx(otherLoc), &hostv1.CreateExitRequest{FromId: testLocID},
		createExitInfo(), okHandler)
	errutil.AssertErrorCode(t, err, "SCOPE_DENIED")
}

// Verifies: INV-PLUGIN-52
func TestInterceptorScopedCallFailsClosedWithEmptyResource(t *testing.T) {
	ic := NewCapabilityInterceptor(InterceptorDeps{
		Engine:         ownLocationEngine{},
		PluginName:     "builder-bot",
		DeclaredAccess: func(_, _ string) (string, bool) { return "write", true },
	})
	// CreateObject with a character placement: GetLocationId() returns "" => ok=false.
	// The interceptor must fail closed rather than forwarding without a scoped resource.
	_, err := ic(scopedDispatchCtx(testLocID),
		&hostv1.CreateObjectRequest{Placement: &hostv1.CreateObjectRequest_CharacterId{CharacterId: "01CHAR0000000000000000000A"}},
		&grpc.UnaryServerInfo{FullMethod: "/holomush.plugin.host.v1.WorldMutationService/CreateObject"},
		okHandler)
	errutil.AssertErrorCode(t, err, "SCOPE_NO_RESOURCE")
}

// Verifies: INV-PLUGIN-52
func TestInterceptorScopedCallFailsClosedWithoutDispatch(t *testing.T) {
	ic := NewCapabilityInterceptor(InterceptorDeps{
		Engine:         ownLocationEngine{},
		PluginName:     "builder-bot",
		DeclaredAccess: func(_, _ string) (string, bool) { return "write", true },
	})
	// No dispatch context on the ctx => a scoped capability call must fail closed.
	_, err := ic(context.Background(), &hostv1.CreateExitRequest{FromId: testLocID},
		createExitInfo(), okHandler)
	errutil.AssertErrorCode(t, err, "SCOPE_NO_DISPATCH")
}

// Verifies: INV-PLUGIN-50
func TestInterceptorNonScopedCapabilityDeniedByPolicy(t *testing.T) {
	// A declared, access-class-permitted, NON-scoped capability (kv read) must
	// still be subject to the default-deny ABAC decision: an operator policy that
	// denies it makes the call unreachable despite declaration. Before option A
	// this call passed through ungated (the len(Scopes)==0 short-circuit).
	ic := NewCapabilityInterceptor(InterceptorDeps{
		Engine:         policytest.DenyAllEngine(),
		PluginName:     "core-objects",
		DeclaredAccess: func(_, _ string) (string, bool) { return "read", true },
	})
	_, err := ic(ctxWithDispatch(t), &hostv1.GetRequest{}, &grpc.UnaryServerInfo{
		FullMethod: "/holomush.plugin.host.v1.KVService/Get",
	}, okHandler)
	errutil.AssertErrorCode(t, err, "CAPABILITY_ACCESS_DENIED")
}

// Verifies: INV-PLUGIN-50
func TestInterceptorNonScopedCapabilityPermittedByPolicy(t *testing.T) {
	// The complement: a declared non-scoped capability permitted by policy is
	// reachable. AllowAllEngine stands in for the default-permit seed.
	ic := NewCapabilityInterceptor(InterceptorDeps{
		Engine:         policytest.AllowAllEngine(),
		PluginName:     "core-objects",
		DeclaredAccess: func(_, _ string) (string, bool) { return "read", true },
	})
	resp, err := ic(ctxWithDispatch(t), &hostv1.GetRequest{}, &grpc.UnaryServerInfo{
		FullMethod: "/holomush.plugin.host.v1.KVService/Get",
	}, okHandler)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestInterceptorEmptyPluginNameFailsClosed(t *testing.T) {
	// Defense-in-depth: an empty PluginName (a misconfiguration — production
	// sources it from the schema-required manifest Name) must fail closed at the
	// ABAC gate rather than panic in access.PluginSubject. A declared, non-exempt
	// capability call reaches the guard.
	ic := NewCapabilityInterceptor(InterceptorDeps{
		Engine:         policytest.AllowAllEngine(),
		PluginName:     "", // misconfigured
		DeclaredAccess: func(_, _ string) (string, bool) { return "read", true },
	})
	_, err := ic(ctxWithDispatch(t), &hostv1.GetRequest{}, &grpc.UnaryServerInfo{
		FullMethod: "/holomush.plugin.host.v1.KVService/Get",
	}, okHandler)
	errutil.AssertErrorCode(t, err, "CAPABILITY_PLUGIN_NAME_MISSING")
}

func TestInterceptorNilEngineFailsClosedForNonScopedMethod(t *testing.T) {
	// Removing the non-scoped short-circuit means a non-scoped declared capability
	// now reaches EvaluateCapabilityAccess. With a nil Engine it must fail closed
	// (EVALUATE_NO_ENGINE), never forward to the handler. Production always wires a
	// real engine (cfg.ABAC.Engine()), so this guards a misconfiguration — symmetric
	// with the nil-DeclaredAccess and empty-PluginName guard tests.
	ic := NewCapabilityInterceptor(InterceptorDeps{
		Engine:         nil, // misconfigured
		PluginName:     "core-objects",
		DeclaredAccess: func(_, _ string) (string, bool) { return "read", true },
	})
	_, err := ic(ctxWithDispatch(t), &hostv1.GetRequest{}, &grpc.UnaryServerInfo{
		FullMethod: "/holomush.plugin.host.v1.KVService/Get",
	}, okHandler)
	errutil.AssertErrorCode(t, err, "EVALUATE_NO_ENGINE")
}

func TestInterceptorAccessReadDeniesWriteMethod(t *testing.T) {
	ic := NewCapabilityInterceptor(InterceptorDeps{
		Engine:         policytest.AllowAllEngine(),
		DeclaredAccess: func(_, _ string) (string, bool) { return "read", true },
	})
	_, err := ic(ctxWithDispatch(t), &hostv1.SetRequest{}, &grpc.UnaryServerInfo{
		FullMethod: "/holomush.plugin.host.v1.KVService/Set",
	}, okHandler)
	errutil.AssertErrorCode(t, err, "ACCESS_CLASS_DENIED")
}

func TestInterceptorAbsentDeclaredAccessPermitsWrite(t *testing.T) {
	ic := NewCapabilityInterceptor(InterceptorDeps{
		Engine:         policytest.AllowAllEngine(),
		PluginName:     "core-objects",
		DeclaredAccess: func(_, _ string) (string, bool) { return "", true }, // declared, no access narrowing
	})
	_, err := ic(ctxWithDispatch(t), &hostv1.SetRequest{}, &grpc.UnaryServerInfo{
		FullMethod: "/holomush.plugin.host.v1.KVService/Set",
	}, okHandler)
	require.NoError(t, err)
}

func TestInterceptorUndeclaredCapabilityDenied(t *testing.T) {
	ic := NewCapabilityInterceptor(InterceptorDeps{
		Engine:         policytest.AllowAllEngine(),
		DeclaredAccess: func(_, _ string) (string, bool) { return "", false }, // not declared
	})
	_, err := ic(ctxWithDispatch(t), &hostv1.GetRequest{}, &grpc.UnaryServerInfo{
		FullMethod: "/holomush.plugin.host.v1.KVService/Get",
	}, okHandler)
	errutil.AssertErrorCode(t, err, "CAPABILITY_NOT_DECLARED")
}

func TestInterceptorPassesThroughSelfGatedCapabilityWhenUndeclared(t *testing.T) {
	ic := NewCapabilityInterceptor(InterceptorDeps{
		Engine:         policytest.AllowAllEngine(),
		DeclaredAccess: func(_, _ string) (string, bool) { return "", false }, // NOT declared
	})
	// emit is self-gated: an undeclared emit call must pass through (not CAPABILITY_NOT_DECLARED).
	resp, err := ic(context.Background(), &hostv1.EmitEventRequest{}, &grpc.UnaryServerInfo{
		FullMethod: "/holomush.plugin.host.v1.EmitService/EmitEvent",
	}, okHandler)
	require.NoError(t, err)
	require.NotNil(t, resp)
	// command-registry likewise (ListCommands is ClassRead, undeclared).
	_, err = ic(context.Background(), &hostv1.ListCommandsRequest{}, &grpc.UnaryServerInfo{
		FullMethod: "/holomush.plugin.host.v1.CommandRegistryService/ListCommands",
	}, okHandler)
	require.NoError(t, err)
}

func TestInterceptorUnmappedHostMethodFailsClosed(t *testing.T) {
	// A host.v1 method whose service is not in the capability token map is
	// unclassifiable and MUST fail closed rather than forward ungated (gum03.5).
	// TestEveryServedCapabilityHasADescriptor structurally prevents the
	// descriptor-missing case for *served* services; this exercises the
	// interceptor denial itself for an unmapped host.v1 service.
	ic := NewCapabilityInterceptor(InterceptorDeps{
		Engine:         policytest.AllowAllEngine(),
		PluginName:     "core-objects",
		DeclaredAccess: func(_, _ string) (string, bool) { return "", true },
	})
	_, err := ic(context.Background(), struct{}{}, &grpc.UnaryServerInfo{
		FullMethod: "/holomush.plugin.host.v1.UnmappedService/DoThing",
	}, okHandler)
	errutil.AssertErrorCode(t, err, "UNCLASSIFIED_CAPABILITY_METHOD")
}

func TestInterceptorNonHostMethodPassesThrough(t *testing.T) {
	ic := NewCapabilityInterceptor(InterceptorDeps{
		Engine:         policytest.AllowAllEngine(),
		DeclaredAccess: func(_, _ string) (string, bool) { return "", false },
	})
	resp, err := ic(context.Background(), struct{}{}, &grpc.UnaryServerInfo{
		FullMethod: "/some.other.Service/Method",
	}, okHandler)
	require.NoError(t, err)
	require.NotNil(t, resp) // handler ran
}

func TestClassifyHostMethodResolvesKnownService(t *testing.T) {
	capToken, method, ok := classifyHostMethod("/holomush.plugin.host.v1.KVService/Set")
	require.True(t, ok)
	require.Equal(t, "kv", capToken)
	require.Equal(t, "Set", method)
}

func TestClassifyHostMethodRejectsNonHostMethod(t *testing.T) {
	_, _, ok := classifyHostMethod("/some.other.Service/Method")
	require.False(t, ok)
}

func TestDeclaredAccessFromManifestReportsDeclaredCapabilityAccess(t *testing.T) {
	m := &plugins.Manifest{
		Name: "p",
		Requires: []plugins.Dependency{
			{Kind: plugins.DependencyCapability, Name: "kv", Access: "read"},
			{Kind: plugins.DependencyCapability, Name: "world.query"}, // undifferentiated
			{Kind: plugins.DependencyService, Name: "holomush.scene.v1.SceneService"},
		},
	}
	lookup := DeclaredAccessFromManifest(m)

	access, declared := lookup("p", "kv")
	require.True(t, declared)
	require.Equal(t, "read", access)

	access, declared = lookup("p", "world.query")
	require.True(t, declared)
	require.Equal(t, "", access)
}

func TestDeclaredAccessFromManifestDeniesUndeclaredCapability(t *testing.T) {
	m := &plugins.Manifest{
		Name:     "p",
		Requires: []plugins.Dependency{{Kind: plugins.DependencyCapability, Name: "kv", Access: "write"}},
	}
	access, declared := DeclaredAccessFromManifest(m)("p", "session")
	require.False(t, declared)
	require.Equal(t, "", access)
}

func TestDeclaredAccessFromManifestIgnoresServiceKindEntries(t *testing.T) {
	m := &plugins.Manifest{
		Name:     "p",
		Requires: []plugins.Dependency{{Kind: plugins.DependencyService, Name: "kv"}},
	}
	// A service-kind entry named "kv" must NOT be treated as a declared capability.
	_, declared := DeclaredAccessFromManifest(m)("p", "kv")
	require.False(t, declared)
}

func TestDeclaredAccessFromManifestNilManifestDeniesAll(t *testing.T) {
	_, declared := DeclaredAccessFromManifest(nil)("p", "kv")
	require.False(t, declared)
}
