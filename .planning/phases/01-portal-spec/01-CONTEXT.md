# Phase 1: Portal SPEC - Context

**Gathered:** 2026-08-01
**Status:** Ready for planning

<domain>
## Phase Boundary

Produce the committed portal SPEC — one document — that fixes the audience/message shape, the
character data and lifecycle model, the profile visibility model, the media-ready profile schema,
and the full new RPC surface. This discharges the precondition PROJECT.md's Out-of-Scope entry
for non-scene portal surfaces demanded, rather than waiving it.

**The deliverable is a document, not code.** Eight of the fourteen catalogued pitfalls are shape
decisions whose cost explodes once Phases 2–6 write code against them. Phase 1 writes them down.

**In scope:** PORTAL-01..10. The SPEC's normative content, its amendments to already-written
roadmap criteria, and a pointer update to the repo's spec-location convention.

**Out of scope:** any schema migration, any proto change, any policy code, any UI. Those are
Phases 2–6. Also out of scope: the full `docs/superpowers/` retirement sweep (see Deferred).
</domain>

<decisions>
## Implementation Decisions

### Audience matrix & message shape (PORTAL-01, PORTAL-02)

- **D-01:** The SPEC mandates **separate proto messages per audience** — `PublicCharacter` /
  `OwnCharacter` / `AdminCharacter` — with **one projection function per (row → audience)**.
  Absence becomes a type-system property: a public projection cannot physically carry an
  owner-only field. This is PITFALLS #2's own recommendation, chosen over the "one message with
  `optional` scalars cleared server-side" alternative because that alternative's guarantee is a
  per-field discipline nothing in the build enforces — a scalar someone forgets to mark `optional`
  marshals `""`, which is *present*, and the absence guarantee fails silently for exactly that
  field. — **Reversibility:** costly — undo touches every character-returning RPC, its projection,
  and every web-client call site once Phases 4–6 build on it.

- **D-02:** **Breaking changes are acceptable.** The maintainer confirmed there are no other users
  of the system. Nothing needs grandfathering, no compatibility shim, no deprecation window.
  Existing message shapes may be replaced outright rather than extended.

- **D-03:** `WebListAllCharacters` **splits into two RPCs**: a public/player-audience list
  returning a `PublicCharacterSummary` carrying identity fields only (`has_active_session`,
  `session_status`, `last_location`, `last_played_at` all dropped), and a separate admin list
  carrying the rich row. Today's single RPC returns presence telemetry for every character —
  the leak PITFALLS #2 names, currently held back by nothing but two hand-written field lists.

- **D-04:** The binding enforcement mechanism is the **census with set equality** over every
  character-returning RPC, and **only** that. A struct-literal lint banning character-shaped proto
  literals outside the projection package was considered and **not** mandated — PORTAL-10 rule 1
  already requires the census, so it costs nothing extra, and it catches the one case a
  per-endpoint suite structurally cannot: a member missing from its own set.

### Character lifecycle (PORTAL-04)

- **D-05:** Lifecycle is a **single `status TEXT NOT NULL DEFAULT 'active'` column with a `CHECK`
  constraint** on the `characters` table, matching the repo's four existing CHECK-enum precedents
  (`policies.effect`, `policies.source`, `entity_properties.visibility`). **`purge` is not a
  state** — it is `DeleteCharacter`, and the row is gone. Rejected: paired `retired_at` timestamp
  (two columns that can disagree) and timestamp-only derivation (no database backstop; every
  reader must derive identically). — **Reversibility:** one-way — changing the shape after Phase 2
  ships the migration requires a new migration plus a one-shot data job.

