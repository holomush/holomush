---
phase: 05-character-identity-ui-public-profiles
plan: 07
subsystem: ui
tags: [svelte5, sveltekit, connectrpc, runes, vitest, tdd, accessibility, aria]

# Dependency graph
requires:
  - phase: 05-character-identity-ui-public-profiles
    plan: 01
    provides: "listMyCharacters / setDefaultCharacter (web/src/lib/characters/client.ts) and the (authed) layout's defaultCharacterId"
  - phase: 05-character-identity-ui-public-profiles
    plan: 02
    provides: "CharacterPortrait.svelte — the one 16% tint treatment, sized 44px here and 80px on the public profile"
  - phase: 05-character-identity-ui-public-profiles
    plan: 06
    provides: "takeCreatedNotice (web/src/lib/characters/createdNotice.ts) — the one-shot notice carrying name, characterId and profileIncomplete"
provides:
  - "web/src/lib/components/characters/RosterCard.svelte — one card, the badge matrix and the template-level session suppression"
  - "web/src/lib/components/characters/CharacterRoster.svelte — the props-only sectioned body with the collapse chip and both empty states"
  - "The rewritten /characters route — two reads joined on the character id, the default control, and the CONSUMER of createdNotice"
  - "The roster selector surface plan 05-08 repoints the eight broken Playwright specs at"
affects: [05-08]

actuals:
  tokens: 9500
  tasks: 3
  commits: 5

tech-stack:
  added: []
  patterns:
    - "A lifecycle switch with a DENYING default arm placed in the template, so the rule holds for every caller rather than for as long as an upstream join remembers to clear the data"
    - "Promise.allSettled over two reads that answer different questions, so the non-load-bearing one degrades the badges instead of the page"
    - "A collapse that REMOVES its region from the markup rather than hiding it, so aria-expanded=\"false\" is not a claim over content still in the accessibility tree"

key-files:
  created:
    - web/src/lib/components/characters/RosterCard.svelte
    - web/src/lib/components/characters/RosterCard.svelte.test.ts
    - web/src/lib/components/characters/CharacterRoster.svelte
    - web/src/lib/components/characters/CharacterRoster.svelte.test.ts
  modified:
    - web/src/routes/(authed)/characters/+page.svelte

key-decisions:
  - "The session badge renders one of TWO AUTHORED WORDS (`Active` | `Offline`) and never forwards `CharacterSummary.sessionStatus`. The shipped card rendered `char.sessionStatus || 'Offline'`, which puts whatever token the server happens to store onto a player-facing badge. UI-SPEC's badge table fixes the vocabulary at two words; this is the same class of finding as 05-06's `rawMessage` — the obvious spelling leaks wire vocabulary at the player."
  - "`idle` and every unrecognised lifecycle share the `Retired` badge rather than earning a word of their own. UI-SPEC's badge table is binding and says `lifecycle not active → Retired only`; authoring an `Idle` badge would teach a player a word for a state v0.13 has no transition into, which is exactly what D-96 forbids."
  - "The roster `h1` changed from `Choose Your Character` to `Your characters`. The page now also creates, sets the default and lists retired characters, so `choose` names a fraction of it. This is a BEHAVIOURAL CHANGE 05-08 must absorb — see the selector table."
  - "One `role=\"alert\"` region carries BOTH write failures (a failed default change and a failed character selection), not the default change alone. The plan's acceptance names only the default change; a select failure is also a write failure and the alternative was routing it into the load-failure state, which would replace the whole roster with `Couldn't load your characters.` — wrong copy for a failure that is not a load. The criterion's substance holds exactly: a create that succeeded is never announced as an alert."
  - "The chip label is `Hide N character(s)` expanded and `Show N character(s)` collapsed — count plus action, and neither form uses a word presuming the section starts collapsed."

patterns-established:
  - "Both REDs reproduce an implementation the repository actively invites rather than a missing module: Task 1's is the shipped roster card lifted verbatim; Task 2's is the ordinary show/hide accordion. 6/12 and 7/13 failed — the passes are the specs a wrong card or a wrong default still satisfies."
  - "A gate that scans the whole file, not only its markup, means a COMMENT may not be what trips it (05-02's discipline). Two comments were reworded rather than the gates loosened."

requirements-completed: [IDENT-05, IDENT-01]

