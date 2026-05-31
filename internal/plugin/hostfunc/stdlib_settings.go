// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"
	"log/slog"

	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// SettingsOps is the narrow store seam the Lua get_setting/set_setting hostfuncs
// delegate to. It is a PURE store partition seam: trust checks (actor recovery,
// principal ownership, GAME-write operator authorization) happen in the hostfunc
// layer above (getSettingFn / setSettingFn), exactly as the binary
// PluginHostService performs them before touching the store. The adapter that
// satisfies this interface (internal/plugin/lua.settingsStoresOpsAdapter)
// selects the store by scope, binds .Plugin(pluginName), and performs
// StringSliceN / SetStringSlice — mirroring the binary GetSetting / SetSetting
// post-resolution store calls.
//
// principalID is the validated owner ULID for PLAYER / CHARACTER scope; it is
// ignored for GAME scope (game settings are not principal-partitioned).
type SettingsOps interface {
	GetSetting(ctx context.Context, scope pluginv1.SettingScope, pluginName, principalID, key string) (values []string, found bool, err error)
	SetSetting(ctx context.Context, scope pluginv1.SettingScope, pluginName, principalID, key string, values []string) error
}

// settingScopeFromString maps the Lua-supplied scope string to the proto enum.
// Unknown/empty strings map to SETTING_SCOPE_UNSPECIFIED so the caller fails
// closed, mirroring the binary host's unspecified-scope rejection.
func settingScopeFromString(s string) pluginv1.SettingScope {
	switch s {
	case "game":
		return pluginv1.SettingScope_SETTING_SCOPE_GAME
	case "player":
		return pluginv1.SettingScope_SETTING_SCOPE_PLAYER
	case "character":
		return pluginv1.SettingScope_SETTING_SCOPE_CHARACTER
	default:
		return pluginv1.SettingScope_SETTING_SCOPE_UNSPECIFIED
	}
}

// resolveSettingsAccess recovers the acting actor and (for PLAYER scope) the
// host-vouched owning player from the Lua VM context (INV-1: NEVER from Lua
// args), maps the scope string, enforces principal ownership for PLAYER /
// CHARACTER via the SHARED pluginauthz.CheckPrincipalOwnership gate (the SAME
// helper the binary host uses, with the SAME expected-owner semantics), and —
// never trusting the wire — confines the partition to the registration-time
// pluginName at the adapter.
//
// On any failure it returns a short, non-leaking Lua-facing message. The
// returned principalID is the canonical ULID string for PLAYER / CHARACTER and
// "" for GAME (ignored downstream).
func (f *Functions) resolveSettingsAccess(
	ctx context.Context, pluginName, scopeStr, principalID string,
) (scope pluginv1.SettingScope, normalizedPrincipal, errMsg string) {
	scope = settingScopeFromString(scopeStr)
	if scope == pluginv1.SettingScope_SETTING_SCOPE_UNSPECIFIED {
		return scope, "", "invalid settings scope"
	}

	if f.settingsOps == nil {
		slog.WarnContext(ctx, "settings host function called but no settings ops configured",
			"plugin", pluginName)
		return scope, "", "settings not available"
	}

	actor, ok := core.ActorFromContext(ctx)
	if !ok || pluginauthz.ActorSubject(actor) == "" {
		// No authenticated actor on the call → fail closed (INV-1 / INV-2).
		return scope, "", "permission denied"
	}

	if scope == pluginv1.SettingScope_SETTING_SCOPE_GAME {
		// GAME is server-wide; no principal ownership. (Reads are open; GAME
		// writes are gated separately by authorizeGameWrite in setSettingFn.)
		return scope, "", ""
	}

	// PLAYER / CHARACTER: enforce ownership via the shared gate against the
	// host-vouched expected owner. A foreign or malformed principal is denied
	// before any store access — identical trust to the binary
	// requirePrincipalOwnership.
	//
	//   - CHARACTER: expected owner is the acting character's ID.
	//   - PLAYER: expected owner is the host-vouched owning player ULID of the
	//     acting character, recovered in-process from the dispatch ctx
	//     (core.OwningPlayerFromContext) — the SAME value the binary path recovers
	//     from the dispatch token (holomush-iokti.19). Absent ⇒ "" ⇒ the shared
	//     gate fails closed, symmetric with the binary DeliverEvent path.
	expectedOwner := actor.ID
	if scope == pluginv1.SettingScope_SETTING_SCOPE_PLAYER {
		expectedOwner, _ = core.OwningPlayerFromContext(ctx)
	}

	pid, ownErr := pluginauthz.CheckPrincipalOwnership(principalID, expectedOwner)
	if ownErr != nil {
		// Do not leak inner error text to the plugin.
		return scope, "", "permission denied"
	}
	return scope, pid.String(), ""
}

