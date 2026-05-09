#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

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
  bats_load_library bats-support
  bats_load_library bats-assert
  init_test_env
}

# I-7: precondition message when flock is missing.
@test "precondition_message: missing flock fails with platform-specific install hint" {
  # Build a PATH that contains everything needed (task, sh, awk, ...) but
  # NOT flock. We do this by symlinking each required tool into a per-test
  # tmp bin dir, then setting PATH to that dir alone. This keeps flock(1)
  # invisible to `command -v flock` regardless of where it lives on the
  # host (macOS: /opt/homebrew/bin; Linux: /usr/bin).
  local sandbox_bin="${BATS_TEST_TMPDIR}/bin"
  mkdir -p "$sandbox_bin"
  for tool in task sh bash env awk sed grep cat printf echo date mkdir rm test ls true false; do
    local resolved
    resolved="$(command -v "$tool" 2>/dev/null)" || continue
    ln -s "$resolved" "$sandbox_bin/$tool"
  done

  PATH="$sandbox_bin" run task -t "$(fixture_taskfile)" pr-prep
  [ "$status" -ne 0 ]
  [[ "$output" == *"brew install flock"* ]]
  [[ "$output" == *"apt install util-linux"* ]]
}

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

# I-10: pr-prep:run MUST NOT appear in `task --list`. YAML-level: the task
# entry exists, has no `desc:`, and has no `internal: true`. Asserted against
# BOTH the fixture (where the lock idiom lives during testing) AND the
# production Taskfile.yaml (so a regression that adds `desc:` to production
# is caught — the fixture and production share the contract).
@test "pr_prep_run_hidden_from_list: not in task --list; YAML asserts no desc and no internal (fixture + production)" {
  local target
  for target in "$(fixture_taskfile)" "Taskfile.yaml"; do
    # CLI-output check (supplementary).
    run task -t "$target" --list
    [ "$status" -eq 0 ]
    if [[ "$output" == *"pr-prep:run"* ]]; then
      printf 'pr-prep:run unexpectedly appeared in `task --list` for %s\n%s\n' \
        "$target" "$output" >&2
      return 1
    fi

    # YAML-level (primary contract). Robust against go-task version drift.
    run yq '.tasks["pr-prep:run"]' "$target"
    [ "$status" -eq 0 ]
    [ -n "$output" ]
    [ "$output" != "null" ]

    run yq '.tasks["pr-prep:run"].desc' "$target"
    [ "$output" = "null" ] || {
      printf 'pr-prep:run.desc must be null in %s, was: %s\n' "$target" "$output" >&2
      return 1
    }

    run yq '.tasks["pr-prep:run"].internal' "$target"
    [ "$output" = "null" ] || {
      printf 'pr-prep:run.internal must be null in %s, was: %s\n' "$target" "$output" >&2
      return 1
    }
  done
}

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
  [ "$elapsed" -lt 2000 ]
  end_collision
}

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

# I-5: when the holder process is SIGKILLed, the next invocation acquires
# the lock within 2s (kernel-managed flock release on fd close).
@test "releases_on_sigkill: lock released within 2s of SIGKILL on holder PID" {
  STUB_SLEEP=30 STUB_MARKER="${BATS_TEST_TMPDIR}/holder-marker" \
    spawn_holder &
  HOLDER_BASH_PID=$!

  if ! wait_for_acquire; then
    kill -9 "$HOLDER_BASH_PID" 2>/dev/null || :
    fail "holder never acquired the lock within 5s"
  fi

  HOLDER_PID="$(holder_pid_from_info)"
  [ -n "$HOLDER_PID" ]

  # SIGKILL every process that has the lock fd open. The inner `task` PID
  # in the info file is one such process, but its child (the stub's
  # `sleep`) inherited the fd and would keep the lock alive if we only
  # killed the parent. Process-group kill is unsafe under bats because
  # bats runs without job control — every process here shares the bats
  # PGID, so `kill -- -$PGID` would euthanize the test runner. lsof gives
  # us exactly the set we want: processes with the fd open.
  LOCK_HOLDERS="$(lsof -t "$LOCK_FILE" 2>/dev/null | tr '\n' ' ')"
  [ -n "$LOCK_HOLDERS" ]
  # shellcheck disable=SC2086 # word-splitting intentional
  kill -9 $LOCK_HOLDERS 2>/dev/null || :

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
    grep -Eq "^\| ${id}[[:space:]]*\|" "$spec" || fail "invariant $id missing from spec table"
  done

  # Each expected test name must appear in the bats file.
  local name
  for name in "${expected[@]}"; do
    grep -q "@test \"$name" "$bats_file" || fail "test name '$name' missing from $bats_file"
  done
}
