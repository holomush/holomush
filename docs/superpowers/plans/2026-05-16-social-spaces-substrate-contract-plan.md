# Social Spaces Substrate Contract Implementation Plan

> **⚠️ SUPERSEDED** — this plan was rejected by `plan-reviewer` round 1 (INV-S5 mechanism gap surfaced; mechanism was undefined for both Lua and binary runtimes). The corrected READY plan is at [`2026-05-17-social-spaces-substrate-contract-plan.md`](2026-05-17-social-spaces-substrate-contract-plan.md). **Do not execute this plan.**
>
> **For agentic workers:** REQUIRED SUB-SKILL: Use `dev-flow:subagent-driven-development` (recommended) or `dev-flow:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the substrate work mandated by the substrate-contract spec ([`docs/superpowers/specs/2026-05-16-social-spaces-substrate-contract.md`](../specs/2026-05-16-social-spaces-substrate-contract.md)): manifest emit-type startup validation (INV-S5), plugin adoption of the new API, fail-closed flip, plus docs/roadmap/bead-hygiene propagation.

**Architecture:** A substrate-side validator at plugin load time compares the manifest-declared `crypto.emits` set against an SDK-side emit-type registry that plugins populate during init. Binary plugins implement a new optional capability interface (`EmitTypeRegistrar`); Lua plugins call a new hostfunc (`holomush.register_emit_type`). Substrate compares the two sets after the plugin's Init RPC returns and before plugin readiness is signaled. Phased rollout: capability with no-op default → plugin adoption × 2 → flip to fail-closed.

**Tech Stack:** Go (standard library + `samber/oops` for typed errors + `slog` for logging), `gopher-lua` for Lua hostfunc, existing plugin manager/lifecycle infrastructure at `internal/plugin/manager.go`, existing Go+Lua parity precedent at `internal/plugin/hostfunc/stdlib_focus.go` and `pkg/plugin/focus_client.go`.

**Tracking:** Design bead `holomush-jg9b` (promoted to epic by `plan-to-beads`). Children `jg9b.1`–`jg9b.7` correspond 1:1 to the Tasks below.

---

## File structure

This plan touches these paths. New files in **bold**, existing files modified by path:line where known.

### New SDK + hostfunc + validator surfaces (Task 1)

- **`pkg/plugin/emit_registry.go`** — typed Go SDK API: `RegisterEmitType(string)`, `RegisterEmitTypes([]string)`, `RegisteredEmitTypes() []string`, plus the `EmitTypeRegistrar` interface that binary plugins implement (or the SDK adapter satisfies on their behalf).
- **`pkg/plugin/emit_registry_test.go`** — unit tests for the registry API.
- **`internal/plugin/emit_type_validator.go`** — substrate-side set-equality validator: compares manifest-declared emit-type set against SDK-registered set. Defaults to "warn only" mode (no-op default).
- **`internal/plugin/emit_type_validator_test.go`** — validator tests covering both directions of mismatch (declared-but-unregistered, registered-but-undeclared).
- **`internal/plugin/hostfunc/stdlib_emit_registry.go`** — Lua hostfunc parity: `holomush.register_emit_type` accumulates per-plugin registrations in hostfunc state.
- **`internal/plugin/hostfunc/stdlib_emit_registry_test.go`** — Lua hostfunc tests.

### Wiring (Task 1)

- Modify `internal/plugin/manager.go::loadPlugin` (around current line 844 — adds the post-Init validation call).

### Plugin adoptions (Tasks 2-3)

- Modify `plugins/core-communication/main.lua` — adds `holomush.register_emit_type(...)` calls for each of the 8 declared event types.
- Modify `plugins/core-scenes/main.go` — adds `RegisterEmitTypes([]string{})` call during init (empty set initially; Phase 4 populates).

### Fail-closed flip (Task 4)

- Modify `internal/plugin/emit_type_validator.go` — change validator mode from warn-only to error-on-mismatch.
- Update existing validator tests to expect errors instead of warnings.

### Documentation (Task 5)

- **`site/docs/extending/substrate-contract.md`** — orientation page covering the substrate-vs-use boundary, eventkit/groupkit SDK design, and INV-S5 manifest emit-type validation behavior.
- Modify `site/docs/extending/*.md` index if needed (depends on existing navigation structure — discover during task).

### Roadmap (Task 6)

- Modify `docs/roadmap.md` — update `theme:social-spaces` section to cite the new spec and reflect updated implementation sequence.

### Bead hygiene (Task 7)

- No file changes; modifies bd state only (`bd update`/`bd note` on `5rh.13`, `5rh.14`, `5rh.15`, `0sc.12`, `djj` epic, `aqq` epic, `5rh.9`).

---

## Task 1: Substrate — manifest emit-type validation capability (no-op default)

**bd:** `holomush-jg9b.1`

**Goal:** Ship the Go SDK API + Lua hostfunc + substrate validator wired into plugin load. Validator defaults to warn-only on mismatch (no-op default). Plugin adoption happens in Tasks 2–3; fail-closed flip in Task 4.

**Files:**

- Create: `pkg/plugin/emit_registry.go`
- Create: `pkg/plugin/emit_registry_test.go`
- Create: `internal/plugin/emit_type_validator.go`
- Create: `internal/plugin/emit_type_validator_test.go`
- Create: `internal/plugin/hostfunc/stdlib_emit_registry.go`
- Create: `internal/plugin/hostfunc/stdlib_emit_registry_test.go`
- Modify: `internal/plugin/manager.go::loadPlugin` (insertion point near line 844)

### Step 1.1: Write the failing unit test for `RegisterEmitType` and `RegisteredEmitTypes`

Create `pkg/plugin/emit_registry_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEmitRegistry_RegisterAndList(t *testing.T) {
	r := NewEmitRegistry()
	r.RegisterEmitType("scene_ic")
	r.RegisterEmitType("scene_ooc")
	r.RegisterEmitTypes([]string{"scene_join", "scene_leave"})

	got := r.RegisteredEmitTypes()
	require.ElementsMatch(t, []string{"scene_ic", "scene_ooc", "scene_join", "scene_leave"}, got)
}

func TestEmitRegistry_DuplicateIgnored(t *testing.T) {
	r := NewEmitRegistry()
	r.RegisterEmitType("say")
	r.RegisterEmitType("say")
	require.Equal(t, []string{"say"}, r.RegisteredEmitTypes())
}

func TestEmitRegistry_EmptyByDefault(t *testing.T) {
	r := NewEmitRegistry()
	require.Empty(t, r.RegisteredEmitTypes())
}
```

- [ ] **Step 1.1: write the test file above**

### Step 1.2: Run the test to verify it fails

Run: `task test -- ./pkg/plugin/ -run TestEmitRegistry`
Expected: FAIL with "undefined: NewEmitRegistry" or similar compilation error.

- [ ] **Step 1.2: run the failing test**

### Step 1.3: Implement `EmitRegistry` in `pkg/plugin/emit_registry.go`

Create `pkg/plugin/emit_registry.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import "sync"

// EmitRegistry accumulates the set of event types a plugin can emit.
// Plugins register types during init; the host reads the set after Init
// returns and compares it against the manifest's crypto.emits declarations
// per INV-S5 (manifest emit-type set MUST equal code-registered set).
//
// Concurrency: registration may occur from any goroutine spawned during
// plugin init. The registry uses a mutex for safety, though most plugins
// register from a single init goroutine.
type EmitRegistry struct {
	mu    sync.Mutex
	types map[string]struct{}
}

// NewEmitRegistry returns an empty registry.
func NewEmitRegistry() *EmitRegistry {
	return &EmitRegistry{types: make(map[string]struct{})}
}

// RegisterEmitType records a single event type the plugin can emit.
// Duplicate registrations are silently ignored (idempotent).
func (r *EmitRegistry) RegisterEmitType(eventType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.types[eventType] = struct{}{}
}

// RegisterEmitTypes records a batch of event types.
func (r *EmitRegistry) RegisterEmitTypes(eventTypes []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range eventTypes {
		r.types[t] = struct{}{}
	}
}

// RegisteredEmitTypes returns the set of registered event types as a
// sorted slice. Order is deterministic for test stability.
func (r *EmitRegistry) RegisteredEmitTypes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.types))
	for t := range r.types {
		out = append(out, t)
	}
	sortStrings(out)
	return out
}

// EmitTypeRegistrar is the optional interface binary plugins may
// implement to expose their EmitRegistry to the host. Plugins that do
// not implement this interface skip the registration handshake; the
// host treats them as having an empty registered set, which under
// no-op default mode (pre-fail-closed-flip) produces only a warning.
type EmitTypeRegistrar interface {
	EmitRegistry() *EmitRegistry
}
```

Add a small private `sortStrings` helper at the bottom (use `sort.Strings` from `sort`):

```go
import "sort"

func sortStrings(s []string) { sort.Strings(s) }
```

- [ ] **Step 1.3: write the implementation file**

### Step 1.4: Run the unit tests to verify they pass

Run: `task test -- ./pkg/plugin/ -run TestEmitRegistry`
Expected: PASS — 3 tests.

- [ ] **Step 1.4: verify unit tests pass**

### Step 1.5: Write the failing validator test

Create `internal/plugin/emit_type_validator_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateEmitTypeSetEquality_MatchingSets(t *testing.T) {
	declared := []string{"scene_ic", "scene_ooc"}
	registered := []string{"scene_ooc", "scene_ic"}
	mismatch, err := ValidateEmitTypeSetEquality(declared, registered)
	require.NoError(t, err)
	require.False(t, mismatch.HasMismatch())
}

func TestValidateEmitTypeSetEquality_DeclaredButUnregistered(t *testing.T) {
	declared := []string{"scene_ic", "scene_ooc", "scene_bogus"}
	registered := []string{"scene_ic", "scene_ooc"}
	mismatch, err := ValidateEmitTypeSetEquality(declared, registered)
	require.NoError(t, err)
	require.True(t, mismatch.HasMismatch())
	require.Equal(t, []string{"scene_bogus"}, mismatch.DeclaredButUnregistered)
	require.Empty(t, mismatch.RegisteredButUndeclared)
}

func TestValidateEmitTypeSetEquality_RegisteredButUndeclared(t *testing.T) {
	declared := []string{"scene_ic"}
	registered := []string{"scene_ic", "scene_typo"}
	mismatch, err := ValidateEmitTypeSetEquality(declared, registered)
	require.NoError(t, err)
	require.True(t, mismatch.HasMismatch())
	require.Equal(t, []string{"scene_typo"}, mismatch.RegisteredButUndeclared)
	require.Empty(t, mismatch.DeclaredButUnregistered)
}

func TestValidateEmitTypeSetEquality_BothDirections(t *testing.T) {
	declared := []string{"a", "b"}
	registered := []string{"b", "c"}
	mismatch, _ := ValidateEmitTypeSetEquality(declared, registered)
	require.True(t, mismatch.HasMismatch())
	require.Equal(t, []string{"a"}, mismatch.DeclaredButUnregistered)
	require.Equal(t, []string{"c"}, mismatch.RegisteredButUndeclared)
}
```

- [ ] **Step 1.5: write the validator test file**

### Step 1.6: Run validator tests to verify failure

Run: `task test -- ./internal/plugin/ -run TestValidateEmitTypeSetEquality`
Expected: FAIL with "undefined: ValidateEmitTypeSetEquality".

- [ ] **Step 1.6: run the failing validator tests**

### Step 1.7: Implement the substrate-side validator

Create `internal/plugin/emit_type_validator.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import "sort"

// EmitTypeMismatch describes the diff between a plugin's manifest-declared
// crypto.emits set and the SDK-registered emit-type set. Either or both
// directions may have entries; HasMismatch reports whether any direction
// has any.
type EmitTypeMismatch struct {
	DeclaredButUnregistered []string
	RegisteredButUndeclared []string
}

// HasMismatch reports whether either direction has a non-empty diff.
func (m EmitTypeMismatch) HasMismatch() bool {
	return len(m.DeclaredButUnregistered) > 0 || len(m.RegisteredButUndeclared) > 0
}

// ValidateEmitTypeSetEquality compares the manifest-declared emit-type set
// against the SDK-registered emit-type set. Per INV-S5, the two sets MUST
// be equal in both directions:
//
//   - DeclaredButUnregistered: types in manifest's crypto.emits that the
//     plugin code never registered (dead manifest declarations / typos).
//   - RegisteredButUndeclared: types the plugin code registered that the
//     manifest never declared (silently plaintext under runtime gate when
//     emitted with Sensitive=false).
//
// Returns the diff. The caller decides whether mismatch is fatal (per
// fail-closed flag — see manager.go::loadPlugin).
func ValidateEmitTypeSetEquality(declared, registered []string) (EmitTypeMismatch, error) {
	declSet := toSet(declared)
	regSet := toSet(registered)

	var mismatch EmitTypeMismatch
	for d := range declSet {
		if _, ok := regSet[d]; !ok {
			mismatch.DeclaredButUnregistered = append(mismatch.DeclaredButUnregistered, d)
		}
	}
	for r := range regSet {
		if _, ok := declSet[r]; !ok {
			mismatch.RegisteredButUndeclared = append(mismatch.RegisteredButUndeclared, r)
		}
	}
	sort.Strings(mismatch.DeclaredButUnregistered)
	sort.Strings(mismatch.RegisteredButUndeclared)
	return mismatch, nil
}

func toSet(s []string) map[string]struct{} {
	out := make(map[string]struct{}, len(s))
	for _, v := range s {
		out[v] = struct{}{}
	}
	return out
}
```

- [ ] **Step 1.7: write the validator implementation**

### Step 1.8: Run validator tests to verify they pass

Run: `task test -- ./internal/plugin/ -run TestValidateEmitTypeSetEquality`
Expected: PASS — 4 tests.

- [ ] **Step 1.8: verify validator tests pass**

### Step 1.9: Write the failing Lua hostfunc test

Create `internal/plugin/hostfunc/stdlib_emit_registry_test.go`. The shape follows `stdlib_focus_test.go` (already in this directory) — examine its structure first for the test harness pattern:

```bash
task test -- ./internal/plugin/hostfunc/ -run TestRegisterFocusFuncs -v
```

Then write:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

func TestRegisterEmitTypeFuncs_AccumulatesRegistrations(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	mod := ls.NewTable()

	reg := NewLuaEmitRegistry()
	RegisterEmitTypeFuncs(ls, mod, reg)

	// Simulate plugin calling holomush.register_emit_type("say")
	ls.SetGlobal("holomush", mod)
	err := ls.DoString(`holomush.register_emit_type("say"); holomush.register_emit_type("pose")`)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"say", "pose"}, reg.Types())
}

func TestRegisterEmitTypeFuncs_RejectsNonString(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	mod := ls.NewTable()
	reg := NewLuaEmitRegistry()
	RegisterEmitTypeFuncs(ls, mod, reg)
	ls.SetGlobal("holomush", mod)

	err := ls.DoString(`holomush.register_emit_type(42)`)
	require.Error(t, err)
}
```

- [ ] **Step 1.9: write the Lua hostfunc test**

### Step 1.10: Run Lua test to verify failure

Run: `task test -- ./internal/plugin/hostfunc/ -run TestRegisterEmitTypeFuncs`
Expected: FAIL with "undefined: NewLuaEmitRegistry / RegisterEmitTypeFuncs".

- [ ] **Step 1.10: run failing Lua test**

### Step 1.11: Implement the Lua hostfunc

Create `internal/plugin/hostfunc/stdlib_emit_registry.go`. Use the `stdlib_focus.go` pattern (registration helper + per-plugin state stored in Lua userdata):

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"sync"

	lua "github.com/yuin/gopher-lua"
)

// LuaEmitRegistry accumulates emit-type registrations from a Lua plugin's
// init code. One instance per plugin. The host reads Types() after the
// plugin's init completes, and validates against manifest's crypto.emits
// per INV-S5.
type LuaEmitRegistry struct {
	mu    sync.Mutex
	types map[string]struct{}
}

// NewLuaEmitRegistry returns an empty registry.
func NewLuaEmitRegistry() *LuaEmitRegistry {
	return &LuaEmitRegistry{types: make(map[string]struct{})}
}

// Types returns the registered event types as a slice (order not
// guaranteed; caller sorts if needed).
func (r *LuaEmitRegistry) Types() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.types))
	for t := range r.types {
		out = append(out, t)
	}
	return out
}

// RegisterEmitTypeFuncs installs holomush.register_emit_type on the given
// module table. Each call appends to reg's state.
func RegisterEmitTypeFuncs(ls *lua.LState, mod *lua.LTable, reg *LuaEmitRegistry) {
	ls.SetField(mod, "register_emit_type", ls.NewFunction(func(ls *lua.LState) int {
		eventType := ls.CheckString(1)
		reg.mu.Lock()
		reg.types[eventType] = struct{}{}
		reg.mu.Unlock()
		ls.Push(lua.LTrue)
		return 1
	}))
}
```

- [ ] **Step 1.11: write the Lua hostfunc implementation**

### Step 1.12: Run Lua tests to verify they pass

Run: `task test -- ./internal/plugin/hostfunc/ -run TestRegisterEmitTypeFuncs`
Expected: PASS — 2 tests.

- [ ] **Step 1.12: verify Lua tests pass**

### Step 1.13: Write the failing wiring integration test

The wiring test verifies that `loadPlugin` actually invokes `ValidateEmitTypeSetEquality` post-Init and logs a warning when there's a mismatch (no-op default mode). Add to `internal/plugin/manager_test.go` (existing file — examine its structure first to follow the pattern):

```bash
task test -- ./internal/plugin/ -run TestManager_LoadPlugin -v
```

Then add a new test:

```go
func TestManager_LoadPlugin_EmitTypeMismatch_WarnsInNoOpMode(t *testing.T) {
	// Construct a manifest with crypto.emits declaring "scene_ic"
	// but the plugin's EmitRegistry registers "scene_ic" + "scene_typo".
	// Expect: loadPlugin succeeds, logs a WARN entry naming the mismatch.
	// (After Task 4's fail-closed flip, this test will be updated to expect
	// loadPlugin to return an error instead.)
	//
	// Use the existing test harness for constructing a stub plugin host
	// and a manifest with crypto.emits. Match the pattern in the existing
	// TestManager_LoadPlugin_* tests in this file.

	// ... implementation depends on existing test harness structure ...
}
```

**Important:** read `internal/plugin/manager_test.go` first to find the existing test-harness helpers (likely `newTestManager`, `discoveredPluginFixture`, etc.). Use the same shape.

- [ ] **Step 1.13: write the wiring test (using existing manager_test.go harness)**

### Step 1.14: Wire the validator into `loadPlugin`

Modify `internal/plugin/manager.go::loadPlugin` (around line 844). After the plugin's Init RPC returns and BEFORE the plugin is marked ready, call the validator. The exact insertion point depends on the current loadPlugin structure — add the call after the existing semantic validation around line 880 (Lua) or after binary plugin Init completes.

```go
// New section in loadPlugin, after existing init steps:

// INV-S5: manifest emit-type startup validation.
declared := manifestDeclaredEmitTypes(dp.Manifest)
registered, ok := readPluginEmitRegistry(host, dp)
if !ok {
	// Plugin does not implement EmitTypeRegistrar / has no hostfunc
	// registration — treat as empty registered set (no-op default).
	registered = nil
}
mismatch, _ := ValidateEmitTypeSetEquality(declared, registered)
if mismatch.HasMismatch() {
	slog.Warn("plugin emit-type set mismatch (INV-S5 will fail-close after rollout)",
		"plugin", dp.Manifest.Name,
		"declared_but_unregistered", mismatch.DeclaredButUnregistered,
		"registered_but_undeclared", mismatch.RegisteredButUndeclared)
	// No-op default: do not return error. Task 4 flips this to fail-closed.
}
```

Helper functions in the same file (or a small new helpers file):

```go
func manifestDeclaredEmitTypes(m *Manifest) []string {
	if m.Crypto == nil {
		return nil
	}
	out := make([]string, 0, len(m.Crypto.Emits))
	for _, e := range m.Crypto.Emits {
		out = append(out, e.EventType)
	}
	return out
}

func readPluginEmitRegistry(host Host, dp *DiscoveredPlugin) ([]string, bool) {
	// For binary plugins: Host exposes a way to obtain the plugin's
	// pluginsdk.EmitTypeRegistrar — likely via Host.PluginInstance(dp.Manifest.Name).
	// For Lua plugins: the LuaHost stores per-plugin LuaEmitRegistry,
	// accessible via a new accessor on the host.
	//
	// Implementation detail: examine the existing host interface in
	// internal/plugin/host.go (or wherever Host is defined). Add a new
	// method like Host.PluginEmitRegistry(name) ([]string, bool) and
	// implement on both LuaHost and GoPluginHost. Lua reads from
	// LuaEmitRegistry.Types(); binary reads via type-asserting the
	// plugin's instance against pluginsdk.EmitTypeRegistrar.
	//
	// This is the design seam the implementer settles based on the
	// current Host interface shape.
	return nil, false // placeholder — replace with real implementation
}
```

**Note:** the `readPluginEmitRegistry` function name + signature is illustrative. The actual implementation may live differently depending on the existing `Host` interface shape — read `internal/plugin/host.go` (or wherever the `Host` interface is defined) to determine the right seam.

- [ ] **Step 1.14: wire the validator into loadPlugin (Lua + binary paths)**

### Step 1.15: Run wiring integration test to verify it passes (warns)

Run: `task test -- ./internal/plugin/ -run TestManager_LoadPlugin_EmitTypeMismatch`
Expected: PASS — log capture sees the WARN entry.

- [ ] **Step 1.15: verify wiring test passes**

### Step 1.16: Run the full package test suite to verify no regressions

Run: `task test -- ./pkg/plugin/ ./internal/plugin/ ./internal/plugin/hostfunc/`
Expected: PASS — all tests including the new ones.

- [ ] **Step 1.16: verify no regressions**

### Step 1.17: Run task lint

Run: `task lint`
Expected: PASS or only pre-existing warnings (no new lint issues from this task's code).

- [ ] **Step 1.17: verify lint clean**

### Step 1.18: Commit

Commit via VCS-appropriate commands per `references/vcs-preamble.md`. Commit message:

```text
feat(plugin): manifest emit-type startup validation capability (INV-S5, jg9b.1)

Adds Go SDK API (pkg/plugin/emit_registry.go), substrate-side set-equality
validator (internal/plugin/emit_type_validator.go), Lua hostfunc parity
(internal/plugin/hostfunc/stdlib_emit_registry.go), and loadPlugin wiring
that compares manifest crypto.emits against SDK-registered set after Init.

Defaults to warn-only mode (no-op default). Task 4 flips fail-closed after
plugins adopt RegisterEmitTypes in tasks 2-3.

bd: holomush-jg9b.1
```

- [ ] **Step 1.18: commit**

---

## Task 2: Plugin adoption — core-communication adopts register_emit_type

**bd:** `holomush-jg9b.2`

**Goal:** `core-communication` (Lua plugin) calls `holomush.register_emit_type` for each of the 8 event types in its `crypto.emits` declaration. Validator passes set-equality for this plugin.

**Files:**

- Modify: `plugins/core-communication/main.lua`

The 8 event types per `plugins/core-communication/plugin.yaml:272-297`:
`say`, `pose`, `ooc`, `emit`, `page`, `whisper`, `pemit`, `whisper_notice`.

### Step 2.1: Inspect current main.lua structure

Run: `cat plugins/core-communication/main.lua` (using the Read tool, not bash cat).

Goal: find the plugin's init section — where it sets up state, registers verbs, etc. Note the patterns used (existing hostfunc calls, table construction).

- [ ] **Step 2.1: read main.lua and identify the init section**

### Step 2.2: Add registration calls at the top of init

Add (near the existing init code, before any event-handler registration):

```lua
-- INV-S5: register the event types this plugin can emit.
-- These MUST match plugin.yaml's crypto.emits block exactly.
holomush.register_emit_type("say")
holomush.register_emit_type("pose")
holomush.register_emit_type("ooc")
holomush.register_emit_type("emit")
holomush.register_emit_type("page")
holomush.register_emit_type("whisper")
holomush.register_emit_type("pemit")
holomush.register_emit_type("whisper_notice")
```

- [ ] **Step 2.2: add register_emit_type calls to main.lua**

### Step 2.3: Run the integration test to verify the plugin loads without warning

Run: `task test:int -- ./internal/plugin/ -run TestPluginIntegration_LoadCoreCommunication`
(Adjust test name to match an existing test that exercises core-communication load. If no such test exists, add one or use a probe-style sanity check.)

Expected: PASS — no WARN logs about emit-type mismatch for core-communication.

- [ ] **Step 2.3: verify plugin loads cleanly**

### Step 2.4: Run task test (unit + manager tests) to verify no regressions

Run: `task test -- ./internal/plugin/ ./plugins/core-communication/...`
Expected: PASS.

- [ ] **Step 2.4: verify no regressions**

### Step 2.5: Commit

Commit message:

```text
feat(core-communication): adopt register_emit_type for INV-S5 (jg9b.2)

Adds holomush.register_emit_type calls for each of the 8 event types
declared in plugin.yaml's crypto.emits: say, pose, ooc, emit, page,
whisper, pemit, whisper_notice. Substrate-side validator now passes
set-equality for this plugin under no-op default mode.

bd: holomush-jg9b.2
```

- [ ] **Step 2.5: commit**

---

## Task 3: Plugin adoption — core-scenes adopts RegisterEmitTypes (empty set)

**bd:** `holomush-jg9b.3`

**Goal:** `core-scenes` (binary plugin) implements `pluginsdk.EmitTypeRegistrar` (or equivalent capability) and registers its current `crypto.emits` set (empty). Phase 4 will populate with `scene_ic`/`scene_ooc`/etc.

**Files:**

- Modify: `plugins/core-scenes/main.go`

### Step 3.1: Inspect main.go for the existing capability-interface pattern

The plugin already implements `Handler`, `ServiceProvider`, `AttributeResolverProvider`, and uses the `FocusClientAware` opt-in pattern (`SetFocusClient` method). Read main.go around lines 31-77 to confirm the pattern.

- [ ] **Step 3.1: read main.go capability pattern**

### Step 3.2: Add an `emitRegistry` field to the `scenePlugin` struct

Modify the struct in main.go:

```go
type scenePlugin struct {
	store       *SceneStore
	service     *SceneServiceImpl
	resolver    *SceneResolver
	auditSrv    *SceneAuditServer
	focusClient pluginsdk.FocusClient
	emitRegistry *pluginsdk.EmitRegistry // INV-S5: lifecycle-init registry
}
```

- [ ] **Step 3.2: add emitRegistry field**

### Step 3.3: Initialize the registry in main()

In main.go's `main()` function (or `Init()` if that's where the plugin sets up state), construct the registry and register the empty set. Look at the existing init flow to identify the right insertion point:

```go
p.emitRegistry = pluginsdk.NewEmitRegistry()
p.emitRegistry.RegisterEmitTypes([]string{})  // Empty set; Phase 4 populates.
```

- [ ] **Step 3.3: initialize emitRegistry in main()**

### Step 3.4: Expose the registry via `EmitRegistry()` method (implements EmitTypeRegistrar)

Add the method on `*scenePlugin`:

```go
// EmitRegistry returns the plugin's emit-type registry for substrate's
// INV-S5 startup validation. See pkg/plugin/emit_registry.go.
func (p *scenePlugin) EmitRegistry() *pluginsdk.EmitRegistry {
	return p.emitRegistry
}
```

- [ ] **Step 3.4: add EmitRegistry() method**

### Step 3.5: Run the integration test to verify the plugin loads without warning

Run: `task test:int -- ./internal/plugin/ -run TestPluginIntegration_LoadCoreScenes`
(Adjust test name to match an existing integration test for core-scenes load.)

Expected: PASS — no WARN logs about emit-type mismatch for core-scenes.

- [ ] **Step 3.5: verify plugin loads cleanly**

### Step 3.6: Run task test for core-scenes

Run: `task test -- ./plugins/core-scenes/...`
Expected: PASS.

- [ ] **Step 3.6: verify core-scenes tests pass**

### Step 3.7: Commit

Commit message:

```text
feat(core-scenes): adopt RegisterEmitTypes for INV-S5 (jg9b.3)

Implements pluginsdk.EmitTypeRegistrar by exposing an EmitRegistry
initialized with the current crypto.emits set (empty). Phase 4 will
populate the registry with scene_ic/scene_ooc/scene_join/scene_leave
in lockstep with manifest updates.

bd: holomush-jg9b.3
```

- [ ] **Step 3.7: commit**

---

## Task 4: Substrate — flip emit-type validation fail-closed

**bd:** `holomush-jg9b.4`

**Goal:** Change `loadPlugin`'s mismatch handling from warn-only to error-on-mismatch. Update tests. After this lands, any plugin with a manifest/code mismatch fails plugin startup.

**Files:**

- Modify: `internal/plugin/manager.go::loadPlugin` (the WARN block from Task 1)
- Modify: `internal/plugin/manager_test.go` (update `TestManager_LoadPlugin_EmitTypeMismatch_WarnsInNoOpMode` test name + assertion)

### Step 4.1: Update the failing test for fail-closed behavior

Rename `TestManager_LoadPlugin_EmitTypeMismatch_WarnsInNoOpMode` → `TestManager_LoadPlugin_EmitTypeMismatch_FailsClosed` in `internal/plugin/manager_test.go`. Update the assertion from "log entry exists" to "loadPlugin returns an error with the mismatch details":

```go
func TestManager_LoadPlugin_EmitTypeMismatch_FailsClosed(t *testing.T) {
	// Same setup as before, but now expect loadPlugin to return error.
	err := m.loadPlugin(ctx, dp, knownTypes, knownActions)
	require.Error(t, err)
	require.Contains(t, err.Error(), "EVENT_TYPE_REGISTRY_MISMATCH")
	require.Contains(t, err.Error(), "scene_typo")  // example
}
```

- [ ] **Step 4.1: update the test to expect error**

### Step 4.2: Run the test to verify it currently fails (loadPlugin still warns, doesn't error)

Run: `task test -- ./internal/plugin/ -run TestManager_LoadPlugin_EmitTypeMismatch_FailsClosed`
Expected: FAIL — "expected error, got nil".

- [ ] **Step 4.2: run failing test**

### Step 4.3: Flip the validator block in `loadPlugin` to return an error

Modify `internal/plugin/manager.go::loadPlugin`. Replace the WARN block from Task 1 with:

```go
// INV-S5: manifest emit-type startup validation — FAIL CLOSED.
declared := manifestDeclaredEmitTypes(dp.Manifest)
registered, _ := readPluginEmitRegistry(host, dp)
mismatch, _ := ValidateEmitTypeSetEquality(declared, registered)
if mismatch.HasMismatch() {
	return oops.Code("EVENT_TYPE_REGISTRY_MISMATCH").
		In("manager").
		With("plugin", dp.Manifest.Name).
		With("declared_but_unregistered", mismatch.DeclaredButUnregistered).
		With("registered_but_undeclared", mismatch.RegisteredButUndeclared).
		Errorf("plugin's crypto.emits manifest does not match registered emit-type set (INV-S5)")
}
```

- [ ] **Step 4.3: flip validator to error-on-mismatch**

### Step 4.4: Run the test to verify it now passes

Run: `task test -- ./internal/plugin/ -run TestManager_LoadPlugin_EmitTypeMismatch_FailsClosed`
Expected: PASS.

- [ ] **Step 4.4: verify fail-closed test passes**

### Step 4.5: Run the full plugin test suite

Run: `task test -- ./internal/plugin/ ./pkg/plugin/ ./internal/plugin/hostfunc/`
Expected: PASS. If any plugin in the test fixture set has a mismatch that previously only warned, this will now fail — investigate and either fix the fixture or confirm the new test catches a real bug.

- [ ] **Step 4.5: verify no regressions**

### Step 4.6: Run integration tests (full plugin loading exercise)

Run: `task test:int`
Expected: PASS. core-communication and core-scenes (adopted in Tasks 2-3) load cleanly. Any other production plugin that hasn't adopted will now fail load — investigate.

- [ ] **Step 4.6: verify integration tests pass**

### Step 4.7: Run task pr-prep to mirror CI

Run: `task pr-prep`
Expected: PASS. This is mandatory per CLAUDE.md before pushing — see Landing the Plane section.

- [ ] **Step 4.7: verify task pr-prep passes**

### Step 4.8: Commit

Commit message:

```text
feat(plugin): flip emit-type validation fail-closed (INV-S5, jg9b.4)

Substrate now fails plugin load on manifest/code emit-type set mismatch.
core-communication and core-scenes adopted RegisterEmitTypes in jg9b.2
and jg9b.3 respectively; this flip completes the INV-S5 rollout.

After this lands, any new plugin declaring crypto.emits MUST also
register the same set via the SDK API (binary) or holomush.register_emit_type
hostfunc (Lua). Mismatch = startup failure with EVENT_TYPE_REGISTRY_MISMATCH.

bd: holomush-jg9b.4
```

- [ ] **Step 4.8: commit**

---

## Task 5: Documentation — substrate-contract orientation in site/docs

**bd:** `holomush-jg9b.5`

**Goal:** Add a contributor-facing orientation page at `site/docs/extending/substrate-contract.md` that introduces the substrate-vs-use boundary, eventkit/groupkit SDKs (named-only; no code yet per INV-S7), and INV-S5 manifest emit-type validation behavior.

**Files:**

- Create: `site/docs/extending/substrate-contract.md`
- Possibly modify: site nav index (depending on existing structure — discover during task)

### Step 5.1: Inspect existing site/docs/extending/ structure

Run: `ls site/docs/extending/` (use Bash tool). Read the existing index or readme to understand naming conventions and navigation patterns.

- [ ] **Step 5.1: inventory extending/ structure**

### Step 5.2: Draft the orientation page

Create `site/docs/extending/substrate-contract.md` with these sections:

1. **What is the substrate contract?** (1 paragraph — substrate-vs-use framing; pointer to canonical spec)
2. **Substrate primitives you can rely on** (table summarizing §1 of the spec — JetStream subjects, crypto envelope, ABAC, focus, host RPCs, storage isolation, audit projection)
3. **The plugin-boundary rule (INV-S1)** (short: plugin PRs touch only `plugins/<name>/` + approved `pkg/plugin/*`; substrate changes are separate work)
4. **Manifest emit-type validation (INV-S5)** (what plugin authors need to do: declare in `crypto.emits`, register via SDK API or hostfunc, types must match)
5. **eventkit + groupkit SDKs (named, not yet built)** (when they will land, N=2 discipline)
6. **References** (link to canonical spec, ADRs, related rules)

Keep prose tight; the canonical detail lives in the spec. This page is the contributor-facing on-ramp.

- [ ] **Step 5.2: draft the orientation page**

### Step 5.3: Run rumdl validation on the new page

Run: `rumdl check site/docs/extending/substrate-contract.md`
Expected: PASS or only auto-fixable issues. If issues, run `rumdl fmt`.

- [ ] **Step 5.3: verify markdown lint**

### Step 5.4: Build the docs site locally to verify rendering

Run: `task docs:serve` (background or short-lived) and visit the page in a browser, OR run `task docs:build` and check the rendered output.

Expected: page renders without missing links or formatting errors.

- [ ] **Step 5.4: verify docs site renders**

### Step 5.5: Update site nav if needed

If the page does not auto-appear in the site navigation, find the relevant index/navigation file (likely under `site/docs/extending/` or `site/mkdocs.yml`/zensical config) and add the new page entry.

- [ ] **Step 5.5: update nav if needed**

### Step 5.6: Commit

Commit message:

```text
docs(extending): add substrate-contract orientation page (jg9b.5)

Contributor on-ramp for the substrate-vs-use boundary, INV-S1 strict
plugin-boundary rule, INV-S5 manifest emit-type validation, and the
named (not-yet-built) eventkit/groupkit SDKs.

Canonical detail lives in the substrate-contract spec at
docs/superpowers/specs/2026-05-16-social-spaces-substrate-contract.md;
this page is the entry point for new plugin authors.

bd: holomush-jg9b.5
```

- [ ] **Step 5.6: commit**

---

## Task 6: Roadmap — update theme:social-spaces narrative

**bd:** `holomush-jg9b.6`

**Goal:** `docs/roadmap.md`'s `theme:social-spaces` section reflects (a) substrate-contract spec landing, (b) eventkit + groupkit as named primitives, (c) updated implementation sequence.

**Files:**

- Modify: `docs/roadmap.md`

### Step 6.1: Read the current theme:social-spaces section

Read `docs/roadmap.md` to find the `theme:social-spaces` section (it covers Scenes, Channels, Forums, Discord with substrate/uses framing).

- [ ] **Step 6.1: read roadmap section**

### Step 6.2: Update the section narrative

Modifications:

- Add a "Substrate-contract" subsection (after "Substrate (shipped)") that:
  - References the new spec by path.
  - Names `eventkit` and `groupkit` SDKs as future primitives (gated on N=2 validation).
  - Names the INV-S5 substrate work (`jg9b.1`-`jg9b.4`) as the next-up enabling work.
  - Names `jg9b` epic as the tracking unit.
- Update the "Uses (in development)" sub-section's Phase 4 frontier note: scene Phase 4 (`5rh.13`) is now unblocked by `jg9b.4` (substrate emit-type validation flip) rather than blocked-by-design.
- Update the channels row (`0sc.12`): note it is the N=2 validating consumer for the SDKs.
- Update Sequencing rationale: scenes Phase 4 first (post `jg9b.4`); channels rework in parallel (also post `jg9b.4`, validating SDKs).

Keep the existing narrative voice and table structure. The change is *additive* and *clarifying*, not a rewrite.

- [ ] **Step 6.2: update theme:social-spaces section**

### Step 6.3: Run rumdl on the modified roadmap

Run: `rumdl check docs/roadmap.md`
Expected: PASS or only auto-fixable issues.

- [ ] **Step 6.3: verify markdown lint**

### Step 6.4: Commit

Commit message:

```text
docs(roadmap): update theme:social-spaces with substrate-contract (jg9b.6)

Reflects the substrate-contract spec landing: references the spec,
names eventkit + groupkit SDKs as future primitives, names the INV-S5
substrate work (jg9b.1-jg9b.4) as the next-up enabling work, and
clarifies that scenes Phase 4 (5rh.13) and channels rework (0sc.12)
are both unblocked by jg9b.4's fail-closed flip.

bd: holomush-jg9b.6
```

- [ ] **Step 6.4: commit**

---

## Task 7: Bead hygiene — propagate spec references to affected beads

**bd:** `holomush-jg9b.7`

**Goal:** Existing beads in scenes/channels/forums/discord epics get pointers to the substrate-contract spec via `bd note`. No code changes; pure bd state hygiene.

**Files:** none (bd state only).

### Step 7.1: Add dep edges

Add edges from `jg9b.4` (substrate fail-closed flip) to the downstream consumers:

```bash
bd dep add holomush-5rh.13 holomush-jg9b.4
bd dep add holomush-0sc.12 holomush-jg9b.4
```

Expected: both dep edges accepted (task-to-task per beads-project.md rules).

- [ ] **Step 7.1: add dep edges**

### Step 7.2: Add bd notes referencing the spec to affected beads

Run each (sequentially, not parallel — bd has an ID race on parallel writes per project memory; safe for notes but keep it tidy):

```bash
bd note holomush-5rh.13 "Substrate-contract spec landed at docs/superpowers/specs/2026-05-16-social-spaces-substrate-contract.md. Phase 4 brainstorm will settle crypto.emits sensitivity matrix and bind to spec §4.1. Blocked by jg9b.4 (substrate emit-type fail-closed flip)."

bd note holomush-5rh.14 "Substrate-contract spec landed. Phase 5 (focus model + multi-connection visibility) binds to spec §4.1 + §1.4. Membership-vs-focus crossover semantics decided in this phase's brainstorm."

bd note holomush-5rh.15 "Substrate-contract spec landed. Phase 6 brainstorm will (a) decide publication-artifact rename (scene_log audit table vs new publication name — see spec §6.1), (b) decide OriginLocationID / PublishVote reinstate, (c) preserve INV-S9 hard privacy boundary plugin-code-enforced (per spec §4.1)."

bd note holomush-0sc.12 "Substrate-contract spec landed. Channels rework is the N=2 validating consumer for eventkit (replay, cryptoemit) and groupkit (membership, focuswire, groupabac) per spec §4.2 + INV-S7. Brainstorm MUST produce a '## SDK primitive validation' section reporting adopt-as-is / API-tweak / reject-as-not-fit per primitive. Blocked by jg9b.4."

bd note holomush-djj "Substrate-contract spec landed. Forums uses eventkit ONLY (NOT groupkit) per spec §4.3 + INV-S10 — forum participation is incidental (posted), not intentional (member). djj.1 design brainstorm decides thread/post model + web UI + eventkit adoption shape."

bd note holomush-aqq "Substrate-contract spec landed. Discord is a bridge plugin: groupkit forbidden (INV-S10); eventkit/replay permitted conditionally if cross-history sync requires ABAC-filtered replay (per spec §4.4). aqq.1 design brainstorm decides OAuth flow + bridge model + presence sync + SDK adoption."

bd note holomush-5rh.9 "Substrate-contract spec §4.3 says forums is OUT of theme-wide SDK scope. When djj forums brainstorm fires, this bead may be reparented under djj (forum integration with scenes) or kept under 5rh (scenes-side hooks). Decision deferred until forums brainstorm."
```

- [ ] **Step 7.2: add bd notes to 7 affected beads**

### Step 7.3: Verify the notes landed

Run for each affected bead:

```bash
bd show holomush-5rh.13 | grep -A 1 "Substrate-contract"
bd show holomush-0sc.12 | grep -A 1 "N=2 validating"
bd show holomush-djj | grep -A 1 "eventkit ONLY"
```

Expected: each grep returns the new note text.

- [ ] **Step 7.3: verify notes landed**

### Step 7.4: Sync bd dolt

Run: `bd dolt push`
Expected: pushes notes to remote dolt; no errors.

- [ ] **Step 7.4: sync bd state**

### Step 7.5: No commit needed (bd state, not git)

This task touches bd state only. No git commit. The bd state is synchronized via `bd dolt push` in step 7.4.

- [ ] **Step 7.5: confirm no git commit needed**

---

## Post-implementation checklist (run after all 7 tasks complete)

- [ ] All 7 child beads (`jg9b.1`–`jg9b.7`) marked closed via `bd close`.
- [ ] Epic `jg9b` automatically reflects completion (rollup: 7/7 closed).
- [ ] `task pr-prep` runs to completion green (mandatory pre-push gate).
- [ ] Single PR (or chain of PRs per bead boundary) opened with link to spec + epic.
- [ ] `pr-review-toolkit:review-pr` runs on the PR(s).
- [ ] After merge: `bd dolt push` to sync final closed state.

## Follow-up beads (out of scope for this plan, named for future tracking)

- `plan-reviewer` memory update for INV-S7's `## SDK primitive validation` artifact-check rule (per design-reviewer round 2 non-blocking #2).
- Parity-test template establishment as part of the first SDK-extraction plan (per design-reviewer round 2 non-blocking #3).
- Binary plugin Prometheus metrics infrastructure (separate substrate-infra brainstorm; per spec §11.1 STILL OPEN).
- Future `task lint:plugin-boundary` CI predicate to mechanically enforce INV-S1 (per ADR `holomush-z1e7`).

These are NOT part of `jg9b`'s scope; file as separate beads when the time comes.
