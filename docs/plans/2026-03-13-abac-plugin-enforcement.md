# ABAC Plugin Enforcement Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the removed `capability.Enforcer` with proper ABAC enforcement for plugins — subject format fix, manifest-defined policies, KV access checks, and plugin attribute resolution.

**Architecture:** Plugins define ABAC policies in their YAML manifests. On load, `Manager` installs policies into `PolicyStore` via a `PluginPolicyInstaller`. The engine evaluates plugin subjects (`"plugin:<name>"`) against these policies using a new `PluginAttributeProvider`. KV operations gain ABAC checks at the hostfunc layer. Default-deny ensures unrecognized plugins are blocked without explicit forbid policies.

**Tech Stack:** Go 1.23, gopher-lua, PostgreSQL (PolicyStore), testify, mockery

**Spec:** `docs/specs/2026-03-13-abac-plugin-enforcement-design.md`

---

## File Structure

### New Files

| File | Responsibility |
| ---- | -------------- |
| `internal/access/policy/attribute/plugin_provider.go` | `PluginAttributeProvider` — resolves `"plugin:<name>"` subjects to `{"name": "<name>"}` |
| `internal/access/policy/attribute/plugin_provider_test.go` | Tests for plugin attribute provider |
| `internal/plugin/policy_installer.go` | `PluginPolicyInstaller` interface + concrete implementation using `PolicyStore` + compiler |
| `internal/plugin/policy_installer_test.go` | Tests for policy installer (install, remove, replace, scope validation) |

### Modified Files

| File | Changes |
| ---- | ------- |
| `internal/access/prefix.go` | Add `ResourceKV`, `PluginSubject()`, `KVResource()` helpers |
| `internal/access/prefix_test.go` | Tests for new helpers; `TestKnownPrefixes_AllConstantsCovered` auto-catches `ResourceKV` |
| `internal/plugin/manifest.go` | Remove `Capabilities`, add `Policies []ManifestPolicy` |
| `internal/plugin/manifest_test.go` | Update parsing tests for new schema |
| `internal/plugin/manager.go` | Add `WithPolicyInstaller` option, call installer in `loadPlugin`/`Close` |
| `internal/plugin/hostfunc/adapter.go` | `SubjectID()` uses `access.PluginSubject()` |
| `internal/plugin/hostfunc/helpers.go` | Subject construction uses `access.PluginSubject()` |
| `internal/plugin/hostfunc/world_write.go` | Subject construction uses `access.PluginSubject()` |
| `internal/plugin/hostfunc/functions.go` | KV functions gain ABAC checks, update package docstring |
| `internal/plugin/hostfunc/functions_test.go` | KV ABAC enforcement tests |
| `internal/plugin/hostfunc/commands.go` | `getCommandHelpFn` gains access check + `character_id` param |
| `internal/plugin/hostfunc/commands_test.go` | Tests for `get_command_help` access check |
| `internal/access/policy/store/store.go` | Add `DeleteBySource` to interface, extend `ValidateSourceNaming` for `plugin:` |
| `internal/access/policy/store/postgres.go` | Implement `DeleteBySource` |
| `plugins/building/plugin.yaml` | Replace `capabilities:` with `policies:` |
| `plugins/communication/plugin.yaml` | Replace `capabilities:` with `policies:` |
| `plugins/echo-bot/plugin.yaml` | Replace `capabilities:` with `policies:` |
| `plugins/help/plugin.yaml` | Replace `capabilities:` with `policies:` |

---

## Chunk 1: Foundation — Subject Format + KV Resource Type

### Task 1: Add `PluginSubject()` helper to prefix.go

**Files:**

- Modify: `internal/access/prefix.go:56-63` (after `CharacterSubject`)
- Modify: `internal/access/prefix_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/access/prefix_test.go`, add:

```go
func TestPluginSubject(t *testing.T) {
	assert.Equal(t, "plugin:echo-bot", access.PluginSubject("echo-bot"))
	assert.Equal(t, "plugin:building", access.PluginSubject("building"))
}

func TestPluginSubject_EmptyName_Panics(t *testing.T) {
	assert.PanicsWithValue(t, "access.PluginSubject: empty name would bypass access control", func() {
		access.PluginSubject("")
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestPluginSubject ./internal/access/...`
Expected: FAIL — `access.PluginSubject` undefined

- [ ] **Step 3: Implement `PluginSubject` in prefix.go**

Add after `CharacterSubject` (around line 63):

```go
// PluginSubject returns a properly formatted plugin subject identifier.
// Panics if name is empty, since an empty subject bypasses access control.
func PluginSubject(name string) string {
	if name == "" {
		panic("access.PluginSubject: empty name would bypass access control")
	}
	return SubjectPlugin + name
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestPluginSubject ./internal/access/...`
Expected: PASS

- [ ] **Step 5: Commit**

```text
feat(access): add PluginSubject() helper with panic guard
```

### Task 2: Add `ResourceKV` constant and `KVResource()` helper

**Files:**

- Modify: `internal/access/prefix.go` (constants block + after last helper)
- Modify: `internal/access/prefix_test.go`

- [ ] **Step 1: Write the failing tests**

In `internal/access/prefix_test.go`, add:

```go
func TestKVResource(t *testing.T) {
	assert.Equal(t, "kv:echo-bot:counter", access.KVResource("echo-bot", "counter"))
	assert.Equal(t, "kv:building:room-count", access.KVResource("building", "room-count"))
}

func TestKVResource_EmptyNamespace_Panics(t *testing.T) {
	assert.PanicsWithValue(t, "access.KVResource: empty namespace would bypass access control", func() {
		access.KVResource("", "key")
	})
}

func TestKVResource_EmptyKey_Panics(t *testing.T) {
	assert.PanicsWithValue(t, "access.KVResource: empty key would bypass access control", func() {
		access.KVResource("ns", "")
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestKVResource ./internal/access/...`
Expected: FAIL — `access.KVResource` undefined

- [ ] **Step 3: Implement**

Add `ResourceKV` to the constants block in `prefix.go`:

```go
ResourceKV = "kv:"
```

Add `ResourceKV` to the `knownPrefixes` slice.

Add the helper function:

