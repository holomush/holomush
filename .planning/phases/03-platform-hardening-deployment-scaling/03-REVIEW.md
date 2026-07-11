---
phase: 03-platform-hardening-deployment-scaling
reviewed: 2026-07-10T00:00:00Z
depth: standard
files_reviewed: 49
files_reviewed_list:
  - .golangci.yaml
  - CLAUDE.md
  - .claude/rules/testing.md
  - cmd/holomush/cmd_audit.go
  - cmd/holomush/cmd_audit_test.go
  - cmd/holomush/core.go
  - cmd/holomush/deps.go
  - cmd/holomush/deps_test.go
  - cmd/holomush/gateway_imports_test.go
  - cmd/holomush/root.go
  - compose.cluster.yaml
  - deploy/nats/README.md
  - deploy/nats/cluster-config.yaml
  - deploy/nats/cluster-server.conf
  - deploy/nats/holomush-server.account.conf
  - deploy/nats/verify-scoping.sh
  - docs/architecture/invariants.yaml
  - internal/cluster/clustertest/external.go
  - internal/eventbus/audit/dlq.go
  - internal/eventbus/audit/dlq_capture_integration_test.go
  - internal/eventbus/audit/dlq_neverdrop_integration_test.go
  - internal/eventbus/audit/dlq_replay_integration_test.go
  - internal/eventbus/audit/dlq_test.go
  - internal/eventbus/audit/lag_metric.go
  - internal/eventbus/audit/projection.go
  - internal/eventbus/audit/projection_dlq_unit_test.go
  - internal/eventbus/audit/replay.go
  - internal/eventbus/audit/replay_test.go
  - internal/eventbus/audit/subsystem.go
  - internal/eventbus/config.go
  - internal/eventbus/config_test.go
  - internal/eventbus/natsdial.go
  - internal/eventbus/scopecheck.go
  - internal/eventbus/scopecheck_test.go
  - internal/eventbus/subsystem.go
  - internal/eventbus/subsystem_external_test.go
  - internal/eventbus/subsystem_test.go
  - internal/observability/server.go
  - internal/observability/server_test.go
  - internal/testsupport/natstest/nats.go
  - internal/testsupport/natstest/nats_test.go
  - scripts/smoke/cluster-smoke.bats
  - scripts/smoke/cluster-smoke.sh
  - site/src/content/docs/operating/how-to/external-nats-deployment.md
  - test/integration/cluster/cluster_test.go
  - test/integration/crypto/cache_invalidation_test.go
  - test/integration/eventbus_external/external_boot_test.go
  - test/integration/eventbus_external/scopecheck_test.go
findings:
  critical: 0
  warning: 6
  info: 4
  total: 10
status: fixed
---

# Phase 3: Code Review Report

**Reviewed:** 2026-07-10
**Depth:** standard
**Files Reviewed:** 49
**Status:** fixed — all 6 warnings + 4 info resolved 2026-07-10 (see Fix Log)

## Summary

Phase 3 (platform hardening: external NATS mode, single-principal subject scoping,
audit DLQ + replay, multi-node crypto-invalidation verification, operator runbook)
is generally well-engineered. Error handling is fail-closed, resource ownership is
disciplined (nats conns/pools/goroutines are consistently closed and rollback paths
unwind cleanly), the shell scripts quote defensively and decide pass/fail by exit
code, the NATS account templates grant exactly the three game-topic prefixes plus
`_INBOX`/`$JS.API` (no over-grant), and the test suites carry real assertions with
no Skip-only stubs. The crypto-invariant surface was intentionally out of scope
(already adjudicated READY by crypto-reviewer).

No BLOCKER-class defects were found (no injection, no data-loss on the normal path,
no crash). The findings are one operability gap (the new DLQ counter is never wired
onto the scraped Prometheus registry — the same class of bug the phase's own
`core.go` change fixed for cluster metrics), a credential-leak risk in the external
dial error, a destructive `rm -rf` in the smoke teardown, and two soundness gaps in
the security-verification paths (a scoping self-check that can be fooled by queued
violations, and a runbook script that reports PASS on a connection failure).

## Warnings

### WR-01: New audit DLQ metric (and lag/skip metrics) is never registered on the scraped registry

**File:** `internal/eventbus/audit/lag_metric.go:58` (and its non-call in `cmd/holomush/`)
**Issue:** This phase adds `DLQMessagesTotal` to `audit.RegisterMetrics`, and its
doc comment states operators "alert on this counter long before bounded DLQ
retention (D-12) would age anything out." But `audit.RegisterMetrics` is called
**only from `internal/eventbus/audit/subsystem_test.go`** — never from production
wiring in `cmd/holomush`. So `holomush_audit_dlq_messages_total` (and the
pre-existing `projection_lag_seconds` / `projection_plugin_owned_skipped_total`)
increment in-process but are never exposed on `/metrics`. This is the exact
"silently unscraped" defect the phase deliberately fixed in `core.go:508-517` for
`cluster_*` and `invalidation_*` metrics (routing them to `obsServer.Registerer()`),
but the audit metrics were left unwired — so the DLQ alerting story (D-11) does not
actually function.
**Fix:** Register the audit collectors on the served registry during boot, mirroring
the cluster-metrics change:
```go
// in runCoreWithDeps, after metricsReg is resolved
audit.RegisterMetrics(metricsReg)
```
Confirm `holomush_audit_dlq_messages_total` appears on a live `/metrics` scrape.

