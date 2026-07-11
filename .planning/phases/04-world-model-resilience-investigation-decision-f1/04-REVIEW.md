---
phase: 04-world-model-resilience-investigation-decision-f1
reviewed: 2026-07-11T00:00:00Z
depth: deep
files_reviewed: 11
files_reviewed_list:
  - internal/testsupport/integrationtest/harness.go
  - internal/testsupport/integrationtest/options.go
  - test/integration/resilience/resilience_suite_test.go
  - test/integration/resilience/chaos_helpers_test.go
  - test/integration/resilience/boot_smoke_test.go
  - test/integration/resilience/m12_lastwritewins_test.go
  - test/integration/resilience/m2_dualwrite_test.go
  - test/integration/resilience/restart_reconnect_test.go
  - docs/adr/holomush-i4784-world-state-model-decision.md
  - docs/adr/README.md
  - docs/reviews/arch-review/2026-07-11/verification/f1-resilience-verdict.md
findings:
  critical: 0
  warning: 4
  info: 6
  total: 10
status: issues_found
---

# Phase 4: Code Review Report

**Reviewed:** 2026-07-11
**Depth:** deep
**Files Reviewed:** 11
**Status:** issues_found

## Summary

Deep review of the two-replica resilience harness seams (`WithExternalNATS` / `WithSharedDatabase`), the four resilience spec files, the MODEL-01 ADR, and the OPS-05 evidence doc. Cross-file verification performed against: `internal/world/service.go` (MoveCharacter commit-then-emit shape, `move_succeeded=true` at the `CHARACTER_MOVE_EVENT_FAILED` wrap), `internal/world/events.go` (`EVENT_EMITTER_MISSING` / `EVENT_EMIT_FAILED`), `internal/world/postgres/location_repo.go` (unguarded full-row `UPDATE`, no version predicate), `internal/world/setup/subsystem.go` (production `world.Service` constructed with no `EventEmitter`), `internal/property/entity_mutator.go` (Get → mutate one field → full-row Update), `samber/oops@v1.22.0` (`Code()` = deepest non-nil code, `error.go:233-250`), `sethvargo/go-retry@v0.3.0` (`DoValue` returns bare `ctx.Err()` when the ctx expires during the backoff wait), `internal/eventbus/publisher.go:322-329` (publisher stamps its own `EVENTBUS_PUBLISH_EXPIRED`/`EVENTBUS_PUBLISH_FAILED` codes), `eventbustest.Embedded` field types, `natstest.NATSEnv` API, the `quarantinetest` gate, and the quarantine bijection meta-test regex.

The core evidence chain the ADR leans on holds: M12's deterministic-interleave spec genuinely asserts the lost update against the unguarded full-row `UPDATE`; M2's three specs assert exactly the commit-survives + `move_succeeded=true` + delivery-decoupled facts the verdict doc quotes; the harness resolves options before bus/DB construction and validates option combinations; no test-support package leaks into production imports (the only non-test importers of `natstest` are other test-support packages, pre-existing). The option-resolution ordering in `Start` (options first, then KEK, DB, bus branching) is correct, and the external-NATS wrap (`eventbustest.Embedded{Bus: sub, JS: sub.JS(), Conn: sub.Conn()}`) type-checks against the real `Embedded` struct.

Four warnings: the M2 flap spec's deepest-code assertion passes only via an undocumented `go-retry` ctx-expiry accident that the in-test comments and the evidence doc both mischaracterize (latent spec breakage + misdirecting comment); the restart spec's "kill" is a no-op for a plugin-less replica and its "no replay at boot" claim is never asserted; cross-Describe teardown asymmetry leaves up to 8 stale replicas reconnect-looping against dead brokers for the rest of the run; and the M12 command-fidelity spec's "50 writes lost" verdict rests on an unasserted premise that both racing commands actually committed.

## Warnings

### WR-01: M2 flap-window deepest-code assertion passes only via an undocumented go-retry ctx-expiry accident; the comments and evidence doc describe the wrong mechanism

**File:** `test/integration/resilience/m2_dualwrite_test.go:27-33` (pausedMoveTimeout comment), `:113-131` (expectMoveEmitFailure doc), `:197-201`; also `docs/reviews/arch-review/2026-07-11/verification/f1-resilience-verdict.md` ("Mechanism", M2 chain paragraph)

**Issue:** The spec asserts `oopsErr.Code() == "EVENT_EMIT_FAILED"` and the comments claim this is "the deepest one the emit path set" after "the retry loop … exhausts". Tracing the actual chain under a frozen broker shows the deepest coded layers are NOT `EVENT_EMIT_FAILED`:

- `internal/eventbus/publisher.go:322-329` stamps `EVENTBUS_PUBLISH_EXPIRED` (on `context.DeadlineExceeded`) or `EVENTBUS_PUBLISH_FAILED` on the failing publish;
- `internal/world/event_store_adapter.go:39-44` wraps that in `EVENT_STORE_APPEND_FAILED`;
- `oops@v1.22.0` `Code()` returns the DEEPEST non-nil code (`error.go:233-250`), so if that attempt error survived, `Code()` would be `EVENTBUS_PUBLISH_EXPIRED`, not `EVENT_EMIT_FAILED`, and the spec would fail.

The assertion passes today only because of a `go-retry` detail nobody documents: the first publish attempt blocks until `moveCtx` (30s) expires, the attempt error is marked retryable, and `retry.DoValue` (`sethvargo/go-retry@v0.3.0/retry.go:79-92`) then hits `ctx.Done()` before/during the backoff wait and returns **bare `ctx.Err()`**, DISCARDING the entire coded attempt chain. `emitWithRetry` wraps that bare `context.DeadlineExceeded` with no code, so `EVENT_EMIT_FAILED` (stamped one layer up in `EmitMoveEvent`) becomes the deepest code by elimination. If the publish ever fails FAST (e.g., a Stop/Start restart chaos primitive instead of `docker pause`, or a dropped connection) the 3 retries exhaust before ctx expiry, `rerr.Unwrap()` preserves the coded chain, and `Code()` becomes `EVENTBUS_PUBLISH_FAILED` — the spec breaks, and the comment ("EVENT_EMIT_FAILED when a wired emitter's publish fails against a frozen broker") plus the verdict doc's mechanism paragraph ("retries the publish 3 times … before surfacing EVENT_EMIT_FAILED") would actively misdirect whoever debugs it. Phase 5 explicitly plans to extend this suite with broker-downtime fault injection (ADR Consequences), which is exactly the fast-fail shape.

**Fix:** Make the assertion robust to both exit paths and correct the mechanism prose:

```go
// Assert the chain CARRIES the emit-path categorization instead of pinning
// the deepest code (which flips between EVENT_EMIT_FAILED and
// EVENTBUS_PUBLISH_* depending on whether ctx expiry or retry exhaustion
// ends the retry loop — see sethvargo/go-retry DoValue ctx.Done() branch).
Expect(errutilChainHasCode(err, "EVENT_EMIT_FAILED")).To(BeTrue())
```

(or keep the exact-code check but document the real mechanism and add the `EVENTBUS_PUBLISH_*` codes as accepted alternates). Update the `pausedMoveTimeout` comment, the `expectMoveEmitFailure` doc, and the verdict doc's M2 mechanism paragraph to state that ctx expiry short-circuits the retry loop and discards the publisher's coded error — that discard, not retry exhaustion, is why `EVENT_EMIT_FAILED` is the deepest code on this path.

### WR-02: Restart spec's "kill" is a no-op and "no replay runs at boot" is never asserted

**File:** `test/integration/resilience/restart_reconnect_test.go:70-110`; `internal/testsupport/integrationtest/harness.go:789-796`

**Issue:** The spec comment says `"Kill" replica B` via `replicaB.Stop()`, but `Server.Stop` only stops the plugin subsystem — and this replica booted with NO plugins, so `Stop()` releases nothing. Replica B's external NATS subsystem, durable consumers, session store, and pgx pool all remain fully live (they are `t.Cleanup`-bound to `suiteT` and survive to suite end) while B' boots. B is never restarted; a third replica is added. Separately, the comment at line ~92 claims "a successful boot IS the assertion that no rebuild/replay runs at startup" — boot success does not assert replay absence; nothing in the spec observes that no replay ran. The ADR/verdict wording ("a restarted replica boots cleanly against the existing EVENTS stream and serves pre-restart state") survives because B'-boots-cleanly + DB-read-back are genuinely asserted, and the replay-absence claim is independently grounded in the F1 archaeology (no replay code exists to run) — but the spec proves strictly less than its own comments claim.

**Fix:** Either (a) add a real teardown seam so B actually dies — e.g., a `Server.StopBus()` that calls the external subsystem's `Stop` (the handle already exists at `harness.go:415-427`) and close B's pool, then boot B'; or (b) reword the spec comment and the verdict framing to "a FRESH replica joining the existing stream/database" and drop the "successful boot IS the assertion" sentence in favor of citing the archaeology for replay-absence.

