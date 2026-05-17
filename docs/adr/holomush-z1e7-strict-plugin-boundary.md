<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Strict Plugin-Boundary: Plugins Must Not Modify internal/

**Date:** 2026-05-16
**Status:** Accepted
**Decision:** holomush-z1e7
**Deciders:** HoloMUSH Contributors

## Context

The HoloMUSH plugin architecture distinguishes substrate (`internal/`,
`pkg/plugin/`) from plugins (`plugins/<name>/`). Substrate changes have
dedicated review gates: `crypto-reviewer` for changes touching the
event-payload-cryptography surface, `abac-reviewer` for changes touching
`internal/access/`, `code-reviewer` for general substrate review. These
gates are designed to fire on substrate PRs.

Plugin PRs in the past have sometimes bundled substrate changes alongside
plugin-domain work — for example, a scenes plugin PR that also tweaks
`internal/eventbus/` or extends `pkg/plugin/`. When this bundling
happens:

1. The substrate review gates may not fire (they are configured to
   trigger on substrate paths, but the PR's primary classification
   might be plugin-domain).
2. Substrate evolution becomes entangled with plugin domain logic,
   making the substrate change harder to audit independently.
3. The trust boundary is blurred: substrate evolves through plugin PRs
   without dedicated substrate review attention.

This is materially different from the rule "plugins must not import
`internal/`." That rule prevents *runtime* boundary crossing. The
stricter rule here prevents *PR-level* boundary crossing — a plugin's
PR cannot reach into substrate even by editing it directly.

## Decision

A plugin PR MUST only modify files within its own plugin directory
(`plugins/<name>/`) and approved SDK packages (`pkg/plugin/*`, generated
proto). Any substrate change a plugin's needs require MUST be:

1. Identified as separate work
2. Filed as a separate bead with appropriate substrate-change context
3. Implemented in a separate PR
4. Reviewed by the appropriate substrate-review gate (`crypto-reviewer`,
   `abac-reviewer`, `code-reviewer`)

Only after the substrate change lands does the plugin PR proceed,
consuming the new substrate capability.

Bundling a substrate change inside a plugin PR is forbidden.

## Rationale

**Substrate review gates only fire on substrate PRs.** The
`crypto-reviewer` agent's triggering rules ([`.claude/agents/crypto-reviewer.md`](../../.claude/agents/crypto-reviewer.md))
specify paths under `internal/eventbus/crypto/`, `internal/eventbus/codec/`,
etc. A plugin PR that incidentally touches one of these paths might or
might not fire the gate depending on PR-classification heuristics. The
bundling pattern can bypass the gate accidentally.

**Substrate evolution must be auditable.** When a substrate change lives
inside a plugin PR, the substrate audit trail (`git log -- internal/`)
shows it as a side-effect of plugin work. Future archaeologists cannot
distinguish "we deliberately evolved substrate to enable this use" from
"a plugin author edited substrate to make their plugin compile."

**Mechanical enforcement is easy.** The check is a one-line predicate:
"does this PR's file list intersect with `internal/`, or with `pkg/plugin/`
outside the approved SDK/generated-proto allowlist?" Human `code-reviewer`
agents can apply this on every plugin PR; a future `task lint:plugin-boundary`
CI predicate is a tractable follow-up.

**Two-PR cost is acceptable.** Plugin authors who discover a substrate
gap mid-implementation must file a separate bead and pause until the
substrate work lands. This is slower than bundling, but the slowdown
is bounded (one bead file + one extra PR cycle) and the trust-model
gain is unbounded (substrate review attention is always proportional
to substrate change risk).

## Alternatives Considered

**Option A: Allow plugins to propose internal/ changes in the same PR**

| Aspect     | Assessment                                                                                                                                                                                          |
| ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Single review cycle for related changes; faster for plugin authors                                                                                                                                  |
| Weaknesses | Substrate review gates may be bypassed; substrate changes become entangled with plugin domain logic; impossible to audit substrate changes independently                                            |

**Option B: Strict separation (chosen)**

| Aspect     | Assessment                                                                                                                                                                                          |
| ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Substrate changes always go through dedicated review gates; plugin authors cannot accidentally extend substrate; file list check is trivially mechanical                                            |
| Weaknesses | Slower for plugin authors who discover substrate gap mid-implementation; requires filing a separate bead for substrate work                                                                          |

## Consequences

**Positive:**

- Substrate changes always go through dedicated review gates (`crypto-reviewer`, `abac-reviewer`, `code-reviewer`)
- Plugin PRs are scoped and reviewable independently of substrate
- Future `task lint:plugin-boundary` CI predicate can mechanically enforce
- Pairs with [`holomush-7kvy`](holomush-7kvy-direct-staticaccesscontrol-replacement.md)-style substrate-boundary discipline to keep `internal/` clean

**Negative:**

- Plugin authors must file a separate bead when they discover a substrate gap
- Two PRs instead of one for plugin + substrate work
- Slows the "discover substrate gap → fix → use" cycle by one PR boundary

**Neutral:**

- Paired with substrate domain-freeness (INV-S2): together they enforce the boundary from both sides
- Existing precedent in `feedback_plugin_boundary` user-memory and `.claude/rules/plugin-runtime-symmetry.md`

## References

- [Substrate Contract Spec — §2.0, INV-S1](../superpowers/specs/2026-05-16-social-spaces-substrate-contract.md)
- [`.claude/rules/plugin-runtime-symmetry.md`](../../.claude/rules/plugin-runtime-symmetry.md) — companion runtime invariant
- [Split Plugin SDK into eventkit and groupkit (`holomush-p7w0`)](holomush-p7w0-split-plugin-sdk-eventkit-groupkit.md)
