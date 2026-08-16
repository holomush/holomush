---
phase: 05-character-identity-ui-public-profiles
plan: 06
subsystem: ui
tags: [svelte5, sveltekit, connectrpc, unicode, runes, vitest, tdd, accessibility]

# Dependency graph
requires:
  - phase: 05-character-identity-ui-public-profiles
    plan: 03
    provides: "createCharacter (web/src/lib/characters/client.ts) → WebCreateCharacter → the facade's CreateCharacter, returning OwnCharacter with the SERVER-stored name and the profile map"
  - phase: 05-character-identity-ui-public-profiles
    plan: 04
    provides: "ByteCounter.svelte (TextEncoder-based, 100-byte short cap) and the props-only component idiom"
  - phase: 05-character-identity-ui-public-profiles
    plan: 02
    provides: "isAlreadyExistsError / isInvalidArgumentError in web/src/lib/connect/errors.ts"
provides:
  - "/characters/new — the structured six-field creation card (IDENT-01, D-87)"
  - "web/src/lib/characters/createFlow.ts — submitCreateCharacter, the authoritative-RPC create flow"
  - "web/src/lib/characters/createdNotice.ts — setCreatedNotice / takeCreatedNotice, the one-shot cross-navigation notice carrying name, characterId and profileIncomplete"
  - "web/src/lib/components/characters/CreateCharacterForm.svelte — the props-only six-field form"
  - "The stable E2E selector surface plan 05-08 repoints the eight broken Playwright specs at"
affects: [05-07, 05-08]

actuals:
  tokens: 9975
  tasks: 3
  commits: 5

tech-stack:
  added: []
  patterns:
    - "ConnectError.rawMessage, never .message, whenever a server string is rendered verbatim — .message carries a [code] prefix that turns 'pass it through untouched' into wire vocabulary at the player"
    - "A partial-outcome flag computed by comparing SUBMITTED keys against RETURNED keys, never map size — the non-empty filter is what keeps the ordinary case from reporting a loss"
    - "A one-shot module value (not a query parameter, not a store) to carry a server-issued confirmation across one navigation"

key-files:
  created:
    - web/src/lib/characters/createFlow.ts
    - web/src/lib/characters/createFlow.test.ts
    - web/src/lib/characters/createdNotice.ts
    - web/src/lib/components/characters/CreateCharacterForm.svelte
    - web/src/lib/components/characters/CreateCharacterForm.svelte.test.ts
    - web/src/routes/(authed)/characters/new/+page.svelte
  modified: []

key-decisions:
  - "The InvalidArgument arm reads ConnectError.rawMessage, not .message. This was NOT in the plan and is the single most consequential finding of the run: .message is the raw string with a `[invalid_argument] ` prefix bolted on, so the obvious spelling of 'render the server message verbatim' renders machine vocabulary at a player on the one surface a first-time player forms their impression."
  - "The name input keeps name=\"characterName\", the exact attribute the eight broken Playwright specs already fill. The five profile inputs take their bare field names. This makes 05-08's repair a navigation change rather than a selector rewrite."
  - "The name's rune counter is ALWAYS shown, not gated behind ByteCounter's within-20%-of-cap display rule. UI-SPEC scopes that rule to BYTE counters on the profile fields; the name is not a byte-capped field, it is the one required field with a hard 2–32 bound, and the always-visible count is part of the static rule's affordance."
  - "Two copy strings are executor-authored because UI-SPEC has no row for them: the required-name message (`Enter a character name.`) and the page heading (`Create a character`). Both are in UI-SPEC's register — declarative, sentence case, naming the next action. The heading matches the roster's create-card label so the link and its destination agree."
  - "The plan's authored partial-create copy (`Created {name}. Some details didn't save — add them on the character page.`) is NOT rendered here. This plan produces the FLAG; plan 05-07's roster renders the notice. Recorded so 05-07 does not re-author it."

patterns-established:
  - "Plausible-wrong RED, twice, each reproducing an in-repo precedent rather than a missing module: the shipped register/+page.svelte catch idiom, and String.length for a code-point cap. 5 failed / 7 passed on both — the passes prove the RED discriminates the defect instead of proving a file is absent."
  - "A two-direction comparison spec on the SAME submitted set, plus the empty-map case, because a single-direction spec passes against a hardcoded constant"