- **D-06:** The CHECK vocabulary ships **all three values from day one** —
  `('active', 'retired', 'idle')` — even though nothing in v0.13 can transition a character into
  `idle`. This is paired with a **normative SPEC rule that every lifecycle read is an exhaustive
  `switch` with `default: deny`**, and a test that **constructs a character directly in `idle`**
  and asserts it is excluded. Rationale: an unreachable enum value plus a non-exhaustive check
  (`if status == "retired" { deny }`) is structurally identical to research CONFLICT 4's rejected
  `members` privacy tier — it **fails open** the day the value becomes reachable. Shipping the
  value with an exhaustive-switch rule and a direct-construction test makes it safe *and*
  non-vacuously tested, the same move EXT-03/EXT-04 make for admin sections. — **Reversibility:**
  one-way — adding or removing a CHECK value later is an `ALTER ... DROP/ADD CONSTRAINT` migration.

- **D-07:** A **retired character is roster-visible** (with an un-retire affordance),
  **unselectable for play**, and **its public profile still resolves and says retired**. Rationale:
  the name stays reserved for rostering (999.6), and scene archives publish the character's name
  publicly via `WebGetPublicSceneArchive` regardless — a 404 profile beside a live archive is an
  inconsistency, not privacy. Retire means "left active play", not "hidden".

### Profile visibility policy (PORTAL-05, PROFILE-03/04/05/10a/11)

> This area expanded materially during discussion. The maintainer replaced the per-owner privacy
> model with a game-operator-controlled one. Read D-08 through D-13 as a set.

- **D-08:** `characters.description` (the in-world "look at" text) is **always public on the
  profile, with no per-owner control**. Rationale: `seed:player-character-colocation` gates *where
  you have to be standing*, not *who may know* — treating it as a privacy boundary would retrofit a
  meaning it never carried. The web removes the co-location requirement, not a privacy control.
  Consequence: PROFILE-11's audit of existing character descriptions is now the **only** gate on
  this text, not a formality.

- **D-09:** **There is no player or character agency over web profile visibility at all.** The
  game configuration is the sole visibility authority. There is no owner-facing per-field
  public/private toggle in v0.13. A player who wants something unpublished does not write it into
  the profile. — **Reversibility:** costly — reintroducing owner control means adding a control
  surface across the facade, the projections, and the UI, and it changes who PROFILE-03's
  "server-enforced visibility" answers to.

- **D-10:** Visibility is a **game-wide, per-attribute viewer-tier floor**. The tier ladder is
  **anonymous < guest < authenticated player**. One configuration governs the whole game, keyed by
  attribute name; every character is governed identically. Rejected: per-character admin overrides
  (a second lookup on every projection, and an override outliving its reason becomes invisible
  policy) and per-character-only configuration (an unset floor is the zero-value-means-allow shape
  EXT-03 forbids elsewhere in this milestone).

- **D-11:** The floor lives as an **ABAC policy family extending `seed:property-*`**
  (`internal/access/policy/seed.go:111-141`), overridden by `policies` rows with `source='admin'` —
  a value the schema already models (`000001_baseline.up.sql:261`). The decision stays inside the
  default-deny engine, consistent with the locked architectural decision and with ADMIN-01's "never
  a bare lookup". **Zero new storage.** Rejected: a settings-store config consumed by the
  projection, because it moves an authorization decision out of the ABAC engine into projection
  code — the precise pattern ADMIN-01 and PITFALLS #4 exist to prevent.

- **D-12:** The floor is **evaluated at read time against the attribute name**. `profile.*` rows
  carry a uniform `visibility` value; the tier-floor policy family evaluates
  `(attribute name, viewer tier)` on read. A configuration change therefore takes effect on the
  **next read with no data migration** — which is what makes "entirely configurable" actually cheap.
  Rejected: stamping `entity_properties.visibility` per row from the config, because every
  configuration change would then require a one-shot backfill across every profile row of every
  character (migrations forbid in-migration backfills). **Phase 2 must confirm `PropertyProvider`'s
  exact attribute shape** (`internal/access/policy/attribute/property.go:61-147`) — the property
  name must be available as an ABAC resource attribute. — **Reversibility:** costly — switching to
  stamped rows later requires the backfill job this decision exists to avoid.

