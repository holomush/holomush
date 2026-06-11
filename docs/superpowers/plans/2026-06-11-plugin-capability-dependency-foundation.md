<!--
SPDX-License-Identifier: Apache-2.0
Copyright 2026 HoloMUSH Contributors
-->

# Plugin Capability & Dependency Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use dev-flow:subagent-driven-development (recommended) or dev-flow:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the overloaded flat-string `requires` field with a typed
capability/service dependency model, make the plugin loader's resolver validate
and order that unified graph with a structured result + fail-fast policy, and
reclassify the four phantom `requires` so the every-boot DAG-fallback bug is
fixed end-to-end.

**Architecture:** A new `Dependency` value type (kind = `capability` | `service`,
plus `version`/`optional` attributes) parsed from string-or-map YAML;
`Manifest.Requires` becomes `[]Dependency` with behavior-preserving accessors
for the existing Lua/binary consumers; a `CapabilityVocabulary` registry of
valid host-capability names; `ResolveDependencyOrder` returns a structured
`ResolveResult` and validates each entry by kind; `resolveLoadOrder` fails the
boot on any non-optional unsatisfied entry or cycle.

**Tech Stack:** Go, `gopkg.in/yaml.v3` (custom `UnmarshalYAML`), testify, the
invariant registry (`cmd/inv-render`).

**Spec:** `docs/superpowers/specs/2026-06-11-plugin-capability-dependency-foundation-design.md`
**Design bead:** `holomush-oeb4d` (epic `holomush-eykuh`).

---

## File Structure

| File | Responsibility | Action |
| --- | --- | --- |
| `internal/plugin/dependency_type.go` | The `Dependency` value type + `UnmarshalYAML` + `RequireServices` constructor | Create |
| `internal/plugin/dependency_type_test.go` | Unit tests for parsing both forms + attributes + constructor | Create |
| `internal/plugin/manifest.go` | `Requires []string` → `[]Dependency`; accessor methods (incl `RequiresDisplay`) | Modify (`:99`, add accessors) |
| `internal/plugin/lua/host.go` | Use `RequiredServiceNames()` accessor (behavior-preserving) | Modify (`:323`,`:402`,`:508`) |
| `internal/plugin/goplugin/host.go` | Use `RequiredServiceNames()` in the broker loop + guard | Modify (`:344`,`:666`,`:667`) |
| `internal/command/handlers/plugin_admin.go` | `plugin info` renderer: `strings.Join(m.Requires…)` → `m.RequiresDisplay()` | Modify (`:86-87`) |
| Test literals: `dependency_test.go`, `manifest_test.go:1615,1651`, `lua/host_test.go:1643`, `goplugin/host_test.go:1225,1301`, `command/handlers/plugin_admin_test.go:100`, `test/integration/plugin/binary_plugin_test.go:131` | Migrate `Requires: []string{…}` → `RequireServices(…)` and `m.Requires` asserts → accessor | Modify (Task 2) |
| `internal/plugin/capability_vocab.go` | `CapabilityVocabulary` registry + minimal default set | Create |
| `internal/plugin/capability_vocab_test.go` | Unit tests for the vocabulary | Create |
| `internal/plugin/dependency.go` | `ResolveResult` + typed, kind-validated resolver | Modify (`ResolveDependencyOrder` at `:30`) |
| `internal/plugin/dependency_test.go` | Extend with typed-entry + structured-result tests | Modify |
| `internal/plugin/manager.go` | `resolveLoadOrder` consumes `ResolveResult` + fail-fast | Modify (`:809`); `LoadAll` propagates error (`:658`) |
| `plugins/core-communication/plugin.yaml` | Reclassify `SessionService` → `capability: session` | Modify (`:10-11`) |
| `plugins/core-objects/plugin.yaml` | Reclassify Property/WorldQuery → capabilities | Modify |
| `plugins/core-aliases/plugin.yaml` | Drop the phantom `requires` block | Modify (`:10-11`) |
| `schemas/plugin.schema.json` | Allow typed `requires` entries (string or object) | Modify |
| `internal/plugin/loadall_regression_test.go` | Real `plugins/*` set resolves with no fallback | Create |
| `docs/architecture/invariants.yaml` | Register `INV-PLUGIN-41..45` | Modify |

---

### Task 1: The `Dependency` value type

**Files:**

