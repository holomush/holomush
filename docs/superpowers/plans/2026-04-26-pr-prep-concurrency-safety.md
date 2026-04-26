# pr-prep Concurrency Safety Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Spec:** `docs/superpowers/specs/2026-04-26-pr-prep-concurrency-safety-design.md` (rev-6, READY).
**Tracking bead:** `holomush-71zq`.
**Goal:** Serialize `task pr-prep` machine-globally per user, with rich collision messaging, by wrapping the existing 9-step body in a `flock(1)`-based harness implemented entirely in `Taskfile.yaml`. Ten named bats tests enforce numbered invariants I-1 through I-10.
**Architecture:** `flock(1)` runs as command entrypoint (kernel-managed advisory lock; auto-release on process death). Lockfile at `${TMPDIR:-/tmp}/holomush-pr-prep/{lock,info}` (per-user on macOS via TMPDIR). Inside the harness, `flock` invokes `sh -c '...'` which sets the bypass env var, writes metadata, and `exec task pr-prep:run`. `pr-prep:run` has neither `desc:` (hidden from `task --list`) nor `internal: true` (would break `exec task pr-prep:run`); the env-var bypass guard is the runtime footgun protection.
**Tech Stack:** go-task 3.50+ (uses embedded `mvdan.cc/sh` interpreter — no `exec N>` redirections; external commands inherit stdin/stdout/stderr only); `flock(1)` from the discoteq/flock Homebrew formula or util-linux on Linux; `bats-core 1.x` for shell tests; `yq 4.x` for YAML-level assertions in tests.

---

## File Structure

| Path                                                                        | Action  | Responsibility                                                                                       |
| --------------------------------------------------------------------------- | ------- | ---------------------------------------------------------------------------------------------------- |
| `Taskfile.yaml` (line ~471–499 + line ~508 + new line near 499 + new task)  | MODIFY  | Replace `pr-prep` body with locked harness; add internal `pr-prep:run`; add `flock`/`bats-core`/`yq` to setup; add `task test:bats` runner |
| `scripts/tests/pr-prep-lock.bats`                                           | CREATE  | All 10 invariant tests (I-1 .. I-10) + meta-test                                                     |
| `scripts/tests/pr-prep-lock-helpers.bash`                                   | CREATE  | Shared shell helpers (`spawn_holder`, `wait_for_acquire`, `cleanup_lock`, etc.)                      |
| `scripts/tests/Taskfile.test.yaml`                                          | CREATE  | Fixture Taskfile mirroring the production lock harness but invoking `pr-prep:stub` instead of the real 9-step body |
| `site/docs/contributing/pr-prep.md`                                         | CREATE  | Operator-facing page describing what `pr-prep` does and the collision-recovery procedure              |
| `site/docs/contributing/index.md`                                           | MODIFY  | Add link to the new `pr-prep.md`                                                                     |
| `site/docs/contributing/pr-guide.md`                                        | MODIFY  | Add a brief callout pointing to `pr-prep.md` for collision-recovery details                          |
| `CLAUDE.md` (lines 547, 550, 876)                                           | MODIFY  | Add a single sentence to each of the three "MUST run task pr-prep" sites noting serialization        |

**Beads to file at execution start:** When `superpowers:subagent-driven-development` (or `superpowers:executing-plans`) begins this plan, create one sub-bead per task as a child of `holomush-71zq` so progress is tracked atomically. Use `BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd create --parent holomush-71zq --title "..." --type=task --priority=2` (per `feedback_bd_in_jj_workspaces` memory). Each task in this plan corresponds to one bead.

---

## Phase A — Test infrastructure

### Task 1: Add `flock`, `bats-core`, `yq` to `task setup`

**Why:** the harness uses `flock(1)`; tests use `bats-core` and `yq`. These are already installed on the maintainer's machine but need to be in `task setup` so a new contributor's environment matches. CI uses `util-linux` (which provides `flock`) by default; CI does not need `bats-core` or `yq` until Task 14 wires bats into `pr-prep:run`.

**Files:**

- Modify: `Taskfile.yaml:502` (the existing `brew install` line in the `setup` task)

- [ ] **Step 1: Inspect current `setup` cmds**

Run: `rg -n "setup:" Taskfile.yaml | head -2 && sed -n '499,513p' Taskfile.yaml`

Expected output around line 502:

```yaml
- brew install golangci-lint gofumpt lefthook actionlint goreleaser dprint cocogitto rumdl yamlfmt binaryen gotestsum
```

- [ ] **Step 2: Append `flock bats-core yq` to the brew install line**

Edit `Taskfile.yaml` line ~508 from:

```yaml
- brew install golangci-lint gofumpt lefthook actionlint goreleaser dprint cocogitto rumdl yamlfmt binaryen gotestsum
```

To:

```yaml
- brew install golangci-lint gofumpt lefthook actionlint goreleaser dprint cocogitto rumdl yamlfmt binaryen gotestsum flock bats-core yq
```

- [ ] **Step 3: Verify all three are already present locally (so the rest of the plan can run without re-running setup)**

Run:

```bash
command -v flock && flock --version
command -v bats && bats --version
command -v yq && yq --version
```

