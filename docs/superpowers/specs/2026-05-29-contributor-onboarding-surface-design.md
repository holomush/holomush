<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Contributor Onboarding Surface — Design

**Status:** Draft
**Beads:** holomush-ec22.17 (anchor), holomush-ec22.21 (folded in)
**Date:** 2026-05-29
**Supersedes / relates:** follow-ups holomush-zao2 (testing guide), site `contributing/` IA restructure (to be filed)

## 1. Problem

The first three artifacts an external contributor encounters are broken or
misleading:

- **README.md** — 4 documentation-link occurrences (lines 89, 144, 148, 149,
  pointing at 3 unique targets) reference the pre-migration `site/docs/` tree
  (and a non-existent `contributors/` folder); the Go prerequisite says `1.23+`
  while `go.mod` is `1.26.2` and CONTRIBUTING says `1.22+` — a three-way drift
  that proves **any hardcoded version number rots after commit**.
- **CONTRIBUTING.md** — 78 lines of generic boilerplate that actively
  misdescribes the project: a `git clone` + feature-branch flow with no mention
  of the real local gate (`task pr-prep`), wrong Go version (`1.22+`), and no
  coverage / test-naming / integration-test guidance.
- **CODE_OF_CONDUCT.md and `.github/` templates** — absent. A contributor
  filing an issue or PR gets no scaffolding.

This reads as unmaintained on first contact — directly opposed to the project's
open-source goal.

## 2. Audience principle (the spine)

HoloMUSH is developed primarily by its maintainer and AI agents using an
internal toolchain: **jujutsu (jj)** workspaces, a **Dolt-backed `bd` (beads)**
tracker, and a stage-gated spec→plan→bead workflow. **External contributors
have none of this** — no `bd` server access, no jj requirement (the repo is
git-colocated, so plain `git` works end-to-end).

> **Invariant of this design:** the maintainer/agent workflow MUST NOT be
> conflated with the external-contributor workflow. CONTRIBUTING.md and the
> `.github/` surface address the **external contributor only**: standard GitHub
> fork → `git` branch → PR, GitHub **Issues** for finding/reporting work, and
> `task` targets for local checks. Maintainer/agent tooling is acknowledged
> exactly once, as explicitly **not required**.

This principle also governs *what CONTRIBUTING links to*: it MUST route
contributors only to audience-appropriate (tool-agnostic) docs, never to
jj/beads-specific maintainer pages.

## 3. Scope

**In scope (one coherent "external-contributor onboarding surface" change):**

- README.md — repoint 4 doc-link occurrences to published `holomush.dev` URLs; remove hardcoded tool versions (reference `go.mod`).
- CONTRIBUTING.md — rewrite as a thin, accurate front-door **router**.
- CODE_OF_CONDUCT.md — add (Contributor Covenant 2.1).
- `.github/` — add issue templates (bug, feature), `config.yml`, and `PULL_REQUEST_TEMPLATE.md`.
- Audience banners on `site/src/content/docs/contributing/how-to/pr-prep.md` and
  `…/sessions.md` marking them maintainer-workflow (jj+beads) pages.

**Out of scope (follow-ups, do not do here):**

- Authoring the testify-vs-ginkgo testing guide → holomush-zao2.
- Full audience-based IA restructure of the site `contributing/` section
  (separating maintainer vs contributor docs throughout) → new bead to be filed.
- README status-section accuracy beyond the Go version (e.g., "planned"
  features that have shipped) — not a link/onboarding defect.

## 4. Design

### 4a. README.md (surgical, in place)

Repoint the broken links to published URLs (all verified `200` on 2026-05-29):

| README line(s) | Old (broken) | New (published URL) |
| --- | --- | --- |
| 89, 148 | `site/docs/contributors/architecture.md` | `https://holomush.dev/contributing/explanation/architecture/` |
| 144 | `docs/reference/verifying-releases.md` | `https://holomush.dev/operating/how-to/deploy/verifying-releases/` |
| 149 | `site/docs/developers/plugin-guide.md` | `https://holomush.dev/extending/tutorials/plugin-guide/` |

