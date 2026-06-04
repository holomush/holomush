// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin_test

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/plugin/goplugin"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/internal/settings"
)

// TestWithEngineStoresEngineInHost is a behavioral wiring-guard for
// holomush-8kkv5.18. It verifies that goplugin.WithEngine correctly propagates
// the given engine into the host's internal engine field.
//
// In production, setup/subsystem.go Start() passes:
//
//	goplugin.WithEngine(s.cfg.ABAC.Engine())
//
// to goplugin.NewHost. Before this fix, WithEngine was absent from hostOpts,
// so s.host.engine was nil and PluginHostService.Evaluate always returned
// EVALUATE_ENGINE_UNCONFIGURED for every binary plugin call.
//
// This test confirms:
//  1. WithEngine is a valid HostOption (compile-time guard).
//  2. The engine stored in the host is the identical instance that was passed in
//     (identity guard — ensures the engine+resolver pair from BuildABACStack is
//     shared between the Lua hostfunc bridge and the binary host).
//
// stubAuditorForGoplugin is a minimal pluginauthz.Auditor implementation used
// by wiring-guard tests. It records nothing — its purpose is to provide a
// non-nil, identifiable Auditor instance for assert.Same checks.
type stubAuditorForGoplugin struct{}

func (s *stubAuditorForGoplugin) Log(_ context.Context, _ audit.Event) error { return nil }

// Compile-time: stubAuditorForGoplugin must satisfy pluginauthz.Auditor.
var _ pluginauthz.Auditor = (*stubAuditorForGoplugin)(nil)

// TestWithAuditLoggerStoresAuditorInHost is a behavioral wiring-guard for
// holomush-p1tq2.5 / INV-PLUGIN-25. It verifies that goplugin.WithAuditLogger
// correctly propagates the given Auditor into the host's internal auditor
// field, confirming the PluginHostService.Evaluate path will emit audit
// events.
//
// In production, setup/subsystem.go Start() passes:
//
//	goplugin.WithAuditLogger(s.cfg.ABAC.AuditLogger())
//
// to goplugin.NewHost. Without this propagation, PluginHostService.Evaluate
// silently drops all audit events regardless of the decision, violating
// spec §5 / INV-PLUGIN-25.
//
// This test confirms:
//  1. WithAuditLogger is a valid HostOption (compile-time guard).
//  2. The auditor stored in the host is the identical instance that was
//     passed in (identity guard — ensures the same *audit.Logger that
//     ABACSubsystem built is the one pluginauthz.Evaluate calls).
func TestWithAuditLoggerStoresAuditorInHost(t *testing.T) {
	aud := &stubAuditorForGoplugin{}

	host := goplugin.NewHost(goplugin.WithAuditLogger(aud))
	require.NotNil(t, host)

	got := host.AuditorForTest()
	assert.NotNil(t, got,
		"binary host MUST have a non-nil auditor after WithAuditLogger option is applied; "+
			"nil means PluginHostService.Evaluate never emits audit events (INV-PLUGIN-25 regression)")
	assert.Same(t, aud, got,
		"host must store the identical auditor instance passed to WithAuditLogger; "+
			"a different instance would break audit-log correlation across subsystems")
}

func TestWithEngineStoresEngineInHost(t *testing.T) {
	eng := policytest.AllowAllEngine()
	require.NotNil(t, eng)

	host := goplugin.NewHost(goplugin.WithEngine(eng))
	require.NotNil(t, host)

	got := host.EngineForTest()
	assert.NotNil(t, got,
		"binary host MUST have a non-nil engine after WithEngine option is applied; "+
			"nil means PluginHostService.Evaluate returns EVALUATE_ENGINE_UNCONFIGURED "+
			"for all binary plugins (holomush-8kkv5.18 regression)")
	assert.Same(t, eng, got,
		"host must store the identical engine instance passed to WithEngine; "+
			"a copy would break attribute-resolver sharing between Lua and binary surfaces")
}

