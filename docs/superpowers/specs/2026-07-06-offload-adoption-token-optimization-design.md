<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Offload-Agent Adoption Mechanisms + Token Optimization Round 2 — Design

- **Bead:** holomush-drf7b
- **Date:** 2026-07-06
- **Status:** Draft (design phase)
- **Author:** AI-assisted (Claude Fable 5) + Sean Brandt
- **Extends:** `docs/superpowers/specs/2026-07-03-token-cost-latency-optimization-design.md`
  (holomush-wagqb) — its RD1/RD2 mechanism decisions carry over unchanged; this
  spec adds the adoption layer wagqb never designed and schedules its
  unimplemented Phases 3–4.

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are
to be interpreted as described in RFC 2119 / RFC 8174.

---

## 1. Context & Problem

The four `local-*` run-and-distill agents (wagqb Phase 2, PR #4569, merged
2026-07-03) exist to keep verbose `task` output out of the expensive main-session
context. They are not being used. Root cause — they shipped with **zero demand
layers**:

| Demand layer | Reviewer agents (adopted) | `local-*` agents (ignored) |
| --- | --- | --- |
| Directive description | "MUST run before any of: `jj git push`…" | "Use when the caller wants…" (passive) |
| Intent-verb hook reminder | `remind-pre-action-review.sh` fires on push/ship/close verbs | none |
| CLAUDE.md policy | Pre-Push Review Gates table | none — worse, CLAUDE.md's Commands section **pushes inline runs** ("MUST run `task test`…") with no delegation guidance |
| Action-moment hook | `enforce-task-runner.sh` hard-blocks raw `go test` → `task test` | the same hook stops there; it never suggests the offload agent |

Meanwhile the always-on token surface still carries the wagqb Phase 3/4 debt:
CLAUDE.md at 36 KB (T3a target ≤ 24 KB), the grepping full-skill force-load
(~2.5 K tok, T2a cheat-sheet designed), `testing.md` 10.7 KB / `invariants.md`
7.4 KB (T2b split designed), `landing-the-plane.md` redundant with CLAUDE.md
(T1c pointer designed). None implemented.

**Harness facts this design rests on** (grounded via claude-code docs,
2026-07-06; bd notes on holomush-drf7b):

1. Hook stdin JSON includes `agent_id` / `agent_type` **only for subagent tool
   calls** — a PreToolUse hook can scope enforcement to the main session.
   (Requires a one-line empirical probe at implementation time; see §7 and RD5.)
2. PreToolUse hooks may return
   `hookSpecificOutput: { permissionDecision: "allow"|"deny"|"ask", permissionDecisionReason }`
   on stdout — the reason is shown to the model. Richer than exit-2/stderr.
3. Project hooks fire for subagent tool calls too (hence the `agent_id` gate).
4. Agent frontmatter `tools:` cannot scope Bash to specific commands; command
   gating belongs in PreToolUse hooks.

## 2. Goals / Non-Goals

**Goals.**

- G1: Make offload the **default path** for verbose `task` runs in the main
  session — enforced at the action moment, not just documented.
- G2: Ship the wagqb Phase 3/4 token reductions (T1c, T2a, T2b, T3a) under
  their existing, reviewer-approved mechanism decisions (RD1/RD2 there).
- G3: Cut the per-session repo-agent description budget (~600 words today)
  without weakening any auto-fire trigger.
- G4 (carried from wagqb): every change individually revertible and measurable;
  no behavioral safety guardrail removed.

**Non-Goals.**

- N1: No new repo skill (user decision, RD4 below).
- N2: No product code, CI, or toolchain changes — `.claude/**`, `CLAUDE.md`,
  and the `site/…/contributing/` relocation targets only.
- N3: No changes to plugin-owned surfaces (jj skill force-load, marketplace
  plugin agents) — operator-side items are documented, not implemented (§4.4).
- N4: MUST NOT weaken the final pre-push gate: the parent session still runs
  `task pr-prep` itself before a real push (schema-regeneration side-effects,
  per CLAUDE.md and `subagent-briefing.md`).

## 3. Design — Adoption layer

### 3.1 `local-check` agent (merges local-test / local-lint / local-build)

One run-and-distill agent replaces three. `.claude/agents/local-check.md` is
created; `local-test.md`, `local-lint.md`, `local-build.md` are deleted.

- **Frontmatter:** `model: sonnet`, `effort: low`, `permissionMode: plan`,
  `tools: [Bash, Read, Grep, Glob]`, `color: green` — unchanged from the three
  it replaces.
