<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Enhancement PR

> **Using the wrong template?**
> — Bug fix: use [fix.md](?template=fix.md)
> — New feature: use [feature.md](?template=feature.md)
> — Internal maintenance, no behavior change: use [chore.md](?template=chore.md)

---

## Linked Issue

> **Required.** This PR will be auto-closed if no valid issue link is found.
> The linked issue **must** have the `approved-enhancement` label. If it does not, this PR will be
> closed without review.

Closes #

> ⛔ **No `approved-enhancement` label on the issue = immediate close.**
> Do not open this PR if a maintainer has not yet approved the enhancement proposal.

---

## What this enhancement improves

<!-- Name the specific command, event, RPC, or behavior being improved. -->

## Before / After

**Before:**
<!-- Describe or show the current behavior. Include example output if applicable. -->

**After:**
<!-- Describe or show the behavior after this enhancement. Include example output if applicable. -->

## How it was implemented

<!-- Brief description of the approach. Point to the key files changed. -->

## Testing

### How I verified the enhancement works

<!-- Manual steps or automated tests. -->

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

### Surfaces tested

- [ ] Telnet
- [ ] Web client (PWA)
- [ ] gRPC / ConnectRPC API
- [ ] holomush CLI
- [ ] N/A (not surface-specific)

---

## Scope confirmation

<!-- Confirm the implementation matches the approved proposal. -->

- [ ] The implementation matches the scope approved in the linked issue — no additions or removals
- [ ] If scope changed during implementation, I updated the issue and got re-approval before continuing

---

## Documentation

Documentation lives in `site/src/content/docs/`, organized by audience.

- [ ] Updated the relevant file(s) under `site/src/content/docs/` to reflect this change
  - Behavior or output change → `guide/` and/or `operating/`
  - Plugin-facing change → `extending/`
  - Architectural change → `contributing/explanation/` and/or `docs/adr/`
  - Changed invariant → `docs/architecture/invariants.yaml` (then `go run ./cmd/inv-render`)
- [ ] Proto changes regenerated and committed (`task proto && task web:generate`)
- [ ] If genuinely no documentation impact (internal refactor, test-only), explain why in this PR

## Checklist

- [ ] Issue linked above with `Closes #NNN` — **PR will be auto-closed if missing**
- [ ] Linked issue has the `approved-enhancement` label — **PR will be closed if missing**
- [ ] Changes are scoped to the approved enhancement — nothing extra included
- [ ] New or updated tests cover the enhanced behavior
- [ ] PR title follows [Conventional Commits](https://www.conventionalcommits.org/) (`type(scope): description`)
- [ ] `task fmt` run and any resulting edits committed (SPDX headers, table reflow)
- [ ] No unnecessary dependencies added
- [ ] No `[ci skip]` / `[skip ci]` in any commit on this branch

## Breaking changes

<!-- Does this enhancement change any existing behavior, output format, event shape, or proto schema?
     If yes, describe exactly what changes and confirm backward compatibility.
     Write "None" if not applicable. -->

None
