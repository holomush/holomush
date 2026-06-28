<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Narrative Release-Notes Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a maintainer-run `/release-notes <tag>` workflow that publishes a narrative TLDR above GoReleaser's mechanical notes (GitHub Release body) and to a `docs/releases/` site section, sourced from `theme:*` roadmap sections + closed bead epics, with commit subjects as fallback.

**Architecture:** Two deterministic bash scripts plus one orchestrating slash command. `release-notes-collect.sh` gathers the raw range materials (filtered commits, referenced beads, roadmap theme sections, coverage gaps) and prints a structured context block. The in-session model (driven by `.claude/commands/release-notes.md`) clusters that block into narrative prose. `release-notes-publish.sh` fetches the existing GoReleaser body, combines it under the narrative, and publishes via `gh release edit --notes-file` (never replacing the mechanical list). The site post and the `starlightSidebarTopics` registration land as a normal post-tag docs PR.

**Tech Stack:** Bash (defensive `set -euo pipefail`), bats-core (git-fixture pattern, `task test:bats`), `gh` CLI, `bd` CLI, Astro Starlight + `starlight-sidebar-topics`, go-task.

**Spec:** `docs/superpowers/specs/2026-06-28-release-notes-narrative-pipeline-design.md`
**Design bead:** holomush-dufb8

---

## File Structure

| File | Responsibility | Created by |
| ---- | -------------- | ---------- |
| `scripts/release-notes-collect.sh` | Resolve `prev..new` tag range; emit structured markdown context (filtered commits, referenced beads, theme sections, coverage gaps) | Task 1 |
| `scripts/release-notes-publish.sh` | Fetch existing release body; build combined draft (narrative + separator + existing); publish via `gh release edit --notes-file`; refuse narrative-only / `--notes` inline | Task 2 |
| `scripts/tests/release-notes.bats` | bats coverage for both scripts (range resolution, bead extraction, coverage accounting, publish guards via mocked `gh`/`bd`) | Tasks 1 & 2 |
| `.claude/commands/release-notes.md` | Orchestrator slash command: run collector → draft narrative in-session → run publisher → emit site page | Task 3 |
| `Taskfile.yaml` (`release:notes:collect`) | Non-interactive wrapper documenting the collector entry point | Task 3 |
| `site/astro.config.mjs` | Register the sixth `releases` sidebar topic (mandatory — unregistered dirs orphan) | Task 4 |
| `site/src/content/docs/releases/index.mdx` | "Release history" landing page | Task 4 |
| `site/src/content/docs/releases/v0.10.0.md` | First narrative post (v0.10.0 backfill) | Task 5 |

---

## Task 1: Range collector script

**Files:**

- Create: `scripts/release-notes-collect.sh`
- Test: `scripts/tests/release-notes.bats`

The collector is deterministic: given a tag, it resolves the previous tag, then prints a structured context block the in-session model consumes. It does NOT write prose. Mirrors the GoReleaser exclude filters (`^docs:`, `^test:`, `^chore:`, merge commits) so the "filtered commits" section matches the mechanical list.

- [ ] **Step 1: Write the failing test for range resolution**

In `scripts/tests/release-notes.bats` (new file). Follow the `cog-release.bats` git-fixture pattern:

```bash
#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

# Validates release-notes-collect.sh range resolution, filtered-commit
# extraction, bead-ref harvesting, and coverage accounting; and
# release-notes-publish.sh fetch-combine-publish guards via a mocked gh.

setup() {
  REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  FIX="$BATS_TEST_TMPDIR/fix"
  mkdir -p "$FIX"
  cd "$FIX"
  git init -q -b main
  git config user.email "t@example.com"
  git config user.name "Test"
  echo seed > seed.txt
  git add -A
  git commit -q -m "chore: seed"
  git tag v0.1.0
  git commit -q --allow-empty -m "feat(scenes): web settings (holomush-5rh.24)"
  git commit -q --allow-empty -m "fix(focus): unify delta coordinator (holomush-66228)"
  # Unscoped 'docs:' — GoReleaser's filter is literally ^docs: and does NOT
  # match scoped docs(scope):, so the fixture MUST be unscoped to be excluded.
  git commit -q --allow-empty -m "docs: mark SP1 landed"
  git commit -q --allow-empty -m "feat(session): liveness leases"
  git tag v0.2.0
}

@test "collect resolves the previous tag from the target tag" {
  run "$REPO_ROOT/scripts/release-notes-collect.sh" v0.2.0
  [ "$status" -eq 0 ]
  [[ "$output" == *"Range: v0.1.0..v0.2.0"* ]]
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd "$(git rev-parse --show-toplevel)" && BATS_LIB_PATH="${BATS_LIB_PATH:-/opt/homebrew/lib:/usr/local/lib:/usr/lib}" bats scripts/tests/release-notes.bats -f "resolves the previous tag"`
Expected: FAIL — `release-notes-collect.sh` does not exist (status 127).

