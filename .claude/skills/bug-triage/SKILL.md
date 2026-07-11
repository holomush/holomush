---
name: bug-triage
description: >-
  Triage a defect signal — a log line, stacktrace, error, crash, or
  bug report — into a well-grounded, well-scoped GitHub issue WITHOUT fixing
  it. Use whenever the user pastes a log/error/stacktrace and wants it
  assessed and filed, says "triage this", "file a bug for this", "what's
  going on with this log", "file a bug issue", or hands you a symptom to
  investigate and track. The core job is investigation, an honest verdict
  (real bug vs. working-as-designed vs. log-hygiene), categorization,
  prioritization, and scoping — then filing in GitHub Issues. Triage is NOT
  a fix: do not change code. Fire this even when the user only pastes a log
  with little instruction — a raw defect signal is the trigger.
---

# Bug Triage

Turn a raw defect signal into a grounded, correctly-scoped GitHub issue. The
deliverable is a *filed issue*, not a code change. Your value is in the
investigation and judgment that precede the issue — anyone can run
`gh issue create`; the hard part is knowing **what** to file, **whether**
it's even a bug, and **how big** the fix actually is.

Triage and repair are different jobs. Mixing them corrupts both: you start
patching before you understand blast radius, and you stop investigating once
you have *a* fix instead of *the* root cause. Hold the line — investigate,
file, stop.

## The one rule

**Do not fix the bug.** No code edits, no "while I'm here" patches. If the fix
is obvious, capture it as a *candidate direction* in the issue and leave it
for the implementer. The user invoked triage on purpose.

## Workflow

Work the signal in this order. Each step has a reason — skipping one tends to
produce an issue that's a duplicate, ungrounded, mis-scoped, or simply wrong.

### 1. Read the signal verbatim

Triage the *actual* message, not your memory of it. If it's an image, read the
pixels; if you can't, ask — a misread log poisons every downstream step. Pull
out: level (ERROR/WARN/…), `msg`, `error.code`, `error.context.*`, the
`service`/`version` (dev vs prod changes the verdict), and every frame of
`error.stacktrace`. The stacktrace is a free map straight to the code.

### 2. Dedup first, before you invest

An issue that duplicates an existing one is worse than no issue — it splinters
the record. Search GitHub Issues *before* the deep dive:

- Use `gh issue list -R holomush/holomush --search "<tokens>" --state all --limit 100`
  with **single broad tokens first** (`KEK`, `cancel`, `alias`, `dependency`),
  then narrow — GitHub's search matches title/body full-text, but multi-word
  queries can miss issues that only literally contain one of the words.
- Always pass `--limit 100` (or paginate) — `gh issue list` defaults to 30
  rows.
- Check **adjacent** issues too, not just exact matches — a sibling in the
  same subsystem often reframes your finding (or is the real home for it).

If you find a live match, stop and tell the user — link or update it instead of
filing a new one.

### 3. Ground in the code

Follow the stacktrace to the exact `path:line`. Per the project's tool
precedence, reach for `mcp__probe__search_code` / `extract_code` before `rg`,
and `rg` before full-file `Read` — probe returns the whole enclosing function
in one call. Then:

- Read the function the error came from **and** the call site that logged it.
- Distinguish **symptom from root cause**. The logged frame is where it
  *surfaced*; the cause is often a manifest, a config default, or a missing
  registration upstream.
- Check **git history** for the suspect line — `git log -S '<symbol>' --oneline
  -- <path>` tells you when and in which PR it was introduced, which often
  reveals intent (a stale declaration from a rework, a deliberate fail-open).
- Assess **blast radius**: does this one signal indicate a localized fault, or
  does it disable something system-wide? (A single bad manifest `requires` can
  void dependency ordering for *every* plugin.)

### 4. Reach an honest verdict

Not every alarming log is a bug. Classify it — and be willing to conclude "not
a bug":

| Verdict | What it means | Typical output |
|---------|---------------|----------------|
| **Functional bug** | Behavior is wrong | bug issue, prioritized by impact |
| **Working as designed** | Intentional (read the code comments + the env: dev vs prod) | usually *no* issue; maybe a doc or log-hygiene follow-up |
| **Log-hygiene / signal** | Behavior fine, but an expected condition is logged at the wrong severity or carries a needless stacktrace | low-priority observability issue |
| **Config / environment** | Missing/!set env var or config, behavior correct given it | doc issue, or nothing |
| **Tracking debt** | Code already does the thing an open issue describes | flag the issue for verification — see §8 |

