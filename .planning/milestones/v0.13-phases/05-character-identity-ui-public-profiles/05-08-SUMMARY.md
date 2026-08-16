---
phase: 05-character-identity-ui-public-profiles
plan: 08
subsystem: testing
tags: [playwright, e2e, docker, privacy, anonymous-access, unicode, accessibility]

# Dependency graph
requires:
  - phase: 05-character-identity-ui-public-profiles
    plan: 02
    provides: "/c/[id] and PublicProfile.svelte — the route and body the anonymous read exercises"
  - phase: 05-character-identity-ui-public-profiles
    plan: 04
    provides: "/characters/[id] — the authoring surface the setup writes pronouns and description through, and the PROFILE-12 notice's one home"
  - phase: 05-character-identity-ui-public-profiles
    plan: 06
    provides: "/characters/new — the six-field card, and the preserved input[name=\"characterName\"] selector"
  - phase: 05-character-identity-ui-public-profiles
    plan: 07
    provides: "the sectioned roster, its badge matrix, the collapse chip and the create-confirmation status region"
provides:
  - "web/e2e/public-profile.spec.ts — the first cookie-less visit to an application path this suite has ever made"
  - "web/e2e/characters-create.spec.ts — the structured creation journey, the server-name fold and the rejection path"
  - "web/e2e/characters-roster.spec.ts — sectioning, badge suppression, the collapse chip, the default change and the PROFILE-12 scope"
  - "web/e2e/helpers/fixtures.ts registerPlayer / createCharacter / enterGameAs — the creation journey spelled out ONCE"
  - "A green E2E Test lane: the phase's exit condition"
affects: []

actuals:
  tokens: 71000
  tasks: 4
  commits: 5

tech-stack:
  added: []
  patterns:
    - "A privacy-parity assertion that compares two CAPTURED texts rather than one captured text against a hardcoded string — a hardcoded expectation passes while the two pages diverge from each other, which is the only thing the property forbids"
    - "A positive control beside a parity assertion, so 'the two failures agree' cannot be satisfied by 'everything agrees'"
    - "A shared journey helper in place of N private copies, adopted at the moment the N copies proved to be the blast radius"

key-files:
  created:
    - web/e2e/public-profile.spec.ts
    - web/e2e/characters-create.spec.ts
    - web/e2e/characters-roster.spec.ts
  modified:
    - web/e2e/helpers/fixtures.ts
    - web/e2e/auth.spec.ts
    - web/e2e/admin.spec.ts
    - web/e2e/session-security.spec.ts
    - web/e2e/negative-journeys.spec.ts
    - web/e2e/character-switcher.spec.ts

key-decisions:
  - "THE PLAN'S NOT-FOUND PARITY PAIR IS UNCONSTRUCTIBLE AND A DIFFERENT PAIR SHIPPED. The plan asks for [a well-formed ULID naming no character] vs [a character the anonymous rung cannot reach]. No such character exists: `seed:profile-reachable` (internal/access/policy/seed.go:710) and `seed:viewer-character-description-read` (:951) BOTH clear `anonymous|guest|player`, so every character in the seeded corpus is reachable at the anonymous rung, and the plan forbids mutating policy from a browser test. The pair shipped is [nonexistent ULID] vs [malformed identifier] — a genuine divergence risk, because the malformed branch returns from GetCharacterProfile BEFORE viewer resolution and is therefore a different code path, not a different input to the same one."
  - "A THIRD TEST WAS ADDED THAT THE PLAN DID NOT ASK FOR: a positive control asserting a populated profile renders DIFFERENTLY from the not-found page. Without it the parity assertion is satisfied by a regression that collapses every /c/<id> render to the same page — `expect(a).toBe(b)` cannot tell 'both are the not-found page' from 'both are blank'."
  - "The eight broken specs were repointed by EXTRACTING the creation journey into fixtures.ts rather than by editing six private copies in place. Six copies of one journey is precisely why deleting one component in plan 05-03 broke eight files; repointing them in place would have rebuilt the same fragility."
  - "characters-roster.spec.ts asserts NO roster ordering. `charRepo.ListByPlayer` still declares no ORDER BY, so a stable-order assertion would pin an implementation accident of a small heap scan and go red on the first plan change with no defect behind it."
  - "negative-journeys.spec.ts's name-collision assertion moved off the server's `already taken` string and onto the authored `That name is taken. Try another.`, WITH a negative check that the server's wording is absent. /characters/new classifies AlreadyExists by CODE and never renders the server's sentence; asserting the old string would have re-asserted a leak the phase deliberately closed."

