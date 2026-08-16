# Phase 1: Portal SPEC - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-08-01
**Phase:** 1-Portal SPEC
**Areas discussed:** Audience wire mechanism, Lifecycle vocabulary, Description visibility (expanded into game-controlled visibility policy), SPEC shape & binding

---

## Audience wire mechanism

### Q1 — How is "absent from the response" achieved on the wire?

| Option | Description | Selected |
|--------|-------------|----------|
| Separate messages per audience | `PublicCharacter`/`OwnCharacter`/`AdminCharacter` as distinct proto messages, one projection per (row → audience), plus a lint banning character-shaped literals outside the projection package. PITFALLS #2's own recommendation. Absence becomes a type-system property. | ✓ |
| One message, `optional` scalars cleared | Single `Character` message, privacy-bearing fields `optional`, cleared server-side. Cheaper, but a scalar someone forgets to mark `optional` marshals `""` — present, not absent — and nothing in the build catches it. | |
| Hybrid — split the public boundary only | Distinct `PublicCharacter` for the logged-out surface; owner and admin share one message. | |

**User's choice:** Separate messages per audience
**Notes:** User added unprompted: *"we're still at a good point that breaking changes are ok, no other current users of the system other than me."* This removed the grandfathering constraint from every subsequent question in this area.

### Q2 — What happens to `WebListAllCharacters`?

| Option | Description | Selected |
|--------|-------------|----------|
| Reclassify as admin-audience | Move the whole RPC behind the admin gate; no public character list in v0.13 (the searchable directory is already deferred). | |
| Split into two RPCs | Public/player list returning identity-only `PublicCharacterSummary`, plus a separate admin list with the rich row. | ✓ |
| Demote in place to public shape | One RPC carrying identity fields only; admins get rich data from character detail instead. | |

**User's choice:** Split into two RPCs
**Notes:** Yields three list surfaces cleanly mapped to three audiences — own roster, public all, admin all.

### Q3 — Which enforcement mechanism binds Phase 4?

| Option | Description | Selected |
|--------|-------------|----------|
| Census with set equality only | PORTAL-10 rule 1 already mandates it, so it costs nothing extra; catches the missing-member case a per-endpoint suite structurally cannot. | ✓ |
| Census plus a struct-literal lint | Adds a custom go-analysis linter; catches inline construction inside a censused RPC. Costs a golangci module-plugin registration plus exclusions. | |
| Census now, lint deferred to an issue | Explicit deferral with recorded rationale. | |

**User's choice:** Census with set equality only

---

## Lifecycle vocabulary

### Q1 — What shape does the lifecycle take on `characters`?

| Option | Description | Selected |
|--------|-------------|----------|
| `status TEXT` + CHECK | Matches the repo's four existing CHECK-enum precedents. Purge is not a state — it is `DeleteCharacter`. | ✓ |
| `status` enum plus `retired_at` | Adds a dated transition for audit and reversibility. Two columns that can disagree. | |
| Nullable timestamps only, state derived | No CHECK to migrate; state machine lives in Go with no database backstop. | |

**User's choice:** `status TEXT` + CHECK

### Q2 — Does the CHECK vocabulary carry `idle` when nothing can reach it?

| Option | Description | Selected |
|--------|-------------|----------|
| Ship all three, mandate exhaustive switch | `('active','retired','idle')` from day one, plus a rule that every lifecycle read is an exhaustive `switch` with `default: deny`, and a test constructing `idle` directly. Safe by construction, non-vacuously tested, no later ALTER. | ✓ |
| Ship only reachable states | `('active','retired')`; `idle` named in prose, added by the migration that implements it. | |
| Ship all three, no extra rule | Cheapest text; reproduces research CONFLICT 4's fail-open shape. | |

**User's choice:** Ship all three, mandate exhaustive switch
**Notes:** Presented alongside the structural rhyme with research CONFLICT 4 — an unimplemented privacy tier whose evaluator reads `switch { case Private: false; default: true }` fails open, and `if status == "retired" { deny }` has exactly the same shape.

### Q3 — What is a retired character's reachability?

