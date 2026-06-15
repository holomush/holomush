// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"encoding/json"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

// dispatchMetadataKey is the single reserved gRPC metadata key carrying the
// host-vouched dispatch envelope across the Lua per-plugin bufconn.
// plugins.NewInProcessConn drops context VALUES, so the server-side request
// context is bare; the scope interceptor (hostcap, INV-PLUGIN-52) needs the
// dispatch context (subject + the acting character's resolved "location") to
// resolve the own-location fence. The actor-stamp companion
// (newActorStampInterceptor) reconstructs the connection-static actor; dispatch
// is per-call dynamic, so it MUST be marshalled across the wire (INV-PLUGIN-51).
//
// The key is host-owned: the outgoing interceptor below always overwrites it
// from the host-vouched DispatchForHost(ctx) value (or clears it), so a plugin
// cannot smuggle forged scope attributes through it — the host controls both
// ends of this in-process transport.
const dispatchMetadataKey = "x-holomush-dispatch"

// dispatchEnvelope is the wire form of pluginauthz.DispatchContext. It is
// JSON-encoded into dispatchMetadataKey by the outgoing interceptor and decoded
// by the incoming interceptor. The field set mirrors DispatchContext exactly so
// the subject and every host-vouched attribute (notably "location") round-trip
// without loss — a dropped attribute fails the scope fence closed.
type dispatchEnvelope struct {
	Subject    string            `json:"subject"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// newDispatchOutgoingInterceptor returns a client-side unary interceptor that
// marshals the host-vouched dispatch context from the CALL ctx into outgoing
// gRPC metadata under dispatchMetadataKey, so it survives the bufconn boundary.
//
// Host-vouched only: the envelope is built solely from
// pluginauthz.DispatchForHost(ctx), which only Host.stampDispatch sets (via
// pluginauthz.WithDispatch). The interceptor unconditionally REPLACES any
// existing dispatchMetadataKey value on the outgoing context — a plugin that
// tried to set the reserved key directly cannot have it honored, because the
// host overwrites it (with the host-vouched envelope, or with nothing when no
// host dispatch is present). This preserves INV-PLUGIN-51 / fail-closed scope
// semantics: absent or forged dispatch reaches the server as "no dispatch", and
// the scope interceptor denies.
func newDispatchOutgoingInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = stripDispatchMetadata(ctx)
		if dc, ok := pluginauthz.DispatchForHost(ctx); ok && dc.Subject != "" {
			if encoded, err := json.Marshal(dispatchEnvelope{Subject: dc.Subject, Attributes: dc.Attributes}); err == nil {
				ctx = metadata.AppendToOutgoingContext(ctx, dispatchMetadataKey, string(encoded))
			}
			// A marshal failure (effectively impossible for this fixed shape)
			// leaves the key absent, which fails closed downstream — correct.
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// stripDispatchMetadata removes any caller-supplied dispatchMetadataKey from the
// outgoing metadata so the host's host-vouched value is authoritative. Without
// this, a plugin that injected the reserved key would have it forwarded when the
// host adds no dispatch of its own (fail-open). Returns a ctx whose outgoing
// metadata carries every key EXCEPT dispatchMetadataKey.
func stripDispatchMetadata(ctx context.Context) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return ctx
	}
	if _, present := md[dispatchMetadataKey]; !present {
		return ctx
	}
	cleaned := md.Copy()
	cleaned.Delete(dispatchMetadataKey)
	return metadata.NewOutgoingContext(ctx, cleaned)
}

// newDispatchStampInterceptor returns a server-side unary interceptor that
// reconstructs the host-vouched pluginauthz.DispatchContext from the incoming
// gRPC metadata and stamps it onto the handler context BEFORE the capability /
// scope interceptor runs. It MUST be chained before hostcap's capability
// interceptor (see bufconn_endpoint.go) so the scope half reads the stamped
// dispatch (INV-PLUGIN-52).
//
// Fail-closed: a missing key, a malformed envelope, or an empty subject leaves
// the context WITHOUT a dispatch context, so the scope interceptor denies
// (SCOPE_NO_DISPATCH). The attributes are host-vouched by construction — only
// the host's outgoing interceptor writes this key, and that interceptor sources
// the envelope from DispatchForHost(ctx), never from plugin-supplied data
// (INV-PLUGIN-51).
func newDispatchStampInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if dc, ok := decodeDispatchFromMetadata(ctx); ok {
			ctx = pluginauthz.WithDispatch(ctx, dc)
		}
		return handler(ctx, req)
	}
}

// decodeDispatchFromMetadata reads the dispatch envelope from the incoming
// metadata. ok is false (no dispatch stamped) when the key is absent, carries
// more than one value, is not valid JSON, or decodes to an empty subject — every
// such case fails closed at scope-enforcement time.
func decodeDispatchFromMetadata(ctx context.Context) (pluginauthz.DispatchContext, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return pluginauthz.DispatchContext{}, false
	}
	vals := md.Get(dispatchMetadataKey)
	if len(vals) != 1 {
		// Absent, or ambiguous (multiple values): fail closed.
		return pluginauthz.DispatchContext{}, false
	}
	var env dispatchEnvelope
	if err := json.Unmarshal([]byte(vals[0]), &env); err != nil {
		return pluginauthz.DispatchContext{}, false
	}
	if env.Subject == "" {
		return pluginauthz.DispatchContext{}, false
	}
	return pluginauthz.DispatchContext{Subject: env.Subject, Attributes: env.Attributes}, true
}
