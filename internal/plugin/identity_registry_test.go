// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/pkg/errutil"
)

// stubIdentityRegistry verifies that the interface can be satisfied
// independently of *Manager (the *Manager conformance is added in T5).
type stubIdentityRegistry struct{}

func (stubIdentityRegistry) NameByID(ulid.ULID) (string, bool) { return "", false }
func (stubIdentityRegistry) IDByName(string) (ulid.ULID, bool)  { return ulid.ULID{}, false }

func TestIdentityRegistryInterfaceIsSatisfiable(_ *testing.T) {
	var _ IdentityRegistry = stubIdentityRegistry{}
}

// stubPluginRepo lets us drive Manager bootstrap without Postgres.
type stubPluginRepo struct {
	rows    []store.PluginRow
	swept   []store.PluginRow
	upserts []store.PluginUpsertInput
}

func (s *stubPluginRepo) Upsert(_ context.Context, in store.PluginUpsertInput) (ulid.ULID, *store.DriftReport, error) {
	s.upserts = append(s.upserts, in)
	for _, r := range s.rows {
		if r.Name == in.Name && r.GcAt == nil {
			return r.ID, nil, nil
		}
	}
	id := ulid.ULID{}
	copy(id[:], []byte(in.Name+"00000000000000000000")[:16])
	return id, nil, nil
}
func (s *stubPluginRepo) ListAll(_ context.Context) ([]store.PluginRow, error) {
	return s.rows, nil
}
func (s *stubPluginRepo) SweepInactive(_ context.Context, _ int) ([]store.PluginRow, error) {
	return s.swept, nil
}

func newManagerForRegistryTest(t *testing.T, repo store.PluginRepo) *Manager {
	t.Helper()
	// NewManager enforces INV-GW-10: a VerbRegistry MUST be passed.
	mgr, err := NewManager(t.TempDir(),
		WithPluginRepo(repo),
		WithVerbRegistry(core.NewVerbRegistry()),
	)
	require.NoError(t, err)
	return mgr
}

// Compile-time conformance — added once Manager has the methods.
var _ IdentityRegistry = (*Manager)(nil)

func TestManagerNameByIDResolvesSystemSentinels(t *testing.T) {
	mgr := newManagerForRegistryTest(t, &stubPluginRepo{})

	name, ok := mgr.NameByID(core.SystemActorULID)
	require.True(t, ok)
	assert.Equal(t, "system", name)

	name, ok = mgr.NameByID(core.WorldServiceActorULID)
	require.True(t, ok)
	assert.Equal(t, "world-service", name)
}

func TestManagerIDByNameDoesNotResolveSentinelLabels(t *testing.T) {
	mgr := newManagerForRegistryTest(t, &stubPluginRepo{})

	_, ok := mgr.IDByName("system")
	assert.False(t, ok, "system label MUST NOT be resolvable via IDByName")
	_, ok = mgr.IDByName("world-service")
	assert.False(t, ok, "world-service label MUST NOT be resolvable via IDByName")
}

func TestManagerNameByIDReturnsFalseForUnregisteredULID(t *testing.T) {
	mgr := newManagerForRegistryTest(t, &stubPluginRepo{})

	random := core.NewULID()
	_, ok := mgr.NameByID(random)
	assert.False(t, ok)
}

func TestManagerBootstrapRefusesPluginRowWithSentinelULID(t *testing.T) {
	repo := &stubPluginRepo{
		rows: []store.PluginRow{{
			ID:   core.SystemActorULID,
			Name: "evil-plugin",
		}},
	}
	_, err := NewManager(t.TempDir(),
		WithPluginRepo(repo),
		WithVerbRegistry(core.NewVerbRegistry()),
	)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_ROW_USES_SENTINEL_ID")
}

func TestManagerBootstrapPopulatesNameByIDFromActiveAndHistoricalRows(t *testing.T) {
	now := time.Now()
	gcAt := now.Add(-7 * 24 * time.Hour)
	activeID := core.NewULID()
	histID := core.NewULID()

	repo := &stubPluginRepo{rows: []store.PluginRow{
		{ID: activeID, Name: "active-plugin", GcAt: nil},
		{ID: histID, Name: "old-plugin", GcAt: &gcAt},
	}}
	mgr := newManagerForRegistryTest(t, repo)

	name, ok := mgr.NameByID(activeID)
	require.True(t, ok)
	assert.Equal(t, "active-plugin", name)

	name, ok = mgr.NameByID(histID)
	require.True(t, ok)
	assert.Equal(t, "old-plugin", name)

	id, ok := mgr.IDByName("active-plugin")
	require.True(t, ok)
	assert.Equal(t, activeID, id)

	_, ok = mgr.IDByName("old-plugin")
	assert.False(t, ok, "deactivated plugin name MUST NOT resolve via IDByName")
}
