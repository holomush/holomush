---
phase: 08-god-object-decomposition
reviewed: 2026-07-19T00:00:00Z
depth: deep
files_reviewed: 20
files_reviewed_list:
  - internal/grpc/server.go
  - internal/grpc/subscribe_handler.go
  - internal/grpc/command_handler.go
  - internal/grpc/lifecycle_handler.go
  - internal/grpc/query_handler.go
  - internal/grpc/list_focus_presence.go
  - internal/grpc/list_available_commands.go
  - internal/grpc/list_session_streams.go
  - internal/grpc/query_stream_history.go
  - internal/grpc/refresh_connection.go
  - internal/grpc/session_identity.go
  - internal/grpc/auth_handlers.go
  - internal/grpc/focus/coordinator.go
  - internal/grpc/focus/kind_policy.go
  - internal/grpc/focus/auto_focus_on_join.go
  - internal/plugin/manager.go
  - internal/plugin/loader.go
  - internal/plugin/runtime.go
  - internal/plugin/identity_store.go
  - internal/plugin/manager_unload.go
  - internal/plugin/manager_unload.go
  - internal/plugin/export_test.go
  - internal/plugin/goplugin/host.go
  - internal/plugin/lua/hostcap_adapter.go
  - internal/plugin/lua/focus_ops_adapter.go
  - internal/plugin/host.go
  - internal/plugin/hostcap/capabilities.go
  - internal/plugin/setup/subsystem.go
  - internal/focuscontract/focuscontract.go
  - internal/eventbus/authguard/adapter_manifest.go
  - internal/testsupport/integrationtest/crypto.go
  - test/meta/phase8_decomposition_test.go
findings:
  critical: 0
  blocker: 0
  warning: 6
  info: 0
  total: 6
status: issues_found
---

# Phase 8: Code Review Report

**Reviewed:** 2026-07-19
**Depth:** deep (cross-file; every moved body diffed against `origin/main` after receiver normalization)
**Files Reviewed:** 20 production files + 1 meta test
**Status:** issues_found (no blockers)

## Summary

This is a genuinely careful refactor. I mechanically diffed every function that moved
out of `internal/grpc/server.go` and `internal/plugin/manager.go` against its
`origin/main` body with receivers normalized away, and the result is close to
byte-identical: the only body-level deltas are the intended delegation rewrites
(`toSubject` → `qualifyStreamSubject`, `runDisconnectHooks` → `fireDisconnectHooks`,
`pluginRepo`/`nameByID`/`activeByName` → `identity.*`, `loaded`/`inflight`/
`pluginHosts` → `runtime.*`). I found **no dropped argument, no reordered parameter
and no wrong-receiver delegation**.

Specifically checked and clean:

- **Asymmetric nil-collaborator semantics survived intact.** `accessEngine` nil ⇒
  `PERMISSION_DENIED` (`list_focus_presence.go:100`) while `characterNameResolver`
  nil ⇒ `INTERNAL` (`list_focus_presence.go:108`), two guards apart, both preserved.
  `commandQuerier` nil ⇒ `PERMISSION_DENIED` (`list_available_commands.go:44`),
  `sceneMute` nil ⇒ fail-open, `accessEngine` nil in `QueryStreamHistory`'s default
  branch ⇒ `STREAM_ACCESS_DENIED`. None homogenised.
- **`IdentityStore.Sweep` is behaviourally equivalent to the inlined sweep** it
  replaced: the `repo == nil || retentionDays <= 0` guard moved inside
  (`identity_store.go:185`), so `LoadAll`'s now-unconditional call is a no-op in the
  same cases the old `if` skipped.
- **`SetIdentityRegistry(l.identity)` is safe.** `IdentityRegistry` is exactly
  `NameByID`/`IDByName` (`host.go:195`), and `Manager`'s versions were already
  one-line forwards into the same maps.
- **`TestLoadPlugin`'s `name`-vs-`manifest.Name` keying was correctly preserved** via
  a dedicated `testCommitNamed` seam (`export_test.go:47-60`) rather than routed
  through `CommitLoaded`. That is the right call and the comment explains why.
- **`internal/focuscontract` is a faithful move** — every type is re-exported from
  `internal/grpc/focus` as a `=` alias, so the two spellings name identical types.
- **`authguard.NewPluginManifestLookup` deletion is safe**: the typed-nil fail-closed
  contract it carried now lives on `Manager.PluginRequestsDecryption`/`PluginCanReadBack`
  (`manager.go:629,643`) *and* on `PluginRuntime`'s copies (`runtime.go:467,492`), so a
  `&Manager{}` fixture with a nil runtime still denies rather than faulting.
