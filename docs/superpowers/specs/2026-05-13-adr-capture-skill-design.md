<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# ADR Capture Skill — Design Spec

**Date:** 2026-05-13
**Status:** Draft
**Bead:** `holomush-b2qy`
**Authors:** HoloMUSH Contributors

## Goal

Capture ADR-worthy decisions from finalized specs and plans before they are
lost to context compaction, by shipping:

1. A repo-local `/capture-adrs` skill that extracts candidate decisions from a
   spec/plan (and its brainstorming session transcript), gets per-candidate
   user approval, files `bd` decision records, writes ADR files under
   `docs/adr/`, and stamps the spec with a content-hash marker.
2. A `PostToolUse` hook that nudges Claude when a watched spec/plan path is
   written/edited and either has no capture marker or carries a stale one.
3. A repo-local `adr-extractor` agent that owns the ADR-worthiness judgment
   logic in its own context, callable by the skill or by future ADR-discovery
   workflows.
4. A one-shot migration that converts the existing 17 ADRs from the
   `NNNN-<slug>.md` numbering convention to a `<bd-id>-<slug>.md` convention
   in which the `bd` decision record is the canonical identifier. Migration
   ships in the same PR as the skill + hook + agent.

## Background and motivation

The repository already has a well-formed `docs/adr/` directory with a
17-record backlog, two valid markdown templates (original vs. current
format), and a README index. ADR 0017 is the most recent and demonstrates
the gold-standard shape: explicit `Decision`, `Alternatives Considered`,
`Rationale`, `Consequences`, and `References` sections.

Recent brainstorming sessions (the trigger for bead `holomush-b2qy`) have
surfaced multiple ADR-worthy decisions that did NOT become ADRs because
they were buried inside spec text and the conversation that produced the
spec. Examples called out on the bead:

- "Plugin tables mirror `events_audit` shape (not host-owned side table)"
- "Manifest-set heuristic + AEAD AAD-binding as two-layer downgrade fence"
- "Clean-break proto reshape, no compat shims"
- "Test fixture lives under `test/integration/plugin/testdata/`, not
  `plugins/` tree"
- "KeySelector wiring symmetric with hot-tier for future substrate work"
- "`plugin_integrity_violation` audit subject has no chain participation"

The bead's premise: these decisions are recoverable from conversation
transcript + spec text *at the moment the spec is finalized*, but become
hard to recover after context compaction or session end. A wrap-up workflow
that runs near the finalize point is the right shape.

`bd` already supports a `decision` issue type (with aliases `dec` and `adr`)
and enforces three required description sections via the `--validate` flag:
`## Decision`, `## Rationale`, `## Alternatives Considered`. This spec
adopts `bd create -t decision --validate` as the canonical write surface and
matches the repo ADR file format to it, so both representations render from
the same source text.

## Architecture overview

```text
              ┌────────────────────────────────────────────────────┐
              │                  /capture-adrs                     │
              │     (.claude/skills/capture-adrs/SKILL.md)         │
              └──────────────────────┬─────────────────────────────┘
                                     │ orchestrates
                                     │
   ┌─────────────────────────────────┼─────────────────────────────────┐
   │                                 │                                 │
   ▼                                 ▼                                 ▼
heuristic                    Agent dispatch                  per-candidate
pre-scan                     (subagent_type =                review loop
spec text                    "adr-extractor")                (AskUserQuestion)
                                     │                                 │
                                     ▼                                 ▼
                            JSON: candidates +              user accept / skip / edit
                            dropped regions                            │
                                                                       ▼
                                                          render markdown body
                                                                       │
                                                       ┌───────────────┴───────────────┐
                                                       ▼                               ▼
                                            bd create -t decision         write docs/adr/<bd-id>-<slug>.md
                                            --validate --description "$body"
                                                       │                               │
                                                       └──────────────┬────────────────┘
                                                                      │
                                                                      ▼
                                                          regenerate docs/adr/README.md
                                                                      │
                                                                      ▼
                                                          stamp spec with marker:
                                                          <!-- adr-capture: sha256=...; ... -->


                              ┌──────────────────────────────────────────┐
   Write|Edit event ────────► │   PostToolUse hook nudge-adr-capture.sh  │
   on watched spec path       │   (.claude/hooks/)                       │
                              └──────────────────────────────────────────┘
                                              │
                                              ▼
                                  read spec, strip marker,
                                  SHA(content) vs marker.sha
                                              │
                                  ┌───────────┴───────────┐
                          match (suppress)         mismatch / missing
                                                          │
                                                          ▼
                                                emit <system-reminder>
                                                "Run /capture-adrs <path>"
```

## Shared conventions

These two conventions are cross-cut by the skill, the hook, and
`adr-doctor.sh`. They are defined once here and referenced by name.

### Watched-path pattern

The skill MUST validate, and the hook MUST match, paths against this
single pattern (recursive — nested subdirectories under the watched
roots are included):

**Single source of truth (POSIX ERE):**

```text
^(.*/)?docs/(superpowers/)?(specs|plans)/.+\.md$
```

Both the skill (Go-like regex check or bash `[[ ... =~ ... ]]`) and the
hook MUST use this regex against the absolute `file_path`. Globstar
patterns (`**`) are forbidden — they require `shopt -s globstar` and
bash >= 4, and macOS ships with bash 3.2. The hook tests its match via:

```bash
if [[ "$file_path" =~ ^(.*/)?docs/(superpowers/)?(specs|plans)/.+\.md$ ]]; then
  # in scope
fi
```

This works on bash 3.2+ and is the canonical implementation across the
skill and hook.

`docs/adr/**` is explicitly NOT in scope and MUST NOT trigger the nudge
(verified by hook test fixture).

**Note on the leading `(.*/)?`:** the regex matches the four families
anywhere in the path, including inside jj workspace roots
(`.worktrees/<name>/docs/specs/...`) and worktree-isolated agent
contexts. This is intentional. A latent over-match exists for any
future path like `site/docs/specs/` (no such directory exists today,
verified). If that ever becomes a real path under `site/`, the regex
will need tightening; until then the looseness is the desired property
for workspace-rooted paths.

