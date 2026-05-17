# Social Spaces Substrate Contract Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `dev-flow:subagent-driven-development` (recommended) or `dev-flow:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the substrate work mandated by the substrate-contract spec ([parent](../specs/2026-05-16-social-spaces-substrate-contract.md)) using the runtime mechanism settled by the [child mechanism spec](../specs/2026-05-17-inv-s5-mechanism-design.md). Specifically: an Init-RPC-driven manifest emit-type startup validator (INV-S5) with Lua + binary parity, plus plugin adoptions, docs, roadmap update, and bead-hygiene propagation. Supersedes the earlier rejected plan at `docs/superpowers/plans/2026-05-16-social-spaces-substrate-contract-plan.md`.

**Architecture:** Per the mechanism spec's Approach X: binary plugins implement an opt-in `pluginsdk.EmitTypeRegistrar` interface; the SDK adapter auto-populates a new `registered_emit_types` field on `pluginv1.InitResponse`. Lua plugins call `holomush.register_emit_type(...)` at top-level; the Lua Host's `Load` gains a second pass that runs top-level code in a stateful state with a capture hostfunc. Both runtimes expose the registered set via a new `Host.PluginEmitRegistry(name) ([]string, bool)` interface method. The validator in `manager.go::loadPlugin` runs after `host.Load` succeeds, fails plugin load on mismatch (fail-closed from day one — no warn-only phase per `feedback_no_prod_shape_for_undeployed`).

**Tech Stack:** Go (stdlib + `samber/oops` for typed errors + `slog` for logging), `gopher-lua` for Lua hostfunc, `hashicorp/go-plugin` for binary subprocess management, protobuf 3 (proto file change requires `task proto` regen), existing plugin manager/lifecycle infrastructure at `internal/plugin/`, existing Go+Lua parity precedent at `internal/plugin/hostfunc/stdlib_focus.go` + `pkg/plugin/focus_client.go`.

**Tracking:** Design bead `holomush-jg9b` (will be promoted to epic by `plan-to-beads`). 5 child tasks (Tasks 1-5 below map 1:1 to `jg9b.2`-`jg9b.6` after materialization). The pre-existing child `jg9b.1` is the INV-S5 mechanism design bead (already READY, references this plan via downstream dep edge).

---

## File structure

This plan touches these paths. **Bold** = new files. Path:line cites for existing files where known.

### Substrate cap + adoptions (Task 2 — the big one)

**Proto:**

- Modify: `api/proto/holomush/plugin/v1/plugin.proto:99-103` (add field 2 to `InitResponse`).
- Modify: `pkg/proto/holomush/plugin/v1/plugin.pb.go` (auto-regenerated via `task proto`).

**SDK (`pkg/plugin/`):**

- **`pkg/plugin/emit_registry.go`** — `EmitRegistry` type + `EmitTypeRegistrar` opt-in interface.
- **`pkg/plugin/emit_registry_test.go`** — unit tests.
- Modify: `pkg/plugin/sdk.go:152-191` (`pluginServerAdapter.Init` populates `RegisteredEmitTypes`).

**Substrate (`internal/plugin/`):**

- **`internal/plugin/emit_type_validator.go`** — `EmitTypeMismatch`, `ValidateEmitTypeSetEquality`.
- **`internal/plugin/emit_type_validator_test.go`** — unit tests.
- Modify: `internal/plugin/host.go` (add `PluginEmitRegistry(name) ([]string, bool)` method to `Host` interface).
- Modify: `internal/plugin/manager.go::loadPlugin` (insertion point after `host.Load` at line 989).

**Lua side (`internal/plugin/lua/` + `internal/plugin/hostfunc/`):**

- **`internal/plugin/hostfunc/stdlib_emit_registry.go`** — `LuaEmitRegistry` + `RegisterEmitTypeFuncs`.
- **`internal/plugin/hostfunc/stdlib_emit_registry_test.go`** — unit tests.
- Modify: `internal/plugin/hostfunc/functions.go:134-194` (add `RegisterWithEmitCapture` entry point).
- Modify: `internal/plugin/lua/host.go:33-46` (add `emitRegistry` field to `luaPlugin` struct).
- Modify: `internal/plugin/lua/host.go:111-163` (add Load second pass + implement `PluginEmitRegistry`).

**Binary side (`internal/plugin/goplugin/`):**

- Modify: `internal/plugin/goplugin/host.go:528-535` (capture `InitResponse.RegisteredEmitTypes` from Init RPC).
- Modify: `internal/plugin/goplugin/host.go:537-542` (extend `loadedPlugin` struct + implement `PluginEmitRegistry`).

**Plugin adoptions:**

- Modify: `plugins/core-communication/main.lua` (8 top-level `holomush.register_emit_type` calls).
- Modify: `plugins/core-objects/main.lua` (5 top-level `holomush.register_emit_type` calls).

**Parity test:**

- **`internal/plugin/manager_parity_test.go`** — Lua + binary parity per mechanism spec INV-M7.

### Docs (Task 3)

- **`site/docs/extending/substrate-contract.md`** — contributor orientation page.
- Possibly modify: `site/docs/extending/` index/nav (discover during task).

### Roadmap (Task 4)

- Modify: `docs/roadmap.md` (theme:social-spaces section update).

### Bead hygiene (Task 5)

- No file changes; modifies bd state via `bd update`/`bd note`/`bd dep add`.

---

## Task 1: Audit precondition — enumerate in-tree plugins with `crypto.emits`

**bd:** `holomush-jg9b.2` (audit, executed FIRST before substrate impl per execution order — though spec §6 table listed substrate first, audit is the logical precondition)

**Goal:** Verify the complete in-tree plugin set that declares non-empty `crypto.emits`. Confirm the mechanism spec's §4.1 inventory is still accurate. Document the audit result on the bead as proof-of-precondition. No code changes.

**Files:** none (bd state + audit notes only).

### Step 1.1: Enumerate all plugin manifests declaring crypto

Run: `rg -l '^crypto:' plugins/`
Expected output:

```text
plugins/core-communication/plugin.yaml
plugins/core-objects/plugin.yaml
plugins/core-scenes/plugin.yaml
```

If the output differs from the expected 3 paths, STOP and update the mechanism spec's §4.1 inventory before proceeding to Task 2.

- [ ] **Step 1.1: run the rg command and verify output matches**

### Step 1.2: For each manifest, classify crypto.emits population

For each path from Step 1.1, read the `crypto:` block and classify:

```bash
for p in plugins/core-communication/plugin.yaml plugins/core-objects/plugin.yaml plugins/core-scenes/plugin.yaml; do
  echo "=== $p ==="
  rg -A 30 '^crypto:' "$p" | rg -c 'event_type:'
done
```

Expected:

- `plugins/core-communication/plugin.yaml` → 8 event types (`say`, `pose`, `ooc`, `emit`, `page`, `whisper`, `pemit`, `whisper_notice`)
- `plugins/core-objects/plugin.yaml` → 5 event types (`object_create`, `object_destroy`, `object_use`, `object_examine`, `object_give`)
- `plugins/core-scenes/plugin.yaml` → 0 event types (empty `emits: []`)

If counts differ from expected, STOP and reconcile with mechanism spec §4.1.

- [ ] **Step 1.2: confirm counts match expected**

### Step 1.3: Verify the runtime type of each plugin

For each plugin, confirm its runtime type matches the spec:

```bash
for p in plugins/core-communication/plugin.yaml plugins/core-objects/plugin.yaml plugins/core-scenes/plugin.yaml; do
  echo "=== $p ==="
  rg '^type:' "$p"
done
```

Expected:

- `plugins/core-communication/plugin.yaml` → `type: lua`
- `plugins/core-objects/plugin.yaml` → `type: lua`
- `plugins/core-scenes/plugin.yaml` → `type: binary`

- [ ] **Step 1.3: confirm runtimes match**

