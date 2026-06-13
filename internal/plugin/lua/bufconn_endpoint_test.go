// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/session"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// fakeSessionAccessForEndpoint is a minimal session.Access that returns an
// empty active-session list. Used so SessionService.ListActive has a real
// backing and the round-trip completes successfully.
type fakeSessionAccessForEndpoint struct{}

var _ session.Access = (*fakeSessionAccessForEndpoint)(nil)

func (f *fakeSessionAccessForEndpoint) ListActive(_ context.Context) ([]*session.Info, error) {
	return nil, nil
}

func (f *fakeSessionAccessForEndpoint) FindByCharacter(_ context.Context, _ ulid.ULID) (*session.Info, error) {
	return nil, nil
}

func (f *fakeSessionAccessForEndpoint) FindByCharacterName(_ context.Context, _ string) (*session.Info, error) {
	return nil, nil
}

func (f *fakeSessionAccessForEndpoint) DeleteByCharacter(_ context.Context, _ ulid.ULID) (*session.Info, error) {
	return nil, nil
}

func (f *fakeSessionAccessForEndpoint) UpdateActivity(_ context.Context, _ string) error {
	return nil
}

func (f *fakeSessionAccessForEndpoint) UpdateLastPaged(_ context.Context, _, _ string) error {
	return nil
}

func (f *fakeSessionAccessForEndpoint) UpdateLastWhispered(_ context.Context, _, _ string) error {
	return nil
}

// newTestFunctions builds a *hostfunc.Functions with a session.Access wired in
// so the SessionService server has a real backing for round-trip tests. All
// other optional dependencies are left nil (fail-closed per the adapter).
func newTestFunctions(t *testing.T) *hostfunc.Functions {
	t.Helper()
	return hostfunc.New(nil, hostfunc.WithSessionAccess(&fakeSessionAccessForEndpoint{}))
}

// TestPluginEndpoint groups the per-plugin bufconn endpoint behaviors as
// table-driven subtests. These scenarios genuinely exercise different endpoint
// code paths (a round-trip, the construct/close lifecycle, and reload-close), so
// each case carries its own setup/action/teardown in run rather than sharing one
// arrange/act/assert table — the {name, run} table keeps every behavior intact
// while satisfying the table-driven test convention.
func TestPluginEndpoint(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			// Stands up an endpoint backed by a real (test) Functions adapter and
			// makes a SessionService.ListActive round-trip over the in-process
			// bufconn, asserting the endpoint serves the Lua capability set
			// (INV-PLUGIN-49). ListActive is chosen because it has a real server
			// impl (session.go:ListActive), fakeSessionAccessForEndpoint backs it,
			// and it needs no dispatch token or ABAC engine.
			name: "serves host caps over the in-process bufconn",
			run: func(t *testing.T) {
				adapter := newLuaHostCapAdapter(newTestFunctions(t))
				// Declare the session capability so the interceptor permits the
				// ListActive round-trip below.
				ep, err := newPluginEndpoint(adapter, &plugins.Manifest{
					Name:     "echo-bot",
					Requires: []plugins.Dependency{{Kind: plugins.DependencyCapability, Name: "session"}},
				})
				require.NoError(t, err)
				defer ep.Close()

				client := hostv1.NewSessionServiceClient(ep.Conn())
				resp, err := client.ListActive(context.Background(), &hostv1.ListActiveRequest{})
				require.NoError(t, err)
				assert.NotNil(t, resp)
				assert.Empty(t, resp.GetSessions()) // fakeSessionAccessForEndpoint returns nil slice
			},
		},
		{
			// Creating an endpoint and calling Close does not panic, and Conn is
			// non-nil after construction.
			name: "exposes a non-nil conn and closes cleanly",
			run: func(t *testing.T) {
				adapter := newLuaHostCapAdapter(newTestFunctions(t))
				ep, err := newPluginEndpoint(adapter, &plugins.Manifest{Name: "test-plugin"})
				require.NoError(t, err)
				assert.NotNil(t, ep.Conn())
				require.NoError(t, ep.Close())
			},
		},
		{
			// Reloading a plugin (Host.Load with the same name) closes the prior
			// bufconn endpoint so its goroutine + listener are not leaked: the old
			// Conn's RPC must error after reload. Without the duplicate-name guard
			// in Load the old endpoint is overwritten without Close and the RPC
			// would still succeed.
			name: "closes the superseded endpoint on reload",
			run: func(t *testing.T) {
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "main.lua"),
					[]byte("function on_event(event) return nil end"), 0o600))

				host := NewHostWithFunctions(hostfunc.New(nil, hostfunc.WithSessionAccess(&fakeSessionAccessForEndpoint{})))
				defer func() { _ = host.Close(context.Background()) }()

				manifest := &plugins.Manifest{
					Name:      "reload-ep-test",
					Version:   "1.0.0",
					Type:      plugins.TypeLua,
					LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
				}

				// First load — endpoint is created.
				require.NoError(t, host.Load(context.Background(), manifest, dir))

				// Capture the "old" endpoint before reload. Accessing host.plugins
				// is valid here because this file is in package lua (white-box).
				host.mu.RLock()
				oldEndpoint := host.plugins[manifest.Name].endpoint
				host.mu.RUnlock()
				require.NotNil(t, oldEndpoint, "expected a non-nil endpoint after first load")

				// Capture the old Conn so we can probe it after reload.
				oldConn := oldEndpoint.Conn()

				// Reload — Load should close oldEndpoint and create a fresh one.
				require.NoError(t, host.Load(context.Background(), manifest, dir))

				// The old conn's underlying server has been stopped. Any RPC
				// through it must now return an error.
				client := hostv1.NewSessionServiceClient(oldConn)
				_, err := client.ListActive(context.Background(), &hostv1.ListActiveRequest{})
				assert.Error(t, err, "RPC on superseded (closed) endpoint conn must fail after reload")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, tc.run)
	}
}