(Source note: the plugin-guide page is `…/extending/tutorials/plugin-guide.mdx`
— MDX, not `.md`; Starlight renders both to the same slug. An implementer
verifying file existence MUST search for `.mdx`.)

**Remove hardcoded tool versions** rather than updating them — they are
guaranteed stale once committed (the three-way drift in §1 is the proof).
Replace `Go 1.23+` with `Go` linking to the source of truth
(`[`go.mod`](go.mod)`); drop any pinned PostgreSQL major. Leave the rest of
README's structure and prose intact.

### 4b. CONTRIBUTING.md (thin front-door router, ~50–70 lines)

A contributor must be able to go from zero to an open PR using only this file +
the published docs it links. Sections:

1. **Welcome** — Apache-2.0; contributions of all kinds welcome; find and report
   work via **GitHub Issues** (link to the issues page), not an internal tracker.
2. **Prerequisites** — Go (version pinned in [`go.mod`](go.mod)), [Task],
   PostgreSQL, Docker (for integration tests). **No hardcoded version numbers** —
   reference `go.mod` as the single source of truth so the doc cannot drift.
3. **Your first PR** — standard git: fork → `git clone` your fork → `task setup`
   → `git checkout -b` → make changes (TDD) → `task lint && task test` →
   `task pr-prep` (note: "CI runs the full suite including integration and E2E")
   → push to your fork → open a PR.
4. **What we expect** — conventional-commit PR titles, tests-first, >80%
   coverage, ACE test names — each **linked** to the canonical page rather than
   restated:
   - Pull Request Guide → `https://holomush.dev/contributing/how-to/pr-guide/`
   - Coding standards → `https://holomush.dev/contributing/reference/coding-standards/`
   - Integration tests → `https://holomush.dev/contributing/how-to/integration-tests/`
   - System architecture → `https://holomush.dev/contributing/explanation/architecture/`
   - Plugin authoring → `https://holomush.dev/extending/tutorials/plugin-guide/`
5. **How we develop (optional — not required for contributors)** — one short
   paragraph: HoloMUSH is developed heavily with AI agents using jj and an
   internal issue tracker; **you need none of it** — standard git, GitHub
   Issues, and GitHub PRs are fully supported and all we ask. (Explains the
   otherwise-confusing `CLAUDE.md`, `.jj/`, and agent-authored commits.)
6. **Code of Conduct** — link to `CODE_OF_CONDUCT.md`.
7. **License** — inbound=outbound Apache-2.0 (retain).

`task pr-prep` is named as the local gate but the jj/beads-flavored
`pr-prep.md` page is **not** linked raw; a one-line caveat notes its examples
use the maintainer jj flow while the `task` itself works identically under git.
`sessions.md` (jj workspaces) is **never** linked from CONTRIBUTING.

### 4c. CODE_OF_CONDUCT.md

Adopt **Contributor Covenant 2.1** verbatim (the de-facto OSS standard), with
the enforcement/reporting contact set to the dedicated role alias
**`conduct@holomush.dev`** (decoupled from any individual; the user confirms the
address is provisionable on the domain).

### 4d. `.github/` templates

- `.github/ISSUE_TEMPLATE/bug_report.yml` — GitHub issue **form**: summary,
  reproduction, expected vs actual, version/commit, environment.
- `.github/ISSUE_TEMPLATE/feature_request.yml` — issue form: problem, proposed
  solution, alternatives.
- `.github/ISSUE_TEMPLATE/config.yml` — `blank_issues_enabled: true` (stay
  low-friction). Contact links: **no Security link** (`SECURITY.md` confirmed
  absent, 2026-05-29); add a **Discussions** contact link only if Discussions is
  enabled on the repo (verify with `gh repo view --json hasDiscussionsEnabled`
  run from the main repo dir at implementation; if disabled or unverifiable,
  omit `contact_links` entirely — a bare `blank_issues_enabled: true` is valid).
- `.github/PULL_REQUEST_TEMPLATE.md` — audience-neutral (used by contributors
  *and* maintainers): summary, linked issue, and a short checklist (tests pass,
  `task pr-prep` run, conventional-commit title). No jj/beads specifics.

