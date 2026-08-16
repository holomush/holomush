---
phase: 05-character-identity-ui-public-profiles
plan: 02
subsystem: web
tags: [sveltekit, svelte5, vitest, connectrpc, accessibility, privacy, adapter-static]

requires:
  - phase: 05-character-identity-ui-public-profiles
    plan: 01
    provides: web/src/lib/characters/client.ts and its getCharacterProfile wrapper
  - phase: 04-shared-facade-helpers-characteraccessservice
    provides: CharacterAccessServer.GetCharacterProfile, projectPublic, and the anonymous-rung degradation in resolveViewerIdentity
provides:
  - "web/src/routes/c/[id]/+page.svelte — the first route the app serves outside (authed)"
  - "web/src/lib/components/characters/PublicProfile.svelte — the props-only read-only renderer whose correctness property is absence"
  - "web/src/lib/components/characters/CharacterPortrait.svelte — the single tinted initial-letter plate (80px profile / 44px roster)"
  - "web/src/lib/components/characters/ProfileMedia.svelte — the media renderer no v0.13 data can reach"
  - "isNotFoundError, isAbortedError, isAlreadyExistsError, isInvalidArgumentError in web/src/lib/connect/errors.ts — the whole classification set this phase needs"
affects: [05-04, 05-06, 05-08]

actuals:
  tokens: 10600
  tasks: 3
  commits: 5

tech-stack:
  added: []
  patterns:
    - "A privacy-critical read-only renderer shares NO component with the authoring form that writes the same names — only the portrait primitive is shared"
    - "A route splits into a shell (route param + fetch + three-way branch, reads $app/stores) and a props-only body (directly mountable under vitest with the repo's mount/unmount idiom)"
    - "A set of sibling ConnectError code predicates is specified by a cross-product table rather than N independent blocks, so a copy-paste that leaves two predicates on one code fails"
    - "Svelte 5 block anchors (<!---->) are stripped before an exact markup comparison, so two renderings of the same element compare on markup rather than on block bookkeeping"

key-files:
  created:
    - web/src/routes/c/[id]/+page.svelte
    - web/src/lib/components/characters/PublicProfile.svelte
    - web/src/lib/components/characters/PublicProfile.svelte.test.ts
    - web/src/lib/components/characters/CharacterPortrait.svelte
    - web/src/lib/components/characters/ProfileMedia.svelte
    - web/src/lib/components/characters/ProfileMedia.svelte.test.ts
  modified:
    - web/src/lib/connect/errors.ts
    - web/src/lib/connect/errors.test.ts

key-decisions:
  - "The public profile and the owner authoring surface share NO presentational component. They render the same twelve names but answer different questions — this one's correctness property is absence, /characters/[id]'s is per-section save — and coupling a privacy-critical renderer to a form is how an improvement to one silently changes the other. CharacterPortrait is the single deliberate exception, because UI-SPEC unifies the portrait treatment and one component is what makes 'one treatment' true rather than asserted."
  - "The route adds no +page.ts. The root layout already declares ssr = false and adapter-static runs with fallback: 'index.html', so every path answers HTTP 200 with the SPA shell and route-level indistinguishability (§8.7) is structural. A load() would only add a second place the not-found decision could be made."
  - "PublicProfile reads each profile key by name and by hand (`'profile.species' in p`). The verbosity is the point: a list of expected names iterated in a loop is the client-side allowlist §8.9 forbids, and it is what turns a field the floor kept back into a visible omission."
  - "ProfileMedia renders nothing at zero rows and PublicProfile calls it twice — once for the portrait slot, once for the gallery — rather than owning a variant flag. Each call is a no-op on its empty half, so the zero-media case has no code path that could grow a placeholder."
  - "The content-warning reveal control's accessible name is the warning text itself (UI-SPEC), with aria-expanded carrying the show/hide semantics. A generic verb would displace the one string a viewer needs in order to decide whether to look."
  - "A media id derives its fetch path at exactly one place (mediaSrc), same-origin, which is what the prerendered SPA's `img-src 'self' data:` policy admits. No serving endpoint exists in v0.13; this is the one line to change when one lands."

