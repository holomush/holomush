<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Plugin-path `mapHistoryError` Context Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Preserve `session_id` and `stream` on the plugin-path `PermissionDenied` oops chain in `mapHistoryError` (matching the outer I-17 gate's log context), and preserve `status.WithDetails` proto messages on `InvalidArgument` pass-through, all without breaking the wire-level opacity invariant.

**Architecture:** Single private function `mapHistoryError` in `internal/grpc/query_stream_history.go` gains two parameters and a small body change in two of its branches. One production caller and seven test callers update mechanically. Two test assertions sharpen, one new test added. Approach (a) signature change per spec §3.3.

**Tech Stack:** Go 1.22+, `google.golang.org/grpc/status` + `codes`, `github.com/samber/oops` v1.21.0, `github.com/stretchr/testify`, `google.golang.org/genproto/googleapis/rpc/errdetails` (new test-file import; transitive dep already in `go.mod:155`), `google.golang.org/protobuf/proto` (already imported), Taskfile.

**Spec:** [`docs/superpowers/specs/2026-04-25-plugin-maphistoryerror-context-design.md`](../specs/2026-04-25-plugin-maphistoryerror-context-design.md)

**Bead:** `holomush-095g.6`

**Working dir:** `/Users/sean/Code/github.com/holomush/.worktrees/095g.6` (jj workspace; parent commit = `main@origin` = PR #267 `yytk`).

**VCS:** jj-colocated repo. The spec+plan currently sits on jj change `uk` (`@` at plan-write time). To avoid clobbering its description and mixing edits into the docs commit (per project memory `feedback_jj_empty_wc_before_task`), the cadence is **new-first, then edit, then describe**:

1. `jj new` — start a fresh empty change on top of `@`.
2. Edit the files for the task.
3. `JJ_EDITOR=true jj --no-pager describe -m "..."` — set the message on the change you just edited.

Task 1 begins with a `jj new` step (Step 0) to peel off from the docs commit. Each subsequent task starts with `jj new` as its first step. Do NOT push until Task 5. NEVER run bare `jj rebase -d main` — follow-up commits stack on top.

---

## File Structure

| File | Action | Responsibility |
| ---- | ------ | -------------- |
| `internal/grpc/query_stream_history.go` | Modify | `mapHistoryError` signature + body (PermissionDenied + InvalidArgument branches); single call-site update at line 233 |
| `internal/grpc/query_stream_history_test.go` | Modify | Update 7 existing `mapHistoryError(...)` callers; sharpen 2 tests; add 1 new test for `WithDetails` round-trip |
| `go.mod` | Modify | Promote `google.golang.org/genproto/googleapis/rpc` from `// indirect` to direct (Task 4's test-file `errdetails` import is the first direct usage) |
| `docs/superpowers/plans/2026-04-25-plugin-maphistoryerror-context.md` | Create | This plan |

The `go.mod` update is performed by `go mod tidy` in Task 4 Step 6 once the new direct import is in place. No production source files beyond the two `internal/grpc/` files need touching.

---

## Task 1: Refactor `mapHistoryError` signature (no behavior change)

**Why this is first:** the signature change is a mechanical refactor that touches 1 production caller + 7 test callers. Doing it in isolation, with all existing assertions still passing, gives a clean checkpoint before any behavior changes. The new params are unused initially — Go does not warn about unused function parameters, so the build stays green.

**Files:**

- Modify: `internal/grpc/query_stream_history.go:233-235` (call site) and `:271` (function declaration)
- Modify: `internal/grpc/query_stream_history_test.go:705,716,727,738,751,766,780` (7 test call sites)

- [ ] **Step 0: Start a fresh jj change**

Critical first step. The plan-writing change `uk` currently has the spec+plan as `@`. Editing source files now and then `jj describe` would (a) merge code edits into the docs commit and (b) overwrite the docs commit's description.

Run:

```bash
jj new
jj st                       # MUST show: "Working copy changes:" empty
jj log -r '@-' --no-pager   # MUST show the docs commit (uk, description starting "docs(plan):")
```

Expected: working copy clean; `@-` is `uk`. If `jj st` shows changes already, STOP and reconcile before proceeding (the previous task may have left uncommitted edits).

- [ ] **Step 1: Update `mapHistoryError` declaration**

In `internal/grpc/query_stream_history.go`, change the function declaration at line 270-271 from:

```go
// mapHistoryError translates eventbus cursor errors to gRPC status codes.
func mapHistoryError(err error) error {
```

to:

```go
// mapHistoryError translates eventbus cursor errors and plugin-emitted
// gRPC status errors into the host's wire-level error vocabulary.
//
// sessionID and stream MUST be the request-scoped values from the
// QueryStreamHistoryRequest. They are attached to the oops chain on the
// PermissionDenied translation so server logs match the outer I-17 gate
// (see internal/grpc/query_stream_history.go:170-173).
//
//nolint:unparam,revive // TODO(holomush-095g.6): sessionID/stream wired in Task 2.
func mapHistoryError(err error, sessionID, stream string) error {
```

Leave the function body unchanged for now. The new params `sessionID` and `stream` are unused in this step. The `//nolint:unparam` directive is required because `.golangci.yaml` has `unparam` enabled (line 31) and will flag the unused params; the directive is removed in Task 2 Step 3 once the params are actually used.

- [ ] **Step 2: Update the production caller**

In `internal/grpc/query_stream_history.go`, change lines 233-235 from:

```go
		return nil, mapHistoryError(oops.Code("INTERNAL").
			With("stream", req.Stream).
			Wrap(fetchErr))
```

to:

```go
		return nil, mapHistoryError(
			oops.Code("INTERNAL").With("stream", req.Stream).Wrap(fetchErr),
			req.SessionId,
			req.Stream,
		)
```

- [ ] **Step 3: Update the 7 test callers**

In `internal/grpc/query_stream_history_test.go`, update each of the 7 `mapHistoryError(...)` calls. For brevity, pick deterministic fixtures `"test-session"` and `"location:test"`. Each call site change is:

| Line | Existing | Replacement |
| ---- | -------- | ----------- |
| 705  | `got := mapHistoryError(wrapped)` | `got := mapHistoryError(wrapped, "test-session", "location:test")` |
| 716  | `got := mapHistoryError(wrapped)` | `got := mapHistoryError(wrapped, "test-session", "location:test")` |
| 727  | `got := mapHistoryError(wrapped)` | `got := mapHistoryError(wrapped, "test-session", "location:test")` |
| 738  | `got := mapHistoryError(orig)` | `got := mapHistoryError(orig, "test-session", "location:test")` |
| 751  | `got := mapHistoryError(pluginErr)` | `got := mapHistoryError(pluginErr, "test-session", "location:test")` |
| 766  | `got := mapHistoryError(pluginErr)` | `got := mapHistoryError(pluginErr, "test-session", "location:test")` |
| 780  | `got := mapHistoryError(eventbus.ErrCursorInvalid)` | `got := mapHistoryError(eventbus.ErrCursorInvalid, "test-session", "location:test")` |

Verify with `rg -n 'mapHistoryError\(' internal/grpc/query_stream_history_test.go` after edits — every line must show three arguments.

- [ ] **Step 4: Run all `internal/grpc` tests to confirm no regressions**

Run: `task test -- ./internal/grpc/...`

Expected: all tests PASS, including all `TestMapHistoryError*`, `TestQueryStreamHistory*`, and `TestQueryStreamHistoryTranslatesPluginPermissionDeniedToOpaqueCode`. No assertions changed, so nothing should fail. If anything fails, the most likely cause is a typo in one of the seven call sites — re-check argument count.

- [ ] **Step 5: Run lint**

Run: `task lint`

Expected: PASS. The pre-added `//nolint:unparam,revive` directive on `mapHistoryError` (Step 1) silences both `unparam` and revive's `unused-parameter` rule (`.golangci.yaml:31` and `:130` respectively). Both fire on unused function parameters; Go's compiler itself does not.

- [ ] **Step 6: Describe the change**

```bash
JJ_EDITOR=true jj --no-pager describe -m "refactor(grpc): add sessionID/stream params to mapHistoryError

No behavior change. Threads request-scoped sessionID/stream through
mapHistoryError so the next change can attach them to the
PermissionDenied oops chain (matching the outer I-17 gate's log
context). The new params are unused in this commit; nolint:unparam
is removed in the next task once the params are wired up.

Bead: holomush-095g.6"
```

(Do NOT run `jj new` here — the next task starts with its own `jj new` step.)

---

## Task 2: Attach `session_id` + `stream` to `PermissionDenied` translation (G1)

**Files:**

- Modify: `internal/grpc/query_stream_history_test.go:747-758` (sharpen test first)
- Modify: `internal/grpc/query_stream_history.go:281-282` (impl after test fails)
- Modify: `internal/grpc/query_stream_history.go` (remove the Task 1 `//nolint:unparam` directive in Step 3)

- [ ] **Step 0: Start a fresh jj change**

Run:

```bash
jj new
jj st                       # MUST show: "Working copy changes:" empty
jj log -r '@-' --no-pager   # MUST show Task 1's "refactor(grpc): add sessionID/stream params" commit
```

- [ ] **Step 1: Sharpen `TestMapHistoryErrorTranslatesPermissionDeniedToOpaqueOopsCode`**

In `internal/grpc/query_stream_history_test.go`, replace the body of `TestMapHistoryErrorTranslatesPermissionDeniedToOpaqueOopsCode` (lines 747-758) with:

```go
func TestMapHistoryErrorTranslatesPermissionDeniedToOpaqueOopsCode(t *testing.T) {
	t.Parallel()

	pluginErr := status.Error(codes.PermissionDenied, "scene audit access denied")
	got := mapHistoryError(pluginErr, "test-session", "location:test")
	require.Error(t, got)

	oopsErr, ok := oops.AsOops(got)
	require.True(t, ok, "translated error MUST be oops-wrapped")
	assert.Equal(t, "STREAM_ACCESS_DENIED", oopsErr.Code(),
		"PermissionDenied from the plugin MUST collapse into the same opaque oops code the outer I-17 gate uses")

	// Log-parity assertion (G1): server-side observability requires the
	// same context fields the outer I-17 gate attaches at
	// internal/grpc/query_stream_history.go:170-173.
	ctx := oopsErr.Context()
	assert.Equal(t, "test-session", ctx["session_id"],
		"PermissionDenied translation MUST attach session_id to the oops chain for log parity")
	assert.Equal(t, "location:test", ctx["stream"],
		"PermissionDenied translation MUST attach stream to the oops chain for log parity")
}
```

- [ ] **Step 2: Run the sharpened test to verify it FAILS**

Run: `task test -- -run TestMapHistoryErrorTranslatesPermissionDeniedToOpaqueOopsCode ./internal/grpc/`

Expected: FAIL. The two new `assert.Equal` lines fail because the current impl at `query_stream_history.go:281-282` does not call `.With(...)` — `ctx["session_id"]` and `ctx["stream"]` are absent (zero value `nil`).

- [ ] **Step 3: Update the impl to attach the context AND remove the Task 1 `unparam` suppression**

In `internal/grpc/query_stream_history.go`, change lines 281-282 from:

```go
			return oops.Code("STREAM_ACCESS_DENIED").
				Errorf("not authorized to read stream")
```

to:

```go
			return oops.Code("STREAM_ACCESS_DENIED").
				With("session_id", sessionID).
				With("stream", stream).
				Errorf("not authorized to read stream")
```

Then remove the `//nolint:unparam,revive // TODO(holomush-095g.6): sessionID/stream wired in Task 2.` directive from the doc-comment block above `func mapHistoryError`. The params are now used; both `unparam` and revive's `unused-parameter` rule are no longer triggered, so the suppression goes.

- [ ] **Step 4: Run the sharpened test to verify it PASSES**

Run: `task test -- -run TestMapHistoryErrorTranslatesPermissionDeniedToOpaqueOopsCode ./internal/grpc/`

Expected: PASS.

- [ ] **Step 5: Run all `internal/grpc` tests to confirm no regressions**

Run: `task test -- ./internal/grpc/...`

Expected: all tests PASS. Special attention to:

- `TestQueryStreamHistoryTranslatesPluginPermissionDeniedToOpaqueCode` (line 868) — MUST still pass with no change. Asserts top-level oops code is `STREAM_ACCESS_DENIED`. Adding `.With(...)` between `.Code(...)` and `.Errorf(...)` does NOT shift the top-level node; the builder accumulates context on the same `OopsErrorBuilder` and `.Errorf(...)` materializes one `OopsError`. (Verified against `samber/oops@v1.21.0/builder.go:206-214`.)

- [ ] **Step 6: Run lint**

Run: `task lint`

Expected: PASS.

- [ ] **Step 7: Describe the change**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(grpc): attach session_id/stream to plugin PermissionDenied oops chain

Plugin-path PermissionDenied translation now attaches the same With()
context the outer I-17 gate at query_stream_history.go:170-173 does.
errutil.LogError walks oops.AsOops(err).Context() at
pkg/errutil/log.go:32-42, so server logs now emit identical attributes
regardless of which authorization wall caught the caller. Wire-level
opacity invariant preserved: top-level oops code remains
STREAM_ACCESS_DENIED with no chain shift
(TestQueryStreamHistoryTranslatesPluginPermissionDeniedToOpaqueCode
still passes).

Bead: holomush-095g.6 (goal G1)"
```

(Do NOT run `jj new` here — the next task starts with its own `jj new` step.)

---

## Task 3: Sharpen e2e pin test for plugin-path denial (G3 regression guard)

**Files:**

- Modify: `internal/grpc/query_stream_history_test.go:868-904` (`TestQueryStreamHistoryTranslatesPluginPermissionDeniedToOpaqueCode`)

The plugin-path PermissionDenied translation now attaches `session_id` and `stream`. The e2e test currently only asserts the top-level oops `Code()`. Sharpening it adds a regression guard against any future change that drops the With() call (e.g., a refactor that misses the context-threading).

- [ ] **Step 0: Start a fresh jj change**

Run:

```bash
jj new
jj st                       # MUST show: "Working copy changes:" empty
jj log -r '@-' --no-pager   # MUST show Task 2's "feat(grpc): attach session_id/stream..." commit
```

- [ ] **Step 1: Add Context assertions to the e2e pin test**

In `internal/grpc/query_stream_history_test.go`, append to the body of `TestQueryStreamHistoryTranslatesPluginPermissionDeniedToOpaqueCode` (after line 903's existing `assert.Equal`, before the closing `}`):

```go
	// G1 regression guard: the plugin-path translation MUST attach the
	// same context the outer I-17 gate does. Without these the server
	// log loses session_id/stream when the plugin wall catches.
	ctx := oopsErr.Context()
	assert.Equal(t, "s1", ctx["session_id"],
		"plugin-path translation MUST attach session_id from the request")
	assert.Equal(t, stream, ctx["stream"],
		"plugin-path translation MUST attach stream from the request")
```

The local `stream` variable is bound at line 871 (`stream, focus := sceneFocusMembership(t)`); it is the first return value of `sceneFocusMembership(t)`, currently `"scene:01HYXSCENE00000000000000CC:ic"`.

- [ ] **Step 2: Run the e2e pin test to verify it PASSES**

Run: `task test -- -run TestQueryStreamHistoryTranslatesPluginPermissionDeniedToOpaqueCode ./internal/grpc/`

Expected: PASS. Task 2 already implemented the With() calls, so the new assertions pass on this run. (If they fail here, Task 2 is incomplete — go back.)

- [ ] **Step 3: Describe the change**

```bash
JJ_EDITOR=true jj --no-pager describe -m "test(grpc): sharpen e2e pin test with session_id/stream Context assertions

TestQueryStreamHistoryTranslatesPluginPermissionDeniedToOpaqueCode now
asserts the With() context fields plugin-path PermissionDenied
attaches, providing a regression guard against a future refactor that
drops them. Closes spec §6 acceptance criterion 5 (server logs carry
session_id/stream): errutil.LogError walks oops.AsOops(err).Context()
at pkg/errutil/log.go:32-42, so the Context() assertion is sufficient
proof that LogError will emit those attributes — no separate manual
log inspection needed.

Bead: holomush-095g.6 (G1 regression guard)"
```

(Do NOT run `jj new` here — the next task starts with its own `jj new` step.)

---

## Task 4: Switch `InvalidArgument` branch to `st.Err()` (G2)

**Files:**

- Modify: `internal/grpc/query_stream_history_test.go` (add 1 new test + `errdetails` import)
- Modify: `internal/grpc/query_stream_history.go:283-288` (`InvalidArgument` branch)
- Modify: `go.mod`, `go.sum` (run `go mod tidy` to promote `genproto/googleapis/rpc` from `// indirect` to direct, since this PR introduces the first direct import)

- [ ] **Step 0: Start a fresh jj change**

Run:

```bash
jj new
jj st                       # MUST show: "Working copy changes:" empty
jj log -r '@-' --no-pager   # MUST show Task 3's "test(grpc): sharpen e2e..." commit
```

- [ ] **Step 1: Add the new `WithDetails` test**

In `internal/grpc/query_stream_history_test.go`, add `"google.golang.org/genproto/googleapis/rpc/errdetails"` to the third import group (the one containing other `google.golang.org/...` packages). Place it alphabetically — `genproto` sorts before `grpc`, so the line goes BEFORE `"google.golang.org/grpc/codes"`:

```go
import (
    "context"
    "errors"
    "io"
    "testing"
    "time"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "google.golang.org/genproto/googleapis/rpc/errdetails"  // NEW
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    "google.golang.org/protobuf/proto"

    "github.com/samber/oops"
    // ... (project imports unchanged)
)
```

`google.golang.org/protobuf/proto` is already imported (line 18).

Append the following test after `TestMapHistoryErrorRetainsCursorInvalidDispatchForNonStatusErrors` (current line 786 — find the closing `}` of that function and add this immediately after):

```go
// TestMapHistoryErrorPassesThroughInvalidArgumentWithDetails verifies that
// status.WithDetails proto messages attached by the plugin survive
// translation through mapHistoryError. Goal G2: pass-through MUST preserve
// the gRPC code AND any structured details. Bare-status pass-through is
// covered separately by TestMapHistoryErrorPassesThroughInvalidArgument
// (line 762).
func TestMapHistoryErrorPassesThroughInvalidArgumentWithDetails(t *testing.T) {
	t.Parallel()

	detail := &errdetails.BadRequest{
		FieldViolations: []*errdetails.BadRequest_FieldViolation{
			{Field: "subject", Description: "malformed"},
		},
	}
	pluginStatus, withErr := status.New(codes.InvalidArgument, "subject malformed").WithDetails(detail)
	require.NoError(t, withErr, "WithDetails MUST succeed for canonical errdetails proto")

	got := mapHistoryError(pluginStatus.Err(), "test-session", "location:test")
	require.Error(t, got)

	gotStatus, ok := status.FromError(got)
	require.True(t, ok, "translated error MUST carry a gRPC status")
	assert.Equal(t, codes.InvalidArgument, gotStatus.Code(),
		"InvalidArgument MUST pass through")

	details := gotStatus.Details()
	require.Len(t, details, 1, "exactly one detail proto MUST round-trip")

	gotDetail, ok := details[0].(*errdetails.BadRequest)
	require.True(t, ok, "detail proto MUST round-trip as *errdetails.BadRequest")
	assert.True(t, proto.Equal(detail, gotDetail),
		"detail proto MUST be byte-equal to the input via proto.Equal")
}
```

- [ ] **Step 2: Run the new test to verify it FAILS**

Run: `task test -- -run TestMapHistoryErrorPassesThroughInvalidArgumentWithDetails ./internal/grpc/`

Expected: FAIL on the `require.Len(t, details, 1, ...)` assertion. The current impl at line 288 calls `status.Errorf(codes.InvalidArgument, "%s", st.Message())`, which constructs a fresh `Status` with no details. `gotStatus.Details()` returns `nil` (length 0).

- [ ] **Step 3: Update the impl to return `st.Err()`**

In `internal/grpc/query_stream_history.go`, change lines 283-288 from:

```go
		case codes.InvalidArgument:
			// Use the extracted status message — using "%v" on err would
			// include the host-side oops wrapper text in the client-visible
			// message (e.g., "stream=...: subject required" instead of just
			// "subject required"). st.Message() is the plugin's original.
			return status.Errorf(codes.InvalidArgument, "%s", st.Message())
```

to:

```go
		case codes.InvalidArgument:
			// Preserves the plugin's gRPC code AND any status.WithDetails
			// proto messages it attached. NOTE: when err is a wrapped status
			// (the production shape per the call site at line 233), grpc's
			// status.FromError rewrites Status.Message to err.Error(),
			// which includes the outer oops chain text. That message-rewrite
			// is unchanged from PR #267's status.Errorf("%s", st.Message())
			// behavior (see spec §5 risk #3). This branch's contribution
			// is strictly Details preservation; message purity is a
			// separate concern.
			return st.Err()
```

- [ ] **Step 4: Run the new test to verify it PASSES**

Run: `task test -- -run TestMapHistoryErrorPassesThroughInvalidArgumentWithDetails ./internal/grpc/`

Expected: PASS.

- [ ] **Step 5: Run all `internal/grpc` tests to confirm no regressions**

Run: `task test -- ./internal/grpc/...`

Expected: all tests PASS. Critical regression guards:

- `TestMapHistoryErrorPassesThroughInvalidArgument` (line 762) — bare-status pin per spec §4.3.1. Still passes because `st.Err()` on a bare `status.Error(codes.InvalidArgument, "subject malformed")` returns wire-equivalent output to `status.Errorf("%s", "subject malformed")`.
- `TestMapHistoryErrorRetainsCursorInvalidDispatchForNonStatusErrors` (line 777) — non-status fallback. Still passes because the cursor-error dispatch chain runs after the gRPC status switch when no status is found.

- [ ] **Step 6: Run `go mod tidy` to promote `genproto/googleapis/rpc` to a direct dependency**

`google.golang.org/genproto/googleapis/rpc` is currently listed as `// indirect` in `go.mod:155`. Now that this PR adds the first direct import (`errdetails` in the test file), tidy will move the line out of the indirect block.

Run: `go mod tidy`

Expected: `go.mod` and `go.sum` are modified — line ~155's `// indirect` comment is removed (or the line moves to the direct-deps block). No other changes. Verify with:

```bash
git diff go.mod go.sum   # jj-colocated repo: git diff still works against the pseudo-HEAD
```

If tidy produces unrelated diffs (e.g., bumping unrelated transitive deps), STOP — that's out of scope for this PR. Investigate before continuing.

- [ ] **Step 7: Run lint**

Run: `task lint`

Expected: PASS. Any new imports in the test file are exercised; `go mod tidy` left `go.mod`/`go.sum` clean.

- [ ] **Step 8: Describe the change**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(grpc): preserve status.WithDetails on mapHistoryError InvalidArgument

InvalidArgument branch returns st.Err() instead of status.Errorf with
formatted message. Preserves the plugin's gRPC code AND any structured
proto details attached via status.WithDetails. Existing
TestMapHistoryErrorPassesThroughInvalidArgument continues to pin the
bare-status case per spec §4.3.1; new test
TestMapHistoryErrorPassesThroughInvalidArgumentWithDetails pins the
WithDetails round-trip per spec §4.3.

Message-rewriting on wrapped statuses is unchanged from PR #267 and
out-of-scope for this change (see spec §5 risk #3).

go.mod/go.sum updated by go mod tidy: errdetails is now a direct import.

Bead: holomush-095g.6 (goal G2)"
```

(Do NOT run `jj new` here — Task 5 doesn't add a new commit, only verification.)

---

## Task 5: Final verification (`task pr-prep`) and bead close

**Files:** none modified — verification + state update only.

- [ ] **Step 1: Run the full PR-prep gate**

Run: `task pr-prep`

Expected: green. Mirrors all CI jobs — lint, format, schema, license, unit, integration, e2e. This is mandatory per project policy ([CLAUDE.md "Pre-Push Review Gates"](../../../CLAUDE.md#pre-push-review-gates)).

If a gate fails:

- **Lint failure:** fix inline. Most likely culprit is the new `errdetails` import alphabetization or an unused import after refactor.
- **Test failure:** re-read the failure trace. Most likely culprit is a forgotten test caller from Task 1 — verify `rg -n 'mapHistoryError\(' internal/grpc/`.
- **License header missing on a new file:** none of this work creates new source files (only the spec + plan, which are docs and do not require Go SPDX headers); but if a header is missing somehow, run `task license:add`.
- **e2e failure:** unlikely — this work is internal to one Go package. If it happens, capture the output and stop; do not push partial green.

If `task pr-prep` is fully green, proceed.

- [ ] **Step 2: Run `/review-code` against the stack**

Run the project's adversarial code reviewer per CLAUDE.md "Pre-Push Review Gates" before push:

```text
/review-code
```

Expected: READY verdict. Address any blocking findings inline (re-run `task pr-prep` after fixes, then re-review). Non-blocking findings may be deferred but should be acknowledged.

- [ ] **Step 3: Update the bead with implementation summary and close it**

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd update holomush-095g.6 --notes "Implementation complete via stacked jj changes on top of main@origin (PR #267).

- mapHistoryError signature gained sessionID/stream params (Task 1 refactor).
- PermissionDenied branch now attaches session_id + stream to oops chain (Task 2, G1).
- E2E pin test sharpened with Context assertions (Task 3, G3 regression guard).
- InvalidArgument branch returns st.Err() to preserve WithDetails proto messages (Task 4, G2).
- Wire-level opacity invariant preserved: TestQueryStreamHistoryTranslatesPluginPermissionDeniedToOpaqueCode unchanged in its existing assertions.
- task pr-prep green; /review-code READY."

BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd close holomush-095g.6
```

- [ ] **Step 4: Set bookmark and push**

The new-first cadence means the stack ends with Task 4's commit on top of `@-` (the post-Task-4 `jj describe` does not start a new change). At this point `@` IS Task 4's commit (non-empty). Verify the stack:

```bash
jj log -r 'main@origin..@' --no-pager
```

Expected: 5 commits on top of `main@origin` — the docs commit `uk` (`docs(plan): ...`) at the bottom, then Tasks 1-4's commits, with Task 4 at `@`.

Determine the tip programmatically (the last non-empty change in the stack) so the bookmark cannot land on the wrong commit:

```bash
TIP=$(jj log -r 'main@origin..@ & ~empty()' --no-graph --no-pager -T 'change_id.short() ++ "\n"' | tail -1)
echo "Pushing tip: $TIP"
jj bookmark create holomush-095g.6 -r "$TIP"
jj git push --branch holomush-095g.6
```

(If `jj st` shows uncommitted changes — i.e., something went wrong and `@` has untracked edits — STOP and reconcile before pushing.)

Verify clean state:

```bash
jj st
jj bookmark list holomush-095g.6
```

Expected: working copy clean; bookmark points to the tip change ID; `jj git push` succeeded.

- [ ] **Step 5: Open the PR**

```bash
gh pr create \
  --base main \
  --head holomush-095g.6 \
  --title "feat(grpc): preserve session_id/stream + status.WithDetails on plugin-path mapHistoryError" \
  --body "$(cat <<'EOF'
## Summary

Code-quality follow-up to PR #267. The plugin-path translation in
`mapHistoryError` (`internal/grpc/query_stream_history.go`) now attaches
the same `session_id`/`stream` context the outer I-17 gate emits, and
the `InvalidArgument` branch preserves `status.WithDetails` proto
messages.

- **G1:** PermissionDenied translation matches the outer I-17 gate's `With()` context.
- **G2:** InvalidArgument pass-through preserves `status.WithDetails` proto messages.
- **G3:** Wire-level opacity invariant preserved — top-level oops code remains `STREAM_ACCESS_DENIED`, pinning test unchanged in its existing assertions.

## Spec & plan

- Spec: `docs/superpowers/specs/2026-04-25-plugin-maphistoryerror-context-design.md`
- Plan: `docs/superpowers/plans/2026-04-25-plugin-maphistoryerror-context.md`
- Bead: `holomush-095g.6` (closed)

## Test plan

- [x] `task pr-prep` — green
- [x] Sharpened `TestMapHistoryErrorTranslatesPermissionDeniedToOpaqueOopsCode` (Context assertions)
- [x] New `TestMapHistoryErrorPassesThroughInvalidArgumentWithDetails` (WithDetails round-trip)
- [x] Sharpened `TestQueryStreamHistoryTranslatesPluginPermissionDeniedToOpaqueCode` (G1 regression guard)
- [x] Existing `TestMapHistoryErrorPassesThroughInvalidArgument` (bare-status pin per spec §4.3.1)
- [x] `/review-code` verdict: READY

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Expected: a PR URL printed to stdout. Capture and report.

---

## Self-review (post-write checklist)

| Check | Result |
| ----- | ------ |
| Spec coverage: G1 (session_id/stream on PermissionDenied) | Task 2 |
| Spec coverage: G2 (WithDetails preservation) | Task 4 |
| Spec coverage: G3 (opacity invariant intact) | Tasks 2 + 3 (sharpened pinning + e2e regression guard) |
| Spec coverage: §3.1 signature change | Task 1 |
| Spec coverage: §3.2 call-site update | Task 1 |
| Spec coverage: §4.1 seven test caller updates | Task 1 |
| Spec coverage: §4.2 sharpened PermissionDenied unit test | Task 2 |
| Spec coverage: §4.3 new WithDetails unit test | Task 4 |
| Spec coverage: §4.3.1 bare-status pin (existing test) | Task 4 Step 5 verification |
| Spec coverage: §4.4 sharpened e2e pin test | Task 3 |
| Spec coverage: §6 acceptance criteria 1-4 (task lint/test/pr-prep + sig) | Task 5 Step 1 |
| Spec coverage: §6 #5 (server logs carry session_id/stream) | Closed by Task 3's `Context()` assertion + `pkg/errutil/log.go:32-42` walking `Context()` (cited in Task 3 commit message) |
| Placeholder scan | No "TBD" / "fill in" / "etc." in steps. All code blocks complete. |
| Type/signature consistency | `mapHistoryError(err error, sessionID, stream string) error` — same signature in all 4 tasks. `oopsErr.Context()` returns `map[string]any` — consistent across Task 2 and Task 3. |
| TDD ordering | Task 1: refactor (no TDD needed). Tasks 2 + 4: red-green-refactor. Task 3: regression guard, passes immediately. Task 5: verification only. |
| jj cadence | All 4 implementation tasks start with `jj new` Step 0 (per `feedback_jj_empty_wc_before_task`); `jj describe` happens at end of task without trailing `jj new`. Task 5 Step 4 uses programmatic tip detection (`jj log -r 'main@origin..@ & ~empty()'`) for bookmark placement. |
