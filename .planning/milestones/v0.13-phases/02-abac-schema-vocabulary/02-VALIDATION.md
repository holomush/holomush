---
phase: 2
slug: abac-schema-vocabulary
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: validated
nyquist_compliant: true
wave_0_complete: true
created: 2026-08-03
filled: 2026-08-05
---

# Phase 2 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Seeded from `02-RESEARCH.md` § Validation Architecture. The Per-Task Verification Map
> was filled by plan `02-11` (the phase's closeout pass) once every other plan had run.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + `testify` (`assert`/`require`); Ginkgo/Gomega for integration |
| **Config file** | `Taskfile.yaml` — no separate test config; the `test:int` package list is hard-coded there |
| **Quick run command** | `task test -- ./internal/<pkg>/` |
| **Full suite command** | `task test` (untagged), then `task test:int` (integration, needs Docker) |
| **Coverage command** | `task test:cover` |
| **Estimated runtime** | ~60s untagged; integration ~155s observed (testcontainers Postgres) |

**Critical:** `task test` does **not** compile `//go:build integration` files. This phase refactored
shared types (the `ExistsByName` interfaces, `world.Character`, `NewPropertyProvider`'s signature), and
`task test:int` is what caught every one of those breaks — see the Observed-gaps section below.

**Delegation rule:** per `CLAUDE.md`, `task test|lint|build|test:int|test:cover` MUST be dispatched
to the `local-check` agent rather than run inline in the parent session — except the FINAL
`task pr-prep` before a push, which runs inline. **Sub-agent executors are exempt** and run them
inline in their own context (`.claude/rules/subagent-briefing.md`); every command recorded in this
file was run that way.

---

## Sampling Rate

- **After every task commit:** `task test -- ./<touched package>/` plus `task lint`
- **After every plan wave:** `task test` (full untagged) — and `task test:int` for any wave that
  touched a shared type or interface
- **Before `/gsd-verify-work`:** `task pr-prep` green; `task test:int` green (the DB-backed
  success criteria live there)
- **Max feedback latency:** ~60s for the untagged lane

---

## Per-Task Verification Map

**Scope, derived rather than transcribed.** The plan list is
`ls .planning/phases/02-abac-schema-vocabulary/02-*-PLAN.md`, which yields **thirteen** plans:
`02-01` … `02-13`. Every one is covered here **except `02-11`** — this file is `02-11`'s own
deliverable, and its tasks are the closeout being audited, which cannot audit itself. That leaves
**twelve plans and 40 tasks**. Deriving the list from the directory rather than from prose is what
makes a later-added plan included by construction; the earlier draft's hard-coded "02-01 through
02-10" silently excluded `02-12` and `02-13`.

**On the `T-02-NN` threat ids.** They are **not** globally unique across this phase — `T-02-06`,
`T-02-07`, `T-02-42`, `T-02-78`, `T-02-79`, `T-02-89`, `T-02-95`, `T-02-103` and `T-02-104` each
name **different** components in different plans (and `T-02-42` names two different threats inside
`02-07` alone). A reference is therefore meaningful only qualified by its plan, which is how the
table below reads them. Recorded as a phase-audit observation, not repaired here: renumbering ids
already cited in twelve SUMMARYs would break more than it fixes.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| T1 (checkpoint) | 02-01 | 1 | IDENT-06 | T-02-04, T-02-80 | The UTS #39 mechanism is chosen deliberately, with the Unicode version pinnable and recorded per row (D-23) | checkpoint:decision | — (auto-selected `generate-into-repo`; `gate="blocking"`, `auto_advance` true) | ✅ | ✅ green |
| T2 | 02-01 | 1 | IDENT-06 | T-02-01, T-02-02, T-02-03, T-02-05, T-02-06, T-02-07 | A whole-script Cyrillic homoglyph of a seeded Latin name is refused against real Postgres; the gate fails closed on a partially populated skeleton column | integration + unit | `task test:int -- ./test/integration/charname/...` | ✅ | ✅ green |
| T3 | 02-01 | 1 | IDENT-06 | T-02-04, T-02-80 | The generated confusables table is reproducible, digest-pinned, and drift-detectable offline | meta-test | `task --force generate:confusables && git diff --exit-code -- internal/charname/confusables_table_gen.go` | ✅ | ✅ green |
| T1 | 02-02 | 2 | IDENT-06 | T-02-06, T-02-07, T-02-09, T-02-10 | §6.1.2's eight-row verdict table as a closed enumeration; an unassigned-script rune is REJECTED fail-closed; no non-Latin name is rejected that its Latin equivalent would pass | unit | `task test -- -run 'MixedScript\|ScriptSet' ./internal/charname/` | ✅ | ✅ green |
| T2 | 02-02 | 2 | IDENT-06 | T-02-09 | An empty normal form is refused with a message that distinguishes "only invisibles" from "blank", without changing the error code callers match on | unit | `task test -- ./internal/charname/ ./internal/world/` | ✅ | ✅ green |
| T3 | 02-02 | 2 | IDENT-08 | T-02-08 | `^[a-zA-Z][a-zA-Z0-9_]*$` is pinned by regression, and the two name policies are mechanically prevented from reaching each other | unit (go/parser guards) | `task test -- ./internal/auth/ ./internal/charname/` | ✅ | ✅ green |
| T1 | 02-03 | 2 | EXT-07 | T-02-11, T-02-14 | No call site can produce a bare prefix; `ViewerSubject` panics on an unrecognized tier, so a client-supplied string cannot reach a subject | unit (tdd) | `task test -- ./internal/access/` | ✅ | ✅ green |
| T2 | 02-03 | 2 | PROFILE-11 | T-02-12, T-02-13, T-02-15, T-02-16, T-02-81 | The `viewer` namespace resolves all three rungs; `player_id` and `roles` are OMITTED (never sentinel) on every unresolved path, with `has_*` witnesses on every path | unit (tdd) | `task test -- ./internal/access/policy/attribute/` | ✅ | ✅ green |
| T1 | 02-04 | 2 | IDENT-09 | T-02-17, T-02-19 | `characters.status` is a closed vocabulary read through ONE exhaustive predicate whose `default` arm DENIES | unit | `task test -- ./internal/world/... ./internal/grpc/` | ✅ | ✅ green |
| T2 | 02-04 | 2 | IDENT-09 | T-02-18, T-02-20, T-02-21 | A character constructed directly in `idle` is excluded from selection; retire does NOT release the name and both hard-delete paths DO | integration | `task test:int -- ./test/integration/world/` | ✅ | ✅ green |
| T3 | 02-04 | 2 | IDENT-09 | T-02-89 | INV-WORLD-5/6 flip to `bound` only against tests that genuinely assert them; INV-WORLD-6's summary is CORRECTED before binding, never bound as written | meta-test | `task test -- ./test/meta/` + `go run ./cmd/inv-render -check` | ✅ | ✅ green |
| T1 | 02-05 | 3 | IDENT-07 | T-02-22, T-02-26, T-02-27 | A block list compiles to an immutable snapshot; the first uncompilable entry aborts the whole compilation naming it; `Match` returns no string, so a rejection cannot echo operator config | unit | `task test -- ./internal/charname/blocklist/` | ✅ | ✅ green |
| T2 | 02-05 | 3 | IDENT-07 | T-02-24, T-02-25, T-02-90, T-02-91, T-02-92, T-02-103, T-02-104 | A malformed value is NOT read as absent; a failed reload leaves the last valid list enforcing; the poll indicator is `(updated_at, md5(value))` so a direct-SQL edit is observed | unit | `task test -- ./internal/charname/blocklist/ ./internal/lifecycle/ ./cmd/holomush/` | ✅ | ✅ green |
| T3 | 02-05 | 3 | IDENT-07 | T-02-23, T-02-28 | The block list rejects at the gate against real Postgres, and a live edit takes effect through the same gate instance | integration | `task test:int -- ./test/integration/charname/` | ✅ | ✅ green |
| T0 (checkpoint) | 02-13 | 3 | PROFILE-11 | T-02-89 | The derived-peer direction is settled BEFORE execution, not discovered in the code | checkpoint:decision | — (auto-selected `no-widening-direction`, which is D-27's locked default) | ✅ | ✅ green |
| T1 | 02-13 | 3 | PROFILE-11 | T-02-87 | `PlayerRoles` is a per-player union behind a func-field seam; the `RoleStore` interface method set is unchanged | unit + integration (tdd) | `task test -- ./internal/store/ ./internal/access/policy/attribute/` | ✅ | ✅ green |
| T2 | 02-13 | 3 | PROFILE-11 | T-02-82, T-02-83, T-02-84, T-02-85, T-02-86, T-02-88, T-02-89 | Three player-keyed peers derived from the ROW never the caller; ALL on the permit side, ANY on the forbid side (D-27); a resolver error omits all three together | unit + integration (tdd) | `task test -- ./internal/access/policy/attribute/` && `task test:int -- ./internal/world/postgres/ ./test/integration/access/` | ✅ | ✅ green |
| T3 | 02-13 | 3 | EXT-07 | T-02-87 | Both `ABACConfig` seams are POPULATED at the production composition root, not merely declared; `principal.viewer.*` resolves in production | integration (tdd) | `task test:int -- ./internal/access/setup/` | ✅ | ✅ green |
| T1 | 02-06 | 4 | IDENT-06 | T-02-75, T-02-76 | `charname.Admitted` has exactly ONE constructor, and the five-rule admission census is demonstrated RED against the real pre-fix writer boundary | meta-test | `task test -- -run 'TestEveryCharacterNameWrite\|TestAdmittedHasExactlyOne\|TestTheSetOfNameAdmitting\|TestAdmissionCensus' ./test/meta/` | ✅ | ✅ green |
| T2 | 02-06 | 4 | IDENT-06, IDENT-09 | T-02-73, T-02-74, T-02-77, T-02-78, T-02-79, T-02-95 | `characters.name` is writable by exactly two methods, both taking the token; `Update` stops writing `name`; `guardSkeleton` serializes the check and the write under a transaction-scoped advisory lock | unit + integration | `task test:int -- ./internal/world/postgres/` | ✅ | ✅ green |
| T3 | 02-06 | 4 | IDENT-06, IDENT-07 | T-02-06, T-02-30, T-02-31, T-02-93, T-02-94 | All three composition roots construct the gate through ONE helper; the production create path and the guest path both reject a block-listed name | integration | `task test:int -- ./test/integration/auth/ ./test/integration/charname/` | ✅ | ✅ green |
| T1 | 02-07 | 4 | PROFILE-11 | T-02-37, T-02-38, T-02-39 | Exactly the rungs with a seeded §8.6 member carry a tier-floor policy; the clearing test is set membership transcribed verbatim, never an ordinal compare or a glob | unit | `task test -- ./internal/access/policy/` | ✅ | ✅ green |
| T2 (checkpoint) | 02-07 | 4 | PROFILE-11 | T-02-41 | The widening's merge gate is fixed BEFORE the permits are authored | checkpoint:decision | — (auto-selected `author-now-gate-on-audit`; D-11's posture already locked) | ✅ | ✅ green |
| T3 | 02-07 | 4 | PROFILE-11 | T-02-40, T-02-41, T-02-42, T-02-95 | Five viewer twins keyed on the DERIVED player peers, never on a character-keyed row field; no viewer seed carries `write` or `delete`; no NEW `resource is character` permit (D-29) | unit | `task test -- ./internal/access/policy/` | ✅ | ✅ green |
| T4 | 02-07 | 4 | EXT-07 | T-02-43, T-02-44 | `seed:admin-section-access` is player-flavored and resource-TYPE-scoped with no enumerated id; every attribute the ten new seeds reference resolves against a REGISTERED provider's declared schema | unit + integration | `task test -- ./internal/access/policy/ ./internal/testsupport/abactest/` && `task test:int -- ./internal/access/setup/` | ✅ | ✅ green |
| T1 | 02-08 | 5 | PROFILE-11 | T-02-45, T-02-49, T-02-50, T-02-51 | §8.5.1's conjunction is TWO `Evaluate` calls ANDed in Go, separated by the action token; reachability is a prior independent call; an infra failure aborts rather than reporting "withheld" | unit | `task test -- ./internal/access/profilevis/` | ✅ | ✅ green |
| T2 | 02-08 | 5 | PROFILE-11 | T-02-47 | A synthetic fourth rung sorting above `guest` in Go byte order clears NEITHER shipped floor | unit | `task test -- -run TierFloor ./internal/access/profilevis/` | ✅ | ✅ green |
| T3 | 02-08 | 5 | PROFILE-11 | T-02-46, T-02-48, T-02-96 | D-04: a `private` row whose floor the viewer clears is ABSENT; §8.6 totality: an unenumerated `profile.*` name is denied, not defaulted | unit | `task test -- ./internal/access/profilevis/` | ✅ | ✅ green |
| T1 | 02-09 | 5 | EXT-07 | T-02-53 | The registry is exactly §10.1's seven ids by set equality; every entry carries a non-empty descriptor derived from its own id; status is a closed vocabulary whose unknown value never reads as available | unit (tdd) | `task test -- ./internal/admin/section/` | ✅ | ✅ green |
| T2 | 02-09 | 5 | EXT-07 | T-02-52, T-02-54, T-02-55, T-02-56, T-02-57, T-02-58, T-02-59 | ABAC is evaluated BEFORE the registry lookup (D-06), so a non-admin's refusal is string-identical across a registered and an unregistered id — proven by a lookup that PANICS if reached | unit (tdd) | `task test -- ./internal/admin/section/` | ✅ | ✅ green |
| T3 | 02-09 | 5 | EXT-07 | T-02-97 | `ValidateAtBoot` has a PRODUCTION call site (step 1 of `BootstrapSubsystem.Prepare`); INV-PRIVACY-11 binds to the assertion that genuinely proves it and nowhere else | unit + integration + meta | `task test -- ./internal/admin/section/ ./test/meta/` && `task test:int -- ./internal/bootstrap/...` | ✅ | ✅ green |
| T1 | 02-10 | 5 | PROFILE-11 | T-02-64, T-02-65, T-02-106, T-02-108 | The audit query is committed, re-runnable and read-only BY CONSTRUCTION; the §8.6 list is re-derived from the SPEC and proven set-equal to `seed.go`'s 23 names; no player-authored text is selected | gate | `rg -v '^\s*--' <both .sql> \| rg -o 'INSERT\|UPDATE\|DELETE\|ALTER\|DROP\|CREATE\|TRUNCATE' \| wc -l` → `0` | ✅ | ✅ green |
| T2 (checkpoint) | 02-10 | 5 | PROFILE-11 | T-02-106 | The evidence-recording scheme is fixed BEFORE the audit runs, so no player text can reach git history | checkpoint:decision | — (**maintainer-selected** `sanitized-ledger`) | ✅ | ✅ green |
| T3 | 02-10 | 5 | PROFILE-11 | T-02-60, T-02-61, T-02-66, T-02-98 | The audit reached a REAL database and its result is recorded with a date, a database identifier and a per-row verdict; an unreachable database is a blocking escalation, never a discharging result | manual (artifact) | `02-AUDIT-RESULT.md` present and non-empty; `rg -io 'could not be run\|unavailable\|connection refused' \| wc -l` → `0` | ✅ | ✅ green |
| T4 (checkpoint) | 02-10 | 5 | PROFILE-11 | T-02-99, T-02-109 | No `remediate` verdict is left unperformed, and no remediation over-reaches the approved set | checkpoint:decision | — (**maintainer-selected** `no-remediation-required`, reflecting a measured zero) | ✅ | ✅ green |
| T4b | 02-10 | 5 | PROFILE-11 | T-02-107 | The recorded verdict carries an approver and a date; a no-remediation verdict performs NO write | gate | `rg -o '^## Remediation verdict$' 02-AUDIT-RESULT.md \| wc -l` → `1` (line-anchored) | ✅ | ✅ green |
| T5 | 02-10 | 5 | PROFILE-11 | T-02-62, T-02-63 | The widening PERMITS the off-location read the colocation policy denied, and the identical call is DENIED against a control corpus excluding it; the `visibility` and `parent_type` guards are pinned on both sides | integration | `task test:int -- ./test/integration/access/` | ✅ | ✅ green |
| T1 | 02-12 | 5 | IDENT-09 | T-02-29, T-02-32, T-02-33, T-02-79, T-02-100, T-02-103 | `SET NOT NULL` precedes `CREATE UNIQUE INDEX` so a unique index over an unbackfilled column cannot silently enforce nothing; the Go migration halts on BOTH normalized-name and skeleton collision sets | integration | `task test:int -- ./internal/store/ ./internal/world/postgres/` | ✅ | ✅ green |
| T2 | 02-12 | 5 | IDENT-09 | T-02-34, T-02-35, T-02-36, T-02-78, T-02-102 | The operator's replacement name routes through the SAME `charname.Gate` the create path uses and writes through `Rename`; no hand-written SQL path exists; the command is reachable when the server refuses to start | integration | `task test:int -- ./cmd/holomush/` | ✅ | ✅ green |
| T3 | 02-12 | 5 | IDENT-09 | T-02-29, T-02-101, T-02-105 | The DATABASE refuses a second row holding one uniqueness key, demonstrated RED against the schema staged at `000055`, with a staging precondition that tells a mis-staging from the demonstration | integration | `task test:int -- ./test/integration/charname/` | ✅ | ✅ green |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

**40 task rows across 12 plans.** Every row's command was observed exiting 0 by the plan that owns
it, and the whole set was re-observed green by this plan's closing `task test` / `task test:int` /
`task lint` / `task fmt:check` sweep.

---

## RESEARCH test-map reconciliation

`02-RESEARCH.md` § Validation Architecture → "Phase Requirements → Test Map" is the row set every
task here must satisfy.

**It has 30 rows, not 31.** Both this file's earlier draft and plan `02-11`'s text said 31; the
count was re-derived mechanically
(`sed -n '1224,1256p' 02-RESEARCH.md | rg '^\| ' | rg -v 'Req / criterion' | rg -v '^\|---' | wc -l`
→ **30**). Recorded rather than papered over: an inherited count nobody re-derived is exactly the
kind of number that makes a coverage claim unfalsifiable.

| # | RESEARCH row | Lands on | Status |
|---|---|---|---|
| 1 | IDENT-06 / crit. 1 — NFKC-collapsible pair rejected at create **and** rename | 02-01 T2, 02-06 T2/T3 | ✅ |
| 2 | IDENT-06 / crit. 1 — `Cf`-padded name normalizes identically | 02-01 T2 | ✅ |
| 3 | IDENT-06 / crit. 1 — Latin+Cyrillic rejected; Latin+Han+Hiragana permitted (all 8 rows) | 02-02 T1 | ✅ |
| 4 | IDENT-06 / crit. 1 — whole-Cyrillic homoglyph rejected by skeleton | 02-01 T2 (retargeted by 02-02 T1 to a genuine whole-script pair) | ✅ |
| 5 | IDENT-06 — name normalizing to empty is rejected | 02-01 T2, 02-02 T2 | ✅ |
| 6 | IDENT-06 / D-23 — stored skeleton carries the pinned `UnicodeVersion` | 02-01 T3 | ✅ |
| 7 | IDENT-07 / crit. 1 — block-list pattern rejects at create **and** rename | 02-05 T1/T3, 02-06 T3 | ✅ |
| 8 | IDENT-07 / D-15 — an uncompilable pattern is a hard startup failure naming the entry | 02-05 T1/T2 | ✅ |
| 9 | IDENT-07 / D-16 — reload failure leaves the last valid list in force | 02-05 T2 | ✅ |
| 10 | IDENT-08 / crit. 3 — non-ASCII / leading-non-letter usernames still rejected | 02-02 T3 | ✅ |
| 11 | IDENT-09 / crit. 2 — two concurrent claims: exactly one succeeds | 02-12 T3 | ✅ |
| 12 | IDENT-09 / crit. 2 — gate demonstrated RED against today's unindexed schema | 02-12 T3 | ✅ |
| 13 | IDENT-09 / D-19 — synthetic collisions incl. an NFKC-only pair detected; index applies cleanly after | 02-12 T3 | ✅ |
| 14 | IDENT-09 / D-22 — collision → migration error naming every set; rollback; schema at prior version | 02-12 T1/T3 | ✅ |
| 15 | PROFILE-11 / crit. 4 — off-location viewer reads public properties **and the in-world description** | 02-10 T5 (**property half only**) | ⚠️ **partial — see below** |
| 16 | PROFILE-11 / crit. 4 — paired positive control (same fixture denied before the widening) | 02-10 T5 (control corpus excluding the permit by name) | ✅ |
| 17 | PROFILE-11 / D-12 — audit query committed and its result recorded | 02-10 T1/T3/T4b | ✅ |
| 18 | EXT-07 / crit. 5 — permits admin, denies builder / plain player / guest across all seven ids | 02-09 T2 | ✅ |
| 19 | EXT-07 / crit. 5 — paired positive control per denial | 02-09 T2 | ✅ |
| 20 | EXT-07 / crit. 5 — id list asserted by set equality, not membership | 02-09 T1 | ✅ |
| 21 | EXT-07 / crit. 5 — an eighth section needs no new policy | 02-09 T2 | ✅ |
| 22 | D-06 / D-07 (INV-PRIVACY-11) — refusal byte-identical across a registered and an unregistered id | 02-09 T2/T3 | ✅ |
| 23 | D-09 — boot validator refuses a zero-valued descriptor; meta-test asserts every entry non-zero | 02-09 T1/T3 | ✅ |
| 24 | D-03 / §8.2.1 — synthetic fourth tier does not clear a floor, RED against an ordinal impl | 02-08 T2 | ✅ *(discriminator moved from the `player` floor to the `guest` floor — see below)* |
| 25 | D-04 — `private` row + a clearing tier ⇒ attribute absent | 02-08 T3 | ✅ |
| 26 | §8.6 totality — a `profile.*` name in no §8.6 row is denied, not defaulted | 02-08 T3 | ✅ |
| 27 | INV-WORLD-5 — character constructed directly in `idle` excluded; reads exhaustive with denying default | 02-04 T1/T2 | ✅ |
| 28 | INV-WORLD-6 — retire does not release the name; the delete path does (paired) | 02-04 T2 | ✅ |
| 29 | §8.4.1 obligation 1 — `warnOnMissingSeedCoverage` does not WARN for `viewer` | 02-07 T4 (with `02-13` T3 supplying the registration) | ✅ |
| 30 | §8.5 obligation — `PropertyProvider` still emits `name` | 02-13 T2 | ✅ |

**29 of 30 rows land fully. One (row 15) lands partially**, and it is a deliberate scope reduction
rather than a coverage gap — see the unmet-criterion section. **No row is unmapped, so no GitHub
issue was filed for an unmapped row.**

**Row 24 — the discriminator moved, and the move is the point.** RESEARCH phrased the fourth-rung
assertion against a **`player`** floor. Plan `02-07` seeds no `player`-rung policy (§8.6's seeded
defaults place every governed row at `anonymous` or `guest`, so that rung has no member — see
`02-CONTEXT.md` D-03), so an assertion phrased against one asks the engine about a policy that does
not exist and reports DENY under **both** implementations: a gate that cannot fail, wearing the
clothes of the sharpest gate in the phase. Plan `02-08` asserts at the **`guest`** floor instead
(`"spectator" >= "guest"` is TRUE in Go byte order, and a `guest`-rung policy actually exists) and
additionally at `anonymous`, so the property proven is *"a newly appended token clears **nothing**"*
rather than *"it fails to clear one particular rung"*. This is a **strengthening** of RESEARCH's row,
recorded so it is not read as a substitution.

---

## RED-first demonstrations (PORTAL-10 rule 4)

**The count is DERIVED from the plans, not fixed here.** Every plan's
`<verification_integrity>` rule 4 was walked and every demonstration it names emitted one row.
The earlier draft named three; the plans name **49**.

Each row records an **observed** non-zero exit or an observed failure verdict. **No row is written
in the conditional past tense** — a demonstration asserted rather than run is itself a finding, and
there are none. *(Stated this way on purpose: a substring gate over this document is disarmed by any
mention of its own trigger, including a sentence whose whole point is that the trigger is absent.
That is `02-10`'s Deviation 1, and `02-09` resolved the same collision by naming the property
instead of spelling it. The gate is a case-insensitive `rg -o … | wc -l` for the three-word
conditional-past phrase over this whole file, compared `-eq 0` — and it counts **0**, file-wide
rather than prose-stripped, which is the stronger of the two available resolutions.)*

| # | Plan | Gate | RED against | Observed |
|---|---|---|---|---|
| 1 | 02-01 | Generated-table drift | `UnicodeVersion` hand-edited 17.0.0 → 16.0.0 in the committed table | exit **201** — "the generator pins Unicode 17.0.0 but the committed table declares 16.0.0" |
| 2 | 02-01 | Confusable check | `SkeletonExists` replaced with a constant `false, false, nil` | exit **201** — 3 named failures (refusal, self-exclusion, fail-closed) |
| 3 | 02-01 | Generator input digest | digest comparison short-circuited to `false && …` | exit **201** |
| 4 | 02-02 | `MixedScript` at the gate | pre-Task-1 `Gate.Check` (no mixed-script step) | exit **1** — the gate **accepted** `раypal` |
| 5 | 02-02 | `MixedScript` unit surface | symbols undefined | exit **1** — 9 compile errors |
| 6 | 02-02 | Empty-normal-form split | pre-fix single message | exit **201** |
| 7 | 02-02 | `charname` → `auth` import guard | synthetic package with a planted import | in-suite — planted file flagged, clean file not |
| 8 | 02-02 | `player.go` → `charname` guard | synthetic `player.go` calling `Normalize` via a same-file helper | in-suite — `charname` detected in the call graph |
| 9 | 02-02 | Whole-script confusable spec | seeded name changed to a non-colliding one | exit **201** — 3 Ginkgo specs |
| 10 | 02-02 | `task fmt:check` | HEAD as committed by 02-01 | non-empty — `gofumpt -l .` listed the generated table |
| 11 | 02-03 | Known-prefix table + constructors | constants not yet existing | exit **201** |
| 12 | 02-03 | `ViewerTierProvider` surface | provider not yet existing | exit **201** |
| 13 | 02-04 | INV-WORLD-5 | `world.Selectable` gate removed from `auth_handlers.go` | `Ran 92 of 92 Specs … FAIL!` attributed to INV-WORLD-5 |
| 14 | 02-04 | INV-WORLD-6 | `mutator.deleteCharacter` short-circuited | `Ran 92 of 92 Specs … FAIL!` attributed to INV-WORLD-6 |
| 15 | 02-05 | Block-list rejection at the gate | pre-Task-1 `Gate.Check` (no `BlockList` field) | exit **1** — 5 compile errors |
| 16 | 02-05 | Reload preserves the last valid list | `Reload` swaps an empty snapshot in BEFORE compiling | exit **201** — `"admin" must still be blocked by the last valid list` |
| 17 | 02-05 | Malformed ≠ absent | `Load` short-circuited to `StringSliceN`'s lenient `(nil,nil)` | exit **201** — 8 failures |
| 18 | 02-05 | Gate sees the reload | gate handed a snapshot instead of the `*Cache` | exit **201** — `the SAME gate instance must now reject it` |
| 19 | 02-05 | Direct-SQL edit is observed | poller compares `updatedAt` only | exit **201** — only that one spec, which is the point |
| 20 | 02-05 | Bootstrap dependency edge | edge removed from `DependsOn()` | exit **201** |
| 21 | 02-05 | Block list at the integration tier | gate's block-list step disabled | exit **201** — 4 Ginkgo specs |
| 22 | 02-05 | Direct-SQL edit, integration tier | poller compares `updatedAt` only | exit **201** — `Timed out after 10.000s`, every other spec green |
| 23 | 02-05 | Malformed edit, integration tier | lenient `Load` | exit **201** |
| 24 | 02-06 | The five-rule admission census | **the tree as it actually stood** — `Update` still wrote `name` ungated | exit **201** — rules A, B, D and E all fired; rule C green and non-vacuous |
| 25 | 02-07 | Corpus-compiles smoke | closing brace dropped from a seed's `when` clause | exit **201** — surfaced at `Cache.Reload` |
| 26 | 02-07 | Attribute-reference coverage | `principal.viewer.tier` → `principal.viewer.teir` | exit **201** — named the undeclared key and listed the supplied ones |
| 27 | 02-07 | Seed-coverage `viewer` no-WARN | `RegisterProvider(viewerProvider)` removed from `BuildABACStack` | exit **201** — the WARN fired naming all seven viewer-referencing seeds |
| 28 | 02-07 | S3b `parent_type` guard *(earned, not required)* | `&& resource.property.parent_type == "character"` dropped | exit **201** — the widening would have published public properties on **locations** |
| 29 | 02-08 | Fourth-rung clearing | both tier floors rewritten to `>= "anonymous"` / `>= "guest"` | exit **1** — `spectator` cleared **both** shipped floors; both paired controls stayed green |
| 30 | 02-08 | D-04 additive permit | `return clearsFloor && rowPermits` → `return clearsFloor` | exit **201** — the `private` row **published**, plus the `admin` row and both D-27 denials: five exposures from a two-line change |
| 31 | 02-08 | §8.6 totality | the 22-name literal list replaced by `like "profile.*"` | exit **201** — `profile.image.gallery.10` **published**, and so did an arbitrary unenumerated name; `.09` control green |
| 32 | 02-08 | Conjunction evaluator (Task 1) | stub implementation | **19 assertion failures across 23 tests** — behavioural, not a compile error |
| 33 | 02-09 | Id set-equality (extra) | `entry("billing", StatusPlanned)` added to the shipped registry | exit **201** — reported as EXTRA |
| 34 | 02-09 | Id set-equality (missing) | `entry("plugins", …)` deleted | exit **201** — reported as MISSING |
| 35 | 02-09 | Byte-identical refusal | registry lookup moved BEFORE the `Evaluate` call (the pre-D-06 shape) | exit **201** — RED for all three non-admin fixtures, and the poisoned lookup fired |
| 36 | 02-09 | Boot validator | a zero-valued descriptor placed in the SHIPPED registry | exit **201** — aborts naming the offending entry |
| 37 | 02-09 | Registry surface (Task 1) | symbols not yet existing | exit **201** |
| 38 | 02-09 | Gate surface (Task 2) | `AssertSectionAccess` not yet existing | exit **201** |
| 39 | 02-09 | Boot surface (Task 3) | `ValidateAtBoot` not yet existing | exit **201** |
| 40 | 02-10 | W1's paired control | control expectation flipped `DefaultDeny` → `Allow` | exit **201** — effect `0` vs `1`; that corpus **is** the tree as it stood before 02-07 |
| 41 | 02-10 | Control non-vacuity guard | excluded policy name misspelled | exit **201** — all 6 specs fail in `BeforeEach` on `removed == 1` |
| 42 | 02-12 | Name-uniqueness gate | the schema staged at `000055` (every migration except `000056`) | exit **201** — the duplicate **succeeded**; the inverted case ships permanently as the paired control |
| 43 | 02-12 | Staging precondition | `WithDisableGlobalRegistry(true)` added, which drops the registered Go migration 55 | exit **201**, failing as a **SETUP error** rather than as the demonstration — which is what it exists to do |
| 44 | 02-12 | `Normalize` concurrency *(post-landing defect)* | the shipped shared `x/text` transformers | exit **201** — three concurrent panics, `slice bounds out of range` in `transform.String`; `TestSkeletonIsSafeForConcurrentUse` passed in the same run as the paired control |
| 45 | 02-13 | Emitted-vs-declared schema coherence | `owner_player_id` deleted from `Schema()` while still emitted | exit **201** — `Should be empty, but was [owner_player_id]` |
| 46 | 02-13 | Derivation direction (D-27) | the ALL branch replaced by the plain union | exit **201** — the permit-side widening the union would have introduced |
| 47 | 02-13 | Player-roles seam (Task 1) | `PlayerRoles` / `WithPlayerRoleLookup` not yet existing | non-zero (build failure) |
| 48 | 02-13 | Derived peers (Task 2) | `NewPropertyProvider` signature not yet widened | exit **201** |
| 49 | 02-13 | Composition-root seams (Task 3) | `ABACConfig` fields absent | exit **201** |

**Derived count: 49.** Per-plan: 02-01 → 3, 02-02 → 7, 02-03 → 2, 02-04 → 2, 02-05 → 9,
02-06 → 1, 02-07 → 4, 02-08 → 4, 02-09 → 7, 02-10 → 2, 02-12 → 3, 02-13 → 5.

**Every mutation was reverted and the file verified byte-identical to HEAD before its commit**
(`git diff --exit-code`), which is what stops a demonstration from shipping as a regression.

**Rule 2 (paired positive control on every denial test) is discharged**, and where it is *not*
applicable the plan says so rather than omitting the claim: `02-07` ships no denial tests and
records that its absence does not satisfy the rule; `02-11` ships no tests at all and records the
same. The plans that DO ship denial tests — `02-02`, `02-04`, `02-05`, `02-06`, `02-08`, `02-09`,
`02-10`, `02-12` — each carry the pairing, and two are worth naming because the pairing is what
makes them non-vacuous at all: `02-08`'s discriminating forbid fixture (`visible_to = [C1, C2]`
**and** `excluded_from = [C1]`, so the permit genuinely fires and the forbid is the only thing
denying) and `02-09`'s poisoned-lookup ordering proof (paired with a permitted caller who **does**
panic, so the no-panic result is ordering rather than an unreachable seam).

