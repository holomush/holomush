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
    subprocess.run(
        ["git", *args], cwd=cwd, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
    )


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
        cwd=repo,
        capture_output=True,
        text=True,
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


def test_fails_when_tag_is_oldest(repo):
    # v0.1.0 is the first tag in the fixture, so PREV resolves to itself.
    p = run_collect(repo, "v0.1.0")
    assert p.returncode == 1
    assert "could not resolve a previous tag" in (p.stdout + p.stderr)


def _write_gh_stub(bin_dir: Path) -> None:
    """Write a fake `gh`: prints $GH_BODY for 'release view'; copies the
    --notes-file contents to $GH_SENTINEL for 'release edit'. Both are read
    from the environment (set per-call in run_publish's env)."""
    bin_dir.mkdir(parents=True, exist_ok=True)
    gh = bin_dir / "gh"
    gh.write_text(
        "#!/usr/bin/env bash\n"
        'if [ -n "$GH_VIEW_FAIL" ] && [ "$1 $2" = "release view" ]; then echo "boom" >&2; exit 1; fi\n'
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
        capture_output=True,
        text=True,
        env=env,
    )


def test_publish_refuses_empty_narrative(tmp_path):
    narr = tmp_path / "narr.md"
    narr.write_text("")
    p = subprocess.run(
        [sys.executable, str(PUBLISH), "--tag", "v0.2.0", "--narrative-file", str(narr)],
        capture_output=True,
        text=True,
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
        bin_dir,
        "v0.2.0",
        narr,
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
        bin_dir,
        "v0.2.0",
        narr,
    )
    assert p.returncode != 0
    assert "existing release body for v0.2.0 is empty" in (p.stdout + p.stderr)
    assert not sentinel.exists()  # release edit MUST NOT have run


def test_publish_fails_when_release_view_errors(tmp_path):
    bin_dir = tmp_path / "bin"
    sentinel = tmp_path / "published.md"
    _write_gh_stub(bin_dir)
    narr = tmp_path / "narr.md"
    narr.write_text("## What changed\nNarrative TLDR here.\n")
    p = run_publish(
        {"GH_VIEW_FAIL": "1", "GH_SENTINEL": str(sentinel)},
        bin_dir,
        "v0.2.0",
        narr,
    )
    assert p.returncode != 0
    assert "failed to fetch release" in (p.stdout + p.stderr)
    assert not sentinel.exists()  # release edit MUST NOT have run


def test_collect_rejects_invalid_tag(repo):
    p = run_collect(repo, "not-a-tag")
    assert p.returncode == 2
    assert "invalid tag" in (p.stdout + p.stderr)


def test_publish_rejects_invalid_tag(tmp_path):
    # A tag carrying shell metacharacters must be refused at validation, well
    # before it could reach the gh subprocess argv.
    narr = tmp_path / "narr.md"
    narr.write_text("x\n")
    p = run_publish({}, tmp_path / "bin", "v1.0.0; rm -rf /", narr)
    assert p.returncode == 2
    assert "invalid tag" in (p.stdout + p.stderr)
