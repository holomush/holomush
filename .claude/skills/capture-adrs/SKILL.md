---
name: capture-adrs
description: |
  Use when the user has finalized a spec or plan and wants to extract
  ADR-worthy decisions into `docs/adr/<adr-id>-<slug>.md` files.
  Triggered by `/capture-adrs <path>` or by the nudge-adr-capture
  hook's reminder. NOT for general ADR audit — use the adr-extractor
  agent directly for that.
---

# /capture-adrs

Extract ADR-worthy decisions from a finalized spec or plan, get
per-candidate user approval, and write ADR files under `docs/adr/`.
Stamp the spec with a content-hash marker when done.

> ADR ids keep the historical `holomush-<suffix>` shape (the format
> `adr-doctor.sh` validates) but are self-minted — the beads tracker
> that used to allocate them was retired 2026-07-09.

Full design: `docs/superpowers/specs/2026-05-13-adr-capture-skill-design.md`.

## When to invoke

- User says `/capture-adrs <path>` (explicit path)
- User says `/capture-adrs` (interactive; prompt for the spec)
- Hook nudged with a `system-reminder` (PostToolUse) — pass the file
  path mentioned in the nudge
- User says `/capture-adrs <path> --dry-run` (no writes) or `--re-run`
  (force re-scan even on matching SHA)

## Tool availability check

You MUST run this skill in the user's main session context, where
`AskUserQuestion` is available. If `AskUserQuestion` is NOT in scope,
fail fast: print "capture-adrs requires AskUserQuestion (main session
context); aborting" and exit. (Per INV-A19.)

## Step-by-step

### 1. Resolve spec path

If the user provided a path, validate it against the regex:

```text
^(.*/)?docs/(superpowers/)?(specs|plans)/.+\.md$
```

Reject paths outside (including any under `docs/adr/`). If no path
given, list recent edits in the four watched roots via:

```bash
git log -n 20 --pretty=format: --name-only -- docs/specs/ docs/plans/ docs/superpowers/specs/ docs/superpowers/plans/ 2>/dev/null | sort -u | head -20
```

Use `AskUserQuestion` with one option per recent file to pick.

### 2. Idempotency check

Read the file. Strip the trailing marker line if present (last line
matching `^<!-- adr-capture: .*-->$`). Compute SHA-256 of the
remainder; take first 16 hex chars.

Decide in this order (first match wins):

1. **Opt-out:** marker has `optout=true` AND `reason="..."` → abort
   with message naming the reason. `--re-run` does NOT override
   opt-out. Exit.
2. **Fresh:** marker has `sha256=<hex>` matching current AND no
   `--re-run` → print "Already captured." + listed ADR ids from
   `adrs=...`. Exit.
3. **Proceed:** marker missing, malformed, or SHA mismatched.

### 3. Heuristic pre-scan

Walk the spec; collect regions matching any of:

- Header `^#+\s+(Options Considered|Alternatives Considered|Decision|Rationale|Trade-?offs|Why not)`
- Inline `(rejected because|chose|chosen|instead of|in favor of|decided against|ruled out|settled on|landed on)`
- `Alternative [A-Z]:` blocks

Output: list of `{start_line, end_line, header}` tuples.

### 4. Resolve transcript path

Look for `$CLAUDE_TRANSCRIPT_PATH` env var; if absent, scan
`~/.claude/projects/$(pwd encoded)/<session-uuid>/` for the JSONL
whose session ID matches `$CLAUDE_SESSION_ID`. If nothing resolves,
use the literal string `"none"`; the agent will run in spec-text-only
mode.

### 5. Dispatch the adr-extractor agent

Use the `Agent` tool with `subagent_type: adr-extractor`. Prompt:

```text
SPEC: <absolute-path>
SPEC_HEURISTIC_REGIONS: [{start_line, end_line, header}, ...]
TRANSCRIPT: <absolute-path-or-"none">
TRANSCRIPT_WINDOW: "100-turns-before-spec-writes"
EXISTING_ADRS_DIR: docs/adr/
OUTPUT_LIMIT: 800

Return JSON per the schema in your system prompt. Do not prepend prose.
```

