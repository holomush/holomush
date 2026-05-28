#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

# INV-2: the EBNF gate MUST catch drift between the DSL parser and the
# generated grammar/railroad artifacts. Regenerates for real (needs network
# for the railroad tool), so this is a slow case by design.

setup_file() {
  if [ ! -f "scripts/tests/Taskfile.test.yaml" ]; then
    echo "ERROR: bats must be invoked from the repo root (try 'task test:bats')." >&2
    exit 1
  fi
}

setup() {
  bats_load_library bats-support
  bats_load_library bats-assert
  EBNF=site/public/reference/policy-dsl.ebnf
  cp "$EBNF" "${BATS_TEST_TMPDIR}/orig.ebnf"
}

teardown() {
  # generate:ebnf:check regenerates the artifact, so the tree is left clean;
  # restore is belt-and-suspenders in case the generator was skipped.
  cp "${BATS_TEST_TMPDIR}/orig.ebnf" "$EBNF"
}

@test "generate:ebnf:check passes when artifacts are current" {
  run task generate:ebnf:check
  assert_success
}

@test "generate:ebnf:check fails when the EBNF artifact has drifted" {
  printf '\nDRIFT-MARKER\n' >> "$EBNF"
  run task generate:ebnf:check
  assert_failure
  assert_output --partial "out of sync"
}
