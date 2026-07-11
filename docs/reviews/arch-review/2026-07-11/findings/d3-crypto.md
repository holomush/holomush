<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# D3 Event Payload Crypto — Findings

**Agent:** crypto-reviewer/opus · **Date:** 2026-07-11 · **Scope examined:** `internal/eventbus/{publisher.go, subscriber.go}`, `internal/eventbus/crypto/{aad,dek,kek}/`, `internal/eventbus/codec/xchacha20poly1305.go`, `internal/eventbus/authguard/guard.go`, `internal/eventbus/history/{cold_postgres.go,dispatcher.go,hot_jetstream.go,readback.go,plugin_aad_adapter.go}`, `internal/eventbus/audit/{dlq.go,projection.go}`, `internal/eventbus/crypto/dek/audit_chain.go`, `internal/plugin/event_emitter.go`, `plugins/core-communication/plugin.yaml`, `cmd/holomush/{core.go,sub_grpc.go,crypto_rekey_wiring.go}`, migrations 000013/000014/000017/000038, master spec + Phase 3a/3d groundings, `evidence/open-issues.json`.

## Summary

The event-payload-crypto subsystem is the most rigorously constructed surface I reviewed. I found **no Blocker**: no plaintext-to-non-participant leak, no key or nonce reuse, and every failure mode I traced is fail-closed. Crypto primitives are correct (per-encrypt random 192-bit XChaCha20-Poly1305 nonce; deterministic hand-rolled AAD binding all cleartext fields incl. codec name, dek_ref, dek_version, and ns-precise timestamp; KEK wrap under the master key with random nonce). The read path fails closed on nil AuthGuard/DEKManager/audit-emitter, forbids plaintext delivery to a plugin without a landed audit record, and zeroes the plaintext buffer on the TOCTOU audit-queue-full fallback. Cold-tier AAD is reconstructed from the stored marshaled envelope (not the truncated column), and the ns-timestamp truncation trap is closed by migration 000038 (BIGINT-ns). The audit DLQ never-drop guarantee holds even in the `msg.Metadata()`-error edge (falls through to deliberate no-ack, retained until MaxAge). KEK is now mandatory to boot (`BOOT_KEK_REQUIRED`) and the DEK manager is wired onto the production publisher — crypto is genuinely active, not dormant.

Findings are all robustness/assurance gaps, not correctness defects: **0 Blocker, 0 High, 2 Medium (both already-tracked), 2 Low (one already-tracked).**

**Subsystem verdict: READY.** The no-plaintext-to-non-participant invariant is upheld end to end; remaining gaps are assurance/posture, not confidentiality.

## Findings

### MEDIUM-1 No end-to-end proof that a real private message is ciphertext at rest / on the bus

- **Severity:** Medium
- **Claim:** The subsystem proves encryption at the unit/component level, but there is no integration test that a *real* `core-communication` `page`/`whisper`/`pemit` emit produces `Sensitive=true` → ciphertext on the live JetStream message and in `events_audit` with crypto activated.
- **Evidence:** `plugins/core-communication/plugin.yaml:295-303` declares `page/whisper/pemit` as `sensitivity: always`; the fence at `internal/plugin/event_emitter.go:146-154` forces `sensitive = (effective == SensitivityAlways)` for both runtimes; `cmd/holomush/core.go:798-802,929` wires the DEK manager onto the publisher. But `evidence/open-issues.json` #4701 ("Integration test: real core-communication page/whisper/pemit emit Sensitive=true & encrypt with crypto enabled") is open, and existing crypto integration tests (`test/integration/crypto/`) exercise the primitive with hand-built rows, not the real plugin emit chain.
- **Impact:** A regression in the plugin→emitter→publisher chain (e.g. a subject-shape change that breaks `contextIDFromSubject`, or a manifest edit) could silently ship a private message in the clear or break it, and no test would catch the confidentiality half against the live bus. Given crypto is now the load-bearing gate for private messaging, this is the single most valuable missing proof.
- **Recommendation:** Add the tracked integration test: real plugin emit → assert `App-Codec: xchacha20poly1305-v1` + non-empty `App-Dek-Ref` on the JetStream message AND `codec != identity` + populated `dek_ref/dek_version` in `events_audit`, with a negative `nats sub`-style plaintext-absence assertion (INV-1/INV-2 at the real emit site).
- **Dedup:** already-tracked:#4701 (see also #4730 meta-test for sensitivity:always claim sites)

