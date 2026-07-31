#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Decide whether a pull request's changed-file list is EXEMPT from the
# issue-first gate documented in CONTRIBUTING.md "### Exempt by file path".
#
# Input:  changed file paths on stdin, one per line (blank lines ignored).
# Output: a human-readable verdict on stdout, diagnostics on stderr.
#
# Exit codes — THREE values, deliberately, not two:
#   0  EXEMPT      every changed file matched an exempt path glob
#   10 NOT EXEMPT  at least one changed file falls outside the exempt sets
#   1  ERROR       the matcher could not run (missing/empty Taskfile var, bad input)
#
# The third value is the point: a caller must NEVER be able to read a script
# malfunction as an exemption. If this collapsed to 0/non-zero, a broken config
# would either wave every PR through or flag every PR, and both look like a
# working gate from the outside. Callers MUST branch on all three.
#
# The exempt globs are read at runtime from Taskfile.yaml's vars
# (DOCS_ONLY_PATHS, DEPENDENCY_ONLY_PATHS, REPO_CONFIG_ONLY_PATHS), which are
# the single source of truth. Matching is done by GIT's own glob engine via
# `:(glob)` pathspec — this script contains NO glob compiler. See
# .planning/quick/260731-ea8-.../260731-ea8-RESEARCH.md Q3 for why every
# hand-written or library alternative was rejected (they mis-handle `**/go.mod`
# against a root-level go.mod, or `go.tool*.mod`, SILENTLY).
#
# Sole consumer: .github/workflows/issue-gate.yaml.

set -euo pipefail

# REPO_ROOT may be overridden by tests; default to script's parent.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${REPO_ROOT:-$(cd "$SCRIPT_DIR/.." && pwd)}"
TASKFILE="$REPO_ROOT/Taskfile.yaml"

if [ ! -f "$TASKFILE" ]; then
  echo "ERROR: Taskfile.yaml not found at $TASKFILE" >&2
  exit 1
fi

# Extract a `  NAME: |` block-scalar var from Taskfile.yaml, dropping comment
# lines and trailing whitespace. yq is deliberately NOT used: it is built only
# inside scripts-tests.yaml's `bats` job (see :62-72 there) and a fresh workflow
# runner has none, so this must work with awk alone.
extract_var() {
  local key="$1"
  # A blank line inside a YAML block scalar is LEGAL and part of the block —
  # grouping globs with one is an ordinary edit. An earlier revision terminated
  # on any non-matching line, so a blank line silently truncated the pattern
  # list at that point: the globs below it vanished and files they covered
  # started reading as non-exempt. Fail-closed, but invisible, and triggered by
  # a formatting change nobody would think to re-test. Blank lines are skipped;
  # the block ends only at a line that is non-blank and not 4-space indented.
  awk -v key="$key" '
    $0 ~ "^  " key ": \\|$" { inblock=1; next }
    inblock && /^[[:space:]]*$/ { next }
    inblock && /^    #/      { next }
    inblock && /^    [^ ]/   { sub(/^    /, ""); sub(/[[:space:]]+$/, ""); print; next }
    inblock                  { exit }
  ' "$TASKFILE"
}

# --- read the changed-file list -------------------------------------------

paths=()
# `|| [ -n "$line" ]` so a final line with no trailing newline is not dropped.
while IFS= read -r line || [ -n "$line" ]; do
  [ -n "$line" ] || continue
  # Defence in depth at a trust boundary: this list is PR-author-influenced and
  # is about to be materialized as filesystem paths. GitHub cannot produce an
  # absolute path or a `..` component in a tree, so either one means something is
  # wrong — fail loud rather than write outside the scratch directory.
  # `..` is listed bare as well as in the three positional forms: an earlier
  # revision omitted it and a lone `..` reached mkdir, which refused only
  # because the filesystem happened to say no. A guard that relies on the
  # operation it guards failing is not a guard.
  case "$line" in
    /* | .. | ../* | */../* | */..)
      echo "ERROR: refusing to materialize suspicious path: $line" >&2
      exit 1
      ;;
  esac
  paths+=("$line")
done

# Empty-input branch. This verdict is DELIBERATE, not a side effect of an empty
# match set: a PR with zero changed files (or a pure merge-commit refresh) has
# nothing to gate, and letting it fall out of the matching logic would make
# "exempt" and "nothing to match" indistinguishable.
if [ "${#paths[@]}" -eq 0 ]; then
  echo "exempt: empty changed-file list"
  exit 0
fi

# --- build the pathspec arguments -----------------------------------------

