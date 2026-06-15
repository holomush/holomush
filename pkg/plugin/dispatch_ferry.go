// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// dispatchMetadataHeader is the reserved metadata key carrying the host-vouched
// dispatch envelope. It MUST stay byte-identical to
// internal/plugin/dispatchwire.MetadataKey — the host marshals the envelope onto
// this key when delivering to a plugin, and reconstructs it from this key on the
// plugin→host capability server. (Mirrors the x-holomush-emit-token
// literal/const split between the host and this SDK.)
const dispatchMetadataHeader = "x-holomush-dispatch"

// dispatchFerryInterceptor returns a client-side unary interceptor, installed on
// the single plugin→host connection (dialPluginHost), that re-attaches the
// host-vouched dispatch envelope from the INCOMING delivery metadata onto each
// outgoing plugin→host call.
//
// A binary plugin is out-of-process and never holds the in-process
// DispatchContext value the Lua bufconn marshals from; it can only forward the
// opaque host-vouched envelope it received on delivery. Scoped capability calls
// (e.g. world.mutation) need that dispatch on the host side to resolve the
// acting character's own-location fence (INV-PLUGIN-51 / INV-PLUGIN-52). The host
// owns both ends — it marshals on delivery and reconstructs server-side — so the
// plugin treats the value as opaque and merely ferries it.
//
// Centralised here (one interceptor per host connection) so every current and
// future host-facing client carries dispatch without per-client wiring.
func dispatchFerryInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(ferryDispatch(ctx), method, req, reply, cc, opts...)
	}
}

// ferryDispatch strips any plugin-supplied outgoing value on the reserved key
// (host-vouched only), then forwards the value carried on the INCOMING delivery
// metadata, if present and unambiguous. A plugin cannot forge scope attributes:
// without a host-delivered envelope no key is forwarded, so the scoped call fails
// closed (SCOPE_NO_DISPATCH) on the host. Ambiguous (multi-value) incoming
// metadata is dropped, matching the host decoder's fail-closed semantics.
func ferryDispatch(ctx context.Context) context.Context {
	// Strip any plugin-injected outgoing value: the host's delivery envelope is
	// authoritative, never plugin-supplied outgoing metadata.
	if out, ok := metadata.FromOutgoingContext(ctx); ok {
		if _, present := out[dispatchMetadataHeader]; present {
			cleaned := out.Copy()
			cleaned.Delete(dispatchMetadataHeader)
			ctx = metadata.NewOutgoingContext(ctx, cleaned)
		}
	}
	in, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}
	vals := in.Get(dispatchMetadataHeader)
	if len(vals) != 1 || vals[0] == "" {
		// Absent, ambiguous, or empty: forward nothing → fail closed downstream.
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, dispatchMetadataHeader, vals[0])
}