- **D-13:** **PROFILE-05 is a hard invariant, not a configurable attribute.** If a viewer can reach
  the profile at all, they see name and pronouns. The configuration may set the profile's own
  reachability floor as high as it likes, but it **cannot raise name or pronouns above that floor**.
  Keeps every reachable profile non-empty and guarantees the initial-letter avatar placeholder
  always has a letter.

- **D-14:** The **seeded default posture** is *identity anonymous, prose guest-floor*: `name`,
  `pronouns` and the in-world `description` readable by anyone; `rumors`/RP-hooks, the "Currently"
  line, the OOC RP-preferences block, and time zone require at least a guest session.

  **Recorded divergence, deliberately chosen:** the maintainer's stated principle is *"anything
  readable in the same location on grid is visible to other logged-in users on the web"* — strict
  grid-parity puts `description` at a **player** floor. The shipped default places it at
  **anonymous**, which is more open. This was surfaced and confirmed: grid-parity is the floor the
  principle guarantees, **not a ceiling on what a game may publish**, and an open default is what
  makes a shareable profile URL worth having. **The SPEC MUST state this divergence explicitly** so
  it reads as a choice rather than an oversight. A game wanting strict grid-parity raises
  `description` to `player` in configuration.

- **D-15:** v0.13 ships the **model plus seeded defaults only — no editing surface**. Phase 1
  specifies the model and the defaults; Phase 2 lands the tier-floor policies alongside the ABAC
  vocabulary it already owns. The editing UI arrives when the **`config` admin section** — already
  registered, role-gated and returning `NOT_IMPLEMENTED` after the gate per EXT-01/EXT-02 — gets
  its handler body. This gives that deferred section its first concrete tenant. Rejected: a minimal
  editor in Phase 6, which the roadmap already flags as the highest-risk phase (net-new trust
  boundary, zero `RoleAdmin` references in `internal/web/`, carries a `--research-phase` flag).

### SPEC artifact & binding (PORTAL-10)

- **D-16:** **One SPEC document**, at the **GSD convention location**:
  `.planning/phases/01-portal-spec/01-SPEC.md`. Rejected: a portal/admin-IA split and a
  master-plus-sub-spec structure — the latter is the source of most entries in
  `.claude/rules/references/design-review-learnings.md` (master-vs-sibling amendment drift, where a
  later spec rolls back a contract a sibling just landed and the amendments table misses it). One
  document has no sibling to drift from.

- **D-17:** PORTAL-10's six verification-integrity rules bind via a **normative
  "Verification Integrity" section in the SPEC, copied verbatim as an acceptance-criteria block
  into every v0.13 `PLAN.md`**, with `gsd-plan-checker` verifying the block is present and
  specialized to that phase. Rejected: a `test/meta/` check over the plan markdown (asserting on
  planning documents is unusual here, and PORTAL-10 rule 4 wants gates demonstrated RED — this one
  is hard to see fail meaningfully) and prose-only (v0.12 catalogued 17 verifications that could
  not fail *with these same review gates already in place*).

