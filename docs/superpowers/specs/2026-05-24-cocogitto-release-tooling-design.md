<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Design: Replace release-please with Cocogitto

**Status:** Draft
**Date:** 2026-05-24
**Design bead:** `holomush-dfsvk`
**Author:** Sean Brandt (with Claude)

## Problem

The first real release campaign (activating `game.holomush.dev`, epic
`holomush-hryj`) exercised the release pipeline end-to-end for the first time and
surfaced a cascade of [release-please](https://github.com/googleapis/release-please)
clunkiness, each fix revealing the next gap:

1. `GITHUB_TOKEN`-created release PRs do not trigger required CI → every release
   needs a manual close+reopen (`holomush-hryj.10`).
2. release-please's generated `CHANGELOG.md` violates the repo's rumdl rules →
   required `CHANGELOG` excludes across three Taskfile rumdl flags
   (`holomush-hryj.11`, #4202/#4204).
3. manifest mode + `package-name` produced component-prefixed tags
   (`holomush-v0.1.0`) that did not match `release.yaml`'s `on.push.tags: 'v*'` →
   required `include-component-in-tag: false` (`holomush-hryj.13`, #4205).
4. two-config-file manifest mode (`release-please-config.json` +
   `.release-please-manifest.json`) plus `release-as` one-shot override semantics
   are fiddly.
5. conventional-commit **parse failures** on real commit subjects containing
   arrows/parens (#4164 `SESSION_NOT_FOUND/EXPIRED → STREAM_ACCESS_DENIED`, #4094
   tailwind bump) produced `unexpected token` errors; those commits were silently
   mis-categorized or dropped from the changelog.

A common root cause underlies #1 and #2: release-please maintains an **in-repo
`CHANGELOG.md`**, which forces a **commit to the protected `main` branch**. That
protected-branch write is what requires the release-PR mechanism, which is what
fails to trigger CI under `GITHUB_TOKEN`.

A second, independent gap: the repo is developed primarily via **`jj`**, which
does not fire **lefthook** git hooks reliably, and all PRs are **squash-merged**
(the squash commit message is composed in the GitHub UI at merge time, never seen
by a local hook). The only conventional-commit validation today is a lefthook
`cog verify` hook — so in practice cc-validation is **de facto absent**, which is
how the #5 parse-failure-inducing subjects reached `main` in the first place.

## Goals

- Derive the next semver from conventional commits, create a `v*` git tag,
  generate GitHub Release notes, and trigger build/sign/publish — **without**
  release-please's release-PR mechanism.
- Eliminate the protected-`main` write, the rumdl `CHANGELOG` excludes, and the
  dual-changelog duplication.
- Move conventional-commit validation to a CI gate that actually fires (it does
  not today).
- Keep [GoReleaser](https://goreleaser.com) unchanged as the artifact engine
  (build / Cosign signing / SBOM / multi-arch / GHCR publish).
- Decouple **cutting a release** from **deploying** it (manual → manual).

## Non-goals

- Changing GoReleaser's build / Cosign signing / SBOM / publish config, the SLSA
  attestation jobs, or the deploy job's **internals**. The *one* GoReleaser change
  in scope is adding `release.target_commitish` (Component 3b), which is required
  for the inline single-run design — it does not touch build/sign/publish behavior.
  The deploy bug `holomush-aocap` (deploy doesn't recreate gateway/core; health
  probe masks stale version) is a separate concern.
- Keeping an in-repo `CHANGELOG.md`. GitHub Release notes become the canonical,
  per-version history.
- The broader **lefthook → `jj fix`** migration for the repo's formatting and
  license hooks. That is filed as a separate bead (see "Out of scope: lefthook
  migration" below); this spec only relocates the one hook the release pipeline
  depends on.

## Background: what already exists

- **Cocogitto (`cog`) is already installed and wired** — `brew install … cocogitto`
  in `Taskfile.yaml`'s `install:tools`, a `cog.toml` at repo root, and a lefthook
  `conventional-commit` hook (`cog verify --file {1}`). Today `cog.toml` restricts
  it to commit-message validation only, with the comment "Release-please handles
  versioning, changelog, and releases."
- **GoReleaser already generates the GitHub Release notes** —
  `.goreleaser.yaml`'s `changelog:` block (sort + `docs:`/`test:`/`chore:`/merge
  filters). Release notes — the must-have — are therefore already covered
  independent of release-please.
- **The `goreleaser` job already dual-triggers** — `release.yaml:44`:
  `needs.release-please.outputs.release_created || startsWith(github.ref, 'refs/tags/')`.
  A plain `v*` tag push already drives the entire build/sign/publish/deploy
  pipeline with no involvement from release-please. **The only contract GoReleaser
  depends on is the `v*` tag.**
- **Squash-merge title is the PR title** — verified via
  `gh api repos/holomush/holomush --jq '{squash_merge_commit_title, squash_merge_commit_message}'`
  → `squash_merge_commit_title: PR_TITLE`, `squash_merge_commit_message: PR_BODY`.
  The commit subject that lands on `main` *is* the PR title, so linting the PR
  title for conventional-commit compliance guarantees every commit `cog` parses is
  well-formed. **This is a GitHub repo setting (not a file in the repo), so it is
  not enforced by version control** — the rollout (step 2) asserts it via `gh api`
  and sets it if drifted, since INV-5 depends on it.
- **Latest released tag is `v0.1.14`.** Git tags are the version state; the
  `.release-please-manifest.json` mirror (`0.1.13` at time of writing, already
  stale vs. the `v0.1.14` tag) is redundant.

## Chosen approach: Cocogitto, tag-only, manual dispatch

`cog bump --auto --disable-bump-commit` derives the next semver from commit
history and creates **just the tag** — no version-bump commit, therefore **no
write to protected `main`**. This single capability dissolves the root cause of
release-please pains #1 and #2.

Alternatives considered and rejected:

- **git-cliff + a tagging script** — git-cliff is changelog-*only*; it does not
  derive versions or tag, so it requires a hand-rolled semver/tag script. More
  moving parts, and it is the abandoned path that left the orphaned
  `RELEASE_APP_ID`/`RELEASE_APP_PRIVATE_KEY` secrets. Its only edge is changelog
  templating, which is moot once the in-repo changelog is dropped.
- **GoReleaser-only** — GoReleaser builds whatever tag it is handed; it cannot
  *derive* the next version from commits. Non-viable as the version engine.
- **semantic-release / knope / release-it** — Node-shaped or
  auto-release-on-merge-shaped; none is already present, and auto-release-on-merge
  is explicitly undesired given the deploy coupling (see below).

## Target pipeline

Three independent stages, two human gates:

```text
[CI on every PR]                  [Gate 1: human dispatches Release]
cog verify "<PR title>"     →     release.yaml (workflow_dispatch):
(commit-lint job, PR-only)        1. cog bump --auto --dry-run          (preview → Actions log)
                                  2. (optional) assert computed bump == expected_increment input
                                  3. cog bump --auto --disable-bump-commit   (creates LOCAL v* tag; no main commit, no push)
                                  4. goreleaser release --clean   (App-token auth; builds from the LOCAL tag,
                                     and via release.target_commitish creates the remote v* tag + GH Release
                                     + artifacts atomically; generates notes natively)
                                     — ALL INLINE in one workflow run
                                     on failure → delete the local tag (no remote tag was created), re-dispatch (INV-8)

                                  [Gate 2: human dispatches Deploy]
                            →     deploy-sandbox job (workflow_dispatch ONLY):
                                  pull tag → migrate → restart → health probe
```

### Why a single inline dispatched run (the `GITHUB_TOKEN` trigger trap)

A tag pushed by a workflow authenticated with the default `GITHUB_TOKEN` will
**not** trigger another workflow (`on: push: tags`) — GitHub suppresses
workflow-triggering-workflow to prevent recursion. This is the **same root cause**
as release-please pain #1. Rather than depend on a cross-workflow tag trigger, the
dispatched release workflow performs version-derivation → tag → GoReleaser
**inline, in one run**. The existing `on: push: tags: v*` trigger is retained only
as a fallback for human-pushed local tags (a human pushing a tag locally is a real
push event and does trigger the workflow normally).

### Why an App token

GoReleaser's GitHub operations (creating the Release and the remote tag) run under
a **GitHub App token**, reusing the currently orphaned `RELEASE_APP_ID` /
`RELEASE_APP_PRIVATE_KEY` secrets — turning an abandoned investment into the
supported path. Rationale: (a) proper bot attribution rather than the ephemeral
`github-actions[bot]` identity; (b) it bypasses any tag-protection rule that would
deny the default `GITHUB_TOKEN`; and (c) on the rare human-pushed-tag fallback
path, a tag created by an App token (unlike one created by `GITHUB_TOKEN`) *can*
trigger the `on: push: tags` workflow if we ever choose to depend on it. Inline
execution means the primary dispatch path does **not** rely on (c).

## Components and changes

### 1. `cog.toml` — flip from validation-only to release-enabled

- Remove the "Cocogitto is used ONLY for commit message validation /
  Release-please handles …" comments.
- **Set `tag_prefix = "v"` (correctness-critical).** `cog.toml` has no
  `tag_prefix` key today; without it `cog bump` creates an *unprefixed* tag
  (`0.2.0`), which breaks both the `on: push: tags: 'v*'` fallback trigger and
  GoReleaser's `v`-prefix expectation. This key MUST be set.
- **Disable cog's changelog entirely** (`disable_changelog = true` or equivalent).
  cog is the version+tag engine **only** — it does **not** generate release notes.
  GoReleaser owns release notes (see Components/2 and the note below). This means
  no in-repo `CHANGELOG.md` *and* no `cog changelog` piping.
- Keep `ignore_merge_commits = true` and the existing `[commit_types]`.
- No `pre_bump_hooks` are required for tag-only mode.

> **Why GoReleaser owns release notes, not `cog`:** GoReleaser already generates
> the GitHub Release notes today via `.goreleaser.yaml`'s `changelog:` block
> (`exclude: [^docs:, ^test:, ^chore:, Merge pull request, Merge branch]`). Passing
> `--release-notes=FILE` (e.g. `<(cog changelog)`) would make GoReleaser **skip its
> own generation and the exclude filters entirely** (confirmed against the
> GoReleaser docs), silently bloating notes with `docs:`/`test:`/`chore:` entries.
> Keeping GoReleaser's native notes generation avoids duplicating those filters in
> a cog template. If grouped/sectioned notes are wanted later, GoReleaser v2's
> `changelog.groups` does it natively — still no `cog changelog` needed.

### 2. `.github/workflows/release.yaml`

- **Remove** the `release-please` job entirely. **Correctness-critical:** the
  `goreleaser` job currently declares `needs: release-please` (`release.yaml:43`)
  and references `needs.release-please.outputs.*` in its `if:` and `Get version`
  step. Every one of those references MUST be removed in the same change, or the
  workflow errors at parse/runtime once the job is gone. This is an explicit
  rewrite step, not a parenthetical.
- **`goreleaser` job:** add the `workflow_dispatch` release entry that runs, in
  order: App-token mint → `cog bump --auto --dry-run` (preview) → optional
  `expected_increment` assertion → `cog bump --auto --disable-bump-commit` →
  push tag (App token) → `goreleaser release --clean` (GoReleaser generates notes
  natively). Retain `on: push: tags: 'v*'` as the human-pushed-tag fallback. The
  `Get version` step's `release-please` branch is removed; version comes from the
  tag (`GITHUB_REF_NAME` on the tag-push path, or the `cog`-created tag on the
  dispatch path).
- **`deploy-sandbox` job → move to a dedicated `.github/workflows/deploy.yaml`.**
  Once the `goreleaser` job also triggers on `workflow_dispatch`, leaving
  `deploy-sandbox` in `release.yaml` would make a single dispatch fire *both*
  release and deploy — defeating the two-gate goal. So `deploy-sandbox` moves to
  its own `deploy.yaml` triggered by `workflow_dispatch` **only** (keeping the
  existing `tag` input + `prod` environment + `deploy-sandbox` concurrency group),
  and the job + its tag-push `if:` branch + its `needs: [verify-release]` are
  **removed** from `release.yaml` (`release.yaml:320-447`). Result: a tag push (or
  release dispatch) cuts the release and does **not** deploy; deploy is a wholly
  separate dispatch. This is the literal "two gates" implementation of INV-4.
- Add a `workflow_dispatch` input `expected_increment`
  (`auto`|`major`|`minor`|`patch`, default `auto`); when not `auto`, the workflow
  fails fast if `cog`'s computed increment differs.

### 3. `commit-lint` — a dedicated workflow, NOT a job in `ci.yaml`

- Validates the PR title against conventional-commit format by running
  `cog verify "${{ github.event.pull_request.title }}"`. Because the squash title
  is the PR title (see Background), a valid PR title guarantees a valid commit
  subject on `main` — which is what `cog bump` parses for version derivation.
- **Why a separate workflow, not a job in `ci.yaml`:** `ci.yaml`'s `pull_request`
  trigger carries `paths-ignore: [site/**, docs/**, "**/*.md", …]`
  (`ci.yaml:16-26`). A `commit-lint` job added there would be **silently skipped on
  docs-only PRs** — yet a docs-only PR still squash-merges its PR title as a commit
  subject on `main`, where a malformed title would break version derivation. The
  check MUST run on **every** PR. Since `paths-ignore` is per-workflow-trigger and
  cannot be overridden per-job, `commit-lint` lives in its own
  `.github/workflows/commit-lint.yaml` with a `pull_request` trigger that has **no
  `paths-ignore`**.
- Reuses the already-installed `cog` binary (via `install:tools`); no new action
  dependency.

### 3b. `.goreleaser.yaml` — set `release.target_commitish` (correctness-critical)

- Add `target_commitish: "{{ .Commit }}"` to the `release:` block
  (`.goreleaser.yaml:169-175`, which currently has only `github`/`draft`/
  `prerelease`). **This is what makes the inline single-run design work.** Per the
  GoReleaser docs, GoReleaser does **not** push local tags; the canonical flow is
  `git push origin <tag>` *before* `goreleaser release`. `target_commitish` is the
  documented mechanism for "creating tags locally before running GoReleaser" — it
  delays remote tag creation so GoReleaser creates the remote `v*` tag (from the
  given commit) *as part of* publishing the Release. Without it, `goreleaser
  release` against a local-only tag fails at the GitHub API call, and INV-8's
  no-orphan-tag guarantee does not hold.
- This is the **only** `.goreleaser.yaml` change; build/sign/SBOM/docker/manifest
  config is untouched (non-goal).

### 4. Delete release-please configuration

- Delete `release-please-config.json`.
- Delete `.release-please-manifest.json`. Git tags are the version source of
  truth; `cog` derives the next version from the latest `v*` tag (`v0.1.14`).

### 5. Taskfile / rumdl cleanup

- Remove the three `CHANGELOG.md` rumdl `--exclude` entries (`lint:markdown`,
  `fmt:markdown`, `fmt:check`) now that no `CHANGELOG.md` exists.
- Add a `task release` convenience target documenting/launching the dispatch (or
  a thin wrapper around `gh workflow run release.yaml`), and keep
  `release:snapshot` / `release:check` as-is.

### 6. lefthook `conventional-commit` hook

- Keep the existing lefthook `cog verify --file {1}` hook as **best-effort local
  feedback** for git-based contributors, but it is no longer authoritative — the
  CI `commit-lint` job is the gate. Documented as such inline.

## Version continuity

`cog` derives the next version from the latest existing `v*` tag (`v0.1.14`). No
manifest file is needed — the git tag history *is* the state. The first `cog`
release after this change bumps from `v0.1.14` according to the conventional
commits merged since. Deleting `.release-please-manifest.json` (mirror value,
already stale) has no effect on version derivation.

## Invariants (RFC2119)

These are numbered for traceable test coverage (per project spec discipline).

- **INV-1 (single-run release):** The release workflow **MUST** perform
  version-derivation, tagging, and GoReleaser invocation within a single dispatched
  run. It **MUST NOT** rely on a `GITHUB_TOKEN`-pushed tag re-triggering a
  downstream workflow.
- **INV-2 (tag is the contract):** A `v`-prefixed semver tag **MUST** be the only
  artifact GoReleaser depends on to build a release. No release-tool-specific
  config file may be required at GoReleaser invocation time.
- **INV-3 (tag-only, no protected-main write):** Cutting a release **MUST NOT**
  create a commit on `main`. Version state lives in git tags only.
- **INV-4 (deploy decoupled):** The deploy job **MUST NOT** be triggered by a tag
  push. Deploy **MUST** be `workflow_dispatch`-only.
- **INV-5 (cc-validated main):** Every commit subject on `main` **MUST** be a valid
  conventional commit, enforced by the CI `commit-lint` job on the PR title (which
  becomes the squash commit subject).
- **INV-6 (robust parsing):** `cog bump --auto` / `cog verify` **MUST** parse
  historical commit subjects containing arrows and parentheses (regression fixtures
  from #4164 and #4094) without error — the specific defect that broke
  release-please version derivation.
- **INV-7 (notes preserved):** Every release **MUST** publish GitHub Release notes,
  generated by GoReleaser's existing `changelog:` config (with its
  `docs:`/`test:`/`chore:`/merge exclude filters). Release notes are the canonical
  version history. The release step **MUST NOT** pass `--release-notes`, which would
  bypass those filters.
- **INV-8 (atomic-or-recoverable cut):** A failed release **MUST NOT** leave an
  orphaned remote `v*` tag with no corresponding GitHub Release. Either the tag is
  pushed only after GoReleaser succeeds, or the failure path deletes the tag (see
  "Failure modes").

## Testing

- **INV-6 regression:** feed the #4164 (`… SESSION_NOT_FOUND/EXPIRED →
  STREAM_ACCESS_DENIED …`) and #4094 commit subjects to `cog verify` and
  `cog bump --auto --dry-run`; assert no parse failure and that the bump is
  computed. This is the highest-value test — it locks the specific release-please
  defect closed for version derivation.
- **Version derivation:** `cog bump --auto --dry-run` against a fixture history
  (or the live repo) asserts the expected next version from a known set of `feat`/
  `fix` commits since `v0.1.14`.
- **PR-title lint:** `cog verify` accepts representative valid PR titles and
  rejects malformed ones (missing type, bad type, no colon).
- **Workflow smoke:** `task release:check` (GoReleaser config validation) and
  `task release:snapshot` (local build, no publish) stay green; a `--dry-run`
  dispatch on a branch exercises the workflow wiring without publishing.

## Failure modes

- **GoReleaser fails after the local tag is created (INV-8):** With
  `release.target_commitish` set (Component 3b), the remote tag is created by
  GoReleaser *as part of* publishing the Release — so a GoReleaser failure leaves
  at most a *local* tag in the ephemeral runner, with no remote tag and no
  half-published Release. Recovery: delete the local tag; nothing on `origin`
  changed. (Alternative ordering — explicit `git push origin <tag>` *before*
  GoReleaser — does **not** require `target_commitish` but inverts this: a failure
  leaves an orphaned remote tag, so the failure path **MUST**
  `git push --delete origin <tag>` to satisfy INV-8. This spec prescribes the
  `target_commitish` ordering; the alternative is noted only to bound the design
  space.) Tag-only releases carry no other state (no commit, no changelog file), so
  deletion is clean and idempotent.
- **App token invalid/expired:** GoReleaser's GitHub auth fails before any tag or
  Release is created → no partial state. Surfaces as a hard workflow failure;
  rollout step 2 pre-validates the token.
- **`expected_increment` mismatch:** the dispatch fails fast *before*
  `cog bump` mutates anything (the assertion runs against `--dry-run` output), so a
  surprise major/minor never produces a tag.
- **Malformed subject reaches `main` despite `commit-lint`:** if a non-cc subject
  somehow lands (e.g. a direct admin merge bypassing the PR gate), `cog bump`
  treats it as a non-incrementing commit rather than erroring (INV-6 covers
  arrows/parens specifically). The dry-run preview catches an unexpectedly small
  bump before the release is cut.

## Rollout

1. Land `cog.toml`, the new `commit-lint.yaml` workflow, and the `release.yaml`
   rewrite together; delete the two release-please config files and the rumdl
   `CHANGELOG` excludes in the same PR.
2. **Pre-validate external state** (these are GitHub-side, not version-controlled):
   - Assert `gh api repos/holomush/holomush --jq .squash_merge_commit_title` is
     `PR_TITLE`; set it via `gh api -X PATCH` if drifted (INV-5 depends on it).
   - Verify the App-token secrets (`RELEASE_APP_ID`/`RELEASE_APP_PRIVATE_KEY`) are
     valid and the App has `contents: write`; if not, regenerate.
   - Check for any tag-protection rule on `v*` that would block the App token.
3. First cocogitto release is a deliberate `workflow_dispatch` from `main` with
   `expected_increment` set, to confirm version, tag, notes, and artifacts before
   trusting `auto`.
4. The deploy gate is exercised separately, confirming the tag push no longer
   auto-deploys.

## Out of scope: lefthook → `jj fix` migration (separate bead)

`jj fix` is a file-content rewriting mechanism (`[fix.tools.*]`: command +
patterns); it cannot gate/reject and has no commit-message concept. It can replace
the repo's content-rewriting hooks (`license:add`, formatting) but **not** the
gate-style hooks (`license:check`) nor commit-message validation — those must move
to CI regardless. Because the release pipeline's only lefthook dependency
(cc-validation) is relocated to CI by this spec, the broader migration is
orthogonal and is tracked as its own bead. This work **establishes the pattern**
("validation moves to CI because local hooks are unreliable under `jj`") that the
migration generalizes.

## Open questions

None blocking. The App-token validity (rollout step 2) is a verification, not a
design decision.

<!-- adr-capture: sha256=6983d9b06569c067; ts=2026-05-24T22:49:09Z; adrs=holomush-jfb9x,holomush-dgsts,holomush-u2exm,holomush-4mmzy -->