```go
// KVResource returns a properly formatted KV resource identifier.
// Format: "kv:<namespace>:<key>". Panics if namespace or key is empty.
func KVResource(namespace, key string) string {
	if namespace == "" {
		panic("access.KVResource: empty namespace would bypass access control")
	}
	if key == "" {
		panic("access.KVResource: empty key would bypass access control")
	}
	return ResourceKV + namespace + ":" + key
}
```

- [ ] **Step 4: Run tests**

Run: `task test -- -run "TestKVResource|TestKnownPrefixes" ./internal/access/...`
Expected: PASS — including `TestKnownPrefixes_AllConstantsCovered` (it auto-detects new constants)

- [ ] **Step 5: Commit**

```text
feat(access): add ResourceKV constant and KVResource() helper
```

### Task 3: Update subject format in hostfunc

**Files:**

- Modify: `internal/plugin/hostfunc/adapter.go:66-68`
- Modify: `internal/plugin/hostfunc/helpers.go:115`
- Modify: `internal/plugin/hostfunc/world_write.go:206`

- [ ] **Step 1: Run existing tests as baseline**

Run: `task test -- ./internal/plugin/hostfunc/...`
Expected: PASS — establishes baseline before refactor

- [ ] **Step 2: Update `adapter.go` SubjectID()**

Change line 67 from:

```go
return "system:plugin:" + a.pluginName
```

to:

```go
return access.PluginSubject(a.pluginName)
```

Add import `"github.com/holomush/holomush/internal/access"`.

- [ ] **Step 3: Update `helpers.go` withMutatorContext**

Change line 115 from:

```go
subjectID := "system:plugin:" + pluginName
```

to:

```go
subjectID := access.PluginSubject(pluginName)
```

Add import `"github.com/holomush/holomush/internal/access"`.

- [ ] **Step 4: Update `world_write.go` findLocationFn**

Change line 206 from:

```go
subjectID := "system:plugin:" + pluginName
```

to:

```go
subjectID := access.PluginSubject(pluginName)
```

Add import `"github.com/holomush/holomush/internal/access"`.

- [ ] **Step 5: Run all hostfunc tests**

Run: `task test -- ./internal/plugin/hostfunc/...`
Expected: PASS — behavior unchanged, just using the helper

- [ ] **Step 6: Verify no remaining hardcoded subjects**

Run: `rg 'system:plugin:' internal/plugin/hostfunc/ --type go`
Expected: No matches in production code (test files may still reference old format for comparison)

- [ ] **Step 7: Commit**

```text
refactor(hostfunc): use access.PluginSubject() for plugin subject format

Changes subject from "system:plugin:<name>" to "plugin:<name>" via the
PluginSubject() helper, making plugin subjects matchable by ABAC policies
with "principal is plugin" conditions.
```

---

## Chunk 2: Manifest Schema + Policy Store Extension

### Task 4: Add `ManifestPolicy` type and update `Manifest` struct

**Files:**

- Modify: `internal/plugin/manifest.go:30-35`
- Modify: `internal/plugin/manifest_test.go`

- [ ] **Step 1: Write the failing test for policy parsing**

In `internal/plugin/manifest_test.go`, add:

```go
func TestParseManifest_WithPolicies(t *testing.T) {
	data := []byte(`
name: test-plugin
version: "1.0.0"
policies:
  - name: "allow-emit"
    dsl: |
      permit(principal, action, resource) when {
        principal is plugin
        and action is "emit"
      };
  - name: "allow-kv"
    dsl: |
      permit(principal, action, resource) when {
        principal is plugin
        and resource like "kv:test-plugin:*"
      };
lua-plugin:
  entry: main.lua
`)
	m, err := plugins.ParseManifest(data)
	require.NoError(t, err)
	assert.Len(t, m.Policies, 2)
	assert.Equal(t, "allow-emit", m.Policies[0].Name)
	assert.Contains(t, m.Policies[0].DSL, "principal is plugin")
	assert.Equal(t, "allow-kv", m.Policies[1].Name)
}

func TestParseManifest_NoPolicies(t *testing.T) {
	data := []byte(`
name: test-plugin
version: "1.0.0"
lua-plugin:
  entry: main.lua
`)
	m, err := plugins.ParseManifest(data)
	require.NoError(t, err)
	assert.Empty(t, m.Policies)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run "TestParseManifest_WithPolicies|TestParseManifest_NoPolicies" ./internal/plugin/...`
Expected: FAIL — `m.Policies` undefined

- [ ] **Step 3: Implement schema changes**

In `internal/plugin/manifest.go`:

Add the `ManifestPolicy` type:

```go
// ManifestPolicy defines an ABAC policy contributed by a plugin.
// Policies are installed into the PolicyStore when the plugin loads.
type ManifestPolicy struct {
	Name string `yaml:"name"`
	DSL  string `yaml:"dsl"`
}
```

In the `Manifest` struct, replace:

```go
Capabilities []string `yaml:"capabilities,omitempty"`
```

with:

```go
Policies []ManifestPolicy `yaml:"policies,omitempty"`
```

- [ ] **Step 4: Fix any tests that reference `Capabilities`**

Search for `m.Capabilities` or `.Capabilities` in `manifest_test.go` and update assertions. Tests that previously checked `Capabilities: ["cap1", "cap2"]` should either be removed or converted to check `Policies`. Note: `CommandSpec.Capabilities` is UNCHANGED — only top-level `Manifest.Capabilities`.

Run: `task test -- ./internal/plugin/...`
Expected: Fix any compilation errors from the removed field.

- [ ] **Step 5: Add validation for ManifestPolicy**

In `manifest.go`, add to `Validate()`:

```go
for i, p := range m.Policies {
	if p.Name == "" {
		return oops.Errorf("policy[%d]: name cannot be empty", i)
	}
	if p.DSL == "" {
		return oops.Errorf("policy[%d] %q: dsl cannot be empty", i, p.Name)
	}
}
```

- [ ] **Step 6: Write validation tests**

```go
func TestParseManifest_PolicyEmptyName(t *testing.T) {
	data := []byte(`
name: test-plugin
version: "1.0.0"
policies:
  - name: ""
    dsl: "permit(principal, action, resource);"
lua-plugin:
  entry: main.lua
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name cannot be empty")
}

