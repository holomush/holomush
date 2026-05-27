<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Retire lefthook + license-eye Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete `lefthook.yaml`; make `task pr-prep` + CI the authoritative quality gate, close the silently-no-op EBNF gate, and replace `addlicense` with `license-eye` (licensing code and functional markdown under one configurable tool).

**Architecture:** No `jj fix` (it is manual-only, no edge over `task fmt`). Every gate lefthook performed already lives in CI/`pr-prep` except EBNF, which gets a new `generate:ebnf:check` task wired into both. `license-eye` (Apache SkyWalking Eyes) replaces `addlicense` via a root `.licenserc.yaml`; `task fmt` becomes the one-command local fixer.

**Tech Stack:** Taskfile (go-task), GitHub Actions, bats (`scripts/tests/`), `license-eye` (`github.com/apache/skywalking-eyes/cmd/license-eye`), Go `go generate`.

**Design spec:** [`docs/superpowers/specs/2026-05-25-retire-lefthook-license-eye-design.md`](../specs/2026-05-25-retire-lefthook-license-eye-design.md)

---

## File Structure

| Path | Action | Responsibility |
| --- | --- | --- |
| `Taskfile.yaml` | Modify | `generate:ebnf:check` (new), `license:*` rewire, `fmt` += `license:add`, `setup` install swaps, `pr-prep:run` EBNF step |
| `.github/workflows/ci.yaml` | Modify | EBNF verify step in the Lint job |
| `.licenserc.yaml` | Create | license-eye config (license content, paths, paths-ignore, markdown language) |
| `lefthook.yaml` | Delete | Retired |
| `cog.toml` | Modify | Drop the lefthook commit-msg-hook comment |
| `docs/CLAUDE.md` | Modify | Replace "Pre-commit Validation" with "Local quality checks" |
| `CLAUDE.md` | Modify | License Headers row; remove lefthook mention |
| `docs/specs/decisions/epic7/general/102-lefthook-markdown-autofix-intentional.md` | Modify | Mark Superseded |
| `scripts/tests/generate-ebnf-check.bats` | Create | INV-2 (EBNF drift caught) |
| `scripts/tests/license-eye.bats` | Create | INV-4 (fmt→check roundtrip), INV-6 (content excluded) |
| `scripts/tests/no-lefthook-refs.bats` | Create | INV-3 (no live lefthook references) |
| functional markdown (`docs/**`, `.claude/rules/**`, root `*.md`) | Modify | One-time SPDX stamping |

---

## Phase 1: Close the EBNF gate

### Task 1: Add `generate:ebnf:check` task + drift test

**Files:**

- Modify: `Taskfile.yaml` (after `generate:ebnf:` block, `Taskfile.yaml:358-367`)
- Create: `scripts/tests/generate-ebnf-check.bats`

- [ ] **Step 1: Write the failing bats test**

Create `scripts/tests/generate-ebnf-check.bats`:

```bash
#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

# INV-2: the EBNF gate MUST catch drift between the DSL parser and the
# generated grammar/railroad artifacts. Regenerates for real (needs network
# for the railroad tool), so this is a slow case by design.

setup_file() {
  if [ ! -f "scripts/tests/Taskfile.test.yaml" ]; then
    echo "ERROR: bats must be invoked from the repo root (try 'task test:bats')." >&2
    exit 1
  fi
}

setup() {
  bats_load_library bats-support
  bats_load_library bats-assert
  EBNF=site/docs/reference/policy-dsl.ebnf
  cp "$EBNF" "${BATS_TEST_TMPDIR}/orig.ebnf"
}

teardown() {
  # generate:ebnf:check regenerates the artifact, so the tree is left clean;
  # restore is belt-and-suspenders in case the generator was skipped.
  cp "${BATS_TEST_TMPDIR}/orig.ebnf" "$EBNF"
}

@test "generate:ebnf:check passes when artifacts are current" {
  run task generate:ebnf:check
  assert_success
}

@test "generate:ebnf:check fails when the EBNF artifact has drifted" {
  printf '\nDRIFT-MARKER\n' >> "$EBNF"
  run task generate:ebnf:check
  assert_failure
  assert_output --partial "out of sync"
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `task test:bats 2>&1 | rg -A2 generate-ebnf-check` (or `bats scripts/tests/generate-ebnf-check.bats` from repo root)
Expected: FAIL — `task generate:ebnf:check` does not exist yet ("task: Task 'generate:ebnf:check' does not exist").

- [ ] **Step 3: Add the `generate:ebnf:check` task**

In `Taskfile.yaml`, immediately after the `generate:ebnf:` block (ends at `Taskfile.yaml:367`), add:

```yaml
  generate:ebnf:check:
    desc: Verify generated EBNF grammar + railroad diagram are current
    cmds:
      - |
        set -euo pipefail
        EBNF=site/docs/reference/policy-dsl.ebnf
        RAIL=site/docs/reference/policy-dsl-railroad.html
        BEFORE=$(sha256sum "$EBNF" "$RAIL" | sha256sum)
        task generate:ebnf
        AFTER=$(sha256sum "$EBNF" "$RAIL" | sha256sum)
        if [ "$BEFORE" != "$AFTER" ]; then
          echo "EBNF/railroad out of sync. Run 'task generate:ebnf' and commit." >&2
          exit 1
        fi
