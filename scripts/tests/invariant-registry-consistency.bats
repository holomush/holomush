#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

# Regression tests for scripts/check-invariant-registry-consistency.sh.
# Primary guard: the markdown-ID extraction must capture each row's FIRST-column
# (primary) invariant ID, not a trailing legacy ID also wrapped in backticks
# (holomush-hz0v4.14.19). A greedy `^.*` sed prefix used to bind the capture to
# the last backtick token, producing spurious drift once rows reference legacy
# IDs.

setup() {
  REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  SCRIPT="$REPO_ROOT/scripts/check-invariant-registry-consistency.sh"
  TMP="$BATS_TEST_TMPDIR"
  export INV_REGISTRY_YAML="$TMP/invariants.yaml"
  export INV_REGISTRY_MD="$TMP/invariants.md"
}

run_check() {
  run bash "$SCRIPT"
}

@test "primary first-column ID is captured even when a row also cites a legacy ID in backticks" {
  cat >"$INV_REGISTRY_YAML" <<'YAML'
invariants:
  - id: INV-CRYPTO-1
    scope: INV-CRYPTO
YAML
  cat >"$INV_REGISTRY_MD" <<'MD'
| ID | Notes |
| --- | --- |
| `INV-CRYPTO-1` | replaces `INV-RB-5` |
MD
  run_check
  [ "$status" -eq 0 ]
  [[ "$output" == *"consistent"* ]]
}

@test "drift is reported when the YAML ID is absent from the markdown table" {
  cat >"$INV_REGISTRY_YAML" <<'YAML'
invariants:
  - id: INV-CRYPTO-1
    scope: INV-CRYPTO
YAML
  cat >"$INV_REGISTRY_MD" <<'MD'
| ID | Notes |
| --- | --- |
| `INV-PRESENCE-1` | unrelated |
MD
  run_check
  [ "$status" -ne 0 ]
  [[ "$output" == *"YAML has INV-CRYPTO-1 but markdown table does not"* ]]
}

@test "drift is reported when the markdown table has an ID absent from YAML" {
  cat >"$INV_REGISTRY_YAML" <<'YAML'
invariants:
  - id: INV-CRYPTO-1
    scope: INV-CRYPTO
YAML
  cat >"$INV_REGISTRY_MD" <<'MD'
| ID | Notes |
| --- | --- |
| `INV-CRYPTO-1` | ok |
| `INV-CRYPTO-2` | replaces `INV-RB-6` |
MD
  run_check
  [ "$status" -ne 0 ]
  [[ "$output" == *"markdown table has INV-CRYPTO-2 but YAML does not"* ]]
}

@test "empty YAML registry skips the check (scaffolding tolerance)" {
  printf 'invariants: []\n' >"$INV_REGISTRY_YAML"
  printf '(no invariants yet)\n' >"$INV_REGISTRY_MD"
  run_check
  [ "$status" -eq 0 ]
  [[ "$output" == *"skipped"* ]]
}
