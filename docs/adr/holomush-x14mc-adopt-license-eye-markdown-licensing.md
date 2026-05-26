<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Adopt license-eye and Extend SPDX Headers to Functional Markdown

**Date:** 2026-05-25
**Status:** Accepted
**Decision:** holomush-x14mc
**Deciders:** HoloMUSH Contributors

## Context

HoloMUSH's license tooling was `addlicense`, scoped to code directories
(`LICENSE_DIRS = api cmd internal pkg plugins scripts`). `addlicense` silently
exits 0 on `.md` files — it has no comment-style mapping for markdown, so it
adds no header and reports no gap. Consequently the project's functional
markdown — design specs, implementation plans, ADRs, and agent-facing rules
under `.claude/rules/` — carried no license header, even though these are
infrastructure artifacts (increasingly consumed by automated agents as
load-bearing input, not human-only prose).

Retiring `lefthook` (`holomush-gcio6`) reworked the license-header automation
anyway: lefthook's `license-headers` pre-commit hook was being removed, and the
header-insertion step had to move into `task fmt`. That made it the right moment
to decide the *tool of record* and the *coverage boundary* for SPDX headers,
rather than bolt a second markdown-only licenser alongside `addlicense`.

## Decision

Replace `addlicense` with `license-eye` (Apache SkyWalking Eyes), driven by a
root `.licenserc.yaml`, as the single tool of record for SPDX license headers.
Extend header coverage to **functional markdown** (`docs/**`, `.claude/rules/**`,
root `*.md`) using the `AngleBracket` comment style (`<!-- … -->`), while
explicitly **excluding user-facing rendered content** (`plugins/**/content/**`,
`site/docs/**` player guides) via `.licenserc.yaml` `paths-ignore`. `task fmt`
runs `license-eye header fix`; `task license:check` / CI run `license-eye header
check`.

## Rationale

- `addlicense` cannot process `.md` (silent exit 0, no header) — a different
  tool was structurally necessary to license markdown at all.
- `license-eye` produces byte-identical `// SPDX-…` headers for existing code
  files (INV-5), so the migration introduces no churn on the code surface.
- The `paths-ignore` set in `.licenserc.yaml` is the canonical, tooling-enforced
  statement of the content-vs-infrastructure-markdown boundary; future file
  additions inherit it automatically rather than relying on convention.
- One declarative config + one `task fmt` / `task license:check` surface
  eliminates the prior split between automated code headers and absent markdown
  headers.

## Alternatives Considered

**Option A: Keep `addlicense`; stamp markdown with a bespoke wrapper**

| Aspect     | Assessment                                                                                          |
| ---------- | --------------------------------------------------------------------------------------------------- |
| Strengths  | No new tool dependency; familiar invocation                                                         |
| Weaknesses | Two tools, two configs, no single exclusion boundary; bespoke wrapper needs maintenance; no md check |

**Option B: `license-eye` with `.licenserc.yaml` (chosen)**

| Aspect     | Assessment                                                                                              |
| ---------- | ------------------------------------------------------------------------------------------------------- |
| Strengths  | One tool, one config; check + fix for code and markdown; byte-identical code headers; declarative scope |
| Weaknesses | New Go tool dependency; `addlicense` removed from `task setup`; one-time large markdown-stamping diff    |

**Option C: License ALL markdown, including `site/docs/**` and plugin content**

| Aspect     | Assessment                                                                                      |
| ---------- | ----------------------------------------------------------------------------------------------- |
| Strengths  | Maximum consistency                                                                             |
| Weaknesses | Stamps machine-readable comments into user-facing/distributed copy (MOTD, player guides); no benefit |

## Consequences

**Positive:**

- Specs, plans, ADRs, and agent rules carry machine-readable authorship/license
  metadata.
- `task fmt` and `task license:check` apply uniformly to code and functional
  markdown.
- The content-vs-infrastructure boundary is explicit and tooling-enforced, not
  convention.

**Negative:**

- A new `license-eye` binary must be installed (`task setup`); contributors who
  skip `task setup` see check failures until they install it.
- The one-time stamping commit is a large mechanical diff across `docs/**` and
  `.claude/rules/**`.

**Neutral:**

- `addlicense` is removed from `task setup`; no parallel-install period.
- The `LICENSE_DIRS` Taskfile variable is superseded by `.licenserc.yaml`
  `paths`.

## References

- [Retire lefthook + license-eye Migration Design — §4 (Replace addlicense), §5 (Markdown scope)](../superpowers/specs/2026-05-25-retire-lefthook-license-eye-design.md)
- [Implementation plan — Phase 2 (Tasks 3–5)](../superpowers/plans/2026-05-25-retire-lefthook-license-eye.md)
- Parent work: bd issue `holomush-gcio6` (lefthook retirement); related pattern ADR `holomush-u2exm`
