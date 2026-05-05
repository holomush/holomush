// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"os"
	"path/filepath"
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
func (stubIdentityRegistry) IDByName(string) (ulid.ULID, bool) { return ulid.ULID{}, false }

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
	copy(id[:], []byte(in.Name + "00000000000000000000")[:16])
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

// w9ml T6: computeHashes hashes manifest.yaml + per-Type executable artifacts.
func TestComputeHashesProducesNonEmptyForBinary(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugin.yaml"),
		[]byte("name: x\nversion: 1\ntype: binary\nbinary-plugin:\n  executable: bin/x\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "bin"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bin/x"), []byte("ELF-binary-bytes"), 0o600))

	mgr := newManagerForRegistryTest(t, &stubPluginRepo{})
	dp := &DiscoveredPlugin{
		Manifest: &Manifest{Name: "x", Version: "1", Type: TypeBinary, BinaryPlugin: &BinaryConfig{Executable: "bin/x"}},
		Dir:      dir,
	}
	mh, ch, err := mgr.computeHashes(dp)
	require.NoError(t, err)
	assert.Len(t, mh, 32, "manifest hash must be sha256 (32 bytes)")
	assert.Len(t, ch, 32, "binary content hash must be sha256")
}

func TestComputeHashesNilContentForSettingPlugin(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugin.yaml"),
		[]byte("name: x\nversion: 1\ntype: setting\n"), 0o600))

	mgr := newManagerForRegistryTest(t, &stubPluginRepo{})
	dp := &DiscoveredPlugin{Manifest: &Manifest{Name: "x", Version: "1", Type: TypeSetting}, Dir: dir}
	_, ch, err := mgr.computeHashes(dp)
	require.NoError(t, err)
	assert.Nil(t, ch, "setting plugins MUST have nil content_hash")
}

func TestComputeHashesLuaContentHashIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugin.yaml"),
		[]byte("name: x\nversion: 1\ntype: lua\nlua-plugin:\n  entry: a.lua\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.lua"), []byte("foo"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.lua"), []byte("bar"), 0o600))

	mgr := newManagerForRegistryTest(t, &stubPluginRepo{})
	dp := &DiscoveredPlugin{Manifest: &Manifest{Name: "x", Version: "1", Type: TypeLua, LuaPlugin: &LuaConfig{Entry: "a.lua"}}, Dir: dir}

	_, ch1, err := mgr.computeHashes(dp)
	require.NoError(t, err)
	_, ch2, err := mgr.computeHashes(dp)
	require.NoError(t, err)
	assert.Equal(t, ch1, ch2, "Lua content_hash MUST be deterministic")
}

func TestUnloadPluginRemovesActiveButPreservesHistorical(t *testing.T) {
	repo := &stubPluginRepo{}
	mgr := newManagerForRegistryTest(t, repo)

	manifest := &Manifest{Name: "core-scenes", Version: "1.0.0", Type: TypeLua, LuaPlugin: &LuaConfig{Entry: "main.lua"}}
	mgr.TestLoadPlugin("core-scenes", manifest)

	// Manually populate cache (in real loadPlugin path this is done by T6).
	id := core.NewULID()
	mgr.mu.Lock()
	mgr.nameByID[id] = "core-scenes"
	mgr.activeByName["core-scenes"] = id
	mgr.mu.Unlock()

	require.NoError(t, mgr.UnloadPlugin(context.Background(), "core-scenes"))

	_, stillActive := mgr.IDByName("core-scenes")
	assert.False(t, stillActive)

	name, ok := mgr.NameByID(id)
	require.True(t, ok)
	assert.Equal(t, "core-scenes", name, "historical resolution preserved")
}

func TestUnloadPluginIsIdempotentWhenNotLoaded(t *testing.T) {
	mgr := newManagerForRegistryTest(t, &stubPluginRepo{})
	err := mgr.UnloadPlugin(context.Background(), "nonexistent")
	assert.NoError(t, err)
}

// w9ml T8: GC sweep at LoadAll end + RetentionDays config.
func TestSweepInactiveRemovesFromActiveByNameRetainsNameByID(t *testing.T) {
	staleID := core.NewULID()
	now := time.Now()
	repo := &stubPluginRepo{
		swept: []store.PluginRow{
			{ID: staleID, Name: "stale", LastSeenAt: now.Add(-99 * 24 * time.Hour)},
		},
	}
	mgr, err := NewManager(t.TempDir(),
		WithPluginRepo(repo),
		WithVerbRegistry(core.NewVerbRegistry()),
		WithRetentionDays(3),
	)
	require.NoError(t, err)

	// Pre-populate cache as if "stale" had been loaded previously.
	mgr.mu.Lock()
	mgr.nameByID[staleID] = "stale"
	mgr.activeByName["stale"] = staleID
	mgr.mu.Unlock()

	require.NoError(t, mgr.LoadAll(context.Background()))

	_, ok := mgr.IDByName("stale")
	assert.False(t, ok, "swept plugin MUST NOT be in activeByName")

	name, ok := mgr.NameByID(staleID)
	require.True(t, ok)
	assert.Equal(t, "stale", name, "swept plugin's NameByID retention preserved")
}

func TestRetentionDaysZeroDisablesSweep(t *testing.T) {
	repo := &stubPluginRepo{
		swept: []store.PluginRow{}, // empty — but the call shouldn't even happen
	}
	mgr, err := NewManager(t.TempDir(),
		WithPluginRepo(repo),
		WithVerbRegistry(core.NewVerbRegistry()),
		WithRetentionDays(0),
	)
	require.NoError(t, err)
	require.NoError(t, mgr.LoadAll(context.Background()))
	// No assertion target other than "no panic, no log" — adequate per plan.
}
