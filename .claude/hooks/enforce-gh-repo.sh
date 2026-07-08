#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# PreToolUse hook: nudge `gh` invocations toward an explicit repo (-R /
# GH_REPO=) when gh's git-based repo discovery might not resolve the remote.
#
# Under native git this is a rare edge case: a linked worktree under
# `<repo-parent>/.worktrees/<name>/` carries a `.git` FILE that links back to
# the main checkout, so `gh` normally auto-detects the remote. This hook
# stays a thin defensive net for the unusual case where a worktree's `.git`
# link is broken or absent — there `gh` follows git's repo-discovery algorithm
# from cwd, walks up to the filesystem boundary at `/Volumes`, and bails with
# the cryptic "fatal: not a git repository ... Stopping at filesystem boundary"
# error. The hook intercepts that at the Bash boundary so the assistant gets an
# actionable message pointing at the two valid fixes (-R / GH_REPO=) instead of
# having to debug git's discovery rules.
#
# Companion to enforce-bd-beads-dir.sh (same shape, different tool). Unlike
# bd, many `gh` subcommands are repo-agnostic (auth, config, version,
# extension, gist, …) and would be false positives if blocked. We maintain
# a small allowlist of those subcommands; everything else is treated as
# repo-aware and gated.
#
# Error strategy: same as enforce-task-runner.sh / enforce-bd-beads-dir.sh
# — fail open on parse errors, block reliably when we know there's a
# problem.

set -uo pipefail

# --- Parse phase: fail open on malformed input ---
trap 'exit 0' ERR

INPUT=$(cat)
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty' 2>/dev/null) || {
  echo "enforce-gh-repo: failed to parse input — enforcement disabled for this command" >&2
  exit 0
}

[[ -z "$COMMAND" ]] && exit 0

# --- Enforcement phase ---
trap - ERR

strip_leading_ws() {
  local s="$1"
  echo "${s#"${s%%[![:space:]]*}"}"
}

# Whitespace-aware first-token extraction. Unlike ${seg%% *}, which only
# splits on space and lets tab-separated commands like `time\tgh pr list`
# slip through as the single word "time\tgh", `read` honours IFS
# (default: space + tab + newline) and gives us the true first token.
first_token() {
  local _w _rest
  read -r _w _rest <<< "$1"
  echo "$_w"
}

