<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Chore PR

> **Using the wrong template?**
>
> - Bug fix: use [fix.md](?expand=1&template=fix.md)
> - Enhancement to existing behavior: use [enhancement.md](?expand=1&template=enhancement.md)
> - New feature: use [feature.md](?expand=1&template=feature.md)

---

## Linked Issue

Closes #

> **Required.** A PR with no valid issue link is closed without review. The linked issue
> must carry the `approved-chore` label — if it does not, ask a maintainer to triage the
> chore before continuing.
>
> Dependency-only, repo-config-only (`.github/**`, but **not** `CODEOWNERS`), and
> documentation-only PRs are exempt from the gate entirely and do not use this template.
> `Taskfile.yaml` and `scripts/**` are **not** exempt — except a file under `scripts/`
> matching one of the listed **lockfile** shapes, which is exempt; a manifest there is
> not, and neither is a lockfile whose shape is unlisted. Full path lists:
> [CONTRIBUTING.md](https://github.com/holomush/holomush/blob/main/CONTRIBUTING.md#exempt-by-file-path).

---

## What this chore changes

<!-- One or two sentences. What internal improvement does this make? -->

## Current state → after

**Before:**
<!-- The debt, gap, or inconsistency as it stands. Include numbers where useful
     (file count, coverage %, build time). -->

**After:**
<!-- The state once this lands. -->

## Why now

<!-- Brief. What does this unblock, or what risk does it retire?
     Skip for mechanical sweeps. -->

## Testing

### How I verified nothing changed behaviorally

<!-- For mechanical or refactoring work, say how you established equivalence —
     existing tests, a negative control, a before/after comparison. -->

### Checks run

- [ ] `task pr-prep` run locally and green — **this is the gate**
- [ ] `task test:int` / `task test:e2e` — only if your change touches those surfaces (CI runs both regardless)

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

- [ ] Updated any contributor-facing docs this affects (`site/src/content/docs/contributing/`)
- [ ] If a new or changed invariant is involved, updated `docs/architecture/invariants.yaml` and ran `go run ./cmd/inv-render`
- [ ] N/A — no documentation impact

## Checklist

- [ ] Issue linked above with `Closes #NNN`, and it carries `approved-chore`
- [ ] Every acceptance condition from the issue's "Done when" is met
- [ ] PR title follows [Conventional Commits](https://www.conventionalcommits.org/) (`type(scope): description`)
- [ ] `task fmt` run and any resulting edits committed (SPDX headers, table reflow)
- [ ] No unnecessary dependencies added
- [ ] No `[ci skip]` / `[skip ci]` in any commit on this branch

## Breaking changes

<!-- A chore should have none by definition. If this one does, it is not a chore —
     refile it as an enhancement or feature. -->

None