### Step 1.4: Verify the top-level idempotency precondition for Lua plugins with crypto.emits

For each Lua plugin with non-empty crypto.emits (`core-communication`, `core-objects`), read the `main.lua` top-level (everything outside function bodies) and verify it contains ONLY:

- `local function` declarations
- `local <var> = ...` declarations (constants)
- Comments
- Top-level handler assignments (e.g., `on_event = function(...) ... end`)

It MUST NOT contain top-level calls to non-idempotent hostfuncs (e.g., `holomush.kv_set(...)`, `holomush.create_location(...)`).

Use Read tool to inspect each main.lua, then `bd note` the verification result.

If a plugin's top-level contains non-idempotent calls, STOP and either (a) refactor the plugin to move those calls into `on_event`/`on_command`, or (b) flag this as a blocker for the mechanism rollout.

- [ ] **Step 1.4: read main.lua for both Lua plugins; verify idempotent top-level**

### Step 1.5: Record audit result on the bead

```bash
bd note holomush-jg9b.2 "Audit complete: 3 plugins declare crypto:, 2 with non-empty emits (core-communication=8 Lua, core-objects=5 Lua). core-scenes empty (gated out by INV-M1). Top-level idempotency verified for both Lua plugins (only local function declarations and constants). Ready to proceed with Task 2 substrate + adoptions."
```

- [ ] **Step 1.5: bd note the audit result**

### Step 1.6: Close the audit bead

```bash
bd close holomush-jg9b.2 --reason="Audit precondition verified: in-tree plugin scope matches mechanism spec §4.1; Lua top-level idempotency confirmed."
```

- [ ] **Step 1.6: close the audit bead**

---

## Task 2: Substrate cap + plugin adoptions (single coherent change)

**bd:** `holomush-jg9b.3` (substrate cap + adoptions — atomic per `feedback_no_prod_shape_for_undeployed`)

**Goal:** Land the full INV-S5 mechanism per the mechanism design spec, with both plugin adoptions, in one coherent change. Fail-closed from day one — no warn-only intermediate state.

This task has many bite-sized TDD steps grouped by commit boundary. Each commit boundary is named **Commit: <description>**; the implementer commits between groups.

**Files:** see "File structure → Substrate cap + adoptions" above.

### Group A: SDK API + validator (no consumers, no runtime impact yet)

#### Step 2.A.1: Write failing test for `EmitRegistry`

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
	require.Equal(t, []string{"scene_ic", "scene_join", "scene_leave", "scene_ooc"}, got)
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

- [ ] **Step 2.A.1: write the test file**

#### Step 2.A.2: Run failing test

Run: `task test -- ./pkg/plugin/ -run TestEmitRegistry`
Expected: FAIL with "undefined: NewEmitRegistry".

- [ ] **Step 2.A.2: verify failure**

#### Step 2.A.3: Implement `EmitRegistry`

Create `pkg/plugin/emit_registry.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"sort"
	"sync"
)

// EmitRegistry accumulates the set of event types a binary plugin can
// emit. Plugins register types during construction (typically in main()
// before pluginsdk.ServeWithServices) or in their Init method. The host
// reads the registered set via InitResponse.registered_emit_types and
// validates against manifest's crypto.emits per INV-S5.
type EmitRegistry struct {
	mu    sync.Mutex
	types map[string]struct{}
}

// NewEmitRegistry returns an empty registry.
func NewEmitRegistry() *EmitRegistry {
	return &EmitRegistry{types: make(map[string]struct{})}
}

// RegisterEmitType records a single event type the plugin can emit.
// Duplicate registrations are idempotent.
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
// sorted slice. Order is deterministic for test stability and for
// reproducible InitResponse population.
func (r *EmitRegistry) RegisteredEmitTypes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.types))
	for t := range r.types {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// EmitTypeRegistrar is the optional interface binary plugins implement
// to expose their EmitRegistry to the host via InitResponse.
//
// Plugins with non-empty crypto.emits in their manifest MUST implement
// this interface; the substrate validator fails load on mismatch.
// Plugins without crypto.emits are out of INV-S5 scope (per INV-M1) and
// may skip.
type EmitTypeRegistrar interface {
	EmitRegistry() *EmitRegistry
}
```

- [ ] **Step 2.A.3: write the implementation**

#### Step 2.A.4: Run tests to verify pass

Run: `task test -- ./pkg/plugin/ -run TestEmitRegistry`
Expected: PASS — 3 tests.

- [ ] **Step 2.A.4: verify pass**

#### Step 2.A.5: Write failing validator test

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
	mismatch := ValidateEmitTypeSetEquality(declared, registered)
	require.False(t, mismatch.HasMismatch())
}

func TestValidateEmitTypeSetEquality_DeclaredButUnregistered(t *testing.T) {
	declared := []string{"scene_ic", "scene_ooc", "scene_bogus"}
	registered := []string{"scene_ic", "scene_ooc"}
	mismatch := ValidateEmitTypeSetEquality(declared, registered)
	require.True(t, mismatch.HasMismatch())
	require.Equal(t, []string{"scene_bogus"}, mismatch.DeclaredButUnregistered)
	require.Empty(t, mismatch.RegisteredButUndeclared)
}

func TestValidateEmitTypeSetEquality_RegisteredButUndeclared(t *testing.T) {
	declared := []string{"scene_ic"}
	registered := []string{"scene_ic", "scene_typo"}
	mismatch := ValidateEmitTypeSetEquality(declared, registered)
	require.True(t, mismatch.HasMismatch())
	require.Equal(t, []string{"scene_typo"}, mismatch.RegisteredButUndeclared)
	require.Empty(t, mismatch.DeclaredButUnregistered)
}

func TestValidateEmitTypeSetEquality_BothDirections(t *testing.T) {
	declared := []string{"a", "b"}
	registered := []string{"b", "c"}
	mismatch := ValidateEmitTypeSetEquality(declared, registered)
	require.True(t, mismatch.HasMismatch())
	require.Equal(t, []string{"a"}, mismatch.DeclaredButUnregistered)
	require.Equal(t, []string{"c"}, mismatch.RegisteredButUndeclared)
}

func TestValidateEmitTypeSetEquality_BothEmpty(t *testing.T) {
	mismatch := ValidateEmitTypeSetEquality(nil, nil)
	require.False(t, mismatch.HasMismatch())
}
```

- [ ] **Step 2.A.5: write validator tests**

#### Step 2.A.6: Run failing validator tests

Run: `task test -- ./internal/plugin/ -run TestValidateEmitTypeSetEquality`
Expected: FAIL with "undefined: ValidateEmitTypeSetEquality".

- [ ] **Step 2.A.6: verify failure**

#### Step 2.A.7: Implement validator

Create `internal/plugin/emit_type_validator.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import "sort"

// EmitTypeMismatch describes the diff between a plugin's manifest-declared
// crypto.emits set and the SDK-registered emit-type set per INV-S5.
type EmitTypeMismatch struct {
	DeclaredButUnregistered []string
	RegisteredButUndeclared []string
}

// HasMismatch reports whether either diff direction has any entries.
func (m EmitTypeMismatch) HasMismatch() bool {
	return len(m.DeclaredButUnregistered) > 0 || len(m.RegisteredButUndeclared) > 0
}

// ValidateEmitTypeSetEquality compares the manifest-declared emit-type
// set against the SDK-registered emit-type set. Per INV-S5, the two
// sets MUST be equal in both directions.
func ValidateEmitTypeSetEquality(declared, registered []string) EmitTypeMismatch {
	declSet := toEmitSet(declared)
	regSet := toEmitSet(registered)

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
	return mismatch
}

