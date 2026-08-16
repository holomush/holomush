# Deferred items — phase 05

Discoveries made during execution that are out of the discovering plan's scope.
Each names the plan that closes it.

## From plan 05-03

### Eight Playwright specs drive the deleted inline create form

**Found during:** Task 2, after deleting the inline create card from
`web/src/routes/(authed)/characters/+page.svelte`.

**What breaks:** the specs below locate `text=Create New Character` and then fill
`input[name="characterName"]`. Both are gone — the card is now a link to
`/characters/new`, and that page does not exist yet.

| File | Lines |
| --- | --- |
| `web/e2e/helpers/fixtures.ts` | 102-103 (the shared character-creation fixture, so every spec using it is affected) |
| `web/e2e/auth.spec.ts` | 104-107, 174-177, 244-… |
| `web/e2e/admin.spec.ts` | 38-41 |
| `web/e2e/negative-journeys.spec.ts` | 86-87, 272-274, 282-283 |
| `web/e2e/character-switcher.spec.ts` | 29-30 |
| `web/e2e/session-security.spec.ts` | 37-40 |

**Why it is not fixed here.** The replacement surface is `/characters/new`, which
is plan **05-06**'s entire deliverable. Rewriting eight specs to drive a page
that does not exist would produce eight tests that fail for a different reason,
and rewriting them twice is worse than rewriting them once.

**Why it is NOT quarantined.** `.claude/rules/testing.md`: quarantine is for
flakiness with an open GitHub issue and no reproducible cause. The cause here is
known and deterministic, so quarantining it would hide a real, expected breakage
behind a mechanism that exists for a different problem.

**Closed by:** plan **05-08**. 05-06 shipped the replacement surface
(`/characters/new`) and 05-07 the sectioned roster; 05-08 repointed the specs at
them, because rewriting them against a page that did not yet exist would have
produced eight tests failing for a different reason.

**How it closed.** `registerAndEnterTerminal` was split into `registerPlayer`,
`createCharacter` and `enterGameAs` in `web/e2e/helpers/fixtures.ts`, and the
five spec-local copies of the creation journey now call those. Three behavioural
changes were absorbed: creation lands on `/characters` rather than `/terminal`
(so every caller gained an explicit `enterGameAs` step), the auto-enter checkbox
is gone, and the roster `h1` is `Your characters`. `input[name="characterName"]`
was deliberately preserved by 05-06, so no field selector changed. The
name-collision assertion in `negative-journeys.spec.ts` moved from the server's
`already taken` string to the authored `That name is taken. Try another.`, with a
negative check that the server's own wording is absent — `/characters/new`
classifies `AlreadyExists` by code and never renders the server's sentence.

**Verified:** `task test:e2e` (whole suite, unscoped) exits **0** — 115 passed,
0 failed, 1 skipped, 116 total. Nothing was quarantined.

**Status:** closed.

## Deferred Items

<!--
The machine-readable ledger `audit-uat` reads. Everything above is the human
narrative; this section is what `uat.cjs::parseDeferredItems` parses.

Two things matter about the heading itself. It BOUNDS the scan: without a level-2
`Deferred Items` heading the parser falls back to treating the whole file as the
section body, and then `parseDeferredTableItems` surfaces every GFM table row —
which is why the `File | Lines` table above was reported as six outstanding items.
And it is the only place a `status:` field is honored: closure requires the exact
token `resolved` (or `done`/`pass` in a table cell). `**Status:** closed.` above is
prose — `closed` is not in that vocabulary and never closed anything.
-->

- item: Eight Playwright specs drive the deleted inline create form
  status: resolved
  resolved_by: plan 05-08
  resolved_at: 2026-08-16
  evidence: "`task test:e2e` exit 0 — 115 passed, 0 failed, 1 skipped. `/characters/new` exists; zero specs reference `Create New Character`; the surviving `input[name=\"characterName\"]` fills are the /register flow, deliberately preserved by 05-06."
