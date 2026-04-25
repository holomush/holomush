#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Sourceable helper. Sets the following variables based on cwd:
#   IS_DEFAULT — "yes" if cwd is the main repo (default jj workspace), else "no"
#   MAIN_REPO  — absolute path to the main repo root
#   WORKTREES  — absolute path to the .worktrees parent dir
#
# Usage (from a Taskfile cmd or a hook script):
#   . "$(git rev-parse --show-toplevel 2>/dev/null || pwd)/scripts/jj-main-repo.sh"
#
# In a jj workspace, .jj/repo is a FILE containing a path (relative to .jj/)
# back to the main checkout's .jj. In the main checkout itself, .jj/repo is
# a DIRECTORY. Verbatim of the technique used by the soon-to-be-deleted
# `gowork` task (Taskfile.yaml:525-530 prior to its removal).

# shellcheck disable=SC2034  # IS_DEFAULT, MAIN_REPO, WORKTREES are consumed by callers that source this helper.
# shellcheck disable=SC2317  # `exit 1` is a deliberate fallback when `return` fails (script invoked, not sourced).
if [ -f ".jj/repo" ]; then
  IS_DEFAULT=no
  POINTER=$(cat ".jj/repo")
  MAIN_REPO=$(cd ".jj/${POINTER}/../.." && pwd -P)
elif [ -d ".jj/repo" ]; then
  IS_DEFAULT=yes
  MAIN_REPO=$(pwd -P)
else
  echo "ERROR: $(pwd) is not a jj repo or workspace (no .jj/repo)" >&2
  return 1 2>/dev/null || exit 1
fi
WORKTREES="$(dirname "$MAIN_REPO")/.worktrees"
