# Social Spaces Substrate Contract Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `dev-flow:subagent-driven-development` (recommended) or `dev-flow:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the substrate work mandated by the substrate-contract spec ([parent](../specs/2026-05-16-social-spaces-substrate-contract.md)) using the runtime mechanism settled by the [child mechanism spec](../specs/2026-05-17-inv-s5-mechanism-design.md). Specifically: an Init-RPC-driven manifest emit-type startup validator (INV-S5) with Lua + binary parity, plus plugin adoptions, docs, roadmap update, and bead-hygiene propagation.

**Architecture:** Per the mechanism spec's Approach X: binary plugins implement an opt-in `pluginsdk.EmitTypeRegistrar` interface; the SDK adapter auto-populates a new `registered_emit_types` field on `pluginv1.InitResponse`. Lua plugins call `holomush.register_emit_type(...)` at top-level; the Lua Host's `Load` uses a branched pass — for crypto-emits plugins, the INV-S5 capture pass replaces the syntax-check pass entirely (one Load-time execution per plugin). Both runtimes expose the registered set via a new `Host.PluginEmitRegistry(name) ([]string, bool)` interface method. The validator in `manager.go::loadPlugin` runs after `host.Load` succeeds, fails plugin load on mismatch (fail-closed from day one).

**Tech Stack:** Go (stdlib + `samber/oops` for typed errors + `slog` for logging), `gopher-lua` for Lua hostfunc, `hashicorp/go-plugin` for binary subprocess management, protobuf 3 (proto file change requires `task proto` regen), existing plugin infrastructure at `internal/plugin/`.

**Tracking:** Design bead `holomush-jg9b` (will be promoted to epic by `plan-to-beads`). 5 child tasks (Tasks 1-5 below map 1:1 to `jg9b.2`-`jg9b.6`). Pre-existing child `jg9b.1` is the INV-S5 mechanism design bead (READY).

**Plan style:** Implementation deliverables (validator, SDK API, host modifications) are shown as complete code blocks because that IS what the bead produces. Test specifications are intent + pattern citations — the implementer mimics existing test harnesses cited at `path:line` and writes the actual test bodies against current repo state. This separation prevents the plan from going stale on type-name drift while still providing a complete contract for the work.

---

## File structure

### Substrate cap + adoptions (Task 2)

**Proto:**

- Modify `api/proto/holomush/plugin/v1/plugin.proto:99-103` — add field 2 to `InitResponse`.
- Modify `pkg/proto/holomush/plugin/v1/plugin.pb.go` (auto-regenerated via `task proto`).

**SDK (`pkg/plugin/`):**

- Create `pkg/plugin/emit_registry.go` — `EmitRegistry` type + `EmitTypeRegistrar` interface.
- Create `pkg/plugin/emit_registry_test.go`.
- Modify `pkg/plugin/sdk.go:152-191` — `pluginServerAdapter.Init` populates `RegisteredEmitTypes`.

**Substrate (`internal/plugin/`):**

- Create `internal/plugin/emit_type_validator.go` — `EmitTypeMismatch`, `ValidateEmitTypeSetEquality`.
- Create `internal/plugin/emit_type_validator_test.go`.
- Modify `internal/plugin/host.go` — add `PluginEmitRegistry(name) ([]string, bool)` to the `Host` interface.
- Modify `internal/plugin/manager.go::loadPlugin` (insertion after `host.Load` at line 989).

**Lua side:**

- Create `internal/plugin/hostfunc/stdlib_emit_registry.go` — `LuaEmitRegistry` + `RegisterEmitTypeFuncs`.
- Create `internal/plugin/hostfunc/stdlib_emit_registry_test.go`.
- Modify `internal/plugin/hostfunc/functions.go:134-194` — add `RegisterWithEmitCapture` entry point.
- Modify `internal/plugin/lua/host.go:33-46` — add `emitRegistry` field to `luaPlugin`.
- Modify `internal/plugin/lua/host.go:111-163` — branched Load pass + `PluginEmitRegistry` implementation.

**Binary side:**

- Modify `internal/plugin/goplugin/host.go:509` — extend Init RPC gate to fire when `crypto.emits` non-empty.
- Modify `internal/plugin/goplugin/host.go:528` — capture `InitResponse.RegisteredEmitTypes`.
- Modify `internal/plugin/goplugin/host.go:183-190` — extend `loadedPlugin` struct with `registeredEmitTypes []string`.
- Modify `internal/plugin/goplugin/host.go:537-544` — struct literal: populate new field.
- Add `PluginEmitRegistry` method on `*Host`.

**Plugin adoptions:**

- Modify `plugins/core-communication/main.lua` — 8 top-level `holomush.register_emit_type` calls.
- Modify `plugins/core-objects/main.lua` — 5 top-level `holomush.register_emit_type` calls.

**Parity test:**

- Create `internal/plugin/goplugin/manager_parity_test.go` (package `goplugin` so it can use in-package `newMockHost` directly — see Step 2.I.1).

### Docs (Task 3)

- Create `site/docs/extending/substrate-contract.md`.

### Roadmap (Task 4)

- Modify `docs/roadmap.md` theme:social-spaces section.

### Bead hygiene (Task 5)

- No file changes — bd state only.
- Step 5.4b also amends parent spec `docs/superpowers/specs/2026-05-16-social-spaces-substrate-contract.md` for bead-id drift.

---

## Task 1: Audit precondition

**bd:** `holomush-jg9b.2`

**Goal:** Verify the complete in-tree plugin set that declares non-empty `crypto.emits`. Confirms the mechanism spec's §4.1 inventory matches current repo state. No code changes.

### Step 1.1: Enumerate crypto.emits plugins

Run: `rg -l '^crypto:' plugins/`

Expected: exactly 3 paths — `plugins/core-communication/plugin.yaml`, `plugins/core-objects/plugin.yaml`, `plugins/core-scenes/plugin.yaml`. If different, STOP and reconcile with mechanism spec §4.1.

