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
# Accepted release-tag shape (semver vX.Y.Z with optional pre-release/build).
# Validated at entry so only this constrained shape ever reaches a subprocess arg.
TAG_RE = re.compile(r"v[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?")


def _run(*args: str, timeout: float | None = None) -> subprocess.CompletedProcess:
    # shell=False (list form): args are passed directly to execve, never through a
    # shell, so command injection is not possible (shlex.quote is a no-op here).
    # The only argv-derived value, `tag`, is additionally validated against TAG_RE
    # in main() and is maintainer-supplied at release time, not untrusted input.
    # nosemgrep: python.lang.security.audit.dangerous-subprocess-use-tainted-env-args.dangerous-subprocess-use-tainted-env-args
    return subprocess.run(args, capture_output=True, text=True, encoding="utf-8", timeout=timeout)


def main() -> int:
    if len(sys.argv) < 2 or not sys.argv[1]:
        print("usage: release_notes_collect.py <vX.Y.Z>", file=sys.stderr)
        return 2
    tag = sys.argv[1]
    if not TAG_RE.fullmatch(tag):
        print(f"::error:: invalid tag {tag!r}; expected vX.Y.Z", file=sys.stderr)
        return 2

    # Previous tag = the semver tag immediately before <tag> in version order.
    tags = _run(
        "git", "tag", "--list", "v[0-9]*.[0-9]*.[0-9]*", "--sort=-v:refname"
    ).stdout.splitlines()
    prev = ""
    for i, t in enumerate(tags):
        if t == tag:
            prev = tags[i + 1] if i + 1 < len(tags) else tag
            break
    if not prev or prev == tag:
        print(f"::error:: could not resolve a previous tag before {tag}", file=sys.stderr)
        return 1

    out: list[str] = []
    out.append(f"# Release context for {tag}")
    out.append("")
    out.append(f"Range: {prev}..{tag}")
    out.append("")

    subjects = _run(
        "git", "log", "--no-merges", "--pretty=%s", f"{prev}..{tag}"
    ).stdout.splitlines()

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
            # (e.g. no .beads in cwd, OS error, or a blocked/locked bd that
            # times out), mirroring the bash ${line:-$id} fallback.
            try:
                res = _run("bd", "show", bead_id, "--json", timeout=10)
                if res.returncode == 0 and res.stdout.strip():
                    line = _enrich(res.stdout) or bead_id
            except Exception:
                pass
        out.append(f"- {line}")
    out.append("")

    out.append("## Coverage gaps (no bead ref)")
    out.append("")
    for s in subjects:
        if EXCLUDE.search(s):
            continue
        if not BEAD.search(s):
            out.append(f"- {s}")
    out.append("")

    out.append("## Roadmap theme sections")
    out.append("")
    out.append("Consult docs/roadmap.md for theme:* sections; the model maps referenced")
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
