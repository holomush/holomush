---
status: resolved
trigger: "GH issue 4986 needs triage and fixing"
created: 2026-08-15
updated: 2026-08-15
issue: https://github.com/holomush/holomush/issues/4986
---

# Debug: admin-portal D-110 sees one extra list call (CI-only)

## Symptoms

**Expected behavior:**
`web/e2e/admin-portal.spec.ts` — "admin portal — D-110 over the real stack › closes the
Sheet, updates the row from the response with no list re-read, and fires one receipt".
The test snapshots the count of `WebAdminListCharacters` / `WebAdminSearchCharacters`
requests as `before`, performs an edit through the Sheet, and asserts the mutation
caused **zero** list re-reads: `expect(listCalls - before).toBe(0)`.

**Actual behavior:**
One extra list call is observed.

```
Error: expect(received).toBe(expected)
Expected: 0
Received: 1
> 632 |     expect(listCalls - before).toBe(0);
```

(Currently `:609`/`:632`; was `:562` before the `ad3fa665d` helper change shifted lines.)

**Error messages:**
Also present in the browser console on this spec AND on the `:248` spec:
`[error] Failed to load resource: 401 (Unauthorized)`. Unexplained. May be a common
factor rather than incidental — this is the strongest untested lead.

**Timeline:**
Surfaced during the #4984 E2E investigation on branch `v013-phase-03` (head `ad3fa665d`).
Split out of #4984. Distinct from #4981 (`tapRowOutsideNameCell`, fixed by `ad3fa665d`).

**Reproduction:**
- **CI: 3 for 3, deterministic.** Runs `31915308350`, `31915929027`, `31918290676` —
  identical failure each time.
- **Local `task test:e2e -- admin-portal`: passes** (14/14; 18/18 alongside `admin.spec.ts`).
- **Local `task test:e2e:cover -- --grep-invert @quarantine`: passed once.**

## CRITICAL reproduction constraint (memory `cmnppxtfhe`)

**`task test:e2e` IS NOT WHAT CI RUNS.** CI runs
`task test:e2e:cover -- --grep-invert @quarantine` (`.github/workflows/ci.yaml:286`).
`test:e2e:cover` has `deps: [docker:build:cover]` — a **coverage-instrumented image**,
which is measurably slower and is where timing-sensitive specs fail. Plain `test:e2e`
has `deps: [docker:build]`.

Three local green runs on `task test:e2e` previously proved nothing and each one
tempted an unearned conclusion. **Same command, same build, same scope — or it is a
different experiment.** Any "cannot reproduce" verdict from a `task test:e2e` run is
inadmissible.

Note the scope difference too: the local scoped run is 14 tests; CI runs 132.

## Eliminated hypotheses (do NOT re-derive)

- hypothesis: The WR-08 change (`121aa8d6e`) causes it — it replaced
  `if (!updated) return` with `if (!updated) throw` on the mutation path.
  eliminated_because: Every `reload()` call site in
  `web/src/routes/(authed)/admin/characters/+page.svelte` is a user-interaction
  handler — `onsearch` (:289), status filter (:295), sort (:305), player filter (:312),
  pagination (:317), clear-filters (:330). **No error path calls `reload()`**, so the
  throw cannot produce a list call.

- hypothesis: A late `CharacterFilterBar` debounce fires after the snapshot.
  eliminated_because: `openSheet` clicks `button.rowbtn` directly (`:148-152`), so it
  never starts the 250ms debounce (`CharacterFilterBar.svelte:43 debounceMs = 250`).
  **>>> THIS ELIMINATION WAS WRONG — it was the answer. <<<** The premise is true but
  too narrow: it asks only whether `openSheet` ARMS the timer, and stops there. The
  timer was armed earlier and elsewhere — by the SETUP helper `gotoAdminCharacters`
  (`web/e2e/helpers/admin.ts:54` `fill(term)`), one call up in `signInAsAdmin`. Lesson:
  when eliminating "a stale timer fires late", the question is not "does the action
  under test start one" but "is one still armed when the measurement begins" — which
  requires looking at everything the setup did, not just the step being measured.
  See Resolution.

## Current Focus