---

## Unmet criteria and deliberate deferrals

| Item | Owner | Why it is not discharged in Phase 2 |
|---|---|---|
| **ROADMAP success criterion 4 — the "in-world description" half** | **Phase 4** | `02-CONTEXT.md` **D-29** defers `seed:profile-public-read-character` — the `resource is character` permit that would carry `characters.description` — because it also gates `world.Service.GetCharacter`, whose `characterToProto` projection returns `PlayerId` and `LocationId`, and whose `principal is character` test admits **every ephemeral guest**. An unconditional permit would let any guest enumerate alt-to-player linkage and live grid position for the whole roster. It lands with the projection narrowing that makes it safe. **This is NOT an instance of D-10/D-11:** those govern `entity_properties` rows, which carry a `visibility` column, and D-11's mandated remedy ("change the row's `visibility`") is what makes that widening acceptable — `characters` has no `visibility` column, so the escape hatch does not exist for this resource. **Criterion 4's `entity_properties` half IS discharged** (permit seeded by `02-07`, proven to widen by `02-10` T5, real-data exposure audited and recorded by `02-10` T3/T4b). |
| **PROFILE-11 — the `characters.description` half** | **Phase 4** | Same cause. PROFILE-11's scope covers both halves; only the `entity_properties` half closes here. |
| **INV-ACCESS-10, INV-ACCESS-11, INV-PRIVACY-9, INV-PRIVACY-10** | **Phase 4** | §13 places their binding against the read path and its **marshaled response**. Phase 2 ships no RPCs, so the surface they describe does not exist. Annotating an engine-decision helper would bind them to the wrong thing — the false-green the binding ratchet exists to catch. All four remain `binding: pending` with **no** `asserted_by`. |
| **INV-ACCESS-12 (character read-surface census)** | **Phase 4** | Derives from generated service descriptors; no v0.13 RPCs exist yet. `pending`, no `asserted_by`. |
| **INV-WORLD-7 (guarded character mutation)** | **Phase 4** (commands land in **Phase 3**) | `pending`, no `asserted_by`. |
| **§10.2's endpoint-level denial test** | **Phase 4** (D-08) | Phase 2 ships the shared helper and proves the gate at helper level with paired positive controls; the endpoint form is unwritable without RPCs. |
| **EXT-04 — the registry ↔ authorization-descriptor census** | **Phase 6** (§12.2) | `02-09` ships a narrower **id** set-equality meta-test over its own registry and explicitly does **not** claim EXT-04. Phase 2's own rule-1 census scope is `02-06`'s character-name admission census. |
| **INV-ACCESS-11's wire-level absence assertion** | **Phase 4** | No wire surface in this phase. |
| **`ADMIN_SECTION_EVALUATION_FAILED` has no test** | **Phase 4** | Driving it needs a stub engine, and `02-09`'s suite deliberately uses only the real engine (a canned-decision double would make every assertion a test of the double's answer). Recorded by `02-09` as a residual, not a defect. |
| **`02-10`'s per-row verdict machinery is unexercised** | re-run | The `in_spec_86` split, the two verdict vocabularies and the digest-based re-check have never adjudicated a real row, because the audited corpus had **zero**. The structure is verified; the behaviour on a populated corpus is not. The query is committed and re-runnable for exactly that. |
| **`02-10`'s B-14 "strictly greater" fixture check** | re-run | On a corpus where set 3 and set 6 are both `0`, `>=` holds and strict `>` cannot. The structural half IS satisfied and verified. |
| **`000055` / `000056` were not rehearsed against real data** | knowingly accepted | The audit needed no column from them, so the restored corpus was audited AS-IS at goose level 53. On a 3-character corpus D-22's duplicate detection would have been a weak rehearsal in any case — but it is a real thing this phase did not buy. |
| **The `02-10` "still publishes `name`" obligation** | **Phase 4** | A **Phase-4 row**, not a Phase-2 gap: §8.8's minimum-identity floor binds against the marshaled projection (INV-PRIVACY-10), which does not exist until the read path does. `02-08` ships its precondition — an anonymous viewer receiving a public `profile.pronouns`, paired with a guest-floor negative on the same rung. |

