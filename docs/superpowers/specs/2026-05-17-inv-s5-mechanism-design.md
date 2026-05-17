# INV-S5 Mechanism Design

## Status

**DRAFT** — pending `design-reviewer` verdict.

**Tracking bead:** `holomush-jg9b.1` (child of parent substrate-contract design bead `holomush-jg9b`).

**Parent spec:** [`docs/superpowers/specs/2026-05-16-social-spaces-substrate-contract.md`](2026-05-16-social-spaces-substrate-contract.md) — defines INV-S5 as an invariant; this spec settles the runtime mechanism.

**Authors:**

- Sean Brandt
- Claude (collaborator)

**Date:** 2026-05-17

---

## Context

The parent substrate-contract spec (READY as of 2026-05-16) mandated INV-S5: a startup-time validator that catches both failure modes the runtime `event_emitter.go::Emit` gate misses — declared-but-unregistered (dead manifest entries) and registered-but-undeclared (silently plaintext emits). The parent spec named the API surface (`RegisterEmitType(string)`) but did not specify HOW the substrate learns the code-registered set from running plugins.

Plan-reviewer round 1 of the parent's implementation plan surfaced this gap:

- **Lua host has no persistent init phase.** [`internal/plugin/lua/host.go:147,153`](../../internal/plugin/lua/host.go) does syntax-check in a throwaway state; [`host.go:198,209,213`](../../internal/plugin/lua/host.go) creates a fresh state per `DeliverEvent` and registers hostFuncs per delivery. There is no place a top-level `holomush.register_emit_type(...)` accumulation would persist for the substrate to read.
- **Binary plugins are out-of-process.** [`internal/plugin/goplugin/host.go:420,528`](../../internal/plugin/goplugin/host.go) spawns the plugin via `exec.Command` and communicates over gRPC. Host-side Go method calls on a plugin struct (`*scenePlugin`) are unreachable; any "expose registry to host" path requires a proto extension or a new RPC.

This spec settles both — symmetrically — using the existing plugin Init lifecycle.

## RFC2119 Keywords

The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY are used per RFC2119.

## Goals

- **MUST** specify a runtime mechanism by which the substrate learns the code-registered emit-type set for each plugin, for both Lua and binary runtimes.
- **MUST** allow the validator to fail plugin load on mismatch (fail-closed from day one).
- **MUST** preserve INV-S3 Go+Lua parity: every primitive ships both runtimes together.
- **MUST** scope INV-S5 to plugins with non-empty `crypto.emits`; plugins without it are unchanged.
- **MUST** specify both failure-mode detection directions: declared-but-unregistered AND registered-but-undeclared.
- **SHOULD** minimize new substrate surface — reuse existing lifecycle hooks where possible.

## Non-goals

- Validation of `crypto.emits` sensitivity values (existing [`internal/plugin/crypto_validator.go::ValidateCrypto`](../../internal/plugin/crypto_validator.go) handles syntax + sensitivity enum validity).
- Validation of host-owned event subjects (system events; not subject to INV-S5).
- Re-running validation on plugin reload (Load-time only).
- Static analysis of Lua source (the Load pass IS dynamic registration; no separate lint).
- Phase 4's specific `crypto.emits` sensitivity matrix for scenes (settled in Phase 4 brainstorm).
- Phased rollout / no-op default / backward-compat shims. Per `feedback_no_prod_shape_for_undeployed`: no releases, no external users, no need for transitional modes — substrate and plugin adoptions land in a single coherent change, fail-closed from day one.

## Invariants