| Option | Description | Selected |
|--------|-------------|----------|
| Roster-visible, unselectable, profile resolves marked retired | Coherent with the name staying reserved for rostering and with scene archives publishing the name publicly regardless. | ✓ |
| Roster-visible, unselectable, profile not-found | Retirement doubles as withdrawal from the public surface. Leaves names in archives with no profile behind them. | |
| Owner chooses at retirement | Stores the choice as a reachability property row. Makes retire partly a privacy control. | |

**User's choice:** Roster-visible, unselectable, profile resolves marked retired

---

## Description visibility → game-controlled visibility policy

> This area started as a narrow PROFILE-10a question and the user redirected it into a new,
> larger requirement. Both halves are logged.

### Q1 — How does `characters.description` get its visibility handling?

| Option | Description | Selected |
|--------|-------------|----------|
| Paired visibility property, default private | Fail-closed; profiles launch with their most substantial text missing. | |
| Paired visibility property, default public | Owner control with a deliberately-public default; PROFILE-11's audit becomes the precondition. | |
| Always public on the profile, no control | The co-location gate was never a privacy control for this text; the web removes the location requirement, not a privacy boundary. | ✓ |

**User's choice:** Always public on the profile, no control

### Q2 — Does "always public" survive the profile-level reachability facet?

| Option | Description | Selected |
|--------|-------------|----------|
| Reachability gates everything | Facade evaluates reachability first; below the floor, not-found-equivalent including description. | |
| Description is public regardless | Literal grid-parity; a private profile still serving text. | |
| No profile-level facet in v0.13 | Per-field visibility only. | |

**User's choice:** *(none — redirected)*
**Notes:** User answered with a new requirement instead: *"I think that we need to have a per character attribute web profile visibilty setting that is admin controlled, not player or character controlled. This needs to have a set of defaults, but be entirely configurable. Some games may want nearly everything about a character visible and scrapable to anonymous users, others may want require guests and allow them to see most things, and still others may want to require actual players as the floor."* An earlier question in this area had also been declined with a request to clarify. Assistant grounded the new requirement against existing substrate (the `seed:property-*` policy family, `policies.source='admin'`, the already-deferred `config` admin section) before reformulating.

### Q3–Q6 — Shape of the game-controlled visibility policy

| Question | Options | Selected |
|---|---|---|
| Granularity | Game-wide per attribute / game-wide + per-character override / per-character only | **Game-wide, per attribute** |
| Mechanism | ABAC policy family with `source='admin'` overrides / settings store consumed by the projection / seeded policy with override deferred | **ABAC policy family, admin-source overrides** |
| Composition with owner privacy | Both must permit / admin floor overrides owner / owner overrides upward | *(see notes)* |
| Delivery in v0.13 | Model + defaults, editor with the config section / minimal editor in Phase 6 / operator tooling only | **Model + defaults; editor with the config section** |

**Notes on composition:** User rejected all three offered options in favour of a stronger position — *"I think that from a web profile standpoint we do not allow player/character agency at all."* This removes owner-facing per-field control entirely rather than subordinating it. Assistant then audited what this displaced and reported that PROFILE-03/04/05 survive (PROFILE-03 never named *who* sets visibility), while ROADMAP Ph4 SC3, Ph5 SC4, PROFILE-12's toggle half, and research CONFLICT 4's owner-UI line require amendment.

### Q7–Q9 — Consequences of the policy

| Question | Options | Selected |
|---|---|---|
| PROFILE-05 (name/pronouns) | Hard invariant / configurable like any attribute | **Hard invariant** |
| Mechanism into the read path | Read-time evaluation on attribute name / stamp `visibility` on the row from config | **Read-time evaluation** |
| Seeded default posture | Guest floor for prose + anonymous identity / anonymous for everything / player floor for everything | **Guest floor for prose, anonymous identity** |

### Q10 — Resolving the description-floor inconsistency

Assistant flagged that the chosen default places `description` at **anonymous**, while the user's own stated grid-parity principle implies a **player** floor — two of the user's statements that did not agree.