func TestParseManifest_PolicyEmptyDSL(t *testing.T) {
	data := []byte(`
name: test-plugin
version: "1.0.0"
policies:
  - name: "my-policy"
    dsl: ""
lua-plugin:
  entry: main.lua
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dsl cannot be empty")
}
```

- [ ] **Step 7: Run all tests**

Run: `task test -- ./internal/plugin/...`
Expected: PASS

- [ ] **Step 8: Commit**

```text
feat(plugin): replace Capabilities with Policies in manifest schema

Plugins now define ABAC policies in their manifest YAML instead of
declaring capabilities. CommandSpec.Capabilities is retained for
character-level command filtering.
```

### Task 5: Extend `ValidateSourceNaming` for `plugin:` prefix

**Files:**

- Modify: `internal/access/policy/store/store.go:61-87`
- Check: `internal/access/policy/store/store_test.go` (if exists)

- [ ] **Step 1: Write the failing test**

Find or create the test file for `ValidateSourceNaming`. Add:

```go
func TestValidateSourceNaming_PluginPrefix(t *testing.T) {
	// plugin: prefix requires source "plugin"
	assert.NoError(t, store.ValidateSourceNaming("plugin:echo-bot:emit", "plugin"))

	// plugin: prefix with wrong source
	err := store.ValidateSourceNaming("plugin:echo-bot:emit", "admin")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "POLICY_SOURCE_MISMATCH")

	// source "plugin" requires plugin: prefix
	err = store.ValidateSourceNaming("my-policy", "plugin")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "POLICY_SOURCE_MISMATCH")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestValidateSourceNaming_PluginPrefix ./internal/access/policy/store/...`
Expected: FAIL — no plugin prefix check yet

- [ ] **Step 3: Implement**

In `ValidateSourceNaming`, add after the lock checks:

```go
hasPluginPrefix := strings.HasPrefix(name, "plugin:")

if hasPluginPrefix && source != "plugin" {
	return oops.Code("POLICY_SOURCE_MISMATCH").
		With("name", name).With("source", source).
		Errorf("policy named 'plugin:*' must have source 'plugin'")
}
if !hasPluginPrefix && source == "plugin" {
	return oops.Code("POLICY_SOURCE_MISMATCH").
		With("name", name).With("source", source).
		Errorf("policy with source 'plugin' must be named 'plugin:*'")
}
```

- [ ] **Step 4: Run tests**

Run: `task test -- ./internal/access/policy/store/...`
Expected: PASS

- [ ] **Step 5: Commit**

```text
feat(policy/store): extend ValidateSourceNaming for plugin: prefix
```

### Task 6: Add `DeleteBySource` to `PolicyStore`

**Files:**

- Modify: `internal/access/policy/store/store.go` (interface)
- Modify: `internal/access/policy/store/postgres.go` (implementation)

- [ ] **Step 1: Add to interface**

In `store.go`, add to the `PolicyStore` interface:

```go
// DeleteBySource deletes all policies with the given source whose name starts
// with namePrefix. Used for bulk cleanup of plugin policies on unload.
DeleteBySource(ctx context.Context, source, namePrefix string) (int64, error)
```

Returns `int64` (count of deleted rows) for observability.

- [ ] **Step 2: Regenerate mocks (required before Tasks 8-9 can compile)**

Adding `DeleteBySource` to the interface breaks any mockery-generated mock.
Regenerate:

Run: `mockery`

Verify no compilation errors in packages that use `PolicyStore` mocks:

Run: `task test -- ./internal/access/policy/...`

- [ ] **Step 3: Implement in postgres.go**

```go
func (s *PostgresStore) DeleteBySource(ctx context.Context, source, namePrefix string) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, oops.In("policy_store").Wrap(err)
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx,
		`DELETE FROM access_policies WHERE source = $1 AND name LIKE $2 || '%'`,
		source, namePrefix)
	if err != nil {
		return 0, oops.In("policy_store").With("source", source).With("prefix", namePrefix).Wrap(err)
	}

	if result.RowsAffected() > 0 {
		_, err = tx.Exec(ctx, `SELECT pg_notify('policy_changed', $1)`, namePrefix)
		if err != nil {
			return 0, oops.In("policy_store").Wrap(err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, oops.In("policy_store").Wrap(err)
	}

	return result.RowsAffected(), nil
}
```

- [ ] **Step 4: Run tests**

Run: `task test -- ./internal/access/policy/store/...`
Expected: PASS (compile check — integration tests would cover the actual SQL)

- [ ] **Step 5: Commit**

```text
feat(policy/store): add DeleteBySource for bulk plugin policy cleanup
```

---

## Chunk 3: Plugin Attribute Provider

### Task 7: Create `PluginAttributeProvider`

**Files:**

- Create: `internal/access/policy/attribute/plugin_provider.go`
- Create: `internal/access/policy/attribute/plugin_provider_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/access/policy/attribute/plugin_provider_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/types"
)

type mockPluginRegistry struct {
	loaded map[string]bool
}

func (m *mockPluginRegistry) IsPluginLoaded(name string) bool {
	return m.loaded[name]
}

func TestPluginProvider_Namespace(t *testing.T) {
	p := NewPluginProvider(&mockPluginRegistry{})
	assert.Equal(t, "plugin", p.Namespace())
}

func TestPluginProvider_ResolveSubject(t *testing.T) {
	registry := &mockPluginRegistry{loaded: map[string]bool{"echo-bot": true}}
	p := NewPluginProvider(registry)

	tests := []struct {
		name        string
		subjectID   string
		expectAttrs map[string]any
		expectNil   bool
	}{
		{
			name:        "loaded plugin",
			subjectID:   "echo-bot",
			expectAttrs: map[string]any{"name": "echo-bot"},
		},
		{
			name:        "unloaded plugin returns nil",
			subjectID:   "unknown-plugin",
			expectAttrs: map[string]any{"name": "unknown-plugin"},
		},
		{
			name:      "empty ID returns nil",
			subjectID: "",
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs, err := p.ResolveSubject(context.Background(), tt.subjectID)
			require.NoError(t, err)
			if tt.expectNil {
				assert.Nil(t, attrs)
			} else {
				assert.Equal(t, tt.expectAttrs, attrs)
			}
		})
	}
}

func TestPluginProvider_ResolveResource(t *testing.T) {
	p := NewPluginProvider(&mockPluginRegistry{})
	attrs, err := p.ResolveResource(context.Background(), "anything")
	require.NoError(t, err)
	assert.Nil(t, attrs)
}

func TestPluginProvider_Schema(t *testing.T) {
	p := NewPluginProvider(&mockPluginRegistry{})
	schema := p.Schema()
	require.NotNil(t, schema)
	assert.Equal(t, types.AttrTypeString, schema.Attributes["name"])
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestPluginProvider ./internal/access/policy/attribute/...`
Expected: FAIL — `NewPluginProvider` undefined

- [ ] **Step 3: Implement the provider**

Create `internal/access/policy/attribute/plugin_provider.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"

	"github.com/holomush/holomush/internal/access/policy/types"
)

// PluginRegistry checks whether a plugin is currently loaded.
type PluginRegistry interface {
	IsPluginLoaded(name string) bool
}

// PluginProvider resolves attributes for plugin subjects.
// Namespace: "plugin". Subject format: the plugin name (after prefix stripping).
type PluginProvider struct {
	registry PluginRegistry
}

// NewPluginProvider creates a provider that resolves plugin subject attributes.
func NewPluginProvider(registry PluginRegistry) *PluginProvider {
	return &PluginProvider{registry: registry}
}

func (p *PluginProvider) Namespace() string { return "plugin" }

func (p *PluginProvider) ResolveSubject(_ context.Context, subjectID string) (map[string]any, error) {
	if subjectID == "" {
		return nil, nil
	}
	return map[string]any{"name": subjectID}, nil
}

func (p *PluginProvider) ResolveResource(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

func (p *PluginProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"name": types.AttrTypeString,
		},
	}
}
```

- [ ] **Step 4: Run tests**

Run: `task test -- -run TestPluginProvider ./internal/access/policy/attribute/...`
Expected: PASS

- [ ] **Step 5: Commit**

```text
feat(access/attribute): add PluginAttributeProvider for plugin subjects
```

---

## Chunk 4: Plugin Policy Installer

### Task 8: Create `PluginPolicyInstaller` interface and implementation

**Files:**

- Create: `internal/plugin/policy_installer.go`
- Create: `internal/plugin/policy_installer_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/plugin/policy_installer_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/access/policy/compiler"
	"github.com/holomush/holomush/internal/access/policy/store"
)

func TestPolicyInstaller_Install(t *testing.T) {
	ps := &fakePolicyStore{}
	c := compiler.NewCompiler()
	installer := plugins.NewPolicyInstaller(ps, c)

	policies := []plugins.ManifestPolicy{
		{Name: "emit-events", DSL: `permit(principal, action, resource) when { principal is plugin and action is "emit" };`},
	}

	err := installer.InstallPluginPolicies(context.Background(), "echo-bot", policies)
	require.NoError(t, err)
	assert.Len(t, ps.created, 1)
	assert.Equal(t, "plugin:echo-bot:emit-events", ps.created[0].Name)
	assert.Equal(t, "plugin", ps.created[0].Source)
}

func TestPolicyInstaller_Install_RejectNonPluginPrincipal(t *testing.T) {
	ps := &fakePolicyStore{}
	c := compiler.NewCompiler()
	installer := plugins.NewPolicyInstaller(ps, c)

	policies := []plugins.ManifestPolicy{
		{Name: "bad-policy", DSL: `permit(principal, action, resource) when { principal is character };`},
	}

	err := installer.InstallPluginPolicies(context.Background(), "evil-plugin", policies)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "principal is plugin")
	assert.Empty(t, ps.created)
}

func TestPolicyInstaller_Install_RejectNilPrincipalType(t *testing.T) {
	ps := &fakePolicyStore{}
	c := compiler.NewCompiler()
	installer := plugins.NewPolicyInstaller(ps, c)

	policies := []plugins.ManifestPolicy{
		{Name: "open-policy", DSL: `permit(principal, action, resource);`},
	}

	err := installer.InstallPluginPolicies(context.Background(), "evil-plugin", policies)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "principal is plugin")
}

func TestPolicyInstaller_Remove(t *testing.T) {
	ps := &fakePolicyStore{}
	c := compiler.NewCompiler()
	installer := plugins.NewPolicyInstaller(ps, c)

	err := installer.RemovePluginPolicies(context.Background(), "echo-bot")
	require.NoError(t, err)
	assert.Equal(t, "plugin", ps.deletedSource)
	assert.Equal(t, "plugin:echo-bot:", ps.deletedPrefix)
}

func TestPolicyInstaller_Install_CompilationError(t *testing.T) {
	ps := &fakePolicyStore{}
	c := compiler.NewCompiler()
	installer := plugins.NewPolicyInstaller(ps, c)

	policies := []plugins.ManifestPolicy{
		{Name: "bad-dsl", DSL: `this is not valid dsl`},
	}

	err := installer.InstallPluginPolicies(context.Background(), "echo-bot", policies)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "compile")
	assert.Empty(t, ps.created)
}

// --- Test doubles ---

// fakePolicyStore implements the subset of store.PolicyStore that
// PolicyInstaller uses. It records calls for assertion.
type fakePolicyStore struct {
	created       []*store.StoredPolicy
	deletedSource string
	deletedPrefix string
}

func (s *fakePolicyStore) Create(_ context.Context, p *store.StoredPolicy) error {
	s.created = append(s.created, p)
	return nil
}

func (s *fakePolicyStore) DeleteBySource(_ context.Context, source, prefix string) (int64, error) {
	s.deletedSource = source
	s.deletedPrefix = prefix
	return 0, nil
}

// Implement remaining PolicyStore interface methods as no-ops
// (Get, GetByID, Update, Delete, ListEnabled, List)
// Only the methods used by PolicyInstaller need real implementations.
```

The test uses the real `compiler.NewCompiler()` for DSL compilation and a fake
`PolicyStore` that matches the real `Create(*StoredPolicy)` signature. The
installer depends on a narrower interface than full `PolicyStore` — define that
interface in `policy_installer.go` (see Step 3).

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestPolicyInstaller ./internal/plugin/...`
Expected: FAIL — types undefined

- [ ] **Step 3: Implement the policy installer**

Create `internal/plugin/policy_installer.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"encoding/json"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/compiler"
	"github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// PluginPolicyInstaller manages ABAC policies contributed by plugins.
type PluginPolicyInstaller interface {
	InstallPluginPolicies(ctx context.Context, pluginName string, policies []ManifestPolicy) error
	RemovePluginPolicies(ctx context.Context, pluginName string) error
	ReplacePluginPolicies(ctx context.Context, pluginName string, policies []ManifestPolicy) error
}

// policyStoreWriter is the subset of store.PolicyStore that PolicyInstaller needs.
// This keeps the plugin system decoupled from the full PolicyStore interface.
type policyStoreWriter interface {
	Create(ctx context.Context, p *store.StoredPolicy) error
	DeleteBySource(ctx context.Context, source, namePrefix string) (int64, error)
}

// PolicyInstaller implements PluginPolicyInstaller using a PolicyStore and DSL compiler.
type PolicyInstaller struct {
	store    policyStoreWriter
	compiler *compiler.Compiler
}

// NewPolicyInstaller creates a PolicyInstaller.
func NewPolicyInstaller(s policyStoreWriter, c *compiler.Compiler) *PolicyInstaller {
	return &PolicyInstaller{store: s, compiler: c}
}

func (pi *PolicyInstaller) InstallPluginPolicies(ctx context.Context, pluginName string, policies []ManifestPolicy) error {
	for _, mp := range policies {
		compiled, warnings, err := pi.compiler.Compile(mp.DSL)
		_ = warnings // warnings are non-fatal; log if desired
		if err != nil {
			return oops.In("plugin_policy").
				With("plugin", pluginName).
				With("policy", mp.Name).
				Wrapf(err, "compile policy DSL")
		}

		// Scope validation: plugin policies MUST target plugin principals only.
		// Check CompiledTarget.PrincipalType != nil && *PrincipalType == "plugin".
		if compiled.Target.PrincipalType == nil || *compiled.Target.PrincipalType != "plugin" {
			return oops.In("plugin_policy").
				With("plugin", pluginName).
				With("policy", mp.Name).
				Errorf("plugin policy must include 'principal is plugin' condition")
		}

		ast, err := json.Marshal(compiled)
		if err != nil {
			return oops.In("plugin_policy").Wrap(err)
		}

		policyName := "plugin:" + pluginName + ":" + mp.Name
		sp := &store.StoredPolicy{
			Name:        policyName,
			Description: "Plugin policy for " + pluginName,
			Effect:      types.PolicyEffectPermit,
			Source:      "plugin",
			DSLText:     mp.DSL,
			CompiledAST: ast,
			Enabled:     true,
			CreatedBy:   "plugin:" + pluginName,
		}

		if err := pi.store.Create(ctx, sp); err != nil {
			return oops.In("plugin_policy").
				With("plugin", pluginName).
				With("policy", policyName).
				Wrap(err)
		}
	}
	return nil
}

func (pi *PolicyInstaller) RemovePluginPolicies(ctx context.Context, pluginName string) error {
	prefix := "plugin:" + pluginName + ":"
	_, err := pi.store.DeleteBySource(ctx, "plugin", prefix)
	if err != nil {
		return oops.In("plugin_policy").With("plugin", pluginName).Wrap(err)
	}
	return nil
}

func (pi *PolicyInstaller) ReplacePluginPolicies(ctx context.Context, pluginName string, policies []ManifestPolicy) error {
	if err := pi.RemovePluginPolicies(ctx, pluginName); err != nil {
		return err
	}
	return pi.InstallPluginPolicies(ctx, pluginName, policies)
}
```

Key points:

- `Compile()` returns 3 values `(compiled, warnings, err)` — warnings are non-fatal
- `policyStoreWriter` is a narrow interface matching only `Create` + `DeleteBySource`
- `ReplacePluginPolicies` combines remove + install (spec §3 atomicity requirement)
- `CompiledTarget.PrincipalType` is `*string` — nil check + value check

- [ ] **Step 4: Adjust test doubles to match real interfaces**

After implementing, update the test doubles to match the actual `PolicyStore.Create` and compiler method signatures. Run:

Run: `task test -- -run TestPolicyInstaller ./internal/plugin/...`
Expected: PASS

- [ ] **Step 5: Commit**

```text
feat(plugin): add PluginPolicyInstaller for manifest-defined ABAC policies

Compiles and installs plugin policies on load with scope validation
(must target "principal is plugin"). Removes by source prefix on unload.
```

### Task 9: Wire `PluginPolicyInstaller` into `Manager`

**Files:**

- Modify: `internal/plugin/manager.go`

- [ ] **Step 1: Add the option**

Add to `Manager` struct:

```go
policyInstaller PluginPolicyInstaller
```

Add option:

```go
// WithPolicyInstaller sets the policy installer for plugin ABAC policies.
func WithPolicyInstaller(pi PluginPolicyInstaller) ManagerOption {
	return func(m *Manager) {
		m.policyInstaller = pi
	}
}
```

- [ ] **Step 2: Call installer in `loadPlugin`**

After the successful `m.luaHost.Load(ctx, dp.Manifest, dp.Dir)` call, add:

```go
if m.policyInstaller != nil && len(dp.Manifest.Policies) > 0 {
	if err := m.policyInstaller.InstallPluginPolicies(ctx, dp.Manifest.Name, dp.Manifest.Policies); err != nil {
		// Roll back the plugin load on policy failure
		_ = m.luaHost.Unload(ctx, dp.Manifest.Name)
		return oops.In("manager").With("plugin", dp.Manifest.Name).Wrapf(err, "install plugin policies")
	}
}
```

- [ ] **Step 3: Call installer in `Close`**

In `Close()`, before clearing loaded plugins, add:

```go
if m.policyInstaller != nil {
	for name := range m.loaded {
		if err := m.policyInstaller.RemovePluginPolicies(ctx, name); err != nil {
			slog.Error("failed to remove plugin policies", "plugin", name, "error", err)
		}
	}
}
```

- [ ] **Step 4: Run tests**

Run: `task test -- ./internal/plugin/...`
Expected: PASS — existing tests don't set `policyInstaller` so the nil check skips it

- [ ] **Step 5: Commit**

```text
feat(plugin): wire PluginPolicyInstaller into Manager lifecycle

Manager calls InstallPluginPolicies after Host.Load() succeeds and
RemovePluginPolicies before Host.Close(). Policy installation failure
rolls back the plugin load.
```

---

## Chunk 5: KV ABAC Checks + get_command_help Access Check

### Task 10: Add ABAC checks to KV host functions

**Files:**

- Modify: `internal/plugin/hostfunc/functions.go:207-309`
- Modify: `internal/plugin/hostfunc/functions_test.go`

- [ ] **Step 1: Write the failing tests**

In `internal/plugin/hostfunc/functions_test.go`, add. Note: `mockKVStore` already
exists in this file (used by existing KV tests with `getData` and `setErr` fields):

```go
func TestKVGet_DeniedByEngine(t *testing.T) {
	kvStore := &mockKVStore{}
	engine := policytest.DenyAllEngine()
	hf := hostfunc.New(kvStore, hostfunc.WithEngine(engine))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	err := L.DoString(`
		local val, err = holomush.kv_get("mykey")
		result_val = val
		result_err = err
	`)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, L.GetGlobal("result_val"))
	errStr := L.GetGlobal("result_err")
	assert.Contains(t, errStr.String(), "access denied")
}

func TestKVGet_AllowedByEngine(t *testing.T) {
	kvStore := &mockKVStore{getData: map[string][]byte{"mykey": []byte("hello")}}
	engine := policytest.AllowAllEngine()
	hf := hostfunc.New(kvStore, hostfunc.WithEngine(engine))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	err := L.DoString(`
		local val, err = holomush.kv_get("mykey")
		result_val = val
		result_err = err
	`)
	require.NoError(t, err)

	assert.Equal(t, "hello", L.GetGlobal("result_val").String())
	assert.Equal(t, lua.LNil, L.GetGlobal("result_err"))
}

func TestKVGet_NilEngine_Denied(t *testing.T) {
	kvStore := &mockKVStore{}
	hf := hostfunc.New(kvStore) // no engine

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	err := L.DoString(`
		local val, err = holomush.kv_get("mykey")
		result_err = err
	`)
	require.NoError(t, err)

	errStr := L.GetGlobal("result_err")
	assert.Contains(t, errStr.String(), "access engine not available")
}

func TestKVSet_DeniedByEngine(t *testing.T) {
	kvStore := &mockKVStore{}
	engine := policytest.DenyAllEngine()
	hf := hostfunc.New(kvStore, hostfunc.WithEngine(engine))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	err := L.DoString(`
		local _, err = holomush.kv_set("mykey", "value")
		result_err = err
	`)
	require.NoError(t, err)

	errStr := L.GetGlobal("result_err")
	assert.Contains(t, errStr.String(), "access denied")
}

func TestKVDelete_DeniedByEngine(t *testing.T) {
	kvStore := &mockKVStore{}
	engine := policytest.DenyAllEngine()
	hf := hostfunc.New(kvStore, hostfunc.WithEngine(engine))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	err := L.DoString(`
		local _, err = holomush.kv_delete("mykey")
		result_err = err
	`)
	require.NoError(t, err)

	errStr := L.GetGlobal("result_err")
	assert.Contains(t, errStr.String(), "access denied")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run "TestKV.*Engine" ./internal/plugin/hostfunc/...`
Expected: FAIL — KV functions don't check engine yet

- [ ] **Step 3: Add ABAC check helper**

In `functions.go`, add a helper:

```go
// checkKVAccess evaluates ABAC for a KV operation. Returns an error string
// for Lua if denied, or empty string if allowed.
func (f *Functions) checkKVAccess(pluginName, action, key string) string {
	if f.engine == nil {
		return "access engine not available"
	}

	subject := access.PluginSubject(pluginName)
	resource := access.KVResource(pluginName, key)

	ctx, cancel := context.WithTimeout(context.Background(), defaultPluginQueryTimeout)
	defer cancel()

	req, err := types.NewAccessRequest(subject, action, resource)
	if err != nil {
		slog.Error("failed to create KV access request",
			"plugin", pluginName, "action", action, "key", key, "error", err)
		return "access check failed"
	}

	decision, err := f.engine.Evaluate(ctx, req)
	if err != nil {
		slog.Error("KV access check engine error",
			"plugin", pluginName, "action", action, "key", key, "error", err)
		return "access check failed"
	}

	if !decision.IsAllowed() {
		return "access denied"
	}
	return ""
}
```

- [ ] **Step 4: Add ABAC check to `kvGetFn`**

Insert after the empty key check, before the nil kvStore check:

```go
if errMsg := f.checkKVAccess(pluginName, "read", key); errMsg != "" {
	L.Push(lua.LNil)
	L.Push(lua.LString(errMsg))
	return 2
}
```

- [ ] **Step 5: Add ABAC check to `kvSetFn`**

Same pattern, using action `"write"`:

```go
if errMsg := f.checkKVAccess(pluginName, "write", key); errMsg != "" {
	L.Push(lua.LNil)
	L.Push(lua.LString(errMsg))
	return 2
}
```

- [ ] **Step 6: Add ABAC check to `kvDeleteFn`**

Same pattern, using action `"delete"`:

```go
if errMsg := f.checkKVAccess(pluginName, "delete", key); errMsg != "" {
	L.Push(lua.LNil)
	L.Push(lua.LString(errMsg))
	return 2
}
```

- [ ] **Step 7: Run all KV tests**

Run: `task test -- -run "TestKV" ./internal/plugin/hostfunc/...`
Expected: PASS

- [ ] **Step 8: Run all hostfunc tests to check for regressions**

Run: `task test -- ./internal/plugin/hostfunc/...`
Expected: PASS — existing KV tests that don't set an engine will now get "access engine not available" errors. Update these tests to use `WithEngine(policytest.AllowAllEngine())` if needed.

- [ ] **Step 9: Commit**

```text
feat(hostfunc): add ABAC enforcement for KV operations

KV get/set/delete now require engine evaluation before accessing the
store. Nil engine returns "access engine not available". Denied access
returns "access denied". Uses plugin:<name> subjects and kv:<ns>:<key>
resources.
```

### Task 11: Add access check to `get_command_help`

**Files:**

- Modify: `internal/plugin/hostfunc/commands.go:221-261`
- Modify: `internal/plugin/hostfunc/commands_test.go`

- [ ] **Step 1: Write the failing test**

In `commands_test.go` (internal package `hostfunc`), add:

```go
func TestGetCommandHelp_AccessDenied(t *testing.T) {
	capCmd := command.NewTestEntry(command.CommandEntryConfig{
		Name:         "secret-cmd",
		Help:         "A secret command",
		Capabilities: []string{"admin.secret"},
		Source:       "test-plugin",
	})

	// Use the existing mockCommandRegistry (slice-based, not map-based)
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{capCmd},
	}
	engine := policytest.DenyAllEngine()
	hf := New(nil, WithCommandRegistry(registry), WithEngine(engine))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	charID := ulid.Make()
	err := L.DoString(fmt.Sprintf(`
		local result, err = holomush.get_command_help("secret-cmd", "%s")
		help_result = result
		help_err = err
	`, charID.String()))
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, L.GetGlobal("help_result"))
	errStr := L.GetGlobal("help_err").String()
	assert.Contains(t, errStr, "access denied")
}

