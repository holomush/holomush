---
phase: 01-portal-spec
plan: 03
subsystem: spec
tags: [spec, lifecycle, name-capture, normalization, unicode, invariant-registry]

requires:
  - phase: 01-portal-spec (plan 01-02)
    provides: 01-SPEC.md sections 2 and 3, the census predicate split into type-reachable and name-reachable members, and the scene-metadata family routed to section 5
provides:
  - "SPEC section 4 (Character Lifecycle) authored to completion — the status column DDL, the three-value vocabulary, the exhaustive-switch rule paired with the direct-construction idle test, retire/idle-out/purge as three distinct operations, and the retired-character semantics"
  - "SPEC section 5 (Name-Capture Surface Inventory) authored to completion — four normative rules plus twelve classified capture sites with one verdict each, two explicitly-excluded candidates, and a cross-listing of all four public export surfaces"
  - "SPEC section 6 (Name Normalization Policy) authored to completion — the two policies stated separately, the character-name pipeline fixed in order, UTS #39 Moderately Restrictive named as the mixed-script rule plus a skeleton check, and the detect/resolve/index sequencing"
  - "INV-WORLD-5 (exhaustive lifecycle reads) and INV-WORLD-6 (retire preserves the name reservation) declared in section 13 and registered, binding: pending"
  - "A seventh queued amendment in section 14 — IDENT-09 and research SUMMARY item 3 both undercount the check-then-insert writers"
affects: [01-04, 01-05, 01-06, phase-2-abac-schema, phase-3-character-management, phase-6-admin]

actuals:
  tokens: 12634
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Shipping an unreachable enum value only when paired with an exhaustive-switch rule AND a direct-construction test, so the value is non-vacuously covered rather than latently fail-open"
    - "Classifying a denormalization by its WRITE path, not its read path — a live read of frozen bytes is still historical"
    - "Separating a stable uniqueness key (indexed, constraint-backed) from a version-volatile confusable skeleton (queried, non-unique), because a UNIQUE constraint whose meaning shifts under a dependency upgrade is a migration hazard"

key-files:
  created:
    - .planning/phases/01-portal-spec/01-03-SUMMARY.md
  modified:
    - .planning/phases/01-portal-spec/01-SPEC.md
    - docs/architecture/invariants.yaml
    - docs/architecture/invariants.md

key-decisions:
  - "The name-capture verdict is decided on the WRITE path, not the read path — GameEvent.actor is recomputed on every read and is still historical, because it is recomputed from frozen bytes; a read-path test would classify it live and be wrong"
  - "A same-name rename is a no-op (no event, no version bump, expected_version still evaluated), not a rejection — rejection turns a harmless retry after a lost response into a user-visible failure"
  - "A case-or-spacing variant of the current name is a REAL rename, not a same-name no-op — it writes the new display form and does not self-collide in the uniqueness index"
  - "The confusable skeleton is NOT the uniqueness key; the normalized form is. The skeleton is separately stored, non-uniquely indexed, and checked by query, because the Unicode confusables table moves between releases"
  - "The mixed-script rule is named as UTS #39 Moderately Restrictive with its permitted script sets tabled, so Phase 2 implements against a specification rather than a judgement call"
  - "INV-WORLD-5 and INV-WORLD-6 land in WORLD per D-18, not ACCESS — a lifecycle state machine is world-model correctness, and filing 'a retired character cannot be selected for play' under ACCESS would make that scope's boundary statement false"

patterns-established:
  - "Stating an explicitly-checked NON-member table beside an inventory, so a later reader does not re-derive the negative"
  - "Recording a proto-versus-handler disagreement in the SPEC as current behavior AND filing it per .claude/rules/proto-doc-comments.md, rather than silently writing the SPEC to either the comment or the code"

requirements-completed: [PORTAL-03, PORTAL-04, PORTAL-07]

duration: 55min
completed: 2026-08-01
status: complete
---

# Phase 1 Plan 03: Character Lifecycle, Name Capture, and Name Normalization Summary