hypothesis: CONFIRMED — the extra call is the debounced `onsearch` armed by the SETUP
  helper `gotoAdminCharacters` (`web/e2e/helpers/admin.ts:54` `fill(term)`), not by
  anything the mutation does. The helper returns as soon as the row is VISIBLE
  (`:55`), which the initial unfiltered `+page.ts` list can already satisfy — so it
  returns with `CharacterFilterBar`'s 250ms timer (`CharacterFilterBar.svelte:56`)
  still armed. The timer then fires `onsearch` → `reload()` → `WebAdminSearchCharacters`
  (`+page.svelte:289` → `:268`) inside the spec's measurement window.
test: RED/GREEN with the CI condition emulated locally by delaying
  `WebAdminUpdateCharacter` (the window between SNAPSHOT and FINAL), fix toggled.
expecting: RED (`Received: 1`) without the helper fix; GREEN with it, same delay.
next_action: DONE — RED and GREEN both obtained, full suite re-verified green. Awaiting
  human confirmation on CI (PR #4984). NOTHING HAS BEEN PUSHED; pushing needs explicit
  approval. The local commit is `fix(e2e): settle the search gotoAdminCharacters starts`.

## Reasoning checkpoint

reasoning_checkpoint:
  hypothesis: "The counted extra request is the setup helper's still-armed 250ms search
    debounce landing between the `before` snapshot and the assertion — a contaminated
    measurement window in the TEST, not a re-read caused by the mutation."
  confirming_evidence:
    - "In-page fetch stack for the late call ends `at Object._e [as onsearch]` →
       `reload()` → `webAdminSearchCharacters`. Directly observed, not inferred."
    - "Timestamps on a GREEN local `test:e2e:cover` run: SNAPSHOT t=…529092,
       FINAL t=…529190, the search fetch t=…529260 — it missed the window by 70ms."
    - "The only list call before the snapshot was `WebAdminListCharacters` whose stack
       ends in SvelteKit's load runner (`+page.ts`), proving the row was found in the
       INITIAL unfiltered list and the helper never waited for the search."
  falsification_test: "If the late request were caused by the mutation, its stack would
    name the save path, and it would not appear on a run where the assertion already
    passed. Both are contradicted."
  fix_rationale: "Make `gotoAdminCharacters` wait for the search it starts to actually
    land, so it returns in a settled state. That drains the armed timer at its source,
    for every spec that uses the helper. It does not weaken the D-110 assertion."
  blind_spots: "The local RED is produced by an injected update-RPC delay emulating CI's
    slower coverage image, not by CI itself. Final proof is CI on the open PR."
  candidate_causes:
    - "code (test): setup helper returns with an unsettled debounce — CONFIRMED"
    - "environment: coverage-instrumented image widens the write round trip, moving the
       250ms timer from outside the window to inside it — CONFIRMED as the CI trigger"
    - "code (product): a mutation-path re-read — REFUTED by the stack"
  and_gate: "yes — BOTH conditions are required. The armed timer alone is harmless (local
    run: fired 70ms late, green); the slow image alone is harmless. The failure needs the
    unsettled helper AND a window wide enough to contain the 250ms mark."

## Investigation constraints

- **Branch:** `v013-phase-03`, worktree
  `/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03`. PR #4984 is OPEN
  against `main`; `E2E Test` is a required check and this issue is its sole blocker.
- **Pushing instrumentation commits to the open PR branch requires explicit user
  approval before the push** — it is outward-facing and lands on a live PR. Local
  work, commits, and analysis do not.
- **MUST NOT use `[ci skip]` / `[skip ci]`** on any commit on this branch (rule
  `sqzzr02e2j`) — the branch has an open PR and a skip directive leaves required
  checks permanently unset.
- Acceptance criterion rule `wdn1abkmd6`: whatever guard or fix lands MUST be shown to
  go RED under this specific bug. A change that leaves the test passing both with and
  without the fix has proven nothing — and this issue's whole difficulty is that the
  test already passes locally.

## Suggested first step (from the issue)

Instrument the spec's request counter to log the URL and a JS stack for each counted
request, then run under `task test:e2e:cover` — so the extra call identifies its own
trigger instead of being inferred from the six candidate handlers.

## Evidence

- timestamp: 2026-08-15 (instrumented local run)
  checked: Ran the ADMISSIBLE command — `task test:e2e:cover -- --grep-invert @quarantine`
    (full 132, coverage image), with the D-110 spec instrumented via `page.addInitScript`
    wrapping `window.fetch` to log a JS stack per list request.
  found: Run GREEN (131 passed, 1 skipped) — but the diagnostics captured the mechanism
    anyway. Ordered log:
      t=…528964  WebAdminListCharacters   stack ends `at async vt` (SvelteKit load runner)
      t=…529092  [SNAPSHOT] before=0
      t=…529190  [FINAL] after=0   → assertion passes
      t=…529260  WebAdminSearchCharacters stack ends `at Object._e [as onsearch]`
  implication: The search request that the CI failure counts DID happen — it merely
    landed 70ms too late to be counted on this fast local machine. The bug is a race
    with a ~70ms margin, not an absent behaviour.