func TestGetCommandHelp_NoCapabilities_NoCheck(t *testing.T) {
	noCapCmd := command.NewTestEntry(command.CommandEntryConfig{
		Name:   "open-cmd",
		Help:   "An open command",
		Source: "test-plugin",
	})

	// Engine denies all, but command has no capabilities so no check needed
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{noCapCmd},
	}
	engine := policytest.DenyAllEngine()
	hf := New(nil, WithCommandRegistry(registry), WithEngine(engine))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	charID := ulid.Make()
	err := L.DoString(fmt.Sprintf(`
		local result, err = holomush.get_command_help("open-cmd", "%s")
		help_result = result
		help_err = err
	`, charID.String()))
	require.NoError(t, err)

	assert.NotEqual(t, lua.LNil, L.GetGlobal("help_result"))
	assert.Equal(t, lua.LNil, L.GetGlobal("help_err"))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run "TestGetCommandHelp_Access" ./internal/plugin/hostfunc/...`
Expected: FAIL — no access check, wrong arg count

- [ ] **Step 3: Update `getCommandHelpFn`**

Modify the function to accept `character_id` as second argument and add access check:

```go
func (f *Functions) getCommandHelpFn(_ string) lua.LGFunction {
	return func(L *lua.LState) int {
		commandName := L.CheckString(1)
		characterID := L.CheckString(2)

		if f.commandRegistry == nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("command registry not available"))
			return 2
		}

		cmd, ok := f.commandRegistry.Get(commandName)
		if !ok {
			L.Push(lua.LNil)
			L.Push(lua.LString(fmt.Sprintf("command %q not found", commandName)))
			return 2
		}

		// Access check for capability-gated commands (use GetCapabilities(), not direct field)
		if len(cmd.GetCapabilities()) > 0 && f.engine != nil {
			charID, err := ulid.Parse(characterID)
			if err != nil {
				L.Push(lua.LNil)
				L.Push(lua.LString("invalid character_id"))
				return 2
			}

			ctx := L.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			subject := access.CharacterSubject(charID.String())
			allowed, _ := f.canExecuteCommand(ctx, subject, cmd)
			if !allowed {
				L.Push(lua.LNil)
				L.Push(lua.LString("access denied"))
				return 2
			}
		}

		// Build result table (preserve existing table construction)
		// ... keep existing table construction that returns (table, nil) ...

		return 2 // result table + nil error — matches existing Lua API contract
	}
}
```

Preserve the existing result table construction (name, help, usage, help_text, source, capabilities).

- [ ] **Step 4: Update existing `get_command_help` tests**

Existing tests that call `get_command_help("name")` need updating to `get_command_help("name", "<valid-ulid>")`. Search for `get_command_help` in `commands_test.go` and add the second argument.

- [ ] **Step 5: Run all command tests**

Run: `task test -- ./internal/plugin/hostfunc/...`
Expected: PASS

- [ ] **Step 6: Update help plugin Lua code**

The `help` plugin calls `holomush.get_command_help(name)`. Update to `holomush.get_command_help(name, ctx.character_id)`. Check `plugins/help/main.lua`.

- [ ] **Step 7: Run integration tests if available**

Run: `task test -- -run "TestHelp" ./internal/plugin/...`
Expected: PASS

- [ ] **Step 8: Commit**

```text
feat(hostfunc): add ABAC check to get_command_help

