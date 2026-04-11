# Extensible Plugin Actions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow plugins to declare custom ABAC action strings (e.g., `join`, `leave`) in their manifests, unblocking the core-channels plugin and any future plugin with domain-specific ABAC actions.

**Architecture:** Three-layer change: (1) relax `Capability.Validate()` to defer action validation to load time, (2) add `actions` field to manifests with `CollectActions` merge pass in the plugin manager, (3) call `ValidateAction` during `loadPlugin` and warn when a plugin borrows an action it doesn't own.

**Tech Stack:** Go 1.26, `internal/command`, `internal/plugin`, `github.com/samber/oops`, `log/slog`, Ginkgo/Gomega for integration tests.

---

## File Map

| File | Change |
|---|---|
| `internal/command/types.go` | Remove `!validActions` check from `Validate()`; add `CoreActions()`, `ValidateAction()` |
| `internal/command/types_test.go` | Update "unknown action" test cases; add `CoreActions` and `ValidateAction` tests |
| `internal/plugin/manifest.go` | Add `Actions []string` field; validate in `Manifest.Validate()` |
| `internal/plugin/manifest_test.go` | Add table-driven cases to `TestParseManifestResourceTypesAndTrust` for `actions` |
| `internal/plugin/manager.go` | Add `CollectActions()`; update `loadPlugin` signature and body; update `LoadAll` Phase 2 |
| `internal/plugin/manager_test.go` | Add `CollectActions` unit tests; add `LoadAll` action validation integration tests |
| `test/integration/plugin/extensible_actions_test.go` | New Ginkgo integration test: custom actions end-to-end through LoadAll |

---

## Task 1: Relax `Capability.Validate()` to accept unknown action strings

The test `TestCapability_ValidateInvalid` has an `"unknown action"` case that must be removed. `TestNewCommandEntry_InvalidCapabilityReturnsError` has the same. A new positive test asserts that unknown actions pass `Validate()`. Only then do we remove the `!validActions` check.

**Files:**
- Modify: `internal/command/types_test.go`
- Modify: `internal/command/types.go:150-157`

- [ ] **Step 1: Update `TestCapability_ValidateInvalid` — remove the "unknown action" case**

  Open `internal/command/types_test.go` at line 590. The table currently contains:

  ```go
  {"unknown action", Capability{Action: "destroy", Resource: "location"}, "action"},
  ```

  Remove that row. The table should become:

  ```go
  func TestCapability_ValidateInvalid(t *testing.T) {
  	tests := []struct {
  		name string
  		cap  Capability
  		want string
  	}{
  		{"empty action", Capability{Action: "", Resource: "location"}, "action"},
  		{"empty resource", Capability{Action: "read", Resource: ""}, "resource"},
  		// Note: unknown resource type is NOT checked by Validate() — it's checked
  		// by ValidateResourceType() at load time with cross-plugin context.
  		{"invalid scope", Capability{Action: "read", Resource: "location", Scope: "everywhere"}, "scope"},
  	}
  	for _, tt := range tests {
  		t.Run(tt.name, func(t *testing.T) {
  			err := tt.cap.Validate()
  			require.Error(t, err)
  			assert.Contains(t, err.Error(), tt.want)
  		})
  	}
  }
  ```

- [ ] **Step 2: Update `TestNewCommandEntry_InvalidCapabilityReturnsError` — remove "unknown action" case**

  Find `TestNewCommandEntry_InvalidCapabilityReturnsError` (around line 648). Remove the case:

  ```go
  {
  	name: "unknown action",
  	caps: []Capability{{Action: "destroy", Resource: "location"}},
  	want: "action",
  },
  ```

- [ ] **Step 3: Add a positive test confirming unknown actions pass `Validate()`**

  Add directly after `TestCapability_ValidateAcceptsUnknownResourceType` (around line 617):

  ```go
  func TestCapability_ValidateAcceptsUnknownAction(t *testing.T) {
  	// Validate() is structural only — unknown actions pass here.
  	// ValidateAction() checks membership at load time with cross-plugin context.
  	c := Capability{Action: "destroy", Resource: "location"}
  	assert.NoError(t, c.Validate())
  }
  ```

- [ ] **Step 4: Run the tests — they should fail because `Validate()` still rejects "destroy"**

  ```bash
  task test -- -run TestCapability_ValidateAcceptsUnknownAction ./internal/command/
  ```

  Expected: FAIL — `"unknown action \"destroy\""` error.

