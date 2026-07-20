// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/pkg/errutil"
)

// identityStoreRepo is a hand-rolled store.PluginRepo for IdentityStore tests.
//
// It exists so this file can exercise IdentityStore with ONLY its own
// collaborator (a store.PluginRepo, or nil). No *plugins.Manager is
// constructed anywhere in this file, so the ErrMissingVerbRegistry guard
// (INV-EVENTBUS-11) is never in play and no integration harness is needed.
// That is the D-02 / SC2 proof for this unit.
type identityStoreRepo struct {
	rows        []store.PluginRow
	listErr     error
	swept       []store.PluginRow
	sweepErr    error
	sweepCalled int
	sweepDays   int
}

func (r *identityStoreRepo) Upsert(
	_ context.Context,
	_ store.PluginUpsertInput,
) (ulid.ULID, *store.DriftReport, error) {
	return core.NewULID(), nil, nil
}

func (r *identityStoreRepo) ListAll(_ context.Context) ([]store.PluginRow, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.rows, nil
}

func (r *identityStoreRepo) SweepInactive(_ context.Context, retentionDays int) ([]store.PluginRow, error) {
	r.sweepCalled++
	r.sweepDays = retentionDays
	if r.sweepErr != nil {
		return nil, r.sweepErr
	}
	return r.swept, nil
}

func activeRow(id ulid.ULID, name string) store.PluginRow {
	return store.PluginRow{ID: id, Name: name, LastSeenAt: time.Now()}
}

func historicalRow(id ulid.ULID, name string) store.PluginRow {
	gc := time.Now()
	return store.PluginRow{ID: id, Name: name, LastSeenAt: time.Now(), GcAt: &gc}
}

// Behavior 1 — the D-02 / SC2 proof.
func TestNewIdentityStoreIsConstructibleWithOnlyANilRepo(t *testing.T) {
	s := plugins.NewIdentityStore(nil, 0)

	require.NotNil(t, s)

	id := core.NewULID()
	s.Register(id, "solo")

	name, ok := s.NameByID(id)
	require.True(t, ok, "a store built with no repo and no Manager MUST still resolve")
	assert.Equal(t, "solo", name)
}

// Behavior 2 — system sentinels land in nameByID only.
func TestBootstrapRegistersSystemSentinelsInNameByIDOnly(t *testing.T) {
	s := plugins.NewIdentityStore(nil, 0)
	require.NoError(t, s.Bootstrap(context.Background()))

	t.Run("resolves the system sentinel by id", func(t *testing.T) {
		name, ok := s.NameByID(core.SystemActorULID)
		require.True(t, ok)
		assert.Equal(t, "system", name)
	})

	t.Run("resolves the world-service sentinel by id", func(t *testing.T) {
		name, ok := s.NameByID(core.WorldServiceActorULID)
		require.True(t, ok)
		assert.Equal(t, "world-service", name)
	})

	t.Run("does not expose sentinels through IDByName", func(t *testing.T) {
		_, ok := s.IDByName("system")
		assert.False(t, ok, "sentinels are nameByID-only, never activeByName")

		_, ok = s.IDByName("world-service")
		assert.False(t, ok, "sentinels are nameByID-only, never activeByName")
	})
}

// Behavior 3 — register/unregister move as a pair (the loadPlugin rollback).
func TestRegisterResolvesBothDirectionsAndUnregisterRemovesBoth(t *testing.T) {
	s := plugins.NewIdentityStore(nil, 0)
	id := core.NewULID()

	s.Register(id, "core-scenes")

	name, ok := s.NameByID(id)
	require.True(t, ok)
	assert.Equal(t, "core-scenes", name)

	gotID, ok := s.IDByName("core-scenes")
	require.True(t, ok)
	assert.Equal(t, id, gotID)

	s.Unregister(id, "core-scenes")

	_, ok = s.NameByID(id)
	assert.False(t, ok, "rollback MUST remove the nameByID entry it just added")
	_, ok = s.IDByName("core-scenes")
	assert.False(t, ok, "rollback MUST remove the activeByName entry it just added")
}

// Behavior 4 — INV-PLUGIN-17. Both halves asserted in ONE test so a future
// edit cannot satisfy half of it.
func TestDeactivateClearsActiveNameButRetainsHistoricalIDResolution(t *testing.T) {
	s := plugins.NewIdentityStore(nil, 0)
	id := core.NewULID()
	s.Register(id, "core-scenes")

	s.Deactivate("core-scenes")

	_, stillActive := s.IDByName("core-scenes")
	assert.False(t, stillActive, "deactivation MUST remove the active name binding")

	name, ok := s.NameByID(id)
	require.True(t, ok, "INV-PLUGIN-17: historical ULID resolution MUST survive deactivation")
	assert.Equal(t, "core-scenes", name)
}

