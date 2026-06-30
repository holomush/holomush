---
name: audit-beads
description: Audit open `bd` issues for stale/closeable beads with grounded evidence
disable-model-invocation: true
---

@agent-bead-auditor Audit open beads using the methodology in your system
prompt. Per your Emission contract, persist the full report to
`.claude/agent-memory/bead-auditor/reports/<timestamp>-<slug>.md`.

**Scope:** $ARGUMENTS

**If no scope was given:** start with the highest-yield patterns across the
full open queue:
1. Title duplicates (`bd list --status open --json | jq -r '.[].title' | sort | uniq -d`). Note: this finds only byte-exact title collisions — semantic duplicates (same finding rephrased) won't appear here. Use it as the floor; cross-reference parent epics and `path:line` citations for richer dedup.
2. Beads with in-bead `Closed:`/`Fixed:` comments where status is still open.
3. Children of closed parent epics (a likely PR-review-finding cluster).
4. Architectural supersession (StaticAccessControl, eventStore/LISTEN, WatchSession, capability enforcer, WASM/Extism, etc.).
Build a candidate list before per-bead verification — pattern recognition is
cheaper than detailed reads.

**If a scope was given:** treat it as one of:
- An epic prefix (`holomush-wfza`, `5ayg`, etc.) — audit all open children.
- A label (`label:need-triage`, `label:pr-review-finding`) — audit beads carrying that label.
- A PR number (`pr:88`) — audit beads tagged with that PR's review finding label.
- A free-form filter — interpret it as a starting query.

**Operational note:** the `bd` database lives in the main repo's `.beads/`,
not in any worktree. If you're invoked from a worktree-local cwd you may
see "no beads database found" — `cd` to the repo root (or the path printed
by `jj root` then `realpath ../..` if in a worktree) and retry.

**Critical anti-patterns** (called out in your system prompt — restated for emphasis):

- Never close a bead based on an in-bead `Closed:`/`Fixed:` comment alone. Verify the cited fix in current code. The 2026-04-26 audit caught two false-fix cases (`wfza.21`, `wfza.62`) where sub-fix beads were closed but the actual code change never landed.
- Never run `task`, `make`, `go build`, or any other long build. You are an investigator using `bd show`, `Read`, `Grep`/`rg`, `Glob`, `gh pr view`.
- Never run mutating jj/git commands. Read-only only.
- Never write to project files other than your own report path under
  `.claude/agent-memory/bead-auditor/reports/`.

**On receipt:** the report IS the deliverable. The orchestrator (parent
Claude or the human) reviews it and executes closures via:

```
bd close <id> -r "<suggested closure comment from report>"
```

For FALSE-FIX and ambiguous KEEP entries:

```
bd label add <id> need-triage
bd note <id> "<finding from report>"
```

You do not execute these yourself.

**For large queues (>50 beads):** dispatch multiple bead-auditor instances
in parallel, each scoped to a distinct epic prefix. Each agent must use a
unique `<slug>` so its report path doesn't collide. Agents do NOT need
separate jj workspaces because they are read-only — same working copy is
fine.
