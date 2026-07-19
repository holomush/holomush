// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
)

// noopPublisher is an eventbus.Publisher that discards events — enough to
// assert the SessionAdmin backing is wired (the backing's behavior is
// covered by the hostcap broadcaster tests).
type noopPublisher struct{}

func (noopPublisher) Publish(context.Context, eventbus.Event) error { return nil }

func mainGameID() string { return "main" }

// TestConfigureSystemBroadcasterWiresLuaHostBackingFromPublisher proves the late
// production wiring (holomush-eykuh.4.2): ConfigureSystemBroadcaster builds a
// system-broadcaster over the publisher and threads it into the Lua host so the
// brokered SessionAdminService serves real broadcasts.
func TestConfigureSystemBroadcasterWiresLuaHostBackingFromPublisher(t *testing.T) {
	s := &PluginSubsystem{
		luaHost: pluginlua.NewHostWithFunctions(hostfunc.New(nil)),
	}
	require.Nil(t, s.luaHost.HostCapabilitiesAdapter().SessionAdmin(), "unset before wiring")

	s.ConfigureSystemBroadcaster(noopPublisher{}, mainGameID)

	require.NotNil(t, s.luaHost.HostCapabilitiesAdapter().SessionAdmin(),
		"ConfigureSystemBroadcaster must wire the Lua host's SessionAdmin from the publisher")
}

// TestConfigureSystemBroadcasterNoopWhenLuaHostUnset proves the call is safe
// before Start has built the Lua host (defensive nil-guard, mirroring
// Manager.ConfigureFocusDeps).
func TestConfigureSystemBroadcasterNoopWhenLuaHostUnset(t *testing.T) {
	s := &PluginSubsystem{}
	require.NotPanics(t, func() { s.ConfigureSystemBroadcaster(noopPublisher{}, mainGameID) })
}
