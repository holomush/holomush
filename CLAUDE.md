# HoloMUSH Development Guide

This document provides instructions for AI coding assistants working on HoloMUSH.

## Project Overview

HoloMUSH is a modern MUSH platform with:

- Go core with event-oriented architecture
- Dual protocol support (telnet + web)
- Plugin system: Lua (gopher-lua), binary (hashicorp/go-plugin), and setting plugins
- PostgreSQL for all data
- SvelteKit PWA for web client

**Architecture Reference**: [docs/plans/2026-01-18-holomush-roadmap-design.md](docs/plans/2026-01-18-holomush-roadmap-design.md)

---

## Documentation Structure

| Directory                | Purpose                                      | Audience                |
| ------------------------ | -------------------------------------------- | ----------------------- |
| `site/docs/`             | Public documentation website (zensical)      | All users               |
| `docs/plans/`            | Implementation plans, in-progress work       | Contributors (internal) |
| `docs/specs/`            | Design specifications, architectural designs | Contributors (internal) |
| `docs/superpowers/plans/`| AI-generated implementation plans (superpowers skill) | Contributors (internal) |
| `docs/superpowers/specs/`| AI-generated design specifications (superpowers skill) | Contributors (internal) |

**Site documentation** (`site/docs/`) is organized by audience:

- `guide/` — For players and game designers
- `operating/` — For people running HoloMUSH servers
- `extending/` — For plugin developers building on HoloMUSH
- `contributing/` — For people contributing to the HoloMUSH codebase
- `reference/` — Auto-generated API and event references

**Build commands:**

```bash
task docs:setup   # Install documentation dependencies
task docs:serve   # Start local dev server
task docs:build   # Build static site
```

For sandbox operations at `game.holomush.dev`, see
[site/docs/operating/sandbox-operations.md](site/docs/operating/sandbox-operations.md)
and [site/docs/operating/sandbox-restore.md](site/docs/operating/sandbox-restore.md).

---

## ⚠️ Protected Branch Policy

**`main` is a protected branch.** Direct commits to main are not allowed.

| Requirement                        | Description                                         |
| ---------------------------------- | --------------------------------------------------- |
| **MUST** create feature branch     | All work happens on feature branches, not main      |
| **MUST** submit PR for review      | All changes to main require a pull request          |
| **MUST** pass CI checks            | Tests and linting must pass before merge            |
| **MUST** use squash merge          | All PRs are squash merged to maintain clean history |
| **MUST NOT** push directly to main | Branch protection enforces this                     |

**See:** [Pull Request Guide](site/docs/contributing/pr-guide.md) for the complete workflow.

---

## Development Principles

### Test-Driven Development

- Tests MUST be written before implementation
- Tests MUST pass before any task is complete
- Use table-driven tests for comprehensive coverage
- Mock external dependencies (database, network)

### Spec-Driven Development

- Work MUST NOT start without a spec/design/plan
- Specs live in `docs/specs/` or `docs/superpowers/specs/`
- Plans live in `docs/plans/` or `docs/superpowers/plans/`
- The `docs/superpowers/` subdirectories are used by AI tooling (superpowers skills) and are equally valid
- All specs and plans MUST use RFC2119 keywords

### RFC2119 Keywords

| Keyword        | Meaning                                    |
| -------------- | ------------------------------------------ |
| **MUST**       | Absolute requirement                       |
| **MUST NOT**   | Absolute prohibition                       |
| **SHOULD**     | Recommended, may ignore with justification |
| **SHOULD NOT** | Not recommended                            |
| **MAY**        | Optional                                   |

## Workflow

### Beads-Driven Task Management