patterns-established:
  - "Full-textContent assertion for an absence contract: the minimal-profile spec compares the WHOLE normalised text, because a substring assertion passes with a stray count, lock glyph or heading-without-body sitting beside it"
  - "Casing enforced by test, not by grep: the mount assertion compares textContent to the source name's first character UNCHANGED, so a toUpperCase() in script fails while a CSS text-transform passes — a grep for toUpperCase would have matched the doc comment explaining the rule"
  - "Each load-bearing assertion is shown RED against a plausible WRONG implementation (targeted neuter + immediate restore), not merely observed green"

requirements-completed: [PROFILE-01, PROFILE-02, PROFILE-06, PROFILE-07, PROFILE-08, PROFILE-09, PROFILE-10a, EXT-08]

coverage:
  - id: D1
    description: "A visitor with no session cookie loads /c/<characterULID> and sees the character's name, pronouns and in-world description rendered from the anonymous rung's response"
    requirement: PROFILE-01
    verification:
      - kind: unit
        ref: "web/src/lib/components/characters/PublicProfile.svelte.test.ts — the identity card, description and fact-pill specs"
        status: pass
      - kind: manual
        ref: "load /c/<a real character ULID> in a browser profile with no cookie"
        status: pending
    human_judgment: true
    rationale: "The renderer and the route's three states are asserted, and the route sends no session handling at all (grep-verified). That a live anonymous browser reaches the page is an end-to-end judgment no vitest mount can make: the route component reads $app/stores and the anonymous degradation happens in the facade."
  - id: D2
    description: "A profile carrying only name and pronouns renders the identity card and stops — no count, lock, greyed section, dashed placeholder, progress indicator, heading-without-body or empty-state copy of any kind"
    requirement: PROFILE-01
    verification:
      - kind: unit
        ref: "PublicProfile.svelte.test.ts#renders a name-and-pronouns profile as a complete card and adds not one other word — asserts the FULL normalised textContent"
        status: pass
    human_judgment: false
  - id: D3
    description: "The client renders exactly the keys present in the response's profile map and holds no list of expected fields to diff against"
    requirement: PROFILE-01
    verification:
      - kind: unit
        ref: "PublicProfile.svelte.test.ts#renders nothing for a profile key it does not lay out, and does not crash"
        status: pass
      - kind: unit
        ref: "acceptance grep — Object.keys / Object.entries / #each over profile: no match in PublicProfile.svelte"
        status: pass
    human_judgment: false
  - id: D4
    description: "A nonexistent character and one below the viewer's reachability floor render the identical page, because the client branches on exactly one condition — gRPC NotFound"
    requirement: PROFILE-01
    verification:
      - kind: unit
        ref: "web/src/lib/connect/errors.test.ts#the phase 5 ConnectError classifiers — cross-product table, one code each"
        status: pass
      - kind: unit
        ref: "acceptance greps on the route — one isNotFoundError call site, no Code. reference, no reason vocabulary, no id interpolation in the not-found markup"
        status: pass
      - kind: manual
        ref: "load /c/01ARZ3NDEKTSV4RRFFQ69G5FAV and /c/<a character below the anonymous floor>; confirm the rendered page is the same"
        status: pending
    human_judgment: true
    rationale: "The single branch and the absent reason/path echo are mechanically verified. That the two causes are visually identical in a browser is the property the plan asks a human to confirm, and it is the one that would catch a difference introduced outside this component (a differing HTTP status, a layout shift, a console message)."
  - id: D5
    description: "profile.rumors, profile.currently, profile.rp_preferences and profile.timezone each render when present and render nothing when absent, with rp_preferences LAST under an out-of-character heading"
    requirement: PROFILE-06
    verification:
      - kind: unit
        ref: "PublicProfile.svelte.test.ts#renders present fields and no heading at all for an absent one"
        status: pass
      - kind: unit
        ref: "PublicProfile.svelte.test.ts#orders the five long-form sections with out-of-character last"
        status: pass
    human_judgment: false
  - id: D6
    description: "An empty characters.description is omitted from the response and renders nothing; a present one renders as the bytes the response carried, pre-wrap, untruncated"
    requirement: PROFILE-10a
    verification:
      - kind: unit
        ref: "PublicProfile.svelte.test.ts#renders no description element when the description is empty"
        status: pass
      - kind: unit
        ref: "PublicProfile.svelte.test.ts#renders the description exactly as the response carried it, wrapping rather than truncating"
        status: pass
    human_judgment: false
  - id: D7
    description: "The sheet renders as a named empty section and the web-DM slot as a non-interactive labelled slot — never a button, never a disabled button, never a dead link — identical for every viewer"
    requirement: EXT-08
    verification:
      - kind: unit
        ref: "PublicProfile.svelte.test.ts#renders the sheet as a named empty section for every input, minimal included"
        status: pass
      - kind: unit
        ref: "PublicProfile.svelte.test.ts#renders the web-DM slot as a non-interactive labelled slot — never a control, never a dead link"
        status: pass
    human_judgment: false
  - id: D8
    description: "Zero media rows render no gallery section, heading or slot; the portrait falls back to the initial letter, uppercased in CSS and aria-hidden"
    requirement: PROFILE-01
    verification:
      - kind: unit
        ref: "ProfileMedia.svelte.test.ts#renders no element at all when there is neither a primary image nor a gallery row"
        status: pass
      - kind: unit
        ref: "ProfileMedia.svelte.test.ts#CharacterPortrait — one glyph, source name unmutated, aria-hidden"
        status: pass
    human_judgment: false
  - id: D9
    description: "The media renderer replaces the portrait with the primary image, maps alt_text to alt, blurs a non-empty content_warning behind a reveal control, renders nothing at zero rows, and falls back to the identical initial-letter placeholder when an img fails to load"
    verification:
      - kind: unit
        ref: "ProfileMedia.svelte.test.ts — seven specs; the UI-SPEC backstop row, unreachable by any v0.13 data"
        status: pass
    human_judgment: false
  - id: D10
    description: "The loading state is one centered role=status line with no skeleton, and any failure that is not NotFound renders the generic copy with a retry control"
    verification:
      - kind: manual
        ref: "the route's three render branches, read at web/src/routes/c/[id]/+page.svelte"
        status: pending
    human_judgment: true
    rationale: "The route component reads $app/stores, which a vitest mount cannot supply without a module mock — the reason the plan split the body out as props-only in the first place. No Playwright spec covers /c/[id] in this plan."