coverage:
  - id: D1
    description: "A retired character never wears a session word, and the suppression is in the template rather than in the data"
    requirement: IDENT-05
    verification:
      - kind: unit
        ref: "RosterCard.svelte.test.ts#shows the Retired badge ONLY for a retired character, with no session word"
        status: pass
      - kind: unit
        ref: "RosterCard.svelte.test.ts#renders no session word for a retired character even when session data IS supplied — the load-bearing spec: it fails if the suppression ever moves upstream into the join"
        status: pass
      - kind: other
        ref: "observed RED: the shipped two-way branch rendered `Offline` on both retired cases and on idle"
        status: pass
    human_judgment: false
  - id: D2
    description: "The unreachable idle lifecycle and any unrecognised status take a denying arm — no session badge, no bespoke prose, no throw"
    requirement: IDENT-05
    verification:
      - kind: unit
        ref: "RosterCard.svelte.test.ts#renders no session word and no bespoke prose for the unreachable idle lifecycle — asserts the absence of /idle/i as well as of the session vocabulary"
        status: pass
      - kind: unit
        ref: "RosterCard.svelte.test.ts#renders without throwing and without a session badge on an unrecognised lifecycle"
        status: pass
    human_judgment: false
  - id: D3
    description: "The badge matrix: Default alongside the session badge when default and active, session alone when not default, Retired alone otherwise"
    requirement: IDENT-05
    verification:
      - kind: unit
        ref: "RosterCard.svelte.test.ts — the three active-lifecycle specs, each asserting the Default badge's presence or absence by data-testid rather than by text"
        status: pass
      - kind: unit
        ref: "CharacterRoster.svelte.test.ts#marks exactly the default character, and only when it is playable — asserts the badge COUNT is 1 and that it sits on the right card"
        status: pass
    human_judgment: false
  - id: D4
    description: "The Not playable section renders expanded, its chip is a real collapse control, and the section is omitted entirely when empty"
    requirement: IDENT-05
    verification:
      - kind: unit
        ref: "CharacterRoster.svelte.test.ts#renders the Not playable section EXPANDED on first paint"
        status: pass
      - kind: unit
        ref: "CharacterRoster.svelte.test.ts#removes the not-playable grid from the markup when the chip is activated — asserts querySelector is null, not that a class was applied"
        status: pass
      - kind: unit
        ref: "CharacterRoster.svelte.test.ts#omits the Not playable section entirely — asserts the heading STRING is absent from innerHTML"
        status: pass
      - kind: unit
        ref: "CharacterRoster.svelte.test.ts#points the chip aria-controls at the not-playable grid it governs — compares against the grid's actual id rather than a literal"
        status: pass
      - kind: other
        ref: "observed RED: the accordion shape rendered the section at zero, started collapsed, and hid the grid with display:none"
        status: pass
    human_judgment: false
  - id: D5
    description: "Zero characters, all-not-playable, one and many each render their authored copy and chrome"
    requirement: IDENT-05
    verification:
      - kind: unit
        ref: "CharacterRoster.svelte.test.ts — the zero-character, all-not-playable, one-playable and singular/plural-chip specs"
        status: pass
      - kind: other
        ref: "acceptance greps — no `<table`, no `@container`; the responsive rule is `@media (min-width: 768px)` over a single-column default"
        status: pass
    human_judgment: false
  - id: D6
    description: "createdNotice is RENDERED to the player, and its profileIncomplete variant links to /characters/{characterId}"
    requirement: IDENT-01
    verification:
      - kind: other
        ref: "acceptance greps — `takeCreatedNotice(` appears at exactly one call site; `profileIncomplete` appears on a non-comment line inside the role=status region; the link is built from `createdNotice.characterId`, never `.name`"
        status: pass
      - kind: manual
        ref: "05-07-PLAN.md Task 3 <human-check> — create a character with all six fields and confirm the confirmation names the SERVER's stored name; then force a partial write and confirm the repair link lands on the identity section"
        status: pending
    human_judgment: true
    rationale: "The branch, the region's role and the link's target are all pinned by grep and by reading, but the notice arrives across a real navigation from a live create. Nothing short of a running grid exercises the producing side and the consuming side together, and the partial variant additionally needs a create whose second transaction actually loses a value. Recorded as pending rather than counted as delivered — this is the ONE truth that 05-06 explicitly handed forward, so it is the one worth being honest about."
  - id: D7
    description: "Two round trips joined on the character id, degrading correctly when only the session read fails"
    requirement: IDENT-05
    verification:
      - kind: other
        ref: "acceptance greps — exactly one `listMyCharacters` and one `webListCharacters` call site; no session-check call anywhere in the route"
        status: pass
      - kind: other
        ref: "reading — Promise.allSettled: a rejected lifecycle read sets loadFailed; a rejected session read leaves the overlay map empty and every row simply has no overlay, which is the same code path as a character absent from the session result"
        status: pass
      - kind: manual
        ref: "05-07-PLAN.md Task 3 acceptance — the session-call-only failure path renders the roster without session badges rather than the failure copy"
        status: pending
    human_judgment: true
    rationale: "The degradation is one branch (`summaries.status === 'fulfilled'`) and is shared with the ordinary absent-from-the-second-result case, which IS asserted — a RosterCard given no `session` prop renders `Offline`. What is not asserted is the route's own wiring, because the route is a page component with two module-level RPC dependencies and the plan's files list does not extend to a stub harness for it. Called out rather than claimed."
  - id: D8
    description: "The portrait is the one shared tinted component, and the roster carries no solid accent plate"
    requirement: IDENT-05
    verification:
      - kind: unit
        ref: "RosterCard.svelte.test.ts#draws the portrait with the one shared tinted component — asserts the portrait class AND aria-hidden AND the absence of bg-primary"
        status: pass
      - kind: other
        ref: "acceptance greps — `CharacterPortrait` matches; `bg-primary` and any six-digit hex or the amber token return no match"
        status: pass
    human_judgment: false
  - id: D9
    description: "The default control reaches the typed RPC, keeps its label in flight, and moves the marker only on success"
    requirement: IDENT-05
    verification:
      - kind: unit
        ref: "RosterCard.svelte.test.ts#keeps the Make default label unchanged in flight and marks it busy — asserts the exact label, aria-busy and disabled"
        status: pass
      - kind: unit
        ref: "RosterCard.svelte.test.ts#reports the character id when Make default is activated, without also selecting the character — asserts onselect was NOT called"
        status: pass
      - kind: other
        ref: "acceptance grep — `sendCommand` returns no match in the route"
        status: pass
    human_judgment: false

