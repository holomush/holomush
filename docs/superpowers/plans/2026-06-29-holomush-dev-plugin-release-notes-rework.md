<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# holomush-dev Plugin + release-notes Python/uv Rework Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate all 8 loose `.claude/commands/*.md` into one in-repo `holomush-dev` plugin (`@skills-dir`), rewrite the release-notes collect/publish bash scripts as PEP 723 Python run via `uv` (bundled in the plugin), and port the bats tests to pytest — preserving behavior exactly.

**Architecture:** A single `@skills-dir` plugin at `.claude/skills/holomush-dev/` with a `.claude-plugin/plugin.json` manifest, eight skills under `skills/<name>/SKILL.md`, and two bundled Python scripts under `scripts/`. Skills invoke bundled scripts via `${CLAUDE_PLUGIN_ROOT}`. Behavior parity is locked by a pytest port of the 11 bats cases, wired into CI.

**Tech Stack:** Claude Code plugins (`@skills-dir`), Python 3.12 (PEP 723 single-file scripts, stdlib only), `uv`, `pytest`, `ruff`, GitHub Actions, go-task.

**Spec:** `docs/superpowers/specs/2026-06-29-holomush-dev-plugin-release-notes-rework-design.md`
**Design bead:** holomush-q55eu

---

## File Structure

| Path | Responsibility |
| --- | --- |
| `.claude/skills/holomush-dev/.claude-plugin/plugin.json` | Plugin manifest (name = namespace) |
| `.claude/skills/holomush-dev/skills/<name>/SKILL.md` | One per migrated command (8 total) |
| `.claude/skills/holomush-dev/scripts/release_notes_collect.py` | Collector (PEP 723) |
| `.claude/skills/holomush-dev/scripts/release_notes_publish.py` | Publisher (PEP 723) |
| `.claude/skills/holomush-dev/scripts/pyproject.toml` | Plugin-local pytest/ruff config |
| `.claude/skills/holomush-dev/scripts/tests/test_release_notes.py` | Pytest port of the 11 bats cases |
| `.github/workflows/scripts-tests.yaml` | + trigger path + a job for the plugin tests |
| `Taskfile.yaml` | `release:notes:collect` → thin `uv run` wrapper |
| `CLAUDE.md`, `.claude/agents/code-reviewer.md`, `.claude/hooks/remind-pre-action-review.sh` | Live namespace references |
| (deleted) `scripts/release-notes-{collect,publish}.sh`, `scripts/tests/release-notes.bats`, `.claude/commands/*.md` (8) | Removed in cutover |

---

## Phase 1: Scaffold the plugin

### Task 1: Plugin manifest + directory skeleton

**Files:**

- Create: `.claude/skills/holomush-dev/.claude-plugin/plugin.json`
- Create: `.claude/skills/holomush-dev/scripts/` (dir), `.claude/skills/holomush-dev/skills/` (dir)

- [ ] **Step 1: Create the manifest**

`.claude/skills/holomush-dev/.claude-plugin/plugin.json`:

```json
{
  "name": "holomush-dev",
  "description": "HoloMUSH in-repo dev tooling: release notes, review gates, pr-prep, workspace + bead helpers.",
  "version": "0.1.0"
}
```

- [ ] **Step 2: Create the component directories** (so later tasks land in existing dirs)

Run: `mkdir -p .claude/skills/holomush-dev/skills .claude/skills/holomush-dev/scripts/tests`
Expected: directories exist.

- [ ] **Step 3: Verify the plugin loads**

Run (from repo root): `claude` then `/reload-plugins`, then `/plugin` (or check `/help`). At this point the manifest has no skills yet, so loading must not error.
Expected: `holomush-dev@skills-dir` appears with no error. (If running headless, skip the interactive check; the manifest is valid JSON — confirm with `python3 -c 'import json,sys; json.load(open(".claude/skills/holomush-dev/.claude-plugin/plugin.json"))'`, expected no output / exit 0.)

- [ ] **Step 4: Commit**

Per `references/vcs-preamble.md` (jj): `jj commit -m "feat(plugin): scaffold holomush-dev @skills-dir plugin manifest (holomush-q55eu)"`

---

## Phase 2: Port release-notes scripts to Python (TDD)

