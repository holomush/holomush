// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/plugin/hostcap"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/settings"
)

// Compile-time assertion: luaHostCapAdapter satisfies hostcap.HostCapabilities.
var _ hostcap.HostCapabilities = (*luaHostCapAdapter)(nil)

// TestLuaAdapterSatisfiesHostCapabilities pins the compile-time contract (INV-PLUGIN-49).
func TestLuaAdapterSatisfiesHostCapabilities(_ *testing.T) {
	var _ hostcap.HostCapabilities = (*luaHostCapAdapter)(nil)
}

// TestLuaAdapterLookupActorUsesContextActor verifies Lua identity comes from
// core.ActorFromContext, not a dispatch token (spec §0 — no forgery surface on Lua side).
func TestLuaAdapterLookupActorUsesContextActor(t *testing.T) {
	a := newLuaHostCapAdapter(hostfunc.New(nil))
	ctx := core.WithActor(context.Background(), core.Actor{Kind: core.ActorPlugin, ID: "echo-bot"})
	actor, subj, err := a.LookupActor(ctx, "echo-bot")
	require.NoError(t, err)
	assert.Equal(t, "echo-bot", actor.ID)
	assert.Equal(t, "plugin:echo-bot", subj)
}

// TestLuaAdapterLookupActorWithNoContextActorFails verifies that absent context actor
// produces an error (fail-closed per spec §0 security model).
func TestLuaAdapterLookupActorWithNoContextActorFails(t *testing.T) {
	a := newLuaHostCapAdapter(hostfunc.New(nil))
	_, _, err := a.LookupActor(context.Background(), "echo-bot")
	assert.Error(t, err)
}

// TestLuaAdapterIssueEmitTokenReturnsUnsupportedError verifies no-token-store behavior:
// the Lua runtime has no emit-token forgery surface, so this always errors.
func TestLuaAdapterIssueEmitTokenReturnsUnsupportedError(t *testing.T) {
	a := newLuaHostCapAdapter(hostfunc.New(nil))
	_, err := a.IssueEmitToken(context.Background(), "echo-bot", core.Actor{})
	assert.Error(t, err)
}

// TestLuaAdapterAccessEngineDelegatesToFunctions spot-checks that AccessEngine delegates
// to the Functions backing. A nil engine is the unconfigured/zero-value case.
func TestLuaAdapterAccessEngineDelegatesToFunctions(t *testing.T) {
	f := hostfunc.New(nil) // no engine wired
	a := newLuaHostCapAdapter(f)
	// unconfigured Functions → nil engine
	assert.Nil(t, a.AccessEngine())
}

// TestLuaAdapterWorldQuerierReturnsNilWhenNoWorldService verifies WorldQuerier returns
// nil when the Functions has no world mutator configured.
func TestLuaAdapterWorldQuerierReturnsNilWhenNoWorldService(t *testing.T) {
	a := newLuaHostCapAdapter(hostfunc.New(nil))
	assert.Nil(t, a.WorldQuerier("echo-bot"))
}

// TestLuaAdapterIdentityRegistrySnapshotReturnsNil verifies nil is returned for the
// identity registry (Lua has no emit-token forgery surface; nil is acceptable per port).
func TestLuaAdapterIdentityRegistrySnapshotReturnsNil(t *testing.T) {
	a := newLuaHostCapAdapter(hostfunc.New(nil))
	assert.Nil(t, a.IdentityRegistrySnapshot())
}

// TestLuaAdapterOwnedResourceTypesReturnsEmptyNonNilMap verifies the adapter
// returns a non-nil empty map — parity with hostfunc/evaluate.go:57 which
// hardcodes OwnedTypes: map[string]bool{} ("Lua plugins own no resource types").
// A non-nil empty map lets the EvalService owned-type gate behave at parity with
// the binary host instead of nil-dereferencing or fail-closing unexpectedly.
func TestLuaAdapterOwnedResourceTypesReturnsEmptyNonNilMap(t *testing.T) {
	a := newLuaHostCapAdapter(hostfunc.New(nil))
	got := a.OwnedResourceTypes("echo-bot")
	require.NotNil(t, got, "Lua adapter must return a non-nil empty map (parity with evaluate.go:57)")
	assert.Empty(t, got)
}

// --- fake FocusOps for focusOpsCoordinatorAdapter tests ---------------------

