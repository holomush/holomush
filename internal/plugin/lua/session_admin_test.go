// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/plugin/hostfunc"
)

// stubSessionAdmin is a no-op hostcap.SessionAdmin for wiring assertions.
type stubSessionAdmin struct{}

func (stubSessionAdmin) BroadcastSystemMessage(context.Context, string) error    { return nil }
func (stubSessionAdmin) DisconnectSession(context.Context, string, string) error { return nil }

// TestLuaAdapterSessionAdminReturnsBackingWhenWired proves the Lua adapter
// surfaces a wired SessionAdmin backing (holomush-eykuh.4.2) — the production
// path for the brokered SessionAdminService over the Lua bufconn endpoint.
func TestLuaAdapterSessionAdminReturnsBackingWhenWired(t *testing.T) {
	a := newLuaHostCapAdapterWithSessionAdmin(hostfunc.New(nil), stubSessionAdmin{})
	require.NotNil(t, a.SessionAdmin())
}

// TestLuaAdapterSessionAdminNilWhenUnset proves the adapter still returns nil
// when no backing is wired — the sessionAdminServer nil-guard then fails closed
// with Unimplemented (the pre-4.2 behavior, preserved for the unwired case).
func TestLuaAdapterSessionAdminNilWhenUnset(t *testing.T) {
	a := newLuaHostCapAdapter(hostfunc.New(nil))
	assert.Nil(t, a.SessionAdmin())
}

// TestHostWithSessionAdminOptionWiresAdapter proves the construction-time
// WithSessionAdmin option threads the backing into the adapter the per-plugin
// bufconn endpoint consumes.
func TestHostWithSessionAdminOptionWiresAdapter(t *testing.T) {
	h := NewHostWithFunctions(hostfunc.New(nil), WithSessionAdmin(stubSessionAdmin{}))
	require.NotNil(t, h.HostCapabilitiesAdapter().SessionAdmin())
}

// TestHostSetSessionAdminLateWiresAdapter proves the late setter wires the
// backing after construction — the production path, because the event appender
// the backing wraps does not exist until the EventBus subsystem starts (after
// the plugin subsystem builds the Lua host). Mirrors SetHistoryReader /
// SetEventEmitter late binding.
func TestHostSetSessionAdminLateWiresAdapter(t *testing.T) {
	h := NewHostWithFunctions(hostfunc.New(nil))
	require.Nil(t, h.HostCapabilitiesAdapter().SessionAdmin(), "unset before late wiring")

	h.SetSessionAdmin(stubSessionAdmin{})

	require.NotNil(t, h.HostCapabilitiesAdapter().SessionAdmin(), "SetSessionAdmin must wire the adapter post-construction")
}
