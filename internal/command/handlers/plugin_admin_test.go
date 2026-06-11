// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"bytes"
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/testsupport/sessiontest"
	"github.com/holomush/holomush/pkg/errutil"
)

// stubPluginLister is a test implementation of PluginLister.
type stubPluginLister struct {
	plugins map[string]*plugins.DiscoveredPlugin
	names   []string
}

func (s *stubPluginLister) ListPlugins() []string {
	return s.names
}

func (s *stubPluginLister) GetLoadedPlugin(name string) (*plugins.DiscoveredPlugin, bool) {
	dp, ok := s.plugins[name]
	return dp, ok
}

func newPluginListerWithPlugins(dps ...*plugins.DiscoveredPlugin) *stubPluginLister {
	lister := &stubPluginLister{
		plugins: make(map[string]*plugins.DiscoveredPlugin, len(dps)),
	}
	for _, dp := range dps {
		lister.plugins[dp.Manifest.Name] = dp
		lister.names = append(lister.names, dp.Manifest.Name)
	}
	return lister
}

type pluginTestSetup struct {
	buf    *bytes.Buffer
	charID ulid.ULID
}

func newPluginTestSetup() *pluginTestSetup {
	return &pluginTestSetup{
		buf:    &bytes.Buffer{},
		charID: ulid.Make(),
	}
}

func (s *pluginTestSetup) makeExec(t *testing.T, args string) *command.CommandExecution {
	t.Helper()
	engine := policytest.NewGrantEngine()
	subject := access.CharacterSubject(s.charID.String())
	engine.GrantCommandExecution(subject, "plugin")
	svc := command.NewTestServices(command.ServicesConfig{
		Engine:  engine,
		Session: sessiontest.NewStore(t),
	})
	return command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   s.charID,
		CharacterName: "Admin",
		PlayerID:      ulid.Make(),
		Args:          args,
		Output:        s.buf,
		Services:      svc,
	})
}

func luaPlugin() *plugins.Manifest {
	return &plugins.Manifest{
		Name:    "core-communication",
		Version: "1.0.0",
		Type:    plugins.TypeLua,
		Storage: plugins.StorageKV,
		Commands: []plugins.CommandSpec{
			{Name: "say"},
			{Name: "pose"},
		},
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}
}

func binaryPlugin() *plugins.Manifest {
	return &plugins.Manifest{
		Name:     "core-scenes",
		Version:  "2.1.0",
		Type:     plugins.TypeBinary,
		Storage:  plugins.StoragePostgres,
		Requires: plugins.RequireServices("holomush.world.v1.WorldService"),
		Provides: []string{"holomush.scene.v1.SceneService"},
		Commands: []plugins.CommandSpec{
			{Name: "scene"},
			{Name: "scenes"},
		},
		BinaryPlugin: &plugins.BinaryConfig{Executable: "core-scenes"},
	}
}

func TestPluginListFormatsLoadedPlugins(t *testing.T) {
	ts := newPluginTestSetup()
	lister := newPluginListerWithPlugins(
		&plugins.DiscoveredPlugin{Manifest: luaPlugin()},
		&plugins.DiscoveredPlugin{Manifest: binaryPlugin()},
	)

	handler := NewPluginHandler(lister)
	err := handler(context.Background(), ts.makeExec(t, "list"))

	require.NoError(t, err)
	output := ts.buf.String()
	assert.Contains(t, output, "Loaded plugins:")
	assert.Contains(t, output, "core-communication")
	assert.Contains(t, output, "lua")
	assert.Contains(t, output, "1.0.0")
	assert.Contains(t, output, "core-scenes")
	assert.Contains(t, output, "binary")
	assert.Contains(t, output, "2.1.0")
}

func TestPluginListShowsMessageWhenNoPlugins(t *testing.T) {
	ts := newPluginTestSetup()
	lister := newPluginListerWithPlugins()

	handler := NewPluginHandler(lister)
	err := handler(context.Background(), ts.makeExec(t, "list"))

	require.NoError(t, err)
	assert.Contains(t, ts.buf.String(), "No plugins loaded.")
}