### Task 2: Plugin-local toolchain config + collect.py (TDD)

**Files:**

- Create: `.claude/skills/holomush-dev/scripts/pyproject.toml`
- Create: `.claude/skills/holomush-dev/scripts/tests/test_release_notes.py` (collect cases)
- Create: `.claude/skills/holomush-dev/scripts/release_notes_collect.py`

- [ ] **Step 1: Create the plugin-local pyproject.toml**

`.claude/skills/holomush-dev/scripts/pyproject.toml`:

```toml
[project]
name = "holomush-dev-scripts"
version = "0.1.0"
requires-python = ">=3.12"
dependencies = []

[dependency-groups]
dev = [
  "pytest>=8.0",
  "ruff>=0.9",
]

[tool.pytest.ini_options]
testpaths = ["tests"]

[tool.ruff]
line-length = 100
target-version = "py312"

[tool.ruff.lint]
select = ["E", "F", "W", "I", "UP"]
ignore = ["E501"]
```

- [ ] **Step 2: Write the failing collect tests**

`.claude/skills/holomush-dev/scripts/tests/test_release_notes.py`:

```python
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
"""Pytest port of scripts/tests/release-notes.bats (collect + publish)."""

import os
import subprocess
import sys
from pathlib import Path

import pytest

SCRIPTS = Path(__file__).resolve().parent.parent
COLLECT = SCRIPTS / "release_notes_collect.py"
PUBLISH = SCRIPTS / "release_notes_publish.py"


def _git(cwd: Path, *args: str) -> None:
    subprocess.run(["git", *args], cwd=cwd, check=True,
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


@pytest.fixture
def repo(tmp_path: Path) -> Path:
    """A hermetic git repo with v0.1.0/v0.2.0/v0.3.0 and known subjects."""
    r = tmp_path / "fix"
    r.mkdir()
    _git(r, "init", "-q", "-b", "main")
    _git(r, "config", "user.email", "t@example.com")
    _git(r, "config", "user.name", "Test")
    _git(r, "config", "commit.gpgsign", "false")
    _git(r, "config", "tag.gpgsign", "false")
    (r / "seed.txt").write_text("seed\n")
    _git(r, "add", "-A")
    _git(r, "commit", "-q", "-m", "chore: seed")
    _git(r, "tag", "v0.1.0")
    for msg in [
        "feat(scenes): web settings (holomush-5rh.24)",
        "fix(focus): unify delta coordinator (holomush-66228)",
        "docs: mark SP1 landed",
        "feat(session): liveness leases",
        "feat(crypto): scene DEK genesis (holomush-5rh.8.29.13)",
        "docs(scenes): settings actions plan",
    ]:
        _git(r, "commit", "-q", "--allow-empty", "-m", msg)
    _git(r, "tag", "v0.2.0")
    _git(r, "commit", "-q", "--allow-empty", "-m", "feat(telnet): keepalive pings")
    _git(r, "commit", "-q", "--allow-empty", "-m", "fix(web): reconnect backoff")
    _git(r, "tag", "v0.3.0")
    return r


def run_collect(repo: Path, tag: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        [sys.executable, str(COLLECT), tag],
        cwd=repo, capture_output=True, text=True,
    )


def test_resolves_previous_tag(repo):
    p = run_collect(repo, "v0.2.0")
    assert p.returncode == 0
    assert "Range: v0.1.0..v0.2.0" in p.stdout


def test_lists_filtered_commits_excludes_docs_test_chore(repo):
    p = run_collect(repo, "v0.2.0")
    assert p.returncode == 0
    assert "feat(scenes): web settings (holomush-5rh.24)" in p.stdout
    assert "feat(session): liveness leases" in p.stdout
    assert "docs: mark SP1 landed" not in p.stdout


def test_harvests_distinct_bead_refs(repo):
    p = run_collect(repo, "v0.2.0")
    assert "holomush-5rh.24" in p.stdout
    assert "holomush-66228" in p.stdout


def test_harvests_multilevel_bead_id_without_truncation(repo):
    p = run_collect(repo, "v0.2.0")
    assert p.returncode == 0
    assert "- holomush-5rh.8.29.13" in p.stdout


def test_keeps_scoped_docs_commits(repo):
    p = run_collect(repo, "v0.2.0")
    assert p.returncode == 0
    assert "docs(scenes): settings actions plan" in p.stdout


def test_reports_coverage_gaps(repo):
    p = run_collect(repo, "v0.2.0")
    assert "## Coverage gaps (no bead ref)" in p.stdout
    assert "feat(session): liveness leases" in p.stdout


def test_emits_all_sections_when_no_bead_refs(repo):
    p = run_collect(repo, "v0.3.0")
    assert p.returncode == 0
    assert "## Coverage gaps (no bead ref)" in p.stdout
    assert "## Roadmap theme sections" in p.stdout
    assert "feat(telnet): keepalive pings" in p.stdout


def test_fails_cleanly_on_missing_tag(repo):
    p = run_collect(repo, "v9.9.9")
    assert p.returncode == 1
    assert "could not resolve a previous tag" in (p.stdout + p.stderr)
```