- [ ] **Step 5: Remove the `!validActions` check from `Capability.Validate()`**

  In `internal/command/types.go`, `Capability.Validate()` (around line 150), delete lines:

  ```go
  if !validActions[c.Action] {
  	return oops.Code("INVALID_CAPABILITY").
  		With("action", c.Action).
  		Errorf("unknown action %q", c.Action)
  }
  ```

  The method should now read:

  ```go
  func (c Capability) Validate() error {
  	if c.Action == "" {
  		return oops.Code("INVALID_CAPABILITY").Errorf("action is required")
  	}
  	if c.Resource == "" {
  		return oops.Code("INVALID_CAPABILITY").Errorf("resource is required")
  	}
  	if !validScopes[c.Scope] {
  		return oops.Code("INVALID_CAPABILITY").
  			With("scope", c.Scope).
  			Errorf("unknown scope %q", c.Scope)
  	}
  	return nil
  }
  ```

- [ ] **Step 6: Run all command package tests**

  ```bash
  task test -- ./internal/command/
  ```

  Expected: PASS.

---

## Task 2: Add `CoreActions()` and `ValidateAction()`

**Files:**
- Modify: `internal/command/types_test.go`
- Modify: `internal/command/types.go`

- [ ] **Step 1: Write failing tests for `CoreActions()` and `ValidateAction()`**

  Add after `TestCoreResourceTypesReturnsCopy` (around line 632) in `internal/command/types_test.go`:

  ```go
  func TestCoreActionsContainsExpectedDefaults(t *testing.T) {
  	actions := CoreActions()
  	for _, expected := range []string{"read", "write", "emit", "enter", "use", "delete", "execute", "admin"} {
  		assert.True(t, actions[expected], "core action %q must be present", expected)
  	}
  }

  func TestCoreActionsReturnsCopy(t *testing.T) {
  	actions := CoreActions()
  	assert.True(t, actions["read"])
  	// Mutating the copy must not affect a second call.
  	actions["invent"] = true
  	actions2 := CoreActions()
  	assert.False(t, actions2["invent"], "subsequent calls must not see prior mutations")
  }

  func TestCapability_ValidateActionWithKnownAction(t *testing.T) {
  	known := map[string]bool{"join": true, "read": true}
  	assert.NoError(t, Capability{Action: "join", Resource: "channel"}.ValidateAction(known))
  	assert.NoError(t, Capability{Action: "read", Resource: "location"}.ValidateAction(known))
  }

  func TestCapability_ValidateActionWithUnknownAction(t *testing.T) {
  	known := map[string]bool{"read": true}
  	err := Capability{Action: "join", Resource: "channel"}.ValidateAction(known)
  	require.Error(t, err)
  	assert.Contains(t, err.Error(), "join")
  	errutil.AssertErrorCode(t, err, "INVALID_CAPABILITY")
  }

  func TestCapability_ValidateActionBoundaryEmptyKnownMap(t *testing.T) {
  	err := Capability{Action: "read", Resource: "location"}.ValidateAction(map[string]bool{})
  	require.Error(t, err, "empty known map must reject any action")
  }
  ```

- [ ] **Step 2: Run to confirm they fail**

  ```bash
  task test -- -run "TestCoreActions|TestCapability_ValidateAction" ./internal/command/
  ```

  Expected: FAIL — `CoreActions` and `ValidateAction` undefined.

- [ ] **Step 3: Add `CoreActions()` and `ValidateAction()` to `internal/command/types.go`**

  Add after `CoreResourceTypes()` (around line 190):

  ```go
  // CoreActions returns a defensive copy of the built-in action set.
  // Used by the plugin manager to build the full known-actions map.
  func CoreActions() map[string]bool {
  	result := make(map[string]bool, len(validActions))
  	for k, v := range validActions {
  		result[k] = v
  	}
  	return result
  }

  // ValidateAction checks that the capability's action is in the provided set.
  // Called during plugin load with a set that includes both core actions and
  // plugin-declared actions.
  func (c Capability) ValidateAction(known map[string]bool) error {
  	if !known[c.Action] {
  		return oops.Code("INVALID_CAPABILITY").
  			With("action", c.Action).
  			Errorf("unknown action %q", c.Action)
  	}
  	return nil
  }
  ```