**Three sections that are one problem: retire must not release the name *because* names are frozen into surfaces no rename can reach, and whether two names are "the same" is decided by a normalization policy that does not exist yet — so all three answers are fixed in one document, together.**

## Performance

- **Duration:** ~55 min
- **Tasks:** 3/3
- **Commits:** 3

## Accomplishments

1. **Authored §4 (Character Lifecycle)** — fixed the storage shape as a single `status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'retired', 'idle'))` column with the DDL written inline for Phase 2 to transcribe, citing all four in-tree CHECK-enum precedents and recording why the two timestamp alternatives were rejected (D-05); shipped all three values from day one **paired with both halves of the safety rule** — the exhaustive `switch` with a denying `default`, and a Phase-2 test that constructs a character directly in `idle` (D-06); stated `retire` / `idle-out` / `purge` as three distinct operations with `purge` identified as the existing irreversible delete and its four FK consequences enumerated; stated normatively that retire MUST NOT release the name with both reasons; and stated all three retired-character properties (D-07).
2. **Authored §5 (Name-Capture Surface Inventory)** — four normative rules and **twelve classified capture sites** (seven `historical`, five `live`) enumerated from the tree, plus two candidates explicitly checked and excluded, plus a cross-listing table mapping each of §3's four public export surfaces to the capture site it serves.
3. **Authored §6 (Name Normalization Policy)** — the two policies stated separately with the threat-model reason they must not merge; the character-name pipeline fixed in order with the mixed-script rule named concretely; the check-then-insert race enumerated correctly; the username rule pinned as a regression guard; and the detect → one-shot resolve → index sequencing fixed.
4. **Declared and registered `INV-WORLD-5` and `INV-WORLD-6`**, `binding: pending`, no `asserted_by`, with `.planning/phases/01-portal-spec/01-SPEC.md` added to the INV-WORLD scope-level `origin_specs` (these are the scope's first v0.13 entries).

## What reading the tree found that the plan's premise did not

The plan, PORTAL-03, and the source research all describe the published scene archive as publishing **character display names** to anonymous readers. **It publishes character IDs.**

| Claim | Tree |
|---|---|
| `participants_snapshot` is *"The participant character names snapshotted at publish time"* (`api/proto/holomush/scene/v1/scene.proto:873-874`, `:957-958`, `:1052-1053`) | `ReadSceneMetaForSnapshot` writes `SELECT character_id FROM scene_participants` (`plugins/core-scenes/publish_store.go:987-1002`). Its own type comment says so: *"Name resolution is a follow-up; character IDs are the available identity surface"* (`:956-960`). |
| `PublishedSceneEntry.speaker` is *"The speaking character's display label for this line"* (`scene.proto:821`) | Assigned from `pl.ActorID` at `plugins/core-scenes/publish_snapshot.go:375` and `plugins/core-scenes/commands.go:107`. |
| `ParticipantInfo.character_name` carries a name | Falls back to the ID — no name resolver is wired (`plugins/core-scenes/service.go:526`, `:536`, `:1509`). |

Corroborated from the other side: `actor_display_name` is documented as *"Empty when name resolution is deferred (scenes today)"* (`api/proto/holomush/comm/v1/comm.proto:25-27`).

**This changes the answer without changing the verdict.** The rows stay `historical` either way — what is frozen at the PUBLISHED transition stays frozen whether the frozen bytes are an id or a name. What changes is the timeline: today's exposure is *smaller* than the requirement assumes, and tomorrow's is exactly as large, because the moment the documented follow-up lands these become frozen **names** with no update path. §5.4 records all three consequences and adds a normative prohibition the requirement could not have anticipated: **when name resolution lands it MUST NOT backfill already-written rows** — that is the mass update Rule 3 forbids, wearing the costume of a data-quality improvement.

The genuine name capture is elsewhere and was found by following the write path rather than the field names: `actor_display_name` on the communication-content payload, stamped from `a.Name` at `pkg/plugin/comm/builder.go:41`, `:48`, `:55`, landing in `events_audit.envelope` and `scene_log.payload`, and read back out into `GameEvent.actor` by `internal/web/translate.go:88-96`.

## The read-path trap §5's Rule 1 exists to avoid

`GameEvent.actor` is recomputed on **every** read. A tie-break rule phrased over the read path classifies it `live` — and is wrong, because it is recomputed **from frozen bytes**. The same trap catches `WebExportScene`, whose own source comment says *"Read IC `scene_log` rows (**live read**, ORDER BY id ASC)"* (`plugins/core-scenes/export.go:103`) while returning entirely frozen values.

Rule 1 is therefore stated as a **write-path** test: a site is `live` only when renaming the character changes what it subsequently serves *without any further write*. Both surfaces come out `historical`, correctly. Had the SPEC said "live if resolved at read time", Phase 3 would have shipped a rename believing two public surfaces would follow it.

## Deviations from Plan

### [Rule 2 — Missing critical enumeration] The check-then-insert race has two writers, not one

- **Found during:** Task 3, while writing §6.1.3's citation of the "two existing check-then-insert writers".
- **Issue:** The plan, `.planning/REQUIREMENTS.md:95-99` (IDENT-09), and `.planning/research/SUMMARY.md:277-281` all name `internal/bootstrap/setup/adapters.go:38-50` and `internal/auth/character_service.go:112-121` as the two racing sites. Those are the shared existence **query** and **one** writer. Grepping `ExistsByName` across production code found a **second** writer: `internal/auth/guest_service.go:227`, inside the guest-name retry-on-collision loop. SUMMARY item 3's argument — *"adding `Rename` doubles the writers into that race"* — is therefore wrong about the arithmetic: `Rename` takes the writers from two to three.
- **Why it matters, not just why it is inaccurate:** a Phase-2 planner sizing the duplicate-detection job from the requirement's letter would audit one creation path and miss the guest one — the path that provisions characters **automatically and at volume**, and therefore the one most likely to hold a pre-existing duplicate.
- **Fix:** §6.1.3 enumerates one query and two writers in a table, with `Rename` marked as the third writer that does not exist yet. A blockquote immediately below records the correction against the requirement text so a reader of both does not have to reconcile them.
- **Files:** `.planning/phases/01-portal-spec/01-SPEC.md`
- **Commit:** `9f49f5f3b`

### [Rule 2 — CLAUDE.md-mandated action] Filed the proto↔handler mismatch as issue #4901

- **Found during:** Task 2.
- **Issue:** `.claude/rules/proto-doc-comments.md` §"Proto ↔ handler mismatch protocol" states that on finding a proto/handler disagreement you MUST file a GitHub issue capturing it and document the CURRENT behavior. The `participants_snapshot` / `speaker` mismatch above is exactly that. The plan did not anticipate the finding, so it contains no such task — but CLAUDE.md directives take precedence over plan instructions.
- **Fix:** Filed <https://github.com/holomush/holomush/issues/4901> (`label: bug`) with the four proto citations, the three implementation citations, the two options (correct the comments vs. land name resolution), and the no-backfill constraint. No schema or proto change was made — SP0 protocol is to document current behavior, not to change the contract.
- **Commit:** referenced from `a507b612a`.

### [Authored beyond the literal action text] §5.3 cross-listing table

- **Found during:** Task 2.
- **Issue:** The plan asked §5 to "cross-reference each of the three public export surfaces section 3 inventoried". Two problems: there are **four** (plan 01-02's finding, already queued as the sixth amendment), and a prose cross-reference does not let a reviewer check the mapping. Worse, the four do **not** all serve the same capture site — `WebExportScene` renders from `scene_log` payloads while the other three serve the `published_scenes` columns.
- **Fix:** Added §5.3 as an explicit four-row table mapping each surface to the §5.2 capture site it serves and the verdict it inherits, which discharges the plan's `key_links` entry mechanically rather than by assertion. Sections renumbered accordingly (the old §5.3 became §5.4, §5.4 became §5.5) and the two forward references updated.

### [Rule 3 — Blocking] `task lint:yaml` failed after the registry edit

- **Found during:** Task 1, before commit. Third occurrence — plans 01-01 and 01-02 both recorded it.
- **Issue:** `yamlfmt` (`max_line_length: 120`) wanted its own wrapping of the new folded `summary:` scalars.
- **Fix:** Ran the sanctioned `task fmt:yaml`; `git diff` confirms it rewrapped only the two new `INV-WORLD-5` / `INV-WORLD-6` entries and touched no other file. Re-ran `go run ./cmd/inv-render -check` afterwards (exit 0) so the generated markdown matches the reformatted YAML.
- **Files:** `docs/architecture/invariants.yaml`
- **Commit:** `bf0d43337`

## Decisions the plan required and this plan made

Three questions the plan explicitly said must be answered rather than left silent:

| Question | Answer | Why this one |
|---|---|---|
| Same-name rename: no-op or rejection? | **No-op** — no event, no version bump, success returned; `expected_version` still evaluated. A case-or-spacing variant is a **real** rename. | Rejection turns a client retry after a lost response into a user-visible failure on a request that already succeeded. |
| Which script mixes are rejected? | **UTS #39 Moderately Restrictive**, tabled: single-script permitted; Latin+Japanese, Latin+Chinese, Latin+Korean permitted; **Latin+Cyrillic, Latin+Greek, Cyrillic+Greek rejected by name**; every other multi-script combination rejected. | Naming a standard profile gives Phase 2 a document to implement against and a later reader a document to check against. Freehand prose gives neither. |
| Is the skeleton the uniqueness key? | **No.** The normalized form is the key (UNIQUE index); the skeleton is separately stored, **non-uniquely** indexed, and checked by query, with the Unicode version pinned and recorded. | The confusables table changes between Unicode releases. A `UNIQUE` constraint whose meaning shifts under a dependency bump can make existing rows retroactively non-compliant with a constraint nobody edited. |

## Verification

| Gate | Result |
|---|---|
| `go run ./cmd/inv-render -check` | exit 0 |
| `task test -- -run 'TestEveryRegistryInvariantHasBinding\|TestRegistryBindingChecks\|TestProvenanceGuard\|TestBoundInvariantsAreGenuinelyAsserted\|TestRegistrySchemaParsesOwnershipFields' ./test/meta/` | exit 0 — 18 tests |
| `task lint:yaml` | exit 0 (exit code read directly, not grepped from output) |
| `task lint:markdown` | exit 0 |
| Task 1 acceptance | DDL fragment present with type, NOT NULL, DEFAULT and a three-value CHECK; the three literals lowercase; exhaustive-switch rule present; direct-construction `idle` test required; `purge` named as not-a-state with a resolving citation; retire-MUST-NOT-release-the-name present; all three retired-character properties present |
| Task 2 plan verify (`≥3` verdict cells, `historical` present) | exit 0 — 14 verdict cells: 8 `historical` (7 rows + Rule 2's table), 6 `live` (5 rows + Rule 2's table). No third token. |
| Task 3 plan verify (`NFKC`, `MUST NOT` present in §6) | `NFKC` ×4, `MUST NOT` ×4 |
| §4 `path:line` citations | 10 extracted, all resolve at the cited lines |
| §5 `path:line` citations | 25 extracted, all resolve — **3 corrected before commit** (see below) |
| §6 `path:line` citations | 11 extracted, all resolve |
| §14 amendment citations | `REQUIREMENTS.md:95-99` and `research/SUMMARY.md:277-281` both resolve at the quoted text |
| §13 ids vs registry | set-equal: `INV-ACCESS-10/11/12`, `INV-PRIVACY-9/10`, `INV-WORLD-5/6` |
| `INV-WORLD-5/6` shape | `binding: pending`, no `asserted_by`, `origin_spec` = the planning SPEC path |
| INV-WORLD scope `origin_specs` | `.planning/phases/01-portal-spec/01-SPEC.md` present exactly once |
| Section headings | still 16, in order; zero `01-03` placeholders remain |
| Deletions in any commit | none (`git diff --diff-filter=D` empty across all three) |
| Untracked files | none |
| Files touched since wave-2 HEAD | exactly three — `01-SPEC.md`, `invariants.yaml`, `invariants.md` |

**Three §5 citations drifted and were corrected before commit**, caught by resolving every extracted citation against the tree rather than trusting the notes they were written from:

| Cited | Actually at that line | Corrected to |
|---|---|---|
| `internal/store/migrations/000001_baseline.up.sql:70` | `player_id TEXT REFERENCES players(id),` | `:71` (`name TEXT NOT NULL,`) |
| `plugins/core-scenes/migrations/000004_create_scene_log.up.sql:22` | `actor_id BYTEA,` | `:23` (`payload BYTEA NOT NULL,`) |
| `api/proto/holomush/scene/v1/scene.proto:327-329` | starts at `string character_id = 1;` | `:328-330` (the name-fallback doc comment) |

## Known Stubs

None. `01-SPEC.md` retains six intentional placeholders — §9–§12 and §14–§16 minus what this wave filled — each naming the plan that authors it. §4, §5 and §6 carry no placeholder line.

## Threat Flags

None new. This plan authored a document and two registry entries; it introduced no endpoint, auth path, file access, or schema change. The plan's own register (T-01-14 … T-01-19) is discharged in text: T-01-14 by §6.1.1–§6.1.3, T-01-15 by §4.4's retire-MUST-NOT-release-the-name clause, T-01-16 by §4.3's paired rule and test plus `INV-WORLD-5`, T-01-17 by §5.1 Rule 3 and §5.4's no-backfill prohibition, T-01-18 by §5.1 Rule 1, and T-01-19 remains `accept` as the plan specified (§6.3 states the sequencing and names the in-migration-backfill prohibition as the reason).

## Notes for the orchestrator

- **STATE.md and ROADMAP.md were not touched.** `git diff 7a2cdd6a9..HEAD --name-only` returns exactly three files.
- **The amendment count is now SEVEN.** Plan 01-03 appended a seventh row to §14's queued block, in the same marked-block style: `REQUIREMENTS.md` IDENT-09 and `research/SUMMARY.md` item 3 both undercount the check-then-insert writers. Plan 01-05 MUST carry all seven; do not renumber or "correct" the count downward.
- **New GitHub issue #4901** — the `participants_snapshot` / `speaker` proto↔handler mismatch. It is a documentation-vs-implementation disagreement in the scenes plugin, not a v0.13 blocker, and it is not on any phase's critical path. Phase 3 and Phase 6 planners should read §5.4 before assuming the public archive already publishes names.
- **The SPEC's `Status:` line (`01-SPEC.md:3`) is now stale** — it still reads *"sections 1, 7, 8 and the section-13 opening authored (plan 01-01)"*. Plan 01-02 left it as-is and so did this plan, deliberately: it is a single line that every remaining wave would contend on. Plan 01-05 or 01-06 should rewrite it once at the end rather than six times.
- **One item for 01-04:** §5.5 places an obligation on §9 — v0.13 adds **no** new name-capture surface, so any new RPC in §9 that would carry a display name instead of an id needs a §5.2 row added first, with a verdict.
- **Actuals scale:** `tokens: 12634` is `chars/4` over the realized diff (`git diff 7a2cdd6a9..HEAD | wc -c` = 50,535), per the ADR-2629 contract. The plan's `estimate.tokens: 70000` is on the whole-run context scale, so the two are not directly comparable; recorded honestly rather than adjusted to look closer.

## Self-Check: PASSED

- Files: `01-SPEC.md`, `invariants.yaml`, `invariants.md`, `01-03-SUMMARY.md` — all FOUND.
- Commits: `bf0d43337`, `a507b612a`, `9f49f5f3b` — all FOUND in `git log`.