- `task build`, `task test -- ./internal/plugin/... ./internal/grpc/... ./test/meta/...`,
  and `task lint` all exit 0.

The six findings below are all WARNING. The most substantive is W-01: a
previously-atomic wiring step is now split across two locks in a way that can
permanently drop a host's event emitter. It is not reachable on today's
single-threaded startup path, which is why it is not a blocker — but the code's own
comment claims "Program order is preserved", and that claim is not true for the
interleaving described.

## Warnings

### WR-01: `ConfigureEventEmitter` / `RegisterHost` lost-update — a host can end up permanently without an event emitter

**File:** `internal/plugin/loader.go:184-201`, `internal/plugin/loader.go:222-236`

**Issue:** On `origin/main` both operations ran wholly inside one `m.mu` section:
`ConfigureEventEmitter` stored `m.eventEmitter` and iterated `m.hosts` under the same
lock (`manager.go:377-391`), and `RegisterHost` read `m.eventEmitter` under that same
lock (`manager.go:288-296`). Post-split, the emitter lives in `PluginRuntime` behind a
different lock, so both sites hoist their runtime access *outside* `l.mu`:

- `RegisterHost` reads the emitter at `loader.go:185` **before** `l.mu.Lock()` at `:187`.
- `ConfigureEventEmitter` stores the emitter at `loader.go:223` **before** `l.mu.Lock()` at `:225`.

That admits an interleaving with no equivalent on `origin/main`:

1. `RegisterHost` reads `emitter == nil` (line 185) — `ConfigureEventEmitter` has not run.
2. `ConfigureEventEmitter` stores the emitter (line 223), takes `l.mu`, iterates
   `l.hosts` (line 227) — the new host is not in the map yet, so it is skipped.
3. `RegisterHost` takes `l.mu`, inserts into `l.hosts` (line 189), sees its stale
   `emitter == nil` (line 190) and skips `SetEventEmitter`.

The host is registered and never receives the emitter. For a binary host that means
its gRPC `EmitEvent` path has no emitter for the process lifetime. This is a lost
update, not a benign widened window, and it is invisible to any test that wires
sequentially.

The same hoist makes the `l.hosts` write (`:189`) and the `hostCaps` write (inside
`CacheHostCapabilities`, `:184`) non-atomic. That one is genuinely benign —
`capabilitiesForLocked` has a discover-on-demand fallback (`runtime.go:199`) — and the
comment at `:172-183` says so. The emitter case has no such fallback and the comment
does not distinguish them.

Not a blocker because production wiring (`cmd/holomush/sub_grpc.go`, `internal/plugin/setup`)
calls `RegisterHost` and `ConfigureEventEmitter` sequentially from one goroutine.

**Fix:** Either document the single-threaded-wiring precondition as load-bearing (and
say that `RegisterHost` is not safe to call concurrently with `ConfigureEventEmitter`,
rather than the current "Program order is preserved"), or close the window by
re-reading the emitter under `l.mu`:

```go
// RegisterHost — read the emitter INSIDE the section that publishes the host,
// so a concurrent ConfigureEventEmitter cannot slip between the read and the
// map insert. runtime.EventEmitter() takes only the runtime's lock, which this
// path does not otherwise hold, so no ordering is created.
l.mu.Lock()
defer l.mu.Unlock()
l.hosts[hostType] = host
if emitter := l.runtime.EventEmitter(); emitter != nil {
    ...
}
```

(Note this reintroduces a runtime-lock acquisition under `l.mu`. If the "never two
unit locks at once" rule is to hold literally, the alternative is an explicit
wiring-phase mutex or a documented precondition — but the current shape is neither.)

### WR-02: `INV-PLUGIN-56` is bound by a test that covers 4 of the 11 production packages its summary claims

**File:** `test/meta/phase8_decomposition_test.go:65-91`, `docs/architecture/invariants.yaml:4078-4086`

**Issue:** The registry entry states the invariant universally: *"no production package
in the `internal/plugin` tree may import the `internal/grpc` tree"*. The binding test
enumerates a hard-coded list of four `fromRel` values — `internal/plugin`,
`internal/plugin/lua`, `internal/plugin/goplugin`, `internal/plugin/hostcap`.

The tree actually holds these additional production packages, none of which the
ratchet inspects: `hostfunc`, `pluginauthz`, `setup`, `cryptowiring`, `dispatchwire`,
`luabridge`, `luabridge/gen`, `plugintest`, `gen-schema`, `mocks`, `hostfunc/mocks`.
I verified none of them imports `internal/grpc` today (only `_test.go` files do, which
is correctly exempt), so the invariant currently *holds* — but a new edge added from,
say, `internal/plugin/setup` would pass CI silently.

