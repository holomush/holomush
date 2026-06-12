// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package pluginparity holds the cross-runtime parity tests that bind the
// plugin host-capability invariants. They stand up the SAME hostcap capability
// servers behind BOTH runtime adapters — the binary *goplugin.Host and the Lua
// *hostfunc.Functions-backed adapter — over the SAME in-process transport, and
// assert the two runtimes consume the single shared RPC contract identically.
package pluginparity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/goplugin"
	"github.com/holomush/holomush/internal/plugin/hostcap"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/plugin/lua"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// parityPluginName is the host-established calling-plugin identity baked into
// each runtime's hostcap base. It is the same value for both endpoints so the
// only registration-level difference is the CapabilitySet.
const parityPluginName = "parity-plugin"

// kvServiceName is the fully-qualified host.v1 KVService name. Both runtimes
// MUST register this same service — the structural witness of single-source
// routing.
const kvServiceName = "holomush.plugin.host.v1.KVService"

// runtimeEndpoint pairs a *grpc.Server (so the test can inspect its registered
// services) with an in-process client conn to that server. The server body, the
// registration source (hostcap.RegisterCapabilities), and the transport
// (plugins.NewInProcessConn) are identical across runtimes; only the
// CapabilitySet and the adapter baked into base differ.
type runtimeEndpoint struct {
	srv  *grpc.Server
	conn grpc.ClientConnInterface
}

// newBinaryEndpoint stands up the binary runtime's view of the host-capability
// surface: a *goplugin.Host registered via hostcap.RegisterCapabilities with the
// BinaryDefaultSet, reached over plugins.NewInProcessConn. This is the SAME
// construction the production binary host service uses (host_service.go).
func newBinaryEndpoint(t *testing.T) runtimeEndpoint {
	t.Helper()

	host := goplugin.NewHost()
	t.Cleanup(func() { _ = host.Close(context.Background()) })

	srv := grpc.NewServer()
	hostcap.RegisterCapabilities(srv, hostcap.NewBase(host, parityPluginName), hostcap.BinaryDefaultSet)

	conn, err := plugins.NewInProcessConn(srv)
	require.NoError(t, err, "binary in-process conn must stand up")
	t.Cleanup(func() { _ = conn.Close() })

	return runtimeEndpoint{srv: srv, conn: conn}
}

// newLuaEndpoint stands up the Lua runtime's view of the SAME host-capability
// surface: the REAL production luaHostCapAdapter (reached via the host's
// HostCapabilitiesAdapter() accessor) registered via the SAME
// hostcap.RegisterCapabilities source — differing ONLY by CapabilitySet
// (LuaDefaultSet) — and reached over the SAME plugins.NewInProcessConn
// transport.
func newLuaEndpoint(t *testing.T) runtimeEndpoint {
	t.Helper()

	luaHost := lua.NewHostWithFunctions(hostfunc.New(nil))
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	adapter := luaHost.HostCapabilitiesAdapter()
	require.NotNil(t, adapter, "lua host must expose its real hostcap adapter")

	srv := grpc.NewServer()
	hostcap.RegisterCapabilities(srv, hostcap.NewBase(adapter, parityPluginName), hostcap.LuaDefaultSet)

	conn, err := plugins.NewInProcessConn(srv)
	require.NoError(t, err, "lua in-process conn must stand up")
	t.Cleanup(func() { _ = conn.Close() })

	return runtimeEndpoint{srv: srv, conn: conn}
}

// TestKVCapabilityIsSingleSourceAcrossRuntimes binds INV-PLUGIN-49.
//
// It proves the host-capability RPC contract is the single source BOTH runtimes
// consume — there is no runtime-specific capability surface. Both the binary
// (*goplugin.Host) and Lua (*hostfunc.Functions-backed adapter) runtimes
// register the SAME hostcap.kvServer via the SAME hostcap.RegisterCapabilities
// source (differing ONLY by CapabilitySet — BinaryDefaultSet vs LuaDefaultSet)
// and reach it over the SAME plugins.NewInProcessConn transport.
//
// KVService is the canonical single-source witness: its kvServer is the
// Unimplemented base (no behavior of its own, no identity/token recovery), so a
// call returns codes.Unimplemented BEFORE any per-runtime trust seam runs. That
// is exactly why this capability binds the routing claim without scaffolding —
// no actor-on-context, no interceptor, no settings backing is needed for either
// runtime to reach the identical handler and get the identical result.
//
// Assertions:
//  1. ROUTING / IDENTICAL RESULT — both runtimes call the SAME RPC
//     (KVService.Get) over their respective in-process transports and receive
//     the IDENTICAL status code (codes.Unimplemented). The codes being equal is
//     the proof that both reach the single shared kvServer handler with no
//     runtime-specific surface in between.
//  2. SINGLE SOURCE (structural) — KVService is present in the GetServiceInfo()
//     service set of BOTH the binary and the Lua *grpc.Server. There is ONE
//     capability-server source (hostcap.RegisterCapabilities); both runtimes
//     registered the identical service, differing only by CapabilitySet.
//
// Verifies: INV-PLUGIN-49
func TestKVCapabilityIsSingleSourceAcrossRuntimes(t *testing.T) {
	binaryEP := newBinaryEndpoint(t)
	luaEP := newLuaEndpoint(t)

	binaryKV := hostv1.NewKVServiceClient(binaryEP.conn)
	luaKV := hostv1.NewKVServiceClient(luaEP.conn)

	// (1) ROUTING / IDENTICAL RESULT: both runtimes call the SAME KVService.Get
	// RPC over their own in-process transport. Because both reach the identical
	// shared kvServer (the Unimplemented base) through the single
	// RegisterCapabilities source, both return codes.Unimplemented — and the two
	// codes are EQUAL. No runtime-specific surface intervenes; if one runtime had
	// its own KV handler the codes could diverge.
	_, binErr := binaryKV.Get(context.Background(), &hostv1.GetRequest{Key: "parity"})
	_, luaErr := luaKV.Get(context.Background(), &hostv1.GetRequest{Key: "parity"})

	require.Error(t, binErr, "binary KVService.Get must return an error (Unimplemented base)")
	require.Error(t, luaErr, "lua KVService.Get must return an error (Unimplemented base)")

	binCode := status.Code(binErr)
	luaCode := status.Code(luaErr)
	assert.Equal(t, codes.Unimplemented, binCode,
		"binary runtime must reach the shared Unimplemented kvServer")
	assert.Equal(t, codes.Unimplemented, luaCode,
		"lua runtime must reach the shared Unimplemented kvServer")
	assert.Equal(t, binCode, luaCode,
		"both runtimes consume the IDENTICAL KVService contract — single-source routing, no runtime-specific surface")

	// (2) SINGLE SOURCE (structural): the SAME KVService is registered on BOTH
	// runtimes' *grpc.Server. There is one capability-server source
	// (hostcap.RegisterCapabilities); both endpoints registered the identical
	// service via it, differing only by CapabilitySet. GetServiceInfo is the
	// wire-level proof that the service set is shared, not duplicated per runtime.
	binInfo := binaryEP.srv.GetServiceInfo()
	luaInfo := luaEP.srv.GetServiceInfo()
	require.Contains(t, binInfo, kvServiceName,
		"binary runtime must register KVService via the single RegisterCapabilities source")
	require.Contains(t, luaInfo, kvServiceName,
		"lua runtime must register KVService via the single RegisterCapabilities source")
}