All work is tracked via [beads](https://github.com/steveyegge/beads):

```text
Spec (docs/specs/)
    ↓
Epic (bd create "..." --epic)
    ↓
Implementation Plan (docs/plans/)
    ↓
Tasks (bd create "..." -p <epic>)
    - Dependencies based on file overlap
    - Dependencies based on conceptual overlap
```

### Daily Workflow

```bash
# 1. Find ready tasks
bd ready

# 2. Select task, understand context
bd show <task-id>

# 3. Write failing tests

# 4. Implement until tests pass
task test

# 5. Update documentation

# 6. Request code review (REQUIRED)

# 7. Address all review findings

# 8. Mark complete
bd close <task-id>
```

### Code Review Requirement

All tasks MUST be reviewed before completion. See
[Pull Request Guide](site/docs/contributing/pr-guide.md) for the complete workflow.

| Requirement                                | Description                                          |
| ------------------------------------------ | ---------------------------------------------------- |
| **MUST** use `pr-review-toolkit:review-pr` | Launch comprehensive review using specialized agents |
| **MUST** address all findings              | Fix issues or document why not applicable            |
| **MUST NOT** skip review                   | Even for "simple" changes                            |

**Quick workflow:**

1. Complete implementation and tests
2. Run `task test` and `task lint`
3. Invoke `/pr-review-toolkit:review-pr`
4. Address all findings
5. Create PR or mark task complete

## Code Conventions

### Go Idioms

- Accept interfaces, return structs
- Errors are values - handle them explicitly
- Use context for cancellation and timeouts
- Prefer composition over inheritance
- When using accessor methods (e.g., `decision.Reason()`), always include `()` — without parens, Go creates a method value (func pointer) that compiles silently when passed to `...any` parameters (`oops.With`, `slog`)

### Random Number Generation

Always use `crypto/rand`, never `math/rand`. For picking from slices, use a
`crypto/rand` + `math/big` helper. The `internal/naming` package has `cryptoIntN(n)`
as an example.

### ULID Generation

Two ULID generators exist; the choice matters because the event store relies
on lex order matching arrival order.

| Use case | Generator | Why |
| --- | --- | --- |
| **Event IDs** (`core.Event.ID`), session IDs | `core.NewULID()` | Monotonic within a millisecond. `PostgresEventStore.Replay` uses `WHERE id > afterID ORDER BY id`; cursor advances use a SQL monotonicity CAS. Non-monotonic event IDs silently break both. |
| **Entity primary keys** (players, locations, characters, exits, objects, policies) | `idgen.New()` | Identity, not ordering. Fresh `crypto/rand` entropy per call. |

Enforced by the `EventIDMustBeMonotonic` ruleguard rule in `gorules/rules.go`
(loaded via `gocritic`). New `core.Event{}` literals using `idgen.New()` will
fail `task lint`.

### Error Handling

Use oops for structured errors with context:

```go
// Wrap existing error with context
return oops.With("plugin", name).With("operation", "load").Wrap(err)

// Create new error
return oops.Errorf("validation failed").With("field", fieldName)

// At API boundaries, add error code
return oops.Code("PLUGIN_LOAD_FAILED").With("plugin", name).Wrap(err)
```

For logging oops errors, use pkg/errutil:

```go
errutil.LogError(logger, "operation failed", err)
```

For testing error codes:

```go
errutil.AssertErrorCode(t, err, "EXPECTED_CODE")
errutil.AssertErrorContext(t, err, "key", expectedValue)
```

### Logging

- Use structured logging (slog)
- Log at appropriate levels (debug, info, warn, error)
- Include relevant context in log entries

### Naming

- Use clear, descriptive names
- Avoid abbreviations except well-known ones (ID, URL, HTTP)
- Package names are lowercase, single words when possible

### Database Migrations

Migrations live in `internal/store/migrations/` and are embedded at compile time.
See the full guide at [site/docs/contributing/database-migrations.md](site/docs/contributing/database-migrations.md).

| Requirement | Description |
| ----------- | ----------- |
| **MUST** use sequential numbering | `000002_`, `000003_`, etc. after baseline |
| **MUST** provide both `.up.sql` and `.down.sql` | Every migration needs a reversible pair |
| **MUST** be idempotent | Use `IF NOT EXISTS`, `IF EXISTS`, `ON CONFLICT DO NOTHING` |
| **MUST NOT** modify the baseline | Add new migrations instead of editing `000001_baseline` |
| **MUST NOT** use triggers or functions | All logic lives in Go; PostgreSQL is storage only |

### License Headers

All source files MUST include SPDX license headers at the top:

**Go files:**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package foo ...
package foo
```

**Shell scripts:**

```bash
#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
```

**Protobuf files:**

```protobuf
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

syntax = "proto3";
```

**YAML files (workflows, configs):**

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
```

| Requirement                         | Description                                         |
| ----------------------------------- | --------------------------------------------------- |
| **MUST** include SPDX header        | All `.go`, `.sh`, `.proto` files                    |
| **SHOULD** include SPDX header      | YAML configuration files where appropriate          |
| **MUST NOT** add to generated files | Skip `*.pb.go` files                                |
| **SHOULD** use `task license:add`   | Automatically adds headers to files missing them    |
| **Auto-added on commit**            | Lefthook pre-commit hook adds headers automatically |

**Directories checked:** `api/`, `cmd/`, `internal/`, `pkg/`, `plugins/`, `scripts/`

**Commands:**

```bash
task license:check   # Verify all files have headers
task license:add     # Add missing headers
```

## Testing

### Coverage Requirements

| Requirement                      | Description                                           |
| -------------------------------- | ----------------------------------------------------- |
| **MUST** maintain >80% coverage  | Per-package coverage must exceed 80%                  |
| **MUST** run `task test:cover`   | To verify coverage before completing work             |
| **SHOULD** target 90%+ coverage  | For core packages (`internal/core`, `internal/world`) |

### Integration Tests and Refactoring

`task test` does NOT compile `//go:build integration` files. When refactoring
shared types, interfaces, or packages, always run `task test:int` to catch
breakage that unit tests miss.

### Test Files

- Tests live next to implementation: `foo.go` → `foo_test.go`
- Integration tests in `*_integration_test.go`
- Use build tags for integration tests: `//go:build integration`

### Test Naming

Test names MUST be sentences that communicate behavior. Follow the ACE
framework: **Action** (what), **Condition** (when/given), **Expectation**
(then/result).

Reference: [Test Names Should Be Sentences](https://bitfieldconsulting.com/posts/test-names)

**Functions without subtests** — the function name itself is the sentence:

| Pattern | Example |
| ------- | ------- |
| Good | `TestConfigDirUsesXDGEnvVarWhenSet` |
| Good | `TestEnsureDirFailsWhenParentIsAFile` |
| Bad | `TestConfigDir_EnvVar` |
| Bad | `TestEnsureDir_Error` |

**Functions with subtests** — parent name identifies the unit under test,
subtest names carry the sentence:

```go
func TestHashPassword(t *testing.T) {
    t.Run("produces valid argon2id hash", func(t *testing.T) { ... })
    t.Run("rejects empty password", func(t *testing.T) { ... })
}
```

| Requirement | Description |
| ----------- | ----------- |
| **MUST** follow ACE | Every test name communicates action, condition, and expectation |
| **MUST** use PascalCase | Top-level function names: `TestConfigDirFallsBackToHomeDotConfig` |
| **SHOULD NOT** use underscores | Exception: `TestType_Method` with subtests (e.g., `TestEngine_Evaluate`) |
| **MUST** use lowercase subtests | Subtest strings: `"returns ErrNotFound for missing character"` |
| **MUST NOT** use vague names | No `"success"`, `"error case"`, `"test 1"` |

### Table-Driven Tests

```go
func TestEventType_String(t *testing.T) {
    tests := []struct {
        name     string
        input    EventType
        expected string
    }{
        {"returns say for event type say", EventTypeSay, "say"},
        {"returns pose for event type pose", EventTypePose, "pose"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.expected, tt.input.String())
        })
    }
}
```

### Mocking

- Use interfaces for dependencies
- Create mock implementations for tests
- Consider using testify/mock for complex mocks

### Plugin Tests (`internal/plugin`)

Lua plugins use gopher-lua which creates fresh VM state per event delivery.
Binary plugins use hashicorp/go-plugin and communicate via gRPC.

| Principle                     | Description                                         |
| ----------------------------- | --------------------------------------------------- |
| State isolation (Lua)         | Each `DeliverEvent` creates a new Lua state         |
| No shared state between tests | No need for special test helpers or shared fixtures |
| Fast startup (Lua)            | ~50μs per Lua state                                 |
| Process isolation (binary)    | Binary plugins run as separate processes via go-plugin |

### Assertions

Use testify for unit test assertions:

```go
// Equality
assert.Equal(t, expected, got)

// Error checking
require.NoError(t, err)
assert.Error(t, err)

// Contains
assert.Contains(t, slice, element)
```

### Test Quality

| Requirement | Description |
| ----------- | ----------- |
| **MUST** test both paths | Every exported function needs at least one positive and one negative test |
| **MUST** assert behavior | No zero-assertion "don't panic" tests |
| **MUST** focus each test | One behavior per test/subtest — if it needs "and," split it |
| **SHOULD** use error codes | Prefer `errutil.AssertErrorCode` or `assert.ErrorIs` over string matching |
| **MUST** use `require` for preconditions | `require.NoError` for setup, `assert.*` for the check under test |

### Mocking with Mockery

Generate mocks with mockery:

```bash
mockery # Uses .mockery.yaml config
```

Use generated mocks:

```go
store := mocks.NewMockEventStore(t)
store.EXPECT().Append(mock.Anything, mock.Anything).Return(nil)
```

### Test Engine Helpers

Use `policytest.GrantEngine` for authorization in tests:

```go
mockAccess := policytest.NewGrantEngine()
mockAccess.GrantCommandExecution(subject, "say", "look") // Layer 1 grants
mockAccess.Grant(subject, "emit", "stream")              // Layer 2 / capability grants
```

Other test engines: `AllowAllEngine()`, `DenyAllEngine()`, `NewErrorEngine(err)`, `NewInfraFailureEngine(t, reason, policyID)`.

### MemoryEventStore

MemoryEventStore is for **unit tests only**. It MUST NOT be used in integration
tests, E2E tests, or production code. Integration and E2E tests MUST use
PostgresEventStore via testcontainers. The `//go:build !integration` tag on
`store_memory.go` enforces this at compile time.

### Integration Tests with Ginkgo/Gomega (BDD)

| Requirement                           | Description                                |
| ------------------------------------- | ------------------------------------------ |
| **MUST** use Ginkgo/Gomega            | All integration tests use BDD-style specs  |
| **MUST** write feature specs          | User stories become `Describe`/`It` blocks |
| **MUST** use `//go:build integration` | Tag all integration test files             |
| **SHOULD** use testcontainers         | For database integration tests             |

**Structure:** Feature specs live in `test/integration/<domain>/`:

```go
//go:build integration

var _ = Describe("Feature Name", func() {
    Describe("User story or capability", func() {
        It("expected behavior in plain English", func() {
            // Given/When/Then pattern
            Expect(result).To(Equal(expected))
        })
    })
})
```

**Async operations:**

```go
Eventually(func() int {
    return len(results)
}).Should(Equal(expected))
```

**Run integration tests:**

```bash
go test -race -v -tags=integration ./test/integration/...
```

## Commands

### Task Commands (Required)

**MUST use `task` for all build, test, lint, and format operations.** Do NOT run `go build`, `go test`, `golangci-lint`, etc. directly.

```bash
task lint      # Run all linters
task fmt       # Format all files
task test      # Run unit tests (compact output via gotestsum)
task build     # Build binary
task dev       # Run dev server
task plugin:build-all              # Discover and compile all binary plugins for linux/amd64
task plugin:build -- core-scenes   # Build a single binary plugin
```

**Test commands accept arguments after `--`:**

```bash
task test                                        # All unit tests
task test -- ./internal/command/                  # Single package
task test -- -run TestCapability ./internal/command/  # Specific test
task test:verbose -- ./internal/command/          # Full verbose output
task test:int                                    # Integration tests (needs Docker)
```

| Requirement                            | Description                                       |
| -------------------------------------- | ------------------------------------------------- |
| **MUST** use `task`                    | Never run Go/lint/fmt commands directly            |
| **MUST** run `task test`               | Before claiming any implementation is complete     |
| **MUST** run `task lint`               | Before committing changes                          |
| **MUST NOT** disable lint/format rules | Without explicit user confirmation                 |
| **SHOULD** run `task fmt`              | Before committing to ensure consistent formatting  |

**MUST** run `task pr-prep` before creating a PR or pushing to a PR branch.
This mirrors all CI jobs (lint, format, schema, license, unit, integration,
E2E) and MUST pass with zero failures. Do NOT push to a PR branch without
a green `task pr-prep`. Docker is always available — never skip E2E tests.

### jj Workspace Commands

Workspaces live in a `.worktrees/` directory that is a sibling of the main repo root
(e.g., `<parent>/.worktrees/<name>`). The exact path is machine-specific.

```bash
jj workspace add <parent>/.worktrees/<name> --name <name> -r main
jj workspace forget <name>  # then: rm -rf <parent>/.worktrees/<name>
task gowork                 # MUST run after add or forget — regenerates go.work
```

| Requirement | Description |
| ----------- | ----------- |
| **MUST** run `task gowork` | After every `jj workspace add` or `jj workspace forget` |

`task gowork` regenerates `go.work` in the main repo root so gopls covers all active
workspaces without "not in workspace" LSP diagnostics. Works from any workspace.

### Beads Commands

```bash
bd ready              # List unblocked tasks
bd create "title"     # Create task
bd show <id>          # View task details
bd close <id>         # Complete task
bd dep add <a> <b>    # Add dependency
```

## Directory Structure

```text
api/                 # Protocol definitions
  proto/             # Protobuf service definitions
build/
  plugins/           # Compiled binary plugin output (gitignored)
cmd/holomush/        # Server entry point
docs/
  plans/             # Implementation plans (internal, in-progress work)
  specs/             # Design specifications (internal)
site/                # Documentation website (zensical)
  docs/
    guide/           # For players and game designers
    operating/       # For server operators
    extending/       # For plugin developers
    contributing/    # For codebase contributors
    reference/       # Auto-generated references
internal/            # Private implementation
  access/            # ABAC access control system
  control/           # Control plane (admin API)
  core/              # Event system, sessions
  grpc/              # gRPC server implementation
  logging/           # Structured logging setup
  observability/     # Metrics and health endpoints
  plugin/            # Plugin system (Lua, binary, settings; manifests, registry, DAG loader)
  store/             # PostgreSQL implementations
  telnet/            # Telnet protocol adapter
  tls/               # TLS certificate management
  web/               # WebSocket adapter (future)
  world/             # World model (objects, locations, exits, scenes)
  xdg/               # XDG base directory support
pkg/                 # Public plugin API
  plugin/            # Plugin SDK types and ServiceProvider interface
  errutil/           # Error handling utilities
plugins/             # Lua and binary plugins (each with manifest.yaml)
scripts/             # Build and utility scripts
  build-plugins.sh   # Plugin build discovery script
test/                # Integration tests
  integration/       # End-to-end test suites
```

## Key Interfaces

### EventStore

```go
type EventStore interface {
    Append(ctx context.Context, event Event) error
    Subscribe(ctx context.Context, stream string) (<-chan ulid.ULID, <-chan error, error)
    Replay(ctx context.Context, stream string, afterID ulid.ULID, limit int) ([]Event, error)
    LastEventID(ctx context.Context, stream string) (ulid.ULID, error)
}
```

### ServiceRegistry (`internal/plugin`)

Maps proto service names (e.g., `holomush.scene.v1.SceneService`) to registered
service implementations. Used by the plugin loader to wire up service
dependencies between plugins.

### ServiceProvider (`pkg/plugin`)

Interface implemented by binary plugins that provide gRPC services. The plugin
host calls `RegisterServices` during plugin startup to let the plugin register
its service implementations with the server.

## Architecture Invariants

### Gateway Boundary

The gateway (`cmd/holomush/gateway.go`, `internal/web/`) is a **protocol
translation layer only**. It MUST NOT access internal services directly:

| Allowed                                  | Prohibited                                    |
| ---------------------------------------- | --------------------------------------------- |
| gRPC calls to core server                | Direct access to `WorldService`               |
| Connection management (register/remove)  | Direct access to `SessionStore` for queries   |
| Protocol translation (ConnectRPC ↔ gRPC) | Direct access to repositories or the database |
| Static file serving                      | Business logic or data aggregation            |

All game state queries (location state, presence, characters) MUST flow
through core server RPCs. The gateway proxies; it does not compute.

## Terminology

Consistent terminology prevents confusion. Use these terms exactly:

| Correct term     | Incorrect / ambiguous | Notes                                                         |
| ---------------- | --------------------- | ------------------------------------------------------------- |
| **location**     | room, area, zone      | A place in the world model. Event type: `location_state`.     |
| **exit**         | door, path, passage   | A connection between locations.                               |
| **character**    | player, user, avatar  | An in-game entity controlled by a player.                     |
| **player**       | user, account         | The human behind one or more characters.                      |
| **session**      | connection            | Server-side state for a character's ongoing presence.         |
| **connection**   | socket, client        | A single client attachment to a session (terminal/telnet/etc).|
| **presence**     | who's here, occupants | Active sessions at a location. Derived from session store.    |
| **grid present** | online, visible       | Character is visible on the grid (has terminal/telnet conn).  |
| **scene**        | RP scene              | A structured roleplay encounter with participants.            |

**MUST NOT** mix terms. `room` is never used in code, comments, types, events,
or variable names. The spatial concept is always `location`.

## Core Systems

### World Model (`internal/world`)

The world model provides the spatial foundation:

- **Objects** - Base entity type with ULID identifiers
- **Locations** - Rooms/areas that contain objects
- **Exits** - Connections between locations (with optional locks)
- **Scenes** - RP scenes with participants and privacy settings

All world operations go through `WorldService` which validates constraints and
persists to PostgreSQL via the repository interface.

### Plugin System (`internal/plugin`)

Three plugin types:

| Type        | Runtime                  | Use case                                    |
| ----------- | ------------------------ | ------------------------------------------- |
| `lua`       | gopher-lua VM            | Lightweight event handlers, game logic      |
| `binary`    | hashicorp/go-plugin subprocess | Complex services with proto contracts  |
| `setting`   | Bootstrap only           | Configuration-only plugins (no runtime)     |

**Manifest schema** (`manifest.yaml`): Each plugin declares `requires` (proto
services it depends on), `provides` (proto services it implements), and
`storage` (database tables it needs). The plugin loader performs DAG dependency
resolution to determine load order and validates that all `requires` are
satisfied by another plugin's `provides`.

**Service registry**: Maps proto service names to implementations. Binary
plugins register services over gRPC; Lua plugins register via the Lua host.

**Plugin admin commands:**

```bash
plugin list            # List loaded plugins with name, type, version
plugin info <name>     # Detailed plugin info (requires, provides, storage, commands)
```

### Access Control (`internal/access`)

Attribute-Based Access Control (ABAC) with phased implementation:

- **Phase 1 (current):** Static evaluator with role-based permissions
- **Phase 2 (future):** Full ABAC with policies and attributes

```go
// Check if subject can perform action on resource
allowed := evaluator.Evaluate(ctx, subject, action, resource)
```

Default deny - explicit permission required for all operations.

### Command Authorization

Commands use two-layer authorization at dispatch time:

1. **Layer 1 — Command Execution:** `engine.Evaluate(subject, "execute", "command:<name>")` — can this character run this command?
2. **Layer 2 — Capability Pre-Flight:** `engine.CanPerformAction(subject, action, resource, scope)` per declared capability — does this character have the class of permissions this command needs?

Commands declare capabilities as structured objects:

```go
Capabilities: []command.Capability{
    {Action: "write", Resource: "location", Scope: command.ScopeLocal},
}
```

Scope: `ScopeSelf` (default, own character), `ScopeLocal` (current location), `ScopeGlobal` (server-wide).

## Patterns

### Event Sourcing

- All game actions produce events
- Events are immutable and ordered
- State is derived from event replay

### HTTP Middleware

When wrapping `http.ResponseWriter` (e.g., cookie middleware), the wrapper
MUST implement `http.Flusher` and `Unwrap()` — ConnectRPC server-streaming
calls `Flush()` after each frame and will error if the interface is missing.

### Web Client

See `web/CLAUDE.md` for SvelteKit-specific patterns including theme system
architecture, shadcn-svelte conventions, Tailwind v4 guidance, and Svelte 5
runes patterns.

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until changes are pushed to the remote. This project is a **jj-colocated repo** — prefer `jj` commands when `.jj/` is present, fall back to plain `git` otherwise.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - `task pr-prep` (mirrors CI)
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY. Detect the VCS first:

   ```bash
   # Detect: jj-colocated repo?
   test -d .jj && echo "jj" || echo "git"
   ```

   **jj-colocated repo** (this project's default):

   ```bash
   jj git fetch
   jj rebase -r <change-id> -d main@origin   # rebase ONLY your change (see memory: feedback_jj_rebase_targeted)
   jj bookmark set <branch> -r @-            # move branch to the commit you want to push
   jj git push --branch <branch>
   jj st                                     # verify clean
   ```

   **Plain git repo** (fallback):

   ```bash
   git pull --rebase
   git push
   git status  # MUST show "up to date with origin"
   ```

5. **Clean up** - Clear stashes, prune remote branches, `jj workspace forget` unused workspaces
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**

- Work is NOT complete until the push succeeds (`jj git push` or `git push`)
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
- **jj-specific**: NEVER use bare `jj rebase -d main` — always scope with `-r <change-id>` to avoid sweeping up other agents' work
