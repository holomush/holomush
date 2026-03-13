# Error Handling Modernization Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Migrate all fmt.Errorf calls to oops for structured error handling with context.

**Architecture:** Replace fmt.Errorf with oops.Errorf/oops.Wrap, adding .With() for context and .Code() at API boundaries. Create pkg/errutil helper package for logging and testing.

**Tech Stack:** github.com/samber/oops v1.16+

**Scope:** 208 error sites across 21 non-test files.

**Dependency:** Task 2 (pkg/errutil) requires testify. Complete Task 1 of testing-libraries-implementation first.

---

## Task 1: Add oops dependency

**Files:**

- Modify: `go.mod`

**Step 1: Add dependency**

```bash
go get github.com/samber/oops@latest
```

**Step 2: Verify installation**

```bash
go mod tidy
task build
```

Expected: Build succeeds with no errors.

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add github.com/samber/oops for structured errors"
```

---

## Task 2: Create pkg/errutil helper package

**Files:**

- Create: `pkg/errutil/doc.go`
- Create: `pkg/errutil/log.go`
- Create: `pkg/errutil/log_test.go`
- Create: `pkg/errutil/testing.go`
- Create: `pkg/errutil/testing_test.go`

**Step 1: Write failing test for LogError**

Create `pkg/errutil/log_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package errutil_test

import (
    "bytes"
    "encoding/json"
    "errors"
    "log/slog"
    "testing"

    "github.com/samber/oops"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/pkg/errutil"
)

func TestLogError_WithOopsError(t *testing.T) {
    var buf bytes.Buffer
    logger := slog.New(slog.NewJSONHandler(&buf, nil))

    err := oops.Errorf("something failed").
        Code("TEST_ERROR").
        With("key", "value")

    errutil.LogError(logger, "operation failed", err)

    var logEntry map[string]any
    require.NoError(t, json.Unmarshal(buf.Bytes(), &logEntry))
    assert.Equal(t, "ERROR", logEntry["level"])
    assert.Equal(t, "operation failed", logEntry["msg"])
    assert.Equal(t, "TEST_ERROR", logEntry["code"])
}

func TestLogError_WithStandardError(t *testing.T) {
    var buf bytes.Buffer
    logger := slog.New(slog.NewJSONHandler(&buf, nil))

    err := errors.New("standard error")

    errutil.LogError(logger, "operation failed", err)

    var logEntry map[string]any
    require.NoError(t, json.Unmarshal(buf.Bytes(), &logEntry))
    assert.Equal(t, "ERROR", logEntry["level"])
    assert.Contains(t, logEntry["error"], "standard error")
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./pkg/errutil/... -v
```

Expected: FAIL - package does not exist.

**Step 3: Create doc.go**

Create `pkg/errutil/doc.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package errutil provides utilities for working with oops structured errors.
package errutil
```

**Step 4: Implement LogError**

Create `pkg/errutil/log.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package errutil

import (
    "log/slog"

    "github.com/samber/oops"
)

// LogError logs an error with structured context if it's an oops error.
// For oops errors, it extracts and logs the message, code, context, and stacktrace.
// For standard errors, it logs the error string.
func LogError(logger *slog.Logger, msg string, err error) {
    if oopsErr, ok := oops.AsOops(err); ok {
        attrs := []any{
            "error", oopsErr.Message(),
        }
        if code := oopsErr.Code(); code != "" {
            attrs = append(attrs, "code", code)
        }
        if ctx := oopsErr.Context(); len(ctx) > 0 {
            attrs = append(attrs, "context", ctx)
        }
        logger.Error(msg, attrs...)
    } else {
        logger.Error(msg, "error", err)
    }
}
```

**Step 5: Run test to verify it passes**

```bash
go test ./pkg/errutil/... -v
```

Expected: PASS

**Step 6: Write failing test for AssertErrorCode**

Add to `pkg/errutil/testing_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package errutil_test

import (
    "testing"

    "github.com/samber/oops"

    "github.com/holomush/holomush/pkg/errutil"
)

func TestAssertErrorCode_MatchingCode(t *testing.T) {
    err := oops.Errorf("test").Code("MY_CODE")
    // Should not fail
    errutil.AssertErrorCode(t, err, "MY_CODE")
}
```

**Step 7: Implement AssertErrorCode**

Create `pkg/errutil/testing.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

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
    require.True(t, ok, "expected oops error, got %T", err)
    assert.Equal(t, code, oopsErr.Code())
}

