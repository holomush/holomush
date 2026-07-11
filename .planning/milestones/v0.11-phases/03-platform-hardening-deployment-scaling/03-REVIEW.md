---
phase: 03-platform-hardening-deployment-scaling
reviewed: 2026-07-10T00:00:00Z
depth: deep
files_reviewed: 46
files_reviewed_list:
  - .claude/rules/testing.md
  - cmd/holomush/cmd_audit_test.go
  - cmd/holomush/cmd_audit.go
  - cmd/holomush/core.go
  - cmd/holomush/deps_test.go
  - cmd/holomush/deps.go
  - cmd/holomush/gateway_imports_test.go
  - cmd/holomush/root.go
  - deploy/nats/cluster-config.yaml
  - deploy/nats/cluster-server.conf
  - deploy/nats/holomush-server.account.conf
  - deploy/nats/README.md
  - deploy/nats/verify-scoping.sh
  - docs/architecture/invariants.md
  - docs/architecture/invariants.yaml
  - internal/cluster/clustertest/external.go
  - internal/eventbus/audit/dlq_capture_integration_test.go
  - internal/eventbus/audit/dlq_neverdrop_integration_test.go
  - internal/eventbus/audit/dlq_replay_integration_test.go
  - internal/eventbus/audit/dlq_test.go
  - internal/eventbus/audit/dlq.go
  - internal/eventbus/audit/lag_metric.go
  - internal/eventbus/audit/projection_dlq_unit_test.go
  - internal/eventbus/audit/projection.go
  - internal/eventbus/audit/replay_test.go
  - internal/eventbus/audit/replay.go
  - internal/eventbus/audit/subsystem.go
  - internal/eventbus/config_test.go
  - internal/eventbus/config.go
  - internal/eventbus/natsdial.go
  - internal/eventbus/scopecheck_test.go
  - internal/eventbus/scopecheck.go
  - internal/eventbus/subsystem_external_test.go
  - internal/eventbus/subsystem_test.go
  - internal/eventbus/subsystem.go
  - internal/observability/server_test.go
  - internal/observability/server.go
  - internal/testsupport/natstest/nats_test.go
  - internal/testsupport/natstest/nats.go
  - scripts/smoke/cluster-smoke.bats
  - scripts/smoke/cluster-smoke.sh
  - site/src/content/docs/operating/how-to/external-nats-deployment.md
  - test/integration/cluster/cluster_test.go
  - test/integration/crypto/cache_invalidation_test.go
  - test/integration/eventbus_external/external_boot_test.go
  - test/integration/eventbus_external/scopecheck_test.go
findings:
  critical: 0
  warning: 0
  info: 0
  total: 0
status: clean
---

# Phase 03: Code Review Report (iteration 3 — closing)

**Reviewed:** 2026-07-10
**Depth:** deep
**Files Reviewed:** 46
**Status:** clean

## Summary

Closing iteration of the fix/re-review loop. Iteration 2 confirmed the four
substantive findings (WR-01/WR-02/IN-01/IN-02) resolved and left two Info
residuals. Two commits landed at HEAD `6a0ee68c6` to address them. I verified
both against the actual diffs and current source, not the fix log. Both are
correct and introduce no new defect. All prior-iteration findings are now
closed, so the phase is **clean**.

The IN-01 *structural* residual (connection-global async error handler; a
theoretical narrow fail-open only on an asymmetric subscribe-denied /
publish-permitted account, confirmable only at the real-NATS Docker integration
tier) is a KNOWN, ACCEPTED, and now in-code-documented limitation. It is not
re-raised as an open finding. See "Accepted residual" below.

Docker-gated integration tests (`//go:build integration`) cannot run in this
environment; where a claim can only be confirmed by running them I say so.

## Verification of the two closing fixes

### Fix 1 — `111107456` `projection.go` plugin-skip debug log → context variant — CORRECT

`internal/eventbus/audit/projection.go:243-248`. The diff is exactly the +2/-1
claimed: `slog.Default().Debug(` became `slog.DebugContext(\n  p.workerCtx,`.
Message ("audit projection skipping plugin-owned subject") and attrs
(`subject`, `plugin`) are byte-identical — no behavioral change beyond threading
the ctx.

