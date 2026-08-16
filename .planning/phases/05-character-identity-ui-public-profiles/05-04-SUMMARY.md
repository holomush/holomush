---
phase: 05-character-identity-ui-public-profiles
plan: 04
subsystem: ui
tags: [svelte5, sveltekit, connectrpc, optimistic-concurrency, update-mask, textencoder, accessibility, vitest]

# Dependency graph
requires:
  - phase: 05-01
    provides: the character flow layer (`web/src/lib/characters/client.ts`) with updateCharacterProfile / updateCharacterDescription / getMyCharacter
  - phase: 05-02
    provides: the shared Connect error predicates in `web/src/lib/connect/errors.ts` (isAbortedError) and the phase's scoped-CSS component idiom
provides:
  - "/characters/[id] — the owner's sectioned authoring surface, five sections over two write RPCs"
  - "ProfileSection.svelte — a reusable dirty/save/status/error unit that is the unit of FAILURE, not just of layout"
  - "ByteCounter.svelte — a TextEncoder-based counter that agrees with the server's byte cap"
  - "The held-out 99/100/101 multi-byte boundary test pinning client/server agreement about a limit"
affects: [05-05, 05-06, 05-08, phase-6-admin-portal, character-retirement-flow]

actuals:
  tokens: 10855
  tasks: 3
  commits: 5

tech-stack:
  added: []
  patterns:
    - "Per-section save as an interface decomposition over a transaction boundary"
    - "untrack() to state a deliberate capture-once prop snapshot rather than suppress the warning"
    - "Error classification imported from a shared predicate module; components never inspect status codes"

key-files:
  created:
    - web/src/lib/components/characters/ByteCounter.svelte
    - web/src/lib/components/characters/ByteCounter.svelte.test.ts
    - web/src/lib/components/characters/ProfileSection.svelte
    - web/src/lib/components/characters/ProfileSection.svelte.test.ts
    - web/src/routes/(authed)/characters/[id]/+page.svelte
  modified: []

key-decisions:
  - "The Reload control performs a full location.reload(), which is exactly what the UI-SPEC copy promises — but it discards unsaved text in the OTHER four sections. Recorded as a known cost, not silently softened into a bespoke soft-refresh."
  - "Load-failure copy authored as `Couldn't load this character. Try again.` — the roster's pattern in the singular, since the UI-SPEC error table has no row for this surface's load failure."
  - "Section 2's field descriptor carries a LOCAL key (`description`), not a mask path, because UpdateCharacterDescription takes no update_mask at all."
  - "ProfileSection snapshots its values at mount via untrack, so a sibling section's save cannot reset a section's in-progress draft."

patterns-established:
  - "Plausible-wrong RED: both TDD tasks went red against the naive implementation an implementer would actually write (`value.length`; `e.message` relaying), not against a missing module — so the RED discriminates the defect rather than proving a file is absent."
  - "Gate-vs-comment discipline: when an acceptance grep was tripped by a comment EXPLAINING the absence of the thing, the prose was reworded rather than the grep weakened (the PublicProfile.svelte precedent, applied three times here)."

requirements-completed: [PROFILE-06, PROFILE-07, PROFILE-08, PROFILE-09, PROFILE-12]

