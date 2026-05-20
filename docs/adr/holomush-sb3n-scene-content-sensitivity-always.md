<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Classify All Scene Content Events as sensitivity:always, Including OOC

**Date:** 2026-05-19
**Status:** Accepted
**Decision:** holomush-sb3n
**Deciders:** HoloMUSH Contributors

## Context

Scenes Phase 4 (`holomush-5rh.13`) declares 8 plugin-owned scene event types in `crypto.emits`. Four carry RP content (`scene_pose`, `scene_say`, `scene_emit`, `scene_ooc`); four are notice events with metadata-only payloads (`scene_join_ic`, `scene_leave_ic`, `scene_pose_order_changed_ic`, `scene_idle_nudge`).

Each event type requires a `sensitivity` classification from the enum at `internal/plugin/crypto_manifest.go:14-21`: `always` (every emit Sensitive=true; AEAD encryption applied), `may` (caller-controlled per emit), or `never` (Sensitive=true emits rejected at the fence).

The core-communication plugin (`plugins/core-communication/plugin.yaml:273-298`) is the precedent for an 8-type matrix:

| Type | Sensitivity | Privacy boundary |
|------|-------------|------------------|
| `say` | `never` | Location (public to all in room) |
| `pose` | `never` | Location |
| `ooc` | `never` | Location |
| `emit` | `never` | Location |
| `whisper_notice` | `never` | Location (no content) |
| `page` | `always` | Participants (between two characters) |
| `whisper` | `always` | Participants (between two characters in same location) |
| `pemit` | `always` | Participants (storyteller to one character) |

The pattern: privacy boundary determines sensitivity. Location-bounded events are public to everyone present → `never`. Participant-bounded events are private to specific characters → `always`. Notice events with no content payload → `never`.

For scenes, INV-S9 ("scene privacy is absolute"; iwzt §3 "no ABAC override") makes the **participant list** the privacy boundary for all scene content. This is the same boundary that drives `whisper`/`page`'s `always` classification — but `scene_say` is verbally analogous to core-communication's `say` (verbal speech in a shared space). The naming similarity creates a temptation to classify `scene_say` as `never` (matching `say`); INV-S9 makes that wrong — scene `say` is participant-only, not location-public.

A complicating consideration arose for `scene_ooc`. Scenes v2 §3.1 specifies that OOC content is "never archived in the published log." Ephemeral OOC chatter is participant-only by INV-S9 but is not subject to long-term confidentiality concerns. This raised whether `sensitivity: may` (caller-controlled) is appropriate for OOC — letting the caller skip encryption for low-stakes chatter.

## Decision

All 4 scene content events (`scene_pose`, `scene_say`, `scene_emit`, `scene_ooc`) are classified `sensitivity: always`. The 4 notice events are classified `sensitivity: never`.

No event in Phase 4 is classified `sensitivity: may`.

The `always` classification commits the 4 content event types to encryption for the lifetime of the spec. Any future reclassification (e.g., to `may`) requires a deliberate manifest migration with `crypto-reviewer` gate.

## Rationale

**INV-S9 is the privacy-boundary contract.** iwzt §3 commits "scene privacy is absolute" with no ABAC override for `scene:<id>:ic` and `scene:<id>:ooc`. The participant list is the unconditional privacy boundary for all scene content — there is no carve-out for OOC. Classifying any content event as `never` would contradict INV-S9.

**`sensitivity: may` has a known fence gap.** Phase 7 INV-P7-7's manifest-set downgrade fence covers only `always`-classified types. Calling a `may`-classified event with `Sensitive=true` is a runtime decision that the substrate fence cannot pre-decrypt-fence-check. The substrate guarantee for `always` is strictly stronger than for `may`.

**The `holomush-mjy3` footgun applies to all content.** ADR `holomush-mjy3` (not yet filed at decision-bead time but recorded as the `object_examine` sensitivity reconsideration) established the principle: classifying a payload-carrying event type as `never` blocks future encryption. The reverse footgun for `may` is the absence of substrate guarantee strength. `always` is the conservative choice when content privacy is load-bearing.

