// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/internal/settings"
	"github.com/holomush/holomush/pkg/errutil"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// --- in-memory settings doubles for the GetSetting/SetSetting host RPC tests
// (holomush-iokti.7). These exercise the real owner-partitioned settings types
// without a database.

// memSysInfo is an in-memory settings.SystemInfoStore backing a game-scope store.
type memSysInfo struct{ data map[string]string }

func newMemSysInfo() *memSysInfo { return &memSysInfo{data: map[string]string{}} }

func (m *memSysInfo) GetSystemInfo(_ context.Context, key string) (string, error) {
	v, ok := m.data[key]
	if !ok {
		return "", settings.ErrNotFound
	}
	return v, nil
}

func (m *memSysInfo) SetSystemInfo(_ context.Context, key, value string) error {
	m.data[key] = value
	return nil
}

// memScopedStore is an in-memory PlayerSettingsStore / CharacterSettingsStore.
// Each principal keeps a single Scoped across For() calls so owner-partition
// writes round-trip within a test.
type memScopedStore struct{ byID map[string]settings.Scoped }

func newMemScopedStore() *memScopedStore {
	return &memScopedStore{byID: map[string]settings.Scoped{}}
}

func (m *memScopedStore) For(_ context.Context, id ulid.ULID) settings.Scoped {
	k := id.String()
	if m.byID[k] == nil {
		m.byID[k] = settings.NewScopedForTest(nil)
	}
	return m.byID[k]
}

func (m *memScopedStore) SetString(ctx context.Context, id ulid.ULID, key, value string) error {
	return m.For(ctx, id).Host().SetString(ctx, key, value)
}

var (
	_ settings.PlayerSettingsStore    = (*memScopedStore)(nil)
	_ settings.CharacterSettingsStore = (*memScopedStore)(nil)
)

// newSettingsServer builds a host wired with in-memory settings stores and the
// given engine (nil = none), plus a context carrying a valid dispatch token for
// actor. The server's pluginName is "plug-A" unless gameStore is shared via the
// lower-level helper below.
func newSettingsServer(
	t *testing.T, eng types.AccessPolicyEngine, actor core.Actor,
) (*pluginHostServiceServer, context.Context) {
	t.Helper()
	return newSettingsServerWith(t, "plug-A",
		settings.NewGameSettings(newMemSysInfo()), eng, actor)
}

// newSettingsServerWith builds a server with an explicit pluginName and game
// store, so isolation tests can share one game store across two plugins.
func newSettingsServerWith(
	t *testing.T,
	pluginName string,
	gameStore settings.GameSettings,
	eng types.AccessPolicyEngine,
	actor core.Actor,
) (*pluginHostServiceServer, context.Context) {
	t.Helper()
	opts := []HostOption{
		WithPlayerSettings(newMemScopedStore()),
		WithCharacterSettings(newMemScopedStore()),
		WithGameSettings(gameStore),
	}
	if eng != nil {
		opts = append(opts, WithEngine(eng))
	}
	h := NewHost(opts...)
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	srv := &pluginHostServiceServer{host: h, pluginName: pluginName}
	ctx, _ := contextWithValidToken(t, srv, actor)
	return srv, ctx
}

func settingsActor(t *testing.T) core.Actor {
	t.Helper()
	return core.Actor{Kind: core.ActorCharacter, ID: core.NewULID().String()}
}

