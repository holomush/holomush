#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
"""
One-shot migration: convert docs/adr/NNNN-<slug>.md to
docs/adr/<bd-id>-<slug>.md, file bd decision records, write stubs at
legacy paths, regenerate README.

Usage:
    python3 scripts/adr-migrate.py            # dry-run (default)
    python3 scripts/adr-migrate.py --apply    # actually mutate state

See docs/superpowers/specs/2026-05-13-adr-capture-skill-design.md §4.
"""
import argparse
import re
import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
ADR_DIR = REPO_ROOT / "docs" / "adr"
LEGACY_PATTERN = re.compile(r"^(\d{4})-([a-z0-9-]+)\.md$")


@dataclass
class LegacyADR:
    path: Path
    number: int
    slug: str
    title: str = ""
    date: str = ""
    status: str = "Accepted"
    superseded_by_number: int | None = None
    context: str = ""
    decision: str = ""
    rationale: str = ""
    alternatives: str = ""
    consequences: str = ""
    references: str = ""
    # Filled in after bd create:
    bd_id: str = ""

    @property
    def new_filename(self) -> str:
        assert self.bd_id, "bd_id not yet allocated"
        return f"{self.bd_id}-{self.slug}.md"


def discover_legacy_adrs() -> list[LegacyADR]:
    """Find all NNNN-<slug>.md files in docs/adr/, sorted by number."""
    out = []
    for p in sorted(ADR_DIR.iterdir()):
        m = LEGACY_PATTERN.match(p.name)
        if not m:
            continue
        out.append(LegacyADR(path=p, number=int(m.group(1)), slug=m.group(2)))
    return out


def parse_legacy(adr: LegacyADR) -> None:
    """Populate fields by parsing the markdown file."""
    text = adr.path.read_text(encoding="utf-8")

    # Title: first H1; strip the "ADR NNNN: " prefix.
    m = re.search(r"^#\s+(.+?)\s*$", text, re.MULTILINE)
    assert m, f"{adr.path}: no H1 found"
    title = m.group(1)
    title = re.sub(r"^ADR\s+\d+:\s*", "", title).strip()
    adr.title = title

    # Date.
    m = re.search(r"^\*\*Date:\*\*\s+(\S+)\s*$", text, re.MULTILINE)
    assert m, f"{adr.path}: no Date header"
    adr.date = m.group(1)

    # Status (may include "Superseded by ADR-XXXX", "[ADR 0014](...)", or "0014").
    # Real-world forms seen in docs/adr/:
    #   **Status:** Superseded by [ADR 0014](0014-...md)   (file body)
    #   **Status:** Superseded by 0014                     (README)
    #   **Status:** Superseded by ADR-0014                 (some hand-written)
    # The regex accepts all three: optional `[`, optional `ADR` token with
    # optional `-` or space separator, leading zeros allowed, then digits.
    m = re.search(r"^\*\*Status:\*\*\s+(.+?)\s*$", text, re.MULTILINE)
    assert m, f"{adr.path}: no Status header"
    status_raw = m.group(1).strip()
    adr.status = status_raw
    sup = re.search(r"Superseded by\s+(?:\[)?(?:ADR[\s-]?)?0*(\d+)", status_raw)
    if sup:
        adr.superseded_by_number = int(sup.group(1))

    # Sections: split on H2 headers and capture content per section name.
    sections = split_h2_sections(text)
    adr.context = sections.get("Context", "")
    adr.decision = sections.get("Decision", "")
    adr.rationale = sections.get("Rationale", "")
    adr.alternatives = sections.get("Alternatives Considered", "")
    adr.consequences = sections.get("Consequences", "")
    adr.references = sections.get("References", "")

    # If current format (no Alternatives Considered H2 but Options Considered
    # nested under Context), lift it out.
    if not adr.alternatives:
        m = re.search(r"###\s+Options Considered\s*\n(.+?)(?=^##\s|\Z)",
                      adr.context, re.DOTALL | re.MULTILINE)
        if m:
            adr.alternatives = m.group(1).strip()
            adr.context = adr.context[:m.start()].rstrip()

    # Some ADRs use a top-level `## Options Considered` H2 instead of
    # `## Alternatives Considered` (ADRs 0005-0008).
    if not adr.alternatives:
        adr.alternatives = sections.get("Options Considered", "")

    # ADRs without any formal alternatives section (ADRs 0003, 0004).
    if not adr.alternatives:
        adr.alternatives = (
            "_No formal alternatives section in the original ADR. "
            "See Decision and Rationale sections for the chosen approach._"
        )

    # ADRs without a separate `## Rationale` section (ADRs 0005-0008 fold
    # rationale into the Decision prose).
    if not adr.rationale:
        adr.rationale = (
            "_See Decision section above. The original ADR did not have a "
            "separate Rationale section._"
        )

    # All required sections must be non-empty after the lift.
    for fld in ("title", "date", "context", "decision", "rationale", "alternatives", "consequences"):
        if not getattr(adr, fld):
            sys.stderr.write(
                f"WARN: {adr.path.name}: empty field {fld!r} after parse\n"
            )


