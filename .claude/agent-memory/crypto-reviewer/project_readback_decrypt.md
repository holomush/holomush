---
name: project-readback-decrypt
description: Host-mediated plugin read-back decrypt architecture (epic m7pxs) — AAD canonical input set, the single shared primitive, and the now-eliminated legacy_id field
metadata:
  type: project
---

Host-mediated read-back decryption (epic `holomush-m7pxs`, design `docs/superpowers/specs/2026-05-25-plugin-readback-decrypt-design.md`) lets a plugin decrypt its OWN sensitive audit rows without ever holding a DEK.

**Why:** plugin-owned `sensitivity:always` subjects (scene IC, comms whisper/page/pemit) are encrypted at rest in plugin audit tables; no read-back decrypt path existed. Two consumers: scene-publish snapshot (C7, direct `DecryptOwnAuditRows` RPC) and participant backfill (routed via fence clean-row path).

**How to apply when reviewing this surface:**

- The single host-side primitive is `decryptPluginRow` in `internal/eventbus/history/readback.go`. It MUST delegate to `decodeAuthorizeAndDispatch` (`dispatcher.go`) — never reimplement AAD/decode. Plugins MUST import no `eventbus/codec`/`crypto`/`dek`.
- **AAD canonical input set** (pinned at `internal/eventbus/crypto/aad/aad.go` Build): magic, event.Id, Subject, Type, Timestamp(UnixNano), Actor(deterministic proto marshal), codecName, dekRef(uint64), dekVersion(uint32). The host rebuilds the Event via `AuditRowToEvent` (`plugin_aad_adapter.go`) which copies exactly Id/Subject/Type/Timestamp/Actor; codec/dekRef/dekVersion are scalar args. SchemaVer and Payload are NOT in AAD.
- **Actor proto has only kind(1) + id(2).** Field 3 `legacy_id` was REMOVED and reserved (epic `holomush-w9ml` landed). So `actorProtoFromRow(kind, id)` reconstructs the COMPLETE actor — no dropped fields, byte-equal marshal. The old "legacy_id must round-trip through cold tier" AAD checklist item is RETIRED.
- **Subject AAD safety pattern (C7 snapshot):** `logRowToAuditRow` sets `Subject = <fullICSubject passed to runSnapshot>`, NOT the row's stored subject column. This is safe because `ReadSceneLogForSnapshot` filters `WHERE subject = $1` with the same value — every returned row's stored subject IS that value by construction. Production IC subject is `dotStyleSceneSubjectIC` = `events.<game>.scene.<id>.ic` (with `.ic`).
- **Batch cap:** host `maxDecryptBatch = 500` in readback.go; over-cap is REJECTED (DECRYPT_BATCH_TOO_LARGE), not clamped. Consumers chunk at ≤500.
- **RowResult contract:** proto oneof, exactly one arm. Host `reasonToWire` never maps a refusal to empty string, so consumer's `GetNoPlaintextReason() != ""` cleanly distinguishes refusal from success. Host always returns 1:1 in order; top-level error only on over-cap.
- **Test fidelity note:** the C7 happy-path integration test (`plugins/core-scenes/publish_snapshot_integration_test.go`) seeds via `intent.Subject = "scene:"+id` which subjectxlate-translates to `events.<game>.scene.<id>` (NO `.ic`, no facet token in legacy form). AAD still matches because seed + runSnapshot use the identical string. It does NOT exercise the production `.ic` subject — acceptable since the subject is consistent on both sides and production correctness depends on the (E5) caller passing the right `.ic` subject.
- **Detect-not-prevent posture (§7.1):** a readback-capable plugin can decrypt all its own historical sensitive events; mitigations are subject-scoping + mandatory INV-19 audit + 500-row cap + operator rekey. ABAC `decrypt`-action gate is deliberately deferred (§7.5). Evaluate omissions as deliberate, not oversights.
