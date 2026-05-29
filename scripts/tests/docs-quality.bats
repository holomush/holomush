#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

# SP5 docs quality structural invariants (INV-1/2/4/7) via scripts/check-docs-quality.sh.
#
# INV-1: rubric page + audit file both present.
# INV-2: audit row count matches content-page count.
# INV-4: all 5 section index.mdx files contain <CardGrid>.
# INV-7: astro.config.mjs has GitHub repo + Discussions social links.
#
# The advisory terminology grep is non-gating and is NOT asserted here.

setup_file() {
  if [ ! -f "scripts/tests/Taskfile.test.yaml" ]; then
    echo "ERROR: bats must be invoked from the repo root (try 'task test:bats')." >&2
    exit 1
  fi
}

setup() {
  bats_load_library bats-support
  bats_load_library bats-assert
  REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  QUALITY_SCRIPT="$REPO_ROOT/scripts/check-docs-quality.sh"
}

@test "SP5 quality invariants pass: rubric+audit exist, row-parity, CardGrid, social links (INV-1/2/4/7)" {
  run bash "$QUALITY_SCRIPT"
  assert_success
}
