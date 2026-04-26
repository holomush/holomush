---
name: go-task internal:true semantics — CLI-blocked, hidden from --list-all
description: tasks marked internal:true cannot be invoked via the task CLI by ANY caller (including subshells inside Taskfile cmds), and are hidden from BOTH --list and --list-all
type: feedback
---

`internal: true` in go-task is enforced by go-task itself, not by the
shell. Any CLI invocation (`task <name>`) is short-circuited by go-task
with stderr `task: Task "<name>" is internal` and exit 202 — BEFORE any
cmd in the task runs. This includes invocation from inside a `sh -c '…'`
block dispatched from another Taskfile cmd, because that subshell is
spawning the `task` binary as a child process.

Internal tasks are reachable only via the YAML `task: <name>` keyword
(in-process call from another task).

Internal tasks are also hidden from BOTH `task --list` and
`task --list-all`. Verified empirically against this repo's existing
`license:run` (Taskfile.yaml:447, declared `internal: true`) — it does
not appear in `task --list-all` output.

**Why:** Spec `pr-prep-concurrency-safety` r2 designed a flock harness
that called `exec task pr-prep:run` from inside its own `sh -c`
subshell, with `pr-prep:run` declared `internal: true`. Empirically
(go-task 3.50.0) this fails: lock acquires, info file populates, then
exec returns 202 and the inner CI body never runs. The same spec also
included an invariant I-10 asserting "MUST appear in `task --list-all`"
which is structurally false for any internal task. Two independent
correctness defects rooted in the same misunderstanding.

**How to apply:** When reviewing a spec that uses `internal: true` on
a task, check ALL of:

1. Is the task ever invoked via `task <name>` (CLI form) anywhere in
   the spec? If yes, that invocation will fail with exit 202. The
   only valid caller forms are `- task: <name>` (YAML) or shelling
   out via `task: …` from another in-process task.
2. Does the spec assert the task appears in `task --list-all`? It
   doesn't. go-task hides internal tasks from `--list-all` too.
3. If the spec layers an env-var "bypass guard" cmd on top of
   `internal: true`, the guard is dead code — go-task's own gate
   protects against direct CLI invocation. The two protections do
   not stack; they are mutually exclusive.

**Workaround for "I want to call this task from a flock COMMAND
context":** drop `internal: true`. Use only the env-var bypass guard
for footgun protection. The guard becomes load-bearing and testable.
Accept that the task appears in `task --list-all` (typically
acceptable; users still see the warning if they invoke it directly
without the env var).
