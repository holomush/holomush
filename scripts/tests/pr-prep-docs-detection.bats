#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

load 'pr-prep-lock-helpers'

# Tests pr-prep's docs-only detection prologue using the existing fixture
# pattern. Each test injects a `git diff` output via PATH shim, runs the
# fixture's `pr-prep`, and asserts which lane stub ran by checking marker
# file existence.
#
# REGRESSION GUARD: every docs-only case asserts the full-lane marker
# was NOT written (file does not exist). Catches the r2 structural defect.

setup_file() {
  if [ ! -f "scripts/tests/Taskfile.test.yaml" ]; then
    echo "ERROR: bats must be invoked from the repo root (try 'task test:bats')." >&2
    exit 1
  fi
}

setup() {
  bats_load_library bats-support
  bats_load_library bats-assert
  init_test_env
  # Docs-detection tests OVERRIDE the helper's force-full setting (added
  # in Step 2). Unset it so the detection prologue actually runs.
  unset HOLOMUSH_PR_PREP_FORCE_FULL

  # Lane markers — must be different paths so we can detect which ran.
  export STUB_MARKER="${BATS_TEST_TMPDIR}/full-marker"
  export STUB_DOCS_MARKER="${BATS_TEST_TMPDIR}/docs-marker"

  # PATH shim for git. Intercepts `git diff --name-only` and `git fetch`
  # by reading injected env vars; other git calls bypass to real git.
  GIT_SHIM_DIR="${BATS_TEST_TMPDIR}/bin"
  mkdir -p "$GIT_SHIM_DIR"
  cat > "$GIT_SHIM_DIR/git" <<'SH'
#!/usr/bin/env bash
if [ "$1" = "diff" ] && [ "$2" = "--name-only" ]; then
  printf '%s\n' "${BATS_GIT_DIFF_OUT:-}"
  exit "${BATS_GIT_DIFF_RC:-0}"
fi
if [ "$1" = "fetch" ]; then
  exit 0
fi
REAL_GIT="$(PATH="${ORIG_PATH}" command -v git)"
exec "$REAL_GIT" "$@"
SH
  chmod +x "$GIT_SHIM_DIR/git"
  export ORIG_PATH="$PATH"
  export PATH="$GIT_SHIM_DIR:$PATH"
}

teardown() {
  unset BATS_GIT_DIFF_OUT BATS_GIT_DIFF_RC ORIG_PATH \
        STUB_MARKER STUB_DOCS_MARKER HOLOMUSH_PR_PREP_FORCE_FULL
}

run_fixture_pr_prep() {
  run task -t "$(fixture_taskfile)" pr-prep
}

assert_docs_lane() {
  [ "$status" -eq 0 ]
  [ -f "$STUB_DOCS_MARKER" ]
  # REGRESSION GUARD: full-lane marker must NOT exist.
  [ ! -f "$STUB_MARKER" ]
}

assert_full_lane() {
  [ "$status" -eq 0 ]
  [ -f "$STUB_MARKER" ]
  # Inverse guard: docs marker must NOT exist.
  [ ! -f "$STUB_DOCS_MARKER" ]
}

@test "docs/single_md: site/src/content/docs/index.md -> docs lane only" {
  export BATS_GIT_DIFF_OUT="site/src/content/docs/index.md"
  run_fixture_pr_prep
  assert_docs_lane
}

@test "docs/root_readme: README.md -> docs lane only" {
  export BATS_GIT_DIFF_OUT="README.md"
  run_fixture_pr_prep
  assert_docs_lane
}

@test "docs/nested_claude: web/CLAUDE.md -> docs lane only" {
  export BATS_GIT_DIFF_OUT="web/CLAUDE.md"
  run_fixture_pr_prep
  assert_docs_lane
}

@test "docs/agents_md: .claude/agents/code-reviewer.md -> docs lane only" {
  export BATS_GIT_DIFF_OUT=".claude/agents/code-reviewer.md"
  run_fixture_pr_prep
  assert_docs_lane
}

@test "docs/pull_request_template: .github/PULL_REQUEST_TEMPLATE.md -> docs lane only (spec §4.1 edge case)" {
  export BATS_GIT_DIFF_OUT=".github/PULL_REQUEST_TEMPLATE.md"
  run_fixture_pr_prep
  assert_docs_lane
}

@test "full/go_source: internal/foo/bar.go -> full lane" {
  export BATS_GIT_DIFF_OUT="internal/foo/bar.go"
  run_fixture_pr_prep
  assert_full_lane
}

@test "full/mixed: site/src/content/docs/index.md + internal/foo.go -> full lane" {
  export BATS_GIT_DIFF_OUT=$'site/src/content/docs/index.md\ninternal/foo.go'
  run_fixture_pr_prep
  assert_full_lane
}

@test "full/claude_hooks: .claude/hooks/pre-commit.sh -> full lane" {
  export BATS_GIT_DIFF_OUT=".claude/hooks/pre-commit.sh"
  run_fixture_pr_prep
  assert_full_lane
}

@test "full/claude_settings: .claude/settings.json -> full lane" {
  export BATS_GIT_DIFF_OUT=".claude/settings.json"
  run_fixture_pr_prep
  assert_full_lane
}

@test "full/ci_yaml: .github/workflows/ci.yaml -> full lane" {
  export BATS_GIT_DIFF_OUT=".github/workflows/ci.yaml"
  run_fixture_pr_prep
  assert_full_lane
}

@test "full/empty_diff: empty diff -> full lane" {
  export BATS_GIT_DIFF_OUT=""
  run_fixture_pr_prep
  assert_full_lane
}

@test "full/git_diff_error: git diff exits non-zero -> full lane" {
  export BATS_GIT_DIFF_RC=1
  export BATS_GIT_DIFF_OUT=""
  run_fixture_pr_prep
  assert_full_lane
}

@test "force_full: HOLOMUSH_PR_PREP_FORCE_FULL=1 forces full lane on docs-only diff" {
  export HOLOMUSH_PR_PREP_FORCE_FULL=1
  export BATS_GIT_DIFF_OUT="site/src/content/docs/index.md"
  run_fixture_pr_prep
  assert_full_lane
}
