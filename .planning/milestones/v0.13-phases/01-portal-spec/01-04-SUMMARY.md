---
phase: 01-portal-spec
plan: 04
subsystem: api
tags: [spec, grpc, proto, abac, admin, optimistic-concurrency, field-mask, privacy, oops, invariants]

# Dependency graph
requires:
  - phase: 01-portal-spec (plan 01-01)
    provides: SPEC skeleton, §1 overview, §7 profile/media model, §8 visibility model, §13 invariants opening
  - phase: 01-portal-spec (plan 01-02)
    provides: §2 audience matrix (the three-audience vocabulary §9 draws every verdict from), §3 read-surface inventory and its §3.4 obligation on §9
  - phase: 01-portal-spec (plan 01-03)
    provides: §4 lifecycle vocabulary, §5 name-capture inventory, §6 name-normalization pipeline
provides:
  - "§9 RPC Surface — the full v0.13 surface on CharacterAccessService: 7 reads + 9 mutations, each with an audience verdict, plus web proxies"
  - "§9.4 the concurrency contract — expected_version as an int32 scalar per request message, absent-or-zero rejected, WORLD_CONCURRENT_EDIT on stale, in-transaction outbox emission"
  - "§9.5 the update-mask contract — allowlist, exact-string matching, order-independence, empty-mask no-op, gate-before-mask ordering"
  - "§9.6 the error-code surface (8 codes with wire statuses) and §9.6.1 the corrected assertion mechanism"
  - "§9.7 the media proto shape, shipping now and empty"
  - "§10 Admin IA — the seven-section registry, the mandatory authorization descriptor, gate-before-NOT_IMPLEMENTED ordering"
  - "§10.5 the per-player admin gating verdict and its four consequences (session role field shape, alt-switcher behavior, nav-only role exposure, registry-driven nav)"
  - "§10.6 the character-edit field-mask allowlist and the rule that generates it"
  - "§10.8 the four explicit exclusions (role mutation, impersonation, break-glass identifier, raw DB console)"
  - "§11 the PORTAL-09 verdict (no) with three reasons and the one bounded permitted sorting surface"
  - "INV-WORLD-7 registered, binding: pending"
  - "Two queued SPEC amendments (8th and 9th) for plan 01-05"
affects: [02-abac-schema-vocabulary, 03-world-character-commands, 04-facade-characteraccess, 05-identity-ui-profiles, 06-admin-portal-shell]

actuals:
  tokens: 14074
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Per-audience typed RPCs on a facade service, mirroring the shipped SceneAccessService shape"
    - "expected_version as a scalar per request message, rejected at the RPC boundary when absent or zero"
    - "Field-mask allowlist as an exact-string map, empty mask a no-op, authorization asserted before mask application"
    - "Section registry with a mandatory authorization descriptor failing at compile time or boot"
    - "Wire-level opacity assertions (status code + generic message) replacing oops-chain code assertions"

key-files:
  created: []
  modified:
    - .planning/phases/01-portal-spec/01-SPEC.md
    - docs/architecture/invariants.yaml
    - docs/architecture/invariants.md

key-decisions:
  - "expected_version is an int32 scalar on each mutation request, transcribed from migration 000049's INTEGER column — not a shared embedded precondition message"
  - "Absent or zero expected_version is rejected at the RPC boundary, because the shipped repo layer treats Version==0 as an unversioned write and would silently accept it"
  - "Retire and un-retire are two intent-named RPCs, not one SetCharacterStatus(status) — a wire-settable status would make the unreachable `idle` value reachable and bypass §4.3's exhaustiveness rule"
  - "The profile URL is keyed on the character id, never the name — rename mutates it and purge can free it for a different character"
  - "The admin gate is evaluated PER PLAYER, matching PlayerHasRole's player-wide semantics and the operator path that already depends on them"
  - "The session role field is player-scoped and singular, not a per-character map; switching alts does not change admin reach"
  - "The admin edit mask contains only fields whose write has no side condition beyond a length cap — which excludes name, status and version by construction"
  - "The one permitted sorting surface is the admin character list, bounded to four intrinsic columns"
  - "oops.AsOops(err).Code() does NOT assert the top-level code — verified empirically against the pinned oops v1.22.0; opacity contracts are asserted over the wire instead"

