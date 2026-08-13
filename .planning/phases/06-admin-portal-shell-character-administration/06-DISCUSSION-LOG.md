# Phase 6: Admin Portal Shell & Character Administration - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-08-13
**Phase:** 6-admin-portal-shell-character-administration
**Areas discussed:** Per-section endpoint shape, Nav registry derivation, Audit envelope
before-values, Acting-identity carriage, `AdminSearchCharacters` semantics, `last_active_at`
rendering, PROFILE-12 retirement copy, Bottom-sheet grab handle, Mutation loop sequencing,
`roles` on `WebCheckSessionResponse`

---

## Framing correction before the discussion began

The scout found that **Phase 2 already shipped the admin-section authorization substrate** —
registry, mandatory descriptor, boot validation, shared gate helper, seed policy, set-equality
meta-test — and that `AssertSectionAccess` has **zero production callers**. It also found
**#4904 (the denial-code oracle) CLOSED** by Phase 2's D-06 gate-then-distinguish, pinned by
`INV-PRIVACY-11`. Both reframed the phase from "build the gate" to "wire the gate that exists",
and removed a gray area that the ROADMAP's sketch-findings line still presents as open.

---

## Per-section endpoint shape

**First option set was REJECTED by the maintainer**, who asked: *"why would we do section
oriented stubs/rpcs? seems like we'd be binding rpcs to ui layout/ordering concerns? what am
I missing?"*

The objection was correct in half and incorrect in half, and both halves mattered:

- **Incorrect half:** a section is not a UI concept — `admin_section:<id>` is a shipped ABAC
  resource family (`internal/access/prefix.go:71`) with its own seed policy. The nav is
  derived from the registry (§10.1), not the reverse.
- **Correct half:** manufacturing seven stub RPCs whose only job is to be denied does bind
  the wire surface to a menu, and was the wrong reading of §10.2's "that section's endpoint".

The pushback also exposed a search failure on my side (rule `7zy1161fh1`): `internal/plugin/hostcap/`
already carries the descriptor-table + interceptor + fail-closed-meta-test pattern that makes
stubs unnecessary. The question was reformulated around that finding.

### Reformulated Q1 — gate carrier

| Option | Description | Selected |
|--------|-------------|----------|
| Descriptor table + interceptor | Mirror `hostcap`: method→section table, interceptor before dispatch, meta-test that an undeclared admin method fails closed. Deviates from ADMIN-02's letter. | ✓ |
| Per-handler assertion as first statement | What §10.4 literally specifies and what `AssertOperatorAdmin` does today. No new machinery, no amendment. A handler omitting the line is ungated with nothing failing. | |
| Both — interceptor as fence, handler call as belt | Satisfies ADMIN-02 verbatim while keeping the structural guarantee. Costs a double ABAC evaluation and two places to read when tracing a denial. | |

