#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

setup_file() {
  if [ ! -f "scripts/tests/Taskfile.test.yaml" ]; then
    echo "ERROR: bats must be invoked from the repo root (try 'task test:bats')." >&2
    exit 1
  fi
}

setup() {
  bats_load_library bats-support
  bats_load_library bats-assert
}

# INV-3: no reference to lefthook in live config/docs. Archived plans/specs
# and the gcio6 spec/plan/ADR (which legitimately discuss the retirement) are
# out of scope — the guard is a fixed allowlist of LIVE files.
@test "lefthook.yaml does not exist" {
  run test -f lefthook.yaml
  assert_failure
}

@test "no lefthook references in live config/docs" {
  run rg -i -n lefthook Taskfile.yaml cog.toml docs/CLAUDE.md CLAUDE.md
  assert_failure # rg exits 1 when there are no matches
}