type fakeFocusOps struct {
	hostfunc.FocusOps // embed for the methods this fake does not exercise

	setConnErr       error
	setConnConnID    ulid.ULID
	setConnFocusKey  *session.FocusKey
	setConnSceneGrid bool

	autoFocused   []ulid.ULID
	autoSkipped   []ulid.ULID
	autoFailed    []hostfunc.FocusFailure
	autoTotal     uint32
	autoErr       error
	autoCharID    ulid.ULID
	autoSceneID   ulid.ULID
	isAnyResult   bool
	isAnyErr      error
	getConnKey    *session.FocusKey
	getConnErr    error
	getConnConnID ulid.ULID
}

func (f *fakeFocusOps) SetConnectionFocus(_ context.Context, connID ulid.ULID, fk *session.FocusKey, isSceneGrid bool) error {
	f.setConnConnID = connID
	f.setConnFocusKey = fk
	f.setConnSceneGrid = isSceneGrid
	return f.setConnErr
}

func (f *fakeFocusOps) AutoFocusOnJoin(_ context.Context, charID, sceneID ulid.ULID) ([]ulid.ULID, []ulid.ULID, []hostfunc.FocusFailure, uint32, error) {
	f.autoCharID = charID
	f.autoSceneID = sceneID
	return f.autoFocused, f.autoSkipped, f.autoFailed, f.autoTotal, f.autoErr
}

func (f *fakeFocusOps) IsAnyConnFocused(_ context.Context, _, _ ulid.ULID) (bool, error) {
	return f.isAnyResult, f.isAnyErr
}

func (f *fakeFocusOps) GetConnectionFocus(_ context.Context, connID ulid.ULID) (*session.FocusKey, error) {
	f.getConnConnID = connID
	return f.getConnKey, f.getConnErr
}

// TestLuaFocusAdapterSetConnectionFocusDelegatesToFocusOps verifies the
// Coordinator-shaped SetConnectionFocus delegates to FocusOps.SetConnectionFocus
// and returns a zero SetConnectionFocusResult (FocusOps cannot populate the
// result fields; the host.v1 focusServer reads only the error).
func TestLuaFocusAdapterSetConnectionFocusDelegatesToFocusOps(t *testing.T) {
	fo := &fakeFocusOps{}
	a := &focusOpsCoordinatorAdapter{fo: fo}
	connID := ulid.Make()
	fk := &session.FocusKey{Kind: "scene", TargetID: ulid.Make()}

	res, err := a.SetConnectionFocus(context.Background(), connID, fk, false)
	require.NoError(t, err)
	assert.Equal(t, connID, fo.setConnConnID)
	assert.Equal(t, fk, fo.setConnFocusKey)
	assert.False(t, fo.setConnSceneGrid)
	// FocusOps is error-only; the result is intentionally zero-valued.
	assert.Nil(t, res.OldFocusKey)
	assert.Empty(t, res.SessionID)
}

// TestLuaFocusAdapterSetConnectionFocusPropagatesError verifies a FocusOps error
// surfaces unchanged.
func TestLuaFocusAdapterSetConnectionFocusPropagatesError(t *testing.T) {
	fo := &fakeFocusOps{setConnErr: errors.New("denied")}
	a := &focusOpsCoordinatorAdapter{fo: fo}
	_, err := a.SetConnectionFocus(context.Background(), ulid.Make(), nil, true)
	require.Error(t, err)
}

// TestLuaFocusAdapterAutoFocusOnJoinTranslatesTupleToResponse verifies the
// FocusOps multi-return is mapped into the focus.AutoFocusOnJoinResponse struct
// the host.v1 focusServer consumes (focused/skipped/failed/total).
func TestLuaFocusAdapterAutoFocusOnJoinTranslatesTupleToResponse(t *testing.T) {
	f1, f2 := ulid.Make(), ulid.Make()
	s1 := ulid.Make()
	failConn := ulid.Make()
	fo := &fakeFocusOps{
		autoFocused: []ulid.ULID{f1, f2},
		autoSkipped: []ulid.ULID{s1},
		autoFailed:  []hostfunc.FocusFailure{{ConnectionID: failConn, Reason: "membership_absent"}},
		autoTotal:   4,
	}
	a := &focusOpsCoordinatorAdapter{fo: fo}
	charID, sceneID := ulid.Make(), ulid.Make()

	resp, err := a.AutoFocusOnJoin(context.Background(), charID, sceneID)
	require.NoError(t, err)
	assert.Equal(t, charID, fo.autoCharID)
	assert.Equal(t, sceneID, fo.autoSceneID)
	assert.Equal(t, []ulid.ULID{f1, f2}, resp.FocusedConnectionIDs)
	assert.Equal(t, []ulid.ULID{s1}, resp.SkippedConnectionIDs)
	require.Len(t, resp.FailedConnectionIDs, 1)
	assert.Equal(t, failConn, resp.FailedConnectionIDs[0].ConnectionID)
	assert.Equal(t, "membership_absent", resp.FailedConnectionIDs[0].Reason)
	assert.Equal(t, uint32(4), resp.TotalConnectionCount)
}

