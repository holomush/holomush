# Phase 4: Shared Facade Helpers & `CharacterAccessService` - Context

**Gathered:** 2026-08-10
**Status:** Ready for planning

> **Decision numbering continues the shared v0.13 sequence.** `D-NN` is ONE
> sequence across every `.planning/phases/*/*-CONTEXT.md`, not per-phase. The
> highest claimed before this file was **D-68** (02.2-CONTEXT). This phase claims
> **D-69 … D-83**. A bare `D-NN` below means THIS file's decision; a
> foreign-phase decision is cited with its phase named.

<domain>
## Phase Boundary

Two pieces of work, in order:

1. **Extract** `resolveAndGate` (guest gate, INV-SCENE-64) and `ownedCharacter`
   (server-side ownership resolution, INV-SCENE-63) out of `SceneAccessServer`
   (`internal/grpc/sceneaccess_service.go:106-150`) into one shared place, so a
   second facade cannot re-implement or skip them.
2. **Build** `holomush.characteraccess.v1.CharacterAccessService` and its
   `Web*` proxies on `holomush.web.v1.WebService`, so character read and write
   reach the web with unauthorized fields **absent from the marshaled response
   bytes** by construction.

Requirements in scope: **IDENT-02** (prose profile edit with server-enforced
caps), **IDENT-02a** (in-world `characters.description` edit), **PROFILE-03**
(per-field visibility by omission), **PROFILE-04** (reachability as its own
facet, not-found-equivalent), **PROFILE-05** (name/pronouns hard floor),
**PROFILE-10** (profile built exclusively from the viewer-filtered slice),
**EXT-06** (media proto shape ships now, empty).

**`01-SPEC.md` is normative and locks most of this phase.** §9 fixes the entire
RPC surface, `int32 expected_version`, the update-mask rules, and the eight
error codes — *"A Phase-4 planner writes every new request and response message
from this section without choosing a field type"* (§9's own words). §7 fixes the
twelve `profile.*` fields and the media naming. §8 fixes the tier ladder, the
conjunction, the totality rule, and opacity. Decisions below settle only what
those sections left open, got wrong, or explicitly deferred here.

**Not in this phase:**

- **Admin RPCs** (`AdminListCharacters`, `AdminSearchCharacters`,
  `AdminGetCharacter`, `AdminUpdateCharacter`, `AdminRetireCharacter`,
  `AdminUnretireCharacter`) — Phase 6, ADMIN-* requirements.
- **`CreateCharacter`** — IDENT-01, not in this phase's requirement set.
- **Owner-facing `RetireCharacter` / `UnretireCharacter`** — REQUIREMENTS marks
  player self-retire *"deferred beyond v0.13"*; Phase 3 closed IDENT-04 as a
  **domain** capability with the admin half in Phase 6 (ADMIN-05).
- **`RenameCharacter`** — out of the milestone entirely (Phase 3 D-44). See D-74.
- **Rendering the profile page** — Phase 5 (PROFILE-01, PROFILE-10a).

</domain>

<decisions>
## Implementation Decisions

### The viewer path and multi-alt owners (Phase 2 D-27's deferred half)

Phase 2 D-27 shipped the conservative **ALL** direction for the player-keyed
derived peers and recorded verbatim that *"the widening is a Phase-4 decision"*.
The grounded consequence, which the ROADMAP does not state:
`entity_properties.owner` is a **scalar** `TEXT` column
(`internal/store/migrations/000001_baseline.sql:360`), not a list, so
`internal/access/policy/attribute/property.go:212-215` reduces the ALL rule to
*"the owning character is that player's only character"*. **`seed:viewer-property-private-read`
is therefore structurally unsatisfiable for any player holding two or more
characters.**

- **D-69:** **Keep the ALL direction unchanged. Split the audiences instead.**
  The `viewer:` principal stays strictly public-facing; the owner-audience RPCs
  (`ListMyCharacters`, `GetMyCharacter`) gate on session resolution + ownership
  and **MUST NOT construct a `viewer:` principal at all**. An owner reaches their
  own private profile fields through the owner audience, so the multi-alt
  asymmetry never governs a path anyone depends on.
  **Rejected: relaxing `owner_player_id` to "the player behind the single owning
  character."** It is a genuine widening, not a bug fix: the grid-side
  `seed:property-private-read` is `owner == principal.character.id`, so character
  D genuinely cannot read character C's private row even when one human plays
  both. Any owner-side relaxation on the web reveals **alt-linkage the grid
  deliberately withholds** — exactly what D-27 declined.
  **Also rejected: the plain union on both permit-side peers** — D-27's declined
  alternative, reopening a settled Phase 2 decision with the largest blast radius.
  — **Reversibility:** reversible — this phase changes no policy text; it
  constrains which principal each audience constructs.

