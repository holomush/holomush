# HoloMUSH Development Guide

This document provides instructions for AI coding assistants working on HoloMUSH.

## Project Overview

HoloMUSH is a modern MUSH platform with:

- Go core with event-oriented architecture
- Dual protocol support (telnet + web)
- Lua plugin system (gopher-lua) with go-plugin for complex extensions
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
        {"returns say for EventTypeSay", EventTypeSay, "say"},
        {"returns pose for EventTypePose", EventTypePose, "pose"},
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

| Principle                     | Description                                         |
| ----------------------------- | --------------------------------------------------- |
| State isolation               | Each `DeliverEvent` creates a new Lua state         |
| No shared state between tests | No need for special test helpers or shared fixtures |
| Fast startup                  | ~50μs per Lua state vs ~1.5s for WASM compilation   |

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
  plugin/            # Plugin system (Lua host, manifests, subscribers)
  store/             # PostgreSQL implementations
  telnet/            # Telnet protocol adapter
  tls/               # TLS certificate management
  web/               # WebSocket adapter (future)
  world/             # World model (objects, locations, exits, scenes)
  xdg/               # XDG base directory support
pkg/                 # Public plugin API
  plugin/            # Plugin SDK types
  errutil/           # Error handling utilities
plugins/             # Lua plugins
scripts/             # Build and utility scripts
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

<!-- BEGIN BEADS INTEGRATION -->
## Issue Tracking with bd (beads)

**IMPORTANT**: This project uses **bd (beads)** for ALL issue tracking. Do NOT use markdown TODOs, task lists, or other tracking methods.

### Why bd?

- Dependency-aware: Track blockers and relationships between issues
- Git-friendly: Auto-syncs to JSONL for version control
- Agent-optimized: JSON output, ready work detection, discovered-from links
- Prevents duplicate tracking systems and confusion

### Quick Start

**Check for ready work:**

```bash
bd ready --json
```

**Create new issues:**

```bash
bd create "Issue title" --description="Detailed context" -t bug|feature|task -p 0-4 --json
bd create "Issue title" --description="What this issue is about" -p 1 --deps discovered-from:bd-123 --json
```

**Claim and update:**

```bash
bd update bd-42 --status in_progress --json
bd update bd-42 --priority 1 --json
```

**Complete work:**

```bash
bd close bd-42 --reason "Completed" --json
```

### Issue Types

- `bug` - Something broken
- `feature` - New functionality
- `task` - Work item (tests, docs, refactoring)
- `epic` - Large feature with subtasks
- `chore` - Maintenance (dependencies, tooling)

### Priorities

- `0` - Critical (security, data loss, broken builds)
- `1` - High (major features, important bugs)
- `2` - Medium (default, nice-to-have)
- `3` - Low (polish, optimization)
- `4` - Backlog (future ideas)

### Workflow for AI Agents

1. **Check ready work**: `bd ready` shows unblocked issues
2. **Claim your task**: `bd update <id> --status in_progress`
3. **Work on it**: Implement, test, document
4. **Discover new work?** Create linked issue:
   - `bd create "Found bug" --description="Details about what was found" -p 1 --deps discovered-from:<parent-id>`
5. **Complete**: `bd close <id> --reason "Done"`

### Auto-Sync

bd automatically syncs with git:

- Exports to `.beads/issues.jsonl` after changes (5s debounce)
- Imports from JSONL when newer (e.g., after `git pull`)
- No manual export/import needed!

### Important Rules

- ✅ Use bd for ALL task tracking
- ✅ Always use `--json` flag for programmatic use
- ✅ Link discovered work with `discovered-from` dependencies
- ✅ Check `bd ready` before asking "what should I work on?"
- ❌ Do NOT create markdown TODO lists
- ❌ Do NOT use external issue trackers
- ❌ Do NOT duplicate tracking systems

For more details, see README.md and docs/QUICKSTART.md.

<!-- END BEADS INTEGRATION -->

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:

   ```bash
   git pull --rebase
   bd sync
   git push
   git status  # MUST show "up to date with origin"
   ```

5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**

- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