// stubPlayerStoreForWiringGuard is a minimal settings.PlayerSettingsStore used
// purely to give assert.Same a concrete, identifiable pointer. It records
// nothing and is never called as part of these wiring-guard tests.
type stubPlayerStoreForWiringGuard struct{}

func (s *stubPlayerStoreForWiringGuard) For(_ context.Context, _ ulid.ULID) settings.Scoped {
	return nil
}

func (s *stubPlayerStoreForWiringGuard) SetString(_ context.Context, _ ulid.ULID, _, _ string) error {
	return nil
}

var _ settings.PlayerSettingsStore = (*stubPlayerStoreForWiringGuard)(nil)

// stubCharacterStoreForWiringGuard mirrors stubPlayerStoreForWiringGuard for
// the character settings store interface.
type stubCharacterStoreForWiringGuard struct{}

func (s *stubCharacterStoreForWiringGuard) For(_ context.Context, _ ulid.ULID) settings.Scoped {
	return nil
}

func (s *stubCharacterStoreForWiringGuard) SetString(_ context.Context, _ ulid.ULID, _, _ string) error {
	return nil
}

var _ settings.CharacterSettingsStore = (*stubCharacterStoreForWiringGuard)(nil)

// stubSysInfoForWiringGuard is a minimal settings.SystemInfoStore used to
// construct a concrete settings.GameSettings instance for assert.Same checks.
type stubSysInfoForWiringGuard struct{}

func (s *stubSysInfoForWiringGuard) GetSystemInfo(_ context.Context, _ string) (string, error) {
	return "", settings.ErrNotFound
}

func (s *stubSysInfoForWiringGuard) SetSystemInfo(_ context.Context, _, _ string) error {
	return nil
}

var _ settings.SystemInfoStore = (*stubSysInfoForWiringGuard)(nil)

// TestSetSettingsStoresPropagatesAllThreeStoresIntoHost is the production-wiring
// regression guard for the settings-store half of goplugin Host (holomush-iokti.15
// Item 1). It mirrors TestWithEngineStoresEngineInHost and
// TestWithAuditLoggerStoresAuditorInHost above.
//
// In production, cmd/holomush/sub_grpc.go ConfigureSettingsDeps calls
// host.SetSettingsStores(player, character, game) after the stores are
// assembled. Before this guard existed, a future refactor could silently drop
// that call (or re-wire one of the three stores incorrectly) and GetSetting /
// SetSetting RPCs would nil-deref at the store boundary — surfacing as
// Unimplemented rather than a startup-time panic.
//
// This test confirms:
//  1. SetSettingsStores is a valid method on *goplugin.Host (compile-time guard).
//  2. After SetSettingsStores(player, character, game), Host.PlayerSettings(),
//     Host.CharacterSettings(), and Host.GameSettings() all return the
//     non-nil, identical store instances that were injected (identity guard).
func TestSetSettingsStoresPropagatesAllThreeStoresIntoHost(t *testing.T) {
	// Three distinct non-nil store instances — concrete enough for assert.Same
	// checks without requiring a database.
	playerStore := &stubPlayerStoreForWiringGuard{}
	characterStore := &stubCharacterStoreForWiringGuard{}
	gameStore := settings.NewGameSettings(&stubSysInfoForWiringGuard{})

	host := goplugin.NewHost()
	require.NotNil(t, host)
	t.Cleanup(func() { _ = host.Close(context.Background()) })

	host.SetSettingsStores(playerStore, characterStore, gameStore)

	gotPlayer := host.PlayerSettingsForTest()
	assert.NotNil(t, gotPlayer,
		"binary host MUST have a non-nil PlayerSettingsStore after SetSettingsStores; "+
			"nil means all PLAYER-scope GetSetting/SetSetting RPCs fail Unimplemented "+
			"(settings-wiring regression guard, holomush-iokti.15)")
	assert.Same(t, playerStore, gotPlayer,
		"host must store the identical PlayerSettingsStore instance passed to SetSettingsStores; "+
			"a different instance would mean the wiring path silently substituted or lost the store")

	gotCharacter := host.CharacterSettingsForTest()
	assert.NotNil(t, gotCharacter,
		"binary host MUST have a non-nil CharacterSettingsStore after SetSettingsStores; "+
			"nil means all CHARACTER-scope GetSetting/SetSetting RPCs fail Unimplemented")
	assert.Same(t, characterStore, gotCharacter,
		"host must store the identical CharacterSettingsStore instance passed to SetSettingsStores")

	gotGame := host.GameSettingsForTest()
	assert.NotNil(t, gotGame,
		"binary host MUST have a non-nil GameSettings after SetSettingsStores; "+
			"nil means all GAME-scope GetSetting/SetSetting RPCs fail Unimplemented")
	assert.Same(t, gameStore, gotGame,
		"host must store the identical GameSettings instance passed to SetSettingsStores")
}

