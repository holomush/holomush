<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Claude PR Review GitHub Action — Design

- **Bead:** holomush-3f5s9
- **Status:** Draft (pending design-reviewer gate)
- **Date:** 2026-05-30
- **Author:** Sean Brandt (with Claude Opus 4.8)

## 1. Summary

A GitHub Action that runs automated code review on HoloMUSH pull requests by
driving the fzymgc-house **`dev-flow:review-pr`** skill through Claude Code in a
runner, with a **configurable model-provider profile** so reviews can run on
Anthropic directly, on DeepSeek's Anthropic-compatible endpoint, or through a
homelab **LiteLLM** router (which fans out to OpenRouter / Ollama / cost-tiered
models).

The action is **internal-first** — built inside the HoloMUSH repo cleanly enough
to be extracted into a standalone reusable action later. Its motivations are
**cost control** (cheap models on routine PRs) and **rate-limit resilience**
(fall back to another provider when Anthropic is throttled), leveraging existing
homelab infrastructure (LiteLLM + a shared Dolt server) reached via a
self-hosted runner.

### 1.1 Goals

- Run `dev-flow:review-pr` end-to-end in CI: gather PR context, dispatch the
  aspect agents, file findings, compute the verdict, post a summary comment.
- Make the model provider a single, named, swappable **profile**.
- Persist review findings to the shared `bd`/Dolt store (`bd dolt pull/push`).
- Keep the GitHub-plumbing surface (auth, comment posting, plugin loading) on
  the official `anthropics/claude-code-action`, not hand-rolled.

### 1.2 Non-goals (v1)

YAGNI — explicitly deferred:

- Inline per-line review comments (summary comment only in v1; inline is v2).
- Rate-limit fallback orchestration (single profile per run in v1).
- `pull_request_target` / privileged fork-PR review with secrets.
- Multi-PR batch review.
- The raw-CLI invocation path (documented as a fallback, not built).

## 2. Architecture

Two units with a clean seam: a **workflow** owns *when/where*, a **composite
action** owns *how* and is the extractable artifact.

```text
.github/
├── workflows/
│   └── claude-review.yml          # triggers, runner selection, profile resolution
└── actions/
    └── claude-pr-review/
        ├── action.yml             # composite: bootstrap → official action → push
        └── profiles.yml           # model-provider profile registry
```

### 2.1 Workflow — `claude-review.yml`

Owns *when* a review runs and *where* (which runner):

- **Triggers:**
  - `workflow_dispatch` — manual, inputs `pr_number`, `model_profile`, `aspects`.
  - `pull_request: [opened, synchronize]` — auto-review, **same-repo branches
    only** (see §5.1).
  - `pull_request` label event — review fork PRs only when a maintainer applies
    the `claude-review` label.
- **Concurrency:** `group: claude-review-${{ pr_number }}`,
  `cancel-in-progress: true` — a new push supersedes an in-flight review.
- **Runner selection:** `runs-on` is taken from the resolved profile
  (self-hosted homelab label for homelab profiles; `ubuntu-latest` for direct
  Anthropic/DeepSeek).
- Passes `pr_number`, resolved `model_profile`, and `aspects` to the composite
  action.

The workflow knows nothing about LiteLLM, Dolt, or the official action's
internals — it names a profile and a PR. That ignorance is what keeps the
composite action extractable.

### 2.2 Composite action — `claude-pr-review/action.yml`

Owns *how*. Public input contract:

| Input | Default | Purpose |
|---|---|---|
| `pr_number` | — | PR to review |
| `model_profile` | `anthropic` | profile name → resolves base_url / model env / secret / runner |
| `aspects` | `all` | forwarded to `/review-pr` |
| `post_comment` | `true` | headless auto-post the summary comment |
| `fail_on` | `none` | `none` \| `changes_requested` — gate the check on the verdict |
| `beads_remote` | (secret ref) | Dolt remote for `bd dolt pull/push` |

## 3. Model-provider profiles

The "configurable model" surface reduces to a **profile registry**. A profile is
not a single model — it is the full Claude Code **tiering env bag** plus a base
URL, an auth-secret name, and a runner hint.

### 3.1 Why the full env bag

`dev-flow:review-pr` dispatches its aspect agents by **logical tier name**: its
Step 5 defaults agents to `sonnet` and escalates specific agents (security,
crypto, large diffs) to `opus`, passing `model: sonnet` / `model: opus` to each
`Task`. Against a non-Anthropic backend those literal names are meaningless
unless Claude Code's tier env vars map them. Those env vars are therefore the
**translation layer** that lets the skill run **unmodified** on any provider:

| Env var | Role | review-pr consumer |
|---|---|---|
| `ANTHROPIC_MODEL` | main / orchestrator model | the `/review-pr` driver |
| `ANTHROPIC_DEFAULT_SONNET_MODEL` | resolves logical "sonnet" | default aspect agents |
| `ANTHROPIC_DEFAULT_OPUS_MODEL` | resolves logical "opus" | escalated agents |
| `ANTHROPIC_DEFAULT_HAIKU_MODEL` | resolves logical "haiku" | background / cheap calls |
| `CLAUDE_CODE_SUBAGENT_MODEL` | subagent model override | the fanned-out aspect agents |
| `CLAUDE_CODE_EFFORT_LEVEL` | reasoning effort | whole run |

`CLAUDE_CODE_SUBAGENT_MODEL` is the primary **cost lever**: review-pr fans out
~8 aspect agents; pointing them at a cheaper tier while the orchestrator runs a
stronger model directly serves the cost-control motivation.

### 3.2 Storage — checked-in `profiles.yml`

Because a profile carries six-plus env vars, a checked-in `profiles.yml` is the
natural home (a `case` statement or per-call workflow inputs would be unwieldy).
Secret **values** live in GitHub secrets and are referenced by name; only names
appear in the file.

```yaml
profiles:
  anthropic:                       # default; GitHub-hosted runner OK
    base_url: ""                   # unset → api.anthropic.com
    auth_secret: ANTHROPIC_API_KEY
    runs_on: ubuntu-latest
    env:
      ANTHROPIC_MODEL: claude-opus-4-8
      CLAUDE_CODE_SUBAGENT_MODEL: claude-sonnet-4-6
      CLAUDE_CODE_EFFORT_LEVEL: high

  deepseek:                        # Anthropic-shaped endpoint, no proxy, hosted OK
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

  litellm:                         # homelab router → OpenRouter / Ollama / cost tiers
    base_url: ${{ vars.HOMELAB_LITELLM_URL }}
    auth_secret: LITELLM_API_KEY
    runs_on: [self-hosted, homelab-claude]   # MUST be registered (see §3.4)
    env:
      ANTHROPIC_MODEL: <litellm model name>          # fill from homelab LiteLLM model registry
      ANTHROPIC_DEFAULT_OPUS_MODEL: <strong tier>
      ANTHROPIC_DEFAULT_SONNET_MODEL: <default tier>
      ANTHROPIC_DEFAULT_HAIKU_MODEL: <cheap tier>
      CLAUDE_CODE_SUBAGENT_MODEL: <cheap tier>
      CLAUDE_CODE_EFFORT_LEVEL: max
```

> **Model IDs above are illustrative** and current as of 2026-05-30
> (`claude-opus-4-8`/`claude-sonnet-4-6` are the repo's current models per
> CLAUDE.md; DeepSeek's Anthropic-endpoint mapping is `claude-opus*` →
> `deepseek-v4-pro`, `claude-sonnet*`/`claude-haiku*` → `deepseek-v4-flash` per
> grounding §9). The plan MUST re-verify the exact model identifiers against each
> provider's live API before pinning them, and fill the `litellm` placeholders
> from the homelab LiteLLM model registry.

### 3.4 Runner-label prerequisite (homelab profiles)

The repo's `.github/actionlint.yaml` registers **only**
`namespace-profile-linux-amd64-{2x4,4x8,8x16}` as valid self-hosted runner
labels; `task lint` runs `actionlint`, which fails on any unregistered label.
The homelab self-hosted runner is the user's own infrastructure (not a Namespace
runner), so the `litellm` profile's label (`homelab-claude` above) is **not**
currently valid.

**Prerequisite (a plan task):** register the homelab runner's label in
`.github/actionlint.yaml` under `self-hosted-runner.labels` **before** the
`litellm` profile workflow can pass lint. The `anthropic` and `deepseek`
profiles run on `ubuntu-latest` (GitHub-hosted) and need no registration.

### 3.3 How the profile reaches the CLI through the official action

The base-URL override works **through** the official action — verified against
its source (grounding, §9). Two facts make this clean:

1. `validateEnvironmentVariables()` on the **default Anthropic provider** (no
   `use_bedrock`/`use_vertex`/`use_foundry`) only requires that
   `ANTHROPIC_API_KEY` *or* `CLAUDE_CODE_OAUTH_TOKEN` is present. It does **not**
   reject `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN`, or the
   `ANTHROPIC_DEFAULT_*_MODEL` vars.