- Create: `internal/plugin/dependency_type.go`
- Test: `internal/plugin/dependency_type_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/plugin/dependency_type_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestDependencyUnmarshalsBareStringAsService(t *testing.T) {
	var d Dependency
	require.NoError(t, yaml.Unmarshal([]byte(`holomush.scene.v1.SceneService`), &d))
	assert.Equal(t, DependencyService, d.Kind)
	assert.Equal(t, "holomush.scene.v1.SceneService", d.Name)
	assert.False(t, d.Optional)
}

func TestDependencyUnmarshalsCapabilityEntry(t *testing.T) {
	var d Dependency
	require.NoError(t, yaml.Unmarshal([]byte(`{capability: world.query}`), &d))
	assert.Equal(t, DependencyCapability, d.Kind)
	assert.Equal(t, "world.query", d.Name)
}

func TestDependencyUnmarshalsServiceEntryWithAttributes(t *testing.T) {
	var d Dependency
	require.NoError(t, yaml.Unmarshal([]byte(`{service: holomush.scene.v1.SceneService, version: ">=1.0.0", optional: true}`), &d))
	assert.Equal(t, DependencyService, d.Kind)
	assert.Equal(t, "holomush.scene.v1.SceneService", d.Name)
	assert.Equal(t, ">=1.0.0", d.Version)
	assert.True(t, d.Optional)
}

func TestDependencyRejectsEntryWithBothKinds(t *testing.T) {
	var d Dependency
	err := yaml.Unmarshal([]byte(`{capability: x, service: y}`), &d)
	assert.Error(t, err)
}

func TestDependencyRejectsEntryWithNeitherKind(t *testing.T) {
	var d Dependency
	err := yaml.Unmarshal([]byte(`{version: ">=1.0.0"}`), &d)
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestDependency ./internal/plugin/`
Expected: FAIL — `undefined: Dependency`.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/plugin/dependency_type.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"github.com/samber/oops"
	"gopkg.in/yaml.v3"
)

// DependencyKind distinguishes a host-provided capability (no DAG edge) from a
// plugin-or-host-provided gRPC service (provider-before-consumer edge).
type DependencyKind string

const (
	// DependencyCapability is a host-provided capability named in the controlled
	// capability vocabulary (short name, e.g. "world.query").
	DependencyCapability DependencyKind = "capability"
	// DependencyService is a gRPC service contract named by its full proto path.
	DependencyService DependencyKind = "service"
)

// Dependency is one typed entry in a manifest's requires list (spec §1).
type Dependency struct {
	Kind     DependencyKind
	Name     string
	Version  string // semver constraint; services only
	Optional bool
	// Scope carries least-privilege parameters; semantics are sub-spec 4. The
	// foundation parses and round-trips it but does not interpret it.
	Scope string
}

// dependencyYAML is the object form accepted by UnmarshalYAML.
type dependencyYAML struct {
	Capability string `yaml:"capability"`
	Service    string `yaml:"service"`
	Version    string `yaml:"version"`
	Optional   bool   `yaml:"optional"`
	Scope      string `yaml:"scope"`
}

// UnmarshalYAML accepts either a bare string (treated as a service path, for
// backward compatibility with the legacy flat-string requires form) or a typed
// object with exactly one of capability/service.
func (d *Dependency) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		d.Kind = DependencyService
		d.Name = value.Value
		return nil
	}
	var raw dependencyYAML
	if err := value.Decode(&raw); err != nil {
		return oops.Code("DEPENDENCY_MALFORMED").Wrap(err)
	}
	hasCap, hasSvc := raw.Capability != "", raw.Service != ""
	if hasCap == hasSvc {
		return oops.Code("DEPENDENCY_KIND_AMBIGUOUS").
			Errorf("a requires entry MUST have exactly one of capability/service")
	}
	d.Version, d.Optional, d.Scope = raw.Version, raw.Optional, raw.Scope
	if hasCap {
		d.Kind, d.Name = DependencyCapability, raw.Capability
	} else {
		d.Kind, d.Name = DependencyService, raw.Service
	}
	return nil
}

