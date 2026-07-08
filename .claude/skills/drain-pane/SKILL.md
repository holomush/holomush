---
name: drain-pane
description: Use when you want to run a /drain autonomously in a detached cmux worker pane instead of firing /goal in your own session — launches the worker, fires the bead-driven /goal condition, and arms a stall-watchdog so the drain self-heals while you walk away.
disable-model-invocation: true
---

# Drain Pane

Launch a `/drain` worker in a **detached cmux pane** and arm a **stall-watchdog**, so an
autonomous bead-drain runs without occupying — or stalling — your orchestrating session.

## Core principle

`/drain` already does the hard part: Phase A–C mint the typed drain bead (`bd create
--type drain`, status `in_progress`, metadata `drain_workspace`/`drain_scope`/`drain_sentinel`)
and Phase D **emits** a thin (~370 char) bead-driven `/goal` condition. This skill is **only**
the two things `/drain` can't do itself: (1) the cmux pane mechanics + launch sequence, and
(2) the watchdog. There is **no handoff file** — the worker reads its entire assignment from
`bd show <drain-id> --json` and gets the protocol from the `dev-flow:draining-beads` skill.

## Usage

```text
/drain-pane <drain-id>
```

`<drain-id>` is an existing **live** drain bead (type=drain, status=in_progress) that you
already minted with `/drain epic <id>`. If you haven't, run that first and pass the bead id
it reports.

**v1 is epic-mode only.** The watchdog's child-probe is epic-specific (it filters
`startswith("<epic>.")` — see Prerequisites); `set`/`cascade` drains are **rejected
fail-fast** until their probe is specified. The `/drain` command supports set/cascade; this
pane-launcher does not yet.

## Prerequisites (refuse early)

```bash
DRAIN_ID="$1"
B=$(bd show "$DRAIN_ID" --json 2>/dev/null)
# bd show returns an array in current bd; the `// .x` fallbacks also accept an object payload.
[ "$(jq -r '.[0].type // .type // empty' <<<"$B")" = "drain" ]   || { echo "Not a drain bead: $DRAIN_ID" >&2; exit 1; }
[ "$(jq -r '.[0].status // .status // empty' <<<"$B")" = "in_progress" ] || { echo "Drain $DRAIN_ID not in_progress (already closed?)" >&2; exit 1; }
# v1 = epic-mode only: the watchdog child-probe below is epic-specific. Fail fast otherwise.
MODE=$(jq -r '.[0].metadata.drain_mode // .metadata.drain_mode // empty' <<<"$B")
[ "$MODE" = "epic" ] || { echo "drain-pane v1 supports epic-mode drains only (got mode='$MODE'); set/cascade need a different watchdog child-probe — not yet specified." >&2; exit 1; }
WORKSPACE=$(jq -r '.[0].metadata.drain_workspace // .metadata.drain_workspace // empty' <<<"$B")
SCOPE=$(jq -r '.[0].metadata.drain_scope // .metadata.drain_scope // empty' <<<"$B")
SENTINEL=$(jq -r '.[0].metadata.drain_sentinel // .metadata.drain_sentinel // empty' <<<"$B")
[ -n "$WORKSPACE" ] && [ -n "$SENTINEL" ] || { echo "Drain bead missing drain_workspace/drain_sentinel metadata" >&2; exit 1; }
```

## Launch sequence (drive cmux from your own pane)

Each step is **verified before the next** — the cwd, direnv, and submit steps each failed
live when chained or assumed (see Gotchas). Capture the new surface ref from step 1 and reuse it.

1. **New pane** (don't steal focus): `cmux new-pane --type terminal --direction right --focus false` → capture `surface:<N>`.
2. **cd as its OWN verified step** — never chain `cd X && claude`:
   `cmux send --surface <s> "cd $WORKSPACE"` → `cmux send-key --surface <s> Enter` → `cmux send --surface <s> "pwd"` + Enter → `cmux read-screen --surface <s>` and **confirm pwd == `$WORKSPACE`**. [Gotcha #1]
3. **`direnv allow`** — a fresh split hits a *blocked* `.envrc`: `cmux send --surface <s> "direnv allow"` + Enter → `read-screen` and confirm `direnv: loading` (not `…is blocked`). [Gotcha #5]
4. **Launch with bypass** — `cmux send --surface <s> "claude --dangerously-skip-permissions"` + Enter; wait ~6s. [Gotcha #2]
5. **Trust-folder prompt** — option 1 ("Yes, I trust this folder") is pre-highlighted: `cmux send-key --surface <s> Enter`.
6. **Fire the thin `/goal`** — substitute `<DRAIN_ID>`/`<SENTINEL>` into the Worker condition below, `cmux send` it, **sleep 3** (it's ~370 chars — long sends race the submit), then `cmux send-key --surface <s> Enter`. `read-screen` and confirm `Goal set:` + `/goal active`. [Gotcha #3, nudge-race]

### Worker condition (the `/goal` payload — submit verbatim)

```text
Drain worker for bead <DRAIN_ID>. Invoke the dev-flow:draining-beads skill for the iteration
protocol, then run `bd show <DRAIN_ID> --json` for your assignment (workspace, mode, scope,
lessons, rejection counts). cd to the workspace named in that bead before any bd/git/file
operation. Before pushing, `git fetch origin && git rebase origin/main` (native git; no VCS skill
needed). Execute exactly ONE ready bead this turn following the protocol, then stop. Goal met when: <SENTINEL>.
```

The rebase clause is load-bearing: a long drain drifts from `main`, so the finish-branch
pre-push `git rebase origin/main` will hit conflicts to resolve — the worker must rebase onto
`origin/main` before pushing, never force-push over the divergence. [Gotcha #7]

## Watchdog (arm after the worker is iterating)

Run as a **background** bash loop in your orchestrating session (`run_in_background: true`).
It nudges the worker when the `/goal` Stop hook drops a re-fire (recurs after long iterations),
and self-completes when the drain bead closes.

```bash
DRAIN_ID="<drain-id>"; SCOPE="<epic-scope>"; SURFACE="<surface-ref>"
NUDGE="Continue the drain: run the next ready iteration now per your active /goal."
prev=-1; strikes=0
while true; do
  st=$(bd show "$DRAIN_ID" --json 2>/dev/null | jq -r '.[0].status // .status // "unknown"')   # array/object-tolerant (matches prerequisites)
  if [ "$st" = "closed" ]; then echo "DRAIN COMPLETE: $DRAIN_ID closed"; break; fi   # completion = DRAIN BEAD CLOSED, never a count [Gotcha: count bug]
  # task-children only — EXCLUDE the drain bead itself (it is an epic child and is in_progress for the whole run) [Gotcha #6]
  inprog=$(bd list --parent "$SCOPE" --status in_progress --json 2>/dev/null | jq 'if type=="array" then ([.[]|select(.id|startswith("'"$SCOPE"'."))]|length) else -1 end')
  closed=$(bd list --parent "$SCOPE" --status closed --json 2>/dev/null | jq 'if type=="array" then ([.[]|select(.id|startswith("'"$SCOPE"'."))]|length) else -1 end')
  [ "$inprog" -lt 0 ] || [ "$closed" -lt 0 ] && { sleep 180; continue; }             # dolt 500 — never nudge on an unreadable poll
  if [ "$inprog" -eq 0 ] && [ "$closed" -eq "$prev" ]; then
    strikes=$((strikes+1))
    if [ "$strikes" -ge 2 ]; then                                                     # ~6min debounce: skip the normal close→claim gap
      cmux send --surface "$SURFACE" "$NUDGE"; sleep 2; cmux send-key --surface "$SURFACE" Enter   # SHORT nudge + 2s gap so it SUBMITS [nudge-race]
      strikes=0
    fi
  else strikes=0; fi
  prev=$closed; sleep 180
