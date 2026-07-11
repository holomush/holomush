<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Cross-model second opinion (codex) — verbatim + adjudication

Codex (GPT-family, via codex-rescue) was asked to pressure-test the review's severity calls, framing honesty, and blind spots. Its critique, and how I resolved each point, below.

## Codex's critique (summary of its verbatim return)

- **Fair Highs:** F2 (gateway OOM — real unauthenticated DoS, cheap to exploit + fix; "flirts with Blocker" but stays High at hobbyist scale unless publicly deployed by default). F4 (events_audit unbounded — High if it's the durable substrate). F8 (nats vuln — directionally fair; severity depends on monitor-port exposure).
- **Overstated:** F5 no-movement → "severe product incompleteness, not an architecture defect"; High only under a *product-readiness* rubric, not operational. F1 event-sourcing docs → Medium-High under "strategic integrity," not operational severity, since fix is docs-only. F7 coverage → Medium (process/governance). F6 PWA → Medium / Low-Medium. F3 DLQ split → Medium (recoverability friction, no data loss).
- **Double-counting:** F1/F5/F6/F7 + F3's tautological test are ONE theme — "assurance artifacts overstate reality." Sharper headline: **"0 Blockers, 3-4 true Highs, several Medium assurance gaps."**
- **Biggest blind spot:** the 11-agent fan-out is good at *local* defects, bad at *emergent* cross-subsystem failure — "What happens when two players act concurrently during a broker flap and one replica restarts?" Gateway retries × command dispatch × ABAC × DB commit × event emit × client state interact across ownership boundaries no single agent owns.
- **Verdict on headline:** "0 Blocker" credible; "8 High" honest as facts but severity-inflated and theme-double-counted.

## Adjudication (where I agree, where I hold)

**AGREE — adopt a dual-rubric severity presentation.** Codex is right that flattening an unauthenticated OOM and a coverage-policy gap into one "High" bucket is misleading. The report now separates:

- **Operational-High (runtime teeth):** F2, F4, F8 — the "true Highs."
- **Product-readiness High:** F5 — see below.
- **Assurance/governance theme (Medium-High as a cluster):** F1, F6, F7 + F3's tautological test → "assurance artifacts overstate reality."

**AGREE — surface the sharper headline alongside the verified count.** Both framings now appear in §1 so the report neither inflates nor buries. The 8 findings are each independently verified (facts are real); the *severity spread* across rubrics is what matters for prioritization.

**HOLD (partial disagreement) — F5 stays High, re-labeled product-readiness.** Codex calls it "incompleteness, not a defect." I hold that it is stronger than incompleteness: the guest flow actively **presents a walkable world** — clickable exit buttons, a room/exits panel — that silently no-ops. A rendered affordance that does nothing is a *broken* affordance, not merely an absent feature, and for a game the product-readiness rubric is arguably the primary one. I concede codex's narrower point: it is not an *architecture* defect. Net: High under product-readiness, explicitly labeled; not ranked beside the OOM under operational severity.

**AGREE — F3 scope already corrected** to external-NATS; codex's "recoverability friction not loss" is exactly why it's High-within-scope, not Blocker. Kept, with the no-data-loss caveat prominent.

**AGREE and ELEVATE — the concurrency blind spot.** This is the most valuable part of codex's feedback. The dimension-scoped fan-out did not exercise emergent multi-replica/broker-flap/reconnect behavior. Added to §6 limitations AND as a top recommended follow-up: a dedicated resilience/chaos pass (concurrent commands + broker partition + replica restart + reconnect). D1-M12 (world writes last-write-wins, no version guard) is the one code-level hint that this class of bug is live under the shipped two-replica deployment — it deserves a reproduction attempt the review did not perform.

## Net effect on the report

Severity re-presented under three rubrics; headline sharpened to "0 Blocker · 3 operational High (F2/F4/F8) · 1 product-readiness High (F5) · a Medium-High assurance-gap theme (F1/F3/F6/F7)"; concurrency-resilience named as the #1 methodology gap and a recommended follow-up. No finding was withdrawn — all 8 remain verified facts; only their *severity framing* changed in response to codex.
