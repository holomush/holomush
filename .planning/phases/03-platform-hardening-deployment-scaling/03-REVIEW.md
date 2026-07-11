---
phase: 03-platform-hardening-deployment-scaling
reviewed: 2026-07-10T00:00:00Z
depth: deep
files_reviewed: 49
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
  warning: 2
  info: 2
  total: 4
status: issues
---

# Phase 3: Code Review Report (DEEP re-review)

**Reviewed:** 2026-07-10
**Depth:** deep (cross-file: eventbus/audit/cluster/crypto boundaries, external-NATS dial + fail-closed boot, single-principal scoping, multi-node crypto invalidation, audit DLQ capture/replay)
**Files Reviewed:** 49
**Status:** issues_found

## Summary

This is a deep re-review at HEAD `46b21e171`, after all 10 findings of the prior
standard-depth review were fixed. I re-verified the fixed state (WR-01..06,
IN-01..04 are all genuinely resolved in the current code — the redaction helper,
minted-dir teardown guard, connectivity precondition, full-drain, prefix-mismatch
fail-loud, header-constant dedup, and nil-batch guards are all present and
correct) and then traced the call chains the standard pass could not: the
`projection.handle → dlq.Capture` Term/Nak decision under real NATS MaxDeliver
semantics, the `ReplayDLQ` original-subject recovery vs the capture-time subject,
the `VerifyAccountScoping` shared-connection async handler, the boot metric
registry wiring, and every `// Verifies:` binding minted in `invariants.yaml`.

Overall the phase is well-engineered: fail-closed boot is correct
(`external_boot_test` proves conn/js stay nil on unreachable/mismatch), the
scoping self-check both fails-closed on default-open and passes the shipped scoped
account (`scopecheck_test` Case A/B), the multi-node invalidation bindings
(INV-CLUSTER-1/2/9) genuinely assert N-of-N ack collection over independent
per-replica connections, the boot metric-registry fix now routes audit + cluster
+ invalidation collectors onto the served registry, and the operator CLIs decide
success by exit code.

No BLOCKER-class defect was found (no injection, no normal-path data loss, no
crash, no credential leak on the primary single-URL path). The two Warnings both
concern the audit DLQ never-drop guarantee (D-09): a real gap at the exact
`MaxDeliver` boundary when DLQ capture fails, and an invariant binding
(INV-EVENTBUS-30) that asserts a clause ("redelivery continues") which no bound
test proves and which is false under standard JetStream semantics.

## Warnings

### WR-01: DLQ never-drop has a gap at the MaxDeliver boundary — a Nak on the final delivery attempt does not redeliver

**File:** `internal/eventbus/audit/projection.go:261-282`
**Issue:** The final-attempt capture branch fires exactly when
`meta.NumDelivered >= uint64(p.cfg.MaxDeliver)` — i.e. on the *last* permitted
delivery. If `p.dlq.Capture(...)` fails there, the code `msg.Nak()`s with the
comment "so redelivery continues — nothing is ever silently dropped." Under
standard JetStream semantics MaxDeliver is a hard ceiling: once a message has been
delivered `MaxDeliver` times, the server will **not** redeliver it even on an
explicit `-NAK` — it emits a `MAX_DELIVERIES` advisory and stops. So on the final
attempt a failed capture yields **no further delivery and no further capture
attempt**. The poison message is then neither persisted, nor in the DLQ, nor
redeliverable; it survives only in the source EVENTS stream (LimitsPolicy) until
`StreamMaxAge` (default 30 days), after which it ages out permanently. The
`dlq_neverdrop_integration_test` (MaxDeliver:2) is consistent with this — its
`failing.Calls()` reaches exactly 1 and the test never asserts a second delivery,
because none occurs.

This is not normal-path loss (the DLQ's independent failure domain makes capture
reliable when Postgres — not the DLQ — is the outage), and it is ERROR-logged, so
it is a robustness/guarantee-accuracy defect rather than a blocker. But the code's
own claim ("redelivery continues") is false, so the D-09 never-drop guarantee is
weaker than documented: a sustained DLQ-domain outage coinciding with a poison
message's final attempt drops the audit record after MaxAge with no automatic
retry.
**Fix:** Make the fallback actually preserve the message rather than rely on a
non-existent redelivery. Options: (a) do NOT Term on the success side until the
capture is durable AND move the capture attempt one delivery *earlier*
(`NumDelivered >= MaxDeliver-1`) so a failed capture still has one real redelivery
left; or (b) on capture failure, leave the message un-acked (no Nak/Term) so it
stays owned by the consumer and re-attempt capture on a subsequent boot / manual
re-consume, and correct the comment to state the guarantee is "retained in EVENTS
until MaxAge; capture is not auto-retried past MaxDeliver." At minimum, fix the
comment so it does not assert redelivery that JetStream will not perform.

### WR-02: INV-EVENTBUS-30 is a partial/misleading binding — the "redelivery continues" clause is asserted by no bound test and is false

