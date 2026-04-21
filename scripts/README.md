# HoloMUSH Scripts

Python utilities for HoloMUSH operators and contributors.

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
