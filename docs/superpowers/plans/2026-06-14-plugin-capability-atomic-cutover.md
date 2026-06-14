<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Plugin-capability atomic cutover + o262d settlement — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the plugin host from coexistence (legacy unconditional Lua host-function injection + opt-in brokered allowlist) to a single declaration-gated, host-brokered capability-consumption path for both runtimes, in one atomic flip, and settle the `o262d` loader-policy bug.

**Architecture:** The unified resolver becomes the single least-privilege grant authority (`ResolveResult.Grants`); both delivery shims (Lua `RegisterHostCaps`, binary broker wiring) consume that grant instead of re-deriving from the manifest (binds `INV-PLUGIN-45`). Prerequisite host-cap backings are completed (`WorldMutationService` server + `FindLocation`; `SessionAdmin` backing; bufconn actor-identity stamping) so the gated path serves every capability the migrated manifests declare. The legacy capability injection is then stripped and the `WithHostCapBridge` allowlist removed in a single flip; language stdlib stays unconditional. `o262d` fail-fast (already live) is refactored behind an explicit policy function so a future `gracefulDegradation` quarantine is a one-point swap.

**Tech Stack:** Go, gRPC (`hashicorp/go-plugin` broker + in-process bufconn for Lua), gopher-lua, `oops` structured errors, testify + Ginkgo, `task` runner.

**Spec:** `docs/superpowers/specs/2026-06-14-plugin-capability-atomic-cutover-design.md`
**Design bead:** `holomush-eykuh.4` (epic). Several tasks map to existing children — noted per task so `plan-to-beads` maps rather than duplicates.

---

## File Structure

| File | Responsibility | Tasks |
| --- | --- | --- |
| `internal/plugin/hostcap/world.go` | `worldServer.FindLocation` + new `worldMutationServer` (CreateLocation/CreateExit/CreateObject) | 1 |
| `internal/plugin/hostcap/register.go` | register `WorldMutationService` in `LuaDefaultSet` | 1 |
| `internal/plugin/lua/host.go` | thread wide session-admin backing into `lua.Host`; consume resolver grants | 2, 5 |
| `internal/plugin/lua/hostcap_adapter.go` | `SessionAdmin()` returns real backing | 2 |
| `internal/plugin/lua/bufconn_endpoint.go` | chain an actor-stamping interceptor before the capability interceptor | 3 |
| `internal/plugin/lua/actor_interceptor.go` (new) | unary interceptor stamping `core.WithActor` from host-established identity | 3 |
| `internal/plugin/dependency.go` | `ResolveResult.Grants` field + per-plugin grant computation | 4 |
| `internal/plugin/manager.go` | expose grants to the load path; o262d policy function | 5, 7 |
| `internal/plugin/goplugin/host.go` | binary broker wiring consumes grants | 5 |
| `plugins/core-building/plugin.yaml`, `plugins/core-objects/plugin.yaml`, `plugins/core-communication/plugin.yaml` | declare missing capabilities | 6 |
| `internal/plugin/hostfunc/functions.go` | strip capability injection; keep stdlib | 8 |
| `internal/plugin/luabridge/marshal_test.go` | `pushBridgeError` opacity test | 10 |

---

## Task 1: Implement `WorldQueryService.FindLocation` + `WorldMutationService` server

> Maps existing bead **holomush-eykuh.4.1** (P1, hard blocker). Both proto + generated stubs already exist (`api/proto/holomush/plugin/host/v1/world.proto:43,58-68`; `pkg/proto/holomush/plugin/host/v1/world_grpc.pb.go`). `worldServer` (`internal/plugin/hostcap/world.go:32`) implements only the 4 query RPCs; `FindLocation` + the entire `WorldMutationService` inherit `Unimplemented`. The `WorldMutator` backing already flows to Lua via `luaHostCapAdapter.WorldMutator()` (`internal/plugin/lua/hostcap_adapter.go`) and is `world.Mutator` (`CreateLocation(ctx, string, *world.Location) error`, `CreateExit`, `CreateObject`, `FindLocationByName`).

**Files:**

- Modify: `internal/plugin/hostcap/world.go`
- Modify: `internal/plugin/hostcap/register.go:53` (LuaDefaultSet branch)
- Test: `internal/plugin/hostcap/world_test.go`

- [ ] **Step 1: Write the failing test for `FindLocation`**

In `world_test.go`, mirror the existing `QueryLocation` test setup (same file). Use a fake `WorldMutator` whose `FindLocationByName` returns a known location id for input `"plaza"` and `world.ErrNotFound` for `"void"`.

