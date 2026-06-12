// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package luabridge

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

// expectedTokens is the host-capability token set the bindings must cover. It
// mirrors plugins.CapabilityServiceNames; keeping a literal copy here (rather
// than importing internal/plugin) keeps the luabridge package free of an import
// cycle while still pinning the exact token spellings.
var expectedTokens = []string{
	"audit", "command-registry", "emit", "eval", "focus", "kv",
	"property", "session", "session.admin", "settings",
	"stream.history", "stream.subscription", "world.mutation", "world.query",
}

// TestRegisteredHostCapBindingsCoversEveryCapabilityToken asserts the generated
// dispatch map is keyed by exactly the capability tokens (not service names) and
// covers the full vocabulary, so no host-capability service is missing a binding
// and no stray non-token key sneaks in.
func TestRegisteredHostCapBindingsCoversEveryCapabilityToken(t *testing.T) {
	got := make([]string, 0, len(registeredHostCapBindings))
	for token := range registeredHostCapBindings {
		got = append(got, token)
	}
	sort.Strings(got)

	want := append([]string(nil), expectedTokens...)
	sort.Strings(want)

	assert.Equal(t, want, got, "dispatch map must be keyed by capability tokens")
}

// TestRegisterBindingInjectsNamespaceTable runs one generated registrar against
// a fresh Lua state with a nil conn and asserts it installs a global table named
// after the capability token whose methods are callable Lua functions. (The conn
// is never dialed here — registration only builds the table; an actual method
// call would dial.)
func TestRegisterBindingInjectsNamespaceTable(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	register := registeredHostCapBindings["kv"]
	require.NotNil(t, register, "kv binding must be registered")

	register(L, nil, "echo-bot")

	tbl, ok := L.GetGlobal("kv").(*lua.LTable)
	require.True(t, ok, "kv global must be a table")
	assert.Equal(t, lua.LTFunction, L.GetField(tbl, "Get").Type(), "kv.Get must be a function")
}