coverage:
  - id: D1
    description: "The client byte counter agrees with the server's byte cap at 99/100/101 for a multi-byte value, and at 3999/4000/4001 for the long cap"
    requirement: PROFILE-06
    verification:
      - kind: unit
        ref: "web/src/lib/components/characters/ByteCounter.svelte.test.ts (7 specs; `pnpm test:unit`)"
        status: pass
    human_judgment: false
  - id: D2
    description: "A section saves independently: only its own mask paths travel, its Save is disabled when clean, and in-flight the label is unchanged with aria-busy=true"
    requirement: PROFILE-06
    verification:
      - kind: unit
        ref: "web/src/lib/components/characters/ProfileSection.svelte.test.ts (11 specs; `pnpm test:unit`)"
        status: pass
    human_judgment: false
  - id: D3
    description: "A concurrent-edit refusal renders the authored copy plus Reload, preserves the typed text and moves focus to the first field; a client-bug refusal collapses to the generic copy and never leaks the code"
    requirement: PROFILE-06
    verification:
      - kind: unit
        ref: "web/src/lib/components/characters/ProfileSection.svelte.test.ts — Aborted / InvalidArgument / focus specs"
        status: pass
    human_judgment: false
  - id: D4
    description: "The four profile masks partition exactly the twelve shipped allowlist paths, none duplicated, none unassigned — so rumors, currently, rp_preferences and timezone are all editable and reachable"
    requirement: PROFILE-07
    verification:
      - kind: other
        ref: "set comparison of the page's path literals against `rg -o '\"profile\\.[a-z_]+\"' internal/grpc/characteraccess_write.go | sort -u` — 12 occurrences, 12 unique, exact match, zero duplicates"
        status: pass
    human_judgment: false
  - id: D5
    description: "profile.rp_preferences travels only as that mask path and has no route to the JSONB settings column on characters"
    requirement: PROFILE-08
    verification:
      - kind: other
        ref: "grep of the route: the only `preferences` matches are the mask path, the name attribute and the visible label — no bare request field"
        status: pass
    human_judgment: false
  - id: D6
    description: "The PROFILE-12 not-retroactive statement renders once, above the first section, as plain muted body text — no icon, no border, no callout, never amber"
    requirement: PROFILE-12
    verification:
      - kind: other
        ref: "grep: no `ffb300` / `--color-cursor` in the route; the `.standing` rule sets colour and type only, no border or background"
        status: pass
      - kind: manual_procedural
        ref: "visual confirmation that it does not read as an alert"
        status: unknown
    human_judgment: true
    rationale: "Whether a standing statement READS as a fact rather than a warning is a visual judgement; the greps pin the mechanics (no icon/border/accent) but not the impression."
  - id: D7
    description: "A conflict in one section leaves the other four untouched, end to end against a live server across two tabs"
    requirement: PROFILE-09
    verification:
      - kind: manual_procedural
        ref: "05-04-PLAN.md Task 3 <human-check> — two-tab concurrent-edit walkthrough"
        status: unknown
    human_judgment: true
    rationale: "Per-section conflict isolation is proven at the component boundary by unit tests, but the round trip through a real characters.version guard has no automated coverage in this plan; vitest is not a CI gate and no E2E spec covers this route yet (05-06/05-08 own that)."

duration: 15min
completed: 2026-08-12
status: complete
---

# Phase 05 Plan 04: The Owner's Authoring Surface Summary

**Five per-section saves over two RPCs that guard the same `characters.version` with no transaction between them — so a conflict costs one section of typing instead of twelve, and the partial-save state stops existing.**

## Performance

- **Duration:** 15 min
- **Started:** 2026-08-12T22:24:51Z
- **Completed:** 2026-08-12T22:40:11Z
- **Tasks:** 3 of 3
- **Files created:** 5 (0 modified)

## Accomplishments

- **`/characters/[id]` ships with five independent sections**, each owning its own Save, mask, status region and error region. The four profile masks partition **exactly** the twelve paths in `updateCharacterProfileMaskablePaths` — verified as a set comparison against the Go source, not asserted: 12 occurrences, 12 unique, exact match, zero duplicates.
- **One `version` cell** feeds every save and is refreshed from every response. It is never re-read to obtain a fresher number, which is the move that would turn a stale client into a silent last-write-wins.
- **The byte counter cannot disagree with the server about a limit.** `validateProfileValue` compares `len(value) > maxBytes` on a Go string — bytes — and the counter now measures with `TextEncoder`. Seven held-out specs pin 99/100/101 and 3999/4000/4001 for multi-byte values, plus the astral-plane case where UTF-16 length (2) and UTF-8 length (4) diverge.
- **No server-supplied string reaches the player.** `CHARACTER_MASK_PATH_UNSUPPORTED` and `CHARACTER_VERSION_REQUIRED` are client bugs and collapse to the generic copy; the test asserts the **absence** of the leaked code, not merely the presence of the authored line.
- **Both TDD tasks went RED against the plausible-wrong implementation**, not a missing module — so each RED reproduced the actual defect (see below).

## Task Commits

1. **Task 1: ByteCounter** — `12e9770d8` (test, RED) → `104b03340` (feat, GREEN)
2. **Task 2: ProfileSection** — `763e144d9` (test, RED) → `760c9f213` (feat, GREEN)
3. **Task 3: The /characters/[id] route** — `a957da21d` (feat)

