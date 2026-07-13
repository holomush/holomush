---
title: "Coding Standards"
---

This reference is for contributors writing or reviewing HoloMUSH code. It covers
Go conventions, testing requirements, build commands, and code style — the rules
that `task lint` enforces and that reviewers check. For the broader development
workflow (spec-first, PR process, issue tracking), see the
[Pull Request Guide](/contributing/how-to/pr-guide/).

## Development Principles

### Test-Driven Development

Write tests before implementation:

1. Write a failing test
2. Implement the minimum code to pass
3. Refactor while keeping tests green
4. Repeat

All tests must pass before any task is considered complete.

### Spec-Driven Development

Work must be guided by specifications:

- Specs define requirements (`docs/specs/`)
- Plans define implementation approach (`docs/plans/`)
- Both use [RFC2119 keywords](#rfc2119-keywords) for requirement levels

## RFC2119 Keywords

Documentation uses these keywords to indicate requirement levels:

| Keyword        | Meaning                                    |
| -------------- | ------------------------------------------ |
| **MUST**       | Absolute requirement                       |
| **MUST NOT**   | Absolute prohibition                       |
| **SHOULD**     | Recommended, may ignore with justification |
| **SHOULD NOT** | Not recommended                            |
| **MAY**        | Optional                                   |

## Go Conventions

### Idiomatic Go

Follow standard Go idioms:

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

For logging oops errors, use `pkg/errutil`:

```go
errutil.LogError(logger, "operation failed", err)
```

### Error Correlation for Plugins

When internal errors occur during plugin host function calls, the error is sanitized
before returning to the plugin. To help operators debug issues reported by plugins,
a correlation ID (ULID) is included in both the log entry and the sanitized error
message.

**Plugin receives:**

```text
internal error (ref: 01KFSM8W2JKVD441APK779B4PN)
```

**Server log contains:**

```text
ERROR internal error in plugin query error_id=01KFSM8W2JKVD441APK779B4PN plugin=my-plugin ...
```

**Correlation workflow:**

1. Plugin reports error to user (e.g., "internal error (ref: 01KFSM8W...)")
2. User contacts server operator with the reference ID
3. Operator searches logs for `error_id=01KFSM8W...`
4. Full error details including stack trace are available in the log entry

This pattern ensures internal details (database errors, stack traces) are never
exposed to plugins while still enabling effective debugging.

### Logging

Use structured logging with slog. When a `context.Context` is in scope,
use the context-carrying variant — it threads `trace_id`/`span_id` into
the log entry, which is how Loki and Sentry correlate logs to traces.

```go
// CORRECT — ctx is available, use InfoContext
slog.InfoContext(ctx, "plugin loaded",
    "plugin", name,
    "version", version,
    "load_time_ms", loadTime.Milliseconds())

// WRONG — ctx is available but dropped; log line is orphaned
slog.Info("plugin loaded", "plugin", name)
```

Bare `slog.Info`/`slog.Warn` are only acceptable in `main`, `init`, or
bare goroutines where no context is reachable. The `sloglint` linter
(run by `task lint`) enforces this and will flag violations.

### Naming

Use clear, descriptive names:

- Avoid abbreviations except well-known ones (ID, URL, HTTP)
- Package names are lowercase, single words when possible
- Use MixedCaps (PascalCase for exported, camelCase for unexported)

### License Headers

All source files must include SPDX license headers:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package foo provides ...
package foo
```

Headers are auto-added by the pre-commit hook, or manually with:

```bash
task license:add
```

## Testing

### Coverage Requirements

| Requirement                  | Threshold |
| ---------------------------- | --------- |
| Minimum per-package coverage | 80%       |
| Target for core packages     | 90%+      |

Verify coverage before completing work:

```bash
task test:cover
```

### Test Organization

- Unit tests: `foo.go` -> `foo_test.go` (same directory)
- Integration tests: `*_integration_test.go` with build tag

```go
//go:build integration

package foo_test
```

### Table-Driven Tests

Use table-driven tests for comprehensive coverage:

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
            assert.Equal(t, tt.expected, tt.input.String())
        })
    }
}
```

### Assertions

Use testify for assertions:

```go
// Equality
assert.Equal(t, expected, got)

// Error checking
require.NoError(t, err)
assert.Error(t, err)

// Contains
assert.Contains(t, slice, element)
```

### Mocking

Use mockery for generating mocks:

```bash
mockery  # Uses .mockery.yaml config
```

Use generated mocks in tests:

```go
store := mocks.NewMockEventStore(t)
store.EXPECT().Append(mock.Anything, mock.Anything).Return(nil)
```

### Integration Tests (BDD)

Integration tests use Ginkgo/Gomega for BDD-style specs:

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

Run integration tests:

```bash
task test:int
```

## Build Commands

Use `task` for all build operations. Do not run Go commands directly:

```bash
task lint      # Run all linters
task fmt       # Format all files
task test      # Run tests
task build     # Build binary
task dev       # Run dev server
```

These commands ensure consistent tooling and configuration.

## Code Style

### Simplicity

Prefer readable code over clever solutions:

- Write code that's easy to understand
- Avoid premature optimization
- Keep functions focused and small

### Comments

- Do not add comments that restate what code does
- Comment the "why", not the "what"
- Use doc comments for exported functions/types

### Consistency

Match existing project patterns:

- Follow the conventions in nearby code
- Use consistent formatting (enforced by `task fmt`)
- Match naming patterns used elsewhere

## Directory Structure

```text
api/                 # Protocol definitions (protobuf)
cmd/holomush/        # Server entry point
docs/
  plans/             # Implementation plans
  specs/             # Design specifications
internal/            # Private implementation
  core/              # Event system, sessions
  grpc/              # gRPC server implementation
  logging/           # Structured logging setup
  observability/     # Metrics and health endpoints
  plugin/            # Plugin host (Lua & Go)
  store/             # PostgreSQL implementations
  telnet/            # Telnet protocol adapter
  tls/               # TLS certificate management
  web/               # WebSocket adapter
  world/             # World engine, characters, locations
  xdg/               # XDG base directory support
pkg/                 # Public plugin API
plugins/             # Core plugins (Lua for customization, Go for performance)
scripts/             # Build and utility scripts
test/integration/    # End-to-end test suites
```

## Key Interfaces

Core interfaces that implementations must satisfy:

### EventStore

```go
type EventStore interface {
    Append(ctx context.Context, event Event) error
    Replay(ctx context.Context, stream string, afterID ulid.ULID, limit int) ([]Event, error)
    LastEventID(ctx context.Context, stream string) (ulid.ULID, error)
    Subscribe(ctx context.Context, stream string) (eventCh <-chan ulid.ULID, errCh <-chan error, err error)
}
```

### AccessControl

```go
type AccessControl interface {
    Check(ctx context.Context, subject, action, resource string) bool
}
```

## Patterns

### Event-Driven Architecture

- All game actions produce events
- Events are immutable and ordered
- World state is canonical in PostgreSQL; the event feed is an append-only audit and notification log (the decided model — see ADR `docs/adr/holomush-i4784-world-state-model-decision.md`)

### Dependency Injection

- Accept interfaces in constructors
- Create implementations at composition root
- Enables testing with mocks

### Context Propagation

- Pass context through call chains
- Use context for cancellation and timeouts
- Include tracing spans in context

## Further Reading

- [Pull Request Guide](/contributing/how-to/pr-guide/) - Contribution workflow
- [Architecture](/contributing/explanation/architecture/) - System design overview