2. The action explicitly forwards `ANTHROPIC_BASE_URL` and the default-model env
   vars to the CLI subprocess, and exposes a **`settings` input** accepting a
   JSON `"env"` object — the recommended way to pass arbitrary env to the CLI.

**Therefore the composite action passes the whole profile env bag via the
action's `settings.env`** (not a step-level `env:` block — composite-action env
shadowing would hide a step block from the CLI; `settings.env`, job-level
`env:`, or a prior `$GITHUB_ENV` write are the working channels):

```yaml
- uses: anthropics/claude-code-action@v1
  with:
    anthropic_api_key: ${{ secrets[profile.auth_secret] }}   # satisfies validation
    settings: |
      { "env": {
          "ANTHROPIC_BASE_URL": "<profile.base_url>",
          "ANTHROPIC_AUTH_TOKEN": "<profile.auth_secret value>",
          "ANTHROPIC_MODEL": "...", "ANTHROPIC_DEFAULT_SONNET_MODEL": "...",
          "ANTHROPIC_DEFAULT_OPUS_MODEL": "...", "CLAUDE_CODE_SUBAGENT_MODEL": "...",
          "CLAUDE_CODE_EFFORT_LEVEL": "max"
      } }
```

**Auth nuance (per-endpoint config, not architectural):** Claude Code sends
`ANTHROPIC_API_KEY` as `x-api-key` and `ANTHROPIC_AUTH_TOKEN` as
`Authorization: Bearer`. DeepSeek's endpoint accepts `x-api-key` (its compat
table lists it "Fully Supported"), so `anthropic_api_key` alone may suffice
there; LiteLLM commonly expects a bearer token, so set `ANTHROPIC_AUTH_TOKEN`
via `settings.env`. Setting both (API key to satisfy validation, AUTH_TOKEN for
the bearer path) is the safe default the plan should adopt unless an endpoint
proves otherwise.

The raw-CLI path (§7) is now a true last resort, retained only against a future
action version tightening provider validation.

## 4. Data flow

```text
trigger (dispatch | same-repo PR push | claude-review label)
        │
        ▼
workflow resolves profile → selects runner → calls composite action
        │
        ▼
[1] install toolchain        node + @anthropic-ai/claude-code, gh, task,
                             bd (brew install steveyegge/beads/bd)
[2] bd bootstrap             bd dolt remote add <name> <DOLT_REMOTE_URL secret>
                             bd bootstrap --yes   (CI=true → non-interactive;
                                                   clones from the Dolt remote)
[3] official action env      profile env bag passed via settings.env (§3.3)
[4] run official action      anthropics/claude-code-action@v1:
                               plugin_marketplaces: fzymgc-house/fzymgc-house-skills
                               plugins: dev-flow@fzymgc-house-skills
                               prompt: "/review-pr <PR> <aspects>" + headless post override
                             (inside: review-pr creates epic + finding beads,
                              aggregates, computes verdict, posts summary comment)
        │
        ▼
[5] bd dolt push             persist epic + findings to shared Dolt
[6] read verdict             from bd severity counts → exit 0 / fail per fail_on
```

### 4.1 Plugin loading

The official action's `plugin_marketplaces` + `plugins` inputs load the
fzymgc-house marketplace and the `dev-flow` plugin, which bundles **both** the
`/review-pr` orchestrator skill and its aspect agents (`code-reviewer`,
`security-auditor`, `silent-failure-hunter`, `pr-test-analyzer`,
`type-design-analyzer`, `comment-analyzer`, `api-contract-checker`,
`spec-compliance`, `code-simplifier`, `slop-hunter`). No agent definitions are
hand-copied into the action.

## 5. Security & failure modes

### 5.1 Trigger safety (fork PRs)

- `pull_request: [opened, synchronize]` runs **only** for same-repo branches:
  guard `if github.event.pull_request.head.repo.full_name == github.repository`.
- Fork PRs are reviewed only when a maintainer applies the `claude-review`
  label, on a privileged path that **does not check out or execute fork code**
  with secrets in scope — the review reads the diff; it never runs the PR's
  build/test.
- No `pull_request_target` in v1.

### 5.2 Secret hygiene

All provider tokens and Dolt credentials are GitHub secrets, referenced by name
in `profiles.yml`, never echoed. Credential export avoids `set -x`. The summary
comment is the only outward artifact.

### 5.3 Failure-mode matrix

