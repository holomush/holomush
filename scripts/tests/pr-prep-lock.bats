#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

bats_load_library_safe bats-support 2>/dev/null || true
bats_load_library_safe bats-assert 2>/dev/null || true
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