The hook's test harness MUST include a nested-subdirectory fixture (e.g.,
`docs/superpowers/specs/2026-05/foo.md`) that asserts the nudge fires,
AND a `docs/adr/foo.md` fixture that asserts it does not.

### Marker convention

The capture marker is a **single HTML-comment line, the LAST line of the
file**:

```html
<!-- adr-capture: sha256=<16-hex>; session=<short>; ts=<RFC3339>; adrs=<id1>,<id2>,... -->
```

Or, for opt-out:

```html
<!-- adr-capture: optout=true; reason="..." -->
```

**Parsing rule** (used by both the skill and the hook; the same byte-level
operation):

1. Read entire file bytes.
2. If the file ends with `\n`, drop the trailing `\n`.
3. Split off the final line. If it matches `^<!-- adr-capture: .*-->$`,
   that line is the marker; the remainder (including the trailing `\n`
   between the prior line and the marker line) is the "stripped content."
4. If the final line does not match, there is no marker; the full file
   (with original trailing `\n`) is the stripped content.

**SHA-256 input** is the byte sequence "stripped content" as defined
above, taken first 16 hex chars (`sha256sum | cut -c1-16`).

**Marker kind disambiguation** (applied after parse):

A parsed marker is classified as one of three kinds by inspecting its
key-value body:

| Kind | Required keys | Notes |
|------|---------------|-------|
| `sha` (capture marker) | `sha256=<16-hex>`, `session=...`, `ts=...`, `adrs=...` | The normal stamp written by the skill. `adrs` MAY be empty. |
| `optout` | `optout=true`, `reason="..."` | User-authored suppression. Has no `sha256=` field. |
| `malformed` | Anything else (prefix present but neither well-formed kind matches) | Treated as "capture marker, SHA mismatch" (i.e., nudge fires; skill re-stamps). |

The disambiguator is the presence of `optout=true`: if it's there with a
non-empty `reason`, the marker is `optout`. If it's missing and
`sha256=<16-hex>` is well-formed, the marker is `sha`. Anything else
falls through to `malformed`.

**Robustness rules:**

- A `<!-- adr-capture:` prefix appearing on a non-last line MUST be
  ignored (not stripped, not parsed). This prevents accidental false-
  stable SHA when the user pastes the prefix as documentation mid-file.
- A `malformed` marker is treated as "capture marker present but SHA
  mismatch" — nudge fires; skill re-stamps on next capture (replacing
  the malformed line).
- An `optout` marker is honored only if it is the LAST line AND parses
  as kind=`optout` per the table above. Mid-file or malformed opt-out
  comments are ignored.

<!-- rumdl-disable-next-line MD032 -->

### Stamping rule (skill side)

The skill stamps the marker by:

1. Ensuring the pre-stamp file content ends with `\n` (append one if
   absent). Call this `normalized_content`.
2. Computing SHA-256 of `normalized_content` (first 16 hex chars).
3. Writing `normalized_content + "<!-- adr-capture: sha256=<hex>; ... -->\n"`.

Both the skill and the hook compute the SHA over the same byte sequence
("stripped content" per the parse rule above), so the post-stamp SHA
the skill recorded matches what the hook computes on the next read.
Files without a pre-stamp trailing newline are made canonical at stamp
time; the round-trip is stable by construction.

## Components

### 1. `/capture-adrs` skill

**Path:** `.claude/skills/capture-adrs/SKILL.md`

The skill is an instructional prompt, not code. It tells Claude how to
render an ADR by following the format in §"ADR format (unified)". There is
no template engine, no template file shared with the migration. Both
renderers (Claude-following-skill and Python-following-spec) produce
conformant markdown because they both follow the format spec.

**Invocation forms:**

| Form | Behavior |
|------|----------|
| `/capture-adrs <spec-or-plan-path>` | Explicit path. Default. |
| `/capture-adrs` | Interactive: list recently-modified specs/plans in watched paths, prompt user to pick. |
| `/capture-adrs <path> --re-run` | Force re-scan even when marker SHA matches. Used after substantive revision the SHA-compare might consider trivial (rare). |
| `/capture-adrs <path> --dry-run` | Show candidates and the report; write nothing. |

**Steps (in order):**

1. **Resolve spec path.** MUST validate the path against the regex in
   §"Watched-path pattern". Reject paths outside (any non-spec/non-plan
   path, anything under `docs/adr/`).