### WR-02: External dial error leaks URL-embedded credentials into the oops context

**File:** `internal/eventbus/natsdial.go:56-58`
**Issue:** `dialExternal` wraps a connect failure with `With("url", cfg.URL)`. In the
shipped compose cluster overlay `cfg.URL` is `nats://holomush-server:holomush-server-smoke@nats:4222`
(`deploy/nats/cluster-config.yaml:26`), and the docs explicitly allow `user:pass` in
the URL for dev clusters (`config.go:68`). On any boot-time dial failure this password
is placed in the structured error and propagated up through `EVENTBUS_EXTERNAL_CONNECT_FAILED`
→ `AUDIT_DLQ_NATS_DIAL_FAILED` (cmd_audit.go:131) → stderr / `errutil.LogErrorContext`,
writing the secret to logs. Same leak surface as the grpc-errors rule ("never leak
inner errors past trust boundaries").
**Fix:** Redact userinfo before attaching. Parse with `net/url` and strip
`u.User`, or attach only host:port:
```go
safe := cfg.URL
if u, perr := url.Parse(cfg.URL); perr == nil && u.User != nil {
    u.User = nil
    safe = u.Redacted() // or u.String()
}
return nil, oops.Code("EVENTBUS_EXTERNAL_CONNECT_FAILED").With("url", safe).Wrap(err)
```

### WR-03: cluster-smoke teardown `rm -rf`s a caller-supplied SMOKE_DATA_DIR despite the comment claiming it never does

**File:** `scripts/smoke/cluster-smoke.sh:128-136` (with `:169`)
**Issue:** `teardown()` runs `rm -rf "${SMOKE_DATA_DIR}"` and the inline comment
asserts it removes "never a caller-supplied SMOKE_DATA_DIR we did not mint." But
line 169 reads `SMOKE_DATA_DIR="${SMOKE_DATA_DIR:-$(mktemp -d ...)}"` — an operator
who exports `SMOKE_DATA_DIR=/some/real/dir` has that directory adopted verbatim and
then recursively deleted on exit. The documented safety contract is not enforced by
the code, creating a data-loss trap.
**Fix:** Only delete a directory the script actually minted. Track provenance:
```go
if [ -z "${SMOKE_DATA_DIR:-}" ]; then
  SMOKE_DATA_DIR="$(mktemp -d "${TMPDIR:-/tmp}/holomush-cluster-smoke.XXXXXX")"
  MINTED_DATA_DIR=1
fi
# teardown:
[ "${MINTED_DATA_DIR:-0}" = 1 ] && [ -d "${SMOKE_DATA_DIR}" ] && rm -rf "${SMOKE_DATA_DIR}"
```

### WR-04: verify-scoping.sh reports PASSED when the verify credential cannot connect at all

**File:** `deploy/nats/verify-scoping.sh:90-118`
**Issue:** Both `assert_publish_denied` and `assert_subscribe_denied` treat *any*
non-zero (and non-124) `nats` exit as "denied → ok". A wrong password, unreachable
broker, or TLS failure makes `nats pub`/`nats sub` exit non-zero for reasons
unrelated to subject scoping, so all six probes "pass" and the script prints
`PASSED` — a vacuous proof. A security-verification tool must distinguish
"denied by scoping" from "never connected."
**Fix:** Add a connectivity precondition before the probes — e.g. a request/reply or
publish on the credential's own permitted `_INBOX.>`/allowed subject that MUST
succeed, and abort with a distinct exit code if it fails:
```bash
if ! nats_rc pub "_INBOX.scopecheck.$$.connectivity" "ping"; then
  echo "verify-scoping: credential could not connect/authenticate; cannot prove scoping" >&2
  exit 4
fi
```

### WR-05: scope self-check drains only one stale violation; a queued subscribe-violation can pre-satisfy the publish probe

**File:** `internal/eventbus/scopecheck.go:124-159`
**Issue:** `probeDenied` drains "any stale violation" with a single non-blocking
`select` receive (lines 132-135). The async error handler feeds an 8-deep buffered
channel shared across both probes. If the subscribe probe generates more than one
`ErrPermissionViolation` (flush + teardown), a leftover violation remains queued;
the subsequent publish probe drains only one, then — even if the publish were
actually *permitted* (a publish-only over-scope) — immediately reads the leftover
violation and concludes `pubDenied = true`, missing the over-scope. This weakens the
fail-closed guarantee of a security self-check.
**Fix:** Drain the channel fully before each probe:
```go
for {
    select {
    case <-violations:
    default:
        goto drained
    }
}
drained:
```
(or use a fresh per-probe channel / handler).

### WR-06: replay silently returns an un-stripped subject on prefix mismatch, corrupting the restored subject column

**File:** `internal/eventbus/audit/replay.go:208-216` (with `cmd/holomush/cmd_audit.go:325-331`)
**Issue:** `originalSubject` returns the DLQ subject **unchanged** when the configured
prefix doesn't match. The replay CLI derives the prefix from the invocation's
`event_bus.game_id` (`dlqConfigForGame`), while capture wrote it from the running
core's game id. If an operator replays with a config whose `game_id` differs from (or
omits, falling back to `main`) the capture-time game id, every recovered
`events_audit.subject` is stored with the `internal.<game>.audit.dlq.` prefix still
prepended — silent data corruption of the restored subject column, masked as success.
**Fix:** Fail loud on prefix mismatch instead of silently passing through, e.g. count
it as `Failed` with a coded reason, or validate that every scanned DLQ subject carries
the expected prefix before replay begins.

