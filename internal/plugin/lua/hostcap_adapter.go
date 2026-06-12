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
//   - SessionAdmin: Lua returns nil until a broadcast/disconnect backing is wired
//     for the bufconn endpoint (holomush-eykuh.4.2). The server-side
//     nil-guard in sessionAdminServer keeps this fail-closed (Unimplemented),
//     so a nil here is safe for both runtimes.
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
// Functions field via exported accessors.
//
// The adapter holds no settings stores of its own: the settings methods recover
// the typed stores from the Functions' settingsOps seam (which lua.Host wires via
// SetSettingsStores) so the host.v1 SettingsService server reaches the SAME stores
// the Lua get_setting/set_setting hostfuncs use (plugin-runtime-symmetry,
// INV-PLUGIN-27). This keeps a single wiring point: SetSettingsStores.
type luaHostCapAdapter struct {
	f *hostfunc.Functions
}

// newLuaHostCapAdapter creates a Lua HostCapabilities adapter wrapping f.
// Settings stores and the focus coordinator are recovered on demand from f's
// settingsOps / focusOps seams, so callers wire them once via
// lua.Host.SetSettingsStores / SetFocusCoordinator.
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

// OwnedResourceTypes returns an empty, non-nil map: Lua plugins own no resource
// types by design — parity with the holomush.evaluate host function, which
// hardcodes OwnedTypes: map[string]bool{} ("Lua plugins own no resource types")
// at hostfunc/evaluate.go:57. Returning a non-nil empty map (not nil) lets the
// host-brokered EvalService owned-type gate behave identically to the binary host
// rather than treating the Lua path as a special unconfigured case.
func (a *luaHostCapAdapter) OwnedResourceTypes(_ string) map[string]bool {
	return map[string]bool{}
}

// settingsStores recovers the typed settings stores from the Functions'
// settingsOps seam. lua.Host.SetSettingsStores wires a *settingsStoresOpsAdapter
// (holding the three typed stores) when, and only when, all three are non-nil; a
// type assertion recovers them. Returns nil stores when settings are unwired so
// the host.v1 SettingsService server fails closed (Unimplemented).
func (a *luaHostCapAdapter) settingsStores() *settingsStoresOpsAdapter {
	so := a.f.GetSettingsOps()
	if so == nil {
		return nil
	}
	adapter, ok := so.(*settingsStoresOpsAdapter)
	if !ok {
		return nil
	}
	return adapter
}

// GameSettings returns the game settings store recovered from the settingsOps
// seam (nil when settings are unwired).
func (a *luaHostCapAdapter) GameSettings() settings.GameSettings {
	s := a.settingsStores()
	if s == nil {
		return nil
	}
	return s.gameStore()
}

// PlayerSettings returns the player settings store recovered from the settingsOps
// seam (nil when settings are unwired).
func (a *luaHostCapAdapter) PlayerSettings() settings.PlayerSettingsStore {
	s := a.settingsStores()
	if s == nil {
		return nil
	}
	return s.playerStore()
}

