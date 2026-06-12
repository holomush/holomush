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

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/goplugin"
	"github.com/holomush/holomush/internal/plugin/hostcap"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/plugin/lua"
	"github.com/holomush/holomush/internal/session"
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
	return newBinaryEndpointWithOpts(t)
}

// newBinaryEndpointWithOpts is newBinaryEndpoint with explicit goplugin host
// options (e.g. goplugin.WithEngine), so a test can wire the binary runtime's
// ABAC engine through the SAME hostcap.RegisterCapabilities source.
func newBinaryEndpointWithOpts(t *testing.T, opts ...goplugin.HostOption) runtimeEndpoint {
	t.Helper()

	host := goplugin.NewHost(opts...)
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
	return newLuaEndpointWithFunctions(t, hostfunc.New(nil))
}

// newLuaEndpointWithFunctions is newLuaEndpoint with an explicit
// *hostfunc.Functions, so a test can wire backings (a session.Access, an ABAC
// engine) through the SAME real luaHostCapAdapter the production LuaDefaultSet
// endpoint consumes.
func newLuaEndpointWithFunctions(t *testing.T, hf *hostfunc.Functions) runtimeEndpoint {
	t.Helper()

	luaHost := lua.NewHostWithFunctions(hf)
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

// emptySessionAccess is a minimal session.Access whose ListActive succeeds with
// an empty result. It gives the Lua SessionService a real backing so a DECLARED
// (LuaDefaultSet) service can be shown reachable — the positive half of the
// declaration-gate contrast (a service the binary set never declares).
type emptySessionAccess struct{}

var _ session.Access = (*emptySessionAccess)(nil)

func (emptySessionAccess) ListActive(context.Context) ([]*session.Info, error) { return nil, nil }
func (emptySessionAccess) FindByCharacter(context.Context, ulid.ULID) (*session.Info, error) {
	return nil, nil
}

func (emptySessionAccess) FindByCharacterName(context.Context, string) (*session.Info, error) {
	return nil, nil
}

func (emptySessionAccess) DeleteByCharacter(context.Context, ulid.ULID) (*session.Info, error) {
	return nil, nil
}
func (emptySessionAccess) UpdateActivity(context.Context, string) error          { return nil }
func (emptySessionAccess) UpdateLastPaged(context.Context, string, string) error { return nil }
func (emptySessionAccess) UpdateLastWhispered(context.Context, string, string) error {
	return nil
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

// sessionServiceName is the fully-qualified host.v1 SessionService name. It is
// declared in LuaDefaultSet but NOT in BinaryDefaultSet (register.go:53-58), so
// it is the canonical witness that the declaration gate (the CapabilitySet
// passed to the SHARED hostcap.RegisterCapabilities) governs which services a
// runtime reaches.
const sessionServiceName = "holomush.plugin.host.v1.SessionService"

// TestDeclarationGateGovernsServiceReachabilityAcrossRuntimes binds
// INV-PLUGIN-44.
//
// The witness is the per-runtime CapabilitySet argument to
// hostcap.RegisterCapabilities (register.go:42): SessionService is declared in
// LuaDefaultSet but NOT in BinaryDefaultSet, so it shows the declaration
// controls which services a runtime reaches:
//
//   - DECLARED ⇒ REACHABLE (INV-PLUGIN-44 positive): the Lua endpoint, whose
//     LuaDefaultSet declares SessionService, serves a real SessionService.ListActive
//     round-trip (the wired emptySessionAccess backs it). The call SUCCEEDS — the
//     declared dependency is obtained through the host broker.
//   - UNDECLARED ⇒ NOT REACHABLE (INV-PLUGIN-44 negative + the least-privilege
//     gate): the binary endpoint, whose BinaryDefaultSet does NOT declare
//     SessionService, has no handler registered for it. The identical ListActive
//     call returns codes.Unimplemented — the binary runtime CANNOT obtain a
//     service it did not declare. "Neither runtime MAY receive an undeclared
//     capability or service."
//
// Non-vacuous: if the gate were bypassed (e.g. RegisterCapabilities ignored the
// set and always registered every service), the binary call would SUCCEED and
// the Unimplemented assertion would fail. If the declared service were not
// actually wired, the Lua call would error and the success assertion would fail.
//
// NOTE: this does NOT bind INV-PLUGIN-45 ("the declaration gate that enforces
// least privilege MUST live at the broker/registry common path shared by both
// runtimes"). Today the per-plugin least-privilege declaration gate is still
// SPLIT per-runtime (Lua: luabridge.RegisterHostCaps VM injection; binary:
// manifest.RequiredServiceNames + dependency.go broker resolution). Its
// consolidation onto a single brokered path is deferred to sub-spec 5 of
// docs/superpowers/specs/2026-06-11-plugin-capability-dependency-foundation-design.md,
// so INV-PLUGIN-45 remains binding: pending until then.
//
// Verifies: INV-PLUGIN-44
func TestDeclarationGateGovernsServiceReachabilityAcrossRuntimes(t *testing.T) {
	// Lua endpoint declares SessionService (LuaDefaultSet) and has it backed.
	luaEP := newLuaEndpointWithFunctions(t,
		hostfunc.New(nil, hostfunc.WithSessionAccess(emptySessionAccess{})))
	// Binary endpoint does NOT declare SessionService (BinaryDefaultSet).
	binaryEP := newBinaryEndpoint(t)

	// Structural witness of the declaration gate: the SAME RegisterCapabilities
	// source registered SessionService on the Lua server but not the binary one —
	// the only difference is the declared CapabilitySet.
	require.Contains(t, luaEP.srv.GetServiceInfo(), sessionServiceName,
		"LuaDefaultSet declares SessionService — it MUST be registered")
	require.NotContains(t, binaryEP.srv.GetServiceInfo(), sessionServiceName,
		"BinaryDefaultSet does NOT declare SessionService — it MUST NOT be registered (least privilege)")

	// DECLARED ⇒ REACHABLE: the Lua runtime obtains the declared SessionService
	// through the host broker and the round-trip succeeds.
	luaSession := hostv1.NewSessionServiceClient(luaEP.conn)
	resp, err := luaSession.ListActive(context.Background(), &hostv1.ListActiveRequest{})
	require.NoError(t, err,
		"a DECLARED service (LuaDefaultSet) MUST be reachable through the host broker")
	require.NotNil(t, resp)
	assert.Empty(t, resp.GetSessions(), "emptySessionAccess backs ListActive with no sessions")

	// UNDECLARED ⇒ NOT REACHABLE: the binary runtime did NOT declare
	// SessionService, so the SAME call has no handler and is denied by the gate.
	binarySession := hostv1.NewSessionServiceClient(binaryEP.conn)
	_, binErr := binarySession.ListActive(context.Background(), &hostv1.ListActiveRequest{})
	require.Error(t, binErr,
		"an UNDECLARED service MUST NOT be reachable from the runtime that did not declare it")
	assert.Equal(t, codes.Unimplemented, status.Code(binErr),
		"the declaration gate denies an undeclared service with Unimplemented (no handler registered)")
}

// TestEvaluateAuthorizationSeamFailsClosedAcrossRuntimes binds INV-PLUGIN-44.
//
// INV-PLUGIN-44 requires every declared dependency be "authorized as
// PluginSubject" — and that neither runtime receive an unauthorized result. The
// EvalService.Evaluate handler is the consumption-path ABAC authorization seam:
// it is declared in BOTH sets (reachable on both runtimes) and runs the host
// ABAC engine for the calling plugin's subject. Its security contract is
// fail-closed: it recovers the acting PluginSubject from host-established
// identity (binary: a host-issued dispatch token in metadata; Lua:
// core.ActorFromContext) BEFORE consulting the engine, and denies when no
// host-vouched identity is present.
//
// Over the in-process transport neither runtime carries a host-established
// identity (no dispatch token is minted; the client context's actor does not
// cross the gRPC boundary onto the server handler context), so the shared
// authorization seam MUST fail closed on BOTH — proving the plugin cannot obtain
// an allow it was not authorized for. Both endpoints additionally wire a
// DenyAllEngine, so even past the identity seam the engine would deny: there is
// no code path on either runtime that yields an allow.
//
// This is the consumption-path facet of plugin-runtime-symmetry: the SAME
// hostcap.evalServer body authorizes both runtimes; neither gets an
// unauthorized capability result. Non-vacuous: if either runtime returned an
// allow (e.g. trusting a plugin-supplied subject, or skipping the seam), the
// require.Error would fail.
//
// SCOPE / COVERAGE GAP: this asserts only fail-closed-when-identity-absent (the
// transport-identity gap). It does NOT prove the engine authorizes a PRESENT
// PluginSubject — INV-PLUGIN-44's "authorized as PluginSubject" clause is bound
// by the declared-reachable / undeclared-not-reachable contrast in
// TestDeclarationGateGovernsServiceReachabilityAcrossRuntimes, not by this
// assertion. Full PluginSubject-authorization coverage (the engine authorizes a
// present, host-vouched subject) is a tracked coverage gap pending the Lua
// identity-transport wiring; this test supports the fail-closed half only.
//
// Verifies: INV-PLUGIN-44
func TestEvaluateAuthorizationSeamFailsClosedAcrossRuntimes(t *testing.T) {
	// Both runtimes wire a DenyAllEngine through the SAME RegisterCapabilities
	// source, so the engine itself never yields an allow either.
	binaryEP := newBinaryEndpointWithOpts(t, goplugin.WithEngine(policytest.DenyAllEngine()))
	luaEP := newLuaEndpointWithFunctions(t,
		hostfunc.New(nil, hostfunc.WithEngine(policytest.DenyAllEngine())))

	// EvalService is declared in BOTH sets — reachable on both runtimes (this is
	// the "declared dependency obtained through the broker" precondition).
	require.Contains(t, binaryEP.srv.GetServiceInfo(), "holomush.plugin.host.v1.EvalService",
		"EvalService is declared in BinaryDefaultSet")
	require.Contains(t, luaEP.srv.GetServiceInfo(), "holomush.plugin.host.v1.EvalService",
		"EvalService is declared in LuaDefaultSet")

	req := &hostv1.EvaluateRequest{Action: "spectate", Resource: "scene:" + ulid.Make().String()}

	// Binary: no host-issued dispatch token on the call ⇒ the authorization seam
	// fails closed before any allow can be produced.
	binResp, binErr := hostv1.NewEvalServiceClient(binaryEP.conn).Evaluate(context.Background(), req)
	require.Error(t, binErr,
		"binary Evaluate MUST fail closed without a host-issued PluginSubject identity")
	assert.Nil(t, binResp, "no allow is produced for an unauthorized binary caller")

	// Lua: no actor on the server-side context ⇒ the SAME seam fails closed.
	luaResp, luaErr := hostv1.NewEvalServiceClient(luaEP.conn).Evaluate(context.Background(), req)
	require.Error(t, luaErr,
		"lua Evaluate MUST fail closed without a host-established PluginSubject identity")
	assert.Nil(t, luaResp, "no allow is produced for an unauthorized lua caller")
}