```

(Mirrors the schema-verify pattern at `Taskfile.yaml:789-798`, but sourced as a reusable task because it is called from two places — `pr-prep:run` and CI — in Task 2.)

- [ ] **Step 4: Run the test to verify it passes**

Run: `bats scripts/tests/generate-ebnf-check.bats` (from repo root)
Expected: PASS (both cases). Note: the first run fetches `railroad@latest` via `go run`; ensure network is available.

- [ ] **Step 5: Lint the Taskfile change**

Run: `task lint:yaml`
Expected: PASS.

- [ ] **Step 6: Commit**

```text
jj describe -m "build(ebnf): add generate:ebnf:check task + drift test (INV-2) (holomush-gcio6)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
jj new
```

### Task 2: Wire `generate:ebnf:check` into pr-prep and CI

**Files:**

- Modify: `Taskfile.yaml:789-798` region (`pr-prep:run`, after the schema-verify block)
- Modify: `.github/workflows/ci.yaml:90-98` region (Lint job, after "Verify schema is current")

- [ ] **Step 1: Add the EBNF step to `pr-prep:run`**

In `Taskfile.yaml`, in `pr-prep:run`, immediately after the schema-verify `cmd:` block (ends `Taskfile.yaml:798`) and before `- echo "▸ Verifying jxo8.5 bead description..."`, add:

```yaml
      - echo "▸ Verifying EBNF + railroad are current..."
      - task: generate:ebnf:check
```

- [ ] **Step 2: Add the EBNF step to the CI Lint job**

In `.github/workflows/ci.yaml`, in the Lint job, immediately after the "Verify schema is current" step (ends `ci.yaml:98`) and before "Check license headers" (`ci.yaml:100`), add:

```yaml
      - name: Verify EBNF is current
        run: task generate:ebnf:check
```

- [ ] **Step 3: Verify pr-prep includes the gate**

Run: `rg -n 'generate:ebnf:check' Taskfile.yaml .github/workflows/ci.yaml`
Expected: three hits — the task definition, the `pr-prep:run` call, the CI step.

- [ ] **Step 4: Smoke-test the gate locally**

Run: `task generate:ebnf:check`
Expected: PASS (exit 0; tree clean — `jj st` shows no changes to the EBNF artifacts).

- [ ] **Step 5: Lint workflow + Taskfile**

Run: `task lint:yaml && task lint:actions`
Expected: PASS. (Note: actionlint shellcheck on `run:` blocks is CI-only; the `run:` here is a single `task` call with no shell logic, so it is safe.)

- [ ] **Step 6: Commit**

```text
jj describe -m "ci(ebnf): gate generate:ebnf:check in pr-prep + CI Lint (holomush-gcio6)