This is the partial-binding case `.claude/rules/invariants.md` calls out explicitly:
"it cannot detect a *partial* binding (a test that asserts only one clause of a
multi-clause invariant — that needs human review)". Since `importUnder` already
prefix-matches on the *target*, the same generalization on the *source* is cheap.

**Fix:** Walk the tree instead of enumerating it, so a new subpackage is covered on
the day it is created:

```go
// Enumerate every production package under internal/plugin rather than a fixed
// list — a new subpackage must inherit the guard, not opt into it.
for _, pkgRel := range walkGoPackages(t, root, "internal/plugin") {
    imports := worldPkgImports(t, root, pkgRel)
    got, found := importUnder(imports, "internal/grpc")
    require.Falsef(t, found, "forbidden edge: %s -> %s", pkgRel, got)
}
```

Keep the seam-2 rows (`internal/eventbus/authguard` → `internal/plugin` and its
mirror) as explicit entries; they are directional and not derivable from a walk.

### WR-03: The facade-structure assertion matches gofmt column alignment by exact substring

**File:** `test/meta/phase8_decomposition_test.go:280-289`

**Issue:** `TestPhase8FacadesHoldNoExtractedState` asserts `CoreServer` holds its four
units via `require.Containsf(string(serverBody), "commandHandler   *CommandHandler")`
— with the literal three-space run gofmt currently emits. Adding any `CoreServer` field
whose name is longer than `lifecycleHandler` re-aligns the whole block and fails all
four assertions with a message ("must hold each extracted unit as a field") that points
at the wrong cause. That is a brittle gate that will be "fixed" by editing the test,
which is the failure mode the file's own header warns against.

Note the sibling `Manager` assertion at `:272-276` already does this correctly via
`structFieldNames` + `ElementsMatch`, which is whitespace-independent.

**Fix:** Reuse the existing helper rather than substring-matching formatted source:

```go
serverFields := structFieldNames(t, filepath.Join(root, "internal/grpc/server.go"), "CoreServer")
for _, f := range []string{"subscribeHandler", "commandHandler", "lifecycleHandler", "queryHandler"} {
    require.Containsf(t, serverFields, f,
        "CoreServer must hold each extracted unit as a field (ARCH-01): %q missing", f)
}
```

### WR-04: `runDisconnectHooks`'s nil-receiver guard converts a wiring-order regression into silent hook loss

**File:** `internal/grpc/lifecycle_handler.go:368-371`, `internal/grpc/server.go:395-400`

**Issue:** `buildHandlers` captures `s.lifecycleHandler.runDisconnectHooks` as a method
value (`server.go:460`) and `s.lifecycleHandler.recomputeSessionLiveness` (`server.go:490`).
Go permits taking a method value on a nil pointer receiver without panicking at capture
time — the fault would occur at call time. The `if h == nil { return }` guard at
`lifecycle_handler.go:369` removes even that.

Consequence: if the documented-as-load-bearing construction order at `server.go:396-399`
is ever reordered so `newCommandHandler`/`newSubscribeHandler` runs before
`newLifecycleHandler`, every quit and admin-boot teardown would stop running its
disconnect hooks — with no panic, no log line, and no failing test. The order comment
(`server.go:386-389`) is the only thing holding it.

The guard's stated rationale (bare `&CoreServer{}` test fixtures previously ranged a nil
`disconnectHooks` slice and no-opped) is legitimate, which is why this is a warning and
not a blocker. The problem is that it also silences a production wiring bug.

**Fix:** Make the ordering mechanically enforced rather than comment-enforced — e.g.
have `buildHandlers` assert its own precondition before the dependent constructors:

```go
func (s *CoreServer) buildHandlers() {
    s.lifecycleHandler = s.newLifecycleHandler()
    s.queryHandler = s.newQueryHandler()
    if s.lifecycleHandler == nil || s.queryHandler == nil {
        panic("grpc.buildHandlers: lifecycle and query handlers must be built before their consumers")
    }
    s.commandHandler = s.newCommandHandler()
    s.subscribeHandler = s.newSubscribeHandler()
}
```

...and keep the nil guard scoped to what it is actually for (a comment naming the
fixture pattern, not "defensive").

### WR-05: `Subscribe` silently skips session-liveness recompute when the injected recomputer is nil — new tolerance, not preserved behavior

