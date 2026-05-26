# Integration Test Harness

This page describes the `internal/testsupport/integrationtest` package — the
canonical harness for HoloMUSH **full-stack integration** tests that need a real
in-process holomush stack (Postgres + embedded NATS JetStream + production
CoreServer) rather than a mocked surface.

> **Tier vocabulary:** "E2E" is reserved for the Playwright browser suite
> (`task test:e2e`). Ginkgo suites run via `task test:int` are **integration**
> tests — including this harness, which is "full-stack integration". See the
> canonical tier table in `.claude/rules/testing.md`.

**Package**: `github.com/holomush/holomush/internal/testsupport/integrationtest`

**Build tag**: `//go:build integration` — the harness is NEVER linked into
production binaries.

> **Unit tests use a lighter helper, not this harness.** If a test only needs
> a `session.Store` — not the whole stack — reach for
> `internal/testsupport/sessiontest.NewStore(t)`. It returns a store backed by
> a fresh database on the shared Postgres testcontainer and deliberately skips
> the `//go:build integration` tag, which makes it the one sanctioned exception
> to "anything using `SharedPostgres` is integration-tagged." Because `task
> test` runs these tests, the packages that use them — `internal/grpc/`,
> `internal/grpc/focus/`, `internal/command/handlers/`, and `internal/session/`
> — need Docker running locally. (`session.Store` has a single implementation,
> `store.PostgresSessionStore`, so there is no in-memory fake to test against.)
> For a session that carries a `PlayerSessionID`, seed its foreign-key parents
> with `sessiontest.NewStoreWithPool(t)` + `SeedPlayerSession`. The full
> rationale is in the design spec at
> `docs/superpowers/specs/2026-05-23-remove-session-memstore-design.md`.

---

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

---

## Test packages currently using the harness

| Package                                | Bead epic | What it asserts                                                |
| -------------------------------------- | --------- | -------------------------------------------------------------- |
| `test/integration/privacy/`            | iwzt      | I-PRIV-1..7 history-scope privacy invariants                   |
| `test/integration/presence/`           | 5b2j      | AC4 (joiner sees prior presence), AC3 / I-PRES-2 (floor bypass) |

When you add a new test package that uses the harness, append it to this
table.

---

## Stack composition

`integrationtest.Start(t)` boots:

- **Postgres**: shared testcontainer (via `test/testutil.SharedPostgres`) +
  per-test database (`testutil.FreshDatabase`) with all migrations applied.
- **NATS JetStream**: in-memory embedded server via
  `internal/eventbus/eventbustest.New(t)`. Per-test isolation; safe for
  parallel tests.
- **CoreServer**: real production constructor `holoGRPC.NewCoreServer(...)`
  wired with `WithAuthService`, `WithPlayerSessionRepo`, `WithCharacterRepo`,
  `WithCharacterNameResolver`, `WithSessionStore`, `WithGuestService`,
  `WithSubscriber`, `WithHistoryReader`, `WithAccessEngine`. Same options
  production passes — same code paths exercised.
- **ABAC engine**: defaults to `allowAllPolicyEngine` (privacy tests focus on
  session/history gates, not role enforcement). Override via
  `WithPolicyEngine(eng)` when denial-path coverage is needed.

---

## Helper catalog

### Connection / lifecycle

| Helper                                | Purpose                                                       |
| ------------------------------------- | ------------------------------------------------------------- |
| `Start(t, opts...) *Server`           | Boot the stack. Default ABAC engine is allow-all.             |
| `Server.Stop()`                       | Idempotent teardown.                                          |
| `WithPolicyEngine(eng) StartOption`   | Override the default ABAC engine (e.g., `DenyAllEngine`).     |

### Session creation (real-path drivers)

