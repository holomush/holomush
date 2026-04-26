# Resume Multi-Tab Session Isolation Plan — Single-Paste Directive

> **In a new Claude Code session, paste this entire document as your first
> message.** The session will load required skills, verify state, and
> resume autonomous execution at Task 10. No other setup required.

---

You are resuming a paused multi-task implementation of `holomush-9q8n` in
the HoloMUSH repo. Tasks 0-9 + 6.5 are complete (locally committed via
jj, not pushed). Task 10 is next. Your job: load context, then drive
the `/loop` skill through Tasks 10-25 with one task per iteration,
each gated by an adversarial code review.

## Step 0 — Load required skills (do this FIRST)

Before any other action, invoke the following skills via the `Skill`
tool, in order. Each loads stateful conventions you'll rely on:

1. **`jj:jujutsu`** — this repo is jj-colocated. The skill loads the jj
   workflow conventions you'll use on every iteration (`jj new`,
   `jj diff -r @`, `jj restore --from @-`, targeted `jj rebase -r`,
   bookmark management, etc.). MUST be loaded before any VCS command.
2. **`beads`** — you'll need `bd remember` / `bd memories` to read
   prior-session memory and persist findings. Run `bd prime` once after
   the skill loads to refresh the workflow context.
3. **`superpowers:using-superpowers`** — establishes the Skill-tool
   invocation discipline. Probably already auto-loaded at session
   start, but invoke explicitly to confirm.

After those three load, recall the prior session's notes:

```bash
bd memories holomush-9q8n
```

You should see an entry confirming Tasks 0-9 + 6.5 done, workspace at
`.worktrees/triage`.

## Step 1 — Verify state

Run from the workspace cwd:

```bash
cd /Users/sean/Code/github.com/holomush/.worktrees/triage
jj log -r '::@ ~ ::main@origin' --no-graph -T 'change_id.short() ++ " " ++ description.first_line() ++ "\n"'
```

You should see exactly **14 commits** ahead of `main@origin`:

| jj id | Plan task | Description |
| --- | --- | --- |
| `mw` | (spec) | docs(spec): multi-tab session isolation design v3 |
| `mu` | (plan) | docs(plan): multi-tab session isolation implementation plan v4 |
| `oy` | Task 0 | test(web): extend mockCoreClient with atomic call counters |
| `klp` | Task 1 | feat(proto): extend CheckPlayerSessionResponse |
| `rt` | Task 2 | feat(proto): extend WebCheckSessionResponse |
| `mvr` | Task 3 | feat(proto): add error_code + current_player_name to web auth responses |
| `ql` | Task 4 | feat(core): populate player_id, is_guest, characters in CheckPlayerSession |
| `oo` | Task 5 | feat(web): forward player_id, is_guest, characters from CheckPlayerSession |
| `op` | Task 6 | feat(web): add cookie-collision gate helper |
| `um` | Task 6.5 | fix(grpc): preserve auth-failure semantics across CheckPlayerSession gRPC boundary |
| `qx` | Task 7 | feat(web): refuse WebCreateGuest with ALREADY_AUTHENTICATED when cookie valid |
| `uss` | Task 8 | feat(web): refuse WebAuthenticatePlayer with ALREADY_AUTHENTICATED when cookie valid |
| `ms` | Task 9 | feat(web): refuse WebCreatePlayer with ALREADY_AUTHENTICATED when cookie valid |
| `mzz` | (this doc) | docs(plan): mid-flight handoff |

If the count or descriptions don't match, **STOP** and surface — state
has drifted, do NOT proceed.

`jj st` should show no working-copy changes (the handoff doc IS this
file, already committed as `mzz`).

Refresh `go.work` once: `task gowork`. (Each new workspace operation
can stale gopls; harmless to refresh.)

## Step 2 — Read what's done and what's next

**Phase 1 (Tasks 0-5): proto + core handler infrastructure** — done.
Proto messages extended, core `CheckPlayerSession` populates the new
fields, gateway `WebCheckSession` forwards them.