duration: 15min
completed: 2026-08-12
status: complete
---

# Phase 05 Plan 02: The Public Profile Page — Summary

**A logged-out visitor can now read a character's profile at `/c/<id>`, the first route this app has ever served outside `(authed)`, rendering exactly the keys the response carried — and a profile carrying only a name and pronouns renders as a complete card with not one other word on it.**

## Performance

- **Duration:** 15 min
- **Started:** 2026-08-12T20:46:42Z
- **Completed:** 2026-08-12T21:01:52Z
- **Tasks:** 3
- **Commits:** 5 (two RED/GREEN pairs plus the route)
- **Files:** 8 (6 created, 2 modified)

## Accomplishments

- **The absence contract is enforced by a component whose sparse rendering is asserted on its full text content.** The minimal-profile spec compares the WHOLE normalised `textContent` to `B Bazian she/her Direct messages Not available yet. Sheet No sheet system yet.` — a substring assertion would have passed with a count, a lock glyph or a bare heading sitting beside it.
- **`/c/[id]` branches on exactly one condition.** A character that does not exist and one below the viewer's reachability floor take the same code, the same message literal and the same render, because the client never learns which one it got.
- **The media renderer's whole failure surface is covered by a test no v0.13 data can reach.** §7.3 ships the media model with zero upload behaviour, so nothing but `ProfileMedia.svelte.test.ts` ever exercises the render path, the content-warning gate or the broken-handle fallback.
- **Every load-bearing assertion was shown RED against a plausible wrong implementation**, not merely observed green — see below.
- **The phase's whole `ConnectError` classification set is written once**, so plans 05-04 and 05-06 import rather than reopen `web/src/lib/connect/errors.ts`.

## Task Commits

1. **Task 1 RED — failing render tests for the profile media pair** — `887f6fa19` (test)
2. **Task 1 GREEN — the portrait primitive and the media renderer** — `5e9a34137` (feat)
3. **Task 2 RED — failing absence-contract tests for PublicProfile** — `b0d8de411` (test)
4. **Task 2 GREEN — PublicProfile, the props-only body** — `824baf1cb` (feat)
5. **Task 3 — the `/c/[id]` route and its one error branch** — `937c768c1` (feat)

## RED observations

Both TDD tasks were observed RED before their implementation, and — because "the module does not exist yet" is a degenerate RED that proves nothing about whether the assertions *discriminate* — each task's load-bearing assertion was additionally shown to fail against a plausible WRONG implementation, then restored. Verbatim:

**1. Task 1, module-absence RED:**

```
Failed to resolve import "./ProfileMedia.svelte" from
"src/lib/components/characters/ProfileMedia.svelte.test.ts". Does the file exist?
Test Files  1 failed | 48 passed (49)
```

**2. Task 1, the casing rule.** `name.charAt(0)` → `name.charAt(0).toUpperCase()` in the script block — the exact "improvement" a reviewer would make:

```
AssertionError: expected 'B' to be 'b'
Tests  1 failed | 473 passed (474)
```

This is why the plan rejected a grep for `toUpperCase`: the doc comment explaining the rule matches such a grep, and a gate that must be suppressed to stay green stops being a gate.

**3. Task 1, the zero-media rule.** `{#if gallery.length > 0}` → `>= 0`, which renders an empty `<ul>` at zero rows — the placeholder slot the absence contract forbids:

```
AssertionError: expected <ul class="gallery svelte-1uzkc8e"></ul> to be null
Tests  2 failed | 472 passed (474)
```

**4. Task 2, module-absence RED:**

```
Failed to resolve import "./PublicProfile.svelte" from
"src/lib/components/characters/PublicProfile.svelte.test.ts". Does the file exist?
Test Files  1 failed | 49 passed (50)
```

**5. Task 2, the absence contract.** One count line added to the card head:

```
AssertionError: expected 'B Bazian NEUTER 1 details she/her Dir…'
                to be 'B Bazian she/her Direct messages Not …'
Tests  2 failed | 482 passed (484)
```

## Verification evidence

`pnpm test:unit` is **not a CI gate** — no Taskfile target and no workflow invokes vitest — so its own summary lines are pasted here rather than claimed. Verbatim, at the final commit:

```
 Test Files  50 passed (50)
      Tests  487 passed (487)
```

(465 at plan start → 487; 22 new tests across three files.)

- `pnpm check` — `COMPLETED 5583 FILES 0 ERRORS 6 WARNINGS 2 FILES_WITH_PROBLEMS`. All six warnings are in files this plan did not touch (`CreateSceneForm.svelte`, `PresenceList.svelte`) and are present at `HEAD~5`.
- `pnpm build` — `✓ built in 4.62s` · `Using @sveltejs/adapter-static` · `Wrote site to "build"`.
- `task lint` — exit 0.
- `task test` — `DONE 11598 tests, 4 skipped in 76.521s`, exit 0.
- `task fmt` — run after each task; produced no uncommitted output.

Every acceptance-criteria grep returns the stated result, with one exception recorded below.

## Files Created/Modified

- `web/src/routes/c/[id]/+page.svelte` — the route: `$derived($page.params.id)`, an `onMount` fetch through `getCharacterProfile`, and three render states (loading / not-found / generic-failure-with-retry / success). No `+page.ts`, no `+error.svelte`, no session handling.
- `web/src/lib/components/characters/PublicProfile.svelte` — the props-only body. Identity card, in-world description, wrapping fact-pill row, five ordered long-form sections with the out-of-character one last, the gallery, the named web-DM slot and the named sheet section.
- `web/src/lib/components/characters/CharacterPortrait.svelte` — the one tinted plate: 16% `color-mix` fill, 32% border, letter in `--color-primary`, Display 32/600 at 80px and Heading 20/600 at 44px, `aria-hidden`.
- `web/src/lib/components/characters/ProfileMedia.svelte` — primary image, content-warning gate, broken-handle fallback, gallery in array order.
- `web/src/lib/components/characters/{PublicProfile,ProfileMedia}.svelte.test.ts` — 19 specs between them.
- `web/src/lib/connect/errors.ts` / `errors.test.ts` — the four new classifiers and their cross-product table.

## Decisions Made

- **No presentational component is shared with the owner authoring surface.** The two render the same twelve names but answer different questions. The one shared primitive is `CharacterPortrait`, because UI-SPEC unifies the portrait treatment and a single component is what makes "one treatment" *true* rather than *asserted*.
- **`ProfileMedia` is called twice from `PublicProfile`** — once with only `primaryImage` for the portrait slot, once with only `gallery` for the section below — rather than carrying a variant flag. Each call is a no-op on its empty half, so the zero-media case has no code path that could ever grow a placeholder slot.
- **The content-warning control's accessible name is the warning text**, with `aria-expanded` carrying the show/hide semantics. The repo's general rule that a CTA needs a distinct verb+noun accessible name is aimed at the five `Save …` buttons that render together; here a generic verb would displace the one string a viewer needs to decide whether to look.
- **The four `ConnectError` classifiers are specified by a cross-product table**, not four independent `describe` blocks. The property that matters is a property of the *set* — each predicate answers for exactly one code and refuses every other predicate's code — and four independent blocks pass happily when a copy-paste leaves two predicates on the same code.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 — Blocking] `target.innerHTML` cannot be `''` for a Svelte 5 component, so the zero-media assertion was expressed on elements and text instead**

