<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Fix PR

> **Using the wrong template?**
> — Enhancement: use [enhancement.md](?template=enhancement.md)
> — Feature: use [feature.md](?template=feature.md)

---

## Linked Issue

> **Required.** This PR will be auto-closed if no valid issue link is found.

Fixes #

> The linked issue must have the `confirmed-bug` label. If it doesn't, ask a maintainer to confirm
> the bug before continuing.

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

## Checklist

- [ ] Issue linked above with `Fixes #NNN` — **PR will be auto-closed if missing**
- [ ] Linked issue has the `confirmed-bug` label
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