**File:** `docs/architecture/invariants.yaml` (INV-EVENTBUS-30) with
`internal/eventbus/audit/dlq_neverdrop_integration_test.go:64` and
`internal/eventbus/audit/dlq_capture_integration_test.go:67`
**Issue:** The invariant summary reads: *"a failed DLQ publish Naks instead, so
redelivery continues and nothing is silently dropped."* Its two `// Verifies:`
tests prove only: (1) capture-success → message in DLQ + counter increments + not
persisted (`dlq_capture`), and (2) capture-failure → DLQ empty + not persisted +
Nak attempted at least once (`dlq_neverdrop`). Neither test asserts that
**redelivery continues** after the Nak — and per WR-01 it does not, because the
Nak lands at `NumDelivered == MaxDeliver`. Per `.claude/rules/invariants.md`,
`TestBoundInvariantsAreGenuinelyAsserted` cannot detect a partial binding (a test
proving only one clause of a multi-clause invariant), which "needs human review"
— this is exactly that case, and the un-proven clause is not merely unproven but
counterfactual.
**Fix:** Either (after WR-01 is fixed to make redelivery/retention real) add an
assertion that the message is re-delivered / recoverable after a failed capture,
or reword the invariant summary to state the guarantee the code actually provides
(e.g. "a failed DLQ publish leaves the message un-acked and retained in the source
stream; it is never Term'd without a durable capture"). Do not leave the summary
claiming redelivery the bound tests never exercise.

## Info

### IN-01: scope self-check shares one violation channel across probes — the WR-05 full-drain does not close the in-flight window (fail-open risk in a fail-closed check)

**File:** `internal/eventbus/scopecheck.go:127-169` (with `:64-72`)
**Issue:** `probeDenied` now fully drains the shared `violations` channel before
each probe (the WR-05 fix). That closes the *enqueued*-stale-violation window but
not the *in-flight* one: a permission violation generated by the subscribe probe
(flush/teardown) is dispatched on nats.go's async callback goroutine and may be
enqueued *after* the publish probe's drain completes but *during* its 3s wait. If
the account were actually publish-over-scoped (publish permitted → no publish
violation of its own), that late subscribe-violation would be read as the publish
probe's result and yield `pubDenied = true` — a false "denied" that lets an
over-scoped account boot, defeating the fail-closed intent. Probability is low
(this runs once at boot before any other subsystem uses the shared connection, so
the only violation source is the immediately-prior subscribe probe), but the
structural cure is a fresh per-probe channel + handler (or a dedicated short-lived
probe connection) rather than a shared 8-deep buffer plus a drain.
**Fix:** Give each probe its own `violations` channel and error handler installed
for the duration of that single probe, so a subscribe-probe violation can never be
mis-attributed to the publish probe.

### IN-02: redactURL only strips userinfo from the FIRST URL of a comma-separated NATS seed list

**File:** `internal/eventbus/natsdial.go:70-79`
**Issue:** `nats.Connect` accepts a comma-separated seed list in one string
(`nats://a:pw@h1:4222,nats://b:pw2@h2:4222`). `url.Parse` treats the whole string
as a single URL whose `User` is only the first credential; setting `u.User = nil`
+ `u.Redacted()` strips `a:pw` but leaves `pw2` embedded in the parsed host/path,
so a multi-URL with credentials in the 2nd+ seed still leaks a password into
`EVENTBUS_EXTERNAL_CONNECT_FAILED` (the exact leak WR-02's single-URL fix closed).
Dev-cluster multi-URL-with-embedded-creds is unusual, so this is Info, but the
redaction is incomplete for that shape.
**Fix:** Split `raw` on `,` and redact each element (or reject/redact wholesale
when more than one credential-bearing seed is present) before attaching to the
error.

---

## Verification of prior-round fixes (re-checked at HEAD)

All 10 prior findings remain correctly fixed; spot-checks:
- WR-01 (audit metrics unscraped): `audit.RegisterMetrics(metricsReg)` present at `core.go:525`, routed to `obsServer.Registerer()`.
- WR-02 (credential leak): `redactURL` at `natsdial.go:70` strips userinfo on the single-URL path (see IN-02 for the multi-URL residual).
- WR-04 (vacuous PASS): connectivity precondition (`exit 4`) at `verify-scoping.sh:127-132`, sound against the shipped `HOLOMUSH_VERIFY` `_INBOX.>` grant.
- WR-05 (single-drain): `probeDenied` now loops to fully drain (`scopecheck.go:138-145`) — see IN-01 for the residual in-flight window.
- WR-06 (silent subject corruption): `originalSubject` returns `(string, bool)`; prefix mismatch → `result.Failed++`, never persisted (`replay.go:193-211`, `235-243`).
- IN-01 (orphaned handler): prior handler saved/restored (`scopecheck.go:62-63`).
- IN-03 (header constant): `audit.HeaderMsgID` exported and reused by the CLI (`cmd_audit.go:252`).
- IN-04 (nil batch): guards present at `replay.go:144` and `cmd_audit.go:245`.

Cross-file checks that came back clean:
- No new bare-`slog.*` logging-rule violations in changed hunks (all new call sites in `projection.go`/`replay.go`/`scopecheck.go`/`core.go` use `*Context` variants with a reachable ctx).
- Boot metric-registry fix is consistent: audit, cluster, and invalidation collectors all land on `metricsReg` (served registry).
- DLQ subject game-id is internally consistent: capture (`core.go:563`) and replay (`cmd_audit.go:331`) both derive from `eventBusConfig.GameID`, and any divergence now fails loud (WR-06).
- INV-CLUSTER-1/2/9 and INV-EVENTBUS-29 bindings genuinely assert their guarantees (N-of-N acks / fail-closed boot). Only INV-EVENTBUS-30 is a partial binding (WR-02).

---

_Reviewed: 2026-07-10_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
