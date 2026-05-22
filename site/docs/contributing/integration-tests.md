# Integration Test Harness

This page describes the `internal/testsupport/integrationtest` package — the
canonical harness for HoloMUSH integration tests that need a real in-process
holomush stack (Postgres + embedded NATS JetStream + production CoreServer)
rather than a mocked surface.

**Package**: `github.com/holomush/holomush/internal/testsupport/integrationtest`

**Build tag**: `//go:build integration` — the harness is NEVER linked into
production binaries.

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

## Related

- `internal/eventbus/eventbustest/` — bus-only harness for unit tests that
  need an embedded NATS but not a full CoreServer.
- `internal/access/policy/policytest/` — `AllowAllEngine` /
  `DenyAllEngine` / `GrantEngine` helpers used with `WithPolicyEngine`.
- `test/testutil/` — shared Postgres testcontainer + fresh-database helpers.
