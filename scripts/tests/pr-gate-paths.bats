#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

# Verify scripts/pr-gate-paths.sh implements the three-valued exemption
# contract (0 = EXEMPT, 10 = NOT EXEMPT, 1 = ERROR) against the authoritative
# Taskfile.yaml path vars.
#
# Most cases run against the REAL Taskfile vars (read-only) — the matcher
# materializes its inputs in a mktemp scratch git repo, so no real repo file is
# ever written. The fail-loud case uses a fixture Taskfile under mktemp -d,
# mirroring the shape of lint-docs-paths-sync.bats.

setup() {
  REPO_ROOT_REAL="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  HELPER="$REPO_ROOT_REAL/scripts/pr-gate-paths.sh"
  TMPDIR_FIX="$(mktemp -d)"
}

teardown() {
  rm -rf "$TMPDIR_FIX"
}

# Feed a newline-terminated path list on stdin against the real Taskfile.
gate() {
  printf '%s\n' "$@" | "$HELPER"
}

# Same, but with zero paths (no trailing newline, genuinely empty stdin).
gate_empty() {
  printf '' | "$HELPER"
}

# Feed a path list against a fixture REPO_ROOT.
gate_with_root() {
  local root="$1"
  shift
  printf '%s\n' "$@" | env REPO_ROOT="$root" "$HELPER"
}

# A Taskfile carrying the two existing vars but NOT REPO_CONFIG_ONLY_PATHS.
write_taskfile_missing_repo_config_var() {
  cat > "$TMPDIR_FIX/Taskfile.yaml" <<'YAML'
version: "3"
vars:
  DOCS_ONLY_PATHS: |
    site/**
    docs/**
    **/*.md
  DEPENDENCY_ONLY_PATHS: |
    **/go.mod
    **/go.sum

tasks:
  default:
    cmds:
      - echo hi
YAML
}

@test "docs-only diff is exempt" {
  run gate "site/index.md" "docs/x.md" ".planning/a/b.md" "README.md" "LICENSE" ".claude/rules/x.md"
  [ "$status" -eq 0 ]
}

@test "dependency-only diff is exempt, including root go.mod and go.tool*.mod" {
  # Root `go.mod` and `go.tool-lint.mod` are the two shapes that defeat the
  # alternatives rejected in RESEARCH.md (gobwas/glob and docs-paths-regex.sh).
  # This case is the regression pin for choosing git pathspec.
  run gate "go.mod" "a/b/go.mod" "go.tool-lint.mod" "go.tool.sum" "Dockerfile" "web/package.json" "compose.prod.yaml"
  [ "$status" -eq 0 ]
}

@test "repo-config diff under .github is exempt" {
  run gate ".github/workflows/ci.yaml" ".github/ISSUE_TEMPLATE/bug_report.yml" ".github/renovate.json"
  [ "$status" -eq 0 ]
}

@test ".github/CODEOWNERS alone is never exempt" {
  run gate ".github/CODEOWNERS"
  [ "$status" -eq 10 ]
  [[ "$output" == *"CODEOWNERS"* ]]
}

@test ".github/CODEOWNERS hidden among exempt .github files is still not exempt" {
  # The silent trap: the exclude query ALONE returns empty for this input, so a
  # matcher that omits the second positive CODEOWNERS query returns 0 here and
  # nothing errors. This case is the only thing that catches that.
  run gate ".github/CODEOWNERS" ".github/workflows/ci.yaml"
  [ "$status" -eq 10 ]
  [[ "$output" == *"CODEOWNERS"* ]]
}

@test "root CODEOWNERS alone is never exempt" {
  run gate "CODEOWNERS"
  [ "$status" -eq 10 ]
}

# GitHub honors CODEOWNERS at exactly three locations: root, .github/, docs/.
# An earlier revision of this suite asserted the opposite for docs/ — that it
# was "an ordinary docs file" and exempt — which pinned a real hole as intended
# behavior: `docs/**` became a self-exempting route to changing review
# ownership. Inverted deliberately; this is a fix, not a regression.
@test "docs/CODEOWNERS alone is never exempt — GitHub honors it there" {
  run gate "docs/CODEOWNERS"
  [ "$status" -eq 10 ]
}

@test "docs/CODEOWNERS mixed with an otherwise-exempt docs file is not exempt" {
  run gate "docs/guide.md" "docs/CODEOWNERS"
  [ "$status" -eq 10 ]
}

# The carve-out is path-exact, not a name match: GitHub ignores a CODEOWNERS
# file anywhere else, so flagging one would be a false positive.
@test "a CODEOWNERS-named file GitHub ignores does not trip the carve-out" {
  run gate "internal/CODEOWNERS"
  [ "$status" -eq 10 ]
}

@test "CODEOWNERS-named file under an exempt tree GitHub ignores stays exempt" {
  run gate "site/src/content/docs/CODEOWNERS"
  [ "$status" -eq 0 ]
}

@test "Taskfile.yaml is never exempt" {
  run gate "Taskfile.yaml"
  [ "$status" -eq 10 ]
}

@test "scripts/foo.sh is never exempt" {
  run gate "scripts/foo.sh"
  [ "$status" -eq 10 ]
}

@test "a listed lockfile shape under scripts/ is exempt" {
  run gate "scripts/uv.lock"
  [ "$status" -eq 0 ]
}

@test "an unlisted lockfile under scripts/ is not exempt" {
  # Colloquially a lockfile, but not a listed DEPENDENCY_ONLY_PATHS shape.
  run gate "scripts/poetry.lock"
  [ "$status" -eq 10 ]
}

@test "paths containing spaces do not break parsing" {
  run gate "docs/a file with spaces.md"
  [ "$status" -eq 0 ]
}

@test "empty changed-file list is a deliberate exempt verdict" {
  run gate_empty
  [ "$status" -eq 0 ]
  [[ "$output" == *"empty changed-file list"* ]]
}

@test "a Taskfile missing REPO_CONFIG_ONLY_PATHS exits 1, not 0 and not 10" {
  # awk returns zero lines with exit 0 on a missing key. Without this guard the
  # matcher would silently treat a broken config as "no exclusions".
  write_taskfile_missing_repo_config_var
  run gate_with_root "$TMPDIR_FIX" "site/index.md"
  [ "$status" -eq 1 ]
  [[ "$output" == *"REPO_CONFIG_ONLY_PATHS"* ]]
}

@test "a mixed diff is not exempt and names the offending file" {
  run gate "site/index.md" "internal/foo.go"
  [ "$status" -eq 10 ]
  [[ "$output" == *"internal/foo.go"* ]]
}