get_command_help now takes (name, character_id) and evaluates command
capabilities before returning help. Commands without capabilities skip
the check. Breaking Lua API change — only the help plugin is affected.
```

---

## Chunk 6: Plugin Manifest Migration

### Task 12: Update plugin manifests

**Files:**

- Modify: `plugins/building/plugin.yaml`
- Modify: `plugins/communication/plugin.yaml`
- Modify: `plugins/echo-bot/plugin.yaml`
- Modify: `plugins/help/plugin.yaml`

- [ ] **Step 1: Update building plugin**

Replace `capabilities:` with policies. The building plugin needs:

- World write permissions (create location, create exit)
- World read permissions (find location)

```yaml
policies:
  - name: "world-write"
    dsl: |
      permit(principal, action, resource) when {
        principal is plugin
        and principal.plugin.name is "building"
        and action in ["write", "create"]
        and resource like "location:*"
      };
  - name: "world-write-exit"
    dsl: |
      permit(principal, action, resource) when {
        principal is plugin
        and principal.plugin.name is "building"
        and action in ["write", "create"]
        and resource like "exit:*"
      };
  - name: "world-read"
    dsl: |
      permit(principal, action, resource) when {
        principal is plugin
        and principal.plugin.name is "building"
        and action is "read"
        and resource like "location:*"
      };
