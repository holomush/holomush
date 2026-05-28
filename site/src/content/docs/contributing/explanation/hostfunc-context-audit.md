---
title: "Host function context audit"
---

Host functions registered on Lua states via
`internal/plugin/hostfunc/functions.go:Register` run as Go code inside
the goroutine dispatched by `Host.invoke`. While Go code runs,
`gopher-lua`'s per-instruction context check is suspended — a host
function that blocks ignores the plugin-level CPU deadline.

This page explains the invariant that keeps that from hanging the
dispatcher, and the meta-test that locks it against regressions. For the
per-function audit, see
[Host function audit table](/contributing/reference/hostfunc-audit-table/);
to add a new host function, see
[Add a host function](/contributing/how-to/add-a-host-function/).

## Invariant

Every exported host function registered on a Lua state MUST either:

- complete in O(1) time (no loops of unbounded length, no I/O), OR
- respect `L.Context()` for any call that could block (RPC, I/O,
  channel wait).

The meta-test
`TestHostFuncsRespectContextOnPreCancelledCtx` in
`internal/plugin/hostfunc/context_audit_test.go` invokes every
registered host function with a pre-cancelled context and asserts it
returns within 250 ms.

## Meta-test scope and limitation

The meta-test invokes each hostfunc with zero arguments. Most
hostfuncs call `L.CheckString(1)` (or similar argument-validation) as
their first action, which raises a Lua error and returns immediately
before any blocking call. This means the meta-test primarily catches:

- A new hostfunc that blocks unboundedly without respecting context
  AND accepts zero arguments.
- A hostfunc whose argument validation itself is slow (> 250 ms).

It does NOT catch:

- A hostfunc that validates args first and then does a blocking call
  without respecting `L.Context()`. Under legitimate traffic (valid
  args), such a hostfunc would hang `Host.invoke`'s wait-for-drain
  bound.

Covering that gap requires per-hostfunc test fixtures (each hostfunc
has a different argument shape) wired against fake backends that
block on `ctx.Done()`. A future contributor wanting to strengthen the
guarantee should add a companion subtest that calls each
blocking-capable hostfunc (KV, world queries, world mutations,
session streams, focus) with valid-shaped arguments and a blocking
fake backend, and asserts each returns promptly on context
cancellation.

Until that companion test lands, the
[audit table](/contributing/reference/hostfunc-audit-table/) plus code
review are the authoritative enforcement of the context-respect
invariant.
