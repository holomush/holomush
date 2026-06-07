<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-pc3bg; do not edit manually; use `/adr update holomush-pc3bg` -->

# Scene Log Reads Ride QueryStreamHistory; No Plugin-Side ReadSceneLog

**Date:** 2026-06-07
**Status:** Accepted
**Decision:** holomush-pc3bg
**Deciders:** Sean Brandt (seanb4t)

## Context

The web portal must display scene logs (live scrollback + ended-scene viewer). The initial design proposed a new plugin-side ReadSceneLog RPC querying scene_log directly. Plan grounding found scene_log payloads are encrypted at rest (sensitivity:always; per-event DEK columns since migration 000005), and INV-47 forbids the plugin from decrypting outside the host seam.

## Decision

Scene log reads route through existing host history machinery: live/scrollback via WebQueryStreamHistory → QueryStreamHistory (hard FocusMembership gate I-17, host-side decryption, ULID-cursor paging); ended-scene structured reads via the new ExportSceneLog(format=jsonl), rendered through the snapshotDecryptor host seam (DecryptOwnAuditRows, batch 500). No plugin-side ReadSceneLog RPC exists.

## Rationale

- A plugin-side reader returns ciphertext — non-functional at the plugin boundary (INV-47).
- QueryStreamHistory's membership gate already enforces participant-only reads, and observers pass it because JoinFocus grants a FocusMembership.
- Reusing ULID-cursor paging and host decryption avoids any new trust surface on the read path.

## Alternatives Considered

**Plugin-side ReadSceneLog RPC** — rejected: would either return ciphertext or require moving decryption into the plugin (violating INV-47) or a new host→plugin decrypt callback.

**History-machinery reuse + jsonl export (chosen)** — zero new read trust surface; client parses jsonl for the ended-scene viewer using the same pose-card components.

## Consequences

- Positive: INV-47 structurally upheld; crypto-reviewer scope limited to the existing seam; one rendering pipeline for viewer and download.
- Negative: ended-scene viewer consumes jsonl (whole-document), not a typed proto stream; live reads must use QueryStreamHistory.
- Neutral: marked/dompurify removed from the design — no markdown parsing in the viewer; markdown is export-only.

Spec: 2026-06-07-web-portal-scenes-design.md §3 D8, §9 V6. Bead: holomush-5rh.8.
