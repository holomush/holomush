#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

# Validates the repo's cog.toml drives tag-only, v-prefixed, changelog-free
# releases and that cog parses commit subjects release-please choked on
# (INV-6: #4164 arrows, #4094 parens). Runs cog against a throwaway fixture
# repo seeded with the real cog.toml, so assertions are deterministic.

setup() {
  REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  FIX="$BATS_TEST_TMPDIR/fix"
  mkdir -p "$FIX"
  cd "$FIX"
  git init -q -b main
  git config user.email "t@example.com"
  git config user.name "Test"
  cp "$REPO_ROOT/cog.toml" .
  echo seed > seed.txt
  git add -A
  git commit -q -m "chore: seed"
  git tag v0.1.0
}

@test "tag-only bump creates a v-prefixed minor tag on 0.x (tag_prefix + pre-major)" {
  git commit -q --allow-empty -m "feat: a new thing"
  cog bump --auto --disable-bump-commit
  run git describe --tags --abbrev=0
  [ "$status" -eq 0 ]
  [ "$output" = "v0.2.0" ]
}

@test "disable_changelog: tag-only bump writes no CHANGELOG.md" {
  git commit -q --allow-empty -m "feat: a new thing"
  cog bump --auto --disable-bump-commit
  [ ! -f CHANGELOG.md ]
}

@test "tag-only bump creates no commit on the branch (no protected-main write)" {
  before="$(git rev-parse HEAD)"
  git commit -q --allow-empty -m "fix: a fix"
  expected="$(git rev-parse HEAD)"
  cog bump --auto --disable-bump-commit
  [ "$(git rev-parse HEAD)" = "$expected" ]
  [ "$expected" != "$before" ]  # sanity: our own commit landed, cog's did not add another
}

@test "INV-6: arrows in a fix subject parse (regression for #4164)" {
  subject="fix(session): map SESSION_NOT_FOUND/EXPIRED → STREAM_ACCESS_DENIED"
  run cog verify "$subject"
  [ "$status" -eq 0 ]
  git commit -q --allow-empty -m "$subject"
  run cog bump --auto --disable-bump-commit
  [ "$status" -eq 0 ]
}

@test "INV-6: parens in a chore subject parse (regression for #4094)" {
  run cog verify "chore(deps): bump tailwindcss (v4) and @tailwindcss/vite"
  [ "$status" -eq 0 ]
}

@test "commit-lint behavior: malformed PR title is rejected" {
  run cog verify "just some words with no type"
  [ "$status" -ne 0 ]
}