func toEmitSet(s []string) map[string]struct{} {
	out := make(map[string]struct{}, len(s))
	for _, v := range s {
		out[v] = struct{}{}
	}
	return out
}
```

- [ ] **Step 2.A.7: write implementation**

#### Step 2.A.8: Run validator tests to verify pass

Run: `task test -- ./internal/plugin/ -run TestValidateEmitTypeSetEquality`
Expected: PASS — 5 tests.

- [ ] **Step 2.A.8: verify pass**

#### Step 2.A.9: Run task lint to confirm no new issues

Run: `task lint`
Expected: PASS or only pre-existing warnings unrelated to the new files.

- [ ] **Step 2.A.9: verify lint clean**

#### Step 2.A.10: Commit Group A

Commit message:

```text
feat(plugin): EmitRegistry SDK + emit-type validator for INV-S5 (jg9b.3)

Group A of jg9b.3 (substrate cap + adoptions). Pure foundation — no
consumers yet; tested in isolation. Subsequent groups wire this into
the plugin lifecycle.

Adds:
- pkg/plugin/emit_registry.go: EmitRegistry + EmitTypeRegistrar interface
- internal/plugin/emit_type_validator.go: ValidateEmitTypeSetEquality
- Unit tests for both
```

- [ ] **Step 2.A.10: commit**

---

### Group B: Lua hostfunc + Functions.RegisterWithEmitCapture entry point

#### Step 2.B.1: Write failing test for Lua hostfunc

Create `internal/plugin/hostfunc/stdlib_emit_registry_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

func TestRegisterEmitTypeFuncs_Accumulates(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	mod := ls.NewTable()
	reg := NewLuaEmitRegistry()
	RegisterEmitTypeFuncs(ls, mod, reg)
	ls.SetGlobal("holomush", mod)

	err := ls.DoString(`holomush.register_emit_type("say"); holomush.register_emit_type("pose")`)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"say", "pose"}, reg.Types())
}

