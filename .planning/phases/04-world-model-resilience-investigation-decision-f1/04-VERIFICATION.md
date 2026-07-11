---
phase: 04-world-model-resilience-investigation-decision-f1
verified: 2026-07-11T18:30:00Z
status: passed
score: 10/10 must-haves verified
behavior_unverified: 0
overrides_applied: 0
---

# Phase 4: World-Model Resilience Investigation & Decision (F1) Verification Report

**Phase Goal:** Build the two-replica resilience harness (OPS-05, #4791), empirically prove M12 last-write-wins corruption and characterize the M2 dual-write window, produce the documented reproducible resilience verdict, and record the genuinely-open MODEL-01 ADR decision (#4784) naming the concrete mechanism Phase 5 implements.
**Verified:** 2026-07-11
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

Roadmap Success Criteria (contract) merged with plan-frontmatter must-haves. Behavior-dependent truths were confirmed by re-running the single named suite **in this verifier's own process** — not by trusting SUMMARY claims:

- `HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience -timeout 30m ./test/integration/resilience/` → **exit 0** (51.1s, fresh run this session)
- `task test:int -- -run TestWorldModelResilience ./test/integration/resilience/` (env unset) → **exit 0, 1 skipped, 0 specs run** (D-05 gate)
- `task test -- -run TestQuarantineRegistryBijection ./test/meta/` → **exit 0**

| #   | Truth | Status | Evidence |
| --- | ----- | ------ | -------- |
| 1   | SC1: A reproducible harness exercises concurrent commands + NATS broker flap + replica restart + client reconnect under the two-replica deployment (#4791) | ✓ VERIFIED | 6 spec files in `test/integration/resilience/` (12 specs: boot smoke ×3, M12 ×3, restart/reconnect/flap ×3, M2 ×3); all four chaos dimensions present (`DetachTransport`/`ReattachTransport` restart_reconnect_test.go:123-124, `pauseBroker` :158, concurrent describe m12:155-202, restart :70-110); suite re-ran green exit 0 this session |
| 2   | SC2: The harness produces a documented, reproducible verdict on whether M12 corrupts world state under concurrency | ✓ VERIFIED | `docs/reviews/arch-review/2026-07-11/verification/f1-resilience-verdict.md` exists (10 verdict-line quotes, reproduction command at line 59, sections Verdict/Reproduction/Evidence/Mechanism/Implications/Cross-refs); reproduction command re-executed green by verifier |
| 3   | SC3: A committed ADR records the world-model decision, grounded in F1 + resilience evidence (#4784) | ✓ VERIFIED | `docs/adr/holomush-i4784-world-state-model-decision.md` — `Status: Accepted` (line 11), token `holomush-i4784` (line 12), links both grounding docs (lines 17, 21, 112) + NORMATIVE one-pager (line 36); indexed in `docs/adr/README.md:19`; #4784 comment live (verified via `gh`), issue left OPEN for Phase 5 |
| 4   | SC4: The ADR names the concrete mechanism Phase 5 (MODEL-03/MODEL-04) will implement | ✓ VERIFIED | ADR Decision section: MODEL-03 version guard (`version INTEGER NOT NULL DEFAULT 1`, CAS writes AND deletes, `WORLD_CONCURRENT_EDIT`, no auto-retry) + MODEL-04 outbox/ordered atomic feed (`feed_position`, leased relay, write-requires-envelope seam); 6 MODEL-03/04 matches; INV-WORLD-* named, registration deferred to Phase 5 per invariants rule |
| 5   | Two in-process CoreServer replicas boot over ONE real NATS JetStream container and ONE shared Postgres DB (D-03) | ✓ VERIFIED | `WithExternalNATS`/`WithSharedDatabase` at options.go:34,52; option resolution (harness.go:313) precedes DB seam (:356-359) and bus seam (:414-428); `Config{Mode,URL}.Defaults()` at :415-418; boot smoke specs green in fresh run |
| 6   | Suite self-skips on the gating Integration Test lane and runs under HOLOMUSH_RUN_QUARANTINED=1, with NO quarantine-registry marker (D-05) | ✓ VERIFIED | Gate at resilience_suite_test.go:50-55 (`quarantinetest.Enabled()` before `RunSpecs`); both directions empirically re-verified this session (skip: exit 0/0 specs; enabled: exit 0/green); marker scan `rg 'quarantined:\|quarantinetest\.Skip(\|@quarantine\|Label("quarantine"'` → 0 matches; bijection meta-test re-run green |
| 7   | M12 lost update empirically reproduced: stale full-row UPDATE silently reverts a committed rename, both writers returning nil (D-06) | ✓ VERIFIED | m12_lastwritewins_test.go:144 asserts read-back name equals ORIGINAL after both nil-error UpdateLocation calls; direct-SQL read-back `SELECT name, description FROM locations` (:103); concurrent-describe N=50 + cross-field k/N specs present; all green in fresh run |
| 8   | A replica restart preserves canonical world state (DB-derived) and a detached client reconnects and resumes delivery | ✓ VERIFIED | restart spec boots B' against existing EVENTS stream + reads pre-restart state via B'.Pool() (:70-110); detach/reattach + LastSeq-advance assertion (:120-141); broker-flap recovery (:158-185); green in fresh run. (Review WR-02 notes the spec comment overclaims "kill"; the asserted facts — fresh replica joins stream + DB read-back — satisfy this truth; replay-absence is grounded in the F1 archaeology) |
| 9   | M2 dual-write window characterized (D-07): DB commit persists, caller error carries move_succeeded=true, delivery decoupled; PLUS production ships NO EventEmitter (notification leg dead code) | ✓ VERIFIED | m2_dualwrite_test.go flap spec: `move_succeeded=true` context assertion (:128-129), `SELECT location_id FROM characters` commit read-back (:139), `EVENT_EMIT_FAILED` (:201); production-shape spec via emitter-less `newWorldService` asserts `EVENT_EMITTER_MISSING`; raw publisher used (`NewRenderingPublisher` → 0 matches); green in fresh run |
| 10  | The MODEL-01 decision was made by the human decider at a blocking checkpoint, weighed genuinely equally (D-01), with intent recorded (D-02) | ✓ VERIFIED | Plan 04 `autonomous: false` + `checkpoint:decision gate="blocking"`; ADR `Deciders: Sean Brandt` (:13), Rationale records genuinely-open provenance + future-state framing (:45), `## Intent (D-02)` section (:30-32); no-lean scan (`we recommend\|recommended option\|leaning toward`) → 0 matches; all five Option-A hard-cost terms present (retention ×6, ARCH-04 ×4, does-not-automatically-dissolve ×1, foundation slice ×2, replay ×9); panel trail docs exist (opinions 29KB, round2 19.5KB, one-pager 6.2KB) with commits `53f6359e9`/`41ced0df1` in git |

**Score:** 10/10 truths verified (0 present-but-behavior-unverified — every behavior-dependent truth was confirmed by a fresh green run of the named suite in this session)

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `internal/testsupport/integrationtest/options.go` | WithExternalNATS + WithSharedDatabase StartOptions | ✓ VERIFIED | Both at :34/:52; consumed by `startReplica` in resilience suite |
| `internal/testsupport/integrationtest/harness.go` | Option resolution before DB/bus; ConnStr()/Bus() accessors | ✓ VERIFIED | cfg resolution :313 < DB :356 < bus :414; accessors :942/:950; external bus wrapped as `eventbustest.Embedded` |
| `test/integration/resilience/resilience_suite_test.go` | Gated TestWorldModelResilience entry | ✓ VERIFIED | Gate-first, marker-free; skip message references #4791 |
| `test/integration/resilience/chaos_helpers_test.go` | startExternalNATS, startReplica, pause/unpause, newWorldService, worldBusAppender, newEmittingWorldService | ✓ VERIFIED | All present; `world.EventAppender` compile-time assertion :75 |
| `test/integration/resilience/boot_smoke_test.go` | Two-replica boot smoke | ✓ VERIFIED | 3 specs, green |
| `test/integration/resilience/m12_lastwritewins_test.go` | 3 graduated M12 specs + M12-VERDICT lines | ✓ VERIFIED | Deterministic + N=50 command + k/N cross-field; verdict emitters match doc quotes |
| `test/integration/resilience/restart_reconnect_test.go` | Restart/reconnect/flap specs + CHAOS-VERDICT lines | ✓ VERIFIED | 3 specs, green |
| `test/integration/resilience/m2_dualwrite_test.go` | Control/flap/production-shape specs + M2-VERDICT lines | ✓ VERIFIED | 3 specs; all four flap legs asserted or recorded per D-07 deviation |
| `docs/reviews/arch-review/2026-07-11/verification/f1-resilience-verdict.md` | OPS-05 verdict document | ✓ VERIFIED | All 6 required sections; 10 verdict-line quotes; neutral (D-01 scan clean) |
| `docs/adr/holomush-i4784-world-state-model-decision.md` | Accepted ADR, Option B, MODEL-03/04 named | ✓ VERIFIED | 21KB, no PENDING remnants, authorization placement recorded (checkAccess pre-write, :101) |
| `docs/adr/README.md` | Index row | ✓ VERIFIED | Row at :19 with token |
| Panel decision inputs (opinions/round2/one-pager) | Checkpoint decision trail | ✓ VERIFIED | All three exist; one-pager referenced as NORMATIVE from ADR |

All 6 test files carry `//go:build integration` (count = 6).

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | --- | --- | ------ | ------- |
| `Start()` option resolution | DB/bus seams | cfg resolved at harness.go:313 before :356/:414 | ✓ WIRED | Seams are live, not dead code |
| Replica boots | idempotent CreateOrUpdateStream | `Config{Mode,URL}.Defaults()` only (:415-418) | ✓ WIRED | Identical desiredStreamConfig per replica |
| Suite gate | quarantine bijection meta-test | `Enabled()` only; no markerLineRE match | ✓ WIRED | Meta-test re-run green |
| M12 read-backs | shared pgxpool | direct `SELECT` via Pool (m12:103, m2:139) | ✓ WIRED | Never via sessions/frames (Pitfall 6) |
| M2 appender | raw Subsystem Publisher | `worldBusAppender` over `s.Bus().Bus.Publisher()`; 0 `NewRenderingPublisher` matches | ✓ WIRED | EMIT_UNKNOWN_VERB avoided by design |
| Verdict doc | spec output | verbatim M12/M2/CHAOS-VERDICT quotes | ✓ WIRED | Format strings match spec emitters (also confirmed byte-for-byte by 04-REVIEW deep review) |
| ADR | grounding docs | links (not duplicates) to f1-eventsourcing-why + f1-resilience-verdict + consensus one-pager | ✓ WIRED | Lines 17/21/36/55/112 |
| ADR | #4784 / verdict doc | issue comment | ✓ WIRED | Last #4784 comment names ADR path + evidence doc; issue OPEN as intended |
| Invariant deferral | Phase 5 spec | INV-WORLD-* named, not registered | ✓ WIRED | Honors `.claude/rules/invariants.md`; no fabricated bindings |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Full opt-in suite green (12 specs: boot/M12/chaos/M2) | `HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience -timeout 30m ./test/integration/resilience/` | exit 0, 51.1s | ✓ PASS |
| D-05 gate: env-unset self-skip | same without env var | exit 0, 1 skipped, 0 specs, 0.47s | ✓ PASS |
| Quarantine bijection undisturbed | `task test -- -run TestQuarantineRegistryBijection ./test/meta/` | exit 0 | ✓ PASS |

### Probe Execution

No `scripts/*/tests/probe-*.sh` probes are declared by any plan in this phase; the phase's runnable verification surface is the gated suite, executed above. SKIPPED (no probes declared).

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ----------- | ----------- | ------ | -------- |
| OPS-05 | 04-01, 04-02, 04-03 | Resilience pass reproduces concurrent commands + broker flap + replica restart + client reconnect; establishes whether M12 corrupts state (#4791) | ✓ SATISFIED | Truths 1, 2, 5-9; suite re-run green; verdict doc; #4791 comment live |
| MODEL-01 | 04-03, 04-04 | Divergence resolved by committed ADR grounded in F1, formally deciding the model (#4784) | ✓ SATISFIED | Truths 3, 4, 10; Accepted ADR + index + #4784 comment |

No orphaned requirements: REQUIREMENTS.md traceability maps exactly MODEL-01 and OPS-05 to Phase 4 (both marked Complete); OPS-01 is an explicit pre-Phase-4 quick fix outside this phase.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| — | — | No TBD/FIXME/XXX/TODO/placeholder markers in any phase-modified file | — | — |

04-REVIEW.md (deep code review, advisory) recorded 0 critical, 4 warnings, 6 info. Assessed against the phase goal:

- **WR-01** (M2 deepest-code assertion passes via a go-retry ctx-expiry accident; comments misattribute the mechanism) — latent spec fragility + prose inaccuracy; the asserted facts (commit persisted, move_succeeded=true, decoupled delivery) remain genuinely proven. Not a goal gap; should be addressed when Phase 5 extends the suite.
- **WR-02** (restart "kill" is a no-op for a plugin-less replica; "no replay at boot" never asserted) — the truth's asserted substance (fresh replica joins existing stream + serves pre-restart state from DB) holds; replay-absence is independently grounded in the F1 archaeology. Comment overclaim, not a goal gap.
- **WR-03** (cross-Describe teardown asymmetry; stale replicas reconnect-loop) — nightly-lane flake-risk hygiene; suite currently green. Not a goal gap.
- **WR-04** ("50 writes lost" rests on unasserted both-paths-write premise) — the deterministic spec independently proves the silent-discard mechanism; the aggregate phrasing could be tightened with a survivor split. Not a goal gap.

These four warnings are quality-improvement follow-ups for Phase 5 (which flips the M12 specs to assert surfaced conflicts anyway, per the ADR slice ordering); none invalidates a phase truth.

### Human Verification Required

None. The one inherently-human element of this phase — the MODEL-01 decision itself — was already made by the human decider (Sean Brandt) at plan 04's blocking `checkpoint:decision` (plan `autonomous: false`), and its provenance is recorded in the ADR Rationale and the panel-trail commits. All remaining truths were verified programmatically or by fresh empirical runs in this session.

### Gaps Summary

No gaps. All four ROADMAP success criteria and all plan-level must-haves are observably true in the codebase:

1. The two-replica harness is real (seams live in `integrationtest.Start`, gated suite with 12 specs) and was re-run green by this verifier — exit 0, both gate directions confirmed.
2. M12 is proven, not asserted: the deterministic spec's assertion that a committed rename reverts to the original after two nil-error writes is in the code and passed in a fresh run.
3. The M2 window is characterized per D-07 (commit persists, move_succeeded=true, delivery decoupled) with the production unwired-emitter finding pinned by its own spec.
4. The verdict doc and the Accepted ADR exist, are substantive, cross-linked, indexed, and name the concrete Phase 5 mechanism (MODEL-03 version guard + MODEL-04 transactional outbox / ordered atomic feed, one-pager NORMATIVE).

---

_Verified: 2026-07-11_
_Verifier: Claude (gsd-verifier)_
