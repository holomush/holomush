# Phase 5: Character Identity UI & Public Profiles - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-08-11
**Phase:** 5-character-identity-ui-public-profiles
**Areas discussed:** signed-out chrome & route group; `CreateCharacter` reshape;
`SetDefaultCharacter`; PROFILE-12's retirement half; create honesty after the
rename deferral; profile authoring form & conflict UX; the two
deliberate-absence surfaces; criteria 4 & 5 verification. Plus two gaps that
surfaced while assembling: `/c/[id]` not-found rendering, and the roster's `Not
playable` default.

**Format note.** The first two rounds ran as multiple-choice. The user then
asked for *"more than a multiple guess set of choices — recommendations and
pros/cons would be good"*, and every round after that led with grounded analysis
and a marked recommendation before the options.

---

## Profile route & URL

| Option | Description | Selected |
|--------|-------------|----------|
| Root-level `/c/[id]` | Sibling of `/login`; inherits the root layout; no new group | ✓ |
| New `(public)` group | A place to hang public-only layout later | |
| Root-level `/characters/[id]` | One namespace with the roster — but `/characters` is inside `(authed)` | |

**User's choice:** Root-level `/c/[id]`.
**Notes:** Grounded on `web/src/routes/(authed)/+layout.ts:26-30`, whose `load()`
redirects to `/login` on session failure — a public profile cannot live there.
The third option would have put one path prefix under two auth postures.

---

## Signed-out chrome

| Option | Description | Selected |
|--------|-------------|----------|
| No new chrome — TopBar as-is | Its anonymous branch already ships and is already unconditional | ✓ |
| TopBar + fix the brand string | Also plumb the game display name (#4905) | |
| A profile-local public header | Duplicates TopBar's branch; two places to drift | |

**User's choice:** No new chrome.
**Notes:** The sketch's recorded gap ("signed-out web chrome is unspecified")
turned out narrower than written — `TopBar.svelte:141-144` renders
`Login`/`Register` for anonymous viewers today on `/login` and `/register`, both
already outside `(authed)`. That pair also discharges 007-C's "an invitation is
legal only if unconditional" constraint by construction. The brand-string fix
(#4905) was deferred as real scope growth.

---

## `CreateCharacter` reshape

| Option | Description | Selected |
|--------|-------------|----------|
| Full reshape, old shape dies same change | New facade RPC + proxy + §3 row + census entry; §2.5 replace-outright | ✓ |
| Full reshape, keep old RPC one release | Two create paths against one unique index | |
| Minimal — extend the existing RPC in place | Leaves the one owner mutation off the facade | |

**User's choice:** Full reshape, old shape dies in the same change.
**Notes:** Surfaced because the ROADMAP's "Phase 5 is established shadcn/runes
patterns, skip research" reads as UI-only, while `characteraccess.proto`
declares six RPCs and `CreateCharacter` is not among them (Phase 4 D-72 defers
each unshipped RPC deliberately, so the census never carries a member without a
handler).

---

## Creation surface

| Option | Description | Selected |
|--------|-------------|----------|
| Route `/characters/new` | Six fields + §6.1 rejection reporting need room | ✓ |
| Inline on the roster, expanded | Keeps the shipped pattern; cramped | |
| Dialog from the roster | No route, but a modal mutation surface | |

**User's choice:** Route `/characters/new` — *"but make sure we align on the
'characters' route prefix"*.
**Notes:** Alignment locked in response: `/characters*` is the owner namespace
under `(authed)` (`/characters`, `/characters/new`, `/characters/[id]`), `/c/*`
is the public one. No prefix spans two auth postures.

---

## `SetDefaultCharacter`

| Option | Description | Selected |
|--------|-------------|----------|
| New `SetDefaultCharacter` on the facade | §9.1: add the RPC, never the command path | ✓ |
| Descope "which is default" from v0.13 | Roster shows a default nobody can change | |
| Fold into `UpdateCharacterProfile` | Different resource, different gate | |

**User's choice:** New RPC on the facade.
**Notes:** Found by grepping for writers of `players.default_character_id`: the
column is read (`WebCheckSessionResponse`) and cleared on retire
(`character_repo.go:539`), and **nothing sets it**. §9.3's mutation table has no
such row, so the SPEC has a gap IDENT-05 depends on.

---

## Its response shape

| Option | Description | Selected |
|--------|-------------|----------|
| Returns the roster (`ListMyCharactersResponse`-shaped) | Client re-renders from server truth | ✓ |
| Returns the one `OwnCharacter` now default | Client patches the rest locally | |
| Returns empty | Name-reachable class, not a descriptor census member | |

**User's choice:** Returns the roster.
**Notes:** Character-shaped ⇒ a §2.6 census member with an `owner` audience
verdict and its own §3 inventory row.

---

## PROFILE-12's retirement half

| Option | Description | Selected |
|--------|-------------|----------|
| Retirement half moves to Phase 6 | Attaches to `AdminRetireCharacter`, where the flow exists | ✓ |
| Ship owner-facing Retire + Unretire too | Reverses a recorded deferral | |
| Notice on the roster's `Not playable` section | A warning where nobody is deciding | |

**User's choice:** Moves to Phase 6.
**Notes:** Criterion 4 names "the retirement flow", but IDENT-04 records player
self-retire as deferred beyond v0.13 and the only retire path is
`AdminRetireCharacter` in Phase 6. A ROADMAP/REQUIREMENTS amendment is owed.

---

## Create honesty after the rename deferral

*(First round the user asked to reformulate; this round led with analysis.)*

| Option | Description | Selected |
|--------|-------------|----------|
| Post-submit echo + static rule copy | Zero backend — the name is already on the wire in `CreateCharacterResponse` | ✓ |
| A pure `NormalizeCharacterName` RPC | Pre-submit echo, no availability promise | |
| Two-step confirm when steps 1-3 rewrote | Needs one of the above to know | |
| 009-A unchanged, override recorded | Accept the cost as the sketch stated it | |

**User's choice:** Post-submit echo + static rule copy (recommended).
**Notes:** The analysis that decided it: sketch 009 recorded its own trigger
condition ("if a later release removes or gates rename, revisit this decision"),
and rename left v0.13 on 2026-08-06 — five days after the sketch. It also
conflated two promises when rejecting variant B. The availability check is
genuinely dishonest (check ≠ insert). The **normalization echo is not** —
`charname.Normalize` is a pure function with no I/O, so the same input always
yields the same display form. Rejecting B discarded both; only the first
deserved it. The client-mirror alternative was rejected specifically because it
would duplicate a security-adjacent normalizer (NFKC, `Cf` strip, full
case-fold) in TypeScript, creating two sources of truth for the value the unique
index depends on.

---

## Owner authoring surface

| Option | Description | Selected |
|--------|-------------|----------|
| `/characters/[id]`, sectioned, edit in place | One dataset, one RPC, one page | ✓ |
| `/characters/[id]` + `/characters/[id]/edit` | Renders identical data twice | |
| Sheet overlay from the roster | Breaks the deliberate two-idiom split | |

**User's choice:** `/characters/[id]`, sectioned, edit in place (recommended).
**Notes:** §8.12 ships no visibility controls, so an owner's read view and edit
view show the *same* dataset — a view/edit split buys nothing and gives `version`
two places to go stale. The real second rendering already exists at `/c/[id]`.

---

## Save model

| Option | Description | Selected |
|--------|-------------|----------|
| Per-section save, description its own section | Two-RPC split becomes a visible section boundary | ✓ |
| Per-section save, description folded in | That section still makes two calls | |
| One Save for the whole form | A two-call chain with no rollback | |

**User's choice:** Per-section save, description its own section (recommended).
**Notes:** `UpdateCharacterProfile` and `UpdateCharacterDescription` are separate
RPCs guarding the **same** `characters.version` with no transaction between
them. A whole-form save that touches both can half-fail with `Aborted` and leave
a partial save and a stale form. Per-section save dissolves the problem: each
response returns the fresh `version` for the next save, a conflict scopes to one
section, and the description — structurally a column, not a row, reached by a
different RPC — becomes a section boundary rather than hiding behind one button.

---

## The two deliberate-absence surfaces

| Option | Description | Selected |
|--------|-------------|----------|
| Scope by viewer-variance; sheet is a named section | §8.9 says "attribute", not chrome | ✓ |
| Scope by viewer-variance; sheet is its own route | Second §8.7 not-found obligation | |
| Drop both, amend the requirements | Discards EXT-08's entire point | |

**User's choice:** Scope by viewer-variance; sheet as a named section
(recommended).
**Notes:** Resolved by finding the two rules govern disjoint sets rather than
conflicting. The discriminating test — *does this element's presence or absence
vary with who is looking?* — was recorded in CONTEXT.md so a reviewer can apply
it. §8.9's own wording is the hinge: "An **attribute** whose floor the viewer
does not clear MUST be absent."

---

## Criterion 4's "next load"

| Option | Description | Selected |
|--------|-------------|----------|
| Amend the criterion to name the poller | Substance unchanged; the latency claim stops overclaiming | ✓ |
| Keep "next load", test drives `Reload()` | Leaves a criterion asserting a latency the system lacks | |
| Test waits out the real poller | Faithful, but a 10s+ sleep coupled to a tunable | |

**User's choice:** Amend the criterion (recommended).
**Notes:** `internal/access/policy/cache.go` holds a compiled snapshot refreshed
by a poller on a **10-second default interval** (`poller.go:35`), so "next load"
is false as written. The substance — read-time evaluation, never stamped, no
backfill — is entirely intact.

---

## What criteria 4 and 5 actually build

| Option | Description | Selected |
|--------|-------------|----------|
| Two integration tests, nothing else | Cite existing coverage rather than reproving it | ✓ |
| Two tests + assert the no-backfill negative | A guard for a failure nobody has proposed | |
| One combined test | Couples two independent properties | |

**User's choice:** Two integration tests, nothing else (recommended).
**Notes:** Prior-phase artifacts and the tree were searched first, per rule
`7zy1161fh1`. Most of both criteria is already covered — the ordinal-comparison
ban, the synthetic-fourth-rung test, the per-read evaluation count, the eleven
enumerated media names, and crucially `TestPropertyRepository_ParentNameUniqueness`
(`internal/world/postgres/property_repo_test.go:430`), which already proves the
`UNIQUE(parent_type,parent_id,name)` constraint EXT-05 was going to reprove. The
honest delta is two tests: a corpus mutation + reload + re-read for criterion 4,
and the eleven real media names through the real read path for EXT-05.

---

## `/c/[id]` not-found rendering

| Option | Description | Selected |
|--------|-------------|----------|
| Inline not-found state on `/c/[id]` | One code, one state, no error boundary needed | ✓ |
| Ship the root `+error.svelte` early | Pulls a Phase 6 deliverable forward | |
| Throw 404 into SvelteKit's default boundary | Un-themed chrome on a shareable public URL | |

**User's choice:** Inline (recommended).
**Notes:** Both causes return the same `CHARACTER_PROFILE_NOT_FOUND` (§9.6 makes
it deliberately one code for two causes), so indistinguishability holds at the
page level with no boundary involved. Constraint carried to Phase 6: when sketch
010-B's shared page ships, `/c/[id]` must adopt it.

---

## Roster `Not playable` default

| Option | Description | Selected |
|--------|-------------|----------|
| Expanded, chip is a collapse control | The sketch's own inclination — these are the player's own characters | ✓ |
| Collapsed by default | Hides the player's own characters behind a click | |
| Expanded, no collapse control | Chip stays decoration, which is what left it open | |

**User's choice:** Expanded, chip as collapse control (recommended).
**Notes:** Relabel the chip away from "hidden", which presumes the opposite
default. `idle` is unreachable in v0.13, so no copy may assume a player sees it.

---

## Claude's Discretion

- Section grouping within the twelve `profile.*` fields, and the exact copy of
  the not-retroactive notice and the name-normalization rule line.
- Which shadcn components to add (`avatar`, `field`, `sonner`, `select`,
  `skeleton` are plausible; the initial-letter portrait is pure CSS in 007-C and
  may need no `avatar`).
- Whether `/c/[id]` and `/characters/[id]` share a presentational identity-card
  component.

## Deferred Ideas

Full list with rationale in CONTEXT.md `<deferred>`. Headlines: the game display
name on the web client (#4905, and `/c/[id]` is the first public shareable page
the platform ships); the shared `+error.svelte` (#4903, Phase 6); owner-facing
Retire/Unretire; `RenameCharacter` + the approval dimension (999.20, which would
restore sketch 009-A's original premise); a pre-submit `NormalizeCharacterName`
RPC; the image uploader; any conditional sign-in invitation (permanently
forbidden as an oracle); an operator-facing tier preview; the populated-corpus
audit re-run (#4937); a character-name length cap; and a `profile.currently`
freshness signal.

Five todos matched by area heuristic and were reviewed but **not folded** — all
`world.Caller` / INV-WORLD internals from Phases 02.1–02.2, none touching this
phase's surfaces.