**Notice events carry no RP content.** `scene_join_ic`, `scene_leave_ic`, `scene_pose_order_changed_ic`, `scene_idle_nudge` payloads carry only metadata (character IDs, names, mode strings, durations). The `whisper_notice` precedent in core-communication classifies a similar "X did something, no content" notice as `never`. Encryption overhead on metadata is unnecessary; visibility to all participants in OOC stream is the intended posture.

**Encryption overhead on ephemeral OOC is acceptable.** Modern AEAD encryption costs microseconds per event; the participant-protection benefit dominates. The "ephemeral OOC chatter" framing is a UX consideration, not a security architecture concern. A future revisit could reclassify if a real performance problem emerges — but the migration cost (deliberate manifest change + reviewer gate) is the correct backstop.

## Alternatives Considered

**Option A: `sensitivity: always` for all 4 content events (chosen).**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | Privacy boundary is the participant list for all scene content, analogous to whisper/page; Phase 7 INV-P7-7 manifest-set downgrade fence covers all 4 types; unconditional substrate guarantee; no spec-level inconsistency with iwzt §3 |
| Weaknesses | Encryption overhead on ephemeral OOC chatter that participants often consider low-sensitivity |

**Option B: `sensitivity: may` for `scene_ooc`, `sensitivity: always` for the other 3.**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | Caller flexibility for ephemeral OOC; avoids encryption cost for low-stakes exchanges |
| Weaknesses | Phase 7 fence gap (may not covered by manifest-set downgrade fence); iwzt §3 admits no OOC carve-out; classification inconsistency within the same participant-bounded boundary; future audit risk that someone observes "OOC is may, why isn't IC?" and lowers IC sensitivity |

**Option C: `sensitivity: never` for `scene_ooc` (matching core-communication's `ooc` classification literally).**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | Verbal analogy with core-communication's `ooc`; no encryption overhead |
| Weaknesses | Contradicts INV-S9 (OOC in scene is participant-only, not location-public); the holomush-mjy3 footgun applies — `never` locks out future encryption without manifest migration; INV-P7-7 fence rejects Sensitive=true emits; a future requirement to encrypt OOC would require both a manifest change AND code change |

## Consequences

**Positive:**

- Uniform unconditional encryption for all scene content; no special-case at the emit gate or audit handler.
- INV-P7-7 manifest-set downgrade fence covers all 4 content types without exception.
- Spec-level consistency with iwzt §3 "scene privacy is absolute"; no carve-outs to document or audit.
- The classification matches the verb-level sensitivity precedent established by core-communication (privacy boundary determines sensitivity; the privacy boundary is the participant list for scenes).

**Negative:**

- AEAD encryption overhead on every OOC emit, regardless of how ephemeral the chatter is. Microseconds per event but multiplied across many participants in active scenes.
- Future reclassification of any content event requires a manifest migration with `crypto-reviewer` gate, not a simple code change. This raises the bar for evolution — defensible since the privacy boundary is load-bearing.

**Neutral:**

- The 4 notice events remain `sensitivity: never` — they carry only metadata (IDs, names, mode strings, durations), analogous to core-communication's `whisper_notice`.
- The decision establishes the principle that "verbal analogy" with core-communication is NOT a sufficient basis for scene-event classification — INV-S9 and the participant-list privacy boundary are the load-bearing inputs.

## References

- [Scenes Phase 4 Design](../superpowers/specs/2026-05-19-scenes-phase-4-streams-and-pose-order-design.md) §2
- [Substrate Contract Spec](../superpowers/specs/2026-05-16-social-spaces-substrate-contract.md) §1.2, INV-S9
- [History Scope Privacy Design](../superpowers/specs/2026-05-17-history-scope-privacy-design.md) §3 (scene privacy absolute)
- [Phase 7 Plugin SDK Design](../superpowers/specs/2026-05-13-event-payload-crypto-phase7-plugin-sdk-design.md) (INV-P7-7 manifest-set downgrade fence covers `always`-only)
- [Master Event-Payload Crypto Design](../superpowers/specs/2026-04-25-event-payload-crypto-design.md) (sensitivity enum + fence semantics)
- [core-communication crypto.emits matrix](../../plugins/core-communication/plugin.yaml) (precedent: privacy boundary determines sensitivity)
- Bead: `holomush-mjy3` (sensitivity:never footgun reconsideration)
- Bead: `holomush-5rh.13` (Phase 4 design)
