<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Feature PR

> **Using the wrong template?**
>
> - Bug fix: use [fix.md](?expand=1&template=fix.md)
> - Enhancement to existing behavior: use [enhancement.md](?expand=1&template=enhancement.md)
> - Internal maintenance, no behavior change: use [chore.md](?expand=1&template=chore.md)

---

## Linked Issue

Closes #

> **Required.** A PR with no valid issue link is closed without review, and the linked
> issue must carry the `approved-feature` label. If it is not labeled yet, close this tab
> and wait — a PR opened ahead of approval gets closed, and re-opening it later loses your
> place in the review queue.

---

## Feature summary

<!-- One paragraph. What does this feature add? Assume the reviewer has read the issue spec. -->

## What changed

### New files

<!-- List every new file added and its purpose. -->

| File | Purpose |
|------|---------|
| | |

### Modified files

<!-- List every existing file modified and what changed in it. -->

| File | What changed |
|------|-------------|
| | |

## Implementation notes

<!-- Describe any decisions made during implementation that were not specified in the issue.
     If any part of the implementation differs from the approved spec, explain why. -->

## Spec compliance

<!-- For each acceptance criterion in the linked issue, confirm it is met. Copy them here and check them off. -->

- [ ] <!-- Acceptance criterion 1 from issue -->
- [ ] <!-- Acceptance criterion 2 from issue -->
- [ ] <!-- Add all criteria from the issue -->

## Testing

### Test coverage

<!-- Describe what is tested and where. Features need new tests; they are written before the
     implementation (TDD) per CONTRIBUTING.md. -->

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

- [ ] The implementation matches the scope approved in the linked issue exactly
- [ ] No additional features, commands, or behaviors were added beyond what was approved
- [ ] If scope changed during implementation, I updated the issue spec and received re-approval

---

## Documentation

Documentation lives in `site/src/content/docs/`, organized by audience.

- [ ] Updated the relevant file(s) under `site/src/content/docs/` to reflect this feature
  - New player/operator command → `guide/` and/or `operating/`
  - New plugin capability or host function → `extending/`
  - Architectural change → `contributing/explanation/` and/or `docs/adr/`
  - New or changed invariant → `docs/architecture/invariants.yaml` (then `go run ./cmd/inv-render`)
- [ ] Proto changes regenerated and committed (`task proto && task web:generate`)
- [ ] If genuinely no documentation impact (rare for features), explain why in this PR

## Checklist

- [ ] Issue linked above with `Closes #NNN`, and it carries `approved-feature`
- [ ] All acceptance criteria from the issue are met (listed above)
- [ ] Implementation scope matches the approved spec exactly
- [ ] Tests were written before the implementation (TDD)
- [ ] New tests cover the happy path, error cases, and edge cases
- [ ] PR title follows [Conventional Commits](https://www.conventionalcommits.org/) (`type(scope): description`)
- [ ] `task fmt` run and any resulting edits committed (SPDX headers, table reflow)
- [ ] No unnecessary external dependencies added
- [ ] No `[ci skip]` / `[skip ci]` in any commit on this branch

## Breaking changes

<!-- Describe any behavior, output format, event shape, proto schema, or database schema changes
     that affect existing deployments. For each breaking change, describe the migration path.
     Write "None" only if you are certain. -->

None

## Screenshots / recordings

<!-- If this feature has any visual output or changes the player experience, include before/after
     screenshots or a short recording. Delete this section if not applicable. -->