def split_h2_sections(text: str) -> dict[str, str]:
    """Return {section_name: body} for each `## Section` block."""
    out: dict[str, str] = {}
    # Find each `## Name` header and the text up to the next `## ` or EOF.
    pattern = re.compile(r"^##\s+(.+?)\s*\n(.*?)(?=^##\s|\Z)",
                         re.DOTALL | re.MULTILINE)
    for m in pattern.finditer(text):
        out[m.group(1).strip()] = m.group(2).strip()
    return out


def slugify(title: str) -> str:
    """Kebab-case the title, drop stop-words, cap 60 chars."""
    s = title.lower()
    s = re.sub(r"[^\w\s-]", "", s)
    s = re.sub(r"\s+", "-", s).strip("-")
    stop = {"a", "an", "the", "for", "of", "to", "in", "on", "with"}
    parts = [p for p in s.split("-") if p and p not in stop]
    return "-".join(parts)[:60].rstrip("-")


def render_adr(adr: LegacyADR, bd_id: str) -> str:
    """Render the unified-format markdown body for the new ADR file."""
    return f"""<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# {adr.title}

**Date:** {adr.date}
**Status:** {adr.status}
**Decision:** {bd_id}
**Originally:** ADR {adr.number:04d}
**Deciders:** HoloMUSH Contributors

## Context

{adr.context}

## Decision

{adr.decision}

## Rationale

{adr.rationale}

## Alternatives Considered

{adr.alternatives}

## Consequences

{adr.consequences}

## References

{adr.references}
"""


def render_bd_description(adr: LegacyADR) -> str:
    """Render the body to feed `bd create -t decision --description`.

    Identical to the file body BUT omits the `**Decision:**` header line
    (the bd record IS the decision; cross-linking is one-way: file → bd).
    """
    return f"""## Context

{adr.context}

## Decision

{adr.decision}

## Rationale

{adr.rationale}

## Alternatives Considered

{adr.alternatives}

## Consequences

{adr.consequences}

## References

{adr.references}

Legacy ADR number: {adr.number:04d}
"""


def run(cmd: list[str], stdin: str | None = None, check: bool = True) -> str:
    """Run a subprocess; return stdout. Raise on non-zero unless check=False."""
    p = subprocess.run(
        cmd,
        input=stdin,
        capture_output=True,
        text=True,
        check=False,
    )
    if check and p.returncode != 0:
        sys.stderr.write(f"ERROR: {cmd!r} exited {p.returncode}\n")
        sys.stderr.write(p.stderr)
        raise SystemExit(p.returncode)
    return p.stdout


BD_ID_PATTERN = re.compile(r"Created issue: (\S+)")
# Self-test on module load: catches a future bd CLI stdout change at import
# time rather than mid-migration (when 17 records have already been written).
assert BD_ID_PATTERN.search(
    "✓ Created issue: holomush-xxxx — title"
), "BD_ID_PATTERN no longer matches bd create stdout; update the regex."


def bd_create_decision(title: str, description: str) -> str:
    """`bd create -t decision --validate`; return the new bd-id."""
    out = run([
        "bd", "create",
        "-t", "decision",
        "--validate",
        "--title", title,
        "--description", description,
    ])
    m = BD_ID_PATTERN.search(out)
    if not m:
        sys.stderr.write(f"ERROR: could not parse bd-id from:\n{out}\n")
        raise SystemExit(1)
    return m.group(1)


def bd_dep_supersedes(new_id: str, old_id: str) -> None:
    """Record a supersedes dep edge: new_id supersedes old_id."""
    run(["bd", "dep", "add", new_id, old_id, "--type", "supersedes"])


def bd_close_superseded(old_id: str, new_id: str) -> None:
    """Close the superseded record with a reason."""
    run(["bd", "close", old_id, "--reason", f"Superseded by {new_id}"])


def bd_dolt_commit(message: str) -> None:
    run(["bd", "dolt", "commit", "-m", message])


def stub_body(a: LegacyADR) -> str:
    return f"""<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Moved

This ADR has moved to **[`{a.new_filename}`]({a.new_filename})**.

- **bd decision:** `{a.bd_id}` (run `bd show {a.bd_id}` for live status)
- **Legacy number:** ADR {a.number:04d}
- **Date migrated:** 2026-05-14
"""