```go
func TestWorldServerFindLocationReturnsMatchedLocation(t *testing.T) {
    s := hostcap.NewWorldQueryServer(newFakeBaseWithMutator(t, fakeMutator{
        findByName: map[string]string{"plaza": "loc_01"},
    }))
    resp, err := s.FindLocation(stampPluginCtx(t, "core-building"),
        &hostv1.FindLocationRequest{Name: "plaza"})
    require.NoError(t, err)
    assert.Equal(t, "loc_01", resp.GetLocationId())
}

func TestWorldServerFindLocationNotFoundIsCodesNotFound(t *testing.T) {
    s := hostcap.NewWorldQueryServer(newFakeBaseWithMutator(t, fakeMutator{}))
    _, err := s.FindLocation(stampPluginCtx(t, "core-building"),
        &hostv1.FindLocationRequest{Name: "void"})
    assert.Equal(t, codes.NotFound, status.Code(err))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestWorldServerFindLocation ./internal/plugin/hostcap/`
Expected: FAIL — `FindLocation` returns `codes.Unimplemented`.

- [ ] **Step 3: Implement `FindLocation` on `worldServer`**

Add to `world.go`, mirroring `QueryLocation` (`world.go:49-85`): pull the `WorldMutator` from the base (nil → `codes.Unimplemented`, matching the query RPCs' nil-guard), stamp ABAC subject `plugin:<name>` (reuse the query path's subject stamping), call `FindLocationByName`, map `world.ErrNotFound` → `codes.NotFound`, any other inner error → log via `errutil.LogErrorContext` + `status.Errorf(codes.Internal, "internal error")` (per `.claude/rules/grpc-errors.md`).

```go
func (s *worldServer) FindLocation(ctx context.Context, req *hostv1.FindLocationRequest) (*hostv1.FindLocationResponse, error) {
    wm := s.host.WorldMutator() // match the query RPCs' accessor (s.host, NOT s.base)
    if wm == nil {
        return nil, status.Errorf(codes.Unimplemented, "world mutation not supported")
    }
    loc, err := wm.FindLocationByName(ctx, req.GetName())
    if errors.Is(err, world.ErrNotFound) {
        return nil, status.Errorf(codes.NotFound, "location not found")
    }
    if err != nil {
        errutil.LogErrorContext(ctx, "FindLocation failed", err, "name", req.GetName())
        return nil, status.Errorf(codes.Internal, "internal error")
    }
    return &hostv1.FindLocationResponse{LocationId: loc.ID.String(), Name: loc.Name}, nil
}
```