duration: 11min
completed: 2026-08-12
status: complete
---

# Phase 05 Plan 07: the sectioned owner roster Summary

**`createdNotice` finally has a reader — the roster renders both of its forms, and the partial variant's link turns a lost profile write from a discovery into a one-click repair. Along the way: the suppression rule moved into the template where it holds for every caller, and the session badge stopped forwarding a server token to a player.**

## Performance

- **Duration:** 11 min (first commit 19:30, last 19:37, plus gates)
- **Tasks:** 3 of 3
- **Commits:** 5 (two RED/GREEN pairs + the route)
- **Files created:** 4, modified 1

## Task Commits

| Task | Commit | What landed |
| --- | --- | --- |
| 1 (RED) | `f3015c808` | `RosterCard.svelte.test.ts` + the shipped card lifted verbatim — 6 failed / 6 passed in-file |
| 1 (GREEN) | `fdcd459c3` | the lifecycle switch, the denying default, `CharacterPortrait` — 545 passed |
| 2 (RED) | `769ac8d4a` | `CharacterRoster.svelte.test.ts` + the ordinary accordion — 7 failed / 6 passed in-file |
| 2 (GREEN) | `ab3d33a71` | omission, expansion, markup removal, the singular — 557 passed |
| 3 | `c8a232d20` | the rewritten `(authed)/characters/+page.svelte` |

## What this plan closes

**1. `createdNotice` had no reader.** 05-06 computed the confirmation and the `profileIncomplete` flag on create and then discarded them: the mechanism was built and tested on the producing side, and a player saw nothing. This roster is the consumer. Both forms now render in the persistent `role="status"` region, and the partial variant links to `/characters/{characterId}`, whose section 1 holds exactly the five fields the create submits — so the recovery lands on the surface that repairs precisely what was lost. Q1's accepted risk is no longer silent.

**2. The roster rendered its own solid `bg-primary` plate.** 05-02 shipped `CharacterPortrait` at the 44px roster size for exactly this, and its own SUMMARY misattributed the adoption to plan 05-08. It is this file, and it is done: `rg -n 'bg-primary'` on `RosterCard.svelte` returns nothing, and the 16% tint is now one treatment by construction across the roster and the public profile rather than two that happen to agree.

**3. Creation lands on `/characters`, not `/terminal`.** The page a just-created character now arrives on is a management surface: it names itself `Your characters`, sections by whether a character can be played, and carries the confirmation naming the name the server actually stored.

## The two REDs, and the finding of the run

Both RED phases went red against **the implementation this repository actively invites**.

