#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

# SP2 Diátaxis IA invariants (INV-1/2/3/5/6) via scripts/check-docs-ia.sh, plus a
# meta-test guarding the slug-map fixture at exactly 52 rows.
#
# The invariant test requires a current site/dist from `task docs:build`; it skips
# cleanly when absent so an unbuilt tree is a no-op rather than a false failure
# (CI builds it before this suite).

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
  IA_SCRIPT="$REPO_ROOT/scripts/check-docs-ia.sh"
  # Task 10 relocates this to scripts/tests/fixtures/sp2-slug-map.tsv; update then.
  SLUG_MAP="$REPO_ROOT/scripts/migration/sp2-slug-map.tsv"
}

@test "sp2 slug-map fixture has exactly 52 rows (move-count guard)" {
  count=$(grep -c $'\t' "$SLUG_MAP")
  assert_equal "$count" "52"
}

@test "docs IA invariants pass: parity/one-bucket/retired/branding/nav (INV-1/2/3/5/6)" {
  [ -d "$REPO_ROOT/site/dist" ] || skip "site/dist not built — run 'task docs:build' first (CI builds it before this suite)"
  run bash "$IA_SCRIPT"
  assert_success
}