| # | Invariant | Enforcement |
|---|-----------|-------------|
| INV-M1 | INV-S5 SHALL apply only to plugins with non-empty `manifest.Crypto.Emits`. Plugins without `crypto.emits` SKIP the Load-pass + validation entirely. | `manager.go::loadPlugin` checks `manifest.Crypto != nil && len(manifest.Crypto.Emits) > 0` before invoking validator |
| INV-M2 | The code-side registry SHALL contain ALL plugin-owned event types the plugin may emit (not just sensitive ones). Host-owned types (e.g., `pluginsdk.HostEventTypeSystem`) MUST NOT be registered. | SDK + hostfunc surface accepts any string; substrate filters host-owned types before comparison (filter list maintained centrally) |
| INV-M3 | Binary plugins with non-empty `crypto.emits` MUST implement `pluginsdk.EmitTypeRegistrar` and populate `pluginv1.InitResponse.registered_emit_types` (new proto field 2). Mismatch fails plugin load. | SDK adapter `pkg/plugin/sdk.go:152 pluginServerAdapter.Init` auto-populates from `EmitTypeRegistrar.EmitRegistry()` |
| INV-M4 | Lua plugins with non-empty `crypto.emits` MUST call `holomush.register_emit_type(<type>)` at top level for every emit type they may produce. The Load-pass captures these calls; missing registrations fail plugin load. | `internal/plugin/lua/host.go::Load` second pass; `internal/plugin/hostfunc/stdlib_emit_registry.go` |
| INV-M5 | The validator SHALL fire in `internal/plugin/manager.go::loadPlugin` AFTER `host.Load` returns successfully and BEFORE the plugin is added to the manager's plugin cache as ready. | `Host.PluginEmitRegistry(name) ([]string, bool)` interface method; validator call in `loadPlugin` post-`host.Load` |
| INV-M6 | Lua Load-pass `DoString` errors SHALL fail plugin load (same shape as the existing syntax-check error). | `lua/host.go::Load` returns wrapped error from the second-pass `DoString` |
| INV-M7 | Every primitive in this design SHALL ship Go SDK + Lua hostfunc + parity test together (per parent spec INV-S3). | Single PR / coordinated change; parity test exercises both runtimes with identical logical scenarios |

---

## 1. Binary side mechanism

### 1.1 Proto extension

Modify `api/proto/holomush/plugin/v1/plugin.proto`:

```proto
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

Generated bindings auto-update at `pkg/proto/holomush/plugin/v1/plugin.pb.go` via existing proto regeneration (`task proto`).

### 1.2 SDK API

Create `pkg/plugin/emit_registry.go`:

```go
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

// EmitTypeRegistrar is the interface a binary plugin implements to
// expose its EmitRegistry to the host via InitResponse.
//
// Plugins with non-empty crypto.emits in their manifest MUST implement
// this interface; the substrate validator fails load on mismatch.
// Plugins without crypto.emits are out of INV-S5 scope and may skip.
type EmitTypeRegistrar interface {
    EmitRegistry() *EmitRegistry
}
```

### 1.3 SDK adapter wiring

Modify `pkg/plugin/sdk.go:152 pluginServerAdapter.Init`. After delegating to the provider's Init (existing behavior), check the `EmitTypeRegistrar` opt-in and populate the response:

```go
// At the end of Init, after the provider's Init returns:
resp := &pluginv1.InitResponse{
    ProvidedServices: providedServices,
}
if registrar, ok := a.serviceProvider.(EmitTypeRegistrar); ok {
    resp.RegisteredEmitTypes = registrar.EmitRegistry().RegisteredEmitTypes()
}
return resp, nil
```

Plugins that do not implement `EmitTypeRegistrar` return empty `RegisteredEmitTypes` — INV-M3 mandates implementation for crypto.emits-declaring plugins; the validator catches non-implementation as a mismatch.

---

## 2. Lua side mechanism

### 2.1 luaPlugin extension

Modify `internal/plugin/lua/host.go:33`:

```go
type luaPlugin struct {
    manifest      *plugins.Manifest
    code          string
    emitRegistry  []string  // INV-S5: populated during Load second pass.
                            // nil when manifest.Crypto.Emits is empty (skipped).
}
```

### 2.2 Load second pass

Modify `internal/plugin/lua/host.go::Load` (starts at line 111). After the existing syntax-check throwaway state (lines 147-155) and before storing the `luaPlugin` (line 157), add a second pass when `manifest.Crypto != nil && len(manifest.Crypto.Emits) > 0`:

```go
// Existing syntax-check pass (unchanged) ...

