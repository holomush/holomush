# Plugin Manifest Alias Declarations — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Plugins declare command aliases in `plugin.yaml`; the loader seeds them to the database, replacing the hardcoded bootstrap alias list.

**Architecture:** Add `Aliases []string` to `CommandSpec`, collect aliases from loaded manifests in `Manager`, seed via existing `AliasSeeder` "insert if absent" path, remove `AliasBootstrapper` and `internal/bootstrap/aliases.go`.

**Tech Stack:** Go, YAML manifest parsing, testify, Ginkgo/Gomega (integration), testcontainers-go (Postgres)

**Spec:** `docs/superpowers/specs/2026-04-06-plugin-manifest-alias-declarations-design.md`

---

## File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `internal/plugin/manifest.go:107-148` | Add `Aliases` field to `CommandSpec`, validate in `CommandSpec.Validate()`, check duplicates in `Manifest.Validate()` |
| Modify | `internal/plugin/manifest_test.go` | Tests for alias parsing, validation, duplicate rejection |
| Create | `internal/plugin/alias_seeder.go` | `CollectManifestAliases()` + `SeedManifestAliases()` functions |
| Create | `internal/plugin/alias_seeder_test.go` | Unit tests for collection and seeding logic |
| Modify | `internal/plugin/manager.go:20-30,182-201` | Add `AliasSeeder`/`AliasCache` options, call `SeedManifestAliases` after `LoadAll` |
| Modify | `internal/plugin/manager_test.go` | Tests for alias seeding integration in `LoadAll` |
| Modify | `plugins/core-communication/plugin.yaml` | Add aliases to say, pose, page, whisper commands |
| Modify | `plugins/core-objects/plugin.yaml` | Add alias to describe command |
| Delete | `internal/bootstrap/aliases.go` | Hardcoded alias list — replaced by manifests |
| Delete | `internal/bootstrap/aliases_test.go` | Tests for deleted file |
| Delete | `internal/bootstrap/alias_bootstrap.go` | `AliasBootstrapper` adapter — replaced by loader seeding |
| Delete | `internal/bootstrap/alias_bootstrap_test.go` | Tests for deleted file |
| Modify | `internal/bootstrap/setup/subsystem.go:202-205` | Remove `AliasBootstrapper` registration |
| Create | `internal/plugin/alias_seeder_integration_test.go` | E2E tests: full startup → DB verification, operator override, persistence |

---

## Task 1: Add `Aliases` field to `CommandSpec` with validation

**Files:**

- Modify: `internal/plugin/manifest.go:107-148`

- [ ] **Step 1: Write failing test — alias field parses from YAML**

Add to `internal/plugin/manifest_test.go`:

```go
func TestParseManifestCommandWithAliases(t *testing.T) {
	yamlData := `
name: test-comm
version: 1.0.0
type: lua
commands:
  - name: say
    aliases:
      - '"'
    help: Say something
  - name: pose
    aliases:
      - ":"
      - ";"
    help: Pose an action
lua-plugin:
  entry: main.lua
