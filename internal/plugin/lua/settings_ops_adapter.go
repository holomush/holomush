// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/settings"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// settingsStoresOpsAdapter adapts the three plugin-partitioned settings stores
// to the hostfunc.SettingsOps store seam. It is the permanent Lua delegation
// seam: it lets the gopher-lua hostfunc layer reach the SAME settings stores the
// binary PluginHostService uses, so both runtimes drive plugin-partitioned
// settings through one common store path (plugin-runtime-symmetry, INV-8).
//
// All trust checks (actor recovery, principal ownership, GAME-write operator
// authorization) are performed by the hostfunc layer BEFORE these methods run —
// exactly as the binary GetSetting / SetSetting resolve the scope and ownership
// before touching the store. By the time GetSetting / SetSetting are called
// here, principalID is the validated owner ULID (for PLAYER / CHARACTER) and the
// plugin partition is bound from pluginName, never the wire.
type settingsStoresOpsAdapter struct {
	player    settings.PlayerSettingsStore
	character settings.CharacterSettingsStore
	game      settings.GameSettings
}

var _ hostfunc.SettingsOps = (*settingsStoresOpsAdapter)(nil)

// scopedFor selects the scope's base Scoped handle. principalID is parsed as a
// ULID for PLAYER / CHARACTER (already validated upstream by
// pluginauthz.CheckPrincipalOwnership); GAME ignores it. Returns a nil Scoped
// and false when the store for the scope is unwired or the scope is unknown, so
// the caller fails closed rather than nil-deref.
func (a *settingsStoresOpsAdapter) scopedFor(
	ctx context.Context, scope pluginv1.SettingScope, principalID string,
) (settings.Scoped, bool) {
	switch scope {
	case pluginv1.SettingScope_SETTING_SCOPE_GAME:
		if a.game == nil {
			return nil, false
		}
		return a.game, true
	case pluginv1.SettingScope_SETTING_SCOPE_PLAYER:
		if a.player == nil {
			return nil, false
		}
		pid, err := ulid.Parse(principalID)
		if err != nil {
			return nil, false
		}
		return a.player.For(ctx, pid), true
	case pluginv1.SettingScope_SETTING_SCOPE_CHARACTER:
		if a.character == nil {
			return nil, false
		}
		pid, err := ulid.Parse(principalID)
		if err != nil {
			return nil, false
		}
		return a.character.For(ctx, pid), true
	default:
		return nil, false
	}
}

// GetSetting reads the plugin-partitioned list value, binding the plugin
// partition from pluginName host-side (NEVER the wire) — mirroring the binary
// GetSetting's base.Plugin(s.pluginName).StringSliceN(key).
func (a *settingsStoresOpsAdapter) GetSetting(
	ctx context.Context, scope pluginv1.SettingScope, pluginName, principalID, key string,
) (values []string, found bool, err error) {
	base, ok := a.scopedFor(ctx, scope, principalID)
	if !ok {
		return nil, false, nil
	}
	values, found = base.Plugin(pluginName).StringSliceN(ctx, key)
	return values, found, nil
}

// SetSetting writes the plugin-partitioned list value, binding the plugin
// partition from pluginName host-side — mirroring the binary SetSetting's
// base.Plugin(s.pluginName).SetStringSlice(key, values).
func (a *settingsStoresOpsAdapter) SetSetting(
	ctx context.Context, scope pluginv1.SettingScope, pluginName, principalID, key string, values []string,
) error {
	base, ok := a.scopedFor(ctx, scope, principalID)
	if !ok {
		return nil
	}
	return base.Plugin(pluginName).SetStringSlice(ctx, key, values) //nolint:wrapcheck // settings store errors propagate as-is to the hostfunc sanitizer
}