// TestGetSettingUnspecifiedScopeRejected: the zero scope value fails closed.
func TestGetSettingUnspecifiedScopeRejected(t *testing.T) {
	t.Parallel()
	actor := settingsActor(t)
	srv, ctx := newSettingsServer(t, nil, actor)

	_, err := srv.GetSetting(ctx, &pluginv1.PluginHostServiceGetSettingRequest{
		Scope: pluginv1.SettingScope_SETTING_SCOPE_UNSPECIFIED,
		Key:   "content.cw_block",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestSetSettingUnspecifiedScopeRejected: same fail-closed guard on the write path.
func TestSetSettingUnspecifiedScopeRejected(t *testing.T) {
	t.Parallel()
	actor := settingsActor(t)
	srv, ctx := newSettingsServer(t, nil, actor)

	_, err := srv.SetSetting(ctx, &pluginv1.PluginHostServiceSetSettingRequest{
		Scope:      pluginv1.SettingScope_SETTING_SCOPE_UNSPECIFIED,
		Key:        "content.cw_block",
		StringList: []string{"violence"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestGetSettingMissingTokenFailsClosed: no dispatch token ⇒ the handler refuses
// to proceed with an empty subject (mirrors Evaluate's EMIT_TOKEN_MISSING).
func TestGetSettingMissingTokenFailsClosed(t *testing.T) {
	t.Parallel()
	actor := settingsActor(t)
	srv, _ := newSettingsServer(t, nil, actor)

	_, err := srv.GetSetting(context.Background(), &pluginv1.PluginHostServiceGetSettingRequest{
		Scope:       pluginv1.SettingScope_SETTING_SCOPE_PLAYER,
		PrincipalId: actor.ID,
		Key:         "content.cw_block",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EMIT_TOKEN_MISSING")
}

// TestGetSettingPlayerForeignPrincipalDenied: a plugin may only read the settings
// of the principal it is acting on behalf of. A foreign principal_id ⇒ denied.
func TestGetSettingPlayerForeignPrincipalDenied(t *testing.T) {
	t.Parallel()
	actor := settingsActor(t)
	srv, ctx := newSettingsServer(t, nil, actor)

	foreign := core.NewULID().String()
	_, err := srv.GetSetting(ctx, &pluginv1.PluginHostServiceGetSettingRequest{
		Scope:       pluginv1.SettingScope_SETTING_SCOPE_PLAYER,
		PrincipalId: foreign,
		Key:         "content.cw_block",
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err),
		"GetSetting MUST deny reading another principal's PLAYER settings")
}

// TestGetSettingCharacterForeignPrincipalDenied: the CHARACTER read arm.
func TestGetSettingCharacterForeignPrincipalDenied(t *testing.T) {
	t.Parallel()
	actor := settingsActor(t)
	srv, ctx := newSettingsServer(t, nil, actor)

	foreign := core.NewULID().String()
	_, err := srv.GetSetting(ctx, &pluginv1.PluginHostServiceGetSettingRequest{
		Scope:       pluginv1.SettingScope_SETTING_SCOPE_CHARACTER,
		PrincipalId: foreign,
		Key:         "content.cw_block",
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// TestSetSettingPlayerForeignPrincipalDenied: the write arm of principal ownership.
func TestSetSettingPlayerForeignPrincipalDenied(t *testing.T) {
	t.Parallel()
	actor := settingsActor(t)
	srv, ctx := newSettingsServer(t, nil, actor)

	foreign := core.NewULID().String()
	_, err := srv.SetSetting(ctx, &pluginv1.PluginHostServiceSetSettingRequest{
		Scope:       pluginv1.SettingScope_SETTING_SCOPE_PLAYER,
		PrincipalId: foreign,
		Key:         "content.cw_block",
		StringList:  []string{"violence"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// PLAYER own-principal round-trip is now covered by
// TestPlayerSettingRoundTripsForOwningPlayer below, which keys ownership on the
// host-vouched owning player carried on the dispatch token (holomush-iokti.19),
// replacing the former character-ID-equality stand-in.

// TestGetSettingNilStoreReturnsUnimplemented: an unwired store fails closed with
// Unimplemented rather than nil-dereferencing.
func TestGetSettingNilStoreReturnsUnimplemented(t *testing.T) {
	t.Parallel()
	actor := settingsActor(t)
	h := NewHost() // no settings stores wired
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	srv := &pluginHostServiceServer{host: h, pluginName: "plug-A"}
	ctx, _ := contextWithValidToken(t, srv, actor)

	assert.NotPanics(t, func() {
		_, err := srv.GetSetting(ctx, &pluginv1.PluginHostServiceGetSettingRequest{
			Scope:       pluginv1.SettingScope_SETTING_SCOPE_PLAYER,
			PrincipalId: actor.ID,
			Key:         "content.cw_block",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})
}

// TestSetSettingGameNilEngineReturnsUnimplemented: a GAME write with the store
// wired but no policy engine fails closed (cannot authorize) without panicking.
func TestSetSettingGameNilEngineReturnsUnimplemented(t *testing.T) {
	t.Parallel()
	actor := settingsActor(t)
	h := NewHost(WithGameSettings(settings.NewGameSettings(newMemSysInfo())))
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	srv := &pluginHostServiceServer{host: h, pluginName: "plug-A"}
	ctx, _ := contextWithValidToken(t, srv, actor)

	assert.NotPanics(t, func() {
		_, err := srv.SetSetting(ctx, &pluginv1.PluginHostServiceSetSettingRequest{
			Scope:      pluginv1.SettingScope_SETTING_SCOPE_GAME,
			Key:        "content.cw_taxonomy",
			StringList: []string{"violence"},
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})
}

// TestSetSettingGameOperatorAllowed: a subject granted write on the per-plugin
// resource "setting:game:plug-A" succeeds (holomush-iokti.15 Item 2: per-plugin
// GAME-write resource so operator policies can scope GAME-write per plugin).
func TestSetSettingGameOperatorAllowed(t *testing.T) {
	t.Parallel()
	actor := settingsActor(t)
	eng := policytest.NewGrantEngine()
	eng.Grant("character:"+actor.ID, "write", pluginauthz.SettingsGameWriteResource("plug-A"))
	srv, ctx := newSettingsServer(t, eng, actor)

	_, err := srv.SetSetting(ctx, &pluginv1.PluginHostServiceSetSettingRequest{
		Scope:      pluginv1.SettingScope_SETTING_SCOPE_GAME,
		Key:        "content.cw_taxonomy",
		StringList: []string{"violence", "gore"},
	})
	require.NoError(t, err)
}

// TestSetSettingGameNonOperatorDenied: a subject without the grant is denied.
func TestSetSettingGameNonOperatorDenied(t *testing.T) {
	t.Parallel()
	actor := settingsActor(t)
	eng := policytest.NewGrantEngine() // grants nothing → deny
	srv, ctx := newSettingsServer(t, eng, actor)

	_, err := srv.SetSetting(ctx, &pluginv1.PluginHostServiceSetSettingRequest{
		Scope:      pluginv1.SettingScope_SETTING_SCOPE_GAME,
		Key:        "content.cw_taxonomy",
		StringList: []string{"violence"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// TestGetSettingGameReadableByAnyPlugin: GAME reads need no engine and succeed
// even with a default-deny engine wired (finding-5 decision).
func TestGetSettingGameReadableByAnyPlugin(t *testing.T) {
	t.Parallel()
	actor := settingsActor(t)
	eng := policytest.NewGrantEngine() // would deny everything if consulted
	srv, ctx := newSettingsServer(t, eng, actor)

	resp, err := srv.GetSetting(ctx, &pluginv1.PluginHostServiceGetSettingRequest{
		Scope: pluginv1.SettingScope_SETTING_SCOPE_GAME,
		Key:   "content.cw_taxonomy",
	})
	require.NoError(t, err, "GAME reads are server-wide readable; no engine check")
	assert.False(t, resp.GetFound())
}

// TestSetSettingMissingTokenFailsClosed: no dispatch token on the write path ⇒
// the handler refuses to proceed (mirrors TestGetSettingMissingTokenFailsClosed,
// finding g3a4c.5).
func TestSetSettingMissingTokenFailsClosed(t *testing.T) {
	t.Parallel()
	actor := settingsActor(t)
	srv, _ := newSettingsServer(t, nil, actor)

	_, err := srv.SetSetting(context.Background(), &pluginv1.PluginHostServiceSetSettingRequest{
		Scope:       pluginv1.SettingScope_SETTING_SCOPE_PLAYER,
		PrincipalId: actor.ID,
		Key:         "content.cw_block",
		StringList:  []string{"violence"},
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EMIT_TOKEN_MISSING")
}

// TestCharacterSettingRoundTripsForOwnPrincipal: writing then reading one's OWN
// CHARACTER settings round-trips the list and leaves string_value empty (Phase 8).
// CHARACTER scope is correct and functional: principal_id == acting character ID
// (finding g3a4c.9 #1).
func TestCharacterSettingRoundTripsForOwnPrincipal(t *testing.T) {
	t.Parallel()
	actor := settingsActor(t)
	srv, ctx := newSettingsServer(t, nil, actor)

	_, err := srv.SetSetting(ctx, &pluginv1.PluginHostServiceSetSettingRequest{
		Scope:       pluginv1.SettingScope_SETTING_SCOPE_CHARACTER,
		PrincipalId: actor.ID,
		Key:         "content.cw_block",
		StringList:  []string{"violence", "gore"},
	})
	require.NoError(t, err)

	resp, err := srv.GetSetting(ctx, &pluginv1.PluginHostServiceGetSettingRequest{
		Scope:       pluginv1.SettingScope_SETTING_SCOPE_CHARACTER,
		PrincipalId: actor.ID,
		Key:         "content.cw_block",
	})
	require.NoError(t, err)
	assert.True(t, resp.GetFound())
	assert.Equal(t, []string{"violence", "gore"}, resp.GetStringList())
	assert.Empty(t, resp.GetStringValue(),
		"string_value MUST stay empty in Phase 8 (list-valued)")
}

// TestSettingInvalidPrincipalIDReturnsInvalidArgument: a malformed or empty
// principal_id fails before the ownership compare (ulid.Parse returns an error
// → InvalidArgument). Finding g3a4c.9 #2.
func TestSettingInvalidPrincipalIDReturnsInvalidArgument(t *testing.T) {
	t.Parallel()
	actor := settingsActor(t)
	srv, ctx := newSettingsServer(t, nil, actor)

	cases := []struct {
		name        string
		principalID string
	}{
		{"empty principal_id", ""},
		{"malformed non-ULID", "not-a-ulid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := srv.GetSetting(ctx, &pluginv1.PluginHostServiceGetSettingRequest{
				Scope:       pluginv1.SettingScope_SETTING_SCOPE_PLAYER,
				PrincipalId: tc.principalID,
				Key:         "content.cw_block",
			})
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err),
				"principal_id=%q must be rejected as InvalidArgument", tc.principalID)
		})
	}
}

// TestPlayerScopeDeniedWhenNoOwningPlayerVouched pins the fail-closed contract
// for PLAYER scope when the dispatch token carries NO host-vouched owning player
// (the iokti.16 test, recomment for the iokti.19 semantics). The default
// newSettingsServer helper issues a token with owner "", so even a valid player
// ULID principal is denied (PermissionDenied): the shared gate refuses a
// PLAYER request without a host-vouched owner. This is the binary mirror of the
// Lua "absent owning player" denial.
func TestPlayerScopeDeniedWhenNoOwningPlayerVouched(t *testing.T) {
	t.Parallel()
	actor := settingsActor(t) // ActorCharacter with character ULID
	srv, ctx := newSettingsServer(t, nil, actor)

	// A valid player ULID, but the token carries no owning player.
	playerULID := core.NewULID().String()

	_, err := srv.GetSetting(ctx, &pluginv1.PluginHostServiceGetSettingRequest{
		Scope:       pluginv1.SettingScope_SETTING_SCOPE_PLAYER,
		PrincipalId: playerULID,
		Key:         "content.cw_block",
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err),
		"PLAYER scope with no host-vouched owning player on the token MUST be "+
			"denied (fail closed, holomush-iokti.19)")
}

// TestPlayerSettingRoundTripsForOwningPlayer is the iokti.19 functional success
// test: when the dispatch token carries an owning player equal to the request's
// principal_id, PLAYER-scope set/get succeeds and round-trips the value. The
// owner partition is keyed by the player ULID. This is the binary counterpart of
// the Lua TestPlayerSettingRoundTripsForOwningPlayer — both runtimes converge on
// the same shared ownership gate (plugin-runtime-symmetry).
func TestPlayerSettingRoundTripsForOwningPlayer(t *testing.T) {
	t.Parallel()
	actor := settingsActor(t) // ActorCharacter
	owningPlayer := core.NewULID().String()

	srv, _ := newSettingsServer(t, nil, actor)
	ctx, _ := contextWithValidTokenOwning(t, srv, actor, owningPlayer)

	_, err := srv.SetSetting(ctx, &pluginv1.PluginHostServiceSetSettingRequest{
		Scope:       pluginv1.SettingScope_SETTING_SCOPE_PLAYER,
		PrincipalId: owningPlayer,
		Key:         "content.cw_block",
		StringList:  []string{"violence", "gore"},
	})
	require.NoError(t, err)

	resp, err := srv.GetSetting(ctx, &pluginv1.PluginHostServiceGetSettingRequest{
		Scope:       pluginv1.SettingScope_SETTING_SCOPE_PLAYER,
		PrincipalId: owningPlayer,
		Key:         "content.cw_block",
	})
	require.NoError(t, err)
	assert.True(t, resp.GetFound())
	assert.Equal(t, []string{"violence", "gore"}, resp.GetStringList())
}

// TestPlayerSettingDeniedWhenPrincipalNotOwningPlayer: even with a host-vouched
// owning player on the token, a principal_id that does NOT equal it is denied —
// a plugin cannot read/write another player's PLAYER settings (holomush-iokti.19).
func TestPlayerSettingDeniedWhenPrincipalNotOwningPlayer(t *testing.T) {
	t.Parallel()
	actor := settingsActor(t)
	owningPlayer := core.NewULID().String()
	otherPlayer := core.NewULID().String() // distinct from the vouched owner

	srv, _ := newSettingsServer(t, nil, actor)
	ctx, _ := contextWithValidTokenOwning(t, srv, actor, owningPlayer)

	_, err := srv.GetSetting(ctx, &pluginv1.PluginHostServiceGetSettingRequest{
		Scope:       pluginv1.SettingScope_SETTING_SCOPE_PLAYER,
		PrincipalId: otherPlayer,
		Key:         "content.cw_block",
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err),
		"PLAYER principal_id MUST equal the token's owning player")
}

// TestGameSettingOwnerPartitionIsolatedAcrossPlugins is the INV-PLUGIN-28 security test:
// a value written by plug-A under its owner partition is invisible to plug-B,
// because the owner is bound host-side from the authenticated plugin name.
func TestGameSettingOwnerPartitionIsolatedAcrossPlugins(t *testing.T) {
	t.Parallel()
	actor := settingsActor(t)
	shared := settings.NewGameSettings(newMemSysInfo())

	engA := policytest.NewGrantEngine()
	// Grant on plug-A's per-plugin resource only (holomush-iokti.15 Item 2).
	engA.Grant("character:"+actor.ID, "write", pluginauthz.SettingsGameWriteResource("plug-A"))
	srvA, ctxA := newSettingsServerWith(t, "plug-A", shared, engA, actor)

	_, err := srvA.SetSetting(ctxA, &pluginv1.PluginHostServiceSetSettingRequest{
		Scope:      pluginv1.SettingScope_SETTING_SCOPE_GAME,
		Key:        "content.cw_taxonomy",
		StringList: []string{"violence"},
	})
	require.NoError(t, err)

	// plug-A reads its own value back.
	respA, err := srvA.GetSetting(ctxA, &pluginv1.PluginHostServiceGetSettingRequest{
		Scope: pluginv1.SettingScope_SETTING_SCOPE_GAME,
		Key:   "content.cw_taxonomy",
	})
	require.NoError(t, err)
	require.True(t, respA.GetFound())
	assert.Equal(t, []string{"violence"}, respA.GetStringList())

	// plug-B, sharing the SAME game store, cannot see plug-A's owner partition.
	srvB, ctxB := newSettingsServerWith(t, "plug-B", shared, nil, actor)
	respB, err := srvB.GetSetting(ctxB, &pluginv1.PluginHostServiceGetSettingRequest{
		Scope: pluginv1.SettingScope_SETTING_SCOPE_GAME,
		Key:   "content.cw_taxonomy",
	})
	require.NoError(t, err)
	assert.False(t, respB.GetFound(),
		"INV-PLUGIN-28: plug-B MUST NOT read plug-A's owner partition")
}
