// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"

	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/core"
)

// newActorStampInterceptor returns a unary server interceptor that stamps the
// host-established plugin identity onto the request context before the capability
// interceptor and handlers run.
//
// The actor is derived purely from the connection-scoped pluginName captured at
// endpoint construction. It MUST NOT read the incoming server context to obtain
// the actor: plugins.NewInProcessConn drops context values across the bufconn
// boundary, so the server-side request context is always bare (no actor stamped
// by the caller). The correct production source is the connection-scoped
// pluginName itself — the host-established identity is {ActorPlugin, pluginName},
// confirmed at pkg/plugin/event_sink.go:67 and api/proto/holomush/plugin/host/v1/emit.proto:32.
//
// This interceptor is identity-only: it stamps who the plugin is. Least-privilege
// gating (whether this plugin is authorized for the requested capability) remains
// the responsibility of the downstream capability interceptor (holomush-eykuh.3).
// If pluginName is empty, ctx is left unstamped and downstream capability gates
// fail closed — correct direction.
func newActorStampInterceptor(pluginName string) grpc.UnaryServerInterceptor {
	// Actor.ID is the plugin name, not a ULID. The ULID requirement in
	// coreActorToEventbusActor (event_emitter.go) applies only to the emit path;
	// on the ABAC path the actor flows through access.PluginSubject(actor.ID),
	// which expects the plugin name string.
	actor := core.Actor{Kind: core.ActorPlugin, ID: pluginName}
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		// Defense-in-depth: Manifest.Validate (manifest.go) rejects empty plugin
		// names before any endpoint is built, so this branch is unreachable in
		// production. If it ever fires, the unstamped ctx makes downstream
		// capability gates fail closed (LookupActor -> ACTOR_NOT_FOUND).
		if pluginName != "" {
			ctx = core.WithActor(ctx, actor)
		}
		return handler(ctx, req)
	}
}
