// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package pluginparity

import (
	"context"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/goplugin"
	"github.com/holomush/holomush/internal/plugin/hostcap"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/plugin/lua"
)

// Shared gated-endpoint builders for the least-privilege parity specs. Both
// runtimes stand up their host.v1 capability surface WITH the production
// capability interceptor chained on, built from manifest via the SAME
// hostcap.DeclaredAccessFromManifest constructor the production install sites
// use (binary: goplugin/host_service.go:50; Lua: lua/bufconn_endpoint.go:44).
// The non-gated parity endpoints in parity_test.go register servers WITHOUT the
// interceptor and prove ROUTING only; these prove GATING.
//
// The interceptor's Engine is parameterized so a spec can drive either the
// declaration gate (engine never reached — declaration fails first) or the
// dynamic ABAC gate (a real engine that an operator policy forbids). The SAME
// engine instance is wired into both runtimes' endpoints so a parity spec proves
// one shared gate denies both identically (INV-PLUGIN-45/50), never two
// per-runtime gates that happen to agree.

// gatedBinaryEndpointWithEngine stands up the binary runtime's gated capability
// surface with the given ABAC engine wired into BOTH the host and the
// capability interceptor.
func gatedBinaryEndpointWithEngine(manifest *plugins.Manifest, engine types.AccessPolicyEngine) runtimeEndpoint {
	GinkgoHelper()

	host := goplugin.NewHost(goplugin.WithEngine(engine))
	DeferCleanup(func() { _ = host.Close(context.Background()) })

	ic := hostcap.NewCapabilityInterceptor(hostcap.InterceptorDeps{
		Engine:         engine,
		PluginName:     manifest.Name,
		DeclaredAccess: hostcap.DeclaredAccessFromManifest(manifest),
	})
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(ic))
	hostcap.RegisterCapabilities(srv, hostcap.NewBase(host, manifest.Name), hostcap.BinaryDefaultSet)

	conn, err := plugins.NewInProcessConn(srv)
	Expect(err).NotTo(HaveOccurred(), "binary gated in-process conn must stand up")
	DeferCleanup(func() { _ = conn.Close() })

	return runtimeEndpoint{srv: srv, conn: conn}
}

// gatedLuaEndpointWithEngine stands up the Lua runtime's gated capability
// surface with the given ABAC engine wired into BOTH the host and the
// capability interceptor.
func gatedLuaEndpointWithEngine(manifest *plugins.Manifest, engine types.AccessPolicyEngine) runtimeEndpoint {
	GinkgoHelper()

	luaHost := lua.NewHostWithFunctions(hostfunc.New(nil, hostfunc.WithEngine(engine)))
	DeferCleanup(func() { _ = luaHost.Close(context.Background()) })

	adapter := luaHost.HostCapabilitiesAdapter()
	Expect(adapter).NotTo(BeNil(), "lua host must expose its real hostcap adapter")

	ic := hostcap.NewCapabilityInterceptor(hostcap.InterceptorDeps{
		Engine:         engine,
		PluginName:     manifest.Name,
		DeclaredAccess: hostcap.DeclaredAccessFromManifest(manifest),
	})
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(ic))
	hostcap.RegisterCapabilities(srv, hostcap.NewBase(adapter, manifest.Name), hostcap.LuaDefaultSet)

	conn, err := plugins.NewInProcessConn(srv)
	Expect(err).NotTo(HaveOccurred(), "lua gated in-process conn must stand up")
	DeferCleanup(func() { _ = conn.Close() })

	return runtimeEndpoint{srv: srv, conn: conn}
}

// gatedBinaryEndpoint is the AllowAll-engine convenience used by the
// declaration-gate specs, which deny BEFORE the engine is consulted.
func gatedBinaryEndpoint(manifest *plugins.Manifest) runtimeEndpoint {
	GinkgoHelper()
	return gatedBinaryEndpointWithEngine(manifest, policytest.AllowAllEngine())
}

// gatedLuaEndpoint is the AllowAll-engine convenience used by the
// declaration-gate specs, which deny BEFORE the engine is consulted.
func gatedLuaEndpoint(manifest *plugins.Manifest) runtimeEndpoint {
	GinkgoHelper()
	return gatedLuaEndpointWithEngine(manifest, policytest.AllowAllEngine())
}