Parse the JSON. On parse failure, retry once with a stricter prompt
("Your previous response was not parseable JSON. Return ONLY the JSON
object."). On second failure, fall back to surfacing the heuristic
regions as one-line candidates (warn the user).

### 6. Per-candidate review loop

For each candidate in `result.candidates`:

Present `AskUserQuestion` with:

- `question`: `"ADR candidate <i>/<n>: <title>"`
- `header`: `"ADR <i>/<n>"` (truncate to 12 chars)
- `options`:
  - **Accept** — "Write this ADR"
  - **Skip** — "Drop this candidate (logged in report)"
  - **Edit** — "Refine fields before accepting"
  - **Show full context** — "Display spec excerpt + transcript quotes"

On **Edit**: ask which field (Title / Context / Decision / Rationale /
Alternatives / Consequences / supersedes), then free-text refinement,
then re-present.

On **Show full context**: print spec lines `start_line..end_line` and
`transcript_quotes`, then re-present.

Collect ALL accept/skip decisions BEFORE writing anything. (Per
INV-A1: skill MUST NOT write files until all triage is complete.)

### 7. Write phase

For each accepted candidate, in order:

1. Render the ADR markdown body following §"ADR format (unified)" in
   the spec — Context, Decision, Rationale, Alternatives Considered,
   Consequences, References — with the `**Date:**`, `**Status:**`,
   `**Deciders:**` header.
2. Mint the ADR id — `holomush-` + 5 random base36 chars, collision-checked
   against existing files:

   ```bash
   while :; do
     id="holomush-$(LC_ALL=C tr -dc 'a-z0-9' < /dev/urandom | head -c 5)"
     ls docs/adr/"$id"-* >/dev/null 2>&1 || break
   done
   ```

3. Compute slug: kebab-case of title, drop stop-words (a, an, the,
   for, of, to, in, on, with), cap 60 chars.
4. Prepend `**Decision:** <adr-id>` line below the `**Status:**` line
   in the body.
5. Write `docs/adr/<adr-id>-<slug>.md` with the full body.
6. If candidate has `supersedes: <existing-adr-id>`: rewrite the
   superseded file's `**Status:**` to `Superseded by <new-adr-id>`.

### 8. Regenerate `docs/adr/README.md`

Walk `docs/adr/`. For each non-stub file (`<adr-id>-<slug>.md`), parse
`**Date:**`, title, `**Status:**`. Sort by date desc. Rewrite the
index table between `## Index` and the next `##` header. Preserve the
migration map between `<!-- BEGIN MIGRATION MAP -->` and `<!-- END
MIGRATION MAP -->` sentinels.

### 9. Stamp the marker

Normalize the spec to end with `\n` (append if absent). Recompute SHA-
256 of normalized content (first 16 hex). Append:

```text
<!-- adr-capture: sha256=<hex>; session=<short>; ts=<RFC3339>; adrs=<id1>,<id2>,... -->
```

(`<short>` = first 8 chars of `$CLAUDE_SESSION_ID` or `cli`; `<ts>` =
`date -u +%Y-%m-%dT%H:%M:%SZ`.)

For zero accepted candidates, `adrs=` is empty but the marker is still
written.

### 10. Final report

Print:

```text
Captured <N> ADRs:
  - <adr-id> <title> → docs/adr/<adr-id>-<slug>.md
  ...

Spec marker written. Run `task fmt` then commit when ready.
```

Skill MUST NOT commit. User does that.

## Failure modes

- ADR file write fails: roll back any partial writes for that
  candidate; continue; report partial.
- Sub-agent JSON malformed twice: fall back to heuristic-only
  candidates with a warning.
- Spec modified mid-flow (e.g., user edits during review): abort
  before stamping; suggest re-running.

## Anti-patterns

- DO NOT commit. (INV-A2)
- DO NOT write files before all triage decisions are collected.
  (INV-A1)
- DO NOT overwrite opt-out markers. (INV-A10)
- DO NOT use a model floor below sonnet for the adr-extractor dispatch.
