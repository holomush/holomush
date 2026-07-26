// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integrationtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWithBuiltinCommandsRegistersQuitOnTheDefaultRegistry pins the seam
// WithBuiltinCommands exists to provide: the harness's default command registry
// is otherwise empty, so quit is undispatchable.
func TestWithBuiltinCommandsRegistersQuitOnTheDefaultRegistry(t *testing.T) {
	srv := Start(t, WithBuiltinCommands())
	defer srv.Stop()

	_, ok := srv.cmdRegistry.Get("quit")
	require.True(t, ok, "WithBuiltinCommands must register the compiled-in quit handler")
	_, ok = srv.cmdRegistry.Get("shutdown")
	require.True(t, ok, "WithBuiltinCommands must register the compiled-in shutdown handler")
}

// TestDefaultStartLeavesTheCommandRegistryEmpty is the negative control for the
// test above: without the option there is no quit to dispatch. It is what makes
// the positive assertion meaningful rather than vacuous.
func TestDefaultStartLeavesTheCommandRegistryEmpty(t *testing.T) {
	srv := Start(t)
	defer srv.Stop()

	_, ok := srv.cmdRegistry.Get("quit")
	require.False(t, ok,
		"the default harness registry must stay empty — if quit is present here, "+
			"the quit smoke spec in the privacy suite proves nothing")
}

// TestWithBuiltinCommandsComposesWithInTreePlugins asserts the documented
// interaction rule: setting BOTH options must start without panicking. The
// registrations WithBuiltinCommands makes are discarded when the plugin
// subsystem's registry is adopted wholesale, and that adopted registry already
// carries the same compiled-in handlers (the plugin subsystem calls RegisterAll
// on its own registry), so quit stays dispatchable either way.
//
// A double-registration would panic through Registry.Register, so reaching the
// assertions at all is half the point of this test.
//
// Plugin-gated: skips when binary plugins are unbuilt (HOLOMUSH_REQUIRE_PLUGINS
// forces failure instead — see plugins.go).
func TestWithBuiltinCommandsComposesWithInTreePlugins(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []StartOption
	}{
		{"builtins-then-plugins", []StartOption{WithBuiltinCommands(), WithInTreePlugins()}},
		{"plugins-then-builtins", []StartOption{WithInTreePlugins(), WithBuiltinCommands()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := Start(t, tc.opts...) // skips here if binary plugins unbuilt
			defer srv.Stop()

			_, ok := srv.cmdRegistry.Get("quit")
			require.True(t, ok,
				"quit must be dispatchable when both options are set, regardless of order")
		})
	}
}