Expected: each prints a version string and exits 0. If any are missing, run `task setup` to install. (The maintainer's machine already has all three as of 2026-04-26.)

- [ ] **Step 4: Run `task fmt` and verify the file passes lint**

Run: `task fmt`

Expected: no errors; the modified `Taskfile.yaml` line is unchanged after format (yamlfmt accepts the longer line).

- [ ] **Step 5: Commit**

Commit using VCS-appropriate commands per `references/vcs-preamble.md`. Suggested message: `chore(setup): add flock, bats-core, yq to brew install for pr-prep concurrency safety`.

---

### Task 2: Add `task test:bats` runner and verify with a sanity test

**Why:** the project has no bats infrastructure yet (only pytest under `scripts/tests/test_*.py`). Before writing any invariant tests we need a runner integrated with the project's task primitives, and we need to confirm that bats finds tests in the expected location.

**Files:**

- Modify: `Taskfile.yaml` — add a new `test:bats` task (insert after the existing `test:e2e:cover` block around line 170, before `test:bench`)
- Create: `scripts/tests/sanity_test.bats` (TEMPORARY — deleted after Task 3 lands)

- [ ] **Step 1: Create the sanity test**

Create `scripts/tests/sanity_test.bats` with content:

```bash
#!/usr/bin/env bats

@test "bats runner sees this file" {
  result=$((1 + 1))
  [ "$result" -eq 2 ]
}
```

- [ ] **Step 2: Add the `test:bats` task to `Taskfile.yaml`**

Insert after the `test:e2e:cover` task (around line 170) and before `test:bench`:

```yaml
test:bats:
  desc: Run bats shell tests under scripts/tests/
  cmds:
    - bats scripts/tests/
```

- [ ] **Step 3: Run the sanity test via the new task target**

Run: `task test:bats`

Expected output (something like):

```text
task: [test:bats] bats scripts/tests/
sanity_test.bats
 ✓ bats runner sees this file

1 test, 0 failures
```

If the test fails or `bats` cannot find the file, debug before continuing. Do NOT skip this verification — the entire plan depends on this runner working.

- [ ] **Step 4: Run `task fmt`**

Run: `task fmt`

Expected: no errors.

- [ ] **Step 5: Commit**

Suggested message: `feat(tasks): add test:bats runner and sanity test`.

---

### Task 3: Create test fixture Taskfile and shared helpers

**Why:** all 10 invariant tests run against a fixture Taskfile (`scripts/tests/Taskfile.test.yaml`) that implements the same lock harness shape as production but invokes a stub inner task instead of the real 9-step `pr-prep` body. Tests share helper functions (`spawn_holder`, `wait_for_acquire`, etc.) — putting them in a separate `.bash` file keeps the test suite readable. The sanity test from Task 2 is deleted at the end of this task.

**Files:**

- Create: `scripts/tests/Taskfile.test.yaml`
- Create: `scripts/tests/pr-prep-lock-helpers.bash`
- Delete: `scripts/tests/sanity_test.bats` (no longer needed; replaced by real tests starting Task 4)

- [ ] **Step 1: Create `scripts/tests/Taskfile.test.yaml`**

```yaml
version: "3"

# Fixture Taskfile for scripts/tests/pr-prep-lock.bats.
#
# Mirrors the production `pr-prep` / `pr-prep:run` shape from `Taskfile.yaml`,
# but invokes `pr-prep:stub` (a configurable stub) instead of the real 9-step
# body. Shares the lockfile path with production by default; tests override
# via $LOCK_DIR_OVERRIDE so they do not contend with a real `task pr-prep`
# running concurrently on the same machine.

vars:
  LOCK_DIR:
    sh: echo "${LOCK_DIR_OVERRIDE:-${TMPDIR:-/tmp}/holomush-pr-prep-bats}"

tasks:
  pr-prep:
    desc: Locked entry point under test
    preconditions:
      - sh: command -v flock >/dev/null 2>&1
        msg: "flock(1) is required. Install with 'brew install flock' (macOS) or 'apt install util-linux' (Linux)."
    cmds:
      - cmd: |
          set -euo pipefail
          LOCK_DIR="{{.LOCK_DIR}}"
          mkdir -p "$LOCK_DIR"
          LOCK="$LOCK_DIR/lock"
          INFO="$LOCK_DIR/info"
          rc=0
          # {{.TASKFILE}} is a go-task BUILT-IN that resolves to the absolute
          # path of the loaded Taskfile (i.e., this fixture). We pass it
          # explicitly to `exec task -t "$3"` so the inner `pr-prep:run`
          # invocation looks up THIS fixture's task, not the project root's
          # Taskfile.yaml (which has its own `pr-prep:run` after Task 13).
          # NOTE: per-`cmd:` `vars:` blocks are silently ignored by go-task
          # (verified empirically against go-task 3.x); do not add one here.
          flock -n -E 75 "$LOCK" sh -c '
            export HOLOMUSH_PR_PREP_BYPASS_LOCK=1
            printf "pid=%s\nworkspace=%s\nstarted_at=%s\n" \
              "$$" "$2" "$(date -u +%FT%TZ)" > "$1"
            exec task -t "$3" pr-prep:run
          ' _ "$INFO" "$PWD" "{{.TASKFILE}}" || rc=$?
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
            echo "ERROR: lock acquire failed unexpectedly (rc=$rc); see flock(1) output above." >&2
            exit "$rc"
          fi

  pr-prep:run:
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
      - task: pr-prep:stub

  pr-prep:stub:
    cmds:
      - cmd: |
          MARKER="${STUB_MARKER:-/tmp/pr-prep-stub-marker.$$}"
          : > "$MARKER"
          if [ -n "${STUB_SLEEP:-}" ]; then
            sleep "$STUB_SLEEP"
          fi
          if [ -n "${STUB_EXIT:-}" ]; then
            exit "$STUB_EXIT"
          fi
```

**Note:** the fixture passes `-t scripts/tests/Taskfile.test.yaml` to the inner `task pr-prep:run` invocation so go-task does not look up the project root's Taskfile (which has its OWN `pr-prep:run` once Task 13 lands).

- [ ] **Step 2: Create `scripts/tests/pr-prep-lock-helpers.bash`**

```bash
#!/usr/bin/env bash
# Shared helpers for scripts/tests/pr-prep-lock.bats.
# Source via: load 'pr-prep-lock-helpers'

# Compute a unique LOCK_DIR per bats run so concurrent test invocations on
# the same machine don't trample each other. Tests source this helper and
# rely on $LOCK_DIR_OVERRIDE being set BEFORE they invoke `task -t ...`.
init_test_env() {
  export LOCK_DIR_OVERRIDE="${BATS_TEST_TMPDIR}/holomush-pr-prep-bats"
  export INFO_FILE="$LOCK_DIR_OVERRIDE/info"
  export LOCK_FILE="$LOCK_DIR_OVERRIDE/lock"
  rm -rf "$LOCK_DIR_OVERRIDE"
}

# Path to the fixture taskfile, computed relative to the repo root.
fixture_taskfile() {
  echo "scripts/tests/Taskfile.test.yaml"
}

# Spawn a holder in the background that runs the fixture pr-prep with the
# given env (e.g., STUB_SLEEP=30) and returns the bash subshell PID. The
# bash subshell is the parent of `task` which is the parent of the
# `flock`-owned `sh -c` subshell which `exec`s into the inner task.
spawn_holder() {
  task -t "$(fixture_taskfile)" pr-prep \
    >"${BATS_TEST_TMPDIR}/holder.out" 2>"${BATS_TEST_TMPDIR}/holder.err" &
  echo $!
}

# Poll for the info file to be populated (up to 5 seconds). Returns 0 if
# populated, 1 if timed out.
wait_for_acquire() {
  local i
  for i in $(seq 1 50); do
    [ -s "$INFO_FILE" ] && return 0
    sleep 0.1
  done
  return 1
}

# Read the holder PID from the info file. Returns empty string if not present.
holder_pid_from_info() {
  awk -F= '/^pid=/{print $2}' "$INFO_FILE" 2>/dev/null
}

# Run the fixture pr-prep in the foreground, capturing exit status, stdout,
# stderr in $status, $output (combined). Mirrors bats run behavior.
fixture_pr_prep() {
  task -t "$(fixture_taskfile)" pr-prep
}

# Run the fixture pr-prep:run in the foreground (direct invocation, exercises
# the bypass guard).
fixture_pr_prep_run() {
  task -t "$(fixture_taskfile)" pr-prep:run
}
```

- [ ] **Step 3: Delete the temporary sanity test**

Run: `rm scripts/tests/sanity_test.bats`

- [ ] **Step 4: Smoke-test the fixture by invoking it directly**

Run:

```bash
LOCK_DIR_OVERRIDE=/tmp/fixture-smoke STUB_MARKER=/tmp/fixture-smoke-marker.$$ \
  task -t scripts/tests/Taskfile.test.yaml pr-prep
echo "---"
[ -f /tmp/fixture-smoke-marker.* ] && echo "marker present: yes" || echo "marker present: NO"
[ -f /tmp/fixture-smoke/info ] && cat /tmp/fixture-smoke/info
rm -rf /tmp/fixture-smoke /tmp/fixture-smoke-marker.*
```

Expected: harness exits 0, info file populated with `pid=`/`workspace=`/`started_at=`, marker file present.

If anything fails here, debug the fixture before writing tests against it. The fixture must work end-to-end before test authoring begins.

- [ ] **Step 5: Run `task fmt`**

Run: `task fmt`

Expected: no errors.

- [ ] **Step 6: Commit**

Suggested message: `test(pr-prep-lock): add fixture Taskfile and shared bats helpers`.

---

## Phase B — Lock harness invariants (TDD)

Each task in this phase: write a failing test → run to verify failure → implement (or extend implementation) → run to verify pass → commit. The fixture Taskfile in `scripts/tests/Taskfile.test.yaml` already implements the full harness shape (Task 3) so most invariants are testable WITHOUT further fixture changes. A few invariants (I-7, I-9) need targeted fixture changes — noted per task.

The bats test file accumulates: the first invariant test creates the file; subsequent invariants append.

### Task 4: I-7 — Precondition fires when `flock` is missing

**Why:** the simplest invariant. The fixture's `preconditions` block already declares the dependency; the test invokes the harness with a stripped `PATH` and asserts the precondition error message.

**Files:**

- Create: `scripts/tests/pr-prep-lock.bats` (NEW)

- [ ] **Step 1: Create the bats file with the I-7 test**

```bash
#!/usr/bin/env bats

bats_load_library bats-support 2>/dev/null || true
bats_load_library bats-assert 2>/dev/null || true
load 'pr-prep-lock-helpers'

# Tests use relative paths (e.g., `task -t scripts/tests/Taskfile.test.yaml`)
# that resolve from the bats CWD. The canonical invocation is `task test:bats`
# from the repo root. Refuse to run from any other CWD so a contributor who
# invokes `bats scripts/tests/pr-prep-lock.bats` directly gets a clear error
# instead of a confusing "task -t: file not found" later.
setup_file() {
  if [ ! -f "scripts/tests/Taskfile.test.yaml" ]; then
    echo "ERROR: bats must be invoked from the repo root (try 'task test:bats')." >&2
    echo "       Current CWD: $PWD" >&2
    exit 1
  fi
}

setup() {
  init_test_env
}

# I-7: precondition message when flock is missing.
@test "precondition_message: missing flock fails with platform-specific install hint" {
  # Strip flock from PATH. /usr/bin/env, /usr/bin/sh, /bin/sh are needed for
  # bats and go-task themselves; restrict PATH to those.
  PATH=/usr/bin:/bin run task -t "$(fixture_taskfile)" pr-prep
  [ "$status" -ne 0 ]
  [[ "$output" == *"brew install flock"* ]]
  [[ "$output" == *"apt install util-linux"* ]]
}
```

- [ ] **Step 2: Run the test**

Run: `task test:bats`

Expected: test passes (1 test, 0 failures). The precondition is already in the fixture from Task 3.

If the test FAILS, debug the precondition message format in the fixture before continuing.

- [ ] **Step 3: Verify the test actually exercises the failure path**

To convince yourself the test is meaningful, temporarily edit the fixture's precondition `msg:` to remove the platform hints, re-run `task test:bats`, observe the test FAIL, then revert. (Do not commit the temporary change.)

- [ ] **Step 4: Commit**

Suggested message: `test(pr-prep-lock): I-7 precondition_message`.

---

### Task 5: I-1 — Acquires when idle (and exits 0 on inner success)

**Why:** the foundational happy-path test. Verifies the harness acquires the lock, populates the info file, runs the inner stub, and exits 0.

**Files:**

- Modify: `scripts/tests/pr-prep-lock.bats` (append)

- [ ] **Step 1: Append the I-1 test to the bats file**

```bash
# I-1: a first invocation on an idle machine acquires the lock and runs the
# inner body to completion.
@test "acquires_when_idle: harness acquires lock and runs inner stub to success" {
  STUB_MARKER="${BATS_TEST_TMPDIR}/marker"
  STUB_SLEEP=0 \
    STUB_MARKER="$STUB_MARKER" \
    run task -t "$(fixture_taskfile)" pr-prep
  [ "$status" -eq 0 ]
  [ -f "$STUB_MARKER" ]
  [ -s "$INFO_FILE" ]
  grep -q '^pid=' "$INFO_FILE"
  grep -q '^workspace=' "$INFO_FILE"
  grep -q '^started_at=' "$INFO_FILE"
}
```

- [ ] **Step 2: Run the bats suite**

Run: `task test:bats`

Expected: 2 tests, 0 failures (I-7 + I-1).

- [ ] **Step 3: Commit**

Suggested message: `test(pr-prep-lock): I-1 acquires_when_idle`.

---

### Task 6: I-10 — `pr-prep:run` is hidden from `task --list` and is not declared internal

**Why:** I-10 has both a CLI-output check and (per spec rev-3 NB-3 / rev-4 prose) a YAML-level `yq` assertion as the primary contract. The YAML assertion is robust against go-task version drift. We test the FIXTURE Taskfile (it has the same shape as production); Task 13 will copy the same shape into `Taskfile.yaml`.

**Files:**

- Modify: `scripts/tests/pr-prep-lock.bats` (append)

- [ ] **Step 1: Append the I-10 test**

```bash
# I-10: pr-prep:run MUST NOT appear in `task --list`. YAML-level: the task
# entry exists, has no `desc:`, and has no `internal: true`.
@test "pr_prep_run_hidden_from_list: not in task --list; YAML asserts no desc and no internal" {
  # CLI-output check (supplementary).
  run task -t "$(fixture_taskfile)" --list
  [ "$status" -eq 0 ]
  ! [[ "$output" == *"pr-prep:run"* ]]

  # YAML-level (primary contract). Robust against go-task version drift.
  local fixture
  fixture="$(fixture_taskfile)"
  run yq '.tasks["pr-prep:run"]' "$fixture"
  [ "$status" -eq 0 ]
  [ -n "$output" ]                       # task entry exists
  [ "$output" != "null" ]

  run yq '.tasks["pr-prep:run"].desc' "$fixture"
  [ "$output" = "null" ]                 # no desc

  run yq '.tasks["pr-prep:run"].internal' "$fixture"
  [ "$output" = "null" ]                 # no internal: true
}
```

- [ ] **Step 2: Run the bats suite**

Run: `task test:bats`

Expected: 3 tests, 0 failures (I-7 + I-1 + I-10).

- [ ] **Step 3: Commit**

Suggested message: `test(pr-prep-lock): I-10 pr_prep_run_hidden_from_list`.

---

### Task 7: I-9 — Bypass guard blocks direct invocation of `pr-prep:run`

**Why:** verifies the env-var bypass guard (the runtime footgun protection that replaced `internal: true`).

**Files:**

- Modify: `scripts/tests/pr-prep-lock.bats` (append)

- [ ] **Step 1: Append the I-9 test**

```bash
# I-9: direct CLI invocation of pr-prep:run without HOLOMUSH_PR_PREP_BYPASS_LOCK=1
# refuses to run, exits non-zero, and prints a message naming the bypass env var.
@test "bypass_guard_blocks: direct pr-prep:run without env var fails with bypass-message" {
  run env -u HOLOMUSH_PR_PREP_BYPASS_LOCK \
    task -t "$(fixture_taskfile)" pr-prep:run
  [ "$status" -ne 0 ]
  [[ "$output" == *"HOLOMUSH_PR_PREP_BYPASS_LOCK"* ]]
  [[ "$output" == *"Use 'task pr-prep' instead"* ]]
}

# Also verify the bypass DOES allow direct invocation (so CI can use it).
@test "bypass_guard_blocks (positive): with env var set, pr-prep:run runs to success" {
  STUB_MARKER="${BATS_TEST_TMPDIR}/bypass-marker"
  HOLOMUSH_PR_PREP_BYPASS_LOCK=1 \
    STUB_MARKER="$STUB_MARKER" \
    run task -t "$(fixture_taskfile)" pr-prep:run
  [ "$status" -eq 0 ]
  [ -f "$STUB_MARKER" ]
}
```

- [ ] **Step 2: Run the bats suite**

Run: `task test:bats`

Expected: 5 tests, 0 failures.

- [ ] **Step 3: Commit**

Suggested message: `test(pr-prep-lock): I-9 bypass_guard_blocks`.

---

### Task 8: I-2 + I-3 + I-8 — Three real collision tests, each enforcing one invariant

**Why:** the spec's invariant table assigns one named test to each of I-2 (`rejects_while_held`), I-3 (`error_includes_metadata`), and I-8 (`non_blocking_acquire`). Per project memory `feedback_invariants_and_docs_as_spec_acceptance` and the meta-test's drift-detection purpose, each invariant MUST have a real enforcing test — `skip` aliases would silently pass even if the spec's I-3/I-8 assertions broke. The three tests share a setup pattern (spawn 30s holder, wait for acquire, run collider) which we factor into helpers in `pr-prep-lock-helpers.bash`. Each test then asserts ONLY its own invariant. Three holder spawns × ~3s each = ~9s added test runtime; acceptable for genuine enforcement.

**Files:**

- Modify: `scripts/tests/pr-prep-lock-helpers.bash` (append two helpers — `start_collision`, `end_collision`)
- Modify: `scripts/tests/pr-prep-lock.bats` (append three tests)

- [ ] **Step 1: Append collision setup/teardown helpers to `pr-prep-lock-helpers.bash`**

Append to `scripts/tests/pr-prep-lock-helpers.bash`:

```bash
# Spawn a 30s background holder for collision tests. Sets globals
# HOLDER_BASH_PID and HOLDER_PID. Returns 0 on successful acquire,
# fails the test (via bats `fail`) if the holder never acquires.
start_collision() {
  STUB_SLEEP=30 STUB_MARKER="${BATS_TEST_TMPDIR}/holder-marker" \
    spawn_holder >/dev/null
  HOLDER_BASH_PID=$!
  if ! wait_for_acquire; then
    kill -9 "$HOLDER_BASH_PID" 2>/dev/null || :
    fail "holder never acquired the lock within 5s"
  fi
  HOLDER_PID="$(holder_pid_from_info)"
  [ -n "$HOLDER_PID" ] || fail "info file did not contain a pid line"
}

# Reap the collision holder. Idempotent.
end_collision() {
  kill "$HOLDER_PID" 2>/dev/null || :
  wait "$HOLDER_BASH_PID" 2>/dev/null || :
}
```

- [ ] **Step 2: Append the three real tests to `scripts/tests/pr-prep-lock.bats`**

```bash
# I-2: a second invocation while the first is in flight exits non-zero
# without running any inner step.
@test "rejects_while_held: second invocation exits non-zero while holder runs" {
  start_collision
  run fixture_pr_prep
  [ "$status" -ne 0 ]
  end_collision
}

# I-3: the collision error contains pid, workspace, started_at, and lockfile path.
@test "error_includes_metadata: collision stderr names the holder PID, workspace, start time, and lockfile" {
  start_collision
  run fixture_pr_prep
  [[ "$output" == *"another pr-prep is running"* ]]
  [[ "$output" == *"pid=$HOLDER_PID"* ]]
  [[ "$output" == *"workspace="* ]]
  [[ "$output" == *"started_at="* ]]
  [[ "$output" == *"Lock file: $LOCK_FILE"* ]]
  end_collision
}

# I-8: collision exits within 2s regardless of holder remaining time (non-blocking).
@test "non_blocking_acquire: collision exits within 2s while holder still has 28s+ to run" {
  start_collision
  start_ms=$(perl -MTime::HiRes=time -e 'printf "%d", time*1000')
  run fixture_pr_prep
  end_ms=$(perl -MTime::HiRes=time -e 'printf "%d", time*1000')
  elapsed=$((end_ms - start_ms))
  # 30s holder vs 2s budget = 15x safety margin. If this fails intermittently,
  # the bug is real — flock -n is supposed to be non-blocking.
  [ "$elapsed" -lt 2000 ]
  end_collision
}
```

- [ ] **Step 3: Run the bats suite**

Run: `task test:bats`

Expected: 8 tests, 0 failures (T4=1, T5=1, T6=1, T7=2, T8=3 = 8).

If any of the three tests fail, debug per the assertion that fired — they enforce different parts of the same scenario, so the failure mode is precise.

- [ ] **Step 4: Commit**

Suggested message: `test(pr-prep-lock): I-2 + I-3 + I-8 collision-detection invariants (3 real tests)`.

---

### Task 9: I-4 — Lock releases on normal exit

**Why:** verifies that after a holder exits cleanly, a sequential second invocation acquires the lock without error. The kernel-managed flock release should make this trivially pass.

**Files:**

- Modify: `scripts/tests/pr-prep-lock.bats` (append)

- [ ] **Step 1: Append the I-4 test**

```bash
# I-4: after a holder exits normally, the next invocation acquires the lock.
@test "releases_on_normal_exit: sequential second invocation succeeds after holder finishes" {
  # First holder runs and exits 0
  STUB_SLEEP=0 STUB_MARKER="${BATS_TEST_TMPDIR}/marker1" \
    run task -t "$(fixture_taskfile)" pr-prep
  [ "$status" -eq 0 ]

  # Second invocation should succeed
  STUB_SLEEP=0 STUB_MARKER="${BATS_TEST_TMPDIR}/marker2" \
    run task -t "$(fixture_taskfile)" pr-prep
  [ "$status" -eq 0 ]
  [ -f "${BATS_TEST_TMPDIR}/marker2" ]
}
```

- [ ] **Step 2: Run the bats suite**

Run: `task test:bats`

Expected: 9 tests, 0 failures.

- [ ] **Step 3: Commit**

Suggested message: `test(pr-prep-lock): I-4 releases_on_normal_exit`.

---

### Task 10: I-5 — Lock releases on SIGKILL (with synchronization model)

**Why:** the most subtle test. Per spec §"Synchronization model for I-5", the test MUST poll for release rather than asserting immediate availability. The kernel cleanup is sub-millisecond on modern macOS/Linux but is not synchronous with the SIGKILL syscall.

**Files:**

- Modify: `scripts/tests/pr-prep-lock.bats` (append)

- [ ] **Step 1: Append the I-5 test**

```bash
# I-5: when the holder process is SIGKILLed, the next invocation acquires
# the lock within 2s (kernel-managed flock release on fd close).
@test "releases_on_sigkill: lock released within 2s of SIGKILL on holder PID" {
  STUB_SLEEP=30 STUB_MARKER="${BATS_TEST_TMPDIR}/holder-marker" \
    spawn_holder >/dev/null
  HOLDER_BASH_PID=$!

  if ! wait_for_acquire; then
    kill -9 "$HOLDER_BASH_PID" 2>/dev/null || :
    fail "holder never acquired the lock within 5s"
  fi

  HOLDER_PID="$(holder_pid_from_info)"
  [ -n "$HOLDER_PID" ]

  # SIGKILL the holder (the inner task PID, post-exec)
  kill -9 "$HOLDER_PID"

  # Poll for the lock to become available (2s budget, 100ms granularity)
  acquired=0
  for _ in $(seq 1 20); do
    if flock -n "$LOCK_FILE" -c true 2>/dev/null; then
      acquired=1
      break
    fi
    sleep 0.1
  done
  [ "$acquired" -eq 1 ]

  # Reap the bash subprocess; ignore exit code (holder was killed)
  wait "$HOLDER_BASH_PID" 2>/dev/null || :
}
```

- [ ] **Step 2: Run the bats suite**

Run: `task test:bats`

Expected: 10 tests, 0 failures.

If the polling consistently fails (lock never acquired within 2s), investigate. On macOS this should release within microseconds; if you see persistent failures, suspect that the SIGKILL'd PID is not actually the flock-fd-owner (maybe the holder structure has changed). Check by inspecting `info`'s `pid=` against `ps -ef | grep flock`.

- [ ] **Step 3: Commit**

Suggested message: `test(pr-prep-lock): I-5 releases_on_sigkill`.

---

### Task 11: I-6 — Non-zero exit on inner failure

**Why:** verifies that when the inner stub exits non-zero, the harness propagates non-zero (specific code is 201 due to go-task wrapping per spec rev-4 thesis).

**Files:**

- Modify: `scripts/tests/pr-prep-lock.bats` (append)

- [ ] **Step 1: Append the I-6 test**

```bash
# I-6: harness exits non-zero when inner task fails. Specific code is not
# contractual (go-task wraps to 201). Assertion is structural.
@test "nonzero_on_failure: inner stub exit 42 produces non-zero harness exit" {
  STUB_EXIT=42 STUB_SLEEP=0 STUB_MARKER="${BATS_TEST_TMPDIR}/marker" \
    run task -t "$(fixture_taskfile)" pr-prep
  [ "$status" -ne 0 ]
  # NOT asserting [ "$status" -eq 42 ] — go-task wraps to 201 per rev-4.
  # NOT asserting [ "$status" -eq 75 ] — 75 is reserved for lock-busy
  # (the harness's flock -E 75), not propagated to the user.
}
```

- [ ] **Step 2: Run the bats suite**

Run: `task test:bats`

Expected: 11 tests, 0 failures.

- [ ] **Step 3: Commit**

Suggested message: `test(pr-prep-lock): I-6 nonzero_on_failure`.

---

### Task 12: Meta-test — All invariants have named tests

**Why:** the meta-test catches drift between the spec's numbered invariants and the bats suite. If a future change adds I-11 to the spec but not to the test file (or removes I-N from the test file but not the spec), this meta-test fails.

**Files:**

- Modify: `scripts/tests/pr-prep-lock.bats` (append)

- [ ] **Step 1: Append the meta-test**

```bash
# Meta-test: every numbered invariant in the spec MUST have a named bats
# test. Catches drift between the spec and the suite.
@test "all_invariants_have_tests: I-1 through I-10 each map to a named test" {
  local spec="docs/superpowers/specs/2026-04-26-pr-prep-concurrency-safety-design.md"
  local bats_file="scripts/tests/pr-prep-lock.bats"

  [ -f "$spec" ] || fail "spec not found at $spec"
  [ -f "$bats_file" ] || fail "bats file not found at $bats_file"

  # Map each invariant to its expected test name.
  declare -A expected=(
    [I-1]="acquires_when_idle"
    [I-2]="rejects_while_held"
    [I-3]="error_includes_metadata"
    [I-4]="releases_on_normal_exit"
    [I-5]="releases_on_sigkill"
    [I-6]="nonzero_on_failure"
    [I-7]="precondition_message"
    [I-8]="non_blocking_acquire"
    [I-9]="bypass_guard_blocks"
    [I-10]="pr_prep_run_hidden_from_list"
  )

  # Each invariant ID must appear in the spec's invariant table.
  local id
  for id in "${!expected[@]}"; do
    grep -q "^| ${id}\b" "$spec" || fail "invariant $id missing from spec table"
  done

  # Each expected test name must appear in the bats file.
  local name
  for name in "${expected[@]}"; do
    grep -q "@test \"$name" "$bats_file" || fail "test name '$name' missing from $bats_file"
  done
}
```

**Note:** Task 8 split I-2/I-3/I-8 into three real tests (each enforcing one invariant). The meta-test enumerates the test names declared across Tasks 4–11 and verifies each appears in the spec invariant table.

- [ ] **Step 2: Run the bats suite**

Run: `task test:bats`

Expected: 12 tests, 0 failures, 0 skipped.

Per-task breakdown: T4=1 (I-7), T5=1 (I-1), T6=1 (I-10), T7=2 (I-9 negative + positive), T8=3 (I-2, I-3, I-8), T9=1 (I-4), T10=1 (I-5), T11=1 (I-6), T12=1 (meta). Total = 12. None skipped.

If a test fails or the count differs, the meta-test's failure message names which invariant ID is missing or which test name doesn't appear in the spec — fix per the message.

- [ ] **Step 3: Commit**

Suggested message: `test(pr-prep-lock): meta-test for invariant-test name alignment`.

---

## Phase C — Production wiring

### Task 13: Wire `pr-prep` and `pr-prep:run` in production `Taskfile.yaml`

**Why:** the fixture proves the harness works. Now apply the same shape to the production `Taskfile.yaml`, leaving the existing 9-step body verbatim under the new `pr-prep:run` task.

**Files:**

- Modify: `Taskfile.yaml` (lines ~471–499 — replace the existing `pr-prep` task with the locked version + new `pr-prep:run` containing the original body verbatim)

- [ ] **Step 1: Read the existing `pr-prep` body verbatim**

Find the start line dynamically (Task 1 added 3 brew packages to the setup line, and the line number cited in the spec may have drifted by ±1 if any preceding tasks changed the file):

Run: `rg -n "^  pr-prep:" Taskfile.yaml`

Then read from the matched line to the end of the task. Suppose `rg` reports `pr-prep:` at line N; run:

```bash
N=<line-from-rg>
sed -n "${N},$((N+30))p" Taskfile.yaml
```

Expected: the `pr-prep:` task definition with the 9 cmds (schema check, license:check, plugin:build-all, lint, fmt:check, test, test:int, test:e2e, final echo).

Save this verbatim — Step 2 reproduces the body inside `pr-prep:run`.

- [ ] **Step 2: Replace the `pr-prep` task body with the lock harness**

Edit `Taskfile.yaml` lines ~471–499. Replace:

```yaml
pr-prep:
  desc: Run all CI checks locally before pushing (mirrors ALL CI jobs)
  cmds:
    - echo "▸ Verifying schema is current..."
    - task: generate:schema
    - cmd: |
        SCHEMA=schemas/plugin.schema.json
        BEFORE=$(sha256sum "$SCHEMA" | cut -d' ' -f1)
        go generate ./internal/plugin/
        AFTER=$(sha256sum "$SCHEMA" | cut -d' ' -f1)
        if [ "$BEFORE" != "$AFTER" ]; then
          echo "ERROR: Schema out of sync with Go types. Run 'task generate:schema' and commit."
          exit 1
        fi
    - echo "▸ Checking license headers..."
    - task: license:check
    - echo "▸ Building binary plugins..."
    - task: plugin:build-all
    - echo "▸ Running linters..."
    - task: lint
    - echo "▸ Checking formatting..."
    - task: fmt:check
    - echo "▸ Running unit tests..."
    - task: test
    - echo "▸ Running integration tests..."
    - task: test:int
    - echo "▸ Running E2E tests..."
    - task: test:e2e
    - echo "✓ All PR checks passed."
```

With:

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
          echo "ERROR: lock acquire failed unexpectedly (rc=$rc); see flock(1) output above." >&2
          exit "$rc"
        fi

pr-prep:run:
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
    - echo "▸ Verifying schema is current..."
    - task: generate:schema
    - cmd: |
        SCHEMA=schemas/plugin.schema.json
        BEFORE=$(sha256sum "$SCHEMA" | cut -d' ' -f1)
        go generate ./internal/plugin/
        AFTER=$(sha256sum "$SCHEMA" | cut -d' ' -f1)
        if [ "$BEFORE" != "$AFTER" ]; then
          echo "ERROR: Schema out of sync with Go types. Run 'task generate:schema' and commit."
          exit 1
        fi
    - echo "▸ Checking license headers..."
    - task: license:check
    - echo "▸ Building binary plugins..."
    - task: plugin:build-all
    - echo "▸ Running linters..."
    - task: lint
    - echo "▸ Checking formatting..."
    - task: fmt:check
    - echo "▸ Running unit tests..."
    - task: test
    - echo "▸ Running integration tests..."
    - task: test:int
    - echo "▸ Running E2E tests..."
    - task: test:e2e
    - echo "✓ All PR checks passed."
```

**Note:** `pr-prep:run` has neither `desc:` nor `internal: true`. It will be hidden from `task --list` (no desc) but reachable by `exec task pr-prep:run` (no internal flag).

- [ ] **Step 3: Verify `task --list` no longer shows `pr-prep:run`**

Run: `task --list | grep -E "^\* pr-prep"`

Expected: only `* pr-prep:` appears (with description); `pr-prep:run` is absent.

Run: `task --list-all | grep -E "^\* pr-prep"`

Expected: BOTH `* pr-prep:` AND `* pr-prep:run:` appear.

- [ ] **Step 4: YAML-level assertion via `yq`**

Run:

```bash
yq '.tasks["pr-prep:run"].desc' Taskfile.yaml
yq '.tasks["pr-prep:run"].internal' Taskfile.yaml
```

Expected: both print `null`.

- [ ] **Step 5: Run `task fmt`**

Run: `task fmt`

Expected: no errors.

- [ ] **Step 6: Smoke-test the production harness with the bypass env var (so we don't tie up Docker for E2E in this validation step)**

Don't actually run `task pr-prep` here — that's covered by Task 19's smoke test. Just verify the YAML parses correctly:

Run: `task --list-all >/dev/null && echo "Taskfile parses"`

Expected: `Taskfile parses`.

- [ ] **Step 7: Commit**

Suggested message: `feat(tasks): wrap pr-prep in flock harness; add internal pr-prep:run`.

---

### Task 14: Add bats suite to `pr-prep:run` body and CI

**Why:** the bats tests should run as part of `pr-prep` (they're cheap — under 30s — and they catch lock-harness regressions). They also need to run in CI. Add as the very first step of `pr-prep:run`'s body so any lock-harness regression fails fast before the slow CI body.

**Files:**

- Modify: `Taskfile.yaml` `pr-prep:run` task body (insert `task: test:bats` near the top, after the bypass guard but before the schema check)

- [ ] **Step 1: Insert `task test:bats` at the top of `pr-prep:run`**

After the bypass-guard cmd in `pr-prep:run`, before the existing `echo "▸ Verifying schema is current..."`, insert:

```yaml
    - echo "▸ Running bats shell tests..."
    - task: test:bats
```

The full top of `pr-prep:run` becomes:

```yaml
pr-prep:run:
  cmds:
    - cmd: |
        if [ "${HOLOMUSH_PR_PREP_BYPASS_LOCK:-}" != "1" ]; then
          ...bypass-guard error...
          exit 1
        fi
    - echo "▸ Running bats shell tests..."
    - task: test:bats
    - echo "▸ Verifying schema is current..."
    - task: generate:schema
    ...remaining steps unchanged...
```

- [ ] **Step 2: Verify by running pr-prep:run directly with the bypass env**

Run: `HOLOMUSH_PR_PREP_BYPASS_LOCK=1 task pr-prep:run` (this WILL run the full pr-prep body — see Step 4 for the abort path if you want to bail early)

Watch the output. The `▸ Running bats shell tests...` line should appear immediately, followed by the bats test runner output, and then the rest of the pr-prep body.

If you want to abort after the bats step (so you don't tie up the machine for 5–15 min), press Ctrl-C after `▸ Running bats shell tests...` finishes. Or skip Step 2 and rely on Task 19's full validation.

- [ ] **Step 3: Verify CI compatibility (no workflow change expected)**

CI does NOT invoke `task pr-prep` directly. The CI workflows call the individual subtasks (`task lint`, `task test:cover`, `task test:int`, `task test:e2e:cover`, `task generate:schema`, etc.) so the lock harness is irrelevant to CI by construction.

Verify by searching the workflows:

Run: `rg -n "task pr-prep|pr-prep:run" .github/`

Expected: zero or only-prose hits (a comment in `.github/workflows/nightly-soak.yml` is acceptable). If any CI workflow actually invokes `task pr-prep` or `task pr-prep:run`, the harness DOES affect it: `task pr-prep` would acquire-and-release the lock (uncontended on a fresh runner — no functional impact, just a few ms of overhead); `task pr-prep:run` would fail at the bypass guard unless the workflow exports `HOLOMUSH_PR_PREP_BYPASS_LOCK=1`. If you find such a workflow, update it inline to either add the env var (for `pr-prep:run` callers) or rename to `task pr-prep` (for `pr-prep:run` callers that should accept the lock).

- [ ] **Step 4: Run `task fmt`**

Run: `task fmt`

Expected: no errors.

- [ ] **Step 5: Commit**

Suggested message: `feat(tasks): run bats suite as first step of pr-prep:run`.

---

## Phase D — Documentation

### Task 15: Create `site/docs/contributing/pr-prep.md`

**Why:** operator-facing page describing the gate, when to run it, and what to do on collision. Per spec §"Documentation Deliverables", this is the canonical home for collision-recovery guidance (NOT `pr-guide.md`, per design-reviewer rev-1 feedback).

**Files:**

- Create: `site/docs/contributing/pr-prep.md`

- [ ] **Step 1: Write the page**

Create `site/docs/contributing/pr-prep.md`:

````markdown
# Pre-Push Quality Gate (`task pr-prep`)

`task pr-prep` is the project's mandatory pre-push gate. It mirrors every CI job (schema check, license check, plugin builds, lint, format, unit tests, integration tests, E2E in Docker) and MUST pass before pushing to a PR branch.

A full run takes 5–15 minutes and is CPU- and Docker-heavy.

## What it does

1. Bats shell tests (concurrency-lock harness)
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
- **Kill it if wedged.** `kill <pid>` (or `kill -9 <pid>` if it doesn't respond). The kernel auto-releases the flock on process death; the next `task pr-prep` invocation will acquire cleanly.
- **Manual escape hatch (last resort):** `rm -f ${TMPDIR:-/tmp}/holomush-pr-prep/lock`. Only use this if the holder is gone but stale state somehow remains — the kernel-managed release should make this unnecessary.

## How the lock works

`flock(1)` opens a file under `${TMPDIR:-/tmp}/holomush-pr-prep/lock` and holds it for the duration of the inner CI body. The kernel automatically releases the lock when the holder process dies (any signal, including SIGKILL, OOM, or kernel panic recovery), so there is no stale-lock-cleanup procedure to worry about.

The lock is per-user on macOS (`$TMPDIR` is automatically per-user there). On Linux it is namespaced by directory under `/tmp`.

## CI behavior

CI runs `task pr-prep` on a fresh single-tenant runner per job, so the lock is always uncontended. No CI workflow change is needed for this gate.

## Direct invocation of `pr-prep:run`

`pr-prep:run` is the inner body that contains the actual 9-step CI mirror. **Do not invoke it directly from a contributor's machine** — it skips the concurrency lock and will race with any other `pr-prep` running on the same machine.

If you have a legitimate reason to invoke it directly (debugging a single step in isolation, for example), set `HOLOMUSH_PR_PREP_BYPASS_LOCK=1`:

```bash
HOLOMUSH_PR_PREP_BYPASS_LOCK=1 task pr-prep:run
```

Without that env var, `pr-prep:run` exits 1 with a message pointing at the bypass.

## Related

- Spec: `docs/superpowers/specs/2026-04-26-pr-prep-concurrency-safety-design.md`
- Tracking bead: `holomush-71zq`
````

- [ ] **Step 2: Lint the new doc**

Run: `task fmt`

(Note: `task fmt` formats the whole repo; the `fmt:*` sub-tasks don't accept CLI args. This is idempotent — already-formatted files are unchanged.)

Expected: no errors. If `rumdl` flags anything, fix in place. (The page uses backtick-fenced code blocks with explicit `text` and `bash` languages, surrounded by blank lines, per `docs/CLAUDE.md`.)

- [ ] **Step 3: Commit**

Suggested message: `docs(contributing): add pr-prep gate page with collision-recovery guide`.

---

### Task 16: Link `pr-prep.md` from `site/docs/contributing/index.md`

**Why:** the new page must be discoverable from the contributing index.

**Files:**

- Modify: `site/docs/contributing/index.md`

- [ ] **Step 1: Read the current index**

Run: `cat site/docs/contributing/index.md`

Identify the natural insertion point — likely a list of contributor guides or an "Operations" section.

- [ ] **Step 2: Insert a link**

Add a new bullet (or section) referencing the new page. Example insertion (adapt to the existing structure):

```markdown
- **[Pre-Push Quality Gate (`task pr-prep`)](pr-prep.md)** — what runs, concurrent-run behavior, collision recovery
```

If the index uses a more elaborate per-page card or table format, follow that pattern instead.

- [ ] **Step 3: Lint**

Run: `task fmt`

Expected: no errors.

- [ ] **Step 4: Commit**

Suggested message: `docs(contributing): link pr-prep.md from index`.

---

### Task 17: Add a callout to `site/docs/contributing/pr-guide.md`

**Why:** `pr-guide.md` is the workflow doc operators read first. It should briefly mention that `pr-prep` is now serialized and link to `pr-prep.md` for collision-recovery details.

**Files:**

- Modify: `site/docs/contributing/pr-guide.md`

- [ ] **Step 1: Read the current pr-guide**

Run: `cat site/docs/contributing/pr-guide.md`

Currently has zero `pr-prep` mentions (verified during design phase). Find the section about pre-push checks or "before opening a PR" — that's the natural insertion point.

- [ ] **Step 2: Add the callout**

Insert a paragraph at the appropriate spot. Suggested wording:

```markdown
Before pushing to a PR branch, run `task pr-prep` to mirror every CI job locally. The gate is serialized per user — only one `task pr-prep` runs at a time on a given machine. If you see a "another pr-prep is running" error, wait for the holder to finish or kill it; see [Pre-Push Quality Gate](pr-prep.md) for details.
```

- [ ] **Step 3: Lint**

Run: `task fmt`

Expected: no errors.

- [ ] **Step 4: Commit**

Suggested message: `docs(contributing): pr-guide.md links to pr-prep.md for collision recovery`.

---

### Task 18: Update CLAUDE.md with serialization note

**Why:** CLAUDE.md has three sites mentioning `task pr-prep` (lines 547, 550, 876). Each gets a single sentence linking to the new doc.

**Files:**

- Modify: `CLAUDE.md` (lines 547, 550, 876 — exact line numbers may drift; locate via `rg`)

- [ ] **Step 1: Locate the three sites**

Run: `rg -n "task pr-prep" CLAUDE.md`

Expected: three hits (or possibly more if other text mentions it; verify each).

- [ ] **Step 2: Identify which sites need the serialization note**

Read the surrounding context for each hit. The sites describing what `task pr-prep` is and when to run it (lines 547 and 550 area) need the note. The "Landing the Plane" mention (line 876) likely doesn't need it — it's a step in a sequence, not a definition.

Pick ONE site for the canonical note (the most prominent one — typically line 547's MUST rule), and leave the others to reference the canonical site or the new contributing doc.

- [ ] **Step 3: Update the canonical site**

Find the existing sentence:

```text
**MUST** run `task pr-prep` before creating a PR or pushing to a PR branch.
```

Add a sentence after it:

```text
The gate is serialized per user — only one `task pr-prep` runs at a time on a given machine. See [pr-prep](site/docs/contributing/pr-prep.md) for collision behavior.
```

- [ ] **Step 4: Verify other sites don't contradict**

Re-read lines 550 and 876. If either makes a claim incompatible with serialization (e.g., "always run two pr-preps in parallel"), reconcile. If they just reference the gate without details, leave them.

- [ ] **Step 5: Lint**

Run: `task fmt`

Expected: no errors.

- [ ] **Step 6: Commit**

Suggested message: `docs(claude): note pr-prep serialization at the canonical MUST rule`.

---

## Phase E — Validation

### Task 19: Acceptance smoke test (acceptance criterion #3)

**Why:** spec §"Acceptance Criteria" item 3 requires the implementer to smoke-test `task pr-prep` end-to-end on the maintainer's macOS workstation: (a) one full successful run, (b) one deliberate collision, (c) one post-success retry. This is operator attestation; the bats suite enforces all numbered invariants automatically.

**Files:**

- No file changes. This is a manual validation task.

- [ ] **Step 1 (a): Full successful run**

Open a clean terminal in the project root.

Run: `time task pr-prep`

Expected: 5–15 minutes; exits 0 with `✓ All PR checks passed.` as the final line. The lock acquires, info populates, the bats suite runs first (under 30s), then the rest of the body runs.

If this fails: check the failure mode. If it's a real CI breakage (lint/test fail), fix it; that's regular project work. If it's a lock-harness regression, debug via the bats suite first.

- [ ] **Step 2 (b): Deliberate collision**

In one terminal, start a holder:

```bash
task pr-prep
```

In another terminal (any directory inside the same repo or any workspace), invoke the gate again:

```bash
task pr-prep
```

Expected: the second invocation exits non-zero within ~1 second with the COLLIDED stderr message. Verify the message contains:

- `pid=` line with the first holder's PID
- `workspace=` line with the first holder's workspace path
- `started_at=` line with the first holder's start timestamp
- `Lock file:` line with the path

Cancel the first holder (Ctrl-C is fine) — its inner body will abort, the lock will release.

- [ ] **Step 3 (c): Post-success retry**

After the first holder from Step 1 (or Step 2's first holder) completes, run `task pr-prep` again from any workspace.

Expected: acquires the lock cleanly without error; runs the inner body to completion.

- [ ] **Step 4: Document the smoke test in the implementation PR description**

When the PR is opened, include a short "Smoke test" section in the description naming the three runs above and their outcomes. The reviewer (human or AI) can then verify.

Suggested PR-description excerpt:

```markdown
## Smoke test

Per spec acceptance criterion #3, validated end-to-end on macOS:

- (a) `task pr-prep` ran to completion in <duration> with exit 0; bats suite passed; full body ran.
- (b) `task pr-prep` in a second terminal while (a) was running exited 1 within ~1s; stderr contained the holder's pid/workspace/started_at; the lockfile path was correct.
- (c) After (a) completed, a fresh `task pr-prep` from another workspace acquired the lock cleanly and ran without error.
```

- [ ] **Step 5: No commit needed for this validation task**

The task produces no file changes. Mark complete in beads and move to PR creation.

---

## Self-Review

Performed inline against the spec rev-6 (`docs/superpowers/specs/2026-04-26-pr-prep-concurrency-safety-design.md`):

**Spec coverage check:** every spec section maps to at least one task:

- §Problem (today's failure mode) — addressed by every task; the entire plan is the fix
- §Goals 1–7 — implemented by Tasks 13 (entry point unchanged, harness wrapper), 1 (portability via brew), 4–12 (testability)
- §Non-goals — N/A (these are scope statements; no tasks needed)
- §Lock Primitive — Task 1 (flock dep), Task 13 (production wiring)
- §Lock File Location — Task 13
- §Lock Harness — Task 3 (fixture), Task 13 (production)
- §Why this avoids prior-revision issues — N/A (historical narrative)
- §Empirical Verification — Task 19 (operator attestation reuses the rev-3 POC pattern at production scope)
- §Setup Change — Task 1
- §Documentation Deliverables — Tasks 15, 16, 17, 18
- §Numbered Invariants I-1..I-10 — Tasks 4 (I-7), 5 (I-1), 6 (I-10), 7 (I-9), 8 (I-2/I-3/I-8), 9 (I-4), 10 (I-5), 11 (I-6), 12 (meta)
- §Test Plan — Tasks 2, 3 (infrastructure); 4–12 (per-test); fixture in Task 3
- §Failure Modes & Edge Cases — N/A as standalone task; the harness implementation in Task 13 handles all listed cases per the diff in the spec
- §Acceptance Criteria 1–7 — implemented across the plan; criterion #3 is Task 19; criterion #4 (CI behavior) is verified in Task 14 Step 3; criterion #5 (docs) is Tasks 15–18; criterion #7 (flock in setup) is Task 1

**Placeholder scan:** searched for "TBD", "TODO", "implement later", "fill in details", "(see above)", "(similar to)". None found. Each step has the actual content.

**Type consistency:** the harness shape (variable names, env var name, exit code 75) is identical between Task 3 (fixture), Task 13 (production), Task 14 (bats integration), and Task 15 (docs page). The bypass env var is consistently named `HOLOMUSH_PR_PREP_BYPASS_LOCK` everywhere.

**Test-name consistency:** the meta-test in Task 12 enumerates the exact test names declared in Tasks 4, 5, 6, 7, 8, 9, 10, 11. Verified: `acquires_when_idle` (Task 5, expected by meta), `rejects_while_held` (Task 8, expected by meta), `error_includes_metadata` (Task 8 alias, expected by meta), `releases_on_normal_exit` (Task 9, expected), `releases_on_sigkill` (Task 10, expected), `nonzero_on_failure` (Task 11, expected), `precondition_message` (Task 4, expected), `non_blocking_acquire` (Task 8 alias, expected), `bypass_guard_blocks` (Task 7, expected), `pr_prep_run_hidden_from_list` (Task 6, expected). All ten match.

No issues found. Plan is ready for execution gate (plan-reviewer).

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-26-pr-prep-concurrency-safety.md`. **Two execution options:**

**1. Subagent-Driven (recommended)** — fresh subagent per task, review between tasks, fast iteration. Each task becomes a sub-bead under `holomush-71zq` and a fresh subagent implements it in this workspace (or its own workspace per the `holomush-8722` hooks once they land).

**2. Inline Execution** — run tasks in this session via `superpowers:executing-plans`, batch execution with checkpoints for review.

**Which approach?**
