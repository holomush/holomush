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

**Closed by:** plan 05-06 (ships `/characters/new`) — the roster fixture and the
seven specs downstream of it are repointed at that page there. `E2E Test` is a
required check protecting `main`, so this MUST be closed before the phase ships;
`task pr-prep` (fast lane) and `task test` are unaffected and green today.

**Status:** open.
