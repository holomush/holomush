<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Cocogitto Release Tooling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace release-please with Cocogitto (`cog`) as a tag-only, manual-dispatch version+tag engine, drop the in-repo `CHANGELOG.md`, move conventional-commit validation to a CI PR-title gate, and split deploy into its own dispatch-only workflow — keeping GoReleaser as the unchanged artifact engine.

**Architecture:** A `workflow_dispatch` of `release.yaml` mints a GitHub App token, installs pinned `cog`, previews+guards the bump, runs `cog bump --auto --disable-bump-commit` (creates a local `v*` tag, no commit to `main`), then runs `goreleaser release --clean` which — via a new `release.target_commitish` in `.goreleaser.yaml` — creates the remote tag + GitHub Release + signed artifacts atomically, all in one run (sidestepping the `GITHUB_TOKEN`-tag-push-doesn't-trigger-workflow trap). Deploy moves to a separate dispatch-only `deploy.yaml`. A `commit-lint.yaml` workflow runs `cog verify` on every PR title (which becomes the squash commit subject).

**Tech Stack:** Cocogitto 7.0.0 (pinned binary), GoReleaser v2, GitHub Actions, bats-core (shell tests), Taskfile, jj.

**Spec:** `docs/superpowers/specs/2026-05-24-cocogitto-release-tooling-design.md` (design bead `holomush-dfsvk`).

**Grounded constants (verified 2026-05-24):**

- Cocogitto release: `7.0.0`; Linux asset `cocogitto-7.0.0-x86_64-unknown-linux-musl.tar.gz`; SHA256 `e03938ff2c4c86d71c00c0f3284dbbe95c5ca76fe34a51f33e945c23010d59bb`. (Local dev `cog` is also 7.0.0.)
- App-token action: `actions/create-github-app-token@bcd2ba49218906704ab6c1aa796996da409d3eb1` (`v3.2.0`).
- Latest release tag in repo: `v0.1.14`. Runners: `namespace-profile-linux-amd64-*` (x86_64).
- `cog verify "<string>"` accepts a positional message arg. `cog.toml` keys: `tag_prefix`, `disable_changelog`, `branch_whitelist`, `disable_bump_commit` (all confirmed via context7 `/cocogitto/cocogitto`).

---

## File structure

| File | Responsibility | Action |
| --- | --- | --- |
| `cog.toml` | `cog` config: tag-only release mode, no changelog | Modify |
| `scripts/tests/cog-release.bats` | Fixture tests: tag prefix, 0.x bump, INV-6 parse, no-changelog | Create |
| `.goreleaser.yaml` | Add `release.target_commitish` (only change) | Modify |
| `.github/actions/install-cog/action.yml` | Composite action: pinned `cog` on PATH (DRY across workflows) | Create |
| `.github/workflows/commit-lint.yaml` | PR-title conventional-commit gate | Create |
| `lefthook.yaml` | Annotate `cog verify` hook as best-effort (CI is authoritative) | Modify |
| `.github/workflows/release.yaml` | Remove release-please job; dispatch goreleaser job (cog inline); remove deploy job | Modify |
| `.github/workflows/deploy.yaml` | Dispatch-only deploy (moved from release.yaml) | Create |
| `release-please-config.json` | release-please config | Delete |
| `.release-please-manifest.json` | release-please version mirror | Delete |
| `Taskfile.yaml` | Drop `CHANGELOG.md` rumdl excludes; add `release`/`deploy` dispatch wrappers | Modify |

---

### Task 1: `cog.toml` release mode + fixture tests

**Files:**

- Create: `scripts/tests/cog-release.bats`
- Modify: `cog.toml`

- [ ] **Step 1: Write the failing test**

Create `scripts/tests/cog-release.bats`:

```bash
#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

# Validates the repo's cog.toml drives tag-only, v-prefixed, changelog-free
# releases and that cog parses commit subjects release-please choked on
# (INV-6: #4164 arrows, #4094 parens). Runs cog against a throwaway fixture
# repo seeded with the real cog.toml, so assertions are deterministic.

setup() {
  REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  FIX="$BATS_TEST_TMPDIR/fix"
  mkdir -p "$FIX"
  cd "$FIX"
  git init -q -b main
  git config user.email "t@example.com"
  git config user.name "Test"
  cp "$REPO_ROOT/cog.toml" .
  echo seed > seed.txt
  git add -A
  git commit -q -m "chore: seed"
  git tag v0.1.0
}

@test "tag-only bump creates a v-prefixed minor tag on 0.x (tag_prefix + pre-major)" {
  git commit -q --allow-empty -m "feat: a new thing"
  cog bump --auto --disable-bump-commit
  run git describe --tags --abbrev=0
  [ "$status" -eq 0 ]
  [ "$output" = "v0.2.0" ]
}

@test "disable_changelog: tag-only bump writes no CHANGELOG.md" {
  git commit -q --allow-empty -m "feat: a new thing"
  cog bump --auto --disable-bump-commit
  [ ! -f CHANGELOG.md ]
}

@test "tag-only bump creates no commit on the branch (no protected-main write)" {
  before="$(git rev-parse HEAD)"
  git commit -q --allow-empty -m "fix: a fix"
  expected="$(git rev-parse HEAD)"
  cog bump --auto --disable-bump-commit
  [ "$(git rev-parse HEAD)" = "$expected" ]
  [ "$expected" != "$before" ]  # sanity: our own commit landed, cog's did not add another
}

@test "INV-6: arrows in a fix subject parse (regression for #4164)" {
  subject="fix(session): map SESSION_NOT_FOUND/EXPIRED → STREAM_ACCESS_DENIED"
  run cog verify "$subject"
  [ "$status" -eq 0 ]
  git commit -q --allow-empty -m "$subject"
  run cog bump --auto --disable-bump-commit
  [ "$status" -eq 0 ]
}

@test "INV-6: parens in a chore subject parse (regression for #4094)" {
  run cog verify "chore(deps): bump tailwindcss (v4) and @tailwindcss/vite"
  [ "$status" -eq 0 ]
}

@test "commit-lint behavior: malformed PR title is rejected" {
  run cog verify "just some words with no type"
  [ "$status" -ne 0 ]
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `task test:bats`
Expected: FAIL — the v-prefixed-tag test fails (current `cog.toml` has no `tag_prefix`, so the tag is `0.2.0` not `v0.2.0`) and the no-`CHANGELOG.md` test fails (current `cog.toml` lacks `disable_changelog`, so the bump writes `CHANGELOG.md`). The two INV-6 tests and the malformed-title test PASS already (they characterize that `cog` lacks release-please's parse defect — if any INV-6 test FAILS, `cog` is unsuitable and the design must be revisited).

- [ ] **Step 3: Rewrite `cog.toml` to release mode**

Replace the entire contents of `cog.toml` with:

```toml
# Cocogitto configuration
# https://docs.cocogitto.io/
#
# cog is the release engine: it derives the next semver from conventional
# commits and creates a TAG ONLY (no bump commit, no in-repo CHANGELOG.md).
# GitHub Release notes are generated by GoReleaser (see .goreleaser.yaml).
# Conventional-commit validation is enforced in CI on PR titles
# (.github/workflows/commit-lint.yaml); the lefthook commit-msg hook is
# best-effort only (jj does not fire it reliably).

[settings]
# v-prefixed tags (v1.2.3) to match GoReleaser and the existing tag history.
tag_prefix = "v"

# Tag-only releases: no in-repo CHANGELOG.md (no commit to protected main).
disable_changelog = true

# Defense-in-depth: only allow a bump from main, so a stray dispatch from a
# feature branch cannot cut a release.
branch_whitelist = ["main"]

ignore_merge_commits = true

# SUPERSEDED DURING IMPLEMENTATION — do NOT copy this block verbatim.
# In cog 7.x an empty `X = {}` entry DISABLES that type (cog then rejects it in
# `cog verify` and fails `cog bump` with "Commit type X not allowed"). The block
# below would therefore disable all of cog's built-in defaults. The shipped
# cog.toml omits `[commit_types]` entirely and relies on cog's default allow-list
# (feat/fix/docs/style/refactor/perf/test/build/ci/chore/revert); new types are
# ADDED only via NON-empty tables (e.g. `deps = { omit_from_changelog = true }`).
# Allowed commit types for `cog verify` (PR-title gate) and bump inference.
[commit_types]
feat = {}
fix = {}
docs = {}
style = {}
refactor = {}
perf = {}
test = {}
build = {}
ci = {}
chore = {}
revert = {}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `task test:bats`
Expected: PASS (all 6 tests in `cog-release.bats`).

- [ ] **Step 5: Commit**

Run: `jj describe -m "feat(release): cog.toml tag-only release mode + fixture tests"` then start a new change with `jj new` (per `references/vcs-preamble.md`; the working copy is already a commit in jj — describe it, do not `git commit`).

---

### Task 2: `.goreleaser.yaml` — `release.target_commitish`

**Files:**

- Modify: `.goreleaser.yaml:169-175` (the `release:` block)

- [ ] **Step 1: Add `target_commitish`**

In `.goreleaser.yaml`, change the `release:` block from:

```yaml
release:
  github:
    owner: holomush
    name: holomush
  draft: false
  prerelease: auto
```

to:

```yaml
release:
  github:
    owner: holomush
    name: holomush
  draft: false
  prerelease: auto
  # The release workflow creates the v* tag LOCALLY (cog bump --disable-bump-commit)
  # and does NOT push it before running goreleaser. target_commitish tells the
  # GitHub Releases API which commit to create the tag from, so goreleaser creates
  # the remote tag as part of publishing the Release — atomically. Without this,
  # `goreleaser release` against a local-only tag fails at the API call. See spec
  # INV-8 and https://goreleaser.com/customization/release/.
  target_commitish: "{{ .Commit }}"
```

- [ ] **Step 2: Verify goreleaser still accepts the config**

Run: `task release:check`
Expected: PASS (no error; `goreleaser check` validates the config including the new key).

- [ ] **Step 3: Verify the key is present and correct**

Run: `yq '.release.target_commitish' .goreleaser.yaml`
Expected output: `{{ .Commit }}`

- [ ] **Step 4: Commit**

Run: `jj describe -m "feat(release): goreleaser target_commitish for local-tag releases"` then `jj new`.

---

### Task 3: `install-cog` composite action

**Files:**

- Create: `.github/actions/install-cog/action.yml`

- [ ] **Step 1: Create the composite action**

Create `.github/actions/install-cog/action.yml` (mirrors `.github/actions/install-task` structure; pins the binary + SHA256 like `ci.yaml`'s tool install):

```yaml
name: Install Cocogitto
# Download the pinned cocogitto (`cog`) release binary, verify its SHA256, and
# put it on PATH. Pins by checksum (same pattern as ci.yaml rumdl/dprint/buf)
# rather than `cargo install` (slow) or a third-party action. x86_64 musl build
# runs on the namespace-profile-linux-amd64 runners.
description: Install the pinned cog binary and add it to PATH.

runs:
  using: composite
  steps:
    - name: Install cocogitto
      shell: bash
      run: |
        set -euo pipefail
        COG_VERSION="7.0.0"
        COG_TARBALL="cocogitto-${COG_VERSION}-x86_64-unknown-linux-musl.tar.gz"
        COG_URL="https://github.com/cocogitto/cocogitto/releases/download/${COG_VERSION}/${COG_TARBALL}"
        COG_SHA256="e03938ff2c4c86d71c00c0f3284dbbe95c5ca76fe34a51f33e945c23010d59bb"
        curl -LsSfO "${COG_URL}"
        echo "${COG_SHA256}  ${COG_TARBALL}" | sha256sum -c -
        sudo tar xzf "${COG_TARBALL}" --overwrite -C /usr/local/bin cog
        rm "${COG_TARBALL}"

    - name: Verify cog
      shell: bash
      run: cog --version
```

- [ ] **Step 2: Lint the action**

Run: `yamlfmt -lint .github/actions/install-cog/action.yml`
Expected: no output, exit 0.

- [ ] **Step 3: Commit**

Run: `jj describe -m "ci(release): install-cog composite action (pinned cog 7.0.0)"` then `jj new`.

> **Note for the implementer:** the cocogitto tarball contains the `cog` binary at the archive root (verified against the 7.0.0 musl asset layout — same shape as rumdl). If `tar … cog` errors with "not found in archive", run `tar tzf "${COG_TARBALL}"` to inspect the layout and adjust the extracted path; do **not** change the pinned version/SHA.

---

### Task 4: `commit-lint.yaml` workflow + lefthook annotation

**Files:**

- Create: `.github/workflows/commit-lint.yaml`
- Modify: `lefthook.yaml:107-110`

- [ ] **Step 1: Create the commit-lint workflow**

Create `.github/workflows/commit-lint.yaml`. It MUST have its own `pull_request` trigger with **no `paths-ignore`** (so docs-only PRs — whose titles still become commit subjects on `main` — are validated; this is why it is not a job in `ci.yaml`, whose `pull_request` trigger has `paths-ignore`).

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

name: Commit Lint

# No paths-ignore: every PR's title is validated, because squash-merge uses the
# PR title as the commit subject on main (squash_merge_commit_title=PR_TITLE),
# which cog bump parses for version derivation. A malformed title on ANY PR
# (including docs-only) would break release version derivation.
on:
  pull_request:
    branches: [main]

permissions:
  contents: read

jobs:
  commit-lint:
    name: Conventional Commit (PR title)
    runs-on: namespace-profile-linux-amd64-2x4
    steps:
      - name: Install cog
        uses: holomush/holomush/.github/actions/install-cog@main

      - name: Verify PR title is a conventional commit
        env:
          PR_TITLE: ${{ github.event.pull_request.title }}
        run: |
          set -euo pipefail
          echo "Validating PR title: ${PR_TITLE}"
          cog verify "${PR_TITLE}"
```

> **Note:** local composite actions are referenced by `./.github/actions/install-cog` only when the repo is checked out. This job does **not** check out the repo (cog verify of a string needs no repo), so it references the action via the `owner/repo/path@ref` form. If actionlint rejects the `@main` pin, add a `actions/checkout` step before it and switch to `./.github/actions/install-cog`. Verify in Step 2.

- [ ] **Step 2: Lint the workflow**

Run: `yamlfmt -lint .github/workflows/commit-lint.yaml && actionlint .github/workflows/commit-lint.yaml`
Expected: no output, exit 0. If actionlint flags the `owner/repo/path@ref` action ref, apply the checkout fallback from the note above and re-run.

- [ ] **Step 3: Annotate the lefthook hook as best-effort**

In `lefthook.yaml`, change:

```yaml
commit-msg:
  commands:
    conventional-commit:
      run: cog verify --file {1}
```

to:

```yaml
# Best-effort local feedback only. jj does NOT fire lefthook reliably, and
# squash-merge composes the landing commit subject from the PR title at merge
# time (never seen by this hook). The authoritative conventional-commit gate is
# .github/workflows/commit-lint.yaml (validates the PR title in CI).
commit-msg:
  commands:
    conventional-commit:
      run: cog verify --file {1}
```

- [ ] **Step 4: Verify lefthook still parses**

Run: `yamlfmt -lint lefthook.yaml`
Expected: no output, exit 0.

- [ ] **Step 5: Commit**

Run: `jj describe -m "ci(release): commit-lint PR-title gate; lefthook hook now best-effort"` then `jj new`.

---

### Task 5: `release.yaml` — remove release-please, dispatch goreleaser inline, remove deploy

**Files:**

- Modify: `.github/workflows/release.yaml`

This task rewrites the trigger/permissions, deletes the `release-please` job, restructures the `goreleaser` job for the dispatch path, and deletes the `deploy-sandbox` job (it moves to `deploy.yaml` in Task 6).

- [ ] **Step 1: Replace the `on:` and `permissions:` blocks**

Replace `release.yaml:6-23` (the `on:` and `permissions:` blocks) with:

```yaml
on:
  push:
    tags:
      - "v*"
  workflow_dispatch:
    inputs:
      expected_increment:
        description: "Guard: fail if cog computes a different bump (auto = no guard)"
        required: false
        default: "auto"
        type: choice
        options:
          - auto
          - major
          - minor
          - patch

# pull-requests:write removed with release-please (no release PR anymore).
permissions:
  contents: write
  packages: write
  id-token: write # Required for Cosign keyless signing
  attestations: write # Required for provenance attestations
```

- [ ] **Step 2: Delete the `release-please` job**

Delete the entire `release-please` job (`release.yaml:26-38` in the original: from `release-please:` through the `manifest-file:` line). The next job (`goreleaser`) becomes the first job.

- [ ] **Step 3: Rewrite the `goreleaser` job header + add dispatch steps**

Replace the `goreleaser` job header and its first steps. Change the job's `needs:`/`if:` and insert the App-token + cog steps. The new job header through the `Get version` step:

```yaml
  goreleaser:
    name: Build, Sign, and Release
    runs-on: namespace-profile-linux-amd64-8x16
    # Runs on a manual release dispatch OR a human-pushed v* tag (fallback).
    # release-please is gone; the v* tag is the only contract.
    if: github.event_name == 'workflow_dispatch' || startsWith(github.ref, 'refs/tags/')
    outputs:
      version: ${{ steps.version.outputs.version }}
    steps:
      - uses: actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd # v6
        with:
          fetch-depth: 0

      # ── Release dispatch path: derive version + create the v* tag inline ──
      # (skipped on the tag-push fallback path, where the tag already exists)
      - name: Mint GitHub App token
        if: github.event_name == 'workflow_dispatch'
        id: app-token
        uses: actions/create-github-app-token@bcd2ba49218906704ab6c1aa796996da409d3eb1 # v3.2.0
        with:
          app-id: ${{ secrets.RELEASE_APP_ID }}
          private-key: ${{ secrets.RELEASE_APP_PRIVATE_KEY }}

      - name: Install cog
        if: github.event_name == 'workflow_dispatch'
        uses: ./.github/actions/install-cog

      - name: Configure git identity for tagging
        if: github.event_name == 'workflow_dispatch'
        run: |
          git config user.name "holomush-release[bot]"
          git config user.email "holomush-release[bot]@users.noreply.github.com"

      - name: Preview + guard version bump
        if: github.event_name == 'workflow_dispatch'
        env:
          EXPECTED: ${{ inputs.expected_increment }}
        run: |
          set -euo pipefail
          echo "Computed next version (dry-run):"
          cog bump --auto --dry-run
          if [ "${EXPECTED}" != "auto" ]; then
            CURRENT="$(git describe --tags --abbrev=0 | sed 's/^v//')"
            TARGET="$(cog bump --auto --dry-run 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | tail -n1)"
            IFS=. read -r cmaj cmin cpat <<< "${CURRENT}"
            IFS=. read -r tmaj tmin tpat <<< "${TARGET}"
            if   [ "${tmaj}" -gt "${cmaj}" ]; then KIND=major
            elif [ "${tmin}" -gt "${cmin}" ]; then KIND=minor
            elif [ "${tpat}" -gt "${cpat}" ]; then KIND=patch
            else KIND=none; fi
            if [ "${KIND}" != "${EXPECTED}" ]; then
              echo "::error::expected ${EXPECTED} bump but cog computed ${KIND} (${CURRENT} -> ${TARGET})"
              exit 1
            fi
          fi

      - name: Cut tag (cog bump, tag-only)
        if: github.event_name == 'workflow_dispatch'
        run: |
          set -euo pipefail
          # Creates a LOCAL v* tag from conventional commits. No bump commit
          # (--disable-bump-commit) and no CHANGELOG.md (disable_changelog in
          # cog.toml). The remote tag is created by goreleaser via target_commitish.
          cog bump --auto --disable-bump-commit
          git describe --tags --abbrev=0

      - name: Get version
        id: version
        run: |
          set -euo pipefail
          if [ "${GITHUB_EVENT_NAME}" = "workflow_dispatch" ]; then
            TAG="$(git describe --tags --abbrev=0)"   # the cog-created local tag
          else
            TAG="${GITHUB_REF_NAME}"                  # the human-pushed tag
          fi
          echo "version=${TAG#v}" >> "$GITHUB_OUTPUT"
          echo "Building version: ${TAG#v}"
```

- [ ] **Step 4: Keep the existing build/sign steps; point GoReleaser at the App token**

The steps from `Set up Go` through `Upload release artifacts` are unchanged **except** the `Run GoReleaser` step's `env`. Change it from:

```yaml
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

to:

```yaml
        env:
          # App token on the dispatch path (creates the remote tag via
          # target_commitish + bypasses any v* tag-protection); GITHUB_TOKEN on
          # the human-pushed-tag fallback path.
          GITHUB_TOKEN: ${{ steps.app-token.outputs.token || secrets.GITHUB_TOKEN }}
```

- [ ] **Step 5: Delete the `deploy-sandbox` job**

Delete the entire `deploy-sandbox` job (original `release.yaml:320-447`, from `deploy-sandbox:` to end of file). It is recreated in `deploy.yaml` (Task 6). The `verify-release` job stays. After deletion the last job in `release.yaml` is `verify-release`.

- [ ] **Step 6: Lint the workflow**

Run: `yamlfmt -lint .github/workflows/release.yaml && actionlint .github/workflows/release.yaml`
Expected: no output, exit 0.

- [ ] **Step 7: Verify no release-please / deploy residue remains**

Run: `rg -n "release-please|deploy-sandbox|release_created" .github/workflows/release.yaml`
Expected: no matches (exit 1 from rg).

- [ ] **Step 8: Commit**

Run: `jj describe -m "feat(release): cocogitto dispatch release, remove release-please + deploy job"` then `jj new`.

---

### Task 6: `deploy.yaml` — dispatch-only deploy

**Files:**

- Create: `.github/workflows/deploy.yaml`

- [ ] **Step 1: Create the deploy workflow**

Create `.github/workflows/deploy.yaml` with the `deploy-sandbox` job moved out of `release.yaml`, triggered by `workflow_dispatch` only. Preserve the `prod` environment, the `deploy-sandbox` concurrency group, and the SSH/snapshot/pull/health step **shapes** from the original `release.yaml` deploy job — but **simplified for dispatch-only**: drop `needs: [verify-release]`, drop the tag-push half of the original `if:`, and drop the `EVENT_NAME`-conditional `VERSION` branches (version always comes from the required `tag` input now). Use the exact YAML below — do **not** re-add the `github.event_name == 'workflow_dispatch'` branching from the source; it is dead code here.

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

name: Deploy Sandbox

# Dispatch-only: deploy is a separate human gate from cutting a release
# (spec INV-4). A release (v* tag + GHCR image + GH Release) must already exist
# for the chosen tag.
on:
  workflow_dispatch:
    inputs:
      tag:
        description: "Release tag to deploy (e.g. v0.2.0)"
        required: true
        type: string

permissions:
  contents: read

jobs:
  deploy-sandbox:
    name: Deploy Sandbox
    runs-on: namespace-profile-linux-amd64-2x4
    environment:
      name: prod
      url: https://game.holomush.dev
    if: vars.SANDBOX_DEPLOY_ENABLED == 'true'
    concurrency:
      group: deploy-sandbox
      cancel-in-progress: false
    steps:
      - name: Install doctl
        uses: digitalocean/action-doctl@v2
        with:
          token: ${{ secrets.DIGITALOCEAN_ACCESS_TOKEN }}

      - name: Configure SSH
        env:
          SSH_PRIVATE_KEY: ${{ secrets.DIGITALOCEAN_SSH_PRIVATE_KEY }}
        run: |
          mkdir -p ~/.ssh
          echo "$SSH_PRIVATE_KEY" > ~/.ssh/id_ed25519
          chmod 600 ~/.ssh/id_ed25519
          ssh-keyscan -H "$(doctl compute droplet get holomush-sandbox-game --format PublicIPv4 --no-header)" >> ~/.ssh/known_hosts

      - name: Resolve droplet IP
        id: droplet
        run: |
          IP=$(doctl compute droplet get holomush-sandbox-game --format PublicIPv4 --no-header)
          echo "ip=${IP}" >> "$GITHUB_OUTPUT"

      - name: Pre-deploy Postgres safety snapshot
        env:
          DROPLET_IP: ${{ steps.droplet.outputs.ip }}
          DISPATCH_TAG: ${{ inputs.tag }}
        run: |
          VERSION="$DISPATCH_TAG"
          ssh -o StrictHostKeyChecking=yes "holomush@${DROPLET_IP}" \
            bash -s -- "${VERSION}" <<'EOF'
            set -euo pipefail
            VERSION="$1"
            cd /opt/holomush
            docker compose --profile tunnel --profile backups exec -T backup \
              /usr/local/bin/backup.sh --tag="pre-deploy:${VERSION}"
          EOF

      - name: Pull + migrate + restart
        env:
          DROPLET_IP: ${{ steps.droplet.outputs.ip }}
          DISPATCH_TAG: ${{ inputs.tag }}
        run: |
          VERSION="$DISPATCH_TAG"
          ssh "holomush@${DROPLET_IP}" bash -s -- "${VERSION}" <<'EOF'
            set -euo pipefail
            VERSION="$1"
            if ! command -v git >/dev/null 2>&1; then
              sudo apt-get update -qq
              sudo apt-get install -y -qq git
            fi
            rm -rf /tmp/holomush-release
            git clone --depth 1 --branch "${VERSION}" \
              https://github.com/holomush/holomush.git /tmp/holomush-release
            cp /tmp/holomush-release/compose.prod.yaml /opt/holomush/compose.yaml
            rm -rf /opt/holomush/docker /opt/holomush/deploy
            cp -r /tmp/holomush-release/docker /opt/holomush/docker
            cp -r /tmp/holomush-release/deploy /opt/holomush/deploy
            rm -rf /tmp/holomush-release

            cd /opt/holomush
            IMAGE_VERSION="${VERSION#v}"
            sed -i "s/^HOLOMUSH_VERSION=.*/HOLOMUSH_VERSION=${IMAGE_VERSION}/" .env
            docker compose --profile tunnel --profile backups pull core gateway cloudflared
            docker compose --profile tunnel --profile backups build backup
            docker compose --profile tunnel --profile backups up -d --no-recreate postgres
            docker compose --profile tunnel --profile backups run --rm core migrate
            docker compose --profile tunnel --profile backups up -d
          EOF

      - name: Health probe
        env:
          DROPLET_IP: ${{ steps.droplet.outputs.ip }}
        run: |-
          ssh "holomush@${DROPLET_IP}" \
            "docker compose -f /opt/holomush/compose.yaml exec -T gateway \
               wget -q --spider http://localhost:9101/healthz/readiness"
```

- [ ] **Step 2: Lint the workflow**

Run: `yamlfmt -lint .github/workflows/deploy.yaml && actionlint .github/workflows/deploy.yaml`
Expected: no output, exit 0.

- [ ] **Step 3: Commit**

Run: `jj describe -m "feat(release): dispatch-only deploy.yaml (split from release)"` then `jj new`.

> **Scope note:** this moves the deploy job verbatim; the `holomush-aocap` deploy bug (`--no-recreate` masks stale gateway/core) is explicitly out of scope (spec Non-goals) and is NOT fixed here.

---

### Task 7: Delete release-please configuration

**Files:**

- Delete: `release-please-config.json`
- Delete: `.release-please-manifest.json`

- [ ] **Step 1: Delete the files**

Run:

```bash
rm release-please-config.json .release-please-manifest.json
```

- [ ] **Step 2: Verify nothing references them**

Run: `rg -n "release-please-config|release-please-manifest|release-please-action" . -g '!docs/**'`
Expected: no matches (exit 1). (The `docs/**` exclusion permits historical references in specs/plans.)

- [ ] **Step 3: Commit**

Run: `jj describe -m "chore(release): delete release-please config + manifest"` then `jj new`.

---

### Task 8: Taskfile cleanup — drop CHANGELOG excludes, add dispatch wrappers

**Files:**

- Modify: `Taskfile.yaml:491,652,669` (rumdl excludes) and `Taskfile.yaml:945-953` (release targets)

- [ ] **Step 1: Remove `CHANGELOG.md` from the three rumdl `--exclude` flags**

In `Taskfile.yaml`, change each of these three lines (491, 652, 669) by removing `,CHANGELOG.md` from the exclude list:

Line 491: `- rumdl check --exclude 'site,.git,.beads,.serena,.claude,CHANGELOG.md' .`
→ `- rumdl check --exclude 'site,.git,.beads,.serena,.claude' .`

Line 652: `- rumdl fmt --exclude 'site,.git,.beads,.serena,.claude,CHANGELOG.md' .`
→ `- rumdl fmt --exclude 'site,.git,.beads,.serena,.claude' .`

Line 669: `- rumdl fmt --check --exclude 'site,.git,.beads,.serena,.claude,CHANGELOG.md' .`
→ `- rumdl fmt --check --exclude 'site,.git,.beads,.serena,.claude' .`

(Correct typo guard: the exclude list is exactly `site,.git,.beads,.serena,.claude` on all three lines.)

- [ ] **Step 2: Add `release` and `deploy` dispatch wrapper targets**

After the `release:snapshot` target (`Taskfile.yaml:953`), add:

```yaml
  release:cut:
    desc: "Cut a release via GitHub Actions (cog derives version + tag). Args: increment=auto|major|minor|patch"
    cmds:
      - gh workflow run release.yaml -f expected_increment={{.increment | default "auto"}}

  release:deploy:
    desc: "Deploy a released tag to the sandbox. Required: tag=vX.Y.Z"
    preconditions:
      - sh: '[ -n "{{.tag}}" ]'
        msg: "tag is required, e.g. task release:deploy tag=v0.2.0"
    cmds:
      - gh workflow run deploy.yaml -f tag={{.tag}}
```

- [ ] **Step 3: Verify formatting and that the targets exist**

Run: `task fmt:check && task --list | rg 'release:cut|release:deploy'`
Expected: `fmt:check` passes; both `release:cut` and `release:deploy` appear in the list.

- [ ] **Step 4: Commit**

Run: `jj describe -m "chore(release): drop CHANGELOG rumdl excludes; add release dispatch wrappers"` then `jj new`.

---

## Final verification

- [ ] **All shell tests pass:** `task test:bats`
- [ ] **All workflows lint:** `actionlint` (run via `task lint:actions` or directly on the three workflow files)
- [ ] **GoReleaser config valid:** `task release:check`
- [ ] **Markdown/format gate clean:** `task fmt:check`
- [ ] **Full pre-PR gate:** `task pr-prep` to completion (green) before pushing.

## Notes for the implementer

- **Secrets dependency:** the dispatch path requires `RELEASE_APP_ID` / `RELEASE_APP_PRIVATE_KEY` to be valid and the App to have `contents: write`. These exist (orphaned from an abandoned git-cliff effort) but MUST be validated before the first real dispatch (spec Rollout step 2). The workflows are mergeable without this, but a release dispatch fails fast without it.
- **Squash setting:** INV-5 depends on `squash_merge_commit_title=PR_TITLE` (verified 2026-05-24). It is a GitHub repo setting, not version-controlled — re-assert with `gh api repos/holomush/holomush --jq .squash_merge_commit_title` if releases mis-derive versions.
- **First release is deliberate:** dispatch `release.yaml` with `expected_increment` set (not `auto`) for the first cocogitto release to confirm the computed version before trusting auto.
- **jj commits:** each task ends with `jj describe` + `jj new`. Do not use `git commit` (blocked by guard hook). See `references/vcs-preamble.md`.
- **ADRs:** this plan's architectural decisions are captured as `holomush-jfb9x` (tag-only release), `holomush-dgsts` (release/deploy two gates), `holomush-u2exm` (CI PR-title validation), `holomush-4mmzy` (inline single-run) — see `docs/adr/`.
- **`branch_whitelist` + checkout:** the dispatch path runs from `github.ref = refs/heads/main`, so `actions/checkout@v6` (no explicit `ref:`) lands the job **on the `main` branch** (not detached), and `cog`'s `branch_whitelist = ["main"]` passes. If a future change sets the checkout `ref:` to a SHA/tag on the dispatch path (detached HEAD), `cog bump` may reject with "not on an allowed branch" — a **clean** failure (no tag/remote state created). If that happens, add `git checkout -B main` before the `cog bump` step rather than removing the whitelist.

<!-- adr-capture: sha256=33850d2ed06e3d89; ts=2026-05-24T22:49:09Z; adrs=holomush-jfb9x,holomush-dgsts,holomush-u2exm,holomush-4mmzy -->
