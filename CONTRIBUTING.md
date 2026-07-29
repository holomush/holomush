<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Contributing to HoloMUSH

Thanks for your interest in contributing! HoloMUSH is open source (Apache-2.0)
and welcomes contributions of all kinds — code, documentation, bug reports, and
feature ideas.

Find work to do, or report a bug or idea, via
[GitHub Issues](https://github.com/holomush/holomush/issues).

> **Read [The Issue-First Rule](#the-issue-first-rule--no-exceptions) before you write any
> code.** Every pull request needs a linked, approved issue. PRs that arrive without one
> are closed.

## Prerequisites

- Go — the required version is pinned in [`go.mod`](go.mod)
- [Task](https://taskfile.dev/) (the task runner this project uses)
- PostgreSQL
- Docker (only needed to run the integration and E2E tests locally)

## Types of contributions

HoloMUSH accepts four types of contributions. Each has a different process and a
different bar for acceptance. **Read this section before opening anything.**

### 🐛 Fix (bug report)

A fix corrects something that is broken, crashes, produces wrong output, or behaves
contrary to documented behavior.

**Process:**

1. Open a [Bug Report issue](https://github.com/holomush/holomush/issues/new?template=bug_report.yml) — fill it out completely.
2. Wait for a maintainer to confirm it is a bug (label: `confirmed-bug`). For obvious, reproducible bugs this is typically fast.
3. Fix it. Write a test that fails without the fix and passes with it.
4. Open a PR using the [Fix PR template](.github/PULL_REQUEST_TEMPLATE/fix.md) — link the confirmed issue with `Fixes #NNN`.

**Rejection reasons:** not reproducible, works-as-designed, duplicate of an existing issue.

---

### ⚡ Enhancement

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

**Rejection reasons:** issue not labeled `approved-enhancement`, scope exceeds what was approved, no written proposal, duplicates existing behavior.

---

### ✨ Feature

A feature adds something new — a command, event type, plugin capability, RPC, or client
surface. Features have the highest bar because they add permanent maintenance burden.

**The bar:** features require a complete written specification approved by a maintainer
before any code is written. A PR for a feature is closed without review if the linked
issue does not carry the `approved-feature` label. Incomplete specs are closed, not
revised by maintainers.

**Process:**

1. **Discuss first** — check [Discussions](https://github.com/holomush/holomush/discussions) to see if the idea has been raised. If it was raised and declined, don't open a new issue.
2. Open a [Feature Request issue](https://github.com/holomush/holomush/issues/new?template=feature_request.yml) with the complete spec. The template requires: the problem being solved and for whom, what is being added, full scope of affected files and systems, user stories, acceptance criteria, breaking-change assessment, and maintenance burden.
3. **Wait for maintainer approval.** A maintainer must label the issue `approved-feature` before you write a single line of code. Approval is not guaranteed — many valid ideas are declined because they conflict with the project's architecture.
4. Write the code. Implement exactly the approved spec. Scope changes require re-approval.
5. Open a PR using the [Feature PR template](.github/PULL_REQUEST_TEMPLATE/feature.md) — link the approved issue with `Closes #NNN`.

**Rejection reasons:** issue not labeled `approved-feature`, spec is incomplete, scope exceeds what was approved, maintenance burden too high.

---

### 🔧 Chore / maintenance

Internal work that improves project health without changing user-facing behavior —
test refactoring, CI/CD, dependency updates, tooling, tech debt.

**Process:**

1. Open a [Chore issue](https://github.com/holomush/holomush/issues/new?template=chore.yml) describing the current state, proposed work, and what "done" means.
2. Wait for a maintainer to triage it.
3. Open a PR using the typed template that best fits the change.

Documentation content problems use the
[Documentation Issue template](https://github.com/holomush/holomush/issues/new?template=docs_issue.yml).

---

## The Issue-First Rule — No Exceptions

> **No code before approval.**

- For **fixes**: open the issue, get `confirmed-bug`, then fix it.
- For **enhancements**: open the issue, get `approved-enhancement`, then code.
- For **features**: open the issue, get `approved-feature`, then code.

PRs that arrive without a properly-labeled linked issue are closed. This is not a
bureaucratic hurdle — it protects you from spending time on work that will be rejected,
and it protects maintainers from reviewing code for changes that were never agreed to.

This rule binds everyone, including maintainers and AI-agent-driven work.

**Exempt by file path:** dependency updates (including automated Renovate PRs), CI/tooling
changes, and documentation-only PRs do not need a typed template or an approved issue. For
any other cross-cutting PR that genuinely does not fit, paste
`<!-- pr-template-exempt: your reason here -->` into the PR body.

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
| `gate-violation` | PR opened without a linked, approved issue |

## Your first pull request

HoloMUSH uses a standard GitHub fork-and-pull-request workflow. The only addition is the
issue-first step.

```bash
# 1. Open (or find) an issue and wait for the approval label — see above.

# 2. Fork the repo on GitHub, then clone YOUR fork
git clone https://github.com/<your-username>/holomush.git
cd holomush

# 3. Install tools and git hooks
task setup

# 4. Create a branch
git checkout -b fix/1234-exit-list-truncated

# 5. Make your change test-first, then run the local checks
task lint
task test
task pr-prep        # the local pre-PR gate; CI also runs integration + E2E

# 6. Push to your fork and open a pull request using a typed template
git push -u origin fix/1234-exit-list-truncated
```

All PRs target `main` and are squash-merged. CI runs the full suite (including integration
and E2E tests) on every pull request.

**No draft PRs.** Open a PR only when the code is complete, `task pr-prep` is green, and
the correct typed template is used.

**Never use `[ci skip]` / `[skip ci]`** in any commit on a branch with an open PR — it
suppresses the required status checks for that commit, leaving the PR blocked with nothing
visibly failing.

## What we expect

A few conventions keep the codebase coherent. Rather than restating them here
(where they'd drift), see the canonical docs:

- **[Pull Request Guide](https://holomush.dev/contributing/how-to/pr-guide/)** —
  PR workflow, review, and merge process; conventional-commit titles.
- **[Coding standards](https://holomush.dev/contributing/reference/coding-standards/)** —
  style, error handling, test naming, the >80% coverage expectation.
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