func TestPluginInfoShowsDetailForLoadedPlugin(t *testing.T) {
	ts := newPluginTestSetup()
	lister := newPluginListerWithPlugins(
		&plugins.DiscoveredPlugin{Manifest: binaryPlugin()},
	)

	handler := NewPluginHandler(lister)
	err := handler(context.Background(), ts.makeExec(t, "info core-scenes"))

	require.NoError(t, err)
	output := ts.buf.String()
	assert.Contains(t, output, "Plugin: core-scenes")
	assert.Contains(t, output, "Version: 2.1.0")
	assert.Contains(t, output, "Type: binary")
	assert.Contains(t, output, "Storage: postgres")
	assert.Contains(t, output, "Requires: service:holomush.world.v1.WorldService")
	assert.Contains(t, output, "Provides: holomush.scene.v1.SceneService")
	assert.Contains(t, output, "Commands: scene, scenes")
}

func TestPluginInfoReturnsErrorForUnknownPlugin(t *testing.T) {
	ts := newPluginTestSetup()
	lister := newPluginListerWithPlugins()

	handler := NewPluginHandler(lister)
	err := handler(context.Background(), ts.makeExec(t, "info nonexistent"))

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, command.CodeTargetNotFound)
}

func TestPluginInfoOmitsEmptyOptionalFields(t *testing.T) {
	ts := newPluginTestSetup()
	lister := newPluginListerWithPlugins(
		&plugins.DiscoveredPlugin{Manifest: luaPlugin()},
	)

	handler := NewPluginHandler(lister)
	err := handler(context.Background(), ts.makeExec(t, "info core-communication"))

	require.NoError(t, err)
	output := ts.buf.String()
	assert.Contains(t, output, "Plugin: core-communication")
	assert.Contains(t, output, "Type: lua")
	assert.NotContains(t, output, "Requires:")
	assert.NotContains(t, output, "Provides:")
}

func TestPluginInfoShowsVerbRegistrations(t *testing.T) {
	ts := newPluginTestSetup()
	m := luaPlugin()
	m.Verbs = []plugins.VerbSpec{
		{Type: "custom_say", Category: "communication", Format: "speech", Label: "says", DisplayTarget: "terminal"},
		{Type: "custom_action", Category: "communication", Format: "action", DisplayTarget: "both"},
	}
	lister := newPluginListerWithPlugins(
		&plugins.DiscoveredPlugin{Manifest: m},
	)

	handler := NewPluginHandler(lister)
	err := handler(context.Background(), ts.makeExec(t, "info core-communication"))

	require.NoError(t, err)
	output := ts.buf.String()
	assert.Contains(t, output, "Verbs:")
	assert.Contains(t, output, "custom_say (communication/speech)")
	assert.Contains(t, output, "custom_action (communication/action)")
}

func TestPluginInfoOmitsVerbsWhenEmpty(t *testing.T) {
	ts := newPluginTestSetup()
	lister := newPluginListerWithPlugins(
		&plugins.DiscoveredPlugin{Manifest: luaPlugin()},
	)

	handler := NewPluginHandler(lister)
	err := handler(context.Background(), ts.makeExec(t, "info core-communication"))

	require.NoError(t, err)
	assert.NotContains(t, ts.buf.String(), "Verbs:")
}

func TestPluginShowsUsageForInvalidSubcommands(t *testing.T) {
	cases := []struct {
		name string
		args string
	}{
		{"shows usage with no subcommand", ""},
		{"shows usage for unknown subcommand", "reload"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := newPluginTestSetup()
			lister := newPluginListerWithPlugins()

			handler := NewPluginHandler(lister)
			err := handler(context.Background(), ts.makeExec(t, tc.args))

			require.NoError(t, err)
			assert.Contains(t, ts.buf.String(), "Usage:")
		})
	}
}
