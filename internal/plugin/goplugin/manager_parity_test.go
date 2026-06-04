// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	"github.com/holomush/holomush/pkg/errutil"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// TestManager_INVS5_ParityAcrossRuntimes verifies that INV-PLUGIN-32 validation is
// equally enforced on both runtimes — Lua and binary — per INV-PLUGIN-39 / INV-PLUGIN-31.
//
// Four scenarios x 2 runtimes = 8 subtests. The "host-owned-filtered"
// scenario verifies INV-PLUGIN-34 round-trips through both the Lua hostfunc path
// and the binary InitResponse.RegisteredEmitTypes path (a regression here
// would mean a new host-owned constant added to pkg/plugin/event.go without
// updating hostOwnedEmitTypes in the validator could silently break
// fail-closed semantics for legitimate plugins).
//
// In addition to verdict parity, each error case asserts the manager rolls
// back successful host.Load on validator rejection (INV-PLUGIN-35 fail-closed +
// rollback) — the rejected plugin MUST NOT appear in mgr.ListPlugins().
//
// This test lives in package goplugin (not _test) so it has direct access
// to newMockHost / mockGRPCPluginClient without exporting helpers.
func TestManager_INVS5_ParityAcrossRuntimes(t *testing.T) {
	type scenario struct {
		name       string
		declared   []string
		registered []string
		wantCode   string // empty = expect success
	}
	scenarios := []scenario{
		{name: "match", declared: []string{"a", "b"}, registered: []string{"a", "b"}, wantCode: ""},
		{name: "declared-but-unregistered", declared: []string{"a", "b"}, registered: []string{"a"}, wantCode: "EVENT_TYPE_REGISTRY_MISMATCH"},
		{name: "registered-but-undeclared", declared: []string{"a"}, registered: []string{"a", "b"}, wantCode: "EVENT_TYPE_REGISTRY_MISMATCH"},
		// INV-PLUGIN-34 round-trip: host-owned types (system, move) appear in the
		// registered set but MUST be filtered before set-equality. The plugin
		// declared only {a, b}; filtered registered set MUST equal {a, b}.
		{name: "host-owned-filtered", declared: []string{"a", "b"}, registered: []string{"a", "b", "system", "move"}, wantCode: ""},
	}

	for _, sc := range scenarios {
		t.Run("lua/"+sc.name, func(t *testing.T) {
			pluginName := "lua-" + sc.name
			dir := t.TempDir()
			pluginsDir := filepath.Join(dir, "plugins")
			writeINVS5LuaPlugin(t, pluginsDir, pluginName, sc.declared, sc.registered)

			luaHost := pluginlua.NewHostWithFunctions(hostfunc.New(nil))
			t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

			mgr, mgrErr := plugins.NewManager(
				pluginsDir,
				plugins.WithLuaHost(luaHost),
				plugins.WithVerbRegistry(core.NewVerbRegistry()),
			)
			require.NoError(t, mgrErr)

			err := mgr.LoadAll(context.Background())
			if sc.wantCode == "" {
				require.NoError(t, err)
				assert.Contains(t, mgr.ListPlugins(), pluginName)
			} else {
				require.Error(t, err)
				errutil.AssertErrorCode(t, err, sc.wantCode)
				// INV-PLUGIN-35 rollback regression: a rejected plugin MUST NOT
				// appear in the manager's plugin list. Without host.Unload
				// in manager.loadPlugin's error path, the host's plugin
				// table (and for binary, a live subprocess + gRPC client)
				// would leak.
				assert.NotContains(t, mgr.ListPlugins(), pluginName,
					"INV-PLUGIN-32 rejection MUST roll back: plugin %q should not appear in manager's plugin list after fail-closed", pluginName)
			}
		})

		t.Run("binary/"+sc.name, func(t *testing.T) {
			pluginName := "bin-" + sc.name
			dir := t.TempDir()
			pluginsDir := filepath.Join(dir, "plugins")
			writeINVS5BinaryPlugin(t, pluginsDir, pluginName, sc.declared)

			host, mockClient := newMockHost(t)
			grpcMockFor(mockClient).setInitResponse(&pluginv1.InitResponse{
				RegisteredEmitTypes: sc.registered,
			})
			t.Cleanup(func() { _ = host.Close(context.Background()) })

			mgr, mgrErr := plugins.NewManager(
				pluginsDir,
				plugins.WithVerbRegistry(core.NewVerbRegistry()),
			)
			require.NoError(t, mgrErr)
			mgr.RegisterHost(plugins.TypeBinary, host)

			err := mgr.LoadAll(context.Background())
			if sc.wantCode == "" {
				require.NoError(t, err)
				assert.Contains(t, mgr.ListPlugins(), pluginName)
			} else {
				require.Error(t, err)
				errutil.AssertErrorCode(t, err, sc.wantCode)
				// INV-PLUGIN-35 rollback regression: see lua/ subtest above.
				assert.NotContains(t, mgr.ListPlugins(), pluginName,
					"INV-PLUGIN-32 rejection MUST roll back: plugin %q should not appear in manager's plugin list after fail-closed", pluginName)
			}
		})
	}
}

// writeINVS5LuaPlugin writes a synthetic Lua plugin under pluginsDir whose
// main.lua calls register_emit_type with `registered` and whose plugin.yaml
// declares `declared` in crypto.emits.
func writeINVS5LuaPlugin(t *testing.T, pluginsDir, name string, declared, registered []string) {
	t.Helper()
	pluginDir := filepath.Join(pluginsDir, name)
	mkdir(t, pluginDir)

	var manifest strings.Builder
	manifest.WriteString("name: ")
	manifest.WriteString(name)
	manifest.WriteString("\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua\n")
	if len(declared) > 0 {
		manifest.WriteString("crypto:\n  emits:\n")
		for _, et := range declared {
			manifest.WriteString("    - event_type: " + et + "\n      sensitivity: never\n")
		}
	}
	writeManifest(t, filepath.Join(pluginDir, "plugin.yaml"), manifest.String())

	var main strings.Builder
	for _, et := range registered {
		main.WriteString(`holomush.register_emit_type("` + et + "\")\n")
	}
	main.WriteString("function on_event(event) return nil end\n")
	writeManifest(t, filepath.Join(pluginDir, "main.lua"), main.String())
}

// writeINVS5BinaryPlugin writes a synthetic binary plugin manifest with the
// declared crypto.emits block; the InitResponse.RegisteredEmitTypes flowing
// through the mock factory is configured by the caller.
func writeINVS5BinaryPlugin(t *testing.T, pluginsDir, name string, declared []string) {
	t.Helper()
	pluginDir := filepath.Join(pluginsDir, name)
	mkdir(t, pluginDir)

	var manifest strings.Builder
	manifest.WriteString("name: ")
	manifest.WriteString(name)
	manifest.WriteString("\nversion: 1.0.0\ntype: binary\nbinary-plugin:\n  executable: ")
	manifest.WriteString(name)
	manifest.WriteString("\n")
	if len(declared) > 0 {
		manifest.WriteString("crypto:\n  emits:\n")
		for _, et := range declared {
			manifest.WriteString("    - event_type: " + et + "\n      sensitivity: never\n")
		}
	}
	writeManifest(t, filepath.Join(pluginDir, "plugin.yaml"), manifest.String())

	// Drop a stub executable so Host.Load's lstat check passes.
	require.NoError(t, createTempExecutable(filepath.Join(pluginDir, name)))
}

func mkdir(t *testing.T, p string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(p, 0o750))
}

func writeManifest(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
