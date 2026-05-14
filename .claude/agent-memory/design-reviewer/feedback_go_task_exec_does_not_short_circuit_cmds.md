---
name: feedback_go_task_exec_does_not_short_circuit_cmds
description: A successful `exec other-task` inside one `- cmd:` entry of a go-task `cmds:` list does NOT prevent subsequent `- cmd:` entries from running. Same for bare `exit 0`. go-task spawns each `- cmd:` in its own subshell.
metadata:
  type: feedback
---

# go-task: `exec` inside one cmd does NOT short-circuit subsequent cmds

**Rule:** When a Taskfile task has multiple `- cmd:` entries under `cmds:`,
each entry runs in its own subshell. A successful `exec <other>` (or bare
`exit 0`) inside cmd1 terminates ONLY that subshell, not the parent `task`
process. go-task then proceeds to run cmd2. Designs that rely on
"cmd1 detects-and-execs to skip cmd2" are structurally broken on the happy path.

**Why:** Verified empirically 2026-05-14 with a fixture Taskfile:

```yaml
cmds:
  - cmd: |
      if [ "$X" = "1" ]; then
        exec sh -c 'echo replaced; exit 0'
      fi
  - cmd: echo "second cmd ran"
```

`X=1 task default` prints BOTH "replaced" AND "second cmd ran". The same
test with `exit 0` instead of `exec` gives the same result.

Only NON-ZERO exit from cmd1 short-circuits cmd2 — and even then, the
parent `task` process exits with go-task's wrapper code (201) rather than
the user's exit code (see `feedback_go_task_exit_code_wrapping.md`).

**How to apply:** When reviewing a spec that proposes a "detect-and-bail
out of full pipeline" pattern using two list entries in `cmds:`, flag it
as a blocking defect. The correct shape is one of:

- **Single `- cmd:`** containing both branches inside one shell body
  (the conditional `exec` either replaces the shell with the alternative
  task, or falls through to the fallback inline).
- **Different task structure** — e.g., separate tasks `pr-prep` and
  `pr-prep:internal-runner` where `pr-prep` is the dispatcher and the
  internal runner is what `pr-prep:run` calls today.

Seen 2026-05-14 in pr-prep-docs-fast-lane-design r2 §4.3.1: spec
proposed `[cmd: detect-and-exec-pr-prep:docs, cmd: flock-and-run-full]`;
empirical test confirmed both cmds run on docs-only diff, falsifying
the spec's central goal.
