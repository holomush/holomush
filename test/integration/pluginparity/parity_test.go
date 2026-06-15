// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package pluginparity

import (
	"context"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/core"
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

// sessionServiceName is the fully-qualified host.v1 SessionService name. It is
// declared in LuaDefaultSet but NOT in BinaryDefaultSet (register.go:53-58), so
// it is the canonical witness that the declaration gate (the CapabilitySet
// passed to the SHARED hostcap.RegisterCapabilities) governs which services a
// runtime reaches.
const sessionServiceName = "holomush.plugin.host.v1.SessionService"

// runtimeEndpoint pairs a *grpc.Server (so the spec can inspect its registered
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
func newBinaryEndpoint() runtimeEndpoint {
	GinkgoHelper()
	return newBinaryEndpointWithOpts()
}

// newBinaryEndpointWithOpts is newBinaryEndpoint with explicit goplugin host
// options (e.g. goplugin.WithEngine), so a spec can wire the binary runtime's
// ABAC engine through the SAME hostcap.RegisterCapabilities source. Per-spec
// resources tear down via DeferCleanup.
func newBinaryEndpointWithOpts(opts ...goplugin.HostOption) runtimeEndpoint {
	GinkgoHelper()

	host := goplugin.NewHost(opts...)
	DeferCleanup(func() { _ = host.Close(context.Background()) })

	srv := grpc.NewServer()
	hostcap.RegisterCapabilities(srv, hostcap.NewBase(host, parityPluginName), hostcap.BinaryDefaultSet)

	conn, err := plugins.NewInProcessConn(srv)
	Expect(err).NotTo(HaveOccurred(), "binary in-process conn must stand up")
	DeferCleanup(func() { _ = conn.Close() })

	return runtimeEndpoint{srv: srv, conn: conn}
}

// newLuaEndpoint stands up the Lua runtime's view of the SAME host-capability
// surface: the REAL production luaHostCapAdapter (reached via the host's
// HostCapabilitiesAdapter() accessor) registered via the SAME
// hostcap.RegisterCapabilities source — differing ONLY by CapabilitySet
// (LuaDefaultSet) — and reached over the SAME plugins.NewInProcessConn
// transport.
func newLuaEndpoint() runtimeEndpoint {
	GinkgoHelper()
	return newLuaEndpointWithFunctions(hostfunc.New(nil))
}

// newLuaEndpointWithFunctions is newLuaEndpoint with an explicit
// *hostfunc.Functions, so a spec can wire backings (a session.Access, an ABAC
// engine) through the SAME real luaHostCapAdapter the production LuaDefaultSet
// endpoint consumes. Per-spec resources tear down via DeferCleanup.
func newLuaEndpointWithFunctions(hf *hostfunc.Functions) runtimeEndpoint {
	GinkgoHelper()

	luaHost := lua.NewHostWithFunctions(hf)
	DeferCleanup(func() { _ = luaHost.Close(context.Background()) })

	adapter := luaHost.HostCapabilitiesAdapter()
	Expect(adapter).NotTo(BeNil(), "lua host must expose its real hostcap adapter")

	srv := grpc.NewServer()
	hostcap.RegisterCapabilities(srv, hostcap.NewBase(adapter, parityPluginName), hostcap.LuaDefaultSet)

	conn, err := plugins.NewInProcessConn(srv)
	Expect(err).NotTo(HaveOccurred(), "lua in-process conn must stand up")
	DeferCleanup(func() { _ = conn.Close() })

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

var _ = Describe("Cross-runtime plugin host-capability parity", func() {
	// It proves the host-capability RPC contract is the single source BOTH
	// runtimes consume — there is no runtime-specific capability surface. Both
	// the binary (*goplugin.Host) and Lua (*hostfunc.Functions-backed adapter)
	// runtimes register the SAME hostcap.kvServer via the SAME
	// hostcap.RegisterCapabilities source (differing ONLY by CapabilitySet —
	// BinaryDefaultSet vs LuaDefaultSet) and reach it over the SAME
	// plugins.NewInProcessConn transport.
	//
	// KVService is the canonical single-source witness: its kvServer is the
	// Unimplemented base (no behavior of its own, no identity/token recovery), so
	// a call returns codes.Unimplemented BEFORE any per-runtime trust seam runs.
	// That is exactly why this capability binds the routing claim without
	// scaffolding — no actor-on-context, no interceptor, no settings backing is
	// needed for either runtime to reach the identical handler and get the
	// identical result.
	//
	// Verifies: INV-PLUGIN-49
	It("consumes the KV capability from a single source across runtimes", func() {
		binaryEP := newBinaryEndpoint()
		luaEP := newLuaEndpoint()

		binaryKV := hostv1.NewKVServiceClient(binaryEP.conn)
		luaKV := hostv1.NewKVServiceClient(luaEP.conn)

		// (1) ROUTING / IDENTICAL RESULT: both runtimes call the SAME
		// KVService.Get RPC over their own in-process transport. Because both
		// reach the identical shared kvServer (the Unimplemented base) through the
		// single RegisterCapabilities source, both return codes.Unimplemented —
		// and the two codes are EQUAL. No runtime-specific surface intervenes; if
		// one runtime had its own KV handler the codes could diverge.
		_, binErr := binaryKV.Get(context.Background(), &hostv1.GetRequest{Key: "parity"})
		_, luaErr := luaKV.Get(context.Background(), &hostv1.GetRequest{Key: "parity"})

		Expect(binErr).To(HaveOccurred(), "binary KVService.Get must return an error (Unimplemented base)")
		Expect(luaErr).To(HaveOccurred(), "lua KVService.Get must return an error (Unimplemented base)")

		binCode := status.Code(binErr)
		luaCode := status.Code(luaErr)
		Expect(binCode).To(Equal(codes.Unimplemented),
			"binary runtime must reach the shared Unimplemented kvServer")
		Expect(luaCode).To(Equal(codes.Unimplemented),
			"lua runtime must reach the shared Unimplemented kvServer")
		Expect(binCode).To(Equal(luaCode),
			"both runtimes consume the IDENTICAL KVService contract — single-source routing, no runtime-specific surface")

		// (2) SINGLE SOURCE (structural): the SAME KVService is registered on BOTH
		// runtimes' *grpc.Server. There is one capability-server source
		// (hostcap.RegisterCapabilities); both endpoints registered the identical
		// service via it, differing only by CapabilitySet. GetServiceInfo is the
		// wire-level proof that the service set is shared, not duplicated per runtime.
		Expect(binaryEP.srv.GetServiceInfo()).To(HaveKey(kvServiceName),
			"binary runtime must register KVService via the single RegisterCapabilities source")
		Expect(luaEP.srv.GetServiceInfo()).To(HaveKey(kvServiceName),
			"lua runtime must register KVService via the single RegisterCapabilities source")
	})

	// The witness is the per-runtime CapabilitySet argument to
	// hostcap.RegisterCapabilities (register.go:42): SessionService is declared in
	// LuaDefaultSet but NOT in BinaryDefaultSet, so it shows the declaration
	// controls which services a runtime reaches:
	//
	//   - DECLARED ⇒ REACHABLE (INV-PLUGIN-44 positive): the Lua endpoint, whose
	//     LuaDefaultSet declares SessionService, serves a real
	//     SessionService.ListActive round-trip (the wired emptySessionAccess backs
	//     it). The call SUCCEEDS — the declared dependency is obtained through the
	//     host broker.
	//   - UNDECLARED ⇒ NOT REACHABLE (INV-PLUGIN-44 negative + the least-privilege
	//     gate): the binary endpoint, whose BinaryDefaultSet does NOT declare
	//     SessionService, has no handler registered for it. The identical
	//     ListActive call returns codes.Unimplemented — the binary runtime CANNOT
	//     obtain a service it did not declare.
	//
	// Non-vacuous: if the gate were bypassed (RegisterCapabilities ignored the set
	// and always registered every service), the binary call would SUCCEED and the
	// Unimplemented assertion would fail. If the declared service were not actually
	// wired, the Lua call would error and the success assertion would fail.
	//
	// NOTE: this does NOT bind INV-PLUGIN-45 ("the declaration gate that enforces
	// least privilege MUST live at the broker/registry common path shared by both
	// runtimes"). Today the per-plugin least-privilege declaration gate is still
	// SPLIT per-runtime; its consolidation onto a single brokered path is deferred
	// to sub-spec 5, so INV-PLUGIN-45 remains binding: pending until then.
	//
	// Verifies: INV-PLUGIN-44
	It("governs service reachability by the declaration gate across runtimes", func() {
		// Lua endpoint declares SessionService (LuaDefaultSet) and has it backed.
		luaEP := newLuaEndpointWithFunctions(
			hostfunc.New(nil, hostfunc.WithSessionAccess(emptySessionAccess{})),
		)
		// Binary endpoint does NOT declare SessionService (BinaryDefaultSet).
		binaryEP := newBinaryEndpoint()

		// Structural witness of the declaration gate: the SAME RegisterCapabilities
		// source registered SessionService on the Lua server but not the binary one
		// — the only difference is the declared CapabilitySet.
		Expect(luaEP.srv.GetServiceInfo()).To(HaveKey(sessionServiceName),
			"LuaDefaultSet declares SessionService — it MUST be registered")
		Expect(binaryEP.srv.GetServiceInfo()).NotTo(HaveKey(sessionServiceName),
			"BinaryDefaultSet does NOT declare SessionService — it MUST NOT be registered (least privilege)")

		// DECLARED ⇒ REACHABLE: the Lua runtime obtains the declared SessionService
		// through the host broker and the round-trip succeeds.
		luaSession := hostv1.NewSessionServiceClient(luaEP.conn)
		resp, err := luaSession.ListActive(context.Background(), &hostv1.ListActiveRequest{})
		Expect(err).NotTo(HaveOccurred(),
			"a DECLARED service (LuaDefaultSet) MUST be reachable through the host broker")
		Expect(resp).NotTo(BeNil())
		Expect(resp.GetSessions()).To(BeEmpty(), "emptySessionAccess backs ListActive with no sessions")

		// UNDECLARED ⇒ NOT REACHABLE: the binary runtime did NOT declare
		// SessionService, so the SAME call has no handler and is denied by the gate.
		binarySession := hostv1.NewSessionServiceClient(binaryEP.conn)
		_, binErr := binarySession.ListActive(context.Background(), &hostv1.ListActiveRequest{})
		Expect(binErr).To(HaveOccurred(),
			"an UNDECLARED service MUST NOT be reachable from the runtime that did not declare it")
		Expect(status.Code(binErr)).To(Equal(codes.Unimplemented),
			"the declaration gate denies an undeclared service with Unimplemented (no handler registered)")
	})

	// INV-PLUGIN-44 requires every declared dependency be "authorized as
	// PluginSubject" — and that neither runtime receive an unauthorized result.
	// The EvalService.Evaluate handler is the consumption-path ABAC authorization
	// seam: it is declared in BOTH sets (reachable on both runtimes) and runs the
	// host ABAC engine for the calling plugin's subject. Its security contract is
	// fail-closed: it recovers the acting PluginSubject from host-established
	// identity (binary: a host-issued dispatch token in metadata; Lua:
	// core.ActorFromContext) BEFORE consulting the engine, and denies when no
	// host-vouched identity is present.
	//
	// Over the in-process transport neither runtime carries a host-established
	// identity (no dispatch token is minted; the client context's actor does not
	// cross the gRPC boundary onto the server handler context), so the shared
	// authorization seam MUST fail closed on BOTH — proving the plugin cannot
	// obtain an allow it was not authorized for. Both endpoints additionally wire
	// a DenyAllEngine, so even past the identity seam the engine would deny.
	//
	// SCOPE: this asserts only fail-closed-when-identity-absent (bare endpoints,
	// no actor-stamp interceptor). The bare endpoint construction here is
	// deliberate — it proves the seam fails closed when no host-established
	// identity is present, independent of the interceptor.
	//
	// Positive identity-present coverage is provided by the production endpoint
	// unit test TestActorStampReachesServerSideThroughRealEndpoint
	// (internal/plugin/lua/bufconn_endpoint_test.go), which drives EvalService
	// through newPluginEndpoint with the actor-stamp interceptor (holomush-eykuh.4.5).
	// End-to-end Lua emit coverage (holo.emit.* → PluginEventEmitter.Emit →
	// actor_kinds_claimable gate) is in lua_emit_test.go in this package
	// (holomush-eykuh.4.10).
	//
	// Verifies: INV-PLUGIN-44
	It("fails the evaluate authorization seam closed across runtimes", func() {
		// Both runtimes wire a DenyAllEngine through the SAME RegisterCapabilities
		// source, so the engine itself never yields an allow either.
		binaryEP := newBinaryEndpointWithOpts(goplugin.WithEngine(policytest.DenyAllEngine()))
		luaEP := newLuaEndpointWithFunctions(
			hostfunc.New(nil, hostfunc.WithEngine(policytest.DenyAllEngine())),
		)

		// EvalService is declared in BOTH sets — reachable on both runtimes (the
		// "declared dependency obtained through the broker" precondition).
		Expect(binaryEP.srv.GetServiceInfo()).To(HaveKey("holomush.plugin.host.v1.EvalService"),
			"EvalService is declared in BinaryDefaultSet")
		Expect(luaEP.srv.GetServiceInfo()).To(HaveKey("holomush.plugin.host.v1.EvalService"),
			"EvalService is declared in LuaDefaultSet")

		req := &hostv1.EvaluateRequest{Action: "spectate", Resource: "scene:" + ulid.Make().String()}

		// Binary: no host-issued dispatch token on the call ⇒ the authorization
		// seam fails closed before any allow can be produced.
		binResp, binErr := hostv1.NewEvalServiceClient(binaryEP.conn).Evaluate(context.Background(), req)
		Expect(binErr).To(HaveOccurred(),
			"binary Evaluate MUST fail closed without a host-issued PluginSubject identity")
		Expect(binResp).To(BeNil(), "no allow is produced for an unauthorized binary caller")

		// Lua: no actor on the server-side context ⇒ the SAME seam fails closed.
		luaResp, luaErr := hostv1.NewEvalServiceClient(luaEP.conn).Evaluate(context.Background(), req)
		Expect(luaErr).To(HaveOccurred(),
			"lua Evaluate MUST fail closed without a host-established PluginSubject identity")
		Expect(luaResp).To(BeNil(), "no allow is produced for an unauthorized lua caller")
	})

	// INV-PLUGIN-22: "PluginHostService.Evaluate's subject is host-derived from
	// the authenticated actor; there is no subject field on the wire (never
	// sourced from plugin/Lua-supplied data)." Two clauses, both proven with NO
	// scaffolding that models non-existent production behavior:
	//
	//  1. NO SUBJECT FIELD ON THE WIRE (structural, both runtimes). The host.v1
	//     EvaluateRequest message — the SINGLE wire contract both runtimes consume
	//     through the shared hostcap.RegisterCapabilities source — carries exactly
	//     {action, resource} and NO subject/actor field. Forgery is impossible BY
	//     CONSTRUCTION; reflecting over the generated descriptor locks this so an
	//     accidental subject-field addition breaks the build.
	//
	//  2. SUBJECT IS HOST-DERIVED (Lua adapter, observed through the REAL
	//     production luaHostCapAdapter). luaHostCapAdapter.LookupActor builds the
	//     ABAC subject as access.PluginSubject(pluginName) from the
	//     HOST-ESTABLISHED pluginName — not from the context actor's ID and not
	//     from any wire field. We prove this is forgery-proof by stamping a FORGED
	//     actor ID on the context and asserting the recovered subject is still
	//     plugin:<host-established-name>, never plugin:<forged-id>.
	//
	// Verifies: INV-PLUGIN-22
	It("derives the evaluate subject from the host, not plugin-supplied data", func() {
		// (1) NO SUBJECT FIELD ON THE WIRE — structural lock on the single shared
		// EvaluateRequest contract. Both runtimes consume this exact message; if it
		// grew a subject field, a plugin could forge the authorization subject.
		md := (&hostv1.EvaluateRequest{}).ProtoReflect().Descriptor()
		fields := md.Fields()
		names := make([]string, 0, fields.Len())
		for i := range fields.Len() {
			names = append(names, string(fields.Get(i).Name()))
		}
		Expect(names).NotTo(ContainElement("subject"),
			"EvaluateRequest MUST NOT carry a subject field (INV-PLUGIN-22): the subject is host-derived, never plugin-supplied")
		Expect(names).NotTo(ContainElement("actor"),
			"EvaluateRequest MUST NOT carry an actor field (INV-PLUGIN-22): identity is host-established, never plugin-supplied")
		Expect(fields.Len()).To(Equal(2),
			"EvaluateRequest MUST have exactly {action, resource}; any additional field is a candidate forgery surface (INV-PLUGIN-22)")

		// (2) SUBJECT IS HOST-DERIVED — observed through the REAL production
		// luaHostCapAdapter the LuaDefaultSet endpoint uses (reached via the host's
		// HostCapabilitiesAdapter() accessor, exactly as newLuaEndpoint wires it).
		luaHost := lua.NewHostWithFunctions(hostfunc.New(nil))
		DeferCleanup(func() { _ = luaHost.Close(context.Background()) })
		adapter := luaHost.HostCapabilitiesAdapter()
		Expect(adapter).NotTo(BeNil(), "lua host must expose its real hostcap adapter")

		// A malicious plugin attempts to forge identity by stamping an arbitrary
		// actor ID on the dispatch context. The adapter MUST ignore that ID for the
		// ABAC subject and derive it solely from the HOST-ESTABLISHED plugin name.
		const forgedActorID = "forged-evil-character-id"
		ctx := core.WithActor(context.Background(),
			core.Actor{Kind: core.ActorCharacter, ID: forgedActorID})

		_, subject, err := adapter.LookupActor(ctx, parityPluginName)
		Expect(err).NotTo(HaveOccurred(), "LookupActor must recover identity when an actor is on the context")

		wantSubject := access.PluginSubject(parityPluginName)
		Expect(subject).To(Equal(wantSubject),
			"the ABAC subject MUST be host-derived from the host-established plugin name (plugin:parity-plugin), proving it is NOT sourced from the context/wire actor")
		Expect(subject).NotTo(ContainSubstring(forgedActorID),
			"the recovered subject MUST NOT contain the plugin-supplied (forged) actor ID; if it did, a plugin could forge its authorization subject (INV-PLUGIN-22)")
	})
})
