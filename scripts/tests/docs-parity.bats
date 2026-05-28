#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

# INV-1 (nav parity): every slug in the zensical nav fixture has a built page.
# INV-5 (migration completeness): every source page has a corresponding built page.
#
# Requires a current site/dist from `bunx astro build` (the Taskfile drives that
# before invoking this suite). The fixture count test guards against silent shrinkage
# of zensical-nav.txt after zensical.toml is removed (Task 19).

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
  NAV_FIXTURE="$REPO_ROOT/scripts/tests/fixtures/zensical-nav.txt"
  PARITY_SCRIPT="$REPO_ROOT/scripts/check-docs-parity.sh"
}

@test "zensical-nav fixture contains exactly 43 slugs (INV-1 guard)" {
  count=$(wc -l < "$NAV_FIXTURE" | tr -d ' ')
  assert_equal "$count" "43"
}

@test "docs parity check passes — all nav slugs and source pages have built pages (INV-1 + INV-5)" {
  run bash "$PARITY_SCRIPT"
  assert_success
  assert_output --partial "✓ nav parity: 43/43"
  assert_output --partial "✓ page migration: 64/64"
}