- [ ] **Step 3: Run the collect tests to verify they fail**

Run: `cd .claude/skills/holomush-dev/scripts && uv run --group dev pytest tests/ -v -k collect or resolves or harvests or filtered or scoped or coverage or sections or missing`
(Or simply `uv run --group dev pytest tests/test_release_notes.py -v`.)
Expected: FAIL/ERROR — `release_notes_collect.py` does not exist yet.

- [ ] **Step 4: Implement collect.py**

`.claude/skills/holomush-dev/scripts/release_notes_collect.py`:

```python
#!/usr/bin/env python3
# /// script
# requires-python = ">=3.12"
# dependencies = []
# ///
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
"""release_notes_collect.py <tag> — print a structured context block for the
/holomush-dev:release-notes workflow. Deterministic data-gathering only; the
in-session model turns this block into narrative prose. No prose is written
here. Faithful port of the former scripts/release-notes-collect.sh.
"""

import re
import shutil
import subprocess
import sys

# GoReleaser-equivalent exclude filters (.goreleaser.yaml changelog block).
# ^docs: is anchored and does NOT match scoped docs(scope): commits — same as
# GoReleaser. Do not "improve" this; the filtered section must mirror the
# mechanical list it cross-checks.
EXCLUDE = re.compile(r"^docs:|^test:|^chore:|Merge pull request|Merge branch")
BEAD = re.compile(r"holomush-[a-z0-9]+(?:\.[0-9]+)*")


def _run(*args: str) -> subprocess.CompletedProcess:
    return subprocess.run(args, capture_output=True, text=True)


def main() -> int:
    if len(sys.argv) < 2 or not sys.argv[1]:
        print("usage: release_notes_collect.py <vX.Y.Z>", file=sys.stderr)
        return 2
    tag = sys.argv[1]

    # Previous tag = the semver tag immediately before <tag> in version order.
    tags = _run("git", "tag", "--list", "v[0-9]*.[0-9]*.[0-9]*",
                "--sort=-v:refname").stdout.splitlines()
    prev = ""
    for i, t in enumerate(tags):
        if t == tag:
            prev = tags[i + 1] if i + 1 < len(tags) else tag
            break
    if not prev or prev == tag:
        print(f"::error:: could not resolve a previous tag before {tag}",
              file=sys.stderr)
        return 1

    out: list[str] = []
    out.append(f"# Release context for {tag}")
    out.append("")
    out.append(f"Range: {prev}..{tag}")
    out.append("")

    subjects = _run("git", "log", "--no-merges", "--pretty=%s",
                    f"{prev}..{tag}").stdout.splitlines()

    out.append("## Filtered commits (mechanical set)")
    out.append("")
    for s in subjects:
        if not EXCLUDE.search(s):
            out.append(f"- {s}")
    out.append("")

    out.append("## Referenced beads")
    out.append("")
    ids: set[str] = set()
    for s in subjects:
        if EXCLUDE.search(s):
            continue
        ids.update(BEAD.findall(s))
    have_bd = shutil.which("bd") is not None
    for bead_id in sorted(ids):
        line = bead_id
        if have_bd:
            # Best-effort enrichment; degrade to the bare id on any failure
            # (e.g. no .beads in cwd), mirroring the bash ${line:-$id} fallback.
            res = _run("bd", "show", bead_id, "--json")
            if res.returncode == 0 and res.stdout.strip():
                line = _enrich(res.stdout) or bead_id
        out.append(f"- {line}")
    out.append("")

    out.append("## Coverage gaps (no bead ref)")
    out.append("")
    for s in subjects:
        if EXCLUDE.search(s):
            continue
        if not re.search(r"holomush-[a-z0-9]+", s):
            out.append(f"- {s}")
    out.append("")

    out.append("## Roadmap theme sections")
    out.append("")
    out.append("Consult docs/roadmap.md for theme:* sections; the model maps "
               "referenced")
    out.append("beads' theme labels to the relevant narrative headings.")

    print("\n".join(out))
    return 0


def _enrich(bd_json: str) -> str:
    """Format a bd show --json payload as '<id> [<type>] <title> labels=...'.

    Returns "" on any parse failure so the caller falls back to the bare id.
    """
    import json
    try:
        data = json.loads(bd_json)
        rec = data[0] if isinstance(data, list) else data
        labels = ",".join(rec.get("labels") or [])
        return f"{rec['id']} [{rec.get('type')}] {rec.get('title')} labels={labels}"
    except Exception:
        return ""


if __name__ == "__main__":
    sys.exit(main())
```