- [ ] **Step 3: Write the minimal collector to resolve the range**

In `scripts/release-notes-collect.sh`:

```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# release-notes-collect.sh <tag> — print a structured context block for the
# /release-notes workflow. Deterministic data-gathering only; the in-session
# model turns this block into narrative prose. No prose is written here.
set -euo pipefail

TAG="${1:?usage: release-notes-collect.sh <vX.Y.Z>}"

# Previous tag = the tag immediately before <tag> in version order.
PREV="$(git tag --sort=-v:refname | grep -A1 -xF "$TAG" | tail -n1)"
if [ -z "$PREV" ] || [ "$PREV" = "$TAG" ]; then
  echo "::error:: could not resolve a previous tag before $TAG" >&2
  exit 1
fi

echo "# Release context for $TAG"
echo
echo "Range: ${PREV}..${TAG}"
echo
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `BATS_LIB_PATH="${BATS_LIB_PATH:-/opt/homebrew/lib:/usr/local/lib:/usr/lib}" bats scripts/tests/release-notes.bats -f "resolves the previous tag"`
Expected: PASS.

- [ ] **Step 5: Write the failing test for filtered commits + bead refs + coverage**

Append to `scripts/tests/release-notes.bats`:

```bash
@test "collect lists filtered commits and excludes docs/test/chore" {
  run "$REPO_ROOT/scripts/release-notes-collect.sh" v0.2.0
  [ "$status" -eq 0 ]
  [[ "$output" == *"feat(scenes): web settings (holomush-5rh.24)"* ]]
  [[ "$output" == *"feat(session): liveness leases"* ]]
  # 'docs: mark SP1 landed' is excluded by the ^docs: filter
  [[ "$output" != *"docs: mark SP1 landed"* ]]
}

@test "collect harvests distinct holomush bead refs" {
  run "$REPO_ROOT/scripts/release-notes-collect.sh" v0.2.0
  [[ "$output" == *"holomush-5rh.24"* ]]
  [[ "$output" == *"holomush-66228"* ]]
}

@test "collect reports commits with no bead ref under coverage gaps" {
  run "$REPO_ROOT/scripts/release-notes-collect.sh" v0.2.0
  # 'feat(session): liveness leases' has no holomush-<id> ref
  [[ "$output" == *"## Coverage gaps (no bead ref)"* ]]
  [[ "$output" == *"feat(session): liveness leases"* ]]
}
```

- [ ] **Step 6: Run the new tests to verify they fail**

Run: `BATS_LIB_PATH="${BATS_LIB_PATH:-/opt/homebrew/lib:/usr/local/lib:/usr/lib}" bats scripts/tests/release-notes.bats -f "collect"`
Expected: the three new tests FAIL (sections not yet emitted); the range test still passes.

- [ ] **Step 7: Extend the collector with the filtered-commit, bead-ref, and coverage sections**

Append to `scripts/release-notes-collect.sh` (after the `Range:` line):

```bash
# GoReleaser-equivalent exclude filters (.goreleaser.yaml:111-119 changelog
# block). Mirror it EXACTLY: ^docs: is anchored and does NOT match scoped
# docs(scope): commits — same as GoReleaser. Do not "improve" this to catch
# scoped prefixes, or the filtered section diverges from the mechanical list
# it cross-checks. The Merge patterns are unanchored exactly as GoReleaser
# leaves them (the collector also passes --no-merges, so they rarely matter).
EXCLUDE='^docs:|^test:|^chore:|Merge pull request|Merge branch'

mapfile -t SUBJECTS < <(git log --no-merges --pretty='%s' "${PREV}..${TAG}")

echo "## Filtered commits (mechanical set)"
echo
for s in "${SUBJECTS[@]}"; do
  printf '%s\n' "$s" | grep -Eqv "$EXCLUDE" && echo "- $s"