- [ ] **Step 4: Run tests**

  ```bash
  task test -- ./internal/command/
  ```

  Expected: PASS.

- [ ] **Step 5: Commit**

  ```bash
  jj --no-pager commit -m "feat(command): relax Capability.Validate(), add CoreActions/ValidateAction"
  ```

---

## Task 3: Add `actions` field to Manifest

**Files:**
- Modify: `internal/plugin/manifest.go`
- Modify: `internal/plugin/manifest_test.go`

- [ ] **Step 1: Write failing tests — add cases to `TestParseManifestResourceTypesAndTrust`**

  In `internal/plugin/manifest_test.go`, find `TestParseManifestResourceTypesAndTrust` (around line 1766). Add four new cases to the `tests` slice before the closing `}`:

  ```go
  {
  	name: "parses actions when declared on a binary plugin",
  	yaml: `
  name: test-plugin
  version: 1.0.0
  type: binary
  binary-plugin:
    executable: test-plugin
  actions: [join, leave]
  `,
  	check: func(t *testing.T, m *plugins.Manifest) {
  		assert.Equal(t, []string{"join", "leave"}, m.Actions)
  	},
  },
  {
  	name: "parses actions when declared on a Lua plugin",
  	yaml: `
  name: test-plugin
  version: 1.0.0
  type: lua
  lua-plugin:
    entry: main.lua
  actions: [craft]
  `,
  	check: func(t *testing.T, m *plugins.Manifest) {
  		assert.Equal(t, []string{"craft"}, m.Actions)
  	},
  },
  {
  	name:    "rejects actions with an empty entry",
  	wantErr: "action",
  	yaml: `
  name: test-plugin
  version: 1.0.0
  type: lua
  lua-plugin:
    entry: main.lua
  actions: [""]
  `,
  },
  {
  	name:    "rejects actions with duplicate entries",
  	wantErr: "duplicate",
  	yaml: `
  name: test-plugin
  version: 1.0.0
  type: binary
  binary-plugin:
    executable: test-plugin
  actions: [join, join]
  `,
  },
  {
  	name: "accepts actions that re-declare a core action",
  	yaml: `
  name: test-plugin
  version: 1.0.0
  type: binary
  binary-plugin:
    executable: test-plugin
  actions: [read]
  `,
  	check: func(t *testing.T, m *plugins.Manifest) {
  		assert.Equal(t, []string{"read"}, m.Actions)
  	},
  },
  ```

- [ ] **Step 2: Run to confirm tests fail**

  ```bash
  task test -- -run TestParseManifestResourceTypesAndTrust ./internal/plugin/
  ```

  Expected: FAIL — `actions` field unknown / not parsed.

- [ ] **Step 3: Add `Actions` field to `Manifest` struct**

  In `internal/plugin/manifest.go`, find the `Manifest` struct (around line 96). Add `Actions` after `ResourceTypes`:

  ```go
  // ABAC trust boundary fields
  ResourceTypes []string     `yaml:"resource_types,omitempty" json:"resource_types,omitempty"`
  Actions       []string     `yaml:"actions,omitempty" json:"actions,omitempty"`
  Trust         *TrustConfig `yaml:"trust,omitempty" json:"trust,omitempty"`
  ```

- [ ] **Step 4: Add `actions` validation to `Manifest.Validate()`**

  In `internal/plugin/manifest.go`, find the `resource_types` validation block (around line 393). Add an `actions` validation block immediately after it (before `return nil`):

  ```go
  // Validate actions: no empty strings, no duplicates.
  // All plugin types may declare actions (unlike resource_types, actions
  // have no structural coupling to AttributeResolverService).
  if len(m.Actions) > 0 {
  	seen := make(map[string]bool, len(m.Actions))
  	for _, a := range m.Actions {
  		if a == "" {
  			return oops.In("manifest").With("name", m.Name).
  				New("action entry must not be empty")
  		}
  		if seen[a] {
  			return oops.In("manifest").With("name", m.Name).With("action", a).
  				New("duplicate action")
  		}
  		seen[a] = true
  	}
  }
  ```

- [ ] **Step 5: Run tests**

  ```bash
  task test -- ./internal/plugin/
  ```

  Expected: PASS.

- [ ] **Step 6: Commit**

  ```bash
  jj --no-pager commit -m "feat(plugin): add actions field to Manifest with validation"
  ```

