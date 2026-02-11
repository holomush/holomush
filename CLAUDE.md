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

| Directory     | Purpose                                      | Audience                |
| ------------- | -------------------------------------------- | ----------------------- |
| `site/docs/`  | Public documentation website (zensical)      | All users               |
| `docs/plans/` | Implementation plans, in-progress work       | Contributors (internal) |
| `docs/specs/` | Design specifications, architectural designs | Contributors (internal) |

**Site documentation** (`site/docs/`) is organized by audience:

- `contributors/` — For people contributing to the HoloMUSH codebase
- `developers/` — For plugin developers building on HoloMUSH
- `operators/` — For people running HoloMUSH servers

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

**See:** [Pull Request Guide](site/docs/contributors/pr-guide.md) for the complete workflow.

---

## Development Principles

### Test-Driven Development

- Tests MUST be written before implementation
- Tests MUST pass before any task is complete
- Use table-driven tests for comprehensive coverage
- Mock external dependencies (database, network)

### Spec-Driven Development

- Work MUST NOT start without a spec/design/plan
- Specs live in `docs/specs/`
- Plans live in `docs/plans/`
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
[Pull Request Guide](site/docs/contributors/pr-guide.md) for the complete workflow.

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

| Requirement                       | Description                                           |
| --------------------------------- | ----------------------------------------------------- |
| **MUST** maintain >80% coverage   | Per-package coverage must exceed 80%                  |
| **MUST** run `task test:coverage` | To verify coverage before completing work             |
| **SHOULD** target 90%+ coverage   | For core packages (`internal/core`, `internal/world`) |

### Test Files

- Tests live next to implementation: `foo.go` → `foo_test.go`
- Integration tests in `*_integration_test.go`
- Use build tags for integration tests: `//go:build integration`

### Table-Driven Tests

```go
func TestEventType_String(t *testing.T) {
    tests := []struct {
        name     string
        input    EventType
        expected string
    }{
        {"say event", EventTypeSay, "say"},
        {"pose event", EventTypePose, "pose"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := tt.input.String(); got != tt.expected {
                t.Errorf("got %q, want %q", got, tt.expected)
            }
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
task test      # Run tests
task build     # Build binary
task dev       # Run dev server
```

| Requirement                            | Description                                       |
| -------------------------------------- | ------------------------------------------------- |
| **MUST** use `task`                    | Never run Go/lint/fmt commands directly           |
| **MUST** run `task test`               | Before claiming any implementation is complete    |
| **MUST** run `task lint`               | Before committing changes                         |
| **MUST NOT** disable lint/format rules | Without explicit user confirmation                |
| **SHOULD** run `task fmt`              | Before committing to ensure consistent formatting |

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
    contributors/    # For codebase contributors
    developers/      # For plugin developers
    operators/       # For server operators
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
    Subscribe(ctx context.Context, stream string, afterID ulid.ULID) (<-chan Event, error)
    Replay(ctx context.Context, stream string, afterID ulid.ULID, limit int) ([]Event, error)
    LastEventID(ctx context.Context, stream string) (ulid.ULID, error)
}
```

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

## Patterns

### Event Sourcing

- All game actions produce events
- Events are immutable and ordered
- State is derived from event replay

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
