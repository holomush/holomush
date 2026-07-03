<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Token / Cost / Latency Optimization for the Claude Code Dev Workflow — Design

- **Bead:** holomush-wagqb
- **Date:** 2026-07-03
- **Status:** Draft (design phase)
- **Author:** AI-assisted (Claude Opus 4.8) + Sean Brandt

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are
to be interpreted as described in RFC 2119 / RFC 8174.

---

## 1. Context & Problem

HoloMUSH is developed primarily by concurrent AI-agent sessions. Every session
pays a large, mostly-fixed **context tax** before the user's first instruction
is processed, and every subagent invocation pays a **cost/latency** bill that is
independent of that context tax. Neither has been measured or tuned.

### 1.1 Measured cold-session tax (grounding: `.claude/` probe, 2026-07-03)

| Always-loaded surface | Size | When |
| --- | --- | --- |
| `CLAUDE.md` | 36 KB (~9K tok) | every session, always |
| `.claude/rules/*.md` | 73 KB total (18 rules) | path-scoped auto-load; stacks fast |
| `jj` skill (full) | ~600 lines (~5K tok) | jj-**plugin** SessionStart hook (not repo-owned; see §3.3) |
| `grepping` skill (full) | large (~2.5K tok) | repo `require-grepping-skill.sh` SessionStart (repo-slimmable) |
| 4 unscoped rules | 15.9 KB (~4K tok) | every session (no `paths:`) |
| `bd prime` output | ~1.2K tok | **appeared twice** this session |
| engram recall + digest | ~2K tok | required first action |

**Estimated cold baseline ≈ 26K tokens**, climbing with every file touched
(path-scoped rules stack — one Go test file pulls `testing.md` 10.7 KB +
`logging.md` 4 KB + `terminology.md` 1.5 KB ≈ 16 KB).

### 1.2 Confirmed drivers

- **Four rules have no `paths:` scope** and load every session regardless of
  relevance: `search-tools.md` (5.5 KB), `beads-project.md` (4.2 KB),
  `subagent-briefing.md` (3.3 KB), `landing-the-plane.md` (2.9 KB).
  `search-tools.md` substantially **duplicates the `grepping` skill** that is
  force-loaded anyway.
- **Over-broad path globs.** `terminology.md` matches
  `**/*.{go,md,proto,lua,svelte,ts}` (nearly any file); `logging.md` any Go file;
  `testing.md` — the **heaviest rule at 10.7 KB** — any `*_test.go`.
- **`bd prime` runs redundantly.** Confirmed: registered at **PreCompact in both**
  `.claude/settings.json` and user-global `~/.claude/settings.json`. Observed:
  the "Beads Workflow Context" block appeared **twice at SessionStart** — the repo
  `settings.json` SessionStart `bd prime` stacking with the beads plugin's own
  `ln`/SessionStart injector (second injector not yet pinned; remediation gated on
  confirming it).
- **Per-turn hooks are NOT a problem** (verified — corrects an earlier hypothesis):
  `remind-pre-action-review.sh` stays silent (`exit 0`) unless the prompt matches
  handoff/plan-intent verbs; `session-reminder.sh` is a **Stop** hook that emits
  only when there is unsynced work. Both already short-circuit.

### 1.3 Two orthogonal axes

| | **Axis 1 — total tokens (context)** | **Axis 2 — cost & latency (per sub-unit)** |
| --- | --- | --- |
| Optimizes | main-thread context growth → fewer compactions, cheaper every turn | \$ and wall-clock of work a subagent does |
| Levers | offload agents · trim always-on · tighten globs · fix hook waste | model tier per agent · `effort` dial · Batches (50%) · prompt caching |

The axes are independent and MUST be applied together: offload to keep main
context clean, **and** run each offloaded agent on the cheapest tier that does
its job reliably.

## 2. Goals / Non-Goals

**Goals.**

- G1: Reduce the cold-session context tax by ~40–50% without losing any guardrail.
- G2: Keep verbose command output (`task test`, `task pr-prep`) out of the main
  thread via offload agents.
- G3: Make model tier and `effort` a deliberate per-agent choice, matched to job.
- G4: Every change MUST be individually revertible and measurable.

**Non-Goals.**

