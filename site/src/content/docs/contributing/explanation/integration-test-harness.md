---
title: "How the integration test harness works"
---

The `internal/testsupport/integrationtest` package is the canonical harness for
HoloMUSH **full-stack integration** tests that need a real in-process holomush
stack (Postgres + embedded NATS JetStream + production CoreServer) rather than a
mocked surface. This page explains how that stack is composed, the conventions
the suites follow, and why. For when and how to use it, see
[Integration test harness](/contributing/how-to/integration-tests/); for the
helper catalog, see
[Integration test harness reference](/contributing/reference/integration-test-harness/).

> **Tier vocabulary:** "E2E" is reserved for the Playwright browser suite
> (`task test:e2e`). Ginkgo suites run via `task test:int` are **integration**
> tests — including this harness, which is "full-stack integration". See the
> canonical tier table in `.claude/rules/testing.md`.

## Unit tests use a lighter helper

If a test only needs a `session.Store` — not the whole stack — it should reach
for `internal/testsupport/sessiontest.NewStore(t)`. It returns a store backed by
a fresh database on the shared Postgres testcontainer and deliberately skips the
`//go:build integration` tag, which makes it the one sanctioned exception to
"anything using `SharedPostgres` is integration-tagged." Because `task test`
runs these tests, the packages that use them — `internal/grpc/`,
`internal/grpc/focus/`, `internal/command/handlers/`, and `internal/session/` —
need Docker running locally. (`session.Store` has a single implementation,
`store.PostgresSessionStore`, so there is no in-memory fake to test against.)
For a session that carries a `PlayerSessionID`, seed its foreign-key parents
with `sessiontest.NewStoreWithPool(t)` + `SeedPlayerSession`. The full rationale
is in the design spec at
`docs/superpowers/specs/2026-05-23-remove-session-memstore-design.md`.

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

## How the whole-system tier works

`WithInTreePlugins()` reuses production's `setup.PluginSubsystem` (which calls
`Manager.LoadAll`) — it does NOT construct `plugins.NewManager` directly in the
harness (INV-WS-1). The harness assembles a temporary `plugins/` overlay (the
source tree's `plugins/` directory + compiled artifacts from `build/plugins/`),
then calls `PluginSubsystem.Start(ctx)`, which DAG-resolves and loads all
in-tree plugins.

**Event emission is NOT wired.** `WithInTreePlugins` does not call
`Manager.ConfigureEventEmitter`. The whole-system census reads plugin load
state and the command registry only. A plugin command that emits events will
fail with "plugin event emitter is not configured" until a future suite wires
it explicitly.

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

## Real ABAC role semantics

Under `WithRealABAC`, the harness seeds the production `seed:*` policy set
(`policy.Bootstrap`) and boots the real engine. `character_roles` are then
evaluated by the engine: `ConnectAuthedWithRoles(ctx, name, []string{"admin"})`
grants role-based permits (e.g. `seed:admin-full-access`); a roleless
`ConnectAuthed` receives only what `seed:*` grants a roleless character. Tests
that pass under allow-all may see denials under `WithRealABAC` until they seed
the roles their actions require.
