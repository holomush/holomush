// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"

	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostcap"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// newPluginHostServiceServer builds the single broker *grpc.Server a binary
// plugin dials back into. Every host-brokered capability is served as a
// capability-scoped holomush.plugin.host.v1 service on this one server, reached
// through the single broker handshake (pkg/plugin.PluginHostServiceName names
// the broker channel, not a gRPC service). The former monolithic
// holomush.plugin.v1.PluginHostService is gone (holomush-eykuh.1, Task 12): its
// authoritative handler bodies now live on the per-capability servers, which
// after holomush-eykuh.2 are the runtime-neutral hostcap servers consumed by both
// the binary and Lua runtimes through the HostCapabilities port (INV-PLUGIN-49).
//
// *Host satisfies hostcap.HostCapabilities (the binary adapter), so the binary
// registration is hostcap.RegisterCapabilities with the BinaryDefaultSet. That
// set omits World/Property/Session — they have no binary consumer in this
// sub-spec — so they are deliberately NOT registered here.
//
// The server chains the host-capability interceptor (holomush-eykuh.3): one
// interceptor per plugin, closing over that plugin's declared capability set via
// hostcap.DeclaredAccessFromManifest. The SAME helper + interceptor are installed
// on the Lua per-plugin server (internal/plugin/lua/bufconn_endpoint.go), so the
// declaration + access-class trust gate is identical across runtimes
// (plugin-runtime-symmetry, INV-PLUGIN-45/49). Engine/Auditor are sourced
// identically on both runtimes through the hostcap.HostCapabilities port
// (host.AccessEngine()/Auditor()); the static half (this task) does not read
// them — Task 10 wires the policy/scope half.
func newPluginHostServiceServer(host *Host, manifest *plugins.Manifest) func([]grpc.ServerOption) *grpc.Server {
	return func(opts []grpc.ServerOption) *grpc.Server {
		return newHostCapabilityServer(
			host,
			hostcap.InterceptorDeps{
				Engine:         host.AccessEngine(),
				Auditor:        host.Auditor(),
				PluginName:     manifest.Name,
				DeclaredAccess: hostcap.DeclaredAccessFromManifest(manifest),
			},
			hostcap.BinaryDefaultSet,
			opts,
		)
	}
}

// newHostCapabilityServer builds the host-capability gRPC server with the
// runtime-neutral interceptor chain: the token-dispatch interceptor (recovers the
// host-vouched DispatchContext from the caller's host-issued emit token) chained
// BEFORE the capability/scope interceptor, then `set` registered against `base`.
//
// Unlike the Lua per-plugin bufconn (internal/plugin/lua/bufconn_endpoint.go),
// which marshals dispatch as host-controlled in-process metadata, a binary plugin
// is out-of-process and controls its own outgoing gRPC metadata. Recovering
// dispatch from plugin-supplied metadata would let it forge the acting-character
// scope; instead the dispatch is bound to the unforgeable host-issued emit token
// at delivery (host.go) and recovered here from the validated token via
// base.LookupDispatch (plugin-runtime-symmetry, INV-PLUGIN-51). Without dispatch,
// a scoped call denies (SCOPE_NO_DISPATCH).
//
// Extracted from newPluginHostServiceServer so tests can register a
// scope-eligible capability set against a custom base — BinaryDefaultSet omits
// WorldMutationService (the binary Host has no world surface), so the scoped path
// is not reachable through the production set today (holomush-ndtq1).
func newHostCapabilityServer(
	base hostcap.HostCapabilities,
	deps hostcap.InterceptorDeps,
	set hostcap.CapabilitySet,
	opts []grpc.ServerOption,
) *grpc.Server {
	ic := hostcap.NewCapabilityInterceptor(deps)
	server := grpc.NewServer(append(opts, grpc.ChainUnaryInterceptor(tokenDispatchInterceptor(base, deps.PluginName), ic))...)
	hostcap.RegisterCapabilities(server, hostcap.NewBase(base, deps.PluginName), set)
	return server
}

// tokenDispatchInterceptor recovers the host-vouched DispatchContext bound to the
// caller's host-issued emit token (base.LookupDispatch) and stamps it onto the
// handler context BEFORE the capability/scope interceptor runs. The dispatch is
// keyed by the host-minted, host-stored token — never read from plugin-controlled
// gRPC metadata — so an untrusted out-of-process plugin cannot forge the
// acting-character subject or scope attributes (INV-PLUGIN-51). A missing or
// invalid token, or a token carrying no character dispatch, leaves the context
// without dispatch, so the scope interceptor denies (SCOPE_NO_DISPATCH).
func tokenDispatchInterceptor(base hostcap.HostCapabilities, pluginName string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if dc, ok := base.LookupDispatch(ctx, pluginName); ok && dc.Subject != "" {
			ctx = pluginauthz.WithDispatch(ctx, dc)
		}
		return handler(ctx, req)
	}
}

// sdkActorKindToCore maps a plugin-SDK ActorKind to the core ActorKind. It is
// retained on the binary host alongside the broker wiring; unknown kinds fall
// back to ActorPlugin (the least-privileged plugin-owned actor).
func sdkActorKindToCore(kind pluginsdk.ActorKind) core.ActorKind {
	switch kind {
	case pluginsdk.ActorCharacter:
		return core.ActorCharacter
	case pluginsdk.ActorSystem:
		return core.ActorSystem
	default:
		return core.ActorPlugin
	}
}