- **D-70:** **`GetCharacterProfile` returns an identical projection regardless of
  who is viewing.** One code path, no self-detection branch. The public profile
  is genuinely public — which is also the only way an owner can verify what they
  are publishing. Keeps criterion 2's marshaled-bytes assertion single-branched
  and keeps §8.9 absence-not-emptiness honest: a response whose *shape* varies by
  viewer identity is the disclosure channel §8.7 exists to close. The owner sees
  everything through `GetMyCharacter`, which is the edit surface.

- **D-71:** The D-69 consequence is pinned by a **new registry invariant plus a
  binding test**, not by a comment. The entry states that the viewer path never
  widens a character-keyed grant to the player behind it; the test seeds a
  two-alt player and asserts the private row is **absent**. It **MUST be
  hand-registered** in `docs/architecture/invariants.yaml` — the orphan check
  walks only `docs/superpowers/specs/`, so a `.planning/` origin_spec is not
  auto-caught (same escape hatch Phase 2 D-07 hit). `property.go:225-227` already
  asks for this in prose: *"Do not 'simplify' the ALL branch into an ANY branch
  for symmetry — that one-line diff reads as cleanup and reintroduces the
  widening."*
  — **Reversibility:** costly — invariant ids are referenced from tests and
  specs; renumbering is a migration, not an edit.

### The RPC surface slice, and what the census compares

- **D-72:** **The proto declares only the RPCs Phase 4 implements** —
  `ListCharacterDirectory`, `GetCharacterProfile`, `ListMyCharacters`,
  `GetMyCharacter`, `UpdateCharacterProfile`, `UpdateCharacterDescription`, plus
  their `Web*` proxies. Phases 5 and 6 add their own rows to the proto **and** to
  §3's inventory in the same change that implements them.
  **Why this is load-bearing, not cosmetic:** §2.6's census derives its set **from
  the generated service descriptors**, keyed on the fully-qualified proto method
  name — *not* from Go handlers. A declared-but-unimplemented RPC is a census
  member the moment the proto compiles, and would acquire an inventory row and an
  audience verdict it cannot honor. Declaring only what ships means every census
  member has a live handler, so criterion 1 needs **no exemption list** — the
  erosion vector never opens.
  — **Reversibility:** reversible — adding a later RPC is additive.

- **D-73:** **Criterion 1's routing census is over the `owner`-audience RPCs,
  both halves of each proxy pair.** The criterion's literal phrasing ("every
  character-returning RPC") cannot hold: `ListCharacterDirectory` and
  `GetCharacterProfile` serve **anonymous** viewers by design (§8.6 seeds
  reachability at `anonymous`), and `resolveAndGate` rejects guests outright —
  so routing them through it would break the public profile. The owner audience
  is exactly the set where *"the caller must be a non-guest player who owns this
  character"* is the contract. Public reads are gated by the reachability floor
  and per-attribute floors, asserted by criteria 2 and 3 separately.
  Mechanism: `go/ast`, set equality with a symmetric-difference diff, following
  `test/meta/world_envelope_census_test.go:187-207` and
  `test/meta/world_caller_census_test.go`.

- **D-74:** **`CharacterAccessService.RenameCharacter` is struck from §9.3** in
  this phase's amendment pass, with the deferral to backlog 999.20 (linked to
  999.6 Rostering) and the Phase 3 D-44 rationale recorded. **§9.4.2's prose must
  be fixed in the same edit** — it currently explains creation's concurrency
  guard by reference to *"the same index the `RenameCharacter` row collides
  against"*, which becomes a dangling reference. The proto declares no
  `RenameCharacter`.
  Note the amendment mechanics: there is **no** sanctioned GSD writer for
  amending SPEC or ROADMAP criterion text; a narrowly-scoped `Edit` of existing
  tool-written text is the defensible path, never a structural change and never a
  new version-bearing or ✅-bearing `###` heading.

