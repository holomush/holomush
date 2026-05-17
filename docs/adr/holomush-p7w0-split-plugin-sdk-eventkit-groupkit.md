<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Split Plugin SDK into eventkit and groupkit by Scope

**Date:** 2026-05-16
**Status:** Accepted
**Decision:** holomush-p7w0
**Deciders:** HoloMUSH Contributors

## Context

Four `theme:social-spaces` uses (Scenes, Channels, Forums, Discord) share
substrate primitives (JetStream + crypto + focus + ABAC + plugin host RPCs)
but differ materially in whether they have intentional membership semantics:

- **Scenes** has stateful group membership (`scene_participants` table, join/leave/kick/invite state machine, hard privacy boundary based on participant status).
- **Channels** has stateful group membership (`channel_members` table, join/leave, type-based access).
- **Forums** is a document store (boards → threads → posts). "Participation" is incidental — you wrote a post, ergo you're in the conversation. There is no membership grant, no membership state, no "thread member" entity.
- **Discord** is a bridge plugin. It mirrors Discord's groups via OAuth-linked accounts; it has no local membership state. The local state is OAuth links + presence sync.

A single unified SDK ("groupkit covering everything shared") would either force
group-domain abstractions onto Forums and Discord (creating phantom
abstractions over fundamentally different shapes) or be diluted to a
lowest-common-denominator API that does not actually capture either pattern
cleanly.

## Decision

Split the plugin-side shared library into two SDKs by scope:

- **`pkg/plugin/eventkit/`** — broadly-useful primitives that compose
  substrate without group-specific state: `replay` (ABAC-filtered history
  fan-out with sensitivity gate) and `cryptoemit` (call-site sensitivity
  assertion).
- **`pkg/plugin/groupkit/`** — stateful-group primitives that require
  explicit member-of-entity state: `membership` (typed wrapper over plugin's
  own participants table), `focuswire` (group-scoped focus subscription
  wiring), and `groupabac` (member-of-resource attribute resolver fragment).

Uses with no intentional membership consume **eventkit** only or nothing.
Forums uses eventkit (replay for paginated thread display, cryptoemit for
private-board posts). Discord defaults to no SDK; eventkit/replay adoption
is permitted conditionally if cross-history sync requires ABAC-filtered
replay.

## Rationale

**Forums shape rejection.** Forums participation is *incidental* (wrote a
post), not *intentional* (granted membership). `groupkit/membership` requires
an explicit member-of-entity state with role vocabulary and join/leave
mutations. Forcing forums into that shape would invent state that does not
exist in the domain.

**Discord shape rejection.** Discord is a bridge between two systems'
notions of "channel membership"; the bridge plugin owns neither side's
membership state. `groupkit/focuswire` has no analog (Discord users do not
focus a HoloMUSH connection on a Discord channel); `groupkit/groupabac` has
no analog (authorization is at the OAuth-link granularity, not the
"thread member" granularity).

**Self-enforcing boundary.** The split makes the import graph self-documenting.
A Forums plugin PR importing `pkg/plugin/groupkit/` is immediately visible
as an invariant violation (INV-S10) — no policy text required to catch it;
the build surface tells the truth.

**Independent N=2 validation.** Each SDK has its own validation cycle
(INV-S7). eventkit's N=2 is met by Scenes + Channels; groupkit's is also
Scenes + Channels. Forums later adopting eventkit only extends eventkit's
validation, never groupkit's. Future non-group uses (announcements, alerts,
audit-streaming plugins) get eventkit without inheriting irrelevant
groupkit concepts.

## Alternatives Considered

**Option A: Unified `groupkit` covering all shared primitives**

| Aspect     | Assessment                                                                                                                                                                                        |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Single import path; simpler discoverability; one N=2 validation cycle                                                                                                                             |
| Weaknesses | Forces group-domain concepts on Forums and Discord; INV-S10 violation would be policy-text-only, not catchable by import-graph inspection; name "groupkit" misleads when the contents are mixed |

**Option B: eventkit + groupkit split by who can consume (chosen)**

| Aspect     | Assessment                                                                                                                                                                              |
| ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Forums adopts eventkit cleanly without pretending to be group-shaped; groupkit's stateful primitives only available to uses that own membership tables; import graph self-documenting |
| Weaknesses | Two N=2 validation cycles; contributors must understand the distinction                                                                                                                 |

**Option C: No SDK — each use implements primitives inline**

| Aspect     | Assessment                                                                                                                                                  |
| ---------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | No shared abstraction risk; zero N=1 extraction pressure                                                                                                    |
| Weaknesses | Scenes and Channels duplicate membership ops, focuswire wiring, ABAC attribute population; 4+ divergent implementations of the same shape                   |

## Consequences

**Positive:**

- INV-S10 is mechanically enforceable via import-graph inspection on Forums and Discord PRs
- eventkit is reusable by future non-group uses (announcement streams, audit-tailing plugins, etc.)
- groupkit's surface area stays narrow; no accidental generalization to forum/discord shapes
- The boundary survives future refactoring: a future contributor who thinks "forums could just use membership" hits the import-graph as a hard signal

**Negative:**

- Two SDK validation cycles instead of one (paid once during channels rework brainstorm)
- Contributors must learn the eventkit/groupkit distinction before starting a new use plugin (documented in SDK READMEs)

**Neutral:**

- Both SDKs live under `pkg/plugin/` (plugin-code-level, not substrate)
- Neither SDK lands code until N=2 discipline is met (decision [`holomush-lrt3`](holomush-lrt3-n2-consumer-validation-sdk-extraction.md))
- Forums' eventkit adoption is post-channels-validation (eventkit's N=2 must be met first)

## References

- [Substrate Contract Spec — §3.1, §2.4, INV-S10](../superpowers/specs/2026-05-16-social-spaces-substrate-contract.md)
- [N=2 Consumer Validation Before SDK Primitive Extraction (`holomush-lrt3`)](holomush-lrt3-n2-consumer-validation-sdk-extraction.md)
- [Strict Plugin-Boundary (`holomush-z1e7`)](holomush-z1e7-strict-plugin-boundary.md)
