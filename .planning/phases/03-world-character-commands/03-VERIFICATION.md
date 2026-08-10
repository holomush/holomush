---
phase: 03-world-character-commands
verified: 2026-08-09T23:55:00Z
status: passed
score: 5/5 must-haves verified
behavior_unverified: 0
overrides_applied: 0
human_verification:

  - test: "Rule on whether IDENT-04 is legitimately `Complete` at Phase 3. Read `.planning/REQUIREMENTS.md:112-114` (the requirement text says a character can be soft-retired, ADMIN-DRIVEN in v0.13) against the codebase fact that `world.Service.RetireCharacter` has ZERO non-test callers — no gRPC RPC, no web handler, no CLI, no admin surface. The ROADMAP Phase 6 SC2 covers the admin surface in substance but the traceability table (`.planning/REQUIREMENTS.md:372`) maps IDENT-04 to Phase 3 ONLY, so nothing re-opens it."
    expected: "Either (a) confirm the domain capability + ABAC surface is what IDENT-04 meant and the admin UI is a separate requirement, or (b) revert IDENT-04 to Pending / split it so the Phase 6 admin half is traced. Decide which."
    why_human: "This is a requirement-scope judgment, not a code fact. The code state is unambiguous and verified; what 'Complete' means for a requirement whose user-facing half lands three phases later is the developer's call."

  - test: "Accept or reject the criterion-3 lane caveat. The two-replica resilience proof (`test/integration/resilience/retire_concurrency_test.go`) lives in a suite gated on `quarantinetest.Enabled()` (`resilience_suite_test.go:50`), so it does NOT run in the gating `task test:int` and a regression in it would not be caught by PR CI."
    expected: "Either accept the ungated proof (the same guarantee IS covered in the gating lane by `test/integration/world/character_retire_atomicity_test.go:134` and `internal/world/service_retire_test.go:83`), or schedule #4953 so the resilience suite can rejoin the gating lane."
    why_human: "Whether an ungated proof is acceptable coverage is a policy decision about CI gating, not a codebase fact."
---

# Phase 3: World Character Commands Verification Report

**Phase Goal:** `world.Service` gains a soft `RetireCharacter` / `UnretireCharacter` pair at the domain layer, version-guarded and emitting through the transactional outbox in-transaction, plus the host-side reactor that makes a retired character actually leave active play, with the `writeCommands` census rows and taxonomy kinds landed in the same change.

**Verified:** 2026-08-09
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Retired character leaves active play, record intact, **name still reserved**, reversible; retire/idle-out/purge distinct; `DeleteCharacter` untouched | ✓ VERIFIED | `character_repo.go:482` — the CAS statement is `UPDATE characters SET status = $2, version = version + 1`; `name`/`normalized_name`/`name_skeleton` are absent from it. `service.go:1029` `UnretireCharacter` is the reversal. `service.go:966-980` exhaustive lifecycle switch with denying default; no transition INTO idle added. `DeleteCharacter` (`service.go:790`) unchanged by the diff except one comment line. Behavioral proof: `test/integration/world/character_lifecycle_test.go` "keeps the name reserved when the retire runs through the production RetireCharacter command" — asserts `reclaim(...)` still errors after a committed `RetireCharacter`. |
| 2 | Retirement is **observably** effective: reactor ends live sessions, notifies the location left, moves to the starting location | ✓ VERIFIED | `internal/retirement/reactor.go:254-334` — status guard → `DeleteByCharacter` → `EmitLeave` at `info.LocationID` (the OLD location) → `EmitSessionEnded` with `SessionEndedCauseRetired` → `MoveCharacter`. Wired at the composition root: `cmd/holomush/core.go:896` `retirement.NewSubsystem(retirement.Config{…})`. Behavioral proof through the REAL relay → durable consumer → handler: `test/integration/retirement/retirement_reactor_test.go:210`. That suite carries **no** quarantine gate and is picked up by `task test:int`'s `./...` (`Taskfile.yaml:288-289`), which the orchestrator established green. |
| 3 | Stale `expected_version` rejected with typed `WORLD_CONCURRENT_EDIT`; the two-replica resilience harness pointed at the new commands passes | ✓ VERIFIED (with caveat — see below) | Guard order pinned at `service.go:953-980`: version precheck runs BEFORE the lifecycle switch. Numeric SQL equality at `character_repo.go:487` (`AND version = $3`, bound `int`). **Independently re-run by this verifier**: the new Describe PASSED both under `-ginkgo.focus=IDENT-10` and inside the full suite run (ginkgo JSON report). Gating-lane coverage of the same guarantee exists at `service_retire_test.go:83` and `test/integration/world/character_retire_atomicity_test.go:134`. |
| 4 | `writeCommands` census and taxonomy list the new commands in the same change; the census meta-test fails if either is missing | ✓ VERIFIED | Both landed in commit `76c9ada9b` alongside the command. `mutator.go:104-105` census rows; `outbox/taxonomy.go:65,70,128-129` kinds + registry entries. The meta-test enforces BOTH directions: `world_envelope_census_test.go:80` (`outbox.IsDeclared` — a missing taxonomy kind fails) and `:195-206` (go/ast set equality against `s.mutator`-routing Service methods — a missing census row fails). Re-run green by this verifier. |
| 5 | `characters.last_active_at` actually written, without a per-event DB write; `INV-WORLD-4`'s writer enumeration amended in the same change | ✓ VERIFIED | Listener does one KV `Put` per character-actor event and no DB write (`listener.go:84`). Flusher drains on a tick into `worldpostgres.UpdateCharacterLastActive` (`activity.go:63`), monotonic via `WHERE … last_active_at < $2`. `INV-WORLD-4` amended THREE→FOUR with the named envelope exemption (`invariants.yaml:5122-5143`); `invariants.md` regenerated (registry meta-tests re-run green). Behavioral proof: `test/integration/charactivity/character_activity_flush_test.go:100` asserts the column advances while `characters.version` and the outbox row count do NOT. |

