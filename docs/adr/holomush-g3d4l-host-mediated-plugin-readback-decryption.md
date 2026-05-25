<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Host-Mediated Plugin Read-Back Decryption (Plugins Never Hold DEKs)

**Date:** 2026-05-25
**Status:** Accepted
**Decision:** holomush-g3d4l
**Deciders:** HoloMUSH Contributors

## Context

Plugin-owned `sensitivity:always` events (scene IC `scene_pose`/`scene_say`/`scene_emit`; comms `whisper`/`page`/`pemit`) are encrypted by the host at emit time; plugins never hold a DEK. No path existed to decrypt these payloads on a history read-back, which made the C7 scene-publish snapshot unbuildable as written (its premise "the plugin already holds DEK access" was false). Two consumers need plaintext on read-back — the snapshot (plugin-initiated, already holds its `scene_log` rows from an in-tx read) and participant web reconnect-backfill — but via structurally different read paths.

## Decision

All plugin read-back decryption is **host-mediated** via a single shared primitive (`fenceCheckRow` → `AuditRowToEvent` → `decodeAuthorizeAndDispatch`). The plugin passes ciphertext rows and receives only plaintext (or typed refusals); a DEK never crosses the plugin boundary. The **snapshot** uses a direct `PluginHostService.DecryptOwnAuditRows` RPC (it already holds its rows); **participant routed reads** use the same primitive wired into the `PluginDowngradeFence` clean-row path.

## Options Considered

- **Route the snapshot through host `QueryHistory` (bead Option A).** Reuses the existing read path, no new RPC. *Rejected:* creates a `plugin→host→plugin→host→plugin` self-loop — the plugin already holds its rows from the in-tx read, making the round-trip incoherent.
- **Grant the plugin DEK access for its own events (bead Option C).** Plugin decrypts locally. *Rejected:* violates the role-isolation invariant that defines the plugin trust boundary (plugins never hold DEKs); conflicts with INV-P7-7/INV-P7-15.
- **Host-side decrypt primitive consumed via a direct entry (bead Option B — chosen).** Host rebuilds AAD, resolves the DEK, decrypts, returns plaintext. One hop for the snapshot, no self-loop; the same primitive serves both consumers.

## Consequences

- **Positive:** plugin DEK isolation (a core security invariant, INV-RB-1) is preserved unconditionally; both consumers reuse identical decrypt logic, so a fix or crypto upgrade applies to both by construction (AAD parity INV-RB-4, fence parity INV-RB-5).
- **Negative:** a new host RPC (`DecryptOwnAuditRows`) plus its Lua hostfunc must ship together (Go+Lua parity); the fence contract changes (see holomush-wfh42).
- **Neutral:** the primitive is subject-agnostic — core-communication and core-scenes share it with no per-plugin special-casing (INV-RB-11).

See spec `docs/superpowers/specs/2026-05-25-plugin-readback-decrypt-design.md` §1, §3.