- **Parameter:** the caller's prompt names a check kind ∈
  `test | lint | build | int | cover` plus optional scope args. Kind → command:

  | Kind | Command | Notes |
  | --- | --- | --- |
  | `test` | `task test [-- <pkg> / -run='…']` | existing local-test protocol verbatim |
  | `lint` | `task lint` | existing local-lint protocol verbatim |
  | `build` | `task build` | existing local-build protocol verbatim |
  | `int` | `task test:int [-- <pkg>]` | test protocol; MUST report a missing-Docker start error as one line, not a log dump |
  | `cover` | `task test:cover` | PASS: overall + any package below the 80% floor, one line each; FAIL: same as test protocol |

- **Carried over verbatim (MUST):** the input-validation rules (package-path
  regex `^\./[A-Za-z0-9_./-]+$`, single-quote refusal and `=`-bound quoting for
  `-run='…'`), sole-command-per-Bash-call (no `| tee`/`| tail`/trailing `echo`),
  exit-code-first verdicts, strict output schemas with caps (≤ 15 failing
  tests, ≤ 20 lint findings, `+N more` truncation notes), never paste raw
  output, never edit files, `task` — never raw `go`/`golangci-lint`.
- **Description (≤ 80 words, directive):** MUST-style — "MUST be dispatched
  instead of running `task test`, `task lint`, `task build`, `task test:int`,
  or `task test:cover` inline in the main session (inline runs flood the main
  context with raw output; hook-enforced). Returns a compact verdict. Append
  `# offload-exempt` to the inline command only when raw output is genuinely
  needed in-thread." The description names the check kinds so the model can
  route without loading the body.
- Callers MAY dispatch several `local-check` agents concurrently for
  independent kinds (test + lint + build), but MUST NOT bundle a `local-check`
  call in a parallel batch with other maybe-failing tool calls (cancel-storm,
  ADR holomush-cr3gq — rule retained from wagqb §4.1).

### 3.2 `local-pr-prep` — description rewrite only

The agent body (contention protocol, `▸ pr-prep result:` file reading,
advisory-PASS reminder) is unchanged. Its description is rewritten directive:
pr-prep **iteration/triage MUST** go through `local-pr-prep`; only the **final
pre-push gate** runs inline in the parent (N4). ≤ 60 words.

### 3.3 Hook enforcement (extend `enforce-task-runner.sh`)

A new rule in the existing top-level-segment parser (reusing its chain/pipe/
control-flow splitting and `first_cmd_word`):

- **Match:** first word `task`, second word ∈ exactly
  `{test, test:int, test:cover, test:verbose, lint, build}`.
  Sub-task variants (`lint:go`, `lint:proto`, `test:e2e`, `docs:*`, …) are
  **deliberately not matched** — narrow start; they are targeted/short-output
  or have no offload wrapper (RD6).
- **Skip conditions (checked in order):**
  1. hook input has a non-empty `agent_id` → subagent session → all new rules
     skipped entirely (existing raw-`go`/lint blocks still apply everywhere);
  2. the **raw** command string contains `# offload-exempt` (checked before
     quote-stripping, mirroring the `# jj-exempt` precedent) → allowed;
- **Action on match (main session, no exempt):** emit
  `hookSpecificOutput: { hookEventName: "PreToolUse", permissionDecision: "deny", permissionDecisionReason: … }`
  where the reason names the exact replacement — e.g. *"Inline `task test`
  floods the main context. Dispatch the `local-check` agent
  (`Agent` tool, `subagent_type: local-check`, prompt: `test -- ./internal/foo/`)
  and read its compact verdict. If raw output is genuinely needed in-thread,
  re-run with `# offload-exempt` appended."*
- **`task pr-prep` / `pr-prep:full` / `pr-prep:docs`:** soft stderr nudge only,
  never deny (N4): *"Iterating? Dispatch `local-pr-prep` for a compact verdict.
  Final pre-push gate? Run inline — the parent MUST run it itself."*
- **Enforcement mode constant:** a single variable at the top of the script,
  `OFFLOAD_ENFORCE=deny|nudge`. Ships as `deny` **only after** the §7 probe
  confirms `agent_id` is present in this CC version's hook input; otherwise
  ships as `nudge` with a follow-up bead to flip it (RD5).
- **Known cost (documented in the hook header):** a deny cancels sibling calls
  in a parallel tool batch. Accepted: the replacement is an `Agent` dispatch,
  verbose task runs are usually solo calls, and the hook header already
  documents this hazard class for the pre-existing hard blocks.
