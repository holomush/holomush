// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// grantAuthorityCap is a real, vocab-known capability token ("kv") that ALSO
// carries a luabridge host-cap binding (luabridge/bindings_gen.go: "kv" ->
// registerKVService), so on the Lua side its presence is observable as the
// injected global `kv`. It is the capability the manifest DECLARES on both
// runtimes; the resolver grant set is what decides whether each shim keeps it.
const grantAuthorityCap = "kv"

// probeKVGlobalLua is a Lua plugin body that returns ONE emit event IFF the
// host-cap global for grantAuthorityCap ("kv") was injected into the VM. The
// emit event is the observable witness: a NON-empty DeliverEvent result means
// the `kv` cap reached the bridge (it was in declaredCaps); an EMPTY result
// means the Lua shim dropped it. The emit table uses the canonical
// {subject,type,payload} shape read by parseEmitEvents.
const probeKVGlobalLua = `
function on_event(event)
    if kv ~= nil then
        return {
            {
                subject = "location.01HLOC0000000000000000000",
                type    = "test:kv-present",
                payload = '{"kv":"present"}',
            }
        }
    end
    return nil
end
`

// loadProbeLuaPlugin writes probeKVGlobalLua into a temp dir under a manifest
// that DECLARES the grantAuthorityCap capability, then loads it into host.
func loadProbeLuaPlugin(t *testing.T, host *pluginlua.Host, name string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probeKVGlobalLua), 0o600))

	manifest := &plugins.Manifest{
		Name:      name,
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
		Requires: []plugins.Dependency{
			{Kind: plugins.DependencyCapability, Name: grantAuthorityCap},
		},
	}
	require.NoError(t, host.Load(context.Background(), manifest, dir),
		"Lua plugin declaring the kv capability must load")
}

// luaKVGlobalInjected drives DeliverEvent and reports whether the `kv` host-cap
// global was injected — observed via the probe plugin's emit return. A non-empty
// emit slice means the bridge injected `kv` (the cap survived the grant filter);
// an empty slice means the Lua shim dropped it.
func luaKVGlobalInjected(t *testing.T, host *pluginlua.Host, name string) bool {
	t.Helper()
	// Stamp a host-vouched plugin actor on ctx, mirroring the production
	// Subscriber / actor-stamp interceptor (see pluginparity/lua_emit_test.go).
	dispatchCtx := core.WithActor(context.Background(), core.Actor{
		Kind: core.ActorPlugin,
		ID:   core.NewULID().String(),
	})
	emits, err := host.DeliverEvent(dispatchCtx, name, pluginsdk.Event{
		ID:     core.NewULID().String(),
		Stream: "location.01HLOC0000000000000000000",
		Type:   "test:probe",
	})
	require.NoError(t, err, "DeliverEvent must succeed — the probe body is valid")
	return len(emits) > 0
}

// binaryDeclaredCaps loads a binary plugin declaring grantAuthorityCap and
// returns the DeclaredCapabilities the host sent to InitRequest — the binary
// shim's observable consumption of the grant set.
func binaryDeclaredCaps(t *testing.T, host *Host, mockClient *mockPluginClient, name string) []string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, createTempExecutable(filepath.Join(dir, name)))

	manifest := &plugins.Manifest{
		Name:         name,
		Version:      "1.0.0",
		Type:         plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{Executable: name},
		Requires: []plugins.Dependency{
			{Kind: plugins.DependencyCapability, Name: grantAuthorityCap},
		},
	}
	require.NoError(t, host.Load(context.Background(), manifest, dir),
		"binary plugin declaring the kv capability must load")

	grpcClient := grpcMockFor(mockClient)
	require.NotNil(t, grpcClient.initReq, "Init must be called")
	require.NotNil(t, grpcClient.initReq.Config, "ServiceConfig must be set")
	return grpcClient.initReq.Config.GetDeclaredCapabilities()
}

