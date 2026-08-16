# Phase 2: ABAC & Schema Vocabulary - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-08-02
**Phase:** 2-ABAC & Schema Vocabulary
**Areas discussed:** Term B request shape, D1 denial-code oracle, profile-public-read audit, Block-list mechanism, Duplicate resolution rule, `last_active_at` column, Migration split shape

**Area selection:** Eight gray areas were offered; seven were selected. "Unicode
library + version pin" was not selected and was assigned to Claude's discretion —
recorded in CONTEXT.md as a research question deliberately left to the planner's
researcher rather than guessed at.

---

## Term B request shape

Analysis presented before the question: §01-SPEC §8.5.1.1 offers two shapes.
Option 2 was shown to violate §8.8 / INV-PRIVACY-10 for the `anonymous` rung,
because `profile.pronouns` is an `entity_properties` row seeded at the `anonymous`
floor, and an anonymous viewer has no character for term B to evaluate against.

| Option | Description | Selected |
|--------|-------------|----------|
| Viewer read-policy twins only | Read-side subset of `seed:property-*` gets `principal is viewer` twins; `seed:property-owner-write` gets none | ✓ |
| Viewer twins for all six | Mirror all six policies verbatim into the viewer namespace | |
| Co-located char subject, DENY if none | §01-SPEC's literal option 2; requires amending §8.8/§8.6 | |

**User's choice:** Viewer read-policy twins only
**Notes:** Recommended option. The write-side exclusion is load-bearing — a `viewer:` subject must never hold a write permit.

| Option | Description | Selected |
|--------|-------------|----------|
| Additive-permit regression test | Private row + clearing tier → assert absent; goes RED if term B is dropped | ✓ |
| Test plus a registry invariant | The test plus a new INV-ACCESS entry | |
| Rely on the SPEC text | §8.5.1.1 states the prohibition; code review is the control | |

**User's choice:** Additive-permit regression test

| Option | Description | Selected |
|--------|-------------|----------|
| One policy per floor value | Three policies, each with a literal name list at that floor | ✓ |
| One policy per attribute name | ~28 policies, one per governed name | |
| One policy, name→floor map | Single policy carrying the whole §8.6 table | |

**User's choice:** One policy per floor value

| Option | Description | Selected |
|--------|-------------|----------|
| Amend §8.5.1.1 in this phase | Record option 2 as rejected, following §14 amendment discipline | |
| Amend plus route to abac-reviewer | The amendment, plus a review gate before merge | ✓ |
| CONTEXT.md only | Leave 01-SPEC.md untouched | |

**User's choice:** Amend plus route to abac-reviewer
**Notes:** Chose the stronger of the two amendment options — `abac-reviewer` found the original §8.5.1.1 residual, so it verifies the rejection does not trade one hole for another.

---

## D1 denial-code oracle

| Option | Description | Selected |
|--------|-------------|----------|
| Gate first, then distinguish | Evaluate the policy before the registry lookup; denied callers get one code | ✓ |
| Collapse to one code entirely | Drop `DENY_ADMIN_SECTION_UNREGISTERED` | |
| Keep both, document the tradeoff | Accept the oracle as low-severity | |

**User's choice:** Gate first, then distinguish
**Notes:** Works because `seed:admin-section-access` is scoped by resource type, so an unregistered id is still covered by the policy.

| Option | Description | Selected |
|--------|-------------|----------|
| Yes — new INV-PRIVACY entry | Parallels INV-PRIVACY-9; needs hand-registration | ✓ |
| Test only, no invariant | Ship the test without a registry id | |
| You decide | Let the planner settle it | |

**User's choice:** Yes — new INV-PRIVACY entry

| Option | Description | Selected |
|--------|-------------|----------|
| Helper-level now, endpoint-level Phase 4 | Two non-vacuous tests; Phase 2's gate proven in Phase 2 | ✓ |
| Defer the whole test to Phase 4 | Matches §10.2's literal wording | |
| Both, plus a bridging meta-test | Adds set-equality coverage over the helper suite | |

