# HoloMUSH Development Guide

This document provides instructions for AI coding assistants working on HoloMUSH.

## Project Overview

HoloMUSH is a modern MUSH platform with:

- Go core with event-oriented architecture
- Dual protocol support (telnet + web)
- WASM plugin system via Extism
- PostgreSQL for all data
- SvelteKit PWA for web client

**Architecture Reference**: [docs/plans/2026-01-17-holomush-architecture-design.md](docs/plans/2026-01-17-holomush-architecture-design.md)

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

**See:** [Pull Request Guide](docs/reference/pull-request-guide.md) for the complete workflow.

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
[Pull Request Guide](docs/reference/pull-request-guide.md) for the complete workflow.

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

```go
// Wrap errors with context
if err != nil {
    return fmt.Errorf("failed to load event: %w", err)
}

// Use custom error types for domain errors
type NotFoundError struct {
    Resource string
    ID       string
}
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

**Lua plugins:**

```lua
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors
```

**Python plugins:**

```python
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
```

**Shell scripts:**

```bash
#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
```

| Requirement                         | Description                                              |
| ----------------------------------- | -------------------------------------------------------- |
| **MUST** include SPDX header        | All `.go`, `.lua`, `.sh`, `.py` files                    |
| **MUST NOT** add to generated files | Skip `*.pb.go` files                                     |
| **SHOULD** use `task license:add`   | Automatically adds headers to files missing them         |
| **Auto-added on commit**            | Lefthook pre-commit hook adds headers automatically      |

**Directories checked:** `cmd/`, `internal/`, `pkg/`, `plugins/`, `scripts/`

**Commands:**

```bash
task license:check   # Verify all files have headers
task license:add     # Add missing headers
```

## Testing

### Coverage Requirements

| Requirement                       | Description                                          |
| --------------------------------- | ---------------------------------------------------- |
| **MUST** maintain >80% coverage   | Per-package coverage must exceed 80%                 |
| **MUST** run `task test:coverage` | To verify coverage before completing work            |
| **SHOULD** target 90%+ coverage   | For core packages (`internal/core`, `internal/wasm`) |

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

### WASM Tests (`internal/wasm`)

The WASM package has special testing requirements due to Extism plugin compilation
overhead and concurrency constraints.

#### Plugin Concurrency Model

**Critical constraint:** Extism plugins are NOT thread-safe for concurrent calls.
This is a fundamental WebAssembly limitation, not language-specific—it applies to
plugins written in Python, Rust, Go, or any other language.

**Why:** WASM linear memory is shared between host and guest. When multiple callers
access the same plugin instance simultaneously, memory corruption occurs. Only one
call can safely execute at a time per plugin instance.

| Component          | Concurrency Model                                           |
| ------------------ | ----------------------------------------------------------- |
| `ExtismHost`       | Thread-safe for `LoadPlugin`/`UnloadPlugin` via RWMutex     |
| `ExtismSubscriber` | Serializes delivery per-plugin (async goroutines, but safe) |
| Plugin instance    | NOT thread-safe - single call at a time per plugin          |

**Production safety:** `ExtismSubscriber.HandleEvent` delivers events asynchronously
via goroutines but each plugin receives events sequentially (one at a time). The
5-second timeout ensures slow plugins don't block the event bus.

#### Test Requirements

| Requirement                              | Description                                              |
| ---------------------------------------- | -------------------------------------------------------- |
| **MUST** use shared host where possible  | Use `getSharedEchoHost()` for tests that read-only       |
| **MUST** use isolated host for mutations | Use `newIsolatedHost()` for tests that close/modify host |
| **MUST NOT** run parallel with shared    | Extism plugins are NOT thread-safe for concurrent calls  |
| **SHOULD** avoid loading plugins in loop | Each `LoadPlugin` takes ~1.5s for 10MB WASM compilation  |

**Test helpers** (in `test_helpers_test.go`):

```go
// Shared host - plugin compiled once, reused across sequential tests
host := getSharedEchoHost(t)
// DO NOT close this host

// Isolated host - for tests needing their own state
host := newIsolatedHost(t)
defer host.Close(context.Background())
```

**When to use each:**

| Helper              | Use Case                                               |
| ------------------- | ------------------------------------------------------ |
| `getSharedEchoHost` | Pattern matching, event delivery, subscriber tests     |
| `newIsolatedHost`   | Close tests, error handling, loading different plugins |

**Parallel test pattern:** When subtests need `t.Parallel()`, each MUST create its
own isolated host:

```go
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        t.Parallel()
        host := newIsolatedHost(t)  // Each parallel subtest needs own host
        defer host.Close(ctx)
        // ...
    })
}
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
  plans/             # Implementation plans
  reference/         # API documentation
  specs/             # Specifications
internal/            # Private implementation
  control/           # Control plane (admin API)
  core/              # Event system, sessions, world engine
  grpc/              # gRPC server implementation
  logging/           # Structured logging setup
  observability/     # Metrics and health endpoints
  proto/             # Generated protobuf code
  store/             # PostgreSQL implementations
  telnet/            # Telnet protocol adapter
  tls/               # TLS certificate management
  wasm/              # Plugin host (Extism)
  web/               # WebSocket adapter (future)
  xdg/               # XDG base directory support
pkg/                 # Public plugin API
  plugin/            # Plugin SDK types
plugins/             # Core plugins (WASM)
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

## Patterns

_This section will evolve as the project develops._

### Event Sourcing

- All game actions produce events
- Events are immutable and ordered
- State is derived from event replay

### ABAC (Attribute-Based Access Control)

- All access checks go through ABAC evaluator
- Policies define subject, resource, action, environment attributes
- Default deny - explicit allow required
