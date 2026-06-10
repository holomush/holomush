---
title: "Integration test harness reference"
---

Catalog of helpers, test packages, and invariants for the
`internal/testsupport/integrationtest` package. For when and how to use the
harness, see
[Integration test harness](/contributing/how-to/integration-tests/); for how
the stack is composed and why, see
[How the integration test harness works](/contributing/explanation/integration-test-harness/).

- **Package**: `github.com/holomush/holomush/internal/testsupport/integrationtest`
- **Build tag**: `//go:build integration` — the harness is NEVER linked into
  production binaries.

## Test packages currently using the harness

| Package                      | Bead epic | What it asserts                                                 |
| ---------------------------- | --------- | --------------------------------------------------------------- |
| `test/integration/privacy/`  | iwzt      | I-PRIV-1..7 history-scope privacy invariants                    |
| `test/integration/presence/` | 5b2j      | AC4 (joiner sees prior presence), AC3 / I-PRES-2 (floor bypass) |

When you add a new test package that uses the harness, append it to this
table.

## Helper catalog

### Connection / lifecycle

| Helper                              | Purpose                                                   |
| ----------------------------------- | --------------------------------------------------------- |
| `Start(t, opts...) *Server`         | Boot the stack. Default ABAC engine is allow-all.         |
| `Server.Stop()`                     | Idempotent teardown.                                      |
| `WithPolicyEngine(eng) StartOption` | Override the default ABAC engine (e.g., `DenyAllEngine`). |

### Session creation (real-path drivers)

| Helper                                                  | Real-path?                                                                                  |
| ------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| `Server.ConnectGuest(ctx) *Session`                     | Yes — CreateGuest RPC + SelectCharacter RPC                                                  |
| `Server.ConnectAuthed(ctx, name) *Session`              | Mixed — direct character/player creation, then SelectCharacter RPC                           |
| `Server.ConnectAuthedWithRoles(ctx, name, roles)`       | Mixed — same as above + direct role insertion                                               |
| `Session.SendCommand(ctx, cmd) error`                   | Yes — HandleCommand RPC (dispatcher has empty registry, so most commands fail by design)     |
| `Session.Logout(ctx)`                                   | Yes — Logout RPC                                                                             |
| `Session.QueryStreamHistory(ctx, stream)`               | Yes — QueryStreamHistory RPC                                                                 |
| `Session.ListFocusPresence(ctx)`                        | Yes — ListFocusPresence RPC                                                                  |
| `Session.EmitDirectEvent(ctx, stream, evType, payload)` | Yes — `eventbus.Publisher.Publish` (production publisher path)                               |

### Test-only escape hatches

These bypass the production pipeline via direct SQL. Each helper's doc
comment notes what it bypasses and why.

| Helper                                           | What it bypasses                                                                                       |
| ------------------------------------------------ | ------------------------------------------------------------------------------------------------------ |
| `Server.NewLocation(ctx) ulid.ULID`              | Bypasses `WorldService.CreateLocation`; direct repo write.                                             |
| `Server.NewSceneWithoutMember(ctx) ulid.ULID`    | Delegates to `NewLocation` (scenes share location PK namespace).                                       |
| `Server.ExpireSession(ctx, sessionID)`           | Forces `status='expired'` + past `expires_at` directly.                                                |
| `Server.SetLocationArrivedAt(ctx, sessionID, t)` | Forces a specific floor timestamp; mirrors post-MovementHook state.                                    |
| `Server.DeleteCharacter(ctx, charID)`            | Cascades cleanup across `sessions`, `player_character_bindings`, `character_roles`, `objects` (FK-safe order). |
| `Server.DeleteSession(ctx, sessionID)`           | Direct `DELETE FROM sessions`; cascades to `session_connections`.                                      |
| `Session.MoveTo(ctx, newLocationID)`             | Updates session row's `location_id` + `location_arrived_at`; does NOT update `characters.location_id`. |

### Accessors

| Helper                   | Purpose                                                                                                                                |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------- |
| `Server.GameID() string` | Returns the embedded bus's game ID (`"main"` by default). Used to construct dot-style NATS subjects like `events.<gameID>.scene.<id>.ic`. |

## Whole-system plugin tier (`WithInTreePlugins`)

The whole-system tier is the top Go-fidelity integration tier (see the canonical
tier table in `.claude/rules/testing.md`). To opt in and build the prerequisite
binary plugins, see
[Opt into the whole-system plugin tier](/contributing/how-to/integration-tests/#opt-into-the-whole-system-plugin-tier).

**What loads:** discover-all finds 8 runtime plugins — 5 core Lua plugins
(`core-aliases`, `core-building`, `core-communication`, `core-help`,
`core-objects`), 1 additional Lua plugin (`echo-bot`), and 2 binary plugins
(`core-scenes`, `test-abac-widget`). The 2 setting plugins
(`setting-crossroads` / `setting-skeleton`, manifest names `crossroads` /
`skeleton`) are configuration-only: they fall through to `loadPlugin`'s
`default` branch, which logs a warning and skips them, so they are not
registered in the Manager's loaded set.

**Accessors:** `PluginManager()`, `CommandRegistry()`, and `ServiceRegistry()`
are only valid when `WithInTreePlugins` was passed; calling them otherwise
panics with a clear message.

### Suite and test packages

| Package                         | Bead epic | What it asserts                                            |
| ------------------------------- | --------- | --------------------------------------------------------- |
| `test/integration/wholesystem/` | 0f0f4     | INV-5: all in-tree plugins load; `help` command registered |

When you add a test package that uses `WithInTreePlugins`, append it to this
table and to the main "Test packages currently using the harness" table above.

### Invariants

| #        | Invariant                                                                                       | Enforcement                                                                                                                       |
| -------- | ----------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| INV-5    | The whole-system suite MUST load all in-tree plugins via `Manager.LoadAll`                       | Routing through `PluginSubsystem.Start`; census asserts full plugin set                                                          |
| INV-WS-1 | `WithInTreePlugins` MUST reuse `setup.PluginSubsystem`, not fork `plugins.NewManager` directly   | Meta-test `TestWithInTreePluginsReusesSubsystem` source-scans `integrationtest/`                                                 |
| INV-WS-3 | Whole-system suite MUST NOT be silently skipped in CI                                            | `HOLOMUSH_REQUIRE_PLUGINS=1` converts the missing-artifact skip to `t.Fatalf`; CI's `task test:int` runs `plugin:build-all` first |
| INV-WS-4 | `WithInTreePlugins` MUST be opt-in; omitting it leaves harness plugin-free                       | Existing targeted suites pass with no edits; harness test asserts no plugin subsystem starts by default                          |

## Related

- `internal/eventbus/eventbustest/` — bus-only harness for unit tests that
  need an embedded NATS but not a full CoreServer.
- `internal/access/policy/policytest/` — `AllowAllEngine` /
  `DenyAllEngine` / `GrantEngine` helpers used with `WithPolicyEngine`.
- `test/testutil/` — shared Postgres testcontainer + fresh-database helpers.
