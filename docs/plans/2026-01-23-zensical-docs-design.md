# HoloMUSH Documentation Site Design

## Overview

A polished documentation site serving plugin developers, game operators, and core
contributors via Zensical on Cloudflare Pages at `docs.holomush.dev`.

## Decisions

| Decision       | Choice                                 | Rationale                                                    |
| -------------- | -------------------------------------- | ------------------------------------------------------------ |
| Framework      | Zensical                               | Battle-tested (Material for MkDocs team), batteries-included |
| Hosting        | Cloudflare Pages                       | Unified ecosystem, PR previews, global CDN                   |
| Theme          | Modern (dark slate, deep-orange/amber) | Matches HoloMUSH logo aesthetic                              |
| Structure      | Separate `site/` folder                | Clean separation from internal docs                          |
| Python tooling | uv                                     | Fast, modern, Astral ecosystem                               |

## Directory Structure

```text
site/                           # Doc site source (Zensical)
  zensical.toml                 # Configuration
  pyproject.toml                # Python dependencies (uv)
  docs/                         # Content (Zensical docs_dir)
    index.md                    # Landing page
    developers/                 # Plugin developers
      getting-started.md
      plugin-guide.md
      api-reference/
    operators/                  # Deployment & ops
      installation.md
      configuration.md
      operations.md
    contributors/               # Core team
      architecture.md
      coding-standards.md
      pr-guide.md
  assets/
    logo.png                    # HoloMUSH orb logo
    favicon.png

docs/                           # Internal (repo-only, unchanged)
  specs/                        # Design specs
  plans/                        # Implementation plans
  CLAUDE.md                     # AI instructions
```

## Configuration

### `site/zensical.toml`

```toml
[project]
site_name = "HoloMUSH Documentation"
site_url = "https://docs.holomush.dev"
site_description = "Modern MUSH platform with WASM plugins"
site_author = "HoloMUSH Contributors"
copyright = "© 2026 HoloMUSH Contributors • Apache-2.0"

docs_dir = "docs"
site_dir = "build"

[project.theme]
variant = "modern"
logo = "assets/logo.png"
favicon = "assets/favicon.png"

[project.theme.palette]
scheme = "slate"
primary = "deep-orange"
accent = "amber"

[project.theme.features]
navigation.tabs = true
navigation.sections = true
navigation.instant = true
search.suggest = true
content.code.copy = true

[project.repo]
url = "https://github.com/holomush/holomush"
name = "GitHub"
```

### `site/pyproject.toml`

```toml
[project]
name = "holomush-docs"
version = "0.1.0"
requires-python = ">=3.12"
dependencies = ["zensical"]

[tool.uv]
dev-dependencies = []
```

## Content Migration

| Source                                 | Destination                                  | Action                       |
| -------------------------------------- | -------------------------------------------- | ---------------------------- |
| `docs/reference/getting-started.md`    | `site/docs/developers/getting-started.md`    | Migrate & polish             |
| `docs/reference/plugin-authoring.md`   | `site/docs/developers/plugin-guide.md`       | Migrate & expand             |
| `docs/reference/operations.md`         | `site/docs/operators/operations.md`          | Migrate                      |
| `docs/reference/pull-request-guide.md` | `site/docs/contributors/pr-guide.md`         | Migrate                      |
| Architecture (from specs)              | `site/docs/contributors/architecture.md`     | Extract & curate             |
| `CLAUDE.md` coding standards           | `site/docs/contributors/coding-standards.md` | Extract human-relevant parts |

### New Content to Create

- `site/docs/index.md` — Landing page with audience cards
- `site/docs/operators/installation.md` — Docker/binary setup
- `site/docs/operators/configuration.md` — Config reference
- `site/docs/developers/api-reference/` — Generated from protobuf (future)

## CI/CD

### `.github/workflows/docs.yml`

```yaml
name: Deploy Docs

on:
  push:
    branches: [main]
    paths: ["site/**"]
  pull_request:
    paths: ["site/**"]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6

      - name: Install uv
        uses: astral-sh/setup-uv@v7
        with:
          version: "0.9.26"

      - name: Install Zensical
        working-directory: site
        run: uv pip install --system zensical

      - name: Build docs
        working-directory: site
        run: zensical build

      - name: Deploy to Cloudflare Pages
        if: github.ref == 'refs/heads/main'
        uses: cloudflare/wrangler-action@v3
        with:
          apiToken: ${{ secrets.CF_API_TOKEN }}
          accountId: ${{ secrets.CF_ACCOUNT_ID }}
          command: pages deploy site/build --project-name=holomush-docs
```

### Cloudflare Setup

1. Create Pages project `holomush-docs` in Cloudflare dashboard
2. Add custom domain `docs.holomush.dev`
3. Add GitHub secrets:
   - `CF_API_TOKEN` — API token with Pages edit permissions
   - `CF_ACCOUNT_ID` — Cloudflare account ID

## Local Development

### Taskfile additions

```yaml
docs:serve:
  desc: Start documentation dev server
  dir: site
  cmds:
    - uv run zensical serve

docs:build:
  desc: Build documentation site
  dir: site
  cmds:
    - uv run zensical build

docs:setup:
  desc: Set up documentation development environment
  dir: site
  cmds:
    - uv sync
```

### Quick start

```bash
cd site
uv sync
uv run zensical serve
# Open http://localhost:8000
```

## Acceptance Criteria

- [ ] Zensical installed and configured in `site/`
- [ ] Existing reference docs migrated to audience-organized structure
- [ ] Navigation tabs show Developers | Operators | Contributors
- [ ] Logo and dark theme applied
- [ ] CI/CD workflow deploys to Cloudflare Pages on main branch push
- [ ] PR previews functional
- [ ] All existing documentation renders correctly
- [ ] `task docs:serve` works locally
