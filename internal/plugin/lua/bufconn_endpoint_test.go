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

// TestPluginEndpointServesHostCapsOverBufconn stands up an endpoint backed by a
// real (test) Functions adapter and makes a SessionService.ListActive round-trip
// call over the in-process bufconn. Asserts the call succeeds and the endpoint
// serves the Lua capability set (INV-PLUGIN-49).
//
// SessionService.ListActive is chosen because: (a) it has a real server
// implementation (session.go:ListActive), (b) fakeSessionAccessForEndpoint
// provides the required backing, and (c) the RPC requires no dispatch token or
// ABAC engine — only a configured SessionAccess, which newTestFunctions wires.
func TestPluginEndpointServesHostCapsOverBufconn(t *testing.T) {
	adapter := newLuaHostCapAdapter(newTestFunctions(t))
	ep, err := newPluginEndpoint(adapter, "echo-bot")
	require.NoError(t, err)
	defer ep.Close()

	client := hostv1.NewSessionServiceClient(ep.Conn())
	resp, err := client.ListActive(context.Background(), &hostv1.ListActiveRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Empty(t, resp.GetSessions()) // fakeSessionAccessForEndpoint returns nil slice
}

// TestPluginEndpointLifecycleLoadAndClose verifies that creating an endpoint
// and calling Close does not panic and that Conn is non-nil after construction.
func TestPluginEndpointLifecycleLoadAndClose(t *testing.T) {
	adapter := newLuaHostCapAdapter(newTestFunctions(t))
	ep, err := newPluginEndpoint(adapter, "test-plugin")
	require.NoError(t, err)
	assert.NotNil(t, ep.Conn())
	require.NoError(t, ep.Close())
}

// TestLuaHostReloadClosesSupersededEndpoint asserts that reloading a plugin
// (calling Host.Load with the same name) closes the prior bufconn endpoint so
// its goroutine + listener are not leaked. It captures the prior endpoint's
// Conn reference, triggers a reload, then attempts an RPC through the old Conn
// and expects an error — because the underlying server and listener have been
// stopped by the reload-close path.
//
// This test FAILS against the unfixed code: without the duplicate-name guard in
// Load, the old endpoint is silently overwritten without Close, so the old Conn
// remains alive and the RPC succeeds (no error returned), causing the assertion
// to fail.
func TestLuaHostReloadClosesSupersededEndpoint(t *testing.T) {
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

	// Capture the endpoint reference before reload. This is the "old" endpoint
	// that the reload must close. Accessing host.plugins is valid here because
	// this file is in package lua (white-box test).
	host.mu.RLock()
	oldEndpoint := host.plugins[manifest.Name].endpoint
	host.mu.RUnlock()
	require.NotNil(t, oldEndpoint, "expected a non-nil endpoint after first load")

	// Capture the old Conn so we can probe it after reload.
	oldConn := oldEndpoint.Conn()

	// Reload — Load should close oldEndpoint and create a fresh one.
	require.NoError(t, host.Load(context.Background(), manifest, dir))

	// The old conn's underlying server has been stopped. Any RPC through it
	// must now return an error.
	client := hostv1.NewSessionServiceClient(oldConn)
	_, err := client.ListActive(context.Background(), &hostv1.ListActiveRequest{})
	assert.Error(t, err, "RPC on superseded (closed) endpoint conn must fail after reload")
}

// TestHostCapBridgeOptInInjectsKVGlobal verifies that a plugin opted into the
// bridge path via WithHostCapBridge receives the kv Lua global in DeliverEvent,
// confirming the end-to-end wiring from host option → DeliverEvent → RegisterHostCaps.
//
// kv is chosen because it has no ABAC-engine or dispatch-token requirement,
// so the RPC can be exercised without a full production stack.
func TestHostCapBridgeOptInInjectsKVGlobal(t *testing.T) {
	dir := t.TempDir()
	// The plugin asserts that the kv global exists and is a table; it stores the
	// result in a Lua global so the test can inspect it via DeliverEvent's
	// on_event return path. If kv is nil the plugin returns "missing_kv".
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.lua"), []byte(`
function on_event(event)
    if type(kv) == "table" then
        return nil
    end
    error("expected kv table, got " .. type(kv))
end
`), 0o600))

	manifest := &plugins.Manifest{
		Name:      "bridge-kv-test",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
		Requires: []plugins.Dependency{
			{Kind: plugins.DependencyCapability, Name: "kv"},
		},
	}

	host := NewHostWithFunctions(
		hostfunc.New(nil),
		WithHostCapBridge("bridge-kv-test"),
	)
	defer func() { _ = host.Close(context.Background()) }()

	require.NoError(t, host.Load(context.Background(), manifest, dir))

	_, err := host.DeliverEvent(context.Background(), "bridge-kv-test", pluginsdk.Event{
		ID: "01ABC", Stream: "location.1", Type: "test",
	})
	require.NoError(t, err, "DeliverEvent must not fail when kv global is present")
}

// TestHostCapBridgeOptedOutPluginUnaffected asserts that a plugin NOT in the
// WithHostCapBridge allowlist does not receive bridge globals. This is the
// coexistence guarantee: production plugins using the legacy hostfunc shim are
// never touched by bridge injection (spec §5 — production unaffected).
func TestHostCapBridgeOptedOutPluginUnaffected(t *testing.T) {
	dir := t.TempDir()
	// The plugin asserts that kv is NOT injected (it should be nil for an
	// opted-out plugin that has not declared it as a legacy service dep).
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.lua"), []byte(`
function on_event(event)
    if kv ~= nil then
        error("kv must not be injected for opted-out plugin, got " .. type(kv))
    end
    return nil
end
`), 0o600))

	manifest := &plugins.Manifest{
		Name:      "legacy-plugin",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
		Requires: []plugins.Dependency{
			{Kind: plugins.DependencyCapability, Name: "kv"},
		},
	}

	// WithHostCapBridge is NOT called for "legacy-plugin" — it opts in a
	// different plugin. The legacy-plugin must see no bridge injection.
	host := NewHostWithFunctions(
		hostfunc.New(nil),
		WithHostCapBridge("some-other-plugin"),
	)
	defer func() { _ = host.Close(context.Background()) }()

	require.NoError(t, host.Load(context.Background(), manifest, dir))

	_, err := host.DeliverEvent(context.Background(), "legacy-plugin", pluginsdk.Event{
		ID: "01ABC", Stream: "location.1", Type: "test",
	})
	require.NoError(t, err, "DeliverEvent must succeed for opted-out plugin without kv injection")
}