done
echo

echo "## Referenced beads"
echo
printf '%s\n' "${SUBJECTS[@]}" \
  | grep -oE 'holomush-[a-z0-9]+(\.[0-9]+)?' \
  | sort -u \
  | while read -r id; do
      # bd show is best-effort: degrade to the bare id if bd is unavailable.
      if command -v bd >/dev/null 2>&1; then
        line="$(bd show "$id" --json 2>/dev/null \
          | { command -v jq >/dev/null 2>&1 && jq -r '"\(.id) [\(.type)] \(.title) labels=\(.labels // [] | join(","))"' || cat; })"
        echo "- ${line:-$id}"
      else
        echo "- $id"
      fi
    done
echo

echo "## Coverage gaps (no bead ref)"
echo
for s in "${SUBJECTS[@]}"; do
  printf '%s\n' "$s" | grep -Eq "$EXCLUDE" && continue
  printf '%s\n' "$s" | grep -Eq 'holomush-[a-z0-9]+' || echo "- $s"
done
echo

echo "## Roadmap theme sections"
echo
echo "Consult docs/roadmap.md for theme:* sections; the model maps referenced"
echo "beads' theme labels to the relevant narrative headings."
```

- [ ] **Step 8: Run the collector tests to verify they pass**

Run: `BATS_LIB_PATH="${BATS_LIB_PATH:-/opt/homebrew/lib:/usr/local/lib:/usr/lib}" bats scripts/tests/release-notes.bats -f "collect"`
Expected: all four `collect` tests PASS.

- [ ] **Step 9: Lint the script and commit**

Run: `cd "$(git rev-parse --show-toplevel)" && task fmt && shellcheck scripts/release-notes-collect.sh`
Expected: shellcheck clean; `task fmt` applies/keeps the SPDX header.
Then commit per `references/vcs-preamble.md` (jj): `jj commit -m "feat(release): range collector for narrative release notes (holomush-dufb8)"`.

---

## Task 2: Publish script with INV-7 guards

**Files:**

- Create: `scripts/release-notes-publish.sh`
- Test: `scripts/tests/release-notes.bats` (extend)

This script enforces the spec's GitHub-publisher contract: `gh release edit --notes-file` **replaces** the body, so it MUST fetch the existing GoReleaser body and combine, never publish a narrative-only body (which would drop the mechanical list and violate jfb9x INV-7).

- [ ] **Step 1: Write the failing test for the combine-and-publish contract**

Append to `scripts/tests/release-notes.bats`. Stub `gh` on `PATH` so the test is deterministic and offline:

```bash
@test "publish refuses an empty narrative file" {
  echo -n "" > "$BATS_TEST_TMPDIR/narr.md"
  run "$REPO_ROOT/scripts/release-notes-publish.sh" --tag v0.2.0 --narrative-file "$BATS_TEST_TMPDIR/narr.md"
  [ "$status" -ne 0 ]
  [[ "$output" == *"narrative file is empty"* ]]
}

@test "publish combines narrative with the existing GoReleaser body" {
  # Mock gh: 'release view' prints a fixture body; 'release edit' records its
  # --notes-file argument's contents to a sentinel file.
  BIN="$BATS_TEST_TMPDIR/bin"; mkdir -p "$BIN"
  cat > "$BIN/gh" <<'EOF'
#!/usr/bin/env bash
if [ "$1 $2" = "release view" ]; then printf '## Changelog\n- feat: existing (#1)\n'; exit 0; fi
if [ "$1 $2" = "release edit" ]; then
  while [ $# -gt 0 ]; do [ "$1" = "--notes-file" ] && cp "$2" "$GH_SENTINEL"; shift; done
  exit 0
fi
exit 0
EOF
  chmod +x "$BIN/gh"
  printf '## What changed\nNarrative TLDR here.\n' > "$BATS_TEST_TMPDIR/narr.md"
  GH_SENTINEL="$BATS_TEST_TMPDIR/published.md" PATH="$BIN:$PATH" \
    run "$REPO_ROOT/scripts/release-notes-publish.sh" --tag v0.2.0 --narrative-file "$BATS_TEST_TMPDIR/narr.md"
  [ "$status" -eq 0 ]
  # Combined body MUST contain BOTH the narrative AND the existing mechanical list.
  grep -q "Narrative TLDR here." "$BATS_TEST_TMPDIR/published.md"
  grep -q "feat: existing (#1)" "$BATS_TEST_TMPDIR/published.md"
}

@test "publish fails closed when the existing release body is empty" {
  # Mock gh: 'release view' returns an EMPTY body; 'release edit' would record a
  # publish. Fail-closed means we MUST exit non-zero and never reach release edit.
  BIN="$BATS_TEST_TMPDIR/bin"; mkdir -p "$BIN"
  cat > "$BIN/gh" <<'EOF'
#!/usr/bin/env bash
if [ "$1 $2" = "release view" ]; then printf ''; exit 0; fi
if [ "$1 $2" = "release edit" ]; then touch "$GH_SENTINEL"; exit 0; fi
exit 0
EOF
  chmod +x "$BIN/gh"
  printf '## What changed\nNarrative TLDR here.\n' > "$BATS_TEST_TMPDIR/narr.md"
  GH_SENTINEL="$BATS_TEST_TMPDIR/published.md" PATH="$BIN:$PATH" \
    run "$REPO_ROOT/scripts/release-notes-publish.sh" --tag v0.2.0 --narrative-file "$BATS_TEST_TMPDIR/narr.md"
  [ "$status" -ne 0 ]
  [[ "$output" == *"existing release body for v0.2.0 is empty"* ]]
  # release edit MUST NOT have run — no narrative-only publish.
  [ ! -e "$BATS_TEST_TMPDIR/published.md" ]
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `BATS_LIB_PATH="${BATS_LIB_PATH:-/opt/homebrew/lib:/usr/local/lib:/usr/lib}" bats scripts/tests/release-notes.bats -f "publish"`
Expected: both FAIL — `release-notes-publish.sh` does not exist (status 127).

- [ ] **Step 3: Write the publish script**

In `scripts/release-notes-publish.sh`:

```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# release-notes-publish.sh --tag <vX.Y.Z> --narrative-file <path>
#
# Publishes the narrative ABOVE the existing GoReleaser release body. Because
# `gh release edit --notes-file` REPLACES the body, this script fetches the
# current body first and combines (narrative + separator + existing). It MUST
# NOT publish a narrative-only body — that would drop the mechanical commit
# list and violate jfb9x INV-7.
set -euo pipefail

TAG=""; NARR=""
while [ $# -gt 0 ]; do
  case "$1" in
    --tag) TAG="${2:?}"; shift 2 ;;
    --narrative-file) NARR="${2:?}"; shift 2 ;;
    *) echo "::error:: unknown arg: $1" >&2; exit 2 ;;
  esac
done
[ -n "$TAG" ] || { echo "::error:: --tag is required" >&2; exit 2; }
[ -n "$NARR" ] || { echo "::error:: --narrative-file is required" >&2; exit 2; }
if [ ! -s "$NARR" ]; then
  echo "::error:: narrative file is empty; refusing to publish (would drop the GoReleaser list)" >&2
  exit 1
fi

EXISTING="$(gh release view "$TAG" --json body -q .body 2>/dev/null || true)"
if [ -z "$EXISTING" ]; then
  # Fail closed: an empty body means GoReleaser hasn't run (or the wrong tag was
  # given). Publishing narrative-only would silently drop the mechanical list and
  # violate INV-7. Surface the anomaly instead of papering over it.
  echo "::error:: existing release body for $TAG is empty — refusing to publish narrative-only (GoReleaser notes missing; INV-7). Run GoReleaser first or check the tag." >&2
  exit 1
fi

COMBINED="$(mktemp)"
trap 'rm -f "$COMBINED"' EXIT
cat "$NARR" > "$COMBINED"
printf '\n\n---\n\n' >> "$COMBINED"
printf '%s\n' "$EXISTING" >> "$COMBINED"

# --notes-file (never --notes inline): the combined file is the whole body.
gh release edit "$TAG" --notes-file "$COMBINED"
echo "Published combined release notes for $TAG" >&2
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `BATS_LIB_PATH="${BATS_LIB_PATH:-/opt/homebrew/lib:/usr/local/lib:/usr/lib}" bats scripts/tests/release-notes.bats -f "publish"`
Expected: both `publish` tests PASS.

- [ ] **Step 5: Run the full bats file to confirm no regressions**

Run: `BATS_LIB_PATH="${BATS_LIB_PATH:-/opt/homebrew/lib:/usr/local/lib:/usr/lib}" bats scripts/tests/release-notes.bats`
Expected: all tests (Task 1 + Task 2) PASS.

- [ ] **Step 6: Lint and commit**

Run: `task fmt && shellcheck scripts/release-notes-publish.sh`
Expected: shellcheck clean.
Commit (jj): `jj commit -m "feat(release): INV-7-safe publisher for narrative release notes (holomush-dufb8)"`.

---

## Task 3: Orchestrator slash command + Taskfile wrapper

**Files:**

- Create: `.claude/commands/release-notes.md`
- Modify: `Taskfile.yaml` (add `release:notes:collect` near the existing `release:*` block around line 1181)

The slash command is the human entry point. It is prose-instructions for the in-session model (not executable code), so it is not bats-tested; it documents the MUST contracts and chains the two scripts. `.claude/` is excluded from `lint:markdown`, so this file is not rumdl-gated.

- [ ] **Step 1: Write the slash command**

In `.claude/commands/release-notes.md`:

```markdown
---
description: Draft and publish a narrative TLDR for a release (GitHub Release body + docs site)
---

Produce human-readable narrative release notes for tag **$ARGUMENTS** (a
`vX.Y.Z` tag). The narrative augments — never replaces — GoReleaser's
mechanical commit list. Do NOT create an in-repo CHANGELOG.md (ADR
holomush-jfb9x).

**Steps:**

1. **Gather context.** Run `task release:notes:collect -- $ARGUMENTS` (or
   `scripts/release-notes-collect.sh $ARGUMENTS`). Read the structured block:
   filtered commits, referenced beads (with theme labels), coverage gaps, and
   the roadmap theme pointer.

2. **Read `docs/roadmap.md`** theme sections matching the referenced beads'
   `theme:*` labels — these carry the *why* and become the narrative headlines.

3. **Draft the narrative** to a temp file. Structure: a 2–3 sentence TLDR, then
   theme-grouped Features, then Fixes, then an "Other changes" catch-all that
   MUST account for every commit in the "Coverage gaps" section — nothing is
   silently dropped. If a commit maps to no theme/epic and is non-trivial, ask
   the maintainer rather than guessing.

4. **Publish to GitHub** (only after the maintainer approves the draft):
   `scripts/release-notes-publish.sh --tag $ARGUMENTS --narrative-file <temp>`.
   The script fetches the existing GoReleaser body and combines; never pass a
   narrative-only body.

5. **Emit the site post** as a SEPARATE post-tag docs change (feature branch →
   PR): write `site/src/content/docs/releases/$ARGUMENTS.md` (same narrative
   body) and add a reverse-chronological link to
   `site/src/content/docs/releases/index.mdx`. Confirm the `releases` topic is
   registered in `site/astro.config.mjs` (Task 4) or the page orphans silently.
```

- [ ] **Step 2: Add the Taskfile collector wrapper**

In `Taskfile.yaml`, after the `release:cut` task (around line 1185), add:

```yaml
  release:notes:collect:
    desc: "Print the structured context block for narrative release notes. Args: <vX.Y.Z>"
    cmds:
      - scripts/release-notes-collect.sh {{.CLI_ARGS}}
```

- [ ] **Step 3: Verify the Taskfile parses and the wrapper runs**

Run: `cd "$(git rev-parse --show-toplevel)" && task release:notes:collect -- v0.10.0 | head -5`
Expected: prints `# Release context for v0.10.0` and `Range: v0.9.0..v0.10.0`.

- [ ] **Step 4: Lint and commit**

Run: `task fmt && task lint:yaml`
Expected: YAML format check passes (Taskfile edit is well-formed).
Commit (jj): `jj commit -m "feat(release): /release-notes orchestrator + collect task (holomush-dufb8)"`.

---

## Task 4: Site release-history section

**Files:**

- Modify: `site/astro.config.mjs:46-80` (add the `releases` topic)
- Create: `site/src/content/docs/releases/index.mdx`

An unregistered directory produces URL-reachable but sidebar-invisible pages — and `task docs:build` + `task lint:markdown` both pass, so the orphaning slips local gates. Registration is mandatory.

- [ ] **Step 1: Register the `releases` topic**

In `site/astro.config.mjs`, inside the `starlightSidebarTopics([ ... ])` array, after the `Reference` topic block (its closing `},` is at line 77) and before the array's closing `]` at line 78, add:

```js
            {
              label: 'Releases',
              link: '/releases/',
              icon: 'rocket',
              items: [{ autogenerate: { directory: 'releases' } }],
            },
```

- [ ] **Step 2: Create the release-history landing page**

In `site/src/content/docs/releases/index.mdx`:

```mdx
---
title: Release history
description: Narrative summaries of each HoloMUSH release.
---

Each release gets a human-readable summary of what changed and why. For the
full mechanical commit list and signed artifacts, see the matching
[GitHub Release](https://github.com/holomush/holomush/releases).

## Releases

- [v0.10.0](/releases/v0.10.0/)
```

- [ ] **Step 3: Build the site to confirm the topic registers (not orphaned)**

Run: `cd "$(git rev-parse --show-toplevel)" && task docs:build`
Expected: build succeeds; the `releases` topic appears in the generated sidebar (no build error). The `v0.10.0` link will 404 until Task 5 creates the page — that is expected at this step.

- [ ] **Step 4: Lint the site markdown**

Run: `task lint:markdown`
Expected: PASS — `site/src/content/docs/releases/index.mdx` conforms to `site/.rumdl.toml`.

- [ ] **Step 5: Commit**

Commit (jj): `jj commit -m "feat(site): release-history section + sidebar topic (holomush-dufb8)"`.

---

## Task 5: v0.10.0 retroactive backfill

**Files:**

- Create: `site/src/content/docs/releases/v0.10.0.md`

Validate the whole pipeline against the real v0.9.0..v0.10.0 range, publish to the existing GitHub Release, and create the first site post.

- [ ] **Step 1: Generate the context block for v0.10.0**

Run: `cd "$(git rev-parse --show-toplevel)" && task release:notes:collect -- v0.10.0`
Expected: structured block with `Range: v0.9.0..v0.10.0`, the filtered commits, referenced beads (web scene settings, session liveness leases, crypto read-back, brand refresh, colon→dot subjects), and coverage gaps.

- [ ] **Step 2: Draft the narrative**

Following `.claude/commands/release-notes.md` steps 2–3, read `docs/roadmap.md` theme sections and draft `site/src/content/docs/releases/v0.10.0.md` with frontmatter:

```mdx
---
title: v0.10.0
description: Web scene management, session liveness, host-mediated crypto read-back, and the holographic brand refresh.
---
```

The body MUST account for every commit in the collector's "Coverage gaps" section under an "Other changes" heading.

- [ ] **Step 3: Publish the combined notes to the existing GitHub Release**

Run (after maintainer approval of the draft body, saved to a temp file):
`scripts/release-notes-publish.sh --tag v0.10.0 --narrative-file <temp-with-body-only>`
Expected: exit 0; the v0.10.0 Release body now leads with the narrative, followed by `---`, followed by the original GoReleaser list. Verify: `gh release view v0.10.0 --json body -q .body | head -20` shows the narrative first and the mechanical list still present below the separator.

- [ ] **Step 4: Build and lint the site**

Run: `task docs:build && task lint:markdown`
Expected: build succeeds; the `releases/v0.10.0/` page renders; markdown lint passes.

- [ ] **Step 5: Commit**

Commit (jj): `jj commit -m "docs(release): v0.10.0 narrative notes + site post (holomush-dufb8)"`.

- [ ] **Step 6: Run the fast pre-push gate**

Run: `task pr-prep`
Expected: exit 0 (`✓ All PR checks passed.`). The diff is bats + scripts + site + docs only — no Docker/int surface — so the fast lane is the correct gate. If it reports contention (~2s, `another pr-prep is running`), wait and re-run; do NOT loop on the lock string.

---

## Post-Implementation Checklist

- [ ] `task test:bats` green (covers `release-notes.bats`).
- [ ] `shellcheck` clean on both new scripts.
- [ ] `task docs:build` succeeds with the `releases` topic in the sidebar.
- [ ] `task lint:markdown` passes on the new site pages.
- [ ] v0.10.0 GitHub Release body shows narrative + separator + original GoReleaser list (INV-7 preserved).
- [ ] No in-repo `CHANGELOG.md` introduced; no write to `main` at release-cut time (jfb9x INV-3 preserved).
- [ ] `task pr-prep` green before push.
<!-- adr-capture: sha256=dbf885e881a92277; session=cli; ts=2026-06-28T20:07:54Z; adrs=holomush-avxk7,holomush-3rmph,holomush-qoxsv -->