---

## Task 4: Add `CollectActions()` to the plugin manager

**Files:**
- Modify: `internal/plugin/manager.go`
- Modify: `internal/plugin/manager_test.go`

- [ ] **Step 1: Write failing tests for `CollectActions()`**

  In `internal/plugin/manager_test.go`, add after `TestCollectResourceTypesReturnsNewMapPerCall` (around line 883):

  ```go
  // CollectActions is the exported test seam that backs the
  // cross-plugin action collection used during LoadAll.

  func TestCollectActionsIncludesCoreActions(t *testing.T) {
  	known := plugins.CollectActions(nil)
  	for _, action := range []string{"read", "write", "emit", "enter", "use", "delete", "execute", "admin"} {
  		assert.True(t, known[action], "core action %q must be included", action)
  	}
  }

  func TestCollectActionsMergesExplicitManifestActions(t *testing.T) {
  	discovered := []*plugins.DiscoveredPlugin{
  		{Manifest: &plugins.Manifest{Name: "p1", Actions: []string{"join"}}},
  		{Manifest: &plugins.Manifest{Name: "p2", Actions: []string{"leave", "vote"}}},
  	}
  	known := plugins.CollectActions(discovered)
  	assert.True(t, known["join"], "declared 'join' should be present")
  	assert.True(t, known["leave"], "declared 'leave' should be present")
  	assert.True(t, known["vote"], "declared 'vote' should be present")
  	assert.True(t, known["read"], "core actions should still be present after merge")
  }

  func TestCollectActionsDeduplicatesAcrossPlugins(t *testing.T) {
  	discovered := []*plugins.DiscoveredPlugin{
  		{Manifest: &plugins.Manifest{Name: "p1", Actions: []string{"join"}}},
  		{Manifest: &plugins.Manifest{Name: "p2", Actions: []string{"join"}}},
  	}
  	known := plugins.CollectActions(discovered)
  	assert.True(t, known["join"], "'join' declared by two plugins should be present once")
  }

  func TestCollectActionsReturnsNewMapPerCall(t *testing.T) {
  	first := plugins.CollectActions(nil)
  	first["mutated"] = true
  	second := plugins.CollectActions(nil)
  	assert.False(t, second["mutated"], "subsequent calls must not see prior mutations")
  }

  func TestCollectActionsIgnoresCapabilityActionsNotInActionsField(t *testing.T) {
  	// Only the explicit Actions manifest field feeds CollectActions.
  	// Action strings in command capabilities are NOT auto-promoted.
  	discovered := []*plugins.DiscoveredPlugin{
  		{Manifest: &plugins.Manifest{
  			Name: "p1",
  			Commands: []plugins.CommandSpec{
  				{Name: "channel", Capabilities: []command.Capability{
  					{Action: "join", Resource: "channel"},
  				}},
  			},
  			// No Actions field declared.
  		}},
  	}
  	known := plugins.CollectActions(discovered)
  	assert.False(t, known["join"], "'join' in capabilities but not in actions field must not appear")
  }
  ```

  Note: this test file imports `command "github.com/holomush/holomush/internal/command"` — check the existing imports and add it if missing.

- [ ] **Step 2: Run to confirm they fail**

  ```bash
  task test -- -run "TestCollectActions" ./internal/plugin/
  ```

  Expected: FAIL — `plugins.CollectActions` undefined.

- [ ] **Step 3: Add `CollectActions()` to `internal/plugin/manager.go`**

  Add directly after `CollectResourceTypes()` (around line 453):

  ```go
  // CollectActions builds the full set of known ABAC actions: core actions plus
  // all actions explicitly declared across discovered plugins. This cross-plugin
  // context is needed for semantic validation during loadPlugin. Exported as a
  // test seam so callers can verify the merge logic without driving LoadAll.
  func CollectActions(discovered []*DiscoveredPlugin) map[string]bool {
  	known := command.CoreActions()
  	for _, dp := range discovered {
  		for _, a := range dp.Manifest.Actions {
  			known[a] = true
  		}
  	}
  	return known
  }
  ```

- [ ] **Step 4: Run tests**

  ```bash
  task test -- ./internal/plugin/
  ```

  Expected: PASS.

- [ ] **Step 5: Commit**

  ```bash
  jj --no-pager commit -m "feat(plugin): add CollectActions for cross-plugin action validation"
  ```