Do not force-fit a bug to satisfy the request. A crisp "this is intentional
fail-open; here's the code comment proving it, and the only real defect is the
stacktrace-at-WARN noise" is a *better* triage than a manufactured P1.

### 5. Categorize, prioritize, label

- **Type:** `bug` label for wrong behavior; `task` label for cleanup/refactor;
  `decision` label, or `task` + `design-needed` labels, when the resolution
  needs a design call, not just code.
- **Priority** = severity × likelihood × blast radius, *with justification in
  the issue*, expressed as a `priority::critical`..`priority::none` label (critical =
  critical, P4 = backlog). A 100%-reproducible defect that silently voids a
  correctness guarantee can be `priority::high` even if today's user-visible
  impact is nil. A scary log that's benign is `priority::low`.
- **Labels:** topical/category labels that match how sibling issues are
  tagged (`plugin`, `web`, `observability`, `handler`, `crypto`, …). A
  `theme:*` label is a lightweight grouping tag — attach one only if an
  existing theme genuinely groups the work; don't invent a new theme for a
  single issue. (Strategic planning lives in GSD, not a roadmap doc.)

### 6. Scope it — class, not site; leave decisions open

- **Scope to the class.** If five sibling call sites share the identical
  defect, the issue covers all of them with the observed one as the exemplar.
  Filing only the one site you saw leaves the bug half-fixed.
- **Leave fix-direction decisions open.** Triage scopes; it does not design.
  When there are two legitimate fixes (e.g. "remove the stale `requires`" vs
  "register the missing service"), record *both* as candidate directions and
  mark the choice as the implementer's design call. Don't pre-decide.
- **Split separable concerns.** If the root-cause fix and a deeper design
  question are distinct, file the concrete bug now and a follow-up for the
  design question, cross-referenced (`Related to #<n>` / `Discovered from
  #<n>`). (One symptom can legitimately spawn two or three issues.)
- **Ask vs. commit.** When the scope/priority is genuinely ambiguous (is this
  even worth filing? one issue or two? what priority?), surface the framing
  and ask the user. When it's clear-cut, just file. Don't dress an obvious
  call up as a question, and don't unilaterally make a judgment the user
  should own.

### 7. File the issue

```bash
gh issue create -R holomush/holomush \
  --title "<symptom-first title naming the actual defect>" \
  --label "bug,priority::<critical|high|medium|low|none>,<topical-labels>" \
  --body '<narrative — see template>'
```

Narrative `--body` template:

```
Goal: <what fixing this achieves>

Symptom (where observed — tip of main? commit? version=dev/prod? date):
  <the verbatim log essentials: level, msg, error.code, key context, top frames>

Root cause: <the upstream cause at path:line — not just the surfacing frame;
  cite the grep/probe evidence and the introducing PR if known>

Blast radius: <localized, or system-wide? currently benign but latent?>

Candidate fix directions (NOT decided here):
  A. <option> — <tradeoff>
  B. <option> — <tradeoff>

Files touched (candidates): <path:line, ...>

Acceptance: <verifiable done-conditions + the verify command>

Out of scope: <the separable concern, with its follow-up issue number if filed>
```

### 8. Tracking-debt: flag, don't close

If your grounding shows an *open* issue's work is already done in the code,
that is a real finding — but **do not close it on the strength of a code
reading alone, and never trust an in-issue "Fixed:"/"Closed:" comment as
proof.** Add a comment (`gh issue comment <number> -R holomush/holomush
--body '...'`) citing the `path:line` evidence and recommend the verification
needed (a round-trip test, an e2e run) before closing. Closing belongs to the
user, with grounded proof.

## Anti-patterns

- **Fixing instead of filing.** The most common failure. Stop at the issue.
- **Forcing a bug.** Concluding "working as designed" is a valid, valuable
  result. Don't invent a P1 to look productive.
- **One-site scoping.** Don't file the single call site when siblings share the
  bug — scope to the class.
- **Pre-deciding the fix.** Recording candidate directions ≠ choosing one.
  Leave the design call to the implementer.
- **Ungrounded findings.** Every claim cites `path:line` or a command you
  actually ran. "grep returned 0 hits" must be re-verifiable — spot-check
  before you assert it.
- **Mismatched theme labels.** Topical labels yes; `theme:*` only when an
  active roadmap theme truly fits.
- **Closing stale issues on a hunch.** Flag for verification (§8).

## Related infrastructure

- CLAUDE.md tool precedence — probe → rg → Read.