## Files Created

- `web/src/lib/components/characters/ByteCounter.svelte` — measures a value in UTF-8 bytes; display rule (within 20% of cap) and comparison rule (strictly greater-than, mirroring the server) kept deliberately separate.
- `web/src/lib/components/characters/ByteCounter.svelte.test.ts` — 7 specs; every fixture asserts its own encoded byte length before asserting the counter, so a drifted fixture fails loudly instead of testing a boundary that moved.
- `web/src/lib/components/characters/ProfileSection.svelte` — the dirty/save/status/error unit. Knows nothing about `version`; imports its error classification.
- `web/src/lib/components/characters/ProfileSection.svelte.test.ts` — 11 specs across mask, dirty, isolation, in-flight, status, both failure classifications, both focus paths and the at-cap case.
- `web/src/routes/(authed)/characters/[id]/+page.svelte` — one `GetMyCharacter`, one version cell, five sections, the standing PROFILE-12 statement.

## The RED phases were discriminating, not ceremonial

The orchestrator asked for RED against a **plausible wrong** implementation rather than a missing module. Both tasks delivered that, and it paid:

- **ByteCounter** landed counting `value.length` — the naive reading of "how long is this string". 7 specs failed; the ASCII spec and the below-threshold spec **passed**, which is the point: `.length` is right for ASCII and wrong for everything else.
- **ProfileSection** landed with the shipped `register/+page.svelte` catch idiom (`e instanceof Error ? e.message : …`) — the in-repo precedent an implementer actually reaches for. It reproduced threat **T-05-04-04** verbatim:

  ```
  AssertionError: expected '[invalid_argument] CHARACTER_MASK_PAT…'
    to contain 'Couldn't save. Try again.'
  Received: "[invalid_argument] CHARACTER_MASK_PATH_UNSUPPORTED: profile.secret"
  ```

  4 specs failed (two classifications, two focus moves); the mask, dirty, in-flight and status specs passed, so the RED isolated exactly the defect.

## Decisions Made

1. **The `Reload` control performs a real page reload.** The authored copy is *"Reload to get the latest, then re-apply your edits"*, so the control does what it says. **Known cost, recorded rather than softened:** a reload also discards unsaved text in the other four sections, which sits in tension with D-93's "a conflict costs one section". Inventing a bespoke soft-refresh would have added a second, subtler state machine not in the plan's prop list, so the literal reading shipped. Flagged below as a candidate for 05-06/05-08.
2. **Load-failure copy authored as `Couldn't load this character. Try again.`** The UI-SPEC error table has no row for this surface; the plan said "roster-style". The roster's string is plural (`your characters`) and wrong on a single-character page, so its *pattern* was followed in the singular.
3. **Section 2's descriptor key is local, not a mask path.** `UpdateCharacterDescription` takes no `update_mask`, so its `path: 'description'` is only a values-map key. `ProfileSection` never learns which case it is in — which is what lets one component serve both RPCs.
4. **`untrack` on the mount snapshot.** A successful save returns the *whole* post-write character; if a section tracked its `values` prop, section 1 saving would reset section 4's baseline and discard the player's draft there. Stating the capture with `untrack` fixed the `state_referenced_locally` warning **at its cause** rather than with a suppression.

## Deviations from Plan

### Auto-fixed issues

**1. [Rule 3 — Blocking] `state_referenced_locally` warning on the mount snapshot**
- **Found during:** Task 2 (GREEN)
- **Issue:** `let loaded = $state({ ...values })` raised two svelte-check warnings, taking the workspace from 6 warnings / 2 files to 8 / 3.
- **Fix:** Wrapped both snapshots in `untrack(() => ({ ...values }))`, expressing the deliberate capture-once rather than suppressing the diagnostic. Per repo rule, no ignore directive was added.
- **Verification:** `pnpm check` exit 0, back to the pre-existing 6 warnings / 2 files.
- **Committed in:** `760c9f213`