**User's choice:** Descriptor table + interceptor.
**Notes:** Choosing this **over** "both" means ADMIN-02's per-handler re-assertion no longer
describes what ships — recorded as amendment 1. The winning argument: redundancy protects
against a human forgetting; a fail-closed descriptor removes the forgetting rather than
surviving it (the same shape as Phase 4's D-79). Settled without asking, from existing rules:
the interceptor mounts core-side only, because `.claude/rules/gateway-boundary.md` and §10.4
both put an `internal/web/` authorization decision in the wrong process.

### Reformulated Q2 — the surface carrying planned-section refusal and the nav

| Option | Description | Selected |
|--------|-------------|----------|
| One generic registry RPC, id as a parameter | `AdminGetSection(section_id)` gate-then-`SECTION_NOT_IMPLEMENTED`, plus `AdminListSections` returning the viewer-permitted set with status. Seven denial assertions iterate `All()` against one endpoint. Census +2 (+2 proxies). | ✓ |
| `AdminListSections` only — no per-id probe | Leanest (census +1), client renders from `status: planned`. But §10.2's denial enumeration loses its per-id subject — one assertion instead of seven, weakening EXT-04's non-vacuity claim. | |
| No section RPC — client mirror + roles | Zero round trip, zero census growth, pinned by a cross-language set-equality test. Costs a second registry in `web/src/` (Pitfall 7's named artifact) and §10.2's test loses its subject entirely. | |

**User's choice:** One generic registry RPC, id as a parameter.
**Notes:** This answered the nav-derivation question in the same stroke — the nav becomes a
server-filtered projection of a real ABAC decision rather than a client guess. D-06's
gate-then-distinguish ordering is explicitly preserved in the handler: `AssertSectionAccess`
runs before `section.Lookup`.

---

## Audit envelope before-values

Grounding that reframed the question: `BuildCharacterProfileUpdatePayload`
(`internal/world/payloads.go:445-466`) carries changed attribute **names and not values**,
stating *"Profile prose is player-authored personal content and the taxonomy's payload rule is
new-values-only AND erasure-safe"*; `payloads_test.go:976` pins the convention. Every one of
§10.6's 13 allowlisted paths is prose. ADMIN-06's "before-values are the whole point" therefore
**contradicts** an existing designed property rather than inheriting from it.

| Option | Description | Selected |
|--------|-------------|----------|
| Split by field kind | Prose → changed names only + before-version on the outbox delta; lifecycle → real before-`status`. Erasure-safety intact. Amendment scoped to prose fields. | ✓ |
| Full before-values, plaintext | ADMIN-06 as written; best content for the deferred audit viewer. Copies player prose into a retained table, breaking erasure-safety; an erasure request can no longer be honored by clearing the row. | |
| Full before-values, encrypted payload | Meets ADMIN-06 with confidentiality at rest. Retained-is-still-retained (encryption is not erasure); pulls `crypto-reviewer` onto the phase; needs a host-event `crypto.emits` equivalent that does not exist. | |

**User's choice:** Split by field kind.
**Notes:** An auditor still learns who, when, which character, which fields, and — for lifecycle
transitions — what the value was. Rated `one-way` in CONTEXT.md: widening the payload later is
additive, but narrowing it is not, because prose already written into `events_audit` partitions
is retained.

---

## Acting-identity carriage

| Option | Description | Selected |
|--------|-------------|----------|
| Player subject only, character omitted | `Actor` = `player:<id>`, already carried verbatim and already an audit requirement by byte identity (`caller.go:42-45`). Acting character omitted — §10.5.1.2 makes it authorization-irrelevant. Amendment strikes "record both". | ✓ |
| Player subject as Actor, acting character in payload | Meets §10.7 verbatim; an auditor sees which alt was in the chair. Costs a durable player↔alt linkage — the disclosure Phase 4's D-27 kept off the read path. | |
| You decide | | |

**User's choice:** Player subject only, character omitted.
**Notes:** Follow-on recorded for the planner — with `Actor` carrying `player:<id>` by byte
identity, an `acting_player_id` payload field would be redundant.

---

## `AdminSearchCharacters` semantics

Grounding: `characters_normalized_name_key` is a btree UNIQUE on `normalized_name`
(`000056:68-69`), so prefix matching is free and substring matching is not.

| Option | Description | Selected |
|--------|-------------|----------|
| Prefix on both, no new index | Rides the existing UNIQUE btree; username needs at most a plain btree. No mid-name matching — `mir` misses `Kaelmir`. *(recommended)* | |
| Substring on both, add a trigram index | `ILIKE '%q%'` over both columns, backed by a new `pg_trgm` GIN migration. Finds a half-remembered character. Costs an extension + index migration and write amplification. | ✓ |
| Prefix on name, exact on username | Cheapest and most predictable. No partial username matching — the case A3 was raised to serve. | |

**User's choice:** Substring on both, add a trigram index — **overriding the recommendation.**
**Notes:** Planner cautions recorded: `pg_trgm` must be creatable in the target deployment (not
universally available on managed Postgres without a pre-approved extension list), and a plain
`CREATE INDEX` should be preferred over `CONCURRENTLY`, which would force
`-- +goose NO TRANSACTION` and surrender atomicity for a table this size.

---

## `last_active_at` rendering

| Option | Description | Selected |
|--------|-------------|----------|
| Relative granularity, `never` sorts last both ways | Coarse relative text is honest about the flush-interval lag (AR-03-03) without a disclaimer. Labeled `Last active`, never `Online`/`Last seen`. | ✓ |
| Absolute timestamp, `never` sorts last both ways | Directly comparable against logs. A precise-looking value that can be a flush interval stale reads as more authoritative than it is, and invites being treated as presence. | |
| Omit the column in v0.13 | Sidesteps staleness and presence-oracle framing. A sort control over an invisible value is worse than no sort, and A1 was raised so the list could show it. | |

**User's choice:** Relative granularity, `never` sorts last both ways.
**Notes:** The both-directions property is the subtle half — most-recent-first gets it free
because `0` is the column minimum, but oldest-first needs an explicit
`ORDER BY (last_active_at = 0), last_active_at ASC`. A test exercising only one direction
passes under the bug. This is the first surface to expose the column; Phase 5 did not.

---

## PROFILE-12 retirement copy (D-91)

| Option | Description | Selected |
|--------|-------------|----------|
| A retire-specific statement for this actor | Corrects the likelier misconception (retire = takedown): reversible, profile stays visible, name stays reserved, published history unchanged. | ✓ |
| Reuse Phase 5's not-retroactive copy verbatim | One string, both surfaces, zero new copy to review. Addressed to someone choosing what to reveal about their own profile — which an admin is not. | |
| No statement — close the retirement half as N/A | The requirement names a PLAYER's retirement flow, which does not ship. The admin confirm would then say nothing about reversibility or visibility. | |

**User's choice:** A retire-specific statement for this actor.
**Notes:** Four clauses are fixed (out of active play; not hidden; name reserved; undoable);
phrasing is Claude's discretion. Surfaced during this area: Phase 6's ROADMAP `Requirements:`
line omits PROFILE-12 entirely, so the clause has no home — recorded as amendment 6, matching
the flag already sitting at `05-VERIFICATION.md:176`.

---

## Bottom-sheet grab handle

| Option | Description | Selected |
|--------|-------------|----------|
| Drop the handle | Close via backdrop, Escape, explicit Cancel/×. No gesture, no broken promise, no new dependency. | ✓ |
| Honor it — real drag-to-dismiss | Pointer-drag with threshold and spring-back, respecting reduced-motion. `bits-ui` Sheet has no swipe-dismiss, so this means hand-rolling or adding a drawer dependency. | |
| You decide | | |

**User's choice:** Drop the handle.
**Notes:** Consistent with the milestone's posture of never showing an affordance it cannot
honor — sketch 009 refuses to promise name availability on the same grounds.

---

## Mutation loop sequencing

| Option | Description | Selected |
|--------|-------------|----------|
| Success closes the Sheet then toasts; a conflict keeps it open | Row updates in place from the response (never a refetch); toast names the RPC. On `Aborted`, the Sheet stays open with typed text, inline conflict, fresh version, focus on the failing field, and no toast. | ✓ |
| Toast first, Sheet stays until dismissed | No re-open needed for a second edit. A toast saying the write landed while the producing form is still on screen reads as unfinished, and the Sheet's `version` header metadata is stale. | |
| You decide | | |

**User's choice:** Success closes the Sheet then toasts; a conflict keeps it open.
**Notes:** Mirrors Phase 5's D-93 conflict behavior exactly, which was verified live in
`05-UAT.md` test 3 (two tabs, one row).

---

## `roles` on `WebCheckSessionResponse`

A gray area **created by** the registry-RPC decision — with the nav server-filtered, ADMIN-08's
`roles` field is no longer strictly required for nav derivation.

| Option | Description | Selected |
|--------|-------------|----------|
| Ship `roles` anyway — ADMIN-08 as written | Player-scoped and singular per §10.5.1.1. Lets the rail decide whether to draw `/admin` without an `AdminListSections` round trip on every authed layout load for non-admin sessions. | ✓ |
| Drop `roles` — rail keys on `AdminListSections` returning non-empty | One source instead of two, no role vocabulary on the wire. Costs an extra RPC per layout load and a third amendment on a field §10.5.1.1 says must not be reshaped later. | |
| Leave it to the planner | | |

**User's choice:** Ship `roles` anyway.
**Notes:** Rated `one-way` in CONTEXT.md on §10.5.1.1's own grounds — reshaping this field later
is a wire-compat change to every caller.

---

## Claude's Discretion

- Where the method→section descriptor table lives (alongside `internal/admin/section/` vs a new
  package), and the `ADMIN_SECTION_NOT_DECLARED` code spelling — `gate.go` already documents a
  six-code taxonomy to extend.
- Whether the two section RPCs land on `CharacterAccessService` or a new admin-facing service.
  Both plus their `Web`-prefixed proxies become §3.4 census members either way.
- Relative-time granularity buckets for `last_active_at`, and the exact wording of the admin
  retire confirm (its four clauses are fixed).

## Deferred Ideas

- Drag-to-dismiss on the phone bottom-sheet — revisit if a drawer dependency arrives for other
  reasons.
- The audit log viewer — the `audit` section stays `planned`; emission ships now so the viewer
  has history (backlog 999.8).
- Role mutation and the `players` section — blocked on #4899.
- Prose/content search over profile fields — §11.1's prohibition reaches it.
- Player-initiated self-retire (IDENT-04, deferred beyond v0.13).
- Character rename (D-44 → backlog Phase 999.20).
- Exposing the game's display name to the web (#4905) — sidestepped here by using `Home`.
