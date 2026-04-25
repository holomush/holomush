# Plugin-path `mapHistoryError`: preserve context + status details

| Field        | Value                                                                                  |
| ------------ | -------------------------------------------------------------------------------------- |
| Status       | DRAFT                                                                                  |
| Date         | 2026-04-25                                                                             |
| Bead         | `holomush-095g.6` (parent: `holomush-095g`, PR #267)                                   |
| Parent spec  | [`2026-04-23-plugin-history-authz-design.md`](2026-04-23-plugin-history-authz-design.md) §5.5 |
| Scope        | Code-quality polish to `internal/grpc/query_stream_history.go::mapHistoryError`        |

## 1. Context

PR #267 landed defense-in-depth scene-membership authz at
`PluginAuditService.QueryHistory` and translated plugin-emitted gRPC
`PermissionDenied` into the same opaque oops code (`STREAM_ACCESS_DENIED`)
that the outer I-17 gate uses. Wire-level opacity is correct.

Two server-side observability gaps remain in
`mapHistoryError` (`internal/grpc/query_stream_history.go:270-303`):

1. **Lost log context on `PermissionDenied`.** The outer I-17 gate at
   `internal/grpc/query_stream_history.go:170-173` emits:

   ```go
   oops.Code("STREAM_ACCESS_DENIED").
       With("session_id", req.SessionId).
       With("stream", req.Stream).
       Errorf("not authorized to read stream")
   ```

   The plugin-path translation at lines 281-282 emits the same code and
   message **without** the `With(...)` context. When `errutil.LogError`
   fires on either path, the I-17 path logs `session_id` and `stream`
   attributes; the plugin path does not. Operator triage suffers.

2. **Lost `status.WithDetails` proto messages on `InvalidArgument`.** Line 288
   round-trips the plugin's status via `status.Errorf("%s", st.Message())`,
   which strips any proto detail messages the plugin attached via
   `status.WithDetails`. Moot today (the plugin emits bare `status.Error`),
   but a footgun for future plugin error enrichment.

## 2. Goals & non-goals

### Goals

- **G1.** Preserve `session_id` and `stream` on the top-level oops chain on
  the plugin-path `PermissionDenied` translation, achieving log parity with
  the outer I-17 gate.
- **G2.** Preserve any `status.WithDetails` proto messages on the
  `InvalidArgument` pass-through.
- **G3.** Keep the wire-level opacity invariant intact: the top-level oops
  code returned to the client MUST remain `STREAM_ACCESS_DENIED`, with the
  same message used by the outer I-17 gate. The pinning test
  `TestQueryStreamHistoryTranslatesPluginPermissionDeniedToOpaqueCode`
  (`internal/grpc/query_stream_history_test.go:868`) MUST still pass
  unchanged in its existing assertions.

### Non-goals

- Touching the I-17 gate, ABAC path, or any other authorization wall.
- Adding `character_id` to the oops chain at either site (the I-17 site
  carries it only via `slog`, not the oops chain — match the existing
  contract).
- Threading `errutil.LogError` semantics or changing how/where the handler
  logs errors.
- Touching `PluginHistoryRouter` or any plugin code.

## 3. Design

### 3.1 Signature change

`mapHistoryError` gains two parameters and a small body change:

```go
// mapHistoryError translates eventbus cursor errors and plugin-emitted
// gRPC status errors into the host's wire-level error vocabulary.
//
// sessionID and stream MUST be the request-scoped values from the
// QueryStreamHistoryRequest. They are attached to the oops chain on the
// PermissionDenied translation so server logs match the outer I-17 gate.
func mapHistoryError(err error, sessionID, stream string) error {
    if st, ok := status.FromError(err); ok {
        switch st.Code() {
        case codes.PermissionDenied:
            // Opaque: collapse plugin-boundary denial into the same oops
            // code the outer I-17 gate uses. Client cannot distinguish
            // "outer wall caught" from "plugin wall caught."
            return oops.Code("STREAM_ACCESS_DENIED").
                With("session_id", sessionID).
                With("stream", stream).
                Errorf("not authorized to read stream")
        case codes.InvalidArgument:
            // Preserves the plugin's gRPC code and any status.WithDetails
            // proto messages. NOTE: when err is a wrapped status (the
            // production shape — `oops.Code("INTERNAL").Wrap(...)` at the
            // call site), grpc's status.FromError rewrites
            // st.Message() to err.Error(), which includes the outer oops
            // chain's text. That message-rewriting is unchanged from
            // PR #267 — see §5 risk #2. This branch's contribution is
            // strictly Details preservation; message purity is a separate
            // concern.
            return st.Err()
        }
    }
    switch {
    case errors.Is(err, eventbus.ErrCursorInvalid):
        return status.Errorf(codes.InvalidArgument, "%v", err)
    case errors.Is(err, eventbus.ErrCursorStale):
        return status.Errorf(codes.FailedPrecondition, "%v", err)
    case errors.Is(err, eventbus.ErrCursorLag):
        return status.Errorf(codes.Unavailable, "%v", err)
    default:
        return err
    }
}
```

### 3.2 Call-site update

Single production caller at `internal/grpc/query_stream_history.go:233-235`:

```go
if fetchErr != nil {
    return nil, mapHistoryError(
        oops.Code("INTERNAL").With("stream", req.Stream).Wrap(fetchErr),
        req.SessionId,
        req.Stream,
    )
}
```

`req.SessionId` and `req.Stream` are already in lexical scope at the call
site. The outer `oops.Code("INTERNAL").With(...)` wrap is preserved
(unchanged) so the cursor-error legacy paths that format with `"%v"` still
include the `stream` context as before.

### 3.3 Why approach (a) was chosen

Two approaches were considered (per spec §5.5 design review and bead
description):

- **(a) Signature change** — chosen.
- **(b) Call-site re-wrap with detection** — rejected. `oops.AsOops(err)`
  returns the outermost oops node, so adding context at the call site via
  `oops.With(...).Wrap(mapped)` would shift the top-level code (the
  outermost node would be context-only, no `Code`), breaking the pinning
  test. Avoiding that requires re-creating the full
  `oops.Code("STREAM_ACCESS_DENIED")...Errorf(...)` chain at the call site,
  duplicating the message and code at two sites and creating drift risk.

`mapHistoryError` is package-private with exactly one production caller, so
the signature change has zero blast radius outside the test file.

## 4. Testing

All tests live in `internal/grpc/query_stream_history_test.go`.

### 4.1 Updates to existing tests

The seven existing tests that call `mapHistoryError` directly (lines 705,
716, 727, 738, 751, 766, 780 — verified via `rg -n "mapHistoryError\("
internal/grpc/query_stream_history_test.go`) gain the two new arguments.
Pass deterministic test fixtures (e.g., `"test-session"`,
`"location:test"`); they are immaterial to the existing assertions.

### 4.2 Sharpened `PermissionDenied` test

`TestMapHistoryErrorTranslatesPermissionDeniedToOpaqueOopsCode` (line 747):

- Continue asserting top-level oops `Code() == "STREAM_ACCESS_DENIED"`.
- **Add:** assert `oops.AsOops(got).Context()` contains
  `session_id == sessionIDFixture` and `stream == streamFixture`.

### 4.3 New `InvalidArgument` test for `WithDetails` round-trip

Add `TestMapHistoryErrorPassesThroughInvalidArgumentWithDetails`:

- Construct a plugin-side error using
  `status.New(codes.InvalidArgument, "subject malformed").WithDetails(detail)`
  where `detail` is an `*errdetails.BadRequest` from
  `google.golang.org/genproto/googleapis/rpc/errdetails`. That module is
  already a transitive dep (`go.mod:155`); this adds the test-file import.
  `errdetails.BadRequest` is the canonical detail proto for
  `codes.InvalidArgument` and avoids drift if downstream callers ever
  inspect details by type.
- Call `mapHistoryError(pluginErr, "test-session", "location:test")`.
- Assert `status.Code(got) == codes.InvalidArgument`.
- Assert `status.Convert(got).Details()` has length 1 and the element is
  an `*errdetails.BadRequest` equal to `detail` (proto.Equal).

### 4.3.1 Bare-status `InvalidArgument` regression pin

The existing `TestMapHistoryErrorPassesThroughInvalidArgument` (line 762)
already pins the bare-status case: input
`status.Error(codes.InvalidArgument, "subject malformed")`, assertion
`status.Code(got) == codes.InvalidArgument`. After the §4.1 signature
update it continues to assert that bare-status inputs round-trip
unchanged. No new test needed; this section is the explicit declaration
that the test serves that role under the new branch implementation.

### 4.4 Sharpened e2e pin test

`TestQueryStreamHistoryTranslatesPluginPermissionDeniedToOpaqueCode` (line
868) gains a single additional assertion:

- After the existing top-level code assertion, assert
  `oopsErr.Context()["session_id"]` equals `"s1"` and
  `oopsErr.Context()["stream"]` equals the local `stream` variable
  bound at `query_stream_history_test.go:871` (the first return value of
  `sceneFocusMembership(t)`, a tuple of `(string, session.FocusMembership)`).

This sharpens the regression guard without changing test setup.

### 4.5 Test coverage targets

- Per-package coverage for `internal/grpc` MUST remain above 80%.
- `mapHistoryError` already has near-100% line coverage; this work
  preserves it.

## 5. Risks

- **Pinning-test regression risk: low.** The top-level oops creation
  remains in `mapHistoryError`. Adding `.With(...)` between `.Code(...)` and
  `.Errorf(...)` does not move the outermost node; `AsOops(err).Code()`
  continues to return `STREAM_ACCESS_DENIED`.
- **`st.Err()` semantic drift on `InvalidArgument`: very low.** Today the
  plugin emits bare `status.Error(codes.InvalidArgument, "...")`. For that
  bare-status case `st.Err()` and `status.Errorf(codes.InvalidArgument,
  "%s", st.Message())` produce wire-equivalent results, since
  `status.FromError` on a bare status takes the direct-type-assertion
  path and `st.Message()` returns the original string. The change
  becomes meaningful only when a future plugin attaches details via
  `status.WithDetails` — the intended behavior per G2.
- **Wrapped-status message rewrite: pre-existing, unchanged.** When err is
  the production shape — `oops.Code("INTERNAL").Wrap(status.Error(...))`
  per the call site at `internal/grpc/query_stream_history.go:233-235` —
  grpc's `status.FromError` reaches the wrapped status via `errors.As`
  and **rewrites** the resulting `Status.Message` to `err.Error()`
  (the outer oops chain's stringification, which includes the
  `stream=...` context). This is grpc's documented behavior for wrapped
  statuses and is unchanged from PR #267's `status.Errorf("%s",
  st.Message())` line. This spec's contribution is strictly Details
  preservation, not message purity. A separate (out-of-scope) follow-up
  could unwrap the inner status before calling `.Err()` if message
  fidelity becomes a requirement.
- **No host-context bleed beyond the existing baseline.** `st.Err()` does
  not introduce *new* host-context leakage relative to the line 288
  PR #267 implementation; it preserves Details without changing the
  Message-rewrite behavior described above.

## 6. Acceptance criteria

A reviewer can verify the work is complete by running:

1. `task lint` — passes.
2. `task test` — passes, including the four updated `TestMapHistoryError…`
   tests, the sharpened `TestQueryStreamHistoryTranslatesPluginPermissionDeniedToOpaqueCode`,
   and the new `TestMapHistoryErrorPassesThroughInvalidArgumentWithDetails`.
3. `task pr-prep` — green (mirrors all CI; required before push per
   project rules).
4. `mapHistoryError` signature is `func mapHistoryError(err error, sessionID, stream string) error`.
5. Server logs on a plugin-path `PermissionDenied` denial carry
   `session_id` and `stream` attributes (manually verified via the e2e
   integration test, which exercises the full path).

## 7. Out-of-scope follow-ups

None. The bead is the terminal work for this observability gap.
