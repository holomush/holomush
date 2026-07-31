<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Enhancement PR

> **Using the wrong template?**
>
> - Bug fix: use [fix.md](?expand=1&template=fix.md)
> - New feature: use [feature.md](?expand=1&template=feature.md)
> - Internal maintenance, no behavior change: use [chore.md](?expand=1&template=chore.md)

---

## Linked Issue

Closes #

> **Required.** A PR with no valid issue link is closed without review, and the linked
> issue must carry the `approved-enhancement` label. If it is not labeled yet, close this
> tab and wait — a PR opened ahead of approval gets closed, and re-opening it later loses
> your place in the review queue.
>
> The `Issue Gate` check enforces this automatically: it applies the `gate-violation`
> label, comments with the reason, and goes red, blocking merge. It never closes the PR —
> closing stays a maintainer decision.

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

### Checks run

- [ ] `task pr-prep` run locally and green — **this is the gate**
- [ ] `task test:int` / `task test:e2e` — only if your change touches those surfaces (CI runs both regardless)

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
- [ ] If genuinely no documentation impact (the improvement is not observable in any doc), explain why in this PR

## Checklist

- [ ] Issue linked above with `Closes #NNN`, and it carries `approved-enhancement`
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