---

## Task 5: Wire `CollectActions` and `ValidateAction` into `LoadAll` / `loadPlugin`

This is the integration point. `loadPlugin` gains a `knownActions` parameter. `LoadAll` Phase 2 collects actions alongside resource types. `loadPlugin` validates each capability action and warns when borrowing from another plugin.

**Files:**
- Modify: `internal/plugin/manager.go`
- Modify: `internal/plugin/manager_test.go`

- [ ] **Step 1: Write failing load integration tests**

  Add after `TestManagerLoadAllAcceptsCapabilityOnAnotherPluginsResourceType` (around line 956) in `internal/plugin/manager_test.go`:

  ```go
  func TestManagerLoadAllRejectsCommandCapabilityOnUnknownAction(t *testing.T) {
  	dir := t.TempDir()
  	pluginsDir := filepath.Join(dir, "plugins")

  	pluginDir := filepath.Join(pluginsDir, "channel-plugin")
  	mkdirAll(t, pluginDir)
  	writeFile(t, filepath.Join(pluginDir, "plugin.yaml"), []byte(`name: channel-plugin
  version: 1.0.0
  type: lua
  commands:
    - name: channel
      capabilities:
        - action: join
          resource: channel
  lua-plugin:
    entry: main.lua`))
  	writeFile(t, filepath.Join(pluginDir, "main.lua"), []byte("function on_event(e) end"))

  	luaHost := pluginlua.NewHost()
  	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

  	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost))
  	err := mgr.LoadAll(context.Background())
  	require.Error(t, err, "load should fail when capability uses an undeclared action")
  	assert.Contains(t, err.Error(), "join")
  }

  func TestManagerLoadAllAcceptsCapabilityWithDeclaredAction(t *testing.T) {
  	dir := t.TempDir()
  	pluginsDir := filepath.Join(dir, "plugins")

  	pluginDir := filepath.Join(pluginsDir, "channel-plugin")
  	mkdirAll(t, pluginDir)
  	writeFile(t, filepath.Join(pluginDir, "plugin.yaml"), []byte(`name: channel-plugin
  version: 1.0.0
  type: lua
  actions: [join, leave]
  commands:
    - name: channel
      capabilities:
        - action: join
          resource: channel
        - action: leave
          resource: channel
  lua-plugin:
    entry: main.lua`))
  	writeFile(t, filepath.Join(pluginDir, "main.lua"), []byte("function on_event(e) end"))

  	luaHost := pluginlua.NewHost()
  	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

  	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost))
  	err := mgr.LoadAll(context.Background())
  	require.NoError(t, err, "load should succeed when action is declared in the plugin manifest")
  	assert.Contains(t, mgr.ListPlugins(), "channel-plugin")
  }

  func TestManagerLoadAllAcceptsCapabilityOnAnotherPluginsAction(t *testing.T) {
  	dir := t.TempDir()
  	pluginsDir := filepath.Join(dir, "plugins")

  	// Plugin A declares the "join" action so Plugin B's capability is valid.
  	declarerDir := filepath.Join(pluginsDir, "action-declarer")
  	mkdirAll(t, declarerDir)
  	writeFile(t, filepath.Join(declarerDir, "plugin.yaml"), []byte(`name: action-declarer
  version: 1.0.0
  type: binary
  actions: [join]
  binary-plugin:
    executable: action-declarer`))

  	// Plugin B uses "join" declared by Plugin A.
  	consumerDir := filepath.Join(pluginsDir, "action-consumer")
  	mkdirAll(t, consumerDir)
  	writeFile(t, filepath.Join(consumerDir, "plugin.yaml"), []byte(`name: action-consumer
  version: 1.0.0
  type: lua
  commands:
    - name: channel
      capabilities:
        - action: join
          resource: channel
  lua-plugin:
    entry: main.lua`))
  	writeFile(t, filepath.Join(consumerDir, "main.lua"), []byte("function on_event(e) end"))

  	luaHost := pluginlua.NewHost()
  	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

  	// No binary host — declarer is silently skipped, but its actions still
  	// feed CollectActions during Phase 2.
  	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost))
  	err := mgr.LoadAll(context.Background())
  	require.NoError(t, err, "consumer should validate against declarer's action")
  	assert.Contains(t, mgr.ListPlugins(), "action-consumer")
  }
  ```