patterns-established:
  - "Registry census by set equality: the section id is the row's first cell and nothing else, so the id set is extractable from table rows rather than inferred from prose"
  - "Generating rule before enumeration: state the rule that produces an allowlist, then the allowlist, so a later reader can test a new candidate against the rule"
  - "Correct-the-rule-and-file-the-issue: when a repo rule contradicts the pinned dependency, document current behavior, file the issue, queue the amendment — never silently restate the rule"

requirements-completed: [PORTAL-06, PORTAL-08, PORTAL-09]

coverage:
  - id: D1
    description: "§9 fixes the full new RPC surface with an audience verdict on every character-returning RPC, discharging §3.4's obligation on the Phase-4 census"
    requirement: PORTAL-06
    verification:
      - kind: other
        ref: "awk '/^## 9\\./{f=1} /^## 10\\./{f=0} f' 01-SPEC.md | rg -c 'expected_version' => 8"
        status: pass
    human_judgment: true
    rationale: "Completeness of an RPC enumeration against a milestone's needs is a judgment call — a mechanical check confirms expected_version is present but cannot confirm no required operation was omitted."
  - id: D2
    description: "§9.4 fixes expected_version as an int32 scalar per request message, rejects absent-or-zero, names WORLD_CONCURRENT_EDIT, and mandates in-transaction outbox emission"
    requirement: PORTAL-06
    verification:
      - kind: other
        ref: "citation sweep: internal/store/migrations/000049_world_version_guard.up.sql:20, internal/world/errors.go:26, internal/world/postgres/character_repo.go:82-85,120,134 all resolve"
        status: pass
    human_judgment: false
  - id: D3
    description: "INV-WORLD-7 declared in §13 and registered with binding: pending and no asserted_by; invariants.md regenerated"
    requirement: PORTAL-06
    verification:
      - kind: unit
        ref: "task test -- -run 'TestEveryRegistryInvariantHasBinding|TestRegistryBindingChecks|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/"
        status: pass
      - kind: other
        ref: "go run ./cmd/inv-render -check"
        status: pass
    human_judgment: false
  - id: D4
    description: "§10 fixes the seven-section registry with a mandatory authorization descriptor and gate-before-NOT_IMPLEMENTED ordering"
    requirement: PORTAL-08
    verification:
      - kind: other
        ref: "set-equality extraction of backticked first cells from §10 table rows == {audit characters config moderation players plugins stats}"
        status: pass
    human_judgment: false
  - id: D5
    description: "§10.5 answers whether /admin gates per acting character or per player, grounded in resolving path:line citations"
    requirement: PORTAL-08
    verification:
      - kind: other
        ref: "citation sweep: internal/store/role_store.go:83,86-93; internal/admin/auth/ingame.go:116,119; internal/admin/auth/operator_admin.go:53 all resolve with the quoted text"
        status: pass
    human_judgment: true
    rationale: "The verdict determines WebCheckSessionResponse's role field shape, which is a wire-compat commitment Phase 4/6 cannot cheaply revisit. The maintainer should confirm the per-player answer before the proto is written."
  - id: D6
    description: "§10.8 records role mutation as an explicit exclusion alongside impersonation, break-glass identifiers and a raw DB console, each with a reason"
    requirement: PORTAL-08
    verification:
      - kind: other
        ref: "rg -q 'Notably absent' over the §10 range"
        status: pass
    human_judgment: false
  - id: D7
    description: "§11 states the PORTAL-09 verdict as an explicit no with three reasons, and bounds the one permitted sorting surface to four intrinsic columns"
    requirement: PORTAL-09
    verification:
      - kind: other
        ref: "rg -q 'MUST NOT' && rg -q 'intrinsic' && ! rg -q 'field_visibility' over the §11 range"
        status: pass
    human_judgment: false

duration: 41min
completed: 2026-08-01
status: complete
---

# Phase 01 Plan 04: RPC Surface, Admin IA, and Sorting Verdict Summary

**Sixteen typed RPCs with `expected_version` as an `int32` scalar on every mutation, a seven-section admin registry gated per player with a mandatory authorization descriptor, and a categorical no on sorting profile fields — plus a corrected assertion mechanism after `oops.AsOops(err).Code()` was empirically proven not to do what three planning artifacts and a repo rule claim it does.**

## Performance

