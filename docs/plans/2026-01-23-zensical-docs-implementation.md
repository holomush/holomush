# Zensical Documentation Site Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Set up HoloMUSH documentation site using Zensical, migrate existing docs, and configure CI/CD for Cloudflare Pages.

**Architecture:** Zensical (Python SSG) in `site/` folder, audience-organized content structure (developers/operators/contributors), Cloudflare Pages deployment via GitHub Actions with uv package management.

**Tech Stack:** Zensical, Python 3.12+, uv, Cloudflare Pages, GitHub Actions

**Design Reference:** See `docs/plans/2026-01-23-zensical-docs-design.md` for detailed configuration and content specifications.

---

## Task 1: Initialize Zensical Project Structure

**Files:**

- Create: `site/pyproject.toml`
- Create: `site/zensical.toml`
- Create: `site/.python-version`
- Modify: `.gitignore`

**Steps:**

1. Create directory: `mkdir -p site/docs site/assets`
2. Create `site/.python-version` with content: `3.12`
3. Create `site/pyproject.toml` per design doc (holomush-docs project with zensical dependency)
4. Create `site/zensical.toml` per design doc (dark slate theme, deep-orange accent)
5. Add to `.gitignore`: `site/build/` and `site/.venv/`
6. Run: `cd site && uv sync` - verify venv created
7. Commit: `feat(docs): initialize Zensical project structure`

---

## Task 2: Create Landing Page and Navigation Structure

**Files:**

- Create: `site/docs/index.md`
- Create: `site/docs/developers/index.md`
- Create: `site/docs/operators/index.md`
- Create: `site/docs/contributors/index.md`

**Steps:**

1. Create `site/docs/index.md` - landing page with audience cards (Developers, Operators, Contributors)
2. Create `site/docs/developers/index.md` - plugin development overview
3. Create `site/docs/operators/index.md` - operations overview
4. Create `site/docs/contributors/index.md` - contributing overview
5. Run: `cd site && uv run zensical build` - verify build succeeds
6. Run: `cd site && uv run zensical serve` - verify navigation tabs work
7. Commit: `feat(docs): add landing page and section indexes`

---

## Task 3: Migrate Getting Started Guide

**Files:**

- Create: `site/docs/developers/getting-started.md`
- Reference: `docs/reference/getting-started.md`

**Steps:**

1. Read `docs/reference/getting-started.md` for content
2. Create `site/docs/developers/getting-started.md` adapted for plugin developers
3. Add tabbed code blocks for OS-specific instructions (macOS/Linux)
4. Run: `cd site && uv run zensical build` - verify build succeeds
5. Commit: `docs(developers): migrate getting started guide`

---

## Task 4: Migrate Plugin Guide

**Files:**

- Create: `site/docs/developers/plugin-guide.md`
- Reference: `docs/reference/plugin-authoring.md`

**Steps:**

1. Read `docs/reference/plugin-authoring.md` for content
2. Create `site/docs/developers/plugin-guide.md` - clean version focused on current plugin systems
3. Remove deprecated WASM/Extism content, focus on Lua and binary plugins
4. Run: `cd site && uv run zensical build` - verify build succeeds
5. Commit: `docs(developers): add plugin guide`

---

## Task 5: Create Operators Documentation

**Files:**

- Create: `site/docs/operators/installation.md`
- Create: `site/docs/operators/configuration.md`
- Create: `site/docs/operators/operations.md`
- Reference: `docs/reference/operations.md`, `docs/reference/getting-started.md`

**Steps:**

1. Create `site/docs/operators/installation.md` - Docker and binary installation
2. Create `site/docs/operators/configuration.md` - CLI flags and env vars reference
3. Create `site/docs/operators/operations.md` - adapted from existing, PostgreSQL monitoring
4. Run: `cd site && uv run zensical build` - verify build succeeds
5. Commit: `docs(operators): add installation, configuration, and operations guides`

---

## Task 6: Create Contributors Documentation

**Files:**

- Create: `site/docs/contributors/architecture.md`
- Create: `site/docs/contributors/coding-standards.md`
- Create: `site/docs/contributors/pr-guide.md`
- Reference: `docs/reference/pull-request-guide.md`, `CLAUDE.md`

**Steps:**

1. Create `site/docs/contributors/architecture.md` - system design overview with ASCII diagram
2. Create `site/docs/contributors/coding-standards.md` - extracted from CLAUDE.md, human-relevant
3. Create `site/docs/contributors/pr-guide.md` - adapted from existing PR guide
4. Run: `cd site && uv run zensical build` - verify build succeeds
5. Commit: `docs(contributors): add architecture, coding standards, and PR guides`

---

## Task 7: Add Logo and Assets

**Files:**

- Copy: Logo to `site/assets/logo.png`
- Create: `site/assets/favicon.png`

**Steps:**

1. Create: `mkdir -p site/assets`
2. Copy HoloMUSH logo to `site/assets/logo.png`
3. Create favicon (resize logo to 32x32 or 64x64) as `site/assets/favicon.png`
4. Run: `cd site && uv run zensical build` - verify logo appears in header
5. Commit: `feat(docs): add logo and favicon assets`

---

## Task 8: Add Taskfile Integration

**Files:**

- Modify: `Taskfile.yaml`

**Steps:**

1. Add to `Taskfile.yaml` after existing tasks:
   - `docs:serve` - Start documentation dev server (`uv run zensical serve`)
   - `docs:build` - Build documentation site (`uv run zensical build`)
   - `docs:setup` - Set up dev environment (`uv sync`)
2. Run: `task docs:setup && task docs:build` - verify tasks work
3. Commit: `feat(docs): add Taskfile integration for docs commands`

---

## Task 9: Add GitHub Actions Workflow

**Files:**

- Create: `.github/workflows/docs.yml`

**Steps:**

1. Create `.github/workflows/docs.yml` per design doc:
   - Trigger on push to main with `site/**` changes
   - Trigger on PRs with `site/**` changes
   - Use `actions/checkout@v6`
   - Use `astral-sh/setup-uv@v7` with version `0.9.26`
   - Build with `uv run zensical build`
   - Deploy with `cloudflare/wrangler-action@v3`
2. Add SPDX license header
3. Run: `task lint:yaml` - verify YAML syntax
4. Commit: `ci(docs): add GitHub Actions workflow for Cloudflare Pages`

---

## Task 10: Final Verification and PR

**Steps:**

1. Full build test:
   - Run: `cd site && uv sync && uv run zensical build`
   - Run: `uv run zensical serve`
   - Verify in browser at `http://localhost:8000`:
     - Landing page loads with cards
     - Navigation tabs work
     - All pages render without errors
     - Logo appears in header
     - Dark theme applied
     - Search works
2. Run project tests: `task test && task lint`
3. Push branch: `git push -u origin feat/zensical-docs`
4. Create PR with summary of changes

---

## Post-Implementation Checklist

- [ ] All tasks completed and committed
- [ ] `task docs:build` succeeds
- [ ] `task docs:serve` shows working site
- [ ] Navigation tabs work correctly
- [ ] All migrated content renders properly
- [ ] PR created and CI passing
- [ ] Cloudflare Pages project created (manual)
- [ ] DNS configured for docs.holomush.dev (manual)
- [ ] GitHub secrets added (manual): `CF_API_TOKEN`, `CF_ACCOUNT_ID`