// Behavior 5 — nil repo degrades to sentinel-only resolution, never errors.
func TestBootstrapWithNilRepoRegistersSentinelsWithoutError(t *testing.T) {
	s := plugins.NewIdentityStore(nil, 3)

	require.NoError(t, s.Bootstrap(context.Background()))

	_, ok := s.NameByID(core.SystemActorULID)
	assert.True(t, ok, "a repo-less store MUST still resolve sentinels")
}

func TestBootstrapHydratesActiveAndHistoricalRowsFromTheRepo(t *testing.T) {
	activeID, historicalID := core.NewULID(), core.NewULID()
	repo := &identityStoreRepo{rows: []store.PluginRow{
		activeRow(activeID, "live"),
		historicalRow(historicalID, "retired"),
	}}
	s := plugins.NewIdentityStore(repo, 3)

	require.NoError(t, s.Bootstrap(context.Background()))

	t.Run("an active row resolves in both directions", func(t *testing.T) {
		name, ok := s.NameByID(activeID)
		require.True(t, ok)
		assert.Equal(t, "live", name)

		gotID, ok := s.IDByName("live")
		require.True(t, ok)
		assert.Equal(t, activeID, gotID)
	})

	t.Run("a gc_at row resolves by id only", func(t *testing.T) {
		name, ok := s.NameByID(historicalID)
		require.True(t, ok)
		assert.Equal(t, "retired", name)

		_, ok = s.IDByName("retired")
		assert.False(t, ok, "a deactivated row MUST NOT resolve as active")
	})
}

func TestBootstrapRejectsAPluginRowUsingAReservedSentinelULID(t *testing.T) {
	repo := &identityStoreRepo{rows: []store.PluginRow{
		activeRow(core.SystemActorULID, "impostor"),
	}}
	s := plugins.NewIdentityStore(repo, 3)

	err := s.Bootstrap(context.Background())

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_ROW_USES_SENTINEL_ID")
}

func TestBootstrapWrapsARepoListFailure(t *testing.T) {
	repo := &identityStoreRepo{listErr: errors.New("boom")}
	s := plugins.NewIdentityStore(repo, 3)

	err := s.Bootstrap(context.Background())

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_MANAGER_BOOTSTRAP")
}

// Behavior 6 — the retention sweep retains nameByID and honours both guards.
func TestSweepRemovesActiveBindingsButRetainsHistoricalIDResolution(t *testing.T) {
	staleID := core.NewULID()
	repo := &identityStoreRepo{swept: []store.PluginRow{activeRow(staleID, "stale")}}
	s := plugins.NewIdentityStore(repo, 3)
	s.Register(staleID, "stale")

	swept, err := s.Sweep(context.Background())

	require.NoError(t, err)
	require.Len(t, swept, 1, "Sweep MUST return the swept rows so callers can log them")
	assert.Equal(t, 3, repo.sweepDays, "the configured retention window MUST reach the repo")

	_, ok := s.IDByName("stale")
	assert.False(t, ok, "a swept plugin MUST NOT remain active")

	name, ok := s.NameByID(staleID)
	require.True(t, ok, "INV-PLUGIN-17: a swept plugin's historical resolution MUST survive")
	assert.Equal(t, "stale", name)
}

func TestSweepIsANoOpWhenDisabledOrRepoless(t *testing.T) {
	t.Run("retention of zero disables the sweep", func(t *testing.T) {
		repo := &identityStoreRepo{}
		s := plugins.NewIdentityStore(repo, 0)

		swept, err := s.Sweep(context.Background())

		require.NoError(t, err)
		assert.Empty(t, swept)
		assert.Equal(t, 0, repo.sweepCalled, "SweepInactive MUST NOT be called when retention is 0")
	})

	t.Run("a nil repo disables the sweep", func(t *testing.T) {
		s := plugins.NewIdentityStore(nil, 3)

		swept, err := s.Sweep(context.Background())

		require.NoError(t, err)
		assert.Empty(t, swept)
	})
}

func TestSweepWrapsARepoFailure(t *testing.T) {
	repo := &identityStoreRepo{sweepErr: errors.New("boom")}
	s := plugins.NewIdentityStore(repo, 3)

	_, err := s.Sweep(context.Background())

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_MANAGER_SWEEP")
}

func TestHasRepoReportsWhetherPersistenceIsWired(t *testing.T) {
	assert.False(t, plugins.NewIdentityStore(nil, 3).HasRepo())
	assert.True(t, plugins.NewIdentityStore(&identityStoreRepo{}, 3).HasRepo())
}