// RequireServices builds a []Dependency of service-kind entries. It exists so
// that test fixtures (and any caller constructing a manifest in Go) can migrate
// from the old []string{...} form with minimal churn.
func RequireServices(names ...string) []Dependency {
	out := make([]Dependency, 0, len(names))
	for _, n := range names {
		out = append(out, Dependency{Kind: DependencyService, Name: n})
	}
	return out
}
```

Add to the test file (Step 1):

```go
func TestRequireServicesConstructsServiceDeps(t *testing.T) {
	deps := RequireServices("a", "b")
	require.Len(t, deps, 2)
	assert.Equal(t, DependencyService, deps[0].Kind)
	assert.Equal(t, "a", deps[0].Name)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run 'TestDependency|TestRequireServices' ./internal/plugin/`
Expected: PASS (6 subtests).

- [ ] **Step 5: Commit**

Commit per `references/vcs-preamble.md`: `feat(plugin): typed Dependency value type (holomush-oeb4d)`.

---

### Task 2: Switch `Manifest.Requires` to `[]Dependency` + accessors

**Files:**

- Modify: `internal/plugin/manifest.go:99` (field type), add accessor methods
- Modify: `internal/plugin/lua/host.go:323,402,508`
- Modify: `internal/plugin/goplugin/host.go:344,667`
- Test: `internal/plugin/manifest_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/plugin/manifest_test.go — add
func TestParseManifestTypedRequiresAccessors(t *testing.T) {
	m, err := ParseManifest([]byte(`
name: t
version: 1.0.0
type: lua
requires:
  - capability: world.query
  - service: holomush.scene.v1.SceneService
`))
	require.NoError(t, err)
	assert.Equal(t, []string{"world.query"}, m.RequiredCapabilities())
	assert.Equal(t, []string{"holomush.scene.v1.SceneService"}, m.RequiredServiceNames())
}

func TestParseManifestLegacyFlatRequiresParsesAsServices(t *testing.T) {
	m, err := ParseManifest([]byte(`
name: t
version: 1.0.0
type: lua
requires:
  - holomush.world.v1.WorldService
`))
	require.NoError(t, err)
	assert.Empty(t, m.RequiredCapabilities())
	assert.Equal(t, []string{"holomush.world.v1.WorldService"}, m.RequiredServiceNames())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestParseManifestTypedRequires ./internal/plugin/`
Expected: FAIL — `m.RequiredCapabilities undefined` and a type error on `Requires`.

- [ ] **Step 3: Change the field + add accessors**

In `internal/plugin/manifest.go`, change line 99 from
`Requires []string ...` to:

```go
	Requires []Dependency `yaml:"requires,omitempty" json:"requires,omitempty"`
```

Add accessor methods near the `Manifest` type:

```go
// RequiredServiceNames returns the names of the service-kind dependencies,
// preserving the legacy flat-string semantics for broker / InjectRequired
// consumers.
func (m *Manifest) RequiredServiceNames() []string {
	out := make([]string, 0, len(m.Requires))
	for _, d := range m.Requires {
		if d.Kind == DependencyService {
			out = append(out, d.Name)
		}
	}
	return out
}

// RequiredCapabilities returns the names of the capability-kind dependencies.
func (m *Manifest) RequiredCapabilities() []string {
	out := make([]string, 0, len(m.Requires))
	for _, d := range m.Requires {
		if d.Kind == DependencyCapability {
			out = append(out, d.Name)
		}
	}
	return out
}

// RequiresDisplay returns human-readable "<kind>:<name>" strings for admin /
// `plugin info` rendering.
func (m *Manifest) RequiresDisplay() []string {
	out := make([]string, 0, len(m.Requires))
	for _, d := range m.Requires {
		out = append(out, string(d.Kind)+":"+d.Name)
	}
	return out
}
```

- [ ] **Step 4: Update the production consumers (behavior-preserving)**

In `internal/plugin/lua/host.go`, at lines 323, 402, 508, change
`requires := p.manifest.Requires` to `requires := p.manifest.RequiredServiceNames()`.
(The legacy `InjectRequired` path keys on proto service names; capability
entries are delivered by the unconditional path in the foundation — injection
gating is sub-spec 3/4, so service-name semantics are preserved here.)

In `internal/plugin/goplugin/host.go`: line 344 `len(m.Requires) > 0` compiles
unchanged (RequiresServices guard). Change the broker-loop guard + range at
lines 666-667 to use service-kind only:

```go
	if len(manifest.RequiredServiceNames()) > 0 && grpcPlugin.broker != nil && h.registry != nil {
		for _, svcName := range manifest.RequiredServiceNames() {
```

In `internal/command/handlers/plugin_admin.go:86-87`, change the `plugin info`
renderer:

```go
	if len(m.Requires) > 0 {
		fmt.Fprintf(&sb, "\nRequires: %s", strings.Join(m.RequiresDisplay(), ", "))
	}
```

- [ ] **Step 5: Migrate the test fixtures (compile fix for the type change)**

Every `Requires: []string{...}` struct literal and every `[]string` assertion
on `m.Requires` breaks at the field-type change. Update each:

- `internal/plugin/dependency_test.go` (lines 16, 28, 39-40, 48, 87-89):
  `Requires: []string{"svc-a"}` → `Requires: RequireServices("svc-a")` (repeat
  per site, preserving each site's service names). *(The 2-arg
  `ResolveDependencyOrder(plugins, nil)` call signature is migrated in Task 4.)*
- `internal/plugin/manifest_test.go:1615,1651`:
  `assert.Equal(t, []string{"holomush.world.v1.WorldService"}, m.Requires)` →
  `assert.Equal(t, []string{"holomush.world.v1.WorldService"}, m.RequiredServiceNames())`.
- `internal/plugin/lua/host_test.go:1643`: `Requires: []string{"test-svc"}` →
  `Requires: plugins.RequireServices("test-svc")` (this is package `lua`; the
  constructor is exported from package `plugins`).
- `internal/plugin/goplugin/host_test.go:1225,1301`: same pattern —
  `plugins.RequireServices("event-store")` / `plugins.RequireServices("holomush.scene.v1.SceneService")`.
- `internal/command/handlers/plugin_admin_test.go:100`:
  `Requires: []string{"holomush.world.v1.WorldService"}` →
  `Requires: plugins.RequireServices("holomush.world.v1.WorldService")`.
- `test/integration/plugin/binary_plugin_test.go:131`:
  `Expect(dp.Manifest.Requires).To(ContainElement("holomush.world.v1.WorldService"))` →
  `Expect(dp.Manifest.RequiredServiceNames()).To(ContainElement("holomush.world.v1.WorldService"))`.

- [ ] **Step 6: Run unit + build**

Run: `task test -- ./internal/plugin/... ./internal/command/...` then `task build`
Expected: PASS; build succeeds (all consumers + fixtures migrated).

- [ ] **Step 7: Run integration (Requires type change is shared-type surface)**

Run: `task test:int -- -run TestDEKIntegration\|TestPlugin ./internal/plugin/... ./test/integration/...`
Expected: PASS. (Per CLAUDE.md: `task test` does not compile integration files; a shared-type change MUST run `task test:int`. This compiles `binary_plugin_test.go`, migrated in Step 5.)

- [ ] **Step 8: Commit**

`feat(plugin): Manifest.Requires becomes typed []Dependency with accessors (holomush-oeb4d)`.

---

### Task 3: Capability vocabulary registry

**Files:**

- Create: `internal/plugin/capability_vocab.go`
- Test: `internal/plugin/capability_vocab_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/plugin/capability_vocab_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCapabilityVocabularyHasRegisteredName(t *testing.T) {
	v := NewCapabilityVocabulary()
	v.Register("world.query")
	assert.True(t, v.Has("world.query"))
	assert.False(t, v.Has("not.a.capability"))
}

func TestDefaultCapabilityVocabularyCoversFoundationMinimum(t *testing.T) {
	v := DefaultCapabilityVocabulary()
	for _, name := range []string{"session", "property", "world.query"} {
		assert.True(t, v.Has(name), "default vocabulary MUST include %q", name)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestCapabilityVocabulary\|TestDefaultCapability ./internal/plugin/`
Expected: FAIL — `undefined: NewCapabilityVocabulary`.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/plugin/capability_vocab.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

// CapabilityVocabulary is the controlled set of valid host-capability names a
// manifest may reference via `requires: [{capability: <name>}]` (spec §1). The
// FULL taxonomy is defined in sub-spec 2; the foundation registers only the
// minimum the four reclassified manifests require.
type CapabilityVocabulary struct {
	names map[string]struct{}
}

// NewCapabilityVocabulary returns an empty vocabulary.
func NewCapabilityVocabulary() *CapabilityVocabulary {
	return &CapabilityVocabulary{names: make(map[string]struct{})}
}

// Register adds a capability name to the vocabulary.
func (v *CapabilityVocabulary) Register(name string) { v.names[name] = struct{}{} }

// Has reports whether name is a registered capability.
func (v *CapabilityVocabulary) Has(name string) bool {
	_, ok := v.names[name]
	return ok
}

// DefaultCapabilityVocabulary returns the foundation's minimal vocabulary —
// only the names the four reclassified manifests (spec §4) depend on. Sub-spec
// 2 replaces this with the full taxonomy bound to capability-scoped contracts.
func DefaultCapabilityVocabulary() *CapabilityVocabulary {
	v := NewCapabilityVocabulary()
	for _, name := range []string{"session", "property", "world.query"} {
		v.Register(name)
	}
	return v
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestCapabilityVocabulary\|TestDefaultCapability ./internal/plugin/`
Expected: PASS.

- [ ] **Step 5: Commit**

`feat(plugin): minimal host-capability vocabulary registry (holomush-oeb4d)`.

---

### Task 4: Structured, kind-validated resolver

**Files:**

- Modify: `internal/plugin/dependency.go` (`ResolveDependencyOrder` at `:30`)
- Test: `internal/plugin/dependency_test.go`

The current signature is
`func ResolveDependencyOrder(plugins []*DiscoveredPlugin, serverServices []string) ([]*DiscoveredPlugin, error)`.
It is replaced by a structured-result form that also takes the capability vocabulary.

- [ ] **Step 1: Write the failing tests**

Migrate the existing scenarios in `dependency_test.go` (their `Requires` struct
literals were already converted to `RequireServices(...)` in Task 2 Step 5):
each existing `order, err := ResolveDependencyOrder(plugins, nil)` becomes
`res, err := ResolveDependencyOrder(plugins, nil, NewCapabilityVocabulary())`,
and assertions on `order[i]` become `res.Ordered[i]`; the two "returns error
for unsatisfied / detects circular" subtests now assert on `res.Unsatisfied` /
`res.Cycles` (non-empty) instead of `assert.Error`. Then add the new tests:

```go
// internal/plugin/dependency_test.go — add

func TestResolveResultReportsUnsatisfiedCapability(t *testing.T) {
	plugins := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "c", Requires: []Dependency{{Kind: DependencyCapability, Name: "world.query"}}}},
	}
	vocab := NewCapabilityVocabulary() // empty — world.query unknown
	res, err := ResolveDependencyOrder(plugins, nil, vocab)
	require.NoError(t, err) // structured result, not a Go error
	require.Len(t, res.Unsatisfied, 1)
	assert.Equal(t, "world.query", res.Unsatisfied[0].Entry.Name)
}

func TestResolveResultSatisfiesRegisteredCapability(t *testing.T) {
	plugins := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "c", Requires: []Dependency{{Kind: DependencyCapability, Name: "session"}}}},
	}
	vocab := DefaultCapabilityVocabulary()
	res, err := ResolveDependencyOrder(plugins, nil, vocab)
	require.NoError(t, err)
	assert.Empty(t, res.Unsatisfied)
	assert.Len(t, res.Ordered, 1)
}

func TestResolveResultMisdeclaredCapabilityThatIsPluginProvided(t *testing.T) {
	plugins := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "consumer", Requires: []Dependency{{Kind: DependencyCapability, Name: "holomush.scene.v1.SceneService"}}}},
		{Manifest: &Manifest{Name: "provider", Provides: []string{"holomush.scene.v1.SceneService"}}},
	}
	res, err := ResolveDependencyOrder(plugins, nil, NewCapabilityVocabulary())
	require.NoError(t, err)
	require.Len(t, res.Unsatisfied, 1)
	assert.Equal(t, "MISDECLARED_DEPENDENCY", res.Unsatisfied[0].Reason)
}

func TestResolveResultOptionalUnsatisfiedIsSkipped(t *testing.T) {
	plugins := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "c", Requires: []Dependency{{Kind: DependencyService, Name: "holomush.absent.v1.X", Optional: true}}}},
	}
	res, err := ResolveDependencyOrder(plugins, nil, NewCapabilityVocabulary())
	require.NoError(t, err)
	assert.Empty(t, res.Unsatisfied)
	assert.Len(t, res.Ordered, 1)
}

func TestResolveResultServiceEdgeOrdersProviderFirst(t *testing.T) {
	plugins := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "consumer", Requires: []Dependency{{Kind: DependencyService, Name: "svc-a"}}}},
		{Manifest: &Manifest{Name: "provider", Provides: []string{"svc-a"}}},
	}
	res, err := ResolveDependencyOrder(plugins, nil, NewCapabilityVocabulary())
	require.NoError(t, err)
	require.Empty(t, res.Unsatisfied)
	assert.Equal(t, "provider", res.Ordered[0].Manifest.Name)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `task test -- -run TestResolveResult ./internal/plugin/`
Expected: FAIL — `ResolveDependencyOrder` arity/return mismatch; `ResolveResult` undefined.

- [ ] **Step 3: Implement the structured resolver**

Replace `ResolveDependencyOrder` in `internal/plugin/dependency.go`. Add the
result types and validate per-kind. Service edges still drive Kahn ordering;
capability entries add no edge. A bare Go `error` is returned only for the
hard structural faults (`DUPLICATE_PLUGIN_NAME`, `DUPLICATE_SERVICE_PROVIDER`);
unsatisfied / misdeclared / cycles are reported in the result.

```go
// UnsatisfiedDep records one declared dependency the resolver could not satisfy.
type UnsatisfiedDep struct {
	Plugin string
	Entry  Dependency
	Reason string // UNSATISFIED_CAPABILITY | UNSATISFIED_SERVICE | MISDECLARED_DEPENDENCY | VERSION_UNSATISFIED
}

// ResolveResult is the structured output of dependency resolution. A future
// per-plugin quarantine policy reads Unsatisfied/Cycles without a resolver
// rewrite (spec §2).
type ResolveResult struct {
	Ordered     []*DiscoveredPlugin
	Unsatisfied []UnsatisfiedDep
	Cycles      [][]string
}

// ResolveDependencyOrder validates and orders the unified dependency graph
// (host capabilities — no edge; plugin/host services — provider-before-consumer
// edge) and returns a structured result. serverServices lists host gRPC services
// (e.g. holomush.world.v1.WorldService); vocab lists valid host capabilities.
func ResolveDependencyOrder(plugins []*DiscoveredPlugin, serverServices []string, vocab *CapabilityVocabulary) (*ResolveResult, error) {
	// ... index byName (DUPLICATE_PLUGIN_NAME), build svcProvider from
	// serverServices + each plugin's Provides (DUPLICATE_SERVICE_PROVIDER) —
	// unchanged from the prior implementation.

	res := &ResolveResult{}
	for _, p := range plugins {
		for _, dep := range p.Manifest.Requires {
			switch dep.Kind {
			case DependencyCapability:
				if !vocab.Has(dep.Name) {
					reason := "UNSATISFIED_CAPABILITY"
					if _, isService := svcProvider[dep.Name]; isService {
						reason = "MISDECLARED_DEPENDENCY"
					}
					if !dep.Optional {
						res.Unsatisfied = append(res.Unsatisfied, UnsatisfiedDep{p.Manifest.Name, dep, reason})
					}
				}
			case DependencyService:
				if _, ok := svcProvider[dep.Name]; !ok {
					if vocab.Has(dep.Name) {
						if !dep.Optional {
							res.Unsatisfied = append(res.Unsatisfied, UnsatisfiedDep{p.Manifest.Name, dep, "MISDECLARED_DEPENDENCY"})
						}
						continue
					}
					if !dep.Optional {
						res.Unsatisfied = append(res.Unsatisfied, UnsatisfiedDep{p.Manifest.Name, dep, "UNSATISFIED_SERVICE"})
					}
				}
				// version check: when svcProvider[dep.Name] is a plugin and dep.Version
				// is set, verify the provider's Manifest.Version satisfies the
				// constraint (reuse the existing Dependencies semver check helper);
				// on miss append VERSION_UNSATISFIED.
			}
		}
	}
	// Kahn's algorithm over service edges + named Dependencies map (unchanged);
	// record cycles in res.Cycles instead of returning CIRCULAR_DEPENDENCY.
	// On success set res.Ordered.
	return res, nil
}
```

> Implementer note: lift the existing `byName` / `svcProvider` construction and
> the Kahn loop verbatim from the prior `ResolveDependencyOrder`
> (`internal/plugin/dependency.go:30-130`); only the per-entry validation and
> the return shape change. Reuse the semver helper already used for
> `Manifest.Dependencies` (`manifest.go:413`).

- [ ] **Step 4: Run to verify pass**

Run: `task test -- -run 'TestResolveResult|TestResolveDependencyOrder' ./internal/plugin/`
Expected: PASS (new + migrated existing scenarios).

- [ ] **Step 5: Commit**

`feat(plugin): structured ResolveResult + kind-validated resolver (holomush-oeb4d)`.

---

### Task 5: Fail-fast loader policy

**Files:**

- Modify: `internal/plugin/manager.go` (`resolveLoadOrder` at `:809`, `LoadAll` at `:658`)
- Test: `internal/plugin/manager_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/plugin/manager_test.go — add
func TestResolveLoadOrderFailsFastOnUnsatisfiedRequired(t *testing.T) {
	m := &Manager{registry: NewServiceRegistry(), capVocab: NewCapabilityVocabulary()}
	discovered := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "c", Requires: []Dependency{{Kind: DependencyService, Name: "holomush.absent.v1.X"}}}},
	}
	_, err := m.resolveLoadOrder(discovered)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_DEPENDENCY_UNSATISFIED")
}

func TestResolveLoadOrderSucceedsWhenAllSatisfied(t *testing.T) {
	m := &Manager{registry: NewServiceRegistry(), capVocab: DefaultCapabilityVocabulary()}
	discovered := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "c", Requires: []Dependency{{Kind: DependencyCapability, Name: "session"}}}},
	}
	ordered, err := m.resolveLoadOrder(discovered)
	require.NoError(t, err)
	assert.Len(t, ordered, 1)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `task test -- -run TestResolveLoadOrder ./internal/plugin/`
Expected: FAIL — `resolveLoadOrder` returns one value, not `([]*DiscoveredPlugin, error)`; `m.capVocab` undefined.

- [ ] **Step 3: Implement**

Add a `capVocab *CapabilityVocabulary` field to `Manager` (default
`DefaultCapabilityVocabulary()` where the manager is constructed in
`internal/plugin/setup/subsystem.go`). Change `resolveLoadOrder` (`manager.go:809`)
to return `([]*DiscoveredPlugin, error)` and fail-fast:

```go
func (m *Manager) resolveLoadOrder(discovered []*DiscoveredPlugin) ([]*DiscoveredPlugin, error) {
	if m.registry == nil {
		return prioritySort(discovered), nil // no registry: legacy priority order
	}
	serverServiceNames := make([]string, 0)
	for _, svc := range m.registry.List() {
		serverServiceNames = append(serverServiceNames, svc.Name)
	}
	res, err := ResolveDependencyOrder(discovered, serverServiceNames, m.capVocab)
	if err != nil {
		return nil, oops.Code("PLUGIN_DEPENDENCY_RESOLVE_FAILED").Wrap(err)
	}
	if len(res.Unsatisfied) > 0 || len(res.Cycles) > 0 {
		return nil, oops.Code("PLUGIN_DEPENDENCY_UNSATISFIED").
			With("unsatisfied", res.Unsatisfied).With("cycles", res.Cycles).
			Errorf("plugin dependency resolution failed; fail-closed (INV-PLUGIN-43)")
	}
	return res.Ordered, nil
}
```

Update `LoadAll` (`manager.go:658`) to propagate the error:

```go
	ordered, err := m.resolveLoadOrder(discovered)
	if err != nil {
		return err
	}
```

Extract the old priority sort into a `prioritySort` helper for the no-registry path.

- [ ] **Step 4: Run unit + integration**

Run: `task test -- ./internal/plugin/...` then `task test:int -- -run TestPlugin ./test/integration/...`
Expected: PASS. (Integration boots the real loader; fail-fast must not trip on the real, now-reclassified plugin set — Task 6 lands those edits, so run Task 6 before the int run if iterating strictly TDD.)

- [ ] **Step 5: Commit**

`feat(plugin): fail-fast loader policy on unsatisfied deps (holomush-oeb4d)`.

---

### Task 6: Reclassify the four manifests + schema + regression test

**Files:**

- Modify: `plugins/core-communication/plugin.yaml`, `plugins/core-objects/plugin.yaml`, `plugins/core-aliases/plugin.yaml`
- Modify: `schemas/plugin.schema.json`
- Create: `internal/plugin/loadall_regression_test.go`

- [ ] **Step 1: Write the failing regression test**

```go
// internal/plugin/loadall_regression_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Verifies: INV-PLUGIN-41
// Verifies: INV-PLUGIN-43
func TestDefaultPluginSetResolvesWithNoUnsatisfiedDeps(t *testing.T) {
	root, err := filepath.Abs("../../plugins")
	require.NoError(t, err)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)

	var discovered []*DiscoveredPlugin
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, rErr := os.ReadFile(filepath.Join(root, e.Name(), "plugin.yaml"))
		if os.IsNotExist(rErr) {
			continue
		}
		require.NoError(t, rErr)
		man, pErr := ParseManifest(data)
		require.NoError(t, pErr, "plugin %s", e.Name())
		discovered = append(discovered, &DiscoveredPlugin{Manifest: man, Dir: e.Name()})
	}

	// Host services present at resolution time on main: only WorldService.
	res, err := ResolveDependencyOrder(discovered, []string{"holomush.world.v1.WorldService"}, DefaultCapabilityVocabulary())
	require.NoError(t, err)
	require.Empty(t, res.Unsatisfied, "default plugin set MUST resolve with no unsatisfied deps")
	require.Empty(t, res.Cycles)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `task test -- -run TestDefaultPluginSetResolves ./internal/plugin/`
Expected: FAIL — `core-aliases`/`core-communication`/`core-objects` still carry phantom `service` requires → `res.Unsatisfied` non-empty.

- [ ] **Step 3: Reclassify the manifests**

`plugins/core-communication/plugin.yaml` — replace the `requires:` block:

```yaml
requires:
  - capability: session
```

`plugins/core-objects/plugin.yaml` — replace the `requires:` block:

```yaml
requires:
  - capability: property
  - capability: world.query
```

`plugins/core-aliases/plugin.yaml` — **delete** the `requires:` block entirely
(lines 10-11; aliases are command-layer via `command.AliasCache`, never a
capability dependency).

- [ ] **Step 4: Update the schema**

In `schemas/plugin.schema.json`, change the `requires` property's `items` to
accept a string **or** a typed object:

```json
"requires": {
  "type": "array",
  "items": {
    "oneOf": [
      { "type": "string" },
      {
        "type": "object",
        "oneOf": [
          { "required": ["capability"] },
          { "required": ["service"] }
        ],
        "properties": {
          "capability": { "type": "string" },
          "service": { "type": "string" },
          "version": { "type": "string" },
          "optional": { "type": "boolean" },
          "scope": { "type": "string" }
        },
        "additionalProperties": false
      }
    ]
  }
}
```

- [ ] **Step 5: Run regression + schema check + full plugin build**

Run: `task test -- -run TestDefaultPluginSetResolves ./internal/plugin/` → PASS
Run: `task lint` (validates `schemas/plugin.schema.json`) → rc=0
Run: `task plugin:build-all` → all binary plugins build

- [ ] **Step 6: Run integration (real loader boots clean)**

Run: `task test:int -- -run TestPlugin ./test/integration/...`
Expected: PASS — no `PLUGIN_DEPENDENCY_UNSATISFIED`, no DAG fallback.

- [ ] **Step 7: Commit**

`fix(plugin): reclassify phantom requires to capabilities; fail-fast resolution (holomush-oeb4d)`.

---

### Task 7: Register the invariants

**Files:**

- Modify: `docs/architecture/invariants.yaml`
- Generated: `docs/architecture/invariants.md` (via `task invariants:render`)

- [ ] **Step 1: Add the five entries**

Append `INV-PLUGIN-41`…`45` to `docs/architecture/invariants.yaml` with the
summaries from the spec's Invariants section. Bindings:

- `INV-PLUGIN-41`, `INV-PLUGIN-43` → `binding: bound`, `asserted_by:
  ["internal/plugin/loadall_regression_test.go"]` (the `// Verifies:` annotations
  from Task 6) plus `internal/plugin/dependency_test.go` for 41.