**`criterion 4` — maintainer decision item.** `.planning/ROADMAP.md`'s success criterion 4 **as it
currently stands already carries the D-29 deferral** ("The **in-world-description half is deferred
to Phase 4** by D-29"). Plan `02-11` was written against the pre-amendment wording and instructed to
escalate the scope question with three options and select none; the escalation and the observed
state are both recorded in `02-11-SUMMARY.md`. **`.planning/ROADMAP.md` was not edited by this
plan, in either direction** — the orchestrator owns ROADMAP updates through `gsd roadmap`, and
inventing or amending structure in a tool-owned parsed file silently changes what the tool sees.

**Criterion 1 is explicitly NOT in the same position and MUST NOT be touched.** `02-CONTEXT.md`
**D-30** records that with all three of its parts shipped — the skeleton index stays non-unique, the
check and the write are serialized under a transaction-scoped advisory lock, and `000055`'s
detection also scans skeleton collisions — criterion 1's "rejected server-side" is true for
concurrent writers **and** for the pre-existing corpus, so **its wording needs no amendment**.

---

## Wave 0 Requirements

- [x] `internal/charname/` pipeline + `internal/charname/syntax` — IDENT-06 pipeline, mixed script, empty normal form *(landed as a dependency-free leaf per D-28, not in `internal/world/`)*
- [x] `internal/charname/skeleton_test.go` + generated-table drift meta-test — IDENT-06, D-23
- [x] `internal/charname/blocklist/*_test.go` — IDENT-07, D-15, D-16
- [x] `internal/access/profilevis/tierfloor_test.go` + `profilevis_test.go` — D-03, D-04, §8.2.1 fourth rung, §8.6 totality *(the behavioural proofs live in the conjunction helper's package, not in `internal/access/policy/`, because that is where the caller-side AND lives)*
- [x] `internal/admin/section/*_test.go` — EXT-07 criterion 5, D-06, D-07/INV-PRIVACY-11, D-09
- [x] `test/integration/world/character_lifecycle_test.go` — INV-WORLD-5, INV-WORLD-6
- [x] `test/integration/charname/name_uniqueness_test.go` + `name_duplicates_test.go` + `name_write_window_test.go` — criterion 2 concurrency, D-19 synthetic collisions, D-22 rollback
- [x] Extend `test/integration/access/seed_policies_test.go` — criterion 4 plus its paired positive control *(S3/S3b; the dedicated `profile_public_read_test.go` carries W1–W6)*
- [x] Extend the seed smoke coverage — a viewer provider double beside `characterProvider` *(landed as `internal/testsupport/abactest` plus an external `package policy_test` file; the in-package form is an import cycle — see `02-07` deviation 2)*
- [x] Extend `internal/auth/player_test.go` — IDENT-08 regression pin for `^[a-zA-Z][a-zA-Z0-9_]*$`
- [x] `02-AUDIT-profile-public-read.sql` + `02-AUDIT-RESULT.md` — D-12 exposure audit artifact *(plus `02-AUDIT-detail-operator-only.sql`, whose FILE is committed and whose OUTPUT never is)*
- [x] Framework install: **none needed** — Ginkgo, Gomega, testify, testcontainers all present

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions | Outcome |
|----------|-------------|------------|-------------------|---------|
| `seed:profile-public-read` exposure audit | PROFILE-11 / D-12 | A **data** question about live rows, not a design one — no assertion can stand in for reading what is actually there | Run the committed read-only query from `02-AUDIT-profile-public-read.sql` against the target database; record the result in `02-AUDIT-RESULT.md`. The widening MUST NOT merge before this exists and is non-empty. | ✅ **Discharged 2026-08-05.** kopia snapshot `7e48a9b592c2e0d302a5da3cf0171835` restored to a throwaway PostgreSQL 18.4 container; **0** public `parent_type='character'` rows (`entity_properties` is empty in its entirety); both detail sets returned zero rows, so there was no player text to adjudicate and none to leak. A legitimate zero **from a reached database** — the T-02-98 escalation actually fired and halted the plan for a session rather than accept an unreachable one. |
| The duplicate report against a real database | IDENT-09 / D-22 | There is no production database in this worktree and the phase has not shipped | Before `000056` is applied to any database holding real characters, run `holomush character name duplicates` and resolve every reported set | ⏳ **Operator obligation at deploy time**, recorded in `000056`'s header, not only here. `000055`'s own halt is the backstop if the step is skipped. |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 60s on the untagged lane
- [x] All rows of RESEARCH § "Phase Requirements → Test Map" land on a task — **30 rows** (re-derived; the inherited "31" was wrong), 29 fully and 1 (row 15) partially by D-29's deliberate deferral. None unmapped, so none filed.
- [x] All RED-first gates observed failing before their fix — **49 demonstrations**, count derived from the plans rather than fixed in prose. Every row records an observed outcome; none is written in the conditional past tense.
- [x] `nyquist_compliant: true` set in frontmatter — **observed, not asserted.** Docker was available for every plan that needed it (`02-01`, `02-05`, `02-06`, `02-12` each record it explicitly), every plan-level `task test:int` exited 0, and `02-11` re-ran the full integration lane at the phase gate. No integration criterion was left unproven for want of an environment.

**Approval:** validated by plan `02-11` (phase closeout), 2026-08-05.