- N1: This is a design; **no implementation** happens in this phase.
- N2: MUST NOT remove or weaken any behavioral **safety** guardrail (pre-push
  rebase truncation warning, jj guard hooks, review gates, MUST-level *behavioral*
  conventions). Cuts relocate detail; they do not drop rules. **Sanctioned
  exception:** a deliberate, safety-preserving *refinement* of a cost/reliability
  tuning convention — the subagent model-floor (§4.3) — is permitted under N2: it
  narrows *which* agents may use haiku, keeps reviewers on opus/fable, and removes
  no safety property (it tightens a rule, it does not drop one).
- N3: No change to the holomush product code, CI, or the beads/jj toolchain.

## 3. Design — Axis 1 (total tokens)

### 3.1 Fix hook waste (T1a) — *trivial, no risk*

- **Grounded finding (RD4):** the repo `.claude/settings.json` registers `bd prime`
  exactly ONCE per event — SessionStart (`:69`) and PreCompact (`:35`). It is
  already the canonical, **portable** single source (fires for every contributor,
  CI, and fresh workspace per CLAUDE.md's `.claude/` inheritance guarantee). The
  observed doubling originates **outside the repo**: the operator's
  `~/.claude/settings.json` also registers PreCompact `bd prime` (`:176`), and the
  second SessionStart copy comes from a non-repo injector — most likely `bd`'s own
  plugin-level prime (the `beads@beads-marketplace` plugin is enabled), though its
  exact hook is not pinned from the repo surface (§1.2).
- **Decision (RD4):** the repo settings are correct as-is and are **NOT changed** —
  keeping the portable single source protects fresh clones / CI / other
  contributors. De-duplication is an **operator-side, personal-config cleanup**
  (remove the duplicate PreCompact `bd prime` from `~/.claude/settings.json`; if a
  plugin injects SessionStart prime universally on that machine, drop whichever
  SessionStart copy is redundant). This cleanup is **out of scope for the repo PR**;
  it is documented so the operator can reclaim the ~1.2K tok/session at their
  discretion (their global also covers non-holomush projects, so the call is
  theirs). Verification: a fresh session shows a **single** Beads Workflow Context
  block after the operator cleanup.

### 3.2 Trim always-on rules (T1b, T1c) — *low risk*

- T1b: `search-tools.md` **SHOULD** be reduced to a ~15-line pointer to the
  `grepping` skill (which is force-loaded), preserving only the repo-specific
  MUSTs (never bare `grep`; probe-before-rg for Go symbols; the exit-code-not-output
  rule) and deleting the content that duplicates the skill.
- T1c (**decided, RD1**): "moment-triggering" is NOT achievable for these — no
  `paths:` glob or hook event maps to "about to push" or "about to dispatch an
  agent," so a rule cannot auto-load at those moments. Concrete resolution:
  - `landing-the-plane.md` is **redundant** with CLAUDE.md's "Landing the Plane"
    section (the canonical copy), the `jj` skill, and the `remind-pre-action-review`
    UserPromptSubmit hook that already fires on push/hand-off intent. Reduce it to
    a **one-line pointer** to CLAUDE.md's section — every MUST already lives there.
  - `subagent-briefing.md` has no clean auto-trigger and is universally relevant
    whenever an agent is dispatched, so it **stays always-on but is slimmed in
    place** (only 3.3 KB; the §4.3 carve-out already edits it). Each MUST retained.

### 3.3 Slim the grepping forced-load; jj is plugin-owned (T2a) — *moderate*

**Scope correction (grounded, round 2):** the two forced skill loads are NOT
symmetric, and only one is repo-actionable.

- **jj — plugin-owned, NOT repo-slimmable.** The jj SessionStart injection is the
  **jj plugin's** `session-start-jj-detect` hook
  (`fzymgc-house-skills/jj/hooks/hooks.json`), which the repo does not own and
  cannot alter via `.claude/**`. That hook already injects a **compact cheat-sheet**
  (git→jj mapping, key concepts, escape hatch) that ALREADY contains verbatim the
  load-bearing safety items this initiative cares about — the pre-push rebase
  `-s`-not-`-r` truncation hazard and the `guard-jj-*` PreToolUse gates — **plus**
  a "REQUIRED: Load jj skill" instruction, which is what actually drives the ~5K
  full-skill load. Therefore: the jj cheat-sheet T2a would build largely already
  exists; suppressing the full jj load is an **upstream/plugin** change outside
  this design's `.claude/**` surface, and a repo-only "load jj on demand" edit
  would directly CONTRADICT the plugin's own REQUIRED-load instruction. The design
  **MUST NOT** attempt to slim the jj load from the repo. It **MAY** file a
  separately-tracked upstream request against the jj plugin to gate its full-skill
  requirement behind on-demand triggers (conflict/rebase).
- **grepping — repo-owned, the actual T2a target.** The grepping forced-load is
  driven by the repo-owned `require-grepping-skill.sh` (`.claude/settings.json`
  SessionStart). The design **SHOULD** replace its full-skill requirement with a
  compact **search-ladder cheat-sheet** injected at SessionStart, letting the full
  `grepping` skill load on demand via the dev-flow plugin's existing
  `nudge-rg-failure` `PostToolUse`/`Bash` hook (plugin-provided at
  `fzymgc-house-skills/dev-flow/hooks/nudge-rg-failure`, not a repo-local script).
- The grepping cheat-sheet **MUST** retain verbatim: never bare `grep` (use `rg`),
  probe-before-rg for Go symbols, the `\|`-alternation and stray-`-r` silent-failure
  traps, and the exit-code-not-output rule.

### 3.4 Split heavy rules (T2b) — *low risk*

- `testing.md` (10.7 KB) and `invariants.md` (7.4 KB) **SHOULD** be split into a
  slim always-relevant core (naming, tier table, the MUSTs, the binding-annotation
  rule) plus a `references/*.md` deep section loaded on demand. The auto-loaded
  portion **SHOULD** target ≤ 5 KB each. **Mechanism (decided, RD1): index-pointer.**
  A `.claude/rules/*.md` file has no built-in `references/` auto-load (that is the
  *skills* idiom), so the core rule keeps its existing `paths:`, carries **every
  MUST inline**, and adds a one-line pointer — `Deep reference:
  .claude/rules/references/<name>-detail.md (read on demand)`. Only genuinely
  non-MUST material (extended examples, rationale, tier catalogues) moves to the
  `references/` file. The split **MUST NOT** relocate any MUST out of the
  auto-loaded core — MUSTs stay where they always load.

### 3.5 Trim CLAUDE.md (T3a) — *higher risk, do last*

- CLAUDE.md (36 KB) is the single biggest always-on cost and the load-bearing
  guardrail index. The design **MAY** relocate exhaustive prose (the pr-prep
  reading-guide, the session-isolation walkthrough, the Pre-Push Gate detail
  tables) into on-demand docs, keeping **every MUST as a one-line index entry**.
- Cuts **MUST** be conservative and MUST-preserving; the diff **MUST** be reviewed
  by a human before landing. **Target (decided, RD2): 36 KB → ≤ 24 KB** —
  conservative, capturing ~⅓ of the always-on cost at low risk. A more aggressive
  target is explicitly **rejected**: a lost guardrail causing one bad push/rebase
  costs far more than the tokens saved. The relocated prose blocks (pr-prep
  reading-guide, session-isolation walkthrough, Pre-Push Gate detail tables) move
  to their existing on-demand homes under
  `site/src/content/docs/contributing/how-to/` (`pr-prep.md`, `pr-guide.md`,
  `sessions.md`); **every MUST stays in CLAUDE.md as a ≤1-line index entry**.
  Note for the T3a follow-on plan: CLAUDE.md's current links to these (lines 77,
  355, 394) point at the pre-`how-to/` paths and are **stale** — fix them in the
  same diff.