- `INV-PLUGIN-42` → `binding: bound`, `asserted_by:
  ["internal/plugin/dependency_test.go"]` (the `MISDECLARED_DEPENDENCY` test).
  Add a `// Verifies: INV-PLUGIN-42` annotation above
  `TestResolveResultMisdeclaredCapabilityThatIsPluginProvided`.
- `INV-PLUGIN-44`, `INV-PLUGIN-45` → `binding: pending` (consumption-path
  invariants; bound in sub-spec 3/4). Do NOT list `asserted_by` while pending.

- [ ] **Step 2: Regenerate + format**

Run: `task fmt` then `task invariants:render`
(`task fmt` first — the `lint:yaml` block-scalar re-wrap gotcha.)

- [ ] **Step 3: Verify meta-tests**

Run: `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/`
Expected: PASS.

- [ ] **Step 4: Commit**

`docs(invariants): register INV-PLUGIN-41..45 (holomush-oeb4d)`.

---

## Verification (whole-plan)

- [ ] `task test -- ./internal/plugin/...` — all unit tests pass
- [ ] `task test:int -- -run TestPlugin ./test/integration/...` — real loader boots with no fallback
- [ ] `task lint` rc=0 (schema + yaml + go)
- [ ] `task plugin:build-all` — binary plugins build
- [ ] Manual: `task dev` boots with **no** `UNSATISFIED_REQUIRES` WARN and no
  `DAG dependency resolution failed` line (the `oeb4d` acceptance criterion).
- [ ] `task pr-prep` green before push.