// AssertErrorContext asserts that err is an oops error with the given context key/value.
func AssertErrorContext(t *testing.T, err error, key string, value any) {
    t.Helper()
    oopsErr, ok := oops.AsOops(err)
    require.True(t, ok, "expected oops error, got %T", err)
    ctx := oopsErr.Context()
    assert.Contains(t, ctx, key)
    assert.Equal(t, value, ctx[key])
}
```

**Step 8: Run all tests**

```bash
go test ./pkg/errutil/... -v -cover
```

Expected: PASS with >80% coverage.

**Step 9: Commit**

```bash
git add pkg/errutil/
git commit -m "feat(errutil): add helper package for oops error logging and testing"
```

---

## Task 3: Migrate internal/tls/certs.go (50 sites)

**Files:**

- Modify: `internal/tls/certs.go`

**Step 1: Add oops import**

Add to imports:

```go
"github.com/samber/oops"
```

**Step 2: Replace fmt.Errorf calls with oops.Wrap**

Pattern for wrapping errors:

```go
// BEFORE
return nil, fmt.Errorf("failed to generate CA key: %w", err)

// AFTER
return nil, oops.Wrap(err).With("operation", "generate CA key")
```

Pattern for errors with context:

```go
// BEFORE
return nil, fmt.Errorf("failed to create game URI: %w", err)

// AFTER
return nil, oops.Wrap(err).
    With("operation", "create game URI").
    With("gameID", gameID)
```

**Step 3: Run existing tests**

```bash
go test ./internal/tls/... -v
```

Expected: All tests pass (oops errors satisfy error interface).

**Step 4: Commit**

```bash
git add internal/tls/certs.go
git commit -m "refactor(tls): migrate to oops for structured errors"
```

---

## Task 4: Migrate cmd/holomush/core.go (25 sites)

**Files:**

- Modify: `cmd/holomush/core.go`

**Step 1: Add oops import**

**Step 2: Replace fmt.Errorf calls**

For validation errors (no wrapping):

```go
// BEFORE
return fmt.Errorf("grpc-addr is required")

// AFTER
return oops.Errorf("grpc-addr is required").Code("CONFIG_INVALID")
```

For wrapped errors:

```go
// BEFORE
return fmt.Errorf("failed to connect to database: %w", err)

// AFTER
return oops.Wrap(err).
    Code("DB_CONNECT_FAILED").
    With("operation", "connect to database")
```

**Step 3: Run tests**

```bash
go test ./cmd/holomush/... -v
```

**Step 4: Commit**

```bash
git add cmd/holomush/core.go
git commit -m "refactor(cmd): migrate core.go to oops with error codes"
```

---

## Task 5: Migrate cmd/holomush/gateway.go (15 sites)

**Files:**

- Modify: `cmd/holomush/gateway.go`

Follow same pattern as Task 4. Add .Code() for CLI-level errors.

**Step 1: Add oops import and migrate**

**Step 2: Run tests**

```bash
go test ./cmd/holomush/... -v
```

**Step 3: Commit**

```bash
git add cmd/holomush/gateway.go
git commit -m "refactor(cmd): migrate gateway.go to oops with error codes"
```

---

## Task 6: Migrate cmd/holomush/status.go (10 sites)

**Files:**

- Modify: `cmd/holomush/status.go`

Follow same pattern as Task 4.

---

## Task 7: Migrate cmd/holomush/migrate.go (3 sites)

**Files:**

- Modify: `cmd/holomush/migrate.go`

Follow same pattern as Task 4.

---

## Task 8: Migrate internal/plugin/goplugin/host.go (16 sites)

**Files:**

- Modify: `internal/plugin/goplugin/host.go`

**Step 1: Add oops import**

**Step 2: Replace fmt.Errorf with context**

```go
// BEFORE
return fmt.Errorf("failed to load plugin: %w", err)