### MEDIUM-2 Crypto can go silently inactive despite a provisioned KEK

- **Severity:** Medium
- **Claim:** `buildRekeyWiring` returns a zero-valued wiring (nil `Manager`) with a **nil error** when a required non-KEK dependency is absent; `core.go` then sets `grpcSub.cfg.RekeyManager = nil`, flipping `cryptoActiveFor` to false even though a KEK booted successfully.
- **Evidence:** `cmd/holomush/crypto_rekey_wiring.go:123-133` (`if deps.Pool == nil || deps.KEKProvider == nil || … || deps.CoordHolder == nil { return rekeyWiring{}, nil }`); `cmd/holomush/core.go:915-929` treats only `rekeyWErr != nil` as fatal and assigns the possibly-nil `rekeyW.Manager`; `cmd/holomush/sub_grpc.go:181,188-192` (`cryptoActiveFor`/`publisherOptionsFor` key off `RekeyManager != nil`). With `RekeyManager` nil, a `Sensitive=true` emit hits `EVENTBUS_SENSITIVE_EVENT_NO_DEK_MANAGER` (`internal/eventbus/publisher.go:227-230`).
- **Impact:** Fail-closed (no plaintext leak — the emit errors), but the operator-facing effect is a silent private-messaging outage: `page/whisper/pemit`/private scene emits all error at runtime while the server booted "with crypto." Only a WARN log (`core.go:922-924`) signals the gap. Diverges from the "KEK presence is the single activation gate" intent.
- **Recommendation:** When `kekProvider != nil` but `buildRekeyWiring` yields a nil `Manager`, treat it as fatal (or a loud, alerting startup error) rather than a soft WARN — a booted KEK with a dead DEK manager is an incoherent production posture, not a degraded-but-serviceable one.
- **Dedup:** already-tracked:#4649

### LOW-1 KEK wrap omits AAD binding the wrapped DEK to its crypto_keys row identity

- **Severity:** Low
- **Claim:** `LocalAEADProvider.Wrap` seals the DEK with a nil AAD, so a wrapped-DEK blob is not cryptographically bound to its `(context_type, context_id, version)` / `crypto_keys.id`.
- **Evidence:** `internal/eventbus/crypto/kek/local_aead.go:122` (`sealed := aead.Seal(nil, nonce, dek, nil)` — final arg AAD is nil); `Unwrap` at `:151` likewise passes nil.
- **Impact:** Confidentiality is not affected — the payload AAD already binds `dek_ref`/`dek_version` (`internal/eventbus/crypto/aad/aad.go:113-114`), so swapping wrapped-DEK blobs between rows yields AEAD tag failure on payload decrypt (a DoS at most), and an attacker without the KEK cannot forge a valid wrap. This is inside the threat model's accepted "DB-write attacker is out of scope" boundary. Flagging as cheap defense-in-depth only.
- **Recommendation:** Consider binding `wrap_key_id` + the row identity into the wrap AAD in a future KEK-format version so a shuffled wrapped_dek fails at unwrap rather than at payload decode. Non-urgent.
- **Dedup:** none

### LOW-2 Meta-test for sensitivity:always emit-site claims not yet present

- **Severity:** Low
- **Claim:** No meta-test asserts that every in-tree `sensitivity: always` Lua plugin actually sets `sensitive` at its emit sites.
- **Evidence:** `evidence/open-issues.json` #4730 open. Note the server-side fence (`internal/plugin/event_emitter.go:146-154`) already forces the flag regardless of the plugin's claim, so this is belt-and-suspenders — a plugin under-claiming is corrected, not leaked.
- **Impact:** Minimal (fence is authoritative); the meta-test would catch author confusion earlier.
- **Recommendation:** Land the tracked meta-test.
- **Dedup:** already-tracked:#4730