## 4. Design — Axis 2 (cost & latency)

### 4.1 Offload agents (T2c)

Four thin "run-and-distill" agents run a `task` command in their own context and
return a strict compact schema — not raw output:

```text
{ verdict: "pass" | "fail",
  summary: string,
  failures: [ { name, file_line, message } ],
  truncated_note: string }
```

- **`local-test`** — runs `task test [-- <pkg/args>]`; returns only failing tests
  - first error line each. Model **sonnet**, `effort: low`. Break-even: test output
  is routinely 2–10K+ tokens; agent overhead ~1.5K → net main-context win almost
  always.
- **`local-pr-prep`** — runs `task pr-prep`; returns the exit-code verdict + the
  `▸ pr-prep result:` `status=` line + any failed gate name. Model **sonnet**,
  `effort: low`. **MUST** be briefed as **iteration/triage only**: the parent
  session MUST run `task pr-prep` itself before the real push, because a subagent
  cannot surface schema-regeneration side-effects (grounding: `subagent-briefing.md`
  - engram gotcha).
- **`local-build`** — runs `task build`; returns pass/fail + the first compile
  errors (`file:line: message`). Model **sonnet**, `effort: low`.
- **`local-lint`** — runs `task lint`; returns pass/fail + one line per finding
  (`file:line: linter: message`), capped with a "+N more" note. Model **sonnet**,
  `effort: low`. (`task lint` fans out across many linters × files, so its output
  is often long — higher offload value than a naive "build/lint output is short"
  heuristic assumes.)

