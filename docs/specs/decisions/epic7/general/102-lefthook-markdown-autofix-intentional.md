<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Decision #102: Lefthook `fmt-markdown` Auto-Fix Is Intentional

**Date:** 2026-02-10
**Phase:** General (infrastructure)
**Status:** Accepted

## Context

The project's lefthook pre-commit configuration includes a `fmt-markdown` hook
that was changed from check-only mode (fail if violations found) to auto-fix mode
(automatically correct violations). During code review, this change was flagged as
potentially unintended.

## Decision

The change from check-only to auto-fix mode for the `fmt-markdown` hook is
**intentional and by design**.

## Rationale

1. **Developer experience:** Auto-fix mode eliminates friction for contributors.
   Markdown formatting is purely mechanical (pipe alignment, indentation) — there's
   no semantic judgment required. Automatic correction prevents blocking commits
   on trivial formatting issues.

2. **Prevents CI failures:** With check-only mode, contributors must manually run
   `task fmt` before committing. This creates a failure point in CI. Auto-fix mode
   removes this cognitive load and ensures consistency without manual steps.

3. **Deterministic output:** The auto-fix uses `dprint` which produces
   deterministic formatting. The format is stable and reproducible across systems.

4. **Lint then auto-fix pattern:** Other checks in the pipeline (lint, test, etc.)
   are still check-only. Markdown formatting is distinct: it's purely cosmetic and
   mechanical, not a code quality issue. Auto-fixing cosmetics while checking
   quality is a reasonable split.

5. **Documentation precedent:** HoloMUSH documentation guidelines
recommend running `task fmt` to align tables. Auto-fixing in the commit hook
aligns with this guidance — the hook performs what the guidelines recommend.

6. **Consistency with project goals:** The full ABAC specification and all
   implementation plans use standardized table formatting for requirements,
   dependencies, and decision matrices. Auto-fix ensures this consistency is
   maintained across all contributions.

## Consequences

- All markdown files are automatically formatted on commit (tables aligned, spacing
  consistent, etc.)
- Contributors no longer need to manually run `task fmt` before commit
- CI passes reliably without formatting-related failures
- All merged documentation follows consistent formatting standards
- Pre-commit performance impact is minimal (dprint is fast)

## Cross-references

- **Documentation Guidelines** — Markdown standards and `task fmt` recommendation
- Lefthook configuration — `.lefthook.yaml` and `.lefthook-local.yaml`