// authorizeGameWrite enforces the operator authorization decision required for a
// GAME-scope SetSetting. It mirrors the binary host's authorizeGameWrite exactly:
// the recovered subject must be permitted to "write" the per-plugin resource
// pluginauthz.SettingsGameWriteResource(pluginName) via the ABAC engine. Using
// the per-plugin resource keeps binary and Lua identical (plugin-runtime-symmetry,
// INV-8; holomush-iokti.15 Item 2). A nil engine or a deny → permission denied;
// engine build errors are logged and surfaced as a generic message (no
// inner-error leak).
func (f *Functions) authorizeGameWrite(ctx context.Context, pluginName, subject string) string {
	if f.engine == nil {
		slog.WarnContext(ctx, "GAME-scope set_setting denied: no access engine configured",
			"plugin", pluginName)
		return "settings not available"
	}
	req, err := types.NewAccessRequest(subject, types.ActionWrite, pluginauthz.SettingsGameWriteResource(pluginName), nil)
	if err != nil {
		slog.ErrorContext(ctx, "build game-write access request failed",
			"plugin", pluginName, "err", err)
		return "access check failed"
	}
	dec, err := f.engine.Evaluate(ctx, req)
	if err != nil {
		slog.ErrorContext(ctx, "game-write authorization failed",
			"plugin", pluginName, "err", err)
		return "access check failed"
	}
	if !dec.IsAllowed() {
		return "permission denied"
	}
	return ""
}

// getSettingFn returns the holomush.get_setting(scope, principal_id, key) Lua
// host function. It mirrors evaluateFn's parity shape: identity is recovered
// from the host-stamped actor on the context (NEVER from Lua args), the plugin
// partition is bound from the registration-time pluginName (NEVER from Lua
// args), and PLAYER / CHARACTER ownership is enforced through the shared
// pluginauthz gate — the same trust the binary GetSetting applies.
//
// Return signature: (values table, found bool) on success;
//
//	(nil, error_string) on any denial / failure.
func (f *Functions) getSettingFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		scopeStr := L.CheckString(1)
		principalID := L.CheckString(2)
		key := L.CheckString(3)

		parentCtx := L.Context()
		if parentCtx == nil {
			parentCtx = context.Background()
		}
		ctx, cancel := context.WithTimeout(parentCtx, defaultPluginQueryTimeout)
		defer cancel()

		scope, normalizedPrincipal, errMsg := f.resolveSettingsAccess(ctx, pluginName, scopeStr, principalID)
		if errMsg != "" {
			L.Push(lua.LNil)
			L.Push(lua.LString(errMsg))
			return 2
		}

		values, found, err := f.settingsOps.GetSetting(ctx, scope, pluginName, normalizedPrincipal, key)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(SanitizeErrorForPlugin(
				PluginErrorContext{Plugin: pluginName, Operation: "get_setting", Subject: "key", SubjectID: key}, err,
			)))
			return 2
		}

		tbl := L.NewTable()
		for _, v := range values {
			tbl.Append(lua.LString(v))
		}
		L.Push(tbl)
		L.Push(lua.LBool(found))
		return 2
	}
}

// setSettingFn returns the holomush.set_setting(scope, principal_id, key, values)
// Lua host function. Same parity shape as getSettingFn, plus the GAME-scope
// operator-authorization gate identical to the binary SetSetting path.
//
// Return signature: (true, nil) on success; (nil|false, error_string) on
// denial / failure.
func (f *Functions) setSettingFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		scopeStr := L.CheckString(1)
		principalID := L.CheckString(2)
		key := L.CheckString(3)
		valuesTbl := L.CheckTable(4)

		values := make([]string, 0, valuesTbl.Len())
		valuesTbl.ForEach(func(_, v lua.LValue) {
			values = append(values, v.String())
		})

		parentCtx := L.Context()
		if parentCtx == nil {
			parentCtx = context.Background()
		}
		ctx, cancel := context.WithTimeout(parentCtx, defaultPluginQueryTimeout)
		defer cancel()

		scope, normalizedPrincipal, errMsg := f.resolveSettingsAccess(ctx, pluginName, scopeStr, principalID)
		if errMsg != "" {
			L.Push(lua.LNil)
			L.Push(lua.LString(errMsg))
			return 2
		}

		// GAME writes require an operator authorization decision. The subject is
		// the recovered actor's subject; resolveSettingsAccess already verified a
		// non-empty subject exists.
		if scope == pluginv1.SettingScope_SETTING_SCOPE_GAME {
			actor, _ := core.ActorFromContext(ctx)
			if authErr := f.authorizeGameWrite(ctx, pluginName, pluginauthz.ActorSubject(actor)); authErr != "" {
				L.Push(lua.LNil)
				L.Push(lua.LString(authErr))
				return 2
			}
		}

		if err := f.settingsOps.SetSetting(ctx, scope, pluginName, normalizedPrincipal, key, values); err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(SanitizeErrorForPlugin(
				PluginErrorContext{Plugin: pluginName, Operation: "set_setting", Subject: "key", SubjectID: key}, err,
			)))
			return 2
		}

		L.Push(lua.LTrue)
		L.Push(lua.LNil)
		return 2
	}
}
