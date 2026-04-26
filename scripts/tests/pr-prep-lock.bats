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