**2. [Rule 3 — Blocking] Three acceptance greps tripped by comments explaining an absence**
- **Found during:** Tasks 1, 2 and 3
- **Issue:** The `.length`, `aria-label` and `retire|…|visibility` gates each matched a *comment* documenting why the thing is absent — e.g. `String.length` in the prose explaining why it is not used.
- **Fix:** Reworded the prose to state the forbidden construct rather than spell it, following the in-phase `PublicProfile.svelte` precedent (*"a gate that has to be suppressed to stay green stops being a gate, so a comment may not be the thing that trips it"*). The greps were left untouched.
- **Verification:** all three now return no match; behaviour unchanged.
- **Committed in:** `104b03340`, `760c9f213`, `a957da21d`

---

**Total deviations:** 2 auto-fixed (both Rule 3). **Impact:** no scope change; both preserved a gate's power rather than trading it away.

## Verification

All gates run inline, judged by exit code:

| Gate | Exit | Result |
|---|---|---|
| `task lint` | 0 | pass |
| `task build` | 0 | pass |
| `task test` | 0 | 11636 tests, 4 skipped (pre-existing) |
| `task fmt` | 0 | **no mutations** — nothing to commit |
| `pnpm check` | 0 | 0 errors, 6 warnings (all pre-existing, PresenceList/CreateSceneForm) |
| `pnpm test:unit` | 0 | **52 files / 509 tests** (from 50 / 489 — this plan adds 18) |
| `pnpm build` | 0 | pass |

Every acceptance grep in the plan returns its stated result. The mask-union check was executed as a set comparison rather than eyeballed:

```
page path occurrences: 12   unique: 12   server allowlist: 12
set diff: EXACT MATCH — union of the four masks == the twelve shipped paths
duplicates across sections: none
```

**One grep is reported honestly rather than as a clean pass:** `rg -n 'preferences'` on the route matches the mask path, the `name` attribute, **and the visible label `RP preferences`**. The prohibition is about a bare `preferences` *field*; a player-facing label is not one, and every honest label for this field contains the word. No request field, column reference or write path named bare `preferences` exists on the surface.

## Issues Encountered

**`pnpm test:unit -- <path>` does not filter.** Passing a path through `pnpm` ran the whole suite anyway (51 files, not 1). Not worth fixing here — the full suite is 8 seconds — but the plan's per-task filtered command does not narrow, so the transcribed summary lines are whole-suite counts. Noted so a later plan does not read those numbers as single-file runs.

**Web tests remain outside CI.** Nothing in `Taskfile.yaml` or `.github/workflows/ci.yaml` invokes vitest, so the 18 specs added here are **not** a merge gate. Every run's own summary lines are transcribed into the task commit bodies as evidence rather than claimed. This is a standing phase-level gap, not introduced by this plan.

## Known Stubs

None. Every field the surface renders is wired to a live RPC; no placeholder, mock or hardcoded value ships.

## Threat Flags

None. The surface introduces no network endpoint, auth path or schema change; it consumes two shipped RPCs whose allowlist and version guard are server-side. The plan's own register (T-05-04-01 … 08) is covered: the mask union is verified against the shipped allowlist, the version guard is single-celled and surfaced rather than retried, the byte boundary is pinned by held-out tests, error codes collapse to authored copy, `rp_preferences` reaches only its mask path, no `sendCommand` exists on the surface, and no audience-control vocabulary appears.

## Notes for the phase reconciliation pass

- **`requirements mark-complete` reported `table_unmatched` again** (as in 05-01/02/03) and skipped the traceability table. Not hand-patched, per instruction — for the phase-end reconciliation.
- **The eight Playwright E2E specs broken by 05-03 remain broken.** Untouched here by instruction; their replacement is 05-06/05-08.
- **`.gsd/` is untracked** in the worktree. It predates this plan (present in the session's opening `git status`) and is GSD runtime output, so it was neither committed nor gitignored here.
- **Candidate follow-up:** the `Reload` control's blast radius (decision 1). If 05-06 or 05-08 revisits this surface, a soft refresh that re-reads the character and updates only the version + the conflicted section's baseline would make the conflict cost genuinely one section. Filed as an observation, not a defect — the current behaviour matches the authored copy.

## Next Phase Readiness

Ready. `ProfileSection` and `ByteCounter` are reusable by any later authoring surface, and the version-cell pattern is the one Phase 6's admin edit surfaces should adopt. The two-tab conflict walkthrough (D7) is the one deliverable still needing human sign-off.

---
*Phase: 05-character-identity-ui-public-profiles*
*Completed: 2026-08-12*