- timestamp: 2026-08-15
  checked: The JS stack of the late request.
  found: `webAdminSearchCharacters` ← `C` (searchAdminCharacters) ← `K` (reload) ←
    `Object._e [as onsearch]`. That is `+page.svelte:286-290` (`onsearch` → `reload()`)
    reaching `:268` (`searchAdminCharacters`).
  implication: The trigger is `onsearch` — one of the six enumerated handlers after all.
    What the enumeration missed is WHO ARMED IT: not the mutation, but the setup helper.

- timestamp: 2026-08-15
  checked: `web/e2e/helpers/admin.ts:48-56` (`gotoAdminCharacters`).
  found: `:54` fills `input[name="q"]`, arming `CharacterFilterBar`'s 250ms debounce
    (`CharacterFilterBar.svelte:53-57`). `:55` then waits only for `rowFor(...)` to have
    count 1 — a condition the INITIAL unfiltered list (page 1, size 50) already satisfies
    for a freshly created character. Confirmed by the first stack being the `+page.ts`
    load, not the search.
  implication: The helper can return with the debounce still armed. Every timing property
    the caller then measures is contaminated by a pending, unrelated list read.

- timestamp: 2026-08-15
  checked: Why CI is deterministic and local is green.
  found: Let F = `fill()`; the timer fires at F+250 regardless of machine speed.
    SNAPSHOT = F+A (row assert + click + sheet + detail fetch); FINAL = F+A+B (typing +
    save click + UPDATE round trip + sheet close). Failure iff `A < 250 < A+B`.
    Local measured A=82, B=98 → 250 > 180, misses → green.
    CI's coverage-instrumented image inflates B (a DB WRITE round trip) past the mark
    while A stays under it → 3/3 deterministic.
  implication: This is an AND-gate: unsettled helper AND a wide enough window.

- timestamp: 2026-08-15
  checked: The `401 (Unauthorized)` lead (ranked strongest in the brief).
  found: Two `POST …/WebService/WebCheckSession` 401s appear on the PASSING local run
    too. Playwright attaches `browser-console-logs` only to FAILING tests, which is why
    they appeared to correlate with the two failing specs.
  implication: RED HERRING — a benign artefact of the shared sign-in flow, present on
    green runs. Not the cause. Eliminated.

- timestamp: 2026-08-15
  checked: Whether any non-`reload()` source can issue a list call (`+page.ts` load
    re-running).
  found: `rg` over `web/src` finds ZERO `invalidate`/`invalidateAll` and ZERO `goto(`
    in the admin surface, and `EditCharacterSheet.svelte:343` calls `e.preventDefault()`
    synchronously so the form never natively submits/navigates.
  implication: `load()` runs once per navigation; it is not a live extra-call source.
    Only `reload()` remained — consistent with the stack.

- timestamp: 2026-08-15
  checked: Whether the counter could double-count via CORS preflight.
  found: Playwright `baseURL` is `http://gateway:8080` and the gateway serves the built
    app, so `import.meta.env.DEV` is false and `transport.ts` uses `baseUrl: ""` —
    same-origin, no `OPTIONS` preflight.
  implication: Eliminated; each logical list call is exactly one counted request.

## Eliminated

(see "Eliminated hypotheses" above)

## Resolution

root_cause: |
  A TEST defect, not a product defect. The product is correct: the D-110 mutation causes
  no list re-read.

  `gotoAdminCharacters` (`web/e2e/helpers/admin.ts:54`) fills `input[name="q"]`, which arms
  `CharacterFilterBar`'s 250ms debounce (`CharacterFilterBar.svelte:53-57`). It then waits
  only for the ROW to be visible (`:55`) — a condition the initial unfiltered `+page.ts`
  list already satisfies for a just-created character — so it can return with the timer
  still armed. The timer later fires `onsearch` → `reload()` → `searchAdminCharacters`
  (`+page.svelte:286-290` → `:268`), issuing a `WebAdminSearchCharacters` read attributable
  to nothing the caller did.

  The D-110 spec snapshots the list-call count after opening the Sheet and asserts a zero
  delta across the mutation. With F = `fill()`, A = F→snapshot and B = snapshot→assertion,
  the stray read is counted exactly when `A < 250 < A+B`. Locally A=82, B=98 → it missed by
  70ms (green). CI's coverage-instrumented image inflates B — a DB WRITE round trip — past
  the mark while A stays under it, so it lands inside the window 3/3.

  AND-gate: BOTH the unsettled helper AND the widened window are required. Neither alone
  fails.