**Score:** 5/5 truths verified (0 present, behavior-unverified)

### Criterion 3 — the honest ruling

Both clauses were examined separately because they are not equally true.

**Clause (a) — "a stale `expected_version` is rejected with the typed `WORLD_CONCURRENT_EDIT` signal rather than silently overwriting": MET, and gated.**
The guarantee is asserted in the *gating* lane by `internal/world/service_retire_test.go:83` (`task test`) and `test/integration/world/character_retire_atomicity_test.go:134` (`task test:int`), plus `character_repo_status_test.go:71` at the repo layer. A regression here fails PR CI.

**Clause (b) — "v0.12's existing two-replica resilience harness, pointed at the new commands, passes": TRUE OF THE NEW SPEC, FALSE OF THE HARNESS.**
This verifier ran the suite twice rather than trusting the SUMMARY:

| Run | Result |
|---|---|
| `HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience ./test/integration/resilience/ -ginkgo.focus='IDENT-10'` | exit 0 — the new spec **passed**, 1 passed / 22 filtered out |
| same without the focus filter (full suite) | exit 201 — **15 passed, 3 panicked, 1 failed, 5 skipped**; `SuiteSucceeded=false`. The IDENT-10 spec is among the 15 **passed**. |

The 4 failures were traced to source and are **pre-existing and unrelated to this phase**, corroborating `deferred-items.md` rather than taking its word:

- 3 panics: `natstest.(*NATSEnv).Conn` → `nats: Authorization Violation` (`internal/testsupport/natstest/nats.go:64` dials `nats.Connect(e.URL)` with no credentials against a scoped account). That file was **not touched by this phase**; last modified `cce89c702`, 2026-07-19.
- 1 failure: `seedOutboxRow: WriteIntent` → "feed counter Allocate requires an ambient mutation transaction; refusing to allocate on the raw pool". That guard is from `7ff05af3c`, 2026-07-13.
- Filed as **holomush/holomush#4953** (verified OPEN via `gh`).