- [ ] **Step 5: Run the collect tests to verify they pass**

Run: `cd .claude/skills/holomush-dev/scripts && uv run --group dev pytest tests/test_release_notes.py -v`
Expected: the 8 collect tests PASS. (publish tests in Task 3 are not yet present.)

- [ ] **Step 6: Commit**

`jj commit -m "feat(plugin): port release-notes collector to PEP 723 Python (holomush-q55eu)"`

### Task 3: publish.py (TDD)

**Files:**

- Modify: `.claude/skills/holomush-dev/scripts/tests/test_release_notes.py` (append publish cases)
- Create: `.claude/skills/holomush-dev/scripts/release_notes_publish.py`

- [ ] **Step 1: Append the failing publish tests**

Append to `.claude/skills/holomush-dev/scripts/tests/test_release_notes.py`:

```python
def _write_gh_stub(bin_dir: Path) -> None:
    """Write a fake `gh`: prints $GH_BODY for 'release view'; copies the
    --notes-file contents to $GH_SENTINEL for 'release edit'. Both are read
    from the environment (set per-call in run_publish's env)."""
    bin_dir.mkdir(parents=True, exist_ok=True)
    gh = bin_dir / "gh"
    gh.write_text(
        "#!/usr/bin/env bash\n"
        'if [ "$1 $2" = "release view" ]; then printf %s "$GH_BODY"; exit 0; fi\n'
        'if [ "$1 $2" = "release edit" ]; then\n'
        '  while [ $# -gt 0 ]; do [ "$1" = "--notes-file" ] && cp "$2" "$GH_SENTINEL"; shift; done\n'
        "  exit 0\n"
        "fi\nexit 0\n"
    )
    gh.chmod(0o755)


def run_publish(env_extra: dict, bin_dir: Path, tag: str, narr: Path):
    env = {**os.environ, "PATH": f"{bin_dir}:{os.environ['PATH']}", **env_extra}
    return subprocess.run(
        [sys.executable, str(PUBLISH), "--tag", tag, "--narrative-file", str(narr)],
        capture_output=True, text=True, env=env,
    )


def test_publish_refuses_empty_narrative(tmp_path):
    narr = tmp_path / "narr.md"
    narr.write_text("")
    p = subprocess.run(
        [sys.executable, str(PUBLISH), "--tag", "v0.2.0", "--narrative-file", str(narr)],
        capture_output=True, text=True,
    )
    assert p.returncode != 0
    assert "narrative file is empty" in (p.stdout + p.stderr)


def test_publish_combines_narrative_above_existing(tmp_path):
    bin_dir = tmp_path / "bin"
    sentinel = tmp_path / "published.md"
    _write_gh_stub(bin_dir)
    narr = tmp_path / "narr.md"
    narr.write_text("## What changed\nNarrative TLDR here.\n")
    p = run_publish(
        {"GH_BODY": "## Changelog\n- feat: existing (#1)\n", "GH_SENTINEL": str(sentinel)},
        bin_dir, "v0.2.0", narr,
    )
    assert p.returncode == 0
    published = sentinel.read_text()
    assert "Narrative TLDR here." in published
    assert "feat: existing (#1)" in published
    # INV-7: narrative MUST be ABOVE the GoReleaser list.
    assert published.index("Narrative TLDR here.") < published.index("feat: existing (#1)")


def test_publish_fails_closed_on_empty_existing_body(tmp_path):
    bin_dir = tmp_path / "bin"
    sentinel = tmp_path / "published.md"
    _write_gh_stub(bin_dir)
    narr = tmp_path / "narr.md"
    narr.write_text("## What changed\nNarrative TLDR here.\n")
    p = run_publish(
        {"GH_BODY": "", "GH_SENTINEL": str(sentinel)},
        bin_dir, "v0.2.0", narr,
    )
    assert p.returncode != 0
    assert "existing release body for v0.2.0 is empty" in (p.stdout + p.stderr)
    assert not sentinel.exists()  # release edit MUST NOT have run
```