**Phase 2 (Tasks 6-9 + 6.5): gateway gates** — done. Cookie-collision
gate (`checkCookieCollision`) is wired into all three guarded RPCs
(`WebCreateGuest`, `WebAuthenticatePlayer`, `WebCreatePlayer`). The
6.5 task fixed a production bug where the gRPC client was wrapping all
errors as `RPC_FAILED`, hiding the inner auth-failure code from the
gate predicate.

**Phase 3 (Tasks 10-15) starts at Task 10.** TypeScript / Svelte
client work. Verification commands change:

| Layer | Verification |
| --- | --- |
| TS unit tests | `cd web && npx vitest run [pattern]` |
| TS type-check | `cd web && npm run check` |
| TS lint | `cd web && npm run lint` |
| Go (per-task default) | `task test -- ./internal/web/...` |
| Component UI assertions | **Playwright (Task 23)** — `@testing-library/svelte` is NOT installed |

Remaining tasks: 10, 11, 12, 13, 14, 15, 15.5, 15.6, 16, 17, 18, 19,
20, 21, 22, 23, 24 (`task pr-prep`), 25 (final code-reviewer + push
prep). 17 tasks to land.

## Step 3 — Lessons from the prior session (carry these into the loop)

1. **`task fmt` reformats unrelated files.** Implementers don't
   reliably revert. Bake the revert protocol into every implementer
   prompt and re-verify `jj diff --name-only` at the controller level
   before dispatching the reviewer.
2. **gopls workspace diagnostics are noise.** `not in workspace` and
   `undefined: Handler` / `undefined: rpcTimeout` show up after every
   Go-edit task. Not real compile errors — `task test` and
   `task lint:go` always pass. Run `task gowork` periodically to
   refresh; otherwise ignore.
3. **`mockCoreClient` is hand-rolled, not mockery.**
   `internal/web/handler_test.go:36-200` defines it. Tests construct
   `client := &mockCoreClient{...}; h := NewHandler(client)`. No
   `EXPECT()` / `.Once()` patterns. Counter fields are `atomic.Int32`
   (Task 0); read via `.Load()`, write via `.Add(1)`.
4. **Adversarial code-review per task is high-value.** Task 6's review
   caught a production-blocking bug (Task 6.5 was the fix). Without
   that review the gate would have shipped broken in production despite
   passing unit tests. Run the reviewer on EVERY substantive task.
5. **Plan-doc minor drift exists.** Plan-doc text near line 3287 says
   "checkSessionReq added in Task 0; referenced by Tasks 5-9", but
   Task 7 dropped that field as part of the `-race` fix. No tasks
   actually use it. If you see the field in code blocks during plan
   reads, ignore the references — the field is gone.

## Step 4 — Run the loop

Once Steps 0-3 are done, invoke the `loop` skill with the directive
below as the args. The loop will read it as `/loop` dynamic-mode input,
run Task 10 immediately, then `ScheduleWakeup` itself for each
subsequent iteration until Task 24 lands clean (or a stop condition
fires).

**Invoke this exactly** (using the `Skill` tool):

```text
Skill: loop
args: <the entire BEGIN/END block below, exclusive of marker lines>
```