## Info

### IN-01: VerifyAccountScoping installs an error handler on the shared long-lived connection and never restores it

**File:** `internal/eventbus/scopecheck.go:62-69`
**Issue:** The scope check calls `conn.SetErrorHandler` on `eventBusSub.Conn()` — the
long-lived connection shared by the event bus, audit projection, cluster, and
invalidation coordinator — and never restores the prior handler. After boot the
connection's async error callback is an orphaned closure forwarding permission
violations to a channel no one reads (which fills and then drops). Benign today
(the connection had no handler), but a latent trap if any component later relies on
connection-level async error surfacing.
**Fix:** Save and restore the previous handler around the check, or run the probe on
a dedicated short-lived connection.

### IN-02: Near-tautological / low-value tests

**File:** `internal/eventbus/audit/replay_test.go:76-84`, `internal/eventbus/audit/dlq_test.go:87-96`
**Issue:** `TestReplayResultZeroValueIsEmpty` asserts a zero-value struct equals its
zero value (no behavior under test). `TestDLQEnsureStreamIsIdempotent` only asserts
the fake was invoked twice — it does not exercise real idempotency (the fake always
succeeds), so the test name overstates what is proven.
**Fix:** Drop the zero-value test; reword/strengthen the idempotency test (or note it
verifies delegation, not idempotency).

### IN-03: Header-name constant drift between show and projection

**File:** `cmd/holomush/cmd_audit.go:246`
**Issue:** `runAuditDLQShow` matches the literal `"Nats-Msg-Id"` while the projection
uses the `headerMsgID` constant (`projection.go:24`). Same value today, but the
duplicated literal can drift if the header is ever renamed.
**Fix:** Export and reuse the `headerMsgID` constant from the audit package.

### IN-04: Fetch loops assume a non-nil batch on a timeout error

**File:** `internal/eventbus/audit/replay.go:135-147`, `cmd/holomush/cmd_audit.go:238-251`
**Issue:** Both loops proceed to `for msg := range batch.Messages()` when `fetchErr`
is a `nats.ErrTimeout`, relying on `batch` being non-nil in that case. This matches
the jetstream client's current behavior, but a nil batch alongside a timeout error
would panic. Consider a defensive `if batch == nil { break }` guard.
**Fix:** Add a nil-batch guard before iterating, or assert the invariant with a comment.

---

## Fix Log — all 10 findings resolved 2026-07-10

| Finding | Fix | Commit |
|---------|-----|--------|
| WR-01 | `audit.RegisterMetrics(metricsReg)` wired into `core.go` boot (served registry) — DLQ counter now on `/metrics` | `20ca18bc0` |
| WR-02 | `redactURL()` strips URL userinfo before attaching to the dial error — no credential leak | `f141b478d` |
| WR-03 | `MINTED_DATA_DIR` guard — teardown only `rm`s a script-minted dir, never caller-supplied | `df4cb5dd7` |
| WR-04 | connectivity precondition (exit 4) distinguishes "denied by scoping" from "never connected" | `3329974cc` |
| WR-05 | `probeDenied` fully drains the violations channel before each probe | `91f2b0bad` |
| WR-06 | `originalSubject` returns `(string, bool)`; prefix mismatch → `Failed`, never persists a corrupted subject (crypto-neutral) | `0ca71b310` |
| IN-01 | save/restore the prior NATS error handler around the scope check | `4427cdb35` |
| IN-02 | dropped tautological zero-value test; reworded the delegation test | `0ca71b310`, `7d71a22b9` |
| IN-03 | exported `audit.HeaderMsgID`; CLI reuses it instead of a duplicated literal | `7d71a22b9` |
| IN-04 | nil-batch guard before iterating fetch results | `0ca71b310`, `7d71a22b9` |

Verified: `task test` green (10212), `task lint` exit 0, `task test:int` green (audit + external-scope surfaces).

---

_Reviewed: 2026-07-10_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
