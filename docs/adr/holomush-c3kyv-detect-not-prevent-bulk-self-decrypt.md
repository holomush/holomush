<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Detect-Not-Prevent Posture for Plugin Bulk Historical Self-Decrypt

**Date:** 2026-05-25
**Status:** Accepted
**Decision:** holomush-c3kyv
**Deciders:** HoloMUSH Contributors

## Context

A plugin with the `readback` capability can, in principle, decrypt **all** of its own historical sensitive events — not just those relevant to the current operation. The only property encryption-at-rest buys *against the owning plugin itself* is temporal: a plugin compromised at time T cannot retroactively recover plaintext for events emitted before T. A broad read-back capability erodes that forward-secrecy-under-compromise property. Genuine prevention would require conditioning the decrypt on domain state (e.g. "only when the scene is COOLOFF with all-yes votes"), coupling scene-domain state into the trusted authorization layer.

## Decision

The security posture for plugin bulk historical self-decrypt is **detect, not prevent**. Bulk access is **bounded** (OwnerMap subject-scope confines it to data the plugin authored; a 500-row `maxDecryptBatch` cap per call; the operator's rekey/DEK-destruction lever revokes unconditionally via INV-P7-15) and **made loud** (a mandatory INV-19 `plugin_decrypt` audit event per decrypt, on a subject the plugin cannot subscribe to; the primitive fails closed if the audit emitter is absent). It is **not prevented** via contextual domain-state conditioning.

## Options Considered

- **Prevent — contextual/consent-gated ABAC** (decrypt permitted only under publish-state attributes). *Rejected:* forces scene-domain state (publish state, vote state) into the trusted ABAC gate — disproportionate complexity for a low-probability concern, given the plugin already sees plaintext at emit time.
- **Detect — subject-scoping + mandatory INV-19 audit + batch cap + DEK-destroy lever — chosen.** Loud, scoped, reversible.

## Consequences

- **Positive:** no coupling of scene-domain state into the crypto authorization layer; the operator retains DEK destruction as a hard revocation; the per-decrypt INV-19 audit trail is load-bearing for forensic detection.
- **Negative:** a compromised plugin can bulk-read its own historical content (bounded to its subjects, audited, but not prevented); realizing the detection value requires operators to monitor audit streams.
- **Neutral:** the 500-row `maxDecryptBatch` cap (distinct from `QueryStreamHistory`'s silent clamp) makes bulk reads multi-hop and auditable at each hop.

See spec §7.1, §7.2. Pairs with the two-gate authz model (holomush-edqh1).
