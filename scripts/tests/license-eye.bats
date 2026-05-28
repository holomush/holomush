#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

setup_file() {
  if [ ! -f "scripts/tests/Taskfile.test.yaml" ]; then
    echo "ERROR: bats must be invoked from the repo root (try 'task test:bats')." >&2
    exit 1
  fi
}

setup() {
  bats_load_library bats-support
  bats_load_library bats-assert
}

# INV-6: license-eye MUST NOT stamp user-facing rendered content. license-eye's
# markdown header is the multi-line AngleBracket form, whose distinctive license
# line is `  ~ SPDX-License-Identifier: ...`. We anchor on `~ SPDX-License-Identifier`
# (the tilde-prefixed form the tool emits) so the guard fires if license-eye ever
# stamps a protected tree, while NOT matching docs that merely *mention* SPDX in a
# code example (those use `// SPDX` / `-- SPDX` / `# SPDX`, never `~ SPDX`).

@test "plugin content markdown stays header-free (INV-6)" {
  run rg -l '~ SPDX-License-Identifier' plugins --glob 'plugins/**/content/**/*.md'
  assert_failure 1 # rg exits 1 (no match); exit 2+ (rg error) must NOT false-green the invariant
}

@test "player-facing site docs stay header-free (INV-6)" {
  # guide/operating/reference are the player- and operator-facing rendered trees.
  # All of site/src/content/docs/** is license-eye paths-ignore'd, so the tool stamps none of
  # it; a few hand-authored headers exist on extending/ developer technical docs
  # (single-line `<!-- SPDX ... -->` form, not license-eye's), which is why this
  # guard targets the player/operator subtrees and license-eye's tilde form.
  run rg -l '~ SPDX-License-Identifier' site/src/content/docs/guide site/src/content/docs/operating site/src/content/docs/reference --glob '*.md'
  assert_failure 1 # rg exits 1 (no match); exit 2+ (rg error) must NOT false-green the invariant
}

# INV-4: after `task fmt`, a freshly-added unheadered in-scope file passes check.
@test "task fmt adds license headers so license:check passes" {
  local gof="internal/zzz_invtest_$$.go"
  local mdf="docs/zzz_invtest_$$.md"
  printf 'package internal\n\nfunc invtest() {}\n' >"$gof"
  printf '# inv test\n\nbody\n' >"$mdf"

  run task fmt
  assert_success

  run grep -q 'SPDX-License-Identifier' "$gof"
  assert_success
  run grep -q 'SPDX-License-Identifier' "$mdf"
  assert_success

  rm -f "$gof" "$mdf"
}