- **Found during:** Task 1
- **Issue:** The plan's acceptance criterion reads "Mounting `ProfileMedia` with `primaryImage: undefined, gallery: []` leaves `target.innerHTML` empty (asserted, not eyeballed)". Svelte 5 emits an `<!---->` anchor comment around every `{#if}` / `{#each}` block, so a component built from conditionals can never produce a literally empty `innerHTML`. The first run failed with `expected ' ' to be ''`.
- **Fix:** The spec now asserts `target.querySelector('*')` is `null`, that the anchor-stripped markup is empty, and that the normalised text is empty. That is *stronger* than the literal criterion — it states "not one element and not one character of text" rather than "this exact string" — and it is not satisfiable by any placeholder.
- **Files modified:** `web/src/lib/components/characters/ProfileMedia.svelte.test.ts`
- **Verification:** shown RED against `gallery.length >= 0` (an empty `<ul>`), see RED observation 3.
- **Committed in:** `5e9a34137`

**2. [Rule 3 — Blocking] The same anchor comments defeat the byte-exact fallback comparison**

- **Found during:** Task 1
- **Issue:** The criterion "comparing the resulting markup to the markup a bare `CharacterPortrait` mount produces" failed as a raw string compare: `"<!----><span …>b</span><!----> <!---->"` versus `"<span …>b</span>"`. The difference is entirely Svelte's block bookkeeping — the rendered element is identical, attribute for attribute.
- **Fix:** A `markup()` helper strips `<!---->` and collapses whitespace before the comparison. Every tag, attribute and text node stays under exact comparison; only the anchors are removed. The helper carries a comment saying why.
- **Files modified:** `web/src/lib/components/characters/ProfileMedia.svelte.test.ts`
- **Committed in:** `5e9a34137`

**3. [Rule 2 — Missing critical] A doc comment tripped the plan's own forbidden-copy gate**

- **Found during:** Task 2
- **Issue:** `PublicProfile.svelte`'s doc comment explained the rule using the word *withheld*, which is in the acceptance grep `withheld|fields are|is private|not shown|restricted|more to see`. That grep — unlike the iteration grep beside it — carries no comment filter, on purpose.
- **Fix:** The comment was reworded to say the same thing without the scanned vocabulary ("a field this viewer may not read … one the floor kept back"), and a note was added recording *why* the wording avoids it. Suppressing the gate, or adding a comment filter to it, was rejected: the plan states the principle itself — "a gate that must be suppressed to stay green stops being a gate".
- **Files modified:** `web/src/lib/components/characters/PublicProfile.svelte`
- **Verification:** the grep returns no match.
- **Committed in:** `824baf1cb`

### Plan criteria that could not be satisfied as literally written

Not a code defect — a grep-arithmetic slip in the plan, recorded so a verifier re-running it is not misled.

- **Task 3, criterion 5:** "`rg -c 'isNotFoundError' 'web/src/routes/c/[id]/+page.svelte'` returns 1". It returns **2**: `rg -c` counts matching *lines*, and the `import { isNotFoundError } from '$lib/connect/errors';` line matches alongside the call. The property the criterion protects — exactly one classification branch — holds and is verifiable as written with a call-shaped pattern: `rg -c 'isNotFoundError\('` returns **1**.

## Issues Encountered

