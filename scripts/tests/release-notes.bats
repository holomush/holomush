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
  # Hermetic: never inherit the global commit.gpgsign/gpg.format — SSH/GPG
  # signing blocks on a locked agent with no TTY in the bats subprocess.
  git config commit.gpgsign false
  git config tag.gpgsign false
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
  # Multi-level sub-bead id: the harvest regex must capture all depth levels,
  # not truncate to the first (holomush-5rh.8).
  git commit -q --allow-empty -m "feat(crypto): scene DEK genesis (holomush-5rh.8.29.13)"
  # Scoped docs(scope): — GoReleaser anchors ^docs: so this is NOT excluded.
  git commit -q --allow-empty -m "docs(scenes): settings actions plan"
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

@test "collect harvests multi-level bead ids without truncation" {
  run "$REPO_ROOT/scripts/release-notes-collect.sh" v0.2.0
  [ "$status" -eq 0 ]
  # The harvested ref under "## Referenced beads" MUST be the full 3-deep id,
  # not truncated to holomush-5rh.8. The leading "- " (no preceding paren)
  # distinguishes the harvested ref line from the filtered-commit subject line.
  [[ "$output" == *"- holomush-5rh.8.29.13"* ]]
}

@test "collect keeps scoped docs(scope): commits (GoReleaser anchors ^docs: only)" {
  run "$REPO_ROOT/scripts/release-notes-collect.sh" v0.2.0
  [ "$status" -eq 0 ]
  # ^docs: is anchored — 'docs(scenes):' is NOT excluded, mirroring GoReleaser.
  [[ "$output" == *"docs(scenes): settings actions plan"* ]]
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

@test "publish refuses an empty narrative file" {
  echo -n "" > "$BATS_TEST_TMPDIR/narr.md"
  run "$REPO_ROOT/scripts/release-notes-publish.sh" --tag v0.2.0 --narrative-file "$BATS_TEST_TMPDIR/narr.md"
  [ "$status" -ne 0 ]
  [[ "$output" == *"narrative file is empty"* ]]
}

@test "publish combines narrative with the existing GoReleaser body" {
  # Mock gh: 'release view' prints a fixture body; 'release edit' records its
  # --notes-file argument's contents to a sentinel file.
  BIN="$BATS_TEST_TMPDIR/bin"; mkdir -p "$BIN"
  cat > "$BIN/gh" <<'EOF'
#!/usr/bin/env bash
if [ "$1 $2" = "release view" ]; then printf '## Changelog\n- feat: existing (#1)\n'; exit 0; fi
if [ "$1 $2" = "release edit" ]; then
  while [ $# -gt 0 ]; do [ "$1" = "--notes-file" ] && cp "$2" "$GH_SENTINEL"; shift; done
  exit 0
fi
exit 0
EOF
  chmod +x "$BIN/gh"
  printf '## What changed\nNarrative TLDR here.\n' > "$BATS_TEST_TMPDIR/narr.md"
  GH_SENTINEL="$BATS_TEST_TMPDIR/published.md" PATH="$BIN:$PATH" \
    run "$REPO_ROOT/scripts/release-notes-publish.sh" --tag v0.2.0 --narrative-file "$BATS_TEST_TMPDIR/narr.md"
  [ "$status" -eq 0 ]
  # Combined body MUST contain BOTH the narrative AND the existing mechanical list.
  grep -q "Narrative TLDR here." "$BATS_TEST_TMPDIR/published.md"
  grep -q "feat: existing (#1)" "$BATS_TEST_TMPDIR/published.md"
  # INV-7: the narrative MUST appear ABOVE the GoReleaser list, not merely be
  # present. A reversed assembly order would otherwise pass the presence checks.
  narr_line=$(grep -n "Narrative TLDR here." "$BATS_TEST_TMPDIR/published.md" | head -1 | cut -d: -f1)
  gor_line=$(grep -n "feat: existing (#1)" "$BATS_TEST_TMPDIR/published.md" | head -1 | cut -d: -f1)
  [ "$narr_line" -lt "$gor_line" ]
}

@test "publish fails closed when the existing release body is empty" {
  # Mock gh: 'release view' returns an EMPTY body; 'release edit' would record a
  # publish. Fail-closed means we MUST exit non-zero and never reach release edit.
  BIN="$BATS_TEST_TMPDIR/bin"; mkdir -p "$BIN"
  cat > "$BIN/gh" <<'EOF'
#!/usr/bin/env bash
if [ "$1 $2" = "release view" ]; then printf ''; exit 0; fi
if [ "$1 $2" = "release edit" ]; then touch "$GH_SENTINEL"; exit 0; fi
exit 0
EOF
  chmod +x "$BIN/gh"
  printf '## What changed\nNarrative TLDR here.\n' > "$BATS_TEST_TMPDIR/narr.md"
  GH_SENTINEL="$BATS_TEST_TMPDIR/published.md" PATH="$BIN:$PATH" \
    run "$REPO_ROOT/scripts/release-notes-publish.sh" --tag v0.2.0 --narrative-file "$BATS_TEST_TMPDIR/narr.md"
  [ "$status" -ne 0 ]
  [[ "$output" == *"existing release body for v0.2.0 is empty"* ]]
  # release edit MUST NOT have run — no narrative-only publish.
  [ ! -e "$BATS_TEST_TMPDIR/published.md" ]
}