requirements-completed: [IDENT-01]

coverage:
  - id: D1
    description: "A player fills six fields on /characters/new, submits once, and lands on /characters with the new character created"
    requirement: IDENT-01
    verification:
      - kind: unit
        ref: "web/src/lib/characters/createFlow.test.ts#returns the created character and stores the notice before navigating"
        status: pass
      - kind: unit
        ref: "web/src/lib/components/characters/CreateCharacterForm.svelte.test.ts#submits all six values once when the name is present"
        status: pass
      - kind: manual
        ref: "05-06-PLAN.md Task 3 <human-check> — sign in, fill six fields with a full-width name, submit, confirm the folded name is echoed and a resubmit preserves all six fields"
        status: pending
    human_judgment: true
    rationale: "Every layer is asserted in isolation and the two halves meet in the route file, but the round trip through a live facade — and the visual confirmation that the echo shows the FOLDED name — cannot be exercised without a running grid. The echo's second site (the roster's status region) is plan 05-07, so the walkthrough is only fully answerable after that lands."
  - id: D2
    description: "The success echo is the SERVER-returned name from OwnCharacter.name, never the submitted string"
    requirement: IDENT-01
    verification:
      - kind: unit
        ref: "createFlow.test.ts#stores the SERVER-returned name, not the submitted string, when the two differ — asserts equality with the response AND inequality with the full-width submission"
        status: pass
      - kind: other
        ref: "observed RED: the naive flow stored the submission and failed exactly this spec plus the plain-name spec"
        status: pass
    human_judgment: false
  - id: D3
    description: "A rejected submit preserves all six entered values and only the error region changes; focus moves to the name field"
    requirement: IDENT-01
    verification:
      - kind: unit
        ref: "CreateCharacterForm.svelte.test.ts — the AlreadyExists and InvalidArgument specs each read all six input values individually after the rejection"
        status: pass
      - kind: unit
        ref: "CreateCharacterForm.svelte.test.ts#moves focus to the name input after a rejection — focus is deliberately parked on `faction` first, so the assertion cannot pass by accident"
        status: pass
    human_judgment: false
  - id: D4
    description: "A name collision renders authored copy and never names the colliding character; an invalid name renders the server message verbatim with an authored fallback; any other failure renders authored generic copy and never a code"
    requirement: IDENT-01
    verification:
      - kind: unit
        ref: "CreateCharacterForm.svelte.test.ts — AlreadyExists spec asserts the ABSENCE of the server's own message alongside the authored line"
        status: pass
      - kind: unit
        ref: "CreateCharacterForm.svelte.test.ts#renders an InvalidArgument message verbatim — asserts exact equality and the absence of both `invalid_argument` and `[`"
        status: pass
      - kind: unit
        ref: "CreateCharacterForm.svelte.test.ts#falls back to authored copy when an InvalidArgument carries no message"
        status: pass
      - kind: unit
        ref: "CreateCharacterForm.svelte.test.ts#renders the generic create-failure copy on any other code and never the rejection message — asserts absence of the code, the detail and the word `internal`"
        status: pass
    human_judgment: false
  - id: D5
    description: "A post-create navigation failure never surfaces as a create failure"
    requirement: IDENT-01
    verification:
      - kind: unit
        ref: "createFlow.test.ts#reports success when the post-create navigation fails — asserts the character is returned, console.warn fired, and the notice still holds"
        status: pass
      - kind: other
        ref: "acceptance grep — try/catch in createFlow.ts guards the navigation only (lines 102/104); the create call is at line 88, outside it"
        status: pass
    human_judgment: false
  - id: D6
    description: "A profile value the create's second transaction lost is reported, computed from submitted-vs-returned keys and never from a read-back"
    requirement: IDENT-01
    verification:
      - kind: unit
        ref: "createFlow.test.ts — five specs: all-five-returned false, strict-subset true (also asserting name and id), name-only false against an EMPTY map, one-submitted in both directions, whitespace-only filtered"
        status: pass
      - kind: other
        ref: "acceptance greps — `rg -o 'profile\\.[a-z_]+'` lists exactly the five governed rows; no getMyCharacter/listMyCharacters/webListCharacters in the flow"
        status: pass
      - kind: manual
        ref: "the notice's player-facing rendering (the partial-outcome copy and the /characters/[id] repair link) is plan 05-07's roster — this plan produces the flag, not the sentence"
        status: pending
    human_judgment: true
    rationale: "The flag is fully computed and pinned in both directions, but nothing renders it yet: the consumer is 05-07's roster status region. Until that lands, a lost profile write is computed and then discarded at the (unbuilt) consumer — the same end state as before this plan, with the mechanism in place. Called out rather than counted as delivered."
  - id: D7
    description: "The name is capped in RUNES at 32 while the five profile fields are capped in BYTES at 100, and the form does not conflate the two units"
    requirement: IDENT-01
    verification:
      - kind: unit
        ref: "CreateCharacterForm.svelte.test.ts#counts the name in code points, not UTF-16 code units — the fixture asserts its own [...].length is 6 and .length is 12 before asserting the counter"
        status: pass
      - kind: unit
        ref: "CreateCharacterForm.svelte.test.ts#renders a byte counter for a short profile field at its own cap, not the name cap — 100 bytes exactly, data-over=false"
        status: pass
      - kind: other
        ref: "observed RED: the naive counter reported `12 / 32` for the astral fixture"
        status: pass
    human_judgment: false
  - id: D8
    description: "The submit button keeps its label in flight, becomes disabled and gains aria-busy=true; every input carries a name attribute and the button carries type=submit"
    requirement: IDENT-01
    verification:
      - kind: unit
        ref: "CreateCharacterForm.svelte.test.ts#keeps the button label byte-identical in flight — asserts before, during and after"
        status: pass
      - kind: unit
        ref: "CreateCharacterForm.svelte.test.ts#renders six named inputs and one submit button carrying the authored label — asserts the input[name] count is exactly 6"
        status: pass
    human_judgment: false
  - id: D9
    description: "No client-side mirror of the name pipeline, no availability check, no debounced lookup, and creation never reaches the command path"
    requirement: IDENT-01
    verification:
      - kind: other
        ref: "acceptance grep — normalize|NFKC|casefold|toLowerCase|availability|debounce in CreateCharacterForm.svelte, comment lines filtered: no match"
        status: pass
      - kind: other
        ref: "acceptance grep — sendCommand in the form and in the route: no match; the submit reaches createCharacter on the typed client through the flow"
        status: pass
      - kind: other
        ref: "acceptance grep — permanent|rename|forever in the form: no match (the property is RESERVED, and rename left the milestone)"
        status: pass
    human_judgment: false

