# HoloMUSH Development Guide

This document provides instructions for AI coding assistants working on HoloMUSH.

## Project Overview

HoloMUSH is a modern MUSH platform with:

- Go core with event-oriented architecture
- Dual protocol support (telnet + web)
- WASM plugin system via wazero
- PostgreSQL for all data
- SvelteKit PWA for web client

**Architecture Reference**: [docs/plans/2026-01-17-holomush-architecture-design.md](docs/plans/2026-01-17-holomush-architecture-design.md)

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

All tasks MUST be reviewed by a separate agent before completion:

| Requirement                                | Description                                          |
| ------------------------------------------ | ---------------------------------------------------- |
| **MUST** use `pr-review-toolkit:review-pr` | Launch comprehensive review using specialized agents |
| **MUST** address all findings              | Fix issues or document why not applicable            |
| **MUST NOT** skip review                   | Even for "simple" changes                            |
| **SHOULD** review after each logical chunk | Don't batch too many changes                         |

**Review process:**

1. Complete implementation and tests
2. Invoke `pr-review-toolkit:review-pr` skill
3. Review agent findings across all specialized reviewers
4. Address each finding (fix or justify skipping)
5. Re-run review if significant changes were made
6. Only then mark task complete

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

## Testing

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
cmd/holomush/        # Server entry point
internal/            # Private implementation
  core/              # Event system, sessions, world engine
  telnet/            # Telnet protocol adapter
  web/               # WebSocket adapter (future)
  store/             # PostgreSQL implementations
  wasm/              # Plugin host (wazero)
pkg/                 # Public plugin API
  plugin/            # Plugin SDK types
  api/               # Game API for plugins
plugins/             # Core plugins (WASM)
docs/
  specs/             # Specifications
  plans/             # Implementation plans
  reference/         # API documentation
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