(Confirm `FindLocationByName`'s exact return shape via `mcp__probe__extract_code FindLocationByName` before writing; adjust field access to match `world.Location`.)

- [ ] **Step 4: Run the FindLocation tests to verify they pass**

Run: `task test -- -run TestWorldServerFindLocation ./internal/plugin/hostcap/`
Expected: PASS.

- [ ] **Step 5: Write the failing test for the `WorldMutationService` server**

```go
func TestWorldMutationServerCreateLocationDelegatesToMutator(t *testing.T) {
    fm := &fakeMutator{}
    s := hostcap.NewWorldMutationServer(newFakeBaseWithMutator(t, fm))
    _, err := s.CreateLocation(stampPluginCtx(t, "core-building"),
        &hostv1.CreateLocationRequest{Name: "Atrium"})
    require.NoError(t, err)
    assert.Equal(t, "Atrium", fm.lastCreatedLocationName)
}

func TestWorldMutationServerCreateLocationNilMutatorIsUnimplemented(t *testing.T) {
    s := hostcap.NewWorldMutationServer(newFakeBaseNoMutator(t))
    _, err := s.CreateLocation(stampPluginCtx(t, "core-building"),
        &hostv1.CreateLocationRequest{Name: "Atrium"})
    assert.Equal(t, codes.Unimplemented, status.Code(err))
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `task test -- -run TestWorldMutationServer ./internal/plugin/hostcap/`
Expected: FAIL — `NewWorldMutationServer` undefined.

- [ ] **Step 7: Implement `worldMutationServer`**

Add to `world.go`: a `worldMutationServer` struct embedding `hostv1.UnimplementedWorldMutationServiceServer` + `hostCapabilityBase`, a `NewWorldMutationServer(base hostCapabilityBase) hostv1.WorldMutationServiceServer` constructor, and `CreateLocation`/`CreateExit`/`CreateObject` methods each: nil-guard `WorldMutator()` → `Unimplemented`; stamp `plugin:<name>` subject; translate the request proto → `world.Location`/exit/object args; call the mutator; map errors per `grpc-errors.md` (opaque internal). Mirror the field translation already used by the legacy hostfunc world-write path — find it via `mcp__probe__search_code "createLocationFn"` (`internal/plugin/hostfunc/functions.go`) and reuse the same arg construction so behavior is at parity.

- [ ] **Step 8: Register `WorldMutationService` in `LuaDefaultSet`**

In `register.go` (`LuaDefaultSet` branch, ~line 53), add:

```go
hostv1.RegisterWorldMutationServiceServer(srv, NewWorldMutationServer(base))
```

Update the doc comment at `register.go:38-40` (currently lists four services) to include `WorldMutationService`.

- [ ] **Step 9: Run hostcap tests + descriptor completeness**

Run: `task test -- ./internal/plugin/hostcap/`
Expected: PASS, including `descriptor_completeness_test.go` (it asserts every registered service has a descriptor entry — add `FindLocation`/WorldMutation entries to `descriptor.go` if it fails; `FindLocation` already has one at `descriptor.go:112`).

- [ ] **Step 10: Commit**

`jj describe -m "feat(plugin): hostcap WorldMutationService server + WorldQueryService.FindLocation (holomush-eykuh.4.1)"` then `jj new`.

---

## Task 2: Wire the `SessionAdmin` (broadcast/disconnect) backing for the Lua path

> Maps existing bead **holomush-eykuh.4.2** (P2). `sessionAdminServer` already exists (`internal/plugin/hostcap/session.go:90`, nil-guards to `Unimplemented`). The gap: `luaHostCapAdapter.SessionAdmin()` returns nil (`internal/plugin/lua/hostcap_adapter.go`) because `Functions` holds only the narrow `session.Access` (via `WithSessionAccess`), not the wide broadcast/disconnect surface. The wide surface is `hostfunc.SessionAccess` shape: `BroadcastSystemMessage` / `DisconnectSession`.

**Files:**

- Modify: `internal/plugin/lua/host.go` (new `HostOption` + field)
- Modify: `internal/plugin/lua/hostcap_adapter.go` (`SessionAdmin()`)
- Modify: construction site `internal/plugin/setup/subsystem.go` (thread the backing)
- Test: `internal/plugin/lua/hostcap_adapter_test.go`, integration in `test/integration/`

- [ ] **Step 1: Identify the production broadcast/disconnect implementor**

Run `mcp__probe__search_code "BroadcastSystemMessage"` and `"DisconnectSession"`. Confirm the concrete type that satisfies the wide surface and where it is available at `internal/plugin/setup/subsystem.go` construction. (Bead note flags none was found cleanly as of 2026-06-12 — resolve this first; if genuinely absent, the backing is the same `session.Manager`/coordinator that the telnet/web `wall` path uses. Record the implementor in the bead before proceeding.)

- [ ] **Step 2: Write the failing adapter test**

```go
func TestLuaAdapterSessionAdminReturnsBackingWhenWired(t *testing.T) {
    f := hostfunc.New(nil)
    a := newLuaHostCapAdapterWithSessionAdmin(f, fakeSessionAdmin{})
    require.NotNil(t, a.SessionAdmin())
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `task test -- -run TestLuaAdapterSessionAdmin ./internal/plugin/lua/`
Expected: FAIL — `SessionAdmin()` returns nil / constructor missing.

- [ ] **Step 4: Add the `WithSessionAdmin` host option + adapter wiring**

In `host.go`, add a `sessionAdmin hostcap.SessionAdmin` field and `WithSessionAdmin(sa hostcap.SessionAdmin) HostOption`. Thread it into `newLuaHostCapAdapter`. In `hostcap_adapter.go`, change `SessionAdmin()` to return the wired backing (nil only when unset — the server still nil-guards). Replace the deferral comment with the live wiring.

- [ ] **Step 5: Run adapter test to verify it passes**

Run: `task test -- -run TestLuaAdapterSessionAdmin ./internal/plugin/lua/`
Expected: PASS.

- [ ] **Step 6: Thread the backing at the production construction site**

In `internal/plugin/setup/subsystem.go`, pass `lua.WithSessionAdmin(<implementor from Step 1>)` where the Lua `Host` is constructed. Mirror how the binary host receives the same surface (plugin-runtime-symmetry — verify the binary side already has it; if not, file a follow-up).

- [ ] **Step 7: Integration test — brokered broadcast at parity**

Add a `test:int` Ginkgo spec asserting a Lua plugin calling `session.broadcast` through the brokered `SessionAdminService` reaches the same broadcast sink as the legacy `SessionCapability` hostfunc path.

Run: `task test:int -- ./test/integration/<domain>/`
Expected: PASS.

- [ ] **Step 8: Commit**

`jj describe -m "feat(plugin): wire Lua SessionAdmin broadcast/disconnect backing (holomush-eykuh.4.2)"` then `jj new`.

---

## Task 3: Stamp actor identity across the Lua bufconn boundary

> New bead **holomush-eykuh.4.5** (create at materialization). Lesson `eykuh.2.11`: `newPluginEndpoint` (`internal/plugin/lua/bufconn_endpoint.go:38-58`) builds `grpc.NewServer(grpc.ChainUnaryInterceptor(ic))` where `ic` is the capability interceptor — but no interceptor stamps `core.WithActor`. `plugins.NewInProcessConn` carries metadata, not context values, so `core.ActorFromContext` is empty server-side → token/identity-requiring caps (`emit`, `settings` GAME-scope, `eval`) fail closed. The adapter already exposes `LookupActor(ctx, pluginName) (core.Actor, string, error)` (`hostcap_adapter.go:92`).

**Files:**

- Create: `internal/plugin/lua/actor_interceptor.go`
- Modify: `internal/plugin/lua/bufconn_endpoint.go:40-51`
- Test: `internal/plugin/lua/actor_interceptor_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestActorStampInterceptorSetsActorOnContext(t *testing.T) {
    var seen core.Actor
    ic := newActorStampInterceptor(fakeActorLookup{actor: core.Actor{Kind: "plugin", ID: "core-communication"}}, "core-communication")
    _, err := ic(context.Background(), nil,
        &grpc.UnaryServerInfo{FullMethod: "/holomush.plugin.host.v1.EmitService/Emit"},
        func(ctx context.Context, _ any) (any, error) { seen, _ = core.ActorFromContext(ctx); return nil, nil })
    require.NoError(t, err)
    assert.Equal(t, "core-communication", seen.ID)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestActorStampInterceptor ./internal/plugin/lua/`
Expected: FAIL — `newActorStampInterceptor` undefined.

- [ ] **Step 3: Implement the interceptor**

```go
// newActorStampInterceptor returns a unary interceptor that resolves the
// per-plugin host-established actor and stamps it onto the request context
// via core.WithActor before the handler (and the capability interceptor) run.
// Identity ONLY — least-privilege gating is the resolver grant (Task 5).
func newActorStampInterceptor(lookup actorLookup, pluginName string) grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
        actor, _, err := lookup.LookupActor(ctx, pluginName)
        if err == nil && actor.ID != "" {
            ctx = core.WithActor(ctx, actor)
        }
        return handler(ctx, req)
    }
}
```

`actorLookup` is a one-method interface (`LookupActor`) satisfied by `*luaHostCapAdapter`. A lookup miss leaves the context unstamped — downstream caps fail closed (correct direction).

- [ ] **Step 4: Run to verify it passes**

Run: `task test -- -run TestActorStampInterceptor ./internal/plugin/lua/`
Expected: PASS.

- [ ] **Step 5: Chain it BEFORE the capability interceptor**

In `bufconn_endpoint.go`, change the server construction so the actor stamp runs first (the capability interceptor and handlers need the stamped actor):

```go
actorIC := newActorStampInterceptor(adapter, pluginName)
srv := grpc.NewServer(grpc.ChainUnaryInterceptor(actorIC, ic)) // nosemgrep: ...
```

Keep the existing `nosemgrep` comment.

- [ ] **Step 6: Parity test — token-requiring cap through the bridge**

Extend the `eykuh.2.11` parity test (currently proves only the token-free `kv` case) to drive `emit` through the production Lua bridge and assert it succeeds once the actor is stamped.

Run: `task test:int -- ./test/integration/<domain>/`
Expected: PASS.

- [ ] **Step 7: Commit**

`jj describe -m "fix(plugin): stamp actor identity across Lua bufconn boundary (holomush-eykuh.4.5)"` then `jj new`.

---

## Task 4: Add `ResolveResult.Grants` + per-plugin grant computation

> R3 part 1. `ResolveResult` (`internal/plugin/dependency.go:21-25`) currently has `Ordered`, `Unsatisfied`, `Cycles`. The resolver already iterates every plugin's deps validating each against the vocabulary + providers (`dependency.go:51-160`). The grant set is the set of tokens that validated successfully — accumulate it in the same loop.

**Files:**

- Modify: `internal/plugin/dependency.go`
- Test: `internal/plugin/dependency_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestResolveDependencyOrderEmitsGrantsForDeclaredCaps(t *testing.T) {
    plugins := []*DiscoveredPlugin{discoveredWithRequires(t, "core-objects",
        "capability:property", "capability:world.query")}
    res, err := ResolveDependencyOrder(plugins, nil, DefaultCapabilityVocabulary())
    require.NoError(t, err)
    assert.ElementsMatch(t, []string{"property", "world.query"}, res.Grants["core-objects"])
}

func TestResolveDependencyOrderGrantsExcludeUndeclared(t *testing.T) {
    plugins := []*DiscoveredPlugin{discoveredWithRequires(t, "core-help")}
    res, err := ResolveDependencyOrder(plugins, nil, DefaultCapabilityVocabulary())
    require.NoError(t, err)
    assert.Empty(t, res.Grants["core-help"])
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestResolveDependencyOrder.*Grants ./internal/plugin/`
Expected: FAIL — `res.Grants` field does not exist.

- [ ] **Step 3: Add the field + populate it**

In `dependency.go`, add to `ResolveResult`:

```go
// Grants maps plugin name → the set of dependency tokens (capability tokens
// like "world.query" and service names) it successfully declared and that
// resolved. This is the single least-privilege grant authority consumed by
// both runtimes' delivery shims (INV-PLUGIN-45). A token NOT in a plugin's
// grant set MUST NOT be wired for that plugin.
Grants map[string][]string
```

Initialize `res.Grants = map[string][]string{}` at the top of `ResolveDependencyOrder`. In the per-dep validation loop, on the success branch (the dep resolved as a capability or service — i.e. it is NOT appended to `res.Unsatisfied`), append the dep's token to `res.Grants[p.Manifest.Name]`. Use the capability token (`dep.Capability`) for capability entries and the service name for service entries, matching what `RegisterHostCaps` / the binary wiring key on. Skip `optional` entries that didn't resolve.

- [ ] **Step 4: Run to verify it passes**

Run: `task test -- -run TestResolveDependencyOrder ./internal/plugin/`
Expected: PASS (existing resolver tests unaffected — `Grants` is additive).

- [ ] **Step 5: Commit**

`jj describe -m "feat(plugin): resolver emits per-plugin Grants set (holomush-eykuh.4)"` then `jj new`.

---

## Task 5: Consolidate both shims onto the resolver grant set

> R3 part 2. Lua derives `declaredCaps := p.manifest.RequiredCapabilities()` at three sites (`internal/plugin/lua/host.go:421,523,647`) and passes it to `RegisterHostCaps`. Binary derives `DeclaredAccessFromManifest(manifest)` + iterates `manifest.RequiredServiceNames()`/`RequiredCapabilities()` (`internal/plugin/goplugin/host.go:806,847`). Replace both manifest re-derivations with the resolver's `Grants[name]`. The resolved grants must be threaded from `Manager` (which calls `resolveLoadOrder`) into each host's `Load`.

**Files:**

- Modify: `internal/plugin/manager.go` (capture + expose grants)
- Modify: `internal/plugin/lua/host.go` (consume grants instead of `RequiredCapabilities()`)
- Modify: `internal/plugin/goplugin/host.go` (consume grants)
- Test: `internal/plugin/lua/host_test.go`, `internal/plugin/goplugin/host_test.go`

- [ ] **Step 1: Write the failing Lua-side test**

Assert that when the host is given a grant set for a plugin that excludes `world.mutation`, `RegisterHostCaps` is called WITHOUT `world.mutation` even if the manifest were to list it — i.e. the grant set, not the manifest, drives injection.

```go
func TestLuaHostInjectsResolverGrantsNotManifest(t *testing.T) {
    h := NewHostWithFunctions(testFns(t), WithPluginGrants(map[string][]string{"p": {"world.query"}}))
    // ... deliver an event to plugin "p" whose manifest declares world.query+world.mutation ...
    // assert only the world.query global is injected
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestLuaHostInjectsResolverGrants ./internal/plugin/lua/`
Expected: FAIL — `WithPluginGrants` undefined.

- [ ] **Step 3: Add grant plumbing to `Manager` + both hosts**

- `manager.go`: after `resolveLoadOrder` returns the `ResolveResult` (refactor `resolveLoadOrder` to return the full result, or stash `res.Grants` on the Manager), pass `res.Grants` to each host's loader (`lua.WithPluginGrants(res.Grants)` / the binary equivalent).
- `lua/host.go`: add `pluginGrants map[string][]string` field + `WithPluginGrants` option. At the three delivery sites, replace `declaredCaps := p.manifest.RequiredCapabilities()` with `declaredCaps := h.pluginGrants[name]`.
- `goplugin/host.go`: replace BOTH binary re-derivations with the grant set for that plugin — the broker-wiring loop over `manifest.RequiredServiceNames()` (~lines 806-835) AND the `InitRequest.Config.DeclaredCapabilities: manifest.RequiredCapabilities()` field (~line 858). Both must source from `Grants[name]` so the resolver is the single authority; missing either leaves a manifest-derived path alive. Orient with `mcp__probe__extract_code` on the `Load` function before editing.

- [ ] **Step 4: Run both hosts' tests**

Run: `task test -- ./internal/plugin/lua/ ./internal/plugin/goplugin/`
Expected: PASS.

- [ ] **Step 5: Run the full plugin package + integration**

Run: `task test -- ./internal/plugin/...` then `task test:int -- ./test/integration/wholesystem/`
Expected: PASS — all plugins still resolve and load.

- [ ] **Step 6: Commit**

`jj describe -m "refactor(plugin): both shims consume resolver grants, single least-privilege gate (holomush-eykuh.4.3)"` then `jj new`.

---

## Task 6: Audit + migrate plugin manifests

> R7. Audit table (verified 2026-06-14 against `plugins/*/main.lua` + `plugin.yaml`; capability tokens from `internal/plugin/capability_vocab.go`). Re-derive from code before editing — do not trust this table blindly.

| Plugin | Add to `requires:` |
| --- | --- |
| `core-building` | `- capability: world.query` and `- capability: world.mutation` (currently has NO `requires:` block) |
| `core-objects` | `- capability: world.mutation` (has `property`, `world.query`) |
| `core-communication` | `- capability: session.admin` (has `session`) |

**Files:**

- Modify: `plugins/core-building/plugin.yaml`, `plugins/core-objects/plugin.yaml`, `plugins/core-communication/plugin.yaml`
- Audit only (expect no change): `plugins/core-help/`, `plugins/echo-bot/`, `plugins/core-aliases/`, and binary plugins (`plugins/core-scenes/` etc.)

- [ ] **Step 1: Re-derive each plugin's actual capability usage**

For every plugin, run `mcp__probe__search_code` / `rg -n "holomush\.(create_|query_|find_)|session\.(broadcast|disconnect|find_by_name)|kv_|emit|eval|settings"` over its `main.lua` (Lua) or Go source (binary), and list the capability tokens each call maps to via `capability_vocab.go`.

- [ ] **Step 2: Write/extend the audit regression test**

Extend the default-set resolver regression (the original `oeb4d` acceptance — find via `mcp__probe__search_code "Unsatisfied"` in `internal/plugin/*_test.go`) so it loads the REAL `plugins/*` manifests and asserts `len(res.Unsatisfied) == 0`. This will be the gate that catches an incomplete migration.

Run: `task test -- -run <regression> ./internal/plugin/`
Expected at this point: PASS (manifests not yet flipped; nothing enforces yet). The test's teeth bite after Task 8.

- [ ] **Step 3: Edit the three manifests**

Add the rows above. Example `plugins/core-building/plugin.yaml` (insert after the existing top-level keys, before `commands:`):

```yaml
requires:
  - capability: world.query
  - capability: world.mutation
```

- [ ] **Step 4: Validate manifests against the schema**

Run: `task lint` (schema check validates `plugin.yaml` against `schemas/plugin.schema.json`).
Expected: PASS.

- [ ] **Step 5: Commit**

`jj describe -m "feat(plugin): declare capability requires for core-building/objects/communication (holomush-eykuh.4)"` then `jj new`.

---

## Task 7: Settle o262d — policy function + per-error-class tests

> R8. Fail-fast is already live but inlined in `resolveLoadOrder` (`internal/plugin/manager.go:836-840`). Factor the fatal decision into an explicit policy function over `ResolveResult` so a future `gracefulDegradation` quarantine is a one-point swap (the foundation's stated seam — `ResolveResult`'s own doc comment at `dependency.go:19` already anticipates "per-plugin quarantine policy reads Unsatisfied/Cycles").

**Files:**

- Modify: `internal/plugin/manager.go`
- Test: `internal/plugin/manager_test.go`
- Close: `holomush-o262d`

- [ ] **Step 1: Write the failing per-error-class table test**

```go
func TestResolveLoadOrderPolicyFatalPerErrorClass(t *testing.T) {
    cases := []struct{ name string; res *ResolveResult }{
        {"unsatisfied requires", &ResolveResult{Unsatisfied: []UnsatisfiedDep{{Reason: "UNSATISFIED_CAPABILITY"}}}},
        {"cycle", &ResolveResult{Cycles: [][]string{{"a", "b", "a"}}}},
    }
    for _, c := range cases {
        t.Run(c.name, func(t *testing.T) {
            err := applyResolvePolicy(c.res, defaultResolvePolicy)
            require.Error(t, err)
            assert.Equal(t, "PLUGIN_DEPENDENCY_UNSATISFIED", oops.AsOops(err).Code())
        })
    }
}

func TestResolveLoadOrderPolicyAllowsCleanResult(t *testing.T) {
    require.NoError(t, applyResolvePolicy(&ResolveResult{}, defaultResolvePolicy))
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestResolveLoadOrderPolicy ./internal/plugin/`
Expected: FAIL — `applyResolvePolicy` / `defaultResolvePolicy` undefined.

- [ ] **Step 3: Extract the policy function**

In `manager.go`, replace the inlined check in `resolveLoadOrder` with a call to a named policy:

```go
// resolvePolicy decides loader behavior from a structured resolve result.
// defaultResolvePolicy is fail-closed (INV-PLUGIN-43): any non-optional
// unsatisfied dep or cycle is fatal. A future gracefulDegradation-gated
// quarantine policy swaps in HERE without touching the resolver.
type resolvePolicy func(*ResolveResult) error

func defaultResolvePolicy(res *ResolveResult) error {
    if len(res.Unsatisfied) > 0 || len(res.Cycles) > 0 {
        return oops.Code("PLUGIN_DEPENDENCY_UNSATISFIED").
            With("unsatisfied", res.Unsatisfied).With("cycles", res.Cycles).
            Errorf("plugin dependency resolution failed; fail-closed (INV-PLUGIN-43)")
    }
    return nil
}

func applyResolvePolicy(res *ResolveResult, p resolvePolicy) error { return p(res) }
```

`resolveLoadOrder` calls `applyResolvePolicy(res, defaultResolvePolicy)` and returns its error. Behavior is unchanged (still always-fatal).

- [ ] **Step 4: Run to verify it passes + no regression**

Run: `task test -- ./internal/plugin/`
Expected: PASS.

- [ ] **Step 5: Document the gracefulDegradation boundary**

Add a comment near the per-plugin `gracefulDegradation` handling (`manager.go ~575-585`) stating it governs per-plugin LOAD failures only, NOT DAG resolution (which is `defaultResolvePolicy`'s fail-closed domain).

- [ ] **Step 6: Close the bug**

Run: `bd close holomush-o262d --reason="Fail-fast already live; refactored behind defaultResolvePolicy seam; per-error-class tests added in manager_test.go"`

- [ ] **Step 7: Commit**

`jj describe -m "refactor(plugin): factor o262d fail-fast into resolvePolicy seam + per-class tests (holomush-o262d)"` then `jj new`.

---

## Task 8: The atomic flip — strip legacy capability injection, remove the allowlist

> R1/R2/R6. THE flip. `hostfunc.Register` (`internal/plugin/hostfunc/functions.go:271`) injects all host funcs unconditionally. Strip the CAPABILITY functions (`kv_*`, `query_*`, `create_*`, world/session caps); KEEP stdlib (`log`, `new_request_id`, `RegisterStdlib`/`holo.fmt`, `register_emit_type`, return-value emit). Remove `WithHostCapBridge` + the `bridgeEnabled` gating so the brokered path is unconditional (grant-gated).

**Files:**

- Modify: `internal/plugin/hostfunc/functions.go:271-347` (`Register`)
- Modify: `internal/plugin/lua/host.go:92` (remove `WithHostCapBridge`), `:428,530,654` (remove `bridgeEnabled` gate; always inject via grants)
- Test: `internal/plugin/lua/host_test.go`, whole-system integration

- [ ] **Step 1: Write the failing test — capability funcs gone from legacy Register**

```go
func TestHostfuncRegisterOmitsCapabilityFunctions(t *testing.T) {
    L := lua.NewState(); defer L.Close()
    hostfunc.New(nil).Register(L, "core-help")
    mod := L.GetGlobal("holomush").(*lua.LTable)
    assert.Equal(t, lua.LNil, mod.RawGetString("query_location"), "capability fn must not be on legacy path")
    assert.NotEqual(t, lua.LNil, mod.RawGetString("log"), "stdlib log must remain")
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run TestHostfuncRegisterOmitsCapability ./internal/plugin/hostfunc/`
Expected: FAIL — `query_location` is still registered.

- [ ] **Step 3: Strip capability injection from `Register`**

In `functions.go`, remove the capability `SetField` lines (`kv_get/set/delete`, `query_*`, `create_*`, and the session/world capability wiring) from `Register` (lines ~291-330). Keep `RegisterStdlib`, `log`, `new_request_id`, and the emit-type/return-value-emit surface. Update the `Register` doc comment to state it now installs language stdlib only; capabilities flow through the brokered path.

- [ ] **Step 4: Make brokered injection unconditional (grant-gated)**

In `lua/host.go`, delete `WithHostCapBridge`, the `bridgeEnabledPlugins` field, and the `bridgeEnabled` checks at the three sites. Call `luabridge.RegisterHostCaps(L, endpoint.Conn(), name, h.pluginGrants[name])` unconditionally (still gated by the grant set — empty grants inject nothing). Remove the now-stale "test-fixture-only" comments. Update callers/tests that passed `WithHostCapBridge`.

- [ ] **Step 5: Naming reconciliation (R6)**

Run `mcp__probe__search_code "world_ext"` and audit any legacy global whose token differs from the brokered token. Since legacy capability injection is now gone (Step 3), confirm no plugin references a retired legacy global name; if any alias must survive for compatibility, point it at the brokered surface (not a parallel one). Record findings in the bead.

- [ ] **Step 6: Run the whole-system load test (the teeth)**

Run: `task test:int -- ./test/integration/wholesystem/` and `task test -- ./internal/plugin/...`
Expected: PASS — every migrated plugin loads, resolves with no `Unsatisfied`, and consumes capabilities through the brokered path. A FAIL here means a manifest declaration is missing (Task 6 incomplete) — fix the manifest, do not weaken the gate.

- [ ] **Step 7: Commit**

`jj describe -m "feat(plugin)!: atomic cutover — brokered capability path only, retire legacy injection (holomush-eykuh.4)"` then `jj new`.

---

## Task 9: Bind INV-PLUGIN-45 (single least-privilege gate)

> R3 part 3. After Task 5 consolidated the gate onto the resolver grant set, prove both runtimes are denied an undeclared dependency IDENTICALLY through that one gate.

**Files:**

- Test: `test/integration/plugin/least_privilege_parity_test.go` (or the existing parity suite — locate via `mcp__probe__search_code "INV-PLUGIN-44"`)
- Modify: `docs/architecture/invariants.yaml` (flip `INV-PLUGIN-45` → `bound`)
- Generated: `docs/architecture/invariants.md` (via `cmd/inv-render`)

- [ ] **Step 1: Write the cross-runtime denial test**

```go
// Verifies: INV-PLUGIN-45
func TestUndeclaredDependencyDeniedIdenticallyAcrossRuntimes(t *testing.T) {
    // A binary plugin and a Lua plugin, each declaring NO "world.mutation".
    // Drive a world.mutation call through each runtime's brokered path.
    // Assert BOTH are denied (grant set excludes world.mutation) — same gate,
    // same denial. The grant comes from ResolveResult.Grants, not the manifest.
}
```

Build it on the resolver grant path: construct a `ResolveResult.Grants` lacking `world.mutation` for both plugins, wire each runtime's shim from it, attempt the call, assert denial for both.

- [ ] **Step 2: Run to verify it fails / then passes**

Run: `task test:int -- -run TestUndeclaredDependencyDenied ./test/integration/plugin/`
Expected: PASS once the shared-grant gate (Task 5) is in place.

- [ ] **Step 3: Flip the registry binding**

In `invariants.yaml`, set `INV-PLUGIN-45` `binding: bound` and add `asserted_by:` listing the test file. Add `// Verifies: INV-PLUGIN-45` above the test (Step 1 already includes it).

- [ ] **Step 4: Regenerate + verify**

Run: `go run ./cmd/inv-render` then `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/`
Expected: PASS.

- [ ] **Step 5: Commit**

`jj describe -m "test(plugin): bind INV-PLUGIN-45 cross-runtime least-privilege gate (holomush-eykuh.4.3)"` then `jj new`.

---

## Task 10: Test `pushBridgeError` opacity strip

> Maps existing bead **holomush-eykuh.4.4** (P3, independent — may land any time). `pushBridgeError` (`internal/plugin/luabridge/marshal.go:314-323`) runs gRPC errors through `status.Convert(...).Message()` to strip inner detail before returning `(nil, msg)` to Lua, but no test drives a `codes.Internal`-with-secret error through it.

**Files:**

- Test: `internal/plugin/luabridge/marshal_test.go`

- [ ] **Step 1: Write the table-driven opacity test**

```go
func TestPushBridgeErrorStripsInnerDetail(t *testing.T) {
    L := lua.NewState(); defer L.Close()
    secret := "table users password=hunter2"
    err := status.Error(codes.Internal, "internal error") // status.Convert keeps only this message
    // build an error whose inner detail carries `secret` but whose status message is opaque:
    wrapped := status.Errorf(codes.Internal, "internal error") // inner secret lives in logs, not status
    n := luabridge.PushBridgeErrorForTest(L, wrapped) // or drive via newPluginMethodInvoker
    msg := L.ToString(-1)
    assert.NotContains(t, msg, "hunter2")
    assert.Equal(t, "internal error", msg)
    _ = err; _ = secret; _ = n
}
```

If `pushBridgeError` is unexported and has no test seam, drive it through a generated binding via `newPluginMethodInvoker` (find via `mcp__probe__search_code "newPluginMethodInvoker"`) with a fake server returning `status.Error(codes.Internal, "internal error")` whose details carry the secret; assert the Lua second return equals the opaque `.Message()` and `NotContains` the secret.

- [ ] **Step 2: Run to verify it passes**

Run: `task test -- -run TestPushBridgeError ./internal/plugin/luabridge/`
Expected: PASS (the strip already works; this test pins it).

- [ ] **Step 3: Commit**

`jj describe -m "test(plugin): assert pushBridgeError opacity strip to Lua (holomush-eykuh.4.4)"` then `jj new`.

---

## Post-implementation checklist

- [ ] `task pr-prep` green (fast lane: schema/license/lint/fmt/unit/build/bats).
- [ ] `task test:int` green (Docker) — wholesystem + plugin parity suites.
- [ ] `INV-PLUGIN-45` shows `binding: bound`; `task test -- ./test/meta/` green.
- [ ] `holomush-o262d` closed; children `eykuh.4.1`/`.4.2`/`.4.3`/`.4.4` closed; `eykuh.4.5` created + closed.
- [ ] No `WithHostCapBridge` references remain (`mcp__probe__search_code "WithHostCapBridge"` → empty).
- [ ] No capability host-function `SetField` remains in `hostfunc.Register`.
- [ ] `docs/roadmap.md` theme `plugin-capability-architecture` updated (epic complete).
<!-- adr-capture: sha256=cc430e24167e2036; session=cli; ts=2026-06-14T14:12:13Z; adrs=holomush-40ssh,holomush-vpg8l,holomush-ptf7b,holomush-05f3v -->