duration: 17min
completed: 2026-08-12
status: complete
---

# Phase 05 Plan 06: `/characters/new` — the structured creation card Summary

**Six fields, one Save, and a rejection that costs the player nothing but the round trip — plus the discovery that `ConnectError.message` carries a `[code]` prefix, which turns the obvious spelling of "render the server's message verbatim" into machine vocabulary shown to a player.**

## Performance

- **Duration:** 17 min (first commit 19:13, last 19:22, plus gates)
- **Tasks:** 3 of 3
- **Commits:** 5 (two RED/GREEN pairs + the route)
- **Files created:** 6, modified 0

## Task Commits

| Task | Commit | What landed |
| --- | --- | --- |
| 1 (RED) | `5e53dd30e` | `createFlow.test.ts` + `createdNotice.ts` + the plausible-wrong flow — 5 failed / 7 passed |
| 1 (GREEN) | `fecb3b67f` | the response-read echo and the submitted-vs-returned comparison — 12 passed |
| 2 (RED) | `bcc4364ad` | `CreateCharacterForm.svelte.test.ts` + the plausible-wrong form — 5 failed / 7 passed |
| 2 (GREEN) | `be405a9d3` | code-based classification, `rawMessage`, the rune counter — 12 passed |
| 3 | `2af8484be` | `web/src/routes/(authed)/characters/new/+page.svelte` |

## The two REDs were discriminating, and one of them found a real bug

Both RED phases went red against **the implementation this repo actively invites**, not against a missing module — and both produced 5 failed / 7 passed, which is the shape that matters: the seven passes are specs a hardcoded constant or a wrong unit still satisfies.