done
```

On `DRAIN COMPLETE`: send a **PushNotification** ("drain complete — landing needs you") and
keep a lightweight idle-poll through the interactive `finishing-a-development-branch` landing.
The worker closes the drain bead **before** that landing, so the landing runs unmonitored and
can stall on a rate-limit or the merge/PR menu. [Gotcha: landing scope gap]

For `set`/`cascade` drains the `startswith("$SCOPE.")` child filter doesn't apply — adapt the
in_progress/closed probes to the explicit id set; v1 is validated for `epic` mode.

## Gotchas (each cost a live mistake)

| # | Trap | Guard |
|---|------|-------|
| 1 | cmux split does NOT inherit cwd; `cd X && claude` chains drop the `cd` | `cd` as its own send, verify `pwd` via read-screen |
| 2 | Worker stalls on the first permission prompt | launch with `--dangerously-skip-permissions` |
| 3 | `/goal` rejects conditions >4000 chars | use the thin bead-driven condition (no handoff file) |
| 5 | Fresh split hits a blocked `.envrc` → no workspace env | `direnv allow` + verify `direnv: loading` before launch |
| 6 | Drain bead is itself an in_progress epic child → stall signature unreachable | filter `startswith("$SCOPE.")` to count task-children only |
| 7 | Long drain drifts from main; pre-push `git rebase origin/main` hits conflicts | worker condition tells it to rebase onto `origin/main` before pushing |
| — | Long multi-line nudge races the TUI submit (types but doesn't send) | SHORT single-line nudge + 2s before Enter; `Escape` clears a stuck box (`C-u` is not a valid key) |
| — | Watchdog completion via closed-count is wrong (review-finding beads inflate it) | completion = **drain bead status==closed**, never a count |
| — | Worker closes the drain bead BEFORE the interactive landing | PushNotification + idle-poll through landing |

## Red flags — STOP

- About to send `cd <ws> && claude` as one line → split it, verify pwd.
- Watchdog completing on `closed >= N` → wrong; key on drain-bead-closed.
- Watchdog counting `--parent <epic> --status in_progress` without the `startswith` filter → always ≥1, never detects a stall.
- Writing a handoff file → unnecessary; the condition is bead-driven.
- Exiting the watchdog at drain-close with no notification → the landing is the human moment.
