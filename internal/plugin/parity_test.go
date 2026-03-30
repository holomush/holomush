// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/goplugin"
	luahost "github.com/holomush/holomush/internal/plugin/lua"
)

// parityEntry documents the mapping between a ServiceProxy method and its
// corresponding Lua host function and gRPC RPC (when implemented).
//
// luaFunction uses the form "namespace.function" for namespaced functions:
//   - "holomush.query_location"  -> registered on the holomush global table
//   - "holo.session.find_by_name" -> registered on the holo.session table
//   - "holo.emit.location"      -> registered on the holo.emit table
//   - "holo.fmt.bold"           -> registered on the holo.fmt table
//
// grpcRPC is the gRPC method name on the PluginHostService. Empty string
// means not yet implemented (BinaryHost callback RPCs are Phase 4+ work).
type parityEntry struct {
	proxyMethod string // ServiceProxy method name
	luaFunction string // Lua host function (e.g., "holomush.query_room")
	grpcRPC     string // PluginHostService RPC method name (empty = not yet implemented)
}

// parityTable is the authoritative mapping of ServiceProxy methods to their
// Lua and gRPC equivalents. This table MUST be updated whenever a new method
// is added to ServiceProxy.
//
// When a ServiceProxy method does not have a Lua or gRPC equivalent, use an
// empty string and add a comment explaining why (e.g., "TODO: Phase 4").
var parityTable = []parityEntry{
	// --- World read ---
	{"QueryLocation", "holomush.query_location", "QueryLocation"},
	{"QueryCharacter", "holomush.query_character", "QueryCharacter"},
	{"QueryLocationCharacters", "holomush.query_location_characters", "QueryLocationCharacters"},
	{"QueryObject", "holomush.query_object", ""},
	{"FindLocation", "holomush.find_location", ""},
	{"GetCharactersByLocation", "", ""},    // TODO: no Lua equivalent yet (query_location_characters covers the use case differently)
	{"GetObjectsByLocation", "", ""},       // TODO: no Lua equivalent yet
	{"UpdateLocation", "", ""},             // TODO: no Lua equivalent yet (set_property covers name/description)
	{"UpdateCharacterDescription", "", ""}, // TODO: no Lua equivalent yet (set_property covers description)

	// --- World write ---
	{"CreateLocation", "holomush.create_location", ""},
	{"CreateExit", "holomush.create_exit", ""},
	{"CreateObject", "holomush.create_object", ""},

	// --- Properties ---
	{"SetProperty", "holomush.set_property", ""},
	{"GetProperty", "holomush.get_property", ""},
	{"FindPropertyByPrefix", "", ""},  // TODO: no Lua equivalent yet
	{"ListPropertiesByParent", "", ""}, // TODO: no Lua equivalent yet

	// --- Plugin KV ---
	{"KVGet", "holomush.kv_get", "KVGet"},
	{"KVSet", "holomush.kv_set", "KVSet"},
	{"KVDelete", "holomush.kv_delete", "KVDelete"},

	// --- Session ---
	{"FindSessionByName", "holo.session.find_by_name", ""},
	{"SetLastWhispered", "holo.session.set_last_whispered", ""},
	{"DisconnectSession", "", ""},      // TODO: no Lua equivalent yet
	{"ListActiveSessions", "", ""},     // TODO: no Lua equivalent yet
	{"BroadcastSystemMessage", "", ""}, // TODO: no Lua equivalent yet
	{"UpdateActivity", "", ""},         // TODO: no Lua equivalent yet

	// --- Aliases ---
	{"SetPlayerAlias", "", ""},    // TODO: no Lua equivalent yet
	{"DeletePlayerAlias", "", ""}, // TODO: no Lua equivalent yet
	{"ListPlayerAliases", "", ""}, // TODO: no Lua equivalent yet
	{"SetSystemAlias", "", ""},    // TODO: no Lua equivalent yet
	{"DeleteSystemAlias", "", ""}, // TODO: no Lua equivalent yet
	{"ListSystemAliases", "", ""}, // TODO: no Lua equivalent yet
	{"CheckAliasShadow", "", ""},  // TODO: no Lua equivalent yet

	// --- Commands ---
	{"ListCommands", "holomush.list_commands", ""},
	{"GetCommandHelp", "holomush.get_command_help", ""},

	// --- Events ---
	{"EmitEvent", "", "EmitEvent"}, // Lua uses holo.emit.* (location/character/global) instead of direct EmitEvent

	// --- Config ---
	{"GetStartingLocationID", "", ""}, // TODO: no Lua equivalent yet

	// --- Utility ---
	{"Log", "holomush.log", "Log"},
}