var emitRegistry []string
if manifest.Crypto != nil && len(manifest.Crypto.Emits) > 0 {
    // INV-S5 Load-pass: run top-level code in a stateful state to capture
    // holomush.register_emit_type calls. Per INV-M4, Lua plugins with
    // non-empty crypto.emits MUST call register_emit_type at top level
    // for every emit type they produce.
    L2, err := h.factory.NewState(ctx)
    if err != nil {
        return oops.In("lua").With("plugin", manifest.Name).With("operation", "load_inv_s5_pass").
            Hint("failed to create INV-S5 capture state").Wrap(err)
    }
    defer L2.Close()

    reg := hostfunc.NewLuaEmitRegistry()
    if h.hostFuncs != nil {
        h.hostFuncs.RegisterWithEmitCapture(L2, manifest.Name, reg, manifest.Requires...)
    }

    if err := L2.DoString(string(code)); err != nil {
        return oops.In("lua").With("plugin", manifest.Name).With("operation", "load_inv_s5_pass").
            With("entry", manifest.LuaPlugin.Entry).
            Hint("INV-S5 Load-pass execution error").Wrap(err)
    }

    emitRegistry = reg.Types()
}

h.plugins[manifest.Name] = &luaPlugin{
    manifest:     manifest,
    code:         string(code),
    emitRegistry: emitRegistry,
}
```

### 2.3 New hostfunc

Create `internal/plugin/hostfunc/stdlib_emit_registry.go`:

```go
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

// RegisterEmitTypeFuncs installs holomush.register_emit_type(type) on the
// given module table; calls append to reg.
//
// Usage: only called via the Functions.RegisterWithEmitCapture path during
// the Lua Host's INV-S5 Load-pass. The standard per-delivery Functions.Register
// path does NOT install this hostfunc — register_emit_type is undefined in
// per-delivery Lua states (no-op-by-absence; calling it raises a Lua error,
// which is the desired behavior since registrations are Load-time-only).
func RegisterEmitTypeFuncs(ls *lua.LState, mod *lua.LTable, reg *LuaEmitRegistry) {
    ls.SetField(mod, "register_emit_type", ls.NewFunction(func(ls *lua.LState) int {
        eventType := ls.CheckString(1)
        reg.add(eventType)
        ls.Push(lua.LTrue)
        return 1
    }))
}
```

### 2.4 Functions.RegisterWithEmitCapture entry point

Modify `internal/plugin/hostfunc/functions.go` (existing `Register` at line 139 is unchanged). Add a new entry point:

```go
// RegisterWithEmitCapture is the variant of Register used during the Lua
// Host's INV-S5 Load-pass. Identical to Register, but ALSO installs
// holomush.register_emit_type which appends to reg. The standard Register
// path does NOT install register_emit_type — see RegisterEmitTypeFuncs godoc.
func (f *Functions) RegisterWithEmitCapture(
    ls *lua.LState,
    pluginName string,
    reg *LuaEmitRegistry,
    requires ...string,
) {
    f.Register(ls, pluginName, requires...)
    // Get the holomush module table that Register just installed:
    if mod, ok := ls.GetGlobal("holomush").(*lua.LTable); ok {
        RegisterEmitTypeFuncs(ls, mod, reg)
    }
}
```

---

## 3. Validator + Host interface extension

### 3.1 Host interface method

Modify `internal/plugin/host.go` to add a new method on the `Host` interface:

```go
type Host interface {
    // ... existing methods unchanged ...

    // PluginEmitRegistry returns the code-registered emit-type set for a
    // loaded plugin, captured during Load. Returns:
    //   - (set, true)  : plugin loaded and opted into INV-S5 (non-empty crypto.emits)
    //   - (nil, true)  : plugin loaded; INV-S5 not applicable (empty crypto.emits)
    //   - (nil, false) : plugin not loaded under this Host
    //
    // Substrate uses the (set, true) case to run set-equality validation
    // against manifest.Crypto.Emits.
    PluginEmitRegistry(name string) ([]string, bool)
}
```

**Lua implementation** (`internal/plugin/lua/host.go`):

```go
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

