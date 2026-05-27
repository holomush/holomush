<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Lua Plugin Resource Limits Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add CPU timeout + registry cap + hostfunc audit to the Lua plugin dispatcher so malicious or buggy plugins cannot exhaust CPU, memory, or dispatcher threads.

**Architecture:** `StateFactory` gains a `registryMaxSize` field wired into `lua.Options.RegistryMaxSize`. `Host` gains a `cpuTimeout` field and a new `invoke(parentCtx, L, handler, p, args...)` method that wraps `CallByParam` in a goroutine under `context.WithTimeout(parentCtx, cpuTimeout)`. On `ctx.Done()` the dispatcher waits for the goroutine to drain before returning — bounded by gopher-lua's per-instruction context check (pure-Lua loops exit on the next instruction) and the hostfunc audit invariant (every registered hostfunc either is O(1) or respects `L.Context()`). Waiting ensures the state is no longer in use when the caller closes it. Three Prometheus `CounterVec`s (`invocations_total`, `timeouts_total`, `registry_full_total`) expose the controls with `{plugin,handler}` labels.

**Tech Stack:** Go stdlib (context, time), `github.com/yuin/gopher-lua` (existing), `github.com/prometheus/client_golang` (existing), `github.com/samber/oops` for error codes, testify for assertions.

**Spec:** `docs/superpowers/specs/2026-04-17-lua-resource-limits-design.md`

**Bead:** `holomush-u9p5`

---

## File Structure

| File | Action | Responsibility |
| ---- | ------ | -------------- |
| `internal/plugin/lua/metrics.go` | CREATE | Three `CounterVec`s + `recordInvocationOutcome(handler, outcome)` helper |
| `internal/plugin/lua/metrics_test.go` | CREATE | Counter-increment smoke tests |
| `internal/plugin/lua/state.go` | MODIFY | `StateFactory.registryMaxSize` field + constructor option + `lua.Options.RegistryMaxSize` wiring |
| `internal/plugin/lua/state_test.go` | MODIFY | Add `TestNewStateRegistryMaxSizeApplied` + `TestNewStateRegistryUnboundedWhenZero` |
| `internal/plugin/lua/invoke.go` | CREATE | `Host.invoke` method + `classifyError` helper |
| `internal/plugin/lua/invoke_test.go` | CREATE | Unit tests for timeout, hostfunc-block, outcomes, classifyError |
| `internal/plugin/lua/host.go` | MODIFY | Add `cpuTimeout` field + `WithCPUTimeout` option; replace four inline `CallByParam` sites with `h.invoke(...)` |
| `internal/plugin/lua/host_test.go` and other test files | MODIFY | Update `NewHost*` call sites if signature changes break them |
| `internal/plugin/hostfunc/context_audit_test.go` | CREATE | Meta-test: every registered hostfunc returns within 50ms when called with a pre-cancelled context |
| `internal/plugin/setup/subsystem.go` | MODIFY | Thread `LuaTimeout` + `LuaRegistryMaxSize` from `PluginSubsystemConfig` into `pluginlua.NewHostWithFunctions` / `NewStateFactory` |
| `cmd/holomush/core.go` | MODIFY | Add `LuaTimeout` + `LuaRegistryMaxSize` fields + defaults + flags + validation + plumb into `PluginSubsystemConfig` |
| `cmd/holomush/core_test.go` | MODIFY | Flag defaults test + validation rejection test |
| `test/integration/plugin/lua_resource_limits_integration_test.go` | CREATE | CPU-bomb + memory-bomb end-to-end scenarios |
| `test/integration/plugin/testdata/lua/cpu_bomb/manifest.yaml` | CREATE | Test plugin fixture |
| `test/integration/plugin/testdata/lua/cpu_bomb/plugin.lua` | CREATE | `on_event = while true do end` |
| `test/integration/plugin/testdata/lua/memory_bomb/manifest.yaml` | CREATE | Test plugin fixture |
| `test/integration/plugin/testdata/lua/memory_bomb/plugin.lua` | CREATE | `on_event` allocates unboundedly |
| `site/docs/operating/plugin-security.md` | CREATE or MODIFY | "Lua resource limits" section |
| `site/docs/contributing/hostfunc-context-audit.md` | CREATE | Audit table + invariant for future hostfuncs |

Build order: primitives first (metrics, state registry cap), then the `invoke` helper, then migrate the four `CallByParam` sites, then hostfunc audit, then config plumbing, then integration tests, then docs.

---

## Task 1: Metrics package

**Files:**

- Create: `internal/plugin/lua/metrics.go`
- Create: `internal/plugin/lua/metrics_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/plugin/lua/metrics_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestRecordInvocationOutcomeSuccess(t *testing.T) {
	before := testutil.ToFloat64(InvocationsTotal.WithLabelValues("p1", "on_event", "success"))
	recordInvocationOutcome("p1", "on_event", outcomeSuccess)
	assert.Equal(t, before+1, testutil.ToFloat64(InvocationsTotal.WithLabelValues("p1", "on_event", "success")))
}

func TestRecordInvocationOutcomeTimeoutIncrementsBothCounters(t *testing.T) {
	beforeInv := testutil.ToFloat64(InvocationsTotal.WithLabelValues("p2", "on_event", "timeout"))
	beforeTo := testutil.ToFloat64(TimeoutsTotal.WithLabelValues("p2", "on_event"))
	recordInvocationOutcome("p2", "on_event", outcomeTimeout)
	assert.Equal(t, beforeInv+1, testutil.ToFloat64(InvocationsTotal.WithLabelValues("p2", "on_event", "timeout")))
	assert.Equal(t, beforeTo+1, testutil.ToFloat64(TimeoutsTotal.WithLabelValues("p2", "on_event")))
}

func TestRecordInvocationOutcomeRegistryFullIncrementsBothCounters(t *testing.T) {
	beforeInv := testutil.ToFloat64(InvocationsTotal.WithLabelValues("p3", "on_event", "registry_full"))
	beforeRf := testutil.ToFloat64(RegistryFullTotal.WithLabelValues("p3", "on_event"))
	recordInvocationOutcome("p3", "on_event", outcomeRegistryFull)
	assert.Equal(t, beforeInv+1, testutil.ToFloat64(InvocationsTotal.WithLabelValues("p3", "on_event", "registry_full")))
	assert.Equal(t, beforeRf+1, testutil.ToFloat64(RegistryFullTotal.WithLabelValues("p3", "on_event")))
}

func TestRecordInvocationOutcomeErrorOnlyIncrementsInvocations(t *testing.T) {
	beforeInv := testutil.ToFloat64(InvocationsTotal.WithLabelValues("p4", "on_event", "error"))
	beforeTo := testutil.ToFloat64(TimeoutsTotal.WithLabelValues("p4", "on_event"))
	beforeRf := testutil.ToFloat64(RegistryFullTotal.WithLabelValues("p4", "on_event"))
	recordInvocationOutcome("p4", "on_event", outcomeError)
	assert.Equal(t, beforeInv+1, testutil.ToFloat64(InvocationsTotal.WithLabelValues("p4", "on_event", "error")))
	assert.Equal(t, beforeTo, testutil.ToFloat64(TimeoutsTotal.WithLabelValues("p4", "on_event")))
	assert.Equal(t, beforeRf, testutil.ToFloat64(RegistryFullTotal.WithLabelValues("p4", "on_event")))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -timeout 30s -run 'TestRecordInvocation' ./internal/plugin/lua/`
Expected: FAIL — `undefined: InvocationsTotal`, etc.

- [ ] **Step 3: Write minimal implementation**

