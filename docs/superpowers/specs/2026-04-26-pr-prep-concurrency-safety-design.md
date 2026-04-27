# pr-prep Concurrency Safety

**Date:** 2026-04-26
**Status:** Design — IMPLEMENTED (rev-7; rev-6 was READY with design-reviewer verdict 2026-04-26 1114; rev-7 amendment 2026-04-27 incorporates six CodeRabbit-driven corrections on PR #2774 — see "Amendment Log" at the end of the document for details. No implementation changes; only spec wording.)
**Tracking bead:** `holomush-71zq`
**Related:** `Taskfile.yaml` `pr-prep` and `pr-prep:run` tasks; `Taskfile.yaml` `test:e2e` task's existing `holomush-e2e` compose-project pre-flight check (the late-failure mode this design addresses early); `docs/superpowers/specs/2026-04-25-session-workspace-isolation-design.md` (multi-agent workspace model that motivates this work); `CLAUDE.md` MUST rule that `task pr-prep` runs before every push

## Problem

`task pr-prep` is the project's mandatory pre-push gate: it mirrors every CI job (schema check, license check, plugin builds, lint, format, unit, integration, E2E in Docker) and is required before any PR push per CLAUDE.md. A full run takes 5–15 minutes and is CPU- and Docker-heavy.

HoloMUSH is developed primarily by autonomous AI agent sessions. Per the session-isolation rule, each agent operates in its own jj workspace. When two or more agents reach the "ready to push" step concurrently, they each invoke `task pr-prep` from their own workspace.

### Observed failure

Today's behavior on collision is a late, noisy failure:

1. Agent A acquires no lock (none exists) and runs `task pr-prep`. After a few minutes it reaches the E2E step and starts the `holomush-e2e` Docker compose project.
2. Agent B, in a different workspace, invokes `task pr-prep` shortly after.
3. Agent B's run proceeds for several minutes — racing on schema regeneration (`go generate ./internal/plugin/`, writes `schemas/plugin.schema.json`), plugin builds (`build/plugins/*`), lint, format, unit, and integration tests. These are workspace-local, so cross-workspace runs do NOT corrupt files; same-workspace runs (rare under the isolation rule, but possible) DO race on these paths.
4. Agent B reaches the E2E step. `Taskfile.yaml:135-139` checks the `holomush-e2e` compose project, finds it running, and emits:

   ```text
   ERROR: E2E infrastructure already running (project: holomush-e2e).
   Stop it with: docker compose -p holomush-e2e down -v
   ```

   Agent B's invocation exits non-zero (201, since go-task wraps the `exit 1` in the pre-flight check) — but only after spending minutes burning CI-equivalent compute. Agent B has no early signal that its work was doomed from the start.
5. The two runs also compete for Docker engine capacity (memory, CPU, ports) and may interfere with each other's results even when only one E2E stack runs.

The user-visible failure mode an operator (human or agent) sees:

- A `pr-prep` invocation that runs for 5+ minutes and then fails with a Docker-compose-collision error
- No actionable identity for the holder ("project: holomush-e2e" tells you the compose project, not which workspace, PID, or session owns it)
- Wasted developer/agent time, sometimes wasted Docker layers, and a confused recovery path ("did I crash, or did someone else's run kill mine?")

## Goals

1. **MUST serialize `task pr-prep` machine-locally.** A second invocation that maps to the same lockfile MUST detect the in-flight run before doing any meaningful work and MUST exit non-zero with a clear, actionable message. The lockfile path is `${TMPDIR:-/tmp}/holomush-pr-prep/lock`, so the effective scope is platform-dependent: per-user on macOS (where `$TMPDIR` is automatically per-user under `/var/folders/<hash>/T/`), and machine-global on Linux (where `$TMPDIR` is typically unset and `/tmp` is shared). The per-user scope on macOS is a side effect of the canonical `${TMPDIR}` choice, not a designed cross-user-isolation property — see Non-goal 5.
2. **MUST be crash-safe.** If the holding process crashes (any signal, including SIGKILL, OOM, kernel panic recovery), the lock MUST release automatically without manual cleanup. This rules out PID-file approaches with manual liveness checking.
3. **MUST surface holder identity in the collision error.** The error MUST include enough information for the operator (human or agent) to find the running instance: PID (kill target), workspace path, start timestamp, lockfile path.
4. **MUST be portable across macOS and Linux** with a single mechanism — no platform-specific branching in the Taskfile.
5. **MUST keep the existing `pr-prep` body unchanged.** The 9-step body is correct and covers all CI jobs; this design adds a lock harness around it without modifying the steps.
6. **MUST keep the entry point unchanged.** Operators and agents continue to invoke `task pr-prep` exactly as before. CLAUDE.md's existing "MUST run `task pr-prep`" rule does not change.
7. **SHOULD be testable in isolation** without needing to run the real `pr-prep` body during the test (which would be slow and require Docker).

## Non-goals

1. **True parallelism.** This design does not enable two `pr-prep` invocations to run concurrently. The user explicitly chose serialization with clear messaging over the complexity of parallelizing all shared resources (compose project namespacing, per-run coverage outputs, etc.).
2. **Step-aware collision messaging.** The error message includes _that_ a run is in progress and _when_ it started, not _which_ of the 9 steps it is currently in. Heartbeat-style step tracking was considered and rejected as low-value, high-surface-area.
3. **Block-and-wait behavior.** The colliding invocation fails fast (non-zero exit, no blocking wait) rather than waiting in a loop. Autonomous agents prefer a clear early signal over a long blocking wait. A `--wait` flag is out of scope.
4. **`--force` override.** Stale locks self-clean when the kernel detects holder death; no override flag is needed for the common case. Recovery from a wedged process tree is process-based — kill all PIDs holding the lockfile open via `kill -9 $(lsof -t "$LOCK")` (see `site/docs/contributing/pr-prep.md` "Killing a wedged holder"). Lockfile-deletion is NOT a valid escape hatch: with advisory `flock(2)`, removing the path while a holder still has its fd open lets a new process create a new inode at the same path and acquire its own (independent) lock — both processes then "hold the lock" simultaneously, defeating the gate.
5. **Cross-user serialization on a single machine.** On macOS, `${TMPDIR}` is per-user (`/var/folders/<hash>/T/`), so user A's lockfile is invisible to user B. The Docker compose project name `holomush-e2e` is engine-global and would still collide across users if two users happened to run `pr-prep` simultaneously. This is acceptable: HoloMUSH dev machines are single-user in practice; the cross-user collision class is rare and the existing `holomush-e2e` pre-flight check still catches it (just with the same late-failure UX). Cross-user lock unification is out of scope.
6. **Locking other long-running tasks** (`test:e2e`, `test:int`). The pattern is general, but YAGNI: `pr-prep` is the one mandatory pre-push gate. Other tasks MAY adopt the same idiom later if collisions become a real problem.
7. **Hardening against deliberate bypass.** A motivated user (or misbehaved agent) can always invoke the inner task body directly. We design for honest-mistake protection (clear naming, soft warnings), not adversarial bypass prevention.

## Design

### Lock Primitive: `flock(1)` as Command Entrypoint

`flock(1)` (from the `flock` Homebrew formula on macOS — verified to be in `homebrew-core`, no tap needed; util-linux on Linux) provides kernel-managed advisory file locking. The kernel automatically releases the lock when the holding process's file descriptor closes — including when the process dies for any reason. This is why it is preferred over a PID-file-with-liveness-check approach: the latter has irreducible PID-reuse race windows that kernel-managed locks avoid by construction.

**The lock fd MUST be owned by `flock(1)` itself, not by a shell `exec N>FILE` redirection.** The original revision of this spec proposed an `exec 9>"$LOCK"; flock -n 9; task pr-prep:run` idiom. Empirical testing during design review (2026-04-26) demonstrated that this does not work in this project's toolchain:

1. go-task uses an embedded Go shell interpreter (`mvdan.cc/sh/v3/interp`) that rejects `exec N>FILE` redirections for `N≥3`. The first line of the proposed harness fails with `exec failed: 1`.
2. Even if the redirection worked, go-task's external-command path uses `os/exec.Cmd` with only `Stdin/Stdout/Stderr` forwarded — `ExtraFiles` is not populated. So fd 9 would not be inherited by the inner `task pr-prep:run` subprocess; the kernel would release the lock at the moment the inner task started, defeating the design.

The corrected idiom invokes `flock(1)` as the parent of the inner work. `flock` opens the lock fd itself (in its own address space) and forks/execs its child command with the fd held. The lock is held until the entire process tree under `flock` exits. This is the canonical `flock(1)` usage and avoids both problems above.

### Lock File Location: `${TMPDIR:-/tmp}/holomush-pr-prep/`

A dedicated directory under `${TMPDIR:-/tmp}` holds two files:

| File   | Purpose                                                                                               |
| ------ | ----------------------------------------------------------------------------------------------------- |
| `lock` | The flock target file. Empty file; its content is never read.                                         |
| `info` | Plain-text metadata written by the holder after acquisition; read by colliders for the error message. |

`${TMPDIR:-/tmp}` is the canonical POSIX runtime-files location:

- On macOS, `$TMPDIR` is automatically per-user (`/var/folders/<hash>/T/`), giving cross-user isolation as a side effect (see Non-goal 5 for the trade-off).
- On Linux, `/tmp` is world-writable with the sticky bit; the directory namespace is sufficient.
- `/var/run/` was considered and rejected: on macOS it is root-owned and not user-writable; on Linux it is a tmpfs that varies in availability.
- NFS-backed `${TMPDIR}` is **out of scope**: `flock(2)` semantics on NFS are undefined and the spec assumes a local filesystem (see Failure Modes table).

### Lock Harness: `flock COMMAND` Inside a Single Taskfile `cmd:`

Empirically verified working pattern (POC executed 2026-04-26 against this project's go-task version, see "Empirical Verification" below):

```yaml
pr-prep:
  desc: Run all CI checks locally before pushing (mirrors ALL CI jobs) [serialized via flock]
  preconditions:
    - sh: command -v flock >/dev/null 2>&1
      msg: "flock(1) is required. Install with 'brew install flock' (macOS) or 'apt install util-linux' (Linux)."
  cmds:
    - cmd: |
        set -euo pipefail
        LOCK_DIR="${TMPDIR:-/tmp}/holomush-pr-prep"
        mkdir -p "$LOCK_DIR"
        LOCK="$LOCK_DIR/lock"
        INFO="$LOCK_DIR/info"
        rc=0
        # flock owns the lock fd in its own address space. -n is non-blocking;
        # -E 75 makes flock exit 75 on EWOULDBLOCK (vs 1, which is also a
        # valid inner-task exit code) so we can distinguish "lock busy" from
        # "inner task failed". The sh -c subshell exports the bypass env var
        # (the inner task would otherwise refuse to run via the footgun guard),
        # writes metadata, then execs the inner task. exec means the subshell's
        # PID becomes the inner task's PID — so the PID written to info is the
        # right kill target.
        flock -n -E 75 "$LOCK" sh -c '
          export HOLOMUSH_PR_PREP_BYPASS_LOCK=1
          printf "pid=%s\nworkspace=%s\nstarted_at=%s\n" \
            "$$" "$2" "$(date -u +%FT%TZ)" > "$1"
          exec task pr-prep:run
        ' _ "$INFO" "$PWD" || rc=$?
        if [ "$rc" -eq 75 ]; then
          {
            echo "ERROR: another pr-prep is running on this machine."
            if [ -s "$INFO" ]; then
              echo
              sed 's/^/  /' "$INFO"
            fi
            echo
            echo "  Lock file: $LOCK"
            echo "  Wait for it to finish, or kill the holder PID if wedged."
          } >&2
          exit 1
        fi
        if [ "$rc" -ne 0 ]; then
          # flock returned a non-zero, non-75 code (e.g., EACCES on the
          # lockfile, or a flock-internal error). flock(1) prints its own
          # diagnostic to stderr; we just propagate the code with a hint.
          echo "ERROR: lock acquire failed unexpectedly (rc=$rc); see flock(1) output above." >&2
          exit "$rc"
        fi

pr-prep:run:
  # No `desc:` and no `internal: true`. Without `desc:`, the task is hidden
  # from `task --list` (the default operator view). It IS visible in
  # `task --list-all` (intentional — contributors should be able to discover
  # it). `internal: true` is NOT used because go-task refuses to run internal
  # tasks invoked via the `task` CLI from any caller, including the harness's
  # own `exec task pr-prep:run`. The footgun protection is the env-var guard
  # below, which fires on direct CLI invocation.
  cmds:
    - cmd: |
        if [ "${HOLOMUSH_PR_PREP_BYPASS_LOCK:-}" != "1" ]; then
          echo "ERROR: pr-prep:run is the unlocked inner body of pr-prep." >&2
          echo "       Do NOT invoke it directly — it skips the concurrency lock and" >&2
          echo "       will race with any other pr-prep on this machine." >&2
          echo "       Use 'task pr-prep' instead." >&2
          echo "       To intentionally bypass (e.g. CI, debugging), set" >&2
          echo "       HOLOMUSH_PR_PREP_BYPASS_LOCK=1." >&2
          exit 1
        fi
    # ... existing 9 steps from current pr-prep, verbatim, no other changes ...
```

**Key properties of this idiom:**

- `flock` is the lock-fd owner. The fd lives in `flock`'s address space, never in a shell.
- The `sh -c '…'` subshell receives `$INFO` as `$1` and `$PWD` as `$2` via positional parameters — this avoids variable-expansion ambiguity between the outer cmd's interpreter and the inner sh.
- `export HOLOMUSH_PR_PREP_BYPASS_LOCK=1` inside the subshell tells the inner `pr-prep:run` task that this invocation is the legitimate locked path — required because the inner task's footgun guard otherwise refuses to run.
- `exec task pr-prep:run` replaces the subshell with the `task` binary, keeping the same PID. The PID written to `info` (`$$` evaluated in the subshell) is therefore the kill target an operator should target if a wedged run needs to be terminated. (Note: `task pr-prep:run` is not declared `internal: true` — go-task refuses to invoke internal tasks via the `task` CLI from any caller, including the harness's own `exec`. See §"Why this avoids the issues from prior revisions".)
- `-E 75` makes `flock` exit 75 on lock-busy (vs 1, which the inner task could legitimately exit with). The harness uses this to print the collision error only on the busy case.
- The harness explicitly handles three cases of `flock`'s exit code: `0` (inner task succeeded — passes through), `75` (lock busy — collision error path), or any other non-zero (`flock`-internal error such as EACCES on the lockfile — propagates the code with a generic diagnostic).
- CI runs `task pr-prep:run` directly with `HOLOMUSH_PR_PREP_BYPASS_LOCK=1` (the lock is meaningless on a fresh CI runner) and the bypass is logged in the CI workflow for auditability.

### Why this avoids the issues from prior revisions

| Issue from prior reviews                                                                     | Resolution (current)                                                                                                                                                                                      |
| -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **rev-1:** `exec 9>FILE` rejected by go-task's mvdan/sh                                      | Eliminated. `flock(1)` opens the fd internally; no shell `exec N>` is used.                                                                                                                              |
| **rev-1:** fd 9 not forwarded to `task pr-prep:run` subprocess                               | Eliminated. `flock` owns the fd and holds it for its child's entire lifetime; child fd inheritance is not used.                                                                                          |
| **rev-1:** Holder PID ambiguity (`$$` of outer shell vs inner task)                          | Resolved. `exec task pr-prep:run` keeps the subshell's PID, so the PID written to `info` belongs to the inner task itself. Empirically verified — info `pid=98511` matched inner task `$$=98511`.       |
| **rev-1:** `pr-prep:run` direct invocation as a footgun                                      | Resolved by two layers: (a) `pr-prep:run` has no `desc:`, so it is hidden from the default `task --list` operator view; (b) the first cmd of `pr-prep:run` refuses to run unless `HOLOMUSH_PR_PREP_BYPASS_LOCK=1` is set, with a clear error pointing operators to `task pr-prep`. |
| **rev-1:** Empty-info race window between acquire and metadata write                         | Documented in Failure Modes table. The `[ -s "$INFO" ]` guard in the collision-error path falls through gracefully when info is unwritten.                                                               |
| **rev-2:** `internal: true` blocks the harness's own `exec task pr-prep:run` (exit 202)      | Eliminated. `pr-prep:run` is NOT marked `internal: true` (rev-2 used it for footgun protection but it broke the harness's CLI invocation). Footgun protection moved to the no-`desc:` + env-var-guard combination above. |
| **rev-2:** Inner task's bypass guard would block the harness's own legitimate invocation    | Resolved. The harness's `sh -c` subshell explicitly `export HOLOMUSH_PR_PREP_BYPASS_LOCK=1` before `exec task pr-prep:run`.                                                                              |
| **rev-2:** Failure-modes table promised an EACCES-handler branch the harness didn't have    | Resolved. The harness now has a `[ "$rc" -ne 0 ]` catch-all branch that propagates non-75 non-zero exit codes with a diagnostic.                                                                          |
| **rev-2:** I-10 asserted `task --list-all` shows internal tasks (false — it hides them)      | Resolved by structural change. The new design hides `pr-prep:run` from `task --list` only (via no `desc:`), so I-10 is rewritten to assert the empirically-true behavior.                                |

### Empirical Verification

A proof-of-concept Taskfile exercising this **exact production layout** (no `desc:` on `pr-prep:run`, no `internal: true`, env-var bypass guard, env-var exported in the harness's `sh -c`) was executed during design (2026-04-26) on the maintainer's macOS machine with `flock 0.4.0` from `/opt/homebrew/bin/flock` and go-task 3.50.0. Five scenarios were verified:

1. **Solo run (no prior holder):** `flock -n -E 75 …` acquired; bypass env var was set; info file populated; inner task ran to completion; harness exited 0. PID preservation confirmed (info `pid=98511` matched inner task `echo` output `pid=98511`).
2. **Direct invocation of `pr-prep:run` without env var:** failed at the bypass guard with the documented error message; exit 201 (go-task wraps the cmd's `exit 1` to its "Command execution error" code).
3. **Direct invocation of `pr-prep:run` with `HOLOMUSH_PR_PREP_BYPASS_LOCK=1`:** ran the inner body normally; exit 0.
4. **`task --list` and `task --list-all`:** `pr-prep:run` was absent from `task --list` (no `desc:`) and present in `task --list-all`. This is the intended UX — operators see only `pr-prep`; contributors using `--list-all` can discover `pr-prep:run`.
5. **Concurrent collision:** background holder with 4s inner sleep; foreground second invocation exited 201 within ~1s (the harness's internal `exit 1` on the lock-busy branch is wrapped by go-task), printed `COLLIDED`, included the holder's PID/workspace/timestamp from `info`.

The exit-code wrapping in scenarios 2 and 5 is go-task's documented behavior (every non-zero user-cmd exit becomes 201 unless `task --exit-code` is used). This is acceptable: the contract is "exit non-zero on failure" plus the human-readable stderr message; the literal exit code is informational only. See I-6.

The POC is preserved at `/tmp/flock-poc-v3/Taskfile.yaml` for the duration of design review. The earlier rev-1 and rev-2 POCs (`/tmp/flock-poc/`, `/tmp/flock-poc-rev2/`) are kept for regression reference but **do not exercise the production layout** and MUST NOT be used as evidence for any future revision. The implementation plan MUST include equivalent automated tests using the bats fixture described in §"Test Plan".

### Setup Change

The `setup` task's `brew install` line gains `flock`:

```yaml
setup:
  cmds:
    - brew install golangci-lint gofumpt lefthook actionlint goreleaser dprint cocogitto rumdl yamlfmt binaryen gotestsum flock
```

`flock` is in `homebrew-core` (verified via `brew info flock`: source `https://github.com/Homebrew/homebrew-core/blob/HEAD/Formula/f/flock.rb`), so no `brew tap` is required. CI Linux runners already have `flock` via the base image's util-linux. No CI workflow change is required for the lock itself, but CI MUST set `HOLOMUSH_PR_PREP_BYPASS_LOCK=1` if it directly invokes `pr-prep:run` (CI is single-tenant per job; the lock is not useful there and the bypass guard would otherwise block CI).

### Documentation Deliverables

1. **New file `site/docs/contributing/pr-prep.md`** — short page covering: what `pr-prep` does (mirrors CI), when to run it (before every push), the concurrent-run behavior (collision error format and process-tree-based recovery), and the sanctioned bypass mechanism (`HOLOMUSH_PR_PREP_BYPASS_LOCK=1` for CI/debugging contexts where the lock is intentionally skipped). Reachable from `site/docs/contributing/index.md`.
2. **`site/docs/contributing/pr-guide.md`** — gains a one-paragraph callout that links to `pr-prep.md` for collision recovery, since pr-guide is the workflow doc operators read first.
3. **`CLAUDE.md`** — the existing "MUST run `task pr-prep`" rule and the "Landing the Plane" reference both gain a single sentence noting that pr-prep is now serialized and pointing to the contributing doc for collision behavior.

These updates are PR-blocking acceptance criteria. The implementation plan MUST schedule them as part of the same PR, not follow-up beads.

## Numbered Invariants

Each invariant is enforced by a named test in the bats suite (see "Test Plan" below). The meta-test `all_invariants_have_tests` (a named `@test` block inside `scripts/tests/pr-prep-lock.bats`) MUST verify that every invariant ID (`I-1` … `I-10`) appears in a test name in `pr-prep-lock.bats`.

| #    | Invariant                                                                                                                                                        | Enforcing test            |
| ---- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------- |
| I-1  | A first invocation of `pr-prep` on an idle machine MUST acquire the lock and run the inner body to completion.                                                   | `acquires_when_idle`      |
| I-2  | A second invocation while the first is in flight MUST exit non-zero without running any inner step.                                                              | `rejects_while_held`      |
| I-3  | The collision error MUST be written to stderr and MUST contain the holder's PID, workspace path, start timestamp, and the lockfile path.                        | `error_includes_metadata` |
| I-4  | When the holder process exits normally, the next invocation MUST acquire the lock successfully.                                                                  | `releases_on_normal_exit` |
| I-5  | When the holder process is killed by SIGKILL, the next invocation MUST acquire the lock successfully within a bounded time (≤ 2 seconds) without manual cleanup. | `releases_on_sigkill`     |
| I-6  | The harness MUST exit non-zero whenever the inner body fails OR the lock is busy. Specific exit codes are not contractual: go-task wraps any user-cmd non-zero exit to its own "Command execution error" code (201 in go-task 3.x). The internal `flock -E 75` is a protocol between `flock(1)` and the harness for distinguishing lock-busy from inner-task-fail when constructing the error message; it is NOT user-facing. | `nonzero_on_failure`      |
| I-7  | When `flock(1)` is not on `PATH`, `task pr-prep` MUST fail at the precondition stage with a message naming the install command for both macOS and Linux.        | `precondition_message`    |
| I-8  | The lock acquire MUST be non-blocking (`flock -n`); the colliding invocation MUST exit within 2 seconds regardless of how long the holder still has to run.     | `non_blocking_acquire`    |
| I-9  | `task pr-prep:run` invoked directly without `HOLOMUSH_PR_PREP_BYPASS_LOCK=1` MUST refuse to run, exit non-zero, and print a message naming the bypass env var.  | `bypass_guard_blocks`     |
| I-10 | `pr-prep:run` MUST NOT appear in `task --list` (default operator view). It MAY appear in `task --list-all`. The Taskfile MUST NOT mark it `internal: true` (which would break the harness's `exec task pr-prep:run`). The contract is enforced primarily at the YAML level via `yq` (robust against go-task version drift); the `task --list` check is a supplementary observed-behavior assertion. | `pr_prep_run_hidden_from_list` |

## Test Plan

Tests use [bats](https://bats-core.readthedocs.io/) (the project already lists `bats-testing-patterns` as a known skill). Test file: `scripts/tests/pr-prep-lock.bats` (alongside the existing `scripts/tests/test_*.py` Python tests). The bats runner is wired as `task test:bats` and runs as the first step of `pr-prep:run` in production `Taskfile.yaml`; this section documents the strategy and invariants.

The tests do NOT invoke the real `pr-prep:run` body. Instead they exercise the locking idiom against a stub fixture Taskfile that defines:

- A test-only `pr-prep` task identical to the production one but invoking `pr-prep:stub` instead of `pr-prep:run`
- A test-only `pr-prep:run` task identical to the production one (for I-9 and I-10) — same bypass guard, but minimal body
- `pr-prep:stub` — a configurable inner task that sleeps, exits with a configurable code, or emits a marker file based on env vars (`STUB_SLEEP`, `STUB_EXIT`, `STUB_MARKER`)

This isolates lock semantics from the real CI body (which is slow and requires Docker).

### Test scenarios

| Test                      | Setup                                                                                          | Assertion                                                                                            |
| ------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `acquires_when_idle`      | No prior holder; stub exits 0                                                                  | Harness exits 0; stub's marker file present                                                          |
| `rejects_while_held`      | Spawn holder with `STUB_SLEEP=30` in background; poll `[ -s "$INFO" ]` until populated         | Second invocation exits non-zero within 2s; stderr matches "another pr-prep". (Empirically the exit code is 201 under go-task 3.x — the assertion is structural: `[ "$status" -ne 0 ]`.) |
| `error_includes_metadata` | As above                                                                                       | Stderr contains `pid=<holder-pid>`, `workspace=<expected-path>`, `started_at=<RFC3339>`, `Lock file:` |
| `releases_on_normal_exit` | Holder runs `STUB_SLEEP=0.1` and exits 0; sequential second invocation                         | Second invocation exits 0                                                                            |
| `releases_on_sigkill`     | Holder runs `STUB_SLEEP=30`; capture `info`'s PID; SIGKILL it; poll for release with 2s budget | New `flock -n` against the lockfile succeeds within 2s of SIGKILL                                    |
| `nonzero_on_failure`      | Stub configured with `STUB_EXIT=42`                                                            | Harness exits non-zero. (Empirically 201; assertion is structural: `[ "$status" -ne 0 ]`. Specific code is not contractual per I-6.) |
| `precondition_message`    | Run with `PATH=/usr/bin` (no `flock`)                                                          | Exits non-zero; stderr contains `brew install flock` and `apt install util-linux`                    |
| `non_blocking_acquire`    | Holder with `STUB_SLEEP=30`; time the second invocation                                        | Second invocation completes within 2s (well under the 30s sleep)                                     |
| `bypass_guard_blocks`     | Invoke `task pr-prep:run` directly without env var set                                         | Exits non-zero (empirically 201, per go-task wrapping); stderr contains `HOLOMUSH_PR_PREP_BYPASS_LOCK` |
| `pr_prep_run_hidden_from_list` | Run `task --list` against the production Taskfile; also assert at the YAML level that `pr-prep:run` exists but has no `desc:` and no `internal: true` (via `yq`) | `pr-prep:run` MUST NOT appear in `task --list` output. YAML-level: `.tasks["pr-prep:run"]` exists; `.desc` is null/absent; `.internal` is null/absent. (The YAML assertion is robust to go-task CLI output format changes.) |

### Synchronization model for I-5 (SIGKILL test)

Per design-reviewer finding #4, the SIGKILL test MUST NOT race the kernel's fd-cleanup. The test follows this pattern:

```bash
# 1. Spawn holder in background; wait for it to acquire (info populated)
task -t Taskfile.test.yaml pr-prep &
HOLDER_BASH_PID=$!
for _ in $(seq 1 50); do [ -s "$INFO" ] && break; sleep 0.1; done
[ -s "$INFO" ] || fail "holder never acquired"

# 2. Read the actual holder PID from info (this is the inner task PID,
#    parent of which is the bash subshell, parent of which is flock)
HOLDER_PID=$(awk -F= '/^pid=/{print $2}' "$INFO")

# 3. SIGKILL the holder
kill -9 "$HOLDER_PID"

# 4. Poll for release with a 2s budget
acquired=0
for _ in $(seq 1 20); do
  if flock -n "$LOCK" -c true; then acquired=1; break; fi
  sleep 0.1
done
[ "$acquired" -eq 1 ] || fail "lock not released within 2s of SIGKILL"

# 5. Reap the bash subprocess; ignore exit code
wait "$HOLDER_BASH_PID" 2>/dev/null || :
```

The 2s budget is empirically generous; the actual kernel cleanup is sub-millisecond on modern macOS and Linux.

## Failure Modes & Edge Cases

| Scenario                                                                               | Behavior                                                                                                                                                                                                                                                                                                                                                                |
| -------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Holder process crashes (any signal, OOM, panic)                                        | Kernel closes fd → `flock` releases automatically. Stale `info` content remains until the next holder overwrites it. ✅                                                                                                                                                                                                                                                  |
| Two `pr-prep` invocations race for the lock at the same instant                        | `flock` guarantees exactly one acquires; the loser gets `EWOULDBLOCK` (exit 75) and prints the collision error. ✅                                                                                                                                                                                                                                                       |
| `${TMPDIR}` is unset                                                                   | Falls back to `/tmp` via shell parameter expansion. ✅                                                                                                                                                                                                                                                                                                                  |
| `${TMPDIR}` is on a `noexec`/`nosuid` mount                                            | Irrelevant — we open the lockfile, we do not execute it. ✅                                                                                                                                                                                                                                                                                                             |
| `${TMPDIR}` is on NFS                                                                  | **Out of scope.** `flock(2)` semantics on NFS are undefined; the kernel may fall back to `fcntl` advisory locking which is not visible across clients. The spec assumes a local filesystem. Documented as a known limitation; CI and dev machines use local TMPDIR.                                                                                                     |
| Empty-info race window: holder acquired flock but has not yet completed metadata write | Microsecond window. Collider's `[ -s "$INFO" ]` is false; collider falls through to "Lock file: …" portion of the error message without holder-identity lines. Acceptable — collider knows there is a holder and where the lockfile is.                                                                                                                                |
| `info` file contains stale metadata after a holder crash                               | First post-crash collider may briefly see stale data. The very next holder rewrites `info` immediately after acquiring (within microseconds). Acceptable. ✅                                                                                                                                                                                                            |
| Lockfile owned by another user (shared dev box without per-user `${TMPDIR}`)           | `flock` fails to acquire with `EACCES`. `flock(1)` itself prints "Permission denied" to stderr; the harness's `[ "$rc" -ne 0 ]` catch-all branch then emits "ERROR: lock acquire failed unexpectedly (rc=$rc); see flock(1) output above." and propagates `$rc`. No collision-metadata block in this case (we never opened the file successfully). Documented in pr-prep.md. |
| Holder process detached via `setsid`/`nohup`                                           | The PID written to `info` is the holder's flock-child task PID, which `setsid` does not affect (it changes session, not PID). `kill <pid>` still works. The holder's process group is independent, but that is irrelevant for lock release. ✅                                                                                                                          |
| Symlink attacks on `${TMPDIR}/holomush-pr-prep/lock`                                   | Bounded by `${TMPDIR}` permissions: per-user on macOS, sticky-bit `/tmp` on Linux. Standard POSIX guarantees. ✅                                                                                                                                                                                                                                                        |
| `flock` not installed on the machine                                                   | `preconditions` check fires with a precise install command per platform. ✅                                                                                                                                                                                                                                                                                             |
| User runs `task pr-prep:run` directly to bypass the lock                               | First cmd of `pr-prep:run` refuses with a clear error pointing at the bypass env var. To intentionally bypass (CI, debugging), set `HOLOMUSH_PR_PREP_BYPASS_LOCK=1`. ✅                                                                                                                                                                                                  |
| Cross-user collision on the same machine (rare)                                        | Lockfile is per-user via `${TMPDIR}`; cross-user invocations do not see each other's lock. They WILL collide on the Docker compose project name `holomush-e2e` at the existing pre-flight check (`Taskfile.yaml:135-139`), failing late as today. Out of scope per Non-goal 5.                                                                                          |

## Acceptance Criteria

The implementation PR is mergeable when:

1. All 10 invariant tests pass under the bats runner integrated by the implementation plan.
2. The meta-test `all_invariants_have_tests` (in `scripts/tests/pr-prep-lock.bats`) passes.
3. The implementer smoke-tests `task pr-prep` end-to-end on the maintainer's macOS workstation: (a) one full successful run; (b) one deliberate collision run — start a holder in another terminal, invoke `task pr-prep` in this terminal while the holder is still running, verify the COLLIDED stderr message and non-zero exit; (c) one post-success retry — after the holder from (a) completes, invoke `task pr-prep` again in the same workspace and verify it acquires the lock without error. This is operator attestation, not a CI gate; the bats suite enforces all numbered invariants.
4. CI passes. The CI workflow MUST set `HOLOMUSH_PR_PREP_BYPASS_LOCK=1` if it invokes `pr-prep:run` directly (or invoke `task pr-prep` and rely on the lock being uncontended on a fresh runner). Either form is acceptable; the implementation plan picks one.
5. `site/docs/contributing/pr-prep.md` exists and is reachable from `site/docs/contributing/index.md`. `site/docs/contributing/pr-guide.md` links to it. CLAUDE.md is updated.
6. The implementation does not modify the body of any of the 9 existing `pr-prep` steps.
7. The `flock` formula is added to the `setup` task's `brew install` line.

## Open Questions

None at design-approval time. Operational details (bats test runner integration, where it wires into `task lint`/`task test`, exact CI workflow change for the bypass env var) are deferred to the implementation plan.

## Amendment Log

### rev-7 (2026-04-27) — Multiple corrections from CodeRabbit feedback on PR #2774

**Trigger:** CodeRabbit feedback on PR #2774 surfaced six accuracy issues across this spec and the docs derived from it.

**Changes:**

1. **Goal 1** rewritten to acknowledge that the lock's scope is platform-dependent (per-user on macOS via `$TMPDIR`, machine-global on Linux where `/tmp` is shared) rather than uniformly per-user. The new wording uses "machine-locally" as the goal and explicitly names the platform behavior as a consequence of the `${TMPDIR:-/tmp}` choice. Non-goal 5 (cross-user serialization) was already accurate and is unchanged.
2. **Non-goal 4 (`--force` override)** rewritten to remove the `rm -f ${TMPDIR:-/tmp}/holomush-pr-prep/lock` recommendation. With advisory `flock(2)`, deleting the path while a holder still has its fd open lets a new process create a new inode at the same path and acquire an independent lock — both processes then "hold the lock" simultaneously, defeating the gate. Recovery is now correctly described as process-based (`kill -9 $(lsof -t "$LOCK")`), with an explicit warning against lockfile deletion. Matches the corrected `pr-prep.md` and `pr-guide.md` shipped earlier in this PR.
3. **`Related` header** changed from line-number-anchored references (`Taskfile.yaml:135-139`, `lines 471-499`, CLAUDE.md `lines 547, 550, 876`) to symbol/task-name references. Line numbers drift across PRs; symbols don't.
4. **§Documentation Deliverables item 1** replaced ambiguous "escape hatch" terminology with explicit naming of the sanctioned bypass mechanism (`HOLOMUSH_PR_PREP_BYPASS_LOCK=1` for CI/debugging contexts). Avoids confusion now that Non-goal 4 explicitly removes the lockfile-deletion escape hatch. §Documentation Deliverables item 3 (CLAUDE.md update target) likewise dropped its line-number references.
5. **Meta-test references corrected.** Lines that pointed at a non-existent file `all_invariants_have_tests.bats` now correctly reference the named `@test` block inside `scripts/tests/pr-prep-lock.bats` (the actual implemented location). Affected: §Numbered Invariants prose and §Acceptance Criteria item 2.
6. **§Test Plan stale wording removed.** The sentence "Bats infrastructure (runner integration ...) is deferred to the implementation plan since no bats suite currently exists in the repo" was true at design time but stale post-implementation. Replaced with a description of the implemented wiring (`task test:bats` + first step of `pr-prep:run`).

**Implementation impact:** None. The lock harness shipped on PR #2774 already implements the correct behavior; only the spec's framing was misleading. The corrected wording is now consistent with `pr-guide.md` and `pr-prep.md` (both also updated on PR #2774).

**Reference:** PR #2774 review threads on `pr-guide.md`, `pr-prep.md`, and this spec.
