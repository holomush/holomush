---
name: go-task wraps user exit codes by default; --exit-code required for passthrough
description: go-task collapses any non-zero user-cmd exit to its own 201 ("Command execution error") unless --exit-code/-x is passed; specs claiming literal-rc invariants need verification
type: feedback
---

go-task does NOT propagate user-cmd exit codes by default. Whenever a
`cmd:` returns non-zero, go-task itself exits with **201** ("Command
execution error") regardless of what the user's command exited with.
Source: context7 `/go-task/task` "Exit Codes" — codes 200-255 are
go-task-specific, 201 specifically means a sub-cmd failed. To pass
through the user's actual exit code, the operator must invoke
`task --exit-code <name>` (or `-x`).

**Verified empirically 2026-04-26 against go-task 3.50.0:**

```yaml
tasks:
  outer:
    cmds:
      - exit 42
```

`task outer` → exit 201. `task --exit-code outer` → exit 42.

This double-wraps when one Taskfile invokes `task` as a child via
`exec`/`sh -c`: each layer wraps non-zero rcs to 201 unless
`--exit-code` is passed at every layer.

**How to apply:** when reviewing specs that:

- Assert a literal exit-code passthrough invariant (e.g., "harness
  MUST exit 42 when inner exits 42")
- Assert a literal `[ "$status" -eq 1 ]` test row when the harness's
  internal failure path does `exit 1`
- Make a CLAUDE.md / operator-instruction commitment to invoke
  `task <name>` (not `task --exit-code <name>`) but separately
  promise exit-code passthrough

…flag the contradiction. The fix is either to (a) weaken the
invariant to "MUST exit non-zero" (which 201 satisfies), (b) add
`--exit-code` to the harness's inner `task` invocation AND update
operator instructions to use `task --exit-code <outer>`, or (c)
choose a sentinel in the high-100s (e.g., -E 75 for flock) that
go-task does NOT wrap, since flock's own exit codes flow through
the shell `rc=$?` capture path rather than through a child `task`
invocation.

Seen 2026-04-26 in pr-prep-concurrency-safety rev-3: I-6
(`propagates_exit_code`) asserted "Harness exits 42" with stub
exiting 42. Empirically the harness exits 201 because the inner
`task pr-prep:run` wraps the user's 42, the outer `task pr-prep`
then wraps the harness's `exit 42`. Test row `rejects_while_held`
similarly asserted "exits 1" for collision; actual is 201 (harness
does `exit 1` on the 75-branch but go-task wraps it).

Note: flock's own non-zero exits (75 EWOULDBLOCK via `-E 75`,
66 EACCES, etc.) are captured by the harness's `rc=$?` BEFORE
go-task sees them; the harness can branch on those values
correctly. Only exit codes that flow up through `cmd:` non-zero
return get wrapped.
