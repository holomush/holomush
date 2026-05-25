<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# PluginDowngradeFence Decrypts Clean Rows for Authorized Routed Readers

**Date:** 2026-05-25
**Status:** Accepted
**Decision:** holomush-wfh42
**Deciders:** HoloMUSH Contributors

## Context

The Phase-7 `PluginDowngradeFence` gated plugin-owned history reads (INV-P7-7 downgrade refusal + INV-P7-15 DEK-existence) and passed **clean rows through as ciphertext**. This was a half-built contract: the fence enforced the structural invariants but delivered no usable plaintext to authorized readers, so participant scene-IC scrollback and comms `whisper`/`page` history were silently broken. The gap was masked by tests running against a `fakeHistoryReader` (`query_stream_history_test.go`), which never exercises the codec/DEK/fence stack. Scene-IC is *armed*: it enters the backfill set via the focus substrate (`subscription_router.go`), so the first scene participant who reconnects gets ciphertext/refused.

## Decision

The fence's clean-row path now **decrypts** via the shared `decryptPluginRow` primitive instead of passing ciphertext through. The reading principal is the character, authorized by the existing **DEK participant-set membership** check (`guard.go:64`). INV-P7-7/INV-P7-15 gate behavior is preserved by extracting the per-row check into a shared `fenceCheckRow` function used by both the fence and the snapshot direct entry.

## Options Considered

- **Keep the fence gate-only; add a separate decrypt tier.** *Rejected:* duplicates routing logic and still fails to deliver plaintext on the path participants already use.
- **Complete the fence — clean rows decrypt via the shared primitive — chosen.** A single primitive serves both the snapshot direct entry and the routed fence, so decrypt logic cannot diverge; fence parity (INV-RB-5) holds by construction.

## Consequences

- **Positive:** participant scene-IC scrollback and comms `whisper`/`page`/`pemit` read-back are fixed with no per-plugin special-casing (INV-RB-11); the fake-reader coverage gap is closed by real-stack integration tests (INV-RB-7).
- **Negative:** all existing fence tests asserting ciphertext-passthrough must migrate to plaintext/refusal assertions (a deliberate migration, not a regression); the fence now requires the read-back deps + the caller identity threaded from `HistoryQuery.Caller`.
- **Neutral:** the fence-contract change is PR-blocking documentation for contributors (spec §11).

See spec §3.2, §3.4. Realizes INV-RB-5, INV-RB-7, INV-RB-11.
