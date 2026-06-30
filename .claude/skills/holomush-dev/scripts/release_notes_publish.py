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
    if not narr.is_file() or not narr.read_text().strip():
        print(
            "::error:: narrative file is empty; refusing to publish "
            "(would drop the GoReleaser list)",
            file=sys.stderr,
        )
        return 1

    view = subprocess.run(
        [
            "gh",
            "release",
            "view",
            args.tag,
            "-R",
            "holomush/holomush",
            "--json",
            "body",
            "-q",
            ".body",
        ],
        capture_output=True,
        text=True,
    )
    if view.returncode != 0:
        err = " ".join(view.stderr.split())
        print(f"::error:: failed to fetch release {args.tag}: {err}", file=sys.stderr)
        return 1
    existing = view.stdout
    if not existing.strip() or existing.strip() == "null":
        print(
            f"::error:: existing release body for {args.tag} is empty — "
            "refusing to publish narrative-only (GoReleaser notes missing; "
            "jfb9x INV-7). Run GoReleaser first or check the tag.",
            file=sys.stderr,
        )
        return 1

    combined = narr.read_text().rstrip("\n") + "\n\n---\n\n" + existing.rstrip("\n") + "\n"
    combined_path = None
    try:
        with tempfile.NamedTemporaryFile("w", suffix=".md", delete=False) as f:
            f.write(combined)
            combined_path = f.name
        edit = subprocess.run(
            [
                "gh",
                "release",
                "edit",
                args.tag,
                "-R",
                "holomush/holomush",
                "--notes-file",
                combined_path,
            ],
            capture_output=True,
            text=True,
        )
        if edit.returncode != 0:
            err = " ".join(edit.stderr.split())
            print(f"::error:: failed to edit release {args.tag}: {err}", file=sys.stderr)
            return 1
    finally:
        if combined_path:
            Path(combined_path).unlink(missing_ok=True)

    print(f"Published combined release notes for {args.tag}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
