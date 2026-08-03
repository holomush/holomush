# Phase 2: ABAC & Schema Vocabulary - Context

**Gathered:** 2026-08-02
**Status:** Ready for planning — **execution gated** on the goose-adoption phase (see D-20)

<domain>
## Phase Boundary

Land the authorization vocabulary, name policy, and schema primitives every later
phase gates on: the `admin_section:` resource prefix and `seed:admin-section-access`,
the `seed:profile-public-read` widening and the viewer-tier floor policy family,
the character lifecycle column, the stored normalized-name column and its `UNIQUE`
index, and the confusable skeleton. **No UI and no new RPCs.**

**`.planning/phases/01-portal-spec/01-SPEC.md` is normative for this phase and
downstream agents MUST read it before planning or implementing.** It is not a
Phase 2 artifact, so it does not appear as a `<spec_lock>` here, but it locks the
pipeline order (§6.1.1), the mixed-script and skeleton rules (§6.1.2), the
uniqueness key and index sequencing (§6.1.3), the block-list evaluation points
(§6.1.4), the duplicate-resolution sequence (§6.3), the lifecycle storage shape
and exhaustiveness rule (§4.1–4.3), the tier ladder and set-membership clearing
test (§8.2–8.2.1), the conjunction (§8.5.1), the totality rule (§8.6), and the
admin registry (§10.1–10.4). Decisions below settle only what §01-SPEC left open
or got wrong.

</domain>

<decisions>
## Implementation Decisions

### Term B and the profile-visibility policy family