**File:** `internal/grpc/subscribe_handler.go:455-456`

**Issue:** `origin/main` called `s.recomputeSessionLiveness(ctx, req.GetSessionId())`
unconditionally whenever `req.GetConnectionId() != ""` (`server.go` pre-split). The
extracted version adds `&& h.recomputeLiveness != nil`. `SubscribeDeps.RecomputeLiveness`
documents "A nil recomputer is a no-op" (`subscribe_handler.go:43`), but this is a new
behavior, not a preserved one: a `SubscribeHandler` built without it now completes the
`add_connection` handshake while leaving the session's `status` / `grid_present`
un-recomputed. That is silently-wrong session state, not a degraded read.

Production always wires it (`server.go:490`), so this is not currently reachable — but
the failure is silent, and the new external `subscribe_handler_test.go` package is
exactly the place where a fixture will omit it.

**Fix:** Fail loudly on the unconfigured path rather than skipping, so a mis-wired unit
is caught at the first Subscribe:

```go
if req.GetConnectionId() != "" {
    if h.recomputeLiveness == nil {
        return oops.Code("NOT_CONFIGURED").Errorf("session liveness recomputer not configured")
    }
    if liveErr := h.recomputeLiveness(ctx, req.GetSessionId()); liveErr != nil {
        ...
    }
}
```

If a nil recomputer must stay tolerated for fixtures, at minimum log it at WARN so it
is not invisible.

### WR-06: `BuildFocusRedirects` reads `loadedOrder` without the lock the file's own discipline note claims guards it

**File:** `internal/plugin/loader.go:439-441` (writer at `:1079-1081`, correct reader at `:423-426`)

**Issue:** `PluginLoader`'s lock-discipline doc states `l.mu` "guards exactly the state
that mutex guarded ... hosts, luaHost and loadedOrder" (`loader.go:87`). `seedAliases`
honours that — it takes `l.mu.RLock()` and copies the slice (`:423-426`). `loadPlugin`
appends under `l.mu.Lock()` (`:1079-1081`). But `BuildFocusRedirects` passes
`l.loadedOrder` straight into `CollectFocusRedirects` with no lock at all (`:440`),
handing an unsynchronized slice header to a function that ranges it.

This is carried over verbatim from `origin/main` (`manager.go:876`) and is therefore not
a regression — but the extraction is precisely the moment the invariant got written
down, and the code now contradicts the paragraph directly above it. Both callers run at
wiring time today (dispatcher setup after `LoadAll`), so no live race exists.

**Fix:** Mirror `seedAliases`:

```go
func (l *PluginLoader) BuildFocusRedirects(registry *command.Registry) (command.FocusRedirectTable, error) {
	l.mu.RLock()
	ordered := make([]*DiscoveredPlugin, len(l.loadedOrder))
	copy(ordered, l.loadedOrder)
	l.mu.RUnlock()
	return CollectFocusRedirects(ordered, registry)
}
```

---

## Notes on things deliberately not flagged

- `Close`'s `l.mu` release between the host-close loop and `runtime.Clear()`
  (`loader.go:1125-1130`) widens a window relative to `origin/main`'s single critical
  section, but the resulting interleaving (a concurrent `CommitLoaded` landing *before*
  the clear rather than after it) is strictly better than the pre-split behavior, which
  leaked the entry into a closed host's maps. Not a finding.
- `loadPlugin`'s split commit — `CommitLoaded` under the runtime lock, then the
  `loadedOrder` append under `l.mu` (`loader.go:1077-1082`) — is non-atomic by
  construction. `LoadAll` loads serially, so nothing observes the gap. Documented in
  place at `:1064-1076`. Not a finding.
- `slog.Warn` (bare, non-`Context`) at `loader.go:412`, `loader.go:504`,
  `runtime.go:579`, `runtime.go:586`: all four are in functions with no `ctx` parameter
  and all four travelled verbatim from `origin/main`. `sloglint`'s `context: scope`
  policy passes. Pre-existing, out of scope for this diff.
- `UnloadPlugin` still leaves a stale `loadedOrder` entry. Pre-existing on `origin/main`.
- No `status.Errorf(codes.X, "...%v", err)` inner-error leaks were introduced in any of
  the four new gRPC handler files — the `.claude/rules/grpc-errors.md` surface is clean.
- Per the review brief I did not re-derive `crypto-reviewer`'s READY on the manifest
  gates, `gsd-verifier`'s 3/3, or `internal/access/` (empty diff). Nothing I found
  contradicts any of them.

---

_Reviewed: 2026-07-19_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