- [ ] **Step 2: Run to confirm they fail**

  ```bash
  task test -- -run "TestManagerLoadAllRejectsCommandCapabilityOnUnknownAction|TestManagerLoadAllAcceptsCapabilityWithDeclaredAction|TestManagerLoadAllAcceptsCapabilityOnAnotherPluginsAction" ./internal/plugin/
  ```

  Expected: FAIL — `loadPlugin` does not yet validate actions, so the rejection test fails (no error) and the acceptance tests may or may not pass.

- [ ] **Step 3: Update `loadPlugin` signature in `internal/plugin/manager.go`**

  Find `func (m *Manager) loadPlugin` (around line 520). Update signature from:

  ```go
  func (m *Manager) loadPlugin(ctx context.Context, dp *DiscoveredPlugin, knownResourceTypes map[string]bool) error {
  ```

  to:

  ```go
  func (m *Manager) loadPlugin(ctx context.Context, dp *DiscoveredPlugin, knownResourceTypes map[string]bool, knownActions map[string]bool) error {
  ```

- [ ] **Step 4: Add action validation loop to `loadPlugin`**

  In `loadPlugin`, find the existing resource type validation block (around line 522):

  ```go
  for i := range dp.Manifest.Commands {
  	cmd := &dp.Manifest.Commands[i]
  	for _, cap := range cmd.Capabilities {
  		if err := cap.ValidateResourceType(knownResourceTypes); err != nil {
  			return oops.In("manager").With("plugin", dp.Manifest.Name).
  				With("command", cmd.Name).Wrap(err)
  		}
  	}
  }
  ```

  Replace with (adds action validation and cross-plugin borrow warning):

  ```go
  coreActions := command.CoreActions()
  ownActions := make(map[string]bool, len(dp.Manifest.Actions))
  for _, a := range dp.Manifest.Actions {
  	ownActions[a] = true
  }
  for i := range dp.Manifest.Commands {
  	cmd := &dp.Manifest.Commands[i]
  	for _, cap := range cmd.Capabilities {
  		if err := cap.ValidateResourceType(knownResourceTypes); err != nil {
  			return oops.In("manager").With("plugin", dp.Manifest.Name).
  				With("command", cmd.Name).Wrap(err)
  		}
  		if err := cap.ValidateAction(knownActions); err != nil {
  			return oops.In("manager").With("plugin", dp.Manifest.Name).
  				With("command", cmd.Name).Wrap(err)
  		}
  		if !coreActions[cap.Action] && !ownActions[cap.Action] {
  			slog.Warn("capability uses action not declared by this plugin",
  				"plugin", dp.Manifest.Name,
  				"command", cmd.Name,
  				"action", cap.Action)
  		}
  	}
  }
  ```

- [ ] **Step 5: Update `LoadAll` Phase 2 to collect actions**

  Find Phase 2 in `LoadAll` (around line 344):

  ```go
  // Phase 2: Collect cross-plugin context.
  knownResourceTypes := CollectResourceTypes(discovered)
  ```

  Add `CollectActions` call:

  ```go
  // Phase 2: Collect cross-plugin context.
  knownResourceTypes := CollectResourceTypes(discovered)
  knownActions := CollectActions(discovered)
  ```

- [ ] **Step 6: Update `loadPlugin` call site in `LoadAll`**

  Find the `loadPlugin` call (around line 353):

  ```go
  if err := m.loadPlugin(ctx, dp, knownResourceTypes); err != nil {
  ```

  Update to:

  ```go
  if err := m.loadPlugin(ctx, dp, knownResourceTypes, knownActions); err != nil {
  ```

- [ ] **Step 7: Run all plugin tests**

  ```bash
  task test -- ./internal/plugin/
  ```

  Expected: PASS.

- [ ] **Step 8: Run full unit test suite**

  ```bash
  task test
  ```

  Expected: PASS.

- [ ] **Step 9: Commit**

  ```bash
  jj --no-pager commit -m "feat(plugin): wire CollectActions and ValidateAction into LoadAll/loadPlugin"
  ```

---

## Task 6: Integration test — custom actions end-to-end

A new Ginkgo test file in `test/integration/plugin/` exercises the full LoadAll pipeline with real plugin manifests on disk.

**Files:**
- Create: `test/integration/plugin/extensible_actions_test.go`

