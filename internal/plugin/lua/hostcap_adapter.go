// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package lua hostcap_adapter.go wires *hostfunc.Functions as a
// hostcap.HostCapabilities adapter for the Lua runtime. It is the symmetric
// counterpart to the *goplugin.Host adapter (binary runtime). Both runtimes
// satisfy the same port; the only per-runtime differences are:
//
//   - LookupActor: Lua reads core.ActorFromContext(ctx) (connection-scoped,
//     no token store). Binary reads the host-issued emit token from ctx metadata.
//   - IssueEmitToken: Lua returns an unsupported error (no forgery surface).
//   - IdentityRegistrySnapshot: Lua returns nil (no token forgery surface).
//   - EventEmitter: Lua returns nil (emissions flow through Lua hostfuncs, not gRPC).
//   - SessionAdmin: Lua returns nil (session.Access does not expose admin ops).
package lua

import (
	"context"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command/commandquery"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/grpc/focus"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostcap"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/settings"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// Compile-time assertion: luaHostCapAdapter satisfies hostcap.HostCapabilities.
var _ hostcap.HostCapabilities = (*luaHostCapAdapter)(nil)

// luaHostCapAdapter adapts *hostfunc.Functions to the hostcap.HostCapabilities
// port, making the Lua runtime a peer to the binary runtime for the host.v1
// capability servers (INV-PLUGIN-49). Every method delegates to the corresponding
// Functions field via exported accessors added in this change.
//
// Additional dependencies (settings stores) are injected via the
// withSettings option after construction when the relevant deps become
// available (SetSettingsStores on lua.Host propagates here).
type luaHostCapAdapter struct {
	f         *hostfunc.Functions
	gameSet   settings.GameSettings
	playerSet settings.PlayerSettingsStore
	charSet   settings.CharacterSettingsStore
}

// newLuaHostCapAdapter creates a Lua HostCapabilities adapter wrapping f.
// Settings stores default to nil (unconfigured); use withSettings to populate
// them once they are available (e.g. when lua.Host.SetSettingsStores is called).
func newLuaHostCapAdapter(f *hostfunc.Functions) *luaHostCapAdapter {
	return &luaHostCapAdapter{f: f}
}

// --- hostcap.HostCapabilities implementation --------------------------------

// AccessEngine returns the ABAC engine from the Functions backing.
func (a *luaHostCapAdapter) AccessEngine() types.AccessPolicyEngine {
	return a.f.Engine()
}

// Auditor returns the plugin-authz auditor from the Functions backing.
func (a *luaHostCapAdapter) Auditor() pluginauthz.Auditor {
	return a.f.Auditor()
}

// EventEmitter returns nil: Lua event emissions flow through hostfuncs (holomush.emit),
// not through the gRPC EmitEvent RPC. The emitServer's nil-guard covers this.
func (a *luaHostCapAdapter) EventEmitter() plugins.PluginIntentEmitter {
	return nil
}

// CommandQuerier returns the command-visibility querier from the Functions backing.
func (a *luaHostCapAdapter) CommandQuerier() *commandquery.Querier {
	return a.f.GetCommandQuerier()
}

// LookupActor recovers the acting Lua plugin identity from the context (spec §0).
// The Lua runtime has no emit-token store; identity is the host-stamped
// core.Actor carried on the dispatch context (INV-PLUGIN-22). pluginName is
// the host-established identity used to build the ABAC subject string.
func (a *luaHostCapAdapter) LookupActor(ctx context.Context, pluginName string) (core.Actor, string, error) {
	actor, ok := core.ActorFromContext(ctx)
	if !ok {
		return core.Actor{}, "", oops.In("lua").
			Code("ACTOR_NOT_FOUND").
			With("plugin", pluginName).
			New("no actor on context: Lua dispatch must stamp core.Actor before calling host capabilities")
	}
	return actor, access.PluginSubject(pluginName), nil
}

// IssueEmitToken returns an unsupported error: the Lua runtime has no emit-token
// forgery surface (no binary gRPC boundary to defend). The binary adapter mints
// tokens for RequestEmitToken; this adapter explicitly rejects the request.
func (a *luaHostCapAdapter) IssueEmitToken(_ context.Context, pluginName string, _ core.Actor) (string, error) {
	return "", oops.In("lua").
		Code("UNSUPPORTED_OPERATION").
		With("plugin", pluginName).
		New("IssueEmitToken is not supported on the Lua runtime: Lua has no emit-token forgery surface")
}

// IdentityRegistrySnapshot returns nil: the Lua runtime has no emit-token
// forgery surface so no identity registry is needed (nil is acceptable per port doc).
func (a *luaHostCapAdapter) IdentityRegistrySnapshot() plugins.IdentityRegistry {
	return nil
}

// OwnedResourceTypes returns nil: the Lua adapter has no manifest-lookup backing.
// Plugin-owned resource types are resolved at Manager level from the manifest;
// the capability servers' nil-guard covers the unconfigured case.
func (a *luaHostCapAdapter) OwnedResourceTypes(_ string) map[string]bool {
	return nil
}

// GameSettings returns the game settings store injected via withSettings (nil when unset).
func (a *luaHostCapAdapter) GameSettings() settings.GameSettings {
	return a.gameSet
}

// PlayerSettings returns the player settings store injected via withSettings (nil when unset).
func (a *luaHostCapAdapter) PlayerSettings() settings.PlayerSettingsStore {
	return a.playerSet
}

// CharacterSettings returns the character settings store injected via withSettings (nil when unset).
func (a *luaHostCapAdapter) CharacterSettings() settings.CharacterSettingsStore {
	return a.charSet
}

// FocusCoordinator returns a focus.Coordinator backed by the Functions' FocusOps shim,
// or nil when FocusOps is unconfigured.
func (a *luaHostCapAdapter) FocusCoordinator() focus.Coordinator {
	fo := a.f.GetFocusOps()
	if fo == nil {
		return nil
	}
	return &focusOpsCoordinatorAdapter{fo: fo}
}

// HistoryReader returns the history reader from the Functions backing (nil when unset).
// hostfunc.HistoryReader and plugins.HistoryReader have the same ReplayTail signature;
// the concrete type satisfies both.
func (a *luaHostCapAdapter) HistoryReader() plugins.HistoryReader {
	hr := a.f.GetHistoryReader()
	if hr == nil {
		return nil
	}
	return hr
}

// ReadbackDecryptor returns a plugins.ReadbackDecryptor backed by the Functions'
// AuditDecryptor, or nil when the decryptor is unconfigured.
func (a *luaHostCapAdapter) ReadbackDecryptor() plugins.ReadbackDecryptor {
	dec := a.f.GetAuditDecryptor()
	if dec == nil {
		return nil
	}
	return &auditDecryptorToReadbackAdapter{d: dec}
}

// PropertyDefinition resolves a registry property by name via the Functions' property registry.
func (a *luaHostCapAdapter) PropertyDefinition(name string) (hostcap.PropertyDefinition, bool) {
	return a.f.GetPropertyRegistry().Lookup(name)
}

// WorldQuerier returns the plugin-subject-stamped world read adapter for the named plugin,
// or nil when no world service is configured. Reuses hostfunc.WorldQuerierAdapter
// (stamped with "plugin:<name>") rather than reimplementing the adapter logic.
func (a *luaHostCapAdapter) WorldQuerier(pluginName string) hostcap.WorldQuerier {
	wm := a.f.GetWorldMutator()
	if wm == nil {
		return nil
	}
	return hostfunc.NewWorldQuerierAdapter(wm, pluginName)
}

// WorldMutator returns the world write surface from the Functions backing (nil when unset).
func (a *luaHostCapAdapter) WorldMutator() hostcap.WorldMutator {
	return a.f.GetWorldMutator()
}

// SessionAccess returns the narrow session read/update surface from the Functions backing.
func (a *luaHostCapAdapter) SessionAccess() session.Access {
	return a.f.GetSessionAccess()
}

// SessionAdmin returns nil: the session.Access backing stored in Functions does not
// expose broadcast/disconnect (those are on the wider hostfunc.SessionAccess interface
// used by the session capability module, not by Functions.sessionAccess which is the
// narrow session.Access). A future task can wire admin ops if a consumer emerges.
func (a *luaHostCapAdapter) SessionAdmin() hostcap.SessionAdmin {
	return nil
}

// --- focusOpsCoordinatorAdapter -------------------------------------------
//
// Adapts hostfunc.FocusOps → focus.Coordinator so the FocusService host.v1
// server (which takes focus.Coordinator) can drive focus operations through
// the Lua-wired FocusOps shim. The extra methods on focus.Coordinator that
// FocusOps lacks (RestoreFocus, RestoreConnectionFocus) are not called by
// the host.v1 servers — they are used only by the session manager — so
// returning an "unimplemented" error there is safe.

type focusOpsCoordinatorAdapter struct {
	fo hostfunc.FocusOps
}

var _ focus.Coordinator = (*focusOpsCoordinatorAdapter)(nil)

func (a *focusOpsCoordinatorAdapter) JoinFocus(ctx context.Context, sessionID string, target session.FocusKey) error {
	return a.fo.JoinFocus(ctx, sessionID, target) //nolint:wrapcheck // FocusOps errors already oops-coded
}

func (a *focusOpsCoordinatorAdapter) LeaveFocus(ctx context.Context, sessionID string, target session.FocusKey) error {
	return a.fo.LeaveFocus(ctx, sessionID, target) //nolint:wrapcheck // FocusOps errors already oops-coded
}

func (a *focusOpsCoordinatorAdapter) LeaveFocusByTarget(ctx context.Context, target session.FocusKey) (session.LeaveByTargetResult, error) {
	return a.fo.LeaveFocusByTarget(ctx, target) //nolint:wrapcheck // FocusOps errors already oops-coded
}

func (a *focusOpsCoordinatorAdapter) PresentFocus(ctx context.Context, sessionID string, target session.FocusKey) error {
	return a.fo.PresentFocus(ctx, sessionID, target) //nolint:wrapcheck // FocusOps errors already oops-coded
}

func (a *focusOpsCoordinatorAdapter) RestoreFocus(_ context.Context, _ string) (focus.RestorePlan, error) {
	// Not called by host.v1 capability servers; only used by the session manager.
	return focus.RestorePlan{}, oops.In("lua").Code("UNSUPPORTED_OPERATION").New("RestoreFocus not supported via Lua FocusOps adapter")
}

func (a *focusOpsCoordinatorAdapter) RestoreConnectionFocus(_ context.Context, _ string, _ ulid.ULID) error {
	// Not called by host.v1 capability servers; only used by the session manager.
	return oops.In("lua").Code("UNSUPPORTED_OPERATION").New("RestoreConnectionFocus not supported via Lua FocusOps adapter")
}

func (a *focusOpsCoordinatorAdapter) IsAnyConnFocused(_ context.Context, _, _ ulid.ULID) (bool, error) {
	// Not called by host.v1 capability servers; only used by the session manager.
	return false, oops.In("lua").Code("UNSUPPORTED_OPERATION").New("IsAnyConnFocused not supported via Lua FocusOps adapter")
}

func (a *focusOpsCoordinatorAdapter) SetConnectionFocus(_ context.Context, _ ulid.ULID, _ *session.FocusKey, _ bool) (focus.SetConnectionFocusResult, error) {
	// Not called by host.v1 capability servers; only used by the session manager.
	return focus.SetConnectionFocusResult{}, oops.In("lua").Code("UNSUPPORTED_OPERATION").New("SetConnectionFocus not supported via Lua FocusOps adapter")
}

func (a *focusOpsCoordinatorAdapter) AutoFocusOnJoin(_ context.Context, _, _ ulid.ULID) (focus.AutoFocusOnJoinResponse, error) {
	// Not called by host.v1 capability servers; only used by the session manager.
	return focus.AutoFocusOnJoinResponse{}, oops.In("lua").Code("UNSUPPORTED_OPERATION").New("AutoFocusOnJoin not supported via Lua FocusOps adapter")
}

func (a *focusOpsCoordinatorAdapter) GetConnectionFocus(_ context.Context, _ ulid.ULID) (*session.FocusKey, error) {
	// Not called by host.v1 capability servers; only used by the session manager.
	return nil, oops.In("lua").Code("UNSUPPORTED_OPERATION").New("GetConnectionFocus not supported via Lua FocusOps adapter")
}

// --- auditDecryptorToReadbackAdapter ---------------------------------------
//
// Adapts hostfunc.AuditDecryptor → plugins.ReadbackDecryptor so the auditServer
// (which calls dec.DecryptOwnRows) can reach the Lua host's audit decryptor.
// The single-row DecryptOwnRow path is not called by the host.v1 servers.

type auditDecryptorToReadbackAdapter struct {
	d hostfunc.AuditDecryptor
}

var _ plugins.ReadbackDecryptor = (*auditDecryptorToReadbackAdapter)(nil)

// DecryptOwnRows delegates to the AuditDecryptor's batch method, unpacking
// the response envelope into the RowResult slice the server expects.
func (a *auditDecryptorToReadbackAdapter) DecryptOwnRows(ctx context.Context, pluginName, _ string, rows []*pluginv1.AuditRow) ([]*pluginv1.RowResult, error) {
	resp, err := a.d.DecryptOwnAuditRows(ctx, pluginName, rows)
	if err != nil {
		return nil, err //nolint:wrapcheck // AuditDecryptor errors already oops-coded
	}
	if resp == nil {
		return nil, nil
	}
	return resp.Results, nil
}

// DecryptOwnRow delegates to the batch path with a single-element slice.
// Not called by host.v1 servers (they use DecryptOwnRows); implemented for
// interface completeness.
func (a *auditDecryptorToReadbackAdapter) DecryptOwnRow(ctx context.Context, pluginName, instanceID string, row *pluginv1.AuditRow) *pluginv1.RowResult {
	results, err := a.DecryptOwnRows(ctx, pluginName, instanceID, []*pluginv1.AuditRow{row})
	if err != nil || len(results) == 0 {
		return nil
	}
	return results[0]
}