- [ ] **Step 1.1: run and verify**

### Step 1.2: Classify crypto.emits population per plugin

Run `rg -A 30 '^crypto:' <path>` for each plugin and count `event_type:` lines. Expected:

- `core-communication`: 8 types (`say`, `pose`, `ooc`, `emit`, `page`, `whisper`, `pemit`, `whisper_notice`)
- `core-objects`: 5 types (`object_create`, `object_destroy`, `object_use`, `object_examine`, `object_give`)
- `core-scenes`: 0 types (`crypto.emits: []`)

If counts differ, STOP and reconcile.

- [ ] **Step 1.2: confirm counts**

### Step 1.3: Confirm plugin runtime types

`rg '^type:' <path>` for each. Expected: `core-communication=lua`, `core-objects=lua`, `core-scenes=binary`.

- [ ] **Step 1.3: confirm runtimes**

### Step 1.4: Verify Lua top-level idempotency precondition

Read `plugins/core-communication/main.lua` and `plugins/core-objects/main.lua` top levels. Confirm only `local function`/`local <const>` declarations + comments — no hostfunc calls like `holomush.kv_set(...)` or `holomush.create_location(...)` at top level. If a plugin's top-level contains non-idempotent calls, STOP — either refactor the plugin or flag as a blocker.

- [ ] **Step 1.4: verify idempotent top-level**

### Step 1.5: Record audit result + close

```bash
bd note holomush-jg9b.2 "Audit complete: 3 plugins declare crypto:, 2 with non-empty emits (core-communication=8 Lua, core-objects=5 Lua). core-scenes empty (gated out by INV-M1). Top-level idempotency verified for both Lua plugins."
bd close holomush-jg9b.2 --reason="Audit precondition verified."
```

- [ ] **Step 1.5: bd note + close**

---

## Task 2: Substrate cap + plugin adoptions

**bd:** `holomush-jg9b.3`

**Goal:** Land the full INV-S5 mechanism per the mechanism design spec, with both plugin adoptions, in one coherent change. Fail-closed from day one.

> **Group execution order matters:** A → B → C → D → E → G → H → F → I. Group F (manager validator wiring) MUST run AFTER Groups G+H (plugin adoptions) — otherwise validator fires fail-closed against not-yet-adopted plugins and the test suite goes RED. Group letters are alphabetic for stable references; execution order overrides alphabetic order.

**Scope note:** INV-S5 validates set-equality between `crypto.emits` event-type strings and the strings the plugin's code registers. It does NOT catch the pre-existing qualified-vs-unqualified runtime drift (manifest declares `say`; runtime emits `core-communication:say`). That's a separate concern filed as a follow-up bead.

### Group A: SDK API + validator (no consumers; commits in isolation)

#### Step 2.A.1: Failing test — `EmitRegistry` API

Add `pkg/plugin/emit_registry_test.go`. Test cases:

1. `Register single type + RegisteredEmitTypes returns sorted slice`
2. `RegisterEmitTypes batch + returns combined sorted set`
3. `Duplicate registration is idempotent`
4. `Empty registry returns empty slice`

Use testify `require`. Test names follow ACE (Action/Condition/Expectation) per `.claude/rules/testing.md`.

Run: `task test -- ./pkg/plugin/ -run TestEmitRegistry`. Expected FAIL — `NewEmitRegistry` undefined.

- [ ] **Step 2.A.1**

#### Step 2.A.2: Implement `EmitRegistry` + `EmitTypeRegistrar` interface

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
// emit. Plugins register types during construction or in Init. The host
// reads the set via InitResponse.registered_emit_types and validates
// against manifest's crypto.emits per INV-S5.
type EmitRegistry struct {
	mu    sync.Mutex
	types map[string]struct{}
}

func NewEmitRegistry() *EmitRegistry {
	return &EmitRegistry{types: make(map[string]struct{})}
}

func (r *EmitRegistry) RegisterEmitType(eventType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.types[eventType] = struct{}{}
}

func (r *EmitRegistry) RegisterEmitTypes(eventTypes []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range eventTypes {
		r.types[t] = struct{}{}
	}
}

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
// Plugins with non-empty crypto.emits MUST implement this interface;
// the substrate validator fails load on mismatch. Plugins without
// crypto.emits are out of INV-S5 scope (per INV-M1) and may skip.
type EmitTypeRegistrar interface {
	EmitRegistry() *EmitRegistry
}
```

Run tests: PASS.

- [ ] **Step 2.A.2**

#### Step 2.A.3: Failing test — validator

Add `internal/plugin/emit_type_validator_test.go`. Test cases:

1. Matching sets → no mismatch
2. Declared-but-unregistered → mismatch with correct extras
3. Registered-but-undeclared → mismatch with correct extras
4. Both directions diff
5. Both empty → no mismatch
6. **INV-M2 host-owned filter**: registered set contains host-owned types (e.g., `system`, `move`, `arrive` from `pkg/plugin/event.go:34-44`) → those are filtered out before comparison; if the remaining filtered set matches `declared`, no mismatch is reported.

Run: FAIL.

- [ ] **Step 2.A.3**

#### Step 2.A.4: Implement validator (with INV-M2 host-owned filter)

Create `internal/plugin/emit_type_validator.go`. Per INV-M2 (mechanism spec line 58), the substrate MUST filter host-owned event types from the registered set before comparison — the registry contains ALL types the plugin may emit, but host-owned types (the per-`pkg/plugin/event.go` constants) are not subject to `crypto.emits` validation:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"sort"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// hostOwnedEmitTypes lists event-type strings that are host-owned (per
// pkg/plugin/event.go constants) and therefore filtered out of the
// registered set before INV-S5 set-equality comparison. Per INV-M2.
var hostOwnedEmitTypes = map[string]struct{}{
	string(pluginsdk.HostEventTypeSystem):          {},
	string(pluginsdk.HostEventTypeSessionEnded):    {},
	string(pluginsdk.HostEventTypeCommandResponse): {},
	string(pluginsdk.HostEventTypeCommandError):    {},
	string(pluginsdk.HostEventTypeArrive):          {},
	string(pluginsdk.HostEventTypeLeave):           {},
	string(pluginsdk.HostEventTypeMove):            {},
	string(pluginsdk.HostEventTypeLocationState):   {},
	string(pluginsdk.HostEventTypeExitUpdate):      {},
}

// EmitTypeMismatch describes the diff between a plugin's manifest-declared
// crypto.emits set and the SDK-registered emit-type set per INV-S5.
type EmitTypeMismatch struct {
	DeclaredButUnregistered []string
	RegisteredButUndeclared []string
}

func (m EmitTypeMismatch) HasMismatch() bool {
	return len(m.DeclaredButUnregistered) > 0 || len(m.RegisteredButUndeclared) > 0
}

// ValidateEmitTypeSetEquality compares the manifest-declared emit-type
// set against the SDK-registered set (with host-owned types filtered out
// per INV-M2). Per INV-S5, the two sets MUST be equal in both directions.
func ValidateEmitTypeSetEquality(declared, registered []string) EmitTypeMismatch {
	declSet := toEmitSet(declared)
	regSet := toEmitSet(filterHostOwned(registered))

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

// filterHostOwned removes host-owned event types from the registered
// set before INV-S5 comparison. Per INV-M2 — substrate filters; the
// SDK + hostfunc surface accepts any string (plugins MAY register host-
// owned types; the validator MUST NOT count them as plugin-owned).
func filterHostOwned(registered []string) []string {
	out := registered[:0:len(registered)]
	for _, r := range registered {
		if _, host := hostOwnedEmitTypes[r]; !host {
			out = append(out, r)
		}
	}
	return out
}

func toEmitSet(s []string) map[string]struct{} {
	out := make(map[string]struct{}, len(s))
	for _, v := range s {
		out[v] = struct{}{}
	}
	return out
}
```

