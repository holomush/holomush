<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Contributing to HoloMUSH

Thanks for your interest in contributing. HoloMUSH is open source (Apache-2.0) and takes
code, documentation, bug reports, and ideas.

One thing to know before you start: **work begins with an issue, not a pull request.**
Open an issue, wait for a maintainer to sign off, then write the code. It is a real
constraint and we hold everyone to it, including maintainers. It exists so nobody spends a
weekend on a change that was never going to land. A PR that shows up without an approved
issue gets closed, and that is a bad outcome for you, so please start with the issue.
Details in [The Issue-First Rule](#the-issue-first-rule--no-exceptions).

A caution about the backlog: nearly all of the 200+ open issues were filed by the
maintainer as working notes, so they assume a lot of context and are not curated for
newcomers. If you want to help and do not have something specific in mind, open a
[Discussion](https://github.com/holomush/holomush/discussions) and ask — that is a faster
route in than reading the issue list.

## Prerequisites

- Go — the required version is pinned in [`go.mod`](go.mod)
- [Task](https://taskfile.dev/) (the task runner this project uses)
- Docker — required. Some unit tests stand up a Postgres container, and the integration
  and E2E suites need the full stack. The Compose file provides PostgreSQL, so you do not
  need to install it separately.

## Types of contributions

HoloMUSH accepts four types of contributions. Each has a different process and a
different bar for acceptance. **Read this section before opening anything.**

Anything that is not one of the four — a question, a half-formed idea, a "would you be
open to…" — starts as a [Discussion](https://github.com/holomush/holomush/discussions).
Blank issues are disabled, so the five templates are the only way in.

### Fix (bug report)

A fix corrects something that is broken, crashes, produces wrong output, or behaves
contrary to documented behavior.

**Process:**

1. Open a [Bug Report issue](https://github.com/holomush/holomush/issues/new?template=bug_report.yml) — fill it out completely.
2. Wait for a maintainer to confirm it is a bug (label: `confirmed-bug`). For obvious, reproducible bugs this is typically fast.
3. Fix it. Write a test that fails without the fix and passes with it.
4. Open a PR using the [Fix PR template](.github/PULL_REQUEST_TEMPLATE/fix.md) — link the confirmed issue with `Fixes #NNN`.

**Why these get declined:** not reproducible, works-as-designed, duplicate of an existing issue.

---

### Enhancement

An enhancement improves an existing feature — better output, faster execution, cleaner
UX, expanded edge-case handling. It does **not** add new commands, event types, RPCs,
or concepts.

**The bar:** enhancements need a scoped written proposal approved by a maintainer before
any code is written. A PR for an enhancement is closed without review if the linked issue
does not carry the `approved-enhancement` label.

**Process:**

1. Open an [Enhancement issue](https://github.com/holomush/holomush/issues/new?template=enhancement.yml) with the full proposal. The template requires: the problem being solved, the concrete benefit, the scope of changes, and alternatives considered.
2. **Wait for maintainer approval.** A maintainer must label the issue `approved-enhancement` before you write a single line of code.
3. Write the code. Keep the scope exactly as approved. If scope creep occurs, comment on the issue and get re-approval before continuing.
4. Open a PR using the [Enhancement PR template](.github/PULL_REQUEST_TEMPLATE/enhancement.md) — link the approved issue with `Closes #NNN`.

**Why these get declined:** issue not labeled `approved-enhancement`, scope exceeds what was approved, no written proposal, duplicates existing behavior.

---

### Feature

A feature adds something new — a command, event type, plugin capability, RPC, or client
surface. Features have the highest bar because they add permanent maintenance burden.

**The bar:** features require a complete written specification approved by a maintainer
before any code is written. A PR for a feature is closed without review if the linked
issue does not carry the `approved-feature` label. Maintainers do not fill in an
incomplete spec; they close it.

**Process:**

1. **Discuss first** — check [Discussions](https://github.com/holomush/holomush/discussions) to see if the idea has been raised. If it was raised and declined, don't open a new issue.
2. Open a [Feature Request issue](https://github.com/holomush/holomush/issues/new?template=feature_request.yml) with the complete spec. The template requires: the problem being solved and for whom, what is being added, full scope of affected files and systems, user stories, acceptance criteria, breaking-change assessment, and maintenance burden.
3. **Wait for maintainer approval.** A maintainer must label the issue `approved-feature` before you write a single line of code. Approval is not guaranteed — many valid ideas are declined because they conflict with the project's architecture.
4. Write the code. Implement exactly the approved spec. Scope changes require re-approval.
5. Open a PR using the [Feature PR template](.github/PULL_REQUEST_TEMPLATE/feature.md) — link the approved issue with `Closes #NNN`.

**Why these get declined:** issue not labeled `approved-feature`, spec is incomplete, scope exceeds what was approved, maintenance burden too high.

---

### Chore / maintenance

Internal work that improves project health without changing user-facing behavior —
refactoring, test quality, tech debt, and the tooling around them.

**The bar:** lower than a feature or enhancement — a chore needs a triaged issue, not a
design review. But it still needs one, because "no user-facing change" is a claim a
maintainer should agree with before you invest the work.

**Already exempt?** Dependency-only updates confined to the listed dependency paths,
repo-config-only changes under `.github/**`, and documentation-only changes bypass the
issue-first gate entirely (see [Exempt by file path](#exempt-by-file-path)) — open those PRs
directly, with no chore intake. File a chore issue for them only if you want the work
tracked. Maintenance that touches product code — refactoring, test quality, tech debt — does
need intake, and so do `Taskfile.yaml` and `scripts/**` — except a **lockfile** under
`scripts/` matching the dependency-only shapes, which is exempt.

**Process:**

1. Open a [Chore issue](https://github.com/holomush/holomush/issues/new?template=chore.yml) describing the current state, proposed work, and what "done" means.
2. **Wait for maintainer triage.** A maintainer applies `approved-chore` once the scope is agreed.
3. Open a PR using the [Chore PR template](.github/PULL_REQUEST_TEMPLATE/chore.md) — link the approved issue with `Closes #NNN`.

If the work turns out to change user-facing behavior, it is not a chore — refile it as an
enhancement or a feature.

**Why these get declined:** issue not labeled `approved-chore`, the change alters user-facing behavior, scope exceeds what was triaged.

Documentation content problems use the
[Documentation Issue template](https://github.com/holomush/holomush/issues/new?template=docs_issue.yml).

---

## The Issue-First Rule — No Exceptions

> **No code before approval.**

- For **fixes**: open the issue, get `confirmed-bug`, then fix it.
- For **enhancements**: open the issue, get `approved-enhancement`, then code.
- For **features**: open the issue, get `approved-feature`, then code.
- For **chores**: open the issue, get `approved-chore`, then code.

PRs that arrive without a properly-labeled linked issue are closed. The point is to keep
you from spending a weekend on something that was never going to be merged, and to keep
maintainers from reviewing code for a change nobody agreed to.

This rule binds everyone, including maintainers and AI-agent-driven work.

### Exempt by file path

Three kinds of PR skip the typed template and the issue-first gate:

- **Documentation-only** — the diff is confined to `site/**`, `docs/**`, `**/*.md`,
  `.planning/**`, `.claude/{agents,commands,rules,agent-memory}/**`, `LICENSE`, or
  `LICENSE_HEADER`. This list mirrors `DOCS_ONLY_PATHS` in
  [`Taskfile.yaml`](Taskfile.yaml), which is the authoritative version.
- **Dependency-only** — the diff is confined to dependency manifests and lockfiles, defined
  by file *shape* rather than by a fixed enumeration: `**/go.mod`, `**/go.sum`,
  `**/package.json`, `**/pnpm-lock.yaml`, `**/pnpm-workspace.yaml`, `**/bun.lock`,
  `**/uv.lock`, `compose*.yaml`, `Dockerfile`, or `**/Dockerfile`. This list mirrors
  `DEPENDENCY_ONLY_PATHS` in [`Taskfile.yaml`](Taskfile.yaml), which is the authoritative
  version. The rule is path-derived: a Renovate PR is exempt if and only if its diff stays
  inside those shapes — being a Renovate PR is not itself an exemption. In particular, a
  dependency PR that **also** carries regenerated code (`pkg/proto/**/*.pb.go`,
  `web/**/*_pb.ts`) is **not** exempt. The buf codegen bumps in `.github/renovate.json` set
  `automerge: false` precisely so a human runs `task proto` / `task web:generate` and
  commits the regenerated stubs on those PRs — those are real source diffs.
- **Repo configuration-only** — the diff is confined to `.github/**`: workflows, composite
  actions, the issue and PR templates, and Renovate config. One carve-out inside that tree:
  if a `CODEOWNERS` file is ever added it is **not** exempt, because changing review
  ownership is a governance decision.

`Taskfile.yaml` and `scripts/**` are deliberately **not** exempt, with one lockfile
carve-out. They define `task pr-prep` and the checks CI runs, so changing them changes the
gate itself — that needs a chore issue like any other maintenance work. The carve-out: a
**lockfile** under `scripts/` matching the dependency-only shapes above (for example
`scripts/uv.lock`) **is** exempt, because a lockfile is not a gate definition. Any
non-lockfile change under `scripts/` stays gated.

If your diff touches anything outside those paths, it is not exempt — you still need a
linked, approved issue.

For a cross-cutting PR where no typed template fits the *shape* of the change, add
`<!-- pr-template-exempt: your reason here -->` to the PR body with a real reason and use
the closest template. The marker explains a template mismatch only. It is informational, it
does not waive the issue-first gate, and a maintainer may reject the claim.

This exemption takes precedence over the chore route above: a PR touching only exempt paths
needs no issue and no typed template, even when the work would otherwise read as a chore.

> **Transitional note:** issues filed before this policy was adopted predate the gate
> labels and are being triaged retroactively. A missing gate label on an older issue is a
> backlog artifact, not an approval.

### Labels used by the gate

| Label | Meaning |
| --- | --- |
| `needs-triage` | Filed, awaiting maintainer triage (applied automatically by the bug, chore, and docs templates) |
| `needs-review` | Proposal filed, awaiting maintainer review (applied automatically by the feature and enhancement templates) |
| `confirmed-bug` | Maintainer confirmed this is a real bug — a fix PR may be opened |
| `approved-feature` | Feature spec approved — implementation may begin |
| `approved-enhancement` | Enhancement proposal approved — implementation may begin |
| `approved-chore` | Chore triaged and accepted — implementation may begin |
| `gate-violation` | PR opened without a linked, approved issue |

Each issue template also applies a type label on filing: `bug`, `enhancement`,
`feature-request`, `type: chore`, or `documentation`.

## Your first pull request

HoloMUSH uses a standard GitHub fork-and-pull-request workflow. The only addition is the
issue-first step.

```bash
# 1. Open (or find) an issue and wait for the approval label — see above.

# 2. Fork the repo on GitHub, then clone YOUR fork
git clone https://github.com/<your-username>/holomush.git
cd holomush

# 3. Install the dev tools. `task setup` uses Homebrew; on Linux or WSL install the
#    equivalents by hand — the pinned Go tools live in the go.tool*.mod modules.
task setup

# 4. Create a branch
git checkout -b fix/1234-exit-list-truncated

# 5. Make your change test-first, then run the local gate
task pr-prep        # lint, format, unit tests, build — the gate that matters

# 6. Push to your fork and open a pull request using a typed template
git push -u origin fix/1234-exit-list-truncated
```

All PRs target `main` and are squash-merged. CI runs the full suite — lint, unit,
integration, and E2E — on any PR that touches code. Documentation-only PRs skip it; a
stand-in job reports the required checks so they can still merge.

**No draft PRs.** Open a PR only when the code is complete, `task pr-prep` is green, and
the correct typed template is used.

**Never use `[ci skip]` / `[skip ci]`** in any commit on a branch with an open PR — it
suppresses the required status checks for that commit, leaving the PR blocked with nothing
visibly failing.

## What we expect

A few conventions keep the codebase coherent. Rather than restating them here
(where they'd drift), see the canonical docs:

- **[Pull Request Guide](https://holomush.dev/contributing/how-to/pr-guide/)** —
  review and merge process; conventional-commit titles.
- **[Coding standards](https://holomush.dev/contributing/reference/coding-standards/)** —
  style, error handling, test naming, and the coverage bar.
- **[Integration tests](https://holomush.dev/contributing/how-to/integration-tests/)** —
  how the Docker-backed integration suite works.
- **[System architecture](https://holomush.dev/contributing/explanation/architecture/)** —
  how the pieces fit together.
- **[Plugin guide](https://holomush.dev/extending/tutorials/plugin-guide/)** —
  writing Lua and binary plugins.

The `task pr-prep` gate covers most of this automatically before you open a PR.

Tests are written **before** the implementation. PR titles follow
[Conventional Commits](https://www.conventionalcommits.org/) (`type(scope): description`)
and are enforced by a required CI check.

## How we develop

HoloMUSH is developed heavily with AI coding agents, using native `git` worktrees for
per-session isolation. That is why you'll see a `CLAUDE.md`, a `.claude/` directory, and
agent-authored commits — they are part of the maintainer workflow, not requirements for
you. You need none of that tooling: standard `git`, GitHub Issues, and GitHub pull
requests are all you need.

The issue-first rule above, however, is not optional for anyone.

## Code of Conduct

This project adheres to a [Code of Conduct](CODE_OF_CONDUCT.md). By
participating, you are expected to uphold it.

## License

By contributing, you agree that your contributions will be licensed under the
[Apache 2.0 License](LICENSE).