// TestResolverGrantSetIsTheSingleAuthorityForBothRuntimes proves the
// resolver-as-single-grant-authority half of INV-PLUGIN-45 (spec R3 / ADR
// holomush-vpg8l): both the binary and Lua shims MUST consume the resolver's
// per-plugin Grants[pluginName] as the authority for what capabilities are
// declared/injected, instead of independently re-deriving from the manifest.
//
// The existing integration binding
// (test/integration/pluginparity/least_privilege_gate_test.go) proves both
// runtimes share the same interceptor CONSTRUCTOR
// (hostcap.DeclaredAccessFromManifest). It does NOT prove the resolver Grants
// set is what the shims consume — it builds DeclaredAccess straight from the
// manifest, bypassing SetPluginGrants entirely. This test closes that gap.
//
// Construction (the reviewer's recipe): both plugins DECLARE the "kv"
// capability in their manifest, but the threaded grant set EXCLUDES it. The
// single grant authority is one shared map fed to BOTH hosts' SetPluginGrants
// (plugins.PluginGrantsConfigurer — the same interface Manager.LoadAll calls
// with res.Grants). If either shim re-derived from the manifest instead of the
// grants, it would keep "kv" — the assertions below would fail.
//
// Non-vacuous: the positive contrast (grant set INCLUDES "kv") proves the cap
// IS kept by both shims when granted, so the negative result is the grant
// filter firing, not an unrelated absence.
//
// Verifies: INV-PLUGIN-45
func TestResolverGrantSetIsTheSingleAuthorityForBothRuntimes(t *testing.T) {
	t.Run("grant set EXCLUDING a declared cap drops it on both runtimes", func(t *testing.T) {
		const luaName = "lua-grant-excluded"
		const binName = "bin-grant-excluded"

		// The single grant authority: a resolver-shaped grant set that does NOT
		// list grantAuthorityCap for either plugin (empty entry = "granted
		// nothing"). This is the shape res.Grants would carry for a plugin whose
		// declared dep the resolver excluded.
		grants := map[string][]string{
			luaName: {},
			binName: {},
		}

		// --- Lua shim ---
		luaHost := pluginlua.NewHostWithFunctions(hostfunc.New(nil))
		t.Cleanup(func() { _ = luaHost.Close(context.Background()) })
		// SetPluginGrants is plugins.PluginGrantsConfigurer — the SAME entry point
		// Manager.LoadAll uses to thread res.Grants into every host.
		luaHost.SetPluginGrants(grants)
		loadProbeLuaPlugin(t, luaHost, luaName)
		assert.False(t, luaKVGlobalInjected(t, luaHost, luaName),
			"Lua shim MUST drop the kv cap when the resolver grant set excludes it — the grant set, not the manifest, is the authority (INV-PLUGIN-45)")

		// --- Binary shim ---
		binHost, mockClient := newMockHost(t)
		t.Cleanup(func() { _ = binHost.Close(context.Background()) })
		binHost.SetPluginGrants(grants)
		declared := binaryDeclaredCaps(t, binHost, mockClient, binName)
		assert.NotContains(t, declared, grantAuthorityCap,
			"binary shim MUST drop the kv cap when the resolver grant set excludes it — the grant set, not the manifest, is the authority (INV-PLUGIN-45)")
	})

	t.Run("grant set INCLUDING the declared cap keeps it on both runtimes (non-vacuous contrast)", func(t *testing.T) {
		const luaName = "lua-grant-included"
		const binName = "bin-grant-included"

		// Same single grant authority shape, now GRANTING grantAuthorityCap.
		grants := map[string][]string{
			luaName: {grantAuthorityCap},
			binName: {grantAuthorityCap},
		}

		// --- Lua shim ---
		luaHost := pluginlua.NewHostWithFunctions(hostfunc.New(nil))
		t.Cleanup(func() { _ = luaHost.Close(context.Background()) })
		luaHost.SetPluginGrants(grants)
		loadProbeLuaPlugin(t, luaHost, luaName)
		assert.True(t, luaKVGlobalInjected(t, luaHost, luaName),
			"Lua shim MUST inject the kv cap when the resolver grant set includes it (proves the negative case is the grant filter firing)")

		// --- Binary shim ---
		binHost, mockClient := newMockHost(t)
		t.Cleanup(func() { _ = binHost.Close(context.Background()) })
		binHost.SetPluginGrants(grants)
		declared := binaryDeclaredCaps(t, binHost, mockClient, binName)
		assert.Contains(t, declared, grantAuthorityCap,
			"binary shim MUST declare the kv cap when the resolver grant set includes it (proves the negative case is the grant filter firing)")
	})
}