- **`p.workerCtx` reachability / no nil-ctx risk:** `handle` is only ever invoked
  by JetStream after `Consume(p.handle)` registers it in `start()`
  (`:203`), and `p.workerCtx = ctx` is assigned on the immediately-preceding line
  (`:202`) — the explicit ordering guard at `:196-200` exists precisely so this
  field is populated before the callback can fire. So on the plugin-skip path
  `p.workerCtx` is guaranteed non-nil. This is the correct lifecycle ctx
  (Subsystem.Stop cancels it) and is the SAME ctx the adjacent, already-context
  DLQ log calls use (`slog.ErrorContext`/`slog.WarnContext(p.workerCtx, …)` at
  `:272`/`:283`), so the fix makes the file internally consistent.
- **Only remaining bare-slog site:** `rg 'slog\.(Default|Info|Warn|Error|Debug)\b'`
  over `projection.go` now returns zero hits — every `slog` call in the file is a
  `*Context` variant. This was the last non-context log site; the fix fully
  satisfies `.claude/rules/logging.md`.

### Fix 2 — `6a0ee68c6` `scopecheck.go` — genuinely COMMENT-ONLY — CORRECT

Verified the claim carefully. The diff is +29/-6 entirely inside the `probeDenied`
doc-comment block (`:114-152`). A mechanical filter of the diff for any
changed line that is NOT a `//` comment, a `+++/---` header, or blank returns
**NONE** — every added and removed line begins with `//`. No executable
statement, control-flow branch, channel/handler install order, timeout, or
fail-closed conclusion changed. `func probeDenied(` and everything below it is
untouched context.

The executable self-check therefore still denies an over-scoped account on every
branch, unchanged from the iteration-2-confirmed behavior:
- subscribe permitted (no violation within `scopeProbeTimeout`) → `!subDenied` →
  `overScoped(probe, "subscribe")` → boot refuses (`:85-87`).
- publish permitted → `!pubDenied` → `overScoped(probe, "publish")` → boot
  refuses (`:105-107`).
- non-permission error from `op` → propagated as `EVENTBUS_SCOPE_CHECK_FAILED`
  → boot refuses (`:172-174`).
- caller-context cancellation → surfaced as an error, never a false "denied"
  (`:183-189`).
- nil conn → `EVENTBUS_ACCOUNT_OVERSCOPED` (`:47-50`).

The comment now accurately describes the residual (below) instead of
overclaiming structural isolation — a net documentation improvement with zero
behavioral risk. `docs/architecture/invariants.yaml`/`.md` were not touched by
either commit, so the generated-not-hand-edited rule is not implicated.

## Accepted residual (not an open finding — recorded per instructions)

**IN-01 structural residual** — `internal/eventbus/scopecheck.go:128-146`. The
per-probe violation channel removes the shared-buffer drain race, but the NATS
async error handler remains a single connection-level slot swapped between the
sequential subscribe and publish probes on the shared long-lived connection.
Isolation is thus timing-dependent, not structural: a *second* subscribe-origin
permission violation dispatched during the publish probe's wait window could be
mis-read as a publish denial (a fail-open in a fail-closed boot check). Its
reachability is bounded to an asymmetric account (SUBSCRIBE to `forbidden.>`
denied while PUBLISH is permitted — the publish probe only runs after subscribe
was denied) AND a denied subscribe emitting more than one violation (normally it
emits exactly one). The structural cure (dedicated short-lived `*nats.Conn` per
probe, or drain+settle before installing the next handler) is deferred because
its fail-closed correctness can only be validated against a real broker with a
subscribe-denied/publish-permitted account at the Docker integration tier —
which cannot be exercised in this environment. This is now honestly documented
in the code comment (that was the entire point of Fix 2). Per the review
instructions it is a known/accepted/documented limitation and is NOT counted as
an open critical/warning/info finding.

---

_Reviewed: 2026-07-10_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
