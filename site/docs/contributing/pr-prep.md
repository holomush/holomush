# Pre-Push Quality Gate (`task pr-prep`)

`task pr-prep` is the project's mandatory pre-push gate. It mirrors every CI job (schema check, license check, plugin builds, lint, format, unit tests, integration tests, E2E in Docker) and MUST pass before pushing to a PR branch.

A full run takes 5–15 minutes and is CPU- and Docker-heavy.

## What it does

1. Bats shell tests (concurrency-lock harness regression check)
2. Schema regeneration check
3. License header check
4. Binary plugin builds
5. Lint
6. Format check
7. Unit tests (Go)
8. Integration tests (Go, with PostgreSQL testcontainer)
9. E2E tests (Playwright in Docker)

## Concurrent runs

`task pr-prep` is serialized machine-globally per user. If you run `task pr-prep` on a machine where another `task pr-prep` is already in flight (typically: another agent session, or another terminal you forgot about), the second invocation exits non-zero immediately with a message like:

```text
ERROR: another pr-prep is running on this machine.

  pid=12345
  workspace=/Volumes/Code/.../.worktrees/feature-x
  started_at=2026-04-26T14:32:11Z

  Lock file: /var/folders/.../T/holomush-pr-prep/lock
  Wait for it to finish, or kill the holder PID if wedged.
```

**What to do:**

- **Wait.** The first run will finish in 5–15 minutes; your second invocation will then succeed.
- **Identify the holder.** The `pid=` line is the kill target. The `workspace=` line tells you which session is running.
- **Kill the holder process tree if wedged** (see below for why a single `kill` is not enough).

> **Do NOT delete the lockfile to "force" recovery.** With advisory `flock(2)` semantics, if you `rm` the lock file path while the original holder still has its file descriptor open, a new process will create a new inode at the same path and acquire its own (independent) lock — both processes can then "hold the lock" simultaneously, defeating the gate. Recovery is process-based: kill the holder process tree (recipes below) and the kernel releases the lock automatically when all file descriptors close.

### Killing a wedged holder

A common surprise: `kill <holder-pid>` alone does NOT always release the lock. The flock file descriptor is inherited by every descendant of the holder (the inner shell, `lint`, `golangci-lint`, `go test`, `docker compose`, etc.). The kernel keeps the lock held until ALL descriptors pointing to that open file description are closed. Killing only the visible holder PID leaves orphaned children still holding the lock.

The reliable form (recommended) — find every PID currently holding the lockfile open and kill them all in one shot, with an empty-output guard:

```bash
LOCK="${TMPDIR:-/tmp}/holomush-pr-prep/lock"
holders=$(lsof -t "$LOCK" 2>/dev/null) && [ -n "$holders" ] && kill -9 $holders
```

This mirrors what the I-5 bats test does at `scripts/tests/pr-prep-lock.bats` and is the same approach the kernel uses internally to decide whether the lock is held.

Alternative on a single-user dev machine where no unrelated `task` invocations are running:

```bash
killall task
```

> **Why not `pkill -P <holder-pid>`?** `pkill -P` only matches direct children, not grandchildren. A wedged `pr-prep` tree typically looks like `task → sh -c → task pr-prep:run → inner shell → golangci-lint/go test/docker compose/...` and the deepest descendants would survive `pkill -P`, continuing to hold the lock fd. The `lsof -t` recipe above handles the full set in one pass. (Future work: tracked as `holomush-71zq.12`, the production harness could `setsid` the holder so a single `kill -- -<PGID>` reliably tears down the whole group.)

After all fd holders die, the kernel releases the lock within microseconds. Run `task pr-prep` again — it should acquire cleanly.

## How the lock works

`flock(1)` opens a file under `${TMPDIR:-/tmp}/holomush-pr-prep/lock` and holds it for the duration of the inner CI body. The kernel automatically releases the lock when the holding processes' file descriptors close — including when they die for any reason. There is no stale-lock-cleanup procedure to worry about, BUT note (per "Killing a wedged holder" above) that descriptor inheritance through child processes means the holder process tree must die for the lock to release.

The lock is per-user on macOS (`$TMPDIR` is automatically per-user there). On Linux it is namespaced by directory under `/tmp`.

## CI behavior

CI does not invoke `task pr-prep` directly — `.github/workflows/ci.yaml` runs the underlying subtasks (`task lint`, `task test:cover`, `task test:int`, `task test:e2e:cover`, `task generate:schema`) on a fresh single-tenant runner per job. The lock harness is therefore irrelevant to CI by construction; no workflow changes are needed.

## Direct invocation of `pr-prep:run`

`pr-prep:run` is the inner body that contains the actual 9-step CI mirror. **Do not invoke it directly from a contributor's machine** — it skips the concurrency lock and will race with any other `pr-prep` running on the same machine.

If you have a legitimate reason to invoke it directly (debugging a single step in isolation, for example), set `HOLOMUSH_PR_PREP_BYPASS_LOCK=1`:

```bash
HOLOMUSH_PR_PREP_BYPASS_LOCK=1 task pr-prep:run
```

Without that env var, `pr-prep:run` exits non-zero with a message pointing at the bypass.

## Related

- Spec: `docs/superpowers/specs/2026-04-26-pr-prep-concurrency-safety-design.md`
- Tracking bead: `holomush-71zq`
- Open follow-up: `holomush-71zq.12` — design issue around descendant fd inheritance (the "kill the process tree" caveat above is the documentation mitigation; future code fix may add `setsid` or a SIGTERM trap to make single-PID kill reliable).
