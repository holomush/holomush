# Roadmap: HoloMUSH

## Overview

HoloMUSH is a mature, actively-developed platform — the event-sourced core, ABAC access control, dual-protocol
(telnet + web) gateways, two-tier plugin host, and the flagship Scenes/RP subsystem are shipped and running
(full context: `milestones/v0.11-ROADMAP.md` "Shipped Foundation"). The v0.11 milestone (Channels, Scenes
lineage completion, platform hardening) shipped 2026-07-11.

The v0.12 milestone (Foundation Hardening, v0.12 Phases 4–9) shipped 2026-07-28 — the event-model decision and
its integrity fixes, operational hardening and CI assurance gates, architecture decomposition, and the
code-health sweep. Full detail: `milestones/v0.12-ROADMAP.md`.

**Active milestone — v0.13 Web Portal: Identity & Admin Foundations (Phases 1–6):** give web players a
complete character identity surface — creation, management, and public profiles with privacy — and stand up
the admin portal shell that gives character administration a home, with both designed to absorb the deferred
portal surfaces without rework. The milestone **opens by producing the portal SPEC** that PROJECT.md's
Out-of-Scope entry named as its precondition, rather than waiving it. Requirements: `REQUIREMENTS.md`
(50 REQ-IDs across PORTAL / IDENT / PROFILE / ADMIN / EXT). Research basis: `research/SUMMARY.md`.
Remaining not-yet-scheduled scope stays in `## Backlog` — promote entries with `/gsd-review-backlog`.

## Milestones

> **Phase-numbering convention (changed at v0.13).** Phase numbers **restart at 1 for every milestone as of
> v0.13** — a phase id is unique *within its milestone*, not across project history. **v0.11 and v0.12 used
> continuous global numbering** (v0.11 Phases 1–3, v0.12 Phases 4–9) and are **not** renumbered: they shipped
> under those numbers, and the archives, PRs, and `milestones/v0.1x-phases/` directories all reference them.
> Wherever an archived phase is named below, its milestone is stated explicitly (e.g. "v0.12 Phase 4"); a
> bare "Phase N" always means the **active milestone**. The `999.x` backlog entries are parking-lot ids, not
> milestone phases — their numbering is independent of both schemes.

- ✅ **v0.11 Social Spaces & Platform Hardening** — v0.11 Phases 1–3 (shipped 2026-07-11) — [archive](milestones/v0.11-ROADMAP.md) · [audit](milestones/v0.11-MILESTONE-AUDIT.md)
- ✅ **v0.12 Foundation Hardening** — v0.12 Phases 4–9 (shipped 2026-07-28) — [archive](milestones/v0.12-ROADMAP.md) · [audit](milestones/v0.12-MILESTONE-AUDIT.md)
- 🚧 **v0.13 Web Portal: Identity & Admin Foundations** — Phases 1–6 (in progress, started 2026-07-31)

## Phases

<details>
<summary>✅ v0.11 Social Spaces & Platform Hardening (v0.11 Phases 1–3) — SHIPPED 2026-07-11</summary>

> **Numbering:** these are **v0.11** phase numbers under the retired continuous global scheme. They are
> *not* the active milestone's Phases 1–6 and are deliberately left un-renumbered.

- [x] v0.11 Phase 1: Channels Subsystem (10/10 plans) — `core-channels` as the social-spaces substrate's second consumer — completed 2026-07-09
- [x] v0.11 Phase 2: Scenes Lineage Completion (7/7 plans) — notifications + telnet polish (templates descoped to backlog) — completed 2026-07-09
- [x] v0.11 Phase 3: Platform Hardening & Deployment Scaling (9/9 plans) — external/clustered NATS, multi-node crypto invalidation, audit DLQ — completed 2026-07-10

Full phase details, requirements mapping, and success criteria: [milestones/v0.11-ROADMAP.md](milestones/v0.11-ROADMAP.md).
Phase execution artifacts: `milestones/v0.11-phases/`.

</details>

<details>
<summary>✅ v0.12 Foundation Hardening (v0.12 Phases 4–9) — SHIPPED 2026-07-28</summary>

> **Numbering:** these are **v0.12** phase numbers under the retired continuous global scheme. They are
> *not* the active milestone's Phases 1–6 and are deliberately left un-renumbered.

- [x] v0.12 Phase 4: World-Model Resilience Investigation & Decision (F1) (4/4 plans) — resilience/concurrency pass + the event-sourcing-vs-CRUD ADR (decision gate) — completed 2026-07-11
- [x] v0.12 Phase 5: World-Model Integrity Fixes (M2/M12) (16/16 plans) — version guard, transactional outbox, event-sourcing doc correction — completed 2026-07-13
- [x] v0.12 Phase 6: Operational Hardening & Assurance Gates (5/5 plans) — `events_audit` retention, nats CVE + vuln-scan gate, DLQ bridge, coverage reconciliation — completed 2026-07-15
- [x] v0.12 Phase 7: Event-Model & Bootstrap Decomposition (11/11 plans) — `core.Event`/`eventbus.Event` collapse, bootstrap→`lifecycle.Orchestrator`, gateway-boundary imports — completed 2026-07-18
- [x] v0.12 Phase 8: God-Object Decomposition (9/9 plans) — CoreServer + plugin/manager decomposition (behavior-preserving) — completed 2026-07-19
- [x] v0.12 Phase 9: Test-Quality & Code-Health Sweep (21/21 plans) — coverage-chain repair, ABAC fail-open fix, session-lifecycle matrix — completed 2026-07-27

Full phase details, requirements mapping, and success criteria: [milestones/v0.12-ROADMAP.md](milestones/v0.12-ROADMAP.md).
Phase execution artifacts: `milestones/v0.12-phases/`. Completion audit: [milestones/v0.12-MILESTONE-AUDIT.md](milestones/v0.12-MILESTONE-AUDIT.md).

**Closed as `override_closeout`:** v0.12 Phase 9 verified `gaps_found` (3/4) because QUAL-02/03/05 were
deliberately deferred with tracking (#4860, #4861, #4875, #4876, #4792); all other phases verified
`passed`. Carried forward: #4880 (CLAUDE.md event-construction rule defect), #4881 (no ruleset↔CI
reconciliation), #4882/#4883 (unquarantined flakes).

</details>

### 🚧 v0.13 Web Portal: Identity & Admin Foundations (Phases 1–6) — IN PROGRESS

**Milestone Goal:** Give web players a complete character identity surface — creation, management, and
public profiles with privacy — and stand up the admin portal shell that gives character administration a
home, with both designed to absorb the deferred portal surfaces without rework.

- [x] **Phase 1: Portal SPEC** — settle every shape decision whose cost explodes after code exists, and discharge PROJECT.md's Out-of-Scope precondition — completed 2026-08-01 (6/6 plans; `01-SPEC.md`, 16 sections; 9 amendments applied; 4 issues opened)
- [ ] **Phase 2: ABAC & Schema Vocabulary** — admin-section + public-profile policy, name normalization + unique index, character lifecycle column
- [x] **Phase 3: World Character Commands** — domain-layer soft `RetireCharacter`/`UnretireCharacter` + the retirement reactor, version-guarded and outbox-emitting (`RenameCharacter` moved to 999.20, 2026-08-06) (completed 2026-08-10)
- [x] **Phase 4: Shared Facade Helpers & `CharacterAccessService`** — one guest/ownership gate; character read/write BFF with privacy enforced by absence (completed 2026-08-11)
- [x] **Phase 5: Character Identity UI & Public Profiles** — creation identity card, multi-alt management, public profile page, per-field visibility (completed 2026-08-13)
- [x] **Phase 6: Admin Portal Shell & Character Administration** — ABAC-gated `/admin`, character administration, six deferred sections registered and denied-after-gate (completed 2026-08-14)

## Phase Details

**Dependency spine.** The milestone opens on the **SPEC** (Phase 1) because eight of the fourteen
catalogued pitfalls are shape decisions — message shape, audience matrix, lifecycle vocabulary,
`expected_version` placement — whose cost explodes once code exists, and because PROJECT.md's Out-of-Scope
entry for non-scene portal surfaces named exactly this SPEC as its precondition. **Vocabulary before
surfaces** (Phase 2): privacy and admin-section policy must exist before anything reads or gates on them,
and the **character lifecycle column** and **normalized-name unique index** are each load-bearing for two
later phases, so they land once here rather than being rediscovered per consumer. **Domain before facade
before UI** (Phases 3 → 4 → 5) mirrors the shipped scenes path exactly. **Admin goes last** (Phase 6)
because it consumes the most and because `internal/web/` contains **zero `RoleAdmin` references today** —
a net-new trust boundary with no existing test suite that would notice if it were wrong.

Phase 3 is *planning*-parallelizable with Phase 2, and its `Retire` needs Phase 2's lifecycle column.
(The former `Rename` ordering constraint — MUST NOT land before Phase 2's unique index, since it adds a
writer to a live check-then-insert race — moves with `Rename` to Phase 999.20; Phase 2's index has since
shipped, so it is satisfied either way.)

**Scheduling note (not a dependency):** `WebCheckSessionResponse.roles` (ADMIN-08) can be pulled into
Phase 4's proto work to avoid a second `web.proto` regeneration cycle in Phase 6. Likewise, Phase 4
should land the full SPEC-defined profile field set — including the PROFILE-06..09 fields that Phase 5
verifies — in one regeneration pass.

**Research posture.** Phases 3 and 6 carry `--research-phase` flags (below); Phase 2 needs a narrow
data audit only. Phases 1, 4, and 5 have well-understood patterns and should skip research — Phase 1 is
synthesis of `research/SUMMARY.md` rather than new research, Phase 4 is a verbatim copy of a fully-traced
shipped path (`internal/grpc/sceneaccess_service.go`), and Phase 5 is established shadcn/runes patterns
plus the existing `web/src/lib/scenes/createFlow.ts` idiom.

