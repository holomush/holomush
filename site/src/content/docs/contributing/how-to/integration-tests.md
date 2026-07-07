---
title: "Integration test harness"
---

This guide covers how to write tests with the
`internal/testsupport/integrationtest` package — choosing it over lighter
options, adding a new test package, and opting into the whole-system plugin
tier or the real ABAC engine. For the helper and invariant catalog, see
[Integration test harness reference](/contributing/reference/integration-test-harness/);
for how the stack is composed and why, see
[How the integration test harness works](/contributing/explanation/integration-test-harness/).

## When to use

The harness is for tests that need to exercise production code paths
end-to-end through the gRPC handler surface, against real Postgres + NATS
JetStream + the production `CoreServer` constructor with its wired options.

Use it when:

- You're asserting **wire-level behavior** (response codes, error envelopes,
  audit-trail effects) and unit-test mocking would miss real handler logic.
- You need **invariant coverage** — privacy floor, presence snapshot, scene
  membership — where the test must observe what a real client would observe.
- You need to **opt the test into a non-default ABAC engine** without rewiring
  the entire CoreServer construction.

Do NOT use it when:

- A unit test with `mockery`-generated mocks would suffice (almost all
  handler-level tests).
- The test only needs an embedded NATS bus — `internal/eventbus/eventbustest`
  is leaner.
- You only need a `CoreServer` field stubbed — direct struct literal
  construction in a `*_test.go` file is fine.