```text
=== BEGIN LOOP PROMPT ===
Execute the next pending task in the multi-tab session isolation plan, end-to-end with adversarial code review scoped to that task's diff.

## Context — read every loop iteration

Workspace: `/Users/sean/Code/github.com/holomush/.worktrees/triage` (jj-colocated)
Tracker: `holomush-9q8n` (P1)
Spec: `docs/superpowers/specs/2026-04-25-multi-tab-session-isolation-design.md`
Plan: `docs/superpowers/plans/2026-04-25-multi-tab-session-isolation.md`
Handoff: `docs/superpowers/plans/2026-04-25-multi-tab-session-isolation-HANDOFF.md` (read this if any state question is unclear)

## State note (mid-flight injection)

Task 6.5 was injected as `fix(grpc): preserve auth-failure semantics...` between Tasks 6 and 7. Task 7 also extended scope by 1 file (`internal/web/handler_test.go` — dropped unused `checkSessionReq` field that was racing under -race in the concurrent test). Plan order with injection:
0 → 1 → 2 → 3 → 4 → 5 → 6 → 6.5 → 7 → 8 → 9 → 10 → 11 → 12 → 13 → 14 → 15 → 15.5 → 15.6 → 16 → 17 → 18 → 19 → 20 → 21 → 22 → 23 → 24 → 25.

`jj log` will show 14 commits ahead of main: spec + plan + Tasks 0/1/2/3/4/5/6/6.5/7/8/9 + handoff doc. **Task 10 is next.**

**Phase 3 starts here** — TypeScript / Svelte. Verification commands:
- TS unit tests: `cd web && npx vitest run [pattern]`
- TS type-check: `cd web && npm run check`
- TS lint: `cd web && npm run lint`
- `@testing-library/svelte` is NOT installed; rely on Playwright (Task 23) for UI assertions. Per-task Vitest tests stay focused on plain TypeScript.

Project conventions in `CLAUDE.md`: `task` not raw go/golangci-lint; `bd` for tracking; conventional-commits; SPDX headers; jj over git.

## State machine

Run `jj log -r '::@ ~ ::main@origin' --no-graph -T 'change_id.short() ++ " " ++ description.first_line() ++ "\n"' 2>&1` from the workspace cwd to enumerate commits between main and the current change. The most recent commit's description starts with `feat(...)`, `test(...)`, `chore(...)`, `fix(...)`, or `docs(...)` — task ordinals come from the plan order, not the message.

Identify the next pending task by scanning the commit log + matching against the plan's task list. If a Task-N commit description is present, that task is done.

## Per-iteration flow (one task)

1. **Identify the next task.** Read the plan section for that task. Extract the full task text with all steps and code blocks.
2. **Create an empty jj change for the implementation:** `jj new -m "<conventional-commits message for the task>"`. The message follows the plan's "Commit" step template for that task. Verify `jj st` shows no working-copy changes before dispatching.
3. **Dispatch implementer** as a fresh `general-purpose` subagent. Give it:
   - The full task text inlined (do NOT make it read the plan file).
   - Workspace path, jj convention reminder, project conventions URL.
   - Explicit "stay in scope" + "ask questions before starting if ambiguous" + report DONE / DONE_WITH_CONCERNS / NEEDS_CONTEXT / BLOCKED.
   - **Stronger scope-revert instruction:** Tell the implementer EXPLICITLY: "After running `task fmt`, run `jj diff --name-only` and confirm ONLY the files this task names appear. If others appear, run `jj restore --from @- <those files>` to revert them BEFORE reporting done. The controller will check this and re-do the cleanup if needed, but it is YOUR responsibility first. Past tasks have leaked these reformats; do not be the next."
   - For Task 15.6 specifically, add the controller note: "When you reach step 2, enumerate `web.CoreClient`'s full method list via `rg -n 'type CoreClient interface' internal/web/handler.go` and add a delegating method (or `errors.New(\"not implemented in test shim\")` for streaming Subscribe) for each before moving on to step 3."
4. **Handle implementer status:**
   - DONE → step 5
   - DONE_WITH_CONCERNS → read concerns; if material, address before review; otherwise note and proceed
   - NEEDS_CONTEXT → provide context, re-dispatch
   - BLOCKED → STOP THE LOOP, surface to user
5. **Verify scope before review.** Run `jj diff --name-only` and confirm only the task's named files. If extra files leaked through despite the implementer's check, run `jj restore --from @- <files>` to drop them.
6. **Run adversarial code-reviewer** (`subagent_type: code-reviewer`) on JUST this task's diff. Scope:
   - Workspace: same path
   - Change under review: `@` (the working-copy commit just produced)
   - Parent: `@-`
   - Tell the reviewer to read ONLY `jj diff -r @` (NOT cumulative `jj diff -r main..@`).
   - Inline the task's spec verbatim so the reviewer can match diff against intent.
   - Ask explicitly: spec compliance? scope creep? compile + test pass? lurking bugs in THIS diff? subtle naming? race-safety where relevant?
   - Verdict: READY / NOT READY.
7. **Handle reviewer verdict:**
   - READY → step 8
   - NOT READY → dispatch a fix subagent (NOT the original implementer — fresh context) with the reviewer's findings + the task spec + the diff. Re-run reviewer. Loop until READY. Hard cap 3 iterations; on the 4th, STOP and surface.
8. **Run task-appropriate verification** as a final smoke check. If either fails, treat as a NOT READY finding and loop:
   - For Go-only changes: `task test -- <touched packages>` and `task lint:go`
   - For TS-only changes: `cd web && npm run check && npm run lint && npx vitest run [pattern]`
   - For mixed changes: both
9. **NEVER push, NEVER `bd close`, NEVER amend a parent commit.** Only the current `@` is yours.
10. **Schedule the next iteration** via the loop's self-pacing. Pace based on task complexity:
    - Trivial mechanical tasks (proto edits, single-file refactors): ≤ 5 min between iterations.
    - Substantive tasks (Task 6/7/11/14/15.6): ≤ 15 min between iterations.
    - Stop iterating once Task 24 lands clean — at that point STOP and surface for human review of the full branch.

## Hard rules

- Each iteration touches at most ONE task. Do not batch tasks.
- Each iteration produces at most ONE jj commit (the implementer's; review fixes amend that commit, they don't create new ones).
- The reviewer reviews ONLY `jj diff -r @`, not cumulative diff.
- If a task's verification command (e.g. `task pr-prep` for Task 24) fails AND you cannot fix it in 3 iterations, STOP THE LOOP and surface.
- If `bd dolt pull` reveals a remote change, STOP THE LOOP and surface (we may have stale state).
- DO NOT modify the spec or plan unless a task explicitly asks for it.
- Memory rules from prior session: jj rebases must be `-r <change-id>` not bare `-d main`; do not edit/describe parent commits while a child commit is the working copy; do not skip `task pr-prep` ever.

## gopls noise (ignore)

Every Go-edit task triggers gopls `not in workspace` and `undefined: Handler` / `undefined: rpcTimeout` diagnostics. They are NOT real compile errors — `task test` and `task lint:go` always pass. Run `task gowork` periodically to refresh; otherwise ignore.

## mockCoreClient is hand-rolled, not mockery

`internal/web/handler_test.go:36-200` defines the mock. Tests construct `client := &mockCoreClient{...}; h := NewHandler(client)`. No `EXPECT()` / `.Once()` patterns. Counter fields are `atomic.Int32` (Task 0); read via `.Load()`, write via `.Add(1)`.

## Stop conditions

- Task 24 (`task pr-prep`) lands clean → STOP, summary report.
- Implementer BLOCKED, or 3 review iterations don't converge → STOP, surface.
- Remote diverges → STOP, surface.
- Any production-code modification spans more than the task's named files → STOP, surface (likely scope creep).

Report each iteration as a one-line update via the user-visible reply: `Task N — DONE (<short summary>)` or `Task N — IN REVIEW (<round>)` or `Task N — STOP (<reason>)`.

When all tasks are complete and `task pr-prep` is clean, post a summary with the final commit list and stop the loop.
=== END LOOP PROMPT ===
```

## Final pre-resumption checklist

- [x] All 14 commits clean — `jj st` shows no working-copy changes
- [x] `bd remember` entry persisted via `bd dolt push`
- [x] Spec + plan unchanged from v3/v4
- [x] Workspace `.worktrees/triage` ready to resume
- [ ] Code NOT pushed to remote (deliberate — push happens after Task 24)
- [ ] `task pr-prep` not run yet (will run AS Task 24)

## If you (the new session) get confused

- Re-read the spec: `docs/superpowers/specs/2026-04-25-multi-tab-session-isolation-design.md` (~580 lines)
- Re-read the plan: `docs/superpowers/plans/2026-04-25-multi-tab-session-isolation.md` (~3300 lines — skim, don't memorize; jump to the section for the task you're about to do)
- Re-read the prior session memory: `bd memories holomush-9q8n`
- Re-read the prior code-reviewer reports: `ls .claude/agent-memory/code-reviewer/reports/2026-04-25-*` (each task's review is persisted)

If state has drifted (commit count off, descriptions wrong, repo dirty),
**STOP and surface to the user**. Do not auto-recover.