# Returns 0 if the WHOLE segment is a standalone GH_REPO assignment
# (with optional `export`), no command after it. Used to track
# `export GH_REPO=foo/bar` followed by `gh pr list` in a later segment.
is_standalone_gh_repo_assignment() {
  local s
  s=$(strip_leading_ws "$1")
  if [[ "$s" =~ ^export[[:space:]]+ ]]; then
    s="${s#"${BASH_REMATCH[0]}"}"
    s=$(strip_leading_ws "$s")
  fi
  if [[ "$s" =~ ^GH_REPO=(\"[^\"]*\"|\'[^\']*\'|[^[:space:]]*)[[:space:]]*$ ]]; then
    return 0
  fi
  return 1
}

# Returns 0 if segment's env-var prefix sets GH_REPO, else 1.
has_gh_repo_env() {
  local s
  s=$(strip_leading_ws "$1")
  while [[ "$s" =~ ^([A-Za-z_][A-Za-z_0-9]*)=(\"[^\"]*\"|\'[^\']*\'|[^[:space:]]*)[[:space:]] ]]; do
    if [[ "${BASH_REMATCH[1]}" == "GH_REPO" ]]; then
      return 0
    fi
    s="${s#"${BASH_REMATCH[0]}"}"
    s=$(strip_leading_ws "$s")
  done
  return 1
}

strip_env_vars() {
  local s="$1"
  while [[ "$s" =~ ^[A-Za-z_][A-Za-z_0-9]*=(\"[^\"]*\"|\'[^\']*\'|[^[:space:]]*)[[:space:]] ]]; do
    s="${s#"${BASH_REMATCH[0]}"}"
    s=$(strip_leading_ws "$s")
  done
  echo "$s"
}

first_cmd_word() {
  local segment="$1"
  segment=$(strip_leading_ws "$segment")
  segment=$(strip_env_vars "$segment")
  local word
  word=$(first_token "$segment")
  while [[ "$word" =~ ^(env|command|exec|sudo|nice|nohup|time|builtin)$ ]]; do
    segment="${segment#"$word"}"
    segment=$(strip_leading_ws "$segment")
    local peek
    peek=$(first_token "$segment")
    while [[ "$peek" == -* ]]; do
      segment="${segment#"$peek"}"
      segment=$(strip_leading_ws "$segment")
      peek=$(first_token "$segment")
    done
    segment=$(strip_env_vars "$segment")
    word=$(first_token "$segment")
  done
  echo "$word"
}

# Returns the gh subcommand (first non-flag arg after `gh`) by skipping
# any leading global flags like `--help`, `--version`. Echoes empty if
# none found (e.g., bare `gh`).
gh_subcommand() {
  local segment="$1"
  segment=$(strip_leading_ws "$segment")
  segment=$(strip_env_vars "$segment")
  # Strip wrappers and their flags (env, sudo, etc.)
  local word
  word=$(first_token "$segment")
  while [[ "$word" =~ ^(env|command|exec|sudo|nice|nohup|time|builtin)$ ]]; do
    segment="${segment#"$word"}"
    segment=$(strip_leading_ws "$segment")
    local peek
    peek=$(first_token "$segment")
    while [[ "$peek" == -* ]]; do
      segment="${segment#"$peek"}"
      segment=$(strip_leading_ws "$segment")
      peek=$(first_token "$segment")
    done
    segment=$(strip_env_vars "$segment")
    word=$(first_token "$segment")
  done
  # word is now `gh`. Drop it.
  segment="${segment#"$word"}"
  segment=$(strip_leading_ws "$segment")
  # Walk forward, skipping flags, until we hit a non-flag arg.
  while [[ -n "$segment" ]]; do
    word=$(first_token "$segment")
    case "$word" in
      --) # End-of-flags marker; whatever follows is positional.
        segment="${segment#"$word"}"
        segment=$(strip_leading_ws "$segment")
        first_token "$segment"
        return
        ;;
      -*) # A flag; skip it. (We don't try to consume its value here —
          # known limitation: `gh -X POST api ...` would surface `POST`
          # as the subcommand. False positive, but harmless: POST is
          # not in the allowlist so it'd be gated, then -R/GH_REPO
          # bypass kicks in for the user's actual gh api call.)
        segment="${segment#"$word"}"
        segment=$(strip_leading_ws "$segment")
        ;;
      *)
        echo "$word"
        return
        ;;
    esac
  done
  echo ""
}

# Returns 0 if the segment contains `-R <val>`, `--repo <val>`, or
# `--repo=<val>` anywhere after `gh`. Permissive — we don't validate
# that <val> looks like owner/repo; gh will reject malformed values.
has_repo_flag() {
  local segment="$1"
  # Tokenise on whitespace; check for known flag forms.
  local in_gh=0
  for tok in $segment; do
    if (( in_gh == 0 )); then
      [[ "$tok" == "gh" ]] && in_gh=1
      continue
    fi
    case "$tok" in
      -R|--repo) return 0 ;;
      --repo=*) return 0 ;;
    esac
  done
  return 1
}

# Resolve worktree context. Same pattern as enforce-bd-beads-dir.sh.
WS_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [ -z "$WS_ROOT" ] || [ ! -e "$WS_ROOT/scripts/git-main-repo.sh" ]; then
  exit 0
fi

# shellcheck source=../../scripts/git-main-repo.sh
( cd "$WS_ROOT" && . "$WS_ROOT/scripts/git-main-repo.sh" >/dev/null 2>&1 ) || exit 0
cd "$WS_ROOT" || exit 0
# shellcheck source=../../scripts/git-main-repo.sh
. "$WS_ROOT/scripts/git-main-repo.sh"

# In the main repo? cwd has .git/, gh discovery succeeds. Allow.
if [ "${IS_DEFAULT:-no}" = "yes" ]; then
  exit 0
fi

# A linked worktree carries a `.git` file (or dir) → gh discovery succeeds.
# This is the NORMAL case under native git, so the hook almost always allows
# here; it only proceeds to nudge when the `.git` link is missing/broken.
if [ -d "$WS_ROOT/.git" ] || [ -f "$WS_ROOT/.git" ]; then
  exit 0
fi

# Repo-agnostic gh subcommands. These don't need a repo and gh won't try
# to discover one for them. Keeping the list short and conservative —
# false negatives (treating an agnostic command as repo-aware) are
# recoverable via -R/GH_REPO; false positives in the OTHER direction
# (letting a repo-aware command through that then fails with the cryptic
# git error) are exactly what we're trying to prevent.
is_repo_agnostic_subcmd() {
  # `browse` and `secret` are intentionally NOT here: both fall back to
  # git repo discovery when no -R/--org/--user is given, which is exactly
  # the failure mode this hook is preventing. The has_repo_flag check
  # earlier in the loop already lets `gh browse -R …` and `gh secret list
  # -R …` through.
  case "$1" in
    auth|config|version|completion|extension|gist|help|status|alias|"")
      return 0 ;;
    ssh-key|codespace|search|attestation)
      # These take their own scoping (--org, --user, query strings) and
      # don't fall back to repo discovery when no -R is given.
      return 0 ;;
    *)
      return 1 ;;
  esac
}

# Resolve the project's <owner>/<repo> for the suggestion text.
# Best-effort: parse the main checkout's origin URL. If anything fails
# (unusual remote, no git, etc.), fall back to "<owner>/<repo>" as a
# placeholder so the help text still reads.
GH_REPO_HINT="<owner>/<repo>"
if origin_url=$(git -C "$MAIN_REPO" remote get-url origin 2>/dev/null); then
  # Match git@host:OWNER/REPO, https://host/OWNER/REPO, or ssh://user@host/OWNER/REPO,
  # with optional trailing .git and optional trailing slash.
  parsed=$(printf '%s\n' "$origin_url" \
    | sed -nE 's#(git@[^:]+:|ssh://[^/]+/|https?://[^/]+/)([^/]+/[^/]+)#\2#p' \
    | sed -E 's#/+$##; s/\.git$//')
  [ -n "$parsed" ] && GH_REPO_HINT="$parsed"
fi

# Strip single- and double-quoted string contents (including across newlines)
# before segment-splitting. This prevents commands like
#   git commit -m 'multi-line\nmessage starting with "gh ..."'
# from false-triggering on lines whose first non-quote token happens to be
# `gh`. Crude — does not handle escaped quotes inside quotes — but covers
# real-world hook inputs. If `gh` appears OUTSIDE quotes anywhere, the
# stripped form preserves it and we detect normally.
STRIPPED=$(printf '%s' "$COMMAND" | perl -0777 -pe "s/'[^']*'//g; s/\"[^\"]*\"//g" 2>/dev/null) || STRIPPED="$COMMAND"
SEGMENTS=$(printf '%s' "$STRIPPED" | awk '{gsub(/ *&& */, "\n"); gsub(/ *; */, "\n"); gsub(/ *\|\| */, "\n"); print}')

# Track whether an earlier segment exported GH_REPO so chained commands like
# `export GH_REPO=foo/bar; gh pr list` aren't false-positive blocked.
gh_repo_seen=0

while IFS= read -r segment; do
  [[ -z "$segment" ]] && continue

  # If this segment is a standalone GH_REPO assignment, remember it for
  # subsequent segments and move on.
  if is_standalone_gh_repo_assignment "$segment"; then
    gh_repo_seen=1
    continue
  fi

  # Inspect every component of the pipeline, not just the leftmost.
  # `jq ... | gh ...` would otherwise silently bypass the gate.
  PIPE_PARTS=$(printf '%s\n' "$segment" | awk '{gsub(/ *\| */, "\n"); print}')
  triggered=0
  triggered_part=""
  while IFS= read -r part; do
    [[ -z "$part" ]] && continue
    word=$(first_cmd_word "$part")
    [ "$word" = "gh" ] || continue

    # User opted in via env prefix on this command, or via a prior
    # `export GH_REPO=...` segment.
    if has_gh_repo_env "$part" || [[ $gh_repo_seen -eq 1 ]]; then
      continue
    fi

    # User opted in via -R/--repo flag.
    if has_repo_flag "$part"; then
      continue
    fi

    # Subcommand is repo-agnostic — gh handles it without a repo.
    subcmd=$(gh_subcommand "$part")
    if is_repo_agnostic_subcmd "$subcmd"; then
      continue
    fi

    triggered=1
    triggered_part="$part"
    break
  done <<< "$PIPE_PARTS"

  [[ $triggered -eq 0 ]] && continue

  # Note: $triggered_part is derived from STRIPPED (quote-stripped form),
  # so we don't interpolate it — quoted args (e.g., `gh pr create -t "Fix x"`)
  # would be lost. The user has the original command in their shell history;
  # we just tell them how to make it work.
  cat >&2 <<EOF
\`gh\` invoked from a worktree ($WS_ROOT) whose .git link is missing or
broken, so gh can't discover the repo and will fail with:
"fatal: not a git repository ... Stopping at filesystem boundary".

Pick one:

  • Set GH_REPO and re-run your gh command, e.g.:
        GH_REPO='$GH_REPO_HINT' gh pr view 123

  • Or pass -R/--repo to gh (e.g., -R '$GH_REPO_HINT').

  • Or set GH_REPO for the whole session (lowest friction for repeated
    gh use):
        export GH_REPO='$GH_REPO_HINT'

Tracked: holomush-kwwu.
EOF
  exit 2
done <<< "$SEGMENTS"

exit 0