- **Duration:** 41 min
- **Started:** 2026-08-01T00:00:00Z (wave 4 start)
- **Completed:** 2026-08-01
- **Tasks:** 3
- **Files modified:** 3

## Accomplishments

- **§9 fixes the whole new RPC surface** — seven reads and nine mutations on `CharacterAccessService`, each carrying an audience verdict drawn from §2.1's three, which discharges the obligation §3.4 placed on this plan. Web proxies are named and declared census pairs, following §3.3's existing core/web twin precedent.
- **§9.4 makes `expected_version` transcribable rather than choosable** — `int32`, from migration `000049`'s `INTEGER` column, as a scalar per request message. The absent-or-zero rejection rule is grounded against a *live* affordance: the shipped repo layer treats `Version == 0` as an unversioned write (`internal/world/postgres/character_repo.go:82-85`, `:120`, `:134`), so a request omitting the field would not fail — it would succeed unguarded.
- **§10.5 answers the gating question research assigned to Phase 1** — per player, not per acting character, grounded in `PlayerHasRole`'s own comment and query and in the operator path that already depends on it. Four consequences are stated normatively, including the session role field's shape, which Phase 4/6 cannot cheaply revisit.
- **§10.2 makes the EXT-04 meta-test non-vacuous** — because the descriptor is a required field, registry↔descriptor set equality is trivially satisfiable, so the mandated test is Pitfall 7's stronger form: a denial assertion against every registered section's endpoint. Seven assertions today, six against sections with no content.
- **§11 states PORTAL-09's verdict as an explicit no** with three reasons in order, and bounds the one permitted sorting surface to a four-row intrinsic-column list so Phase 6 does not have to infer it.
- **Two queued amendments (8th and 9th)**, both load-bearing rather than cosmetic — see Deviations.

## Task Commits

1. **Task 1: Author §9 — the RPC surface and concurrency contract** — `b3f3318f3` (docs)
2. **Task 2: Author §10 — admin IA, the gating verdict, the role-mutation exclusion** — `e7785fef3` (docs)
3. **Task 3: Author §11 — the sorting and filtering verdict** — `42d7bdb67` (docs)

## Files Created/Modified

- `.planning/phases/01-portal-spec/01-SPEC.md` — §9, §10, §11 authored to completion; §13 gains INV-WORLD-7 and its count line updated seven→eight; §14's queued block grows from seven to nine amendments.
- `docs/architecture/invariants.yaml` — `INV-WORLD-7` added under the `INV-WORLD` scope, `binding: pending`, no `asserted_by`. The scope record's `origin_specs:` already carried the SPEC path from plan 01-03; no duplicate added.
- `docs/architecture/invariants.md` — regenerated with `go run ./cmd/inv-render`; no hand edits inside the generated regions.

## Decisions Made

Beyond the decisions CONTEXT.md had already locked, this plan settled the following. Each was a real fork, not a transcription:

- **Retire and un-retire are two intent-named RPCs, not one `SetCharacterStatus(status)`.** A status-setting RPC would put the lifecycle vocabulary on the wire, which makes `idle` a client-supplied value. §4.2 ships `idle` with no transition into it; a wire-settable status field *is* such a transition, added by accident and reachable by `curl`, and it bypasses §4.3's exhaustiveness rule because the write side never consults it. The same reasoning excludes `status` from the admin edit mask.
- **The profile URL is keyed on the character id, never the name.** §6.1 makes the name renameable and §4.4 makes it releasable by purge — so a name-keyed URL breaks on rename and, worse, silently resolves to a *different* character after a freed name is reclaimed.
- **The admin edit mask is generated by a rule, not enumerated by taste:** a path is eligible only if writing it has no side condition beyond a length cap. That excludes `name` (normalization pipeline + unique index), `status` (state machine) and `version` (concurrency guard) by construction rather than by remembering to leave them out, and it gives a later reader a test to apply to a new candidate.
- **The section gate uses `read` to reach a section and `write` for a mutation within it, both on the same `admin_section:<id>` resource.** One resource family keeps EXT-07's "every future section at zero additional policy cost" true (the seed policy is resource-type-scoped), while keeping mutations distinguishable from reads in policy.
- **`CHARACTER_PROFILE_NOT_FOUND` is deliberately one code for two causes** — unreachable profile and nonexistent character. Two codes would separate the cases and disclose existence, which is what §8.7's floor was set to withhold.
- **`CreateCharacter` is recorded as a reshape, not an addition**, so its §3.3 census row survives. Deleting the row would make the census RED for the wrong reason.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 — Missing Critical] `oops.AsOops(err).Code()` does not assert the top-level code**