def regenerate_readme(adrs: list[LegacyADR]) -> None:
    """Rewrite docs/adr/README.md end-to-end."""
    by_date_desc = sorted(adrs, key=lambda a: a.date, reverse=True)
    index_rows = "\n".join(
        f"| [{a.title}]({a.new_filename}) | {a.date} | {a.status} | `{a.bd_id}` |"
        for a in by_date_desc
    )
    migration_rows = "\n".join(
        f"| ADR {a.number:04d} | `{a.bd_id}` | [{a.new_filename}]({a.new_filename}) |"
        for a in sorted(adrs, key=lambda a: a.number)
    )
    content = f"""<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Architecture Decision Records (ADRs)

This directory contains Architecture Decision Records (ADRs) documenting
significant design decisions made during HoloMUSH development. Each ADR
captures the context, options considered, decision made, and consequences
of architectural choices.

ADRs are immutable once accepted. If a decision is reversed, a new ADR
supersedes the old one; the bd decision record gains a `--type supersedes`
edge and the file's `**Status:**` reflects the supersession.

## Index

| Title | Date | Status | bd decision |
|-------|------|--------|-------------|
{index_rows}

<!-- BEGIN MIGRATION MAP -->

## Migration map (2026-05-14)

The legacy `NNNN-<slug>.md` numbering was retired in favor of
bd-decision IDs. Stubs at the old paths preserve external references.

| Legacy | bd decision | Current file |
|--------|-------------|--------------|
{migration_rows}

<!-- END MIGRATION MAP -->

## Format

See `docs/superpowers/specs/2026-05-13-adr-capture-skill-design.md`
§"ADR format (unified)" for the canonical template. All ADRs use one
format: Context, Decision, Rationale, Alternatives Considered,
Consequences, References.

## Template

New ADRs are written by the `/capture-adrs` skill, which renders from
the spec's format definition. To write one manually, follow the same
shape and use `bd create -t decision --validate` to file the record.

## Writing guidelines

| Guideline                 | Description                                                                                              |
| ------------------------- | -------------------------------------------------------------------------------------------------------- |
| **Immutability**          | ADRs are permanent records — do not edit accepted ADRs to change decisions                               |
| **Supersession**          | To reverse a decision, create a new ADR and mark the old one as "Superseded by `<bd-id>`"                |
| **RFC2119 keywords**      | Use MUST/SHOULD/MAY in consequences when describing implementation requirements                          |
| **Comprehensive options** | Document ALL options considered, not just the chosen one                                                 |
| **Trade-off clarity**     | Consequences should honestly capture both benefits and costs                                             |
| **Future-proof**          | Assume readers in 5 years won't have context — explain everything                                        |

## References

- [Michael Nygard's ADR template](https://github.com/joelparkerhenderson/architecture-decision-record)
- [ADR Tools GitHub](https://github.com/npryce/adr-tools)
- [RFC 2119: Key words for RFCs](https://www.ietf.org/rfc/rfc2119.txt)
"""
    (ADR_DIR / "README.md").write_text(content, encoding="utf-8")


def verify_post_migration(adrs: list[LegacyADR]) -> None:
    """Inline asserts enforcing INV-A12, A13."""
    real_files = sorted(
        p.name for p in ADR_DIR.iterdir()
        if re.match(r"^[a-z0-9]+-[a-z0-9-]+\.md$", p.name)
        and not re.match(r"^\d{4}-", p.name)
    )
    stubs = sorted(
        p.name for p in ADR_DIR.iterdir()
        if re.match(r"^\d{4}-.+\.md$", p.name)
    )
    assert len(real_files) == 17, f"expected 17 real files, got {len(real_files)}: {real_files}"
    assert len(stubs) == 17, f"expected 17 stubs, got {len(stubs)}: {stubs}"
    assert (ADR_DIR / "README.md").exists(), "README.md missing"
    assert not (ADR_DIR / "legacy").exists(), "legacy/ subdirectory MUST NOT exist (INV-A12 flat-stub rule)"

    # Each stub links to an existing real file.
    for stub_name in stubs:
        stub = ADR_DIR / stub_name
        text = stub.read_text(encoding="utf-8")
        m = re.search(r"\[`([a-z0-9-]+\.md)`\]", text)
        assert m, f"{stub_name}: no link in stub"
        target = m.group(1)
        assert (ADR_DIR / target).exists(), f"{stub_name} links to missing {target}"

    # Supersession edge present (ADR 0007 → 0014).
    sup = [a for a in adrs if a.superseded_by_number is not None]
    if sup:
        # Should be exactly 1 in the current backlog.
        assert len(sup) == 1, f"expected 1 supersession, got {len(sup)}"
        a = sup[0]
        superseder = next(s for s in adrs if s.number == a.superseded_by_number)
        # Cross-check the dep edge exists.
        deps = run(["bd", "dep", "list", superseder.bd_id])
        assert "supersedes" in deps and a.bd_id in deps, (
            f"supersession edge missing: {superseder.bd_id} should supersede {a.bd_id}\n{deps}"
        )

    print(f"\n✓ Post-migration verification passed: "
          f"{len(real_files)} real + {len(stubs)} stubs + README + "
          f"{len(sup)} supersession edges.")