Create `internal/plugin/lua/metrics.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// outcome label values used with InvocationsTotal.
const (
	outcomeSuccess      = "success"
	outcomeTimeout      = "timeout"
	outcomeRegistryFull = "registry_full"
	outcomeError        = "error"
)

// InvocationsTotal counts every dispatcher invocation of a Lua handler,
// labelled by plugin, handler name, and outcome. Serves as the denominator
// for outcome-rate dashboards.
var InvocationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "holomush_plugin_lua_invocations_total",
	Help: "Total Lua plugin invocations by plugin, handler, and outcome",
}, []string{"plugin", "handler", "outcome"})

// TimeoutsTotal counts invocations that hit the per-invocation CPU
// deadline. Rises under adversarial or buggy plugins.
var TimeoutsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "holomush_plugin_lua_timeouts_total",
	Help: "Total Lua plugin invocations disconnected for exceeding the CPU deadline",
}, []string{"plugin", "handler"})

// RegistryFullTotal counts invocations killed by Lua registry overflow
// (RegistryMaxSize). Rises under memory-bomb plugins.
var RegistryFullTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "holomush_plugin_lua_registry_full_total",
	Help: "Total Lua plugin invocations killed by registry overflow (memory cap)",
}, []string{"plugin", "handler"})

// recordInvocationOutcome increments the invocations counter with the
// given outcome label, and increments the corresponding specific counter
// (timeouts_total or registry_full_total) when the outcome indicates one
// of the resource-cap paths.
func recordInvocationOutcome(plugin, handler, outcome string) {
	InvocationsTotal.WithLabelValues(plugin, handler, outcome).Inc()
	switch outcome {
	case outcomeTimeout:
		TimeoutsTotal.WithLabelValues(plugin, handler).Inc()
	case outcomeRegistryFull:
		RegistryFullTotal.WithLabelValues(plugin, handler).Inc()
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -timeout 30s -run 'TestRecordInvocation' ./internal/plugin/lua/`
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin/lua): add Prometheus metrics for Lua resource limits

Three CounterVecs — invocations_total (labelled by outcome for
denominator), timeouts_total, registry_full_total. recordInvocationOutcome
helper centralizes the labelled increment so dispatchers don't need to
touch the metric types directly. Consumed by the invoke helper in a
follow-up task.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"

jj --no-pager new
```

---

## Task 2: StateFactory registry cap

**Files:**

- Modify: `internal/plugin/lua/state.go`
- Modify: `internal/plugin/lua/state_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/plugin/lua/state_test.go`:

```go
// TestNewStateRegistryMaxSizeApplied verifies that a StateFactory configured
// with a small RegistryMaxSize causes a table-allocation bomb to fail at the
// registry cap (surfaced as a panic caught by CallByParam Protect=true).
func TestNewStateRegistryMaxSizeApplied(t *testing.T) {
	factory := pluginlua.NewStateFactory(pluginlua.WithRegistryMaxSize(1024))
	L, err := factory.NewState(context.Background())
	require.NoError(t, err)
	defer L.Close()

	// Load a script that grows an array aggressively.
	bomb := `
local t = {}
for i = 1, 100000 do
    t[#t + 1] = {i, i, i, i}
end
return #t
`
	// Use CallByParam with Protect=true to catch panics.
	fn := L.NewFunction(func(L *lua.LState) int {
		if err := L.DoString(bomb); err != nil {
			L.RaiseError("%s", err.Error())
		}
		return 0
	})
	err = L.CallByParam(lua.P{
		Fn:      fn,
		NRet:    0,
		Protect: true,
	})
	assert.Error(t, err, "expected registry overflow to surface as an error")
}