| Helper                                       | Real-path?  |
| -------------------------------------------- | ----------- |
| `Server.ConnectGuest(ctx) *Session`          | Yes — CreateGuest RPC + SelectCharacter RPC |
| `Server.ConnectAuthed(ctx, name) *Session`   | Mixed — direct character/player creation, then SelectCharacter RPC |
| `Server.ConnectAuthedWithRoles(ctx, name, roles)` | Mixed — same as above + direct role insertion |
| `Session.SendCommand(ctx, cmd) error`        | Yes — HandleCommand RPC (dispatcher has empty registry, so most commands fail by design) |
| `Session.Logout(ctx)`                        | Yes — Logout RPC                                              |
| `Session.QueryStreamHistory(ctx, stream)`    | Yes — QueryStreamHistory RPC                                  |
| `Session.ListFocusPresence(ctx)`             | Yes — ListFocusPresence RPC                                   |
| `Session.EmitDirectEvent(ctx, stream, evType, payload)` | Yes — `eventbus.Publisher.Publish` (production publisher path) |

### Test-only escape hatches

These bypass the production pipeline via direct SQL. Each helper's doc
comment notes what it bypasses and why.

| Helper                                              | What it bypasses                                                  |
| --------------------------------------------------- | ----------------------------------------------------------------- |
| `Server.NewLocation(ctx) ulid.ULID`                 | Bypasses `WorldService.CreateLocation`; direct repo write.        |
| `Server.NewSceneWithoutMember(ctx) ulid.ULID`       | Delegates to `NewLocation` (scenes share location PK namespace).  |
| `Server.ExpireSession(ctx, sessionID)`              | Forces `status='expired'` + past `expires_at` directly.           |
| `Server.SetLocationArrivedAt(ctx, sessionID, t)`    | Forces a specific floor timestamp; mirrors post-MovementHook state. |
| `Server.DeleteCharacter(ctx, charID)`               | Cascades cleanup across `sessions`, `player_character_bindings`, `character_roles`, `objects` (FK-safe order). |
| `Server.DeleteSession(ctx, sessionID)`              | Direct `DELETE FROM sessions`; cascades to `session_connections`. |
| `Session.MoveTo(ctx, newLocationID)`                | Updates session row's `location_id` + `location_arrived_at`; does NOT update `characters.location_id`. |

### Accessors

| Helper                          | Purpose                                                     |
| ------------------------------- | ----------------------------------------------------------- |
| `Server.GameID() string`        | Returns the embedded bus's game ID (`"main"` by default). Used to construct dot-style NATS subjects like `events.<gameID>.scene.<id>.ic`. |

### Custom Gomega matchers

| Matcher                                | Purpose                                                     |
| -------------------------------------- | ----------------------------------------------------------- |
| `MatchOopsCode(expected) types.GomegaMatcher` | Asserts `oops.AsOops(err).Code() == expected`. Top-level code only; does NOT chain-walk. |

---

## Conventions

### `*testing.T` propagation

Each test package's `*_suite_test.go` declares a package-level
`var suiteT *testing.T` that `TestPrivacy` / `TestPresence` / etc. populates.
Ginkgo `Describe` blocks pass `suiteT` to `integrationtest.Start(suiteT)` —
Ginkgo's `GinkgoT()` does NOT satisfy `*testing.T` and cannot be passed
directly.

### Cleanup context

`BeforeEach` derives `ctx` with `context.WithTimeout(context.Background(),
90*time.Second)` (cancel is suppressed via `//nolint:govet` — pre-existing
project convention). `AfterEach` derives a SEPARATE `cleanupCtx` from
`context.Background()` for Logout calls so cleanup can run even if `ctx` is
about to expire.

### Per-test harness

Most `Describe` blocks construct a fresh `Server` in `BeforeEach` and tear
down in `AfterEach`. This guarantees per-spec isolation (fresh Postgres DB,
fresh embedded NATS) at the cost of ~1-2 seconds setup per spec. Sharing a
single `Server` across specs is possible but requires manual cleanup between
specs — not a pattern currently used in the codebase.