| Option | Description | Selected |
|--------|-------------|----------|
| Keep anonymous — default deliberately more open | Grid-parity is the floor the principle guarantees, not a ceiling on what a game may publish. SPEC states the divergence explicitly. | ✓ |
| Move description to a player floor | Strict grid-parity as the default. Also thins PROFILE-01's logged-out-visitor promise. | |
| Guest floor for description | Splits the difference; simplifies the default table to two tiers. | |

**User's choice:** Keep anonymous — default is deliberately more open

---

## SPEC shape & binding

### Q1 — One SPEC document or several?

| Option | Description | Selected |
|--------|-------------|----------|
| One SPEC | Single document; no sibling to drift from. | ✓ (with correction) |
| Portal SPEC plus admin-IA SPEC | Split at the Phase 6 seam. | |
| Master SPEC plus per-domain sub-specs | Mirrors the crypto master/sub-epic structure. | |

**User's choice:** *"let's please follow the GSD convention for SPEC files, superpowers is gone. One spec is fine. If GSD has no convention/requirement then we can put it in the spot 1 defines, but let's update the appropriate CLAUDE.md files"*
**Notes:** The question had offered `docs/superpowers/specs/` as the location. User corrected the location while accepting the one-document shape. Assistant verified GSD's convention is `${phase_dir}/${padded_phase}-SPEC.md` and that `docs/superpowers` is referenced in ~20 files including live CI gates, then split the retirement into its own scope question (below).

### Q2 — How do PORTAL-10's six rules reach later plans?

| Option | Description | Selected |
|--------|-------------|----------|
| Normative section + verbatim AC block per plan | `gsd-plan-checker` verifies presence and phase-specialization. | ✓ |
| Same, plus a meta-test over plan files | Mechanical, but asserts on planning markdown and is hard to demonstrate RED. | |
| Normative section only | v0.12 catalogued 17 unfalsifiable verifications with these same gates in place. | |

**User's choice:** Normative section plus verbatim AC block in each plan

### Q3 — Which invariant registry scopes?

| Option | Description | Selected |
|--------|-------------|----------|
| ACCESS, PRIVACY and WORLD | Three existing scopes; lifecycle lands in WORLD, whose boundary already covers MODEL-01 world-model guarantees. | ✓ |
| ACCESS and PRIVACY only | Most literal reading of rule 6; makes ACCESS's boundary statement false for lifecycle. | |
| Declare a new PORTAL boundary | Keeps v0.13's guarantees together; a new scope must earn itself. | |

**User's choice:** ACCESS, PRIVACY and WORLD

### Q4 — How much `docs/superpowers` retirement lands in Phase 1?

| Option | Description | Selected |
|--------|-------------|----------|
| Pointer update only; retirement as its own issue | Phase 1 updates CLAUDE.md + spec-path rules; the ~20-file sweep gets an issue with the file list. | ✓ |
| Retire it fully inside Phase 1 | No half-state; but a second milestone-sized change inside the opening phase, touching the docs fast-lane gates. | |
| Write the SPEC now, defer every doc change | CLAUDE.md would actively contradict where the milestone's own SPEC lives. | |

**User's choice:** Pointer update only; retirement as its own issue

---

## Claude's Discretion

Taken without consultation, announced before CONTEXT.md was written:

- **PORTAL-09 verdict = no** — no v0.13 surface sorts or filters on a profile field; read-time tier evaluation makes it incoherent as well as expensive.
- **`expected_version` carriage** — a scalar field on each mutation request, matching migration `000049`'s shape, not a shared embedded precondition message.
- **Read-surface inventory (PORTAL-02) and name-capture inventory (PORTAL-03)** — enumeration work, not forks.
- **The seven admin section ids** — as already named in EXT-01.
- **SPEC filename and internal section ordering.**

## Deferred Ideas

- Admin editing UI for the visibility-floor configuration — lands with the `config` admin section's handler body (EXT-01/02 already register it gated-and-`NOT_IMPLEMENTED`).
- Full `docs/superpowers/` retirement sweep — ~20 files including the invariant orphan-check walk root and both docs-paths gates. Own issue.
- Struct-literal lint for character-shaped proto literals — considered, consciously not mandated; the next increment if the census proves insufficient.
- A fourth viewer-tier rung, and the representation of the visibility configuration for the future editor — raised as possible topics, not pursued.