- **`pnpm test:unit -- <path>` does not filter.** The trailing path argument is passed through but the run still executes all 50 test files. Every verification here therefore reports the whole suite, which is stronger than the plan's scoped invocation and is what the pasted summary lines show.
- **Two of the three plan criteria that needed adjusting were the same root cause** — Svelte 5's block anchor comments make any raw-`innerHTML` equality assertion unstable. Any later plan writing a component spec in this codebase should compare on elements/text, or strip anchors first.
- **`requirements mark-complete` ticks the checkbox but does not update the traceability table.** All eight ids came back `"surface": "traceability", "applied": false` with `table_unmatched` listing every one, despite the rows existing in the expected shape (`| PROFILE-01 | Phase 5 | Pending |`, identical in form to rows the tool has previously written `Complete` into). Plan 05-01 hit the same thing — `IDENT-05` is still `Pending` in that table — so this is a pre-existing tool limitation, not something this plan introduced. **The rows were deliberately left alone rather than hand-patched:** REQUIREMENTS.md is tool-owned, and patching half a phase by hand while the sibling plan's row stays `Pending` produces exactly the inconsistency the table exists to prevent. The phase verifier should reconcile all of Phase 5's rows in one pass.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- `web/src/lib/connect/errors.ts` now carries the whole set this phase needs. **Plans 05-04 and 05-06 import `isAbortedError`, `isAlreadyExistsError` and `isInvalidArgumentError`; neither should reopen the file to add them.**
- `CharacterPortrait.svelte` exists with a `size` prop already carrying the roster's 44px case, but **the shipped roster still renders its own solid `bg-primary` plate** (`(authed)/characters/+page.svelte`). UI-SPEC's "the tint everywhere, including on the shipped roster card" is therefore **not yet true**. That file is plan 05-08's (the sectioned roster rewrite) and is deliberately untouched here — 05-08 must swap it for `CharacterPortrait`.
- `/c/[id]` has **no `<title>` and no OG/social metadata** — flagged assumption A6 in the plan, deferred because the game's own display name is server-side-only (#4905) and branding INV-6 forbids hardcoding the platform name.
- `/c/[id]` sets **no `+error.svelte`**; D-95 hands the single root boundary to Phase 6 (#4903), and this route adopts it when it lands.
- The **command palette on a public page** (T-05-02-07, GitHub **#4962**) is unchanged and still out of scope: `visibleSections()` offers `(authed)` destinations to an anonymous viewer, and the `(authed)` redirect is the fail-safe working.

## Known Stubs

None in the sense the scan means — no hardcoded empty value flows to a rendered field, and no component receives permanently-mock data.

Two renderings are **intentionally empty by contract and are not stubs**: the sheet section (`Sheet` / `No sheet system yet.`) and the web-DM slot (`Direct messages` / `Not available yet.`) are PROFILE-02 and EXT-08's *deliverable* — a named empty slot rather than a dead affordance — and both are authored constants asserted by test.

One renderer is **unreachable by design rather than unfinished**: `ProfileMedia.svelte` has no v0.13 input, because §7.3 ships the media model with zero upload behaviour and nothing mints a media identifier. Its behaviour is pinned by the held-out test that is the UI-SPEC backstop row, and its one open derivation — `mediaSrc()` maps a media id to `/media/<id>`, a path no server route serves in v0.13 — is documented in the component as the single line to change when a serving endpoint lands. It renders nothing at zero rows, so no player can reach it.

## Deferred / out of scope

- The untracked `.gsd/` directory predates this plan's first commit (it is in the pre-execution `git status`) and is GSD runtime output, not a product of any task here. It is left untouched rather than committed or gitignored — out of scope per the executor's scope boundary.

## Threat Flags

None. Every surface this plan added is inside the plan's own `<threat_model>`: the route and its uniform not-found render are T-05-02-01 through T-05-02-06, and the one accepted residual (T-05-02-07, the command palette) is tracked as #4962. This plan added no network endpoint, no auth path, no file access and no schema change — it is entirely client-side rendering over an RPC that shipped in phase 04.

---
*Phase: 05-character-identity-ui-public-profiles*
*Completed: 2026-08-12*

## Self-Check: PASSED

All key files verified present on disk and all five task commits verified in
`git log`:

- FOUND: `web/src/routes/c/[id]/+page.svelte`,
  `web/src/lib/components/characters/{PublicProfile,CharacterPortrait,ProfileMedia}.svelte`,
  `web/src/lib/components/characters/{PublicProfile,ProfileMedia}.svelte.test.ts`,
  `web/src/lib/connect/errors.ts`
- FOUND: `887f6fa19`, `5e9a34137`, `b0d8de411`, `824baf1cb`, `937c768c1`
- `pnpm test:unit` 487/487 · `pnpm check` 0 errors · `pnpm build` ✓ ·
  `task lint` exit 0 · `task test` exit 0 (11598 tests, 4 skipped)