### Direct event injection

`Session.EmitDirectEvent` is the canonical way to plant events from a test —
the harness's dispatcher has an empty command registry, so production
`SendCommand("pose hello")` returns `COMMAND_REJECTED`. `EmitDirectEvent`
goes through the same `eventbus.Publisher.Publish` path production uses,
so JetStream ack + audit semantics match.

---

## Adding a new test package

1. Create the test directory under `test/integration/<name>/`.
2. Add a `<name>_suite_test.go` with the standard Ginkgo bootstrap (mirror
   `test/integration/privacy/privacy_suite_test.go`).
3. Import the harness:

    ```go
    import "github.com/holomush/holomush/internal/testsupport/integrationtest"
    ```

4. Update the "Test packages currently using the harness" table above.
5. If your test needs a non-default ABAC engine, pass `WithPolicyEngine(eng)`
    to `Start`.
6. If your test needs a new escape hatch (e.g., to produce a state shape no
    existing helper can), add the helper to `harness.go` or `session.go` with
    a doc comment that explains:

    - What it bypasses.
    - Why production code can't reasonably reach that state.
    - FK / cascade side effects (when applicable).

---

## Whole-system plugin tier (`WithInTreePlugins`)

The harness supports an opt-in **whole-system** mode that loads all in-tree
plugins, exercising the same production plugin wiring path. This is the top
Go-fidelity integration tier (see canonical tier table in
`.claude/rules/testing.md`).

### Opt-in

```go
srv := integrationtest.Start(t, integrationtest.WithInTreePlugins())
```

Omitting `WithInTreePlugins` leaves the plugin subsystem nil — existing
targeted suites (`privacy`, `presence`) are unaffected (INV-WS-4).

### How it works