```

- [ ] **Step 2: Update communication plugin**

The communication plugin emits to location and character streams:

```yaml
policies:
  - name: "emit-location"
    dsl: |
      permit(principal, action, resource) when {
        principal is plugin
        and principal.plugin.name is "communication"
        and action is "emit"
        and resource like "stream:*"
      };
```

- [ ] **Step 3: Update echo-bot plugin**

```yaml
policies:
  - name: "emit-events"
    dsl: |
      permit(principal, action, resource) when {
        principal is plugin
        and principal.plugin.name is "echo-bot"
        and action is "emit"
        and resource like "stream:*"
      };
```

- [ ] **Step 4: Update help plugin**

The help plugin needs command list and help access:

```yaml
policies:
  - name: "command-access"
    dsl: |
      permit(principal, action, resource) when {
        principal is plugin
        and principal.plugin.name is "help"
        and action is "read"
        and resource like "command:*"
      };
```

- [ ] **Step 5: Verify manifests parse**

Run: `task test -- -run "TestParseManifest" ./internal/plugin/...`
Expected: PASS — or write a quick smoke test that parses each real manifest file

- [ ] **Step 6: Commit**

```text
feat(plugins): migrate manifests from capabilities to ABAC policies

All 4 plugin manifests (building, communication, echo-bot, help) now
define ABAC policies instead of capability declarations. Each policy
scopes to its own plugin name via principal.plugin.name condition.
```

---

## Chunk 7: Package Docstring + Documentation

### Task 13: Update package docstring

**Files:**

- Modify: `internal/plugin/hostfunc/functions.go:4-8`

- [ ] **Step 1: Update the package docstring**

Replace the current docstring (line 7):

```go
// Access control is enforced via ABAC policies at the service layer.
```

with:

```go
// Access control is enforced via ABAC policies: world operations at the
// service layer (world.Service.checkAccess), KV operations at the hostfunc
// layer (checkKVAccess), and command access via the AccessPolicyEngine.
```

- [ ] **Step 2: Commit**

```text
docs(hostfunc): update package docstring to reflect ABAC enforcement points
```

### Task 14: Update `WithWorldService` comment

**Files:**

- Modify: `internal/plugin/hostfunc/functions.go:50-51`

- [ ] **Step 1: Update comment**

Change line 50-51 from:

```go
// Each plugin will get its own adapter with authorization subject "system:plugin:<name>".
```

to:

```go
// Each plugin will get its own adapter with authorization subject "plugin:<name>".
```

- [ ] **Step 2: Check for other stale `system:plugin:` references**

Run: `rg 'system:plugin:' --type go -l`

Update any remaining comments or non-test references.

- [ ] **Step 3: Commit**

```text
docs(hostfunc): update comments for new plugin subject format
```

---

## Chunk 8: Seed Validation Tests + CI

### Task 15: Seed policy validation tests

**Files:**

- Create: `internal/access/policy/seed_validation_test.go`

- [ ] **Step 1: Write seed validation tests**

Create a test that loads all seed policies into an engine and verifies expected decisions:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/types"
)

func TestSeedPolicies_PluginSubjectsDefaultDeny(t *testing.T) {
	// Plugin subjects should not match any seed policies (all seed policies
	// target "principal is character"). This confirms default-deny for plugins.
	engine := buildSeedEngine(t)

	req, err := types.NewAccessRequest(
		access.PluginSubject("unknown-plugin"),
		"read",
		access.LocationResource("01TESTLOCATION00000"),
	)
	require.NoError(t, err)

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, decision.IsAllowed(), "plugin subject should be denied by default")
}

// buildSeedEngine creates an engine loaded with all seed policies.
// Implementation depends on the actual engine construction API — check
// existing engine tests in internal/access/policy/engine_test.go for
// the pattern used to construct test engines with compiled policies.
func buildSeedEngine(t *testing.T) types.AccessPolicyEngine {
	t.Helper()
	// TODO: Build engine with seed policies — adapt to actual API
	// Look at how engine_test.go constructs engines, e.g.:
	// - policy.NewEngine(policy.WithCacheLoader(...))
	// - or use the policy store + seeder to install then create engine
	t.Skip("implement buildSeedEngine using actual engine construction pattern")
	return nil
}
```