### WR-03: Cross-Describe teardown asymmetry — stale replicas reconnect-loop against terminated brokers for the rest of the suite

**File:** `test/integration/resilience/chaos_helpers_test.go:158-165` (startExternalNATS DeferCleanup) vs `:172-180` (startReplica on suiteT); `internal/testsupport/integrationtest/harness.go:420-426`

**Issue:** Each Describe's NATS container is terminated by `DeferCleanup(env.Terminate)` registered in `BeforeAll`, which fires at that Ordered container's end (AfterAll-equivalent). But every replica's resources — external `eventbus.Subsystem` (with its `nats.Conn`), plugin subsystems (M12 boots TWO full plugin stacks), and pgx pools — are registered via `t.Cleanup` on `suiteT` and are released only after ALL Describes finish. Consequence: after each Describe completes, its replicas' NATS clients enter reconnect loops against a dead URL (nats.go default: 60 attempts × 2s ≈ 2 minutes) while the next Describe runs; across the four Describes up to 8 replicas accumulate live. Worst case, if Docker reassigns a terminated broker's mapped host port to a later Describe's container, a stale replica reconnects INTO the new broker and re-establishes its durable consumers/publishers there — cross-contaminating the later Describe's `LastSeq`-based assertions (boot smoke, restart/reconnect). Low probability, but it is exactly the flake class this opt-in suite is supposed to keep out of the nightly lane, and the suite-end `sub.Stop` errors it produces are only log-suppressed (`harness.go:423-425`).

**Fix:** Give each Describe a way to release its replicas at container end: expose a `Server` method that stops the external bus subsystem (and closes the pool), and call it from an explicit `AfterAll` in each Describe — or configure the external-mode `nats.Conn` with `MaxReconnects(0)`/short reconnect budget for the resilience seam so a dead broker fails fast instead of looping.

### WR-04: M12 command-fidelity verdict claims "50 writes lost" but the spec never verifies the superseded write committed

**File:** `test/integration/resilience/m12_lastwritewins_test.go:155-202`; inherited by `docs/reviews/arch-review/2026-07-11/verification/f1-resilience-verdict.md` and `docs/adr/holomush-i4784-world-state-model-decision.md` ("every round loses one write")

**Issue:** Per round the spec asserts (a) both `describe here` commands report success and (b) the surviving description is exactly one of {alpha, beta}. From this it *infers* "one write silently superseded" — valid only if BOTH commands actually committed a write each round. If one replica's command path were green-but-write-less (the sibling `set` command in `plugins/core-objects` is exactly such a write-less stub, per the verdict doc's own "Grounded correction"), every round would still pass with the other replica's value always surviving, and the "N writes lost" verdict would be hollow. Nothing in the spec (or the suite) demonstrates that replica B's *command-path* write ever lands — the deterministic spec (spec 1) proves B's *service-path* write only. Rounds where beta survives would prove it, but the spec neither tracks nor reports the alpha/beta survivor split.

**Fix:** Tally survivors and surface the split in the verdict line so the evidence shows both command paths wrote:

```go
if gotDesc == alpha { alphaSurvived++ } else { betaSurvived++ }
...
reportVerdict(fmt.Sprintf(
    "M12-VERDICT: concurrent-describe: ... (survivor split: A=%d B=%d)",
    alphaSurvived, betaSurvived))
```

Optionally assert `alphaSurvived > 0 && betaSurvived > 0` over N=50 (near-certain if both paths write; if scheduling bias makes that flaky, keep it report-only and let the split carry the evidence).

## Info

### IN-01: pauseBroker/unpauseBroker leak Docker clients

**File:** `test/integration/resilience/chaos_helpers_test.go:237,249`
**Issue:** `testcontainers.NewDockerClientWithOpts(ctx)` is called per pause/unpause and the returned client is never closed (no `defer cli.Close()`). Two flap specs × 2 calls each per run.
**Fix:** `defer func() { _ = cli.Close() }()` after the client is created, or create one client per Describe.

### IN-02: supersededWrites is a constant masquerading as a measurement

**File:** `test/integration/resilience/m12_lastwritewins_test.go:160,195-196`
**Issue:** `supersededWrites++` runs unconditionally every round, so it always equals `rounds`; the verdict prints it as "%d writes lost" as if measured. The inference is sound (see WR-04) but the variable adds nothing.
**Fix:** Drop the counter and print `rounds` directly, or replace it with the survivor-split tally from WR-04.

### IN-03: Boot-smoke Eventually swallows the Info error

