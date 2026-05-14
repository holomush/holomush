#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

# Verify lint-docs-paths-sync.sh detects drift across the canonical
# Taskfile var and the four mirror locations in ci.yaml + ci-docs-skip.yaml.
# Tests use fixtures, not the real repo files, so they're independent of
# other tasks' ordering.

setup() {
  REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  HELPER="$REPO_ROOT/scripts/lint-docs-paths-sync.sh"
  TMPDIR_FIX="$(mktemp -d)"
  mkdir -p "$TMPDIR_FIX/.github/workflows"
}

teardown() {
  rm -rf "$TMPDIR_FIX"
}

write_taskfile() {
  cat > "$TMPDIR_FIX/Taskfile.yaml" <<'YAML'
version: "3"
vars:
  DOCS_ONLY_PATHS: |
    site/**
    docs/**
    **/*.md
YAML
}

write_taskfile_missing_var() {
  cat > "$TMPDIR_FIX/Taskfile.yaml" <<'YAML'
version: "3"
vars:
  BINARY_NAME: holomush
YAML
}

write_ci_yaml() {
  local push_paths="$1" pr_paths="$2"
  cat > "$TMPDIR_FIX/.github/workflows/ci.yaml" <<YAML
name: CI
on:
  push:
    branches: [main]
    paths-ignore:
$push_paths
  pull_request:
    branches: [main]
    paths-ignore:
$pr_paths
YAML
}

write_ci_skip_yaml() {
  local push_paths="$1" pr_paths="$2"
  cat > "$TMPDIR_FIX/.github/workflows/ci-docs-skip.yaml" <<YAML
name: CI
on:
  push:
    branches: [main]
    paths:
$push_paths
  pull_request:
    branches: [main]
    paths:
$pr_paths
YAML
}

@test "matching across all five extraction points passes" {
  write_taskfile
  PATHS="      - site/**
      - docs/**
      - \"**/*.md\""
  write_ci_yaml "$PATHS" "$PATHS"
  write_ci_skip_yaml "$PATHS" "$PATHS"
  run env REPO_ROOT="$TMPDIR_FIX" bash "$HELPER"
  [ "$status" -eq 0 ]
}

@test "drift between Taskfile and ci.yaml fails" {
  write_taskfile
  DRIFTED="      - site/**
      - docs/**"
  PATHS="      - site/**
      - docs/**
      - \"**/*.md\""
  write_ci_yaml "$DRIFTED" "$DRIFTED"
  write_ci_skip_yaml "$PATHS" "$PATHS"
  run env REPO_ROOT="$TMPDIR_FIX" bash "$HELPER"
  [ "$status" -ne 0 ]
  [[ "$output" == *"ci.yaml"* ]]
}

@test "drift between ci.yaml and ci-docs-skip.yaml fails" {
  write_taskfile
  PATHS="      - site/**
      - docs/**
      - \"**/*.md\""
  DRIFTED="      - site/**
      - docs/**"
  write_ci_yaml "$PATHS" "$PATHS"
  write_ci_skip_yaml "$DRIFTED" "$DRIFTED"
  run env REPO_ROOT="$TMPDIR_FIX" bash "$HELPER"
  [ "$status" -ne 0 ]
  [[ "$output" == *"ci-docs-skip.yaml"* ]]
}

@test "drift between push.paths-ignore and pull_request.paths-ignore in ci.yaml fails" {
  write_taskfile
  PUSH="      - site/**
      - docs/**
      - \"**/*.md\""
  PR_DRIFTED="      - site/**
      - docs/**"
  write_ci_yaml "$PUSH" "$PR_DRIFTED"
  write_ci_skip_yaml "$PUSH" "$PUSH"
  run env REPO_ROOT="$TMPDIR_FIX" bash "$HELPER"
  [ "$status" -ne 0 ]
}

@test "missing DOCS_ONLY_PATHS in Taskfile fails (no silent null)" {
  write_taskfile_missing_var
  PATHS="      - site/**"
  write_ci_yaml "$PATHS" "$PATHS"
  write_ci_skip_yaml "$PATHS" "$PATHS"
  run env REPO_ROOT="$TMPDIR_FIX" bash "$HELPER"
  [ "$status" -ne 0 ]
  [[ "$output" == *"DOCS_ONLY_PATHS"* ]]
}