**Why all four, incl. build/lint (decision RD3):** the offload win **scales with
the MAIN session's model cost**. On an opus/fable main session, even the short
output of `task build`/`task lint` is expensive to ingest into the main context,
while the subagent distills it at cheaper sonnet rates — so the break-even output
size drops well below where build/lint sit, and the net win holds even for
short-output commands. Marginal cost to add is ~zero (identical contract +
template).

**Guardrails (MUST):** (a) these agents MUST be read-only-briefed — subagents
auto-snapshot `.claude/` writes into the parent `@`; (b) a `local-*` agent call
**MUST NOT** be bundled in a tool batch with other maybe-failing calls (parallel
cancel-storm); (c) model floor **sonnet** (§4.3).

**Honest accounting:** offload agents do not reduce *total* system tokens (the
command output is still generated + read, in the sub-context) — they reduce
**main-thread context growth**, which drives compaction and per-turn cost.

### 4.2 Right-tier the existing fleet (T2d) — *low effort, compounds*

The design **SHOULD** set `model`/`effort` per agent to match job shape:

| Tier | Job shape | Agents |
| --- | --- | --- |
| **Haiku** — cheap/fast, low-judgment | schema-constrained output, correctness binary **and verified downstream** | future mechanical fan-out (log→JSON distillers, verified codemods) |
| **Sonnet** — workhorse (current floor) | modest judgment | `local-test`/`local-pr-prep`, Explore fan-out, `fix-worker`, `bead-auditor` |
| **Opus / Fable** — reasoning-critical | cost of error dominates | `code`/`crypto`/`abac`/`design`/`plan` reviewers |

Reviewer-tier agents **MUST NOT** be downgraded for cost — a missed
security/design defect costs orders of magnitude more than the tokens saved.

**Where tier choice compounds:** (1) **fan-out** multiplies the choice by N — cheap
collectors, expensive synthesis; (2) **verify-cheap / judge-expensive** — many
cheap finders + few expensive verifiers on survivors; (3) the **`effort` dial** is
itself per-agent — `effort: low` on a *sonnet* agent cuts token spend and
tool-call appetite (coupled on 4.7/4.8) and usually beats dropping to haiku,
because `effort` **errors on Haiku 4.5** (grounding: claude-api skill — this is
repo-unverifiable and currently backs SHOULD-level guidance only; see the §5 risk
row before any MUST-level decision rests on it).

### 4.3 `subagent-briefing.md` carve-out

The current rule states a blanket sonnet floor. The design **SHOULD** replace it
with a narrow, explicit exception:

> Sub-agent dispatch — default model floor is `sonnet`. `haiku` is permitted
> **only** for agents whose output is schema-constrained **and** independently
> verified downstream (e.g. a mechanical distiller in a fan-out whose result a
> sonnet+ verifier checks). **Never** `haiku` for an agent whose judgment the
> caller acts on unverified (test triage, review, flake-vs-real). Prefer
> **`effort: low` on a sonnet agent** over dropping to haiku — it keeps the effort
> dial (which errors on haiku) and higher reliability. Reviewer agents
> (code/crypto/abac/design/plan) stay **opus/fable** — never downgrade for cost.

### 4.4 Non-tier cost levers (documentary)

- **Batches API = 50% cost** for non-latency-sensitive bulk work (one-time audits,
  scheduled doc generation) — **not** the interactive dev loop.
- **Prompt caching** (5-min TTL) benefits repeated same-prompt agent invocations;
  back-to-back `local-test` iterations benefit most.

These are noted for the plan to exploit where applicable; neither requires new
infrastructure here.

## 5. Risks & Mitigations

| Risk | Mitigation |
| --- | --- |
| A trim drops a guardrail → expensive mistake (bad push/rebase) | Non-Goal N2; every cut is MUST-preserving; T3a human-reviewed; changes individually revertible |
| Slimmed skill cheat-sheet omits a critical warning | §3.3 enumerates the verbatim-retain list; verify against current skill before landing |
| Offload agent returns a false "pass" | §4.1: parent re-runs pr-prep before push; agents return failures verbatim, not judgments, where possible |
| Haiku carve-out over-applied | §4.3 restricts to schema-constrained + downstream-verified; reviewer tier fenced |
| `effort`-errors-on-haiku claim (§4.2/§4.3) is repo-unverifiable | Sourced from the claude-api skill; backs SHOULD-level guidance only. Plan MUST independently confirm (1-line agent-config probe) before any MUST-level decision relies on it |
| Measurement is subjective | §7 defines a concrete before/after measurement protocol |