- The test only needs a `session.Store`, not the whole stack — reach for
  `internal/testsupport/sessiontest.NewStore(t)` instead (the one sanctioned
  exception to the `//go:build integration` rule; rationale in
  [How the integration test harness works](/contributing/explanation/integration-test-harness/#unit-tests-use-a-lighter-helper)).

## Add a new test package

1. Create the test directory under `test/integration/<name>/`.
2. Add a `<name>_suite_test.go` with the standard Ginkgo bootstrap (mirror
   `test/integration/privacy/privacy_suite_test.go`).
3. Import the harness:

    ```go
    import "github.com/holomush/holomush/internal/testsupport/integrationtest"
    ```

4. Update the "Test packages currently using the harness" table in the
   [reference](/contributing/reference/integration-test-harness/#test-packages-currently-using-the-harness).
5. If your test needs a non-default ABAC engine, pass `WithPolicyEngine(eng)`
   to `Start`.
6. If your test needs a new escape hatch (e.g., to produce a state shape no
   existing helper can), add the helper to `harness.go` or `session.go` with
   a doc comment that explains:

   - What it bypasses.
   - Why production code can't reasonably reach that state.
   - FK / cascade side effects (when applicable).

## Opt into focus delivery

`WithFocusDelivery` wires a real `focus.Coordinator` + `SessionStreamRegistry`
so the plugin host's `JoinFocus` / `AutoFocusOnJoin` path reaches the live
Subscribe filter set. Without it, those RPCs short-circuit with "focus
coordinator not configured" and no scene-stream subscription is ever added.

```go
ts := integrationtest.Start(
    t,
    integrationtest.WithInTreePlugins(),
    integrationtest.WithFocusDelivery(),
)
```

**Requires `WithInTreePlugins()`** — the coordinator is injected into the
loaded plugin hosts via `Manager.ConfigureFocusDeps`.

### How the harness builds the coordinator senders

Both production (`cmd/holomush`) and the integration harness build the
coordinator's focus-delivery senders through the same helper:

```go
holoGRPC.FocusStreamCoordinatorOptions(streamRegistry)
```

`FocusStreamCoordinatorOptions` bundles a `StreamSender` and a
`ConnectionSender`, both backed by the same `SessionStreamRegistry`. Because
the harness calls this helper rather than hand-rolling the adapters, it is a
faithful production mirror by construction (INV-FS-4).

### Testing Lua runtime symmetry with `WithExtraPluginDir`

`WithExtraPluginDir(dir)` stages an additional plugin directory into the load
path alongside the in-tree plugins. This is the mechanism for the Lua
runtime-symmetry test:

```go
ts := integrationtest.Start(
    suiteT,
    integrationtest.WithInTreePlugins(),
    integrationtest.WithPluginCrypto(),
    integrationtest.WithFocusDelivery(),
    integrationtest.WithExtraPluginDir("testdata/lua/focus_join"),
)
```

The fixture at `test/integration/scenes/testdata/lua/focus_join/` is a minimal
Lua plugin that exposes the `luafocusjoin` command, which calls
`holomush.auto_focus_on_join` (the hostfunc registered at
`internal/plugin/hostfunc/stdlib_focus.go`). The integration test at
`test/integration/scenes/lua_focus_parity_test.go` (INV-FS-3) loads this
fixture and asserts that the Lua path delivers a live scene IC event to the
joiner's Subscribe stream — the same end-to-end assertion used by the binary
`scene join` keystone, proving plugin-runtime symmetry.

`dir` is resolved relative to the test package directory (Go runs tests with
CWD equal to the package directory).

## Opt into the whole-system plugin tier

The harness supports an opt-in **whole-system** mode that loads all in-tree
plugins, exercising the same production plugin wiring path:

```go
srv := integrationtest.Start(t, integrationtest.WithInTreePlugins())
```

Omitting `WithInTreePlugins` leaves the plugin subsystem nil — existing
targeted suites are unaffected.

### Prerequisite: build the binary plugins

Binary plugins (`core-scenes`, `test-abac-widget`) must be compiled before
running the whole-system suite:

```bash
task plugin:build-all   # compile all binary plugins for linux/amd64
```

`task test:int` runs `plugin:build-all` automatically, so CI always has fresh
artifacts. If you run integration tests outside `task test:int` without prior
builds, the harness skips: locally it calls `t.Skip`; when
`HOLOMUSH_REQUIRE_PLUGINS=1` is set (as in CI), it calls `t.Fatalf` so CI
never silently green-skips past missing binaries.

When you add a test package that uses `WithInTreePlugins`, append it to both
harness tables in the
[reference](/contributing/reference/integration-test-harness/).

## Opt into the real ABAC engine

By default the harness wires an allow-all ABAC engine. To exercise the
**real seeded ABAC engine** (production's `abacsetup.NewABACSubsystem` path),
opt in:

```go
ts := integrationtest.Start(t, integrationtest.WithRealABAC())
```

Compose with `WithInTreePlugins()` for cross-plugin ABAC coverage — the plugin
subsystem registers its attribute providers on the engine's own resolver:

```go
ts := integrationtest.Start(t, integrationtest.WithInTreePlugins(), integrationtest.WithRealABAC())
```

Under `WithRealABAC`, tests that pass under allow-all may see denials until
they seed the roles their actions require — see
[live role semantics](/contributing/explanation/integration-test-harness/#real-abac-role-semantics).

## Session-store testing (Docker required)

Tests in `internal/grpc/`, `internal/grpc/focus/`, `internal/command/handlers/`, and `internal/session/` that exercise `session.Store`-touching logic require Docker even under `task test` — they use the `internal/testsupport/sessiontest.NewStore(t)` helper, which is backed by a fresh database on the shared Postgres testcontainer. This is the **deliberate exception** to the "SharedPostgres tests MUST be `//go:build integration`" convention (`session.Store` has exactly one implementation — `store.PostgresSessionStore` — so there is no in-memory fake to test against). See [docs/superpowers/specs/2026-05-23-remove-session-memstore-design.md](https://github.com/holomush/holomush/blob/main/docs/superpowers/specs/2026-05-23-remove-session-memstore-design.md) for the rationale.

| Requirement                                | Description                                                                                              |
| ------------------------------------------ | -------------------------------------------------------------------------------------------------------- |
| **MUST** use `sessiontest.NewStore(t)`     | For any test needing a `session.Store` — never construct one ad hoc.                                     |
| **MUST** seed FK parents when needed       | Sessions Set with a non-zero `PlayerSessionID` need `sessiontest.NewStoreWithPool(t)` + `SeedPlayerSession(t, pool, ps)` (the `sessions.player_session_id` FK is enforced). |
| **MUST NOT** add `//go:build integration`  | The `sessiontest` package is the deliberate exception; Ginkgo suites pass their captured `suiteT`.       |
| **MUST** have Docker running               | Absence surfaces as testcontainers container-start errors at test runtime, not compile failures.         |

## Related

- `internal/eventbus/eventbustest/` — bus-only harness for unit tests that
  need an embedded NATS but not a full CoreServer.
- `internal/access/policy/policytest/` — `AllowAllEngine` /
  `DenyAllEngine` / `GrantEngine` helpers used with `WithPolicyEngine`.
- `test/testutil/` — shared Postgres testcontainer + fresh-database helpers.