**Binary implementation** (`internal/plugin/goplugin/host.go`): cache `InitResponse.RegisteredEmitTypes` on `loadedPlugin` (existing struct) during `Load`'s `pluginClient.Init` call (line 528 area). Add accessor:

```go
type loadedPlugin struct {
    manifest             *plugins.Manifest
    client               *hashiplug.Client
    registeredEmitTypes  []string  // populated from InitResponse.RegisteredEmitTypes
    // ... existing fields ...
}

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

### 3.2 Validator

Create `internal/plugin/emit_type_validator.go`:

```go
package plugins

import "sort"

// EmitTypeMismatch describes the diff between a plugin's manifest-declared
// crypto.emits set and the SDK-registered emit-type set.
type EmitTypeMismatch struct {
    DeclaredButUnregistered []string
    RegisteredButUndeclared []string
}

func (m EmitTypeMismatch) HasMismatch() bool {
    return len(m.DeclaredButUnregistered) > 0 || len(m.RegisteredButUndeclared) > 0
}

// ValidateEmitTypeSetEquality compares the manifest-declared emit-type set
// against the SDK-registered emit-type set. Per INV-S5, the two sets MUST
// be equal in both directions.
func ValidateEmitTypeSetEquality(declared, registered []string) EmitTypeMismatch {
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
    return mismatch
}