Note: `filterHostOwned` uses a `s[:0:len(s)]` slice trick to filter in-place without allocating; this is fine because the callers pass a freshly-returned `RegisteredEmitTypes()` slice that is not held elsewhere.

Run tests: PASS — all 6 cases (including the new INV-M2 filter case).

- [ ] **Step 2.A.4**

#### Step 2.A.5: Lint + commit Group A

Run `task lint`. Commit:

```text
feat(plugin): EmitRegistry SDK + emit-type validator for INV-S5 (jg9b.3)

Group A: foundation only — no consumers yet. EmitRegistry +
EmitTypeRegistrar interface in pkg/plugin/; ValidateEmitTypeSetEquality
in internal/plugin/. Unit tests cover both directions of mismatch.
```

- [ ] **Step 2.A.5**

---

### Group B: Lua hostfunc + RegisterWithEmitCapture

#### Step 2.B.1: Failing test — Lua hostfunc

Add `internal/plugin/hostfunc/stdlib_emit_registry_test.go`. Test cases:

1. `holomush.register_emit_type` accumulates calls into the registry
2. Duplicate registration is idempotent
3. Non-string argument raises Lua error

Pattern: follow `stdlib_focus_test.go` for state setup (`lua.NewState`, module table, register fn). Use testify `require`.

Run: FAIL.

- [ ] **Step 2.B.1**

#### Step 2.B.2: Implement Lua hostfunc

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

func NewLuaEmitRegistry() *LuaEmitRegistry {
	return &LuaEmitRegistry{types: make(map[string]struct{})}
}

func (r *LuaEmitRegistry) add(t string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.types[t] = struct{}{}
}

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
// Only called via Functions.RegisterWithEmitCapture during the Lua
// Host's INV-S5 Load-pass. The standard per-delivery Functions.Register
// path does NOT install register_emit_type — a per-delivery call
// dispatches to nil and raises "attempt to call a nil value", which is
// correct end-state behavior (registrations are Load-time-only) by
// absence-by-default.
func RegisterEmitTypeFuncs(ls *lua.LState, mod *lua.LTable, reg *LuaEmitRegistry) {
	ls.SetField(mod, "register_emit_type", ls.NewFunction(func(ls *lua.LState) int {
		eventType := ls.CheckString(1)
		reg.add(eventType)
		ls.Push(lua.LTrue)
		return 1
	}))
}
```

Run tests: PASS.

- [ ] **Step 2.B.2**

#### Step 2.B.3: Failing test — `Functions.RegisterWithEmitCapture`

Add a test to `internal/plugin/hostfunc/functions_internal_test.go` (the method is on `*Functions` so internal-test gives the cleanest access; existing `functions_internal_test.go` already has package-private test patterns to mimic).

Test cases:

1. After `RegisterWithEmitCapture`, a Lua script calling `holomush.register_emit_type("x")` adds `"x"` to the passed registry.
2. Two `register_emit_type("x")` calls in the same script result in registry containing `{"x"}` (idempotent at hostfunc level — already covered in Step 2.B.1, but verifies the integration via `RegisterWithEmitCapture` doesn't break it).
3. `RegisterWithEmitCapture` installs the standard `holomush.*` namespace too: after the call, `holomush.log` and other stdlib functions are still present (this confirms the wrapper invokes the base `Register` and doesn't replace the table).

Run: FAIL (`RegisterWithEmitCapture` method doesn't exist).

- [ ] **Step 2.B.3**

#### Step 2.B.4: Implement `Functions.RegisterWithEmitCapture`

Modify `internal/plugin/hostfunc/functions.go`. Add below the existing `Register` (around line 194):

```go
// RegisterWithEmitCapture is the variant of Register used during the
// Lua Host's INV-S5 Load-pass. Identical to Register, but ALSO installs
// holomush.register_emit_type which appends to reg.
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

Run test: PASS.

- [ ] **Step 2.B.4**

#### Step 2.B.5: Lint + commit Group B

Commit:

```text
feat(plugin/hostfunc): Lua register_emit_type hostfunc + capture entry point (jg9b.3)

Group B: LuaEmitRegistry + RegisterEmitTypeFuncs (new file),
Functions.RegisterWithEmitCapture entry point (variant of Register
that ALSO installs register_emit_type). No host-side wiring yet —
Group E connects it to lua/host.go::Load.
```

- [ ] **Step 2.B.5**

---

### Group C: Proto field + binary SDK adapter

#### Step 2.C.1: Modify proto

Edit `api/proto/holomush/plugin/v1/plugin.proto` lines 99-103. Add field 2 to `InitResponse`:

```proto
message InitResponse {
  repeated string provided_services = 1;

  // Set of plugin-owned event types this plugin may emit. Host validates
  // set-equality against manifest's crypto.emits per INV-S5. Plugins
  // without crypto.emits leave empty and skip validation; plugins WITH
  // crypto.emits MUST populate (mismatch fails load).
  repeated string registered_emit_types = 2;
}
```

Run `task proto`. Verify `pkg/proto/holomush/plugin/v1/plugin.pb.go` regenerates with `RegisteredEmitTypes` field + getter.

- [ ] **Step 2.C.1**

#### Step 2.C.2: Failing test — SDK adapter populates InitResponse

Add a test to `pkg/plugin/sdk_test.go`. Cases:

1. Provider implementing `EmitTypeRegistrar` with registered set `[foo, bar]` → adapter Init returns `InitResponse{RegisteredEmitTypes: [bar, foo]}` (sorted).
2. Provider NOT implementing `EmitTypeRegistrar` → adapter Init returns empty `RegisteredEmitTypes`.

Pattern: examine existing `sdk_test.go` / `sdk_adapter_test.go` for ServiceProvider stub patterns. Compose new test providers that include `EmitRegistry()` method.

Run: FAIL.

- [ ] **Step 2.C.2**

#### Step 2.C.3: Implement adapter wiring

Modify `pkg/plugin/sdk.go::pluginServerAdapter.Init` (line 152). Replace the final `return &pluginv1.InitResponse{}, nil` (after the existing optional-interface delegation code at lines 158-189) with:

```go
// INV-S5: populate RegisteredEmitTypes from EmitTypeRegistrar if the
// provider opts in. Plugins without crypto.emits leave the set empty.
resp := &pluginv1.InitResponse{}
if registrar, ok := a.serviceProvider.(EmitTypeRegistrar); ok {
	resp.RegisteredEmitTypes = registrar.EmitRegistry().RegisteredEmitTypes()
}
return resp, nil
```

The `provided_services` field (proto field 1) is not currently populated by the adapter and remains out of scope.

Run test: PASS.

- [ ] **Step 2.C.3**

#### Step 2.C.4: Lint + commit Group C

Commit:

```text
feat(plugin): InitResponse.registered_emit_types proto field + SDK adapter (jg9b.3)

Group C: proto field 2 on InitResponse + regenerated bindings; SDK
adapter (pluginServerAdapter.Init) auto-populates from EmitTypeRegistrar
opt-in. Lua side already shipped in Group B; Host interface comes in D.
```

- [ ] **Step 2.C.4**

---

### Group D: Host interface + per-runtime implementations

#### Step 2.D.1: Failing test — Lua Host `PluginEmitRegistry`

Add 3 tests to `internal/plugin/lua/host_test.go` (file is `package lua_test` — qualify host constructors as `pluginlua.NewHost*` per existing test imports):

1. Not-loaded plugin → `(nil, false)`
2. Loaded plugin without `crypto.emits` → `(nil, true)` (existing syntax-check pass unchanged)
3. Loaded plugin with `crypto.emits` matching registered set → `(set, true)` — **this one is expected to FAIL at this step** because Group E hasn't replaced the syntax-check pass yet; it will be flipped green at Step 2.E.3

