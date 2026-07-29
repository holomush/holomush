<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Chore PR

> **Using the wrong template?**
> — Bug fix: use [fix.md](?template=fix.md)
> — Enhancement to existing behavior: use [enhancement.md](?template=enhancement.md)
> — New feature: use [feature.md](?template=feature.md)

---

## Linked Issue

> **Required.** This PR will be auto-closed if no valid issue link is found.

Closes #

> The linked issue must have the `approved-chore` label. If it doesn't, ask a maintainer to
> triage the chore before continuing.

---

## What this chore changes

<!-- One or two sentences. What internal improvement does this make? -->

## Current state → after

**Before:**
<!-- The debt, gap, or inconsistency as it stands. Include numbers where useful
     (file count, coverage %, build time, dependency version). -->

**After:**
<!-- The state once this lands. -->

## Why now

<!-- Brief. What does this unblock, or what risk does it retire?
     Skip for routine dependency bumps and mechanical sweeps. -->

## Testing

### How I verified nothing changed behaviorally

<!-- For mechanical or refactoring work, say how you established equivalence —
     existing tests, a negative control, a before/after comparison. -->

### Test tiers run

- [ ] `task test` (unit)
- [ ] `task test:int` (integration — requires Docker)
- [ ] `task test:e2e` (Playwright)
- [ ] `task pr-prep` run locally and green

### Platforms tested

- [ ] macOS
- [ ] Linux
- [ ] Windows / WSL
- [ ] N/A (not platform-specific)

---

## Scope confirmation

- [ ] This changes **no** user-facing behavior (commands, output, event shapes, proto schemas, config)
- [ ] Changes are scoped to the triaged chore — nothing extra included
- [ ] If this turned out to change user-facing behavior, I refiled it as an enhancement or feature

---

## Documentation

- [ ] Updated any contributor-facing docs this affects (`site/src/content/docs/contributing/`, `.claude/rules/`)
- [ ] If a new or changed invariant is involved, updated `docs/architecture/invariants.yaml` and ran `go run ./cmd/inv-render`
- [ ] N/A — no documentation impact

## Checklist

- [ ] Issue linked above with `Closes #NNN` — **PR will be auto-closed if missing**
- [ ] Linked issue has the `approved-chore` label
- [ ] Every acceptance condition from the issue's "Done when" is met
- [ ] PR title follows [Conventional Commits](https://www.conventionalcommits.org/) (`type(scope): description`)
- [ ] `task fmt` run and any resulting edits committed (SPDX headers, table reflow)
- [ ] No unnecessary dependencies added
- [ ] No `[ci skip]` / `[skip ci]` in any commit on this branch

## Breaking changes

<!-- A chore should have none by definition. If this one does, it is not a chore —
     refile it as an enhancement or feature. -->

None
