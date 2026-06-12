// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"testing"

	"github.com/stretchr/testify/require"

	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// TestHostBrokerServerServesFocusService pins the broker-server contract: the
// single broker *grpc.Server built by newPluginHostServiceServer registers ALL
// the capability-scoped host.v1 services AND, during the migration window
// (until Task 12), the legacy god-service. GetServiceInfo is the wire-level
// proof that a binary plugin can reach each service over the one broker conn.
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
	// Old service still registered during migration (deleted in Task 12).
	require.Contains(t, info, "holomush.plugin.v1.PluginHostService")
}

// TestPluginV1HostV1SharedTypeRoundTrip pins the 1:1 correspondence between the
// pluginv1 and hostv1 copies of the shared enums. host.v1 is a self-contained
// package (Task 2) with its OWN copies of FocusKind / FocusFailureReason /
// SettingScope / StreamReplayMode distinct from the pluginv1 types the *Host
// internals speak; the per-capability servers translate at their boundary. This
// table-driven test asserts every pluginv1 enum value has a hostv1 counterpart
// with the SAME numeric wire value, so a later one-sided edit to either proto
// (adding/renumbering a value) fails here loudly instead of silently producing a
// wrong translation.
func TestPluginV1HostV1SharedTypeRoundTrip(t *testing.T) {
	t.Run("FocusKind values map 1:1 by wire number", func(t *testing.T) {
		cases := []struct {
			plugin pluginv1.FocusKind
			host   hostv1.FocusKind
		}{
			{pluginv1.FocusKind_FOCUS_KIND_UNSPECIFIED, hostv1.FocusKind_FOCUS_KIND_UNSPECIFIED},
			{pluginv1.FocusKind_FOCUS_KIND_SCENE, hostv1.FocusKind_FOCUS_KIND_SCENE},
		}
		require.Len(t, cases, len(pluginv1.FocusKind_name),
			"every pluginv1.FocusKind value must be covered")
		require.Len(t, cases, len(hostv1.FocusKind_name),
			"every hostv1.FocusKind value must be covered")
		for _, tc := range cases {
			require.Equal(t, int32(tc.plugin), int32(tc.host),
				"FocusKind %s wire value must match", tc.plugin)
			// Translation round-trips back to the same pluginv1 value.
			require.Equal(t, tc.host, focusKindToHostV1(focusKindFromHostV1(tc.host)))
		}
	})

	t.Run("FocusFailureReason values map 1:1 by wire number", func(t *testing.T) {
		cases := []struct {
			plugin pluginv1.FocusFailureReason
			host   hostv1.FocusFailureReason
		}{
			{pluginv1.FocusFailureReason_FOCUS_FAILURE_REASON_UNSPECIFIED, hostv1.FocusFailureReason_FOCUS_FAILURE_REASON_UNSPECIFIED},
			{pluginv1.FocusFailureReason_FOCUS_FAILURE_REASON_MEMBERSHIP_ABSENT, hostv1.FocusFailureReason_FOCUS_FAILURE_REASON_MEMBERSHIP_ABSENT},
			{pluginv1.FocusFailureReason_FOCUS_FAILURE_REASON_CONNECTION_NOT_FOUND, hostv1.FocusFailureReason_FOCUS_FAILURE_REASON_CONNECTION_NOT_FOUND},
		}
		require.Len(t, cases, len(pluginv1.FocusFailureReason_name),
			"every pluginv1.FocusFailureReason value must be covered")
		require.Len(t, cases, len(hostv1.FocusFailureReason_name),
			"every hostv1.FocusFailureReason value must be covered")
		for _, tc := range cases {
			require.Equal(t, int32(tc.plugin), int32(tc.host),
				"FocusFailureReason %s wire value must match", tc.plugin)
			require.Equal(t, tc.host, autoFocusFailureReasonToHostV1ByReason(tc.plugin))
		}
	})

	t.Run("SettingScope values map 1:1 by wire number", func(t *testing.T) {
		cases := []struct {
			plugin pluginv1.SettingScope
			host   hostv1.SettingScope
		}{
			{pluginv1.SettingScope_SETTING_SCOPE_UNSPECIFIED, hostv1.SettingScope_SETTING_SCOPE_UNSPECIFIED},
			{pluginv1.SettingScope_SETTING_SCOPE_GAME, hostv1.SettingScope_SETTING_SCOPE_GAME},
			{pluginv1.SettingScope_SETTING_SCOPE_PLAYER, hostv1.SettingScope_SETTING_SCOPE_PLAYER},
			{pluginv1.SettingScope_SETTING_SCOPE_CHARACTER, hostv1.SettingScope_SETTING_SCOPE_CHARACTER},
		}
		require.Len(t, cases, len(pluginv1.SettingScope_name),
			"every pluginv1.SettingScope value must be covered")
		require.Len(t, cases, len(hostv1.SettingScope_name),
			"every hostv1.SettingScope value must be covered")
		for _, tc := range cases {
			require.Equal(t, int32(tc.plugin), int32(tc.host),
				"SettingScope %s wire value must match", tc.plugin)
			// Translation round-trips host→plugin→host.
			require.Equal(t, tc.plugin, settingScopeToPluginV1(tc.host))
		}
	})

	t.Run("StreamReplayMode values map 1:1 by wire number", func(t *testing.T) {
		cases := []struct {
			plugin pluginv1.StreamReplayMode
			host   hostv1.StreamReplayMode
		}{
			{pluginv1.StreamReplayMode_STREAM_REPLAY_MODE_UNSPECIFIED, hostv1.StreamReplayMode_STREAM_REPLAY_MODE_UNSPECIFIED},
			{pluginv1.StreamReplayMode_STREAM_REPLAY_MODE_FROM_CURSOR, hostv1.StreamReplayMode_STREAM_REPLAY_MODE_FROM_CURSOR},
			{pluginv1.StreamReplayMode_STREAM_REPLAY_MODE_LIVE_ONLY, hostv1.StreamReplayMode_STREAM_REPLAY_MODE_LIVE_ONLY},
		}
		require.Len(t, cases, len(pluginv1.StreamReplayMode_name),
			"every pluginv1.StreamReplayMode value must be covered")
		require.Len(t, cases, len(hostv1.StreamReplayMode_name),
			"every hostv1.StreamReplayMode value must be covered")
		for _, tc := range cases {
			require.Equal(t, int32(tc.plugin), int32(tc.host),
				"StreamReplayMode %s wire value must match", tc.plugin)
		}
	})

	t.Run("FocusKey translates host→plugin preserving fields", func(t *testing.T) {
		hk := &hostv1.FocusKey{
			Kind:     hostv1.FocusKind_FOCUS_KIND_SCENE,
			TargetId: "01J0000000000000000000ABCD",
		}
		pk := focusKeyToPluginV1(hk)
		require.NotNil(t, pk)
		require.Equal(t, pluginv1.FocusKind_FOCUS_KIND_SCENE, pk.GetKind())
		require.Equal(t, hk.GetTargetId(), pk.GetTargetId())
		require.Nil(t, focusKeyToPluginV1(nil), "nil host key translates to nil")
	})
}