**Task 1 — the shipped card, lifted verbatim.** Its `{#if session?.hasActiveSession}Active{:else}Offline{/if}` branch has no lifecycle knowledge at all, so it rendered `Offline` on a retired character, on an idle one, and on an unrecognised status. Six of twelve specs failed; the six that passed are the three active-lifecycle cases and the mechanics a card with no lifecycle knowledge still gets right. That separation is the point: **a two-way session branch is indistinguishable from a correct card for every character a developer is likely to have in their test data.**

**Task 2 — the ordinary accordion.** Section always rendered, starts collapsed, collapses with `display: none`, pluralises unconditionally. Seven of thirteen failed. The one worth naming is the collapse: a `display: none` grid is still in the DOM, so `aria-expanded="false"` becomes a claim about content that may still be announced. The spec asserts `querySelector(...)` is `null`, which a class-based assertion cannot.

**The finding of the run is smaller than 05-06's but the same shape.** The shipped card renders `char.sessionStatus || 'Offline'` — it forwards whatever token the server stores into a player-facing badge. UI-SPEC's badge table fixes the session vocabulary at exactly two words. The obvious spelling ("show the session status") is the one that leaks wire vocabulary, exactly as `ConnectError.message` did in 05-06. The card now renders `Active` or `Offline` and reads `sessionStatus` for nothing.

## The suppression rule, and why it is in the template

`Retired · Offline` is meaningless, and the two vocabularies collide on the token `active` — a character's lifecycle can be `active` and its session can be `Active`, and they mean different things. D-96 resolves it by suppressing the session badge on every non-`active` lifecycle.

**Clearing the session overlay in the route's join would produce identical pixels today.** It would also hold only for as long as the join kept remembering to do it, and the join is the part of this page most likely to be rewritten (it exists only because two messages disagree about what a character is). The rule is therefore a template branch, and the spec that pins it supplies session data *deliberately* and demands the vocabulary still be absent. If a future refactor moves the suppression upstream, that spec is what notices.

The switch has a **denying default**: `idle`, and anything the server adds later, take the arm that grants no session badge and no `Default` badge. `idle` shares the `Retired` marker rather than earning a word of its own, per UI-SPEC's binding `lifecycle not active → Retired only` — authoring an `Idle` badge would teach a player a word for a state v0.13 has no transition into.

## Ordering — what the key actually is

The plan carries `IDENT-05/ordering` as a BACKSTOP truth, and UI-SPEC's `populated | roster ordering within Playable` row is the phase's one unresolved UI row. **Here is what ships, stated plainly so a human can rule on it:**

- **The ordering key is the server's return order from `webListMyCharacters`, filtered.** `CharacterRoster` calls `characters.filter(...)` twice — once for `status === 'active'`, once for everything else — and `Array.prototype.filter` is order-preserving. Nothing in this plan sorts, and no client-side comparator exists.
- **Upstream, `charRepo.ListByPlayer` declares no ORDER BY** (05-01 recorded this). PostgreSQL without an `ORDER BY` gives no guarantee, so **two consecutive loads with no intervening write are not *proven* to render the same order** — in practice a small unchanging heap scan will, but that is an implementation accident, not a contract.
- **After a successful default change the order changes source**: the route re-renders from the roster `SetDefaultCharacter` returned, not from the original read. Both are the same server-side query, so this is not a second ordering — but it is a second call site the eventual `ORDER BY` must cover.
- **Sectioning already defuses what the question was really about.** A retired character can no longer float to the top of the grid, because it is in the other section.

**The determinism truth is therefore NOT established by this plan**, and cannot be from the client. The fix is one `ORDER BY` in `charRepo.ListByPlayer` — a server change, outside this plan's files.

## Selectors and accessible names for plan 05-08

**Preserved — 05-08 needs to change nothing here:**

| What | Selector / value |
| --- | --- |
| Character name on a card | `[data-testid="char-name"]` — used by `character-switcher.spec.ts:47,102` and `negative-journeys.spec.ts:277` |
| Create affordance | `a[data-testid="create-character"]`, `href="/characters/new"`, visible text `Create a character` |
| Default control | `button[data-testid="make-default"]`, `name="makeDefault"`, accessible name `Make default` |
| Default marker | `[data-testid="default-badge"]`, text `Default` |
| Loading copy | `Loading characters…` |

**Changed — 05-08 must absorb this:**