def apply_migration(adrs: list[LegacyADR]) -> None:
    """Mutate state: create bd records, rename files, write stubs, regen README."""
    # Pass 1: create bd records for every ADR; populate bd_id.
    for a in adrs:
        a.slug = slugify(a.title) or a.slug
        body = render_bd_description(a)
        a.bd_id = bd_create_decision(a.title, body)
        print(f"  bd: {a.bd_id} ← ADR-{a.number:04d} ({a.title!r})")

    # Pass 2: build number→bd-id index for supersession edges.
    by_number = {a.number: a for a in adrs}

    # Pass 3: rename files + write stubs.
    # jj has no `mv` subcommand; the snapshotter auto-detects the rename
    # from a filesystem move, preserving `jj log --follow` continuity
    # (INV-A17).
    for a in adrs:
        new_path = ADR_DIR / a.new_filename
        body = render_adr(a, a.bd_id)
        # Filesystem rename — jj detects this when it next snapshots.
        a.path.rename(new_path)
        # Overwrite the renamed file with the unified-format body.
        new_path.write_text(body, encoding="utf-8")
        # Write stub at the legacy path (fresh file at the OLD name).
        a.path.write_text(stub_body(a), encoding="utf-8")
        print(f"  renamed: {a.path.name} → {a.new_filename}; stub written")

    # Pass 4: supersession edges + close.
    for a in adrs:
        if a.superseded_by_number is None:
            continue
        superseder = by_number.get(a.superseded_by_number)
        if superseder is None:
            sys.stderr.write(
                f"WARN: ADR-{a.number:04d} says superseded by "
                f"ADR-{a.superseded_by_number:04d} but that ADR is "
                f"not present.\n"
            )
            continue
        bd_dep_supersedes(superseder.bd_id, a.bd_id)
        bd_close_superseded(a.bd_id, superseder.bd_id)
        # Rewrite the superseded file's Status header to use bd-id.
        new_path = ADR_DIR / a.new_filename
        text = new_path.read_text(encoding="utf-8")
        text = re.sub(
            r"^\*\*Status:\*\*\s+.+$",
            f"**Status:** Superseded by {superseder.bd_id}",
            text,
            count=1,
            flags=re.MULTILINE,
        )
        new_path.write_text(text, encoding="utf-8")
        # Mirror the on-disk Status update into the in-memory model so
        # Pass 5's regenerate_readme() emits the new bd-id in the README's
        # Status column. (Without this, the README index would retain the
        # parsed legacy "Superseded by [ADR 0014](0014-...md)" markdown link
        # which then points at the legacy stub instead of the renamed file.)
        a.status = f"Superseded by {superseder.bd_id}"
        print(f"  supersession: {a.bd_id} ← superseded by {superseder.bd_id}")

    # Pass 5: regenerate README.
    regenerate_readme(adrs)

    # Pass 6: inline assertions (INV-A12, A13).
    # Run BEFORE bd_dolt_commit so a verify failure leaves the bd database
    # un-committed and the working copy abandonable via `jj abandon`.
    verify_post_migration(adrs)

    # Pass 7: bd dolt commit. Only reached if verification passed.
    bd_dolt_commit("migration: import 17 legacy ADRs as decision records")


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--apply", action="store_true",
                    help="Actually mutate state (default: dry-run)")
    args = ap.parse_args()

    adrs = discover_legacy_adrs()
    print(f"Found {len(adrs)} legacy ADR files in {ADR_DIR}.")
    for a in adrs:
        parse_legacy(a)
        a.slug = slugify(a.title) or a.slug  # prefer title-derived slug
        print(f"  ADR-{a.number:04d}  →  <bd-id>-{a.slug}.md  "
              f"({a.date}, {a.status})"
              + (f"  [supersededBy=ADR-{a.superseded_by_number:04d}]"
                 if a.superseded_by_number else ""))

    if not args.apply:
        print("\nDry-run complete. Re-run with --apply to mutate state.")
        return 0

    apply_migration(adrs)
    print("\nMigration apply phase 1 (bd create) complete.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