The exact implementation of `buildSeedEngine` depends on the engine's test construction API. Check how existing engine tests create engines with policies and follow that pattern.

- [ ] **Step 2: Implement `buildSeedEngine`**

Look at existing engine tests to find the pattern for constructing a test engine with seed policies. Likely involves compiling seeds and creating an engine with a mock/in-memory store.

- [ ] **Step 3: Add more seed validation cases**

Add cases for known character access patterns (e.g., character can read their own location, character can emit to a stream they're in, etc.). These serve as a regression suite for the seed policy set.

- [ ] **Step 4: Run tests**

Run: `task test -- -run TestSeedPolicies ./internal/access/policy/...`
Expected: PASS

- [ ] **Step 5: Commit**

```text
test(policy): add seed policy validation tests

Verifies expected ABAC decisions for seed policies, including that
plugin subjects receive default-deny (no seed policies target plugins).
```

### Task 16: CI static analysis for AccessControl.Check

**Files:**

- Modify: `.golangci.yml` (if `forbidigo` is available)
- Or create: `internal/access/policy/legacy_check_test.go`

- [ ] **Step 1: Check if forbidigo is available**

Run: `rg 'forbidigo' .golangci.yml` to see if it's already configured.

- [ ] **Step 2: If forbidigo is available, add rule**

Add to `.golangci.yml` under `linters-settings.forbidigo.forbid`:

```yaml
- p: 'AccessControl\.Check'
  msg: "Use AccessPolicyEngine.Evaluate instead of legacy AccessControl.Check"
```

- [ ] **Step 3: If forbidigo is not available, create AST-based test**

Create `internal/access/policy/legacy_check_test.go`:

```go
func TestNoLegacyAccessControlCheck(t *testing.T) {
	// Walk production Go files and verify no AccessControl.Check references
	// This is deferred — see spec §10 for options
	t.Skip("deferred: implement forbidigo or AST-based check")
}
```

- [ ] **Step 4: Run lint**

Run: `task lint`
Expected: PASS

- [ ] **Step 5: Commit**

```text
chore(ci): add static analysis guard for legacy AccessControl.Check
```

---

## Integration Test Notes

The spec (§8) calls for integration tests verifying end-to-end flows:

- Plugin loads → policies appear in store → engine permits declared operations
- Plugin unloads → policies removed → engine denies
- Plugin with no policies → all operations denied (default-deny)

These require a running PostgreSQL instance and are tagged
`//go:build integration`. They should be added to
`test/integration/plugin/` or `internal/plugin/` following the existing
`help_integration_test.go` pattern. Implementation of these is deferred
to a follow-up task since they require the full stack wired up.

---

## Post-Implementation Checklist

- [ ] Run full test suite: `task test`
- [ ] Run linter: `task lint`
- [ ] Run formatter: `task fmt`
- [ ] Verify no `system:plugin:` references remain in production code: `rg 'system:plugin:' --type go -l | grep -v _test.go`
- [ ] Verify no top-level `Capabilities` in plugin manifests: `rg '^capabilities:' plugins/`
- [ ] Update beads: close completed tasks, create follow-up tasks for deferred items
- [ ] Push to remote: `git push`
