// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package dispatchwire is the single source of truth for the host-vouched
// dispatch envelope that crosses a plugin transport boundary, shared by both
// plugin runtimes (plugin-runtime-symmetry, INV-PLUGIN-51).
//
// The scope half of the capability interceptor (hostcap, INV-PLUGIN-52) needs
// the per-call dispatch context — the acting character's subject plus the
// host-resolved attributes (notably "location") — to resolve an own-location
// fence. Context VALUES do not survive a gRPC boundary (the Lua bufconn drops
// them; a binary plugin is a separate process), so the dispatch context MUST be
// marshalled onto the wire as gRPC metadata and reconstructed server-side before
// the scope interceptor runs.
//
// The key is host-owned and fail-closed by construction: the host always
// overwrites it from the host-vouched pluginauthz.DispatchForHost value (or
// clears it), so a plugin cannot smuggle forged scope attributes through it.
// Absent, ambiguous, malformed, or empty-subject metadata reconstructs to "no
// dispatch", which the scope interceptor denies (SCOPE_NO_DISPATCH).
package dispatchwire

import (
	"context"
	"encoding/json"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

// MetadataKey is the single reserved gRPC metadata key carrying the host-vouched
// dispatch envelope across a plugin transport boundary.
//
// The binary SDK ferry (pkg/plugin) copies this same key by literal; the two
// MUST stay in sync (mirrors the x-holomush-emit-token literal/const split).
const MetadataKey = "x-holomush-dispatch"

// envelope is the wire form of pluginauthz.DispatchContext. The field set
// mirrors DispatchContext exactly so the subject and every host-vouched
// attribute round-trips without loss — a dropped attribute fails the scope
// fence closed.
type envelope struct {
	Subject    string            `json:"subject"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// AttachOutgoing makes the host-vouched dc authoritative on the outgoing
// context: it first strips any caller-supplied MetadataKey, then re-attaches a
// freshly marshalled envelope when dc carries a non-empty subject. A plugin that
// injected the reserved key directly cannot have it honored — the host
// overwrites it (with the host-vouched envelope, or with nothing). A marshal
// failure (effectively impossible for this fixed shape) leaves the key absent,
// which fails closed downstream.
func AttachOutgoing(ctx context.Context, dc pluginauthz.DispatchContext) context.Context {
	ctx = StripOutgoing(ctx)
	if dc.Subject == "" {
		return ctx
	}
	encoded, err := json.Marshal(envelope{Subject: dc.Subject, Attributes: dc.Attributes})
	if err != nil {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, MetadataKey, string(encoded))
}

// StripOutgoing removes any MetadataKey value from the outgoing metadata so the
// host's host-vouched value is authoritative. Without this, a plugin that
// injected the reserved key would have it forwarded when the host adds no
// dispatch of its own (fail-open).
func StripOutgoing(ctx context.Context) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return ctx
	}
	if _, present := md[MetadataKey]; !present {
		return ctx
	}
	cleaned := md.Copy()
	cleaned.Delete(MetadataKey)
	return metadata.NewOutgoingContext(ctx, cleaned)
}

// DecodeFromIncoming reads the dispatch envelope from the incoming metadata.
// ok is false (no dispatch) when the key is absent, carries more than one value,
// is not valid JSON, or decodes to an empty subject — every such case fails
// closed at scope-enforcement time.
func DecodeFromIncoming(ctx context.Context) (pluginauthz.DispatchContext, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return pluginauthz.DispatchContext{}, false
	}
	vals := md.Get(MetadataKey)
	if len(vals) != 1 {
		// Absent, or ambiguous (multiple values): fail closed.
		return pluginauthz.DispatchContext{}, false
	}
	var env envelope
	if err := json.Unmarshal([]byte(vals[0]), &env); err != nil {
		return pluginauthz.DispatchContext{}, false
	}
	if env.Subject == "" {
		return pluginauthz.DispatchContext{}, false
	}
	return pluginauthz.DispatchContext{Subject: env.Subject, Attributes: env.Attributes}, true
}

// StampInterceptor returns a server-side unary interceptor that reconstructs the
// host-vouched pluginauthz.DispatchContext from the incoming gRPC metadata and
// stamps it onto the handler context BEFORE the capability / scope interceptor
// runs. It MUST be chained before hostcap's capability interceptor so the scope
// half reads the stamped dispatch (INV-PLUGIN-52).
//
// Fail-closed: a missing key, a malformed envelope, or an empty subject leaves
// the context WITHOUT a dispatch context, so the scope interceptor denies
// (SCOPE_NO_DISPATCH). The attributes are host-vouched by construction — only a
// host writes this key, and only from DispatchForHost, never from
// plugin-supplied data (INV-PLUGIN-51).
func StampInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if dc, ok := DecodeFromIncoming(ctx); ok {
			ctx = pluginauthz.WithDispatch(ctx, dc)
		}
		return handler(ctx, req)
	}
}