**File:** `test/integration/resilience/boot_smoke_test.go:90-96`
**Issue:** The polled func returns `0` on `infoErr`, so a persistent `stream.Info` failure surfaces as an opaque "expected 0 to be > baseline" timeout instead of the underlying error. The sibling `lastSeq` helper (restart_reconnect_test.go:40-44) already does this right — Gomega retries failed assertions inside `Eventually`.
**Fix:** Reuse `lastSeq(ctx, stream)` here (it is in the same package) instead of the inline error-swallowing closure.

### IN-04: eventsStream/lastSeq helpers live in the wrong file

**File:** `test/integration/resilience/restart_reconnect_test.go:28-44`
**Issue:** `eventsStream` and `lastSeq` are shared helpers consumed by `m2_dualwrite_test.go` (lines 149, 217) but are defined in the restart spec file; `chaos_helpers_test.go` is the designated helpers file and already documents itself as such.
**Fix:** Move both helpers to `chaos_helpers_test.go`.

### IN-05: No guard against WithSharedDatabase × WithPluginCrypto KEK env clobber

**File:** `internal/testsupport/integrationtest/options.go:46-48`; `internal/testsupport/integrationtest/harness.go:349-350`
**Issue:** Every `Start` re-points the process-global `HOLOMUSH_KEK_FILE`/`HOLOMUSH_KEK_PASSPHRASE` at the newest replica's ephemeral keyfile via `t.Setenv`. The `WithSharedDatabase` doc calls this "benign while the resilience suite avoids WithPluginCrypto" — but unlike `WithPluginCrypto`-requires-`WithInTreePlugins` (harness.go:317-319), nothing enforces the constraint; a future two-replica crypto suite would silently point replica 1's env-based KEK reads at replica 2's keyfile.
**Fix:** Add a panic guard in `Start` (`WithSharedDatabase` + `WithPluginCrypto` ⇒ panic with a pointer to the KEK env limitation), or a sentence in `Start`'s KEK comment stating the multi-Start env semantics explicitly.

### IN-06: Pause windows have no failure-path unpause guard

**File:** `test/integration/resilience/m2_dualwrite_test.go:191-195`; `test/integration/resilience/restart_reconnect_test.go:158-164`
**Issue:** `pauseBroker` … `unpauseBroker` pairs are not protected by `DeferCleanup`; both windows are deliberately assertion-free so the ordinary failure path cannot strand the container paused, but a Ginkgo interrupt/timeout landing inside the window leaves the shared broker frozen for the remaining specs of the same Ordered container, which then fail confusingly (the container itself is still terminated at Describe end).
**Fix:** Register `DeferCleanup(func(){ unpauseBroker(ctx, env) })` immediately after each `pauseBroker` (unpausing an already-running container errors — tolerate it), or add a paused-flag guard.

---

## Verified clean (deep-dive results worth recording)

- **Evidence chain holds.** Every load-bearing claim in `f1-resilience-verdict.md` and the ADR maps to a real assertion or a disclosed inference: MoveCharacter's commit-then-emit + `move_succeeded=true` (service.go, verified), `EVENT_EMITTER_MISSING`/`EVENT_EMIT_FAILED` codes (events.go, verified), unguarded full-row `UPDATE` with no version column (location_repo.go, verified), production `world.Service` without an emitter (setup/subsystem.go:66-77, verified), entity_mutator read-modify-write (verified), 12-spec count (3+3+3+3, verified), and all quoted verdict-line formats match the code's format strings byte-for-byte modulo interpolations.
- **Quarantine-gate posture is correct per D-05.** `TestWorldModelResilience` gates on `quarantinetest.Enabled()` before `RunSpecs`; no file in the package matches the bijection meta-test's `markerLineRE` (`quarantinetest\.Skip\(|quarantined:|@quarantine|Label\("quarantine"`), so the deliberate absence of a `test/quarantine.yaml` row cannot trip the meta-test.
- **No production import leak.** The only non-test importers of `natstest`/`integrationtest` outside `internal/testsupport/` are `internal/cluster/clustertest/*` (a pre-existing test-support package) and a comment-only mention in `cryptowiring`.
- **Harness seams are sound.** Options resolve before the DB/bus branches; `WithExternalNATS` wraps the production subsystem in a correctly-typed `eventbustest.Embedded`; cleanup ordering on a single `t` is LIFO-safe (bus stops before the event store closes; the shared-DB drop registered by replica 1 runs after replica 2's pool closes); the moby `ContainerPause`/`ContainerUnpause` multi-return calls discard only the result value and assert the error.

---

_Reviewed: 2026-07-11_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