### The D-29 permit and the description widening (criterion 6)

Phase 2 shipped only **half** of PROFILE-11. `seed:profile-public-read` is
`resource is property`, so it widens `entity_properties` rows with
`parent_type='character'`. The in-world description is a **column** on
`characters` (§7.1), reached through `resource is character`, which no widening
has touched. That is the half D-29 deferred here.

- **D-75:** The permit takes a **distinct narrow ABAC action** on
  `resource is character` — a `read_description`-shaped action, unconditional and
  off-location — served by **its own read path with its own projection**
  returning name + description only. `GetCharacter` keeps requiring `read` and
  keeps its colocation clause **untouched**, so the grid path does not move.
  "Without `PlayerId` or `LocationId`" is therefore **structural**, not a
  field-clearing step someone can forget. Follows Phase 3 D-40's precedent of
  splitting a new capability into its own action rather than reusing an existing
  one.
  **Rejected: narrowing `worldv1.CharacterInfo` itself** (dropping `player_id` /
  `location_id`) — `location_id` is load-bearing for movement and presence
  rendering, so this is a breaking change rippling through every grid consumer,
  well outside a BFF-facade phase. **Rejected: an unconditional `read` with a
  DSL `when`-clause guard** — the DSL cannot constrain what the *caller* will
  project, so the guard would describe a property of the handler rather than of
  the request; that is the shoe-horn Phase 3 D-47 rejected one level up.
  — **Reversibility:** costly — a seeded ABAC action becomes vocabulary other
  policies and tests key on.

- **D-76:** **Both a `character`-flavored permit and a `viewer`-flavored twin
  ship**, following Phase 2 D-01's twinning pattern. The character permit closes
  D-29's literal deferral (off-location grid read); the viewer twin is what lets
  the web profile reach the description at all — the web reader is a `viewer:`
  principal and **no viewer permit on `resource is character` exists today**,
  so without it Phase 5's PROFILE-10a rendering has nothing to call.
  Consequence to honor: this phase adds ABAC seeds, so the **`abac-reviewer` gate
  fires before push** (`/holomush-dev:review-abac`).

