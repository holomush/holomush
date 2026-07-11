---
phase: 03-platform-hardening-deployment-scaling
fixed_at: 2026-07-10T00:00:00Z
review_path: .planning/phases/03-platform-hardening-deployment-scaling/03-REVIEW.md
iteration: 2
findings_in_scope: 2
fixed: 2
skipped: 0
status: all_fixed
---

# Phase 03: Code Review Fix Report (iteration 2)

**Fixed at:** 2026-07-10
**Source review:** .planning/phases/03-platform-hardening-deployment-scaling/03-REVIEW.md
**Iteration:** 2

**Summary:**
- Findings in scope: 2 (fix_scope: all — both Info findings addressed)
- Fixed: 2
- Skipped: 0

Iteration 1 fixed WR-01, WR-02, IN-01 (per-probe channel/handler), and IN-02
(seed redaction) across 4 commits; the iteration-2 deep re-review confirmed all
four genuinely resolved. This iteration addresses the two remaining Info items,
both pre-existing (not introduced by the iteration-1 fixes). `task test --
./internal/eventbus/...` and `task lint` both pass (exit 0); `task fmt` produced
no changes to the touched files.

## Fixed Issues

### IN-02: bare `slog.Default().Debug` on the plugin-owned skip path

**Files modified:** `internal/eventbus/audit/projection.go`
**Commit:** 111107456
**Applied fix:** The plugin-owned-subject skip path in `projection.handle`
logged via `slog.Default().Debug(...)`, a non-context variant, even though
`p.workerCtx` is a reachable struct field (assigned in `start()` before
`Consume` registers the callback). Changed to
`slog.DebugContext(p.workerCtx, ...)` per `.claude/rules/logging.md` so
`trace_id`/`span_id` propagate. Verified `p.workerCtx` is the same lifecycle
context already used by the new DLQ log calls at `:271`/`:282`. Package tests
green.

### IN-01 (residual): scope-probe isolation comment overclaimed

**Files modified:** `internal/eventbus/scopecheck.go`
**Commit:** 6a0ee68c6
**Applied fix:** Comment-only correction (the SAFE remedy the reviewer
identified). The `probeDenied` doc comment asserted that the per-probe channel
keeps a straggler violation "in the prior probe's now-dead channel rather than
this one" — accurate only while the prior probe's handler is still installed.
The async error handler is a single connection-level slot swapped between the
sequential subscribe/publish probes on the shared long-lived connection, so the
isolation is timing-dependent, not structural. The comment now accurately
describes the residual connection-global-handler window (a second
subscribe-origin violation during the publish probe's wait window could be
mis-read as a publish denial) and its narrow reachability (asymmetric account
where SUBSCRIBE to `forbidden.>` is denied while PUBLISH is permitted, AND the
denied subscribe emits more than one violation), and records that the structural
cure is deferred.

**No behavioral change was made to this security-sensitive boot path**, by
design. The invasive structural cure (a dedicated short-lived `*nats.Conn` per
probe, or a `conn.Flush()`+settle to drain the callback path before installing
the next handler) was deliberately NOT attempted: its fail-closed correctness
can only be validated against a real-broker Docker integration test configured
with a subscribe-denied / publish-permitted account, which is unavailable in
this environment. A wrong change here could let an over-scoped account boot
(fail-open in a fail-closed self-check), so it is left as a documented
Info-level known-limitation.

## Known Limitation (deferred, documented)

**IN-01 structural cure — deferred.** The residual is a theoretical fail-open,
extremely narrow (asymmetric-account-only; requires a denied subscribe to emit
>1 violation, whereas the normal flush round-trip emits exactly one and teardown
`Unsubscribe` of an already-rejected sub typically emits none). It is a residual
of running sequential probes on one shared long-lived connection — not newly
introduced by any iteration-1 or iteration-2 fix. The structural remedy requires
the real-NATS Docker integration tier (`//go:build integration`,
subscribe-denied / publish-permitted account) to validate its fail-closed
behavior before it can be landed safely; until then it is captured in the
in-code comment and here.

---

_Fixed: 2026-07-10_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 2_
