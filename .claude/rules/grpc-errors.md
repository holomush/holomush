<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

---
paths:
  - "internal/grpc/**"
  - "plugins/**/*.go"
  - "internal/web/**"
  - "**/*handler*.go"
---

# gRPC Error Handling Rules

These rules come from CodeRabbit findings on PR #267 (`holomush-095g`). They were caught after design + spec + code reviews missed them, which is why they're codified here.

## Never leak inner errors past trust boundaries

**Rule:** never use `status.Errorf(codes.Internal, "...: %v", err)` in a gRPC handler when `err` is an internal error.

**Why:** the `%v` substitution drops the inner error text into `Status.message`, which is wire-level and reaches clients. That leaks internals (table names, query fragments, stack pointers, file paths).

**Pattern:**

```go
// WRONG — leaks inner error text to client
return nil, status.Errorf(codes.Internal, "load scene: %v", err)

// RIGHT — log internally, return generic message
errutil.LogErrorContext(ctx, "load scene failed", err)
return nil, status.Errorf(codes.Internal, "internal error")
```

**Log with `errutil.LogErrorContext(ctx, msg, err, extraAttrs...)`** (per the
CLAUDE.md Error Handling rule), not a bare `slog.*Context`. It forwards `ctx`
for trace/span correlation **and** extracts the oops `code` + context map as
structured fields — a bare `slog.ErrorContext(ctx, msg, "err", err)` flattens
the oops error to a plain string, losing the code you would filter on in
Loki/Sentry. It still takes extra key/value attrs for handler context
(`"scene_id", id`).

**Return `status.Errorf` with a static string (no format verb):** it is
wrapcheck-allowlisted, so the opaque return needs no `//nolint`. `status.Error(codes.Internal, "internal error")` is behaviorally identical but is **not** allowlisted, so it requires a line-scoped `//nolint:wrapcheck` (see [Linter compliance](#linter-compliance)). Either is fine; prefer `Errorf` to keep the diff nolint-free.

## Translate gRPC errors at ONE layer only

**Rule:** convert `status.Status` ↔ `oops` codes at exactly one layer in the call chain — the outermost call site.

**Why:** double-translation breaks `status.FromError` chain-walking. Example: `fetchHistoryFramesFromBus` calls `mapHistoryError`, then an outer caller wraps with `oops.Code(INTERNAL).Wrap(...)` and calls `mapHistoryError` again. The inner translation already converted the `status.Error` into a fresh `oops` with no `GRPCStatus` method. Net result: the outer `INTERNAL` survives, opacity invariants are broken.

**Pattern:** translate at the topmost point in the call chain that crosses the gRPC boundary, never inside helpers.

## Wire opacity needs TOP-LEVEL code assertions

**Rule:** `errutil.AssertErrorCode` walks `errors.Is` and silently passes on double-wrap (e.g., `oops(INTERNAL).Wrap(oops(STREAM_ACCESS_DENIED))` would falsely satisfy a check for `STREAM_ACCESS_DENIED`).

**Pattern:** for opacity contracts, use `oops.AsOops(err).Code()` to assert the **top-level** code, not chain-walking helpers.

```go
// WRONG — chain-walks; misses double-wrapping
errutil.AssertErrorCode(t, err, "STREAM_ACCESS_DENIED")

// RIGHT — asserts top-level code, no chain walk
got := oops.AsOops(err).Code()
require.Equal(t, "STREAM_ACCESS_DENIED", got)
```

## Linter compliance

When fixing lint failures in gRPC pass-through code, use **line-scoped** `//nolint:wrapcheck` directives with explanatory comments — NOT config-wide ignore patterns in `.golangci.yaml`. Repo precedent: `internal/web/handler.go:381,418,460,484` use `//nolint:wrapcheck // gRPC status errors pass through as-is`. There are 27+ such directives in the codebase. Widening `.golangci.yaml` violates CLAUDE.md ("MUST NOT disable lint/format rules without explicit user confirmation") and bypasses code-reviewer scrutiny on the actual offending site.