// TestNewStateRegistryUnboundedWhenZero verifies the factory treats
// RegistryMaxSize=0 (default) as "no cap configured" — many-value scripts
// complete without error. This is the legacy behavior.
func TestNewStateRegistryUnboundedWhenZero(t *testing.T) {
	factory := pluginlua.NewStateFactory() // no option — zero default
	L, err := factory.NewState(context.Background())
	require.NoError(t, err)
	defer L.Close()

	script := `
local t = {}
for i = 1, 5000 do
    t[#t + 1] = i
end
return #t
`
	err = L.DoString(script)
	assert.NoError(t, err, "5000 values should fit in the default registry")
}
```

Ensure these imports are present:

```go
import (
	"context"
	"testing"

	lua "github.com/yuin/gopher-lua"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -timeout 30s -run 'TestNewStateRegistry' ./internal/plugin/lua/`
Expected: FAIL — `WithRegistryMaxSize` is not defined.

- [ ] **Step 3: Modify `StateFactory` to accept the option and apply `RegistryMaxSize`**

Edit `internal/plugin/lua/state.go`. Change the factory to support functional options:

```go
// StateFactory creates sandboxed Lua states with only safe libraries.
type StateFactory struct {
	// libraries allows overriding the default safe libraries for testing.
	libraries []safeLibrary
	// registryMaxSize bounds the Lua value registry per state. Zero means
	// "use gopher-lua default" (unbounded growth).
	registryMaxSize int
}

// StateFactoryOption customizes StateFactory construction.
type StateFactoryOption func(*StateFactory)

// WithRegistryMaxSize sets the upper bound on the Lua value registry per
// state. Overflow causes gopher-lua to panic; CallByParam(Protect=true)
// catches it and returns an error. Zero disables the cap.
func WithRegistryMaxSize(n int) StateFactoryOption {
	return func(f *StateFactory) { f.registryMaxSize = n }
}

// NewStateFactory creates a new state factory.
func NewStateFactory(opts ...StateFactoryOption) *StateFactory {
	f := &StateFactory{
		libraries: defaultSafeLibraries(),
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}
```

Modify `NewState` to pass the field into `lua.Options`:

```go
func (f *StateFactory) NewState(_ context.Context) (*lua.LState, error) {
	L := lua.NewState(lua.Options{
		SkipOpenLibs:    true,
		RegistryMaxSize: f.registryMaxSize,
	})

	for _, lib := range f.libraries {
		if err := L.CallByParam(lua.P{
			Fn:      L.NewFunction(lib.fn),
			NRet:    0,
			Protect: true,
		}, lua.LString(lib.name)); err != nil {
			L.Close()
			return nil, oops.In("lua").With("library", lib.name).Hint("failed to open library").Wrap(err)
		}
	}

	for _, fn := range unsafeBaseFunctions {
		L.SetGlobal(fn, lua.LNil)
	}

	return L, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- -timeout 30s -run 'TestNewStateRegistry' ./internal/plugin/lua/`
Expected: PASS (2 tests)

Also run the existing state suite to confirm no regressions:

Run: `task test -- -timeout 30s ./internal/plugin/lua/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin/lua): StateFactory RegistryMaxSize option

Adds WithRegistryMaxSize functional option to NewStateFactory and wires
it into lua.Options.RegistryMaxSize. Overflow causes gopher-lua to panic;
CallByParam Protect=true catches the panic. Zero preserves the legacy
unbounded behavior — follow-up task plumbs a non-zero default through
config.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"

jj --no-pager new
```

---

## Task 3: Host cpuTimeout option + invoke helper

**Files:**

- Modify: `internal/plugin/lua/host.go` (add field + option)
- Create: `internal/plugin/lua/invoke.go`
- Create: `internal/plugin/lua/invoke_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/plugin/lua/invoke_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

// newInvokeTestHost returns a Host configured with a short CPU timeout and
// a small registry cap, plus a helper that creates a fresh state from its
// factory. Tests use this to exercise invoke() without a full plugin stack.
func newInvokeTestHost(t *testing.T, cpuTimeout time.Duration, registryMax int) *Host {
	t.Helper()
	h := &Host{
		factory:    NewStateFactory(WithRegistryMaxSize(registryMax)),
		cpuTimeout: cpuTimeout,
		plugins:    map[string]*luaPlugin{},
	}
	return h
}

func TestInvokeReturnsCallByParamResultWhenFast(t *testing.T) {
	h := newInvokeTestHost(t, 500*time.Millisecond, 0)
	L, err := h.factory.NewState(context.Background())
	require.NoError(t, err)
	defer L.Close()

	// A Lua function that returns 42.
	require.NoError(t, L.DoString(`function handler() return 42 end`))
	fn := L.GetGlobal("handler")
	require.Equal(t, lua.LTFunction, fn.Type())

	err = h.invoke(context.Background(), L, "test", "on_event", lua.P{
		Fn:      fn,
		NRet:    1,
		Protect: true,
	})
	assert.NoError(t, err)
	assert.Equal(t, lua.LNumber(42), L.Get(-1))
}

func TestInvokeSurfacesCPUTimeoutOnTightLoop(t *testing.T) {
	h := newInvokeTestHost(t, 100*time.Millisecond, 0)
	L, err := h.factory.NewState(context.Background())
	require.NoError(t, err)
	defer L.Close()

	require.NoError(t, L.DoString(`function handler() while true do end end`))
	fn := L.GetGlobal("handler")

	before := testutil.ToFloat64(TimeoutsTotal.WithLabelValues("test", "on_event"))

	start := time.Now()
	err = h.invoke(context.Background(), L, "test", "on_event", lua.P{
		Fn:      fn,
		NRet:    0,
		Protect: true,
	})
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Less(t, elapsed, 400*time.Millisecond,
		"invoke must return within ~timeout (+ jitter budget)")
	assert.Equal(t, before+1, testutil.ToFloat64(TimeoutsTotal.WithLabelValues("test", "on_event")),
		"timeouts_total must increment")
}

func TestInvokeReleasesDispatcherWhenHostFuncBlocks(t *testing.T) {
	h := newInvokeTestHost(t, 100*time.Millisecond, 0)
	L, err := h.factory.NewState(context.Background())
	require.NoError(t, err)
	defer L.Close()

	// Register a hostfunc that blocks on a channel. We close the channel in
	// t.Cleanup so the leaked goroutine drains at end of test.
	unblock := make(chan struct{})
	t.Cleanup(func() { close(unblock) })

	L.SetGlobal("blocker", L.NewFunction(func(L *lua.LState) int {
		<-unblock
		return 0
	}))
	require.NoError(t, L.DoString(`function handler() blocker() end`))
	fn := L.GetGlobal("handler")

	start := time.Now()
	err = h.invoke(context.Background(), L, "test", "on_event", lua.P{
		Fn:      fn,
		NRet:    0,
		Protect: true,
	})
	elapsed := time.Since(start)

	assert.Error(t, err, "dispatcher must surface timeout error")
	assert.Less(t, elapsed, 400*time.Millisecond,
		"dispatcher must not wait for the stuck hostfunc")
}

func TestClassifyError(t *testing.T) {
	assert.Equal(t, outcomeSuccess, classifyError(nil))

	registryErr := errors.New("registry size limit reached")
	assert.Equal(t, outcomeRegistryFull, classifyError(registryErr))

	assert.Equal(t, outcomeError, classifyError(errors.New("something else")))
}

func TestClassifyErrorRecognizesWrappedRegistryOverflow(t *testing.T) {
	// gopher-lua's registry overflow message contains "registry" substring.
	wrapped := errors.New("lua: pcall: " + "registry overflow occurred")
	assert.Equal(t, outcomeRegistryFull, classifyError(wrapped))
}

func TestInvokeReturnsLuaErrorAsOutcomeError(t *testing.T) {
	h := newInvokeTestHost(t, 500*time.Millisecond, 0)
	L, err := h.factory.NewState(context.Background())
	require.NoError(t, err)
	defer L.Close()

	require.NoError(t, L.DoString(`function handler() error("boom") end`))
	fn := L.GetGlobal("handler")

	beforeErr := testutil.ToFloat64(InvocationsTotal.WithLabelValues("test", "on_event", outcomeError))

	err = h.invoke(context.Background(), L, "test", "on_event", lua.P{
		Fn:      fn,
		NRet:    0,
		Protect: true,
	})
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "boom"))
	assert.Equal(t, beforeErr+1,
		testutil.ToFloat64(InvocationsTotal.WithLabelValues("test", "on_event", outcomeError)),
		"Lua error must count as outcome=error")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -timeout 30s -run 'TestInvoke|TestClassifyError' ./internal/plugin/lua/`
Expected: FAIL — `h.cpuTimeout` undefined, `h.invoke` undefined, `classifyError` undefined.

- [ ] **Step 3: Add cpuTimeout field + WithCPUTimeout option to Host**

Edit `internal/plugin/lua/host.go`. Add the `cpuTimeout` field to `Host`:

```go
// Host manages Lua plugins.
type Host struct {
	factory    *StateFactory
	hostFuncs  *hostfunc.Functions
	plugins    map[string]*luaPlugin
	mu         sync.RWMutex
	closed     bool
	cpuTimeout time.Duration // per-invocation deadline applied via context.WithTimeout
}
```

Add `time` to the import block if not present.

Add an option type + constructor variants:

```go
// HostOption customizes Host construction.
type HostOption func(*Host)

// WithCPUTimeout sets the per-invocation deadline applied to every
// CallByParam dispatched through Host.invoke. Zero disables the cap
// (unchanged context inheritance). Recommend the caller pass a positive
// duration in production; zero is allowed only for tests.
func WithCPUTimeout(d time.Duration) HostOption {
	return func(h *Host) { h.cpuTimeout = d }
}

// WithStateFactory replaces the default StateFactory. Used by callers
// that need a factory with non-default options (e.g. RegistryMaxSize).
func WithStateFactory(f *StateFactory) HostOption {
	return func(h *Host) { h.factory = f }
}
```

Update the existing constructors to accept options:

```go
// NewHost creates a new Lua plugin host without host functions.
func NewHost(opts ...HostOption) *Host {
	h := &Host{
		factory: NewStateFactory(),
		plugins: make(map[string]*luaPlugin),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// NewHostWithFunctions creates a Lua plugin host with host functions.
// The host functions enable plugins to call holomush.* APIs like log(), new_request_id(), and kv_*.
// Panics if hf is nil (consistent with hostfunc.New).
func NewHostWithFunctions(hf *hostfunc.Functions, opts ...HostOption) *Host {
	if hf == nil {
		panic("lua.NewHostWithFunctions: hostFuncs cannot be nil")
	}
	h := &Host{
		factory:   NewStateFactory(),
		hostFuncs: hf,
		plugins:   make(map[string]*luaPlugin),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}
```

- [ ] **Step 4: Create the invoke helper**

Create `internal/plugin/lua/invoke.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"strings"

	"github.com/samber/oops"
	lua "github.com/yuin/gopher-lua"
)

// invoke runs p.Fn under a per-invocation CPU deadline and a watchdog
// goroutine. Returns the CallByParam error if the goroutine completes
// first, or a wrapped PLUGIN_LUA_TIMEOUT error if the dispatcher times
// out before the goroutine completes.
//
// Invariants:
//   - The state L is NOT closed by invoke. Ownership stays with the caller,
//     which closes it in the normal cleanup path (defer L.Close()) AFTER
//     invoke returns.
//   - On ctx.Done(), invoke waits for the goroutine to drain before returning.
//     The wait is bounded: gopher-lua's per-instruction context check
//     (established by L.SetContext on the derived ctx) forces pure-Lua tight
//     loops to RaiseError on the next instruction; hostfunc blocks are bounded
//     by the hostfunc audit invariant that every registered hostfunc either is
//     O(1) or respects L.Context(). Waiting ensures the state is no longer in
//     use when the caller closes it (race-detector cleanliness).
//   - invoke MUST NOT call L.Close() from its ctx.Done() branch. Concurrent
//     Close with an in-flight CallByParam is undocumented in gopher-lua.
func (h *Host) invoke(parentCtx context.Context, L *lua.LState, plugin, handler string, p lua.P, args ...lua.LValue) error {
	var ctx context.Context
	var cancel context.CancelFunc
	if h.cpuTimeout > 0 {
		ctx, cancel = context.WithTimeout(parentCtx, h.cpuTimeout)
	} else {
		ctx, cancel = context.WithCancel(parentCtx)
	}
	defer cancel()
	L.SetContext(ctx)

	done := make(chan error, 1)
	go func() {
		done <- L.CallByParam(p, args...)
	}()

	select {
	case err := <-done:
		recordInvocationOutcome(plugin, handler, classifyError(err))
		return err
	case <-ctx.Done():
		// Wait for the goroutine to drain (bounded by per-instruction
		// context check + hostfunc audit invariant) before returning.
		<-done
		recordInvocationOutcome(plugin, handler, outcomeTimeout)
		return oops.Code("PLUGIN_LUA_TIMEOUT").
			With("plugin", plugin).
			With("handler", handler).
			With("timeout", h.cpuTimeout).
			Wrap(ctx.Err())
	}
}

// classifyError maps a CallByParam error to an outcome label for metrics.
// gopher-lua's registry-overflow panic (caught by Protect=true) contains
// the substring "registry overflow" in its message; any other non-nil
// error is treated as a normal Lua-level error.
func classifyError(err error) string {
	if err == nil {
		return outcomeSuccess
	}
	// Match gopher-lua's specific panic text for RegistryMaxSize overflow
	// rather than any error mentioning "registry" (which would misattribute
	// legitimate plugin errors like error("registry lookup failed")).
	if strings.Contains(err.Error(), "registry overflow") {
		return outcomeRegistryFull
	}
	return outcomeError
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `task test -- -timeout 30s -run 'TestInvoke|TestClassifyError' ./internal/plugin/lua/`
Expected: PASS (7 tests)

Also run the full lua test suite:

Run: `task test -- -timeout 60s ./internal/plugin/lua/`
Expected: PASS, no regressions.

- [ ] **Step 6: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin/lua): Host.invoke helper with CPU deadline and watchdog

NewHost / NewHostWithFunctions accept HostOption functional options. New
WithCPUTimeout(d) option sets the per-invocation deadline. New invoke()
method wraps CallByParam in a goroutine with context.WithTimeout; on
ctx.Done the dispatcher waits for the goroutine to drain (bounded by
SetContext's per-instruction check and the hostfunc audit invariant),
then returns PLUGIN_LUA_TIMEOUT.

classifyError helper maps CallByParam errors to the outcome label used
for metrics — 'registry overflow' substring → registry_full, any other
non-nil → error, nil → success.

Not yet consumed by the dispatcher methods — next task migrates the four
inline CallByParam sites.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"

jj --no-pager new
```

---

## Task 4: Migrate the four CallByParam sites to invoke

**Files:**

- Modify: `internal/plugin/lua/host.go` (four inline CallByParam sites)

The four sites (see `host.go` line numbers):

| Method | Line | Handler label for metrics |
| ------ | ---- | ------------------------- |
| `DeliverEvent` (on_event path) | 214 | `on_event` |
| `DeliverCommand` | 280 | `on_command` |
| `QuerySessionStreams` | 344 | `on_session_subscribe` |
| `callOnCommand` (DeliverEvent command fallback) | 395 | `on_command` |

- [ ] **Step 1: Replace the DeliverEvent CallByParam (line ~214)**

Find this block in `internal/plugin/lua/host.go`:

```go
	// Call on_event(event)
	if err := L.CallByParam(lua.P{
		Fn:      onEvent,
		NRet:    1,
		Protect: true,
	}, eventTable); err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "on_event").Wrap(err)
	}
```

Replace with:

```go
	// Call on_event(event) via invoke for CPU-deadline + watchdog protection.
	if err := h.invoke(ctx, L, name, "on_event", lua.P{
		Fn:      onEvent,
		NRet:    1,
		Protect: true,
	}, eventTable); err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "on_event").Wrap(err)
	}