| Was | Now | Where it bites |
| --- | --- | --- |
| `h1` `Choose Your Character` | `h1` **`Your characters`** | `character-switcher.spec.ts:65` locates `text=Choose Your Character`. That spec is already in 05-08's eight (it clicks `text=Create New Character` and `button[role="checkbox"]`, both deleted by 05-03), so this is one more line in a file 05-08 is rewriting, not a new file to touch. |

**New — available to 05-08 if it wants stronger anchors:**

| What | Selector |
| --- | --- |
| Any roster card (playable or not) | `[data-testid="roster-card"]` — the playable ones carry `role="button"` and are clickable across the whole surface |
| Not-playable card's profile link | `[data-testid="view-profile"]`, `href="/c/{id}"`, text `View profile →` |
| Session badge | `[data-testid="session-badge"]`, text `Active` or `Offline` |
| Lifecycle badge | `[data-testid="lifecycle-badge"]`, text `Retired` |
| Collapse chip | `button[data-testid="not-playable-toggle"]`, `aria-expanded`, `aria-controls="roster-not-playable"` |
| Not-playable grid | `[data-testid="not-playable-grid"]` — **absent from the DOM when collapsed or when empty** |
| Load retry | `button[data-testid="retry-load"]`, text `Try again` |
| Create confirmation | the page's single `[role="status"]`; the repair link inside it is `a[href^="/characters/"]` with text `character page` |
| Write failure | the page's single `[role="alert"]` |

**One more behavioural note for 05-08:** the card click target is unchanged in effect — `[data-testid="char-name"]` is inside the clickable card, so `charCard.click()` still bubbles to the selection handler and still lands on `/terminal`.

## Deviations from Plan

**None requiring a rule.** No bug, missing-critical-functionality, blocking issue or architectural change arose.

Four plan instructions were read narrowly rather than literally, all recorded as decisions in the frontmatter:

1. **`rg -c 'takeCreatedNotice'` returning 1 is unsatisfiable as written** — a named import and a call site are two lines, and `rg -c` counts lines. The substance is one CALL site, which `rg -c 'takeCreatedNotice\('` confirms as **1**.
2. **`rg -n 'webCheckSession'` returning no match** initially matched two explanatory COMMENTS. Rather than loosen the gate, the comments were reworded to say "the layout's own session check" — per 05-02's discipline that a gate scanning the whole file must not be trippable by a comment. Same treatment for `permanent` in `CharacterRoster.svelte`.
3. **The `role="alert"` region carries both write failures**, not the default change alone. See the frontmatter decision; the criterion's substance (a succeeded create is never an alert) holds exactly.
4. **The chip label** and the roster `h1` are executor-authored — UI-SPEC has no row for either. Both are in its register (sentence case, naming the action) and both are flagged below for the next copy revision.

## Verification

All run inline in this worktree, judged by **exit code**:

| Gate | Exit | Result |
| --- | --- | --- |
| `task lint` | 0 | pass |
| `task build` | 0 | pass |
| `task test` | 0 | 11636 tests, 4 skipped (unchanged) |
| `task fmt` | 0 | **no mutations** — nothing to commit |
| `cd web && pnpm check` | 0 | 0 errors, 6 warnings (all pre-existing: CreateSceneForm, PresenceList) |
| `cd web && pnpm test:unit` | 0 | `Test Files 56 passed (56)` / `Tests 557 passed (557)` — from 54/533, this plan adds 24 |
| `cd web && pnpm build` | 0 | `✓ built` |

