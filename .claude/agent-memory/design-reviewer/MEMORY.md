# design-reviewer agent memory

This file accumulates HoloMUSH-specific patterns of good and bad designs
discovered during adversarial design review. Entries are added by the agent
itself after completing a review.

Keep under 200 lines. Curate — don't hoard.

## Common spec weaknesses in this codebase

- [oops vs gRPC code conflation](feedback_oops_vs_grpc_code_conflation.md) — HoloMUSH specs often call oops codes "gRPC codes"; flag wording, do not block if pattern matches existing codebase convention
- [grpc FromError message rewrite on wrapped statuses](feedback_grpc_fromerror_message_rewrite.md) — `st.Message()` on a status reached via `errors.As` is the OUTER err.Error(); flag "verbatim" / "wire-equivalent" claims that ignore this
- **"Mirrors existing technique X" claims need verbatim verification.** When a spec says "use the same technique as `Y`", read both sides and diff them. Authors paraphrase or reconstruct from memory and produce structurally different (broken) variants. Seen 2026-04-25 in session-workspace-isolation: spec claimed to mirror `Taskfile.yaml:521-530` gowork's `.jj/repo` resolution, but the provided snippet computed a different path that failed when invoked from a worktree.
- **Shell-snippet error handling in spec docs is rarely verified.** Patterns like `var=$(cmd | tail -n 1) || return` (bash) and `set var (cmd | tail -n 1); or return $status` (fish) do NOT detect failure of `cmd` — the pipeline status is the LAST command's status. Run the snippet before believing it. Seen 2026-04-25.
- **Spec "verifiable by …" criteria with overly-broad jj revsets.** `jj log -r 'all()'` returns full history; for experiment-level verification prefer `mutable()` or scoped revsets. Not blocking but degrades the diagnostic value.
- **Stated rationale for shell idioms can be wrong even when the idiom works.** A spec may correctly include a `cd` or `cd && cmd` block but provide an incorrect *reason* (e.g., "needed because X requires Y" where Y is false). This is documentation rot risk, not a correctness blocker. Verify rationale claims by running the inner command without the `cd` to see if it really fails. Seen 2026-04-25 in session-workspace-isolation r3: spec claimed `cd "$MAIN_REPO"` was needed for `jj git fetch`, but `jj git fetch` works from any workspace because it resolves repo storage via `.jj/repo`.

## Interfaces and boundaries that recur

- **`.jj/repo` dir-vs-file**: in the main checkout (`default` workspace) it is a directory; in any other jj workspace it is a file containing a relative path back to the main repo. Anything that needs to know "am I in the default workspace" or "where is MAIN_REPO" should use this. Reference implementation lives at `Taskfile.yaml:521-530`.
- **SessionStart hook output**: plain stdout is concatenated as additional context. `bd prime` is the canonical example. JSON `hookSpecificOutput.additionalContext` is the alternative but no in-tree hook uses it.
- **`task` cannot mutate caller's shell `pwd`/env**: any spec that wants "after this task, your shell is in directory X" must be a shell function/wrapper, not a task target. Treat as a hard constraint when reviewing automation specs.