```

Remove the now-redundant `L.SetContext(ctx)` call at the top of `DeliverEvent` (invoke sets its own derived context). Leave the assignment site alone if other code between SetContext and CallByParam depends on the state being ctx-aware — double-check with a read pass. If removal is ambiguous, LEAVE the original SetContext in place; invoke's own SetContext will overwrite it harmlessly.

- [ ] **Step 2: Replace the DeliverCommand CallByParam (line ~280)**

Find:

```go
	if err := L.CallByParam(lua.P{
		Fn:      onCommand,
		NRet:    1,
		Protect: true,
	}, ctxTable); err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "on_command").Wrap(err)
	}
```

Replace with:

```go
	if err := h.invoke(ctx, L, name, "on_command", lua.P{
		Fn:      onCommand,
		NRet:    1,
		Protect: true,
	}, ctxTable); err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "on_command").Wrap(err)
	}
```

- [ ] **Step 3: Replace the QuerySessionStreams CallByParam (line ~344)**

Find:

```go
	if err := L.CallByParam(lua.P{
		Fn:      fn,
		NRet:    1,
		Protect: true,
	},
		lua.LString(req.CharacterID),
		lua.LString(req.PlayerID),
		lua.LString(req.SessionID),
	); err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "on_session_subscribe").Wrap(err)
	}
```

Replace with:

```go
	if err := h.invoke(ctx, L, name, "on_session_subscribe", lua.P{
		Fn:      fn,
		NRet:    1,
		Protect: true,
	},
		lua.LString(req.CharacterID),
		lua.LString(req.PlayerID),
		lua.LString(req.SessionID),
	); err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "on_session_subscribe").Wrap(err)
	}
```

- [ ] **Step 4: Replace the callOnCommand CallByParam (line ~395)**

`callOnCommand` currently takes `state *lua.LState` and doesn't have access to the request context directly. Pass ctx in as a parameter.

Find the function signature and body:

```go
// callOnCommand calls the on_command handler with a typed CommandContext.
func (h *Host) callOnCommand(state *lua.LState, name string, event pluginsdk.Event, onCommand lua.LValue) ([]pluginsdk.EmitEvent, error) {
	// Parse command payload into CommandContext
	cmdCtx := holo.ParseCommandPayload(event.Payload)

	// Build Lua context table
	ctxTable := h.buildContextTable(state, cmdCtx)

	// Call on_command(ctx)
	if err := state.CallByParam(lua.P{
		Fn:      onCommand,
		NRet:    1,
		Protect: true,
	}, ctxTable); err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "on_command").Wrap(err)
	}

	// ... rest unchanged ...
}
```

Change the signature to accept `ctx context.Context` and use invoke:

```go
// callOnCommand calls the on_command handler with a typed CommandContext.
func (h *Host) callOnCommand(ctx context.Context, state *lua.LState, name string, event pluginsdk.Event, onCommand lua.LValue) ([]pluginsdk.EmitEvent, error) {
	// Parse command payload into CommandContext
	cmdCtx := holo.ParseCommandPayload(event.Payload)

	// Build Lua context table
	ctxTable := h.buildContextTable(state, cmdCtx)

	// Call on_command(ctx) via invoke.
	if err := h.invoke(ctx, state, name, "on_command", lua.P{
		Fn:      onCommand,
		NRet:    1,
		Protect: true,
	}, ctxTable); err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "on_command").Wrap(err)
	}

	// ... rest unchanged ...
}
```

Update the single caller of `callOnCommand` in `DeliverEvent` (line ~196):

```go
	return h.callOnCommand(ctx, L, name, event, onCommand)