`WithInTreePlugins()` reuses production's `setup.PluginSubsystem` (which calls
`Manager.LoadAll`) — it does NOT construct `plugins.NewManager` directly in the
harness (INV-WS-1). The harness assembles a temporary `plugins/` overlay (the
source tree's `plugins/` directory + compiled artifacts from `build/plugins/`),
then calls `PluginSubsystem.Start(ctx)`, which DAG-resolves and loads all
in-tree plugins.

**What loads:** discover-all finds 8 runtime plugins — 5 core Lua plugins
(`core-aliases`, `core-building`, `core-communication`, `core-help`,
`core-objects`), 1 additional Lua plugin (`echo-bot`), and 2 binary plugins
(`core-scenes`, `test-abac-widget`). The 2 setting plugins
(`setting-crossroads` / `setting-skeleton`, manifest names `crossroads` /
`skeleton`) are configuration-only: they fall through to `loadPlugin`'s
`default` branch, which logs a warning and skips them, so they are not
registered in the Manager's loaded set.

**Event emission is NOT wired.** `WithInTreePlugins` does not call
`Manager.ConfigureEventEmitter`. The whole-system census reads plugin load
state and the command registry only. A plugin command that emits events will
fail with "plugin event emitter is not configured" until a future suite wires
it explicitly.

**Accessors panic without the option.** `PluginManager()`,
`CommandRegistry()`, and `ServiceRegistry()` are only valid when
`WithInTreePlugins` was passed; calling them otherwise panics with a clear
message.

### Prerequisite: build the binary plugins

Binary plugins (`core-scenes`, `test-abac-widget`) must be compiled before
running the whole-system suite:

```bash
task plugin:build-all   # compile all binary plugins for linux/amd64
```

`task test:int` runs `plugin:build-all` automatically, so CI always has fresh
artifacts. If you run integration tests outside `task test:int` without prior
builds, the harness skips (INV-WS-3): locally it calls `t.Skip`; when
`HOLOMUSH_REQUIRE_PLUGINS=1` is set (as in CI), it calls `t.Fatalf` so CI
never silently green-skips past missing binaries.

### Sequence

```mermaid
sequenceDiagram
    participant S as Suite (Ginkgo)
    participant H as integrationtest.Start
    participant PS as PluginSubsystem
    participant M as plugin.Manager
    participant SRV as integrationtest.Server
    S->>H: Start(t, WithInTreePlugins())
    H->>H: binary-gate check (skip or fatal if artifacts absent)
    H->>H: assemblePluginsDir (overlay plugins/ + build/plugins/)
    H->>PS: setup.NewPluginSubsystem(cfg).Start(ctx)
    PS->>M: LoadAll(ctx) [strict, DAG-resolved]
    M-->>PS: 8 runtime plugins loaded; commands/verbs/aliases registered
    PS-->>H: Manager, CommandRegistry, ServiceRegistry
    H-->>S: *Server (srv)
    S->>SRV: PluginManager().ListPlugins() — census assertion
    S->>SRV: CommandRegistry().Get("help") — command registration assertion
```

### Suite and test packages

| Package                                 | Bead epic | What it asserts                                              |
| --------------------------------------- | --------- | ------------------------------------------------------------ |
| `test/integration/wholesystem/`         | 0f0f4     | INV-5: all in-tree plugins load; `help` command registered   |

When you add a test package that uses `WithInTreePlugins`, append it to this
table and to the main "Test packages currently using the harness" table above.

### Invariants

| # | Invariant | Enforcement |
| --- | --- | --- |
| INV-5 | The whole-system suite MUST load all in-tree plugins via `Manager.LoadAll` | Routing through `PluginSubsystem.Start`; census asserts full plugin set |
| INV-WS-1 | `WithInTreePlugins` MUST reuse `setup.PluginSubsystem`, not fork `plugins.NewManager` directly | Meta-test `TestWithInTreePluginsReusesSubsystem` source-scans `integrationtest/` |
| INV-WS-3 | Whole-system suite MUST NOT be silently skipped in CI | `HOLOMUSH_REQUIRE_PLUGINS=1` converts the missing-artifact skip to `t.Fatalf`; CI's `task test:int` runs `plugin:build-all` first and globs `./...`, so the suite always runs |
| INV-WS-4 | `WithInTreePlugins` MUST be opt-in; omitting it leaves harness plugin-free | Existing targeted suites pass with no edits; harness test asserts no plugin subsystem starts by default |

---

## Real ABAC (`WithRealABAC`)

By default the harness wires an allow-all ABAC engine — most integration tests
assert session/history floors, which are ABAC-independent. To exercise the
**real seeded ABAC engine** (production's `abacsetup.NewABACSubsystem` path),
opt in:

```go
ts := integrationtest.Start(t, integrationtest.WithRealABAC())
```

This seeds the production `seed:*` policy set (`policy.Bootstrap`) and boots the
real engine. Compose with `WithInTreePlugins()` for cross-plugin ABAC coverage —
the plugin subsystem registers its attribute providers on the engine's own
resolver:

```go
ts := integrationtest.Start(t, integrationtest.WithInTreePlugins(), integrationtest.WithRealABAC())
```

**Live role semantics.** Under `WithRealABAC`, `character_roles` are evaluated by
the engine. `ConnectAuthedWithRoles(ctx, name, []string{"admin"})` grants
role-based permits (e.g. `seed:admin-full-access`); a roleless `ConnectAuthed`
receives only what `seed:*` grants a roleless character. Tests that pass under
allow-all may see denials under `WithRealABAC` until they seed the roles their
actions require.

---

## Related

- `internal/eventbus/eventbustest/` — bus-only harness for unit tests that
  need an embedded NATS but not a full CoreServer.
- `internal/access/policy/policytest/` — `AllowAllEngine` /
  `DenyAllEngine` / `GrantEngine` helpers used with `WithPolicyEngine`.
- `test/testutil/` — shared Postgres testcontainer + fresh-database helpers.