**User's choice:** Helper-level now, endpoint-level Phase 4

| Option | Description | Selected |
|--------|-------------|----------|
| Boot validation + meta-test | Boot validator refuses zero-valued descriptors; meta-test in CI | ✓ |
| Unexported field + constructor | Pushes closer to compile-time; costs the readable literal | |
| Both | Constructor, boot validation, and meta-test | |

**User's choice:** Boot validation + meta-test

---

## profile-public-read audit

| Option | Description | Selected |
|--------|-------------|----------|
| Scope to §8.6's enumerated names | Exposure bounded by construction; audit becomes confirmation | |
| Widen all public character rows | Any `parent_type='character'` row at `visibility='public'` | ✓ |
| Widen all, minus a denylist | Broad widening with explicit forbids | |

**User's choice:** Widen all public character rows
**Notes:** Chose against the recommendation. Follow-up analysis established that web exposure stays bounded regardless (term A denies any name in no §8.6 row), so the real consequence is on the grid read path — which prompted the fourth question below.

| Option | Description | Selected |
|--------|-------------|----------|
| Committed read-only query + recorded result | Re-runnable; recorded count is the evidence | ✓ |
| Ad-hoc query, result in the summary | Less machinery, unverifiable afterwards | |
| Query plus a standing meta-test | Turns the audit into a durable guarantee | |

**User's choice:** Committed read-only query + recorded result

| Option | Description | Selected |
|--------|-------------|----------|
| Confirm — ship at anonymous | §8.11 already records this as a deliberate divergence | ✓ |
| Reconsider — raise to guest | Tightens toward grid parity; needs a §14 amendment | |
| Audit the descriptions first, then decide | Decide on evidence | |

**User's choice:** Confirm — ship at anonymous

| Option | Description | Selected |
|--------|-------------|----------|
| Intended — public means public on the grid too | The colocation restriction was the anomaly | ✓ |
| Not intended — split web from grid | Restrict the widening to the `viewer:` subject | |
| Intended, but stage it | Two behavior changes, two reviews | |

**User's choice:** Intended — public means public on the grid too
**Notes:** Chose against the recommendation, deliberately. Consequence recorded: the audit's job is to catch rows relying on colocation as de-facto privacy, and the fix for any such row is to change the row's `visibility`, never to narrow the policy.

---

## Block-list mechanism

| Option | Description | Selected |
|--------|-------------|----------|
| settings game scope, `core.*` key | Reuses `SetStringSlice` + `holomush_system_info`; no new table | ✓ |
| A dedicated blocklist table | Self-documenting; new table for one consumer | |
| Compiled-in default + settings override | Never empty; two sources of truth | |

**User's choice:** settings game scope, `core.*` key

| Option | Description | Selected |
|--------|-------------|----------|
| Refuse to start, log the bad pattern | Boot-time compile; operator finds their own error | ✓ |
| Skip the bad pattern, log loudly | Stays up; fails open on that pattern | |
| Reject the write at set time | No write path exists in v0.13 to guard | |

**User's choice:** Refuse to start, log the bad pattern

**Superseded question — caching.** An initial question offered
invalidate-on-write / read-per-validation / short-TTL, and "Cache compiled,
invalidate on write" was selected. That answer was then found to conflict with
the absence of any in-process write path (v0.13 ships no editing surface, so
edits are direct SQL and nothing observes them). The question was reframed after
discovering the repo's own pattern — `internal/access/policy/cache.go` plus
`internal/access/policy/poller.go` — and re-asked:

| Option | Description | Selected |
|--------|-------------|----------|
| Mirror cache + poller | Compiled snapshot refreshed by a version poll on `updated_at` | ✓ |
| Compile at boot, restart to apply | Simplest; not live | |
| Read and compile per validation | Matches existing settings usage; live by construction | |

