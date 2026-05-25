<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Two-Gate Plugin Self-Decrypt Authorization Without an ABAC Decrypt Action

**Date:** 2026-05-25
**Status:** Accepted
**Decision:** holomush-edqh1
**Deciders:** HoloMUSH Contributors

## Context

Authorizing a plugin to decrypt its **own** historical events on read-back needed a principal model. The cross-plugin decrypt capability (`crypto.consumes.requests_decryption`, INV-45) is live-delivery-only and structurally inapplicable: a self-reference fails the validator (`crypto_validator.go`), and it conflates live-delivery consume with read-back. The ABAC `decrypt` action exists in `checkPlugin` (`guard.go:129`) but **no production policy and no `dek` resource type satisfy it** — it always-denies in production today (the only grant is a test helper). The snapshot principal is fixed by architecture: it runs in the plugin's lifecycle ticker, and there is no System `IdentityKind`.

## Decision

Plugin self-decrypt is authorized by **exactly two gates**, each evaluated once (default-deny):

1. **g1 — OwnerMap subject-ownership**, enforced host-side at primitive entry (`OwnerMap.Resolve(subject).PluginName == principal.PluginName`). The sole subject-scope check.
2. **g2 — manifest `crypto.emits[].readback: true`** via a new `ManifestLookup.PluginCanReadBack`, distinct from the consumes-side `PluginRequestsDecryption`. Loader-validated; default `false` (the explicit opt-in).

There is **no ABAC `decrypt`-action gate**. Wiring one is deferred to a follow-up bead (it would also fix the latent live-delivery always-deny).

## Rationale

The ABAC `decrypt` action always-denies in production today — no `dek` resource type and no permit policy satisfy it (the only grant is a test helper) — and `requests_decryption` (INV-45) is live-delivery-only and structurally inapplicable to read-back. A 3-gate or ABAC-only model would couple this work to absent crypto-ABAC plumbing for no security gain over OwnerMap subject-ownership + the manifest `readback` opt-in + the mandatory INV-19 audit. Two concrete, default-deny, runtime-symmetric gates unblock C7 now; wiring the ABAC action (which also fixes the latent live-delivery always-deny) is a documented follow-up.

## Alternatives Considered

- **Reuse `requests_decryption` / relax the self-ref validator.** *Rejected:* conflates live-delivery with read-back; stretches INV-45 dependency semantics.
- **ABAC-only seeded grant, or a 3-gate model (manifest + ABAC + ownership).** *Rejected:* both depend on production ABAC plumbing — a `dek` resource type + a `decrypt` permit policy — that does not exist; coupling this work to absent infrastructure for no security gain over gates 1–2 + INV-19 audit.
- **Two gates (OwnerMap + manifest flag), ABAC deferred — chosen.** Concrete, verifiable, default-deny, runtime-symmetric.

## Consequences

- **Positive:** C7 is unblocked without waiting for `dek`-ABAC plumbing; default-deny preserved (a plugin without `readback: true` is denied even on its own subjects); the `ManifestLookup` extension is narrow (existing fakes gain a default-`false` method).
- **Negative:** the ABAC `decrypt` action remains unbuilt; the live-delivery `checkPlugin` ABAC call always-denies in production today — that latent gap is now documented but not fixed.
- **Neutral:** the deferral is filed as a follow-up (spec §7.5); the crypto-reviewer must evaluate it as a deliberate scope decision.

See spec §4, §7.5. Pairs with the detect-not-prevent posture (holomush-c3kyv).