// TestLuaHostServerDeniesUndeclaredCapability proves the capability interceptor
// is installed on the Lua per-plugin bufconn server identically to the binary
// broker server: a plugin whose manifest declares NO capabilities is denied
// (fail-closed) when it calls a gated host.v1 method (KVService.Get) over the
// in-process conn. The binary mirror is
// goplugin.TestBinaryHostServerDeniesUndeclaredCapability; both stand up the
// REAL per-plugin server and dial the ACTUALLY-INSTALLED interceptor built from
// the SAME hostcap.DeclaredAccessFromManifest helper, so together they prove the
// trust gate is identical across runtimes (plugin-runtime-symmetry).
//
// Verifies: INV-PLUGIN-45
func TestLuaHostServerDeniesUndeclaredCapability(t *testing.T) {
	adapter := newLuaHostCapAdapter(newTestFunctions(t))
	// Manifest declares no capability requires — kv is therefore undeclared.
	ep, err := newPluginEndpoint(adapter, &plugins.Manifest{Name: "no-caps-plugin"})
	require.NoError(t, err)
	defer ep.Close()

	client := hostv1.NewKVServiceClient(ep.Conn())
	_, err = client.Get(context.Background(), &hostv1.GetRequest{Key: "k"})
	require.Error(t, err, "undeclared capability call must be denied by the interceptor")
	assert.Contains(t, err.Error(), "plugin did not declare capability")
}

// TestHostCapBridgeInjection verifies the bridge opt-in/opt-out coexistence
// guarantee (spec §5): a plugin in the WithHostCapBridge allowlist receives the
// kv Lua global in DeliverEvent (end-to-end host option → DeliverEvent →
// RegisterHostCaps), while a plugin NOT in the allowlist sees no bridge global,
// so production plugins on the legacy hostfunc shim are never touched.
//
// kv is chosen because it has no ABAC-engine or dispatch-token requirement, so
// the RPC can be exercised without a full production stack. Each plugin's Lua
// body asserts the expected presence/absence of the kv global and raises a Lua
// error on mismatch, so a wrong injection surfaces as a DeliverEvent error.
func TestHostCapBridgeInjection(t *testing.T) {
	const optInBody = `
function on_event(event)
    if type(kv) == "table" then
        return nil
    end
    error("expected kv table, got " .. type(kv))
end
`
	const optOutBody = `
function on_event(event)
    if kv ~= nil then
        error("kv must not be injected for opted-out plugin, got " .. type(kv))
    end
    return nil
end
`
	tests := []struct {
		name string
		// pluginName is the plugin under test; bridgeAllowlist is the single
		// plugin opted into the bridge (equal to pluginName for the opt-in case,
		// a different name for the opt-out case).
		pluginName      string
		bridgeAllowlist string
		luaBody         string
	}{
		{
			name:            "opted-in plugin receives the kv bridge global",
			pluginName:      "bridge-kv-test",
			bridgeAllowlist: "bridge-kv-test",
			luaBody:         optInBody,
		},
		{
			name:            "opted-out plugin receives no bridge global",
			pluginName:      "legacy-plugin",
			bridgeAllowlist: "some-other-plugin",
			luaBody:         optOutBody,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "main.lua"), []byte(tc.luaBody), 0o600))

			manifest := &plugins.Manifest{
				Name:      tc.pluginName,
				Version:   "1.0.0",
				Type:      plugins.TypeLua,
				LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
				Requires: []plugins.Dependency{
					{Kind: plugins.DependencyCapability, Name: "kv"},
				},
			}

			host := NewHostWithFunctions(
				hostfunc.New(nil),
				WithHostCapBridge(tc.bridgeAllowlist),
			)
			defer func() { _ = host.Close(context.Background()) }()

			require.NoError(t, host.Load(context.Background(), manifest, dir))

			_, err := host.DeliverEvent(context.Background(), tc.pluginName, pluginsdk.Event{
				ID: "01ABC", Stream: "location.1", Type: "test",
			})
			require.NoError(t, err)
		})
	}
}