// AFTER
return oops.Wrap(err).
    With("plugin", name).
    With("operation", "load")
```

**Step 3: Run tests**

```bash
go test ./internal/plugin/goplugin/... -v
```

**Step 4: Commit**

```bash
git add internal/plugin/goplugin/host.go
git commit -m "refactor(plugin): migrate goplugin host to oops"
```

---

## Task 9: Migrate internal/plugin/manifest.go (13 sites)

**Files:**

- Modify: `internal/plugin/manifest.go`

Add plugin name context to all errors.

---

## Task 10: Migrate internal/plugin/lua/host.go (9 sites)

**Files:**

- Modify: `internal/plugin/lua/host.go`

Add plugin name and operation context.

---

## Task 11: Migrate internal/plugin/schema.go (8 sites)

**Files:**

- Modify: `internal/plugin/schema.go`

---

## Task 12: Migrate internal/plugin remaining files

**Files:**

- Modify: `internal/plugin/manager.go` (3 sites)
- Modify: `internal/plugin/capability/enforcer.go` (2 sites)
- Modify: `internal/plugin/lua/state.go` (1 site)

---

## Task 13: Migrate internal/store/postgres.go (13 sites)

**Files:**

- Modify: `internal/store/postgres.go`

Add stream ID and query context:

```go
return oops.Wrap(err).
    With("stream", stream).
    With("operation", "append event")
```

---

## Task 14: Migrate internal/grpc (14 sites)

**Files:**

- Modify: `internal/grpc/server.go` (7 sites)
- Modify: `internal/grpc/client.go` (7 sites)

Add .Code() for gRPC boundary errors.

---

## Task 15: Migrate internal/control/grpc_server.go (13 sites)

**Files:**

- Modify: `internal/control/grpc_server.go`

Add .Code() for control API boundary errors.

---

## Task 16: Migrate remaining packages

**Files:**

- Modify: `internal/core/engine.go` (5 sites)
- Modify: `internal/core/ulid.go` (1 site)
- Modify: `internal/observability/server.go` (3 sites)
- Modify: `internal/xdg/xdg.go` (2 sites)
- Modify: `internal/telnet/server.go` (1 site)
- Modify: `pkg/plugin/sdk.go` (1 site)

---

## Task 17: Update CLAUDE.md error handling patterns

**Files:**

- Modify: `CLAUDE.md`

Add oops patterns to Error Handling section:

```markdown
### Error Handling

Use oops for structured errors with context:

\`\`\`go
// Wrap existing error with context
return oops.Wrap(err).
With("plugin", name).
With("operation", "load")

// Create new error
return oops.Errorf("validation failed").With("field", fieldName)

// At API boundaries, add error code
return oops.Wrap(err).
Code("PLUGIN_LOAD_FAILED").
With("plugin", name)
\`\`\`
```

---

## Task 18: Final validation

**Step 1: Run all tests**

```bash
task test
```

**Step 2: Run linter**

```bash
task lint
```

**Step 3: Verify no fmt.Errorf remains (except tests)**

```bash
grep -r "fmt\.Errorf" --include="*.go" | grep -v "_test.go" | wc -l
```

Expected: 0

**Step 4: Commit any final fixes**

```bash
git add .
git commit -m "refactor: complete oops migration for structured errors"
```

---

## Summary

| Package                | Files  | Error Sites |
| ---------------------- | ------ | ----------- |
| internal/tls           | 1      | 50          |
| cmd/holomush           | 4      | 53          |
| internal/plugin        | 6      | 52          |
| internal/store         | 1      | 13          |
| internal/grpc          | 2      | 14          |
| internal/control       | 1      | 13          |
| internal/core          | 2      | 6           |
| internal/observability | 1      | 3           |
| internal/xdg           | 1      | 2           |
| internal/telnet        | 1      | 1           |
| pkg/plugin             | 1      | 1           |
| **Total**              | **21** | **208**     |
