<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Claude PR-Review GitHub Action Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an internal-first GitHub Action that runs the fzymgc-house `dev-flow:review-pr` skill on HoloMUSH PRs via the official `anthropics/claude-code-action`, with a swappable model-provider profile (Anthropic / DeepSeek / homelab LiteLLM).

**Architecture:** A composite action (`.github/actions/claude-pr-review/`) wraps the official action: it installs the toolchain, bootstraps `bd` against the shared Dolt store, resolves a named profile into the Claude Code env bag (passed via the official action's `settings.env`), runs `/review-pr <PR>` headlessly, then `bd dolt push`es findings and gates the check. A thin workflow (`.github/workflows/claude-review.yml`) owns triggers and runner selection via a two-job `resolve → review` structure (profile→runner must be known before the job starts). Findings live in `bd`; v1 posts the skill's summary comment only.

**Tech Stack:** GitHub Actions (composite action + workflow YAML), Bash, `yq`, `bats` (resolver tests), `actionlint`, the `bd`/Dolt CLI (`brew install steveyegge/beads/bd`), `anthropics/claude-code-action@v1`, Claude Code CLI.

**Spec:** `docs/superpowers/specs/2026-05-30-claude-pr-review-action-design.md`
**Design bead:** `holomush-3f5s9`

**Note on TDD shape:** This plan produces YAML + Bash artifacts. Only the profile
resolver (`resolve-profile.sh`) is a unit testable with `bats` — it gets a true
red→green TDD cycle (Task 2). The action/workflow YAML is verified by `yq` parse,
`yamlfmt`, `actionlint` (workflow only), and a documented manual smoke test
(Task 10). Commit after every task.

---

## File structure

| Path | Responsibility |
| --- | --- |
| `.github/actions/claude-pr-review/profiles.yml` | Named provider profiles → `base_url` / `auth_secret` / `runs_on` / model env bag |
| `.github/actions/claude-pr-review/resolve-profile.sh` | Read `profiles.yml` + profile name → emit `runs_on`, `base_url`, and the `settings.env` JSON for the official action |
| `.github/actions/claude-pr-review/action.yml` | Composite action: install → bd bootstrap → resolve env → run official action → bd push → verdict gate |
| `.github/workflows/claude-review.yml` | Triggers, same-repo guard, concurrency, `resolve`→`review` jobs |
| `.github/actionlint.yaml` | Register the homelab self-hosted runner label (`§3.4` prereq) |
| `scripts/tests/resolve-profile.bats` | Unit tests for the resolver |
| `site/src/content/docs/contributing/how-to/claude-pr-review.md` | Operator docs: required secrets, runner setup, triggering, smoke test |

---

## Phase 1: Profile foundation

### Task 1: Profile registry (`profiles.yml`)

**Files:**

- Create: `.github/actions/claude-pr-review/profiles.yml`

- [ ] **Step 1: Create the profiles file**

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Model-provider profiles for the claude-pr-review action.
# Secret VALUES live in GitHub secrets and are referenced by name via
# `auth_secret`; only names appear here. See the design spec §3.
#
# Model IDs below are illustrative and current as of 2026-05-30; re-verify
# against each provider's live API before relying on them (spec §3.2).
profiles:
  anthropic:                       # default; GitHub-hosted runner
    base_url: ""                   # empty → api.anthropic.com
    auth_secret: ANTHROPIC_API_KEY
    runs_on: ubuntu-latest
    env:
      ANTHROPIC_MODEL: claude-opus-4-8
      CLAUDE_CODE_SUBAGENT_MODEL: claude-sonnet-4-6
      CLAUDE_CODE_EFFORT_LEVEL: high

  deepseek:                        # Anthropic-shaped endpoint, no proxy
    base_url: https://api.deepseek.com/anthropic
    auth_secret: DEEPSEEK_API_KEY
    runs_on: ubuntu-latest
    env:
      ANTHROPIC_MODEL: deepseek-v4-pro
      ANTHROPIC_DEFAULT_OPUS_MODEL: deepseek-v4-pro
      ANTHROPIC_DEFAULT_SONNET_MODEL: deepseek-v4-flash
      ANTHROPIC_DEFAULT_HAIKU_MODEL: deepseek-v4-flash
      CLAUDE_CODE_SUBAGENT_MODEL: deepseek-v4-flash
      CLAUDE_CODE_EFFORT_LEVEL: max

  litellm:                         # homelab router; self-hosted runner
    base_url: ${HOMELAB_LITELLM_URL}
    auth_secret: LITELLM_API_KEY
    runs_on: homelab-claude
    env:
      ANTHROPIC_MODEL: PLACEHOLDER-fill-from-litellm-registry
      ANTHROPIC_DEFAULT_OPUS_MODEL: PLACEHOLDER-strong-tier
      ANTHROPIC_DEFAULT_SONNET_MODEL: PLACEHOLDER-default-tier
      ANTHROPIC_DEFAULT_HAIKU_MODEL: PLACEHOLDER-cheap-tier
      CLAUDE_CODE_SUBAGENT_MODEL: PLACEHOLDER-cheap-tier
      CLAUDE_CODE_EFFORT_LEVEL: max
```

> The `litellm` `PLACEHOLDER-*` model names and `${HOMELAB_LITELLM_URL}` are
> intentional operator-config slots, NOT plan placeholders — they are filled
> from the homelab LiteLLM model registry at deploy time (documented in Task 10).
> The `anthropic` and `deepseek` profiles are complete and usable as written
> (pending the §3.2 model-ID re-verification).

- [ ] **Step 2: Verify it parses**

Run: `yq '.profiles | keys' .github/actions/claude-pr-review/profiles.yml`
Expected: `["anthropic", "deepseek", "litellm"]`

- [ ] **Step 3: Commit**

```bash
jj describe -m "feat(ci): claude-pr-review provider profile registry (holomush-3f5s9)"
jj new
```

---

### Task 2: Profile resolver (`resolve-profile.sh`) — TDD with bats

**Files:**

- Create: `.github/actions/claude-pr-review/resolve-profile.sh`
- Test: `scripts/tests/resolve-profile.bats`

The resolver reads `profiles.yml` and a profile name, then writes three
`name=value` lines to the file named by `$GITHUB_OUTPUT` (GitHub Actions step
output convention): `runs_on`, `base_url`, and `settings_env` (a compact JSON
object for the official action's `settings.env`). The auth token is injected by
the caller (the action), not the resolver, so the resolver never touches secrets.

- [ ] **Step 1: Write the failing bats test**

```bash
#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0

setup() {
  RESOLVER="$BATS_TEST_DIRNAME/../../.github/actions/claude-pr-review/resolve-profile.sh"
  PROFILES="$BATS_TEST_DIRNAME/../../.github/actions/claude-pr-review/profiles.yml"
  GITHUB_OUTPUT="$(mktemp)"
  export GITHUB_OUTPUT
}

teardown() { rm -f "$GITHUB_OUTPUT"; }

@test "resolves anthropic profile to ubuntu-latest with empty base_url" {
  run bash "$RESOLVER" anthropic "$PROFILES"
  [ "$status" -eq 0 ]
  grep -q '^runs_on=ubuntu-latest$' "$GITHUB_OUTPUT"
  grep -q '^base_url=$' "$GITHUB_OUTPUT"
  grep -q 'ANTHROPIC_MODEL' "$GITHUB_OUTPUT"
}

@test "resolves deepseek profile with the deepseek anthropic base_url" {
  run bash "$RESOLVER" deepseek "$PROFILES"
  [ "$status" -eq 0 ]
  grep -q '^base_url=https://api.deepseek.com/anthropic$' "$GITHUB_OUTPUT"
}

@test "emits settings_env as a single-line JSON object" {
  run bash "$RESOLVER" deepseek "$PROFILES"
  [ "$status" -eq 0 ]
  line="$(grep '^settings_env=' "$GITHUB_OUTPUT" | head -1)"
  json="${line#settings_env=}"
  echo "$json" | yq -p=json '.ANTHROPIC_DEFAULT_SONNET_MODEL' | grep -q 'deepseek-v4-flash'
}

@test "exits non-zero with a message for an unknown profile" {
  run bash "$RESOLVER" no-such-profile "$PROFILES"
  [ "$status" -ne 0 ]
  [[ "$output" == *"unknown profile"* ]]
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `bats scripts/tests/resolve-profile.bats`
Expected: FAIL — resolver script does not exist yet.

- [ ] **Step 3: Write the resolver**

```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Resolve a claude-pr-review provider profile into GitHub Actions step outputs.
# Usage: resolve-profile.sh <profile-name> [profiles.yml path]
# Writes runs_on, base_url, settings_env (JSON) to $GITHUB_OUTPUT.
# Does NOT handle secrets — the auth token is injected by the caller.
set -euo pipefail

profile="${1:?usage: resolve-profile.sh <profile-name> [profiles.yml]}"
profiles_file="${2:-"$(dirname "$0")/profiles.yml"}"

if [[ -z "$(yq ".profiles.\"$profile\"" "$profiles_file")" || \
      "$(yq ".profiles.\"$profile\"" "$profiles_file")" == "null" ]]; then
  echo "resolve-profile: unknown profile '$profile'" >&2
  exit 1
fi

# Plain yq (NOT -o=json) for scalar fields — -o=json double-quotes the string
# (`"ubuntu-latest"`), which breaks `runs-on` and the bats assertions.
runs_on="$(yq ".profiles.\"$profile\".runs_on" "$profiles_file")"
base_url="$(yq ".profiles.\"$profile\".base_url" "$profiles_file")"
# base_url may carry a ${HOMELAB_LITELLM_URL}-style ref; expand from env.
base_url="$(eval "echo \"$base_url\"")"
settings_env="$(yq -o=json -I=0 ".profiles.\"$profile\".env" "$profiles_file")"

{
  echo "runs_on=$runs_on"
  echo "base_url=$base_url"
  echo "settings_env=$settings_env"
} >> "$GITHUB_OUTPUT"
```

- [ ] **Step 4: Make the resolver executable**

Run: `chmod +x .github/actions/claude-pr-review/resolve-profile.sh`

- [ ] **Step 5: Run the test to verify it passes**

Run: `bats scripts/tests/resolve-profile.bats`
Expected: PASS (4 tests). The resolver uses plain `yq` for `runs_on`/`base_url`
(unquoted scalars) and `yq -o=json -I=0` only for the `env` object, so the
`^runs_on=ubuntu-latest$` assertion matches the bare value.

> Requires `yq` (mikefarah) on PATH. If `task test:bats` is run via the repo's
> environment it is present; if running this single suite ad hoc, install per
> the pattern in Task 4 Step 1b first.

- [ ] **Step 6: Run the repo bats gate**

Run: `task test:bats`
Expected: PASS — all bats suites including the new one.

- [ ] **Step 7: Commit**

```bash
jj describe -m "feat(ci): profile resolver for claude-pr-review action (holomush-3f5s9)"
jj new
```

---

### Task 3: Register the homelab runner label (spec §3.4 prereq)

**Files:**

- Modify: `.github/actionlint.yaml`

- [ ] **Step 1: Add the homelab label**

Edit `.github/actionlint.yaml` so `self-hosted-runner.labels` includes the
homelab runner label used by the `litellm` profile:

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

self-hosted-runner:
  labels:
    - namespace-profile-linux-amd64-2x4
    - namespace-profile-linux-amd64-4x8
    - namespace-profile-linux-amd64-8x16
    - homelab-claude
```

- [ ] **Step 2: Verify actionlint config is valid**

`actionlint` is pinned in `go.tool-lint.mod` (not the main module), so invoke it
through the repo's wiring. Either run the full lint gate:

Run: `task lint`

…or invoke actionlint directly with the correct modfile (the form `task lint`
uses via `GO_TOOL_LINT`):

Run: `GOWORK=off go tool -modfile=go.tool-lint.mod actionlint -config-file .github/actionlint.yaml .github/workflows/*`
Expected: exit 0 (no workflow references `homelab-claude` yet, so findings are
unchanged — this step just confirms the config still parses).

- [ ] **Step 3: Commit**

```bash
jj describe -m "ci(actionlint): register homelab-claude self-hosted runner label (holomush-3f5s9)"
jj new
```

---

## Phase 2: Composite action

### Task 4: Composite action skeleton — metadata, inputs, toolchain install

**Files:**

- Create: `.github/actions/claude-pr-review/action.yml`

- [ ] **Step 1: Write the action metadata + inputs + install steps**

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
name: Claude PR Review
description: >-
  Run the fzymgc-house dev-flow:review-pr skill on a pull request via Claude
  Code, with a swappable model-provider profile. Bootstraps bd against the
  shared Dolt store and posts a summary review comment.

inputs:
  pr_number:
    description: PR number to review.
    required: true
  model_profile:
    description: Profile name from profiles.yml (anthropic | deepseek | litellm).
    required: false
    default: anthropic
  auth_token:
    description: >-
      Provider auth token (the secret named by the profile's auth_secret).
      Used as ANTHROPIC_AUTH_TOKEN and as the official action's anthropic_api_key.
    required: true
  aspects:
    description: Aspects forwarded to /review-pr (all | code | security | ...).
    required: false
    default: all
  post_comment:
    description: Auto-post the summary comment headlessly.
    required: false
    default: "true"
  fail_on:
    description: none | changes_requested — whether the verdict fails the check.
    required: false
    default: none
  dolt_remote_url:
    description: Dolt remote URL (with credentials) for bd bootstrap/push.
    required: true
  github_token:
    description: Token used by the skill's gh calls (pr comment / api).
    required: true

runs:
  using: composite
  steps:
    - name: Set up Go (for yq build)
      uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6
      with:
        go-version-file: go.mod
        cache: false

    - name: Install yq (mikefarah)
      # The resolver and the verdict step parse YAML/JSON with mikefarah yq.
      # Built from go.tool.mod (pinned) via the module proxy — mirrors
      # .github/workflows/scripts-tests.yaml; avoids the GitHub Releases CDN
      # flake class and Ubuntu's incompatible python `yq`.
      shell: bash
      run: |
        set -euo pipefail
        mkdir -p "$HOME/.local/bin"
        GOWORK=off go build -modfile=go.tool.mod \
          -o "$HOME/.local/bin/yq" github.com/mikefarah/yq/v4
        echo "$HOME/.local/bin" >> "$GITHUB_PATH"
        "$HOME/.local/bin/yq" --version

    - name: Install Node and Claude Code CLI
      shell: bash
      run: |
        set -euo pipefail
        npm install -g @anthropic-ai/claude-code
        claude --version

    - name: Install bd (beads)
      shell: bash
      run: |
        set -euo pipefail
        brew install steveyegge/beads/bd
        bd version
```

> **Step 1b (above):** the Go+yq install block is the canonical way to get
> `yq` on PATH in this repo (grounded against `scripts-tests.yaml`). The
> resolver (Task 6) and verdict step (Task 8) both depend on it, so it MUST
> precede them. `actions/checkout` is run by the calling workflow (Task 9), so
> `go.mod`/`go.tool.mod` are present when this step runs.

- [ ] **Step 2: Verify the YAML parses**

Run: `yq '.inputs | keys' .github/actions/claude-pr-review/action.yml`
Expected: a list including `pr_number`, `model_profile`, `auth_token`,
`aspects`, `post_comment`, `fail_on`, `dolt_remote_url`, `github_token`.

- [ ] **Step 3: Format**

Run: `task fmt`
Expected: no diff (or an applied yamlfmt normalization that you then include).

- [ ] **Step 4: Commit**

```bash
jj describe -m "feat(ci): claude-pr-review composite action skeleton + toolchain install (holomush-3f5s9)"
jj new
```

---

### Task 5: Composite action — bd bootstrap step

**Files:**

- Modify: `.github/actions/claude-pr-review/action.yml` (append a step after "Install bd")

- [ ] **Step 1: Add the bd bootstrap step**

Insert after the "Install bd (beads)" step, before any review step:

```yaml
    - name: Bootstrap beads from shared Dolt
      shell: bash
      env:
        DOLT_REMOTE_URL: ${{ inputs.dolt_remote_url }}
      run: |
        set -euo pipefail
        # CI=true makes bd bootstrap non-interactive automatically.
        bd dolt remote add origin "$DOLT_REMOTE_URL"
        bd bootstrap --yes
        bd where        # diagnostic: confirm the active DB location
```

- [ ] **Step 2: Verify the YAML still parses**

Run: `yq '.runs.steps[].name' .github/actions/claude-pr-review/action.yml`
Expected: the step list now includes `Bootstrap beads from shared Dolt`.

- [ ] **Step 3: Commit**

```bash
jj describe -m "feat(ci): bd bootstrap step in claude-pr-review action (holomush-3f5s9)"
jj new
```

---

### Task 6: Composite action — resolve profile into env

**Files:**

- Modify: `.github/actions/claude-pr-review/action.yml` (append a resolve step)

- [ ] **Step 1: Add the resolve step**

Insert after the bootstrap step:

```yaml
    - name: Resolve model profile
      id: profile
      shell: bash
      env:
        HOMELAB_LITELLM_URL: ${{ vars.HOMELAB_LITELLM_URL }}
      run: |
        set -euo pipefail
        "${{ github.action_path }}/resolve-profile.sh" \
          "${{ inputs.model_profile }}" \
          "${{ github.action_path }}/profiles.yml"
```

This populates `steps.profile.outputs.runs_on`, `.base_url`, `.settings_env`
(the resolver writes to `$GITHUB_OUTPUT`).

- [ ] **Step 2: Verify YAML parses and the step id is wired**

Run: `yq '.runs.steps[] | select(.id == "profile") | .name' .github/actions/claude-pr-review/action.yml`
Expected: `Resolve model profile`

- [ ] **Step 3: Commit**

```bash
jj describe -m "feat(ci): resolve model profile into env in claude-pr-review action (holomush-3f5s9)"
jj new
```

---

### Task 7: Composite action — run the official action (headless review)

**Files:**

- Modify: `.github/actions/claude-pr-review/action.yml` (append the official-action step)

- [ ] **Step 1: Pin the official action SHA**

Resolve and record the current `v1` commit SHA (repo convention is SHA-pinning
with a version comment):

```bash
gh api repos/anthropics/claude-code-action/git/refs/tags/v1 \
  --jq '.object.sha'
```

Note the SHA for the next step's `uses:` line (replace `PINNED_SHA`).

- [ ] **Step 2: Add the official-action step**

Insert after the resolve step. The whole profile env bag goes through
`settings.env`; `ANTHROPIC_AUTH_TOKEN` is merged in from the `auth_token` input
(the resolver intentionally never sees the secret). The prompt carries the
headless post override (v1 bridge for upstream issue #128).

```yaml
    - name: Run review via claude-code-action
      uses: anthropics/claude-code-action@PINNED_SHA # v1
      with:
        anthropic_api_key: ${{ inputs.auth_token }}
        # plugin_marketplaces takes Git URLs (newline-separated); plugins takes
        # bare plugin names (newline-separated). NOT owner/repo and NOT
        # name@marketplace (grounded against the action source, bead note round3).
        plugin_marketplaces: https://github.com/fzymgc-house/fzymgc-house-skills.git
        plugins: dev-flow
        settings: |
          {
            "env": ${{ steps.profile.outputs.settings_env }}
          }
        prompt: |
          /review-pr ${{ inputs.pr_number }} ${{ inputs.aspects }}

          You are running headless in CI. There is no human to confirm.
          When review-pr reaches its "offer to post" step, post the summary
          comment without asking (post_comment=${{ inputs.post_comment }}).
          Do not wait for any interactive input.
      env:
        ANTHROPIC_BASE_URL: ${{ steps.profile.outputs.base_url }}
        ANTHROPIC_AUTH_TOKEN: ${{ inputs.auth_token }}
        GH_TOKEN: ${{ inputs.github_token }}
        GITHUB_TOKEN: ${{ inputs.github_token }}
```

> **Implementer note (spec §3.3):** `settings.env` is the channel that survives
> composite-action env shadowing. The job-level `env:` here sets
> `ANTHROPIC_BASE_URL` (which the action explicitly forwards) and the `GH_TOKEN`
> the skill's `gh` calls need. If a future action version stops forwarding
> `ANTHROPIC_BASE_URL`, move it into the `settings.env` JSON (it is already there
> for the model vars). The `auth_token` is set both as `anthropic_api_key`
> (satisfies `validateEnvironmentVariables`) and `ANTHROPIC_AUTH_TOKEN` (bearer
> path for LiteLLM); DeepSeek accepts the x-api-key path too.

- [ ] **Step 3: Verify the YAML parses**

Run: `yq '.runs.steps[] | select(.uses) | .uses' .github/actions/claude-pr-review/action.yml`
Expected: the pinned `anthropics/claude-code-action@<sha>` reference.

- [ ] **Step 4: Commit**

```bash
jj describe -m "feat(ci): invoke claude-code-action for headless review (holomush-3f5s9)"
jj new
```

---

### Task 8: Composite action — persist findings + verdict gate

**Files:**

- Modify: `.github/actions/claude-pr-review/action.yml` (append push + gate steps)

The verdict reproduces review-pr's own logic (spec §5.3): count open findings on
the PR's review epic labelled `severity:critical` or `severity:important`.

- [ ] **Step 1: Add the push + gate steps**

Insert after the review step:

```yaml
    - name: Persist findings to shared Dolt
      if: always()
      shell: bash
      run: |
        set -euo pipefail
        # Pull immediately before push to minimise the concurrent-writer window;
        # retry once on conflict (spec §5.3).
        bd dolt pull || true
        bd dolt push || { bd dolt pull && bd dolt push; }

    - name: Compute verdict and gate
      id: verdict
      shell: bash
      run: |
        set -euo pipefail
        epic="$(bd list --label "pr-review,pr:${{ inputs.pr_number }}" --json \
          | yq -p=json '.[0].id')"
        if [[ -z "$epic" || "$epic" == "null" ]]; then
          echo "verdict=unknown" >> "$GITHUB_OUTPUT"
          echo "No review epic found for PR ${{ inputs.pr_number }}." >&2
          exit 0
        fi
        must_fix="$(bd list --parent "$epic" --status open --json \
          | yq -p=json '[.[] | select(.labels[] == "severity:critical" or .labels[] == "severity:important")] | length')"
        echo "verdict_count=$must_fix" >> "$GITHUB_OUTPUT"
        if [[ "$must_fix" -gt 0 ]]; then
          echo "verdict=changes_requested" >> "$GITHUB_OUTPUT"
          if [[ "${{ inputs.fail_on }}" == "changes_requested" ]]; then
            echo "::error::Review verdict CHANGES REQUESTED ($must_fix must-fix finding(s))."
            exit 1
          fi
        else
          echo "verdict=pass" >> "$GITHUB_OUTPUT"
        fi
```

- [ ] **Step 2: Verify YAML parses**

Run: `yq '.runs.steps[].name' .github/actions/claude-pr-review/action.yml`
Expected: list now ends with `Persist findings to shared Dolt` and
`Compute verdict and gate`.

- [ ] **Step 3: Format and commit**

```bash
task fmt
jj describe -m "feat(ci): persist findings + verdict gate in claude-pr-review action (holomush-3f5s9)"
jj new
```

---

## Phase 3: Workflow and operator docs

### Task 9: The workflow (`claude-review.yml`)

**Files:**

- Create: `.github/workflows/claude-review.yml`

Two jobs: `resolve` (always `ubuntu-latest`) outputs the runner label + the
auth-secret name; `review` runs on that label and calls the composite action.
The same-repo guard (spec §5.1) lives on the auto trigger; fork PRs run only via
the `claude-review` label.

- [ ] **Step 1: Write the workflow**

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

name: Claude Review

on:
  workflow_dispatch:
    inputs:
      pr_number:
        description: PR number to review
        required: true
      model_profile:
        description: Profile (anthropic | deepseek | litellm)
        required: false
        default: anthropic
      aspects:
        description: Aspects for /review-pr
        required: false
        default: all
  pull_request:
    types: [opened, synchronize, labeled]

permissions:
  contents: read
  pull-requests: write
  issues: write

concurrency:
  group: claude-review-${{ github.event.pull_request.number || inputs.pr_number }}
  cancel-in-progress: true

jobs:
  resolve:
    # Run unless this is a fork PR push without the claude-review label.
    if: >-
      github.event_name == 'workflow_dispatch' ||
      (github.event.pull_request.head.repo.full_name == github.repository &&
       github.event.action != 'labeled') ||
      (github.event.action == 'labeled' &&
       github.event.label.name == 'claude-review')
    runs-on: ubuntu-latest
    outputs:
      runs_on: ${{ steps.r.outputs.runs_on }}
      profile: ${{ steps.r.outputs.profile }}
      auth_secret: ${{ steps.r.outputs.auth_secret }}
      pr_number: ${{ steps.r.outputs.pr_number }}
    steps:
      - uses: actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd # v6
      - uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6
        with:
          go-version-file: go.mod
          cache: false
      - name: Install yq (mikefarah)
        run: |
          set -euo pipefail
          mkdir -p "$HOME/.local/bin"
          GOWORK=off go build -modfile=go.tool.mod \
            -o "$HOME/.local/bin/yq" github.com/mikefarah/yq/v4
          echo "$HOME/.local/bin" >> "$GITHUB_PATH"
      - id: r
        env:
          HOMELAB_LITELLM_URL: ${{ vars.HOMELAB_LITELLM_URL }}
        run: |
          set -euo pipefail
          profile="${{ github.event.inputs.model_profile || 'anthropic' }}"
          pr="${{ github.event.inputs.pr_number || github.event.pull_request.number }}"
          dir=.github/actions/claude-pr-review
          # Resolve runs_on for the chosen profile.
          "$dir/resolve-profile.sh" "$profile" "$dir/profiles.yml"
          # resolve-profile.sh already appended runs_on/base_url/settings_env;
          # add the auth-secret name and pr_number for the review job.
          auth_secret="$(yq ".profiles.\"$profile\".auth_secret" "$dir/profiles.yml")"
          echo "profile=$profile" >> "$GITHUB_OUTPUT"
          echo "auth_secret=$auth_secret" >> "$GITHUB_OUTPUT"
          echo "pr_number=$pr" >> "$GITHUB_OUTPUT"

  review:
    needs: resolve
    runs-on: ${{ needs.resolve.outputs.runs_on }}
    steps:
      - uses: actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd # v6
      - uses: ./.github/actions/claude-pr-review
        with:
          pr_number: ${{ needs.resolve.outputs.pr_number }}
          model_profile: ${{ needs.resolve.outputs.profile }}
          aspects: ${{ github.event.inputs.aspects || 'all' }}
          auth_token: ${{ secrets[needs.resolve.outputs.auth_secret] }}
          dolt_remote_url: ${{ secrets.DOLT_REMOTE_URL }}
          github_token: ${{ secrets.GITHUB_TOKEN }}
          fail_on: none
```

> **Implementer notes:**
> - `actions/checkout` and `actions/setup-go` are SHA-pinned above to the same
>   SHAs every other repo workflow uses (`de0fac2…` / `4a3601…`). Confirm they
>   still match (`rg 'actions/checkout@' .github/workflows`) before committing.
> - `secrets[needs.resolve.outputs.auth_secret]` uses dynamic secret indexing —
>   GitHub evaluates `secrets[<expr>]` in the `with:` context. Confirm during the
>   smoke test (Task 10) that the resolved name maps to a configured secret.
> - The `resolve` job's `runs_on` output comes from the resolver's
>   `$GITHUB_OUTPUT` write; the explicit `echo` lines add the secret name + PR.

- [ ] **Step 2: Lint the workflow**

Run: `GOWORK=off go tool -modfile=go.tool-lint.mod actionlint -config-file .github/actionlint.yaml .github/workflows/claude-review.yml`
Expected: exit 0. (`GOWORK=off -modfile=go.tool-lint.mod` is required — `actionlint`
lives in `go.tool-lint.mod`, not the main module, and `go.work` mode breaks
`-modfile`.) If actionlint flags `secrets[...]` dynamic indexing, prefer a static
`fromJSON` map over a disable directive.

- [ ] **Step 3: Run the repo lint gate**

Run: `task lint`
Expected: PASS (actionlint + yamlfmt clean).

- [ ] **Step 4: Commit**

```bash
task fmt
jj describe -m "feat(ci): claude-review workflow (resolve+review jobs, triggers, guards) (holomush-3f5s9)"
jj new
```

---

### Task 10: Operator docs + manual smoke test

**Files:**

- Create: `site/src/content/docs/contributing/how-to/claude-pr-review.md`
  (the `contributing/` dir holds only `index.mdx` + `how-to/`/`explanation/`/`reference/` subdirs; the doc goes under `how-to/`)

- [ ] **Step 1: Write the operator doc**

````markdown
---
title: Claude PR Review action
description: Running the automated Claude PR-review GitHub Action.
---

The `claude-review` workflow runs the `dev-flow:review-pr` skill on a PR via
Claude Code and posts a summary review comment.

## Required secrets

| Secret | Used for |
| --- | --- |
| `ANTHROPIC_API_KEY` | `anthropic` profile auth |
| `DEEPSEEK_API_KEY` | `deepseek` profile auth |
| `LITELLM_API_KEY` | `litellm` profile auth |
| `DOLT_REMOTE_URL` | `bd` bootstrap/push against the shared Dolt store |

## Required variables

| Variable | Used for |
| --- | --- |
| `HOMELAB_LITELLM_URL` | base URL of the homelab LiteLLM Anthropic endpoint |

## Runners

- `anthropic` / `deepseek` profiles run on `ubuntu-latest` (GitHub-hosted).
- `litellm` runs on a self-hosted runner labelled `homelab-claude` that can reach
  the homelab LiteLLM and Dolt server. Register that label in
  `.github/actionlint.yaml` (already done) and bring up a runner with it.

## Triggering

- **Manual:** Actions → Claude Review → Run workflow → enter PR number + profile.
- **Auto (same-repo PRs):** runs on PR open/synchronize.
- **Fork PRs:** apply the `claude-review` label to opt a fork PR in.

## Cost control

The `deepseek` profile (and any `litellm` profile pointing at a cheap tier) keeps
routine reviews inexpensive; `CLAUDE_CODE_SUBAGENT_MODEL` puts the ~8 review-pr
aspect agents on the cheap tier. Rate-limit fallback (Anthropic → LiteLLM) is
planned for v2.
````

- [ ] **Step 2: Fill the litellm profile model names**

Edit `.github/actions/claude-pr-review/profiles.yml`: replace the `litellm`
`PLACEHOLDER-*` model values with real model identifiers from the homelab
LiteLLM model registry (e.g. the names LiteLLM exposes for the chosen
OpenRouter/Ollama backends). Leave `${HOMELAB_LITELLM_URL}` as-is (resolved from
the repo variable at runtime).

- [ ] **Step 3: Verify the §3.2 model IDs against live APIs**

Re-confirm the `anthropic` and `deepseek` profile model identifiers against each
provider's current model list before the first real run (spec §3.2). Fix any
that have changed.

- [ ] **Step 4: Manual smoke test (cheap profile, throwaway PR)**

1. Configure `DEEPSEEK_API_KEY` + `DOLT_REMOTE_URL` secrets in the repo.
2. Open a tiny throwaway PR.
3. Actions → Claude Review → Run workflow → PR number + `model_profile: deepseek`.
4. Confirm: the `resolve` job picks `ubuntu-latest`; the `review` job bootstraps
   `bd`, runs `/review-pr`, and a summary comment appears on the PR carrying the
   `<!-- pr-review:<bead-id> -->` marker.
5. Re-run on the same PR; confirm the comment updates in place (idempotency,
   spec §5.4) rather than duplicating.
6. `bd show <epic-id>` against the shared store; confirm findings persisted via
   `bd dolt push`.

Record the smoke-test outcome in a `bd note holomush-3f5s9`.

- [ ] **Step 5: Lint docs and commit**

```bash
task lint
task fmt
jj describe -m "docs(ci): claude-pr-review operator guide + litellm profile fill (holomush-3f5s9)"
jj new
```

---

## Out of scope (v1 — per spec §1.2)

- Inline per-line review comments (upstream issue #129; v2).
- Rate-limit fallback orchestration (Anthropic → LiteLLM on 429; v2).
- `pull_request_target` privileged fork review with secrets.
- Multi-PR batch review.
- The raw Claude Code CLI invocation path (spec §7; documented fallback only).
<!-- adr-capture: sha256=15220a6f7a8c5382; session=cli; ts=2026-05-30T21:23:26Z; adrs= -->