| Failure | Behavior |
|---|---|
| `bd dolt pull` fails (Dolt unreachable) | Fail fast, clear message; no review attempted (don't run with a broken findings store) |
| Model endpoint 429 / quota | v1: fail the job with a labeled message (`profile=<name> rate-limited`); v2: trigger fallback profile |
| Provider auth fails | Fail fast; hint "check `ANTHROPIC_AUTH_TOKEN` (not `ANTHROPIC_API_KEY`) for profile X" |
| Claude run exits non-zero | Surface the CLI exit; do **not** `bd dolt push` partial state |
| `bd dolt push` conflict | pull-then-retry once; if still conflicting, leave local + emit a warning artifact (never lose findings) |
| review-pr finds PR merged/closed | skill self-reconciles (its Step 3/12); action exits 0 |
| Verdict = CHANGES_REQUESTED | exit 0 unless `fail_on: changes_requested`, then exit 1 to gate merge |

### 5.4 Idempotency

Re-running on the same PR is safe: review-pr keys off the existing
`pr-review,pr:<n>` epic and increments a `turn:N` label; the summary comment
carries the `<!-- pr-review:<bead-id> -->` marker so it updates in place rather
than duplicating.

### 5.5 bd-in-CI bootstrap

The bootstrap is a solved sequence, grounded against the `bd` CLI (§9):

```bash
brew install steveyegge/beads/bd          # public homebrew tap
bd dolt remote add origin "$DOLT_REMOTE_URL"   # creds from secret
bd bootstrap --yes                         # CI=true ⇒ non-interactive;
                                           # clones DB from the configured Dolt remote
# … review runs (review-pr writes epic + findings) …
bd dolt push                               # persist
```

Why the previously-flagged risks are handled:

1. **`.beads/redirect` absolute-path (reviewer finding 3)** — **non-issue**.
   The redirect file is **gitignored** (per `.beads/.gitignore`), so it is
   absent on a fresh CI clone; `bd` resolves to the repo's own `.beads/`. There
   is no stale macOS path to override. `bd where` confirms the active location
   for debugging.
2. **`bd` install** — `brew install steveyegge/beads/bd` (the repo's
   `Taskfile.yaml` uses the same tap). `bd bootstrap` is purpose-built for
   "setting up beads on a fresh clone" and auto-detects non-interactive mode when
   `CI=true`.
3. **Dolt auth from CI** — a `DOLT_REMOTE_URL` secret (with embedded
   credentials, or paired with a token secret) wired via `bd dolt remote add`.
   The plan confirms the exact credential form `bd dolt` expects non-interactively.
4. **Concurrent-writer / merge risk** — a human session and the CI run could
   both push; the `pr-review` epic is run-local so conflict risk is low. Pull
   immediately before push; tolerant retry (§5.3).
5. **Headless confirmation override** — review-pr Step 10 says "do NOT post
   without confirmation." v1 overrides via the prompt ("you are headless in CI;
   post the summary without asking"); the durable fix is upstream Issue 1 (§8).
6. **`gh` auth inside the skill** — review-pr shells out to `gh pr comment` /
   `gh api`. Confirm `GH_TOKEN` is exported into the CLI's shell, not only the
   action's own steps.

## 6. Testing strategy

GitHub Actions is awkward to unit-test, so the strategy is layered:

- **`act` / local dry-run** of the composite action with a stub profile against a
  throwaway PR, asserting step ordering and env export (no real model spend).
- **`profiles.yml` schema test** — a validator (consistent with the repo's
  `task lint` schema checks) asserting every profile has
  `base_url`/`auth_secret`/`runs_on`/`env`, and that referenced secrets are
  wired in the workflow.
- **One live smoke PR** against the cheap `deepseek` profile exercising the full
  pull → review → comment → push loop before wiring the Anthropic profile.
- **bd push/pull integration** verified against a **scratch Dolt branch** first
  (not main) to de-risk the concurrent-writer path.

## 7. Alternatives considered

- **Raw Claude Code CLI in the composite action** — provider-agnostic by
  construction and full control over flags/env/exit handling, but reimplements
  GitHub plumbing the official action provides (token wiring, the inline-comment
  MCP tool for v2). **Kept as a documented fallback** if the official action's
  provider validation rejects the LiteLLM/DeepSeek base-URL override (§3.3).
- **Workflow-only, no composite action** — fewest files, but not extractable;
  rejected against the "extractable later" goal.
- **Ephemeral throwaway `bd` DB per run** — avoids Dolt-in-CI auth, but loses
  findings and breaks cross-run `/address-findings`; rejected in favor of
  `bd dolt pull/push` against the shared store.
- **Bundling `claude-code-router` as a per-job proxy** — unnecessary once a
  central LiteLLM router is available; DeepSeek needs no proxy at all (native
  Anthropic-shaped endpoint). Rejected as redundant complexity.

## 8. Upstream requirements (fzymgc-house/fzymgc-house-skills)

`dev-flow:review-pr` has gaps that make headless CI use require workarounds.
These are filed upstream as **requirements** (not solutions), as three issues on
`fzymgc-house/fzymgc-house-skills`:

- **[#128](https://github.com/fzymgc-house/fzymgc-house-skills/issues/128)** — Issue 1 below
- **[#129](https://github.com/fzymgc-house/fzymgc-house-skills/issues/129)** — Issue 2 below
- **[#130](https://github.com/fzymgc-house/fzymgc-house-skills/issues/130)** — Issue 3 below

- **Issue 1 (enhancement) — headless/CI mode + machine-readable verdict**
  (combines requirements A + B; they ship together):
  - Non-interactive mode that auto-posts the summary without the Step 10
    confirmation prompt and never blocks on stdin (opt-in; default unchanged).
  - Machine-readable verdict: non-zero exit on CHANGES REQUESTED and/or a JSON
    summary (`{verdict, critical, important, suggestions, epic}`) matching the
    prose verdict exactly.
- **Issue 2 (enhancement) — optional inline per-line review comments**
  (requirement C; v2 enabler): opt-in inline review comments anchored to
  file+line, in addition to the summary, degrading gracefully when a finding has
  no precise anchor.
- **Issue 3 (documentation) — model-tier env-var contract** (requirement D):
  document that the skill relies on `ANTHROPIC_DEFAULT_{OPUS,SONNET,HAIKU}_MODEL`
  and `CLAUDE_CODE_SUBAGENT_MODEL`; runs unmodified against non-Anthropic
  providers when they are mapped; list the logical tiers it uses.

## 9. Grounding traces

Recorded on bead holomush-3f5s9 (`bd show holomush-3f5s9`):

- `.github` survey — no existing Claude action; local composite actions are
  `install-cog`/`install-formatters`/`install-task` only.
- deepwiki `anthropics/claude-code-action` — Agent Mode via `prompt` runs on
  `pull_request` without `@claude`; model via `claude_args --model`; skills via
  `plugin_marketplaces` + `plugins`; tools via `--allowedTools`; providers limited
  to Anthropic/Bedrock/Vertex/Foundry (no native OpenRouter/Ollama).
- exa — DeepSeek ships a native Anthropic-compatible endpoint
  (`https://api.deepseek.com/anthropic`); `ANTHROPIC_AUTH_TOKEN` + tier env vars;
  `claude-opus*` → `deepseek-v4-pro`, `claude-sonnet*`/`claude-haiku*` →
  `deepseek-v4-flash`.
- deepwiki `musistudio/claude-code-router` — translation-proxy approach for
  OpenAI-shaped backends; unnecessary here given central LiteLLM.
- read — `dev-flow:review-pr` SKILL.md: bead-native, summary-only post,
  confirmation-gated, aspect agents bundled in the `dev-flow` plugin.
- deepwiki `anthropics/claude-code-action` (round 2, §3.3) —
  `validateEnvironmentVariables()` on the default Anthropic provider only
  requires `ANTHROPIC_API_KEY`|`CLAUDE_CODE_OAUTH_TOKEN`; does **not** reject
  `ANTHROPIC_BASE_URL`/`AUTH_TOKEN`/`DEFAULT_*`. Action forwards
  `ANTHROPIC_BASE_URL` + default-model env to the CLI; `settings.env` JSON is the
  recommended channel for the full env bag (composite-action env shadowing means
  a step-level `env:` block is hidden from the CLI).
- `bd` CLI (§5.5) — install `brew install steveyegge/beads/bd`; `bd bootstrap`
  is the fresh-clone primitive (auto non-interactive under `CI=true`; clones from
  the configured Dolt remote / `refs/dolt/data`). `.beads/redirect` is gitignored,
  so absent on a fresh CI clone — the absolute-path concern does not arise.
- `.github/actionlint.yaml` — registers only
  `namespace-profile-linux-amd64-{2x4,4x8,8x16}` self-hosted labels; the homelab
  runner label must be added there before the `litellm` profile passes lint (§3.4).

**Verify at plan time (not yet grounded):** the exact `plugins:`
input syntax for the official action (the `dev-flow@fzymgc-house-skills`
marketplace-qualifier form, §4.1), and the live model identifiers for each
provider (§3.2 note).
<!-- adr-capture: sha256=33f1a7f09e559925; session=cli; ts=2026-05-30T21:57:05Z; adrs= -->