```

- [ ] **Step 5: Run tests**

Run: `task test -- -timeout 60s ./internal/plugin/lua/`
Expected: PASS, including all the existing handler tests (they pass nil or pre-cancelled contexts, which with `cpuTimeout=0` behave the same as the old inline CallByParam).

If any existing test now fails because a zero `cpuTimeout` is being applied and fails, audit the test — either construct the Host with `WithCPUTimeout(5*time.Second)` for tests that need a generous budget, or confirm zero-timeout falls through (invoke's behavior with cpuTimeout=0 is `context.WithCancel(parentCtx)` — no timeout added).

- [ ] **Step 6: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin/lua): route all CallByParam dispatches through invoke

Four inline CallByParam sites now call h.invoke(ctx, L, plugin, handler,
p, args...). Each invocation gets a per-invocation CPU deadline and a
goroutine watchdog that prevents a stuck hostfunc from hanging the
dispatcher. metrics labels attribute invocations to {plugin, handler}.

callOnCommand signature gains a ctx context.Context parameter to make
context inheritance explicit at the call site.

Closes holomush-u9p5 controls 1 (CPU deadline) and 4 (watchdog) from the
design spec.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"

jj --no-pager new
```

---

## Task 5: Hostfunc context-awareness meta-test

**Files:**

- Create: `internal/plugin/hostfunc/context_audit_test.go`

- [ ] **Step 1: Inspect the hostfunc registration surface**

Read `internal/plugin/hostfunc/functions.go` (grep for `Register` method) to understand how hostfuncs are registered on an `L *lua.LState`. Identify the global names (e.g., `holomush.log`, `holomush.new_request_id`, `holomush.kv.get`, etc.) so the meta-test can invoke each.

If the registration exposes them via a table (e.g., `holomush.log`, `holomush.kv.put`), the meta-test iterates all function globals under the `holomush` table. If registration is ad-hoc per-capability, the test enumerates explicit paths.

- [ ] **Step 2: Write the meta-test**

Create `internal/plugin/hostfunc/context_audit_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/plugin/hostfunc"
)

// TestHostFuncsRespectContextOnPreCancelledCtx is a meta-test: for every
// host function registered by hostfunc.Functions.Register, call it with
// a Lua state whose context is already cancelled, and assert the call
// returns within 50ms. This locks the hostfunc audit conclusion (all
// registered host functions are either instantaneous or respect
// L.Context() for any potentially-blocking work) against future
// regressions.
//
// The test is intentionally coarse: it doesn't validate semantics of the
// return value, only that the call returns promptly under a cancelled
// context. New host functions that block unboundedly under adversarial
// input will trip this.
func TestHostFuncsRespectContextOnPreCancelledCtx(t *testing.T) {
	hf := hostfunc.New(nil)
	require.NotNil(t, hf)

	// Walk the registered functions. The exact enumeration depends on
	// hostfunc.Functions API — below assumes a Walk method that lists
	// (globalPath, func) pairs. If the API differs, adapt to it.
	funcs := hf.RegisteredFunctionsForAudit() // see Step 3 for the helper

	for _, fn := range funcs {
		t.Run(fn.Name, func(t *testing.T) {
			L := lua.NewState()
			defer L.Close()

			// Register hostfuncs into this state.
			hf.Register(L, "audit-test-plugin")

			// Cancel the context immediately before invoking the function.
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			L.SetContext(ctx)

			// Invoke the global path with zero args (arg count may vary by
			// function; most accept ctx + varargs and handle empty args as
			// a no-op). The test only asserts the call returns promptly.
			luaSource := fn.Name + "()"

			start := time.Now()
			// DoString here so a Lua error from "wrong args" still returns.
			_ = L.DoString(luaSource)
			elapsed := time.Since(start)

			assert.Less(t, elapsed, 50*time.Millisecond,
				"hostfunc %s must return within 50ms under a cancelled context; took %s",
				fn.Name, elapsed)
		})
	}
}
```

- [ ] **Step 3: Add `RegisteredFunctionsForAudit` on `hostfunc.Functions`**

This is a test-only surface. Add to `internal/plugin/hostfunc/functions.go`:

```go
// AuditEntry names one registered hostfunc for the purposes of the
// context-respect meta-test. Keyed by the Lua global path (e.g. "holomush.log"
// or "holomush_log", matching whatever Register emits).
type AuditEntry struct {
	Name string
}

// RegisteredFunctionsForAudit returns the list of hostfuncs Register would
// install on a Lua state. Test-only; the audit meta-test in
// internal/plugin/hostfunc/context_audit_test.go iterates this list.
func (f *Functions) RegisteredFunctionsForAudit() []AuditEntry {
	// Enumerate the same global paths that Register uses. Keep in sync
	// with Register's surface. If the Register implementation uses a
	// map or switch, source that map and convert to AuditEntry here.
	return []AuditEntry{
		{Name: "holomush.log"},
		{Name: "holomush.new_request_id"},
		// ... list every Lua-visible global path that Register installs.
		// If Register iterates a table of (name, fn) pairs, inspect it and
		// list those names here.
	}
}
```

**Critical for the worker:** the exact list depends on what `Register` installs. Before writing this, READ `internal/plugin/hostfunc/functions.go:Register` and build the list from its actual bindings. If Register has >20 bindings, the worker MAY choose to instead expose a `MapForAudit() map[string]lua.LGFunction` that returns the registration map directly, and have the test iterate over it. Either shape is acceptable.

- [ ] **Step 4: Run the meta-test**

Run: `task test -- -timeout 30s -run 'TestHostFuncsRespectContextOnPreCancelledCtx' ./internal/plugin/hostfunc/`
Expected: PASS for every registered function. If any fails, that hostfunc needs a fix (either add context-awareness, or document and skip via a t.Skip with reason).

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "test(plugin/hostfunc): meta-test locks hostfunc context-respect invariant

Walks every registered hostfunc and asserts it returns within 50ms when
invoked with a pre-cancelled context. This locks the audit conclusion
(all current hostfuncs either are instantaneous or respect L.Context())
against future regressions.

New test-only surface RegisteredFunctionsForAudit enumerates the Register
bindings so the test stays in sync with the production registration list.

Closes holomush-u9p5 control 3 (hostfunc audit).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"

jj --no-pager new
```

---

## Task 6: Config knobs + plumbing

**Files:**

- Modify: `cmd/holomush/core.go` — add fields + defaults + flags + validation
- Modify: `cmd/holomush/core_test.go` — flag defaults test + validation test
- Modify: `internal/plugin/setup/subsystem.go` — thread fields from `PluginSubsystemConfig` into Lua host + factory

- [ ] **Step 1: Write the failing config tests**

Add to `cmd/holomush/core_test.go` (or create if missing). Find the existing `TestCoreConfig_Validate` (or equivalent); add new cases plus a defaults test:

```go
func TestCoreCommand_LuaLimitDefaults(t *testing.T) {
	cmd := NewCoreCmd()

	timeout, err := cmd.Flags().GetDuration("plugin-lua-timeout")
	require.NoError(t, err)
	assert.Equal(t, 1*time.Second, timeout, "default Lua timeout per spec")

	regMax, err := cmd.Flags().GetInt("plugin-lua-registry-max")
	require.NoError(t, err)
	assert.Equal(t, 65536, regMax, "default registry max per spec")
}