**PORTAL-10 is binding on every phase.** The six verification-integrity rules (census with set equality;
paired positive controls on every denial test; assertions against marshaled response bytes; gates
demonstrated RED against the pre-fix state; wire-level assertion of every opacity and authorization contract; invariant-scope
discipline) are SPEC content, not a capability — but every phase plan below carries them as acceptance
criteria. v0.12's audit catalogued 17 instances of *"a verification that cannot fail"*, and research found
that the natural test for nearly every privacy and authorization property in this milestone **passes while
the property is false**.

### Phase 1: Portal SPEC

**Goal**: Produce the committed portal SPEC that fixes the audience/message shape, character data and lifecycle model, per-field privacy model, media-ready profile schema, and the full new RPC surface — the precondition PROJECT.md's Out-of-Scope entry demanded, satisfied rather than waived.
**Depends on**: Nothing (opens the milestone)
**Requirements**: PORTAL-01, PORTAL-02, PORTAL-03, PORTAL-04, PORTAL-05, PORTAL-06, PORTAL-07, PORTAL-08, PORTAL-09, PORTAL-10
**Success Criteria** (what must be TRUE):

1. The SPEC names an **audience matrix** (public / owner / admin) with a distinct message shape per audience, such that a field a viewer may not see is **absent from the response** rather than present-and-hidden by the client — and backs it with a **read-surface inventory** enumerating every character-returning RPC, including all **four** existing public export surfaces — the fourth is `WebListPublishedScenes`, whose `participants_snapshot` is a frozen participant projection served unauthenticated in bulk — with the audience each serves.
2. The SPEC fixes the character **lifecycle** as three distinct operations — `retire`, `idle-out`, `purge` — states in normative language that **retire MUST NOT release the name**, and carries a **name-capture surface inventory** giving each denormalized-name site (immutable event payloads, `scene_log` via `WebGetPublicSceneArchive`) a historical-vs-live verdict.
3. The SPEC defines the profile/media data model as `entity_properties` rows (`profile.*`, `profile.image.primary`, `profile.image.gallery.00..09`) with intrinsic values (`name`, `description`, lifecycle status, `version`) staying columns, and states character-name and player-username normalization as **two separate policies**.
4. Every mutation request message in the SPEC's RPC surface carries **`expected_version`**; the SPEC records role mutation as an **explicit exclusion** from character administration; and it answers "does any v0.13 surface sort or filter on a profile field?" with a stated verdict rather than silence.
5. The SPEC mandates the six **verification-integrity rules** (census set-equality, paired positive controls, marshaled-bytes assertions, gates demonstrated RED pre-fix, wire-level opacity assertions, invariant-scope discipline) as binding acceptance criteria that every later phase plan inherits.

**Plans**: 6/6 plans executed
**UI hint**: no

Plans:
**Wave 1**

- [x] 01-01-PLAN.md — SPEC skeleton + profile/media data model + profile visibility model, declared and registered (tracer)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 01-02-PLAN.md — audience matrix + per-audience message shape + exhaustive read-surface inventory

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 01-03-PLAN.md — character lifecycle + name-capture surface inventory + name normalization policy

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 01-04-PLAN.md — full RPC surface with `expected_version` + admin IA and exclusions + sorting/filtering verdict

**Wave 5** *(blocked on Wave 4 completion)*

- [x] 01-05-PLAN.md — verification-integrity rules + amendments/divergences table + applying the nine amendments

**Wave 6** *(blocked on Wave 5 completion)*

- [x] 01-06-PLAN.md — citation verification sweep + grounding trace + spec-location pointer edit + completeness gate

### Phase 01.1: Migration framework: adopt goose for Go migrations (INSERTED)

**Goal**: Replace golang-migrate with goose so Go code can run as a numbered migration interleaved with the SQL ones — the capability Phase 2's normalized-name backfill requires and golang-migrate structurally cannot provide (it has no hook surface, and startup applies ALL migrations via `Up()` inline at `cmd/holomush/core.go:279-282` before any bootstrap step can run, so a same-release migration depending on a Go backfill is unreachable).
**Requirements**: None — enabling work; no v0.13 REQ-ID maps to it.
**Depends on:** Phase 1
**Gates**: Phase 2 execution
**Success Criteria** (what must be TRUE):