- [ ] **Step 1: Create `test/integration/plugin/extensible_actions_test.go`**

  ```go
  // SPDX-License-Identifier: Apache-2.0
  // Copyright 2026 HoloMUSH Contributors

  //go:build integration

  package plugin_test

  import (
  	"context"
  	"os"
  	"path/filepath"

  	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
  	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

  	plugins "github.com/holomush/holomush/internal/plugin"
  	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
  )

  var _ = Describe("Plugin loading with custom actions", func() {
  	var (
  		pluginsDir string
  		luaHost    *pluginlua.Host //nolint:typecheck // pluginlua.Host is *lua.Host
  	)

  	BeforeEach(func() {
  		pluginsDir = GinkgoT().TempDir()
  		luaHost = pluginlua.NewHost()
  		DeferCleanup(func() { _ = luaHost.Close(context.Background()) })
  	})

  	writePlugin := func(name, yamlContent, luaContent string) {
  		dir := filepath.Join(pluginsDir, name)
  		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
  		Expect(os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(yamlContent), 0o644)).To(Succeed())
  		if luaContent != "" {
  			Expect(os.WriteFile(filepath.Join(dir, "main.lua"), []byte(luaContent), 0o644)).To(Succeed())
  		}
  	}

  	It("loads a plugin that declares non-core actions in the actions field", func() {
  		writePlugin("channels", `
  name: channels
  version: 1.0.0
  type: lua
  actions: [join, leave]
  commands:
    - name: channel
      capabilities:
        - action: join
          resource: location
        - action: leave
          resource: location
  lua-plugin:
    entry: main.lua
  `, "function on_event(e) end")

  		mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost))
  		Expect(mgr.LoadAll(context.Background())).To(Succeed())
  		Expect(mgr.ListPlugins()).To(ContainElement("channels"))
  	})

  	It("rejects a plugin whose capability uses an undeclared action", func() {
  		writePlugin("bad-plugin", `
  name: bad-plugin
  version: 1.0.0
  type: lua
  commands:
    - name: channel
      capabilities:
        - action: join
          resource: location
  lua-plugin:
    entry: main.lua
  `, "function on_event(e) end")

  		mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost))
  		err := mgr.LoadAll(context.Background())
  		Expect(err).To(HaveOccurred())
  		Expect(err.Error()).To(ContainSubstring("join"))
  	})

  	It("loads two plugins where one borrows an action declared by the other", func() {
  		// Plugin A declares "join"; Plugin B uses it without declaring it.
  		writePlugin("action-declarer", `
  name: action-declarer
  version: 1.0.0
  type: binary
  actions: [join]
  binary-plugin:
    executable: action-declarer
  `, "")

  		writePlugin("action-borrower", `
  name: action-borrower
  version: 1.0.0
  type: lua
  commands:
    - name: channel
      capabilities:
        - action: join
          resource: location
  lua-plugin:
    entry: main.lua
  `, "function on_event(e) end")

  		// No binary host — declarer is silently skipped; its declared actions
  		// still feed CollectActions during Phase 2.
  		mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost))
  		Expect(mgr.LoadAll(context.Background())).To(Succeed())
  		Expect(mgr.ListPlugins()).To(ContainElement("action-borrower"))
  	})
  })
  ```

- [ ] **Step 2: Run integration tests**

  ```bash
  task test:int
  ```

  Expected: PASS (all three `It` blocks green).

- [ ] **Step 3: Run lint**

  ```bash
  task lint
  ```

  Expected: PASS with no new warnings.

- [ ] **Step 4: Commit**

  ```bash
  jj --no-pager commit -m "test(integration): extensible plugin actions end-to-end"
  ```

---

## Task 7: Final verification

- [ ] **Step 1: Run full pr-prep**

  ```bash
  task pr-prep
  ```

  Expected: all checks green (lint, format, schema, license, unit, integration, E2E).

- [ ] **Step 2: Update beads issue**

  ```bash
  bd close holomush-275o --reason "Implemented: actions field in manifest, CollectActions, ValidateAction, cross-plugin borrow warning. Channels plugin unblocked."
  ```

---

## Out of Scope

- Updating `plugins/core-channels/plugin.yaml` to add `actions: [join, leave]` — this is in scope for holomush-0sc.12.
- Removing the redundant `scene` entry from `validResourceTypes` — noted in holomush-275o but deferred.