**Caveat carried forward:** the whole resilience suite is nightly/opt-in (`resilience_suite_test.go:50` gates on `quarantinetest.Enabled()`, #4791), so the new spec did **not** run in the green `task test:int` the orchestrator established. Its pass is real but ungated. Because clause (a)'s guarantee is independently gated, this is a WARNING, not a BLOCKER — surfaced as a human decision above.

### Required Artifacts

| Artifact | Expected | Status | Details |
|---|---|---|---|
| `internal/world/outbox/taxonomy.go` | Two kinds + registry entries, `AppSchemaVersion` 2 | ✓ VERIFIED | 217 lines; both kinds declared and registered |
| `internal/world/service.go` | `RetireCharacter` + `UnretireCharacter` | ✓ VERIFIED | Both present with full guard chains (`:929`, `:1029`) |
| `internal/world/mutator.go` | Two census rows + `setCharacterStatus` | ✓ VERIFIED | `:104-105`, routes through `mutate()` |
| `internal/world/postgres/character_repo.go` | `SetStatus` CAS + same-tx default-character clear | ✓ VERIFIED | `:476-527`, D-34 clear at `:511-519` inside `withTx` |
| `internal/eventbus/consumer/consumer.go` | Relocated `CreateWithRetry` | ✓ VERIFIED | 87 lines; 4 production callers |
| `internal/retirement/reactor.go` | Guarded fanout handler | ✓ VERIFIED | 360 lines, real implementation |
| `internal/retirement/subsystem.go` | Lifecycle subsystem | ✓ VERIFIED | 329 lines; constructed in `core.go:896` |
| `internal/charactivity/{subsystem,listener,flusher}.go` | KV buffer + tick flush | ✓ VERIFIED | 443 / 97 / 182 lines; `LastRevision` guard at `flusher.go:62` |
| `internal/world/postgres/activity.go` | Monotonic writer at the fenced boundary | ✓ VERIFIED | 75 lines; single predicated UPDATE |
| `internal/core/session_ended_payload.go` | `SessionEndedCauseRetired` | ✓ VERIFIED | Present, consumed at `reactor.go:313` |
| `internal/testsupport/integrationtest/options.go` | `WithOutboxRelay` + `WithRetirementReactor` | ✓ VERIFIED | Real subsystems constructed at `harness.go:974,1031` — not stubs |
| `test/integration/retirement/*` | Suite entry + four Describes | ✓ VERIFIED | `TestRetirementReactor` is a real `-run` target; 6 `It`s, no skips |
| `docs/architecture/invariants.yaml` | INV-WORLD-4 THREE→FOUR | ✓ VERIFIED | `:5122` "exactly FOUR sanctioned out-of-world writers" with the exemption clause |

### Key Link Verification

| From | To | Via | Status |
|---|---|---|---|
| `service.go` | `mutator.go` | `s.mutator.setCharacterStatus` (2 sites — the census AST cross-check keys on exactly this) | ✓ WIRED |
| `mutator.go` | `character_repo.go` | `characterWriter.SetStatus` inside `mutate()` | ✓ WIRED |
| `mutator.go` | `taxonomy.go` | census Kind strings == declared kinds (bijection meta-test green) | ✓ WIRED |
| `audit/projection.go`, `audit/plugin_consumer.go`, `retirement`, `charactivity` | `eventbus/consumer` | `consumer.CreateWithRetry` — all four callers | ✓ WIRED |
| `cmd/holomush/core.go` | `retirement`, `charactivity` | composite literal supplies real deps | ✓ WIRED |
| `charactivity/subsystem.go` | `world/postgres/activity.go` | `ActivityWriter` func injected at `core.go:920-921` — no SQL escapes the writer boundary | ✓ WIRED |
| `integrationtest/harness.go` | `world/setup`, `internal/retirement` | `NewOutboxRelaySubsystem` (`:974`), `retirement.NewSubsystem` (`:1031`) | ✓ WIRED |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|---|---|---|---|
| Two-replica stale-version rejection (criterion 3) | `HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience ./test/integration/resilience/ -ginkgo.focus='IDENT-10'` | exit 0; ginkgo JSON: IDENT-10 spec `passed` | ✓ PASS |
| Full resilience suite health | same, no focus filter | exit 201; 15 passed / 3 panicked / 1 failed — all 4 traced to pre-phase commits | ✗ FAIL (pre-existing, #4953) |
| Census + fence + invariant registry meta-tests | `task test -- -run 'TestWorldEnvelopeCensus\|TestWorldSQLFence\|TestEveryRegistryInvariantHasBinding\|TestProvenanceGuard\|TestBoundInvariantsAreGenuinelyAsserted\|TestInvariantsMarkdownMatchesRegistry' ./test/meta/` | 21 tests, exit 0 | ✓ PASS |
| Issue #4953 exists and is open | `gh issue view 4953` | `{"number":4953,"state":"OPEN"}` | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plans | Status | Evidence |
|---|---|---|---|
| IDENT-10 | 03-01, 03-03, 03-05, 03-06 | ✓ SATISFIED | Both new mutations reject `expected_version <= 0` before any read (`service.go:931`, `:1031`), reject stale with `WORLD_CONCURRENT_EDIT`, and emit through the outbox in-transaction (`mutate()` → one envelope per command, proven by `character_retire_atomicity_test.go`). The `last_active_at` flush is named as an argued, registered envelope exemption rather than an unstated gap. |
| IDENT-04 | 03-01, 03-02, 03-04, 03-05, 03-06 | ⚠️ SATISFIED IN SUBSTANCE — scope question raised | Soft retire preserves record + name and is reversible; the reactor makes it observably effective; admin-only ABAC surface is seeded and proven (`test/integration/access/evaluation_test.go:124-208`, admin permit + non-admin deny on own character with a positive control). **But** `RetireCharacter`/`UnretireCharacter` have **zero non-test callers** — no RPC, handler or CLI. See human-verification item 1. |

**Traceability consistency:** the flip is clean. `git diff` of `.planning/REQUIREMENTS.md` over the phase shows exactly four line changes — two checkbox flips (`:112`, `:139`) and two traceability rows (`:372`, `:378`) — with **no requirement text reworded**. The hand-set rows after `requirements.mark-complete` returned `table_unmatched` are internally consistent with the checkboxes and with the phase-coverage row at `:351`.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|---|---|---|---|---|
| — | — | — | — | **None.** A scan of all 51 changed `.go`/`.yaml` files for `TBD`/`FIXME`/`XXX` and for `TODO`/`HACK`/`PLACEHOLDER`/"not yet implemented" returned **zero** matches. No stub returns, no empty handlers, no hardcoded-empty data reaching output. |

The nine warnings in `03-REVIEW.md` were read against the success criteria. None falsifies one:

- **WR-03** (listener buffer is not monotonic — an out-of-order redelivery can clobber a newer buffered timestamp) is a real defect, but it is a *different* race from the one plan 03-05's must-have pins (the flusher-side read/delete window, which IS revision-guarded at `flusher.go:62` and tested). Its effect is bounded staleness, not a failure to write — criterion 5 stands.
- **WR-07** (`aggregateFromSubject` reads the last token, not the aggregate token) is latent, not live: today's world-envelope subjects are exactly four tokens (`outbox/wire.go:159-160`), which the review itself states.
- WR-01, WR-02, WR-04, WR-05, WR-06, WR-08, WR-09 are operational/robustness quality issues outside the five criteria.

### Human Verification Required

Both items are in the frontmatter. Neither blocks the phase goal; both are judgment calls this verifier cannot resolve from the codebase.

1. **IDENT-04 marked Complete with no reachable admin surface** — decide whether the domain capability + ABAC seed is what the requirement meant, or split/re-open it so Phase 6's admin half is traced.
2. **Criterion 3's proof is ungated** — decide whether an opt-in-only two-replica proof is acceptable, given the same guarantee is gated elsewhere.

### Gaps Summary

**No gaps.** All five ROADMAP success criteria are achieved in the codebase, each with a behavioral test rather than presence alone, and every plan-frontmatter artifact and key link is present, substantive and wired. Nothing was accepted on a SUMMARY's word: the resilience claim, the meta-test enforcement, the invariant amendment, the issue filing, and the REQUIREMENTS.md diff were each re-derived from the repository or re-executed by this verifier.

The one place where the SUMMARY narrative and reality diverge is **criterion 3's literal wording**: the harness does not "pass" — it is red with four pre-existing failures, and the phase's own spec within it is nightly/opt-in. The phase's `deferred-items.md` documented this honestly and filed #4953, and the substantive guarantee is proven and gated elsewhere, so this is recorded as a caveat and a human decision rather than a gap.

---

_Verified: 2026-08-09_
_Verifier: Claude (gsd-verifier)_