- [ ] **Step 2: Run publish tests to verify they fail**

Run: `cd .claude/skills/holomush-dev/scripts && uv run --group dev pytest tests/test_release_notes.py -v -k publish`
Expected: FAIL/ERROR — `release_notes_publish.py` does not exist.

- [ ] **Step 3: Implement publish.py**

`.claude/skills/holomush-dev/scripts/release_notes_publish.py`:

```python
#!/usr/bin/env python3
# /// script
# requires-python = ">=3.12"
# dependencies = []
# ///
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
"""release_notes_publish.py --tag <vX.Y.Z> --narrative-file <path>

Publishes the narrative ABOVE the existing GoReleaser release body. Because
`gh release edit --notes-file` REPLACES the body, this fetches the current body
first and combines (narrative + separator + existing). It MUST NOT publish a
narrative-only body — that would drop the mechanical commit list and violate
jfb9x INV-7. Faithful port of the former scripts/release-notes-publish.sh.
"""

import argparse
import subprocess
import sys
import tempfile
from pathlib import Path


def main() -> int:
    ap = argparse.ArgumentParser(add_help=False)
    ap.add_argument("--tag")
    ap.add_argument("--narrative-file")
    try:
        args, unknown = ap.parse_known_args()
    except SystemExit:
        return 2
    if unknown:
        print(f"::error:: unknown arg: {unknown[0]}", file=sys.stderr)
        return 2
    if not args.tag:
        print("::error:: --tag is required", file=sys.stderr)
        return 2
    if not args.narrative_file:
        print("::error:: --narrative-file is required", file=sys.stderr)
        return 2

    narr = Path(args.narrative_file)
    if not narr.is_file() or narr.stat().st_size == 0:
        print("::error:: narrative file is empty; refusing to publish "
              "(would drop the GoReleaser list)", file=sys.stderr)
        return 1

    view = subprocess.run(
        ["gh", "release", "view", args.tag, "-R", "holomush/holomush",
         "--json", "body", "-q", ".body"],
        capture_output=True, text=True,
    )
    if view.returncode != 0:
        err = " ".join(view.stderr.split())
        print(f"::error:: failed to fetch release {args.tag}: {err}", file=sys.stderr)
        return 1
    existing = view.stdout
    if not existing.strip():
        print(f"::error:: existing release body for {args.tag} is empty — "
              "refusing to publish narrative-only (GoReleaser notes missing; "
              "jfb9x INV-7). Run GoReleaser first or check the tag.",
              file=sys.stderr)
        return 1

    combined = narr.read_text() + "\n\n---\n\n" + existing.rstrip("\n") + "\n"
    with tempfile.NamedTemporaryFile("w", suffix=".md", delete=False) as f:
        f.write(combined)
        combined_path = f.name
    try:
        edit = subprocess.run(
            ["gh", "release", "edit", args.tag, "-R", "holomush/holomush",
             "--notes-file", combined_path],
            capture_output=True, text=True,
        )
        if edit.returncode != 0:
            err = " ".join(edit.stderr.split())
            print(f"::error:: failed to edit release {args.tag}: {err}", file=sys.stderr)
            return 1
    finally:
        Path(combined_path).unlink(missing_ok=True)

    print(f"Published combined release notes for {args.tag}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
```