## Strengths

- **Correct AEAD usage.** `internal/eventbus/codec/xchacha20poly1305.go:43-56` generates a fresh 24-byte random nonce per `Encode` from `crypto/rand`; 192-bit nonce space makes reuse-under-random-nonce negligible even for a busy single-DEK context. KEK wrap (`kek/local_aead.go:118-127`) does the same. No nonce or key reuse anywhere.
- **Deterministic, complete AAD binding.** `internal/eventbus/crypto/aad/aad.go:62-117` binds magic+version, id, subject, type, ns-timestamp, deterministic-marshaled actor, codec name, dek_ref, and dek_version — matching master spec §4.2 — and is the single shared function for encode and decode.
- **Fail-closed read path.** `internal/eventbus/subscriber.go:636-708` and `history/dispatcher.go:198-231`: deny → metadata-only+empty payload; nil DEKManager/audit-emitter → typed error, never panic; plugin decrypt refused unless the audit record lands; AUDIT_QUEUE_FULL fallback zeroes the plaintext buffer (`subscriber.go:697-703`) — the INV-CRYPTO-28 / TOCTOU defenses are real and tested (`decode_delivery_test.go`, `readback_test.go`).
- **AuthGuard is default-deny by construction** (`authguard/guard.go:27-39` nil-dep rejection; operator kind always denied → AdminReadStream; unknown kind denied).
- **Cold-tier byte-equality trap closed.** `history/cold_postgres.go:407-443` reconstructs the envelope via `proto.Unmarshal(row.Envelope)` and validates DEK columns (`EVENTBUS_COLD_DEK_COLUMNS_MISSING`/`_BAD_DEK_COLUMNS`); migration 000038 moved timestamps to BIGINT-ns so AAD round-trips at sub-µs resolution (`plugin_aad_reconstruction_test.go:36-99`, INV-STORE-5).
- **DLQ never-drop is genuinely never-drop.** `audit/projection.go:254-298` + `dlq.go:148-162`: capture-before-Term, no counter increment on failed publish, and the `msg.Metadata()`-error edge falls through to deliberate no-ack (retained to MaxAge) — verified against a real broker (`dlq_neverdrop_integration_test.go`).
- **Runtime symmetry upheld at the shared fence.** `internal/plugin/event_emitter.go::Emit` computes `sensitive` from the manifest for both Lua and binary; there is no runtime-specific plaintext bypass.
- **Rekey forward-secrecy plumbing.** `crypto_rekey_wiring.go`/`core.go:876-930` deliberately share cache-pointer identity between the DEK manager and the invalidation Coordinator (Phase 3c Decision 5) so a cross-replica Rekey cannot leave a replica serving stale OLD DEK material for the cache TTL. Hash-chained rekey/policy audit (`crypto/dek/audit_chain.go`) with RFC 8785 JCS canonicalization gives tamper-evidence.

## Not examined

- Vault Transit provider path (design lists it; I reviewed only the default LocalAEAD provider — the production default).
- Cluster invalidation Coordinator internals (`crypto/invalidation/`, INV-53..60) beyond confirming the synchronous N-of-N ack contract gates Rekey/Rotate; the pill/probe protocol correctness is CLUSTER-scope, out of D3.
- Rekey 7-phase orchestrator crash-resume checkpoint FSM (`rekey_*.go`) — verified it exists and is heavily tested; did not re-derive resumability.
- ABAC policy DSL for the plugin `decrypt` / `read_own_history` grants — deferred to the abac-reviewer seam.
- Semantic correctness of participant seeding for whisper/page sender-vs-recipient (tracked #4748); assessed only that it cannot leak (fail-closed), not that it is UX-complete.