`
	m, err := ParseManifest([]byte(yamlData))
	require.NoError(t, err)
	require.Len(t, m.Commands, 2)
	assert.Equal(t, []string{`"`}, m.Commands[0].Aliases)
	assert.Equal(t, []string{":", ";"}, m.Commands[1].Aliases)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestParseManifestCommandWithAliases ./internal/plugin/`
Expected: FAIL — `CommandSpec` has no `Aliases` field.

- [ ] **Step 3: Add `Aliases` field to `CommandSpec`**

In `internal/plugin/manifest.go`, add the field after `Name`:

```go
type CommandSpec struct {
	// Name is the canonical command name (e.g., "say", "teleport").
	Name string `yaml:"name" json:"name" jsonschema:"required,minLength=1"`

	// Aliases lists trigger strings that expand to this command.
	// Each alias is seeded as a system alias during plugin loading.
	Aliases []string `yaml:"aliases,omitempty" json:"aliases,omitempty"`

	// ... remaining fields unchanged ...
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestParseManifestCommandWithAliases ./internal/plugin/`
Expected: PASS

- [ ] **Step 5: Write failing test — empty alias string rejected**

Add to `internal/plugin/manifest_test.go`:

```go
func TestCommandSpecValidateRejectsEmptyAlias(t *testing.T) {
	spec := CommandSpec{
		Name:    "say",
		Aliases: []string{""},
	}
	err := spec.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `task test -- -run TestCommandSpecValidateRejectsEmptyAlias ./internal/plugin/`
Expected: FAIL — no validation for empty aliases yet.

- [ ] **Step 7: Add alias validation to `CommandSpec.Validate()`**

In `internal/plugin/manifest.go`, add to `CommandSpec.Validate()` before the return:

```go
func (c *CommandSpec) Validate() error {
	if err := command.ValidateCommandName(c.Name); err != nil {
		return oops.In("command").Wrap(err)
	}

	if c.HelpText != "" && c.HelpFile != "" {
		return oops.In("command").With("name", c.Name).New("cannot specify both helpText and helpFile")
	}

	// Validate aliases
	seenAliases := make(map[string]bool)
	for i, alias := range c.Aliases {
		if alias == "" {
			return oops.In("command").With("name", c.Name).With("alias_index", i).New("alias must not be empty")
		}
		if seenAliases[alias] {
			return oops.In("command").With("name", c.Name).With("alias", alias).New("duplicate alias")
		}
		seenAliases[alias] = true
	}

	for i, cap := range c.Capabilities {
		if err := cap.Validate(); err != nil {
			return oops.In("command").With("name", c.Name).With("capability_index", i).Wrap(err)
		}
	}

	return nil
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `task test -- -run TestCommandSpecValidateRejectsEmptyAlias ./internal/plugin/`
Expected: PASS

- [ ] **Step 9: Write failing test — duplicate alias within command rejected**

Add to `internal/plugin/manifest_test.go`:

```go
func TestCommandSpecValidateRejectsDuplicateAlias(t *testing.T) {
	spec := CommandSpec{
		Name:    "pose",
		Aliases: []string{":", ":"},
	}
	err := spec.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate alias")
}
```

- [ ] **Step 10: Run test to verify it passes (validation already handles this)**

Run: `task test -- -run TestCommandSpecValidateRejectsDuplicateAlias ./internal/plugin/`
Expected: PASS — the validation added in Step 7 already catches this.

- [ ] **Step 11: Write test — manifest without aliases parses normally**

Add to `internal/plugin/manifest_test.go`:

```go
func TestParseManifestCommandWithoutAliasesBackwardCompatible(t *testing.T) {
	yamlData := `
name: test-plugin
version: 1.0.0
type: lua
commands:
  - name: look
    help: Look around
lua-plugin:
  entry: main.lua
`
	m, err := ParseManifest([]byte(yamlData))
	require.NoError(t, err)
	require.Len(t, m.Commands, 1)
	assert.Nil(t, m.Commands[0].Aliases)
}
```

- [ ] **Step 12: Run test to verify it passes**

Run: `task test -- -run TestParseManifestCommandWithoutAliasesBackwardCompatible ./internal/plugin/`
Expected: PASS

- [ ] **Step 13: Run full manifest test suite**

Run: `task test -- ./internal/plugin/ -run TestParseManifest`
Expected: All existing tests still PASS.

- [ ] **Step 14: Commit**

```
feat(plugin): add Aliases field to CommandSpec with validation

Commands can now declare aliases in plugin.yaml. Empty strings and
duplicates within a command are rejected at parse time.
```

---

## Task 2: Add `CollectManifestAliases` and `SeedManifestAliases` functions

**Files:**

- Create: `internal/plugin/alias_seeder.go`
- Create: `internal/plugin/alias_seeder_test.go`

- [ ] **Step 1: Write failing test — `CollectManifestAliases` gathers aliases from manifests**

Create `internal/plugin/alias_seeder_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectManifestAliasesGathersFromMultiplePlugins(t *testing.T) {
	plugins := []*DiscoveredPlugin{
		{Manifest: &Manifest{
			Name: "core-communication",
			Commands: []CommandSpec{
				{Name: "say", Aliases: []string{`"`}},
				{Name: "pose", Aliases: []string{":", ";"}},
			},
		}},
		{Manifest: &Manifest{
			Name: "core-objects",
			Commands: []CommandSpec{
				{Name: "describe", Aliases: []string{"desc"}},
			},
		}},
	}

	aliases, err := CollectManifestAliases(plugins)
	require.NoError(t, err)
	require.Len(t, aliases, 4)

	expected := map[string]ManifestAlias{
		`"`:    {Alias: `"`, Command: "say", Plugin: "core-communication"},
		":":    {Alias: ":", Command: "pose", Plugin: "core-communication"},
		";":    {Alias: ";", Command: "pose", Plugin: "core-communication"},
		"desc": {Alias: "desc", Command: "describe", Plugin: "core-objects"},
	}
	for _, a := range aliases {
		want, ok := expected[a.Alias]
		assert.True(t, ok, "unexpected alias: %s", a.Alias)
		assert.Equal(t, want.Command, a.Command)
		assert.Equal(t, want.Plugin, a.Plugin)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestCollectManifestAliasesGathersFromMultiplePlugins ./internal/plugin/`
Expected: FAIL — `CollectManifestAliases` and `ManifestAlias` do not exist.

- [ ] **Step 3: Implement `CollectManifestAliases`**

Create `internal/plugin/alias_seeder.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"log/slog"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
)

// ManifestAlias is an alias declaration collected from a plugin manifest.
type ManifestAlias struct {
	Alias   string
	Command string
	Plugin  string
}

// AliasSeeder is the subset of store.AliasRepository needed for seeding.
type AliasSeeder interface {
	GetSystemAliases(ctx context.Context) (map[string]string, error)
	SetSystemAlias(ctx context.Context, alias, command, createdBy string) error
}

// CollectManifestAliases gathers all alias declarations from loaded plugin
// manifests. If two plugins declare the same alias, the first one (by slice
// order, which reflects load order) wins and a warning is logged. The
// duplicate is omitted from the result.
func CollectManifestAliases(loaded []*DiscoveredPlugin) ([]ManifestAlias, error) {
	var result []ManifestAlias
	seen := make(map[string]string) // alias → owning plugin name

	for _, dp := range loaded {
		for _, cmd := range dp.Manifest.Commands {
			for _, alias := range cmd.Aliases {
				if owner, exists := seen[alias]; exists {
					slog.Warn("duplicate alias across plugins, skipping",
						"alias", alias,
						"command", cmd.Name,
						"plugin", dp.Manifest.Name,
						"owner", owner,
					)
					continue
				}
				seen[alias] = dp.Manifest.Name
				result = append(result, ManifestAlias{
					Alias:   alias,
					Command: cmd.Name,
					Plugin:  dp.Manifest.Name,
				})
			}
		}
	}
	return result, nil
}

// SeedManifestAliases persists collected aliases to the database and loads
// all system aliases into the cache. Uses "insert if absent" semantics —
// existing aliases are not overwritten.
func SeedManifestAliases(ctx context.Context, aliases []ManifestAlias, repo AliasSeeder, cache *command.AliasCache) error {
	existing, err := repo.GetSystemAliases(ctx)
	if err != nil {
		return oops.With("operation", "get existing aliases").Wrap(err)
	}

	var seeded []string
	for _, a := range aliases {
		if _, exists := existing[a.Alias]; exists {
			continue
		}
		if err := repo.SetSystemAlias(ctx, a.Alias, a.Command, a.Plugin); err != nil {
			slog.Error("failed to seed alias",
				"alias", a.Alias,
				"command", a.Command,
				"plugin", a.Plugin,
				"error", err,
			)
			continue
		}
		seeded = append(seeded, a.Alias)
	}

	if len(seeded) > 0 {
		slog.Info("seeded plugin aliases", "aliases", seeded)
	}

	// Reload all system aliases into cache.
	all, reloadErr := repo.GetSystemAliases(ctx)
	if reloadErr != nil {
		return oops.With("operation", "reload aliases into cache").Wrap(reloadErr)
	}
	cache.LoadSystemAliases(all)

	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestCollectManifestAliasesGathersFromMultiplePlugins ./internal/plugin/`
Expected: PASS

- [ ] **Step 5: Write failing test — cross-plugin duplicate alias skipped**

Add to `internal/plugin/alias_seeder_test.go`:

```go
func TestCollectManifestAliasesSkipsDuplicateAcrossPlugins(t *testing.T) {
	plugins := []*DiscoveredPlugin{
		{Manifest: &Manifest{
			Name: "plugin-a",
			Commands: []CommandSpec{
				{Name: "say", Aliases: []string{`"`}},
			},
		}},
		{Manifest: &Manifest{
			Name: "plugin-b",
			Commands: []CommandSpec{
				{Name: "speak", Aliases: []string{`"`}},
			},
		}},
	}

	aliases, err := CollectManifestAliases(plugins)
	require.NoError(t, err)
	require.Len(t, aliases, 1)
	assert.Equal(t, "say", aliases[0].Command)
	assert.Equal(t, "plugin-a", aliases[0].Plugin)
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `task test -- -run TestCollectManifestAliasesSkipsDuplicateAcrossPlugins ./internal/plugin/`
Expected: PASS — already handled by `CollectManifestAliases`.

- [ ] **Step 7: Write failing test — `CollectManifestAliases` returns empty for no aliases**

Add to `internal/plugin/alias_seeder_test.go`:

```go
func TestCollectManifestAliasesReturnsEmptyWhenNoAliases(t *testing.T) {
	plugins := []*DiscoveredPlugin{
		{Manifest: &Manifest{
			Name:     "no-aliases",
			Commands: []CommandSpec{{Name: "look"}},
		}},
	}

	aliases, err := CollectManifestAliases(plugins)
	require.NoError(t, err)
	assert.Empty(t, aliases)
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `task test -- -run TestCollectManifestAliasesReturnsEmptyWhenNoAliases ./internal/plugin/`
Expected: PASS

- [ ] **Step 9: Write failing test — `SeedManifestAliases` seeds to repo and loads cache**

Add to `internal/plugin/alias_seeder_test.go`:

```go
func TestSeedManifestAliasesSeedsNewAndLoadsCache(t *testing.T) {
	repo := &fakeAliasSeeder{existing: map[string]string{}}
	cache := command.NewAliasCache()

	aliases := []ManifestAlias{
		{Alias: `"`, Command: "say", Plugin: "core-communication"},
		{Alias: "desc", Command: "describe", Plugin: "core-objects"},
	}

	err := SeedManifestAliases(context.Background(), aliases, repo, cache)
	require.NoError(t, err)

	// Verify repo was called
	assert.Equal(t, "say", repo.existing[`"`])
	assert.Equal(t, "describe", repo.existing["desc"])

	// Verify cache was loaded
	all := cache.ListSystemAliases()
	assert.Equal(t, "say", all[`"`])
	assert.Equal(t, "describe", all["desc"])
}
```

- [ ] **Step 10: Write the `fakeAliasSeeder` test helper**

Add to `internal/plugin/alias_seeder_test.go`:

```go
// fakeAliasSeeder is a minimal in-memory implementation for unit tests.
type fakeAliasSeeder struct {
	existing  map[string]string
	creators  map[string]string
	setErr    error
}

func (f *fakeAliasSeeder) GetSystemAliases(_ context.Context) (map[string]string, error) {
	result := make(map[string]string, len(f.existing))
	for k, v := range f.existing {
		result[k] = v
	}
	return result, nil
}

func (f *fakeAliasSeeder) SetSystemAlias(_ context.Context, alias, cmd, createdBy string) error {
	if f.setErr != nil {
		return f.setErr
	}
	if f.existing == nil {
		f.existing = make(map[string]string)
	}
	if f.creators == nil {
		f.creators = make(map[string]string)
	}
	f.existing[alias] = cmd
	f.creators[alias] = createdBy
	return nil
}
```

- [ ] **Step 11: Run test to verify it passes**

Run: `task test -- -run TestSeedManifestAliasesSeedsNewAndLoadsCache ./internal/plugin/`
Expected: PASS

- [ ] **Step 12: Write failing test — `SeedManifestAliases` skips existing aliases**

Add to `internal/plugin/alias_seeder_test.go`:

```go
func TestSeedManifestAliasesSkipsExistingAliases(t *testing.T) {
	repo := &fakeAliasSeeder{
		existing: map[string]string{`"`: "shout"},
	}
	cache := command.NewAliasCache()

	aliases := []ManifestAlias{
		{Alias: `"`, Command: "say", Plugin: "core-communication"},
		{Alias: "desc", Command: "describe", Plugin: "core-objects"},
	}

	err := SeedManifestAliases(context.Background(), aliases, repo, cache)
	require.NoError(t, err)

	// " should NOT be overwritten — still "shout"
	assert.Equal(t, "shout", repo.existing[`"`])
	// desc should be seeded
	assert.Equal(t, "describe", repo.existing["desc"])
}
```

- [ ] **Step 13: Run test to verify it passes**

Run: `task test -- -run TestSeedManifestAliasesSkipsExistingAliases ./internal/plugin/`
Expected: PASS

- [ ] **Step 14: Write failing test — `SeedManifestAliases` continues on DB error**

Add to `internal/plugin/alias_seeder_test.go`:

```go
func TestSeedManifestAliasesContinuesOnSetError(t *testing.T) {
	repo := &fakeAliasSeeder{
		existing: map[string]string{},
		setErr:   errors.New("db error"),
	}
	cache := command.NewAliasCache()

	aliases := []ManifestAlias{
		{Alias: `"`, Command: "say", Plugin: "core-communication"},
	}

	err := SeedManifestAliases(context.Background(), aliases, repo, cache)
	// Should not return error — DB failure is logged, not fatal
	require.NoError(t, err)
}
```

Add `"errors"` to the import block.

- [ ] **Step 15: Run test to verify it passes**

Run: `task test -- -run TestSeedManifestAliasesContinuesOnSetError ./internal/plugin/`
Expected: PASS

- [ ] **Step 16: Write test — `SeedManifestAliases` sets correct `createdBy`**

Add to `internal/plugin/alias_seeder_test.go`:

```go
func TestSeedManifestAliasesSetsCreatedByToPluginName(t *testing.T) {
	repo := &fakeAliasSeeder{existing: map[string]string{}}
	cache := command.NewAliasCache()

	aliases := []ManifestAlias{
		{Alias: `"`, Command: "say", Plugin: "core-communication"},
	}

	err := SeedManifestAliases(context.Background(), aliases, repo, cache)
	require.NoError(t, err)
	assert.Equal(t, "core-communication", repo.creators[`"`])
}
```

- [ ] **Step 17: Run test to verify it passes**

Run: `task test -- -run TestSeedManifestAliasesSetsCreatedByToPluginName ./internal/plugin/`
Expected: PASS

- [ ] **Step 18: Run all alias_seeder tests**

Run: `task test -- ./internal/plugin/ -run "TestCollectManifest|TestSeedManifest"`
Expected: All PASS.

- [ ] **Step 19: Commit**

```
feat(plugin): add CollectManifestAliases and SeedManifestAliases

Collects aliases from loaded plugin manifests, detects cross-plugin
duplicates, and seeds to DB with insert-if-absent semantics.
```

---

## Task 3: Wire alias seeding into `Manager.LoadAll`

**Files:**

- Modify: `internal/plugin/manager.go:20-30,182-201`
- Modify: `internal/plugin/manager_test.go`

- [ ] **Step 1: Write failing test — `LoadAll` seeds aliases when seeder configured**

Add to `internal/plugin/manager_test.go`:

```go
func TestManagerLoadAllSeedsAliasesFromManifests(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create plugin with aliases
	commDir := filepath.Join(pluginsDir, "test-comm")
	mkdirAll(t, commDir)
	manifest := `name: test-comm
version: 1.0.0
type: lua
commands:
  - name: say
    aliases:
      - '"'
    help: Say something
lua-plugin:
  entry: main.lua`
	writeFile(t, filepath.Join(commDir, "plugin.yaml"), []byte(manifest))
	writeFile(t, filepath.Join(commDir, "main.lua"), []byte("function on_event(e) end"))

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	repo := &fakeAliasSeederMgr{existing: map[string]string{}}
	cache := command.NewAliasCache()

	mgr := plugins.NewManager(pluginsDir,
		plugins.WithLuaHost(luaHost),
		plugins.WithAliasSeeder(repo, cache),
	)
	err := mgr.LoadAll(context.Background())
	require.NoError(t, err)

	// Verify alias was seeded
	assert.Equal(t, "say", repo.existing[`"`])

	// Verify cache was loaded
	all := cache.ListSystemAliases()
	assert.Equal(t, "say", all[`"`])
}
```

Also add the test helper to `manager_test.go`:

```go
// fakeAliasSeederMgr is a minimal in-memory AliasSeeder for manager tests.
type fakeAliasSeederMgr struct {
	existing map[string]string
}

func (f *fakeAliasSeederMgr) GetSystemAliases(_ context.Context) (map[string]string, error) {
	result := make(map[string]string, len(f.existing))
	for k, v := range f.existing {
		result[k] = v
	}
	return result, nil
}

func (f *fakeAliasSeederMgr) SetSystemAlias(_ context.Context, alias, cmd, _ string) error {
	if f.existing == nil {
		f.existing = make(map[string]string)
	}
	f.existing[alias] = cmd
	return nil
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestManagerLoadAllSeedsAliasesFromManifests ./internal/plugin/`
Expected: FAIL — `WithAliasSeeder` option does not exist.

- [ ] **Step 3: Add `WithAliasSeeder` option and seeding call to `Manager`**

In `internal/plugin/manager.go`, add fields to `Manager`:

```go
type Manager struct {
	pluginsDir      string
	luaHost         Host
	hosts           map[Type]Host
	pluginHosts     map[string]Host
	policyInstaller PluginPolicyInstaller
	registry        *ServiceRegistry
	aliasSeeder     AliasSeeder
	aliasCache      *command.AliasCache
	loaded          map[string]*DiscoveredPlugin
	mu              sync.RWMutex
}
```

Add the option function:

```go
// WithAliasSeeder configures alias seeding from plugin manifests during LoadAll.
func WithAliasSeeder(seeder AliasSeeder, cache *command.AliasCache) ManagerOption {
	return func(m *Manager) {
		m.aliasSeeder = seeder
		m.aliasCache = cache
	}
}
```

Update `LoadAll` to seed aliases after all plugins load:

```go
func (m *Manager) LoadAll(ctx context.Context) error {
	discovered, err := m.Discover(ctx)
	if err != nil {
		return err
	}

	ordered := m.resolveLoadOrder(discovered)

	for _, dp := range ordered {
		if err := m.loadPlugin(ctx, dp); err != nil {
			slog.Error("failed to load plugin",
				"plugin", dp.Manifest.Name,
				"priority", dp.Manifest.EffectivePriority(),
				"error", err)
			continue
		}
	}

	// Seed aliases from loaded plugin manifests.
	if m.aliasSeeder != nil && m.aliasCache != nil {
		if err := m.seedAliases(ctx); err != nil {
			slog.Error("failed to seed plugin aliases", "error", err)
		}
	}

	return nil
}
```

Add the `seedAliases` helper method:

```go
// seedAliases collects alias declarations from all loaded plugin manifests
// and seeds them into the database.
func (m *Manager) seedAliases(ctx context.Context) error {
	m.mu.RLock()
	loaded := make([]*DiscoveredPlugin, 0, len(m.loaded))
	for _, dp := range m.loaded {
		loaded = append(loaded, dp)
	}
	m.mu.RUnlock()

	aliases, err := CollectManifestAliases(loaded)
	if err != nil {
		return err
	}
	if len(aliases) == 0 {
		return nil
	}

	return SeedManifestAliases(ctx, aliases, m.aliasSeeder, m.aliasCache)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestManagerLoadAllSeedsAliasesFromManifests ./internal/plugin/`
Expected: PASS

- [ ] **Step 5: Write test — `LoadAll` without seeder configured still works**

Add to `internal/plugin/manager_test.go`:

```go
func TestManagerLoadAllWithoutAliasSeederSkipsSeeding(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	commDir := filepath.Join(pluginsDir, "test-comm")
	mkdirAll(t, commDir)
	manifest := `name: test-comm
version: 1.0.0
type: lua
commands:
  - name: say
    aliases:
      - '"'
    help: Say something
lua-plugin:
  entry: main.lua`
	writeFile(t, filepath.Join(commDir, "plugin.yaml"), []byte(manifest))
	writeFile(t, filepath.Join(commDir, "main.lua"), []byte("function on_event(e) end"))

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	// No WithAliasSeeder — should not panic or error
	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost))
	err := mgr.LoadAll(context.Background())
	require.NoError(t, err)

	loaded := mgr.ListPlugins()
	assert.Len(t, loaded, 1)
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `task test -- -run TestManagerLoadAllWithoutAliasSeederSkipsSeeding ./internal/plugin/`
Expected: PASS

- [ ] **Step 7: Run all manager tests**

Run: `task test -- ./internal/plugin/ -run "TestManager"`
Expected: All PASS.

- [ ] **Step 8: Commit**

```
feat(plugin): wire alias seeding into Manager.LoadAll

Manager accepts WithAliasSeeder option. After loading all plugins,
collects aliases from manifests and seeds to DB via AliasSeeder.
```

---

## Task 4: Add aliases to plugin manifests

**Files:**

- Modify: `plugins/core-communication/plugin.yaml`
- Modify: `plugins/core-objects/plugin.yaml`

- [ ] **Step 1: Add aliases to `core-communication/plugin.yaml`**

Add `aliases` to the `say`, `pose`, `page`, and `whisper` commands:

```yaml
commands:
  - name: say
    aliases:
      - '"'
    # ... rest unchanged ...

  - name: pose
    aliases:
      - ":"
      - ";"
    # ... rest unchanged ...

  - name: page
    aliases:
      - "p"
    # ... rest unchanged ...

  - name: whisper
    aliases:
      - "w"
    # ... rest unchanged ...
```

- [ ] **Step 2: Add alias to `core-objects/plugin.yaml`**

Add `aliases` to the `describe` command:

```yaml
commands:
  - name: describe
    aliases:
      - "desc"
    # ... rest unchanged ...
```

- [ ] **Step 3: Verify manifests parse correctly**

Run: `task test -- -run TestParseManifest ./internal/plugin/`
Expected: All PASS (ensures YAML is valid).

- [ ] **Step 4: Commit**

```
feat(plugin): declare aliases in core-communication and core-objects manifests

Moves ", :, ;, p, w aliases to core-communication and desc to
core-objects. Source of truth shifts from bootstrap to plugin manifests.
```

---

## Task 5: Remove bootstrap alias files and wiring

**Files:**

- Delete: `internal/bootstrap/aliases.go`
- Delete: `internal/bootstrap/aliases_test.go`
- Delete: `internal/bootstrap/alias_bootstrap.go`
- Delete: `internal/bootstrap/alias_bootstrap_test.go`
- Modify: `internal/bootstrap/setup/subsystem.go:202-205`

- [ ] **Step 1: Remove `AliasBootstrapper` registration from `subsystem.go`**

In `internal/bootstrap/setup/subsystem.go`, remove lines 202-205:

```go
// Remove these lines:
// 5. Register alias bootstrapper (priority 500).
s.aliasRepo = store.NewPostgresAliasRepository(pool)
s.aliasCache = command.NewAliasCache()
runner.Register(bootstrap.NewAliasBootstrapper(s.aliasRepo, s.aliasCache))
```

Note: `s.aliasRepo` and `s.aliasCache` are still needed — they will be initialized here but wired to the plugin manager instead. Check if the subsystem passes them to the Manager via `WithAliasSeeder`. If the subsystem already creates the Manager, add the option there. If not, keep the field assignments and wire them in the appropriate place.

Specifically, replace those lines with:

```go
// 5. Initialize alias repository and cache for plugin-driven alias seeding.
s.aliasRepo = store.NewPostgresAliasRepository(pool)
s.aliasCache = command.NewAliasCache()
```

Then ensure the Manager is created with `WithAliasSeeder(s.aliasRepo, s.aliasCache)` wherever `NewManager` is called in the subsystem setup. This wiring depends on the subsystem's structure — check `subsystem.go` for where `NewManager` is invoked and add the option.

- [ ] **Step 2: Delete `internal/bootstrap/aliases.go`**

Run: `rm internal/bootstrap/aliases.go`

- [ ] **Step 3: Delete `internal/bootstrap/aliases_test.go`**

Run: `rm internal/bootstrap/aliases_test.go`

- [ ] **Step 4: Delete `internal/bootstrap/alias_bootstrap.go`**

Run: `rm internal/bootstrap/alias_bootstrap.go`

- [ ] **Step 5: Delete `internal/bootstrap/alias_bootstrap_test.go`**

Run: `rm internal/bootstrap/alias_bootstrap_test.go`

- [ ] **Step 6: Check for remaining references to deleted symbols**

Run: `rg "AliasBootstrapper|SeedSystemAliases|standardAliases|alias_bootstrap" --type go internal/`

Expected: No matches in Go source files. Documentation references are OK.

- [ ] **Step 7: Verify compilation**

Run: `task build`
Expected: Builds successfully with no errors.

- [ ] **Step 8: Run all unit tests**

Run: `task test`
Expected: All PASS — no test references deleted code.

- [ ] **Step 9: Commit**

```
refactor(bootstrap): remove hardcoded alias seeding

AliasBootstrapper and SeedSystemAliases are replaced by plugin
manifest-driven alias seeding in the plugin Manager.LoadAll path.
```

---

## Task 6: Move `AliasSeeder` interface to plugin package

**Files:**

- Modify: `internal/plugin/alias_seeder.go`
- Modify: `internal/bootstrap/setup/subsystem.go` (if needed)

- [ ] **Step 1: Verify `AliasSeeder` interface is already in `internal/plugin/alias_seeder.go`**

The `AliasSeeder` interface was defined in Task 2's `alias_seeder.go`. Confirm it compiles and that `store.PostgresAliasRepository` satisfies it.

Run: `task build`
Expected: PASS — `store.AliasRepository` is a superset of `plugins.AliasSeeder`, so `PostgresAliasRepository` satisfies it implicitly.

- [ ] **Step 2: Verify the old `bootstrap.AliasSeeder` interface is no longer referenced**

Run: `rg "bootstrap\.AliasSeeder" --type go internal/`
Expected: No matches (the type was in the deleted `aliases.go`).

- [ ] **Step 3: Run full test suite**

Run: `task test`
Expected: All PASS.

- [ ] **Step 4: Commit (if any changes were needed)**

Only commit if adjustments were required. Otherwise skip.

---

## Task 7: E2E integration tests — alias seeding with Postgres

**Files:**

- Create: `internal/plugin/alias_seeder_integration_test.go`

- [ ] **Step 1: Write integration test — aliases seeded to database on startup**

Create `internal/plugin/alias_seeder_integration_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugins_test

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/command"
	plugins "github.com/holomush/holomush/internal/plugin"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/test/testutil"
)

var _ = Describe("Plugin Alias Seeding Integration", func() {
	var (
		pgEnv   *testutil.PostgresEnv
		repo    *store.PostgresAliasRepository
		cache   *command.AliasCache
		cleanup func()
	)

	BeforeEach(func() {
		ctx := context.Background()
		var err error
		pgEnv, err = testutil.StartPostgres(ctx)
		Expect(err).NotTo(HaveOccurred())

		migrator, err := store.NewMigrator(pgEnv.ConnStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(migrator.Up()).To(Succeed())
		_ = migrator.Close()

		pool, err := store.NewPool(ctx, pgEnv.ConnStr)
		Expect(err).NotTo(HaveOccurred())

		repo = store.NewPostgresAliasRepository(pool)
		cache = command.NewAliasCache()

		cleanup = func() {
			pool.Close()
			_ = pgEnv.Terminate(ctx)
		}
	})

	AfterEach(func() {
		cleanup()
	})

	Describe("Full startup with manifest aliases", func() {
		It("seeds all declared aliases to the database", func() {
			ctx := context.Background()

			pluginsDir, err := findPluginsDir()
			Expect(err).NotTo(HaveOccurred())

			luaHost := pluginlua.NewHost()
			defer func() { _ = luaHost.Close(ctx) }()

			mgr := plugins.NewManager(pluginsDir,
				plugins.WithLuaHost(luaHost),
				plugins.WithAliasSeeder(repo, cache),
			)
			Expect(mgr.LoadAll(ctx)).To(Succeed())

			// Verify DB has the aliases
			aliases, err := repo.GetSystemAliases(ctx)
			Expect(err).NotTo(HaveOccurred())

			Expect(aliases).To(HaveKeyWithValue(`"`, "say"))
			Expect(aliases).To(HaveKeyWithValue(":", "pose"))
			Expect(aliases).To(HaveKeyWithValue(";", "pose"))
			Expect(aliases).To(HaveKeyWithValue("p", "page"))
			Expect(aliases).To(HaveKeyWithValue("w", "whisper"))
			Expect(aliases).To(HaveKeyWithValue("desc", "describe"))

			// Verify cache was loaded
			cached := cache.ListSystemAliases()
			Expect(cached).To(HaveKeyWithValue(`"`, "say"))
			Expect(cached).To(HaveKeyWithValue("desc", "describe"))
		})
	})

	Describe("Operator override via sysunsalias", func() {
		It("allows operator to change an alias command and keeps it on restart", func() {
			ctx := context.Background()

			pluginsDir, err := findPluginsDir()
			Expect(err).NotTo(HaveOccurred())

			// First load — seed aliases
			luaHost1 := pluginlua.NewHost()
			mgr1 := plugins.NewManager(pluginsDir,
				plugins.WithLuaHost(luaHost1),
				plugins.WithAliasSeeder(repo, cache),
			)
			Expect(mgr1.LoadAll(ctx)).To(Succeed())
			_ = luaHost1.Close(ctx)

			// Operator changes " to point to "shout" instead of "say"
			Expect(repo.SetSystemAlias(ctx, `"`, "shout", "operator")).To(Succeed())

			// Second load — simulate restart
			cache2 := command.NewAliasCache()
			luaHost2 := pluginlua.NewHost()
			mgr2 := plugins.NewManager(pluginsDir,
				plugins.WithLuaHost(luaHost2),
				plugins.WithAliasSeeder(repo, cache2),
			)
			Expect(mgr2.LoadAll(ctx)).To(Succeed())
			_ = luaHost2.Close(ctx)

			// " should still point to "shout" — existing alias is not overwritten
			aliases, err := repo.GetSystemAliases(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(aliases).To(HaveKeyWithValue(`"`, "shout"))
		})
	})

	Describe("Idempotent seeding", func() {
		It("does not error when loaded twice against same database", func() {
			ctx := context.Background()

			pluginsDir, err := findPluginsDir()
			Expect(err).NotTo(HaveOccurred())

			// First load
			luaHost1 := pluginlua.NewHost()
			mgr1 := plugins.NewManager(pluginsDir,
				plugins.WithLuaHost(luaHost1),
				plugins.WithAliasSeeder(repo, cache),
			)
			Expect(mgr1.LoadAll(ctx)).To(Succeed())
			_ = luaHost1.Close(ctx)

			// Second load — same DB, should be idempotent
			cache2 := command.NewAliasCache()
			luaHost2 := pluginlua.NewHost()
			mgr2 := plugins.NewManager(pluginsDir,
				plugins.WithLuaHost(luaHost2),
				plugins.WithAliasSeeder(repo, cache2),
			)
			Expect(mgr2.LoadAll(ctx)).To(Succeed())
			_ = luaHost2.Close(ctx)

			aliases, err := repo.GetSystemAliases(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(aliases).To(HaveKeyWithValue(`"`, "say"))
			Expect(aliases).To(HaveKeyWithValue("desc", "describe"))
		})
	})
})
```

- [ ] **Step 2: Check that `store.NewPool` exists or identify the correct pool constructor**

Run: `rg "func NewPool" --type go internal/store/`

If `NewPool` does not exist, use `pgx.Connect` or whatever the project uses. Adjust the test setup accordingly. The `setupPostgresContainer` pattern in `internal/store/postgres_integration_test.go` shows the established approach — follow that pattern.

- [ ] **Step 3: Run integration tests**

Run: `task test:int -- ./internal/plugin/ -run "Plugin Alias Seeding"`
Expected: All PASS.

- [ ] **Step 4: Commit**

```
test(plugin): add E2E integration tests for manifest alias seeding

Verifies aliases are seeded to Postgres on startup, that operator
overrides survive restarts (insert-if-absent semantics), and that
the cache is populated correctly.
```

---

## Task 8: Final verification

- [ ] **Step 1: Run full unit test suite**

Run: `task test`
Expected: All PASS.

- [ ] **Step 2: Run full integration test suite**

Run: `task test:int`
Expected: All PASS.

- [ ] **Step 3: Run linter**

Run: `task lint`
Expected: No errors.

- [ ] **Step 4: Run formatter**

Run: `task fmt`
Expected: No changes needed (or apply formatting).

- [ ] **Step 5: Run `task pr-prep`**

Run: `task pr-prep`
Expected: All CI-equivalent checks PASS.