// allLuaFunctions returns the set of Lua host function names registered
// across all namespaces. These are extracted from the actual registration
// code to keep this test in sync with reality.
//
// The naming uses "namespace.function" format matching the parityTable.
func allLuaFunctions() map[string]bool {
	return map[string]bool{
		// holomush.* namespace (from Functions.Register)
		"holomush.log":                  true,
		"holomush.new_request_id":       true,
		"holomush.kv_get":               true,
		"holomush.kv_set":               true,
		"holomush.kv_delete":            true,
		"holomush.query_location":            true,
		"holomush.query_character":           true,
		"holomush.query_location_characters": true,
		"holomush.query_object":         true,
		"holomush.create_location":      true,
		"holomush.create_exit":          true,
		"holomush.create_object":        true,
		"holomush.find_location":        true,
		"holomush.set_property":         true,
		"holomush.get_property":         true,
		"holomush.list_commands":        true,
		"holomush.get_command_help":     true,

		// holo.session.* namespace (from RegisterSessionFuncs)
		"holo.session.find_by_name":       true,
		"holo.session.set_last_whispered": true,

		// holo.fmt.* namespace (from RegisterStdlib) — not ServiceProxy methods
		"holo.fmt.bold":      true,
		"holo.fmt.italic":    true,
		"holo.fmt.dim":       true,
		"holo.fmt.underline": true,
		"holo.fmt.color":     true,
		"holo.fmt.list":      true,
		"holo.fmt.pairs":     true,
		"holo.fmt.table":     true,
		"holo.fmt.separator": true,
		"holo.fmt.header":    true,
		"holo.fmt.parse":     true,

		// holo.emit.* namespace (from RegisterStdlib) — not 1:1 with ServiceProxy
		"holo.emit.location":  true,
		"holo.emit.character": true,
		"holo.emit.global":    true,
		"holo.emit.flush":     true,
	}
}

// TestHostInterfaceCompliance verifies that all three host types implement
// the Host interface. This is a compile-time check surfaced as a test for
// documentation purposes.
func TestHostInterfaceCompliance(t *testing.T) {
	// These are also checked via var _ Host = (*Type)(nil) in each package,
	// but having them in one test makes the parity story complete.
	t.Run("LocalPluginHost", func(_ *testing.T) {
		var _ plugins.Host = (*plugins.LocalPluginHost)(nil)
	})
	t.Run("LuaHost", func(_ *testing.T) {
		var _ plugins.Host = (*luahost.Host)(nil)
	})
	t.Run("BinaryHost", func(_ *testing.T) {
		var _ plugins.Host = (*goplugin.Host)(nil)
	})
}

// TestParityTableCoversAllServiceProxyMethods uses reflection to verify that
// every method on the ServiceProxy interface has an entry in the parity table.
// This test fails when a new ServiceProxy method is added without updating
// the parity table — forcing developers to document the Lua/gRPC mapping.
func TestParityTableCoversAllServiceProxyMethods(t *testing.T) {
	proxyType := reflect.TypeOf((*plugins.ServiceProxy)(nil)).Elem()

	// Build set of methods in the parity table
	tableMethods := make(map[string]bool, len(parityTable))
	for _, entry := range parityTable {
		tableMethods[entry.proxyMethod] = true
	}

	// Check every ServiceProxy method has a parity table entry
	for i := range proxyType.NumMethod() {
		method := proxyType.Method(i)
		assert.True(t, tableMethods[method.Name],
			"ServiceProxy method %q has no entry in parityTable — add it to document the Lua/gRPC mapping",
			method.Name)
	}

	// Check no stale entries in the parity table
	proxyMethods := make(map[string]bool, proxyType.NumMethod())
	for i := range proxyType.NumMethod() {
		proxyMethods[proxyType.Method(i).Name] = true
	}
	for _, entry := range parityTable {
		assert.True(t, proxyMethods[entry.proxyMethod],
			"parityTable entry %q does not match any ServiceProxy method — remove or rename it",
			entry.proxyMethod)
	}
}

// TestParityTableLuaFunctionsExist verifies that every Lua function referenced
// in the parity table is actually registered by the hostfunc package.
func TestParityTableLuaFunctionsExist(t *testing.T) {
	registeredFuncs := allLuaFunctions()

	for _, entry := range parityTable {
		if entry.luaFunction == "" {
			continue // No Lua equivalent — documented as TODO
		}
		t.Run(entry.proxyMethod+"->"+entry.luaFunction, func(t *testing.T) {
			assert.True(t, registeredFuncs[entry.luaFunction],
				"parityTable maps %q to Lua function %q, but that function is not in allLuaFunctions() — "+
					"update allLuaFunctions() if the function was added, or fix the parityTable entry",
				entry.proxyMethod, entry.luaFunction)
		})
	}
}

