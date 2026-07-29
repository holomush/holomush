<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Fix PR

> **Using the wrong template?**
>
> - Enhancement: use [enhancement.md](?expand=1&template=enhancement.md)
> - Feature: use [feature.md](?expand=1&template=feature.md)
> - Internal maintenance, no behavior change: use [chore.md](?expand=1&template=chore.md)

---

## Linked Issue

Fixes #

> **Required.** A PR with no valid issue link is closed without review. The linked issue
> must carry the `confirmed-bug` label — if it does not, ask a maintainer to confirm the
> bug before continuing.

---

## What was broken

<!-- One or two sentences. What was the incorrect behavior? -->

## What this fix does

<!-- One or two sentences. How does this fix the broken behavior? -->

## Root cause

<!-- Brief explanation of why the bug existed. Skip for trivial typo/doc fixes. -->

## Testing

### How I verified the fix

<!-- Describe manual steps or point to the automated test that proves this is fixed. -->

### Regression test added?

- [ ] Yes — added a test that fails without the fix and passes with it
- [ ] No — explain why: <!-- e.g., environment-specific, non-deterministic -->

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

## Checklist

- [ ] Issue linked above with `Fixes #NNN`, and it carries `confirmed-bug`
- [ ] Fix is scoped to the reported bug — no unrelated changes included
- [ ] Regression test added (or explained why not)
- [ ] PR title follows [Conventional Commits](https://www.conventionalcommits.org/) (`type(scope): description`)
- [ ] `task fmt` run and any resulting edits committed (SPDX headers, table reflow)
- [ ] No unnecessary dependencies added
- [ ] No `[ci skip]` / `[skip ci]` in any commit on this branch

## Breaking changes

<!-- Does this fix change any existing behavior, output format, event shape, or proto schema that
     deployments might depend on? If yes, describe. Write "None" if not applicable. -->

None