**Task 1 — the flow.** The naive version echoed `fields.name` and inferred a lost profile write from `Object.keys(profile).length < 5`. It passed the all-five-returned spec and the strict-subset spec, and failed the name-only case, the one-submitted case in both directions, the whitespace filter and both echo specs. That is exactly the separation the plan asked for: **a size comparison reports a loss on every name-only create**, which is the first create every player ever makes.

**Task 2 — the form, and the finding of the run.** The naive version used the shipped `register/+page.svelte` catch idiom, `e instanceof Error ? e.message : …`. It produced:

```
expected 'that name contains no visible characters; please use letters'
received '[invalid_argument] that name contains no visible characters; please use letters'
```

`ConnectError.message` is the raw string with a `[code]` prefix bolted on; `rawMessage` is the server's own words. **The plan says "renders the error's own message verbatim" and never names which property that is** — and the property a reader reaches for first is the wrong one. This is not cosmetic: `CHARACTER_NAME_INVALID` is the single code whose message is deliberately passed through to a player, precisely because the pipeline authored those strings for players. Prefixing them with a gRPC code name undoes the reason the exception exists, and it does so on the one surface where a first-time player forms their impression.

The second RED was the counter: `12 / 32` for a six-code-point astral name. Every ASCII fixture agrees with the bug.

## The four subtle rules, and where each is pinned

| Rule | Where it is pinned |
| --- | --- |
| **1. A rejected submit preserves all six values** | Both rejection specs read all six inputs *individually* after the refusal — not a sample. Preservation is by construction (nothing resets a field), and the specs are the regression guard that keeps it so. Focus is asserted with the cursor deliberately parked on `faction` first, so it cannot pass by accident. |
| **2. The echo is honest; no client-side mirror** | The flow reads `character.name` from the response; the differing-names spec asserts equality with the response *and* inequality with the submission. The negative grep (`normalize\|NFKC\|casefold\|toLowerCase\|availability\|debounce`, comment lines filtered) returns no match on the form. |
| **3. The non-empty filter is load-bearing** | Five specs, including the two the plan named as separating: the name-only create against an **empty** response map (false), and the same submitted key returning vs not returning (false, then true). A whitespace-only value is filtered on the same rule the handler applies. |
| **4. Three unit systems stay apart** | `[...name].length` against 32; `ByteCounter` against 100 bytes on the five profile fields. Both asserted, and the astral / 100-byte fixtures each assert their own measurement before asserting the counter, so a drifted fixture fails loudly. |

## Selectors and accessible names for plan 05-08

This is the surface 05-08 should target when repointing the eight Playwright specs. **Every one of these is asserted by a unit spec**, so a drift here fails `pnpm test:unit` before it fails E2E.

| What | Selector / value |
| --- | --- |
| Roster create affordance | `a[data-testid="create-character"]`, `href="/characters/new"`, visible text **`Create a character`** |
| Destination route | `/characters/new` |
| Page heading | `h1` — **`Create a character`** |
| Character name input | **`input[name="characterName"]`** — *unchanged from the deleted inline form* |
| Pronouns / concept / species / age / faction | `input[name="pronouns"]`, `[name="concept"]`, `[name="species"]`, `[name="age"]`, `[name="faction"]` |
| Submit | `button[type="submit"]`, accessible name **`Create character`** |
| Rune counter | `[data-testid="name-counter"]`, text `N / 32` |
| Byte counters (profile fields) | `[data-testid="byte-counter"]` — only within 20% of the 100-byte cap |
| Error region | `[role="alert"]` (the only one on the page) |

**Three changes 05-08 must make beyond the selector swap:**

1. **`text=Create New Character` is gone.** The card now reads `Create a character` (UI-SPEC Primary CTAs). Prefer `[data-testid="create-character"]` over text.
2. **The checkbox is gone.** `fixtures.ts:104` clicks `button[role="checkbox"]`; the creation card has no checkbox and never did in the new design.
3. **Creation no longer lands on `/terminal`.** `submitCreateCharacter` navigates to **`/characters`**. `registerAndEnterTerminal` (`fixtures.ts:102-106`) expects `/terminal` immediately after the create click, so it needs an added step: after landing on `/characters`, click the new character's card (`[data-testid="char-name"]`'s card) to select it, then await `/terminal`.