- **D-18:** v0.13 allocates invariants into **`ACCESS`, `PRIVACY` and `WORLD`** — three existing
  scopes, no new boundary. Authorization and the tier-floor policy → `ACCESS`; per-field and
  profile-reachability guarantees → `PRIVACY`; the character lifecycle state machine → `WORLD`
  (declared boundary: world-model correctness guarantees born from the MODEL-01 ADR; it already
  owns version-guard and outbox invariants). Rejected: `ACCESS`+`PRIVACY` only (filing "a retired
  character cannot be selected for play" under ACCESS makes that scope's boundary statement false)
  and a new `PORTAL` scope (rule 6 exists precisely because pre-2026-05 specs each minted their own
  family and a migration had to dig them all out). — **Reversibility:** one-way — canonical ids are
  referenced from tests and other specs; renumbering is a migration, not an edit.

  **Note the escape hatch:** `test/meta/invariant_registry_test.go:341` walks **only**
  `docs/superpowers/specs/`. A SPEC living in `.planning/` is invisible to the orphan check either
  way, so v0.13's invariant entries **MUST be hand-registered** in
  `docs/architecture/invariants.yaml`. This is the documented escape hatch in
  `.claude/rules/invariants.md`, not a new gap.

- **D-19:** Phase 1 updates the **spec-location pointer only**: `CLAUDE.md`'s Spec-Driven
  Development section (plus the rules files that name spec paths) to state that GSD milestone specs
  live at `.planning/phases/<phase>/<NN>-SPEC.md`, with `docs/superpowers/` named as historical.
  The full retirement sweep is deferred (see Deferred Ideas).

### SPEC amendments required

The SPEC MUST carry an amendments section recording these, so a downstream planner does not plan to
a criterion that no longer holds:

| Artifact | Amendment |
|---|---|
| `ROADMAP.md` Phase 4, success criterion 3 | Written as *"An owner can set any profile field to `public` or `private`…"* — no owner-facing control exists under D-09. Restate as the game-configured tier floor, retaining the exhaustive-`switch`-with-`default: deny` clause. |
| `ROADMAP.md` Phase 5, success criterion 4 | Written as *"An owner flips a field between public and private…"* — the toggle does not exist. Restate around a configuration change taking effect on next read. |
| `REQUIREMENTS.md` PROFILE-12 | The retirement half stands. The "visibility toggle" half has no toggle to attach its not-retroactive statement to — re-seat that statement where a player authors profile fields. |
| `.planning/research/SUMMARY.md` CONFLICT 4 | *"ship `public` and `private` in the v0.13 UI"* no longer applies to an owner-facing UI. The tier vocabulary decision survives; its UI expression does not. |

PROFILE-03, PROFILE-04 and PROFILE-05 survive and need **no** amendment — PROFILE-03 says
"server-enforced visibility… with sane defaults… enforcement by omission" and never names *who*
sets it; PROFILE-04's reachability facet is re-seated as the profile-level tier floor; PROFILE-05
is re-seated as a constraint on the configuration (D-13).

### Claude's Discretion

Taken without further consultation:

- **PORTAL-09 verdict: no.** No v0.13 surface sorts or filters on a profile field. Reinforced by
  D-12 — read-time tier evaluation makes sorting or filtering on privacy-bearing property rows
  incoherent as well as expensive. The deferred searchable directory's indexing need stays additive
  and non-blocking.
- **`expected_version` carriage:** a scalar field on each mutation request message, matching
  migration `000049`'s existing shape — not a shared embedded precondition message.
- The read-surface inventory (PORTAL-02) and name-capture inventory (PORTAL-03) are **enumeration
  work**, not forks — the SPEC lists them exhaustively with an audience / historical-vs-live verdict
  per entry.
- The seven admin section ids are as already named: `characters` (available) + `stats`, `players`,
  `moderation`, `audit`, `config`, `plugins` (planned).
- SPEC filename and internal section ordering.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Milestone source material (read first)
- `.planning/research/SUMMARY.md` — the synthesis this SPEC is written from; §"Adjudicated
  conflicts" CONFLICT 1 (property rows beat JSONB, decisively) and CONFLICT 4 (tier count) are
  settled inputs, not open questions
- `.planning/research/PITFALLS.md` — the fourteen catalogued pitfalls; #1 (return-all-and-hide),
  #2 (list/search/export leak), #3 (privacy is not retroactive), #4 (route-guard-only admin auth),
  #5 (reserved capacity that rots)
- `.planning/research/ARCHITECTURE.md` — the in-tree citations that won CONFLICT 1
- `.planning/ROADMAP.md` §"Phase Details" — phase boundary and success criteria (note the two
  amendments above)
- `.planning/REQUIREMENTS.md` — PORTAL-01..10 verbatim, plus the IDENT/PROFILE/ADMIN/EXT
  requirements later phases inherit

### Privacy & access control
- `internal/access/policy/seed.go:111-141` — the existing `seed:property-*` policy family the tier
  floor extends
- `internal/access/policy/attribute/property.go:61-147` — `PropertyProvider`; **Phase 2 must
  confirm the property name is available as an ABAC resource attribute** (D-12)
- `internal/world/service.go:1144-1171` — the fail-closed per-property filter loop
- `.claude/rules/abac-providers.md` — omit-don't-sentinel; the same rule D-01's message shape
  expresses at the wire
- `internal/store/migrations/000001_baseline.up.sql:350-373` — `entity_properties` per-row
  `visibility` / `visible_to` / `excluded_from` + `UNIQUE(parent_type,parent_id,name)`
- `internal/store/migrations/000001_baseline.up.sql:259-261` — `policies.effect` and
  `policies.source` CHECK vocabularies (`source='admin'` is D-11's override mechanism)

### World model & lifecycle
- `internal/store/migrations/000001_baseline.up.sql:67-76` — the `characters` table as it stands
  (no status column, no unique name index)
- `internal/world/service.go:745-777` — `DeleteCharacter`; cascades `entity_properties`, emits a
  tombstone, **not reversible**, never wired to a player-facing button
- `internal/world/service.go:799-836` — `UpdateCharacterDescription`, already ABAC-gated
- `internal/world/validation.go:114-126` — `NormalizeCharacterName` (no NFKC, no `Cf` strip, no
  confusable folding)
- `docs/adr/holomush-i4784-world-state-model-decision.md` — MODEL-01; the `WORLD` invariant scope's
  declared boundary

### Proto surface
- `api/proto/holomush/web/v1/web.proto:496` — `CharacterSummary`, the message D-03 splits
- `api/proto/holomush/core/v1/core.proto:80-107` — the core-side character RPCs
- `.claude/rules/gateway-boundary.md` §"Structural writes use typed RPCs" — why the new surface is
  typed RPCs, not the command path
- `.claude/rules/proto-doc-comments.md` — every new proto element needs a Go-grounded comment

### Verification integrity & invariants
- `docs/architecture/invariants.yaml` — the registry; `ACCESS`, `PRIVACY`, `WORLD` boundary
  statements
- `.claude/rules/invariants.md` — define/respect/bind/regenerate workflow, and the
  `docs/superpowers/specs/`-only orphan-check limitation (D-18)
- `test/meta/invariant_registry_test.go:341` — the orphan check's hard-coded walk root
- `.claude/rules/testing.md` — ACE naming, tiers, `// Verifies:` bindings
- `.claude/rules/references/design-review-learnings.md` — master-vs-sibling spec drift, the failure
  D-16 avoids
- `.claude/rules/grpc-errors.md` — top-level `oops.AsOops(err).Code()` vs. chain-walking
  `errutil.AssertErrorCode` (PORTAL-10 rule 5)

### Admin surface
- `internal/admin/auth/ingame.go:115` — `AssertOperatorAdmin`'s shape, and the documented
  "RoleAdmin (any character)" semantics
- `internal/store/role_store.go:83-93` — `PlayerHasRole` is **player-wide**; issue #4899
- `web/src/lib/nav/sections.ts:41-47` — the existing `as const satisfies` section-registry pattern
  to mirror for admin IA; do not add a library

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets

- **`entity_properties` per-row privacy is already shipped end-to-end** — the column trio, the
  `PropertyProvider`, six seed policies, and a fail-closed filter loop. The profile/media model is
  **reuse, not invention**: profile fields and media refs are rows, so "1 primary + 10 gallery
  without a later migration" is literally an `INSERT` and `UNIQUE(parent_type,parent_id,name)`
  enforces exactly-one-primary for free.
- **`policies.source='admin'` already exists** in the CHECK vocabulary — D-11's "entirely
  configurable" needs no new storage and no new concept.
- **`world.Service.UpdateCharacterDescription` and `DeleteCharacter` already exist and are already
  ABAC-gated.** Only **rename** and **soft-retire** are genuinely absent at the domain layer.
- **v0.12's two-replica resilience harness** should be pointed at the new mutation RPCs rather than
  writing fresh concurrency tests.
- **`web/src/lib/nav/sections.ts`** already implements the derived-union + visibility-gate registry
  pattern the admin IA needs.

### Established Patterns

- **Enum-by-CHECK** is the repo's convention for state vocabularies — four precedents. D-05 follows it.
- **Seed policy families** (`seed:property-public-read` / `-private-read` / `-admin-read` /
  `-restricted-*`) are the precedent D-11 extends; a tier floor is another family member, not a new
  mechanism.
- **Default-deny ABAC with `(false, err)` on infra error** — the tier floor must not become the one
  place a lookup failure reads as permissive.
- **Migrations forbid in-migration backfills** — this is the constraint that makes D-12's read-time
  evaluation strictly better than stamping rows.

### Integration Points

- **Where the tier floor meets the read path:** the projection functions of D-01 consume the
  viewer-filtered property slice; PROFILE-10 forbids the facade calling
  `PropertyReader.ListByParent` / `PropertyRepository.ListByParent` directly.
- **Where the lifecycle meets authorization:** character selection, profile reachability, and admin
  disable all read `status`; D-06's exhaustive-switch rule applies at each.
- **Where the audience matrix meets the census:** every character-returning RPC — including the
  three existing public export surfaces and `WebGetPublicSceneArchive`'s denormalized names — is a
  census member.
- **Gateway boundary holds throughout:** `internal/web/` stays protocol translation; every
  visibility decision is core-side.

</code_context>

<specifics>
## Specific Ideas

- *"We're still at a good point that breaking changes are ok, no other current users of the system
  other than me."* — the constraint that makes D-01 and D-03 cheap.
- *"Anything that is readable in the same location 'on grid' is visible to other logged in users on
  the web."* — the grid-parity principle. Recorded as a **floor the principle guarantees, not a
  ceiling on what a game may publish** (see D-14's recorded divergence).
- *"I think that from a web profile standpoint we do not allow player/character agency at all."* —
  D-09, stated more strongly than the option offered.
- *"Some games may want nearly everything about a character visible and scrapable to anonymous
  users, others may want require guests and allow them to see most things, and still others may
  want to require actual players as the floor."* — the three postures the configuration must
  express; the SPEC should show all three as worked examples of the same table.
- *"Let's please follow the GSD convention for SPEC files, superpowers is gone."* — D-16 and D-19.

</specifics>

<deferred>
## Deferred Ideas

- **Admin editing UI for the visibility-floor configuration** — lands when the `config` admin
  section gets its handler body. EXT-01/EXT-02 already register it as gated-and-`NOT_IMPLEMENTED`,
  so this is a body replacement, not new wiring. (D-15)
- **Full `docs/superpowers/` retirement sweep** — ~20 files, several of them live gates:
  `test/meta/invariant_registry_test.go:341` (orphan-check walk root), `scripts/docs-paths-regex.sh`
  and `scripts/lint-docs-paths-sync.sh` (pr-prep docs fast-lane classification),
  `scripts/adr-doctor.sh`, `scripts/check-docs-quality.sh`, `gorules/plugin.go`,
  `internal/store/spec_meta_test.go`, `Taskfile.yaml`, plus doc-comment references in
  `api/proto/holomush/core/v1/core.proto` and `pkg/plugin/*.go`, and relocation of the 140 existing
  spec files. Its own issue — it touches the gates that decide whether a PR takes the docs fast
  lane, which is exactly where a silent fail-open would live. Phase 1 does the pointer update only.
  (D-19)
- **Struct-literal lint** banning character-shaped proto literals outside the projection package —
  considered and consciously not mandated (D-04). If the census proves insufficient in practice,
  this is the next increment.
- **A fourth viewer-tier rung** and the representation of the visibility configuration for the
  future editor — raised as possible discussion topics, not pursued. The tier ladder is a string
  enum, so a rung is an append.

</deferred>

---

*Phase: 1-Portal SPEC*
*Context gathered: 2026-08-01*