// TestWithSettingsOptionsPopulateHostStores is the construction-time companion to
// TestSetSettingsStoresPropagatesAllThreeStoresIntoHost (holomush-iokti.17 .3/.8).
// The earlier guard covers the late-bound SetSettingsStores path; this one covers
// the three HostOption constructors WithPlayerSettings / WithCharacterSettings /
// WithGameSettings, mirroring TestWithEngineStoresEngineInHost.
//
// Both wiring surfaces exist in production: the gRPC subsystem late-binds via
// SetSettingsStores, while NewHost callers (and tests) can inject at construction
// via the options. A future refactor that dropped or mis-wired one option would
// silently make the corresponding scope's GetSetting / SetSetting RPC fail
// Unimplemented; this guard pins each option to its host field.
//
// This test confirms:
//  1. WithPlayerSettings / WithCharacterSettings / WithGameSettings are valid
//     HostOptions (compile-time guard).
//  2. After NewHost with all three, Host.PlayerSettings(), CharacterSettings(),
//     and GameSettings() return the non-nil, identical instances injected
//     (identity guard).
func TestWithSettingsOptionsPopulateHostStores(t *testing.T) {
	playerStore := &stubPlayerStoreForWiringGuard{}
	characterStore := &stubCharacterStoreForWiringGuard{}
	gameStore := settings.NewGameSettings(&stubSysInfoForWiringGuard{})

	host := goplugin.NewHost(
		goplugin.WithPlayerSettings(playerStore),
		goplugin.WithCharacterSettings(characterStore),
		goplugin.WithGameSettings(gameStore),
	)
	require.NotNil(t, host)
	t.Cleanup(func() { _ = host.Close(context.Background()) })

	gotPlayer := host.PlayerSettingsForTest()
	assert.NotNil(t, gotPlayer,
		"binary host MUST have a non-nil PlayerSettingsStore after WithPlayerSettings; "+
			"nil means all PLAYER-scope GetSetting/SetSetting RPCs fail Unimplemented "+
			"(settings-wiring regression guard, holomush-iokti.17)")
	assert.Same(t, playerStore, gotPlayer,
		"host must store the identical PlayerSettingsStore instance passed to WithPlayerSettings")

	gotCharacter := host.CharacterSettingsForTest()
	assert.NotNil(t, gotCharacter,
		"binary host MUST have a non-nil CharacterSettingsStore after WithCharacterSettings; "+
			"nil means all CHARACTER-scope GetSetting/SetSetting RPCs fail Unimplemented")
	assert.Same(t, characterStore, gotCharacter,
		"host must store the identical CharacterSettingsStore instance passed to WithCharacterSettings")

	gotGame := host.GameSettingsForTest()
	assert.NotNil(t, gotGame,
		"binary host MUST have a non-nil GameSettings after WithGameSettings; "+
			"nil means all GAME-scope GetSetting/SetSetting RPCs fail Unimplemented")
	assert.Same(t, gameStore, gotGame,
		"host must store the identical GameSettings instance passed to WithGameSettings")
}
