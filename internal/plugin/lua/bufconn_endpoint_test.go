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

	"github.com/holomush/holomush/internal/access/policy/policytest"
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
// so the SessionService server has a real backing for round-trip tests, plus an
// AllowAll ABAC engine. The engine is required because the capability
// interceptor now runs the default-deny ABAC decision for EVERY declared
// non-exempt capability (holomush-kplrr, INV-PLUGIN-50), not just scope-eligible
// ones — without it a non-scoped capability round-trip fails closed with
// EVALUATE_NO_ENGINE. Production wires the real engine via
// hostfunc.WithEngine(cfg.ABAC.Engine()) (setup/subsystem.go). Other optional
// dependencies are left nil (fail-closed per the adapter).
func newTestFunctions(t *testing.T) *hostfunc.Functions {
	t.Helper()
	return hostfunc.New(nil,
		hostfunc.WithSessionAccess(&fakeSessionAccessForEndpoint{}),
		hostfunc.WithEngine(policytest.AllowAllEngine()))
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
			// and it is non-scoped (no dispatch token needed). It IS subject to the
			// default-deny ABAC decision (INV-PLUGIN-50); newTestFunctions wires an
			// AllowAll engine so the declared session capability is permitted.
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

// TestHostCapBridgeInjection verifies the DECLARATION gate of the now-unconditional
// host-brokered capability path (holomush-eykuh.4): a plugin that DECLARES the kv
// capability receives the kv Lua global in DeliverEvent (end-to-end Load →
// DeliverEvent → RegisterHostCaps), while a plugin that declares NO capability
// sees no host-cap global. After the atomic cutover there is no allowlist opt-in —
// the manifest declaration (filtered through the resolver grant set) IS the gate.
//
// kv is chosen because it has no ABAC-engine or dispatch-token requirement, so
// the RPC can be exercised without a full production stack. Each plugin's Lua
// body asserts the expected presence/absence of the kv global and raises a Lua
// error on mismatch, so a wrong injection surfaces as a DeliverEvent error.
func TestHostCapBridgeInjection(t *testing.T) {
	const declaredBody = `
function on_event(event)
    if type(kv) == "table" then
        return nil
    end
    error("expected kv table, got " .. type(kv))
end
`
	const undeclaredBody = `
function on_event(event)
    if kv ~= nil then
        error("kv must not be injected for a plugin that did not declare it, got " .. type(kv))
    end
    return nil
end
`
	tests := []struct {
		name string
		// pluginName is the plugin under test; declares controls whether the
		// manifest declares the kv capability (the declaration gate).
		pluginName string
		declares   bool
		luaBody    string
	}{
		{
			name:       "plugin declaring kv receives the kv host-cap global",
			pluginName: "kv-declaring-test",
			declares:   true,
			luaBody:    declaredBody,
		},
		{
			name:       "plugin declaring no capability receives no host-cap global",
			pluginName: "no-caps-plugin",
			declares:   false,
			luaBody:    undeclaredBody,
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
			}
			if tc.declares {
				manifest.Requires = []plugins.Dependency{
					{Kind: plugins.DependencyCapability, Name: "kv"},
				}
			}

			host := NewHostWithFunctions(
				hostfunc.New(nil),
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

// TestActorStampReachesServerSideThroughRealEndpoint proves that the
// newActorStampInterceptor (holomush-eykuh.4.5) stamps {ActorPlugin, pluginName}
// onto the server-side handler context through the REAL newPluginEndpoint wiring
// — not just the isolated interceptor unit. This closes the gap identified in
// eykuh.2.11: without the interceptor, luaHostCapAdapter.LookupActor returned
// ACTOR_NOT_FOUND because plugins.NewInProcessConn drops context values and the
// server-side ctx was bare.
//
// Proof shape: EvalService.Evaluate calls LookupActor internally. With the
// actor stamp interceptor installed (the production path), LookupActor succeeds
// because {ActorPlugin, pluginName} is stamped on ctx by the interceptor before
// the handler runs. The call then proceeds past the identity seam and reaches the
// ABAC engine (AllowAllEngine → Allowed: true). Without the interceptor the call
// would fail at LookupActor with ACTOR_NOT_FOUND, never reaching the engine.
//
// This test exercises the real newPluginEndpoint path (actor interceptor + capability
// interceptor + LuaDefaultSet servers), proving actor transport through the
// production wiring — not via a fake interceptor or stub.
func TestActorStampReachesServerSideThroughRealEndpoint(t *testing.T) {
	const pluginName = "actor-proof-plugin"

	// Wire an AllowAllEngine so EvalService.Evaluate proceeds past the engine-nil
	// guard and actually calls LookupActor. Without the actor stamp, LookupActor
	// returns ACTOR_NOT_FOUND; with it, LookupActor succeeds and the engine allows.
	adapter := newLuaHostCapAdapter(hostfunc.New(nil, hostfunc.WithEngine(policytest.AllowAllEngine())))

	// Declare the "eval" capability so the capability interceptor permits the call.
	ep, err := newPluginEndpoint(adapter, &plugins.Manifest{
		Name: pluginName,
		Requires: []plugins.Dependency{
			{Kind: plugins.DependencyCapability, Name: "eval"},
		},
	})
	require.NoError(t, err)
	defer ep.Close()

	client := hostv1.NewEvalServiceClient(ep.Conn())
	// Use "command:foo" as the resource: the "command" type has a carve-out in
	// pluginauthz.Evaluate (commandResourceType) that bypasses the owned-types
	// entitlement check. Lua plugins own no resource types, so any plugin-typed
	// resource (e.g. "scene:…") would fail EVALUATE_UNENTITLED_TYPE before the
	// engine is consulted. The command carve-out lets us reach the engine and
	// confirm the actor identity seam is passed.
	resp, err := client.Evaluate(context.Background(), &hostv1.EvaluateRequest{
		Action:   "execute",
		Resource: "command:foo",
	})

	// The call must succeed: the actor stamp interceptor stamped {ActorPlugin,
	// pluginName} → LookupActor found the actor → entitlement carve-out for
	// "command" → AllowAllEngine allowed → response returned. Any error here means
	// the actor did NOT reach the server side (e.g. ACTOR_NOT_FOUND from LookupActor).
	require.NoError(t, err,
		"EvalService.Evaluate must succeed when the actor stamp interceptor provides the identity: "+
			"an error means the actor did not reach the server-side handler context")
	require.NotNil(t, resp)
	assert.True(t, resp.GetAllowed(),
		"AllowAllEngine must produce an allow once the identity seam is passed")
}
