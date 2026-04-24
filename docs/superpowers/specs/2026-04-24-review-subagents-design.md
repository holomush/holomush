# Review Sub-Agents Design

**Date:** 2026-04-24
**Status:** Draft
**Authors:** Sean Brandt (with Claude Opus 4.7)

## Problem

HoloMUSH development produces three kinds of artifacts that need review before
they hand off to the next lifecycle stage:

1. **Code** — diffs on a feature branch, before `jj git push` or PR creation.
2. **Plans** — implementation plans from `superpowers:writing-plans`, before
   execution begins.
3. **Designs/specs** — design documents from `superpowers:brainstorming`, before
   plans are written.

The existing review surface is strong for the code path post-PR
(`pr-review-toolkit:review-pr`, CodeRabbit via `code-review`) but has two gaps:

- No adversarial gate **before** code hits a PR. The main agent is the
  implementer and therefore the wrong actor to judge whether its own work is
  done. Self-review is inherently biased.
- No adversarial gate at all for plans and designs. These currently rely on the
  brainstorming and writing-plans skills' own self-review loops, which are
  performed by the same agent that produced the artifact.

The failure mode is not rare. Recent work has shown:

- PRs land with findings that a fresh pass would have caught (per memory:
  `feedback_pagination_cursor_advancement` — a multi-round cursor advancement
  bug that slipped through multiple passes).
- Plans get executed with missing steps or untestable acceptance criteria.
- Specs get implemented before non-obvious ambiguities are resolved.

## Goals

- Provide three independent read-only sub-agents that adversarially verify
  code, plans, and designs before they hand off.
- Each sub-agent MUST be constitutionally adversarial: its prompt, persona,
  and tooling are designed to find flaws, not validate work.
- Each sub-agent MUST ground every finding in the actual artifacts (code, spec,
  plan, linked beads issue, external library docs). No model-prior claims.
- Each sub-agent MUST emit a binary verdict (READY / NOT READY), forcing a call
  rather than allowing soft "mostly good" outputs.
- Each sub-agent MUST be invokable both automatically (via Claude Code's
  description-driven orchestrator) and explicitly (via a slash command).
- Each sub-agent MUST NOT have write access to the repository. Reviewers
  produce reports; they do not apply fixes.

## Non-Goals

- Replacing or duplicating `pr-review-toolkit:review-pr`. That remains the
  PR-time review surface. The new agents fire **before** the PR.
- Creating new beads issues. Findings return to the implementer; the
  implementer decides what to track. At most, the agent MAY suggest appending
  notes to an existing in-progress bead.
- Applying fixes. Reviewers are read-only by construction.
- Supporting parallel review of the same artifact by multiple new agents in
  one pass. Chain them manually if needed.

## Design

### Three sub-agents, each guarding one hand-off

| Agent | File | Fires before | Reviews |
|---|---|---|---|
| `code-reviewer` | `.claude/agents/code-reviewer.md` | `bd close`, `jj git push`, PR creation | `jj diff --from <merge-base>` output + full touched files + linked beads issue + spec the issue references |
| `plan-reviewer` | `.claude/agents/plan-reviewer.md` | `superpowers:executing-plans` invocation | The plan file + the spec it claims to implement |
| `design-reviewer` | `.claude/agents/design-reviewer.md` | `superpowers:writing-plans` invocation | The spec file in `docs/superpowers/specs/` or `docs/specs/` |