- [ ] **Step 4: Run all tests to verify they pass**

Run: `cd .claude/skills/holomush-dev/scripts && uv run --group dev pytest tests/test_release_notes.py -v`
Expected: all 11 tests PASS (8 collect + 3 publish).

- [ ] **Step 5: Lint**

Run: `cd .claude/skills/holomush-dev/scripts && uv run --group dev ruff check . && uv run --group dev ruff format --check .`
Expected: clean (fix with `ruff format .` if needed and re-run).

- [ ] **Step 6: Commit**

`jj commit -m "feat(plugin): port release-notes publisher to PEP 723 Python (holomush-q55eu)"`

### Task 4: Remove bash + bats, wire CI + Taskfile

**Files:**

- Delete: `scripts/release-notes-collect.sh`, `scripts/release-notes-publish.sh`, `scripts/tests/release-notes.bats`
- Modify: `.github/workflows/scripts-tests.yaml`
- Modify: `Taskfile.yaml:1186-1189` (`release:notes:collect`)

- [ ] **Step 1: Delete the bash scripts and bats test**

Run: `git rm scripts/release-notes-collect.sh scripts/release-notes-publish.sh scripts/tests/release-notes.bats`
Expected: three files removed.

- [ ] **Step 2: Add the CI trigger path + a plugin-tests job**

In `.github/workflows/scripts-tests.yaml`, add to the `on.pull_request.paths` list:

```yaml
      - ".claude/skills/holomush-dev/scripts/**"
```

And add a new job (sibling of `test:`):

```yaml
  plugin-scripts:
    name: Plugin scripts tests (holomush-dev)
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6
      - name: Install uv
        uses: astral-sh/setup-uv@fac544c07dec837d0ccb6301d7b5580bf5edae39 # v8.2.0
        with:
          version: "0.11.24"
      - name: Run pytest
        working-directory: .claude/skills/holomush-dev/scripts
        run: uv run --group dev pytest tests/ -v
      - name: Lint with ruff
        working-directory: .claude/skills/holomush-dev/scripts
        run: |-
          uv run --group dev ruff check .
          uv run --group dev ruff format --check .
```

- [ ] **Step 3: Repoint the Taskfile collect target**

Find the `release:notes:collect` task in `Taskfile.yaml` (`rg -n 'release:notes:collect' Taskfile.yaml`). Replace its `cmds` invocation of the bash script with the thin `uv run` wrapper:

```yaml
      - uv run "{{.ROOT_DIR}}/.claude/skills/holomush-dev/scripts/release_notes_collect.py" {{.CLI_ARGS}}
```

(Use the Taskfile's existing root-dir variable; if it is named differently, match the file's convention. Do not add a `publish` task — publish stays plugin-only.)

- [ ] **Step 4: Verify bats no longer references the deleted file and pytest is green**

