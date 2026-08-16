# Deferred items — phase 06.1

Out-of-scope discoveries logged during execution. These are NOT fixed here.

## E2E flake: admin-portal.spec.ts:293 (plan 06.1-04's Escape/close/Cancel test)

Discovered while running plan 06.1-05's full-suite gate.

**Symptom.** Under the FULL `task test:e2e` suite, the 375px phone-band test
`closes on Escape, on the close control and on Cancel` fails intermittently
inside `tapRowOutsideNameCell` — either `expect(hit.tag).toBe('BUTTON')` receives
`HEADER`, or `scrollIntoViewIfNeeded` reports "Element is not attached to the
DOM". Both are the same race: the debounced search re-renders the table between
the scroll and the hit test.

**Frequency observed.** Four full-suite runs on the same tree: one 129/0 green,
three 128/1 red (the red ones split between this test and, once, the D-110
mutation-loop test in the same file).

**Not caused by plan 06.1-05.** Reproduced with plan 05's new
`admin portal — the band boundaries` describe block marked `.skip`, at
126 passed / 1 failed — same test, same assertion. Scoped runs
(`task test:e2e -- admin-portal.spec.ts`) are 14/14 green every time.

**Shape of a fix (not attempted here).** `tapRowOutsideNameCell` needs the same
retry-the-whole-gesture treatment `clickRowAction` already carries: wrap the
scroll + rect read + hit test + click in one `expect(async () => {…}).toPass()`
so a mid-gesture re-render retries the gesture rather than failing it. It is in
this file's own helper, so it is a 06.1-04 surface, not a 06.1-05 one.

Also recorded in `.planning/WINDOWS.md`.
