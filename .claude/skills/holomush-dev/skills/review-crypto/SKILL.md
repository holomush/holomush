---
name: review-crypto
description: Adversarially review crypto-domain code changes against the master spec invariants before /gsd-code-review runs
---

@agent-crypto-reviewer Review the crypto-domain code changes described below
against `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` (the
master spec) and the Phase 3a/3d grounding docs. This gate runs BEFORE
`/gsd-code-review` for any change touching the event-payload-cryptography surface
(`internal/eventbus/crypto/`, `internal/eventbus/codec/`,
`internal/eventbus/history/dispatcher.go`,
`internal/eventbus/history/cold_postgres.go`,
`internal/plugin/event_emitter.go::Emit`,
`internal/eventbus/audit/projection.go`, plugin manifest `crypto.emits`
declarations, or migrations affecting `crypto_keys` / `events_audit`).

**Target:** $ARGUMENTS

**If no target was given:** review the full branch diff against the merge
base. Use `git diff origin/main...HEAD` to get the diff. Review the diff AND
the full files it touches.

**If a target was given:** treat it as either a path (review that file's
changes vs merge base), a commit revset (review that revset's diff), or a
PR number (fetch with `gh pr diff <n>` and review).

**Before writing findings:** read the master spec sections cited in the
agent's "Required reading" section — at minimum §2 (Invariants), §3
(Architecture), §4.2 (AAD binding), §7 (Plugin authorization), §8 (Cold tier
handling), and the Phase 3d grounding doc Appendix A (§7.7 amendment). You
MUST know the invariants before judging compliance with them.

Apply the adversarial stance and invariant checklist from your agent prompt.
Produce findings grounded in `path:line` for code claims and `§N.N` for spec
claims, with a binary READY / NOT READY verdict.

**On receipt:** the agent's response IS the full report — read it. The agent
also persists the full report to `.claude/agent-memory/crypto-reviewer/reports/`
when it has memory configured. Do NOT spawn a second agent to "retrieve full
findings"; subagents are stateless across invocations.

**Ordering:** this gate runs BEFORE `/gsd-code-review`. After crypto-reviewer
returns READY, then run `/gsd-code-review` for the generic adversarial pass.
If crypto-reviewer returns NOT READY, address findings before running
`/gsd-code-review` (so it doesn't waste a pass on code that's still
crypto-broken).
