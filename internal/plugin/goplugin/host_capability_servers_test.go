// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// TestHostBrokerServerServesFocusService pins the broker-server contract: the
// single broker *grpc.Server built by newPluginHostServiceServer registers ALL
// the capability-scoped host.v1 services (and no longer the deleted monolithic
// god-service, holomush-eykuh.1 Task 12). GetServiceInfo is the wire-level proof
// that a binary plugin can reach each service over the one broker conn.
func TestHostBrokerServerServesFocusService(t *testing.T) {
	h := NewHost() // constructor at internal/plugin/goplugin/host.go:280
	build := newPluginHostServiceServer(h, &plugins.Manifest{Name: "test-plugin"})
	srv := build(nil)
	t.Cleanup(srv.Stop)

	info := srv.GetServiceInfo()
	require.Contains(t, info, "holomush.plugin.host.v1.FocusService")
	require.Contains(t, info, "holomush.plugin.host.v1.EmitService")
	require.Contains(t, info, "holomush.plugin.host.v1.EvalService")
	require.Contains(t, info, "holomush.plugin.host.v1.SettingsService")
	require.Contains(t, info, "holomush.plugin.host.v1.StreamHistoryService")
	require.Contains(t, info, "holomush.plugin.host.v1.StreamSubscriptionService")
	require.Contains(t, info, "holomush.plugin.host.v1.AuditService")
	require.Contains(t, info, "holomush.plugin.host.v1.CommandRegistryService")
	require.Contains(t, info, "holomush.plugin.host.v1.KVService")
	// The legacy monolithic holomush.plugin.v1.PluginHostService is gone
	// (holomush-eykuh.1, Task 12): only the capability-scoped host.v1 services
	// are registered on the broker now.
	require.NotContains(t, info, "holomush.plugin.v1.PluginHostService")

	// The 5 Lua-only host.v1 services are declared in the host/v1 protos but
	// INTENTIONALLY NOT registered on the binary broker — they have no binary
	// consumer, so BinaryDefaultSet omits them and LuaDefaultSet adds them
	// (hostcap.register.go). Pin the omission so a future accidental addition to
	// the binary set is caught here.
	require.NotContains(t, info, "holomush.plugin.host.v1.PropertyService")
	require.NotContains(t, info, "holomush.plugin.host.v1.SessionService")
	require.NotContains(t, info, "holomush.plugin.host.v1.SessionAdminService")
	require.NotContains(t, info, "holomush.plugin.host.v1.WorldQueryService")
	require.NotContains(t, info, "holomush.plugin.host.v1.WorldMutationService")
}

// TestBinaryHostServerDeniesUndeclaredCapability proves the capability
// interceptor is installed on the binary broker server: a plugin whose manifest
// declares NO capabilities is denied (fail-closed) when it calls a gated host.v1
// method (KVService.Get) over the real broker conn. The denial surfaces as a
// gRPC error carrying the CAPABILITY_NOT_DECLARED reason. The Lua-runtime mirror
// is internal/plugin/lua.TestLuaHostServerDeniesUndeclaredCapability; both stand
// up the REAL per-plugin server and dial the ACTUALLY-INSTALLED interceptor, so
// together they prove the trust gate is identical across runtimes.
//
// Verifies: INV-PLUGIN-45
func TestBinaryHostServerDeniesUndeclaredCapability(t *testing.T) {
	h := NewHost()
	// Manifest declares no capability requires — kv is therefore undeclared.
	build := newPluginHostServiceServer(h, &plugins.Manifest{Name: "no-caps-plugin"})
	srv := build(nil)

	conn, err := plugins.NewInProcessConn(srv)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	client := hostv1.NewKVServiceClient(conn)
	_, err = client.Get(context.Background(), &hostv1.GetRequest{Key: "k"})
	require.Error(t, err, "undeclared capability call must be denied by the interceptor")
	assert.Contains(t, err.Error(), "plugin did not declare capability")
}
