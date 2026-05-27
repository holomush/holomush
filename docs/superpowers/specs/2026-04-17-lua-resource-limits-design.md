<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Lua Plugin Resource Limits Design

**Status:** Draft
**Date:** 2026-04-17
**Author:** seanb4t (via Claude Opus 4.7)
**Bead:** holomush-u9p5 (P0, security-finding)
**Related:** holomush-abbg (PR #229, telnet DoS hardening — landed 2026-04-17, established the per-invocation-timeout + watchdog pattern this spec applies to Lua)

## RFC2119 Keywords

| Keyword        | Meaning                                    |
| -------------- | ------------------------------------------ |
| **MUST**       | Absolute requirement                       |
| **MUST NOT**   | Absolute prohibition                       |
| **SHOULD**     | Recommended, may ignore with justification |
| **SHOULD NOT** | Not recommended                            |
| **MAY**        | Optional                                   |

## Problem

Lua plugin execution has no CPU, memory, or stack bounds. A malicious or buggy plugin can:

1. **Burn CPU indefinitely.** A tight `while true do end` in `on_event` runs until the Go goroutine is killed — normally never, because nothing kills it.
2. **Exhaust process memory.** A table-allocation bomb (`while true do t[#t+1]={} end`) grows the Go heap unbounded, OOMing the core server.
3. **Block dispatcher threads.** When a plugin invokes a hostfunc (registered from Go), gopher-lua's context check is suspended until the hostfunc returns. A pathological hostfunc hangs the dispatcher along with the plugin.

Today the core calls `L.SetContext(ctx)` (`internal/plugin/lua/host.go:173-180`) but the `ctx` passed in is the caller's context with no per-invocation deadline — if the caller's context has no timeout, Lua execution has no timeout.

## gopher-lua API research (informs the design)

Verified against `gopher-lua@v1.1.1` before this spec was written. Selected findings that shape the design:

- **`CallStackSize` default is 256.** The bead's original item "(1) Set CallStackSize=256" is a no-op against the current default. No change needed.
- **Context IS checked at every VM instruction when `SetContext` is set.** `mainLoopWithContext` (`vm.go:37-65`) has a `select` on `L.ctx.Done()` on every instruction, not only at function-call boundaries as the bead implied. A pure-Lua tight loop MUST cancel promptly if context expiry fires.
- **gopher-lua has no instruction-count hook API.** `SetMx` is a memory monitor that calls `os.Exit(3)` — crashes the entire process — and is unsuitable. The bead's original item "(2) Add instruction count hook" is not implementable; the per-instruction context check serves the same purpose.
- **gopher-lua has no native memory cap.** `RegistryMaxSize` on `lua.Options` bounds the Lua value registry; overflow causes a panic that `CallByParam(Protect: true)` catches and returns as an error. This is the closest primitive to a memory cap.
- **Host functions block context checks.** A Go function registered via `NewFunction` runs to completion even after context expiry. Defensive hostfunc audit is required.
- **`L.Close()` concurrent with in-flight `CallByParam` is undocumented.** The design MUST NOT rely on it as a kill switch.

## Scope

**In scope:**

1. Per-invocation CPU deadline at every plugin dispatcher entry point (A).
2. Lua registry cap via `RegistryMaxSize` (B).
3. Hostfunc audit for context-awareness (C).
4. Non-blocking dispatcher pattern: wrap `CallByParam` in a goroutine so a stuck hostfunc can't hang the dispatcher (D).

**Out of scope:**

- External `runtime.MemStats`-polling memory watchdog. Process-wide, coarse, attribution-blind; deferred unless a specific plugin class demands it.
- Instruction-count-based CPU cap. Not achievable with gopher-lua v1.1.1.
- Concurrent-safe `L.Close()` from a watchdog goroutine. Undocumented; not relied on.
- Binary-plugin resource limits. Binary plugins run in separate processes with their own OS-level limits; a separate design problem.

## Design

### Four controls

| # | Control | Default | Config knob |
| - | ------- | ------- | ----------- |
| 1 | Per-invocation CPU timeout (A) | 1 s | `--plugin-lua-timeout` |
| 2 | Lua registry cap (B) | 65536 values | `--plugin-lua-registry-max` |
| 3 | Hostfunc context-awareness audit (C) | — | — (code review pass) |
| 4 | Dispatcher non-blocking drop (D) | uses control 1's timeout | — |

Defaults chosen for an interactive MUSH: 1 second is roughly an order of magnitude above any legitimate event-handler execution and an order of magnitude below the `rpcTimeout = 10 * time.Second` used elsewhere in the codebase. 65536 active Lua values is generous for any current plugin and catches bombs long before process memory is noticed.

### Registry cap (control 2)

`StateFactory` gains a `registryMaxSize int` field, plumbed from `pluginConfig.LuaRegistryMaxSize`. `NewState` applies it in `lua.Options`:

```go
func (f *StateFactory) NewState(_ context.Context) (*lua.LState, error) {
    L := lua.NewState(lua.Options{
        SkipOpenLibs:    true,
        RegistryMaxSize: f.registryMaxSize,
    })
    // ... existing library loading unchanged ...
}
```

`CallStackSize` is left unset so gopher-lua's default of 256 applies. The struct field in `lua.Options` is zero-valued; gopher-lua treats zero as "use default" for this field specifically.

### Dispatcher unification (controls 1 + 4)

The four dispatcher methods (`DeliverEvent`, `DeliverCommand`, `QuerySessionStreams`, `callOnCommand`) today each inline `L.SetContext(ctx)` + `L.CallByParam(...)`. A new private method on `Host` replaces each inline call:

```go
// invoke runs the given CallByParam under a per-invocation CPU deadline and
// a watchdog goroutine. Returns the CallByParam error, or a wrapped
// context.DeadlineExceeded if the dispatcher timed out before the
// goroutine completed.
//
// invoke waits for the goroutine to drain on the ctx.Done branch. The wait is bounded after the dispatcher
// returns — gopher-lua's per-instruction context check (SetContext) forces
// pure-Lua tight loops to exit promptly; hostfunc blocks are bounded by
// the hostfunc audit (all hostfuncs MUST respect their ctx).
func (h *Host) invoke(
    parentCtx context.Context,
    L *lua.LState,
    handler string, // for metrics labels
    p lua.P,
    args ...lua.LValue,
) error {
    ctx, cancel := context.WithTimeout(parentCtx, h.cpuTimeout)
    defer cancel()
    L.SetContext(ctx)

    done := make(chan error, 1)
    go func() {
        done <- L.CallByParam(p, args...)
    }()

    select {
    case err := <-done:
        h.recordInvocationOutcome(handler, classifyError(err))
        return err
    case <-ctx.Done():
        // Wait for the goroutine to drain. Bounded: pure-Lua tight loops
        // RaiseError on the next instruction (per-instruction ctx check);
        // hostfunc paths are bounded by the hostfunc audit invariant that
        // every registered hostfunc either is O(1) or respects L.Context().
        // Waiting ensures the state is no longer in use when the caller's
        // defer L.Close() runs (required for race-detector correctness).
        <-done
        h.recordInvocationOutcome(handler, "timeout")
        return oops.Code("PLUGIN_LUA_TIMEOUT").
            With("handler", handler).
            With("timeout", h.cpuTimeout).
            Wrap(ctx.Err())
    }
}
```

`classifyError` inspects the returned error for `RegistryMaxSize` panic signature and returns `"registry_full"`, `"error"`, or `"success"` (nil) accordingly. The classifier lives next to `invoke` so it can be unit-tested without the full dispatcher.

`handler` is one of `"on_event"`, `"on_command"`, `"on_session_subscribe"` — a small, fixed set used as a Prometheus label. Plugin name is looked up from the host context.

### Hostfunc audit (control 3)

Audit output documented in `site/docs/contributing/hostfunc-context-audit.md` (new file) with a table of each registered hostfunc, the audit finding (`OK`, `FIX`, or `BOUNDED-BY-RPC`), and any code change applied. Expected outcome: most hostfuncs are thin wrappers around world-service RPC calls that already enforce timeouts via `context`; the audit codifies this as an invariant going forward.

If any hostfunc is found to block without respecting context, a fix lands in the same PR as a targeted edit.

A meta-test `TestHostFuncsRespectContext` walks each registered hostfunc, invokes it with a pre-cancelled context, and asserts each returns within 50 ms. This locks the audit conclusion against future regressions.

### Config

Plugin subsystem config (`cmd/holomush/core.go` where `pluginConfig` lives) gains two fields:

```go
type pluginConfig struct {
    // ... existing fields ...
    LuaTimeout         time.Duration `koanf:"lua_timeout"`
    LuaRegistryMaxSize int           `koanf:"lua_registry_max_size"`
}
```

Cobra flag registration:

```go
cmd.Flags().DurationVar(&cfg.LuaTimeout, "plugin-lua-timeout", defaultPluginLuaTimeout, "per-invocation CPU deadline for Lua plugins")
cmd.Flags().IntVar(&cfg.LuaRegistryMaxSize, "plugin-lua-registry-max", defaultPluginLuaRegistryMax, "max Lua registry size per plugin state")
```

Defaults as named constants (`defaultPluginLuaTimeout = 1 * time.Second`, `defaultPluginLuaRegistryMax = 65536`).

`Validate()` rejects non-positive values with `CONFIG_INVALID`.

### Metrics

New package globals in `internal/plugin/lua/metrics.go` (mirror the pattern at `internal/telnet/metrics.go` or `internal/audit/plugin_metrics.go`):

| Metric | Type | Labels | Purpose |
| ------ | ---- | ------ | ------- |
| `holomush_plugin_lua_invocations_total` | CounterVec | `plugin`, `handler`, `outcome` | Denominator for outcome-rate dashboards |
| `holomush_plugin_lua_timeouts_total` | CounterVec | `plugin`, `handler` | CPU-cap violations; attribution by plugin+handler |
| `holomush_plugin_lua_registry_full_total` | CounterVec | `plugin`, `handler` | Memory-cap violations |

`outcome` label values: `success`, `timeout`, `registry_full`, `error`. Cardinality cap ≈ (plugins: low tens) × (handlers: single digits) × (outcomes: 4) ≤ low hundreds of series.

Registered with the observability server alongside existing command + telnet metrics.

## Error handling

| Event | Response | Logging | Metric |
| ----- | -------- | ------- | ------ |
| CPU timeout (pure-Lua runaway; ctx expiry caught by mainLoopWithContext) | `CallByParam` returns error from `L.RaiseError(ctx.Err())`; dispatcher returns this error | `warn` with plugin + handler | `timeouts_total++`, `invocations_total{outcome="timeout"}++` |
| Dispatcher reaches timeout (hostfunc or Lua blocking past timeout) | Dispatcher waits for the goroutine to drain (bounded by hostfunc audit invariant + per-instruction ctx check), then returns `oops.Code("PLUGIN_LUA_TIMEOUT")...Wrap(ctx.Err())` | `warn` with plugin + handler | Same pair as above (single invocation; not double-counted) |
| Registry overflow caught by `Protect: true` | Dispatcher returns wrapped panic-recovery error | `warn` with plugin + handler | `registry_full_total++`, `invocations_total{outcome="registry_full"}++` |
| Normal Lua error (`error("boom")`) | Dispatcher returns error from CallByParam | `debug` (existing) | `invocations_total{outcome="error"}++` |
| Happy path | Dispatcher returns nil | `debug` (existing) | `invocations_total{outcome="success"}++` |
| Config non-positive limit | Startup failure, `CONFIG_INVALID` | — | — |

Goroutine-drain invariants the design MUST uphold:

- The dispatcher MUST NOT call `L.Close()` from its `ctx.Done()` branch. Concurrent `Close` with in-flight `CallByParam` is undocumented in gopher-lua.
- The goroutine MUST NOT `L.Close()` the state it received — state ownership stays with the caller, which closes it in the existing per-event cleanup path after `CallByParam` returns (which it will, because SetContext causes the instruction check to RaiseError, or because the hostfunc returns and the subsequent instruction hits the RaiseError).
- The outer per-event `L.Close()` (existing code path) MUST run even when the dispatcher returns timeout early. This means the outer code MUST defer `L.Close()` before calling `invoke`, which is already the case.

## Testing

Unit tests use short real timeouts (≤ 200 ms) consistent with the pattern established in PR #229. No clock abstraction introduced.

### Unit tests

| Test | File | Covers |
| ---- | ---- | ------ |
| `TestNewStateRegistryMaxSizeApplied` | `internal/plugin/lua/state_test.go` | Factory with `RegistryMaxSize=1024`; script trips the limit; `CallByParam` returns panic-recovery error |
| `TestNewStateRegistryUnboundedWhenZero` | `internal/plugin/lua/state_test.go` | Factory with `RegistryMaxSize=0`; many-value script succeeds |
| `TestInvokeReturnsCallByParamResultWhenFast` | `internal/plugin/lua/host_invoke_test.go` | Baseline: script returns immediately; invoke returns nil |
| `TestInvokeSurfacesCPUTimeoutOnTightLoop` | `internal/plugin/lua/host_invoke_test.go` | `cpuTimeout=100ms`, tight loop; invoke returns timeout error within 300 ms |
| `TestInvokeReleasesDispatcherWhenHostFuncBlocks` | `internal/plugin/lua/host_invoke_test.go` | Hostfunc blocks on a channel; invoke returns within 300 ms; goroutine drains after test cleanup |
| `TestInvokeCountsOutcomeAsTimeout` | `internal/plugin/lua/host_invoke_test.go` | Timeout fires; timeouts_total++, invocations_total{outcome="timeout"}++ |
| `TestInvokeCountsOutcomeAsRegistryFull` | `internal/plugin/lua/host_invoke_test.go` | Registry overflow; registry_full_total++, invocations_total{outcome="registry_full"}++ |
| `TestInvokeCountsOutcomeAsSuccessAndError` | `internal/plugin/lua/host_invoke_test.go` | Happy path and `error("boom")` paths count correctly |
| `TestClassifyError` | `internal/plugin/lua/host_invoke_test.go` | Unit test for `classifyError`: nil → "success"; registry-panic string → "registry_full"; other → "error" |
| `TestHostFuncsRespectContext` | `internal/plugin/hostfunc/context_audit_test.go` | For each registered hostfunc, invoke with a pre-cancelled context; assert return within 50 ms. Table-driven over the registry. |
| `TestPluginConfigValidateRejectsNonPositiveLuaLimits` | `cmd/holomush/core_test.go` (or wherever pluginConfig.Validate is tested) | Zero/negative LuaTimeout and LuaRegistryMaxSize surface `CONFIG_INVALID` |
| `TestPluginCommandLuaLimitDefaults` | `cmd/holomush/core_test.go` | Flag defaults match spec (1s, 65536) |

### Integration tests

Under `//go:build integration` in `test/integration/plugin/lua_resource_limits_integration_test.go`:

| Test | Covers |
| ---- | ------ |
| `TestLuaPluginCPUBombDoesNotHangDispatcher` | Load test plugin with `on_event` = tight loop; send event; dispatcher returns timeout within `cpuTimeout + 500ms`; a subsequent event dispatches normally, proving process stayed responsive |
| `TestLuaPluginMemoryBombDoesNotOOM` | Load test plugin that allocates in a loop; send event; dispatcher returns registry_full error; `runtime.MemStats.Alloc` before/after delta `< 10 MB` |

Test plugins live under `test/integration/plugin/testdata/lua/cpu_bomb/` and `memory_bomb/` following the existing test-plugin pattern.

### Not tested

- Concurrent `L.Close()` safety — the design does not rely on it.
- Never-returning hostfunc goroutine-leak — the hostfunc audit + `TestHostFuncsRespectContext` prevents it from occurring in practice.
- gopher-lua `SetContext`'s per-instruction overhead — vendor-documented, not HoloMUSH's concern.

### PR-prep gate

`task pr-prep` MUST be green before merge — lint, format, schema, license, unit, integration, E2E. Same rule as PRs #225 / #229.

## Documentation

Two files updated:

- `site/docs/operating/plugin-security.md` (create if missing): new section "Lua resource limits" documenting the four controls, two flags, tuning guidance, and the three metrics.
- `site/docs/contributing/hostfunc-context-audit.md` (new): the audit table + invariant that all new hostfuncs MUST respect their context parameter.

## Risks

| Risk | Likelihood | Mitigation |
| ---- | ---------- | ---------- |
| Default `LuaTimeout=1s` too tight for a plugin that does a DB-backed hostfunc chain | Low | Operator raises the flag; audit outcome documents expected hostfunc duration |
| Default `RegistryMaxSize=65536` too low for a plugin that caches many values | Low | Operator raises the flag; metric attribution makes this visible |
| gopher-lua v1.1.1 context-per-instruction behavior changes in a future release | Low | Pinned via `go.mod`; Dependabot upgrades reviewed; the watchdog-drop pattern degrades gracefully (dispatcher still returns timeout, goroutine drain may slip for stuck hostfunc) |
| Hostfunc audit misses a slow-path hostfunc | Low | Meta-test `TestHostFuncsRespectContext` catches obvious blockers; a deeper hostfunc doing real I/O would still be bounded by the underlying RPC's own timeout |
| Goroutine leak from a never-returning hostfunc | Very Low | Hostfunc audit rules this out; if one slips in, leaked goroutines accumulate slowly and are visible in `runtime.NumGoroutine` monitoring |

## Non-goals (explicit)

- This PR does NOT add `runtime.MemStats` polling watchdog.
- This PR does NOT add an instruction-count hook (gopher-lua has no such API).
- This PR does NOT change `CallStackSize` (already 256 by default).
- This PR does NOT call `L.Close()` from the dispatcher's ctx.Done() branch.
- This PR does NOT change plugin loading, lifecycle, or the manifest schema.
- This PR does NOT introduce binary-plugin resource limits.

## Success criteria

- `task pr-prep` passes cleanly before merge.
- `holomush_plugin_lua_invocations_total{outcome="timeout"}` visible in operator's Prometheus after deploy.
- Manual test: a plugin whose `on_event` is `while true do end` fires a timeout within `~1 s` and leaves the process responsive to further events.
- Manual test: a plugin that allocates `{[1]=1, ..., [1000000]=1}` in a loop returns a `registry_full` outcome and the process memory does not grow unbounded.
- Existing plugin integration tests (`internal/plugin/*_integration_test.go`) remain green.