1. All 44 migration pairs (88 files) are converted to goose's single-file `-- +goose Up` / `-- +goose Down` format, and a fresh-database migrate-up produces an **application schema** byte-identical to the pre-conversion schema — proven by a one-time `pg_dump --schema-only` diff with the bookkeeping objects excluded (`schema_migrations`, `goose_db_version`, `goose_db_version_id_seq`), not by tests passing. The bookkeeping end-state is asserted separately: `goose_db_version` present with 44 rows at `version_id > 0` plus goose's version-0 bootstrap row (45 total), and no `schema_migrations`.
2. The recorded-state cutover from `schema_migrations` (a single current version) to `goose_db_version` (a row per applied migration) is proven against a database seeded at the pre-conversion version, **including the negative control that a second application is a no-op or refuses** — applying it twice is unrecoverable.
3. `holomush migrate` keeps up/down/status/version parity, and `force` is **removed** along with its docs and tests in the same change — goose commits the migration body and its version row in one transaction, so the dirty state `force` exists to repair cannot arise. Startup auto-migration still runs before bootstrap with `HOLOMUSH_DB_AUTO_MIGRATE` honored.
4. A Go migration registered between two SQL migrations runs in version order, demonstrated by an **integration test with a fixture chain** (a small SQL→Go→SQL chain in a testcontainer, with the Go step observing the prior SQL step's effect) — so the capability Phase 2 depends on is proven before Phase 2 relies on it, without adding anything permanent to the real 44-migration chain.
5. `.claude/rules/database-migrations.md` and the migration meta-tests are updated to the new format in the same change.

**Plans:** 7/7 plans complete
**UI hint**: no
**Research flag**: resolved in discussion — goose ships no baseline / adopt-existing command, so adopt derives the 44 rows from the embedded migration FS (D-02/D-04). Seeding is the one unrecoverable step.

Plans:

**Wave 1**

- [x] 01.1-01-PLAN.md — tracer: swap golang-migrate for goose and convert the 44 pairs to single-file format, proven end-to-end on a fresh database (criterion 1, criterion 3's `force` removal)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 01.1-02-PLAN.md — integration-tier rework: one-direction migration accessor, its three callers, and the bookkeeping end-state spec
- [x] 01.1-03-PLAN.md — format enforcement: D-13 `$$`↔StatementBegin/End meta-test and the static `lint:no-timestamptz` glob pins (criterion 5)
- [x] 01.1-04-PLAN.md — the adopt gate (D-01..D-06) with both negative controls and the ascending-order rollback guard (criterion 2)
- [x] 01.1-05-PLAN.md — supply-chain retirement: the five `osv-scanner.toml` docker/docker suppressions and issue #4817

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 01.1-06-PLAN.md — Go/SQL interleave proof and the D-08 package seam with its registration guard (criterion 4)
- [x] 01.1-07-PLAN.md — rules, contributor guide, `new-migration` skill, operator docs, and the D-16 rehearsal / D-18 expiring rollback (criterion 5)

### Phase 2: ABAC & Schema Vocabulary

**Goal**: Land the authorization vocabulary, name policy, and schema primitives every later phase gates on — `admin_section:` + `seed:admin-section-access`, `seed:profile-public-read`, the character **lifecycle column**, and the **normalized-name unique index** — with no UI and no new RPCs, which is where an unverified assumption surfaces cheapest.
**Depends on**: Phase 1
**Requirements**: IDENT-06, IDENT-07, IDENT-08, IDENT-09, PROFILE-11, EXT-07
**Success Criteria** (what must be TRUE):

1. A character name that is visually identical to an existing one — differing only by NFKC-normalizable codepoints, `Cf` format characters, or a mixed-script confusable — is rejected server-side, and a name matching the configurable regex block list is rejected server-side, both at create and at rename.
2. Two concurrent attempts to claim the same normalized name against real Postgres cannot both succeed; the gate is **demonstrated RED against today's unindexed schema before the unique index lands**, and any pre-existing duplicates are detected and resolved by a one-shot job first (migrations forbid in-migration backfills).
3. Player usernames still reject non-ASCII input — the existing `^[a-zA-Z][a-zA-Z0-9_]*$` rule is pinned by a regression guard rather than re-implemented.
4. An off-location viewer can read a character's **public properties** where `seed:player-character-colocation` previously **denied** it — shipped only after an audit establishes exactly which existing `parent_type='character' AND visibility='public'` rows the widened policy exposes. The **in-world-description half is deferred to Phase 4** by D-29 (`02-CONTEXT.md`): the `resource is character` permit that would carry `characters.description` also gates `world.Service.GetCharacter`, whose `characterToProto` projection returns `PlayerId` and `LocationId` and whose `principal is character` test admits every ephemeral guest — so it lands with Phase 4's projection narrowing rather than ahead of it. The description audit moves with it.
5. `seed:admin-section-access` permits an admin and denies a builder, a plain player, and a guest across **all seven section ids** — each denial paired with a positive control proving the subject would otherwise have been permitted, and the id list asserted by set equality — and an eighth section added later needs no new policy.

**Plans**: 13/13 plans executed
**UI hint**: no
**Research flag**: narrow slice only — the existing-public-character-property audit is a *data* question, not a design one, but it must be answered before `seed:profile-public-read` merges. Full `--research-phase` is not warranted.

**Sketch findings** (must be answered in this phase): **A1** — `characters.last_active_at` does not exist and cannot be derived (`sessions` rows are reaped; `session_connections.last_seen_at` is a gateway lease). Needs a durable column (epoch-ns `BIGINT`) + a §11.3 row permitting sort/filter. **A2** — the admin list sorts by the joined `players.username`, which §11.3 never enumerates; §11.3's `characters.player_id` row ("never an ordering") stays correct, add a new row. **D1 (DEFECT, route to `abac-reviewer`)** — §10.3 requires a planned-section refusal to reveal nothing about which sections exist, but §10.4 defines two distinguishable denial codes (`DENY_ADMIN_SECTION` vs `DENY_ADMIN_SECTION_UNREGISTERED`), giving a registry-enumeration oracle. §13 pins none of it though `INV-PRIVACY-9` does the same job for profiles. Source: `.planning/sketches/002-*/README.md`, `003-*/README.md`. **Name pipeline UI** (009): §6.1's four steps run before every check, so "is it taken" is asked about the *normalized key*. Three accepted cases still **rewrite** what the player typed; the winner (submit-and-report) accepts that because rename exists. The confusable message MUST NOT name the colliding character, and it is safe **only because names are public at the `anonymous` floor** — if a game raises that floor (§8.6 permits it) the message becomes an oracle. A name of only invisibles looks like an empty box to the player, so "please enter a name" needs different wording.

Plans:

- [x] 02-01-PLAN.md — TRACER: confusable-name rejection end-to-end (generator, skeleton, §6.1.1 pipeline, migration 000054, one create path)
- [x] 02-02-PLAN.md — Mixed-script restriction, empty-normal-form, and the IDENT-08 username regression pin
- [x] 02-03-PLAN.md — ABAC vocabulary: viewer/profile/admin_section prefixes, ViewerTierProvider with its roles seam
- [x] 02-04-PLAN.md — Character lifecycle: closed status vocabulary, exhaustive reads, INV-WORLD-5 and INV-WORLD-6
- [x] 02-05-PLAN.md — Configurable name block list: compiled snapshot, two-signal poller, boot validation
- [x] 02-06-PLAN.md — Gated name admission: the `charname.Admitted` token, the writer boundary, and the admission census
- [x] 02-07-PLAN.md — Seed policy family: tier floors, viewer twins, reachability, admin-section, public-read widening
- [x] 02-08-PLAN.md — Profile-visibility conjunction helper with the fourth-rung and additive-permit RED gates
- [x] 02-09-PLAN.md — Admin section registry, gate-then-distinguish, INV-PRIVACY-11
- [x] 02-10-PLAN.md — PROFILE-11 exposure audit artifact and the widening's paired proof
- [x] 02-12-PLAN.md — Duplicate detection, Go backfill, unique index, resolution CLI, RED-first uniqueness proof
- [x] 02-13-PLAN.md — Row-identity resolution: player-scoped roles, PropertyProvider player-keyed peers, provider registration
- [x] 02-11-PLAN.md — SPEC amendments, validation map, abac-reviewer routing, phase gate

### Phase 02.1: World Caller Model (INSERTED)

**Why this exists (inserted 2026-08-07):** `world.Service` takes `subjectID string` and internally
reconstructs a `types.AccessRequest{Subject, Action, Resource, Attributes}` with `Attributes`
hardcoded `nil` (`internal/world/service.go:214`). The authorization layer has carried a per-call
attribute channel since Phase 3b — `types.NewAccessRequest`'s 4th parameter
(`internal/access/policy/types/types.go:143`), overlaid onto `bags.Action` and readable in the DSL
as `action.*` (`engine.go:252-265`) — and the world API has no way to reach it. That is a
pre-existing modeling defect in the caller argument, not a gap created by background jobs; jobs are
simply the first caller that makes it visible. Threading it as a variadic option was rejected: an
execution context that every call semantically has should not be optional. Full derivation:
`.planning/phases/02.2-background-job-authorization-model/02.2-CONTEXT.md` D-56.

**Goal**: `world.Service`'s 21 public commands take a typed caller value instead of a bare
`subjectID string`, so caller identity and the execution context it acts under travel together and
cannot be supplied half-way. `checkAccess` forwards that context to `types.NewAccessRequest`,
replacing the hardcoded `nil`.
**Depends on**: Phase 2 (ABAC vocabulary, attribute-provider substrate)
**Blocks**: Phase 02.2 (the job model needs this carrier), and transitively Phase 3
**Requirements**: AUTHZ-01 (minted 2026-08-08 during `/gsd-discuss-phase 02.1`)

**Shape (decided 2026-08-07, `02.2-CONTEXT.md` D-56/D-57):** typed constructors, not a bare struct
— `world.HumanCaller(subjectID)`, `world.JobCaller(name, provenance)`, `world.SystemCaller()` —
so invalid combinations (a human carrying job provenance, a job with no provenance) are
unrepresentable, and the `job.`-namespaced attribute keys are produced in exactly one place.
*(Amended 2026-08-08, `02.1-CONTEXT.md` D-62: `JobCaller` itself lands in Phase 02.2 once its
provenance vocabulary settles; 02.1 ships `HumanCaller`/`SystemCaller` plus the caller type's
internal attribute channel, so adding `JobCaller` later is purely additive — no signature churn,
no `checkAccess` change.)*

**Verified blast radius (2026-08-07 — grep-confirmed, do not re-estimate from method count):**

- **21 public `Service` methods** take `(ctx context.Context, subjectID string, …)` — a uniform
  slot on every one, so the change is symmetric rather than per-command.

- **47 production call sites** across 12 files; **347 test call sites** across 13 files.
- **3+ interfaces redeclare the signatures** and must move in lockstep:
  `internal/world/mutator.go` (11 methods), `internal/command/types.go` (9+),
  `internal/grpc/server.go` (2) — plus mockery regeneration for their mocks.

- `subjectID` is positional arg 2 on all 21 methods, so the test-site migration is a structural
  codemod (`ast-grep`), not 347 judgment calls.

**Success Criteria** (what must be TRUE):

1. No public `world.Service` command takes a bare `subjectID string`; every one takes a typed
   caller value, and there is no overload or variadic escape hatch that preserves the old shape.

2. `checkAccess` passes the caller's execution context to `types.NewAccessRequest` — the
   hardcoded `nil` at `service.go:214` is gone — and a world write can reach the DSL as `action.*`.

3. Behavior is unchanged for every existing caller: the 47 production sites migrate to
   `HumanCaller`/`SystemCaller` with identical authorization outcomes, proven by the existing
   suites passing without assertion changes.

4. `internal/grpc/location_follow.go:197` — the **single** production `access.WithSystemSubject`
   call site — is migrated to an explicit `SystemCaller`, so the ambient context marker is no
   longer how a system operation is declared at the world boundary.

5. `abac-reviewer` returns READY: the refactor changes how the subject and its context reach the
   engine, which is an access-control surface even though no policy text changes.

**UI hint**: no
**Research flag**: `--research-phase` recommended — `SystemCaller()` interacts with the S1 defense
(`engine.go:92-101` requires **both** a bare `"system"` subject **and** `access.IsSystemContext(ctx)`;
a bare subject without the marker is a hard `SYSTEM_SUBJECT_REJECTED`), so a caller *value* must
influence the *context*. That seam is unverified.

**Plans:** 3/3 plans complete

Plans:

- [x] 02.1-01-PLAN.md — Tracer: `world.Caller` type + constructors, the `checkAccess` seam, and the
  `GetLocation`/`GetExitsByLocation` slice end-to-end (proves criteria 2 and 4); plus the committed
  ast-grep codemod rules (D-63).

- [x] 02.1-02-PLAN.md — The atomic flip: the remaining 21 command signatures, all six redeclaring
  interfaces, the 31 production call sites, the `internal/property` chain (D-66), and the codemod
  over the test tier; gated on `task test` **and** `task test:int`.

- [x] 02.1-03-PLAN.md — `INV-WORLD-8` census binding + registry entry + `INV-WORLD` scope amendment
  (D-65/D-67/D-68), then the `abac-reviewer` READY gate and `task pr-prep`.

### Phase 02.2: Background Job Authorization Model (INSERTED)

**Why this exists (inserted 2026-08-07, renumbered from 02.1 the same day):** Phase 3's retirement
reactor surfaced a platform gap rather than creating one. A host subsystem that consumes an event
and then performs a world write has no honest way to authorize itself today. Three candidate
answers were examined and all three fail: a synthetic `system:retirement` principal cannot be
narrowed, because `parseEntityType` (`internal/access/policy/engine.go:542-548`) matches on the
prefix only, so any permit written for it also grants `system:bootstrap`; borrowing the originating
actor off the envelope (`Envelope.Actor`, set from the same `subjectID` that passed `checkAccess` —
`internal/world/service.go:1079`) is wrong on the merits, because a player authorized to retire
their own character was never authorized to end sessions, emit to a location, or move a character;
and `access.WithSystemSubject` (`internal/access/context.go:12`) is a total bypass
(`engine.go:91-105`) that puts every future background consumer outside the default-deny
chokepoint. Full derivation: `.planning/phases/03-world-character-commands/03-CONTEXT.md` D-45/D-47.

**Goal**: Background jobs — event-driven reactors, flushers, and any future host subsystem that
acts on the world — get a first-class ABAC identity **plus per-execution attributes the policy
engine can test**, so a job's authority is scoped to the work it is currently performing rather
than granted as a blanket capability or borrowed from the human whose command triggered it.
**Depends on**: Phase 2 (ABAC vocabulary, attribute-provider substrate, schema registry),
**Phase 02.1 (World Caller Model)** — `JobCaller` is the carrier this phase's attributes ride on.
**Requirements**: AUTHZ-02 (minted 2026-08-08 during `/gsd-discuss-phase 02.1`)
**Blocks**: Phase 3 (its retirement reactor consumes this model)

**Substrate already verified (2026-08-07), so this is composition, not invention:**

- **Per-execution attributes already have a carrier.** `types.NewAccessRequest(subject, action,
  resource, attrs)` (`internal/access/policy/types/types.go:143`) takes per-call attributes,
  reserved-key-validates them (`:153-159`), deep-clones them (`:160-166`), and the engine overlays
  them onto `bags.Action` (`engine.go:252-265`) where the DSL reads them as `action.*`. Reaching it
  from a world write is **Phase 02.1's** job, not this phase's.

- **Instance-scoping is expressible — proven, not assumed.** `Comparison.Left`/`Right` are both
  `*Expr` and `Expr` may be an `AttrRef` (`internal/access/policy/dsl/ast.go:145-150,208-222`), and
  16+ shipped seeds already compare across bags (`seed.go:41,47,113,131`). So
  `when { action.job.trigger_subject == resource.id }` parses and evaluates. This is the concrete
  reason a grants-only design was rejected — no static grant list can express it.

- **The provider shape has a 15-line template.** `PluginProvider`
  (`internal/access/policy/attribute/plugin_provider.go:36-58`) is a non-character principal
  provider gated on a registry — `Namespace()`, `ResolveSubject` returning `nil, nil` for
  non-matching refs, `ResolveResource` returning `nil, nil`, and `Schema()`. An unknown plugin
  resolving to `nil, nil` is what makes it fail closed; the job provider copies that property.

- **`action` is the strictest namespace in the compiler — and registering it is load-bearing.**
  `validateAttributes` (`internal/access/policy/compiler.go:149-170`) skips unregistered
  namespaces, warns on an undeclared key in a registered namespace, but **hard-errors** for
  `action` specifically. `seed.go:332` already ships `action.dispatch_location` with no `action`
  namespace registered, so registering `action` without declaring that key in the same change
  turns a shipped seed into a boot-time compile error.

**Success Criteria** (what must be TRUE):

1. A background job authorizes its world writes under **its own identity** — not a borrowed
   human actor's, and not an ABAC bypass. `access.WithSystemSubject` appears on no reactor or
   flusher path.

2. Authority is scoped by **per-execution runtime attributes**, not static grants alone: policy
   can express "this job may write only the aggregate whose event it is currently handling", and
   a test proves the same job is DENIED against a different aggregate.

3. The `action` namespace is registered in the schema registry with **every** key it must carry —
   the job provenance triple, the already-shipped `dispatch_location`, and the resolver-owned
   `name` — so a typo'd `action.*` reference is a policy-compile failure at boot (cache reload) rather than a silent
   default-deny, and no shipped seed regresses.

4. Every background consumer gets a `job:` identity and a **declared capability class**
   (`principal.job.name`, `principal.job.writes`, via the liveness-gated registry and provider);
   **only event-driven** consumers additionally get **per-execution instance scoping** (the
   provenance triple bound against `resource.id`). A timer-driven job's authority is therefore
   **necessarily coarse**, and the documentation says so plainly rather than implying it is
   instance-scoped. Existing `WithSystemSubject` / `SystemCaller()` call sites are enumerated,
   with the note that `rg 'SystemCaller\(\)'` — not `rg WithSystemSubject` — is the enumerating
   grep after Phase 02.1 (migration itself belonged to Phase 02.1).

5. `abac-reviewer` returns READY on the new principal type, its provider, its schema, and its
   seeds.

**UI hint**: no
**Research flag**: `--research-phase` recommended — the reserved-key interaction
(`IsReservedActionKey`) plus `warnOnMissingSeedCoverage` / `RegisteredNamespaces()` coupling are
unverified seams, and timer-driven jobs (Phase 3's `last_active_at` flusher) carry no triggering
event, so what they present as per-execution context is an open question that MUST NOT be invented
by a planner.

**Plans:** 5/5 plans complete

Plans:
**Wave 1**

- [x] 02.2-01-PLAN.md — Tracer: the `job:` principal end to end (subject, registry, provider, JobCaller, fixture seed, paired permit/deny)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 02.2-02-PLAN.md — Production ABAC wiring + the D-58 principal-aware deny code

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 02.2-03-PLAN.md — The exhaustive `action.*` audit + D-60 `action` namespace registration

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 02.2-04-PLAN.md — D-66 compiler↔SchemaRegistry wiring (4 sites) + D-67 fatal-for-all-sources

**Wave 5** *(blocked on Wave 4 completion)*

- [x] 02.2-05-PLAN.md — INV-ACCESS-13, the contributor/operator doc, and the criterion-4 amendment

### Phase 3: World Character Commands

**Scope narrowed (2026-08-06):** `RenameCharacter` was **removed from this phase and from the v0.13 milestone** and moved to the backlog as **Phase 999.20**, linked to Phase 999.6 (Character Rostering & Transfer). Reason: rename cannot be specified until the character identity model gains an **approval dimension**, which does not exist — `characters.status` is `active|retired|idle` only, so the intended "rename permitted only before a character is approved for play" rule has no state to read. Designing that dimension touches bound `INV-WORLD-5`, 01-SPEC §4.4, PORTAL-04, character creation's initial state, and an approving actor (an admin surface, Phase 6), which is milestone-scale. Retire has **no** dependency on approval and stays. Full rationale: `.planning/phases/03-world-character-commands/03-CONTEXT.md` D-44.

**Goal**: `world.Service` gains a soft `RetireCharacter` / `UnretireCharacter` pair at the domain layer, version-guarded and emitting through the transactional outbox in-transaction, plus the host-side reactor that makes a retired character actually leave active play, with the `writeCommands` census rows and taxonomy kinds landed in the same change.
**Depends on**: Phase 1 (SPEC), **Phase 02.2 (Background-Job Authorization Model)** and transitively **Phase 02.1 (World Caller Model)**. Planning parallelizes with Phase 2; execution requires Phase 2's lifecycle column before `Retire`, and the retirement reactor requires 02.2's job-identity model before it can authorize its `MoveCharacter` call at all (see 03-CONTEXT.md D-45, superseded 2026-08-07 by D-47; the job model was renumbered 02.1 → 02.2 on 2026-08-07 when the world caller model was split out ahead of it — see 02.2-CONTEXT.md D-56).
**Requirements**: IDENT-04, IDENT-10
**Also lands here**: the `last_active_at` write seam Phase 2 deferred (D-24) — a *separate*, general-purpose character-activity subsystem, unrelated to retirement beyond sharing this phase. Phase 3 therefore adds **two** subsystems (`SubsystemID` 18→20, two 5-site compile cascades).
**Success Criteria** (what must be TRUE):

1. A retired character leaves active play with its record intact and **its name still reserved**, and the retirement is reversible — retire, idle-out, and purge stay three distinct operations, and the irreversible `DeleteCharacter` path (which cascades `entity_properties` and emits a tombstone) is untouched by the retire flow.
2. Retirement is *observably* effective, not merely recorded: a host reactor consuming `character_retired` ends the character's live sessions, notifies the location it left, and moves it to the configured starting location.
3. A stale `expected_version` on any new character mutation is rejected with the typed `WORLD_CONCURRENT_EDIT` signal rather than silently overwriting — v0.12's existing two-replica resilience harness, pointed at the new commands, passes.
4. The `writeCommands` census and the mutation taxonomy list the new commands in the same change that introduces them; the census meta-test fails if either is missing.
5. `characters.last_active_at` is actually written — Phase 2 shipped the column and every read path, but nothing writes it. A character's activity updates it without a per-event database write, and `INV-WORLD-4`'s writer enumeration is amended in the same change that adds the writer.

**Plans**: 6/6 plans executed
**UI hint**: no
**Research flag**: `--research-phase` recommended — the `writeCommands` census bijection semantics (`internal/world/mutator.go:78-100`) are genuinely unverified, and this repo has a documented history of plans failing on unverified seam assumptions.

**Sketch findings**: **Where `last_active_at` is written** — **resolved 2026-08-06** (03-CONTEXT.md D-42): an event listener over session start/end plus character activity, buffered in a **NATS JetStream KV** bucket (which MUST set `Storage: FileStorage` explicitly — buckets do not inherit the stream's config) and flushed periodically to the column by its **own general-purpose subsystem**, separate from the retirement reactor. The seam MUST NOT be the lease-refresh path (`internal/session/session.go:485` `RefreshConnection`). The flusher is a fourth out-of-world writer and amends `INV-WORLD-4`'s enumeration in the same change. **Can admins rename at all?** — deferred to Phase 999.20 with rename; sketch 004's `Rename…` affordance is **not** live in v0.13. **Sketch 009-A depended on rename existing** — that dependency is now unmet; sketch 009 finding #5 ("names are reserved, not permanent") is **false for v0.13** and the creation copy must be corrected. **Roster:** a non-`active` lifecycle MUST suppress the shipped session badge (`Active`/`Offline`), which is a *different vocabulary* from `characters.status` — note the split is already structural (session status lives on the session row, `internal/session/session.go:21`), so this is a rendering concern, not a missing column. Player self-retire is **not** specified — every retire path sketched is `AdminRetireCharacter`. Source: `.planning/sketches/002-*/README.md`, `004-*/README.md`.

Plans:
**Wave 1**

- [x] 03-01-PLAN.md — Retire/Unretire domain commands (caller-shaped): tracer through `mutate()`, two taxonomy kinds + census rows, `CharacterRepository.SetStatus` CAS, the D-34 default-character clear (wave 1)
- [x] 03-02-PLAN.md — Shared substrate: the D-46 consumer-retry relocation, two subsystem skeletons (`internal/retirement`, `internal/charactivity`), and the full 13-site `SubsystemID` 18→20 cascade landed once (wave 1)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 03-03-PLAN.md — IDENT-10 proof: two-replica stale-version rejection `Describe` in the resilience harness, retire row+envelope atomicity, name-reservation assertion, INV-WORLD-6 defect filed (wave 2)
- [x] 03-04-PLAN.md — Retirement reactor: sessions-only fanout idempotent under redelivery, `JobCaller` authorization per 02.2's landed model, admin-only retire/unretire surface (user decision 2026-08-07; no player self-retire seed) + conditional `job:retirement` grant (wave 2)

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 03-05-PLAN.md — `last_active_at`: writer-boundary flush function, KV listener/flusher subsystem with revision-conditional deletes, the `INV-WORLD-4` THREE→FOUR enumeration amendment, and the `WithCharacterActivity` harness option + charactivity suite (wave 3)

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 03-06-PLAN.md — Full-stack retirement proof: `WithOutboxRelay`/`WithRetirementReactor` harness StartOptions and the `test/integration/retirement/` suite (fanout, feed order, redelivery idempotency, instance-scope DENY) (wave 4)

### Phase 4: Shared Facade Helpers & `CharacterAccessService`

**Goal**: Extract `resolveAndGate`/`ownedCharacter` into one shared place, then build the `CharacterAccessService` BFF facade and its `WebCharacter*` proxies so character read and write reach the web with unauthorized fields absent from the marshaled response by construction.
**Depends on**: Phase 2 (ABAC vocabulary + `profile.*` convention), Phase 3 (domain commands)
**Requirements**: IDENT-02, IDENT-02a, PROFILE-03, PROFILE-04, PROFILE-05, PROFILE-10, EXT-06
**Success Criteria** (what must be TRUE):

1. The guest gate and ownership check exist in **exactly one place**, and a census with **set equality** over every character-returning RPC proves each one routes through it — so an RPC added later that skips the gate fails the test rather than shipping.
2. A field the viewer may not see is **absent from the marshaled response bytes** — asserted against the wire, not a populated Go struct — and a character whose profile is unreachable returns a not-found-equivalent, never "this profile is private".
3. The **game-configured, per-attribute viewer-tier floor** governs every profile field — visibility is configuration, not an owner control (v0.13 ships no player or character agency over it) — and the configuration cannot raise `name` or `pronouns` above the profile's own reachability floor; an unrecognized tier is denied by an exhaustive `switch` with `default: deny`.
4. An owner can edit prose profile fields and the in-world `characters.description` over the web, with over-cap input rejected server-side and the description write reaching the existing `world.Service.UpdateCharacterDescription` rather than a parallel path.
5. The profile read path is built **exclusively** from the viewer-filtered property slice — a direct `PropertyReader.ListByParent`/`PropertyRepository.ListByParent` call from the facade fails the build or the test — and the proto ships the media shape now, empty: `ProfileImage{media_id, alt_text, content_warning}` + `primary_image` + `repeated gallery [max_items = 10]`.
6. An off-location viewer can **read** a character's in-world description where `seed:player-character-colocation` previously **denied** it — the half of Phase 2's criterion 4 deferred here by D-29. It ships only together with the criterion-2 projection narrowing, so the read path returns `description` without `PlayerId` or `LocationId`, and only after an audit establishes exactly which existing character descriptions it exposes. A permit of the bare shape `permit(principal is character, action in ["read"], resource is character)` — unconditional, gating the whole `CharacterInfo` projection — does **not** satisfy this criterion.

**Plans**: 9/9 plans executed
**UI hint**: no

**Sketch findings** (must be answered in this phase): **A3** — `AdminSearchCharacters` (§9.2) currently "searches names" (character names); the admin list needs it extended to player usernames. **ACCEPTED** as the design, implemented in **Phase 6** — D-72 defers the admin RPCs; answered 2026-08-10 by D-81, no Phase-4 code. **A2's RPC half** — the list RPC must accept a sort key for the joined `players.username`. **ACCEPTED** as the design, implemented in **Phase 6**; §11.3's row was already amended in Phase 2 by D-26. **Admin rename census decision** (see Phase 3) — **WITHDRAWN.** Phase 3 D-44 removed rename from v0.13, so sketch 004's `Rename…` affordance is not live in v0.13 and sketch 009 finding #5 ("names are reserved, not permanent") is false for v0.13; answered 2026-08-10 by D-81, no code. Source: `.planning/sketches/002-*/README.md`.

Plans:
**Wave 1**

- [x] 04-01-PLAN.md — Tracer: an anonymous off-location visitor reads a character's name and in-world description end to end (proto slice, `read_description` seeds, facade, web proxy, wiring)
- [x] 04-02-PLAN.md — Extract `resolveAndGate` and `ownedCharacter` onto one embedded `playerGate`
- [x] 04-03-PLAN.md — Amend 01-SPEC §9.3/§9.4.2 for the struck `RenameCharacter` row; record the three sketch verdicts

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 04-04-PLAN.md — The viewer-filtered property slice, marshaled-bytes absence, tier exhaustiveness, and the remaining Phase-4 proto surface
- [x] 04-09-PLAN.md — The profile-attribute domain write: taxonomy kind, mutator seam, and the guarded `world.Service` command (runs at wave 2, before 04-06)

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 04-05-PLAN.md — The owner audience (`ListMyCharacters`, `GetMyCharacter`) and the alt-linkage invariant

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 04-06-PLAN.md — The write facade: mask allowlist, byte caps, version guard, and the description edit

**Wave 5** *(blocked on Wave 4 completion)*

- [x] 04-07-PLAN.md — `ListCharacterDirectory` and the removal of `WebListAllCharacters`

**Wave 6** *(blocked on Wave 5 completion)*

- [x] 04-08-PLAN.md — The routing census and the descriptor-derived character-returning RPC census

### Phase 5: Character Identity UI & Public Profiles

**Goal**: Web players get the whole identity surface — a structured creation card replacing the name-only stub, one place to manage every alt, and a public profile page a logged-out visitor can read — plus the media-schema proof with no uploader.
**Depends on**: Phase 4
**Requirements**: IDENT-01, IDENT-05, PROFILE-01, PROFILE-02, PROFILE-06, PROFILE-07, PROFILE-08, PROFILE-09, PROFILE-10a, PROFILE-12, EXT-05, EXT-08
**Success Criteria** (what must be TRUE):

1. A player creates a character through a **structured identity card** (name, pronouns as its own field, concept, species, age, faction) instead of the name-only stub, and manages every one of their characters — including which is default — from one place.
2. A **logged-out visitor** loads a character's public profile at a stable URL and sees the in-world description alongside the public `profile.*` fields — rumors / RP-hooks, the volatile "Currently" line, the OOC RP-preferences block, and time zone — with blank fields hiding themselves and an initial-letter avatar placeholder where no image exists.
3. Profile and sheet are **separate surfaces** and the sheet ships **empty**; the profile action bar carries a named empty slot for web DMs rather than a dead "message this character" button.
4. A change to the game's viewer-tier configuration is what a logged-out visitor sees on the **next load** — the floor is evaluated at read time and never stamped onto a row, so no backfill exists; and both the retirement flow and the surface where a player authors profile fields **state in the UI** that privacy is not retroactive over already-published history.
5. One primary plus ten gallery image property rows insert through the **real schema** and read back, and an eleventh primary is rejected by `UNIQUE(parent_type,parent_id,name)` — demonstrating the "no migration later" claim rather than asserting it, with no uploader.

**Sketch findings** (design decided in sketches 007, 008, 009; read `.planning/sketches/MANIFEST.md` round-2 findings before planning): **profile = identity card, not a long-form page** (007-C) — a bounded card carrying portrait/name/pronouns/concept/description that is complete at any fill level, with long-form sections growing *below* it and simply absent when withheld. **The page MUST NOT explain its own sparseness** — §7.5 + §8.9 make a blank field and a withheld field indistinguishable, so no counts, no lock icons, no greyed sections; a sign-in invitation is legal only if **unconditional**, and 007-C ships none. **Under the seeded defaults `guest` and `player` render identically** (no §8.6 row seeds `player`), so any tier preview must derive distinct outcomes from the live floor set, not offer a hardcoded three-way toggle. **The gallery never renders in v0.13** — §7.3 ships the media model with zero upload behavior, so build the renderer but ship no empty "coming soon" slots. **Roster is sectioned** (008-B): `Playable` grid first with the create card, then `Not playable`; every card in the top grid is uniformly clickable. **A non-`active` lifecycle MUST suppress the session badge** — the shipped `Active`/`Offline` badge is *session* state and collides with `characters.status`. **Creation is submit-and-report** (009-A), no live availability check — it cannot be honest across check-and-insert. Source: `.planning/sketches/007-*/README.md`, `008-*/README.md`, `009-*/README.md`.

**Plans**: 7/8 plans executed
**UI hint**: yes

Plans:
**Wave 1**

- [x] 05-01-PLAN.md — TRACER: `SetDefaultCharacter` end to end (proto → census → repo → facade → proxy → roster control)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 05-02-PLAN.md — The public profile route `/c/[id]` and the absence contract
- [x] 05-03-PLAN.md — `CreateCharacter` reshape: proto, census, constructor, facade handler, repointed proxy

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 05-04-PLAN.md — The owner authoring surface `/characters/[id]`, per-section save, PROFILE-12 notice
- [x] 05-05-PLAN.md — Criteria 4 and 5 integration specs, the INV-ACCESS-10 binding decision, the owed amendments

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 05-06-PLAN.md — The creation route `/characters/new` and the authoritative create flow

**Wave 5** *(blocked on Wave 4 completion)*

- [x] 05-07-PLAN.md — The sectioned roster `/characters` with its badge matrix and default control

**Wave 6** *(blocked on Wave 5 completion)*

- [x] 05-08-PLAN.md — E2E: the logged-out profile visit, the structured create, the roster journey

### Phase 6: Admin Portal Shell & Character Administration

**Goal**: Stand up `/admin` as an ABAC-gated trust boundary with character administration as its working section, six deferred sections registered / gated / refusing **after** the gate, and audit emission with before-values on every admin mutation.
**Depends on**: Phase 2 (`admin_section:` vocabulary + `seed:admin-section-access`), Phase 4 (shared facade helpers). The authorization gate is the **first thing built in this phase**, before any section, so every subsequent section inherits it.
**Requirements**: ADMIN-01, ADMIN-02, ADMIN-03, ADMIN-04, ADMIN-05, ADMIN-06, ADMIN-08, EXT-01, EXT-02, EXT-03, EXT-04
**Success Criteria** (what must be TRUE):

1. A non-admin calling an admin RPC **directly, bypassing the route entirely**, is denied — the decision is ABAC on an `admin_section:` resource, never a bare `PlayerHasRole` lookup and never a route-guard or gateway decision — with the denial asserted **over the wire** — the mapped `status.Code(err)` plus a generic `status.Convert(err).Message()` in which no internal code string appears — the specific typed `DENY_*` code asserted with `errutil.AssertErrorCode`, and a paired positive control proving an admin would have been permitted.
2. An admin lists, searches, opens, and edits characters; the edit surface accepts only an explicit **field-mask allowlist that excludes roles**, and admin disable/delete moves a character through the **same lifecycle states** as player-initiated retire — the irreversible `DeleteCharacter` path is reachable from no player-facing button.
3. Every admin mutation emits its audit envelope **in the same transaction** as the state change, carrying the acting **player** id, not only the character; lifecycle transitions additionally carry **before-values** (profile updates are new-values-only by D-103 erasure-safety, so they carry none). Projection of that envelope into `events_audit` is **out of scope for this phase** — the world outbox relay publishes through a bare `JetStreamPublisher` and the audit projector requires an `App-Rendering` header that only `RenderingPublisher` writes, so no relayed world envelope is projected today; tracked in #4971.
4. All six deferred sections (stats, players, moderation, audit, config, plugins) are registered, role-gated, and return `NOT_IMPLEMENTED` **after** the gate — a non-admin hitting one is *denied*, not told it is unimplemented — and a meta-test asserts **set equality** between the section registry and the authorization-descriptor set, so a section registered without a descriptor fails at compile time or at boot.
5. The `roles` hint exposed on `WebCheckSessionResponse` is **non-authoritative** — it is a drawing aid only, and a caller who acts on a role it names still meets a denial at the admin RPC. (The nav that consumes it is Phase 06.1's; this phase owes only the hint and the denial behind it.)

**Plans**: 4/4 plans executed
**UI hint**: no — the web surface moved to Phase 06.1 on 2026-08-13; this phase is proto, gate, RPC, SQL and audit only.
**Research flag**: `--research-phase` recommended — there is no in-repo precedent for the web gateway making an admin decision (`internal/web/` has zero `RoleAdmin` references); `AssertOperatorAdmin`'s shape must be transposed across a different auth model, and the reserved-section descriptor mechanism needs a concrete fail-at-boot design.

**Sketch findings** (the web-surface findings from sketches 001–006 and 010 moved to Phase 06.1 with the plans that consume them; only the two that constrain this phase's *server* contract remain here — read `.planning/sketches/MANIFEST.md` locked-decisions table before planning): **There is no delete in this portal** — §9.3 has no `AdminDeleteCharacter` and §4.4 forbids wiring purge to an admin button, so this phase MUST NOT ship an admin-reachable irreversible delete RPC. Status is a **transition picker that never sends a status value** (004/C), so the write RPCs take a transition verb, never a client-supplied target status.

Plans:
**Wave 1**

- [x] 06-01-PLAN.md — Tracer: the fail-closed admin section gate, end-to-end from proto to handler

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 06-02-PLAN.md — `AdminGetSection`, the six planned sections refusing after the gate, and the non-authoritative `roles` hint

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 06-04-PLAN.md — Admin character reads: `pg_trgm` migration 000057, the `players` join, both-direction ordering, three read RPCs

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 06-05-PLAN.md — Admin character writes and same-transaction audit emission with before-status

### Phase 06.1: Admin Portal Web Surface (INSERTED)

**Goal**: Ship the operator-facing web surface for the admin portal — the shadcn component set and the single root error boundary, the admin shell with its permission-filtered nav, the character table, and the edit Sheet with its mutation loop — against the gate and RPCs Phase 6 delivers.
**Depends on**: Phase 6 (the `holomush.adminportal.v1` wire contract, the fail-closed section gate, and the character read/write RPCs). Split out of Phase 6 on 2026-08-13 after three cross-AI convergence cycles showed the combined phase regressing at the seams between its backend and web halves; the four web plans moved here unchanged, carrying their review dispositions.
**Requirements**: ADMIN-03, ADMIN-07
**Success Criteria** (what must be TRUE):

1. Admin navigation is filtered from the **registry contract** returned by `AdminListSections`, not from template `{#if}` blocks; the roles on `WebCheckSessionResponse` change only what is drawn, and drawing a link the viewer may not use still ends in a denial at the RPC.
2. Exactly **one** `+error.svelte` exists under `web/src/routes/`, and `/admin`'s not-found is the *ordinary* one — a second boundary would kill per-viewer indistinguishability with nothing failing.
3. An admin lists, searches, sorts and opens characters in the table, and edits them through the Sheet; the Sheet's field set is the server's field-mask allowlist, and a denied or absent character renders the ordinary not-found.
4. The responsive treatment uses the **same viewport mechanism** as the shipped rail (`@media (max-width: 767px)`), so the rail's collapse and the admin shell's collapse fire at the same moment by construction rather than by coincidence.

**Plans**: 6/6 plans executed (4 executed; 2 gap-closure plans added 2026-08-15 for G-06.1-2 and G-06.1-3)

Plans:
**Wave 1**

- [x] 06.1-01-PLAN.md — Eleven shadcn components, the single root `+error.svelte` with its count meta-test, and the owed upstream amendments

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 06.1-02-PLAN.md — The admin shell, the server-projected permission-filtered nav, and the planned-section state

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 06.1-03-PLAN.md — The character table: click-header sort, one status filter, coarse `Last active`, four list states

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 06.1-04-PLAN.md — The edit Sheet, the D-110 mutation loop, the retire confirm, and the phone-band + indistinguishability E2E proofs

**Wave 5** *(gap closure — G-06.1-2)*

- [x] 06.1-05-PLAN.md — One phone-band breakpoint: the Sheet through the shared media-query hook, nine `@media` rules onto Tailwind's own tokens, a source census, and the 767/768 boundary sweep

**Wave 6** *(gap closure — G-06.1-3, blocked on Wave 5)*

- [x] 06.1-06-PLAN.md — The Go/TypeScript parity guard over the Sheet's thirteen paths, its two byte caps and its path-to-cap mapping, replacing the self-echoing assertion

> **Review provenance.** These four plans were reviewed as `06-03`, `06-06`, `06-07` and `06-08` across
> three cross-AI cycles; `06-REVIEWS.md` in Phase 6 records those cycles under the **old** names and is
> deliberately left unrewritten, because rewriting it would misattribute what reviewers actually said.
> Mapping: `06-03`→`06.1-01`, `06-06`→`06.1-02`, `06-07`→`06.1-03`, `06-08`→`06.1-04`.
> Outstanding at split time: 7 HIGH and ~30 actionable findings from cycle 3, dispositioned per plan.

## Progress

> **Reading this table.** Phase numbers **restart at 1 per milestone as of v0.13**; v0.11 and v0.12 used the
> retired continuous global numbering and are **not** renumbered. The `Phase` column is therefore only
> unique *within* a milestone — always read it together with the `Milestone` column (rows `1.`–`9.` under
> v0.11/v0.12 are archived phases; rows `1.`–`6.` under v0.13 are the active milestone).

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Channels Subsystem | v0.11 | 6/6 | In Progress|  |
| 2. Scenes Lineage Completion | v0.11 | 13/13 | In Progress|  |
| 3. Platform Hardening & Deployment Scaling | v0.11 | 6/6 | Complete    | 2026-08-10 |
| 4. World-Model Resilience Investigation & Decision (F1) | v0.12 | 9/9 | In Progress|  |
| 5. World-Model Integrity Fixes (M2/M12) | v0.12 | 7/8 | In Progress|  |
| 6. Operational Hardening & Assurance Gates | v0.12 | 4/4 | Complete    | 2026-08-14 |
| 7. Event-Model & Bootstrap Decomposition | v0.12 | 11/11 | Complete    | 2026-07-18 |
| 8. God-Object Decomposition | v0.12 | 9/9 | Complete   | 2026-07-19 |
| 9. Test-Quality & Code-Health Sweep | v0.12 | 21/21 [^p9] | Complete | 2026-07-27 |
| 1. Portal SPEC | v0.13 | 0/TBD | Not started | - |
| 2. ABAC & Schema Vocabulary | v0.13 | 0/TBD | Not started | - |
| 3. World Character Commands | v0.13 | 0/TBD | Not started | - |
| 4. Shared Facade Helpers & CharacterAccessService | v0.13 | 9/9 | Complete    | 2026-08-11 |
| 5. Character Identity UI & Public Profiles | v0.13 | 8/8 | Complete    | 2026-08-13 |
| 6. Admin Portal Shell & Character Administration | v0.13 | 0/TBD | Not started | - |

[^p9]: All 21 plans executed, but plan 09-21 produced no SUMMARY (it performed the phase's only
unconditional push and opened PR #4874). Hence "20/21" appears in some records and "21/21" in others —
neither is exactly right: *executed* and *documented* diverge by one. Its five must_haves were re-derived
live and are recorded in `milestones/v0.12-phases/09-.../09-VERIFICATION.md`.

## Deferred (Not in This Roadmap)

See `milestones/v0.11-REQUIREMENTS.md` "v2 Requirements" for full detail. Deferred strategic
clusters now live as first-class parking-lot entries in the `## Backlog`
section below (Forums → 999.4, Discord → 999.5, non-scene web-portal
surfaces → 999.1/999.8) — route each through `/gsd-spec-phase` before
roadmapping.

## Backlog

Strategic clusters consolidated from the beads → GitHub Issues migration
(2026-07-09). Member-level detail: [`.planning/archive/beads/TRIAGE.md`](archive/beads/TRIAGE.md).
Promote an entry with `/gsd-review-backlog` when ready.

The 2026-07-11 L7 architecture review (PR #4807) filed 23 discrete issues #4784–#4806
(epic E1 #4806) that overlap the foundation clusters below; per-cluster `**Related
issues:**` lines cross-link them. The issues track the discrete work; these clusters carry
the strategic frame. Reviewed 2026-07-28 (`/gsd-review-backlog`): 18 entries kept, **999.9 removed** (see below).
Re-reviewed 2026-07-31 (`/gsd-review-backlog`): **17 entries kept, 999.3 removed** (see below).

> **2026-07-31 review — scope reconciliation.** This pass verified cluster contents against the
> tree rather than re-reading goal prose, and three entries were wrong:
>
> - **999.3 is REMOVED.** Its only real content was scene templates (SCENEFWD-01, descoped
>   2026-07-08). Filed standalone as **#4897** and dropped from the backlog as someday/maybe —
>   a one-item cluster, the same disposition 999.9's last item got via #4886. `holomush-5rh`'s
>   other children were already separately filed (#4643, #4728) or shipped (`.19` → v0.11 Phase 2).
> - **999.2 was 8 items and is actually 1.** Seven were Epic 10's *pre-implementation* breakdown
>   and shipped in v0.11 Phase 1; only full-text search remains. See its entry.
> - **999.11's per-scope counts had drifted** and buried CRYPTO (102 pending, 40% of the total)
>   as a "long tail". Recounted in its entry.
>
> **Generalized lesson — beads-migrated item counts are an UPPER bound, not a residual.** Epics
> were migrated as whole clusters *including* their build-the-schema/build-the-commands children,
> so an epic that was subsequently implemented still lists them as pending. The count is exactly
> the number that inflates when sizing a milestone. Re-verify against the tree before promoting
> any 999.x cluster sourced from the beads migration.

> **v0.13 spine (selected 2026-07-31, PARTIALLY PROMOTED 2026-07-31):** **999.1 Web Client Portal
> completion** was the only player-facing cluster with substantial *verified* unbuilt scope (6
> concrete items: offline/PWA #4803, wiki/help, character profiles, character creation UI, admin
> portal, DM web surface). `/gsd-new-milestone` promoted **two and a half of the six** into milestone
> v0.13 (Phases 1–6): character creation/management UI (`qve.15`, non-roster), public character
> profiles (`qve.9`, no avatars), and the **shell only** of the admin portal (`qve.10`) — taken so
> character administration has a home. **Still in the backlog:** offline/PWA (`qve.7`), wiki
> (`qve.8`), web DMs (`qve.17`), and the six non-character admin sections (which overlap 999.8).
> See the 999.1 entry below for the residual.

> **v0.12 outcome (shipped 2026-07-28):** milestone **v0.12 Foundation Hardening** (Phases 4–9, archived at
> `milestones/v0.12-ROADMAP.md`) pulled the bulk of **999.9** (architecture decomposition → ARCH-01..05) and
> **999.10** (code health & test quality → QUAL-01..05), and addressed the arch-review operational Highs
> (→ OPS-01..05) plus the F1 event-model decision (→ MODEL-01..04).
>
> **999.9 is now REMOVED — exhausted.** v0.12 delivered five of its six named goals; the sixth
> (focus-redirect hot-path cache) was split out to #4886 rather than keeping a one-item cluster alive. That
> issue is explicit that the path is **unprofiled**, and that closing it unmeasured is an acceptable outcome
> — a cache on an unmeasured path trades invalidation surface for an unmeasured gain.
>
> **999.10 is kept and is now the larger residual of the two.** QUAL-02/03/05 shipped only partially
> (#4860, #4861, #4792), and the repo-wide coverage gate QUAL-01 aimed at **does not exist** — no codecov
> context is required on `main` (#4875, #4876). Detail in `milestones/v0.12-REQUIREMENTS.md`.
>
> **999.13's arch-review overlap also closed** (#4785/#4786/#4787/#4790, all shipped as OPS-01..04); its
> DR/backup/KMS core is untouched and the cluster stays.

### Phase 999.1: Web Client Portal completion (BACKLOG — PARTIALLY PROMOTED to v0.13)

**Goal:** Round out the web portal beyond scenes: offline support, wiki/help pages, character profiles + creation/management UI, admin portal, and a web surface for 1:1 direct messages.
**Source:** beads migration — 7 item(s) incl. epic(s) `holomush-qve`; member list in TRIAGE.md
**Related issues:** arch-review F6 PWA/offline #4803 (overlaps the offline-support + web-surface goals).
**Promoted to v0.13 (2026-07-31):** `qve.15` character creation + management UI (non-roster scope — roster integration stays deferred to 999.6), `qve.9` public character profiles (per-field privacy; avatars deferred to 999.16), and `qve.10` admin portal **shell + character administration only**.
**Residual (still backlog):** `qve.7` offline support / PWA (#4803), `qve.8` wiki portal, `qve.17` web surface for 1:1 direct messages, and the six non-character admin sections — stats, player management, moderation, audit viewer, config editor, plugin management — which overlap 999.8 and ship in v0.13 as *registered, role-gated, `NOT_IMPLEMENTED`-after-the-gate* stubs so wiring one later replaces a handler body rather than adding a check.
**Requirements:** v0.13 portion → IDENT-*/PROFILE-*/ADMIN-*/EXT-* (see `REQUIREMENTS.md`); residual TBD
**Plans:** 0 plans (residual)

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.2: Channels — remaining scope (BACKLOG)

**Goal:** Full-text search over channel message history. **This is the only member of the
original 8-item cluster that v0.11 Phase 1 did not deliver.**
**Scope reconciled 2026-07-31** (the entry's own "verify against what v0.11 Phase 1 delivered"
caution, finally acted on). The other 7 beads members were Epic 10's *pre-implementation*
breakdown and shipped in v0.11 Phase 1 (10/10 plans, 2026-07-09), verified against the tree:
`.3` schema → `migrations/000001_channels.up.sql`; `.4` join/leave/list/say →
`commands.go:63-85`; `.5` channel types → `types.go`; `.6` moderation mute/ban/ops →
`mute`/`ban`/`kick`/`transfer` subcommands + `moderate-own-channel` policy; `.7` history +
replay-on-join → `history` subcommand; `.2` implementation plan → consumed by v0.11 Phase 1.
Only `.8` (full-text search) remains: no `tsvector`, `GIN`, or `pg_trgm` exists in
`plugins/core-channels/migrations/` (earlier greps for "search" matched `search_path`, the
Postgres schema setting — a false positive worth not repeating).
**Sizing caution:** beads epics were migrated as whole clusters *including* their
pre-implementation task breakdowns, so an implemented epic still lists "build the schema"
children. Item counts on any 999.x cluster sourced from beads migration are an UPPER bound,
not a residual — re-verify against the tree before sizing a milestone off them.
**Source:** beads migration — 8 item(s) incl. epic(s) `holomush-0sc`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.4: Forums (BACKLOG)

**Goal:** Forum boards/threads/posts with web UI, moderation, notifications, and in-game integration. No design exists yet — needs brainstorm + spec before planning (theme:social-spaces).
**Source:** beads migration — 9 item(s) incl. epic(s) `holomush-djj`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.5: Discord Integration (BACKLOG)

**Goal:** Discord bridge plugin: bot, channel bridging, OAuth account linking, notifications, presence sync. Depends on channels substrate + an unbuilt OAuth substrate (theme:social-spaces).
**Source:** beads migration — 8 item(s) incl. epic(s) `holomush-aqq`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.6: Character Rostering & Transfer (BACKLOG)

**Goal:** Roster characters and transfer them between players (epic holomush-gloh).
**Source:** beads migration — 1 item(s) incl. epic(s) `holomush-gloh`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.7: Inventory & Object Manipulation (BACKLOG)

**Goal:** Inventory and object-interaction model; design task first (epic holomush-ni99).
**Source:** beads migration — 2 item(s) incl. epic(s) `holomush-ni99`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.8: Admin Web UI & Config (BACKLOG — substrate consumed by v0.13)

**Goal:** Operator tools: /admin route, server stats, player management, config surface (epics holomush-g4pb + holomush-7nub; overlaps the web-portal admin page — consolidate at design time).
**Source:** beads migration — 3 item(s) incl. epic(s) `holomush-g4pb`; member list in TRIAGE.md
**Consumed by v0.13 (2026-07-31):** the `/admin` route, its ABAC trust boundary (`admin_section:` resource + `seed:admin-section-access`, which covers every future section at zero policy cost), the section registry, and the shared authorization helper. The "consolidate at design time" note this entry carried is now discharged — v0.13's Phase 1 SPEC is that design point.
**Residual (still backlog):** the six section *bodies* — server stats, player management, moderation, audit log viewer, config editor, plugin management. v0.13 ships each **registered, role-gated, returning `NOT_IMPLEMENTED` after the gate**, so implementing one later replaces a handler body rather than standing up a surface. Audit *emission* (with before-values) also lands in v0.13, so the deferred audit viewer inherits real history rather than launching empty.
**Requirements:** TBD (residual)
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.10: Code health & test-quality program (BACKLOG)

**Goal:** Codebase humanization/de-slop, ACE naming violations, weak/skeleton tests, security polish batch, coverage backfill on Phase-1.5 infra packages, session-lifecycle test matrix.
**Source:** beads migration — 8 item(s) incl. epic(s) `holomush-ec22`, `holomush-89o9`; member list in TRIAGE.md
**Related issues:** arch-review F7 coverage #4804 (overlaps the coverage-backfill goal).
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.11: Invariant registry backfill program (BACKLOG)

**Goal:** Bind pending INV-* registry entries per scope, migrate INV-DOCS/INV-BRANDING scopes, reclassify entries that fail the invariant bar (epic holomush-hz0v4).
**Scope (recounted 2026-07-31 from `docs/architecture/invariants.yaml` — 243 pending, 103 bound, 346 total):**
CRYPTO 102 · SCENE 60 · PLUGIN 29 · EVENTBUS 19 · ACCESS 8 · STORE 8 · TELEMETRY 8 ·
COMMAND 3 · SESSION 3 · CLUSTER 1 · COMM 1 · PRESENCE 1. CHANNEL/PRIVACY/WORLD are fully bound.
(Sum checks: 102+60+29+19+8+8+8+3+3+1+1+1 = 243.) INV-BRANDING and INV-DOCS are *declared*
scopes carrying `status: pending` with ZERO numbered entries — they contribute nothing to the
count above; migrating them is separate work from binding entries, hence both goals.
**CRYPTO is 42% of the remaining work** and is the correct entry point, not a long-tail
afterthought — an earlier version of this entry led with SCENE and buried crypto, and also
carried stale PLUGIN/EVENTBUS counts (39/28) that had since been partially backfilled.
Recount before scoping; these numbers move under normal test work. **Anchor the pattern**
(`^\s+binding:\s*pending`) — an unanchored count also matches 11 YAML comments that discuss
pending bindings in prose, which is how this entry first shipped "254".
**Source:** beads migration — 11 item(s) incl. epic(s) `holomush-hz0v4`, `holomush-s6wp`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.12: Observability & vendor-neutral telemetry (BACKLOG)

**Goal:** Vendor-neutral error/telemetry/metrics abstraction at every seam (epic holomush-ionvr), error-event seam design, signal-hygiene so benign conditions stop masquerading as ERROR/WARN.
**Source:** beads migration — 3 item(s) incl. epic(s) `holomush-ionvr`, `holomush-yxfbi`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.13: Ops & deployment resilience (BACKLOG)

**Goal:** Disaster recovery + backup/restore guides, background DB sync to object storage, gateway-survival deploy strategy, Tailscale admin access, remote KMS substrate (VaultTransitProvider + rotation CLIs).
**Source:** beads migration — 6 item(s) incl. epic(s) `holomush-aub5`; member list in TRIAGE.md
**Related issues:** none open. The arch-review overlap (F2 gateway OOM #4785, F3 DLQ #4787, F4 events_audit unbounded #4786, F8 nats CVE #4790) all shipped in v0.12 as OPS-01..04 and is closed — this cluster's remaining scope is the DR/backup/object-storage/Tailscale/KMS core, which is untouched.
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.14: Platform & security design seeds (BACKLOG)

**Goal:** Design-needed platform work: load/perf harness + SLOs, feature-flag system, audit-backfill CLI, audit drift detector, KEK fail-closed decision, plugin scene-metadata privacy decision, comm event-type extensibility, plugin hostfunc authorization, ABAC fair-share timeout + debug endpoint.
**Source:** beads migration — 9 item(s); member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.15: Documentation program (BACKLOG)

**Goal:** Comprehensive features/usage/admin/operator/player docs under site/docs, consolidated system-design documentation, session-lifecycle diagram, unified in-game + website help system.
**Source:** beads migration — 4 item(s) incl. epic(s) `holomush-k7qy`, `holomush-rm9g`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.16: Feature wishlist (BACKLOG)

**Goal:** Player/operator-facing capabilities awaiting prioritization: rich text (markdown + emoji), operator-defined color themes, interface-backed content/blob storage, plugin-authoring Claude Code skill.
**Source:** beads migration — 4 item(s); member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.17: iOS Client (stretch) (BACKLOG)

**Goal:** Native iOS client (Epic 13) — stretch goal; depends on stable web/API surface.
**Source:** beads migration — 1 item(s) incl. epic(s) `holomush-5g6`; member list in TRIAGE.md
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.18: Release process coherence (BACKLOG)

**Goal:** Review release procedures end-to-end and make them coherent: consider restoring
release-please (or keeping cog — evaluate, don't assume), align the release flow with GSD
practices/idioms (milestone close ↔ release cut, labels tracking cog-computed semver per
PROJECT.md Key Decisions), and produce better release notes than the current
GoReleaser-generated ones. Not necessarily all one tool, but something coherent.
**Source:** captured 2026-07-11 at v0.11 milestone close (milestone-relabel session — the
v1.0/v0.11 label drift and the GSD-tagging/cog collision motivated this review)
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.19: Restore lefthook + speed up the inner loop (BACKLOG)

**Goal:** Now that the repo is back on native git only (jj retired), restore lefthook git
hooks (worktree creation currently warns "no lefthook config found") and look for further
inner-loop speedups. Investigate: reinstate a lefthook config so `task workspace:new`
worktrees auto-install hooks (pre-commit fmt/lint, commit-msg conventional-commit check to
match CI's PR-title gate), and profile the `task pr-prep` fast lane / `task lint` / `task
test` cycle for wins (caching, scoping, parallelism). Aim: tighter edit→check feedback.
**Source:** captured 2026-07-11 at v0.11 milestone close (multiple worktree sessions this
day emitted "No lefthook config" warnings on every commit)
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)

### Phase 999.20: Character Rename & the Approval Dimension (BACKLOG)

**Goal:** Give the character identity model an **approval dimension**, then land
`world.Service.RenameCharacter` on top of it — renaming permitted only while a character
is not yet approved for play, with the core name immutable afterwards.
**Source:** removed from v0.13 Phase 3 on 2026-08-06 during `/gsd-discuss-phase 3`; full
rationale and evidence in `.planning/phases/03-world-character-commands/03-CONTEXT.md`
(D-44, plus the deferred-ideas section).
**Related:** Phase 999.6 (Character Rostering & Transfer) — rostering is defined in
REQUIREMENTS v2 as "a distinct transition *out of* retired, which is why retire must not
release the name", so the approval axis and the rostering transition must be designed
together, not separately.
**Requirements:** IDENT-03 (moved out of v0.13); the rename half of IDENT-10.
**Plans:** 0 plans

Design inputs this item MUST carry:

- **The approval dimension does not exist.** `characters.status` is `active|retired|idle`
  only (`000054_character_identity_and_lifecycle.sql`). "Rename only before approval" has
  no state to read; the honest substitute — "has this character ever emitted a comm
  payload" — means querying the audit log to authorize a write.

- **Two naming frictions.** `rostered` collides with 999.6's reserved meaning, and
  `retired` is a lifecycle value 01-SPEC §4.4 deliberately keeps distinct from `purge` —
  do not merge the approval axis into the lifecycle axis. Separately, a `session_status`
  column on `characters` would **duplicate** an existing one: session status already lives
  on the session row (`internal/session/session.go:21`).

- **Blast radius.** Bound `INV-WORLD-5` (closed vocabulary + exhaustive-switch), 01-SPEC
  §4.4, PORTAL-04, character creation's initial state, and an approving actor (an admin
  surface, so it interacts with Phase 6).

- **The substrate is already built.** `CharacterRepository.Rename`
  (`internal/world/postgres/character_repo.go:212`) is version-guarded, runs `guardSkeleton`
  with self-exclusion, and writes its outbox envelope inside its own transaction. Its doc
  comment carries the rule: it MUST NOT be routed through `worldMutator.mutate()` (two
  envelopes for one rename). No `character_renamed` taxonomy kind exists yet — the shipped
  operator CLI emits rename as `character_updated`; reconcile rather than duplicate.

- **`INV-WORLD-6` must be resolved here.** Its text claims a name becomes claimable again
  only via a tombstone-emitting hard delete over exactly two paths, but rename frees the
  old `normalized_name` — and the shipped operator CLI already does so in production. Its
  binding test never exercises rename. Options recorded in 03-CONTEXT.md: narrow it to
  lifecycle transitions, widen the enumeration, or add a former-names reservation table
  (which closes identity inheritance while still permitting rename).

- **Read `01-SPEC.md` §5 first** — the name-capture surface inventory, six frozen sites vs
  five live ones, is what makes any rename argument checkable.

- **UI consequences.** Sketch 004's `Rename…` affordance and sketch 009's "names are
  reserved, not permanent" copy both assume rename ships; both are false for v0.13 and
  become true again only with this item.

Plans:

- [ ] TBD (promote with /gsd-review-backlog when ready)
