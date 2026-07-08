#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Sourceable helper. Sets the following variables based on cwd:
#   IS_DEFAULT — "yes" if cwd is the primary worktree (main checkout), else "no"
#   MAIN_REPO  — absolute path to the main repo root (the primary worktree)
#   WORKTREES  — absolute path to the .worktrees parent dir
#
# Usage (from a Taskfile cmd or a hook script):
#   . "$(git rev-parse --show-toplevel 2>/dev/null || pwd)/scripts/git-main-repo.sh"
#
# Detection: git's SHARED repository dir (`--git-common-dir`) lives inside the
# PRIMARY worktree as <main-repo>/.git. A LINKED worktree's per-worktree git dir
# (`--git-dir`) is <main-repo>/.git/worktrees/<name>, distinct from the common
# dir; in the primary worktree the two are identical. Comparing them yields
# IS_DEFAULT, and the common dir's parent is the main repo root. Both are
# absolute (`--path-format=absolute`, git >=2.31) and cwd-stable, so this works
# from any subdirectory of any worktree.

# shellcheck disable=SC2034  # IS_DEFAULT, MAIN_REPO, WORKTREES are consumed by callers that source this helper.
# shellcheck disable=SC2317  # `exit 1` is a deliberate fallback when `return` fails (script invoked, not sourced).
COMMON_DIR=$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null || true)
GIT_DIR=$(git rev-parse --path-format=absolute --git-dir 2>/dev/null || true)
if [ -z "$COMMON_DIR" ] || [ -z "$GIT_DIR" ]; then
  echo "ERROR: $(pwd) is not a git repository (git rev-parse --git-common-dir failed)" >&2
  return 1 2>/dev/null || exit 1
fi
if [ "$GIT_DIR" = "$COMMON_DIR" ]; then
  IS_DEFAULT=yes
else
  IS_DEFAULT=no
fi
# The common dir is <main-repo>/.git; its parent is the main repo root.
MAIN_REPO=$(cd "$(dirname "$COMMON_DIR")" && pwd -P)
WORKTREES="$(dirname "$MAIN_REPO")/.worktrees"
