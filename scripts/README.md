<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# HoloMUSH Scripts

Utilities for HoloMUSH operators and contributors — Python helpers, shell
generators, and `ast-grep` codemod rules.

## bootstrap_seed_secrets.py

Interactive helper that collects, validates, and writes the seven GitHub Secrets
required by the `bootstrap-sandbox` workflow.

### Prerequisites

- Python 3.12+
- `gh` CLI authenticated (`gh auth status`)
- `ssh-keygen` in PATH (for private-key fingerprint validation)

### Running

```bash
# Auto-detect repo from gh CLI context
./scripts/bootstrap_seed_secrets.py

# Explicit repo (forks or testing)
./scripts/bootstrap_seed_secrets.py --repo OWNER/NAME

# Validate inputs without writing (dry run)
./scripts/bootstrap_seed_secrets.py --dry-run

# Overwrite existing secrets without prompting
./scripts/bootstrap_seed_secrets.py --overwrite
```

The script runs three phases:

1. **Collect** — prompts for all secrets up-front with hidden input and a
   first4…last4 confirmation step.
2. **Validate** — exercises the exact API endpoints the bootstrap workflow uses.
   If any check fails, the script exits with a clear error summary and writes
   nothing.
3. **Write** — calls `gh secret set` for each secret, then verifies the
   `updatedAt` timestamp via `gh secret list` within 60 seconds.

### Developer workflow

The scripts directory uses [uv](https://docs.astral.sh/uv/) for dependency management.

```bash
# Install dev dependencies
cd scripts
uv sync

# Run tests
uv run pytest tests/

# Lint
uv run ruff check .
uv run ruff format --check .

# Auto-fix formatting
uv run ruff format .
```

Tests use `monkeypatch` / `unittest.mock` — no real API calls or network access.

## codemod/

`ast-grep` rules that mechanise the `world.Service` bare-subject-string →
`world.Caller` migration, plus a read-only census probe. Full documentation,
including the per-rule `ignores:` table, the three load-bearing constraints, the
hand-migrated surfaces, and the measured baselines, lives in
[`codemod/README.md`](codemod/README.md).

### Prerequisites

- `ast-grep` 0.45.1+ (`ast-grep --version`) — **not** installed by `task setup`
  and **not** present in CI. These rules are a local developer step, never a CI
  gate.

### Running

```bash
# Preview (dry run)
ast-grep scan -r scripts/codemod/world-caller-arg2.yml .

# Apply in place
ast-grep scan -r scripts/codemod/world-caller-arg2.yml -U .

# Read-only census — MUST NOT be run with -U
ast-grep scan -r scripts/codemod/probe-subject-param.yml . | rg -c '┌─ '
```