- **D-77:** **The mandated exposure audit is discharged by citing Phase 2's
  recorded result. Phase 4 authors NO new audit and performs no ceremonial
  re-run.**
  `.planning/phases/02-abac-schema-vocabulary/02-AUDIT-profile-public-read.sql`
  already covers `characters.description`: result set **(4)** (`:149-160`,
  non-empty descriptions) and set **(5)** (`:163-174`, guest-provisioned
  characters), with its header naming *"the two `characters`-column rows (name,
  in-world description)"*. It is committed, re-runnable, read-only by
  construction, and emits no player-authored text. Phase 2 wrote the description
  half of the audit and deferred only the **policy**.
  Its recorded result was a legitimate **zero** against the restored sandbox — 3
  characters, 0 non-empty descriptions, `entity_properties` empty entirely.
  GitHub **#4937** (OPEN, `priority::medium`, `awaiting-precursor`) asks for a
  re-run **against a populated corpus**, precisely because the per-row ledger has
  never adjudicated a real row. No populated corpus exists, so re-running against
  the same sandbox reproduces the same zero and proves nothing new.
  **Action: comment on #4937 recording that Phase 4 shipped the widening on
  Phase 2's evidence, and leave it OPEN.** Do not close it; do not author a
  Phase-4 audit artifact.

  > **This decision is also an anti-pattern record.** Three grades of ceremony
  > were offered for authoring a new audit before anyone checked whether one
  > existed. A criterion phrased as a **noun** ("only after an audit
  > establishes…") names a *property to establish*, not an artifact to author.
  > Normative form: engram rule `7zy1161fh1`. Narrative and citations: engram
  > memory `r65waekn3h`. Repo-side: a new section at the head of
  > `.claude/rules/references/plan-review-learnings.md`.
  > **The planner MUST NOT create a Phase-4 audit query.**

### Extraction shape and enforcement "by construction"

- **D-78:** The extracted helpers live in a **small `playerGate` struct embedded
  by both facades** (`internal/grpc`), carrying `playerSessionRepo`,
  `playerRepo`, `charRepo`. Method promotion keeps every existing
  `s.resolveAndGate(...)` / `s.ownedCharacter(...)` call site in
  `sceneaccess_service.go` **byte-identical**, so the extraction is a struct move
  rather than a rewrite of ~20 call sites.
  This shape is chosen because it is what the in-repo census precedent already
  reads: `world_envelope_census_test.go:136` uses
  `bodyReferencesSelector(fn.Body, recvName, "mutator")` — a `go/ast` check that
  a method body references a **named selector on its own receiver**. Free
  functions would force a novel AST predicate; a new package would force exported
  names and a larger move for a boundary neither the census nor the compiler
  needs.

- **D-79:** **Criterion 5 is satisfied by the type system, not by a lint rule or
  a meta-test.** The facade's world dependency is a **narrow interface** exposing
  only `world.Service.ListPropertiesByParent` (`internal/world/service.go:1410`,
  which takes a `Caller` and applies the fail-closed per-property filter) plus
  the character reads it needs. `PropertyRepository.ListByParent` is not in the
  facade's type set, so a direct call is a **compile error** — the strongest
  reading of "fails the build", with zero rules, zero suppression vocabulary and
  zero maintenance surface.
  Precedent is in the very file being extended: `sceneaccess_service.go:28-31`,
  *"the narrow interface `SceneAccessServer` needs from the plugin manager — only
  `BeginServiceDispatch`."*
  **Rejected: a `gorules` ruleguard analyzer and a `test/meta` AST test** — both
  add a second gate over a property the compiler already enforces, which §2.6
  explicitly warns costs *"a rule, a suppression vocabulary, and a maintenance
  surface for coverage the census already has"*.

- **D-80:** Criterion 2 is asserted by **seeding each withheld field with a
  distinctive sentinel, marshaling the response, and asserting the sentinel's
  bytes do not appear.** This is genuinely stronger than a post-unmarshal field
  check: for a proto3 non-optional string, an absent field and an empty one are
  **indistinguishable after unmarshal** — and "omitted vs present-and-empty" is
  the exact distinction §7.5 and §8.9 hang on. §2.7 mandates the byte level
  specifically so a hint field cannot satisfy the invariant.

### Caps, tier resolution, and the sketch findings

- **D-81:** The three ROADMAP sketch findings are **answered here and built
  nowhere** — all three are admin surface, and D-72 defers the admin RPCs:
  - **Admin rename census** — **WITHDRAWN.** Phase 3 D-44 removed rename from
    v0.13, so §9.3's `AdminRenameCharacter` row goes with the owner-facing one
    (D-74). Sketch 004's `Rename…` affordance is **not** live in v0.13, and
    sketch 009's finding #5 ("names are reserved, not permanent") is false for
    v0.13.
  - **A3** — `AdminSearchCharacters` extends to player usernames: **ACCEPTED** as
    the design, implemented in Phase 6.
  - **A2's RPC half** — the admin list RPC accepts a sort key for the joined
    `players.username`: **ACCEPTED** as the design, implemented in Phase 6.
    (§11.3's row was already amended in Phase 2 by D-26.)

- **D-82:** IDENT-02's server-enforced caps **reuse the shipped world constants**
  rather than minting new numbers: short single-line fields (`pronouns`,
  `concept`, `species`, `age`, `faction`, `currently`, `timezone`) cap at
  `MaxNameLength = 100`; long multi-paragraph fields (`appearance`,
  `personality`, `biography`, `rumors`, `rp_preferences`) at
  `MaxDescriptionLength = 4000` (`internal/world/validation.go:20-21`), with the
  same valid-UTF-8 and control-characters-except-newline/tab rules
  `ValidateDescription` applies (`validation.go:95-110`). Enforced **in the
  facade handler**, where the oops code and §9.6 wire status live.
  **IDENT-02a needs no new cap.** The description write reaches
  `world.Service.UpdateCharacterDescription`, which already runs that validation
  — Phase 4 inherits 4000 chars, UTF-8 validity and control-char rejection for
  free.

- **D-83:** The **viewer tier is resolved in the facade, at viewer-principal
  construction**, carrying criterion 3's exhaustive `switch` with `default:
  deny`: no session → `anonymous`, session with `IsGuest` → `guest`, non-guest
  session → `player`. The facade is the only layer holding the session, so this
  keeps the gateway computing nothing (§9.1) and leaves the ABAC layer unchanged.
  The resolved tier flows into the viewer principal that Phase 2's
  `internal/access/policy/attribute/viewer.go` already consumes.
  **Constraint to preserve:** `player_id` is deliberately **OMITTED** on the
  `anonymous` rung so every identity-bearing twin is unsatisfiable there rather
  than matching an empty peer. That is the omit-don't-sentinel guarantee
  (`.claude/rules/abac-providers.md`), not an accident of the policy text.

### Claude's Discretion

- Exact naming of the new ABAC action (D-75), the two seed policy ids (D-76), and
  the `playerGate` struct/field names (D-78), so long as the shapes hold.
- The new registry invariant's scope and id (D-71) — `INV-PRIVACY` or
  `INV-ACCESS` per the `boundary` declarations in `invariants.yaml`; allocate the
  next free `N` in the chosen scope.
- Test-file placement, tier, and naming throughout, per `.claude/rules/testing.md`.
- Whether the narrow world interface (D-79) is one interface or two (read vs
  mutate), provided `ListByParent` is unreachable from the facade either way.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Normative for this phase — read first
- `.planning/phases/01-portal-spec/01-SPEC.md` **§9 (lines 1927-2304)** — the
  whole RPC surface. §9.1 (typed RPC not command path; gateway computes nothing;
  projection not assembly), §9.2 (read surface + audiences), §9.3 (mutation
  surface; **amended by D-74**), §9.4-9.4.3 (`int32 expected_version`, absent-or-
  zero rejected, exactly-one-of-two-concurrent-mutations-succeeds), §9.5 (the
  four update-mask rules and the gate ordering), §9.6 + §9.6.1 (the eight error
  codes, wire opacity, and the differential assertion), §9.7 (media shape).
- `.planning/phases/01-portal-spec/01-SPEC.md` **§7 (lines 1077-1219)** — §7.1
  column-vs-row split, §7.2 the twelve `profile.*` fields, §7.3 media naming
  (zero-padded `gallery.00`…`.09`), §7.4 the description is always public on the
  profile, §7.5 the empty profile and absence-not-emptiness.
- `.planning/phases/01-portal-spec/01-SPEC.md` **§8** — §8.2-8.2.1 (tier ladder,
  set-membership clearing, never string ordering), §8.4.1 (viewer principal),
  §8.4.2 (reachability as its own resource), §8.5.1 + §8.5.1.1 (the conjunction),
  §8.6 (configured postures + totality rule; two seeded policies, not three),
  §8.7 (opacity), §8.8 (name/pronouns hard floor), §8.9 (enforcement by absence).
- `.planning/phases/01-portal-spec/01-SPEC.md` **§2.6-2.7 + §3.4** — the census
  binding mechanism (descriptor-derived, fully-qualified method name, set
  equality with symmetric-difference diff) and the no-visibility-hints-on-the-wire
  rule. §3.4 fixes the expected set as §3.3 ∪ §9's character-returning rows minus
  §2.4's deletions.
- `.planning/REQUIREMENTS.md` — IDENT-02, IDENT-02a, PROFILE-03, PROFILE-04,
  PROFILE-05, PROFILE-10, EXT-06 (in scope); PROFILE-10a, PROFILE-11 (context);
  IDENT-03 (removed, Phase 3 D-44).

### Prior-phase decisions this phase depends on
- `.planning/phases/02-abac-schema-vocabulary/02-CONTEXT.md` — **D-01** (viewer
  twins, no write twin), **D-10/D-11** (the widening posture, and why it does NOT
  extend to `resource is character`), **D-12** (the committed re-runnable audit
  pattern), **D-27** (the ALL/ANY derived-peer directions — its deferred half is
  D-69 here), **D-29** (the deferred permit — D-75/D-76 here).
- `.planning/phases/02.1-world-caller-model/02.1-CONTEXT.md` — the `Caller` model
  `world.Service` methods now take, including `ListPropertiesByParent`.
- `.planning/phases/03-world-character-commands/03-CONTEXT.md` — **D-39**
  ("their own" is policy-enforced, and its note that Phase 4's projection
  narrowing is related), **D-40** (distinct actions over reused `write` — the
  precedent D-75 follows), **D-44** (rename leaves the milestone).

### Audit evidence — cite, do not re-author (D-77)
- `.planning/phases/02-abac-schema-vocabulary/02-AUDIT-profile-public-read.sql`
  — sets (4) `:149-160` and (5) `:163-174` read `characters.description`.
- `.planning/phases/02-abac-schema-vocabulary/02-AUDIT-RESULT.md` — the recorded
  zero, with corpus identification.
- `.planning/phases/02-abac-schema-vocabulary/02-10-SUMMARY.md` — what the audit
  proved and the explicit limit on how far the zero reaches.
- GitHub **#4937** — open, `awaiting-precursor`; comment, do not close.

### Code the phase modifies or must match
- `internal/grpc/sceneaccess_service.go:106-150` — `ownedCharacter` /
  `resolveAndGate`, the extraction source; `:28-31` the narrow-interface
  precedent D-79 copies; `:846-902` `UpdateScene` + `updateSceneMaskablePaths`,
  the update-mask precedent §9.5 adopts verbatim.
- `internal/world/service.go:1410` — `ListPropertiesByParent(ctx, Caller, …)`,
  the filtered accessor PROFILE-10 mandates; `:799-836`
  `UpdateCharacterDescription`, which IDENT-02a MUST reach.
- `internal/world/validation.go:20-21,95-110` — `MaxNameLength = 100`,
  `MaxDescriptionLength = 4000`, `ValidateDescription`. D-82.
- `internal/world/grpc_server.go:127-138` — `characterToProto`, the projection
  criterion 6 forbids widening onto; `api/proto/holomush/world/v1/world.proto:77-91`
  — `CharacterInfo` and its `player_id` / `location_id`.
- `internal/access/policy/seed.go:51-54` — `seed:player-character-colocation`;
  `:649-660` the two tier-floor policies; `:678-680` reachability; `:756-786` the
  five viewer twins and the D-27 transcription comment.
- `internal/access/policy/attribute/property.go:178-240` —
  `resolveDerivedPlayerPeers`, whose doc comment states the multi-alt
  consequence; `:212-215` is the exact line D-69 turns on.
- `internal/access/policy/attribute/viewer.go` — the viewer principal provider
  D-83 feeds.
- `internal/store/migrations/000001_baseline.sql:354-375` —
  `entity_properties`, incl. the scalar `owner TEXT` at `:360` and
  `UNIQUE(parent_type, parent_id, name)` at `:368`.
- `test/meta/world_envelope_census_test.go:115-141,187-207` — `serviceMutatingMethods`
  and `bodyReferencesSelector`, the census precedent D-73/D-78 follow.
- `api/proto/holomush/sceneaccess/v1/sceneaccess.proto` — the facade shape §9.1
  says to build to; `api/proto/holomush/web/v1/web.proto:177` `WebCreateCharacter`.

### Repo rules that constrain the implementation
- `.claude/rules/gateway-boundary.md` — §"Structural writes use typed RPCs, not
  the command path"; the governing rule for §9.1 (ADR `holomush-v4qmu`).
- `.claude/rules/abac-providers.md` — omit-don't-sentinel; directly binding on
  D-83's anonymous rung.
- `.claude/rules/invariants.md` — define / respect / bind / regenerate; the
  hand-registration escape hatch D-71 hits.
- `.claude/rules/grpc-errors.md` — wire opacity and `errutil.LogErrorContext`.
  **Caution:** its §"Wire opacity needs TOP-LEVEL code assertions" is DRIFTED —
  `oops.AsOops` returns two values and `Code()` returns the *deepest* code
  (issue **#4949**, and 01-SPEC §9.6.1 documents the same, issue **#4902**).
  Assert over the wire per §9.6.1, never over the oops chain.
- `.claude/rules/references/plan-review-learnings.md` — head section, "A
  criterion phrased as a noun is not a build order". Binding on how this phase's
  plans treat criteria 1, 2, 5 and 6.
- `.claude/rules/testing.md`, `.claude/rules/logging.md`,
  `.claude/rules/proto-doc-comments.md` (every new proto element needs a
  Go-grounded leading comment; `task lint:proto` must be green).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **The whole ABAC vocabulary already ships.** Phase 2 delivered the `viewer:`
  principal and its provider, the two tier-floor policies, the reachability
  policy, the five viewer twins, and the `seed:profile-public-read` widening.
  Phase 4 builds a facade over shipped policy — it does not design the model.
- **`world.Service.ListPropertiesByParent`** is the filtered accessor with the
  fail-closed per-property loop already in place. PROFILE-10 is satisfied by
  *calling the right thing*, not by writing a filter.
- **`SceneAccessServer` is the working template** for a facade: session
  resolution, guest gate, ownership, narrow dependency interfaces, and an
  update-mask handler with a closed allowlist. §9.1 says to build to its shape.
- **`world.ValidateDescription`** already enforces IDENT-02a's cap plus UTF-8 and
  control-char rules on the path the facade must use.

### Established Patterns
- **`go/ast` set-equality censuses** (`world_envelope_census_test.go`,
  `world_caller_census_test.go`) — with a symmetric-difference diff, and
  `bodyReferencesSelector` for "this method routes through that receiver field".
- **Narrow dependency interfaces** declared at the consumer
  (`sceneAccessPluginManager`) — the idiomatic in-tree way to make an unwanted
  call fail to compile.
- **Permits combine disjunctively; deny overrides** (`engine.go:591-611`) — which
  is why §8.5.1's conjunction is ANDed by the *caller*, not by the engine.
- **Additive seeding** — a new permit is added at a new `SeedVersion` rather than
  editing a shipped policy, so an admin-customized row cannot collide.

### Integration Points
- New facade registers alongside `SceneAccessServer` in the gRPC server wiring;
  `Web*` proxies land on `WebService` in `internal/web/`.
- The `playerGate` struct is embedded by both `SceneAccessServer` and the new
  `CharacterAccessServer`.
- Two new ABAC seed entries → **`abac-reviewer` fires before push**.
- Proto changes → `task proto && task web:generate`, commit `*.pb.go` +
  `*_pb.ts` in the same change or CI fails the stale-diff check.

</code_context>

<specifics>
## Specific Ideas

- **"Stop over-engineering checks."** Recorded verbatim in substance, because it
  changed a decision in this session: *"lets not invent novel things just because
  — especially when there are already in tree solutions, tests, or OSS tooling
  that will do the job."* It is why D-77 authors no audit, D-79 uses the type
  system instead of a lint rule, D-78 matches the existing census predicate, and
  D-82 reuses the shipped constants. A plan that reaches for new machinery in any
  of those four places is reversing a decision, not filling a gap.
- **The public profile is genuinely public** (D-70). An owner viewing their own
  profile page sees exactly what a stranger sees — that is a feature, because it
  is the only way to verify what you are publishing.

</specifics>

<deferred>
## Deferred Ideas

- **A populated-corpus re-run of the exposure audit** — GitHub #4937, open and
  `awaiting-precursor`. The per-row ledger, the two verdict vocabularies, the
  digest-based re-check and B-14's "strictly greater" fixture check remain
  behaviorally unexercised until a real corpus exists. Not this phase's to
  create (D-77).
- **Relaxing `owner_player_id` to the row's player** (D-69's rejected option) —
  a real widening that would need `abac-reviewer` signoff and a §8.5 amendment.
  Reconsider only if a product requirement appears for owners to see their own
  private fields *on the public profile page*.
- **Admin RPCs, A2's sort key and A3's username search** — Phase 6, decided here
  (D-81), built there.
- **`CreateCharacter` (IDENT-01) and profile rendering (PROFILE-01,
  PROFILE-10a)** — Phase 5.
- **Rename + the approval dimension** — backlog 999.20, linked to Phase 999.6
  (Phase 3 D-44). Carries the profile-URL key question, which §9.2 has since
  settled for v0.13 (keyed on character id, never name) but which rostering will
  reopen.
- **A lint banning character-shaped proto struct literals outside the projection
  package** — §2.6 considered and deliberately did NOT mandate it. A future PR
  adding it is *"an increment, not a correction"*, and it **MUST NOT** become
  grounds for relaxing the census.
- **Whether `viewer:` and `admin_section:` should be registered in
  `knownPrefixes`** — they parse without it (`internal/access/prefix.go` has zero
  production callers). Hygiene, carried from Phase 2.

</deferred>

---

*Phase: 4-Shared Facade Helpers & `CharacterAccessService`*
*Context gathered: 2026-08-10*
