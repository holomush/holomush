// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
)

// noopAppender is a core.EventAppender that discards events — enough to assert
// the SessionAdmin backing is wired (the backing's behavior is covered by the
// hostcap broadcaster tests).
type noopAppender struct{}

func (noopAppender) Append(context.Context, core.Event) error { return nil }

// TestConfigureSystemBroadcasterWiresLuaHostBackingFromAppender proves the late
// production wiring (holomush-eykuh.4.2): ConfigureSystemBroadcaster builds a
// system-broadcaster over the appender and threads it into the Lua host so the
// brokered SessionAdminService serves real broadcasts.
func TestConfigureSystemBroadcasterWiresLuaHostBackingFromAppender(t *testing.T) {
	s := &PluginSubsystem{
		luaHost: pluginlua.NewHostWithFunctions(hostfunc.New(nil)),
	}
	require.Nil(t, s.luaHost.HostCapabilitiesAdapter().SessionAdmin(), "unset before wiring")

	s.ConfigureSystemBroadcaster(noopAppender{})

	require.NotNil(t, s.luaHost.HostCapabilitiesAdapter().SessionAdmin(),
		"ConfigureSystemBroadcaster must wire the Lua host's SessionAdmin from the appender")
}

// TestConfigureSystemBroadcasterNoopWhenLuaHostUnset proves the call is safe
// before Start has built the Lua host (defensive nil-guard, mirroring
// Manager.ConfigureFocusDeps).
func TestConfigureSystemBroadcasterNoopWhenLuaHostUnset(t *testing.T) {
	s := &PluginSubsystem{}
	require.NotPanics(t, func() { s.ConfigureSystemBroadcaster(noopAppender{}) })
}
