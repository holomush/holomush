<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Event Payload Cryptography — Phase 3a Substrate Grounding

## Status

**APPROVED** — resolves four open design questions deferred by Phase 2 to Phase 3a. Modifies the master spec inline at the sections cited below. Companion document, not a replacement: the master spec at [`2026-04-25-event-payload-crypto-design.md`](2026-04-25-event-payload-crypto-design.md) remains authoritative for everything else.

## Authors

- Sean Brandt
- Claude (collaborator)

## Date

2026-05-02

## Context

Master spec dated 2026-04-25 declared the emit-side flow (§3 architecture, §5 layered interfaces) but left four substrate-level seams unspecified. Phase 2 (`holomush-8qri`) shipped `internal/eventbus/crypto/{aad,dek,kek}` with explicit deferral comments — `internal/eventbus/crypto/aad/aad.go:10-11` says *"Phase 3 codecs call it from Encode and Decode"* without specifying how the codec receives AAD; `pkg/plugin/event.go:114` keeps `EmitIntent.Payload string` without saying how plugins express per-event sensitivity. An attempt to write a Phase 3a plan without resolving these seams produced six fictional API references; the plan-reviewer caught it.

This doc resolves each seam against the actual Phase 1 + Phase 2 surface, edits the master spec where wording becomes inconsistent, and lists the bead updates needed.

---

## Decision 1 — Codec receives AAD via explicit parameter

**Decision:** Extend `codec.Codec.Encode/Decode` signatures with an explicit `aad []byte` parameter:

```go
type Codec interface {
    Encode(ctx context.Context, plaintext []byte, key Key, aad []byte) ([]byte, error)
    Decode(ctx context.Context, ciphertext []byte, key Key, aad []byte) ([]byte, error)
}
```