- **Tests:** bats coverage for the new matcher — main-vs-subagent input, each
  matched task name, exempt token, pr-prep nudge path, `OFFLOAD_ENFORCE` modes.

### 3.4 Policy text

- **CLAUDE.md, Commands section** (lands inside the §4.2 T3a trim, same diff):
  two new requirement rows — (1) **MUST** delegate `task test|lint|build|
  test:int|test:cover` to `local-check` (and pr-prep iteration to
  `local-pr-prep`) instead of inline Bash; hook-enforced; `# offload-exempt`
  escape hatch; (2) a `local-check` PASS verdict **satisfies** "MUST run
  `task test` before claiming complete"; the **final** `task pr-prep` before a
  push still runs inline in the parent (N4).
- **`remind-pre-action-review.sh`:** one new intent-verb branch — prompt
  matching `run (the )?tests?|does it (build|compile)|check (the )?lint|run
  lint|is (it|the build) green` injects a one-line reminder to dispatch
  `local-check` rather than running the task inline. Silent otherwise (the
  hook's existing short-circuit shape).
- **`subagent-briefing.md`:** the model-tier table row and cancel-storm rule
  are updated to name `local-check`/`local-pr-prep` (done inside the T1c slim,
  §4.1, same diff — no double edit).

## 4. Design — Token round 2

### 4.1 wagqb Phase 3 (T1c, T2a, T2b) — scheduled, mechanisms unchanged

Implemented exactly per wagqb RD1 (index-pointer; no false moment-triggering):

- **T1c:** `landing-the-plane.md` → one-line pointer to CLAUDE.md's canonical
  "Landing the Plane" section. `subagent-briefing.md` slimmed in place — every
  MUST retained; local-* references updated per §3.4.
- **T2a:** `require-grepping-skill.sh` stops requiring the full `grepping`
  skill; it injects a compact search-ladder cheat-sheet instead. The
  cheat-sheet **MUST retain verbatim:** never bare `grep` (use `rg`),
  probe-before-rg for Go symbols, the `\|`-alternation and stray-`-r`
  silent-failure traps, and the exit-code-not-output rule. The full skill loads
  on demand via the dev-flow plugin's existing `nudge-rg-failure` PostToolUse
  hook.
- **T2b:** `testing.md` and `invariants.md` split index-pointer style: the
  auto-loaded core keeps its `paths:` and **every MUST inline**, targets
  ≤ 5 KB each, and points at `.claude/rules/references/<name>-detail.md` for
  extended examples, rationale, and tier catalogues.

### 4.2 wagqb Phase 4 (T3a) — CLAUDE.md trim, scheduled per RD2

36 KB → **≤ 24 KB**, MUST-preserving, conservative. The pr-prep reading-guide,
session-isolation walkthrough, and Pre-Push Gate detail tables relocate to
their existing homes under `site/src/content/docs/contributing/` (`pr-prep.md`,
`pr-guide.md`, `sessions.md`); every MUST stays in CLAUDE.md as a ≤ 1-line
index entry. The three stale pre-`how-to/` links (wagqb noted lines 77/355/394)
are fixed in the same diff. The §3.4 delegation rows land as part of the
trimmed Commands section. **The diff MUST be human-reviewed before landing.**

### 4.3 Reviewer/aux description trims

`crypto-reviewer` (110 words), `bead-auditor` (95), `code-reviewer` (78)
descriptions are compressed ~50%. Constraint: descriptions are the auto-fire
trigger surface — **every MUST-trigger phrase, path list, and skip-override
clause is retained**; only connective prose is cut. Agent bodies are untouched
(bodies cost nothing until spawned). Combined with §3.1's 3→1 merge, the
repo-agent description budget drops from ~600 to ~350 words per session.

### 4.4 Operator-side documentary note (no repo change)

RD4-of-wagqb-style note, recorded here for the operator: the per-session agent
listing is dominated by **marketplace plugin agents** (~40+ descriptions from
cicd-automation, jvm-languages, python-development, observability-monitoring,
…). Disabling unused plugins for this project in `~/.claude` config would save
more tokens per session than every repo-side trim in this spec combined. The
call is the operator's (their config spans other projects); out of repo scope.

## 5. Risks & Mitigations

| Risk | Mitigation |
| --- | --- |
| `agent_id` absent from hook input on the current CC version → deny would also fire inside subagents | §7 probe **before** enabling; `OFFLOAD_ENFORCE=nudge` fallback mode + follow-up bead (RD5) |
| Deny cancels sibling calls in a parallel batch | Documented in hook header; verbose runs are usually solo; replacement is an Agent dispatch |
| Hard deny blocks a legitimate inline need | `# offload-exempt` token (mirrors `# jj-exempt`); pr-prep is never denied (N4) |
| `local-check` false PASS | Exit-code-first protocol retained verbatim; final pr-prep still runs in parent (N4) |
| CLAUDE.md trim drops a guardrail | wagqb N2 discipline: MUST-preserving cuts, ≤ 24 KB conservative target, human-reviewed diff, guardrail checklist (§7) |
| Cheat-sheet omits a critical grepping warning | §4.1 verbatim-retain list, verified against the current skill before landing |
| Description trims weaken reviewer auto-fire | §4.3 constraint: trigger phrases/paths/skip-clauses retained verbatim; prose only |
| One bundled PR is hard to review | One jj commit per item, each independently revertible (G4); T3a commit explicitly flagged for human review |

## 6. Delivery & Phasing

One PR, one jj commit per item, in this order (cheap/safe → careful):

1. §3.1 + §3.2 — `local-check` merge + `local-pr-prep` description
2. §3.3 — hook enforcement (+ bats) — lands in the mode the §7 probe allows
3. §3.4 — `remind-pre-action-review.sh` intent verbs
4. §4.1 — T1c, T2a, T2b (one commit each)
5. §4.3 — description trims
6. §4.2 — T3a CLAUDE.md trim (last; human-reviewed; carries the §3.4 CLAUDE.md rows)
7. §4.4 — documentary note (in this spec + bd note; no code)

## 7. Verification

- **`agent_id` probe (gates §3.3 deny mode):** a temporary logging line in a
  PreToolUse hook captures the input JSON during one `local-check` (or any
  subagent) dispatch and one main-session Bash call; confirm the field's
  presence/absence split. Remove the probe in the same branch.
- **Hook bats matrix:** per §3.3 — matched names, exempt token, subagent skip,
  pr-prep nudge, both `OFFLOAD_ENFORCE` modes.
- **Offload behavior check:** in a fresh session, ask for "run the tests for
  ./internal/naming/" — expect a `local-check` dispatch (or a deny that leads
  to one), not inline output.