Repairs the silently-no-op lefthook ebnf-sync hook (it diffed dead
site/docs/developers/* paths; generator writes site/docs/reference/*).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
jj new
```

---

## Phase 2: License tooling migration (addlicense → license-eye)

### Task 3: Adopt license-eye — config, task rewire, install swap

**Files:**

- Create: `.licenserc.yaml`
- Modify: `Taskfile.yaml` — `license:check` (`:688-692`), `license:add` (`:694-697`), `license:run` (`:699-712`), `setup` addlicense install (`:842`)

- [ ] **Step 1: Create `.licenserc.yaml`**

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# license-eye (Apache SkyWalking Eyes) config — replaces addlicense.
# Licenses code AND functional markdown (specs/plans/ADRs/rules). User-facing
# rendered content (plugin landing/MOTD, player-facing site docs) is excluded.
header:
  license:
    content: |
      SPDX-License-Identifier: Apache-2.0
      Copyright 2026 HoloMUSH Contributors
  paths:
    - "api/**"
    - "cmd/**"
    - "internal/**"
    - "pkg/**"
    - "plugins/**"
    - "scripts/**"
    - "docs/**"
    - ".claude/rules/**"
    - "*.md"
  paths-ignore:
    - "**/*.pb.go"
    - "vendor/**"
    - "internal/web/dist/**"
    - "plugins/**/content/**"
    - "site/docs/**"
    - "AGENTS.md"
  language:
    Markdown:
      extensions: [".md"]
      comment_style_id: AngleBracket
  comment: on-failure   # inert under `task`-based invocation; honored only if the GH Action is ever wired
```

- [ ] **Step 2: Rewire the `license:*` tasks**

In `Taskfile.yaml`, replace the `license:check` / `license:add` / `license:run` blocks (`:688-712`) with:

```yaml
  license:check:
    desc: Check SPDX license headers (code + functional markdown)
    cmds:
      - license-eye header check

  license:add:
    desc: Add missing SPDX license headers (code + functional markdown)
    cmds:
      - license-eye header fix
```

(`license:run` and the `LICENSE_DIRS` indirection are removed — `.licenserc.yaml` `paths` supersedes them. Leave the top-level `LICENSE_DIRS` var only if another task references it; verify with `rg -n 'LICENSE_DIRS' Taskfile.yaml` and delete the var if `license:run` was its sole consumer.)

- [ ] **Step 3: Swap the install in `setup`**

In `Taskfile.yaml:842`, replace:

```yaml
      - go install github.com/google/addlicense@latest
```

with:

```yaml
      - go install github.com/apache/skywalking-eyes/cmd/license-eye@latest
```

- [ ] **Step 4: Install the tool locally**

Run: `go install github.com/apache/skywalking-eyes/cmd/license-eye@latest`
Expected: `license-eye` on `PATH` (`command -v license-eye`).

- [ ] **Step 5: Verify code-header parity (INV-5)**

Run: `task license:add` then `jj diff --stat`
Expected: the diff touches ONLY markdown files (newly stamped) and any genuinely-header-less code files — **no existing `// SPDX-License-Identifier` line in a `.go`/`.sh`/`.proto` file is modified**. Confirm with: `jj diff --git | rg '^[+-].*SPDX-License-Identifier' | rg '\.go|\.sh|\.proto'` → expect no changed code-header lines (additions to previously-headerless files are acceptable; modifications to existing ones are not).

**Contingency:** if `license:add` rewrites existing code headers (license-eye's matcher didn't recognize the existing form), add a `pattern:` regex under `header.license` in `.licenserc.yaml` matching the existing header, e.g.:

```yaml
  license:
    content: |
      SPDX-License-Identifier: Apache-2.0
      Copyright 2026 HoloMUSH Contributors
    pattern: |
      SPDX-License-Identifier: Apache-2\.0
      Copyright \d{4} HoloMUSH Contributors
```

Re-run Step 5 until existing code headers are untouched.

- [ ] **Step 6: Discard the markdown stamping for now**

The `license:add` in Step 5 also stamped markdown — that is Task 4's deliverable as a separate commit. Discard it from this change so this commit is tooling-only:

Run: `jj diff --name-only | rg '\.md$'` to list the stamped markdown, then restore ONLY those changed files: `jj diff --name-only | rg '\.md$' | xargs -r jj restore --` (do NOT use a broad `glob:**/*.md` — it would also touch unrelated in-progress markdown). Re-run `jj diff --stat` and confirm only `.licenserc.yaml` + `Taskfile.yaml` (+ any code files that were genuinely missing headers) remain.

- [ ] **Step 7: Commit (tooling only)**

```text
jj describe -m "build(license): replace addlicense with license-eye (holomush-gcio6)

.licenserc.yaml drives license-eye for code + functional markdown.
Code-file headers unchanged (INV-5). Markdown stamping lands separately.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
jj new
```

### Task 4: Stamp functional markdown + content-exclusion test

**Files:**

- Modify: functional markdown across `docs/**`, `.claude/rules/**`, root `*.md` (mechanical)
- Create: `scripts/tests/license-eye.bats` (INV-6 case)

- [ ] **Step 1: Write the INV-6 failing test**

Create `scripts/tests/license-eye.bats`:

```bash
#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

setup_file() {
  if [ ! -f "scripts/tests/Taskfile.test.yaml" ]; then
    echo "ERROR: bats must be invoked from the repo root (try 'task test:bats')." >&2
    exit 1
  fi
}

setup() {
  bats_load_library bats-support
  bats_load_library bats-assert
}

# INV-6: license-eye MUST NOT stamp user-facing rendered content.
@test "plugin content markdown stays header-free after license-eye" {
  run rg -l 'SPDX-License-Identifier' plugins --glob 'plugins/**/content/**/*.md'
  assert_failure   # rg exits 1 when no file matches → no content md carries a header
}

# INV-6: site player-facing docs stay header-free.
@test "site player docs stay header-free" {
  run rg -l 'SPDX-License-Identifier' site/docs --glob '*.md'
  assert_failure
}
```

- [ ] **Step 2: Run to verify it passes pre-stamp (guards the exclusion before and after)**

Run: `bats scripts/tests/license-eye.bats`
Expected: PASS (content md is header-free today). This case is a standing guard; it must remain PASS after Step 3 stamps functional markdown.

- [ ] **Step 3: Stamp functional markdown**

Run: `task license:add`
Expected: `<!-- SPDX-License-Identifier: Apache-2.0 -->` / `<!-- Copyright 2026 HoloMUSH Contributors -->` added to in-scope markdown lacking it (root `CLAUDE.md`, `README.md`, `docs/roadmap.md`, and unheadered files under `docs/**` and `.claude/rules/**`).

- [ ] **Step 4: Verify exclusion held + scope correct**

Run: `bats scripts/tests/license-eye.bats` (INV-6 still PASS) and `rg -L 'SPDX-License-Identifier' --glob 'docs/**/*.md' | head`
Expected: INV-6 PASS; spot-check that `docs/**` markdown now carries headers and `plugins/**/content/**` does not.

- [ ] **Step 5: Verify the check now passes repo-wide**

Run: `task license:check`
Expected: PASS (exit 0) — code + functional markdown all headered.

- [ ] **Step 6: Lint the newly-stamped markdown**

Run: `task lint:markdown && task fmt:check`
Expected: PASS. (If rumdl flags a blank-line rule around the new header, run `task fmt:markdown` and re-check.)

- [ ] **Step 7: Commit the stamping as its own commit**

```text
jj describe -m "docs(license): stamp SPDX headers on functional markdown (holomush-gcio6)

One-time mechanical license-eye fix over docs/**, .claude/rules/**, root *.md.
User-facing content excluded (INV-6).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
jj new
```

### Task 5: `task fmt` becomes the one-command local fixer

**Files:**

- Modify: `Taskfile.yaml` — `fmt` task list (`:127-131` region)
- Modify: `scripts/tests/license-eye.bats` (add INV-4 case)

- [ ] **Step 1: Add the INV-4 failing test**

Append to `scripts/tests/license-eye.bats`:

```bash
# INV-4: after `task fmt`, a freshly-added unheadered in-scope file passes check.
@test "task fmt adds license headers so license:check passes" {
  local gof="internal/zzz_invtest_$$.go"
  local mdf="docs/zzz_invtest_$$.md"
  printf 'package internal\n\nfunc invtest() {}\n' > "$gof"
  printf '# inv test\n\nbody\n' > "$mdf"

  run task fmt
  assert_success

  run grep -q 'SPDX-License-Identifier' "$gof"
  assert_success
  run grep -q 'SPDX-License-Identifier' "$mdf"
  assert_success

  rm -f "$gof" "$mdf"
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `bats scripts/tests/license-eye.bats -f "task fmt adds license"`
Expected: FAIL — `task fmt` does not yet run `license:add`, so the temp files have no header.

- [ ] **Step 3: Add `license:add` to `task fmt`**

In `Taskfile.yaml`, the `fmt:` task (`:125-131`) currently runs `fmt:go`, `fmt:yaml`, `fmt:markdown`, `fmt:dprint`. Add a `license:add` step **last** (so formatting settles before headers are applied), preserving all existing entries:

```yaml
  fmt:
    desc: Format all files and apply license headers
    cmds:
      - task: fmt:go
      - task: fmt:yaml
      - task: fmt:markdown
      - task: fmt:dprint
      - task: license:add
```

(Confirm the exact current shape first with `rg -n -A6 '^  fmt:' Taskfile.yaml` — do NOT drop `fmt:yaml`; only append `license:add`.)

- [ ] **Step 4: Run to verify it passes**

Run: `bats scripts/tests/license-eye.bats -f "task fmt adds license"`
Expected: PASS.

- [ ] **Step 5: Full bats + lint**

Run: `task test:bats && task lint:yaml`
Expected: PASS.

- [ ] **Step 6: Commit**

```text
jj describe -m "build(fmt): task fmt applies license headers (one-command fixer, INV-4) (holomush-gcio6)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
jj new
```

---

## Phase 3: Retire lefthook + documentation

### Task 6: Delete `lefthook.yaml` + setup cleanup + cog.toml

**Files:**

- Delete: `lefthook.yaml`
- Modify: `Taskfile.yaml` — `setup` brew line (`:839`) + `lefthook install` line; `setup` desc (`:834`)
- Modify: `cog.toml:7-9`

- [ ] **Step 1: Delete the lefthook config**

Run: `jj file untrack lefthook.yaml 2>/dev/null; rm -f lefthook.yaml` (or simply `rm lefthook.yaml` — jj snapshots the deletion).
Expected: `lefthook.yaml` gone; `jj st` shows it removed.

- [ ] **Step 2: Remove lefthook from `setup`**

In `Taskfile.yaml`:

- Line `:839` brew install — remove the `lefthook` token from:
  `- brew install go-task lefthook goreleaser dprint cocogitto rumdl binaryen flock bats-core yq`
  → `- brew install go-task goreleaser dprint cocogitto rumdl binaryen flock bats-core yq`
- Remove the `- lefthook install` line entirely.
- Update `setup` `desc` (`:834`) from "Install all dev tools and git hooks" → "Install all dev tools".

- [ ] **Step 3: Update the cog.toml comment**

In `cog.toml:7-9`, replace:

```toml
# Conventional-commit validation is enforced in CI on PR titles
# (.github/workflows/commit-lint.yaml); the lefthook commit-msg hook is
# best-effort only (jj does not fire it reliably).
```

with:

```toml
# Conventional-commit validation is enforced in CI on PR titles
# (.github/workflows/commit-lint.yaml). There is no local commit-msg hook
# (jj does not fire git hooks reliably; CI is authoritative).
```

- [ ] **Step 4: Verify nothing else references the deleted file**

Run: `rg -n 'lefthook install|lefthook\.yaml' Taskfile.yaml; test -f lefthook.yaml && echo PRESENT || echo GONE`
Expected: no Taskfile hits; `GONE`.

- [ ] **Step 5: Lint**

Run: `task lint:yaml`
Expected: PASS.

- [ ] **Step 6: Commit**

```text
jj describe -m "build: retire lefthook.yaml; CI + pr-prep are authoritative (holomush-gcio6)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
jj new
```

### Task 7: Documentation sweep + ADR supersession + no-lefthook meta-test

**Files:**

- Modify: `docs/CLAUDE.md` (Pre-commit Validation section)
- Modify: `CLAUDE.md` (License Headers row, `:226`)
- Modify: `docs/specs/decisions/epic7/general/102-lefthook-markdown-autofix-intentional.md` (Status)
- Create: `scripts/tests/no-lefthook-refs.bats` (INV-3)
- ADR `holomush-u2exm` supersession note (via `dev-flow:evolve-adr`)

- [ ] **Step 1: Write the INV-3 failing meta-test**

Create `scripts/tests/no-lefthook-refs.bats`:

```bash
#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

setup_file() {
  if [ ! -f "scripts/tests/Taskfile.test.yaml" ]; then
    echo "ERROR: bats must be invoked from the repo root (try 'task test:bats')." >&2
    exit 1
  fi
}

setup() {
  bats_load_library bats-support
  bats_load_library bats-assert
}

# INV-3: no reference to lefthook in live config/docs. Archived plans/specs
# and the gcio6 spec/plan/ADR (which legitimately discuss the retirement) are
# out of scope — the guard is a fixed allowlist of LIVE files.
@test "lefthook.yaml does not exist" {
  run test -f lefthook.yaml
  assert_failure
}

@test "no lefthook references in live config/docs" {
  run rg -i -n lefthook Taskfile.yaml cog.toml docs/CLAUDE.md CLAUDE.md
  assert_failure   # rg exits 1 when there are no matches
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `bats scripts/tests/no-lefthook-refs.bats`
Expected: the second case FAILS — `docs/CLAUDE.md` and `CLAUDE.md` still mention lefthook.

- [ ] **Step 3: Rewrite the `docs/CLAUDE.md` pre-commit section**

In `docs/CLAUDE.md`, replace the "## Pre-commit Validation" section (the one that reads "The project uses lefthook for pre-commit checks. Documentation changes MUST pass: `task lint:markdown`") with:

```markdown
## Local quality checks

There is no git pre-commit hook (the repo is `jj`-primary; `jj` does not fire
git hooks reliably). Run `task fmt` to format and apply license headers, and
`task pr-prep` to mirror the full CI gate before pushing. Documentation changes
MUST pass `task lint:markdown`.
```

- [ ] **Step 4: Update the root `CLAUDE.md` License Headers row**

In `CLAUDE.md` (License Headers table, `:226`), replace the row:

```markdown
| **Auto-applied** by lefthook        | `task license:add` runs on commit; `task license:check` verifies |
```

with:

```markdown
| **Applied** by `task fmt`            | `task fmt` adds headers via `license-eye`; `task license:check` / CI verify |
```

If any other lefthook mention exists in `CLAUDE.md`, remove or reword it (verify with `rg -n -i lefthook CLAUDE.md`).

- [ ] **Step 5: Mark decision-102 superseded**

In `docs/specs/decisions/epic7/general/102-lefthook-markdown-autofix-intentional.md`, change the `**Status:** Accepted` line (`:10`) to:

```markdown
**Status:** Superseded by holomush-gcio6 (lefthook retired; markdown formatting now via `task fmt` → rumdl, license headers via `license-eye`)
```

- [ ] **Step 6: Run the meta-test to verify it passes**

Run: `bats scripts/tests/no-lefthook-refs.bats`
Expected: PASS (both cases).

- [ ] **Step 7: Supersede ADR holomush-u2exm**

Use the `dev-flow:evolve-adr` skill to add a supersession/amendment note to `holomush-u2exm` recording that the best-effort local commit-msg hook no longer exists (lefthook retired by `holomush-gcio6`); `commit-lint.yaml` remains the sole authoritative conventional-commit gate. Then:

Run: `bd note holomush-u2exm "Amended by holomush-gcio6: lefthook retired; no local commit-msg hook remains; commit-lint.yaml is the sole gate."`

- [ ] **Step 8: Full local gate**

Run: `task test:bats && task lint && task fmt:check`
Expected: PASS.

- [ ] **Step 9: Commit**

```text
jj describe -m "docs: sweep lefthook references; supersede decision-102 + ADR u2exm (holomush-gcio6)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
jj new
```

---

## Post-implementation checklist

- [ ] `task pr-prep` green end-to-end (single command; confirm `✓ All PR checks passed`).
- [ ] All six invariants exercised: INV-1 (coverage table), INV-2 (`generate-ebnf-check.bats`), INV-3 (`no-lefthook-refs.bats`), INV-4 (`license-eye.bats` fmt case), INV-5 (Task 3 Step 5 diff check), INV-6 (`license-eye.bats` content cases).
- [ ] `lefthook.yaml` deleted; `rg -i lefthook` over the INV-3 allowlist is empty.
- [ ] `license-eye` installed in `setup`; `addlicense` removed.
- [ ] EBNF gate present in both `pr-prep:run` and `ci.yaml`.
- [ ] Branch landed via PR (squash merge), per the protected-branch policy.

<!-- adr-capture: sha256=1ce7f75236d255bf; ts=2026-05-26T01:21:39Z; adrs=holomush-x14mc -->
