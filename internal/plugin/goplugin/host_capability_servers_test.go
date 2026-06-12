// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHostBrokerServerServesFocusService pins the broker-server contract: the
// single broker *grpc.Server built by newPluginHostServiceServer registers ALL
// the capability-scoped host.v1 services (and no longer the deleted monolithic
// god-service, holomush-eykuh.1 Task 12). GetServiceInfo is the wire-level proof
// that a binary plugin can reach each service over the one broker conn.
func TestHostBrokerServerServesFocusService(t *testing.T) {
	h := NewHost() // constructor at internal/plugin/goplugin/host.go:280
	build := newPluginHostServiceServer(h, "test-plugin")
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
}