2. **Idempotency check.** Read content, strip the trailing marker line
   per §"Marker convention", compute SHA-256 (first 16 hex chars). Parse
   any existing marker.

   Decide outcome in this order (first match wins):

   - Marker present + `optout=true` + well-formed (per §"Marker
     convention") → abort with a clear message naming the opt-out
     `reason` field. `--re-run` does NOT override opt-out; only the user
     can remove the marker manually. Exit 0.
   - Marker present + `sha256=` matches stripped content SHA + not
     `--re-run` → print "Already captured." + listed bd-ids. Exit 0.
   - Marker missing OR `sha256=` mismatch OR malformed → proceed to
     step 3.

3. **Heuristic pre-scan.** Walk the spec, identify candidate regions by
   header (`Options Considered`, `Alternatives Considered`, `Decision`,
   `Rationale`, `Trade-?offs`, `Why not ...`) and inline phrases (regex
   listed in §"Detection methodology"). Output `[(start_line, end_line,
   surrounding_header), ...]`.
4. **Resolve transcript.** Look for `$CLAUDE_SESSION_TRANSCRIPT_PATH`; fall
   back to scanning `~/.claude/projects/<encoded-cwd>/<session-uuid>/`
   for the JSONL whose session ID matches `$CLAUDE_SESSION_ID`. If neither
   resolves, set transcript path to `"none"` and print a soft warning.
5. **Agent dispatch.** Invoke `adr-extractor` via the Agent tool. Prompt
   includes: spec path, heuristic regions, transcript path, transcript
   window strategy, existing ADRs dir, output word cap.
6. **Parse candidate JSON.** On parse failure, retry once with a stricter
   prompt. On second failure, fall back to heuristic-only candidates and
   warn the user.
7. **Per-candidate review loop.** For each candidate, present
   `AskUserQuestion` with options: **Accept** / **Skip** / **Edit** /
   **Show full context**. Edit branch prompts for field selection, then
   free-text refinement, then re-presents. Show-full-context branch prints
   the spec excerpt + transcript quotes, then re-asks the same question.
   All triage decisions are collected before any write begins.
8. **Write phase.** For each accepted candidate:
   1. Render unified markdown body following the format in §"ADR format
      (unified)" — Claude does this directly per the skill's instructions;
      no external template engine.
   2. `bd create -t decision --validate --description "$body"` →
      `<bd-id>`. On validation failure, abort this candidate (others
      continue) and report.
   3. Prepend `**Decision:** <bd-id>` to body.
   4. Compute slug from title (kebab-case, drop stop-words, cap 60 chars).
   5. Write `docs/adr/<bd-id>-<slug>.md`.
   6. If candidate has `supersedes: <existing-bd-id>` set:
      - `bd dep add <new-bd-id> <existing-bd-id> --type supersedes` to record
        the edge.
      - `bd close <existing-bd-id> --reason "Superseded by <new-bd-id>"` to
        retire the old record.
      - Rewrite the superseded repo file's `**Status:**` header.
9. **Regenerate README.** Rebuild `docs/adr/README.md` index by scanning
   `docs/adr/*.md` (excluding stub files matched by `^[0-9]{4}-.*\.md$`),
   parsing metadata, sorting by date desc. Migration-map section is
   preserved between sentinel comments
   `<!-- BEGIN MIGRATION MAP --> ... <!-- END MIGRATION MAP -->`.
10. **Stamp marker.** Append `<!-- adr-capture: sha256=<16-hex>;
    session=<short>; ts=<RFC3339>; adrs=<id1>,<id2>,... -->` to the spec.
    For zero-candidate captures, `adrs=` is empty but marker is still
    written.
11. **Final report.** Summary listing captured ADRs and paths. Skill does
    not commit; user does.

**Failure modes:**

- bd write fails (e.g., dolt lock) → roll back any files written for this
  candidate, continue with remaining candidates, report partial completion.
- Sub-agent returns malformed JSON twice → fall back to heuristic-only,
  warn user.
- Spec file modified between read and write phase → abort before stamping
  marker; user re-runs.

### 2. `nudge-adr-capture.sh` hook

**Path:** `.claude/hooks/nudge-adr-capture.sh`

**Wiring in `.claude/settings.json`:** appended to the existing
`PostToolUse` block matching `Edit|Write`, alongside `auto-format.sh` and
`go-vet.sh`.

**Behavior:**

1. Read `PostToolUse` JSON from stdin; extract `tool_name` and
   `tool_input.file_path`.
2. Bail (exit 0, no stdout) unless `tool_name ∈ {Edit, Write}` and
   `file_path` matches the watched-path pattern (see §"Watched-path
   pattern" below).
3. Strip the trailing marker line from content if present (see §"Marker
   convention" for the exact rule); compute SHA-256 of the remainder
   (first 16 hex chars). Parse any marker.
4. Decide outcome:
   - Marker absent → nudge with reason `no marker`.
   - Marker present + `optout=true` → suppress (exit 0).
   - Marker present + SHA mismatch → nudge with reason
     `content changed since capture`.
   - Marker present + SHA match → suppress (exit 0).
5. On nudge: emit a single JSON object to stdout matching Claude Code's
   documented `PostToolUse` `additionalContext` contract:

   ```json
   {
     "hookSpecificOutput": {
       "hookEventName": "PostToolUse",
       "additionalContext": "adr-capture: <file_path> was modified (<reason>). Run /capture-adrs <file_path> to extract any ADR-worthy decisions, or --dry-run to preview."
     }
   }
   ```

   Then exit 0. Claude Code threads `additionalContext` next to the tool
   result on the next model request; Claude sees it. Bare stdout (the
   pattern that works only for `UserPromptSubmit` / `UserPromptExpansion`
   / `SessionStart`) does NOT reach Claude for `PostToolUse` and MUST
   NOT be used here.

**Constraints:**

- MUST exit 0 on all non-error paths so the underlying Edit/Write
  succeeds.
- MUST NOT block or delay the Edit/Write.
- MUST handle missing or unreadable file paths gracefully (exit 0, no
  stdout).
- MUST emit valid JSON on the nudge path (verified by `jq -e .` in the
  test harness).
- MUST NOT emit any stdout on suppress paths.
- Target ≤ 100 LOC of POSIX shell. `shellcheck` MUST pass.

**Known gaps (not in scope for the hook):**

- Hook only fires on Claude Code-invoked `Edit`/`Write`. Out-of-band
  edits — manual editor, `jj describe -i`, `dprint fmt`, `task fmt`, the
  `auto-format.sh` hook itself rewriting the file — will NOT trigger
  `PostToolUse` and therefore will NOT nudge. Users with such workflows
  MUST invoke `/capture-adrs <path>` manually. Wiring an equivalent
  check into the pre-commit hook chain or `task pr-prep` is a
  documented follow-up bead.
- Hook only watches the spec/plan path families (see §"Watched-path
  pattern"). Edits under `docs/adr/` MUST NOT trigger the nudge.

### 3. `adr-extractor` agent

**Path:** `.claude/agents/adr-extractor.md`

**Frontmatter:**

```yaml
---
name: adr-extractor
description: |
  Read-only agent that scans a finalized spec/plan and (optionally) its
  brainstorming session transcript to identify ADR-worthy decisions.
  Returns strict JSON. Used by /capture-adrs but reusable for batch
  retrospective extraction and audit workflows.
model: sonnet
tools:
  - Read
  - Grep
  - Glob
  - mcp__probe__search_code
  - mcp__probe__extract_code
  - Bash
skills:
  - jj:jujutsu
---
```

**System prompt MUST encode:**

1. **Four-criterion worthiness test.** A candidate is ADR-worthy iff ALL of:
   - It is architectural (not implementation detail like naming
     conventions, file layout choices, etc.)
   - It has a defensible alternative that was considered and rejected (the
     presence of a real trade-off is the signal)
   - It will be load-bearing for future decisions or contributors
   - It is not already captured in `docs/adr/` (agent MUST grep / probe
     before proposing)
2. **Output contract.** Strict JSON conforming to the schema in §"Output
   schema". No prose preamble. On internal failure: `{"error": "<reason>"}`.
3. **Existing-ADR check.** Before proposing, agent MUST search `docs/adr/`
   and `bd list --type decision` for related records. Sets `supersedes`
   when appropriate.
4. **Transcript scan strategies** (in priority order):
   - Windowed: locate the spec's Write/Edit tool calls in transcript;
     read 100 turns before each (cap configurable).
   - Brainstorming-skill marker: if transcript contains a
     `superpowers:brainstorming` Skill invocation, scan from that point
     forward.
   - Full-transcript fallback with keyword Grep: `reject`, `chose`,
     `alternative`, `trade-off`, `instead of`.
5. **Read-only contract.** Agent MUST NOT write files. Only the
   orchestrating skill writes.
6. **Output word cap.** Total response (all candidates + dropped regions)
   MUST fit within the cap supplied by the caller (default 800 words).

**Invocation contract:** the caller (skill) sends a structured prompt:

```text
SPEC: <absolute-path>
SPEC_HEURISTIC_REGIONS: [(line_start, line_end, header), ...]
TRANSCRIPT: <absolute-path | "none">
TRANSCRIPT_WINDOW: "full" | "from-brainstorm-skill-marker" | "100-turns-before-spec-writes"
EXISTING_ADRS_DIR: docs/adr/
OUTPUT_LIMIT: 800

Return JSON per the schema in your system prompt. Do not prepend prose.
```

**Output schema:**

```jsonc
{
  "candidates": [
    {
      "title": "string",
      "context": "2-4 sentences",
      "options_considered": [
        {
          "name": "string",
          "strengths": "string",
          "weaknesses": "string",
          "chosen": true | false
        }
      ],
      "decision": "1-2 sentences",
      "rationale": ["bullet", "bullet"],
      "consequences": {
        "positive": ["..."],
        "negative": ["..."],
        "neutral": ["..."]
      },
      "spec_section": "§3.5",
      "start_line": 123,
      "end_line": 147,
      "transcript_quotes": ["..."],
      "worthiness_score": 0,
      "supersedes": null
    }
  ],
  "dropped": [
    { "region": "spec §4.2", "reason": "implementation detail (slug casing)" }
  ]
}
```

### 4. One-shot migration

**Path:** `scripts/adr-migrate.py` (~150 LOC, single file). Pure Python
stdlib — no third-party deps. Migration runs once during this PR, then the
script can be archived (kept for self-documentation; not load-bearing for
future work).

**Taskfile target:** `task adr:migrate` (default `--dry-run`);
`task adr:migrate -- --apply` lands changes.

**Per-ADR steps:**

1. Parse `docs/adr/NNNN-<slug>.md` into a dict (handles both original and
   current format families — only 17 inputs, all known shapes, simple
   regex section-splitter is sufficient).
2. Render unified markdown body via Python f-string template inlined in
   the script (no external `.tmpl` file — the format spec in §"ADR format
   (unified)" is the canonical template; the script just substitutes into
   it).
3. `bd create -t decision --validate --description "$body"` → `<bd-id>`.
4. Rename the file from `docs/adr/NNNN-<slug>.md` to
   `docs/adr/<bd-id>-<slug>.md` via the filesystem (`Path.rename` /
   `shutil.move`). jj has no `mv` subcommand; the working-copy
   snapshotter auto-detects renames and `jj log --follow` works on the
   new path. There is no need to invoke `jj` for the rename.
5. Rewrite frontmatter: drop "ADR NNNN:" prefix from H1, add `**Decision:**
   <bd-id>`, add `**Originally:** ADR <NNNN>`.
6. Write stub at old path `docs/adr/NNNN-<slug>.md`:

   ```markdown
   <!-- SPDX-License-Identifier: Apache-2.0 -->
   <!-- Copyright 2026 HoloMUSH Contributors -->

   # Moved

   This ADR has moved to **[`<bd-id>-<slug>.md`](<bd-id>-<slug>.md)**.

   - **bd decision:** `<bd-id>` (run `bd show <bd-id>` for live status)
   - **Legacy number:** ADR <NNNN>
   - **Date migrated:** 2026-05-14
   ```

7. Resolve supersession edges (ADR 0007 → 0014 in the current backlog):
   - Look up new bd-id for superseder.
   - `bd dep add <new-bd-id> <existing-bd-id> --type supersedes` to record
     the edge (`supersedes` is a first-class `bd dep` type).
   - `bd close <existing-bd-id> --reason "Superseded by <new-bd-id>"` to
     retire the old record.
   - Rewrite superseded repo file's `**Status:**` header.

**Post-migration directory shape (flat, by design):**

After `--apply` lands, `docs/adr/` contains exactly **35 files**:

| Group | Count | Pattern |
|-------|-------|---------|
| Stub files at legacy paths | 17 | `[0-9]{4}-<slug>.md` |
| Real ADR files at bd-id paths | 17 | `<bd-id>-<slug>.md` |
| Index | 1 | `README.md` |

Stubs stay flat (not relocated to `docs/adr/legacy/`) because:

- Any prior absolute link `docs/adr/0009-custom-go-native-abac-engine.md`
  in a commit message, PR body, or external doc keeps working without
  redirect logic.
- The README's index-row regex `^<bd-id>-` excludes stub rows
  automatically; readers see one row per ADR.
- The cost is bounded: 17 stub files at ~10 lines each, formatted by
  `dprint` once per commit, is negligible.

Implementations MUST NOT move stub files into a subdirectory. A future
follow-up bead may revisit this decision once external link decay is
measurable.

**README regeneration:** end-to-end rewrite using the shape in §"README
shape". Migration map preserved between sentinel comments.

**Dry-run behavior** (`task adr:migrate` with no `--apply` flag):

- MUST NOT call `bd create`, `bd dep add`, `bd close`, `bd dolt commit`.
- MUST NOT rename any files. MUST NOT write any files.
- MUST print the planned `NNNN → bd-id` mapping (the bd-ids are not yet
  allocated, so the dry-run prints `NNNN → <new>` for each).
- MUST exit 0 on success.
- After a dry-run, `jj st` MUST report no working-copy changes.

**bd dolt commit:** at end of `--apply`, runs `bd dolt commit -m
"migration: import 17 legacy ADRs as decision records"`.

**Inline verification asserts** (script exits non-zero if any fails — these
are `assert` statements in the script, not a separate subsystem):

| Check | Expected |
|-------|----------|
| Count of `<bd-id>-*.md` real files | 17 |
| Count of `[0-9]{4}-*.md` stubs | 17 |
| Each stub links to an existing real file | 17/17 |
| Each real file has `**Decision:**` line referencing existing bd-id | 17/17 |
| `bd list --type decision` count | ≥17 |
| Each bd record passes `--validate` (re-running it is a no-op verify) | 17/17 |
| README index row count | 17 |
| README migration map row count | 17 |
| Supersession edges resolve to real bd-ids | 1 |

**Failure recovery:** the migration is a single jj change; if the script
fails partway, the user runs `jj abandon` (or fixes manually) and reruns.
No idempotency layer in the script — the diff makes partial state obvious.

**No tests for the migration script.** It runs once, the human reviewing
the PR eyeballs the 17-ADR diff, and the inline asserts catch structural
breakage. The durable health check (`adr-doctor.sh`) covers the long-term
invariants.

## ADR format (unified)

```markdown
# <Title>

**Date:** YYYY-MM-DD
**Status:** Accepted | Proposed | Superseded by <bd-id>
**Decision:** <bd-id>
**Deciders:** HoloMUSH Contributors
<!-- migrations only: -->
**Originally:** ADR <NNNN>

## Context

<2-4 sentences: problem, constraints>

## Decision

<one paragraph: what was decided>

## Rationale

- <factor 1>
- <factor 2>

## Alternatives Considered

- **<option>:** <weakness summary> — rejected
- **<chosen>:** <strength summary> — selected

## Consequences

**Positive:**

- ...

**Negative:**

- ...

**Neutral:**

- ...

## References

- Spec: `<spec-path>` §<section>
- Plan: `<plan-path>` (if applicable)
- Supersedes: <bd-id> (if applicable)
- Legacy ADR number: <NNNN> (migrations only)
```

Both renderers — the skill (Claude follows the format spec when rendering
from candidate JSON) and the migration script (Python f-string
substitutes into the format spec from the parsed legacy dict) — produce
conformant markdown by independently following this format. There is no
shared template file. Validator-required sections (`## Decision`,
`## Rationale`, `## Alternatives Considered`) are always present.

## README shape

```markdown
<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Architecture Decision Records (ADRs)

(purpose, immutability paragraph, kept from current README)

## Index

| Title | Date | Status | bd decision |
|-------|------|--------|-------------|
| [<title>](<bd-id>-<slug>.md) | 2026-05-13 | Accepted | `<bd-id>` |
| ... sorted by date desc |  |  |  |

<!-- BEGIN MIGRATION MAP -->
## Migration map (2026-05-13)

Legacy `NNNN-<slug>.md` filenames retired in favor of bd-decision IDs.
Stubs at the old paths preserve external references.

| Legacy | bd decision | Current file |
|--------|-------------|--------------|
| ADR 0001 | `<bd-id>` | [<bd-id>-opaque-session-tokens.md](<bd-id>-opaque-session-tokens.md) |
| ... all 17 |  |  |

<!-- END MIGRATION MAP -->

## Format

(single canonical format documented here; dual-format section removed)

## Template for new ADRs

(unified template, matching the skill's render)

## Writing guidelines

(unchanged from current README)
```

## Detection methodology

### Heuristic pre-scan regex set

| Pattern | What it flags |
|---------|---------------|
| `^#+\s+(Options Considered\|Alternatives Considered\|Decision\|Rationale\|Trade-?offs)\b` | Section headers |
| `^#+\s+Why not\b` | Negative-framing headers |
| `(?i)\b(rejected because\|chose .* because\|chosen because\|decided against\|ruled out)\b` | Inline rationale phrases |
| `(?i)\b(instead of\|in favor of\|over\b.*\bbecause)\b` | Inline pivot phrases |
| `(?i)\b(we settled on\|we landed on\|abandoning .* for\|prefer .* over)\b` | Convergence phrases |
| `(?i)\bAlternative [A-Z]:` | Numbered alternative blocks |
| `(?i)\btrade.?off\b` | Trade-off phrases |

The regex set is a recall booster, not a precision filter — false
positives are fine because the agent's worthiness test culls them. The
set is deliberately broader than the obvious header matches so the
windowed transcript scan has something to anchor on even when the spec
text is sparse.

Output: list of `(start_line, end_line, surrounding_header)` regions.

### Transcript window strategies

1. **Windowed (default).** Find Write/Edit tool calls in transcript whose
   `tool_input.file_path` matches the spec path. Read 100 turns before each
   write (configurable cap).
2. **Brainstorm-marker.** If transcript contains `Skill: superpowers:brainstorming`
   invocation, scan from that turn forward.
3. **Full-fallback.** Keyword Grep across full transcript for
   decision-shaped phrases; read matching regions.

If no transcript is resolvable, spec-text-only mode is acknowledged in the
final report.

### Four-criterion worthiness test

A candidate is ADR-worthy iff ALL of:

1. **Architectural** (not implementation detail).
2. **Has rejected alternatives** with real trade-off.
3. **Load-bearing** for future decisions or contributors.
4. **Not already captured** in `docs/adr/`.

Agent scores each candidate 0-4 by criterion-pass count. Score < 4 is
flagged as borderline in the review loop; user still decides.

## `adr-doctor.sh` health check

**Path:** `scripts/adr-doctor.sh` (~80 LOC, POSIX shell + `bd` + `jq`).

The durable post-migration health check. Wired into `task lint`. Runs
after every working-copy change to `docs/adr/` so drift is caught at
edit time, not at PR time.

**CLI:**

```sh
scripts/adr-doctor.sh             # run all checks; exit 0 if clean, non-zero on any failure
scripts/adr-doctor.sh --explain   # additionally print which invariants each check covers
```

**Checks (each one named with the invariant it supports):**

| Check | Asserts |
|-------|---------|
| `count_real_files` (INV-A12) | `find docs/adr -maxdepth 1 -regex '.*/[a-z0-9-]+-[a-z0-9-]+\.md' -not -regex '.*/[0-9]\{4\}-.*'` returns 17. |
| `count_stubs` (INV-A12) | `find docs/adr -maxdepth 1 -regex '.*/[0-9]\{4\}-.*\.md'` returns 17. |
| `count_readme` (INV-A12) | `docs/adr/README.md` exists, total files in directory = 35. |
| `no_legacy_subdir` (INV-A12) | `[ ! -d docs/adr/legacy ]` (stubs MUST remain flat). |
| `stub_links_real` (INV-A12) | For each stub, grep the `<bd-id>-<slug>.md` link target out of the body and assert the target file exists. |
| `file_has_decision_header` (INV-A4, INV-A5) | For each `<bd-id>-*.md`, grep `^\*\*Decision:\*\* <bd-id>$` matches the filename's bd-id prefix; `bd show <bd-id>` returns 0. |
| `file_has_validator_sections` (INV-A4) | Each real ADR file contains `## Decision`, `## Rationale`, `## Alternatives Considered` headers. |
| `agent_frontmatter` (INV-A14, INV-A15) | `.claude/agents/adr-extractor.md` frontmatter sets `model: sonnet` AND its `tools:` list does NOT contain `Write`, `Edit`, or `NotebookEdit`. |
| `hook_executable` | `.claude/hooks/nudge-adr-capture.sh` is executable and passes `shellcheck`. |
| `forbid_skill_commits` (INV-A2) | `grep -E '(jj commit\|jj describe\|git commit\|git add)' .claude/skills/capture-adrs/SKILL.md` returns no matches. The skill MUST NOT commit; user runs commits. |
| `invariant_coverage` (meta-test) | See §Invariants §Meta-test for the canonical rule. Walks every `\| INV-A<n> \|` row, validates each row's `Surface` column against the fixed enum, and for `doctor`-tagged invariants requires a matching `# INV-A<n>` comment in this script. |
| `supersession_edges` (INV-A13) | For each ADR file whose `**Status:**` line contains "Superseded by `<bd-id>`", `bd dep list <bd-id> \| grep -q supersedes` succeeds (i.e., the superseder's bd record has a `supersedes` dep edge to this file's bd-id). |

**Exit codes:** 0 on full pass; 1 on any check failure with a per-check
report on stderr; 2 on missing tools (`bd`, `jq`) or unexpected
preconditions.

**Wiring:** `Taskfile.yaml` `lint:adr` target is added under `lint`'s
`cmds:` list (`- task: lint:adr`); runs `scripts/adr-doctor.sh`.
`task lint` therefore runs it. CI fails on non-zero.

## Invariants

The following RFC2119 invariants MUST hold once the work lands. Each
invariant carries a `Surface` value from this fixed enum:

| Surface | Meaning |
|---------|---------|
| `doctor` | `scripts/adr-doctor.sh` has a named check for this invariant; runs on `task lint`. |
| `hook-test` | Asserted by a fixture in `.claude/hooks/nudge-adr-capture.test.sh`. |
| `skill-test` | Asserted by a fixture in the skill's end-to-end / integration test set (run manually via `task test:skills`). |
| `migration-assert` | Asserted by an inline `assert` in `scripts/adr-migrate.py`. |
| `manual` | Verified by PR review or one-shot manual procedure; no automated assertion. |

| ID | Surface | Statement | Test detail |
|----|---------|-----------|-------------|
| INV-A1 | `skill-test` | The skill MUST NOT write any files until all per-candidate triage decisions are collected. | Skill end-to-end test against fixture; assert no writes occur if user "Skip"s every candidate. |
| INV-A2 | `doctor` | The skill MUST NOT commit. Skill writes are uncommitted; user runs the commit. | `adr-doctor.sh` greps `SKILL.md` for `jj commit`/`jj describe`/`git commit`/`git add` and fails on any match. |
| INV-A3 | `skill-test` | Every accepted candidate MUST result in exactly one `bd create -t decision --validate` call. | Skill integration test with synthetic 3-candidate fixture; assert bd record count delta == accept count. |
| INV-A4 | `doctor` | Every ADR file MUST satisfy the `bd create -t decision --validate` rules (presence of `## Decision`, `## Rationale`, `## Alternatives Considered`). The file's `**Decision:** <bd-id>` header MUST link to a bd decision record. The skill and the migration are responsible for keeping the two representations in sync at write time; no post-hoc section-by-section equality is asserted. | `adr-doctor.sh` greps each `<bd-id>-*.md` for the three required `##` headers AND extracts the `**Decision:** <bd-id>` value AND runs `bd show <bd-id>` to confirm the record exists. The bd description's `--validate` conformance is a creation-time invariant enforced by the skill/migration; not re-checked here. |
| INV-A5 | `doctor` | The `**Decision:** <bd-id>` line MUST resolve via `bd show <bd-id>` for every file in `docs/adr/<bd-id>-*.md`. | `adr-doctor.sh` `file_has_decision_header` check; wired into `task lint`. |
| INV-A6 | `hook-test` | The hook MUST exit 0 on every non-error path; it MUST NOT block Edit/Write. | `nudge-adr-capture.test.sh` cases assert exit code 0 for: non-spec path, no marker, stale marker, fresh marker, opt-out marker, missing file. |
| INV-A7 | `hook-test` | The hook MUST suppress nudges when marker SHA matches stripped content SHA. | `nudge-adr-capture.test.sh` "fresh marker" fixture asserts stdout is empty. |
| INV-A8 | `hook-test` | The hook MUST emit, on the nudge path, a single JSON object on stdout matching `{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"..."}}` with the spec path embedded in `additionalContext`. Bare `<system-reminder>` strings are forbidden (PostToolUse stdout does not reach Claude as text — only `additionalContext` JSON does). | `nudge-adr-capture.test.sh` "no marker" and "stale marker" fixtures: assert `jq -e '.hookSpecificOutput.hookEventName == "PostToolUse"'` passes AND `jq -e '.hookSpecificOutput.additionalContext \| contains("<spec-path>")'` passes. |
| INV-A9 | `skill-test` | The skill MUST write the marker even when zero candidates are accepted (so the hook does not re-nudge on the same content). | Skill end-to-end test with "user skips all" fixture; assert marker is written with `adrs=` empty. |
| INV-A10 | `hook-test` + `skill-test` | The skill AND hook MUST honor a well-formed `optout=true` marker as a suppression signal. The skill MUST NOT overwrite an opt-out marker; if invoked on an opt-out-marked spec it MUST abort with a clear message naming the opt-out `reason`, regardless of `--re-run`. The hook MUST exit 0 with no stdout on opt-out specs. | `nudge-adr-capture.test.sh` "opt-out marker" fixture asserts hook stdout empty + exit 0. Skill end-to-end test "opt-out spec" fixture asserts the skill aborts with the reason in its message AND does NOT modify the file. |
| INV-A11 | `manual` | The migration MUST land in a single jj change; partial failure recovery is `jj abandon` + fix + rerun, not a script-level idempotency layer. | Manual verification in the PR (commit log shows single change). |
| INV-A12 | `migration-assert` + `doctor` | After successful migration, `docs/adr/` MUST contain exactly **35 files**: 17 stub files matching `[0-9]{4}-<slug>.md`, 17 real files matching `<bd-id>-<slug>.md`, and 1 `README.md`. Exactly 17 bd decision records of type `decision` MUST exist with descriptions that pass `--validate`. Stubs MUST remain flat (no `legacy/` subdirectory). | Inline `assert` in migration script (exits non-zero if mismatched); `adr-doctor.sh` `count_real_files` + `count_stubs` + `count_readme` + `no_legacy_subdir` checks enforce post-merge on `task lint`. |
| INV-A13 | `migration-assert` + `doctor` | The supersession edge (ADR 0007 → 0014) MUST survive migration: the new superseder record has a `--type supersedes` dep on the superseded; the superseded record is closed with reason; the file's `**Status:**` reflects it. | Inline `assert` in migration script; `adr-doctor.sh` `supersession_edges` check confirms `bd dep list <id>` returns a `supersedes` edge for every "Superseded by" file. |
| INV-A14 | `doctor` | The `adr-extractor` agent frontmatter MUST specify `model: sonnet`. | `adr-doctor.sh` `agent_frontmatter` check. |
| INV-A15 | `doctor` | The `adr-extractor` agent MUST NOT have file-write tools in its allowlist (no `Write`, no `Edit`, no `NotebookEdit`). | `adr-doctor.sh` `agent_frontmatter` check (same check, second predicate). |
| INV-A16 | `hook-test` | The hook MUST only watch the spec/plan path families per §"Watched-path pattern"; it MUST NOT fire on edits to other paths (including `docs/adr/` itself, `cmd/`, `internal/`, etc.). | `nudge-adr-capture.test.sh` includes "docs/adr/ edit" and "internal code edit" fixtures that assert no stdout. |
| INV-A17 | `manual` | The migration MUST preserve `jj log --follow` continuity (rename via filesystem move so jj's snapshotter detects the rename; do NOT delete-then-create the file). | Manual verification on migration PR via `jj log --follow <new-path>`; documented in migration runbook. |
| INV-A18 | `migration-assert` | The migration script's `--dry-run` mode MUST NOT mutate any state: no `bd create`/`bd dep add`/`bd close`/`bd dolt commit`, no file renames, no file writes. After `task adr:migrate` (with no `--apply`), `jj st` MUST report no working-copy changes. | Inline `assert` block at script exit in `scripts/adr-migrate.py` when `--apply` is absent; PR-time manual `jj st` check is the secondary signal. |
| INV-A19 | `manual` | The skill MUST work in the user's main Claude Code session context, inheriting its tool set. `AskUserQuestion` is assumed available; if not, the skill text instructs the agent to fail fast with a clear message. | Skill prompt text. Reviewed manually whenever the skill is edited. |

**Meta-test:** `scripts/adr-doctor.sh` includes an `invariant_coverage`
check that:

1. Greps this spec for `^| INV-A[0-9]+ |` rows and extracts each row's
   `Surface` column value.
2. Asserts the value is a member of the fixed enum `{doctor, hook-test,
   skill-test, migration-assert, manual}` (combinations joined by ` + `
   are allowed; each token MUST be in the enum).
3. For every invariant whose Surface contains `doctor`, asserts that
   `scripts/adr-doctor.sh` has a named check that mentions the
   invariant ID (e.g., `# INV-A4` comment in the relevant check
   function).
4. Fails CI if any invariant row is missing a Surface value, the value
   is not in the enum, or a `doctor`-tagged invariant has no matching
   named check in the script.

The check does NOT attempt to verify `hook-test`/`skill-test`/
`migration-assert` coverage at the source level — those surfaces are
asserted by their own test harnesses, which CI runs separately and
which the meta-test trusts to either pass or fail.

## Testing strategy

The testing surface is intentionally minimal. The migration is a one-shot
verified by human PR review + inline asserts. The durable pieces (hook,
agent, skill, health check) get appropriate coverage; the one-shot does
not.

| Component | Approach | Location |
|-----------|----------|----------|
| Migration script | No automated tests. Runs once. Inline `assert` statements catch structural breakage during the run; PR review catches semantic issues by eyeballing the 17-ADR diff. | (no test file) |
| Hook script | `shellcheck` + a small bats-style test feeding synthetic stdin JSON for each branch (non-spec path, no marker, fresh marker, stale marker, opt-out, missing file). | `.claude/hooks/nudge-adr-capture.test.sh` |
| `adr-extractor` agent | Manual fixture regression set under `.claude/agents/adr-extractor.testdata/`. `task test:agents` runs the agent against each fixture; not wired into `task test` (slow/expensive). Used when modifying the agent's system prompt. | `.claude/agents/adr-extractor.testdata/` |
| `/capture-adrs` skill end-to-end | Dogfooded on this very spec during the implementation PR; captured ADRs (if any) ship in the same PR as proof. No automated test — skills are prompts. | manual artifact |
| Health check | `scripts/adr-doctor.sh` wired into `task lint`; greps `docs/adr/` for orphans, missing headers, unresolved bd-ids, format conformance. Durable; the migration is verified by this check after the fact. | `scripts/adr-doctor.sh` |
| Invariant meta-test | `adr-doctor.sh` greps this spec for `INV-A<n>` and asserts each has a test reference (or is explicitly marked manual-verification). | (same file) |

## Out of scope

1. Automated capture from sources other than the four documented spec/plan
   path families. Decisions in code comments, commit messages, Slack
   threads, or PR descriptions are not in scope here.
2. `task pr-prep`-level enforcement that every modified spec has a current
   capture marker before push. (Candidate follow-up bead.)
3. Auto-supersession detection beyond what the extractor surfaces from
   spec/transcript signal. No fuzzy "this topic feels similar to ADR-N"
   matching.
4. Cross-project ADR federation or import from other repos.
5. ADR search UI / web view. `bd search`, `bd show <id>`, and `grep` are
   the search surface.
6. Retroactive capture for specs that produced decisions outside the 17
   migrated. The migration handles the existing `docs/adr/` set; specs
   that never produced an ADR but should have are not auto-mined.
   (Candidate follow-up bead: `task adr:sweep`.)
7. Per-PR ADR enforcement bot (e.g., GitHub Action gating review on
   marker freshness). (Candidate follow-up bead.)
8. Decision deprecation (vs supersession) lifecycle tooling. Manual bd
   record + file edit is the current shape.

## Files modified / created

The full delta the implementation PR ships:

**New files:**

| Path | Description |
|------|-------------|
| `.claude/skills/capture-adrs/SKILL.md` | The `/capture-adrs` skill body |
| `.claude/agents/adr-extractor.md` | Read-only sonnet agent for ADR detection |
| `.claude/hooks/nudge-adr-capture.sh` | PostToolUse hook |
| `.claude/hooks/nudge-adr-capture.test.sh` | Hook test harness |
| `.claude/agents/adr-extractor.testdata/<N>.md` | Manual fixture set (~5 files) |
| `scripts/adr-migrate.py` | One-shot migration script (~150 LOC) |
| `scripts/adr-doctor.sh` | Durable health check (~80 LOC) |
| `docs/superpowers/specs/2026-05-13-adr-capture-skill-design.md` | This spec |
| `docs/adr/<bd-id>-<slug>.md` (×17) | Migrated real ADR files |

**Modified files:**

| Path | Change |
|------|--------|
| `.claude/settings.json` | Add `nudge-adr-capture.sh` to PostToolUse `Edit\|Write` matcher block |
| `Taskfile.yaml` | Add `adr:migrate` and `lint:adr` targets; append `- task: lint:adr` to `lint`'s `cmds:` list |
| `docs/adr/README.md` | Full rewrite per §"README shape" |
| `docs/adr/[0-9]{4}-<slug>.md` (×17) | Rewritten as stubs at legacy paths |
| `.beads/dolt/...` | Dolt commit adding 17 decision records + 1 supersession edge |
| `CLAUDE.md` | Update ADR references to new bd-id convention (if any exist) |

**No new third-party dependencies.** `adr-migrate.py` uses Python stdlib
only; `adr-doctor.sh` uses POSIX shell + `bd` + `jq` (already required
elsewhere in the repo).

## References

- Bead: `holomush-b2qy` "Wrap-up skill/hook: ADR capture on spec/plan
  finalize"
- Existing ADR conventions: `docs/adr/README.md` (current, pre-migration)
- bd CLI surface: `bd create -t decision --validate` (aliases `dec`,
  `adr`); validator requires `## Decision`, `## Rationale`, `##
  Alternatives Considered`
- Repo agent pattern: `.claude/agents/code-reviewer.md`,
  `.claude/agents/bead-auditor.md` (style reference for `adr-extractor`)
- Repo skill pattern: `.claude/skills/bead-create-smart/SKILL.md`,
  `.claude/skills/bead-chain-design/SKILL.md` (style reference for
  `/capture-adrs`)
- Hook pattern: `.claude/hooks/nudge-probe-over-rg.sh`,
  `.claude/hooks/remind-pre-action-review.sh` (style reference)
- ADR 0017 (`admin-readstream-bypasses-history-reader`): gold-standard
  example of the unified format target
<!-- adr-capture: sha256=6d6269129baf5ec3; session=dogfood; ts=2026-05-14T13:58:54Z; adrs= -->