func TestRegisterEmitTypeFuncs_DuplicateIdempotent(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	mod := ls.NewTable()
	reg := NewLuaEmitRegistry()
	RegisterEmitTypeFuncs(ls, mod, reg)
	ls.SetGlobal("holomush", mod)

	err := ls.DoString(`holomush.register_emit_type("say"); holomush.register_emit_type("say")`)
	require.NoError(t, err)
	require.Equal(t, []string{"say"}, reg.Types())
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

- [ ] **Step 2.B.1: write test**

#### Step 2.B.2: Run failing test

Run: `task test -- ./internal/plugin/hostfunc/ -run TestRegisterEmitTypeFuncs`
Expected: FAIL with "undefined: NewLuaEmitRegistry".

- [ ] **Step 2.B.2: verify failure**

#### Step 2.B.3: Implement Lua hostfunc

Create `internal/plugin/hostfunc/stdlib_emit_registry.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"sort"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

// LuaEmitRegistry accumulates registrations from holomush.register_emit_type
// calls during a Lua plugin's INV-S5 Load-pass. One instance per plugin.
type LuaEmitRegistry struct {
	mu    sync.Mutex
	types map[string]struct{}
}

// NewLuaEmitRegistry returns an empty registry.
func NewLuaEmitRegistry() *LuaEmitRegistry {
	return &LuaEmitRegistry{types: make(map[string]struct{})}
}

func (r *LuaEmitRegistry) add(t string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.types[t] = struct{}{}
}

// Types returns the registered event types as a sorted slice.
func (r *LuaEmitRegistry) Types() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.types))
	for t := range r.types {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// RegisterEmitTypeFuncs installs holomush.register_emit_type(type) on
// the given module table; calls append to reg.
//
// Usage: only called via Functions.RegisterWithEmitCapture during the
// Lua Host's INV-S5 Load-pass. The standard per-delivery
// Functions.Register path does NOT install register_emit_type. A
// per-delivery call holomush.register_emit_type("x") will dispatch to
// nil and raise Lua's "attempt to call a nil value" error, failing
// the handler. This is correct end-state behavior (registrations are
// Load-time-only) by absence-by-default.
func RegisterEmitTypeFuncs(ls *lua.LState, mod *lua.LTable, reg *LuaEmitRegistry) {
	ls.SetField(mod, "register_emit_type", ls.NewFunction(func(ls *lua.LState) int {
		eventType := ls.CheckString(1)
		reg.add(eventType)
		ls.Push(lua.LTrue)
		return 1
	}))
}
```

- [ ] **Step 2.B.3: write implementation**

#### Step 2.B.4: Run hostfunc tests to verify pass

Run: `task test -- ./internal/plugin/hostfunc/ -run TestRegisterEmitTypeFuncs`
Expected: PASS — 3 tests.

- [ ] **Step 2.B.4: verify pass**

#### Step 2.B.5: Write failing test for Functions.RegisterWithEmitCapture

Add to `internal/plugin/hostfunc/functions_test.go` (existing file — examine it first to follow harness patterns):

```bash
ls internal/plugin/hostfunc/functions_test.go internal/plugin/hostfunc/functions_internal_test.go
```

Either file works (use the existing internal-test if you need package-private access). Add:

```go
func TestFunctions_RegisterWithEmitCapture_InstallsHostfunc(t *testing.T) {
	f := &Functions{} // zero-valued is OK for this test — only needs the holomush table assembly path
	ls := lua.NewState()
	defer ls.Close()

	reg := NewLuaEmitRegistry()
	f.RegisterWithEmitCapture(ls, "test-plugin", reg)

	err := ls.DoString(`holomush.register_emit_type("x")`)
	require.NoError(t, err)
	require.Equal(t, []string{"x"}, reg.Types())
}
```

- [ ] **Step 2.B.5: write test (in functions_internal_test.go if package-private access needed)**

#### Step 2.B.6: Run failing test

Run: `task test -- ./internal/plugin/hostfunc/ -run TestFunctions_RegisterWithEmitCapture`
Expected: FAIL with "undefined: RegisterWithEmitCapture".

- [ ] **Step 2.B.6: verify failure**

#### Step 2.B.7: Implement Functions.RegisterWithEmitCapture

Modify `internal/plugin/hostfunc/functions.go`. Add new method below the existing `Register` (which ends around line 194):

```go
// RegisterWithEmitCapture is the variant of Register used during the
// Lua Host's INV-S5 Load-pass. Identical to Register, but ALSO
// installs holomush.register_emit_type which appends to reg. The
// standard Register path does NOT install register_emit_type —
// see RegisterEmitTypeFuncs godoc.
func (f *Functions) RegisterWithEmitCapture(
	ls *lua.LState,
	pluginName string,
	reg *LuaEmitRegistry,
	requires ...string,
) {
	f.Register(ls, pluginName, requires...)
	if mod, ok := ls.GetGlobal("holomush").(*lua.LTable); ok {
		RegisterEmitTypeFuncs(ls, mod, reg)
	}
}
```

- [ ] **Step 2.B.7: add the method**

#### Step 2.B.8: Run test to verify pass

Run: `task test -- ./internal/plugin/hostfunc/ -run TestFunctions_RegisterWithEmitCapture`
Expected: PASS.

- [ ] **Step 2.B.8: verify pass**

#### Step 2.B.9: Run task lint

Run: `task lint`
Expected: PASS.

- [ ] **Step 2.B.9: verify lint clean**

#### Step 2.B.10: Commit Group B

Commit message:

```text
feat(plugin/hostfunc): Lua register_emit_type hostfunc + capture entry point (jg9b.3)

Group B of jg9b.3. Adds the Lua side of the INV-S5 mechanism:
- LuaEmitRegistry + RegisterEmitTypeFuncs (stdlib_emit_registry.go)
- Functions.RegisterWithEmitCapture entry point (variant of Register
  that also installs register_emit_type)

No host-side wiring yet (Group F connects the Lua Host's Load to this).
```

- [ ] **Step 2.B.10: commit**

---

### Group C: Proto change + binary SDK adapter

#### Step 2.C.1: Modify the proto

Edit `api/proto/holomush/plugin/v1/plugin.proto` lines 99-103. Replace:

```proto
// InitResponse is returned by the plugin after initialization.
message InitResponse {
  // gRPC service names this plugin provides on the go-plugin transport.
  repeated string provided_services = 1;
}
```

With:

```proto
// InitResponse is returned by the plugin after initialization.
message InitResponse {
  // gRPC service names this plugin provides on the go-plugin transport.
  repeated string provided_services = 1;

  // Set of plugin-owned event types this plugin may emit. Host validates
  // set-equality against manifest's crypto.emits per INV-S5.
  // Plugins without crypto.emits leave this empty and skip validation.
  // Plugins WITH crypto.emits MUST populate this set; mismatch fails load.
  repeated string registered_emit_types = 2;
}
```

- [ ] **Step 2.C.1: edit proto**

#### Step 2.C.2: Regenerate proto bindings

Run: `task proto`
Expected: regenerates `pkg/proto/holomush/plugin/v1/plugin.pb.go` with the new field. Verify the regenerated file has `RegisteredEmitTypes` field on `InitResponse`.

```bash
rg "RegisteredEmitTypes" pkg/proto/holomush/plugin/v1/plugin.pb.go
```

Expected: 2+ hits (field declaration + getter method).

- [ ] **Step 2.C.2: regenerate and verify**

#### Step 2.C.3: Write failing test for SDK adapter

Add to `pkg/plugin/sdk_test.go` (existing file — examine harness):

```go
func TestPluginServerAdapter_Init_PopulatesRegisteredEmitTypes(t *testing.T) {
	// A test ServiceProvider that ALSO implements EmitTypeRegistrar.
	provider := &testProviderWithEmitRegistry{
		registry: NewEmitRegistry(),
	}
	provider.registry.RegisterEmitTypes([]string{"foo", "bar"})

	adapter := &pluginServerAdapter{serviceProvider: provider}
	resp, err := adapter.Init(context.Background(), &pluginv1.InitRequest{})
	require.NoError(t, err)
	require.Equal(t, []string{"bar", "foo"}, resp.GetRegisteredEmitTypes())
}

func TestPluginServerAdapter_Init_NoEmitRegistry_EmptyResponse(t *testing.T) {
	// A ServiceProvider that does NOT implement EmitTypeRegistrar.
	provider := &testProviderPlain{}
	adapter := &pluginServerAdapter{serviceProvider: provider}
	resp, err := adapter.Init(context.Background(), &pluginv1.InitRequest{})
	require.NoError(t, err)
	require.Empty(t, resp.GetRegisteredEmitTypes())
}

type testProviderWithEmitRegistry struct {
	registry *EmitRegistry
}

func (p *testProviderWithEmitRegistry) EmitRegistry() *EmitRegistry { return p.registry }

type testProviderPlain struct{}
```

If the test file has existing stubs, harmonize with them. The two test types may need additional method implementations to satisfy existing interfaces (read the existing test helpers first).

- [ ] **Step 2.C.3: write SDK adapter test**

#### Step 2.C.4: Run failing test

Run: `task test -- ./pkg/plugin/ -run TestPluginServerAdapter_Init_Populates`
Expected: FAIL — the adapter does not yet populate `RegisteredEmitTypes`.

- [ ] **Step 2.C.4: verify failure**

#### Step 2.C.5: Modify SDK adapter Init

Modify `pkg/plugin/sdk.go` around line 152 (the existing `pluginServerAdapter.Init` method). At the end of the method (just before the existing `return` of `&pluginv1.InitResponse{}`), insert population of `RegisteredEmitTypes`:

```go
// At the end of Init, after delegating to the provider's optional Init:
resp := &pluginv1.InitResponse{}
if registrar, ok := a.serviceProvider.(EmitTypeRegistrar); ok {
	resp.RegisteredEmitTypes = registrar.EmitRegistry().RegisteredEmitTypes()
}
return resp, nil
```

The existing `provided_services` field (proto field 1) is not currently populated by the adapter and is orthogonal to this change.

- [ ] **Step 2.C.5: modify adapter Init**

#### Step 2.C.6: Run test to verify pass

Run: `task test -- ./pkg/plugin/ -run TestPluginServerAdapter_Init_Populates`
Expected: PASS — both sub-tests.

- [ ] **Step 2.C.6: verify pass**

#### Step 2.C.7: Run task lint

Run: `task lint`
Expected: PASS.

- [ ] **Step 2.C.7: verify lint clean**

#### Step 2.C.8: Commit Group C

Commit message:

```text
feat(plugin): InitResponse.registered_emit_types proto field + SDK adapter (jg9b.3)

Group C of jg9b.3. Adds the binary side of INV-S5:
- api/proto/.../plugin.proto: registered_emit_types field 2 on InitResponse
- Regenerated plugin.pb.go
- pkg/plugin/sdk.go: pluginServerAdapter.Init auto-populates the field
  when the plugin implements EmitTypeRegistrar

Lua side already shipped in Group B. Host interface extension comes
in Group D.
```

- [ ] **Step 2.C.8: commit**

---

### Group D: Host interface extension + implementations

#### Step 2.D.1: Write failing test for Lua Host PluginEmitRegistry

Add to `internal/plugin/lua/host_test.go` (existing file — examine first):

```go
func TestLuaHost_PluginEmitRegistry_LoadedPluginWithCryptoEmits(t *testing.T) {
	// Load a synthetic Lua plugin with manifest declaring crypto.emits: [a, b]
	// and code calling register_emit_type for "a" and "b". After Load, verify
	// PluginEmitRegistry returns ([a, b], true).
	//
	// Use the existing test harness for constructing a stub plugin manifest
	// and registry; follow the pattern in TestHost_Load_* tests in this file.
}

func TestLuaHost_PluginEmitRegistry_NotLoaded_ReturnsFalse(t *testing.T) {
	h := NewHost(...)  // use existing test constructor
	got, ok := h.PluginEmitRegistry("nonexistent")
	require.False(t, ok)
	require.Nil(t, got)
}

func TestLuaHost_PluginEmitRegistry_LoadedWithoutCryptoEmits_ReturnsNilTrue(t *testing.T) {
	// Load a synthetic Lua plugin with manifest crypto: nil (or emits: []).
	// Verify PluginEmitRegistry returns (nil, true) — loaded but INV-S5 not applicable.
}
```

Implementer fills in the harness setup based on existing test patterns.

- [ ] **Step 2.D.1: write Lua host PluginEmitRegistry tests**

#### Step 2.D.2: Run failing tests

Run: `task test -- ./internal/plugin/lua/ -run TestLuaHost_PluginEmitRegistry`
Expected: FAIL — method doesn't exist yet.

- [ ] **Step 2.D.2: verify failure**

#### Step 2.D.3: Extend Host interface

Modify `internal/plugin/host.go`. Add to the `Host` interface:

```go
// PluginEmitRegistry returns the code-registered emit-type set for a
// loaded plugin, captured during Load. Returns:
//   - (set, true)  : plugin loaded and opted into INV-S5 (non-empty crypto.emits)
//   - (nil, true)  : plugin loaded; INV-S5 not applicable (empty crypto.emits)
//   - (nil, false) : plugin not loaded under this Host
//
// Substrate uses the (set, true) case to run set-equality validation
// against manifest.Crypto.Emits in manager.go::loadPlugin.
PluginEmitRegistry(name string) ([]string, bool)
```

- [ ] **Step 2.D.3: add interface method**

#### Step 2.D.4: Implement on Lua Host

Modify `internal/plugin/lua/host.go`. Add `emitRegistry` field to `luaPlugin` struct (around line 33):

```go
type luaPlugin struct {
	manifest     *plugins.Manifest
	code         string
	emitRegistry []string // INV-S5: populated during Load second pass; nil when crypto.emits empty
}
```

Add the method:

```go
// PluginEmitRegistry implements plugins.Host.
func (h *Host) PluginEmitRegistry(name string) ([]string, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	p, ok := h.plugins[name]
	if !ok {
		return nil, false
	}
	return p.emitRegistry, true
}
```

(Note: the Lua Host's Load is still using the old shape that doesn't populate `emitRegistry`. The Load second-pass implementation comes in Group E. For now, `emitRegistry` defaults to nil for all plugins.)

- [ ] **Step 2.D.4: implement on Lua host**

#### Step 2.D.5: Run Lua tests (some pass, some fail — Load not yet updated)

Run: `task test -- ./internal/plugin/lua/ -run TestLuaHost_PluginEmitRegistry`
Expected: 2 of 3 tests pass (the not-loaded and empty-crypto-emits cases). The "loaded with crypto.emits" test still fails because Load doesn't populate emitRegistry yet.

- [ ] **Step 2.D.5: verify partial pass (2/3)**

#### Step 2.D.6: Write failing test for binary Host PluginEmitRegistry

Add to `internal/plugin/goplugin/host_test.go` (existing file — examine first):

```go
func TestGoPluginHost_PluginEmitRegistry_LoadedPluginWithCryptoEmits(t *testing.T) {
	// Use the existing test fixture pattern (likely involves a real subprocess
	// or a test-only Host fixture). Plugin with crypto.emits and EmitTypeRegistrar
	// returning [a, b]. After Load + Init, PluginEmitRegistry returns ([a, b], true).
}

func TestGoPluginHost_PluginEmitRegistry_NotLoaded_ReturnsFalse(t *testing.T) {
	h := &Host{plugins: map[string]*loadedPlugin{}}
	got, ok := h.PluginEmitRegistry("nonexistent")
	require.False(t, ok)
	require.Nil(t, got)
}
```

- [ ] **Step 2.D.6: write binary host tests**

#### Step 2.D.7: Run failing tests

Run: `task test -- ./internal/plugin/goplugin/ -run TestGoPluginHost_PluginEmitRegistry`
Expected: FAIL.

- [ ] **Step 2.D.7: verify failure**

#### Step 2.D.8: Implement on binary Host

Modify `internal/plugin/goplugin/host.go`. Add `registeredEmitTypes` field to `loadedPlugin` struct (around line 537):

```go
type loadedPlugin struct {
	manifest             *plugins.Manifest
	client               *hashiplug.Client
	registeredEmitTypes  []string  // INV-S5: populated from InitResponse.RegisteredEmitTypes
	// ... existing fields ...
}
```

Modify the existing `pluginClient.Init(ctx, initReq)` call (line 528 area) to CAPTURE the response:

```go
initResp, initErr := pluginClient.Init(ctx, initReq)
if initErr != nil {
	client.Kill()
	if certDir != "" {
		_ = os.RemoveAll(certDir)
	}
	return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "init").Wrap(initErr)
}
// Cache registered emit types for INV-S5 validation by the manager.
var registeredEmitTypes []string
if initResp != nil {
	registeredEmitTypes = initResp.GetRegisteredEmitTypes()
}
```

Then in the `h.plugins[manifest.Name] = &loadedPlugin{...}` block (around line 537), include `registeredEmitTypes: registeredEmitTypes`.

For plugins that SKIP the Init RPC (`len(manifest.Requires) == 0 && len(manifest.Provides) == 0 && manifest.Storage != plugins.StoragePostgres`), `registeredEmitTypes` stays nil — that's the correct behavior because those plugins don't have crypto.emits either (INV-M1 gate skips them).

Add the method:

```go
// PluginEmitRegistry implements plugins.Host.
func (h *Host) PluginEmitRegistry(name string) ([]string, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	p, ok := h.plugins[name]
	if !ok {
		return nil, false
	}
	return p.registeredEmitTypes, true
}
```

- [ ] **Step 2.D.8: implement on binary host**

#### Step 2.D.9: Run binary tests

Run: `task test -- ./internal/plugin/goplugin/ -run TestGoPluginHost_PluginEmitRegistry`
Expected: PASS (at least the not-loaded case; the loaded case may require integration-test plumbing).

- [ ] **Step 2.D.9: verify pass**

#### Step 2.D.10: Run task lint

Run: `task lint`
Expected: PASS.

- [ ] **Step 2.D.10: verify lint clean**

#### Step 2.D.11: Commit Group D

Commit message:

```text
feat(plugin): PluginEmitRegistry method on Host interface + impls (jg9b.3)

Group D of jg9b.3. Adds the cross-runtime accessor:
- internal/plugin/host.go: PluginEmitRegistry interface method
- internal/plugin/lua/host.go: Lua impl (returns luaPlugin.emitRegistry)
- internal/plugin/goplugin/host.go: binary impl (returns
  loadedPlugin.registeredEmitTypes captured from InitResponse)

Lua Host's emitRegistry remains nil for all plugins until Group E
adds the Load second pass that populates it. Validator wiring in
manager.go comes in Group F.
```

- [ ] **Step 2.D.11: commit**

---

### Group E: Lua Host Load second pass

#### Step 2.E.1: Update Lua Host integration test to exercise crypto.emits plugin

Re-run the test from Step 2.D.1 (`TestLuaHost_PluginEmitRegistry_LoadedPluginWithCryptoEmits`). It should still fail because Load doesn't yet populate `emitRegistry`. Confirm the test exists and the harness compiles.

- [ ] **Step 2.E.1: confirm failing test still in place**

#### Step 2.E.2: Modify Lua Host Load to add second pass

Modify `internal/plugin/lua/host.go::Load` (function starts at line 111). After the existing syntax-check throwaway state ends (after line 155) and before storing the `luaPlugin` (line 157), insert:

```go
// INV-S5 Load-pass: capture top-level register_emit_type calls into
// a per-plugin LuaEmitRegistry. Only fires for plugins with non-empty
// crypto.emits per INV-M1. See docs/superpowers/specs/2026-05-17-inv-s5-mechanism-design.md.
var emitRegistry []string
if manifest.Crypto != nil && len(manifest.Crypto.Emits) > 0 {
	L2, err := h.factory.NewState(ctx)
	if err != nil {
		return oops.In("lua").With("plugin", manifest.Name).
			With("operation", "load_inv_s5_pass").
			Hint("failed to create INV-S5 capture state").Wrap(err)
	}
	defer L2.Close()

	reg := hostfunc.NewLuaEmitRegistry()
	if h.hostFuncs != nil {
		h.hostFuncs.RegisterWithEmitCapture(L2, manifest.Name, reg, manifest.Requires...)
	}

	if err := L2.DoString(string(code)); err != nil {
		return oops.In("lua").With("plugin", manifest.Name).
			With("operation", "load_inv_s5_pass").
			With("entry", manifest.LuaPlugin.Entry).
			Hint("INV-S5 Load-pass execution error").Wrap(err)
	}

	emitRegistry = reg.Types()
}
```

Then modify the existing `h.plugins[manifest.Name] = &luaPlugin{...}` to include the new field:

```go
h.plugins[manifest.Name] = &luaPlugin{
	manifest:     manifest,
	code:         string(code),
	emitRegistry: emitRegistry,
}
```

Add the `hostfunc` import to the file if not already present.

- [ ] **Step 2.E.2: modify Load**

#### Step 2.E.3: Run Lua tests to verify all PluginEmitRegistry tests pass

Run: `task test -- ./internal/plugin/lua/ -run TestLuaHost_PluginEmitRegistry`
Expected: PASS — all 3 tests.

- [ ] **Step 2.E.3: verify pass**

#### Step 2.E.4: Write additional Lua Host test for Load-pass error path

Add:

```go
func TestLuaHost_Load_INVS5PassExecutionError_Fails(t *testing.T) {
	// Synthetic plugin with manifest crypto.emits: [a] but main.lua top-level
	// that throws a Lua error (e.g., `error("intentional")` at top level).
	// Verify Load returns error with operation="load_inv_s5_pass".
}
```

Then run and verify FAIL→implementation→PASS.

- [ ] **Step 2.E.4: write Load-pass error test, verify it passes (covered by existing error path in implementation)**

#### Step 2.E.5: Run task lint

Run: `task lint`
Expected: PASS.

- [ ] **Step 2.E.5: verify lint clean**

#### Step 2.E.6: Commit Group E

Commit message:

```text
feat(plugin/lua): Load second pass captures emit-type registrations (jg9b.3)

Group E of jg9b.3. Lua Host's Load now does a SECOND stateful pass
for plugins with non-empty crypto.emits, running top-level code in a
state with holomush.register_emit_type registered to capture into a
per-plugin LuaEmitRegistry. The captured types feed PluginEmitRegistry
for the validator (Group F).

DoString errors in the Load-pass fail plugin load (same shape as
syntax-check errors).
```

- [ ] **Step 2.E.6: commit**

---

### Group F: Manager wiring (validator call + fail-closed)

#### Step 2.F.1: Write failing test for manager validator wiring

Add to `internal/plugin/manager_test.go`:

```go
func TestManager_LoadPlugin_EmitTypeMismatch_FailsClosed(t *testing.T) {
	// Synthetic Lua plugin with manifest crypto.emits: [a, b] but code that
	// only calls register_emit_type("a"). loadPlugin returns
	// EVENT_TYPE_REGISTRY_MISMATCH oops error naming "b" in
	// declared_but_unregistered.
}

func TestManager_LoadPlugin_EmitTypeMatch_Succeeds(t *testing.T) {
	// Synthetic plugin with matching declared + registered sets. loadPlugin
	// succeeds; manager.plugins contains the plugin.
}

func TestManager_LoadPlugin_NoCryptoEmits_SkipsValidation(t *testing.T) {
	// Synthetic plugin with no crypto block. loadPlugin succeeds and never
	// calls host.PluginEmitRegistry (verified via spy host or absence-of-error
	// on a host whose PluginEmitRegistry would panic).
}
```

Use the existing test harness in `manager_test.go` for synthesizing plugins. Follow existing patterns.

- [ ] **Step 2.F.1: write 3 manager tests**

#### Step 2.F.2: Run failing tests

Run: `task test -- ./internal/plugin/ -run TestManager_LoadPlugin_EmitType`
Expected: at least one FAIL (validator not yet wired).

- [ ] **Step 2.F.2: verify failure**

#### Step 2.F.3: Wire validator into loadPlugin

Modify `internal/plugin/manager.go::loadPlugin`. After the existing `host.Load(...)` call at line 989 returns successfully, insert (before the plugin is added to the manager's plugin cache):

```go
// INV-S5: manifest emit-type startup validation. Scope per INV-M1:
// only plugins with non-empty crypto.emits participate.
if dp.Manifest.Crypto != nil && len(dp.Manifest.Crypto.Emits) > 0 {
	registered, ok := host.PluginEmitRegistry(dp.Manifest.Name)
	if !ok {
		return oops.Code("PLUGIN_EMIT_REGISTRY_UNAVAILABLE").
			In("manager").With("plugin", dp.Manifest.Name).
			Errorf("host loaded plugin but PluginEmitRegistry returned not-found")
	}
	declared := manifestDeclaredEmitTypes(dp.Manifest)
	mismatch := ValidateEmitTypeSetEquality(declared, registered)
	if mismatch.HasMismatch() {
		return oops.Code("EVENT_TYPE_REGISTRY_MISMATCH").
			In("manager").With("plugin", dp.Manifest.Name).
			With("declared_but_unregistered", mismatch.DeclaredButUnregistered).
			With("registered_but_undeclared", mismatch.RegisteredButUndeclared).
			Errorf("plugin crypto.emits manifest does not match registered emit-type set (INV-S5)")
	}
}
```

Add the helper function in the same file:

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
```

- [ ] **Step 2.F.3: wire validator**

#### Step 2.F.4: Run all 3 manager tests to verify pass

Run: `task test -- ./internal/plugin/ -run TestManager_LoadPlugin_EmitType`
Expected: PASS — all 3.

- [ ] **Step 2.F.4: verify pass**

#### Step 2.F.5: Run full plugin test suite to check for regressions

Run: `task test -- ./internal/plugin/ ./pkg/plugin/ ./internal/plugin/hostfunc/ ./internal/plugin/lua/ ./internal/plugin/goplugin/`
Expected: PASS. Any regression here is likely a test fixture missing crypto.emits coverage — investigate.

- [ ] **Step 2.F.5: verify no regressions**

#### Step 2.F.6: Run task lint

Run: `task lint`
Expected: PASS.

- [ ] **Step 2.F.6: verify lint clean**

#### Step 2.F.7: Commit Group F (validator is now active in fail-closed mode)

Commit message:

```text
feat(plugin/manager): wire INV-S5 validator fail-closed (jg9b.3)

Group F of jg9b.3. Validator now active in manager.loadPlugin: any
plugin with non-empty crypto.emits whose code-registered set differs
from the manifest set fails plugin load with EVENT_TYPE_REGISTRY_MISMATCH.

At this point any in-tree plugin that hasn't adopted RegisterEmitType
(Groups G and H next) will fail to load. The audit precondition
(jg9b.2) confirmed the set to be exactly core-communication and
core-objects; those adoptions are next.
```

- [ ] **Step 2.F.7: commit**

---

### Group G: core-communication adopts holomush.register_emit_type

#### Step 2.G.1: Read main.lua to find the right insertion point

Use Read tool on `plugins/core-communication/main.lua` to find the top-level section. Per the audit (jg9b.2), top-level is `local function` declarations only. Insert the registration calls at the top of the file (after `local function trim` style declarations if any — or right after the SPDX header for clarity).

- [ ] **Step 2.G.1: read main.lua, identify insertion point**

#### Step 2.G.2: Add registration calls

Add to `plugins/core-communication/main.lua` (near the top, before any handler assignments):

```lua
-- INV-S5: register the 8 event types this plugin can emit.
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

- [ ] **Step 2.G.2: add registration calls**

#### Step 2.G.3: Verify the plugin loads cleanly

Run a load integration test (if one exists for core-communication) or invoke `task test:int` if it exercises plugin loading:

```bash
task test:int -- ./internal/plugin/...
```

Expected: PASS, with no EVENT_TYPE_REGISTRY_MISMATCH errors for core-communication.

If no such test exists, manually verify by booting the server in dev mode and checking logs for plugin load success.

- [ ] **Step 2.G.3: verify clean load**

#### Step 2.G.4: Commit Group G

Commit message:

```text
feat(core-communication): adopt register_emit_type for INV-S5 (jg9b.3)

Adds 8 top-level holomush.register_emit_type calls matching
plugin.yaml's crypto.emits block: say, pose, ooc, emit, page,
whisper, pemit, whisper_notice. Plugin now loads cleanly under
fail-closed INV-S5 validation.
```

- [ ] **Step 2.G.4: commit**

---

### Group H: core-objects adopts holomush.register_emit_type

#### Step 2.H.1: Read main.lua to find the right insertion point

Use Read tool on `plugins/core-objects/main.lua` — top-level is `local function trim`, `local function lower`, `local function has_prefix` declarations. Insert registration calls after the SPDX header, before the first `local function`.

- [ ] **Step 2.H.1: read main.lua, identify insertion point**

#### Step 2.H.2: Add registration calls

Add to `plugins/core-objects/main.lua` (near the top):

```lua
-- INV-S5: register the 5 event types this plugin can emit.
-- These MUST match plugin.yaml's crypto.emits block exactly.
holomush.register_emit_type("object_create")
holomush.register_emit_type("object_destroy")
holomush.register_emit_type("object_use")
holomush.register_emit_type("object_examine")
holomush.register_emit_type("object_give")
```

- [ ] **Step 2.H.2: add registration calls**

#### Step 2.H.3: Verify the plugin loads cleanly

Run: `task test:int -- ./internal/plugin/...`
Expected: PASS, no EVENT_TYPE_REGISTRY_MISMATCH for core-objects.

- [ ] **Step 2.H.3: verify clean load**

#### Step 2.H.4: Commit Group H

Commit message:

```text
feat(core-objects): adopt register_emit_type for INV-S5 (jg9b.3)

Adds 5 top-level holomush.register_emit_type calls matching
plugin.yaml's crypto.emits block: object_create, object_destroy,
object_use, object_examine, object_give. Plugin now loads cleanly
under fail-closed INV-S5 validation.
```

- [ ] **Step 2.H.4: commit**

---

### Group I: Parity test + final verification

#### Step 2.I.1: Write parity test

Create `internal/plugin/manager_parity_test.go` (new file in the `plugins` package). The test exercises the same logical scenario (manifest declares `[a, b]`, code registers `[a, b]`) through BOTH a Lua plugin path and a binary plugin path, asserting identical validator output.

The implementer chooses fixture cost: either spin up a real binary subprocess (using existing goplugin test fixtures under `internal/plugin/goplugin/`) or use a test-only Host fixture that satisfies the Host interface without process isolation.

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestManager_INVS5_ParityAcrossRuntimes verifies that the validator
// produces identical output for Lua and binary plugins given identical
// manifest declarations and code registrations. Per INV-S3 / INV-M7.
func TestManager_INVS5_ParityAcrossRuntimes(t *testing.T) {
	scenarios := []struct {
		name       string
		declared   []string
		registered []string
		wantMismatch bool
	}{
		{"match", []string{"a", "b"}, []string{"a", "b"}, false},
		{"declared-but-unregistered", []string{"a", "b"}, []string{"a"}, true},
		{"registered-but-undeclared", []string{"a"}, []string{"a", "b"}, true},
	}

	for _, s := range scenarios {
		t.Run(s.name+"-lua", func(t *testing.T) {
			// Set up Lua Host fixture, synthesize plugin manifest+code,
			// run loadPlugin, assert mismatch outcome matches s.wantMismatch.
		})
		t.Run(s.name+"-binary", func(t *testing.T) {
			// Set up binary Host fixture (test-only or subprocess),
			// synthesize plugin manifest + EmitTypeRegistrar impl,
			// run loadPlugin, assert mismatch outcome matches s.wantMismatch.
		})
	}
}
```

Implementer fills in the harness setup per existing test fixtures in the package.

- [ ] **Step 2.I.1: write parity test scaffold and fill in via existing fixtures**

#### Step 2.I.2: Run parity test

Run: `task test -- ./internal/plugin/ -run TestManager_INVS5_ParityAcrossRuntimes`
Expected: PASS — all 6 sub-tests (3 scenarios × 2 runtimes).

- [ ] **Step 2.I.2: verify pass**

#### Step 2.I.3: Run full pr-prep

Run: `task pr-prep`
Expected: PASS green. Mandatory pre-push gate per CLAUDE.md.

- [ ] **Step 2.I.3: verify pr-prep passes**

#### Step 2.I.4: Final commit (parity test + any pr-prep cleanups)

Commit message:

```text
test(plugin): INV-S5 parity test across Lua + binary runtimes (jg9b.3)

Final group of jg9b.3. Adds parity test exercising 3 scenarios
(match, declared-but-unregistered, registered-but-undeclared) across
both Lua and binary plugin paths per INV-M7 / INV-S3.

task pr-prep passes green; jg9b.3 work complete.
```

- [ ] **Step 2.I.4: commit**

#### Step 2.I.5: Close jg9b.3

```bash
bd close holomush-jg9b.3 --reason="Substrate cap + plugin adoptions landed atomically; fail-closed INV-S5 validation active; parity test passes; pr-prep green."
```

- [ ] **Step 2.I.5: close the bead**

---

## Task 3: Documentation — substrate-contract orientation page

**bd:** `holomush-jg9b.4`

**Goal:** Contributor-facing orientation page at `site/docs/extending/substrate-contract.md` covering substrate-vs-use boundary, eventkit/groupkit SDK design (named-only per INV-S7), and INV-S5 manifest emit-type validation behavior.

**Files:**

- Create: `site/docs/extending/substrate-contract.md`
- Possibly modify: site nav (discover during task)

### Step 3.1: Inventory existing site/docs/extending/ structure

Use Read tool or `ls site/docs/extending/` to see existing pages. Note the file naming + frontmatter conventions.

- [ ] **Step 3.1: inspect existing pages**

### Step 3.2: Draft the orientation page

Create `site/docs/extending/substrate-contract.md` with sections:

1. **What is the substrate contract?** (1 paragraph — substrate-vs-use framing; pointer to canonical spec at `docs/superpowers/specs/2026-05-16-social-spaces-substrate-contract.md`)
2. **Substrate primitives you can rely on** (summary table from spec §1)
3. **The plugin-boundary rule (INV-S1)** (short: plugin PRs touch only `plugins/<name>/` + approved `pkg/plugin/*`)
4. **Manifest emit-type validation (INV-S5)** — what plugin authors do:
   - Declare types in `plugin.yaml::crypto.emits`
   - **Lua plugins:** call `holomush.register_emit_type(<type>)` at top level for every type
   - **Binary plugins:** implement `pluginsdk.EmitTypeRegistrar` interface
   - Mismatch fails plugin load with `EVENT_TYPE_REGISTRY_MISMATCH`
   - Top-level idempotency note for Lua
5. **eventkit + groupkit SDKs (named, not yet built)** — N=2 discipline, when they will land
6. **References** (canonical spec, INV-S5 mechanism spec, ADRs)

Keep prose tight (~150 lines). Canonical detail stays in the specs.

- [ ] **Step 3.2: draft the page**

### Step 3.3: Lint the new page

Run: `rumdl check site/docs/extending/substrate-contract.md`
Expected: PASS or auto-fixable issues. If auto-fixable: run `rumdl fmt site/docs/extending/substrate-contract.md` and re-verify, but read the diff to confirm rumdl didn't mangle prose (per the earlier `+`-as-list-marker incident).

- [ ] **Step 3.3: verify lint clean**

### Step 3.4: Build the docs site to verify rendering

Run: `task docs:build`
Expected: site builds without errors. Spot-check the rendered page if `task docs:serve` is available.

- [ ] **Step 3.4: verify docs build**

### Step 3.5: Update site nav if needed

If the page does not auto-appear in nav, find the relevant index file under `site/docs/extending/` or the site config (`site/mkdocs.yml` or equivalent) and add the entry.

- [ ] **Step 3.5: update nav if needed**

### Step 3.6: Commit

Commit message:

```text
docs(extending): add substrate-contract orientation page (jg9b.4)

Contributor on-ramp for the substrate-vs-use boundary, INV-S1 plugin
boundary rule, INV-S5 manifest emit-type validation (with Lua + binary
adoption guidance), and named-not-yet-built eventkit/groupkit SDKs.

Canonical detail lives in the substrate-contract spec at
docs/superpowers/specs/2026-05-16-social-spaces-substrate-contract.md
and the INV-S5 mechanism design at
docs/superpowers/specs/2026-05-17-inv-s5-mechanism-design.md.
```

- [ ] **Step 3.6: commit**

### Step 3.7: Close jg9b.4

```bash
bd close holomush-jg9b.4 --reason="Substrate-contract orientation page shipped at site/docs/extending/substrate-contract.md."
```

- [ ] **Step 3.7: close the bead**

---

## Task 4: Roadmap — update theme:social-spaces section

**bd:** `holomush-jg9b.5`

**Goal:** `docs/roadmap.md`'s `theme:social-spaces` section reflects (a) substrate-contract spec landing, (b) eventkit + groupkit as named primitives, (c) INV-S5 substrate work completion, (d) updated sequencing (Phase 4 + channels rework now both unblocked).

**Files:**

- Modify: `docs/roadmap.md`

### Step 4.1: Read the current theme:social-spaces section

Use Read tool on `docs/roadmap.md` to locate the `theme:social-spaces` section.

- [ ] **Step 4.1: read roadmap section**

### Step 4.2: Update the section narrative

Modifications:

- Add a "Substrate-contract" subsection (after "Substrate (shipped)") that:
  - References both specs (parent substrate-contract + INV-S5 mechanism).
  - Names `eventkit` and `groupkit` SDKs as future primitives (gated on N=2 validation per INV-S7).
  - Notes the INV-S5 substrate work has shipped under `jg9b` epic.
- Update the "Uses (in development)" sub-section:
  - Phase 4 (`5rh.13`): now unblocked (was blocked by undefined mechanism; INV-S5 mechanism shipped via jg9b)
  - Channels rework (`0sc.12`): unblocked; documented as the N=2 validating consumer for eventkit + groupkit
- Update Sequencing rationale to note both can proceed in parallel after `jg9b.3` lands.

Additive and clarifying — preserve the existing narrative voice.

- [ ] **Step 4.2: update section**

### Step 4.3: Lint

Run: `rumdl check docs/roadmap.md`
Expected: PASS.

- [ ] **Step 4.3: verify lint clean**

### Step 4.4: Commit

Commit message:

```text
docs(roadmap): update theme:social-spaces with substrate-contract (jg9b.5)

Reflects the substrate-contract spec + INV-S5 mechanism spec landing:
references both specs, names eventkit + groupkit SDKs as future
primitives, marks the INV-S5 substrate work shipped under jg9b epic,
and clarifies that scenes Phase 4 (5rh.13) and channels rework
(0sc.12) are both unblocked.
```

- [ ] **Step 4.4: commit**

### Step 4.5: Close jg9b.5

```bash
bd close holomush-jg9b.5 --reason="Roadmap theme:social-spaces section updated to reflect substrate-contract + INV-S5 landing."
```

- [ ] **Step 4.5: close the bead**

---

## Task 5: Bead hygiene — propagate spec references to affected beads

**bd:** `holomush-jg9b.6`

**Goal:** Existing beads in scenes/channels/forums/discord epics get dep edges to `jg9b.3` (substrate cap) and `bd note` pointers to the substrate-contract + INV-S5 mechanism specs.

**Files:** none (bd state only).

### Step 5.1: Add dep edges

Add edges from `jg9b.3` (substrate + adoptions) to the downstream consumers. Per the mechanism spec §6 dep-edge-recite note, these edges replace any prior dependency on the now-non-existent `jg9b.4` from the original spec proposal.

```bash
bd dep add holomush-5rh.13 holomush-jg9b.3
bd dep add holomush-0sc.12 holomush-jg9b.3
```

Expected: both dep edges accepted (task-to-task per beads-project.md).

- [ ] **Step 5.1: add dep edges**

### Step 5.2: Add bd notes referencing both specs to affected beads

Run each sequentially (no parallel `bd create`/`bd note` per `feedback_bd_create_no_parallel` — safe for notes but keep tidy):

```bash
bd note holomush-5rh.13 "Substrate-contract spec landed at docs/superpowers/specs/2026-05-16-social-spaces-substrate-contract.md; INV-S5 mechanism settled at docs/superpowers/specs/2026-05-17-inv-s5-mechanism-design.md. Phase 4 brainstorm will populate core-scenes' crypto.emits with scene_ic/scene_ooc/scene_join/scene_leave/etc. AND implement pluginsdk.EmitTypeRegistrar on scenePlugin (mechanism spec §6 'core-scenes adoption deferred to Phase 4 when crypto.emits gets populated'). Unblocked by jg9b.3."

bd note holomush-5rh.14 "Substrate-contract spec landed. Phase 5 (focus model + multi-connection visibility) binds to spec §4.1 + §1.4. Membership-vs-focus crossover semantics decided in this phase's brainstorm."

bd note holomush-5rh.15 "Substrate-contract spec landed. Phase 6 brainstorm will (a) decide publication-artifact rename (scene_log audit table vs new publication name — see spec §6.1), (b) decide OriginLocationID / PublishVote reinstate, (c) preserve INV-S9 hard privacy boundary plugin-code-enforced (per spec §4.1)."

bd note holomush-0sc.12 "Substrate-contract spec landed; INV-S5 mechanism shipped. Channels rework is the N=2 validating consumer for eventkit (replay, cryptoemit) and groupkit (membership, focuswire, groupabac) per spec §4.2 + INV-S7. Brainstorm MUST produce a '## SDK primitive validation' section reporting adopt-as-is / API-tweak / reject-as-not-fit per primitive. Unblocked by jg9b.3."

bd note holomush-djj "Substrate-contract spec landed. Forums uses eventkit ONLY (NOT groupkit) per spec §4.3 + INV-S10 — forum participation is incidental, not intentional membership. djj.1 design brainstorm decides thread/post model + web UI + eventkit adoption shape."

bd note holomush-aqq "Substrate-contract spec landed. Discord is a bridge plugin: groupkit forbidden (INV-S10); eventkit/replay permitted conditionally if cross-history sync requires ABAC-filtered replay (per spec §4.4). aqq.1 design brainstorm decides OAuth flow + bridge model + presence sync + SDK adoption."

bd note holomush-5rh.9 "Substrate-contract spec §4.3 says forums is OUT of theme-wide SDK scope. When djj forums brainstorm fires, this bead may be reparented under djj (forum integration with scenes) or kept under 5rh (scenes-side hooks). Decision deferred until forums brainstorm."
```

- [ ] **Step 5.2: add bd notes to 7 affected beads**

### Step 5.3: Verify the notes landed

Spot check:

```bash
bd show holomush-5rh.13 | grep -A 1 "Substrate-contract"
bd show holomush-0sc.12 | grep -A 1 "N=2 validating"
bd show holomush-djj | grep -A 1 "eventkit ONLY"
```

Expected: each grep returns the new note text.

- [ ] **Step 5.3: verify notes landed**

### Step 5.4: Sync bd dolt

Run: `bd dolt push`
Expected: pushes notes + dep edges to remote dolt; no errors.

- [ ] **Step 5.4: sync bd state**

### Step 5.5: Close jg9b.6

```bash
bd close holomush-jg9b.6 --reason="Bead hygiene complete: dep edges added (5rh.13, 0sc.12 → jg9b.3); notes added to 7 affected beads; bd dolt push synced."
```

- [ ] **Step 5.5: close the bead**

---

## Post-implementation checklist (after all 5 tasks complete)

- [ ] All 5 child beads (`jg9b.2`–`jg9b.6`) marked closed.
- [ ] Epic `jg9b` automatically reflects completion (rollup: 5/5 closed; jg9b.1 design bead also closed by this skill's auto-fire chain or manually).
- [ ] `task pr-prep` green from Task 2 Step 2.I.3.
- [ ] Single PR (or coordinated PR chain per task) opened with link to both specs + epic.
- [ ] `pr-review-toolkit:review-pr` runs on the PR(s).
- [ ] After merge: `bd dolt push` to sync final closed state.

## Follow-up beads (out of scope for this plan, named for future tracking)

- `plan-reviewer` memory update for INV-S7's `## SDK primitive validation` artifact-check rule (per parent-spec design-reviewer round 2 non-blocking #2).
- Parity-test template establishment as a project-wide convention (per parent-spec design-reviewer round 2 non-blocking #3).
- Binary plugin Prometheus metrics infrastructure (separate substrate-infra brainstorm; per parent spec §11.1 STILL OPEN).
- Future `task lint:plugin-boundary` CI predicate to mechanically enforce INV-S1 (per ADR `holomush-z1e7`).

These are NOT part of `jg9b`'s scope; file as separate beads when the time comes.
