// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/core"
)

// TestActorStampInterceptorStampsPluginIdentityFromConnectionScope proves the
// interceptor stamps core.Actor{Kind: core.ActorPlugin, ID: pluginName} onto the
// handler context without reading the incoming server context. The incoming ctx is
// context.Background() (bare — no actor), which proves the stamp originates from
// the connection-scoped pluginName captured at endpoint construction, not from any
// value on the incoming request context (which is always bare server-side because
// plugins.NewInProcessConn drops context values across the bufconn boundary).
func TestActorStampInterceptorStampsPluginIdentityFromConnectionScope(t *testing.T) {
	const pluginName = "core-communication"

	var seen core.Actor
	var seenOK bool

	ic := newActorStampInterceptor(pluginName)
	_, err := ic(
		context.Background(), // bare — proves stamp does NOT read incoming ctx
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/holomush.plugin.host.v1.EmitService/Emit"},
		func(ctx context.Context, _ any) (any, error) {
			seen, seenOK = core.ActorFromContext(ctx)
			return nil, nil
		},
	)
	require.NoError(t, err)
	require.True(t, seenOK, "actor must be stamped on the handler context")
	assert.Equal(t, core.ActorPlugin, seen.Kind,
		"stamped actor kind must be ActorPlugin (the host-established plugin identity)")
	assert.Equal(t, pluginName, seen.ID,
		"stamped actor ID must be the connection-scoped pluginName, not an incoming context value")
}

// TestActorStampInterceptorStampsDifferentPluginNames proves the interceptor uses
// the connection-scoped pluginName (captured at construction) and not a shared
// global, so two interceptors built for different plugin names stamp distinct actors.
func TestActorStampInterceptorStampsDifferentPluginNames(t *testing.T) {
	names := []string{"plugin-alpha", "plugin-beta"}

	for _, name := range names {
		t.Run("stamps correct ID for "+name, func(t *testing.T) {
			var seen core.Actor
			ic := newActorStampInterceptor(name)
			_, err := ic(
				context.Background(),
				nil,
				&grpc.UnaryServerInfo{},
				func(ctx context.Context, _ any) (any, error) {
					seen, _ = core.ActorFromContext(ctx)
					return nil, nil
				},
			)
			require.NoError(t, err)
			assert.Equal(t, name, seen.ID)
			assert.Equal(t, core.ActorPlugin, seen.Kind)
		})
	}
}
