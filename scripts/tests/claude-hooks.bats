#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Executes the colocated .claude/hooks/*.test.sh harnesses (which previously
# had no wired runner) as part of `task test:bats` — which pr-prep runs.

setup() {
  REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
}

@test "enforce-task-runner hook harness passes" {
  run "$REPO_ROOT/.claude/hooks/enforce-task-runner.test.sh"
  [ "$status" -eq 0 ]
}

@test "nudge-adr-capture hook harness passes" {
  run "$REPO_ROOT/.claude/hooks/nudge-adr-capture.test.sh"
  [ "$status" -eq 0 ]
}
