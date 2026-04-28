---
name: Taskfile shell idioms must be verified against mvdan/sh, not /bin/sh
description: go-task uses mvdan.cc/sh/v3/interp not /bin/sh; many shell idioms fail silently or with non-obvious errors
type: feedback
---

go-task's inline `cmd:` blocks execute under `mvdan.cc/sh/v3/interp` (a
pure-Go shell interpreter), NOT `/bin/sh`. When reviewing specs that
embed shell idioms in a Taskfile `cmd:`, verify the idiom against
mvdan/sh's supported features — not your assumption of POSIX sh.

**Why:** Specs in this repo have repeatedly assumed full bash/POSIX
semantics in Taskfile cmds and shipped non-functional designs. A
specific case observed 2026-04-26: spec proposed
`exec 9>"$LOCK"; flock -n 9; task pr-prep:run` which empirically fails
because mvdan/sh's `Runner.redir` only supports redirections for
fds 0/1/2; `exec N>FILE` for N≥3 is rejected. Additionally, even if
fd 9 had opened, go-task's external-command path uses `os/exec.Cmd`
with only Stdin/Stdout/Stderr — `ExtraFiles` is never set, so higher
fds never propagate to subprocesses.

**How to apply:** When a spec uses any of the following inside a
Taskfile inline `cmd:` block, flag for empirical verification:

- `exec N>` or `exec N<` for N >= 3 (NOT supported)
- `coproc`, named pipes via process substitution `<(cmd)` (mvdan/sh
  support varies)
- Reliance on fd inheritance through `os/exec` boundaries (NOT
  supported — only fds 0/1/2 forward; populating ExtraFiles is not
  configurable from a Taskfile)
- `set -o pipefail` outside `set: [pipefail]` task config (works in
  multi-line but not across cmds)
- Background jobs that outlive the cmd (`&`, `wait`, job control)
- Signal handlers (`trap`)

**Fix recipe for fd-using idioms:** wrap in `/bin/sh -c '…'` so a
real shell handles the fd, OR (better) use the tool's own native
form — e.g., `flock COMMAND` instead of `exec 9>; flock -n 9`. The
canonical `flock(1)` invocation form `flock LOCKFILE COMMAND` opens
the fd inside the `flock` binary itself, sidestepping mvdan/sh
entirely.
