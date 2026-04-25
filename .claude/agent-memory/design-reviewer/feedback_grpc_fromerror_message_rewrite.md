---
name: grpc FromError message rewrite on wrapped statuses
description: When a status error is wrapped (e.g. by oops), grpc's status.FromError rewrites the Message field with the outer err.Error(). Specs claiming "verbatim" pass-through must account for this.
type: feedback
---

When a HoloMUSH spec proposes round-tripping a gRPC status error via
`status.FromError(err)` followed by `st.Err()` or `status.Errorf("%s", st.Message())`,
verify whether the production call shape passes the status DIRECTLY or
WRAPPED.

**Why:** `google.golang.org/grpc/status/status.go:96-127` has two paths:

- If `err` directly implements `GRPCStatus()` (bare `status.Error`):
  returns the status's own `proto`, message untouched.
- If `err` wraps a `GRPCStatus()`-bearing inner error (any `oops.Wrap`,
  `fmt.Errorf("%w", ...)`, etc.): returns a NEW status with
  `p.Message = err.Error()` — i.e., the OUTER error's full text replaces
  the inner status's clean message.

`Status.Details()` is preserved in both paths, but `Status.Message()` is
NOT. Test fixtures that pass bare status errors will not catch this
divergence; production paths that double-wrap WILL hit it.

**How to apply:** When reviewing a spec that touches gRPC status
translation:

1. Check if the `mapXxxError`-style function's production caller wraps
   the input (look for `oops.Code(...).Wrap(...)`, `oops.With(...).Wrap(...)`).
2. If yes, `st.Message()` is NOT the plugin's original message — it's the
   outer wrapper's `Error()` output. Flag any spec language claiming
   "verbatim", "pass-through", or "wire-equivalent" for the message field.
3. `st.Details()` survives the rewrite, so designs targeting
   `WithDetails` preservation are still sound.
4. If the spec wants TRUE verbatim message pass-through, it must
   `errors.As(err, &innerStatus)` to extract the inner status BEFORE
   calling `.Message()` or `.Err()`.
