---
title: "Pre-Push Quality Gate (`task pr-prep`)"
---

:::note[Maintainer workflow]
This page documents the maintainer/agent workflow; its examples assume the
project's internal tooling and git-worktree session isolation. **External
contributors don't need any of that** — `task pr-prep` works identically in a
plain `git` checkout. See
[CONTRIBUTING.md](https://github.com/holomush/holomush/blob/main/CONTRIBUTING.md)
for the contributor workflow.
:::

`task pr-prep` is the project's mandatory pre-push gate. It MUST pass before pushing to a PR branch. HoloMUSH uses a two-lane design so the gate is always fast locally while the heavyweight integration and E2E checks run in CI.

## Lanes

### Fast lane (default, mandatory)

`task pr-prep` runs the fast lane by default:

1. Bats shell tests (concurrency-lock harness regression check)
2. Schema regeneration check
3. License header check
4. Binary plugin builds
5. Lint
6. Format check
7. Unit tests (Go)
8. Build

The fast lane requires no Docker and holds no flock. A typical run takes 2–5 minutes. It is always safe to run concurrently across multiple agent sessions.

On docs-only diffs, `task pr-prep` auto-delegates to `task pr-prep:docs` instead (see [Docs-only fast lane](#docs-only-fast-lane) below).

### Full lane (opt-in)

`task pr-prep:full` adds the integration and E2E gate on top of everything the fast lane runs:

8. Integration tests (Go, with PostgreSQL testcontainer)
9. E2E tests (Playwright in Docker)

The full lane is **flock-serialized machine-globally per user** — only one runs at a time. A typical full run takes 5–15 minutes and is CPU- and Docker-heavy.

Use the full lane when your diff touches integration test surface (Ginkgo suites, Playwright specs, or their shared helpers). You can also trigger it from the auto-detecting entry point:

```bash
HOLOMUSH_PR_PREP_FORCE_FULL=1 task pr-prep
```

### CI-required checks: Integration Test and E2E Test

`Integration Test` and `E2E Test` are **required CI checks protecting `main`**. They run in CI on Namespace runners with Testcontainers Cloud — not in the mandatory local fast lane. A PR cannot merge until both are green in CI.

Quarantined specs are excluded from both gating CI runs and from the full lane's Docker-backed suite. They run only nightly and locally with `HOLOMUSH_RUN_QUARANTINED=1`. See [quarantine.md](/contributing/how-to/quarantine/) for details.

### Docs lane

`task pr-prep:docs` runs automatically when every changed path is docs-only (see [Docs-only fast lane](#docs-only-fast-lane) below). It has no Docker dependency and does not acquire the flock.

## Concurrent runs (full lane)

`task pr-prep:full` is serialized machine-globally per user via `flock(1)`. The fast lane (`task pr-prep`) has no such restriction — concurrent fast-lane runs across agent sessions are safe. If you run `task pr-prep:full` on a machine where another full-lane run is already in flight, the second invocation exits non-zero immediately with a message like:

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

## Reading the result (for automation)

If you script around `task pr-prep` — a retry loop, a babysitter, an agent — read the **exit code** and the **result file**, not the terminal text. Two things make stdout the wrong signal:

- go-task collapses every non-zero exit to `201`, so the exit code alone can't tell a lock collision apart from a real gate failure.
- pr-prep's own `pr-prep-lock.bats` self-test prints the string `another pr-prep is running` on healthy runs. A loop that greps for that string to detect contention treats every passing run as a collision and re-runs forever. This happened in May 2026.

Every run writes a result file and prints a line prefixed with `▸ pr-prep result:` (match this prefix to find the path — don't assume a fixed line number, since wrappers may prepend output):

```text
▸ pr-prep result: /var/folders/.../T/holomush-pr-prep/runs/20260525T120000Z-12345.result
```

The file holds `key=value` lines:

```text
status=pass
lane=fast
exit=0
finished_at=2026-05-25T12:14:03Z
```

(A full-lane run writes `lane=full`. A contention exit writes `lane=full` with `status=contention`.)

Branch on `status`, which is `pass`, `fail`, or `contention`:

- `contention` — another run holds the lock. Wait, then retry. (It also returned in ~2s having run nothing.)
- `fail` — a real check failed. Read the log and fix it; do not retry.
- `pass` — you are clear to push.

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

## Docs-only fast lane

`task pr-prep` auto-detects when a diff touches only documentation paths
(per the canonical `DOCS_ONLY_PATHS` list in `Taskfile.yaml`'s `vars:` block)
and delegates to `task pr-prep:docs`, a lightweight lane that runs:

- `lint:markdown` (rumdl)
- `lint:yaml` (yamlfmt)
- `lint:docs-symmetry` (AGENTS.md ↔ CLAUDE.md byte-equivalence)
- `fmt:check` (dprint + rumdl)
- `license:check` (addlicense)
- `lint:docs-paths-sync` (verifies the canonical glob list is in sync
  across `Taskfile.yaml` + `.github/workflows/ci.yaml` + `.github/workflows/ci-docs-skip.yaml`)

The docs lane has no Docker dependency, runs no Go compile, and does not
acquire the `pr-prep` flock — concurrent docs lanes are safe.

### When is a diff "docs-only"?

A diff is docs-only when every changed path matches one of the canonical
globs:

- `site/**`
- `docs/**`
- `**/*.md` (including `README.md`, `web/CLAUDE.md`, `.github/PULL_REQUEST_TEMPLATE.md`)
- `.claude/agents/**`
- `.claude/commands/**`
- `.claude/rules/**`
- `.claude/agent-memory/**`
- `LICENSE`, `LICENSE_HEADER`

Non-docs paths (any `.go`, `.proto`, `.yaml` outside the included dirs,
`.claude/hooks/**`, `.claude/settings*.json`, `.github/workflows/**`, etc.)
route the entire diff to the full lane.

### Forcing the full lane

```bash
HOLOMUSH_PR_PREP_FORCE_FULL=1 task pr-prep
```

Use this when a markdown change references not-yet-merged code via
`mkdocstrings`-style includes, or when you want full validation regardless
of diff classification.

### Uncommitted-changes caveat

Lane detection uses `git diff --name-only origin/main...HEAD`, which reflects
only **committed** changes. If you've edited files but not yet committed them,
the diff won't include them and the docs-vs-code lane may be misclassified.
Commit your work before `task pr-prep` so detection sees the full change set.

### Same-name skip workflow

On the CI side, `.github/workflows/ci.yaml` has `paths-ignore` for the
same glob list. A separate workflow `.github/workflows/ci-docs-skip.yaml`
runs on the inverse path filter with workflow name `CI` and jobs named
`Lint`, `Test`, `Build`, `Integration Test`, `E2E Test`. GitHub's
check-identity rule treats `(workflow_name, job_name)` as the same required
check across files, so branch-protection required checks stay green on
docs-only PRs without invoking the full pipeline.

The `Lint` job there is **not** a no-op: it runs the real docs lane
(`task pr-prep:docs`) so markdown and ADR violations are caught on
docs-only PRs instead of landing on `main` unchecked (holomush-3zrvh).
`Test`, `Build`, `Integration Test`, and `E2E Test` remain no-op `echo`
stand-ins — docs changes don't exercise those.

If you ever edit `DOCS_ONLY_PATHS`, edit all three locations
(Taskfile.yaml + ci.yaml + ci-docs-skip.yaml) and run
`task lint:docs-paths-sync` to verify byte-equivalence.

- Open follow-up: `holomush-71zq.12` — design issue around descendant fd inheritance (the "kill the process tree" caveat above is the documentation mitigation; future code fix may add `setsid` or a SIGTERM trap to make single-PID kill reliable).