// TestLuaFocusAdapterAutoFocusOnJoinPropagatesError verifies a FocusOps error
// yields a zero response and the error.
func TestLuaFocusAdapterAutoFocusOnJoinPropagatesError(t *testing.T) {
	fo := &fakeFocusOps{autoErr: errors.New("store down")}
	a := &focusOpsCoordinatorAdapter{fo: fo}
	resp, err := a.AutoFocusOnJoin(context.Background(), ulid.Make(), ulid.Make())
	require.Error(t, err)
	assert.Empty(t, resp.FocusedConnectionIDs)
}

// TestLuaFocusAdapterIsAnyConnFocusedDelegates verifies direct delegation.
func TestLuaFocusAdapterIsAnyConnFocusedDelegates(t *testing.T) {
	fo := &fakeFocusOps{isAnyResult: true}
	a := &focusOpsCoordinatorAdapter{fo: fo}
	got, err := a.IsAnyConnFocused(context.Background(), ulid.Make(), ulid.Make())
	require.NoError(t, err)
	assert.True(t, got)
}

// TestLuaFocusAdapterGetConnectionFocusDelegates verifies direct delegation and
// that the FocusKey pointer is returned unchanged.
func TestLuaFocusAdapterGetConnectionFocusDelegates(t *testing.T) {
	fk := &session.FocusKey{Kind: "scene", TargetID: ulid.Make()}
	fo := &fakeFocusOps{getConnKey: fk}
	a := &focusOpsCoordinatorAdapter{fo: fo}
	connID := ulid.Make()
	got, err := a.GetConnectionFocus(context.Background(), connID)
	require.NoError(t, err)
	assert.Equal(t, connID, fo.getConnConnID)
	assert.Equal(t, fk, got)
}

// TestLuaFocusAdapterRestoreFocusUnsupported verifies the server-unreachable
// RestoreFocus stub stays fail-closed (not on FocusOps; session-manager only).
func TestLuaFocusAdapterRestoreFocusUnsupported(t *testing.T) {
	a := &focusOpsCoordinatorAdapter{fo: &fakeFocusOps{}}
	_, err := a.RestoreFocus(context.Background(), "session-1")
	assert.Error(t, err)
}

// --- minimal store stubs for settings-recovery tests ------------------------

type stubPlayerSettingsStore struct{ settings.PlayerSettingsStore }

type stubGameSettings struct{ settings.GameSettings }

// TestLuaAdapterSettingsStoresRecoveredFromSettingsOps verifies that when the
// underlying Functions has the settingsStoresOpsAdapter wired (via
// SetSettingsStores), the hostcap adapter recovers and returns the SAME typed
// stores — so the host.v1 SettingsService server reaches the binary-parity stores.
func TestLuaAdapterSettingsStoresRecoveredFromSettingsOps(t *testing.T) {
	player := &stubPlayerSettingsStore{}
	character := settings.NewNullCharacterSettingsStore()
	game := &stubGameSettings{}

	f := hostfunc.New(nil)
	f.SetSettingsOps(&settingsStoresOpsAdapter{player: player, character: character, game: game})
	a := newLuaHostCapAdapter(f)

	assert.Equal(t, game, a.GameSettings())
	assert.Equal(t, player, a.PlayerSettings())
	assert.Equal(t, character, a.CharacterSettings())
}

// TestLuaAdapterSettingsStoresNilWhenSettingsOpsUnset verifies the stores stay
// nil (fail-closed) when no settings adapter is wired.
func TestLuaAdapterSettingsStoresNilWhenSettingsOpsUnset(t *testing.T) {
	a := newLuaHostCapAdapter(hostfunc.New(nil))
	assert.Nil(t, a.GameSettings())
	assert.Nil(t, a.PlayerSettings())
	assert.Nil(t, a.CharacterSettings())
}
