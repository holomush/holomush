# Error Handling Modernization

**Goal:** Adopt oops and errgroup for structured error handling and concurrent patterns.

**Scope:** Full codebase migration (316 error sites). No backward compatibility required - zero releases to date.

**Architecture Reference:** [HoloMUSH Architecture](2026-01-17-holomush-architecture-design.md)

---

## Dependencies

```go
github.com/samber/oops v1.16+
golang.org/x/sync/errgroup
```

## Patterns

### Error Creation

```go
// Simple error with context
return oops.Errorf("plugin not found").With("name", name)

// Wrapping existing error
return oops.Wrap(err).With("plugin", name).With("operation", "load")
```

### API Boundaries

At public API boundaries (gRPC handlers, CLI commands), add error codes for programmatic handling:

```go
// In a gRPC handler or CLI command
return oops.Errorf("authentication failed").
    Code("AUTH_FAILED").
    With("user", userID)
```

### Sentinel Errors

Sentinel errors remain standard `errors.New()`:

```go
var ErrStreamEmpty = errors.New("stream is empty")
```

Wrap with context when returning:

```go
return oops.Wrap(ErrStreamEmpty).With("stream", streamID)
```

### Concurrent Operations

Replace `sync.WaitGroup` with `errgroup`:

```go
g, ctx := errgroup.WithContext(ctx)
for _, plugin := range plugins {
    g.Go(func() error {
        return loadPlugin(ctx, plugin)
    })
}
return g.Wait()
```

## Migration Scope

| Package             | Error Sites | Notes                                        |
| ------------------- | ----------- | -------------------------------------------- |
| `internal/plugin/*` | ~80         | Plugin loading, host functions, capabilities |
| `internal/grpc`     | ~60         | Server/client, includes WaitGroup → errgroup |
| `internal/store`    | ~40         | Database operations                          |
| `internal/core`     | ~30         | Engine, events                               |
| `pkg/plugin`        | ~20         | SDK types                                    |
| `cmd/`              | ~15         | CLI commands                                 |
| Other               | ~70         | telnet, tls, control, etc.                   |

### What Gets Migrated

- All `fmt.Errorf` calls → `oops.Errorf` or `oops.Wrap`
- Add `.With()` context for key identifiers (plugin names, stream IDs, user IDs)
- Add `.Code()` at public API boundaries (gRPC handlers, CLI commands)
- `sync.WaitGroup` → `errgroup` in `internal/grpc/server.go`

### What Stays Unchanged

- Sentinel errors (`var ErrFoo = errors.New(...)`)
- Test helpers and mocks

## Logging Integration

### Helper Package

Create `pkg/errutil/log.go`:

```go
package errutil

import (
    "log/slog"
    "github.com/samber/oops"
)

// LogError logs an error with structured context if it's an oops error.
func LogError(logger *slog.Logger, msg string, err error) {
    if oopsErr, ok := oops.AsOops(err); ok {
        logger.Error(msg,
            "error", oopsErr.Message(),
            "code", oopsErr.Code(),
            "context", oopsErr.Context(),
            "stacktrace", oopsErr.Stacktrace(),
        )
    } else {
        logger.Error(msg, "error", err)
    }
}
```

### Error Codes Convention

- Use SCREAMING_SNAKE: `PLUGIN_NOT_FOUND`, `AUTH_FAILED`, `STREAM_EMPTY`
- Codes are for programmatic handling, messages are for humans
- Only add codes at package boundaries where callers might switch on them

## Testing

### Test Helpers

Create `pkg/errutil/testing.go`:

```go
package errutil

import (
    "testing"
    "github.com/samber/oops"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// AssertErrorCode asserts that err is an oops error with the given code.
func AssertErrorCode(t *testing.T, err error, code string) {
    t.Helper()
    oopsErr, ok := oops.AsOops(err)
    require.True(t, ok, "expected oops error")
    assert.Equal(t, code, oopsErr.Code())
}
```

### Compatibility

- Existing tests continue to work - oops errors satisfy the `error` interface
- `errors.Is()` and `errors.As()` work with oops-wrapped errors

## Acceptance Criteria

- [ ] All `fmt.Errorf` migrated to `oops.Errorf` or `oops.Wrap`
- [ ] Key identifiers added via `.With()` (plugin names, stream IDs, etc.)
- [ ] Error codes added at gRPC and CLI boundaries
- [ ] `sync.WaitGroup` replaced with `errgroup` in grpc/server.go
- [ ] `pkg/errutil` helper package created with `LogError` and test utilities
- [ ] All existing tests pass
- [ ] No new linter warnings

## Implementation Strategy

**Big bang:** Single large PR migrating everything at once. Acceptable risk given zero releases and comprehensive test coverage.

## Related Issues

- beads: holomush-1hq.22 (Investigate adopting oops and errgroup)