**Web tests are still not a CI gate** (issue #4964): nothing in `Taskfile.yaml` or `.github/workflows/ci.yaml` invokes vitest or svelte-check. Every run's own summary lines are transcribed into the task commit bodies as evidence rather than claimed, per the plan's acceptance criteria.

Every acceptance grep returns its stated result:

- `CharacterPortrait` in `RosterCard.svelte` → **matches** (import + use at 44px); `bg-primary` → **no match**; `#[0-9a-fA-F]{6}|ffb300` → **no match**; `/c/` → the profile link built from the character **id**
- `aria-expanded|aria-controls` in `CharacterRoster.svelte` → **both**; `hidden characters|show hidden|permanent` → **no match**; `<table` → **no match**; `@container` → **no match**, the responsive rule is `@media (min-width: 768px)` over a single-column default
- route: `listMyCharacters` and `webListCharacters` → **one call site each**, both inside one `Promise.allSettled`; `webCheckSession` → **no match**; `takeCreatedNotice(` → **1**; `role="status"` and `role="alert"` → **both**; `profileIncomplete` → line 164, a non-comment `{#if}`; `sendCommand` → **no match**; `lastPlayedAt|lastLocation|formatDate` → **no match**; `<Input|Checkbox` → **no match**

## Known Stubs

**None.** No placeholder component, disabled control, hardcoded value or "coming soon" copy ships. Every branch is reachable: both empty states, both chip states, all four lifecycle arms, both notice variants, both failure modes.

## Deferred / out of scope

- **Task 3's `<human-check>` is pending** and is the plan's most consequential unrun verification. It is the only thing that exercises the create → navigate → announce round trip end to end, and the partial variant additionally needs a create whose second transaction really does lose a value. Recorded in `.planning/WINDOWS.md` as an `unrun-verify`.
- **The session-call-only failure path** is verified by reading rather than by execution. Its branch is shared with the character-absent-from-the-second-result case, which IS asserted at the card level (no `session` prop → `Offline`), but the route's own wiring has no stub harness — the plan's files list does not extend to one and the plan permits manual verification here.
- **The ordering determinism truth is not established** and cannot be from the client. See the ordering section above; the fix is one `ORDER BY` in `charRepo.ListByPlayer`.
- **Two more executor-authored strings** join 05-06's list for the next UI-SPEC copy revision: the collapse chip's `Hide N character(s)` / `Show N character(s)` and the roster `h1` `Your characters`.
- **The eight Playwright specs remain 05-08's**, per the dispatch. `web/e2e/**` was not touched.

## Threat Flags

**None.** No new network endpoint, auth path, file access pattern or schema change. This plan's own register is covered:

- **T-05-07-01** (another player's roster) — both list RPCs resolve whose roster is returned from the header token; neither request message carries a player id, and the route passes none.
- **T-05-07-02** (presence telemetry on a retired character) — suppressed in the template, and the assertion supplies session data **deliberately**. Worth marking as *actively* verified rather than planned: a presence-only assertion would pass with the leak sitting beside it.
- **T-05-07-03** (a forged create confirmation) — read once from a module value written only by a successful create; there is no URL parameter to craft, and `rg` confirms the route reads no search param.
- **T-05-07-04** (default change through the command parser) — the typed RPC through the character client; `sendCommand` negative-grepped in all three files.
- **T-05-07-05** (roster load on every visit) — `accept`ed as planned; two bounded owner-scoped reads behind session auth.
- **T-05-07-06** (a stale default marker after a failed set) — `defaultCharacterId` is assigned only inside the `try` after the await resolves; the `catch` sets the alert copy and leaves the marker where the server last put it.

## Tooling note

`requirements mark-complete` reported `table_unmatched` for both `IDENT-05` and `IDENT-01` (as in 05-01 through 05-06) — neither the checkboxes nor the traceability table updated this time (`write_set_complete: false`). Not hand-patched, per the dispatch instruction; for the phase-end reconciliation. Tracked in the #4963/#4964 family.

`state add-decision` records each decision as `[Phase ?]` rather than `[Phase 05]` — a pre-existing labelling gap in the same family, noted rather than worked around.

## Next Phase Readiness

- **05-08** has the three-part selector table above: what is preserved (five entries it need not touch), the one heading that changed and where it bites, and eight new anchors it may prefer over text matching. The one behavioural note that matters: card selection still works through `[data-testid="char-name"]`.
- **The phase-end reconciliation** should confirm (a) the requirements traceability table across all seven plans, (b) that `deferred-items.md`'s Playwright entry closes once 05-08 lands, and (c) the two pending human checks — 05-06's create walkthrough and this plan's roster walkthrough — which are now answerable together, because the first one's first assertion is about this page's confirmation line.

## Self-Check: PASSED

| Claim | Result |
| --- | --- |
| `web/src/lib/components/characters/RosterCard.svelte` | FOUND |
| `web/src/lib/components/characters/RosterCard.svelte.test.ts` | FOUND |
| `web/src/lib/components/characters/CharacterRoster.svelte` | FOUND |
| `web/src/lib/components/characters/CharacterRoster.svelte.test.ts` | FOUND |
| `web/src/routes/(authed)/characters/+page.svelte` | FOUND (modified) |
| `f3015c808` `fdcd459c3` `769ac8d4a` `ab3d33a71` `c8a232d20` | all FOUND |
| deletions across the five commits | **none** (`git diff --diff-filter=D` empty) |
| `web/e2e/**` untouched | confirmed — no e2e path in any of the five commits |

---
*Phase: 05-character-identity-ui-public-profiles*
*Completed: 2026-08-12*