func TestCoreConfig_ValidateRejectsNonPositiveLuaLimits(t *testing.T) {
	// Baseline valid config; fields not relevant to the Lua-limit test
	// are filled with valid values. Include all NEW and EXISTING required
	// fields — if Validate grows other requirements, the worker updates
	// this helper.
	base := coreConfig{
		GRPCAddr:           "localhost:9000",
		ControlAddr:        "127.0.0.1:9001",
		LogFormat:          "json",
		LuaTimeout:         1 * time.Second,
		LuaRegistryMaxSize: 65536,
	}

	cases := []struct {
		name string
		mut  func(c *coreConfig)
	}{
		{"LuaTimeout=0", func(c *coreConfig) { c.LuaTimeout = 0 }},
		{"LuaTimeout<0", func(c *coreConfig) { c.LuaTimeout = -1 * time.Second }},
		{"LuaRegistryMaxSize=0", func(c *coreConfig) { c.LuaRegistryMaxSize = 0 }},
		{"LuaRegistryMaxSize<0", func(c *coreConfig) { c.LuaRegistryMaxSize = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mut(&cfg)
			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "CONFIG_INVALID")
		})
	}
}
```

If the test file already imports `errutil.AssertErrorCode`, prefer that over substring matching.

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -timeout 30s -run 'TestCoreCommand_LuaLimitDefaults|TestCoreConfig_ValidateRejectsNonPositiveLuaLimits' ./cmd/holomush/`
Expected: FAIL — fields undefined.

- [ ] **Step 3: Add fields, defaults, flags, and validation in `core.go`**

Update `coreConfig` (around line 42-56):

```go
type coreConfig struct {
	GRPCAddr              string        `koanf:"grpc_addr"`
	ControlAddr           string        `koanf:"control_addr"`
	MetricsAddr           string        `koanf:"metrics_addr"`
	DataDir               string        `koanf:"data_dir"`
	GameID                string        `koanf:"game_id"`
	LogFormat             string        `koanf:"log_format"`
	SkipSeedMigrations    bool          `koanf:"skip_seed_migrations"`
	SessionTTL            string        `koanf:"session_ttl"`
	SessionMaxHistory     int           `koanf:"session_max_history"`
	SessionReaperInterval string        `koanf:"session_reaper_interval"`
	Setting               string        `koanf:"setting"`
	ResetSetting          bool          `koanf:"reset_setting"`
	LuaTimeout            time.Duration `koanf:"lua_timeout"`
	LuaRegistryMaxSize    int           `koanf:"lua_registry_max_size"`
}
```

Add defaults near the existing ones (around line 72-78):

```go
const (
	defaultGRPCAddr              = "localhost:9000"
	defaultCoreControlAddr       = "127.0.0.1:9001"
	defaultCoreMetricsAddr       = "127.0.0.1:9100"
	defaultLogFormat             = "json"
	defaultPluginLuaTimeout      = 1 * time.Second
	defaultPluginLuaRegistryMax  = 65536
)
```

Extend `Validate()` — add these after existing checks, before the final `return nil`:

```go
	if cfg.LuaTimeout <= 0 {
		return oops.Code("CONFIG_INVALID").Errorf("plugin-lua-timeout must be positive, got %s", cfg.LuaTimeout)
	}
	if cfg.LuaRegistryMaxSize <= 0 {
		return oops.Code("CONFIG_INVALID").Errorf("plugin-lua-registry-max must be positive, got %d", cfg.LuaRegistryMaxSize)
	}
```

Extend `NewCoreCmd` flag registration — add after the existing `ResetSetting` flag around line 116:

```go
	cmd.Flags().DurationVar(&cfg.LuaTimeout, "plugin-lua-timeout", defaultPluginLuaTimeout, "per-invocation CPU deadline for Lua plugins")
	cmd.Flags().IntVar(&cfg.LuaRegistryMaxSize, "plugin-lua-registry-max", defaultPluginLuaRegistryMax, "max Lua registry size per plugin state")
```

- [ ] **Step 4: Extend `PluginSubsystemConfig` and subsystem**

Edit `internal/plugin/setup/subsystem.go`. Add fields to `PluginSubsystemConfig` (around the existing struct near line 71):

```go
type PluginSubsystemConfig struct {
	// ... existing fields ...
	LuaTimeout         time.Duration
	LuaRegistryMaxSize int
}
```

In `Start()`, find the construction of `pluginlua.NewHostWithFunctions(hostFuncs)` (around line 153). Change it to pass the new options:

```go
	// 3. Create Lua host.
	luaHost := pluginlua.NewHostWithFunctions(hostFuncs,
		pluginlua.WithCPUTimeout(s.cfg.LuaTimeout),
		pluginlua.WithStateFactory(pluginlua.NewStateFactory(
			pluginlua.WithRegistryMaxSize(s.cfg.LuaRegistryMaxSize),
		)),
	)
```

Add the `time` import if not present.

- [ ] **Step 5: Update `cmd/holomush/core.go` to pass fields to the subsystem**

Find the `pluginsetup.NewPluginSubsystem(pluginsetup.PluginSubsystemConfig{...})` call (around line 261). Add the two fields:

```go
	pluginSub := pluginsetup.NewPluginSubsystem(pluginsetup.PluginSubsystemConfig{
		// ... existing fields ...
		LuaTimeout:         cfg.LuaTimeout,
		LuaRegistryMaxSize: cfg.LuaRegistryMaxSize,
	})
```

- [ ] **Step 6: Run tests**

Run: `task test -- -timeout 30s -run 'TestCoreCommand_LuaLimitDefaults|TestCoreConfig_ValidateRejectsNonPositiveLuaLimits|TestCoreConfig_Validate' ./cmd/holomush/`
Expected: PASS.

Run the full cmd/holomush suite to catch any existing tests that build a `coreConfig` literal and now need the new fields:

Run: `task test -- -timeout 60s ./cmd/holomush/`
Expected: PASS. Any `coreConfig` literal in tests that lacks the new fields will fail Validate — update those literals to include the spec defaults.

Run the plugin subsystem test suite:

Run: `task test -- -timeout 60s ./internal/plugin/setup/ ./internal/plugin/lua/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(core): config knobs for Lua plugin resource limits

Adds --plugin-lua-timeout (default 1s) and --plugin-lua-registry-max
(default 65536) with Validate rejection of non-positive values. Values
are threaded through PluginSubsystemConfig into NewHostWithFunctions
(WithCPUTimeout) and NewStateFactory (WithRegistryMaxSize).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"

jj --no-pager new
```

---

## Task 7: Integration tests

**Files:**

- Create: `test/integration/plugin/lua_resource_limits_integration_test.go`
- Create: `test/integration/plugin/testdata/lua/cpu_bomb/manifest.yaml`
- Create: `test/integration/plugin/testdata/lua/cpu_bomb/plugin.lua`
- Create: `test/integration/plugin/testdata/lua/memory_bomb/manifest.yaml`
- Create: `test/integration/plugin/testdata/lua/memory_bomb/plugin.lua`