fix: |
  `web/e2e/helpers/admin.ts` — `gotoAdminCharacters` now awaits the
  `WebAdminSearchCharacters` response it starts (waiter registered before the `fill` so a
  fast answer cannot be missed) before asserting the row. The helper returns settled, so
  its own debounce can no longer land in a caller's measurement window. This drains the
  timer at its source rather than widening/narrowing any assertion; the D-110 assertion is
  untouched and still `toBe(0)`.
verification: |
  REPRODUCED and PROVEN RED→GREEN locally, under the admissible command
  (`task test:e2e:cover`, the coverage-instrumented image CI uses).

  The CI condition was emulated by injecting a 400ms `page.route` delay on
  `WebAdminUpdateCharacter` — the same axis CI slows (the write round trip that widens B).
  Identical harness both runs; only the fix toggled:

    RED   (harness, no fix):  `task test:e2e:cover -- --grep "no list re-read"` → 1 failed,
                              `Expected: 0 / Received: 1` — the exact CI signature.
                              Timeline: SNAPSHOT t=…847622, search fetch t=…847813 (INSIDE),
                              FINAL t=…848466 after=1. A=59 < 250 < 903=A+B, as predicted.
    GREEN (harness, fix):     same command → 1 passed. Timeline: search t=…931466 fires and
                              is AWAITED, SNAPSHOT t=…931562, FINAL t=…932410 after=0. The
                              window is 848ms — as wide as RED's 903ms — and empty, so the
                              fix works by draining the timer, not by shrinking the window.

  Full-suite regression, exact CI command `task test:e2e:cover -- --grep-invert @quarantine`
  (132 collected): 131 passed, 1 skipped, exit 0.
  `pnpm check` (svelte-check): 0 errors.

  HONEST LIMITS: the local RED was produced by an INJECTED delay, not by CI itself — the
  machine here is too fast to fail unaided (an uninstrumented full run passed with a 70ms
  margin). The causal chain is directly observed (in-page JS stack naming `onsearch`, plus
  timestamps on both sides of the boundary), so this is not inference from six candidates.
  But final confirmation is CI on PR #4984, which has NOT been run — nothing was pushed.

  CI CONFIRMATION (added after the above was written): pushed as `33731f215`.
  `E2E Test` = SUCCESS on PR #4984, along with all eight required checks. The prediction
  held: the fix that was labelled UNPROVEN against real CI is now proven against it.
  Note `Build` failed once on this SHA and passed on re-run — an UNRELATED pre-existing
  flake (a `bits-ui` body-scroll-lock `setTimeout` firing after test-env teardown:
  `ReferenceError: document is not defined`, 772/772 tests passing, run failed on the
  unhandled error alone). Two CI runs on the identical SHA disagreed on `Build`, which is
  nondeterminism by direct demonstration; this change touches no file under `web/src/`.
files_changed:
  - web/e2e/helpers/admin.ts (awaited search settle in gotoAdminCharacters)
  - commit 20ae6b2fc (the settle) and 33731f215 (code-review follow-ups: fully-qualified
    RPC path; assert `ok()` so a failed search names the RPC instead of blaming the row
    15s later; comment trimmed to the durable claim)

why_not_caught: |
  No gate existed for this class. `task test:e2e` (and even an uninstrumented
  `test:e2e:cover`) passes on developer hardware because the race has a ~70ms margin there;
  only CI's slower coverage image reliably crosses it. The debug session's earlier
  elimination of "a late debounce" was too narrow — it checked only whether `openSheet`
  arms the timer, and never considered that the SETUP helper had already armed it.
recurrence_guard: |
  The settle in `gotoAdminCharacters` is the guard: reverting it restores the 3/3 CI
  failure in D-110, which is a real detector on CI. Acknowledged weakness — it is not a
  detector on fast local hardware. A stronger durable guard (asserting quiescence before
  any count snapshot) was NOT added; noted as a follow-up rather than claimed.