## 6. Phasing (recommended)

1. **Phase 1 (quick wins, ~0 risk):** T1b (search-tools pointer), T2d (right-tier
   fleet); T1a is operator-side config cleanup (RD4) — documented, not in the repo
   PR. (T1c/T2a/T2b remain Phase 3; their mechanism is decided per RD1 but execution
   is a follow-on plan.)
2. **Phase 2 (offload):** T2c — `local-test`, `local-pr-prep`, `local-build`,
   `local-lint` (RD3: all four; the offload win scales with the main-session model
   cost, so build/lint pay off on an opus/fable main session) + §4.3 carve-out.
3. **Phase 3 (structural):** T1c, T2a, T2b.
4. **Phase 4 (careful):** T3a CLAUDE.md trim, human-reviewed diff.

## 7. Verification

- **Context tax:** capture SessionStart-injected token count (via
  `messages.count_tokens` on the assembled preamble, or a manual char/4 proxy)
  before and after each phase; record the delta per initiative.
- **Offload agents:** measure main-thread token growth for a representative
  `task test` run inline vs via `local-test`.
- **Guardrail preservation:** a checklist asserting every pre-change MUST still
  appears somewhere reachable (index entry, on-demand doc, or skill).
- **`bd prime` de-dupe:** fresh session shows exactly one Beads Workflow Context
  block.

## 8. Invariant Registry Check

This design introduces **no** new `INV-<SCOPE>-N` registry invariant. It concerns
AI-tooling configuration (`.claude/**`), not a holomush system-behavior guarantee;
there is no TOOLING scope in `docs/architecture/invariants.yaml` and none is
warranted (per `.claude/rules/invariants.md` "what rises to a registry invariant").
The subagent model-floor rule is a `.claude/rules` convention, not a system
invariant, and remains so.

## 9. Resolved Decisions

All design decisions are resolved; none defers to implementation. Each is baked
into the section cited.

- **RD1 — On-demand load mechanism (§3.2 T1c, §3.4 T2b): index-pointer, and no
  false "moment-triggering."** Rule cores keep their `paths:` and hold every MUST
  inline; only non-MUST reference material moves to
  `.claude/rules/references/<name>-detail.md` (read on demand). `landing-the-plane.md`
  → one-line pointer to CLAUDE.md's canonical "Landing the Plane" section;
  `subagent-briefing.md` → slimmed in place (no auto-trigger exists for "dispatch").
  Rejected: `paths:` sub-scoping (cannot express "at push"/"at dispatch") and
  skill-ification (skills are for procedures, not reference dumps).
- **RD2 — CLAUDE.md trim target (§3.5 T3a): 36 KB → ≤ 24 KB, conservative,
  MUST-preserving, human-reviewed.** A more aggressive target is rejected — a lost
  guardrail costs more than the tokens saved. Prose relocates to existing
  `site/src/content/docs/contributing/` docs; every MUST stays as a ≤1-line index entry.
- **RD3 — Offload-agent set (§4.1 T2c): `local-test`, `local-pr-prep`,
  `local-build`, `local-lint`.** All four follow the same run-and-distill contract.
  The offload win **scales with the MAIN session's model cost**: on an opus/fable
  main session even the short output of `task build`/`task lint` is expensive to
  ingest into the main context while the subagent distills at cheaper sonnet rates,
  so the break-even output size drops below where build/lint sit and the net win
  holds. Marginal cost to add is ~zero (identical contract + template);
  `task lint`'s output is often long anyway (many linters × files).
- **RD4 — `bd prime` de-dup (§3.1 T1a): no repo change; operator-side cleanup.**
  The repo already registers `bd prime` exactly once per event (the portable
  canonical source); the doubling is entirely operator/plugin config outside the
  repo. The operator removes the duplicate from `~/.claude/settings.json` at their
  discretion. No repo PR change.
<!-- adr-capture: sha256=90893a2742603107; session=cli; ts=2026-07-03T16:05:18Z; adrs=holomush-w1a26,holomush-etnfd,holomush-cr3gq -->