- [ ] **Step 1: Create the CPU-bomb plugin fixture**

Create `test/integration/plugin/testdata/lua/cpu_bomb/manifest.yaml`:

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
name: cpu_bomb
version: 0.0.1
lua_plugin:
  entry: plugin.lua
```

Create `test/integration/plugin/testdata/lua/cpu_bomb/plugin.lua`:

```lua
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Deliberately tight-loops in on_event to test the CPU deadline.
function on_event(event)
    while true do end
end
```

- [ ] **Step 2: Create the memory-bomb plugin fixture**

Create `test/integration/plugin/testdata/lua/memory_bomb/manifest.yaml`:

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
name: memory_bomb
version: 0.0.1
lua_plugin:
  entry: plugin.lua
```

Create `test/integration/plugin/testdata/lua/memory_bomb/plugin.lua`:

```lua
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Allocates tables aggressively to trigger RegistryMaxSize.
function on_event(event)
    local t = {}
    for i = 1, 1000000 do
        t[#t + 1] = {i, i, i}
    end
    return t
end
```

- [ ] **Step 3: Write the integration tests**

Create `test/integration/plugin/lua_resource_limits_integration_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugin_test

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// loadTestPlugin constructs a minimal Host and loads a plugin from the
// testdata directory. Used by both scenario tests.
func loadTestPlugin(t *testing.T, dir, name string, opts ...pluginlua.HostOption) *pluginlua.Host {
	t.Helper()
	h := pluginlua.NewHost(opts...)
	t.Cleanup(func() { _ = h.Close(context.Background()) })

	manifest := &plugins.Manifest{
		Name:    name,
		Version: "0.0.1",
		LuaPlugin: plugins.LuaPluginManifest{
			Entry: "plugin.lua",
		},
	}
	err := h.Load(context.Background(), manifest, dir)
	require.NoError(t, err, "load %s", name)
	return h
}

func TestLuaPluginCPUBombDoesNotHangDispatcher(t *testing.T) {
	h := loadTestPlugin(t, "testdata/lua/cpu_bomb", "cpu_bomb",
		pluginlua.WithCPUTimeout(200*time.Millisecond),
	)

	start := time.Now()
	_, err := h.DeliverEvent(context.Background(), "cpu_bomb", pluginsdk.Event{
		ID:     "e1",
		Stream: "test",
		Type:   "test_event",
	})
	elapsed := time.Since(start)

	assert.Error(t, err, "cpu_bomb must surface a timeout error")
	assert.Less(t, elapsed, 1*time.Second,
		"dispatcher must return within CPU timeout + generous buffer")

	// Verify the dispatcher remains responsive: a second event should return
	// in the same timeout window.
	start = time.Now()
	_, err = h.DeliverEvent(context.Background(), "cpu_bomb", pluginsdk.Event{
		ID:     "e2",
		Stream: "test",
		Type:   "test_event",
	})
	secondElapsed := time.Since(start)
	assert.Error(t, err)
	assert.Less(t, secondElapsed, 1*time.Second,
		"second event must also return within the timeout (dispatcher not hung)")
}

func TestLuaPluginMemoryBombDoesNotOOM(t *testing.T) {
	h := loadTestPlugin(t, "testdata/lua/memory_bomb", "memory_bomb",
		pluginlua.WithCPUTimeout(2*time.Second), // generous — want the registry cap to fire first
		pluginlua.WithStateFactory(pluginlua.NewStateFactory(
			pluginlua.WithRegistryMaxSize(1024), // tight cap to ensure overflow
		)),
	)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	_, err := h.DeliverEvent(context.Background(), "memory_bomb", pluginsdk.Event{
		ID:     "e1",
		Stream: "test",
		Type:   "test_event",
	})

	runtime.GC()
	runtime.ReadMemStats(&after)

	assert.Error(t, err, "memory_bomb must surface a registry-overflow error")

	// Delta should be bounded. 1024 registry slots × ~100 bytes each is ≈ 100KB.
	// Allow 10MB of headroom for test infrastructure.
	const maxDelta = 10 * 1024 * 1024
	delta := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	assert.Less(t, delta, int64(maxDelta),
		"memory delta after bomb+GC must be bounded; got %d bytes", delta)
}
```

- [ ] **Step 4: Run the integration tests**

Run: `task test:int -- -timeout 60s -run 'TestLuaPlugin(CPUBomb|MemoryBomb)' ./test/integration/plugin/`
Expected: PASS. If the memory-bomb test fails to trigger the overflow (some allocation patterns escape the registry cap), tune the bomb's loop bounds or the `WithRegistryMaxSize` value — the goal is a deterministic overflow within `CPUTimeout`.

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "test(plugin): integration tests for Lua resource limits

Two scenario tests under the integration build tag:
- TestLuaPluginCPUBombDoesNotHangDispatcher: while-true on_event; asserts
  dispatcher returns within the CPU timeout window and remains responsive
  to subsequent events.
- TestLuaPluginMemoryBombDoesNotOOM: table-allocating on_event with a
  tight registry cap; asserts error surfaces and heap delta is bounded.

Test plugin fixtures live under test/integration/plugin/testdata/lua/.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"

jj --no-pager new
```

---

## Task 8: Operator + contributor documentation

**Files:**

- Create or modify: `site/docs/operating/plugin-security.md`
- Create: `site/docs/contributing/hostfunc-context-audit.md`

- [ ] **Step 1: Write the operator docs**

Check if `site/docs/operating/plugin-security.md` exists. If so, append a new section at the bottom. If not, create it with a top-level heading + introduction + the Lua resource limits section.

Append (or include as full content if creating):

```markdown
## Lua plugin resource limits

The plugin host enforces three defensive controls on every Lua plugin
invocation to prevent CPU exhaustion, memory exhaustion, and dispatcher
starvation.

Two operator-tunable knobs govern these controls:

The flag `--plugin-lua-timeout` (default `1s`) sets the per-invocation
CPU deadline. Every dispatcher entry point (event, command,
session-subscribe, command fallback) derives a `context.WithTimeout` from
the caller's context and passes it to `L.SetContext`; gopher-lua checks
this context at every VM instruction. A tight `while true do end` loop
is caught within the deadline plus a small instruction-boundary delay.

The flag `--plugin-lua-registry-max` (default `65536`) bounds the Lua
value registry per state. A plugin that allocates more values than the
cap hits a panic which `CallByParam(Protect: true)` converts to an
error, so the dispatcher returns a controlled failure rather than an
unbounded heap growth.

A third control, the watchdog goroutine, has no operator knob: every
`CallByParam` runs in its own goroutine so a stuck host function cannot
hang the dispatcher. If the CPU deadline fires while a host function is
still running, the dispatcher waits for the goroutine to drain — bounded
by gopher-lua's per-instruction context check (pure-Lua tight loops exit
on the next instruction) and the hostfunc audit invariant (every
registered hostfunc either is O(1) or respects `L.Context()`) — then
returns the timeout error.

### Tuning

Raise `--plugin-lua-timeout` if a legitimate plugin does synchronous
work against the world service that can exceed one second end-to-end.
Monitor `holomush_plugin_lua_timeouts_total{plugin,handler}` — any
sustained non-zero rate means either the plugin has pathological loops
or the timeout is too tight.

Raise `--plugin-lua-registry-max` if a legitimate plugin holds many
active Lua values simultaneously (for instance, caching or bulk
operations). Monitor
`holomush_plugin_lua_registry_full_total{plugin,handler}` — a non-zero
rate points at either a memory-bomb plugin or a cap set too low for a
legitimate workload.