**User's choice:** Mirror cache + poller
**Notes:** Supersedes the earlier invalidate-on-write answer. Polling is correct here for two independent reasons — no in-process writer to hook, and multi-replica convergence that an in-process invalidation cannot provide.

---

## Duplicate resolution rule

| Option | Description | Selected |
|--------|-------------|----------|
| Halt and report, no auto-resolution | Operator decides per collision, as §6.3 anticipates | ✓ |
| Oldest `created_at` keeps it | Deterministic, fully automatable | |
| Most-recently-active keeps it | Not currently derivable — `last_active_at` does not exist yet | |

**User's choice:** Halt and report, no auto-resolution

| Option | Description | Selected |
|--------|-------------|----------|
| Operator supplies the new name | Validated through the full pipeline + block list | ✓ |
| Auto-suffix with a short id fragment | Never collides; machine-generated name reaches a player | |
| Placeholder + forced rename on next login | Needs a flow that does not exist until Phase 3 | |

**User's choice:** Operator supplies the new name

| Option | Description | Selected |
|--------|-------------|----------|
| Synthetic-collision integration test | Includes an NFKC-only pair the old check could not catch | ✓ |
| Record the zero result and move on | Index creation is the backstop; fails during a deploy window | |
| Synthetic test plus a dry-run mode | Adds operator preview surface | |

**User's choice:** Synthetic-collision integration test

---

## `last_active_at` column

| Option | Description | Selected |
|--------|-------------|----------|
| Yes — column only, no writer | Schema primitive here, write seam in Phase 3 | ✓ |
| Yes — column and writer together | Pulls Phase 3's hot-write hazard analysis forward | |
| No — defer both to Phase 3 | Splits Phase 2's DDL across phases | |

**User's choice:** Yes — column only, no writer

| Option | Description | Selected |
|--------|-------------|----------|
| Nullable, NULLS LAST on sort | NULL means never; diverges from §4.1's NOT NULL posture | |
| NOT NULL DEFAULT 0 | Mirrors §4.1 exactly; 0 is a sentinel | ✓ |
| NOT NULL, default to `created_at` | No sentinel; asserts activity that never happened | |

**User's choice:** NOT NULL DEFAULT 0 — "but use the epoch timestamp"
**Notes:** Clarified that `0` **is** the Unix epoch and is the never-active sentinel. Consequences recorded in CONTEXT.md: `0` needs a named constant, and any "last active" rendering must special-case it rather than showing 1970.

| Option | Description | Selected |
|--------|-------------|----------|
| Amend both now, with the SPEC edits | Fold A1 and A2 into the same amendment pass as §8.5.1.1 | ✓ |
| Amend `last_active_at` only | Leaves A2's discrepancy open across two phases | |
| Defer both to Phase 4 | Column ships whose permitted use the SPEC forbids | |

**User's choice:** Amend both now, with the SPEC edits

---

## Migration split shape

This area took three passes. The first two questions were withdrawn for
clarification and are recorded because the reasoning changed materially.

**Pass 1 (withdrawn).** Offered two-migrations-with-job-between /
one-per-concern / all-DDL-then-later-release. User response: *"1 makes sense, but
what does this look like for users/deployers? can we not automate the backfill as
part of the migration work (migration post hook or something?)"*

Investigation established: startup applies **all** migrations via `Up()` inline
(`cmd/holomush/core.go:279-282` → `internal/bootstrap/migration.go:65`) strictly
before any bootstrap step (`orch.StartAll`, `core.go:857`), so a same-release
migration depending on a Go backfill is unreachable — the job that would unblock
the migration cannot run because the migration blocks the boot.

**Pass 2 (withdrawn).** Reframed around a two-release shape. User asked to look
beyond current usage: *"does the migration framework we're using support per
migration callbacks/hooks? are there others that might? is there an extensible
way to do so?"*