### 4e. Audience banners (site docs)

Prepend a short admonition to `pr-prep.md` and `sessions.md` (Starlight `:::note`
or equivalent) identifying them as **maintainer workflow** pages assuming jj +
the internal tracker, and pointing external contributors to CONTRIBUTING.md.
This keeps the CONTRIBUTING router honest without restructuring the site IA.

## 5. Invariants (RFC2119, verifiable)

- **INV-1** — README MUST NOT contain any link to the legacy `site/docs/` tree
  or `docs/reference/` paths. Every documentation link in README MUST be a
  published `holomush.dev` URL that resolves `200`. *(check: the "no legacy
  path" clause is `rg`-verifiable and CI-enforceable; the "resolves `200`"
  clause is a **manual pre-merge gate** — `task pr-prep:docs` (markdownlint/
  rumdl) does NOT perform HTTP resolution on external URLs, and no
  `lychee`/link-checker is wired in. **Accepted tradeoff:** published-URL links
  can rot silently post-merge with no automated detector; this is the cost of
  reader-facing URLs over repo-relative paths. Mitigation: verify each URL by
  `curl -I` before merge.)*
- **INV-2** — README and CONTRIBUTING MUST NOT hardcode a Go (or other tool)
  version number; the Go prerequisite MUST reference `go.mod` as the source of
  truth. *(check: `rg -n "[Gg]o 1\.[0-9]+|PostgreSQL [0-9]" README.md
  CONTRIBUTING.md` → 0 hits)*
- **INV-3** — CONTRIBUTING.md MUST NOT present `jj`, `bd`/beads, or `jj
  workspace` commands as contributor steps. Any mention of them MUST appear
  only within the "How we develop" section, framed as not-required.
  *(check: `rg` for `^\s*(jj|bd)` command lines = 0)*
- **INV-4** — `CODE_OF_CONDUCT.md` MUST exist at repo root and CONTRIBUTING's
  Code of Conduct section MUST link to it. *(check: file exists + `rg` link)*
- **INV-5** — `.github/ISSUE_TEMPLATE/` MUST contain a bug and a feature
  template, and `.github/PULL_REQUEST_TEMPLATE.md` MUST exist. *(check: `fd`)*
- **INV-6** — CONTRIBUTING MUST NOT link `sessions.md`; `pr-prep.md` MUST be
  referenced only as the `task pr-prep` gate with the maintainer-flow caveat.
  *(check: `rg`)*
- **INV-7** — `pr-prep.md` and `sessions.md` MUST each carry an audience banner
  identifying them as maintainer (jj+beads) workflow pages. *(check: `rg`)*

## 6. Verification

- `task pr-prep` (docs lane: markdown lint, link checks, fmt, license headers) green.
- Manual `curl -I` (or browser) confirms every new README/CONTRIBUTING URL resolves `200`.
- `rg "site/docs/|docs/reference/" README.md` → 0 hits (INV-1).
- `rg -n "[Gg]o 1\.[0-9]+|PostgreSQL [0-9]" README.md CONTRIBUTING.md` → 0 hits
  (INV-2: no hardcoded tool versions).
- License headers present on new `.md`/`.yml` files where applicable (`task license:check`).

## 7. Resolved / confirmed-at-implementation decisions

1. **CoC enforcement contact** — RESOLVED: dedicated alias
   `conduct@holomush.dev` (see §4c).
2. **`config.yml` contact links** — Security link: NO (`SECURITY.md` absent,
   confirmed 2026-05-29). Discussions link: verify `hasDiscussionsEnabled` at
   implementation; omit if disabled/unverifiable (see §4d).

## 8. Follow-ups (file as separate beads)

- **holomush-zao2** — author the testify-vs-ginkgo contributor testing guide;
  once it exists, add it to the CONTRIBUTING router.
- **(new)** — audience-based IA restructure of the site `contributing/` section:
  cleanly separate maintainer-workflow docs (jj, beads, pr-prep, sessions) from
  contributor-facing docs throughout, rather than relying on per-page banners.