Run: `task test:bats` — Expected: PASS, no `release-notes.bats` (it's gone).
Run: `cd .claude/skills/holomush-dev/scripts && uv run --group dev pytest tests/ -v` — Expected: 11 PASS.
Run: `task release:notes:collect -- v0.10.0` from a checkout that has tags — Expected: prints the context block (parity with the old bash output).

- [ ] **Step 5: Commit**

`jj commit -m "build(plugin): wire plugin pytest into CI, repoint Taskfile, remove bash+bats (holomush-q55eu)"`

---

## Phase 3: Plugin skills

### Task 5: release-notes skill

**Files:**

- Create: `.claude/skills/holomush-dev/skills/release-notes/SKILL.md`

- [ ] **Step 1: Write the skill**

`.claude/skills/holomush-dev/skills/release-notes/SKILL.md` (body adapted from the old `.claude/commands/release-notes.md`; script calls use `${CLAUDE_PLUGIN_ROOT}` + `uv run`):

```markdown
---
name: release-notes
description: Draft and publish a narrative TLDR for a release (GitHub Release body + docs site). Invoke as /holomush-dev:release-notes <vX.Y.Z>.
disable-model-invocation: true
---

Produce human-readable narrative release notes for tag **$ARGUMENTS** (a
`vX.Y.Z` tag). The narrative augments — never replaces — GoReleaser's
mechanical commit list. Do NOT create an in-repo CHANGELOG.md (ADR
holomush-jfb9x).

**Steps:**

1. **Gather context.** Run
   `uv run "${CLAUDE_PLUGIN_ROOT}/scripts/release_notes_collect.py" $ARGUMENTS`
   (or `task release:notes:collect -- $ARGUMENTS`). Read the structured block:
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
   `uv run "${CLAUDE_PLUGIN_ROOT}/scripts/release_notes_publish.py" --tag $ARGUMENTS --narrative-file <temp>`.
   The script fetches the existing GoReleaser body and combines; never pass a
   narrative-only body.

5. **Emit the site post** as a SEPARATE post-tag docs change (feature branch →
   PR): write `site/src/content/docs/releases/$ARGUMENTS.md` (same narrative
   body) and add a reverse-chronological link to
   `site/src/content/docs/releases/index.mdx`. Confirm the `releases` topic is
   registered in `site/astro.config.mjs` or the page orphans silently. The
   frontmatter MUST set `slug: releases/$ARGUMENTS` — a `vX.Y.Z` filename would
   otherwise be slugified to a dot-stripped URL (`v0.10.0` → `/releases/v0100/`),
   breaking the index link and the docs-IA parity gate.
```

- [ ] **Step 2: Verify it resolves**

Run: `claude` → `/reload-plugins` → confirm `/holomush-dev:release-notes` is listed.
Expected: skill present under the `holomush-dev` namespace.

- [ ] **Step 3: Commit**

`jj commit -m "feat(plugin): release-notes skill in holomush-dev (holomush-q55eu)"`

### Task 6: Migrate the seven prompt-skills + delete loose commands

**Files (create each `SKILL.md`, delete each `commands/*.md`):**

- `audit-beads`, `landing-sequence`, `pr-prep`, `review-abac`, `review-code`, `review-crypto`, `spawn-workspace`

For each command `<name>`, the SKILL.md **body is the verbatim body** of the existing `.claude/commands/<name>.md` (everything after its frontmatter), with the frontmatter replaced and the cross-references namespaced (Step 2 below). Do not rewrite the prose.

- [ ] **Step 1: Create each skill directory + SKILL.md with new frontmatter**

For each `<name>`, create `.claude/skills/holomush-dev/skills/<name>/SKILL.md`. Frontmatter per skill (keep each command's existing `description:` text verbatim; set `name:` and the `disable-model-invocation` value from this table):

| name | disable-model-invocation |
| --- | --- |
| audit-beads | `true` |
| landing-sequence | `true` |
| pr-prep | `true` |
| spawn-workspace | `true` |
| review-abac | *(omit the line)* |
| review-code | *(omit the line)* |
| review-crypto | *(omit the line)* |

Frontmatter shape (example, `pr-prep`):

```markdown
---
name: pr-prep
description: Run the appropriate pr-prep lane (fast by default; full for int+e2e) and surface the first failure clearly
disable-model-invocation: true
---
```

Frontmatter shape (example, `review-code` — auto-invocable, no disable line):

```markdown
---
name: review-code
description: Adversarially review uncommitted or branch-local code before push
---
```

Copy each body verbatim, e.g.: `tail -n +<N> .claude/commands/<name>.md` (where `<N>` is the line after the closing `---`) appended below the new frontmatter.

- [ ] **Step 2: Namespace the inter-skill cross-references**

In the new `review-abac` and `review-crypto` SKILL.md bodies, the closing line says "invoke `/review-code`". Change to `/holomush-dev:review-code`:

- `review-abac` body: `… then invoke /review-code …` → `/holomush-dev:review-code`
- `review-crypto` body: `… then invoke /review-code …` → `/holomush-dev:review-code`

Leave all `@agent-*` references unchanged (those agents live in `.claude/agents/` and are not migrated). `task …` invocations are unchanged.

- [ ] **Step 3: Delete the eight loose commands**

Run: `git rm .claude/commands/release-notes.md .claude/commands/audit-beads.md .claude/commands/landing-sequence.md .claude/commands/pr-prep.md .claude/commands/review-abac.md .claude/commands/review-code.md .claude/commands/review-crypto.md .claude/commands/spawn-workspace.md`
Expected: eight files removed.

- [ ] **Step 4: Verify all eight skills resolve and the bare commands are gone**

Run: `claude` → `/reload-plugins` → confirm `/holomush-dev:{release-notes,audit-beads,landing-sequence,pr-prep,review-abac,review-code,review-crypto,spawn-workspace}` all resolve and the bare `/pr-prep` etc. no longer exist.
Expected: 8 namespaced skills present; no loose commands.

- [ ] **Step 5: Commit**

`jj commit -m "feat(plugin): migrate 7 prompt-skills into holomush-dev, remove loose commands (holomush-q55eu)"`

---

## Phase 4: Live references + final verification

### Task 7: Update live namespace references

**Files:**

- Modify: `CLAUDE.md`
- Modify: `.claude/agents/code-reviewer.md:132`
- Modify: `.claude/hooks/remind-pre-action-review.sh`

- [ ] **Step 1: CLAUDE.md — Pre-Push Review Gates table**

In the gate table (around lines 184–186), update the Invocation column:

- `/review-code [<target>]` → `/holomush-dev:review-code [<target>]`
- `/review-crypto` → `/holomush-dev:review-crypto`
- `/review-abac` → `/holomush-dev:review-abac`

Then `rg -n '/release-notes|/pr-prep|/audit-beads|/landing-sequence|/spawn-workspace' CLAUDE.md` and namespace any **live** mention to `/holomush-dev:<name>`. **MUST NOT** change `/review-design` or `/review-plan` (those are `dev-flow` plugin commands, not migrated). Do not touch `task pr-prep` (the go-task target).

- [ ] **Step 2: Agent body**

`.claude/agents/code-reviewer.md:132` — change "If invoked via `/review-code` with an argument" → "If invoked via `/holomush-dev:review-code` with an argument". Then `rg -n '/review-abac|/review-code|/review-crypto|/pr-prep|/audit-beads|/landing-sequence|/spawn-workspace|/release-notes' .claude/agents/` and namespace any remaining live agent-body references.

- [ ] **Step 3: Hook reminder text**

`.claude/hooks/remind-pre-action-review.sh` — in the three reminder strings (lines ~47, ~60, ~70), change the bare `/review-code`, `/review-crypto`, `/review-abac` to `/holomush-dev:review-code`, `/holomush-dev:review-crypto`, `/holomush-dev:review-abac`. Leave the `Agent` / `subagent_type:` fallbacks unchanged.

- [ ] **Step 4: Verify no stale live references remain**

Run: `rg -n '/(release-notes|audit-beads|landing-sequence|pr-prep|review-abac|review-code|review-crypto|spawn-workspace)\b' CLAUDE.md .claude/agents .claude/hooks`
Expected: every hit is the namespaced `/holomush-dev:<name>` form (or `task pr-prep`); no bare migrated-command slash invocations.

- [ ] **Step 5: Commit**

`jj commit -m "docs(plugin): namespace live references to holomush-dev skills (holomush-q55eu)"`

### Task 8: Full verification + format

**Files:** none (verification + `task fmt` output)

- [ ] **Step 1: Format (applies SPDX headers, reflows tables)**

Run: `task fmt`
Expected: any header/format changes applied. Commit them (they are part of this change).

- [ ] **Step 2: Plugin load smoke test**

Run (from repo root): `claude` → `/reload-plugins` → `/holomush-dev:release-notes` resolves; run
`uv run "$(pwd)/.claude/skills/holomush-dev/scripts/release_notes_collect.py" v0.10.0` from a tagged checkout.
Expected: skill resolves; script prints the context block.

- [ ] **Step 3: Pre-PR gate**

Run: `task pr-prep` (sole command; read exit code).
Expected: `status=pass exit=0` — bats (no release-notes.bats), license, lint, fmt:check, unit, build all green.

- [ ] **Step 4: Commit any fmt output**

`jj commit -m "chore(plugin): task fmt output for holomush-dev migration (holomush-q55eu)"` (skip if the tree is already clean after Step 1's commit).
<!-- adr-capture: sha256=a427f5c21ef4f3ae; session=7900b2a3; ts=2026-06-30T00:49:22Z; adrs=holomush-jpn4w -->