The name attribute was deliberately kept as `characterName` for exactly this reason — it reduces the eight specs' repair to a navigation change rather than a field rewrite.

## Decisions Made

1. **`rawMessage`, not `message`** — see above. Recorded as the run's finding.
2. **The name input keeps `name="characterName"`** rather than a bare `name`. `name="name"` would also collide conceptually with the five profile fields' bare names, and continuity with the existing E2E selector is free.
3. **The rune counter is always shown.** UI-SPEC's within-20%-of-cap display rule is scoped to *byte counters* on the profile fields. The name is not byte-capped, it is the one required field with a hard 2–32 bound, and its counter belongs to the static rule line that always renders beneath it. A gated counter would also have made the plan's own stated behaviour ("a six-code-point string reports 6") unobservable.
4. **Two executor-authored strings**, both in UI-SPEC's register and both flagged for the next copy revision: `Enter a character name.` (no UI-SPEC row for a required-field message on this surface; `register/+page.svelte`'s `'Username is required.'` is the in-repo pattern, reworded to name the action) and the `h1` `Create a character` (no UI-SPEC row for the page heading; matched to the roster card's label so the link and its destination agree).
5. **The plan's partial-create copy is not rendered here.** `Created {name}. Some details didn't save — add them on the character page.` belongs to the roster's status region, which is plan 05-07. This plan ships the flag; 05-07 ships the sentence. Recorded so 05-07 does not re-author it and so the string lands in UI-SPEC once, not twice.

## Deviations from Plan

**None requiring a rule.** No bug, missing-critical-functionality, blocking issue or architectural change arose. The `rawMessage` finding is not a deviation — the plan says "verbatim" and `rawMessage` is what delivers that; it is recorded above because the plan does not name the property and the wrong one is the one a reader reaches for.

Two plan instructions were read narrowly rather than literally, both recorded as decisions above: the rune counter's display rule (decision 3) and the two authored strings (decision 4).

## Verification

All run inline in this worktree, judged by **exit code**:

| Gate | Exit | Result |
| --- | --- | --- |
| `task lint` | 0 | pass |
| `task build` | 0 | pass |
| `task test` | 0 | 11636 tests, 4 skipped (unchanged) |
| `task fmt` | 0 | **no mutations** — nothing to commit |
| `cd web && pnpm check` | 0 | 0 errors, 6 warnings (all pre-existing: PresenceList, CreateSceneForm) |
| `cd web && pnpm test:unit` | 0 | `Test Files 54 passed (54)` / `Tests 533 passed (533)` — from 52/509, this plan adds 24 |
| `cd web && pnpm build` | 0 | pass |

**Web tests are still not a CI gate** (issue #4964): nothing in `Taskfile.yaml` or `.github/workflows/ci.yaml` invokes vitest. Every run's own summary lines are transcribed into the task commit bodies as evidence rather than claimed, per the plan's acceptance criteria.

Every acceptance grep returns its stated result:

- `try {|catch` in `createFlow.ts`, comment lines filtered → lines **102 and 104 only**, the navigation guard; the create call is line 88, outside it
- `toast|sonner` under `web/src/lib/characters/` → **no match**
- `rg -o 'profile\.[a-z_]+' createFlow.ts | sort -u` → exactly `profile.age`, `profile.concept`, `profile.faction`, `profile.pronouns`, `profile.species` — five, no sixth
- `getMyCharacter|listMyCharacters|webListCharacters` in `createFlow.ts` → **no match**
- `rg -c 'name="'` on the form → **6**; `rg -c 'type="submit"'` → **1**
- `normalize|NFKC|casefold|toLowerCase|availability|debounce` on the form, comment lines filtered → **no match**
- `permanent|rename|forever` on the form → **no match**
- `ls 'web/src/routes/(authed)/characters/new/'` → `+page.svelte`, nothing else
- `$state`, `sendCommand|webCreateCharacter`, `@container` in the route → **no match**; the responsive rule is `@media (min-width: 768px)`

## Known Stubs

**None.** No placeholder component, disabled control, hardcoded value or "coming soon" copy ships. Every field is wired to the live RPC through the flow, and every branch of the error table is reachable and asserted.

**One declared cross-plan seam, which is not a stub.** `createdNotice` is written by this plan and **read by plan 05-07's roster** — the plan's own `key_links` says so. Until 05-07 lands, a successful create navigates to `/characters` and the confirmation is computed but never rendered, including the `profileIncomplete` flag. That is the same end state as before this plan for the *player*, with the mechanism now in place and tested on the producing side. It is a forward reference in exactly the sense 05-03's `/characters/new` link was, not a placeholder: the store's shape is final and its consumer is a named, scheduled plan. Flagged here so the phase reconciliation can confirm 05-07 actually consumes it.

## Deferred / out of scope

- **The eight Playwright specs remain broken.** This plan ships the replacement page they need; **plan 05-08 rewrites the specs**, per the dispatch instruction and the plan's own scope. The selector table above exists so 05-08 is not guessing. `deferred-items.md`'s entry stays **open** — its "Closed by: plan 05-06" line should be read as "the surface lands in 05-06, the specs are repointed in 05-08". `E2E Test` is a required check on `main`, so this still MUST close before the phase ships.
- **The human walkthrough** (Task 3's `<human-check>`) is pending and only fully answerable after 05-07, because its first assertion is about the roster's confirmation line.
- **The partial-create copy row is not folded into UI-SPEC.** The plan explicitly declines to edit UI-SPEC (its 28 consideration rows are a verified tally). Two further authored strings from this run — the required-name message and the page heading — join that list for the next copy revision.

## Threat Flags

**None.** No new network endpoint, auth path, file access pattern or schema change. The plan's own register is covered: the name pipeline is mirrored nowhere client-side (T-05-06-01, negative-grepped); the confusable message is rendered with nothing appended and the collision copy names no character (T-05-06-02, asserted as an absence); only `InvalidArgument` renders a server string and the generic spec asserts the rejection's message is absent (T-05-06-03); the confirmation crosses the navigation in a module value a URL cannot forge (T-05-06-04); a post-create navigation failure never reports failure (T-05-06-05); `sendCommand` appears in neither the form nor the route (T-05-06-06). T-05-06-07 remains `accept`ed — no client-side rate limit was added, and the facade's per-player limit is the control.

Two rows are worth marking as *actively* verified rather than planned: **T-05-06-02** and **T-05-06-03** are each pinned by an assertion on the **absence** of the leaked string, not merely the presence of the authored line — a presence-only assertion passes with the leak sitting beside it.

## Tooling note

`requirements mark-complete` reported `table_unmatched` again (as in 05-01 through 05-05) — the checkboxes tick but the traceability table does not update. Not hand-patched, per the dispatch instruction; for the phase-end reconciliation. Tracked in the #4963/#4964 family.

## Next Phase Readiness

- **05-07** has everything it needs: `takeCreatedNotice()` returns `{ name, characterId, profileIncomplete }` or `null`, clearing on read. It must render the base row `Created {name}.` and, when `profileIncomplete`, the authored variant `Created {name}. Some details didn't save — add them on the character page.` with `the character page` linking to `/characters/{characterId}`.
- **05-08** has the selector table above and the three behavioural changes the fixture rewrite must absorb.

## Self-Check: PASSED

| Claim | Result |
| --- | --- |
| `web/src/lib/characters/createFlow.ts` | FOUND |
| `web/src/lib/characters/createFlow.test.ts` | FOUND |
| `web/src/lib/characters/createdNotice.ts` | FOUND |
| `web/src/lib/components/characters/CreateCharacterForm.svelte` | FOUND |
| `web/src/lib/components/characters/CreateCharacterForm.svelte.test.ts` | FOUND |
| `web/src/routes/(authed)/characters/new/+page.svelte` | FOUND |
| `5e53dd30e` `fecb3b67f` `bcc4364ad` `be405a9d3` `2af8484be` | all FOUND |
| deletions across the five commits | **none** (`git diff --diff-filter=D` empty) |

---
*Phase: 05-character-identity-ui-public-profiles*
*Completed: 2026-08-12*
