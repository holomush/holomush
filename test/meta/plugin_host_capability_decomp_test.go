// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Verifies: INV-PLUGIN-47
package meta

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPluginHostServiceIsDeleted asserts the god-service no longer exists and
// every former RPC is rehomed into a holomush.plugin.host.v1 service (or is the
// explicitly retired Log RPC).
func TestPluginHostServiceIsDeleted(t *testing.T) {
	proto, err := os.ReadFile("../../api/proto/holomush/plugin/v1/plugin.proto")
	require.NoError(t, err)
	assert.NotContains(t, string(proto), "service PluginHostService",
		"PluginHostService must be deleted (spec §5, INV-PLUGIN-47)")

	// Every former RPC resolves to a host.v1 service member under its NEW name
	// (KVGet→Get, KVSet→Set, KVDelete→Delete per the Orientation table; all
	// others keep their name). Log is retired, not rehomed (spec §4, §5).
	// Map: host.v1 RPC name (as written in the .proto) → file it must appear in.
	rehomed := map[string]string{
		"EmitEvent": "emit.proto", "RequestEmitToken": "emit.proto",
		"JoinFocus": "focus.proto", "LeaveFocus": "focus.proto", "LeaveFocusByTarget": "focus.proto",
		"PresentFocus": "focus.proto", "SetConnectionFocus": "focus.proto", "GetConnectionFocus": "focus.proto",
		"AutoFocusOnJoin": "focus.proto", "IsAnyConnFocused": "focus.proto",
		"Evaluate": "eval.proto", "DecryptOwnAuditRows": "audit.proto",
		"QueryStreamHistory": "stream.proto", "AddSessionStream": "stream.proto", "RemoveSessionStream": "stream.proto",
		"ListCommands": "command_registry.proto", "GetCommandHelp": "command_registry.proto",
		"GetSetting": "settings.proto", "SetSetting": "settings.proto",
		"Get": "kv.proto", "Set": "kv.proto", "Delete": "kv.proto",
	}
	for rpcName, file := range rehomed {
		body, err := os.ReadFile(filepath.Join("../../api/proto/holomush/plugin/host/v1", file))
		require.NoError(t, err, "rehome target %s missing", file)
		assert.Contains(t, string(body), "rpc "+rpcName+"(",
			"RPC %s must be declared in host/v1/%s (rehoming, INV-PLUGIN-47)", rpcName, file)
	}
	// The retired Log RPC must NOT reappear anywhere in host.v1.
	for _, file := range []string{"emit.proto", "kv.proto", "stream.proto"} {
		body, err := os.ReadFile(filepath.Join("../../api/proto/holomush/plugin/host/v1", file))
		require.NoError(t, err, "rehome target %s missing", file)
		assert.NotContains(t, string(body), "rpc Log(", "Log is retired, not rehomed (spec §4)")
	}
}