Investigation established: golang-migrate v4.19.1 has **no** hook/callback/event
surface (verified against module source — zero matches). Its extension points are
the `source.Driver` and `database.Driver` interfaces (`database/driver.go:45-82`)
plus `migrate.NewWithInstance` (`migrate.go:171`). `Migrator.Migrate(version uint)`
already exists (`internal/store/migrate.go:118`) and is on `migrateIface`, but no
caller wires it. goose supports Go migrations natively
(`AddMigrationContext` / `AddMigrationNoTxContext`, transaction-controlled,
interleaved by version with SQL). `sql-migrate`, `dbmate` and `tern` are SQL-only;
`atlas` and `bun/migrate` also support Go steps.

**Pass 3 (answered).**

| Option | Description | Selected |
|--------|-------------|----------|
| Staged runner, Go steps keyed by version | Uses the existing `Migrate(version)`; no dependency change | |
| Switch to goose | Go migrations first-class; costs a version-table cutover | ✓ |
| Driver decorator on `database.Driver` | Supported without a fork; `SetVersion` fires twice per migration | |
| Two releases, no framework change | Zero new machinery; two deploys, Phase 3 waits | |

**User's choice:** *"2 goose is the correct thing to do here, **but** it may be worth blocking phase 2 on a separate workstream/spike/effort doing the goose work first"*

| Option | Description | Selected |
|--------|-------------|----------|
| Insert a new phase before Phase 2 | Own discuss/plan/execute/verify cycle; v0.13 grows to 7 phases | ✓ |
| Spike first, then decide | De-risks the cutover before committing | |
| Parallel workstream, Phase 2 gated | Keeps the phase list about the portal | |
| Backlog it, ship the interim | Nothing blocks; the interim becomes what ships | |

**User's choice:** Insert a new phase before Phase 2
**Notes:** Must be created via `/gsd-phase` — hand-editing `ROADMAP.md` is forbidden by a standing repo rule.

| Option | Description | Selected |
|--------|-------------|----------|
| Plan now, gate execution | Everything but sequencing is framework-independent | ✓ |
| Pause Phase 2 entirely | Discards seven areas already decided | |
| Plan now, keep two-release as fallback | Conditional branch through the safety-critical step | |

**User's choice:** Plan now, gate execution

| Option | Description | Selected |
|--------|-------------|----------|
| Migration fails with report; separate CLI resolves | Migration decides, human acts, migration re-verifies | ✓ |
| Migration fails; operator resolves with direct SQL | Bypasses the pipeline and block list | |
| Auto-resolve, report after | Reverses the halt-and-report decision | |

**User's choice:** Migration fails with report; separate CLI resolves

| Option | Description | Selected |
|--------|-------------|----------|
| Column beside the skeleton | Per-row version; stale subset is queryable | ✓ |
| Single settings key, game-wide | Cheap; wrong during a partial recompute | |
| Compile-time constant plus a meta-test | Zero storage; does not record what existing rows used | |

**User's choice:** Column beside the skeleton

---

## Claude's Discretion

- **Unicode mechanism for UTS #39 confusables/skeleton.** Offered as an eighth
  gray area and not selected. Explicitly **not** decided during this discussion:
  `golang.org/x/text` covers NFKC and script extensions, but the confusables table
  and skeleton algorithm are in neither the stdlib nor `x/text`, and naming an
  unverified third-party package would have been a guess. Routed to
  `/gsd-plan-phase`'s researcher with one binding constraint — the Unicode version
  MUST be pinnable and recorded per-row.
- Exact policy ids and names for the three tier-floor policies and the viewer
  read-policy twins.
- Test-file placement and naming throughout.

## Deferred Ideas

Nothing was deferred out of scope. The only new work that arrived — goose
adoption — was promoted to its own inserted phase rather than deferred, because
Phase 2 execution depends on it.

Two questions were surfaced and consciously left for later phases: whether
`viewer:` and `admin_section:` should be registered in `knownPrefixes` (they parse
without it), and the `last_active_at` write seam (Phase 3, with the explicit
hazard that it must not hook `RefreshConnection`).