Each agent is defined by a single Markdown file with YAML frontmatter, per
[Claude Code sub-agent conventions](https://code.claude.com/docs/en/sub-agents).

### Adversarial persona (shared across all three)

Every agent's body starts with the same persona contract. The contract
explicitly forbids common failure modes of self-review:

- The agent did not write the artifact. It has no investment in the approach.
- Completion ≠ correctness. The implementer's claim of done is irrelevant.
- "Shortest path," "least change," "small PR" are NOT virtues when they come
  at the cost of correctness, safety, or meeting the spec.
- Findings are not softened. No "this is mostly good" preamble.
- Pushback from the implementer is not a reason to downgrade a finding — the
  agent verifies the disagreement against grounded sources and restates.
- The agent reads the upstream source of truth FIRST (spec for code,
  spec+goals for plans, goals for designs) before reading the artifact under
  review.

### Grounding discipline (MUST)

Every finding MUST be grounded in the artifacts at hand. The agent MUST NOT
rely on implicit model knowledge. Concrete rules:

- Before asserting a claim about code: read the file; cite `path:line`.
- Before claiming something is missing: `Grep`/`Glob` for it. Absence is a
  finding only after verification.
- Before claiming something is wrong: read callers, callees, and tests.
  A function unsafe in isolation may be safe in context.
- When judging spec or plan content: quote the artifact verbatim.
- "Typically," "usually," "best practice," "most codebases" in the agent's
  own output are red flags that MUST be replaced with concrete citations.

### External grounding (MUST)

For any claim about a library, API, version, or idiomatic pattern, the agent
MUST verify against current authoritative sources via the MCP tools listed
below. Training data is insufficient — library APIs drift.

- `mcp__context7__query-docs` / `mcp__context7__resolve-library-id` — library
  documentation.
- `mcp__deepwiki__ask_question` / `mcp__deepwiki__read_wiki_contents` — GitHub
  repository state, READMEs, release history.
- `mcp__exa__web_search_exa` / `mcp__exa__get_code_context_exa` — current web
  state, CVEs, current idioms.
- `WebFetch` — specific URLs cited by the implementer.

External grounding MUST NOT become theatre. A finding that cites `context7` but
doesn't identify a concrete problem is still not a finding. Citations prove
claims; they do not create them.

### Dependency hygiene (code-reviewer only)

When the diff adds a new dependency OR when a finding could be resolved by
touching a dependency, the code-reviewer MUST verify:

- Version currency: is the pinned version the most current stable release?
  If not, is the reason stated? "No stated reason to pin old version" is
  itself a finding.
- Maintenance: is the library actively maintained? Cite last-release /
  issue-count evidence from `deepwiki` or `exa`.
- Ecosystem fit: is it the popular, well-supported choice, or a niche pick?
  Prefer the more functional, more popular, more actively-released option
  unless there is a grounded reason (license, size, specific feature) to
  pick the niche one.
- Overlap: does `go.mod` already contain a library that solves the same
  problem? Flag overlap.
- Resolvable-by-bump: if a finding could be resolved by a version bump, check
  whether current pins lag behind upstream. Cite the gap.

### Output contract (uniform)

Every agent emits a Markdown report in chat with this structure:

```
## Summary
[One paragraph: what the artifact does, what it claims to satisfy, the agent's
overall read — grounded only in what was actually inspected.]

## Blocking findings
### 1. [Severity: Critical | High] <short title>
- Location: `path:line` or section reference
- Evidence: <verbatim quote or citation>
- Issue: <what is wrong>
- Why it blocks: <consequence if this hands off>
- Required resolution: <what the implementer must do>

## Non-blocking findings
### 1. [Severity: Medium | Low] <short title>
... same format ...

## Verification evidence
- Read: <list of files>
- Searched: <list of greps run and what was sought>
- External grounding: <list of library/version checks with sources cited>

## Verdict
- [ ] READY
- [ ] NOT READY — see blocking findings above
```

The verdict is **binary**. "Mostly ready" is not an option. Either the
blocking findings list is empty (READY) or it is not (NOT READY).

## File Layout

```
.claude/
├── agents/
│   ├── abac-reviewer.md              # existing, unchanged
│   ├── code-reviewer.md              # NEW
│   ├── plan-reviewer.md              # NEW
│   └── design-reviewer.md            # NEW
├── commands/
│   ├── review-code.md                # NEW
│   ├── review-plan.md                # NEW
│   └── review-design.md              # NEW
└── agent-memory/                     # NEW, checked into VCS
    ├── code-reviewer/
    ├── plan-reviewer/
    └── design-reviewer/
```

`.claude/agent-memory-local/` MUST be added to `.gitignore` to match the
local-scope memory pattern documented in Claude Code sub-agents; the
project-scope `agent-memory/` directory is intentionally checked in.

## Frontmatter

All three agents share this pattern, with per-agent variations below:

```yaml
---
name: code-reviewer
description: |
  Use proactively before claiming a beads task complete, before `jj git push`,
  or before creating a PR. Adversarial independent reviewer — not the
  implementer's ally. All findings must be grounded in the repo and cited.
  Read-only.
model: opus
effort: high
permissionMode: plan
color: red
tools:
  - Read
  - Grep
  - Glob
  - Bash
  - WebFetch
  - mcp__context7__resolve-library-id
  - mcp__context7__query-docs
  - mcp__deepwiki__ask_question
  - mcp__deepwiki__read_wiki_contents
  - mcp__exa__web_search_exa
  - mcp__exa__get_code_context_exa
skills:
  - superpowers:verification-before-completion
  - code-review-excellence
  - elements-of-style:writing-clearly-and-concisely
  - systems-programming:go-concurrency-patterns
  - jj:jujutsu
memory: project
maxTurns: 50
---
```

| Field | Rationale |
|---|---|
| `model: opus` | Per project CLAUDE.md model-selection rule (highest available). Review quality is precision-bound. |
| `effort: high` | Reviews MUST NOT take shortcuts. |
| `permissionMode: plan` | Belt-and-suspenders with the tool allowlist. Even if the prompt is subverted, the permission layer blocks writes. |
| `tools` (allowlist) | Explicit allowlist MUST exclude `Write`, `Edit`, `NotebookEdit`. The MCP set covers external grounding. |
| `skills` (preloaded) | Skills inject craft (verification discipline, clear writing) that complements the persona. Subagents do not inherit skills from the parent, so they MUST be listed. |
| `memory: project` | Accumulates project-specific anti-patterns in `.claude/agent-memory/<agent>/MEMORY.md`, shared via VCS. |
| `maxTurns: 50` | Bounded runaway investigation. |
| `color` | UI distinction: red (code), yellow (plan), purple (design). |

Per-agent diffs (note: `jj:jujutsu` is preloaded on ALL three agents because
this repo is jj-colocated and the `jj:jujutsu` skill mandates activation on
any VCS operation — even read-only `jj log` / `jj blame` the reviewers may use
for artifact context):

- **`plan-reviewer`**: `color: yellow`; `skills` drops `systems-programming:go-concurrency-patterns` and `code-review-excellence`, adds `superpowers:writing-plans`. Final list: `superpowers:verification-before-completion`, `elements-of-style:writing-clearly-and-concisely`, `superpowers:writing-plans`, `jj:jujutsu`.
- **`design-reviewer`**: `color: purple`; `skills` is `superpowers:brainstorming` + `superpowers:writing-plans` + `elements-of-style:writing-clearly-and-concisely` + `superpowers:verification-before-completion` + `jj:jujutsu`.

Fields intentionally NOT used in v1:

| Field | Why omitted |
|---|---|
| `hooks` | Auto-appending findings to beads violates the "intentional, not automatic" principle. The agent MAY suggest a note in its text output; the human decides. |
| `isolation: worktree` | Overkill for read-only review. |
| `mcpServers` (inline) | External-grounding MCPs are already session-scoped; no need to redefine. |
| `disallowedTools` | `tools` allowlist is more explicit and auditable. |
| `background: true` | Reviews MUST block. The verdict is required before hand-off. |

## Per-Agent Body Skeletons

Each agent's body (below the frontmatter) follows this shape:

1. **Identity and stance** — the adversarial persona contract (shared text).
2. **Grounding discipline** — the MUST rules (shared text).
3. **External grounding** — MCP usage and citation discipline (shared text).
4. **Artifact-specific section** — what to read first, what to search for,
   what "done" means for this artifact type.
5. **Workflow** — ordered steps: scope detection → upstream read → artifact
   read → gap search → external grounding pass → self-check → emit.
6. **Output format** — the uniform report structure.
7. **Persistent memory** — instructions to consult and curate
   `.claude/agent-memory/<agent>/MEMORY.md`.

For `code-reviewer`, the artifact-specific section adds the dependency
hygiene rules.

For `plan-reviewer`, the artifact-specific section requires:

- Read the spec in full FIRST.
- For every spec requirement (numbered or bulleted), verify the plan has a
  step that addresses it. Requirements without plan steps are blocking gaps.
- For every plan step, verify it has a definition of done (an observable,
  testable outcome). Steps without DoD are blocking gaps.
- Flag scope bloat: plan steps that do NOT trace to a spec requirement.

For `design-reviewer`, the artifact-specific section requires:

- Read the stated goal / user problem / epic FIRST.
- The spec MUST have:
  - A stated problem.
  - Goals and non-goals.
  - A definition of done / acceptance criteria that are testable.
  - Named interfaces or boundaries that can be verified against the codebase.
- Flag: requirements that cannot be turned into a test, interfaces that
  contradict existing code (verified by grep), non-goals that are actually
  implicit goals, overlap with existing specs in `docs/specs/` or
  `docs/superpowers/specs/`.

## Invocation Model

Two invocation paths, both supported:

1. **Auto-invocation** via the `description` frontmatter field. The
   descriptions are written as trigger phrases matching natural hand-off
   moments ("before claiming a beads task complete," "after writing a plan,"
   "after writing a spec"). Claude Code's orchestrator routes to the right
   agent semantically.
2. **Explicit invocation** via slash commands in `.claude/commands/`:
   - `/review-code [path-or-artifact]` → `@agent-code-reviewer`
   - `/review-plan <plan-path>` → `@agent-plan-reviewer`
   - `/review-design <spec-path>` → `@agent-design-reviewer`

Slash command files are thin dispatchers. Each pins the target agent via
`@agent-<name>` and passes `$ARGUMENTS` as the review scope.

## Integration With Existing Workflow

- `superpowers:brainstorming` writes a spec → `design-reviewer` fires
  (auto or via `/review-design`) → spec revised if NOT READY → once READY,
  `writing-plans` proceeds.
- `superpowers:writing-plans` writes a plan → `plan-reviewer` fires (auto or
  via `/review-plan`) → plan revised if NOT READY → once READY,
  `executing-plans` or `subagent-driven-development` proceeds.
- Implementation complete, before `bd close` / `jj git push` / PR creation →
  `code-reviewer` fires (auto or via `/review-code`) → work revised if NOT
  READY → once READY, hand off to push → `pr-review-toolkit:review-pr` then
  runs on the PR for the post-push review surface.

The new agents complement, do not replace, `pr-review-toolkit:review-pr`.
Two review surfaces, two moments: pre-push adversarial gate (these) and
PR-time multi-specialist review (existing).

## Persistent Memory

Each agent uses `memory: project`, which creates
`.claude/agent-memory/<agent-name>/` with a `MEMORY.md` file that the agent
reads at startup and curates after each review. Enabling `memory` auto-enables
`Read`, `Write`, and `Edit` tools for the memory directory only; the
`permissionMode: plan` restriction still prevents writes outside it.

Agent-memory directories are checked into VCS. The intent is that the
code-reviewer learns HoloMUSH-specific anti-patterns over time (e.g.,
"`jj rebase -d main` without `-r <change-id>` sweeps up other agents' work"),
and those patterns become available to future review passes without
re-learning.

`MEMORY.md` files MUST stay under 200 lines. Agents MUST curate, not hoard.

## Acceptance Criteria / Definition of Done

The implementation of this spec is complete when ALL of the following hold:

1. `.claude/agents/code-reviewer.md`, `.claude/agents/plan-reviewer.md`, and
   `.claude/agents/design-reviewer.md` exist with the frontmatter and body
   structure described above.
2. `.claude/commands/review-code.md`, `.claude/commands/review-plan.md`, and
   `.claude/commands/review-design.md` exist and correctly dispatch to their
   respective agents.
3. `.claude/agent-memory/` exists with empty `MEMORY.md` stubs for each agent,
   checked into VCS.
4. `.claude/agent-memory-local/` is added to `.gitignore`.
5. Each agent can be invoked via its slash command on a sample artifact
   (e.g., this spec for `/review-design`) and produces a report conforming
   to the output contract — Summary, Blocking findings, Non-blocking findings,
   Verification evidence, binary Verdict.
6. Each agent's output cites specific files, lines, and (where applicable)
   external sources. No finding is ungrounded.
7. An attempt to have any of the three agents use `Write` or `Edit` fails
   (verified by prompting the agent to try) — the tool allowlist and
   `permissionMode: plan` enforce read-only behaviour.
8. `task lint` passes (no new lint regressions from Markdown additions).
9. `CLAUDE.md` is updated to document the new review gate in the workflow
   section, referencing the three agents by name and their invocation
   moments.

## Risks and Open Questions

- **Auto-invocation noise.** If the `description` fields are too broad, the
  orchestrator may invoke reviewers in contexts where they aren't wanted.
  Mitigation: start with conservative trigger phrases; tighten if false
  positives exceed roughly 10% in practice.
- **Skill preload weight.** Preloading four skills per agent increases the
  startup context. If latency becomes noticeable, drop the
  language-specific skill (go-concurrency) and let the agent load it
  on-demand.
- **Memory drift.** Agent memory can accumulate outdated anti-patterns as
  the codebase evolves. Mitigation: the 200-line cap forces curation;
  revisit each `MEMORY.md` quarterly.
- **Grounding cost.** External grounding via MCP tools adds wall-clock time
  to every review. Acceptable tradeoff given the quality goal; measure
  after a few weeks of use and adjust if needed.

## Future Work (out of scope for v1)

- Optional `bd create` integration when a review produces blocking findings
  that cross a threshold (e.g., ≥3 criticals) — for now, human decides.
- A fourth agent (`adr-reviewer`?) that reviews Architecture Decision
  Records specifically, if/when HoloMUSH starts using ADRs heavily.
- A `superpowers:subagent-driven-development` hook that auto-invokes
  `code-reviewer` between the "task complete" and "bead close" transitions,
  in addition to the description-driven auto-invocation path.
