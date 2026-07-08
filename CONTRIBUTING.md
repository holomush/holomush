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

## Prerequisites

- Go — the required version is pinned in [`go.mod`](go.mod)
- [Task](https://taskfile.dev/) (the task runner this project uses)
- PostgreSQL
- Docker (only needed to run the integration tests locally)

## Your first pull request

HoloMUSH uses a standard GitHub fork-and-pull-request workflow:

```bash
# 1. Fork the repo on GitHub, then clone YOUR fork
git clone https://github.com/<your-username>/holomush.git
cd holomush

# 2. Install tools and git hooks
task setup

# 3. Create a branch
git checkout -b my-change

# 4. Make your change test-first, then run the local checks
task lint
task test
task pr-prep        # the local pre-PR gate; CI also runs integration + E2E

# 5. Push to your fork and open a pull request
git push -u origin my-change
```

That's the whole flow — plain `git`, your own fork, a PR. CI runs the full
suite (including integration and E2E tests) on every pull request.

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

## How we develop (optional — not required for contributors)

HoloMUSH is developed heavily with AI coding agents, using native `git`
worktrees for per-session isolation and an internal issue tracker. **You need
none of that.** Standard `git`, GitHub Issues, and GitHub pull requests are
fully supported and are the only thing we ask of contributors. (This is why
you'll see a `CLAUDE.md`, a `.claude/` directory, and agent-authored commits —
they're part of the maintainer workflow, not requirements for you.)

## Code of Conduct

This project adheres to a [Code of Conduct](CODE_OF_CONDUCT.md). By
participating, you are expected to uphold it.

## License

By contributing, you agree that your contributions will be licensed under the
[Apache 2.0 License](LICENSE).
