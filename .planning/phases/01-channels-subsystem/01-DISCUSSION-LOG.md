# Phase 1: Channels Subsystem - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-08
**Phase:** 1-Channels Subsystem
**Areas discussed:** Phase-1 scope, Faction gating (CHAN-04), Message privacy/crypto, Web surface, Moderation authority, Creation policy, Retention & pruning, Payload contract

---

## Phase-1 scope line

| Option | Description | Selected |
|--------|-------------|----------|
| Core+types+history | 10.1/10.2/10.3/10.5, defer 10.4 moderation — minimal CHAN-01…05 set | |
| + ban/mute data model | Core+types+history plus ban/mute enforcement substrate, no mod commands | |
| Full spec (10.1–10.4) | All sub-phases incl. full moderation | ✓ |

**User's choice:** Full spec (10.1–10.4)
**Notes:** Chose the largest option — full Epic-10 initial scope incl. moderation. Later narrowed by the Moderation-authority decision (no op/deop). OUT only: search, rich text, bridging.

---

## Faction gating (CHAN-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Private/admin types | CHAN-04 satisfied by private+admin types, player-level enforcement | |
| True faction gating | Build per-character faction attribute ABAC now (spec's future work) | |
| Private now + seam | Private/admin types now + ChannelAttributeProvider/policy seam for later faction attribute | ✓ |

**User's choice:** Private now + seam
**Notes:** Ship enforced private/admin types (player-level) + build the provider/policy seam so a future `principal.faction` clause drops in with no migration. Character-attribute pipeline deferred.

---

## Message privacy / crypto

| Option | Description | Selected |
|--------|-------------|----------|
| Plaintext all (never) | sensitivity: never everywhere, no crypto.emits | ✓ |
| Encrypt private/admin | Mixed: public plaintext, private/admin encrypted | |
| Encrypt all (always) | Mirror scenes fully | |

**User's choice:** Plaintext all (never)
**Notes:** No crypto.emits → no crypto-reviewer gate. Coherent with admin oversight, future search, and Discord/Slack bridge parity.

---

## Web surface

| Option | Description | Selected |
|--------|-------------|----------|
| Telnet-first (no web) | Telnet/CLI channel commands only | ✓ |
| Telnet + minimal web read | Add minimal web list/feed/post surface | |
| Full web workspace | Mirror the core-scenes web workspace | |

**User's choice:** Telnet-first (no web)
**Notes:** Matches the spec's web non-goal + absent roadmap UI hint. Web channels become a spec'd follow-on (WEBPORTFWD-01).

---

## Moderation authority

| Option | Description | Selected |
|--------|-------------|----------|
| Full owner/op model | Owner moderates + appoints ops; ops moderate; admin overrides | |
| Owner moderates, no ops | Owner + admin moderate; drop op/deop delegation | ✓ |
| Staff-only moderation | Only admin moderates | |

**User's choice:** Owner moderates, no ops
**Notes:** Role model member/owner + admin; no `op` role/commands this phase. Narrows the full-spec 10.4. Reconciliation: the spec's role CHECK includes 'op' — keep dormant (lighter) or drop.

---

## Creation policy

| Option | Description | Selected |
|--------|-------------|----------|
| Admin-default + grant seam | Admin-only seed + rate limit + ABAC grant seam | ✓ |
| Open player creation | Any registered player creates, rate-limited | |
| Strictly admin, no seam | Admin-only, skip rate-limit/grant machinery | |

**User's choice:** Admin-default + grant seam
**Notes:** Admin-only by seed policy, 5/hr rate limit enforced, ABAC shaped so operators can grant creation to roles/players with no code change.

---

## Retention & pruning

| Option | Description | Selected |
|--------|-------------|----------|
| 30d default + pruning in | 30-day retention, background pruning ships | ✓ |
| 30d default, defer pruning | Retention config now, pruning job deferred | |
| 90d default + pruning in | 90-day retention, pruning ships | |

**User's choice:** 30d default + pruning in
**Notes:** 30-day default (admin extended/unlimited), background pruning job in scope (10.5 requirement; unbounded history is an ops liability).

---

## Payload contract

| Option | Description | Selected |
|--------|-------------|----------|
| Conform to CommunicationContent | channel_say/pose use comm.v1.CommunicationContent | ✓ |
| Bridge-ready payload now | Implement bespoke ChannelMessagePayload with bridge fields | |
| Extend the contract | Add channel/bridge fields to CommunicationContent itself | |

**User's choice:** Conform to CommunicationContent
**Notes:** Content events use the canonical contract (symmetric enforcement + shared rendering). Channel identity via subject/stream + live name lookup; notices use own small payloads; bridge source/author_name added in Epic 12 without precluding.

## Claude's Discretion

- `=name` / `=name :pose` / `=name ;semipose` parser wiring (reuse say/pose no_space semantics).
- Keep vs drop the dormant `op` role enum value (lightest path).
- Naming of any new channel `INV-<SCOPE>-N` invariants.

## Deferred Ideas

- op/deop delegation; true per-character faction gating; web channel surface; full-text search (0sc.8); rich text (vrzu); Discord/Slack bridging + channel_bridges table (Epic 12); eventkit/groupkit SDK extraction (follow-on to CHAN-05, INV-S7).
