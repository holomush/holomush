<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Design: `holomush-dev` in-repo plugin + release-notes Python/uv rework

- **Design bead:** holomush-q55eu
- **Status:** DRAFT (awaiting design-reviewer)
- **Date:** 2026-06-29
- **Supersedes (partially):** the implementation shape of the release-notes
  pipeline shipped in PR #4542 (epic holomush-dufb8). The *behavior* and the
  ADR jfb9x narrative-notes contract are unchanged; only the packaging
  (slash command → plugin skill) and the script language (bash → Python/uv)
  change.

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY**
are to be interpreted as described in RFC 2119 / RFC 8174.

## 1. Problem

The release-notes pipeline that merged in PR #4542 was built two ways the
project no longer wants:

1. **Bash scripts.** `scripts/release-notes-collect.sh` and
   `scripts/release-notes-publish.sh` are bash; the repo's established pattern
   for non-trivial scripting is Python in `scripts/` with `uv`, `pytest`, and
   `ruff` (`scripts/pyproject.toml`; `uv run` is the sanctioned test runner per
   `scripts-tests.yaml`, guarded by INV-7 which keeps `uv` on the scripts side
   only).
2. **The outdated slash-command format.** `.claude/commands/release-notes.md`
   (and the repo's seven other loose `.claude/commands/*.md`) use the flat
   command format. The current Claude Code guidance is to express
   user/agent-invocable behavior as **skills**, packaged in a **plugin**, so
   behavior, helper scripts, and metadata travel together and are namespaced.

This design reworks the release-notes pipeline on both axes and, in the same
effort, migrates all eight loose commands into one in-repo plugin.

## 2. Grounding

Primary-source facts (recorded as grounding notes on holomush-q55eu):

- **`@skills-dir` plugins are real and zero-config.** "Any folder under a
  skills directory that contains a `.claude-plugin/plugin.json` manifest is
  loaded as a plugin named `<name>@skills-dir` on the next session, with no
  marketplace and no install step … discovered in place rather than copied into
  the plugin cache." — <https://code.claude.com/docs/en/plugins-reference#skills-directory-plugins>
- **Project-scope load path + trust.** A project-scope `@skills-dir` plugin
  "loads only from the `.claude/skills/` of the directory where you start Claude
  Code. They do not walk up to the repository root … Launch from the repository
  root, or run `/reload-plugins` after changing directories." It loads only
  after the same trust gate that governs `.claude/settings.json`. — same page.
- **Structure.** `plugin.json` is the *only* file inside `.claude-plugin/`; all
  component dirs (`skills/`, `commands/`, `agents/`, `hooks/`, `scripts/`,
  `bin/`) sit at the plugin **root**. — plugins-reference, *Standard plugin layout*.
- **Bundled scripts** are referenced via `${CLAUDE_PLUGIN_ROOT}` (absolute path
  to the plugin's directory). — plugins-reference.
- **Namespacing.** A plugin skill is invoked `/<plugin>:<skill>`; the namespace
  is the manifest `name`. — <https://code.claude.com/docs/en/plugins>.
- **Alternatives rejected:** a local marketplace
  (`.claude-plugin/marketplace.json` + `extraKnownMarketplaces` +
  `enabledPlugins`) is heavier and its directory-source relative-path
  resolution is currently buggy (anthropics/claude-code#23978, needs a manual
  `/plugin marketplace add ./` per clone); `.claude/plugins/` auto-discovery
  does **not** exist (anthropics/claude-code#58147, open/rejected in 2.1.x).
- **Repo precedent (probe):** Python scripts live in `scripts/` with
  `scripts/pyproject.toml` (`holomush-scripts`, py3.12, ruff + pytest);
  `scripts/{adr-migrate,bootstrap_seed_secrets}.py` are the existing examples.
- **Command inventory (probe):** seven of eight loose commands are pure
  prompt-skills (audit-beads, landing-sequence, pr-prep, review-abac,
  review-code, review-crypto, spawn-workspace) that invoke `@agent-*` subagents
  or run `task`; only release-notes carries helper scripts.

## 3. Goals / Non-goals

### Goals

- **MUST** create one in-repo plugin `holomush-dev` via the `@skills-dir`
  mechanism at `.claude/skills/holomush-dev/`.
- **MUST** migrate all eight loose `.claude/commands/*.md` into the plugin as
  skills under `.claude/skills/holomush-dev/skills/<name>/SKILL.md`.
- **MUST** rewrite the release-notes collect/publish scripts in Python as
  PEP 723 single-file scripts run via `uv run`, bundled in the plugin's
  `scripts/` directory, preserving current behavior exactly.
- **MUST** port the 11 bats test cases to `pytest`.
- **MUST** update all *live* references to the renamed commands and removed
  scripts; **MUST NOT** rewrite historical plan/spec records.

### Non-goals

- **MUST NOT** change the narrative-notes contract (ADR jfb9x / INV-7:
  narrative augments, never replaces, the GoReleaser mechanical list; no in-repo
  CHANGELOG.md).
- **MUST NOT** fold the loose `.claude/skills/*` (bug-triage, capture-adrs,
  drain-pane, new-integration-test, new-migration) into the plugin. They
  coexist with the plugin under `.claude/skills/` and MAY migrate in a later
  effort.
- **MUST NOT** touch the installed `dev-flow` (or other) plugins.
- **MUST NOT** add a local marketplace or `.claude/settings.json` plugin entry
  (the `@skills-dir` mechanism needs none).

## 4. Design

### 4.1 Plugin layout

```text
.claude/skills/holomush-dev/
├── .claude-plugin/
│   └── plugin.json
├── skills/
│   ├── release-notes/SKILL.md
│   ├── audit-beads/SKILL.md
│   ├── landing-sequence/SKILL.md
│   ├── pr-prep/SKILL.md
│   ├── review-abac/SKILL.md
│   ├── review-code/SKILL.md
│   ├── review-crypto/SKILL.md
│   └── spawn-workspace/SKILL.md
└── scripts/
    ├── release_notes_collect.py
    ├── release_notes_publish.py
    └── tests/
        └── test_release_notes.py
```

`plugin.json` (manifest):

```json
{
  "name": "holomush-dev",
  "description": "HoloMUSH in-repo dev tooling: release notes, review gates, pr-prep, workspace + bead helpers.",
  "version": "0.1.0"
}
```

- The plugin loads as `holomush-dev@skills-dir`; skills are invoked
  `/holomush-dev:<name>`.
- It coexists with the existing loose `.claude/skills/*` skills (those have no
  `.claude-plugin/` manifest, so they remain plain skills).

### 4.2 Skill frontmatter policy

Each `SKILL.md` carries `name:` and `description:` frontmatter. Model-invocation
is set per skill by intent:

| Skill | `disable-model-invocation` | Rationale |
| --- | --- | --- |
| release-notes | `true` | Explicit maintainer-run release step. |
| spawn-workspace | `true` | Explicit setup action. |
| landing-sequence | `true` | Explicit session-completion run. |
| pr-prep | `true` | Explicit pre-push run. |
| audit-beads | `true` | Explicit maintenance run. |
| review-abac | *omitted* | Claude SHOULD auto-fire at the ABAC gate. |
| review-code | *omitted* | Claude SHOULD auto-fire at the pre-push gate. |
| review-crypto | *omitted* | Claude SHOULD auto-fire at the crypto gate. |

The skill bodies are preserved from the current command bodies, with two
edits: (a) release-notes references its scripts via
`uv run "${CLAUDE_PLUGIN_ROOT}/scripts/release_notes_collect.py" <tag>`; (b)
inter-skill cross-references are rewritten to the namespace (e.g. review-abac /
review-crypto "then invoke `/holomush-dev:review-code`"). `@agent-*` references
are unchanged — those agents live in `.claude/agents/` and are independent of
this move.

### 4.3 release-notes scripts (Python + uv)

`release_notes_collect.py` and `release_notes_publish.py` reproduce the exact
behavior of the current bash scripts, including the two corrections that merged
after #4542's first review round:

- **collect:** resolve `PREV` from semver tags only
  (`git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname`); print the
  filtered mechanical commit set; harvest **EXCLUDE-filtered** referenced bead
  IDs (multi-level IDs preserved, distinct, sorted); coverage-gaps section;
  roadmap-theme pointer. Fail cleanly when the tag does not exist.
- **publish:** `--tag` / `--narrative-file`; fetch the existing GoReleaser body
  via `gh release view`; **fail closed** on empty narrative and on empty
  existing body (jfb9x INV-7); combine `narrative + "\n\n---\n\n" + existing`;
  `gh release edit --notes-file`; `-R holomush/holomush` explicit.

Each file carries a PEP 723 header:

```python
# /// script
# requires-python = ">=3.12"
# dependencies = []
# ///
```

Stdlib only; `git`, `gh`, and `bd` are invoked via `subprocess`. Invocation is
`uv run "${CLAUDE_PLUGIN_ROOT}/scripts/<name>.py" …`.

### 4.4 Tests (pytest)

`scripts/tests/test_release_notes.py` (inside the plugin) ports all 11 bats
cases (8 collect, 3 publish). Tests build a temporary git repo fixture and put
a stub **`gh`** executable on `PATH`, then invoke the scripts via `uv run` and
assert on stdout/exit code. The current bats fixtures
(`scripts/tests/release-notes.bats`) stub **`gh` only**: the collect script's
`bd show` call is guarded by `command -v bd` with a bare-id fallback and is not
exercised, so the pytest port MUST stub `gh` only and MUST NOT stub `bd`. The
publish fail-closed guards (empty narrative; empty existing body) **MUST** be
covered.

**Toolchain + CI (resolves former Open Question #1).** The plugin's Python
scripts and tests use a **plugin-local** `pyproject.toml` at
`.claude/skills/holomush-dev/scripts/pyproject.toml` (dev group: `pytest`,
`ruff`; ruff config mirroring `scripts/pyproject.toml`). The tests live with
the bundled scripts, so a plugin-local config is cleaner than extending the
repo `scripts/pyproject.toml` `testpaths` across directory boundaries. CI
(`.github/workflows/scripts-tests.yaml`) MUST be updated to:

- add `.claude/skills/holomush-dev/scripts/**` to the workflow `paths:`
  trigger (it currently triggers only on `scripts/**`), and
- run `uv run --group dev pytest` (and `ruff check`) with
  `working-directory: .claude/skills/holomush-dev/scripts` (the existing job
  runs with `working-directory: scripts` and would not otherwise discover the
  plugin tests).

This is the load-bearing "no behavior drift" gate for the bash→Python rewrite.

### 4.5 Removals and live-reference updates (same change)

**Delete:**

- `.claude/commands/{release-notes,audit-beads,landing-sequence,pr-prep,review-abac,review-code,review-crypto,spawn-workspace}.md`
- `scripts/release-notes-collect.sh`, `scripts/release-notes-publish.sh`
- `scripts/tests/release-notes.bats`

**Update (live references only):**

- `CLAUDE.md` — the Pre-Push Review Gates table and the `/release-notes`,
  `/pr-prep`, `/audit-beads`, `/landing-sequence`, `/spawn-workspace`,
  **`/review-abac`, `/review-code`, `/review-crypto`** mentions →
  `/holomush-dev:<name>`. **MUST NOT** rename `/review-design` or
  `/review-plan`: those are `dev-flow` *plugin* commands (no
  `.claude/commands/review-{design,plan}.md` exist) and are out of scope —
  rename exactly the eight migrated commands, not a `/review-*` glob.
- `.claude/agents/*.md` — agent bodies that name a bare migrated command MUST be
  updated to the namespaced form (e.g. `.claude/agents/code-reviewer.md:132`
  "If invoked via `/review-code`" → `/holomush-dev:review-code`). Grep all agent
  files for `/review-abac`, `/review-code`, `/review-crypto`, `/pr-prep`,
  `/audit-beads`, `/landing-sequence`, `/spawn-workspace`, `/release-notes`.
- `.claude/hooks/remind-pre-action-review.sh` — reminder text naming the review
  commands → namespaced names (review-abac/code/crypto only).
- `.github/workflows/scripts-tests.yaml` — add `.claude/skills/holomush-dev/scripts/**`
  to `paths:` and run pytest/ruff with
  `working-directory: .claude/skills/holomush-dev/scripts` (see §4.4).
- `Taskfile.yaml` — `release:notes:collect` repointed to a thin `uv run`
  wrapper over the new Python script (see §8); the bats target no longer
  includes the deleted release-notes file.

**Do NOT update:** historical `docs/superpowers/{plans,specs}/*` references to
the old command names — they are point-in-time records. `task pr-prep` (the
go-task target) is unrelated to the `/pr-prep` slash command and is unchanged.

## 5. Error handling & fidelity

- The Python scripts MUST preserve exit-code semantics (e.g. publish exits
  non-zero and prints an `::error::` line on empty narrative / empty existing
  body) so CI and callers behave identically.
- INV-7 has **no** risk here (confirmed against the gate in `Taskfile.yaml`):
  the gate scans `site/` and `Taskfile.yaml` only and bans docs-side `uv`;
  `.claude/skills/` is entirely outside its scope, and any Taskfile line
  referencing a `scripts`-pathed command passes the gate's `rg -v 'scripts'`
  filter. The bundled Python therefore introduces no INV-7 exposure regardless
  of the Taskfile-wrapper decision.

## 6. Testing strategy

- **Unit/behavioral:** pytest port (§4.4), green under `uv run pytest`.
- **Lint:** ruff over the new Python; `task lint` / `task pr-prep` fast lane
  green.
- **Plugin load smoke:** after install, `/holomush-dev:release-notes` resolves
  and `uv run "${CLAUDE_PLUGIN_ROOT}/scripts/release_notes_collect.py" <tag>`
  runs from a workspace root.
- **No behavior drift:** spot-check `collect`/`publish` output against the
  pre-rework bash output for a known tag range.

## 7. Risks

- **Hard cutover** removes the bare `/pr-prep`, `/review-*`, etc. invocations;
  mitigated by updating CLAUDE.md + the hook so the namespaced names are
  discoverable. No compatibility shims (removing the command format is a goal).
- **`@skills-dir` cwd caveat:** the plugin loads from the launch dir's
  `.claude/skills/`. Each jj workspace root carries its own tracked `.claude/`,
  so workspace-root launches work; sub-dir launches need `/reload-plugins`.
  Document this in the plugin or CLAUDE.md.
- **Test-fidelity:** the pytest port must keep all 11 behaviors green,
  especially the publish INV-7 fail-closed guards.

## 8. Resolved decisions

1. **ruff/pytest config location** — RESOLVED: a **plugin-local**
   `pyproject.toml` at `.claude/skills/holomush-dev/scripts/` (dev group:
   `pytest`, `ruff`). The tests live with the bundled scripts; a plugin-local
   config avoids cross-directory `testpaths` escaping the repo `scripts/`
   package, and pairs with the CI `working-directory` change in §4.4/§4.5.
2. **`release:notes:collect` Taskfile target** — RESOLVED: keep it as a thin
   `uv run "$REPO/.claude/skills/holomush-dev/scripts/release_notes_collect.py"`
   wrapper so CI and humans can run collection without the plugin. (`publish`
   stays plugin-only, invoked from the skill, since it is the
   maintainer-approved outward-facing step.)

## 9. Implementation phases (for the plan)

1. Scaffold `holomush-dev` plugin (`plugin.json`, dir skeleton); smoke-load.
2. Port release-notes scripts to Python (PEP 723) + pytest; verify behavior
   parity; wire into scripts test lane; delete bash + bats.
3. Migrate the seven prompt-skills (command → SKILL.md, frontmatter, namespace
   cross-refs); delete the loose commands.
4. Update live references (CLAUDE.md scoped to the 8 migrated commands;
   `.claude/agents/*.md` bodies; `remind-pre-action-review.sh`;
   `scripts-tests.yaml` trigger + working-directory; Taskfile wrapper); verify
   docs-symmetry + docs lints; `task pr-prep` green.
<!-- adr-capture: sha256=c526d761cae853e1; session=7900b2a3; ts=2026-06-30T00:49:22Z; adrs=holomush-jpn4w -->
