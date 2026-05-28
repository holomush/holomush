#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

# Test the glob->regex compilation for DOCS_ONLY_PATHS.
# The helper reads DOCS_ONLY_PATHS from Taskfile.yaml via yq and emits
# one anchored extended regex.

setup() {
  REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  HELPER="$REPO_ROOT/scripts/docs-paths-regex.sh"
}

# Happy path: run against the real Taskfile.yaml in the repo root.
run_helper() {
  REGEX="$(bash "$HELPER")"
}

@test "regex matches site/ paths" {
  run_helper
  echo "site/src/content/docs/index.md" | grep -E "$REGEX"
}

@test "regex matches docs/ paths" {
  run_helper
  echo "docs/superpowers/specs/foo.md" | grep -E "$REGEX"
}

@test "regex matches root README.md" {
  run_helper
  echo "README.md" | grep -E "$REGEX"
}

@test "regex matches nested *.md (web/CLAUDE.md)" {
  run_helper
  echo "web/CLAUDE.md" | grep -E "$REGEX"
}

@test "regex matches .claude/agents/*.md" {
  run_helper
  echo ".claude/agents/code-reviewer.md" | grep -E "$REGEX"
}

@test "regex matches .github/PULL_REQUEST_TEMPLATE.md (intentional per spec §4.1)" {
  run_helper
  echo ".github/PULL_REQUEST_TEMPLATE.md" | grep -E "$REGEX"
}

@test "regex does NOT match internal/foo.go" {
  run_helper
  run bash -c "echo internal/foo.go | grep -E '$REGEX'"
  [ "$status" -ne 0 ]
}

@test "regex does NOT match .claude/hooks/pre-commit.sh" {
  run_helper
  run bash -c "echo .claude/hooks/pre-commit.sh | grep -E '$REGEX'"
  [ "$status" -ne 0 ]
}

@test "regex does NOT match .claude/settings.json" {
  run_helper
  run bash -c "echo .claude/settings.json | grep -E '$REGEX'"
  [ "$status" -ne 0 ]
}

@test "regex does NOT match .github/workflows/ci.yaml" {
  run_helper
  run bash -c "echo .github/workflows/ci.yaml | grep -E '$REGEX'"
  [ "$status" -ne 0 ]
}

@test "regex matches literal LICENSE (no extension)" {
  run_helper
  echo "LICENSE" | grep -E "$REGEX"
}

@test "regex does NOT match LICENSE.txt (literal LICENSE only)" {
  run_helper
  run bash -c "echo LICENSE.txt | grep -E '$REGEX'"
  [ "$status" -ne 0 ]
}

@test "helper fails (does NOT emit ^(null)$) when DOCS_ONLY_PATHS is missing" {
  # Fixture: a Taskfile.yaml without DOCS_ONLY_PATHS. Override REPO_ROOT
  # via env (helper uses ${REPO_ROOT:-...}).
  FIX="$(mktemp -d)"
  cat > "$FIX/Taskfile.yaml" <<'YAML'
version: "3"
vars:
  BINARY_NAME: holomush
YAML
  run env REPO_ROOT="$FIX" bash "$HELPER"
  [ "$status" -ne 0 ]
  [[ "$output" == *"DOCS_ONLY_PATHS"* ]]
  # CRITICAL: must NOT silently emit the broken regex.
  [[ "$output" != "^(null)$" ]]
  rm -rf "$FIX"
}

@test "helper fails when DOCS_ONLY_PATHS is literally null" {
  FIX="$(mktemp -d)"
  cat > "$FIX/Taskfile.yaml" <<'YAML'
version: "3"
vars:
  DOCS_ONLY_PATHS:
YAML
  run env REPO_ROOT="$FIX" bash "$HELPER"
  [ "$status" -ne 0 ]
  [[ "$output" != "^(null)$" ]]
  rm -rf "$FIX"
}
