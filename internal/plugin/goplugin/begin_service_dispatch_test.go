// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/pkg/errutil"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// ferryToIncoming simulates the plugin-side metadata ferry: the dispatch
// context's outgoing metadata (token + advisory actor headers) arrives as
// incoming metadata on a PluginHostService call, exactly as
// pkg/plugin/evaluate_client.go forwards it.
func ferryToIncoming(dispatchCtx context.Context, t *testing.T) context.Context {
	t.Helper()
	md, ok := metadata.FromOutgoingContext(dispatchCtx)
	require.True(t, ok, "dispatch context must carry outgoing metadata")
	return metadata.NewIncomingContext(context.Background(), md)
}

// TestBeginServiceDispatchTokenAuthorizesEvaluateAsDispatchedActor proves the
// full loop: a token minted by BeginServiceDispatch, when ferried back on a
// PluginHostService.Evaluate call, resolves to the minted actor as the ABAC
// subject — only the dispatched character's subject holds the grant.
func TestBeginServiceDispatchTokenAuthorizesEvaluateAsDispatchedActor(t *testing.T) {
	t.Parallel()
	charID := core.NewULID().String()
	sceneID := core.NewULID().String()
	eng := policytest.NewGrantEngine()
	eng.Grant("character:"+charID, "spectate", "scene:"+sceneID)

	manifest := &plugins.Manifest{
		Name:          "core-scenes",
		Type:          plugins.TypeBinary,
		ResourceTypes: []string{"scene"},
	}
	h := newTestHostWithEngine(t, "core-scenes", manifest, eng)
	defer func() { _ = h.Close(context.Background()) }()

	actor := core.Actor{Kind: core.ActorCharacter, ID: charID}
	dispatchCtx, release, err := h.BeginServiceDispatch(context.Background(), "core-scenes", actor, "01PLAYER000000000000000000")
	require.NoError(t, err)
	defer release()

	// The token in the dispatch metadata resolves to the minted actor and
	// the supplied owning player.
	md, _ := metadata.FromOutgoingContext(dispatchCtx)
	tokens := md.Get("x-holomush-emit-token")
	require.Len(t, tokens, 1)
	storedActor, ownerPlayer, ok := h.tokenStore.Lookup("core-scenes", tokens[0])
	require.True(t, ok)
	assert.Equal(t, actor, storedActor)
	assert.Equal(t, "01PLAYER000000000000000000", ownerPlayer)

	// Full loop: Evaluate with the ferried token is allowed only because the
	// subject derived from the token is character:<charID>.
	srv := &pluginHostServiceServer{host: h, pluginName: "core-scenes"}
	resp, err := srv.Evaluate(ferryToIncoming(dispatchCtx, t), &pluginv1.PluginHostServiceEvaluateRequest{
		Action:   "spectate",
		Resource: "scene:" + sceneID,
	})
	require.NoError(t, err)
	assert.True(t, resp.GetAllowed(), "grant exists only for the minted character subject")
}

// TestBeginServiceDispatchReleaseRevokesToken verifies the returned release
// func revokes the token: a second Evaluate with the same ferried token fails
// EMIT_TOKEN_REJECTED.
func TestBeginServiceDispatchReleaseRevokesToken(t *testing.T) {
	t.Parallel()
	manifest := &plugins.Manifest{
		Name:          "core-scenes",
		Type:          plugins.TypeBinary,
		ResourceTypes: []string{"scene"},
	}
	h := newTestHostWithEngine(t, "core-scenes", manifest, policytest.AllowAllEngine())
	defer func() { _ = h.Close(context.Background()) }()

	actor := core.Actor{Kind: core.ActorCharacter, ID: core.NewULID().String()}
	dispatchCtx, release, err := h.BeginServiceDispatch(context.Background(), "core-scenes", actor, "")
	require.NoError(t, err)

	srv := &pluginHostServiceServer{host: h, pluginName: "core-scenes"}
	incoming := ferryToIncoming(dispatchCtx, t)
	_, err = srv.Evaluate(incoming, &pluginv1.PluginHostServiceEvaluateRequest{
		Action:   "spectate",
		Resource: "scene:" + core.NewULID().String(),
	})
	require.NoError(t, err, "token is valid before release")

	release()

	_, err = srv.Evaluate(incoming, &pluginv1.PluginHostServiceEvaluateRequest{
		Action:   "spectate",
		Resource: "scene:" + core.NewULID().String(),
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EMIT_TOKEN_REJECTED")
}

// TestBeginServiceDispatchAttachesAdvisoryActorMetadata verifies the dispatch
// context carries the advisory actor-kind/-id headers so plugin handlers can
// run defense-in-depth identity checks.
func TestBeginServiceDispatchAttachesAdvisoryActorMetadata(t *testing.T) {
	t.Parallel()
	manifest := &plugins.Manifest{Name: "core-scenes", Type: plugins.TypeBinary}
	h := newTestHostWithEngine(t, "core-scenes", manifest, policytest.AllowAllEngine())
	defer func() { _ = h.Close(context.Background()) }()

	charID := core.NewULID().String()
	dispatchCtx, release, err := h.BeginServiceDispatch(
		context.Background(), "core-scenes",
		core.Actor{Kind: core.ActorCharacter, ID: charID}, "",
	)
	require.NoError(t, err)
	defer release()

	kind, id, ok := pluginsdk.ActorMetadataFromOutgoingContext(dispatchCtx)
	require.True(t, ok)
	assert.Equal(t, pluginsdk.ActorCharacter, kind)
	assert.Equal(t, charID, id)
}

// TestBeginServiceDispatchFailsWhenHostClosed verifies the closed-host guard.
func TestBeginServiceDispatchFailsWhenHostClosed(t *testing.T) {
	t.Parallel()
	h := NewHost()
	require.NoError(t, h.Close(context.Background()))

	_, _, err := h.BeginServiceDispatch(
		context.Background(), "core-scenes",
		core.Actor{Kind: core.ActorCharacter, ID: core.NewULID().String()}, "",
	)
	require.ErrorIs(t, err, ErrHostClosed)
}

// TestBeginServiceDispatchFailsWhenPluginNotLoaded verifies the unknown-plugin guard.
func TestBeginServiceDispatchFailsWhenPluginNotLoaded(t *testing.T) {
	t.Parallel()
	h := NewHost()
	defer func() { _ = h.Close(context.Background()) }()

	_, _, err := h.BeginServiceDispatch(
		context.Background(), "no-such-plugin",
		core.Actor{Kind: core.ActorCharacter, ID: core.NewULID().String()}, "",
	)
	require.ErrorIs(t, err, ErrPluginNotLoaded)
}