- **D-01:** Term B (the row-keyed half of §8.5.1's conjunction) is issued against
  **viewer-flavored read policies only**. The read-side subset of the shipped
  `seed:property-*` family (`internal/access/policy/seed.go:110-145`) gains
  `principal is viewer` twins evaluating the same `visibility` / `visible_to` /
  `excluded_from` row semantics. `seed:property-owner-write`
  (`seed.go:128-133`) gets **no twin** — a `viewer:` subject must never hold a
  write permit. — **Reversibility:** costly — the twins are referenced by every
  profile read path and by the Phase 4 census; replacing the shape later means
  re-seeding policies and re-deriving every profile-read test fixture.
- **D-02:** §01-SPEC §8.5.1.1's **option 2 is REJECTED.** "Term B evaluates
  against a co-located character subject and DENIES where one does not exist"
  violates §8.8 / INV-PRIVACY-10 for the `anonymous` rung: an anonymous viewer
  has no character, `profile.pronouns` is an `entity_properties` row (§7.1)
  seeded at the `anonymous` floor (§8.6), so term B would deny it and a reachable
  profile would carry `name` (a one-term `characters` column) without `pronouns`.
  The failure is closed, so no test in the suite would catch it — it surfaces as
  "the public profile looks bare", which is precisely the symptom §8.5.1.1 warns
  provokes the forbidden repair. — **Reversibility:** one-way — this is a
  correction to a shipped normative section; reversing it reopens the §8.8
  violation.
- **D-03:** The tier-floor configuration ships as **three policies, one per floor
  rung** (`anonymous` / `guest` / `player`), each carrying an explicit literal
  list of the §8.6 attribute names at that floor, ANDed with §8.2.1's
  set-membership clearing test. Not one policy per attribute name, and not a
  single name→floor map. The totality rule holds: names are matched as whole
  strings, no glob or prefix, and a name in no list is denied rather than
  defaulted.
- **D-04:** Phase 2 ships an **additive-permit regression test**: seed a
  `profile.*` row at `visibility='private'`, give the viewer a tier that clears
  that name's floor, and assert the attribute is **absent**. Under the
  conjunction it is absent; if term B is ever dropped the tier permit alone
  publishes it and the test goes RED. This is the mechanical guard that makes
  §8.5.1.1's prohibition enforceable rather than advisory.
- **D-05:** §8.5.1.1 is **amended in this phase** to record option 2 as rejected
  and D-01 as settled, following the §14 amendment discipline Phase 1 established.
  The finding is additionally **routed to `abac-reviewer`** before Phase 2 merges —
  `abac-reviewer` identified the §8.5.1.1 residual originally and has the context
  to confirm the rejection does not trade one hole for another.

### Admin section registry and the D1 denial-code oracle

- **D-06:** **Gate first, then distinguish.** `seed:admin-section-access` is
  evaluated **before** the registry lookup. A caller the gate denies always
  receives `DENY_ADMIN_SECTION`, whether or not the section id is registered;
  only a caller the gate permits can ever receive
  `DENY_ADMIN_SECTION_UNREGISTERED`. This closes the registry-enumeration oracle
  the sketch round found between §10.3 and §10.4 while keeping the operator
  diagnostic. It is the same ordering §10.3 already mandates for
  `NOT_IMPLEMENTED`, applied one field over, and it works because
  `seed:admin-section-access` is scoped by resource **type** (§10.4, EXT-07) so
  an unregistered id is still covered by the policy.
- **D-07:** The behavior is pinned by a **new `INV-PRIVACY` registry entry** —
  next free id in scope — bound by a test asserting a non-admin's refusal is
  byte-identical across a registered and an unregistered section id. Parallels
  INV-PRIVACY-9 for profiles. It **MUST be hand-registered** in
  `docs/architecture/invariants.yaml`: the orphan check walks only
  `docs/superpowers/specs/`, so a `.planning/` origin_spec is not auto-caught. —
  **Reversibility:** costly — invariant ids are referenced from tests and specs;
  renumbering is a migration, not an edit.
- **D-08:** §10.2's mandated non-vacuous denial test runs at the **shared
  authorization helper** level in Phase 2 (seven assertions, one per registered
  section), with the **endpoint-level** form landing in Phase 4 once the RPCs
  exist. Phase 2 ships no RPCs, so the endpoint form is unwritable here — but the
  phase that builds the gate proves the gate.
- **D-09:** §10.2's "compile time or at boot" requirement is carried by **boot
  validation plus a meta-test**: a boot validator refuses to start on a
  zero-valued or partially-zero authorization descriptor, and a meta-test asserts
  every registry entry has a non-zero action and resource. Go has no `satisfies`,
  and §10.2 explicitly permits boot as the enforcement point.

### `seed:profile-public-read` and the exposure audit

- **D-10:** The widening permits off-location reads of **any**
  `parent_type='character'` row at `visibility='public'` — it is **not** scoped to
  §8.6's enumeration. Web exposure remains bounded regardless: a name in no §8.6
  row is denied by term A, so no legacy row reaches the web through this change. —
  **Reversibility:** costly — see D-11; narrowing later is a visible behavior
  regression for grid readers.
- **D-11:** The grid-path consequence is **intended**: widening
  `seed:player-character-colocation` means an off-location character in-game can
  read public properties it previously could not, and the tier floor does not gate
  that path. `public` means public on the grid as well as the web; the colocation
  restriction was the anomaly. The audit's job is therefore to find rows that were
  relying on colocation as de-facto privacy, and **the fix for any such row is to
  change the row's `visibility`, never to narrow the policy.**
- **D-12:** The audit is a **committed read-only query with its result recorded**
  in the phase artifacts — re-runnable, and the recorded count is the evidence
  criterion 4 asks for rather than an unverifiable claim in a summary.
- **D-13:** The in-world description **stays at the `anonymous` floor**. §8.11
  already records this as a deliberate divergence from strict grid-parity; Phase 1
  decided it and Phase 2 implements it unchanged.

### Character-name block list (IDENT-07 / §6.1.4)

- **D-14:** The block list lives in the **settings game scope under a `core.*`
  key**, stored in `holomush_system_info`, seeded by migration with
  `ON CONFLICT DO NOTHING` so an operator override survives re-application
  (pattern: `000007_seed_scene_defaults.up.sql:7-9`). Reuses
  `settings.SetStringSlice` (`internal/settings/game.go:147`); no new table, and
  no change to the namespace allowlist (`internal/settings/namespaces.go:15-20`),
  which already admits `core`. — **Reversibility:** costly — the key name becomes
  operator-facing configuration once seeded.
- **D-15:** A pattern that fails to compile is a **hard startup failure** naming
  the offending entry. The whole list is validated and compiled at boot. Same
  posture as D-09's registry validation: the misconfiguration is found by the
  operator who caused it, not by the first player whose name slips through.
- **D-16:** The compiled list refreshes by **mirroring the repo's existing
  DB-backed-config pattern** — `internal/access/policy/cache.go` (atomic
  compiled snapshot, read barrier) plus `internal/access/policy/poller.go`
  (version poll on a cheap indicator, reload on change; 10s default at
  `poller.go:96`). Poll `holomush_system_info`'s `updated_at` for the key. This is
  correct where invalidate-on-write is not: there is no in-process writer to hook
  (v0.13 ships no editing surface per §8.12, so edits are direct SQL), and
  HoloMUSH is multi-replica, so an in-process invalidation would only fix the
  replica that served the write. A pattern that fails to compile makes `Reload`
  fail, leaving the **last valid list in force** rather than silently unenforced
  (precedent: `cache_test.go:166-188` — reload failure does not partially update).
- **Note:** Go's `regexp` is RE2 — linear time, no backtracking — so
  operator-supplied patterns carry **no ReDoS risk**. The residual risk is a
  *wrong* pattern (over-broad), not a slow one. Do not add backtracking-defense
  machinery.

### Pre-existing duplicate resolution (§6.3)

- **D-17:** **Halt and report; no auto-resolution.** The job detects and reports
  every collision set with enough context to decide (ids, names, owners,
  `created_at`) and resolves nothing on its own. §6.3 explicitly says this step
  can require a judgement call about which of two colliding characters keeps its
  name.
- **D-18:** The **operator supplies the replacement name**, which is then
  validated through the full §6.1.1 pipeline and the block list before being
  written — so no machine-generated name ever reaches a player, and the
  replacement is guaranteed to satisfy the constraint about to be indexed over it.
- **D-19:** Because real data will very likely contain **zero** collisions, the
  job is proven by a **synthetic-collision integration test** against real
  Postgres, seeding deliberately colliding rows — **including an NFKC-only pair
  the live `LOWER(name)` check could never have caught**
  (`internal/bootstrap/setup/adapters.go:38-50`) — then asserting detection and
  that the `UNIQUE` index applies cleanly afterwards. A job that has only ever run
  against clean data is a job nobody has watched work.

### Migration framework and sequencing

- **D-20:** **goose adoption is a new phase inserted before this one**, and Phase
  2 **execution is gated on it**. Phase 2 planning proceeds now — every other
  decision here is independent of the migration framework. The phase MUST be
  inserted with `/gsd-phase` (never by hand-editing `ROADMAP.md`).
  **LANDED 2026-08-02 as Phase 01.1** ("Migration framework: adopt goose for Go
  migrations", `ROADMAP.md:166`, directory
  `.planning/phases/01.1-migration-framework-adopt-goose-for-go-migrations/`).
  Note it is a **decimal insertion, NOT a renumbering** — GSD's insert-phase
  workflow explicitly forbids renumbering existing phases, so Phases 2-6 keep
  their numbers, this directory keeps its `02-` prefix, and every "Phase 2"/"Phase
  4" reference in `01-SPEC.md` and `REQUIREMENTS.md` stays valid. — **Reversibility:**
  one-way — the cutover converts recorded migration state from golang-migrate's
  single-version `schema_migrations` to goose's row-per-migration
  `goose_db_version`; applied twice it is unrecoverable.
  - **Why:** golang-migrate v4.19.1 has **no hook or callback surface** (verified
    against the module source — the only extension points are the `source.Driver`
    and `database.Driver` interfaces at `database/driver.go:45-82`, plus
    `migrate.NewWithInstance` at `migrate.go:171`). Startup applies **all**
    migrations via `Up()` (`internal/bootstrap/migration.go:65`, called inline at
    `cmd/holomush/core.go:279-282`) strictly before any bootstrap step
    (`orch.StartAll`, `core.go:857`), so a same-release migration that depends on
    a Go backfill is unreachable. goose supports Go migrations natively
    (`AddMigrationContext` / `AddMigrationNoTxContext`), interleaved by version
    with SQL and with transaction control.
  - **Sizing note for that phase:** goose uses a **single file per migration**
    with `-- +goose Up` / `-- +goose Down` annotations; this repo has **49
    migrations as paired `.up.sql` / `.down.sql` files**. Adoption therefore
    rewrites all 49 pairs and touches the embedded FS wiring, the `Migrator`
    wrapper and its `migrateIface` mock (`internal/store/migrate.go:42-50`), the
    `holomush migrate` CLI (`cmd/holomush/migrate.go:254-290`), the migration
    meta-tests, and `.claude/rules/database-migrations.md`. The sharp edge is the
    version-table cutover, not the rewrite.
- **D-21:** Post-goose, Phase 2's DDL sequences as **three numbered migrations in
  one release**: (A) `status`, `last_active_at`, `normalized_name` (nullable),
  skeleton + its **non-unique** index, and the block-list settings seed; (B) a
  **Go migration** performing the backfill and duplicate detection; (C)
  `SET NOT NULL` on `normalized_name` **then** `CREATE UNIQUE INDEX`. Ordering is
  enforced by the framework rather than by a runbook.
  - **C's ordering is load-bearing:** Postgres treats `NULL`s as distinct for
    uniqueness, so a `UNIQUE` index over an unbackfilled nullable column
    **succeeds and enforces nothing** — a green deploy with IDENT-09's guarantee
    silently absent. `SET NOT NULL` first makes B's execution a precondition of C.
- **D-22:** A Go migration cannot pause for judgement, so on collision it
  **returns an error naming every collision set**; the transaction rolls back and
  startup aborts, leaving the schema at the prior version. The operator resolves
  via a **dedicated CLI command** that routes the replacement name through the
  full §6.1.1 pipeline and block list (D-18), then re-runs migrations. Resolution
  MUST NOT be a hand-written SQL runbook — direct `UPDATE` bypasses both the
  pipeline and the block list, and can seat a name the index about to be created
  would reject.
- **D-23:** The skeleton's Unicode version is recorded in a **per-row column
  beside the skeleton**, as §6.1.2 asks ("recorded alongside the stored
  skeleton"). Per-row means a stale subset is queryable after a confusables-table
  upgrade and a partial or interrupted recompute is visible rather than silent. —
  **Reversibility:** one-way — dropping or re-typing the column is a migration.

### Lifecycle and roster primitives

- **D-24:** `characters.last_active_at` **lands in Phase 2** as a schema
  primitive; the **write seam is Phase 3's** (session-store create — and per the
  Phase 3 sketch finding it MUST NOT hook the lease-refresh path,
  `internal/session/session.go:485` `RefreshConnection`, which would make every
  character a hot write per lease interval). — **Reversibility:** one-way —
  removing a shipped column is a migration.
- **D-25:** The column is **`BIGINT NOT NULL DEFAULT 0`, where `0` is the Unix
  epoch and is the never-active sentinel.** No nulls, consistent with §4.1's
  no-unknown-state discipline. `BIGINT` epoch matches where the schema is going
  (`000044_pregfo6_gap_timestamps_to_bigint`). Consequences to honor: `0` needs a
  named constant, and any "last active" rendering MUST special-case it rather than
  displaying 1970. Useful side effect — `0` is the minimum, so a most-recent-first
  sort places never-active rows last without a `NULLS LAST` clause.
- **D-26:** **Both §11.3 amendments land in this phase**, folded into the same
  amendment pass as D-05: add `last_active_at` as a permitted sort column (sketch
  A1), and add the joined `players.username` row (sketch A2) that the admin list
  already sorts by and §11.3 never enumerated. §11.3's existing
  `characters.player_id` row ("never an ordering") stays correct — A2 is a
  different claim, not a contradiction of it.

### Claude's Discretion

- **Unicode mechanism for UTS #39 confusables/skeleton.** Offered as a gray area
  and not selected, so it is mine — but it is a genuine research question, not a
  preference, and it is deliberately **not** decided here. `golang.org/x/text`
  covers NFKC (step 1) and script extensions for UTS #24 (Mechanism A); the
  confusables table and skeleton algorithm (Mechanism B) are in neither the
  stdlib nor `x/text`. `/gsd-plan-phase`'s researcher selects the mechanism —
  maintained third-party package, generated-into-repo table, or vendored data.
  **Binding constraint regardless of choice:** the Unicode version MUST be
  pinnable and MUST be recorded per-row (D-23).
- Exact policy ids/names for the three tier-floor policies and the viewer
  read-policy twins, so long as D-01 and D-03's shapes hold.
- Test-file placement and naming throughout, per `.claude/rules/testing.md`.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Normative for this phase — read first
- `.planning/phases/01-portal-spec/01-SPEC.md` — **the governing spec.** §4.1–4.3
  (lifecycle storage, vocabulary, exhaustiveness rule), §6 (name normalization —
  §6.1.1 pipeline order, §6.1.2 mixed-script + skeleton, §6.1.3 uniqueness key and
  index sequencing, §6.1.4 block list, §6.2 usernames unchanged, §6.3 duplicates
  precede the index), §7.1 (column-vs-row split), §8.2–8.2.1 (tier ladder,
  set-membership clearing), §8.4–8.4.2 (policy family, viewer principal,
  reachability resource), §8.5.1 + §8.5.1.1 (the conjunction; **amended by D-02**),
  §8.6 (configured postures + totality rule), §8.8 (name/pronouns hard floor),
  §8.11 (grid-parity divergence), §10.1–10.4 (admin registry, descriptor,
  refuse-after-gate, authorization shape), §11.1–11.3 (sorting verdict; **amended
  by D-26**), §13 (invariants).
- `.planning/ROADMAP.md` — Phase 2 entry: goal, the five success criteria, the
  research flag, and the sketch findings (A1, A2, D1, name-pipeline UI) this
  phase must answer.
- `.planning/REQUIREMENTS.md` — IDENT-06, IDENT-07, IDENT-08, IDENT-09,
  PROFILE-11, EXT-07.

### Repo rules that constrain the implementation
- `.claude/rules/invariants.md` — defining/binding workflow; the orphan check
  walks only `docs/superpowers/specs/`, so D-07's entry needs hand-registration.
- `.claude/rules/database-migrations.md` — pairing, idempotency, no triggers or
  functions, **no long-running backfills inside migrations** (the rule that makes
  D-21's Go migration the right home for the backfill). **Updated by the
  goose-adoption phase (D-20).**
- `.claude/rules/abac-providers.md` — omit-don't-sentinel for optional attributes;
  directly relevant to the viewer principal's attribute bag.
- `.claude/rules/testing.md` — ACE naming, tiers, `// Verifies:` annotations.
- `.claude/rules/logging.md` — `*Context` slog variants wherever a `ctx` is in scope.
- `docs/architecture/invariants.yaml` — registry source of truth; D-07 adds an entry.

### Code the phase modifies or must match
- `internal/access/policy/seed.go:110-145` — the six shipped `seed:property-*`
  policies D-01 twins; `seed:property-owner-write` at `:128-133` gets no twin.
- `internal/access/policy/engine.go:591-611` — `combineDecisions`; deny-overrides,
  permits combine **disjunctively**, which is why §8.5.1's conjunction is ANDed by
  the caller and not by the engine.
- `internal/access/policy/dsl/evaluator.go:185-201` — `compareStrings` is Go byte
  order; `:317-336` `evalInList` is false on an unresolved LHS. Together these are
  why §8.2.1 forbids ordinal comparison for the tier ladder.
- `internal/access/prefix.go:23-33` — resource-prefix family; Phase 2 adds
  `admin_section:` and `AdminSectionResource()`.
- `internal/access/policy/cache.go`, `internal/access/policy/poller.go` — the
  compiled-snapshot + version-poll pattern D-16 mirrors.
- `internal/world/validation.go:60,69-105,114-126` — `characterNameRegex`,
  `ValidateCharacterName`, `NormalizeCharacterName`. Phase 2 **replaces**
  `NormalizeCharacterName` (§6.1.5), not extends it.
- `internal/auth/player.go:24-25,31,167` — the username rule IDENT-08 pins by
  regression guard. **Do not touch** (§6.2).
- `internal/bootstrap/setup/adapters.go:38-50` — the shared `LOWER(name)`
  existence query; `internal/auth/character_service.go:112-121` and
  `internal/auth/guest_service.go:227` — the two live writers racing on it.
- `internal/settings/game.go:56-63,134,147`, `internal/settings/namespaces.go:15-20`
  — settings game scope, `SetStringSlice`, and the `core` namespace for D-14.
- `internal/store/migrations/000001_baseline.up.sql:67-76` (no `characters.status`,
  no unique/`LOWER` name index today), `:259,261,294,358` (enum-by-CHECK
  precedents), `:350-371` (`entity_properties` shape).
- `internal/store/migrations/000007_seed_scene_defaults.up.sql:7-9` — the
  `ON CONFLICT DO NOTHING` seed pattern D-14 reuses.
- `internal/store/migrate.go`, `internal/bootstrap/migration.go:46-71`,
  `cmd/holomush/core.go:279-282`, `cmd/holomush/migrate.go:254-290` — everything
  the goose-adoption phase (D-20) rewrites.
- `web/src/lib/nav/sections.ts:35-47` — the registry **shape** §10.1 says to
  mirror core-side (not the location).
- `internal/admin/auth/operator_admin.go:37-64`, `internal/admin/auth/ingame.go:117-118`
  — the re-assert-at-every-entry-point precedent §10.4 transposes.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`policy.Cache` + `policy.Poller`** — a complete, tested solution to
  "DB-backed config, no in-process writer, must take effect live, multi-replica".
  D-16 mirrors it for the block list rather than inventing invalidation.
- **`settings.SetStringSlice` + `holomush_system_info`** — list-of-strings config
  already works end to end; no new storage needed for D-14.
- **Enum-by-`CHECK` precedent** — four instances in the baseline migration alone
  (`:259,261,294,358`), so §4.1's `status` column follows house style exactly.
- **`Migrator.Migrate(version uint)`** (`internal/store/migrate.go:118`) — already
  implemented and already on `migrateIface`, just never wired to any caller. Noted
  because it was the basis of a rejected alternative to D-20; the goose-adoption
  phase supersedes it.
- **`AssertOperatorAdmin`** (`internal/admin/auth/operator_admin.go:37-64`) — the
  shared-helper-called-first pattern, with its rationale recorded at the call site.

### Established Patterns
- **Deny-overrides with disjunctive permit combining** (`engine.go:591-611`) —
  the single most important constraint on D-01: any satisfied permit permits, so
  the tier floor MUST be a separate evaluation ANDed by the caller.
- **New entity prefixes parse without registration** — `ParseEntityRef` /
  `knownPrefixes` (`internal/access/prefix.go`) has **zero production callers**;
  every engine path uses `SplitN(ref, ":", 2)`. So `viewer:` and `admin_section:`
  parse today. Convenient, but they are unregistered rather than blessed — worth
  a deliberate decision in planning.
- **Sentinel-written-last one-shot bootstrap** (`internal/bootstrap/setting.go:103-112,156`)
  — crash mid-way re-runs the whole sequence; failure blocks startup
  (`internal/plugin/bootstrap.go:64-67`). D-22's failure posture matches.
- **Go regexes are compile-time `MustCompile` allowlists everywhere** — there is
  **no** configurable regex list, blocklist, or denylist anywhere in the tree
  today. D-14 introduces the first one; there is no existing shape to copy.

### Integration Points
- `seed:admin-section-access` + the `admin_section:` prefix are consumed by Phase 4's
  admin RPCs; Phase 2 ships the vocabulary and the helper, Phase 4 the endpoints (D-08).
- The tier-floor family and viewer twins are consumed by Phase 4's
  `CharacterAccessService` projections; INV-ACCESS-10/11 bind there, not here.
- The normalized-name `UNIQUE` index gates Phase 3's `RenameCharacter` (§6.1.3);
  the `status` column gates Phase 3's `RetireCharacter`.
- `last_active_at` is written in Phase 3 and read by Phase 4's admin roster.

</code_context>

<specifics>
## Specific Ideas

- **"Public means public on the grid too."** D-11 is a deliberate posture, not a
  side effect: the colocation restriction on public character properties is
  treated as the anomaly, and the audit exists to catch rows that were relying on
  it as de-facto privacy. The remedy for such a row is to change the row's
  `visibility`, never to narrow the policy.
- **goose is the correct tool, but not this phase's work.** Recorded verbatim in
  substance: adopt goose, and block Phase 2 execution on a separate effort that
  does it first, rather than letting an interim two-release shape ship and become
  the thing Phase 3 inherits.
- **`0` as the epoch sentinel** was chosen explicitly over a nullable column —
  §4.1's no-unknown-state discipline carried over to `last_active_at` even though
  "never active" is a genuine case.

</specifics>

<deferred>
## Deferred Ideas

- **Nothing was deferred out of scope.** The one item that arrived as new work —
  goose adoption — was promoted to its own inserted phase (D-20) rather than
  deferred, because Phase 2 execution depends on it.
- **Follow-ups belonging to later phases, recorded so they are not lost:**
  - The `last_active_at` **write seam** is Phase 3's, with the explicit hazard
    that it MUST NOT hook `RefreshConnection` (`internal/session/session.go:485`).
  - INV-ACCESS-10, INV-ACCESS-11, INV-PRIVACY-9, INV-PRIVACY-10, INV-ACCESS-12 and
    INV-WORLD-7 all bind in **Phase 4**; only INV-WORLD-5, INV-WORLD-6 and D-07's
    new entry are Phase 2's to bind.
  - Whether `viewer:` and `admin_section:` should be **registered** in
    `knownPrefixes` — they parse without it, so this is hygiene rather than a
    blocker, but it is a real question someone should answer deliberately.

</deferred>

---

*Phase: 2-ABAC & Schema Vocabulary*
*Context gathered: 2026-08-02*