`IdentityCodec` ignores `aad`. Sensitive codecs (`xchacha20poly1305-v1`, future `aes-gcm-v1`) pass `aad` straight to `aead.Seal/Open` as the additional-data argument. The caller (Phase 3a's emit path) builds `aad` via `internal/eventbus/crypto/aad.Build(...)` before calling `Encode`.

**Why over alternatives:**

- *Extending `codec.Key` to carry AAD bytes:* `codec.Key` is per-DEK identity (`KeyID + Version + Bytes`); AAD is per-event. Bundling per-event data into a per-DEK type bakes a lifetime mismatch into the substrate that surfaces as cache bugs once Phase 3c (DEK cache) lands — a cached `Key` would carry stale AAD for a different event.
- *Passing AAD via `context.Context` value:* opaque, untyped, error-prone. Crypto APIs in the Go standard library (`crypto/cipher.AEAD.Seal/Open`) name AAD as an explicit parameter; the convention exists for the same lifetime reason.
- *Codec internally calls `aad.Build`:* Phase 2's docstring suggested this, but it requires the codec to receive the proto `*Event` via some channel. That couples the codec interface to the proto layer and makes the codec impossible to test without proto fixtures. The caller is the right boundary.

**Decode-side AAD convention for Phase 3a.** The interface change is symmetric (Decode also gains `aad []byte`) but Phase 3a's only sensitive *Encode* path is the emit side. The fan-out *Decode* path is Phase 3b. So in Phase 3a the production Decode callsites (`subscriber.go:412`, `history/hot_jetstream.go:379`, `audit/plugin_consumer.go:354`) all receive `IdentityCodec`-only events and pass `aad = nil`. `IdentityCodec.Decode` ignores `aad`, so this is a no-op.

The single exception: Phase 3a's integration test (T9) round-trips a sensitive event through `xchacha20poly1305-v1` to verify INV-25 (AAD tamper detection) end-to-end. The test reconstructs AAD by calling `aad.Build(event, codecName, dekRef, dekVersion)` against the same proto Event it just published — the bus message and the audit row carry the same byte-equal payload (INV-21), and headers carry codec/dek_ref/dek_version, so the test has every input it needs. Production Phase 3a never calls Decode against a sensitive payload because no production subscriber path exists yet for sensitive events.

This `nil`-AAD convention for non-Phase-3a-aware callers ends in Phase 3b, when AuthGuard + decrypt-on-fanout lands. Phase 3b's plan-writer adds `aad.Build(...)` calls to the production Decode sites at the same time it makes them sensitive-aware. The grounding doc here scopes only Phase 3a.

**Spec change:** master spec §5.1 line 794-808 currently says *"`codec.Codec.Encode/Decode(plaintext, codec.Key)` — substrate type, unchanged interface"*. Edited to drop "unchanged" and reflect the new 4-arg signature. The DRAFT spec status makes this edit honest.

**Cost:** the interface change ripples to every implementer and every caller. Verified callsite + impl surface (via `rg`):

**Codec implementations** (each gains the new `aad []byte` parameter, even if ignored):

- `internal/eventbus/codec/codec.go:76,81` — `IdentityCodec.Encode/Decode` (ignores `aad`)
- `internal/eventbus/codec/registry_test.go:66,70` — `stubCodec.Encode/Decode` (test fixture)
- `internal/eventbus/publisher_test.go:62,66` — `errCodec.Encode/Decode` (test fixture)
- `internal/eventbus/codec/xchacha20poly1305.go` (NEW) — passes `aad` to `aead.Seal/Open`

**Production Encode callsites:** `internal/eventbus/publisher.go:200`.

**Production Decode callsites:** `internal/eventbus/subscriber.go:412`, `internal/eventbus/history/hot_jetstream.go:379`, `internal/eventbus/audit/plugin_consumer.go:354`.

**Test callsites:** `internal/eventbus/codec/codec_test.go:19,22,29,31` (4 calls).

No external implementers (test stubs are internal). Phase 3a-aware callers (the new emit path in `internal/plugin/event_emitter.go`) supply `aad` from `aad.Build(...)`. Pre-Phase-3a-aware callers (the listed production Encode/Decode sites) supply `nil` until they migrate; `IdentityCodec` ignores `nil`, and Phase 3a's `Crypto.Enabled=false` default keeps sensitive codecs off the production hot path.

## Decision 2 — Per-emit sensitivity: `Sensitive bool` on `EmitIntent`; hard-reject for over-claim

**Decision:** Add `Sensitive bool` to `pkg/plugin/event.go::EmitIntent`. Default `false`. Host evaluates effective sensitivity per the truth table below; over-claim (plugin sets `Sensitive=true` on a `manifest=never` event type) hard-rejects with `oops.Code("EVENT_SENSITIVITY_NOT_DECLARED")`.

```text
manifest=never  + Sensitive=false → effective=never (publish plaintext)
manifest=never  + Sensitive=true  → REJECT (INV-6)
manifest=may    + Sensitive=false → effective=never (publish plaintext)
manifest=may    + Sensitive=true  → effective=always (encrypt)
manifest=always + Sensitive=false → REJECT (INV-7)
manifest=always + Sensitive=true  → effective=always (encrypt)
```

**Why over alternatives:**

- *No SDK field; manifest is the sole source of truth:* breaks `manifest=may` types — there's no per-event signal to distinguish "this whisper is private" from "this whisper is open RP". `may` exists exactly because some types are sometimes-sensitive at the emit-site's discretion.
- *Tri-state field on the intent:* invents a fourth state (`unspecified`) that interacts with manifest in undefined ways. The plugin only ever expresses yes/no; the manifest's three states (`never`/`may`/`always`) live on the type, not the instance. Tri-state on the intent is shaped wrong.
- *Silent downgrade for over-claim:* tolerant of manifest/code drift but encourages drift. Hard reject is symmetric with INV-7's hard reject for under-claim on `always` types. The manifest is the contract; both directions of violation are loud.

**Phase 1 already named this** — `internal/plugin/crypto_manifest.go:10-11`: *"`SensitivityMay`: the emit-site decides per-event via the `Sensitive` flag. The plugin's emit code carries the runtime decision."* This decision adds the field Phase 1 promised.

**Spec change:** none — INV-5, INV-6, INV-7 in master spec §2 already describe this contract correctly. Master spec §5.1 (line 785) shows `EventSink.Emit(event, Sensitive=true, ContextID)` in the emit-flow diagram; this decision adds the SDK-side field (`EmitIntent.Sensitive`) that feeds that parameter at the gRPC boundary.

**Cost:** one field on `pkg/plugin/event.go::EmitIntent`. Additive — unaware plugins keep `Sensitive=false` (zero value) and behave exactly as before. The new field is consulted only when the Phase 3a `Crypto.Enabled` feature flag is true; until then the field is read but the fence path is bypassed.

## Decision 3 — DEK version surfaces via `codec.Key.Version`

**Decision:** Add a `Version uint32` field to `codec.Key`:

```go
// internal/eventbus/codec/codec.go
type Key struct {
    ID      KeyID
    Version uint32  // NEW — completes the (id, version) DEK identity
    Bytes   []byte
}
```

`dek.Manager.GetOrCreate(ctx, ctxID, initial)` populates `Version` from the `crypto_keys.version` column it already reads (`internal/eventbus/crypto/dek/manager.go:117` writes `Version: 1` when minting; `selectActive` returns the active row whose version is whatever Phase 4 rotation has incremented to). `IdentityCodec` callers pass `codec.Key{}` zero-value and get `Version=0`, which is correct for plaintext events.

**Companion edit required.** `dek.Material.AsCodecKey` is the only egress point that constructs a `codec.Key` from a DEK. Today (`internal/eventbus/crypto/dek/material.go:43-47`) it takes only `id codec.KeyID`:

```go
// Was:
func (m *Material) AsCodecKey(id codec.KeyID) codec.Key {
    return codec.Key{ID: id, Bytes: out}
}
// Becomes:
func (m *Material) AsCodecKey(id codec.KeyID, version uint32) codec.Key {
    return codec.Key{ID: id, Version: version, Bytes: out}
}
```

Three callsites in `internal/eventbus/crypto/dek/manager.go` are updated to pass version: `manager.go:139` (mint path — passes `1`, the just-minted row's version), `manager.go:148` (cache-hit path — passes the `Version` it just used as part of the cache key), `manager.go:201` (DB-unwrap path — passes `r.Version` from the row already read at `manager.go:192`). The `gorules/analyzers/codeckeybytesallowlist` analyzer continues to allow `codec.Key` literal construction inside `internal/eventbus/codec/...` and `internal/eventbus/crypto/...`; the `AsCodecKey` site is in the latter, so no analyzer surface change.

**Why over alternatives:**

- *Return `(codec.Key, uint32, error)` from `GetOrCreate`:* spreads the same DEK-identity component across two return positions; every caller has to thread it. `codec.Key` is the natural per-call carrier.
- *Hardcode `1` in Phase 3a, refactor at Phase 4:* guarantees that Phase 4 rotation work touches every emit/decrypt callsite that just landed in Phase 3a. Worst possible churn ordering.

**Phase 2 already treats `(KeyID, Version)` as the DEK identity at every other boundary** — `dek.CacheKey{KeyID, Version}`, `store.selectByID(keyID, version)`, `Manager.Resolve(keyID, version)`, `crypto_keys.version` column, `aad.Build(..., dekVersion)`, `App-Dek-Version` header. `codec.Key` is the only place version is omitted. This decision closes a Phase 2 oversight, not a redesign.

**Spec change:** master spec §5.1 layered-interfaces text edited to show the 3-field `codec.Key`. The shape is descriptive (it documents what's in the substrate), not prescriptive (the spec doesn't otherwise constrain key shape).

**Cost:** one field added to a struct. Existing zero-value usages in `publisher.go:184` (`var key codec.Key`) and `subscriber.go:398` continue to work — `Version=0` is the right default for IdentityCodec.

## Decision 4 — No SDK payload type change

**Decision:** Keep `pkg/plugin/event.go::EmitIntent.Payload string`. No SDK type change. Encryption operates on `[]byte` host-side at the boundary where `internal/plugin/event_emitter.go:144` already converts via `payload := []byte(intent.Payload)`.

**Trace through layers:**

| Layer | Type | Source |
|---|---|---|
| Plugin SDK | `Payload string` (JSON) | `pkg/plugin/event.go:114` |
| Host boundary | `[]byte(intent.Payload)` cast | `internal/plugin/event_emitter.go:144` |
| Codec input | `[]byte` plaintext | new `Codec.Encode(ctx, plaintext, key, aad)` signature |
| Codec output | `[]byte` ciphertext | same |
| Internal eventbus | `Event.Payload []byte` | `internal/eventbus/types.go:99` (comment already anticipates ciphertext) |
| Wire proto | `bytes payload = 6` | `pkg/proto/holomush/eventbus/v1/eventbus.proto` |

**Why no change:** the plugin never sees ciphertext. Encryption happens host-side at the `string→[]byte→codec.Encode` seam. On the Phase 3b decrypt path, the spec at §4.5 already specifies that subscribers receiving sensitive events get either plaintext (decrypted bytes back to JSON string for SDK) or `payload = empty bytes` + `metadata_only = true`. Neither case requires the SDK to model ciphertext.

**Spec change:** none.

**Cost:** none.

---

## Master spec edits required

| Master spec section | Edit |
|---|---|
| §5.1 (line 793-808) | `codec.Codec.Encode/Decode` signatures gain `aad []byte`. Drop "unchanged interface" wording. Reflect the 3-field `codec.Key`. |
| §3 emit flow box (line 373) | Already correct (`Codec.Encrypt(payload, DEK, AAD=metadata)` is logical, not Go) — leave as-is. |
| §3 cold-tier rekey flow (line 1290) | Same — leave as-is. |
| §11.1 phase 3 row | Reword the "downgrade-attack fence" reference: emit-time fence is INV-6 + INV-7 (this is correct in §2 already; the §11.1 text just needs to say "INV-6/7 fence" instead of leaving it ambiguous with INV-50). |

INV-6, INV-7 (emit-time fence), INV-25 (AAD tamper), INV-49 (header→column round-trip) all correctly described in §2 and unchanged.

INV-50 (cold-tier read fence, Phase 7) correctly scoped — this doc does NOT modify INV-50.

## Bead updates required

| Bead | Change |
|---|---|
| `holomush-ojw1.1` (Phase 3a sub-epic) | Description: replace "INV-50 downgrade fence" with "INV-6/7 emit-time fence". Add note that codec interface is being extended (Q1) and `codec.Key` gets a `Version` field (Q3). |
| `holomush-ojw1.1.2` (T2: codec impl) | Title/description: codec impl uses new `Encode/Decode(ctx, plaintext, key, aad []byte)` signature. The Codec interface change is part of this task. |
| `holomush-ojw1.1.3` (T3: ManifestLookup.LookupEventSensitivity) | Description: clarify `Sensitivity` is already in `package plugins` from Phase 1; this task only adds the lookup method. Add a sub-task: append `Sensitive bool` to `pkg/plugin/event.go::EmitIntent`. |
| `holomush-ojw1.1.4` (T4: emit-time fence) | Title: rename "downgrade-attack fence (INV-50)" → "emit-time sensitivity fence (INV-6/7)". Add: hard-reject for over-claim AND under-claim via `EVENT_SENSITIVITY_NOT_DECLARED` / `EVENT_SENSITIVITY_OVER_CLAIM`. |
| `holomush-ojw1.1.5` (T5: DEK acquisition) | Description: reflect that `key.Version` is read directly off the returned `codec.Key` (Q3); `intent.Sensitive` (Q2) drives the effective-sensitivity computation. |
| `holomush-ojw1.1.6` (T6: encrypt switch) | Description: codec call site uses new 4-arg signature; AAD bytes built by caller via `aad.Build(event, codecName, dekRef, dekVersion)`. |
| `holomush-ojw1.1.7`, `.8`, `.9`, `.10` | Unchanged. |

The Phase 3a plan (`docs/superpowers/plans/2026-05-02-event-payload-crypto-phase3a-codec-emit.md`) is to be revised against this grounding by the next `superpowers:writing-plans` pass.

---

## Out of scope

This document does not address:

- Decryption path (Phase 3b)
- DEK cache + invalidation (Phase 3c)
- Cold-tier crypto, NATS deny rules, flag flip (Phase 3d)
- Operator break-glass (Phase 5)
- Vault provider (Phase 6)
- Plugin SDK helpers and plugin-owned audit (Phase 7)
- Site documentation (Phase 8)