Pattern: synthesize plugin under `t.TempDir()` following the harness shape used by existing `setupCommunicationTest` at `internal/plugin/communication_integration_test.go:34-87` (just don't import the integration helper — recreate the t.TempDir-based equivalent inline since this is a unit test).

Run: at least one FAIL (interface method doesn't exist).

- [ ] **Step 2.D.1**

#### Step 2.D.2: Add `PluginEmitRegistry` to Host interface

Modify `internal/plugin/host.go`. Add to the `Host` interface (existing definition around line 43):

```go
// PluginEmitRegistry returns the code-registered emit-type set for a
// loaded plugin, captured during Load. Returns:
//   - (set, true)  : plugin loaded and opted into INV-S5 (non-empty crypto.emits)
//   - (nil, true)  : plugin loaded; INV-S5 not applicable (empty crypto.emits)
//   - (nil, false) : plugin not loaded under this Host
PluginEmitRegistry(name string) ([]string, bool)
```

- [ ] **Step 2.D.2**

#### Step 2.D.3: Implement on Lua Host

Modify `internal/plugin/lua/host.go`. Add `emitRegistry` field to `luaPlugin` (existing struct around line 33):

```go
type luaPlugin struct {
	manifest     *plugins.Manifest
	code         string
	emitRegistry []string // INV-S5: populated during Load capture pass; nil when crypto.emits empty
}
```

Add the method on `*Host`:

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

Run tests from Step 2.D.1: 2/3 PASS; the crypto.emits one still FAILS (emitRegistry stays nil until Group E).

- [ ] **Step 2.D.3**

#### Step 2.D.4: Failing test — binary Host `PluginEmitRegistry`

Add 2 tests to `internal/plugin/goplugin/host_test.go` (file is `package goplugin` — in-package; can use `newMockHost` directly):

1. Not-loaded plugin → `(nil, false)`
2. Loaded plugin where mock InitResponse returned `RegisteredEmitTypes: [a, b]` → `(set, true)`

**Mock extension required** (these changes live in `host_test.go`, all in-package — no exported surface needed):

- Add field `initResponse *pluginv1.InitResponse` to the existing `mockGRPCPluginClient` struct.
- Add method `func (m *mockGRPCPluginClient) setInitResponse(r *pluginv1.InitResponse) { m.initResponse = r }` (lowercase; package-internal).
- Modify the existing `Init` method on `mockGRPCPluginClient` (around line 123) to return `m.initResponse` when non-nil, else fall back to `&pluginv1.InitResponse{}` (preserves existing-test behavior).
- Add a single-line accessor for the inner gRPC mock so callers (Step 2.D.4 test 2 and Step 2.I.1's binary branch) don't have to do the awkward `mockClient.protocol.pluginClient.(*mockGRPCPluginClient)` cast at every call site:

  ```go
  // grpcMockFor extracts the underlying *mockGRPCPluginClient from a
  // *mockPluginClient returned by newMockHost. Use this when tests need
  // to configure InitResponse via setInitResponse.
  func grpcMockFor(c *mockPluginClient) *mockGRPCPluginClient {
      return c.protocol.pluginClient.(*mockGRPCPluginClient)
  }
  ```

Step 2.D.4 test 2 then reads: `grpcMockFor(mockClient).setInitResponse(&pluginv1.InitResponse{RegisteredEmitTypes: []string{"a", "b"}})`.

Run: FAIL (`PluginEmitRegistry` not yet implemented).

- [ ] **Step 2.D.4**

#### Step 2.D.5: Implement on binary Host

Modify `internal/plugin/goplugin/host.go`:

**Struct definition (line 183-190)** — extend `loadedPlugin` with one new field:

```go
type loadedPlugin struct {
	manifest             *plugins.Manifest
	client               PluginClient
	plugin               pluginv1.PluginServiceClient
	conn                 grpc.ClientConnInterface
	certDir              string
	broker               *hashiplug.GRPCBroker
	registeredEmitTypes  []string // INV-S5: populated from InitResponse.RegisteredEmitTypes
}
```

**Init RPC gate (line 509)** — extend the existing condition to also fire when crypto.emits is non-empty:

```go
needsInit := len(manifest.Requires) > 0 ||
	len(manifest.Provides) > 0 ||
	manifest.Storage == plugins.StoragePostgres ||
	(manifest.Crypto != nil && len(manifest.Crypto.Emits) > 0)
if needsInit {
	// ... existing Init RPC body unchanged ...
}
```

**Init RPC call (line 528)** — capture the response instead of discarding:

```go
initResp, initErr := pluginClient.Init(ctx, initReq)
if initErr != nil {
	client.Kill()
	if certDir != "" {
		_ = os.RemoveAll(certDir) //nolint:errcheck // best-effort cleanup
	}
	return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "init").Wrap(initErr)
}
```

After the `needsInit` block ends, capture `registeredEmitTypes` for the struct literal:

```go
var registeredEmitTypes []string
if needsInit && initResp != nil {
	registeredEmitTypes = initResp.GetRegisteredEmitTypes()
}
```

In the existing struct literal at lines 537-544, add `registeredEmitTypes: registeredEmitTypes` to the field assignments.

**`PluginEmitRegistry` method:**

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

Run tests: PASS.

- [ ] **Step 2.D.5**

#### Step 2.D.6: Lint + commit Group D

**Note on test suite state at this commit:** the Step 2.D.1 subtest `TestLuaHost_PluginEmitRegistry_LoadedPluginWithCryptoEmits` remains RED at this commit boundary — it is intentionally on a chain that includes one failing test until Group E lands the branched Load pass (Step 2.E.2 flips it green). All other subtests pass. The pr-prep gate at Step 2.I.2 is the only mandatory all-green checkpoint within Task 2; intermediate commits in this group chain are acceptable to leave one specific test red, provided each subsequent commit moves toward green.

Commit:

```text
feat(plugin): PluginEmitRegistry method + Host interface extension (jg9b.3)

Group D: Host.PluginEmitRegistry method on the interface; impls on Lua
and binary hosts. Binary side captures InitResponse.RegisteredEmitTypes;
gate at host.go:509 extended to fire Init for crypto.emits plugins.
Lua side returns luaPlugin.emitRegistry (still nil until Group E lands
the branched Load pass that populates it).

Intermediate test suite state: one Lua subtest red until Group E;
pr-prep gate at Step 2.I.2 enforces final all-green.
```

- [ ] **Step 2.D.6**

---

### Group E: Lua Load branched pass

#### Step 2.E.1: Modify Lua Host Load to use branched pass

Per amended mechanism spec §2.2: the existing syntax-check pass at `lua/host.go:147-155` runs `DoString` WITHOUT calling `hostFuncs.Register`. `holomush` is undefined there. If a Lua plugin's top-level acquires `holomush.register_emit_type(...)` calls (Groups G/H), the syntax-check pass would fail. The fix REPLACES the syntax-check pass with a branched implementation.

Modify `internal/plugin/lua/host.go::Load`. **REPLACE** the entire existing syntax-check block (lines 147-155) with:

```go
// Branch the Load pass on whether INV-S5 capture is needed.
//
// Plugins WITHOUT non-empty crypto.emits: existing syntax-check
// throwaway state (no hostfuncs). Unchanged from today.
//
// Plugins WITH non-empty crypto.emits: capture-and-validate pass
// (hostfuncs registered including register_emit_type). The captured
// registry is stored on luaPlugin for the validator
// (manager.go::loadPlugin reads via Host.PluginEmitRegistry).
var emitRegistry []string
L, err := h.factory.NewState(ctx)
if err != nil {
	return oops.In("lua").With("plugin", manifest.Name).With("operation", "load").
		Hint("failed to create validation state").Wrap(err)
}
defer L.Close()

if manifest.Crypto != nil && len(manifest.Crypto.Emits) > 0 {
	// INV-S5 capture pass: hostfuncs registered, captures top-level
	// holomush.register_emit_type calls into per-plugin LuaEmitRegistry.
	reg := hostfunc.NewLuaEmitRegistry()
	if h.hostFuncs != nil {
		h.hostFuncs.RegisterWithEmitCapture(L, manifest.Name, reg, manifest.Requires...)
	}
	if err := L.DoString(string(code)); err != nil {
		return oops.In("lua").With("plugin", manifest.Name).With("operation", "load").
			With("entry", manifest.LuaPlugin.Entry).
			Hint("INV-S5 capture pass execution error").Wrap(err)
	}
	emitRegistry = reg.Types()
} else {
	// Existing syntax-check pass — no hostfuncs registered.
	if err := L.DoString(string(code)); err != nil {
		return oops.In("lua").With("plugin", manifest.Name).With("operation", "load").
			With("entry", manifest.LuaPlugin.Entry).
			Hint("syntax error").Wrap(err)
	}
}
```

Update the existing `luaPlugin` struct literal (around line 157) to set the new field:

```go
h.plugins[manifest.Name] = &luaPlugin{
	manifest:     manifest,
	code:         string(code),
	emitRegistry: emitRegistry,
}
```

Add the `hostfunc` package import if not already present.

- [ ] **Step 2.E.1**

#### Step 2.E.2: Re-run Step 2.D.1 tests

The previously-failing crypto.emits PluginEmitRegistry test now PASSES because Load populates `emitRegistry`.

Run: `task test -- ./internal/plugin/lua/ -run TestLuaHost_PluginEmitRegistry`. Expected: ALL PASS.

- [ ] **Step 2.E.2**

#### Step 2.E.3: Add capture-pass error test

Add a test verifying that a crypto.emits plugin whose top-level Lua throws an error fails `Load` with `operation=load` + Hint mentioning "INV-S5 capture pass execution error".

Pattern: same synthetic-plugin harness as Step 2.D.1; plugin's `main.lua` contains `error("intentional")` at top level + `crypto.emits: [a]`.

Run: PASS (the implementation in Step 2.E.1 already produces this error shape).

- [ ] **Step 2.E.3**

#### Step 2.E.4: Lint + commit Group E

Commit:

```text
feat(plugin/lua): branched Load pass for INV-S5 capture (jg9b.3)

Group E: REPLACES the syntax-check pass at lua/host.go:147-155 with a
branched implementation per amended mechanism spec §2.2. Non-crypto
plugins keep existing syntax-check; crypto plugins use the INV-S5
capture pass which doubles as syntax check (hostfuncs registered,
holomush.register_emit_type accumulates into per-plugin LuaEmitRegistry).

Result: one Load-time execution per plugin regardless of crypto.emits
scope. Opt-in scope per ADR holomush-7h0c preserved.
```

- [ ] **Step 2.E.4**

---

### Group G: core-communication adoption

> Execution order: Groups G + H run BEFORE Group F. See Task 2 preamble.

#### Step 2.G.1: Add `register_emit_type` calls

Read `plugins/core-communication/main.lua` to identify the top-level region (after SPDX header, before the first `local function`). Insert 8 calls matching `plugin.yaml::crypto.emits`:

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

Run an integration test that loads core-communication (or a smoke load). Expected PASS; no warning logs.

- [ ] **Step 2.G.1**

#### Step 2.G.2: Lint + commit

Commit:

```text
feat(core-communication): adopt register_emit_type for INV-S5 (jg9b.3)

8 top-level holomush.register_emit_type calls matching plugin.yaml's
crypto.emits block: say, pose, ooc, emit, page, whisper, pemit,
whisper_notice.
```

- [ ] **Step 2.G.2**

---

### Group H: core-objects adoption

#### Step 2.H.1: Add `register_emit_type` calls

Insert at top of `plugins/core-objects/main.lua` (after SPDX header, before first `local function`):

```lua
-- INV-S5: register the 5 event types this plugin can emit.
-- These MUST match plugin.yaml's crypto.emits block exactly.
holomush.register_emit_type("object_create")
holomush.register_emit_type("object_destroy")
holomush.register_emit_type("object_use")
holomush.register_emit_type("object_examine")
holomush.register_emit_type("object_give")
```

Run integration smoke load: PASS.

- [ ] **Step 2.H.1**

#### Step 2.H.2: Lint + commit

Commit:

```text
feat(core-objects): adopt register_emit_type for INV-S5 (jg9b.3)

5 top-level holomush.register_emit_type calls matching plugin.yaml's
crypto.emits block: object_create, object_destroy, object_use,
object_examine, object_give.
```

- [ ] **Step 2.H.2**

---

### Group F: Manager validator wiring (fail-closed)

> **EXECUTION ORDER:** must run AFTER Groups G + H. See Task 2 preamble.

#### Step 2.F.1: Failing test — manager validator

Add 3 tests to `internal/plugin/manager_test.go`. Cases:

1. Mismatch (declared `[a, b]`, registered `[a]`) → `loadPlugin` returns error with `oops` code `EVENT_TYPE_REGISTRY_MISMATCH`
2. Match → `loadPlugin` succeeds
3. No `crypto.emits` block → `loadPlugin` succeeds without consulting `PluginEmitRegistry`

Use `errutil.AssertErrorCode(t, err, "EVENT_TYPE_REGISTRY_MISMATCH")` from `pkg/errutil` for code-identity assertions.

Pattern: synthesize plugin manifests under `t.TempDir()` (one per test) with the appropriate `crypto.emits` + main.lua `register_emit_type` shape; call `plugins.NewManager` with `plugins.WithLuaHost(...)`; assert on `mgr.LoadAll(ctx)` outcome.

Run: at least one FAIL (validator not wired yet).

- [ ] **Step 2.F.1**

#### Step 2.F.2: Wire validator into `loadPlugin`

Modify `internal/plugin/manager.go::loadPlugin`. After `host.Load(ctx, dp.Manifest, dp.Dir)` returns successfully (line 989) AND before the plugin enters the manager's plugin cache:

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

Add the helper in the same file:

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

Run tests from Step 2.F.1: ALL PASS.

- [ ] **Step 2.F.2**

#### Step 2.F.3: Full plugin test suite

**Precondition check:** Before running this step, verify Groups G + H landed by `git log --oneline -5` (or `jj log -r @-` ancestry) and confirming the two adoption commits ("feat(core-communication): adopt register_emit_type" and "feat(core-objects): adopt register_emit_type") are present on the current branch. If either is missing, STOP — execute the missing group(s) first. The two adoption type-lists (8 for core-communication, 5 for core-objects) are sourced from Step 1.2's verified count; any deviation MUST trigger a Task 2 rewind.

Run: `task test -- ./internal/plugin/ ./pkg/plugin/ ./internal/plugin/hostfunc/ ./internal/plugin/lua/ ./internal/plugin/goplugin/`

Expected: PASS, because Groups G + H already adopted `register_emit_type` for the two real plugins with non-empty `crypto.emits`. The validator now active fires against already-matching sets.

**If you see `EVENT_TYPE_REGISTRY_MISMATCH` errors here against `core-communication` or `core-objects`**: the precondition check missed something. STOP, verify Groups G/H, re-run.

- [ ] **Step 2.F.3**

#### Step 2.F.4: Lint + commit

Commit:

```text
feat(plugin/manager): wire INV-S5 validator fail-closed (jg9b.3)

Group F: validator active in manager.loadPlugin. Plugins with
non-empty crypto.emits whose code-registered set differs from manifest
fail load with EVENT_TYPE_REGISTRY_MISMATCH. Groups G+H (Lua plugin
adoptions) already landed, so this commit doesn't regress real plugins.
```

- [ ] **Step 2.F.4**

---

### Group I: Parity test + final verification

#### Step 2.I.1: Parity test

Create `internal/plugin/goplugin/manager_parity_test.go` (file is `package goplugin` so it can use the in-package `newMockHost` and `mockGRPCPluginClient` directly — no exported wrappers needed; eliminates the cross-package visibility issues plan-reviewer R3 caught).

Test: `TestManager_INVS5_ParityAcrossRuntimes`. Three scenarios × 2 runtimes (Lua + binary) = 6 subtests:

| Scenario | declared | registered | Expected |
|----------|----------|------------|----------|
| match | `[a, b]` | `[a, b]` | `LoadAll` succeeds |
| declared-but-unregistered | `[a, b]` | `[a]` | `LoadAll` fails with `EVENT_TYPE_REGISTRY_MISMATCH` |
| registered-but-undeclared | `[a]` | `[a, b]` | `LoadAll` fails with `EVENT_TYPE_REGISTRY_MISMATCH` |

Lua runtime: synthesize plugin under `t.TempDir()`, construct manager with `plugins.WithLuaHost(pluginlua.NewHostWithFunctions(hostfunc.New(nil)))`, call `LoadAll`. Assert via `errutil.AssertErrorCode`.

Binary runtime: call `newMockHost(t)` directly (same package); configure the inner `mockGRPCPluginClient.setInitResponse(...)` (via Step 2.D.4's mock extension) to return the scenario's `registered` set; synthesize a binary plugin manifest with the scenario's `declared` set; construct manager; call `mgr.RegisterHost(plugins.TypeBinary, binaryHost)` (no `WithBinaryHost` option exists; the registration method is on `*Manager` at `manager.go:280`); call `LoadAll`; assert.

Pattern reference: existing `internal/plugin/communication_integration_test.go:34-87` for the synthetic-plugin tempdir harness; `internal/plugin/goplugin/host_test.go:176-186` for `newMockHost`.

Run: PASS — 6 subtests.

- [ ] **Step 2.I.1**

#### Step 2.I.2: Full pr-prep

Run: `task pr-prep`. Expected: GREEN. Mandatory per CLAUDE.md.

- [ ] **Step 2.I.2**

#### Step 2.I.3: Lint + commit + close jg9b.3

Commit:

```text
test(plugin): INV-S5 parity test across Lua + binary runtimes (jg9b.3)

Final group of jg9b.3. Parity test in package goplugin (in-package
access to mock harness; eliminates cross-package visibility issues).
3 scenarios × 2 runtimes = 6 subtests covering INV-M7/INV-S3 parity.

task pr-prep green; jg9b.3 work complete.
```

```bash
bd close holomush-jg9b.3 --reason="Substrate cap + plugin adoptions landed atomically; fail-closed INV-S5 active; parity test passes; pr-prep green."
```

- [ ] **Step 2.I.3**

---

## Task 3: Documentation — substrate-contract orientation page

**bd:** `holomush-jg9b.4`

**Goal:** Contributor-facing orientation page at `site/docs/extending/substrate-contract.md`.

### Step 3.1: Inspect existing extending/ structure

`ls site/docs/extending/`. Note naming conventions + nav structure.

- [ ] **Step 3.1**

### Step 3.2: Draft the page

Sections:

1. What the substrate contract is (pointer to canonical spec)
2. Substrate primitives plugin authors can rely on (summary table from spec §1)
3. Plugin-boundary rule INV-S1 (plugin PRs touch only `plugins/<name>/` + approved `pkg/plugin/*`)
4. Manifest emit-type validation INV-S5 — what plugin authors must do:
   - Declare types in `plugin.yaml::crypto.emits`
   - Lua: call `holomush.register_emit_type(<type>)` at top level for every type
   - Binary: implement `pluginsdk.EmitTypeRegistrar` interface
   - Top-level idempotency note for Lua
5. eventkit + groupkit SDKs (named, not yet built per INV-S7)
6. References (specs, ADRs)

Target ~150 lines. Canonical detail stays in the specs.

- [ ] **Step 3.2**

### Step 3.3: Lint + build + commit + close

Run `rumdl check`, `task docs:build`. If both pass:

```text
docs(extending): add substrate-contract orientation page (jg9b.4)

Contributor on-ramp for INV-S1 plugin boundary, INV-S5 manifest
emit-type validation (Lua + binary adoption), and named-not-yet-built
eventkit/groupkit SDKs. Canonical detail in the two superpowers/specs.
```

```bash
bd close holomush-jg9b.4 --reason="Orientation page shipped."
```

- [ ] **Step 3.3**

---

## Task 4: Roadmap update

**bd:** `holomush-jg9b.5`

**Goal:** Update `docs/roadmap.md`'s `theme:social-spaces` section to reflect substrate-contract landing.

### Step 4.1: Read and update

Locate `theme:social-spaces` section. Add a "Substrate-contract" subsection referencing both specs; mark INV-S5 substrate work shipped under `jg9b`; note Phase 4 (`5rh.13`) and channels rework (`0sc.12`) both unblocked.

- [ ] **Step 4.1**

### Step 4.2: Lint + commit + close

```text
docs(roadmap): update theme:social-spaces with substrate-contract (jg9b.5)
```

```bash
bd close holomush-jg9b.5 --reason="Roadmap reflects substrate-contract + INV-S5 landing."
```

- [ ] **Step 4.2**

---

## Task 5: Bead hygiene

**bd:** `holomush-jg9b.6`

**Goal:** Propagate spec references + dep edges to affected beads. No file changes (except Step 5.4b parent-spec amendment).

### Step 5.1: Add dep edges

Mechanism spec §6 re-cite: parent spec's old `jg9b.4` now corresponds to `jg9b.3` (substrate cap + adoptions). Wire dep edges:

```bash
bd dep add holomush-5rh.13 holomush-jg9b.3
bd dep add holomush-0sc.12 holomush-jg9b.3
```

- [ ] **Step 5.1**

### Step 5.2: Add bd notes to affected beads

Sequentially (no parallel `bd note` per `feedback_bd_create_no_parallel`):

```bash
bd note holomush-5rh.13 "Substrate-contract + INV-S5 mechanism specs landed. Phase 4 brainstorm will populate core-scenes' crypto.emits with scene_ic/scene_ooc/etc. AND implement pluginsdk.EmitTypeRegistrar. Unblocked by jg9b.3."

bd note holomush-5rh.14 "Substrate-contract spec landed. Phase 5 (focus model + multi-connection visibility) binds to spec §4.1 + §1.4."

bd note holomush-5rh.15 "Substrate-contract spec landed. Phase 6 brainstorm decides: publication-artifact rename, OriginLocationID/PublishVote reinstate, INV-S9 hard privacy boundary preservation."

bd note holomush-0sc.12 "Substrate-contract spec landed; INV-S5 mechanism shipped. Channels rework is the N=2 validating consumer for eventkit + groupkit per spec §4.2 + INV-S7. Brainstorm MUST produce a '## SDK primitive validation' section. Unblocked by jg9b.3."

bd note holomush-djj "Substrate-contract spec landed. Forums uses eventkit ONLY (NOT groupkit) per spec §4.3 + INV-S10."

bd note holomush-aqq "Substrate-contract spec landed. Discord is a bridge plugin: groupkit forbidden (INV-S10); eventkit/replay permitted conditionally."

bd note holomush-5rh.9 "Substrate-contract spec §4.3 says forums is OUT of theme-wide SDK scope. Reparenting decision deferred to djj brainstorm."
```

Verify notes via `bd show <id>` spot-checks.

- [ ] **Step 5.2**

### Step 5.3: Sync bd state

`bd dolt push`.

- [ ] **Step 5.3**

### Step 5.4b: Amend parent spec for bead-id drift

Mechanism spec §6 said the parent spec needs amendment for stale `jg9b.N` references. Run `rg -n "jg9b\.(1|4|7)" docs/superpowers/specs/2026-05-16-social-spaces-substrate-contract.md`. Expected hits: lines 48 (INV-S5 row), 113 (§1.2 prose), 430 (§3.4), 534 (§4.2), 693 (§7.1 diagram), 717 + 721 (§7.2). Update each to the new numbering: `jg9b.1` = INV-S5 design bead; `jg9b.2` = audit; `jg9b.3` = substrate + adoptions; `jg9b.4` = docs; `jg9b.5` = roadmap; `jg9b.6` = hygiene.

Run `rumdl check`. Commit:

```text
docs(specs): amend parent substrate-contract for materialized bead chain (jg9b.6)

Brings parent spec §7.1/§7.2 and prose references in line with the
actual bead chain (jg9b.1 = mechanism design; jg9b.2-jg9b.6 = work
beads). Documentation hygiene.
```

- [ ] **Step 5.4b**

### Step 5.5: Close jg9b.6

```bash
bd close holomush-jg9b.6 --reason="Hygiene complete: dep edges added; 7 affected beads noted; parent spec bead-id drift amended."
```

- [ ] **Step 5.5**

---

## Post-implementation checklist

- [ ] All 5 child beads (`jg9b.2`–`jg9b.6`) closed
- [ ] Epic `jg9b` rolls up 5/5 closed
- [ ] `task pr-prep` green (Step 2.I.2)
- [ ] PR opened with link to both specs + epic
- [ ] `pr-review-toolkit:review-pr` runs on the PR
- [ ] After merge: `bd dolt push`

## Follow-up beads (out of scope; file as separate beads when relevant)

- `plan-reviewer` memory update for INV-S7's `## SDK primitive validation` artifact-check rule.
- Parity-test template establishment as a project-wide convention.
- Binary plugin Prometheus metrics infrastructure (parent spec §11.1 STILL OPEN).
- `task lint:plugin-boundary` CI predicate to mechanically enforce INV-S1 (per ADR `holomush-z1e7`).
- Qualified-vs-unqualified emit-type drift: runtime emits use `core-communication:say` (qualified) while manifest declares `say` (unqualified); `LookupEmitSensitivity` does literal string compare and silently falls through. INV-S5 does not catch this (different drift class). File a follow-up to normalize the qualifier at one boundary OR update `LookupEmitSensitivity` to match both forms.
- Lua top-level idempotency lint check (future `task lint:plugin-top-level-idempotency`).