// CharacterSettings returns the character settings store recovered from the
// settingsOps seam (nil when settings are unwired).
func (a *luaHostCapAdapter) CharacterSettings() settings.CharacterSettingsStore {
	s := a.settingsStores()
	if s == nil {
		return nil
	}
	return s.characterStore()
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

// SessionAdmin returns nil: the broadcast/disconnect admin surface is the WIDE
// hostfunc.SessionAccess shim, which is NOT held by Functions (Functions stores
// only the narrow session.Access via WithSessionAccess). Wiring the wide surface
// would require threading a new dependency through lua.Host construction, which is
// deferred to a future migration task (holomush-eykuh.4.2). Returning nil is safe:
// sessionAdminServer.Broadcast/Disconnect nil-guard this port and fail closed with
// Unimplemented (session.go), protecting both runtimes.
func (a *luaHostCapAdapter) SessionAdmin() hostcap.SessionAdmin {
	return nil
}

// --- focusOpsCoordinatorAdapter -------------------------------------------
//
// Adapts hostfunc.FocusOps → focus.Coordinator so the host.v1 FocusService
// server (which takes focus.Coordinator) drives focus operations through the
// Lua-wired FocusOps shim. The four methods the focusServer actually calls —
// SetConnectionFocus, AutoFocusOnJoin, IsAnyConnFocused, GetConnectionFocus —
// delegate to FocusOps with faithful return-shape translation (see each method).
//
// Three Coordinator methods are NOT on FocusOps and are NOT reached by the
// host.v1 servers (only by the in-process session manager): RestoreFocus,
// RestoreConnectionFocus, and LeaveFocusByTarget/JoinFocus/LeaveFocus/PresentFocus
// pass through directly. RestoreFocus / RestoreConnectionFocus stay fail-closed
// "unsupported" stubs because FocusOps has no equivalent source.

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

// IsAnyConnFocused delegates directly: the FocusOps and Coordinator signatures
// are identical (characterID, sceneID) → (bool, error).
func (a *focusOpsCoordinatorAdapter) IsAnyConnFocused(ctx context.Context, characterID, sceneID ulid.ULID) (bool, error) {
	return a.fo.IsAnyConnFocused(ctx, characterID, sceneID) //nolint:wrapcheck // FocusOps errors already oops-coded
}

// SetConnectionFocus delegates to FocusOps and returns a zero-valued
// focus.SetConnectionFocusResult. FocusOps.SetConnectionFocus is error-only, so
// the result's three fields (OldFocusKey, SessionID, CharLocationID) have no
// FocusOps source — they are intentionally lossy here. This is safe: those
// fields exist so the production *defaultCoordinator can drive per-connection
// subscription deltas in-process (INV-SCENE-38), and the host.v1 focusServer
// (servers.go SetConnectionFocus) discards the result entirely, reading only the
// error. A faithful zero result is therefore correct for every reachable caller.
func (a *focusOpsCoordinatorAdapter) SetConnectionFocus(ctx context.Context, connectionID ulid.ULID, focusKey *session.FocusKey, isSceneGrid bool) (focus.SetConnectionFocusResult, error) {
	if err := a.fo.SetConnectionFocus(ctx, connectionID, focusKey, isSceneGrid); err != nil {
		return focus.SetConnectionFocusResult{}, err //nolint:wrapcheck // FocusOps errors already oops-coded
	}
	return focus.SetConnectionFocusResult{}, nil
}

// AutoFocusOnJoin delegates to FocusOps and maps the multi-return tuple into a
// focus.AutoFocusOnJoinResponse. The slices and total count map faithfully;
// hostfunc.FocusFailure → focus.AutoFocusFailure (identical ConnectionID/Reason
// fields). SessionID and CharLocationID are intentionally lossy (FocusOps does
// not surface them) — like the SetConnectionFocus result fields, they exist for
// the production coordinator's in-process delta driving and are NOT read by the
// host.v1 focusServer (servers.go AutoFocusOnJoin reads only the slices + total).
func (a *focusOpsCoordinatorAdapter) AutoFocusOnJoin(ctx context.Context, characterID, sceneID ulid.ULID) (focus.AutoFocusOnJoinResponse, error) {
	focused, skipped, failed, total, err := a.fo.AutoFocusOnJoin(ctx, characterID, sceneID)
	if err != nil {
		return focus.AutoFocusOnJoinResponse{}, err //nolint:wrapcheck // FocusOps errors already oops-coded
	}
	var respFailed []focus.AutoFocusFailure
	if len(failed) > 0 {
		respFailed = make([]focus.AutoFocusFailure, len(failed))
		for i, f := range failed {
			respFailed[i] = focus.AutoFocusFailure{
				ConnectionID: f.ConnectionID,
				Reason:       f.Reason,
			}
		}
	}
	return focus.AutoFocusOnJoinResponse{
		FocusedConnectionIDs: focused,
		SkippedConnectionIDs: skipped,
		FailedConnectionIDs:  respFailed,
		TotalConnectionCount: total,
	}, nil
}

// GetConnectionFocus delegates directly: the FocusOps and Coordinator signatures
// are identical (connectionID) → (*session.FocusKey, error).
func (a *focusOpsCoordinatorAdapter) GetConnectionFocus(ctx context.Context, connectionID ulid.ULID) (*session.FocusKey, error) {
	return a.fo.GetConnectionFocus(ctx, connectionID) //nolint:wrapcheck // FocusOps errors already oops-coded
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