patterns-established:
  - "The one RED of the run was found by the test, not by review: `innerText` reports text AFTER `text-transform`, so the section headings arrive as `PLAYABLE` while `getByRole('heading', { name: 'Not playable' })` — which reads DOM text — sees the authored casing. The two assertions in that test now deliberately spell the same heading differently, with the reason stated."
  - "Every absence assertion in this plan is paired with the presence assertion it would otherwise be mistaken for: ASCII-fold present AND full-width absent; Retired badge present AND session word absent; not-found pages equal AND a real profile unequal."

requirements-completed: [IDENT-01, IDENT-05, PROFILE-01, PROFILE-12]

coverage:
  - id: D1
    description: "A browser context carrying NO session cookie loads /c/<a real character ULID> and sees the name, the pronouns and the in-world description"
    requirement: PROFILE-01
    verification:
      - kind: e2e
        ref: "public-profile.spec.ts#a browser carrying no session cookie reads a character name, pronouns and description at /c/[id]"
        status: pass
      - kind: e2e
        ref: "the same test asserts `anon.cookies()` has length 0 BEFORE the read, so a leaked cookie fails there rather than silently converting this into an authenticated test"
        status: pass
    human_judgment: false
  - id: D2
    description: "Two opaque /c/[id] outcomes render an identical page that names neither the identifier nor a reason"
    requirement: PROFILE-01
    verification:
      - kind: e2e
        ref: "public-profile.spec.ts#two opaque /c/[id] failures render identical pages that name neither the identifier nor a reason — captured-vs-captured equality, plus twelve reason-word negative assertions"
        status: pass
      - kind: e2e
        ref: "public-profile.spec.ts#the not-found page is distinguishable from a real profile, so the parity check is not vacuous"
        status: pass
      - kind: other
        ref: "PARTIAL against the plan's wording — the [unreachable character] arm is unconstructible under the shipped seed set; see the decision above and WINDOWS #21"
        status: partial
    human_judgment: true
    rationale: "The §8.7 property has two arms — 'nonexistent looks like unreachable' and 'malformed looks like nonexistent'. The second is proven end to end. The first cannot be exercised from a browser in v0.13 because no seeded character sits below the anonymous floor, and manufacturing one would mean editing the policy seed set from a test. The arm that IS proven is the one with a distinct code path behind it; the unproven arm is two inputs to the same `Reachable`-denied return, and plan 05-05's integration specs assert over that return directly."
  - id: D3
    description: "A signed-in player creates a character through the six-field card at /characters/new and lands on /characters with it present"
    requirement: IDENT-01
    verification:
      - kind: e2e
        ref: "characters-create.spec.ts#the roster create affordance is a link to /characters/new, not an inline input — asserts href AND that no name input exists on the roster"
        status: pass
      - kind: e2e
        ref: "characters-create.spec.ts#a full-width name is created and the confirmation names the folded ASCII form the server stored"
        status: pass
    human_judgment: false
  - id: D4
    description: "The creation confirmation names the SERVER-returned display name, which for a full-width-Latin submission differs from what was typed"
    requirement: IDENT-01
    verification:
      - kind: e2e
        ref: "characters-create.spec.ts — the confirmation is asserted to CONTAIN the ASCII fold and NOT CONTAIN the full-width submission; the roster card is asserted the same way in both directions"
        status: pass
    human_judgment: false
  - id: D5
    description: "A taken name re-renders the form with all six values intact and shows the authored taken copy"
    requirement: IDENT-01
    verification:
      - kind: e2e
        ref: "characters-create.spec.ts#a taken name is refused with the authored copy and costs the player none of the six values — six toHaveValue assertions, one per input, read by name attribute"
        status: pass
      - kind: e2e
        ref: "the same test asserts the message names neither the typed nor the stored form, and that the URL is still /characters/new"
        status: pass
    human_judgment: false
  - id: D6
    description: "The roster renders Playable first with the create link, and Not playable second whose count chip collapses it"
    requirement: IDENT-05
    verification:
      - kind: e2e
        ref: "characters-roster.spec.ts#a retired character sits in Not playable… — document-order index comparison by exact heading match, plus the create link asserted absent from the not-playable grid"
        status: pass
      - kind: e2e
        ref: "characters-roster.spec.ts#the Not playable chip starts expanded and collapsing REMOVES its grid from the markup — aria-expanded before/after, and toHaveCount(0) rather than a class check"
        status: pass
    human_judgment: false
  - id: D7
    description: "A retired character's card shows the retired badge and no session word"
    requirement: IDENT-05
    verification:
      - kind: e2e
        ref: "characters-roster.spec.ts — lifecycle-badge is `Retired`, session-badge has count 0, AND the card's innerText is asserted not to match /\\b(active|offline)\\b/i"
        status: pass
    human_judgment: false
  - id: D8
    description: "Setting a different character as default moves the Default badge and shows the authored confirmation"
    requirement: IDENT-05
    verification:
      - kind: e2e
        ref: "characters-roster.spec.ts#setting a different character as default moves the badge and announces it without a reload — no page.reload() between the click and the assertions; badge count asserted to be exactly 1 afterwards"
        status: pass
    human_judgment: false
  - id: D9
    description: "The not-retroactive statement is present on /characters/[id] and absent from every other surface in the phase"
    requirement: PROFILE-12
    verification:
      - kind: e2e
        ref: "characters-roster.spec.ts#the not-retroactive statement is on /characters/[id] and on no other surface — present there, absent from /characters, absent from a cookie-less /c/[id]"
        status: pass
    human_judgment: false
  - id: D10
    description: "The eight Playwright specs broken by plan 05-03 are repointed and passing; E2E Test is green"
    requirement: IDENT-01
    verification:
      - kind: e2e
        ref: "task test:e2e (whole suite, unscoped) — exit 0, 115 passed, 0 failed, 1 skipped, 116 total"
        status: pass
      - kind: other
        ref: "rg for `Create New Character`, `role=\"checkbox\"`, `Choose Your Character`, `has-text(\"Create\")` across web/e2e/ returns one COMMENT and no code"
        status: pass
    human_judgment: false