- **Context tax:** wagqb §7 protocol — before/after cold-session token proxy
  (char/4 on the assembled preamble) per commit; record deltas on the bead.
- **Guardrail preservation:** checklist asserting every pre-change MUST still
  appears somewhere reachable (CLAUDE.md index entry, rule core, on-demand
  reference, or site doc).
- **Existing gates:** `task pr-prep` green (docs lane will not apply — hook +
  agent changes are not docs-only).

## 8. Invariant Registry Check

No new `INV-<SCOPE>-N`. Like wagqb (§8 there), this is AI-tooling configuration
(`.claude/**`), not a holomush system-behavior guarantee; no TOOLING scope
exists in `docs/architecture/invariants.yaml` and none is warranted.

## 9. Resolved Decisions

- **RD1 — Enforcement mode (user, 2026-07-06): hard deny + escape hatch.**
  Inline `task test|lint|build|test:int|test:cover|test:verbose` in the main
  session → PreToolUse JSON `deny` naming the `local-check` replacement;
  `# offload-exempt` runs inline; pr-prep is soft-nudge only (N4). Soft-only
  and ask-mode rejected: repo history (raw `go test` block) shows nudges get
  rationalized away; ask-mode adds friction to unattended sessions.
- **RD2 — Agent shape (user): merge test/lint/build → `local-check`;
  `local-pr-prep` stays separate.** The pr-prep contention/result-file protocol
  and advisory-only semantics don't share the check contract. All-four merge
  and keep-all-four rejected.
- **RD3 — Scope (user): bundle adoption + wagqb Phases 3/4 in one effort.**
  Overrides the sequenced-plans recommendation; review-size risk accepted and
  mitigated by commit-per-item + human review on T3a.
- **RD4 — No new repo skill (user).** Demand lives in hook + descriptions +
  CLAUDE.md policy; a composite verify skill would add an invocation surface
  nothing forces sessions to hit (YAGNI).
- **RD5 — Deny gated on the `agent_id` probe (design).** If the field is
  absent on the current CC version, §3.3 ships `OFFLOAD_ENFORCE=nudge` and a
  follow-up bead tracks the flip. Rationale: without main-vs-subagent
  discrimination, a deny would break the offload agents themselves.
- **RD6 — Narrow hook match list (design).** Exactly six `task` names; sub-task
  variants (`lint:go`, `test:e2e`, `docs:*`) stay inline-allowed — they are
  targeted/short-output or have no wrapper. Widening is a one-line follow-up if
  usage shows leakage.