- **Found during:** Task 1 (§9.6, naming the mandated assertion helper)
- **Issue:** The plan directed the SPEC to state that downstream tests assert the top-level code rather than chain-walking, per `.claude/rules/grpc-errors.md`. Verifying the claim against the tree showed **both halves of the rule are false** under the pinned `github.com/samber/oops v1.22.0` (`go.mod:32`). `OopsError.Code()` is documented in the dependency itself as *"returns the error code from the deepest error in the chain"* and is implemented as a recursive `getDeepestErrorCode` walk. `errutil.AssertErrorCode` (`pkg/errutil/testing.go:15-20`) is a thin wrapper over the same two calls, so the two spellings are behaviorally identical and both pass on the double-wrap the rule cites them to distinguish. Additionally `oops.AsOops` returns `(OopsError, bool)`, so the single-expression spelling is not a compilable call. Confirmed empirically, not by reading source: a throwaway probe against the pinned version returned `code=STREAM_ACCESS_DENIED` for `oops.Code("INTERNAL").Wrap(oops.Code("STREAM_ACCESS_DENIED")…)`.
- **Why this is Rule 2 and not a documentation nit:** the claim is **PORTAL-10 rule 5 itself** (`.planning/REQUIREMENTS.md:67-68`), one of the six verification-integrity rules §12 copies verbatim into every v0.13 plan. Writing it into the SPEC as given would have propagated an assertion that cannot fail on the leak it exists to catch into every phase of the milestone — the exact *"verification that cannot fail"* failure PORTAL-10 was written to end.
- **Fix:** §9.6.1 mandates asserting the **wire** instead — `status.Code(err)`, a generic `status.Convert(err).Message()`, and the internal code string absent from it, with a differential two-viewer assertion where the contract is indistinguishability. `errutil.AssertErrorCode` stays endorsed for asserting *which* internal code was produced. Filed as issue **#4902** (current behavior documented; no rule file changed, per the mismatch protocol). Queued as the **eighth** amendment with an explicit instruction that plan 01-05 MUST reconcile §12's rule-5 text with it before writing §12.
- **Files modified:** `.planning/phases/01-portal-spec/01-SPEC.md` (§9.6.1, §14)
- **Verification:** Empirical probe against the pinned version; all six citation sites (`REQUIREMENTS.md:67-68`, `ROADMAP.md:119`, `:136`, `:250`, `research/SUMMARY.md:162`, `research/PITFALLS.md:406-408`, `.claude/rules/grpc-errors.md:54-67`) enumerated and confirmed to resolve.
- **Committed in:** `b3f3318f3`

**2. [Rule 1 — Bug] `events_audit` has no transactional write path**

- **Found during:** Task 2 (§10.7, stating in-transaction audit emission)
- **Issue:** ADMIN-06 (`.planning/REQUIREMENTS.md:152-153`) and ROADMAP Phase 6 criterion 3 (`.planning/ROADMAP.md:252`) both require every admin mutation to write an `events_audit` row *"in the same transaction"*. No such path exists. `events_audit` is written only by the asynchronous JetStream audit projection (`internal/eventbus/audit/projection.go:319-331` → `writeAuditRow`, `INSERT INTO events_audit` at `:434`) plus the retention-partition mover (`internal/eventbus/audit/retention_partitions.go:546`). Nothing in a mutation's transaction touches the table.
- **Fix:** §10.7 states the durability boundary precisely — the **envelope** is transactional (`INV-WORLD-1`); the `events_audit` row is projected from it at-least-once with DLQ capture — and defers the wording correction to §14 as the **ninth** amendment. The guarantee ADMIN-06 actually wants (an admin mutation cannot commit without its audit record durably queued) is fully delivered by the envelope boundary; only the sentence naming the wrong table needs to change.
- **Why it matters:** a Phase-6 planner building to the criterion's letter would find no transactional path and either invent a bespoke direct insert — bypassing the codec / `dek_ref` / dedup contract `writeAuditRow` maintains, and creating a second writer to a partitioned table — or quietly weaken the criterion to whatever was built.
- **Files modified:** `.planning/phases/01-portal-spec/01-SPEC.md` (§10.7, §14)
- **Verification:** `rg` over all non-test Go for `events_audit` returned only the projection, the retention mover, comments, and test helpers; the three cited line numbers confirmed.
- **Committed in:** `e7785fef3`

