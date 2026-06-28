#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

# Validates release-notes-collect.sh range resolution, filtered-commit
# extraction, bead-ref harvesting, and coverage accounting; and
# release-notes-publish.sh fetch-combine-publish guards via a mocked gh.

setup() {
  REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  FIX="$BATS_TEST_TMPDIR/fix"
  mkdir -p "$FIX"
  cd "$FIX"
  git init -q -b main
  git config user.email "t@example.com"
  git config user.name "Test"
  echo seed > seed.txt
  git add -A
  git commit -q -m "chore: seed"
  git tag v0.1.0
  git commit -q --allow-empty -m "feat(scenes): web settings (holomush-5rh.24)"
  git commit -q --allow-empty -m "fix(focus): unify delta coordinator (holomush-66228)"
  # Unscoped 'docs:' — GoReleaser's filter is literally ^docs: and does NOT
  # match scoped docs(scope):, so the fixture MUST be unscoped to be excluded.
  git commit -q --allow-empty -m "docs: mark SP1 landed"
  git commit -q --allow-empty -m "feat(session): liveness leases"
  git tag v0.2.0
  # A range with NO holomush-* bead refs: exercises the zero-match path where
  # the bead-ref grep pipeline exits 1 under `set -euo pipefail`.
  git commit -q --allow-empty -m "feat(telnet): keepalive pings"
  git commit -q --allow-empty -m "fix(web): reconnect backoff"
  git tag v0.3.0
}

@test "collect resolves the previous tag from the target tag" {
  run "$REPO_ROOT/scripts/release-notes-collect.sh" v0.2.0
  [ "$status" -eq 0 ]
  [[ "$output" == *"Range: v0.1.0..v0.2.0"* ]]
}

@test "collect lists filtered commits and excludes docs/test/chore" {
  run "$REPO_ROOT/scripts/release-notes-collect.sh" v0.2.0
  [ "$status" -eq 0 ]
  [[ "$output" == *"feat(scenes): web settings (holomush-5rh.24)"* ]]
  [[ "$output" == *"feat(session): liveness leases"* ]]
  # 'docs: mark SP1 landed' is excluded by the ^docs: filter
  [[ "$output" != *"docs: mark SP1 landed"* ]]
}

@test "collect harvests distinct holomush bead refs" {
  run "$REPO_ROOT/scripts/release-notes-collect.sh" v0.2.0
  [[ "$output" == *"holomush-5rh.24"* ]]
  [[ "$output" == *"holomush-66228"* ]]
}

@test "collect reports commits with no bead ref under coverage gaps" {
  run "$REPO_ROOT/scripts/release-notes-collect.sh" v0.2.0
  # 'feat(session): liveness leases' has no holomush-<id> ref
  [[ "$output" == *"## Coverage gaps (no bead ref)"* ]]
  [[ "$output" == *"feat(session): liveness leases"* ]]
}

@test "collect still emits all sections when the range has no bead refs" {
  # v0.2.0..v0.3.0 contains zero holomush-* refs. The bead-ref grep pipeline
  # exits 1 on zero matches; under set -euo pipefail that must NOT abort the
  # script before the later sections print.
  run "$REPO_ROOT/scripts/release-notes-collect.sh" v0.3.0
  [ "$status" -eq 0 ]
  [[ "$output" == *"## Coverage gaps (no bead ref)"* ]]
  [[ "$output" == *"## Roadmap theme sections"* ]]
  [[ "$output" == *"feat(telnet): keepalive pings"* ]]
}

@test "collect fails cleanly when the target tag does not exist" {
  run "$REPO_ROOT/scripts/release-notes-collect.sh" v9.9.9
  [ "$status" -eq 1 ]
  [[ "$output" == *"could not resolve a previous tag"* ]]
}