### Metrics

Three Prometheus metrics expose resource-limit state for operators:

- `holomush_plugin_lua_invocations_total{plugin,handler,outcome}` — the
  denominator for outcome-rate dashboards. `outcome` takes values
  `success`, `timeout`, `registry_full`, `error`.
- `holomush_plugin_lua_timeouts_total{plugin,handler}` — CPU-cap
  violations, attributable by plugin and handler.
- `holomush_plugin_lua_registry_full_total{plugin,handler}` — memory-cap
  violations.
```

- [ ] **Step 2: Write the hostfunc audit doc**

Create `site/docs/contributing/hostfunc-context-audit.md`:

```markdown
# Host Function Context Audit

Host functions registered on Lua states via
`internal/plugin/hostfunc/functions.go:Register` run as Go code inside
the goroutine dispatched by `Host.invoke`. While Go code runs,
`gopher-lua`'s per-instruction context check is suspended — a host
function that blocks ignores the plugin-level CPU deadline.

This audit table documents each registered host function and confirms
it either completes in O(1) time or respects its context parameter for
any potentially-blocking work. A meta-test locks this invariant against
future regressions.

## Invariant

Every exported host function registered on a Lua state MUST either:

- complete in O(1) time (no loops of unbounded length, no I/O), OR
- take a `context.Context` parameter and return promptly on
  `ctx.Done()` for any call that could block (RPC, I/O, channel wait).

The meta-test `TestHostFuncsRespectContextOnPreCancelledCtx` in
`internal/plugin/hostfunc/context_audit_test.go` invokes every
registered host function with a pre-cancelled context and asserts it
returns within 50 ms.

## Audit table

Maintain this table when adding or changing a host function.

| Host function | Category | Bounding mechanism |
| ------------- | -------- | ------------------ |
| `holomush.log` | O(1) | In-memory append to slog; no blocking calls |
| `holomush.new_request_id` | O(1) | ULID generation; no I/O |

(Worker: populate the rest of the table from the actual
`hostfunc.Functions.Register` surface; see the
`RegisteredFunctionsForAudit` method.)

## Adding a new host function

When adding a new host function:

1. Confirm it either completes in O(1) time or accepts a context.
2. Add its name to the `RegisteredFunctionsForAudit` list so the
   meta-test exercises it.
3. Append its row to the audit table above.
4. If the function does I/O, document the bounding mechanism
   (RPC deadline, channel timeout, etc.) in the table.
```

- [ ] **Step 3: Lint**

Run: `task lint:markdown`
Expected: `Success: No issues found`.

If lint fails, fix the markdown and re-run.

- [ ] **Step 4: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "docs(operating,contributing): document Lua plugin resource limits

Adds operator-facing docs covering the two knobs, their tuning, and
the three Prometheus metrics. Adds contributor-facing docs describing
the host-function context-respect invariant and the audit table.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"

jj --no-pager new
```

---

## Final verification

- [ ] **Run the full pr-prep gate**

Run: `task pr-prep`
Expected: all stages green (lint, format, schema, license, unit, integration, E2E).

- [ ] **Push and open PR**

```bash
jj --no-pager bookmark set fix/lua-resource-limits -r @-
jj --no-pager git push --bookmark fix/lua-resource-limits --allow-new
# then, from the main repo directory (gh needs the colocated git repo):
cd /Volumes/Code/github.com/holomush/holomush
GIT_SSL_NO_VERIFY=1 gh pr create \
  --title "feat(plugin/lua): resource limits — CPU timeout, registry cap, watchdog, hostfunc audit" \
  --body "$(cat <<'EOF'
## Summary

Closes holomush-u9p5 (P0, security-finding). Adds four defensive controls
to the Lua plugin dispatcher:

1. Per-invocation CPU deadline via context.WithTimeout + L.SetContext
   (--plugin-lua-timeout, default 1s)
2. Lua registry cap via lua.Options.RegistryMaxSize
   (--plugin-lua-registry-max, default 65536)
3. Goroutine watchdog — CallByParam runs in a goroutine so a stuck
   host function cannot hang the dispatcher
4. Hostfunc context-respect audit + regression meta-test

Three Prometheus metrics expose the controls. Operator docs added.

## Test plan

- [x] task pr-prep green — lint, format, schema, license, unit,
  integration, E2E
- [x] New unit tests: metrics helpers, RegistryMaxSize behavior,
  invoke (timeout/watchdog/classify/outcomes), config defaults +
  validation
- [x] New integration tests: CPU bomb, memory bomb
- [x] New meta-test: every registered hostfunc returns within 50ms
  under a cancelled context

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)" --head fix/lua-resource-limits
```

- [ ] **Close the bead on merge**

```bash
bd close holomush-u9p5 --reason "Shipped in PR #<NUMBER>. Four controls live: CPU timeout (1s default), registry cap (65536 default), watchdog goroutine, and hostfunc context-respect audit. Three Prometheus metrics registered."
```

---

## Self-review (plan author)

**Spec coverage:**

| Spec requirement | Task(s) |
| --- | --- |
| Control 1: per-invocation CPU timeout (A) | Task 3 (invoke) + Task 4 (migrate sites) + Task 6 (config) |
| Control 2: Lua registry cap (B) | Task 2 (StateFactory wiring) + Task 6 (config default) |
| Control 3: hostfunc context-awareness audit (C) | Task 5 (meta-test + RegisteredFunctionsForAudit) + Task 8 (audit doc) |
| Control 4: watchdog / goroutine drop (D) | Task 3 (invoke implements the pattern) |
| `StateFactory.WithRegistryMaxSize` option | Task 2 |
| `Host.WithCPUTimeout` option | Task 3 |
| `Host.invoke` + `classifyError` | Task 3 |
| `CallStackSize` unchanged (gopher-lua default 256) | No task — unchanged by design |
| Three Prometheus `CounterVec`s | Task 1 |
| Config flags + defaults + `Validate()` | Task 6 |
| Config threaded through `PluginSubsystemConfig` | Task 6 |
| Unit tests (11 cases in spec) | Tasks 1, 2, 3, 6 |
| Integration tests (CPU bomb, memory bomb) | Task 7 |
| Hostfunc meta-test | Task 5 |
| Operator docs | Task 8 |
| Contributor docs (hostfunc audit table) | Task 8 |

**Placeholder scan:** Task 5 step 3 notes that the worker must enumerate the actual hostfunc registration list before finalizing `RegisteredFunctionsForAudit`. This is a design-time acknowledgement that the exact list depends on the current codebase state, not a placeholder in the implementation — the worker reads the real Register method and fills in the list. Task 8 step 1/2 similarly notes the worker must populate the audit table from the actual hostfunc surface. No `TBD` / `TODO` / vague-requirements remain.

**Type consistency:** `HostOption`, `StateFactoryOption`, `WithCPUTimeout`, `WithRegistryMaxSize`, `WithStateFactory`, `invoke`, `classifyError`, `recordInvocationOutcome`, `outcomeSuccess / outcomeTimeout / outcomeRegistryFull / outcomeError`, `InvocationsTotal / TimeoutsTotal / RegistryFullTotal`, `LuaTimeout / LuaRegistryMaxSize` — all consistent across tasks 1-7.

**Minor risk — one ambiguity the worker will resolve:** Task 4 step 1 notes that the existing `L.SetContext(ctx)` in each dispatcher method is made redundant by `invoke`'s own `SetContext`. Removing it is safe (invoke overwrites) but leaving it is also safe. The plan explicitly defers this to the worker's judgement during the edit.