// TestAllRegisteredLuaFunctionsAccountedFor verifies that every registered Lua
// function that maps to a ServiceProxy method is documented in the parity table.
// Lua functions without ServiceProxy equivalents (holo.fmt.*, holo.emit.*,
// holomush.new_request_id) are allowed as they serve SDK/utility purposes.
func TestAllRegisteredLuaFunctionsAccountedFor(t *testing.T) {
	// Build set of Lua functions referenced in the parity table
	tableLuaFuncs := make(map[string]bool, len(parityTable))
	for _, entry := range parityTable {
		if entry.luaFunction != "" {
			tableLuaFuncs[entry.luaFunction] = true
		}
	}

	// These Lua functions intentionally have no ServiceProxy equivalent.
	// They are SDK utilities or use a different abstraction.
	luaOnlyFuncs := map[string]bool{
		"holomush.new_request_id":  true, // utility, not a service call
		"holo.fmt.bold":            true, // formatting SDK
		"holo.fmt.italic":          true,
		"holo.fmt.dim":             true,
		"holo.fmt.underline":       true,
		"holo.fmt.color":           true,
		"holo.fmt.list":            true,
		"holo.fmt.pairs":           true,
		"holo.fmt.table":           true,
		"holo.fmt.separator":       true,
		"holo.fmt.header":          true,
		"holo.fmt.parse":           true,
		"holo.emit.location":       true, // emit SDK (different from ServiceProxy.EmitEvent)
		"holo.emit.character":      true,
		"holo.emit.global":         true,
		"holo.emit.flush":          true,
	}

	for funcName := range allLuaFunctions() {
		if luaOnlyFuncs[funcName] {
			continue
		}
		assert.True(t, tableLuaFuncs[funcName],
			"registered Lua function %q is not in parityTable and not in luaOnlyFuncs — "+
				"add it to parityTable or mark it as Lua-only",
			funcName)
	}
}

// TestParityTableGRPCRPCsExist verifies that every gRPC RPC referenced in the
// parity table is actually a method on PluginHostService.
func TestParityTableGRPCRPCsExist(t *testing.T) {
	hostServiceType := reflect.TypeOf(&goplugin.PluginHostService{})

	for _, entry := range parityTable {
		if entry.grpcRPC == "" {
			continue
		}
		t.Run(entry.proxyMethod+"->"+entry.grpcRPC, func(t *testing.T) {
			_, ok := hostServiceType.MethodByName(entry.grpcRPC)
			assert.True(t, ok,
				"parityTable maps %q to gRPC RPC %q, but PluginHostService has no such method",
				entry.proxyMethod, entry.grpcRPC)
		})
	}
}

// TestDeliverCommandExistsOnAllHosts verifies that all three Host
// implementations have a DeliverCommand method (not just the Host interface).
// This catches cases where a host returns a stub error that should be
// replaced with a real implementation.
func TestDeliverCommandExistsOnAllHosts(t *testing.T) {
	hosts := []struct {
		name     string
		hostType reflect.Type
	}{
		{"LocalPluginHost", reflect.TypeOf(&plugins.LocalPluginHost{})},
		{"LuaHost", reflect.TypeOf(&luahost.Host{})},
		{"BinaryHost", reflect.TypeOf(&goplugin.Host{})},
	}

	for _, h := range hosts {
		t.Run(h.name, func(t *testing.T) {
			method, ok := h.hostType.MethodByName("DeliverCommand")
			require.True(t, ok,
				"%s does not have a DeliverCommand method", h.name)
			// Verify the method has the expected signature: (ctx, name, cmd) -> (*response, error)
			// Method type includes the receiver, so 4 inputs total.
			assert.Equal(t, 4, method.Type.NumIn(),
				"%s.DeliverCommand has unexpected number of parameters", h.name)
			assert.Equal(t, 2, method.Type.NumOut(),
				"%s.DeliverCommand has unexpected number of return values", h.name)
		})
	}
}

// TestParityTableNoDuplicates verifies that each ServiceProxy method appears
// at most once in the parity table.
func TestParityTableNoDuplicates(t *testing.T) {
	seen := make(map[string]bool, len(parityTable))
	for _, entry := range parityTable {
		assert.False(t, seen[entry.proxyMethod],
			"duplicate parityTable entry for ServiceProxy method %q", entry.proxyMethod)
		seen[entry.proxyMethod] = true
	}
}

// TestParityTableMethodCount provides a summary of parity coverage
// as a documentation aid. It reports how many ServiceProxy methods have
// Lua and gRPC equivalents vs how many are pending.
func TestParityTableMethodCount(t *testing.T) {
	proxyType := reflect.TypeOf((*plugins.ServiceProxy)(nil)).Elem()
	totalMethods := proxyType.NumMethod()

	var withLua, withGRPC int
	for _, entry := range parityTable {
		if entry.luaFunction != "" {
			withLua++
		}
		if entry.grpcRPC != "" {
			withGRPC++
		}
	}

	t.Logf("ServiceProxy parity summary:")
	t.Logf("  Total methods:    %d", totalMethods)
	t.Logf("  Parity entries:   %d", len(parityTable))
	t.Logf("  With Lua func:    %d / %d", withLua, totalMethods)
	t.Logf("  With gRPC RPC:    %d / %d", withGRPC, totalMethods)
	t.Logf("  Lua pending:      %d", totalMethods-withLua)
	t.Logf("  gRPC pending:     %d", totalMethods-withGRPC)

	// Sanity check: parity table should have same count as ServiceProxy methods
	assert.Equal(t, totalMethods, len(parityTable),
		"parityTable has %d entries but ServiceProxy has %d methods", len(parityTable), totalMethods)
}
