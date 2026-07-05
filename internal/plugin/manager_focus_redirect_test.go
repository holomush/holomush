// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
	plugins "github.com/holomush/holomush/internal/plugin"
)

func registerBareCommand(t *testing.T, reg *command.Registry, name string) {
	t.Helper()
	// The command only needs to EXIST so CollectFocusRedirects's registry.Get
	// finds the target. Set PluginName so the entry satisfies BOTH checks: the
	// NewTestEntry construction (types.go:670) AND Registry.Register's own
	// independent guard (registry.go:39 — Handler()==nil && PluginName()=="" →
	// ErrNilHandler). NewTestEntry returns a CommandEntry value, not a pointer.
	require.NoError(t, reg.Register(command.NewTestEntry(command.CommandEntryConfig{
		Name: name, PluginName: "core-" + name,
	})))
}

func TestCollectFocusRedirectsBuildsVerbKeyedTable(t *testing.T) {
	reg := command.NewRegistry()
	registerBareCommand(t, reg, "scene")
	discovered := []*plugins.DiscoveredPlugin{
		{Manifest: &plugins.Manifest{Name: "core-scenes", FocusRedirects: []plugins.FocusRedirect{
			{FocusKind: "scene", Verbs: []string{"pose", "say"}, TargetCommand: "scene"},
		}}},
	}
	table, err := plugins.CollectFocusRedirects(discovered, reg)
	require.NoError(t, err)
	target, ok := table.Target("pose", "scene")
	assert.True(t, ok)
	assert.Equal(t, "scene", target)
	_, ok = table.Target("say", "scene")
	assert.True(t, ok)
}

func TestCollectFocusRedirectsRejectsUnknownTargetCommand(t *testing.T) {
	reg := command.NewRegistry() // "scene" NOT registered
	discovered := []*plugins.DiscoveredPlugin{
		{Manifest: &plugins.Manifest{Name: "core-scenes", FocusRedirects: []plugins.FocusRedirect{
			{FocusKind: "scene", Verbs: []string{"pose"}, TargetCommand: "scene"},
		}}},
	}
	_, err := plugins.CollectFocusRedirects(discovered, reg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "target")
}

func TestCollectFocusRedirectsRejectsDuplicateVerbKind(t *testing.T) {
	reg := command.NewRegistry()
	registerBareCommand(t, reg, "scene")
	registerBareCommand(t, reg, "arena")
	discovered := []*plugins.DiscoveredPlugin{
		{Manifest: &plugins.Manifest{Name: "core-scenes", FocusRedirects: []plugins.FocusRedirect{
			{FocusKind: "scene", Verbs: []string{"pose"}, TargetCommand: "scene"},
		}}},
		{Manifest: &plugins.Manifest{Name: "core-arena", FocusRedirects: []plugins.FocusRedirect{
			{FocusKind: "scene", Verbs: []string{"pose"}, TargetCommand: "arena"},
		}}},
	}
	_, err := plugins.CollectFocusRedirects(discovered, reg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestCollectFocusRedirectsTrimsWhitespacePaddedVerbs(t *testing.T) {
	reg := command.NewRegistry()
	registerBareCommand(t, reg, "scene")
	discovered := []*plugins.DiscoveredPlugin{
		{Manifest: &plugins.Manifest{Name: "core-scenes", FocusRedirects: []plugins.FocusRedirect{
			{FocusKind: "scene", Verbs: []string{" pose "}, TargetCommand: "scene"},
		}}},
	}
	table, err := plugins.CollectFocusRedirects(discovered, reg)
	require.NoError(t, err)
	_, ok := table.Target("pose", "scene") // looked up by the trimmed key
	assert.True(t, ok, "a whitespace-padded manifest verb must be stored under its trimmed key")
}