duration: 58min
completed: 2026-08-12
status: complete
---

# Phase 05 Plan 08: the three user journeys through a real browser Summary

**`E2E Test` is green again — the phase's exit condition, closed by repointing the eight specs plan 05-03 knowingly broke — and a browser carrying no cookie at all now reads a public profile, which is the first time this application has served an authenticated-app path to an anonymous visitor and been able to prove it.**

## Performance

- **Duration:** 58 min
- **Tasks:** 4 of 3 planned (one added — see Deviations)
- **Commits:** 4 task commits + this one
- **Files created:** 3, modified 6

## Task Commits

| Task | Commit | What landed |
| --- | --- | --- |
| 0 (added) | `96cc96aa1` | the eight-spec repoint + the `registerPlayer` / `createCharacter` / `enterGameAs` split |
| 1 | `689e2708a` | `public-profile.spec.ts` — the cookie-less read, the parity pair, the positive control |
| 2 | `6a4ab2390` | `characters-create.spec.ts` — the link, the rule line, the fold, the rejection |
| 3 | `24d93c701` | `characters-roster.spec.ts` — sections, suppression, chip, default, PROFILE-12 scope |

## The exit condition, closed

`E2E Test` is a required check protecting `main` and it was **deliberately red**: plan 05-03 deleted the roster's inline create form, and six files — one of them the shared fixture — still drove it. This plan closed that first, before writing anything new, because the three new specs are built on the same fixture and would have failed for the same reason.

**It was repointed by extraction, not by editing six copies in place.** `registerAndEnterTerminal` is now `registerPlayer` + `createCharacter` + `enterGameAs`, and the five spec-local copies call those. That is the actual finding of the repoint: *six private copies of one journey is what turned a single component deletion into an eight-file breakage*, and fixing them in place would have rebuilt the fragility at the same size. The next time the creation flow changes, one function changes.

Three behavioural changes were absorbed, all named in advance by the 05-06 and 05-07 selector tables:

| Change | How it was absorbed |
| --- | --- |
| Creation lands on `/characters`, not `/terminal` | every caller gained an explicit `enterGameAs` step — entering the game is now a second act |
| The auto-enter checkbox is gone | `button[role="checkbox"]` deleted from all five copies |
| The roster `h1` is `Your characters` | `character-switcher.spec.ts:65` repointed |

