// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/pkg/errutil"
)

type dispatchMarkerKey struct{}

// dispatchCapableHost is a mockBinaryHost that additionally implements the
// ServiceDispatcher capability, recording the arguments it was routed.
type dispatchCapableHost struct {
	mockBinaryHost
	gotPlugin string
	gotActor  core.Actor
	gotOwner  string
	released  bool
}

func (h *dispatchCapableHost) BeginServiceDispatch(ctx context.Context, pluginName string, actor core.Actor, ownerPlayerID string) (context.Context, func(), error) {
	h.gotPlugin, h.gotActor, h.gotOwner = pluginName, actor, ownerPlayerID
	return context.WithValue(ctx, dispatchMarkerKey{}, "dispatched"), func() { h.released = true }, nil
}

// newManagerWithBinaryPlugin loads one binary plugin named "svc-plugin" onto
// the given host and returns the manager.
func newManagerWithBinaryPlugin(t *testing.T, host plugins.Host) *plugins.Manager {
	t.Helper()
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	pluginDir := filepath.Join(pluginsDir, "svc-plugin")
	mkdirAll(t, pluginDir)
	writeFile(t, filepath.Join(pluginDir, "plugin.yaml"), []byte(`name: svc-plugin
version: 1.0.0
type: binary
binary-plugin:
  executable: svc-plugin`))

	mgr, err := plugins.NewManager(pluginsDir, plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, err)
	mgr.RegisterHost(plugins.TypeBinary, host)
	require.NoError(t, mgr.LoadAll(context.Background()))
	return mgr
}

// TestManagerBeginServiceDispatchRoutesToOwningHost verifies that the manager
// resolves the plugin's host and delegates with the caller's arguments intact.
func TestManagerBeginServiceDispatchRoutesToOwningHost(t *testing.T) {
	host := &dispatchCapableHost{}
	mgr := newManagerWithBinaryPlugin(t, host)

	actor := core.Actor{Kind: core.ActorCharacter, ID: "01HCHAR0000000000000000000"}
	ctx, release, err := mgr.BeginServiceDispatch(context.Background(), "svc-plugin", actor, "01PLAYER000000000000000000")
	require.NoError(t, err)
	require.NotNil(t, release)

	assert.Equal(t, "svc-plugin", host.gotPlugin)
	assert.Equal(t, actor, host.gotActor)
	assert.Equal(t, "01PLAYER000000000000000000", host.gotOwner)
	assert.Equal(t, "dispatched", ctx.Value(dispatchMarkerKey{}), "host's dispatch context must be returned")

	release()
	assert.True(t, host.released, "release must propagate to the host")
}

// TestManagerBeginServiceDispatchFailsForUnknownPlugin verifies the typed
// not-loaded error for a plugin name no host owns.
func TestManagerBeginServiceDispatchFailsForUnknownPlugin(t *testing.T) {
	mgr := newManagerWithBinaryPlugin(t, &dispatchCapableHost{})

	_, _, err := mgr.BeginServiceDispatch(
		context.Background(), "no-such-plugin",
		core.Actor{Kind: core.ActorCharacter, ID: "01HCHAR0000000000000000000"}, "",
	)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_NOT_LOADED")
}

// TestManagerBeginServiceDispatchFailsWhenHostLacksCapability verifies the
// typed error when the owning host does not implement ServiceDispatcher
// (e.g. the Lua host — Lua plugins serve no gRPC services).
func TestManagerBeginServiceDispatchFailsWhenHostLacksCapability(t *testing.T) {
	mgr := newManagerWithBinaryPlugin(t, &mockBinaryHost{})

	_, _, err := mgr.BeginServiceDispatch(
		context.Background(), "svc-plugin",
		core.Actor{Kind: core.ActorCharacter, ID: "01HCHAR0000000000000000000"}, "",
	)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SERVICE_DISPATCH_UNSUPPORTED")
}