# Read every var and FAIL LOUD when one is missing or empty.
#
# This guard is the whole reason the ERROR exit code exists. awk prints zero
# lines and exits 0 for a key that is not present — indistinguishable from a var
# that exists and is empty, and indistinguishable from success. Without this
# check a renamed or deleted var would degrade the matcher to "no exclusions",
# which is not an error anyone would notice.
#
# The check runs HERE, in the main shell, and not inside a helper feeding a
# process substitution: `exit 1` from `< <(helper)` terminates only the subshell
# and the script sails on with an empty pattern list. (Observed — the bats case
# "a Taskfile missing REPO_CONFIG_ONLY_PATHS exits 1" caught exactly that.)
patterns=""
for var in DOCS_ONLY_PATHS DEPENDENCY_ONLY_PATHS REPO_CONFIG_ONLY_PATHS; do
  value="$(extract_var "$var")"
  if [ -z "$value" ]; then
    echo "ERROR: vars.$var not found or empty in $TASKFILE" >&2
    exit 1
  fi
  patterns+="$value"$'\n'
done

exclude_specs=()
while IFS= read -r pattern; do
  [ -n "$pattern" ] || continue
  # Pure string prefixing — NOT glob compilation. git parses the glob.
  exclude_specs+=(":(exclude,glob)$pattern")
done <<< "$patterns"

# --- materialize the input in a scratch git index --------------------------

scratch="$(mktemp -d)"
cleanup() { rm -rf "$scratch"; }
trap cleanup EXIT

# `-b` avoids git's default-branch advice on stderr. No commit is made: git
# ls-files reads the INDEX, so `git add` alone is sufficient and the script
# needs no committer identity. A fresh init also has no .gitignore, which is
# what makes `git add -A --force` faithful to the input list.
git -C "$scratch" init -q -b gate

for p in "${paths[@]}"; do
  # `--` because $p is PR-author-influenced: a file named `-e` makes dirname
  # parse it as an option ("dirname: illegal option -- e"), and that stderr is
  # folded into the verdict the workflow posts as a public PR comment.
  mkdir -p "$scratch/$(dirname -- "$p")"
  : > "$scratch/$p"
done
git -C "$scratch" add -A --force

# Query 1 — everything NOT matched by any exempt glob. With only :(exclude)
# terms present, git implicitly matches all remaining files.
nonexempt="$(git -C "$scratch" -c core.quotePath=false ls-files -- "${exclude_specs[@]}")"

# Query 2 — the CODEOWNERS carve-out, as a SEPARATE positive query.
#
# This CANNOT be folded into query 1 and the failure mode of trying is SILENT.
# Git applies all :(exclude) terms last and ANDs them, so `.github/**` swallows
# `.github/CODEOWNERS` with no way to re-include it: a diff of
# `.github/CODEOWNERS` + `.github/workflows/ci.yaml` makes query 1 return EMPTY,
# and a matcher without this second query calls that exempt and reports success.
# GitHub honors a CODEOWNERS file in exactly THREE locations: the repository
# root, `.github/`, and `docs/`. All three must appear here. An earlier revision
# listed only the first two and asserted in this very comment that
# "`docs/CODEOWNERS` is an ordinary docs file and stays exempt" — which was
# false, and made `docs/**` a self-exempting route to changing review ownership:
# a PR adding `docs/CODEOWNERS` assigning `internal/**` to its own author passed
# the gate GREEN. That is the same shape of hole the carve-out exists to close.
#
# The bare (non-glob) pathspecs match those exact paths and nothing else, so a
# file merely NAMED CODEOWNERS elsewhere (`internal/CODEOWNERS`) is not caught
# here — correctly, since GitHub ignores it. Do not widen this to
# `**/CODEOWNERS`: that would flag files GitHub does not honor.
owners="$(git -C "$scratch" -c core.quotePath=false ls-files -- ':(glob).github/CODEOWNERS' 'CODEOWNERS' 'docs/CODEOWNERS')"

# --- verdict ---------------------------------------------------------------

if [ -z "$nonexempt" ] && [ -z "$owners" ]; then
  echo "exempt: all ${#paths[@]} changed file(s) matched an exempt pattern"
  exit 0
fi

if [ -n "$nonexempt" ]; then
  echo "not exempt: $(printf '%s\n' "$nonexempt" | wc -l | tr -d ' ') file(s) outside the exempt path sets"
  printf '%s\n' "$nonexempt"
fi

if [ -n "$owners" ]; then
  echo "not exempt: CODEOWNERS is never exempt (changing review ownership is a governance decision)"
  printf '%s\n' "$owners"
fi

exit 10