`input[name="characterName"]` was **deliberately preserved by 05-06**, and that decision paid: not one field selector changed across the whole repoint.

**One assertion changed meaning rather than merely moving.** `negative-journeys.spec.ts` asserted the server's own `already taken` string. `/characters/new` classifies `AlreadyExists` by **code** and renders authored copy, never the server's sentence — so re-asserting the old string would have re-asserted a leak the phase closed on purpose. It now asserts `That name is taken. Try another.` **and** that the server's wording is absent.

**Nothing was quarantined.** `rg -n 'quarantinetest|@quarantine'` over the three new specs returns no match, and no row was added to `test/quarantine.yaml`. Quarantine is for flakiness with an open issue; every failure encountered here had a known, deterministic cause and was fixed.

## The finding of the run: the plan's parity pair does not exist

Task 1 asks for a not-found parity comparison between **a well-formed ULID naming no character** and **a character whose profile the anonymous rung cannot reach**, built "from the seeded corpus rather than by mutating policy from a browser test."

**There is no such character.** Both gates on the anonymous read clear every rung:

```
internal/access/policy/seed.go:710   seed:profile-reachable
  permit(principal is viewer, action in ["read"], resource is profile)
  when { principal.viewer.tier in ["anonymous", "guest", "player"] };

internal/access/policy/seed.go:951   seed:viewer-character-description-read
  permit(principal is viewer, action in ["read_description"], resource is character)
  when { principal.viewer.tier in ["anonymous", "guest", "player"] };
```

v0.13 ships the reachability floor **at anonymous** (§8.4.2, §7.4), by design — the clearing list is the whole configuration surface, and raising it is what a *game* does, not what a browser test does. So the corpus contains no below-floor character, and the plan's own prohibition rules out manufacturing one.

**What shipped instead is the other arm of the same §8.7 property**, and it is the arm with a real divergence risk behind it: **[a well-formed ULID naming no character]** vs **[a malformed identifier]**. `GetCharacterProfile` returns for a malformed id *before* viewer resolution runs (`characteraccess_service.go:427`) — a genuinely different code path that could drift apart from the ordinary miss. The unreachable arm, by contrast, is a second input to the *same* `Reachable`-denied return.

The comparison is **captured-vs-captured**: both pages' visible text is read and compared for equality, because a hardcoded expected string passes while the two pages diverge from each other, which is the only thing the property actually forbids.

**A third test was added that the plan did not ask for.** `expect(a).toBe(b)` cannot distinguish "both are the not-found page" from "both are blank" or from "every `/c/<id>` render collapsed to one page". The positive control asserts a populated profile renders **differently** from the not-found page and contains the character's name while the not-found page does not — without it, the parity assertion is satisfied by a regression far worse than the one it guards.

## The one RED, and what found it

Every other failure in this run was a fixture error I introduced. One was a real property of the page that reading the source did not reveal:

```
Expected: >= 0
Received:    -1
```

`headings.allInnerTexts()` returned `PLAYABLE` and `NOT PLAYABLE`. `CharacterRoster.svelte`'s `.section-heading` carries `text-transform: uppercase`, and **`innerText` reports rendered text**, while `getByRole('heading', { name: 'Not playable' })` — two lines earlier in the same test, and passing — reads the **DOM** text, which is not transformed. The two assertions now deliberately spell the same heading differently, with the reason stated in the file, because a future reader will otherwise "fix" the inconsistency.

The other failure was mine and worth recording because it will bite the next author: **`MaxUsernameLength = 30`** (`internal/auth/player.go:25`), and `uniqueSceneUser` spends 25 characters on scaffolding — so a registration prefix longer than **five characters** silently overflows and registration fails with a 401 and no visible message. Ten of eleven tests failed on it at once.

## Every absence assertion is paired

The phase's privacy and vocabulary properties are all *absences*, and a presence-only assertion passes with the leak sitting beside it. Each one here is paired with the presence assertion it would otherwise be mistaken for:

| Property | Presence | Absence (the load-bearing half) |
| --- | --- | --- |
| The echo reads the server, not the form | confirmation contains the ASCII fold | confirmation does **not** contain the full-width submission |
| Session suppression on a retired card | `lifecycle-badge` is `Retired` | card text matches no `/\b(active\|offline)\b/i`, and `session-badge` count is 0 |
| Opaque not-found | both pages render `Not found` | the two texts are **equal**, name neither identifier, and match none of twelve reason words |
| The parity is not vacuous | — | a real profile's text is **unequal** to the not-found text |
| The taken copy names no character | authored line matches exactly | names neither the typed nor the stored form |
| PROFILE-12 scope | present on `/characters/[id]` | absent from `/characters` **and** from a cookie-less `/c/[id]` |

## What this file explicitly does NOT prove

`public-profile.spec.ts`'s header says so in terms, so a later reader does not mistake it for the privacy proof: **it does not discharge PORTAL-10 rule 3** (01-SPEC §12.1 rule 3), which requires assertions against **marshaled response bytes** and states that a Playwright DOM assertion does not satisfy it. A field absent from the rendered page may still have been on the wire. That obligation belongs to plan 05-05's integration specs, which are named in the header.

The plan's `<verification>` note that **no denial tests ship here** holds as written: every spec drives a permitted journey plus the uniform not-found render, so PORTAL-10 rule 2's paired-positive-control obligation is not-applicable rather than omitted.

## Deviations from Plan

### 1. [Rule 3 — blocking issue] A task the plan does not contain was executed first

- **Found during:** planning the run, before Task 1.
- **Issue:** the plan's three specs all build on `web/e2e/helpers/fixtures.ts`, which was itself one of the eight broken files. Writing them first would have produced three new specs failing for a reason that had nothing to do with them, on top of an already-red required check.
- **Fix:** Task 0 — repoint all six files, extracting the journey into three helpers.
- **Files modified:** `fixtures.ts`, `auth.spec.ts`, `admin.spec.ts`, `session-security.spec.ts`, `negative-journeys.spec.ts`, `character-switcher.spec.ts`.
- **Commit:** `96cc96aa1`.

### 2. [Rule 1 — plan asks for an unconstructible fixture] The not-found parity pair

- **Found during:** Task 1, reading `internal/access/policy/seed.go` for the unreachable case.
- **Issue:** no character in the seeded corpus is below the anonymous floor (citations above), and the plan forbids mutating policy from a browser test.
- **Fix:** shipped [nonexistent ULID] vs [malformed id], the arm with a distinct code path, plus a positive control the plan did not ask for.
- **Commit:** `689e2708a`. Recorded in `WINDOWS.md` #21 so the uncovered arm is visible at ship time.

### 3. [Judgement] `git status --porcelain web/e2e/helpers/` — read per task, not per plan

Each of Tasks 1–3 has an acceptance criterion that this is empty. It is, at each of those tasks: Task 0 committed the helper change before Task 1 began, and Tasks 1–3 added no helper. The criterion's substance — *the three new specs needed no new database lookup* — holds exactly: they use only the three shipped lookups the plan named.

### 4. [Judgement] `rg -c 'browser.newContext'` and `rg -n 'storageState'`

`browser.newContext` matches **4** times (≥ 1 as required), and `storageState` returns no match — no signed-in state is reused for any anonymous context. The three anonymous reads each run against a page from a fresh context, never the fixture's `page`.

## Verification

All run inline in this worktree, judged by **exit code**, never by matching a string in the output:

| Gate | Exit | Result |
| --- | --- | --- |
| `task test:e2e` (whole suite, **unscoped**) | **0** | **115 passed, 0 failed, 1 skipped, 116 total** |
| `task test:e2e -- <the three new specs>` | 0 | 11 passed |
| `task test:e2e -- auth + character-switcher + negative-journeys` | 0 | 24 passed (the repoint, before the new specs existed) |
| `task lint` | 0 | pass |
| `task build` | 0 | pass |
| `task test` | 0 | 11636 tests, 4 skipped (unchanged) |
| `task fmt` | 0 | **no mutations** — nothing to commit |
| `cd web && pnpm check` | 0 | 0 errors, 6 warnings (all pre-existing: CreateSceneForm, PresenceList) |
| `cd web && pnpm test:unit` | 0 | `Test Files 56 passed (56)` / `Tests 557 passed (557)` — unchanged; this plan adds no unit test |

The 1 skipped E2E spec is pre-existing and unrelated (quarantine `grepInvert`), not anything this plan touched.

Acceptance greps:

- `rg -n 'quarantinetest\|@quarantine'` over the three new specs → **no match**
- `rg -n 'Create New Character\|role="checkbox"\|Choose Your Character'` over `web/e2e/` → **one comment, no code**
- `rg -n 'getCharacterByName\|getCharactersByPlayerId'` in `public-profile.spec.ts` → **matches** (`getCharacterByName`)
- `rg -n 'name='` in `characters-create.spec.ts` → the six field selectors, driven by `name` attributes
- `git status --porcelain web/e2e/helpers/` → **empty**

## Known Stubs

**None.** No placeholder spec, no `test.skip`, no `test.fixme`, no hardcoded expectation standing in for a real assertion. Every test drives the real Docker stack through a real browser.

## Deferred / out of scope

- **The unreachable-vs-nonexistent parity arm has no E2E coverage** and cannot have any until a game raises the reachability floor above anonymous. `WINDOWS.md` #21.
- **`charRepo.ListByPlayer` still has no `ORDER BY`.** Roster ordering is not provably deterministic, so this plan asserts none — every roster assertion is order-independent. The fix is one server-side `ORDER BY`, outside this plan's files. `WINDOWS.md` #22.
- **Two human walkthroughs remain unrun** (`WINDOWS.md` #19, #20 — 05-06's create walkthrough and 05-07's roster walkthrough). This plan proves the *mechanics* of both journeys end to end, which narrows what the walkthroughs are for: they are now judgement calls about wording, spacing and whether the `Not playable` section *reads* as the player's own characters rather than as something withheld. Task 3's own `<human-check>` is the same question and is likewise pending; it is a design judgement no assertion can make.
- **`requirements mark-complete` reported `updated: false`** with empty `marked_complete` / `not_found` for all four IDs — the same `table_unmatched` family seen in 05-01 through 05-07. Not hand-patched, per the dispatch; for the phase-end reconciliation (#4963/#4964).
- **Web tests are still not a CI gate** (#4964). `E2E Test` **is**, and it is now green.

## Threat Flags

**None.** No new network endpoint, auth path, file access pattern or schema change; this plan ships test files only. Its own register is discharged:

- **T-05-08-01** (a public page that quietly requires a session) — every anonymous read runs against `browser.newContext()`, and the context is asserted to hold **zero cookies** before the read. That assertion is what makes the reuse failure loud instead of silent.
- **T-05-08-02** (distinguishable not-found causes) — captured-vs-captured equality, plus the positive control that stops the comparison being vacuous.
- **T-05-08-03** (the echoed name leaking the raw input) — the confirmation is asserted **not** to contain the full-width submission; an echo reading local state fails.
- **T-05-08-04** (presence telemetry on a retired character) — the retired card's text is asserted to match no session word, with session data reaching the roster normally.
- **T-05-08-05** (a green suite bought by quarantine) — negative-grepped in all three specs; `test/quarantine.yaml` unchanged.
- **T-05-08-06** (E2E lane contention) — `accept`ed as planned. Every run's verdict was read from the exit code; the task's already-running refusal never fired.

## Next Phase Readiness

- **The phase is executable-complete.** `E2E Test` is green with nothing quarantined, and `deferred-items.md`'s Playwright entry is closed with the verification recorded in it.
- **The phase-end reconciliation** should confirm (a) the requirements traceability table across all eight plans, which no plan's `mark-complete` has updated, and (b) the three pending human judgements — 05-06's, 05-07's and this plan's — which are now one sitting: create a character with a full-width name, land on the roster, retire one by hand, and read the page.
- **Two server-side follow-ups** are named and unowned: the `ORDER BY` on `ListByPlayer`, and the fact that raising the profile reachability floor above anonymous would, for the first time, make the plan's original parity pair constructible.

## Self-Check: PASSED

| Claim | Result |
| --- | --- |
| `web/e2e/public-profile.spec.ts` | FOUND |
| `web/e2e/characters-create.spec.ts` | FOUND |
| `web/e2e/characters-roster.spec.ts` | FOUND |
| `96cc96aa1` `689e2708a` `6a4ab2390` `24d93c701` | all FOUND |
| deletions across the four commits | **none** (`git diff --diff-filter=D` empty) |
| quarantine markers in the three new specs | **none** |
| `git status --porcelain web/e2e/helpers/` | empty |
| `task test:e2e` unscoped | exit **0**, 115/115 passed |

---
*Phase: 05-character-identity-ui-public-profiles*
*Completed: 2026-08-12*