func toSet(s []string) map[string]struct{} {
    out := make(map[string]struct{}, len(s))
    for _, v := range s {
        out[v] = struct{}{}
    }
    return out
}
```

### 3.3 Manager wiring

Modify `internal/plugin/manager.go::loadPlugin` (function starts at line 849). After `host.Load(...)` returns successfully (line 989) and BEFORE the plugin is added to the manager's plugin cache:

```go
// INV-S5: manifest emit-type startup validation. Scope per INV-M1:
// only plugins with non-empty crypto.emits participate.
if dp.Manifest.Crypto != nil && len(dp.Manifest.Crypto.Emits) > 0 {
    registered, ok := host.PluginEmitRegistry(dp.Manifest.Name)
    if !ok {
        // Host loaded the plugin but cannot report registry: programming error.
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

Helper:

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

---

## 4. Rollout

Per `feedback_no_prod_shape_for_undeployed` and confirmed by user 2026-05-17: HoloMUSH has no releases, no external users, and all plugins are in-tree. The substrate cap and both plugin adoptions land in a single coherent change (one PR or coordinated PRs that merge together). Fail-closed from day one. No no-op-default phase.

### 4.1 Audit precondition

Before the change merges: enumerate all in-tree plugin manifests and verify which declare `crypto.emits`. The known set at spec time:

- `plugins/core-communication/plugin.yaml` — non-empty crypto.emits (8 types). Adopts in this change.
- `plugins/core-scenes/plugin.yaml` — empty crypto.emits. Adopts in this change (implements `EmitTypeRegistrar` with empty registry).

Other in-tree plugins (`plugins/core-aliases/`, `plugins/core-building/`, `plugins/core-help/`, `plugins/core-objects/`, `plugins/echo-bot/`, `plugins/setting-crossroads/`, `plugins/setting-skeleton/`, `plugins/test-abac-widget/`) MUST be inventoried during implementation to confirm none have non-empty `crypto.emits` that go unaddressed. Per INV-M1, plugins without `crypto.emits` need zero changes.

### 4.2 Change shape

A single PR (or 1 substrate + 2 plugin-adoption PRs that merge together) containing:

1. Proto field addition to `pluginv1.InitResponse` + regenerated bindings.
2. `pkg/plugin/emit_registry.go` (SDK API + `EmitTypeRegistrar` interface).
3. `pkg/plugin/sdk.go` modification (adapter populates `InitResponse.RegisteredEmitTypes`).
4. `internal/plugin/lua/host.go` modification (Load second pass + `luaPlugin.emitRegistry`).
5. `internal/plugin/hostfunc/stdlib_emit_registry.go` (new Lua hostfunc + `LuaEmitRegistry`).
6. `internal/plugin/hostfunc/functions.go` (new `RegisterWithEmitCapture` entry point).
7. `internal/plugin/host.go` interface extension (`PluginEmitRegistry` method).
8. `internal/plugin/lua/host.go` + `internal/plugin/goplugin/host.go` interface implementations.
9. `internal/plugin/emit_type_validator.go` (validator).
10. `internal/plugin/manager.go::loadPlugin` modification (validator call after `host.Load`).
11. `plugins/core-communication/main.lua` modification (8 top-level `holomush.register_emit_type` calls).
12. `plugins/core-scenes/main.go` modification (implements `EmitTypeRegistrar` with empty registry; field added to `scenePlugin` struct).
13. Tests per §5.

---

## 5. Testing strategy

### 5.1 Unit tests

| Package | Tests |
|---------|-------|
| `pkg/plugin/emit_registry_test.go` | RegisterEmitType / RegisterEmitTypes / RegisteredEmitTypes / duplicate-idempotency / empty-default |
| `internal/plugin/emit_type_validator_test.go` | matching sets / declared-but-unregistered / registered-but-undeclared / both directions / empty-vs-empty |
| `internal/plugin/hostfunc/stdlib_emit_registry_test.go` | hostfunc accumulation / non-string arg rejection / duplicate-idempotency |

### 5.2 Lua host integration test

Add to `internal/plugin/lua/host_test.go` (or create a new test file):

- **Pass case:** load a synthetic Lua plugin with manifest `crypto.emits: [a, b]` and code `holomush.register_emit_type("a"); holomush.register_emit_type("b")`. Verify Load succeeds; `PluginEmitRegistry` returns `[a, b]`.
- **Mismatch — declared-but-unregistered:** manifest `crypto.emits: [a, b]` but code registers only `a`. Verify validator returns mismatch with `DeclaredButUnregistered: [b]`.
- **Mismatch — registered-but-undeclared:** manifest `crypto.emits: [a]` but code registers `a` and `b`. Verify validator returns mismatch with `RegisteredButUndeclared: [b]`.
- **No crypto.emits:** plugin without `crypto.emits` block; Load second pass SHOULD NOT fire; `PluginEmitRegistry` returns `(nil, true)`.
- **DoString error in Load pass:** plugin's top-level code intentionally throws; Load returns error wrapped with `load_inv_s5_pass` operation.

### 5.3 Binary plugin integration test

Add to `internal/plugin/goplugin/host_test.go` (or analogous test file):

Use a test stub plugin (similar to existing test fixtures under `internal/plugin/`) that:

- Implements `EmitTypeRegistrar` with a known set.
- Has manifest with non-empty `crypto.emits`.

Verify same matrix: pass / declared-but-unregistered / registered-but-undeclared / empty / not-implementing-interface-with-non-empty-crypto-emits (mismatch).

### 5.4 Manager wiring integration test

Add to `internal/plugin/manager_test.go`:

- `TestManager_LoadPlugin_EmitTypeMismatch_FailsClosed` — plugin with manifest/code mismatch fails `loadPlugin` with `EVENT_TYPE_REGISTRY_MISMATCH` oops code.
- `TestManager_LoadPlugin_EmitTypeMatch_Succeeds` — plugin with matching sets loads successfully.
- `TestManager_LoadPlugin_NoCryptoEmits_SkipsValidation` — plugin without `crypto.emits` loads without validator firing.

### 5.5 Parity test (per INV-M7 / parent INV-S3)

Add a parity test that exercises the SAME logical scenario (manifest declares `[a, b]`, code registers `[a, b]`) through BOTH a Lua plugin path and a binary plugin path; assert identical validator output. Failure mode is also exercised symmetrically.

This test lives at `internal/plugin/inv_s5_parity_test.go` (new file) so the parity assertion is co-located with the substrate validator.

---

## 6. Relationship to parent spec & bead chain

This design's READY verdict unblocks the parent spec's implementation work. The parent spec's bead chain (originally `jg9b.1`-`jg9b.7`) needs renumbering — `jg9b.1` is now this design bead.

Updated parent bead chain after this design lands READY (collapsed per `feedback_no_prod_shape_for_undeployed`):

| Bead | Title |
|---|---|
| `jg9b.2` | Substrate + plugin adoptions (single coherent change: proto, SDK, validator, Lua host, hostfunc, manager wiring, core-communication adopt, core-scenes adopt; fail-closed) |
| `jg9b.3` | Audit: enumerate in-tree plugins, confirm none with `crypto.emits` go unaddressed |
| `jg9b.4` | Docs: substrate-contract orientation page in `site/docs/extending/` |
| `jg9b.5` | Roadmap: update `theme:social-spaces` section in `docs/roadmap.md` |
| `jg9b.6` | Bead hygiene: notes + dep edges on `5rh.13`, `5rh.14`, `5rh.15`, `0sc.12`, `djj`, `aqq`, `5rh.9` |

The parent spec's plan is re-written after this design lands READY; that re-write materializes the new chain via `plan-to-beads`.

---

## 7. Areas needing deeper design

None blocking. Two minor items deferred:

| Area | Why deferred |
|------|--------------|
| Host-owned event type filter list location | INV-M2 says host-owned types are excluded from validation. The list (currently just `pluginsdk.HostEventTypeSystem`) lives in `pkg/plugin/event.go` constants. Implementation can reference these directly; no new central registry needed unless the list grows. |
| Parity-test pattern as project-wide convention | INV-M7 specifies parity testing for this design; the broader pattern (formal "parity test" convention for all Go+Lua hostfunc pairs) is a separate substrate-infra concern. File as a future bead if/when more SDK primitives land. |

---

## 8. References

### Within the repository

- [`docs/superpowers/specs/2026-05-16-social-spaces-substrate-contract.md`](2026-05-16-social-spaces-substrate-contract.md) — parent substrate-contract spec defining INV-S5.
- [`docs/adr/holomush-3vsb-manifest-emit-type-startup-validation.md`](../../adr/holomush-3vsb-manifest-emit-type-startup-validation.md) — ADR for INV-S5.
- [`.claude/rules/plugin-runtime-symmetry.md`](../../.claude/rules/plugin-runtime-symmetry.md) — Go + Lua parity invariant (parent spec INV-S3).
- [`.claude/rules/plugin-manifest.md`](../../.claude/rules/plugin-manifest.md) — manifest field reference.

### Working precedents cited

- [`pkg/plugin/focus_client.go`](../../pkg/plugin/focus_client.go) + [`internal/plugin/hostfunc/stdlib_focus.go`](../../internal/plugin/hostfunc/stdlib_focus.go) — Go + Lua parity template the new emit-registry primitives follow.
- [`internal/plugin/crypto_manifest.go:14-21`](../../internal/plugin/crypto_manifest.go) — sensitivity enum (`always`/`may`/`never`).
- [`internal/plugin/sensitivity_fence.go:23-48`](../../internal/plugin/sensitivity_fence.go) — runtime truth table this validator complements.
- [`internal/plugin/lua/host.go:111`](../../internal/plugin/lua/host.go) — Lua Host.Load (target of the second-pass modification).
- [`internal/plugin/goplugin/host.go:528`](../../internal/plugin/goplugin/host.go) — binary Init RPC call site (target of registry capture).
- [`internal/plugin/manager.go:849,989`](../../internal/plugin/manager.go) — loadPlugin + host.Load call site (target of validator wiring).
- [`pkg/plugin/sdk.go:152`](../../pkg/plugin/sdk.go) — pluginServerAdapter.Init (target of InitResponse population).

---

## Document history

| Date | Action | Notes |
|------|--------|-------|
| 2026-05-17 | DRAFT authored | Brainstorming session under bead `holomush-jg9b.1`; closes mechanism gap surfaced by parent-plan-reviewer round 1 |