**3. [Rule 1 — Bug] Two drifted line citations corrected before commit**

- **Found during:** Task 2 (post-authoring citation sweep)
- **Issue:** The ninth amendment initially cited `.planning/ROADMAP.md:251` for Phase 6 criterion 3 (line 251 is criterion 2) and `.claude/rules/grpc-errors.md:54-70` (the section ends at `:67`).
- **Fix:** Corrected to `:252` and `:54-67` respectively.
- **Verification:** All 61 full-path citations in §9–§11 and the two new amendment rows were extracted and checked for file existence and in-range line numbers; the load-bearing ones were additionally verified line-by-line against their quoted text.
- **Committed in:** `e7785fef3`

---

**Total deviations:** 3 auto-fixed (1 missing-critical, 2 bugs)
**Impact on plan:** No scope creep. Deviations 1 and 2 are corrections to *inputs* the plan told this section to restate; restating either as given would have written a known-false claim into a normative SPEC. Both are recorded as queued amendments rather than silently applied, and both carry an issue or an explicit instruction to the downstream plan.

## Issues Encountered

- **The §10 set-equality verification constrains §10's table layout.** The plan's check extracts every backticked lowercase first cell from every table row in the §10 range, so any *other* table in §10 with a backticked lowercase identifier in its first cell would have broken it. The field-mask allowlist was therefore written as a fenced code block rather than a table. Resolved without weakening the check — the constraint is a feature: it forces the section-id set to be the only such set in the section.
- **`.claude/rules/grpc-errors.md`'s recommendation and `errutil.AssertErrorCode`'s implementation had already converged** — the rule describes a helper that chain-walks and an alternative that does not, but the shipped helper *is* the alternative, and neither does what the rule claims. This meant reading the source was not sufficient to settle it; the empirical probe was.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

**Ready for plan 01-05** (§12 Verification Integrity, §14 Amendments, §15 Out of Scope):

- **§14's queued block now holds NINE amendments, not seven.** Plan 01-05 MUST carry all nine; do not renumber or reduce.
- **The eighth amendment blocks §12.** PORTAL-10 rule 5 as written is false against the tree, and §12 is the section that copies the six rules verbatim into every v0.13 plan. **01-05 MUST reconcile §12's rule-5 text with the amendment row before writing §12** — the two cannot ship disagreeing, and shipping rule 5 as-is propagates an unfalsifiable assertion into every remaining phase. Issue #4902 has the grounding.
- **§15 (Out of Scope) should cross-reference rather than restate.** §9.2, §9.7, §10.8 and §11.4 each carry a "Notably absent" block with reasons; a §15 that re-enumerates them risks the two drifting.

**Ready for later phases:**

- Phase 4 can write every new request and response message from §9 without choosing a field type, and now knows that `WORLD_CONCURRENT_EDIT` has no wire mapping today — §9.6 fixes it as `Aborted` and flags it as an addition.
- Phase 6 can write the section registry and its descriptors from §10 without choosing a gating model, and has the field-mask allowlist and its generating rule.
- Phase 2's `admin_section:` prefix and `AdminSectionResource()` land against §10.4's stated resource family and action pair.

**Open item deliberately surfaced, not resolved:** the per-player-vs-per-character role storage asymmetry is issue **#4899**, still open. §10.5 decides what v0.13 does; it does not resolve the underlying question, and §10.8 makes role mutation's exclusion contingent on that.

## Self-Check: PASSED

- All four claimed files exist on disk.
- All three task commits resolve in `git log` (`b3f3318f3`, `e7785fef3`, `42d7bdb67`).
- `STATE.md` and `ROADMAP.md` untouched across the whole plan range, as instructed.
- `go run ./cmd/inv-render -check` exits 0; the four registry meta-tests pass; `task lint:yaml` and `task lint:markdown` are clean.
- §10's set-equality extraction yields exactly the seven section ids.
- Every full-path `path:line` citation in §9–§11 and the two new amendment rows resolves within its file.

---
*Phase: 01-portal-spec*
*Completed: 2026-08-01*
</content>
</invoke>
