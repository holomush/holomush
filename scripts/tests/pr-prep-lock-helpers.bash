#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
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
  # Existing lock tests do not exercise docs-only detection. Force the full
  # lane so the detection prologue (added to the fixture in the next step
  # of this task) falls through. The new pr-prep-docs-detection.bats
  # explicitly `unset`s this in its own setup to re-enable detection.
  export HOLOMUSH_PR_PREP_FORCE_FULL=1
}

# Path to the fixture taskfile, computed relative to the repo root.
fixture_taskfile() {
  echo "scripts/tests/Taskfile.test.yaml"
}

# Run the holder in the foreground, redirecting stdout/stderr to test
# tmpdir files. Callers MUST background this and capture $! themselves:
#
#   spawn_holder &
#   HOLDER_BASH_PID=$!
#
# The bash subshell that bats backgrounds is the parent of `task`, which is
# the parent of the `flock`-owned `sh -c` subshell which `exec`s into the
# inner task. Tests that need the inner holder PID (the one written to
# $INFO_FILE by pr-prep:run) should use `holder_pid_from_info` after
# `wait_for_acquire`.
spawn_holder() {
  task -t "$(fixture_taskfile)" pr-prep \
    >"${BATS_TEST_TMPDIR}/holder.out" 2>"${BATS_TEST_TMPDIR}/holder.err"
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

# Spawn a 30s background holder for collision tests. Sets globals
# HOLDER_BASH_PID and HOLDER_PID. Returns 0 on successful acquire,
# fails the test (via bats `fail`) if the holder never acquires.
start_collision() {
  STUB_SLEEP=30 STUB_MARKER="${BATS_TEST_TMPDIR}/holder-marker" \
    spawn_holder &
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
