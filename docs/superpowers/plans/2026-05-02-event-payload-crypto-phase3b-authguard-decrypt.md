<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Event Payload Crypto — Phase 3b — AuthGuard + Decrypt-on-Fanout + Metadata-Only Delivery

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land subscriber-side decryption: an `AuthGuard` that gates plaintext delivery to character/player/plugin recipients via DEK participant set + plugin manifest + ABAC engine; decrypt-on-fanout when permitted; `metadata_only=true` delivery when denied; `DecryptAuditEmitter` with backpressure for plugin recipients (INV-19); replacement of the `EVENTBUS_HISTORY_SENSITIVE_NOT_SUPPORTED_PHASE3A` fail-fast in the hot-tier history reader. Default `Crypto.Enabled=false` keeps the path dark in production until Phase 3d.

**Architecture:** Seven substrate decisions, all closed in the grounding doc R4. Decision 0 restores master spec §4.1 wire shape (encrypt only `event.Payload`, not the marshaled envelope) — Phase 3a drift restoration. Decision 1 introduces `internal/eventbus/authguard/` with typed `Identity`, typed `Decision`, and dependency interfaces. Decision 6 extends `accesstypes.AccessRequest` with caller-supplied `Attributes` overlaying the `Action` bag; reserved key `"name"` is server-only. Decision 7 introduces `player_character_bindings` substrate (master spec §4.3a) — the binding entity AuthGuard's character branch matches on. Decision 3 ships `internal/eventbus/authguard/audit/` with a `queuedAuditEmitter` satisfying both `DecryptAuditEmitter` and `BackpressureChecker` interfaces. Decisions 4 and 5 wire `metadata_only` onto `corev1.EventFrame` (delivery shape, not storage shape) and extend `Subscriber.OpenSession` with a required `Identity` parameter, with AuthGuard wired into both the live Subscribe path and the hot-tier history reader.

**Tech Stack:** Go 1.22+, the existing eventbus substrate (`internal/eventbus/{codec,publisher,subscriber}`, `internal/eventbus/crypto/{aad,dek}`, `internal/eventbus/history`), the existing access engine (`internal/access/policy/{types,engine,attribute,policytest,seed,dsl}`), Phase 1 manifest grammar (`internal/plugin/crypto_manifest.go`, `crypto_validator.go`), `internal/world/postgres.Transactor` (`transactor.go:27` — `InTransaction(ctx, fn)` helper for the Tx-via-context pattern), proto generation via `task proto`.

**Grounding:** [`docs/superpowers/specs/2026-05-02-event-payload-crypto-phase3b-grounding.md`](../specs/2026-05-02-event-payload-crypto-phase3b-grounding.md) (R4, READY per four design-reviewer passes) — seven substrate decisions with citations; **mandatory pre-read** before this plan. Companion to the merged Phase 3a plan at [`2026-05-02-event-payload-crypto-phase3a-codec-emit.md`](2026-05-02-event-payload-crypto-phase3a-codec-emit.md). Master spec amendments shipped in the same commit as the grounding doc: §4.3a (binding entity, NEW), §6.1 step-2 amendment.

**Bead:** `holomush-ojw1.2`. Each task closes one or more sub-beads (numbering assigned during plan execution).

---

## File structure

| File | Status | Responsibility |
| --- | --- | --- |
| `internal/eventbus/publisher.go` | MODIFY | Encrypt `event.Payload` only, not the marshaled envelope; envelope.Payload becomes ciphertext for sensitive events; AAD built before encrypt from cleartext envelope. |
| `internal/eventbus/subscriber.go` | MODIFY | `decodeDelivery` proto-unmarshals first; conditionally decrypts `envelope.Payload` if codec != identity; rebuilds AAD from envelope; AuthGuard.Check + decrypt-or-stamp-metadata_only flow. New `WithSubscriberAuthGuard` / `WithSubscriberDEKManager` / `WithSubscriberDecryptAuditEmitter` options. `OpenSession` signature gains required `identity authguard.Identity`. `jetStreamDelivery` carries `metadataOnly bool`. |
| `internal/eventbus/subscriber_test.go` | MODIFY | All `OpenSession` callsites updated for new signature; new sensitive-codec decrypt tests covering AuthGuard permit/deny outcomes. |
| `internal/eventbus/history/hot_jetstream.go` | MODIFY | Replace `EVENTBUS_HISTORY_SENSITIVE_NOT_SUPPORTED_PHASE3A` fail-fast with proto-unmarshal-first + AuthGuard + decrypt-or-stamp-metadata_only. New constructor options mirroring subscriber's. |
| `internal/eventbus/audit/projection.go` | UNCHANGED | INV-21 byte-equality preserved (still mirrors `msg.Data()` verbatim); no semantic change. |
| `internal/eventbus/audit/projection_test.go` | UNCHANGED | INV-21 assertion still holds. |
| `test/integration/crypto/emit_test.go` | MODIFY | INV-21 assertion stays; ADD assertions: `proto.Unmarshal(msg.Data())` succeeds; `envelope.Payload != []byte(plaintext)`; `envelope.Subject` and `envelope.Type` cleartext on wire. |
| `internal/access/policy/types/types.go` | MODIFY | `AccessRequest` gains `Attributes map[string]any`; `NewAccessRequest` takes 4th `attrs` parameter; `reservedActionKeys` map; `ACCESS_REQUEST_RESERVED_ATTRIBUTE` typed error. |
| `internal/access/policy/types/types_test.go` | MODIFY | Update existing 3-arg test calls; add reserved-key validation test. |
| `internal/access/policy/engine.go` | MODIFY | `Evaluate` overlays `req.Attributes` onto `bags.Action` after resolver returns; caller wins on conflict. |
| `internal/access/policy/engine_test.go` | MODIFY | Update existing test calls to pass `nil` 4th arg; add Action-overlay composition tests using existing fixtures. |
| `internal/access/policy/policytest/helpers_test.go` | MODIFY | Update 6 existing test calls to pass `nil` 4th arg. |
| 11 other production callsites of `NewAccessRequest` | MODIFY | Each becomes `NewAccessRequest(s, a, r, nil)`. Mechanical. |
| `internal/store/migrations/000015_create_player_character_bindings.up.sql` | CREATE | `player_character_bindings` table per master spec §4.3a; UNIQUE INDEX + back-population. |
| `internal/store/migrations/000015_create_player_character_bindings.down.sql` | CREATE | DROP TABLE, DROP INDEXes. |
| `internal/world/postgres/binding_repo.go` | CREATE | `BindingRepository` type (in `package postgres`, alongside `character_repo.go`) with `Current(ctx, characterID string) (string, error)`, `Create(ctx, playerID, characterID, reason string) (string, error)`, `End(ctx, bindingID, reason string) error`. Uses the existing private `execerFromCtx` helper at `helpers.go:42` for Tx composition. Typed errors `BINDING_NOT_FOUND`, `BINDING_ALREADY_ENDED`. |
| `internal/world/postgres/binding_repo_test.go` | CREATE | Integration tests against testpool harness; Tx-composition test confirming Create+failure → no binding row persisted. |
| `internal/world/postgres/character_repo.go:46` | MODIFY | `Create` switches from `r.pool.Exec` to `execerFromCtx(ctx, r.pool).Exec` (one-line change matching `Delete` pattern at line 76). |
| `internal/grpc/auth_handlers.go:369` (`CreateCharacter`) | MODIFY | Wrap `characterService.Create` + `bindingStore.Create` in `transactor.InTransaction`. Path A (regular character creation). |
| `internal/auth/guest_service.go:113` (`CreateGuest`) | MODIFY | Wrap `s.players.Create` (already happens) + `s.chars.Create` + `bindingStore.Create` in `transactor.InTransaction`. Path B (unified-guest-auth, PR #181). Replaces existing best-effort cleanup at line 116 with Tx rollback. |
| `internal/eventbus/crypto/dek/manager.go` | MODIFY | `Manager` interface gains `Participants(ctx, keyID, version) ([]Participant, error)`; production `manager` impl delegates to `m.store.selectByID`; `NewManagerForUnitTest` returns `DEK_MANAGER_NOT_CONFIGURED` via existing `configured()` guard. |
| `internal/eventbus/crypto/dek/manager_integration_test.go` | MODIFY | Add Participants integration test using existing `newTestPool` harness. |
| `api/proto/holomush/core/v1/core.proto:136-216` | MODIFY | `EventFrame` gains `bool metadata_only = 10`. |
| `pkg/proto/holomush/core/v1/core.pb.go` | REGENERATE | `task proto` regenerates Go bindings. |
| `internal/eventbus/bus.go:18-25` | MODIFY | `Subscriber.OpenSession` signature gains `identity authguard.Identity`. `Delivery` interface (`bus.go:43-52`) gains `MetadataOnly() bool`. |
| `internal/plugin/manager.go` | MODIFY | New exported `*Manager.PluginRequestsDecryption(pluginName, eventType string) bool` method walking `Crypto.Consumes[].RequestsDecryption[]` per `crypto_validator.go:60-93` qualified-ref form. |
| `internal/plugin/manager_test.go` (or new `crypto_manifest_lookup_test.go`) | CREATE/MODIFY | Unit test for PluginRequestsDecryption. |
| `internal/eventbus/authguard/authguard.go` | CREATE | `Identity`, `IdentityKind`, `CheckRequest`, `Decision`, `DecisionCode`, `AuthGuard` interface, `ParticipantLookup` / `ManifestLookup` / `ABACEngine` / `BackpressureChecker` interfaces, `New(...)` constructor. |
| `internal/eventbus/authguard/identity.go` | CREATE | Typed constructors with input validation; uses `idgen.New()` (per project's crypto/rand convention) for any internal ULIDs. |
| `internal/eventbus/authguard/identity_test.go` | CREATE | Constructor input-validation tests. |
| `internal/eventbus/authguard/guard.go` | CREATE | `Guard` concrete impl with §7.2 four-branch decision tree. Uses `accesstypes.Decision.PolicyID()` for `Decision.GrantID` extraction. |
| `internal/eventbus/authguard/guard_test.go` | CREATE | Branch-by-branch unit tests; INV-43 operator-deny; uses `policytest.GrantEngine` / `policytest.NewErrorEngine` from `internal/access/policy/policytest/helpers.go`. |
| `internal/eventbus/authguard/adapter_dek.go` | CREATE | `dek.Manager` adapter satisfying `ParticipantLookup`. |
| `internal/eventbus/authguard/adapter_dek_test.go` | CREATE | Adapter unit test. |
| `internal/eventbus/authguard/adapter_manifest.go` | CREATE | `*plugin.Manager` adapter satisfying `ManifestLookup`. |
| `internal/eventbus/authguard/adapter_manifest_test.go` | CREATE | Adapter unit test. |
| `internal/eventbus/authguard/audit/emitter.go` | CREATE | `PluginDecryptRecord`, `DecryptAuditEmitter` interface, `queuedAuditEmitter` impl with per-plugin queues + drain goroutines. `WithGameID` option (no hardcoded "holomush"). |
| `internal/eventbus/authguard/audit/emitter_test.go` | CREATE | Backpressure threshold tests; queue-full → throttle; drain-below-50% → unthrottle; drain-side failure metrics. |
| `internal/grpc/server.go:679` (Subscribe handler) | MODIFY | After `s.sessionStore.Get`, query `s.bindings.Current(ctx, info.CharacterID.String())`; call `authguard.NewCharacterIdentity(playerID, characterID, bindingID)`; pass `Identity` to `OpenSession`; read `delivery.MetadataOnly()` and stamp `EventFrame.metadata_only`. |
| `internal/grpc/server_helpers_test.go`, `internal/grpc/subscribe_server_test.go` | MODIFY | All `fakeSubscriber`/`stubSubscriber.OpenSession` signatures updated for `Identity` parameter. |
| `internal/grpc/query_stream_history.go` | MODIFY | Same Identity-construction pattern as Subscribe. |
| `internal/eventbus/eventbustest/embedded_test.go` | MODIFY | OpenSession signature update. |
| `internal/eventbus/integration_test.go` | MODIFY | OpenSession signature update. |
| `internal/eventbus/subscriber_round3_test.go` | MODIFY | OpenSession signature update. |
| `internal/eventbus/publisher_test.go` | MODIFY | OpenSession signature update (3 callsites). |
| `internal/eventbus/bus_test.go` | MODIFY | `fakeBus.OpenSession` signature update. |
| `test/integration/auth/auth_suite_test.go` | MODIFY | `unusedSubscriber.OpenSession` signature update. |
| `test/integration/eventbus_e2e/multi_protocol_fanout_test.go`, `reconnect_resume_test.go` | MODIFY | OpenSession signature updates. |
| `internal/access/policy/seed.go` | MODIFY | Add seed policy denying `subscribe` on `audit.>` for plugin and character principals. |
| `internal/access/policy/seed_test.go` | MODIFY | Add test for the new seed policy. |
| `internal/store/migrations/000016_seed_audit_subscribe_deny.up.sql` | CREATE | Idempotent INSERT of the new policy for existing deployments (the seed mechanism only runs at fresh-bootstrap; deployed databases need a migration). |
| `internal/store/migrations/000016_seed_audit_subscribe_deny.down.sql` | CREATE | DELETE of the policy. |
| `test/integration/crypto/decrypt_test.go` | CREATE | INV-8, INV-9 (participant + non-participant) integration tests using `testutil` harness pattern from `emit_test.go`. |
| `test/integration/crypto/plugin_decrypt_test.go` | CREATE | INV-17, INV-18, INV-19, INV-20 (plugin manifest + ABAC + audit + isolation) integration tests. |
| `test/integration/crypto/history_decrypt_test.go` | CREATE | INV-22 (QueryStreamHistory parity), INV-25 (AAD tamper under restored shape). |
| `test/integration/crypto/metadata_only_test.go` | CREATE | INV-26 (delivery contract: empty payload + populated metadata + flag set). |

**Existing migrations 000013 (`crypto_keys`) and 000014 (`events_audit_dek_columns`) remain untouched. Phase 3b adds 000015 (binding table) and 000016 (audit-deny policy).** Proto regeneration: `task proto` regenerates `pkg/proto/...` from `api/proto/...` whenever a `.proto` file changes; Task T5 invokes it once.

---

## Tasks

### Task T0: Verify clean working copy and current main

Each task commits separately. If the worktree starts with uncommitted changes or is not based on the latest main, the first commit picks up unrelated work.

- [ ] **Step 1: Verify clean working copy.**

Run: `jj --no-pager st`

Expected: working copy contains only the Phase 3b grounding doc commit; no other uncommitted changes.

- [ ] **Step 2: Verify base.**

Run: `jj --no-pager log -r 'main..@' --no-graph | head -10`

Expected: shows only the grounding-doc commit (`docs(crypto): Phase 3b grounding doc R4`) sitting on top of Phase 3a's merge.

- [ ] **Step 3: Verify Phase 3a is merged.**

Run: `jj --no-pager log -r 'main' --no-graph | head -1`

Expected: includes `Phase 3a — codec + emit + sensitivity fence (holomush-ojw1.1)`.

- [ ] **Step 4: Verify build is green at base.**

Run: `task build`

Expected: build succeeds.

If broken: stop and fix the base before adding 3b changes.

---

### Task T1: Restore §4.1 wire shape — encrypt `event.Payload` only (Decision 0)

Phase 3a's `publisher.go` proto-marshals the entire `eventbusv1.Event` and feeds the marshaled bytes to `codec.Encode`, producing one ciphertext blob as `msg.Data`. Master spec §4.1 lines 480-490 specifies that only `event.Payload` (field 6) is ciphertext; fields 1-5 + field 7 (rendering) stay cleartext on the wire as proto fields. This task restores that shape.

**Files:**

- Modify: `internal/eventbus/publisher.go:189-291` (the `Publish` method's encrypt path)
- Modify: `internal/eventbus/subscriber.go:354-435` (`decodeDelivery`)
- Modify: `internal/eventbus/history/hot_jetstream.go:346-408` (`decodeJetStreamMessage`)
- Modify: `test/integration/crypto/emit_test.go` (assertion updates)

- [ ] **Step 1: Add a failing test for the restored wire shape.**

In `test/integration/crypto/emit_test.go`, after the existing INV-21 assertion at line 180 (`assert.Equal(t, msg.Data(), row.Payload, "INV-21: ...")`), add:

```go
// Decision 0: msg.Data is the marshaled envelope (cleartext metadata
// fields + ciphertext payload field), NOT a single ciphertext blob.
var envelope eventbusv1.Event
require.NoError(t, proto.Unmarshal(msg.Data(), &envelope), "msg.Data MUST unmarshal as eventbusv1.Event")
assert.NotEqual(t, []byte(plaintext), envelope.Payload, "envelope.Payload MUST be ciphertext, not plaintext")
assert.Equal(t, "scene:01HXXXTESTSCENE000000000", envelope.Subject, "envelope.Subject MUST be cleartext on the wire")
assert.Equal(t, "test-plugin:whisper", envelope.Type, "envelope.Type MUST be cleartext on the wire")
require.NotNil(t, envelope.Timestamp, "envelope.Timestamp MUST be cleartext on the wire")
require.NotNil(t, envelope.Actor, "envelope.Actor MUST be cleartext on the wire")
```

Add the import `eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"` and `"google.golang.org/protobuf/proto"` if not present.

- [ ] **Step 2: Run the test to verify it fails against Phase 3a's shape.**

Run: `task test:int -- -tags=integration -run TestSensitiveEmitProducesCiphertextOnBusAndInAudit ./test/integration/crypto/...`

Expected: FAIL — `proto.Unmarshal(msg.Data(), &envelope)` returns an error or `envelope.Subject` is empty (Phase 3a's `msg.Data` is one ciphertext blob, not a marshaled envelope).

- [ ] **Step 3: Update `publisher.go` to encrypt only `event.Payload`.**

In `internal/eventbus/publisher.go`, locate the encrypt block at lines 189-287. Currently:

```go
envelope := &eventbusv1.Event{ /* fields 1-7 with raw payload */ }
plainBytes, err := proto.Marshal(envelope)
// ... codec selection, key resolution, AAD build ...
encoded, err := c.Encode(ctx, plainBytes, key, aadBytes)
msg.Data = encoded
```

Replace with the restored shape — encrypt the payload bytes first, then marshal the envelope:

```go
envelope := &eventbusv1.Event{
    Id:        event.ID.Bytes(),
    Subject:   string(event.Subject),
    Type:      string(event.Type),
    Timestamp: timestamppb.New(event.Timestamp),
    Actor:     ActorToProto(event.Actor),
    Payload:   event.Payload, // raw plaintext for now; replaced below if sensitive
    Rendering: RenderingToProto(event.Rendering),
}

// ... existing codec selection / key resolution block (lines 206-244 unchanged) ...

c, err := codec.Resolve(codecName)
if err != nil {
    return oops.Code("EVENTBUS_CODEC_UNKNOWN").With("codec", string(codecName)).Wrap(err)
}

// ... existing legacy-selector key-resolve block (lines 254-268 unchanged) ...

// AAD binds (codec, key id/version, envelope identity) into the AEAD
// authentication tag for sensitive events. Built BEFORE encrypt because
// it reads cleartext envelope fields (id, subject, type, timestamp,
// actor) that aad.Build extracts. aad.Build NEVER reads event.Payload,
// so the raw payload still being in envelope.Payload at this point is fine.
var aadBytes []byte
if event.Sensitive {
    ab, aErr := aad.Build(envelope, string(codecName), uint64(key.ID), key.Version)
    if aErr != nil {
        return oops.Code("EVENTBUS_AAD_BUILD_FAILED").Wrap(aErr)
    }
    aadBytes = ab
}

// DECISION 0: encrypt ONLY event.Payload (the bytes), not the marshaled
// envelope. For identity codec this is a no-op (IdentityCodec.Encode
// returns input unchanged). For sensitive codec, envelope.Payload is
// replaced with ciphertext.
ciphertext, err := c.Encode(ctx, event.Payload, key, aadBytes)
if err != nil {
    return oops.Code("EVENTBUS_CODEC_ENCODE_FAILED").
        With("codec", string(codecName)).
        Wrap(err)
}
envelope.Payload = ciphertext

// Now marshal the envelope (with cleartext metadata + maybe-ciphertext
// payload field). The marshaled bytes go on the wire as msg.Data.
plainBytes, err := proto.Marshal(envelope)
if err != nil {
    return oops.Code("EVENTBUS_ENVELOPE_MARSHAL_FAILED").Wrap(err)
}

msg := &nats.Msg{
    Subject: string(event.Subject),
    Data:    plainBytes,
    Header:  nats.Header{},
}
// ... rest of header stamping unchanged ...
```

The diff is: move `proto.Marshal` AFTER the `c.Encode` call; pass `event.Payload` (not `plainBytes`) to `c.Encode`; assign the encoded result back into `envelope.Payload`; marshal the envelope last.

- [ ] **Step 4: Update `subscriber.go::decodeDelivery` to proto-unmarshal first.**

In `internal/eventbus/subscriber.go`, locate `decodeDelivery` at line 357. Replace the codec-decode-then-proto-unmarshal logic with proto-unmarshal-then-conditional-decode:

```go
func decodeDelivery(ctx context.Context, msg jetstream.Msg, selector codec.KeySelector) (Event, error) {
    h := msg.Headers()
    _ = telemetry.ExtractContext(ctx, h)

    msgIDStr := h.Get(HeaderMsgID)
    if msgIDStr == "" {
        return Event{}, oops.Code("EVENTBUS_SUBSCRIBE_MISSING_HEADER").
            With("header", HeaderMsgID).Errorf("missing header")
    }
    id, err := ulid.Parse(msgIDStr)
    if err != nil {
        return Event{}, oops.Code("EVENTBUS_SUBSCRIBE_BAD_MSG_ID").
            With("value", msgIDStr).Wrap(err)
    }
    schemaVer := h.Get(HeaderSchemaVersion)
    if schemaVer != SchemaVersion {
        return Event{}, oops.Code("EVENTBUS_SUBSCRIBE_SCHEMA_MISMATCH").
            With("got", schemaVer).With("want", SchemaVersion).Errorf("schema version mismatch")
    }
    codecNameStr := h.Get(HeaderCodec)
    if codecNameStr == "" {
        return Event{}, oops.Code("EVENTBUS_SUBSCRIBE_MISSING_HEADER").
            With("header", HeaderCodec).Errorf("missing header")
    }
    c, err := codec.Resolve(codec.Name(codecNameStr))
    if err != nil {
        return Event{}, oops.Code("EVENTBUS_SUBSCRIBE_UNKNOWN_CODEC").Wrap(err)
    }

    // DECISION 0: proto-unmarshal FIRST. msg.Data is the marshaled
    // envelope (cleartext fields + maybe-ciphertext payload field).
    var envelope eventbusv1.Event
    if unmarshalErr := proto.Unmarshal(msg.Data(), &envelope); unmarshalErr != nil {
        return Event{}, oops.Code("EVENTBUS_SUBSCRIBE_UNMARSHAL_FAILED").Wrap(unmarshalErr)
    }

    // For identity codec, envelope.Payload IS the plaintext — no decode.
    // For sensitive codecs, T9 will add AuthGuard.Check + AAD reconstruct
    // + decrypt-or-stamp-metadata_only here. T1 keeps the existing
    // Phase 3a "non-identity gets a passthrough decode with nil AAD"
    // behavior so that tests remain green; that gets replaced in T9.
    if codec.Name(codecNameStr) != codec.NameIdentity {
        var key codec.Key
        if selector != nil {
            k, kerr := selector.SelectForDecrypt(ctx, codec.Name(codecNameStr), 0)
            if kerr != nil {
                return Event{}, oops.Code("EVENTBUS_SUBSCRIBE_KEY_FETCH_FAILED").
                    With("codec", codecNameStr).Wrap(kerr)
            }
            key = k
        }
        plain, decErr := c.Decode(ctx, envelope.Payload, key, nil)
        if decErr != nil {
            return Event{}, oops.Code("EVENTBUS_SUBSCRIBE_DECODE_FAILED").
                With("codec", codecNameStr).Wrap(decErr)
        }
        envelope.Payload = plain
    }

    ev := Event{
        ID:        id,
        Subject:   Subject(envelope.GetSubject()),
        Type:      Type(envelope.GetType()),
        Timestamp: envelope.GetTimestamp().AsTime(),
        Actor:     actorFromProto(envelope.GetActor()),
        Payload:   envelope.GetPayload(),
        Rendering: RenderingFromProto(envelope.GetRendering()),
    }
    if meta, mErr := msg.Metadata(); mErr == nil && meta != nil {
        ev.Seq = meta.Sequence.Stream
    }
    return ev, nil
}
```

The codec-decode call's input shifts from `msg.Data()` (the whole wire body) to `envelope.Payload` (the proto field), and its output replaces `envelope.Payload` rather than feeding into a separate `proto.Unmarshal`.

- [ ] **Step 5: Update `hot_jetstream.go::decodeJetStreamMessage` with the same shape.**

In `internal/eventbus/history/hot_jetstream.go`, replace the `EVENTBUS_HISTORY_SENSITIVE_NOT_SUPPORTED_PHASE3A` fail-fast at line 376-380 and the surrounding decode logic with the proto-unmarshal-first shape:

```go
func decodeJetStreamMessage(ctx context.Context, msg jetstream.Msg, selector codec.KeySelector) (eventbus.Event, error) {
    h := msg.Headers()
    msgIDStr := h.Get(eventbus.HeaderMsgID)
    if msgIDStr == "" {
        return eventbus.Event{}, oops.Code("EVENTBUS_HISTORY_MISSING_HEADER").
            With("header", eventbus.HeaderMsgID).Errorf("missing header")
    }
    id, err := ulid.Parse(msgIDStr)
    if err != nil {
        return eventbus.Event{}, oops.Code("EVENTBUS_HISTORY_BAD_MSG_ID").Wrap(err)
    }
    codecName := h.Get(eventbus.HeaderCodec)
    if codecName == "" {
        return eventbus.Event{}, oops.Code("EVENTBUS_HISTORY_MISSING_HEADER").
            With("header", eventbus.HeaderCodec).Errorf("missing header")
    }
    c, err := codec.Resolve(codec.Name(codecName))
    if err != nil {
        return eventbus.Event{}, oops.Code("EVENTBUS_HISTORY_UNKNOWN_CODEC").
            With("codec", codecName).Wrap(err)
    }

    // DECISION 0: proto-unmarshal FIRST.
    var envelope eventbusv1.Event
    if unmarshalErr := proto.Unmarshal(msg.Data(), &envelope); unmarshalErr != nil {
        return eventbus.Event{}, oops.Code("EVENTBUS_HISTORY_UNMARSHAL_FAILED").Wrap(unmarshalErr)
    }

    if codec.Name(codecName) != codec.NameIdentity {
        var key codec.Key
        if selector != nil {
            k, kerr := selector.SelectForDecrypt(ctx, codec.Name(codecName), 0)
            if kerr != nil {
                return eventbus.Event{}, oops.Code("EVENTBUS_HISTORY_KEY_FETCH_FAILED").
                    With("codec", codecName).Wrap(kerr)
            }
            key = k
        }
        plain, decErr := c.Decode(ctx, envelope.Payload, key, nil)
        if decErr != nil {
            return eventbus.Event{}, oops.Code("EVENTBUS_HISTORY_DECODE_FAILED").
                With("codec", codecName).Wrap(decErr)
        }
        envelope.Payload = plain
    }

    return eventbus.Event{
        ID:        id,
        Subject:   eventbus.Subject(envelope.GetSubject()),
        Type:      eventbus.Type(envelope.GetType()),
        Timestamp: envelope.GetTimestamp().AsTime(),
        Actor:     actorFromEnvelope(envelope.GetActor()),
        Payload:   envelope.GetPayload(),
        Rendering: eventbus.RenderingFromProto(envelope.GetRendering()),
    }, nil
}
```

The `EVENTBUS_HISTORY_SENSITIVE_NOT_SUPPORTED_PHASE3A` block is removed entirely. Task T9 will add AuthGuard + AAD + real decrypt; this task is the wire-shape restoration only.

- [ ] **Step 6: Run the integration test to verify it passes.**

Run: `task test:int -- -tags=integration -run TestSensitiveEmitProducesCiphertextOnBusAndInAudit ./test/integration/crypto/...`

Expected: PASS — proto.Unmarshal succeeds, envelope.Payload is ciphertext, envelope.Subject/Type/Timestamp/Actor are cleartext.

- [ ] **Step 7: Run the full unit-test suite to verify no regressions.**

Run: `task test`

Expected: PASS — identity-codec paths unchanged.

- [ ] **Step 8: Run the full integration suite.**

Run: `task test:int`

Expected: PASS.

- [ ] **Step 9: Commit.**

```shell
JJ_EDITOR=true jj --no-pager describe -m "feat(crypto): Phase 3b T1 — restore §4.1 wire shape (encrypt event.Payload only) (holomush-ojw1.2)

Phase 3a's publisher.go encrypted the full marshaled envelope, producing
one ciphertext blob as msg.Data. Master spec §4.1 lines 480-490 specifies
that only event.Payload (proto field 6) is ciphertext; fields 1-5 + 7
remain cleartext on the wire as proto fields. This commit restores the
specified shape per Phase 3b grounding doc Decision 0.

publisher.go now encrypts event.Payload only and marshals the envelope
(with the ciphertext payload field) as msg.Data. subscriber.go and
hot_jetstream.go proto-unmarshal first, then conditionally decrypt the
Payload field for non-identity codecs.

INV-21 holds. INV-25 (AAD tamper) holds — aad.Build reads only metadata
fields, so AAD inputs are identical at encrypt and decrypt sites.

The hot-tier history reader's EVENTBUS_HISTORY_SENSITIVE_NOT_SUPPORTED_PHASE3A
fail-fast is removed; Task T9 will add AuthGuard + real decrypt." && jj --no-pager new
```

---

### Task T2: Extend `accesstypes.AccessRequest` with caller-supplied `Attributes` (Decision 6)

Master spec §7.2 Branch 3 calls `Evaluate(subject, action, resource, attributes={event_type, plugin_name, plugin_inst})`. The current substrate at `internal/access/policy/types/types.go:107-130` doesn't carry attributes. This task adds them with the Action-bag overlay convention; reserved key `"name"` is server-only.

**Files:**

- Modify: `internal/access/policy/types/types.go:107-130` (struct + constructor + reserved-key validation)
- Modify: `internal/access/policy/types/types_test.go` (existing tests + new validation tests)
- Modify: `internal/access/policy/engine.go:227` area (overlay block after resolver returns)
- Modify: `internal/access/policy/engine_test.go` (composition tests)
- Modify: 13 callsite files per `rg -l 'NewAccessRequest\(' --type go` (25 callsites total):
  - `internal/grpc/query_stream_history.go:182`
  - `internal/plugin/hostfunc/commands.go:180`
  - `internal/plugin/hostfunc/functions.go:296`
  - `internal/world/service.go:127`
  - `internal/command/rate_limit_middleware.go:85`
  - `internal/command/access.go:25`
  - `internal/world/service_helpers_test.go:271`
  - `internal/access/policy/policytest/helpers_test.go` (6 callsites)
  - `internal/access/policy/engine_test.go:2219`
  - `internal/access/policy/types/types_test.go:222,244`
  - `test/integration/plugin/abac_widget_test.go`
  - `test/integration/plugin/binary_plugin_test.go`

- [ ] **Step 1: Write failing tests for the new field + reserved-key validation + Action-overlay composition.**

In `internal/access/policy/types/types_test.go`, add:

```go
func TestNewAccessRequestAcceptsNilAttributes(t *testing.T) {
    req, err := NewAccessRequest("character:01ABC", "read", "location:01XYZ", nil)
    require.NoError(t, err)
    assert.Nil(t, req.Attributes)
}

func TestNewAccessRequestAcceptsCallerAttributes(t *testing.T) {
    attrs := map[string]any{
        "event_type":  "core-comm:whisper",
        "plugin_inst": "01INST",
    }
    req, err := NewAccessRequest("plugin:mod-filter", "decrypt", "dek:dm:01HABC", attrs)
    require.NoError(t, err)
    assert.Equal(t, "core-comm:whisper", req.Attributes["event_type"])
    assert.Equal(t, "01INST", req.Attributes["plugin_inst"])
}

func TestNewAccessRequestRejectsReservedNameKey(t *testing.T) {
    // "name" is reserved (resolver writes req.Action verb into bags.Action["name"]).
    // Caller-supplied "name" would silently overwrite the resolver value.
    attrs := map[string]any{"name": "something"}
    _, err := NewAccessRequest("character:01ABC", "read", "location:01XYZ", attrs)
    errutil.AssertErrorCode(t, err, "ACCESS_REQUEST_RESERVED_ATTRIBUTE")
}
```

In `internal/access/policy/engine_test.go`, add Action-overlay tests at the end of the file (next to the existing `TestEvaluate*` tests around line 2200+). These reuse existing test fixtures — no new helpers invented.

```go
func TestEvaluateOverlaysCallerAttributesOntoActionBag(t *testing.T) {
    // Verifies Decision 6 R3 composition rule: caller-supplied attrs land
    // in bags.Action; caller wins on conflict for non-reserved keys.
    eng, _, _, _ := newTestEngine(t) // existing helper

    // Register a permit policy conditioned on action.event_type.
    pol := types.SeedPolicy{
        Name:        "test-action-overlay",
        Description: "test policy referencing action.event_type",
        DSLText:     `permit(principal is character, action in ["decrypt"], resource is stream) when { action.event_type == "core-comm:whisper" };`,
        SeedVersion: 1,
    }
    require.NoError(t, eng.RegisterSeedPolicy(t.Context(), pol)) // existing API

    req, err := types.NewAccessRequest(
        "character:01ABC",
        "decrypt",
        "stream:audit",
        map[string]any{"event_type": "core-comm:whisper"},
    )
    require.NoError(t, err)
    decision, err := eng.Evaluate(t.Context(), req)
    require.NoError(t, err)
    assert.True(t, decision.IsAllowed(),
        "caller-supplied action.event_type=core-comm:whisper MUST overlay bags.Action so the policy's when clause matches")
}

func TestEvaluateNilCallerAttributesIsNoOp(t *testing.T) {
    eng, _, _, _ := newTestEngine(t)
    req, err := types.NewAccessRequest("character:01ABC", "read", "location:01XYZ", nil)
    require.NoError(t, err)
    _, err = eng.Evaluate(t.Context(), req)
    require.NoError(t, err)
    // No assertion on Allow/Deny — the test confirms nil attrs do not panic
    // and Resolve still runs to completion.
}
```

If `newTestEngine` or `RegisterSeedPolicy` do not exist with those exact signatures, use the existing `engine_test.go` patterns at lines 2200+ (find the closest existing `TestEvaluate*` test and copy its setup).

- [ ] **Step 2: Run the new tests to verify they fail.**

Run: `task test -- -run TestNewAccessRequest ./internal/access/policy/types/`
Run: `task test -- -run TestEvaluateOverlaysCaller ./internal/access/policy/`

Expected: FAIL — `NewAccessRequest` takes 3 args; `req.Attributes` field doesn't exist; `Evaluate` doesn't overlay.

- [ ] **Step 3: Update `AccessRequest` struct + `NewAccessRequest` + reserved-keys map.**

In `internal/access/policy/types/types.go:107-130`:

```go
// reservedActionKeys lists keys the resolver owns and a caller MUST NOT
// supply via req.Attributes. Phase 3b grounding doc Decision 6 R3.
//
// "name": resolver writes req.Action verb into bags.Action["name"] at
// internal/access/policy/attribute/resolver.go:170. Caller-supplied
// "name" would silently overwrite the resolver value.
var reservedActionKeys = map[string]struct{}{
    "name": {},
}

// AccessRequest represents a subject attempting an action on a resource.
//
// Fields are exported for test assertion readability (mock.MatchedBy comparisons
// and struct literal matching). Production code MUST use NewAccessRequest() which
// validates all fields are non-empty and rejects reserved attribute keys.
//
// Attributes carry caller-supplied per-call context. Composition rule
// (Phase 3b grounding Decision 6 R3): caller-supplied Attributes land
// in bags.Action specifically; caller wins on key conflict; reserved
// keys (currently "name") are rejected at NewAccessRequest precondition.
type AccessRequest struct {
    Subject    string         // "character:01ABC", "plugin:mod-filter", "system"
    Action     string         // "read", "write", "delete", "enter", "execute", "emit", "decrypt"
    Resource   string         // "location:01XYZ", "command:dig", "dek:scene:01ABC"
    Attributes map[string]any // nil = no caller-supplied per-call attributes
}

// NewAccessRequest creates a validated AccessRequest. Returns an error if
// Subject, Action, or Resource is empty, or if attrs contains a reserved
// key. attrs MAY be nil (the common case for callers without per-call
// context to supply).
func NewAccessRequest(subject, action, resource string, attrs map[string]any) (AccessRequest, error) {
    if subject == "" {
        return AccessRequest{}, oops.In("access").With("field", "subject").Errorf("access request: subject must not be empty")
    }
    if action == "" {
        return AccessRequest{}, oops.In("access").With("field", "action").Errorf("access request: action must not be empty")
    }
    if resource == "" {
        return AccessRequest{}, oops.In("access").With("field", "resource").Errorf("access request: resource must not be empty")
    }
    for k := range attrs {
        if _, reserved := reservedActionKeys[k]; reserved {
            return AccessRequest{}, oops.Code("ACCESS_REQUEST_RESERVED_ATTRIBUTE").
                With("key", k).
                Errorf("caller-supplied attribute key %q is server-reserved", k)
        }
    }
    return AccessRequest{
        Subject:    subject,
        Action:     action,
        Resource:   resource,
        Attributes: attrs,
    }, nil
}
```

- [ ] **Step 4: Add the Action-bag overlay in `policy.Engine.Evaluate`.**

In `internal/access/policy/engine.go`, locate the resolver call at line 227 (`bags, resolveErr := e.resolver.Resolve(ctx, req)`). Immediately after the existing fail-closed handling block ends (right before "Step 7: Load snapshot and filter policies" at line 255), add:

```go
// Decision 6 R3: caller-supplied per-call attributes overlay bags.Action.
// Caller wins on non-reserved key conflict. Reserved keys (currently
// "name", which the resolver writes at resolver.go:170) are blocked at
// NewAccessRequest precondition. bags.Action is always non-nil here
// because resolver.go:165 allocates it; the nil-init below is a guard
// for test paths that hand-construct AttributeBags{} with zero-value maps.
if len(req.Attributes) > 0 {
    if bags.Action == nil {
        bags.Action = make(map[string]any, len(req.Attributes))
    }
    for k, v := range req.Attributes {
        bags.Action[k] = v
    }
}
```

- [ ] **Step 5: Update all 25 call sites to pass `nil` 4th arg.**

For each callsite identified by `rg -n 'NewAccessRequest\(' --type go internal/ pkg/ cmd/ test/`, change the call from:

```go
req, err := types.NewAccessRequest(subject, action, resource)
```

to:

```go
req, err := types.NewAccessRequest(subject, action, resource, nil)
```

Mechanical update; do all 25 callsites in the same commit. Production callsites (per substrate verification): `internal/grpc/query_stream_history.go:182`, `internal/plugin/hostfunc/commands.go:180`, `internal/plugin/hostfunc/functions.go:296`, `internal/world/service.go:127`, `internal/command/rate_limit_middleware.go:85`, `internal/command/access.go:25`. Test callsites: `internal/world/service_helpers_test.go:271`, all 6 in `internal/access/policy/policytest/helpers_test.go`, `internal/access/policy/engine_test.go:2219`, both in `internal/access/policy/types/types_test.go`, `test/integration/plugin/abac_widget_test.go`, `test/integration/plugin/binary_plugin_test.go`.

- [ ] **Step 6: Regenerate mockery (bare invocation per `.mockery.yaml`).**

Run: `mockery`

If `mockery` is not on PATH or `.mockery.yaml` is missing, this step is a no-op (the project may not generate mocks for `policy.Engine` at all). Verify by checking `git diff` after running.

- [ ] **Step 7: Run the new tests to verify they pass.**

Run: `task test -- -run TestNewAccessRequest ./internal/access/policy/types/`
Run: `task test -- -run TestEvaluateOverlaysCaller ./internal/access/policy/`

Expected: PASS.

- [ ] **Step 8: Run the full unit-test suite.**

Run: `task test`

Expected: PASS — every existing `NewAccessRequest` callsite now has a `nil` 4th arg; engine merge logic is exercised by the new composition tests.

- [ ] **Step 9: Commit.**

```shell
JJ_EDITOR=true jj --no-pager describe -m "feat(access): AccessRequest gains caller-supplied Attributes (Decision 6) (holomush-ojw1.2)

Phase 3b grounding doc Decision 6 R3. Master spec §7.2 Branch 3 calls
Evaluate with attributes={event_type, plugin_name, plugin_inst}; the
substrate at internal/access/policy/types/types.go:107-130 didn't
carry them. This commit adds AccessRequest.Attributes and a 4th arg
to NewAccessRequest.

Composition rule: caller-supplied req.Attributes overlay bags.Action
specifically (the resolver at attribute/resolver.go:170 already
populates bags.Action[\"name\"] with the action verb; that key is
reserved and blocked at NewAccessRequest). For non-reserved keys,
caller wins on conflict. Other bags (Subject, Resource, Environment)
are server-resolved only.

Pre-0.1.0 status: no shipped consumers. All 25 existing callsites
across 13 files updated to pass nil 4th arg." && jj --no-pager new
```

---

### Task T3: Add `player_character_bindings` substrate (Decision 7)

The binding entity AuthGuard's character branch matches on. Master spec §4.3a defines schema and lifecycle. This task lands the migration, the Go store, the `CharacterRepository.Create` Tx-via-context switchover, and the Tx-wrap of both production character-creation paths.

**Files:**

- Create: `internal/store/migrations/000015_create_player_character_bindings.up.sql`
- Create: `internal/store/migrations/000015_create_player_character_bindings.down.sql`
- Create: `internal/world/postgres/binding_repo.go` — placed in `package postgres` (NOT `internal/store/`) because it needs Tx-via-context composability with `CharacterRepository.Create`, and that requires sharing the package-private `execerFromCtx` helper at `helpers.go:42`. Name `binding_repo.go` mirrors the existing `character_repo.go` pattern.
- Create: `internal/world/postgres/binding_repo_test.go` (or extend an existing test file in the same package)
- Modify: `internal/world/postgres/character_repo.go:46-56` (`Create` switches to `execerFromCtx`)
- Modify: `internal/grpc/auth_handlers.go:369` (`CreateCharacter` Tx-wrap)
- Modify: `internal/auth/guest_service.go:113` (`CreateGuest` Tx-wrap)
- Modify: server-construction wiring (cmd/holomush/main.go or wherever) to inject `*postgres.BindingRepository`

- [ ] **Step 1: Create the migration up-script.**

`internal/store/migrations/000015_create_player_character_bindings.up.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Phase 3b grounding doc Decision 7 / master spec §4.3a.
-- Bindings are long-lived player↔character tenures (weeks/months,
-- spanning many sessions). binding_id is the load-bearing identifier
-- in §7.2 Branch 1 AuthGuard decisions and crypto_keys.participants.

CREATE TABLE player_character_bindings (
    id            TEXT PRIMARY KEY,             -- ULID-format string
    player_id     TEXT NOT NULL REFERENCES players(id),
    character_id  TEXT NOT NULL REFERENCES characters(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at      TIMESTAMPTZ,
    ended_reason  TEXT
);

-- Exactly one active binding per character.
CREATE UNIQUE INDEX idx_pcb_active_per_character
    ON player_character_bindings (character_id) WHERE ended_at IS NULL;

-- Player-side index for "what's this player's active binding for character X" lookups.
CREATE INDEX idx_pcb_player_active
    ON player_character_bindings (player_id) WHERE ended_at IS NULL;

-- Back-population: every existing character with a non-NULL player_id
-- gets a binding row. Orphan characters (player_id IS NULL, permitted
-- by the baseline schema's nullable FK) are excluded; they have no
-- active binding and Subscribe will return BINDING_MISSING for them
-- under Phase 3d's flag flip. Phase 4 wizard-transfer or character
-- deletion will resolve those edge cases.
INSERT INTO player_character_bindings (id, player_id, character_id, created_at, ended_reason)
SELECT
    -- Synthetic ULID via gen_random_uuid → text; we don't have generate_ulid()
    -- as a PG function, so use a deterministic shape: 26-char base32 from
    -- a hash of (player_id || character_id) padded to ULID length. Real
    -- ULID generation happens at INSERT time in Go for new rows; this
    -- back-population just needs unique TEXT values matching the column
    -- type and unique constraint.
    encode(digest(player_id || character_id, 'sha256'), 'hex')::TEXT,
    player_id,
    id,
    NOW(),
    'back_populated_at_migration_000015'
FROM characters
WHERE player_id IS NOT NULL
ON CONFLICT (character_id) WHERE ended_at IS NULL DO NOTHING;
```

**Note on `ended_reason = 'back_populated_at_migration_000015'`:** the back-populated rows have `ended_at = NULL` (active) but ended_reason marks their provenance. This is unusual but useful for forensics. Alternative: leave ended_reason NULL for back-populated rows; the migration can be either way. Pick NULL if simpler.

**Note on the synthesized id:** the SHA-256-hex shape is 64 chars, not 26 (ULID length). Either:

- (a) Use a deterministic hex shape and document that back-populated binding_ids look different from new ULIDs.
- (b) Switch to `gen_random_bytes(10)` + base32 encoding to get a real-ULID-shaped 26-char ID. Requires `pgcrypto` extension (which is already enabled per existing migrations using `digest()` — verify with `rg "CREATE EXTENSION" internal/store/migrations/`).

Pick (b) if `gen_random_bytes` is available; else (a) with a doc-comment explaining the shape difference.

- [ ] **Step 2: Create the migration down-script.**

`internal/store/migrations/000015_create_player_character_bindings.down.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP INDEX IF EXISTS idx_pcb_player_active;
DROP INDEX IF EXISTS idx_pcb_active_per_character;
DROP TABLE IF EXISTS player_character_bindings;
```

- [ ] **Step 3: Write failing tests for the binding store.**

`internal/store/binding_store_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import (
    "context"
    "testing"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/world/postgres"
    "github.com/holomush/holomush/pkg/errutil"
    "github.com/holomush/holomush/test/testutil"
)

func TestBindingRepositoryCreateAndCurrent(t *testing.T) {
    pool := setupTestPool(t)
    bs := postgres.NewBindingRepository(pool)

    // Seed a player + character (use the existing testutil helpers if
    // present; else inline INSERTs).
    playerID, characterID := seedTestPlayerAndCharacter(t, pool)

    bindingID, err := bs.Create(t.Context(), playerID, characterID, "initial_bind")
    require.NoError(t, err)
    require.NotEmpty(t, bindingID)

    got, err := bs.Current(t.Context(), characterID)
    require.NoError(t, err)
    assert.Equal(t, bindingID, got)
}

func TestBindingRepositoryCurrentNotFound(t *testing.T) {
    pool := setupTestPool(t)
    bs := postgres.NewBindingRepository(pool)

    _, err := bs.Current(t.Context(), "01HNONEXISTENTCHAR")
    errutil.AssertErrorCode(t, err, "BINDING_NOT_FOUND")
}

func TestBindingRepositoryEnd(t *testing.T) {
    pool := setupTestPool(t)
    bs := postgres.NewBindingRepository(pool)
    playerID, characterID := seedTestPlayerAndCharacter(t, pool)

    bindingID, err := bs.Create(t.Context(), playerID, characterID, "initial_bind")
    require.NoError(t, err)

    require.NoError(t, bs.End(t.Context(), bindingID, "wizard_transfer"))

    // Current should now return BINDING_NOT_FOUND.
    _, err = bs.Current(t.Context(), characterID)
    errutil.AssertErrorCode(t, err, "BINDING_NOT_FOUND")
}

func TestBindingRepositoryEndAlreadyEnded(t *testing.T) {
    pool := setupTestPool(t)
    bs := postgres.NewBindingRepository(pool)
    playerID, characterID := seedTestPlayerAndCharacter(t, pool)

    bindingID, err := bs.Create(t.Context(), playerID, characterID, "initial_bind")
    require.NoError(t, err)
    require.NoError(t, bs.End(t.Context(), bindingID, "wizard_transfer"))

    err = bs.End(t.Context(), bindingID, "wizard_transfer")
    errutil.AssertErrorCode(t, err, "BINDING_ALREADY_ENDED")
}

// setupTestPool and seedTestPlayerAndCharacter use the existing
// testutil.SharedPostgres + testutil.FreshDatabase pattern from
// test/integration/crypto/emit_test.go:59-61.
func setupTestPool(t *testing.T) *pgxpool.Pool {
    t.Helper()
    shared := testutil.SharedPostgres(t)
    connStr := testutil.FreshDatabase(t, shared)
    ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
    defer cancel()
    pool, err := pgxpool.New(ctx, connStr)
    require.NoError(t, err)
    t.Cleanup(pool.Close)
    return pool
}

func seedTestPlayerAndCharacter(t *testing.T, pool *pgxpool.Pool) (playerID, characterID string) {
    t.Helper()
    playerID = "01HTESTPLAYER000000000000"
    characterID = "01HTESTCHARACTER0000000000"
    _, err := pool.Exec(t.Context(),
        `INSERT INTO players (id, username, password_hash) VALUES ($1, $2, $3)`,
        playerID, "testuser", "stub-hash")
    require.NoError(t, err)
    _, err = pool.Exec(t.Context(),
        `INSERT INTO characters (id, player_id, name) VALUES ($1, $2, $3)`,
        characterID, playerID, "TestCharacter")
    require.NoError(t, err)
    return playerID, characterID
}
```

- [ ] **Step 4: Run the tests to verify they fail.**

Run: `task test:int -- -tags=integration -run TestBindingRepository ./internal/world/postgres/...`

Expected: FAIL — `BindingStore` type and methods don't exist; migration not yet applied so `player_character_bindings` table missing.

- [ ] **Step 5: Implement `internal/world/postgres/binding_repo.go`.**

Place this file in `package postgres` so it can use the existing private `execerFromCtx` helper at `helpers.go:42` (which uses the package-private `txKey{}` value populated by `Transactor.InTransaction` at `transactor.go:34`). Same pattern as `character_repo.go::Delete` at line 76.

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
    "context"
    "errors"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/samber/oops"

    "github.com/holomush/holomush/internal/idgen"
)

// BindingRepository persists player↔character bindings (Phase 3b
// grounding doc Decision 7 / master spec §4.3a). Bindings are long-lived
// tenures (weeks/months, spanning many sessions); binding_id is the
// load-bearing identifier in §7.2 Branch 1 AuthGuard decisions and
// crypto_keys.participants[].binding_id.
//
// Tx-composable via execerFromCtx (helpers.go:42): callers may compose
// Create/End in a transaction opened by Transactor.InTransaction
// (transactor.go:27), participating in the same Tx that creates the
// character row.
type BindingRepository struct {
    pool *pgxpool.Pool
}

// NewBindingRepository wraps a pgxpool.Pool.
func NewBindingRepository(pool *pgxpool.Pool) *BindingRepository {
    return &BindingRepository{pool: pool}
}

// Current returns the active binding_id for characterID. Returns
// BINDING_NOT_FOUND if no active binding exists.
func (s *BindingRepository) Current(ctx context.Context, characterID string) (string, error) {
    var bindingID string
    err := execerFromCtx(ctx, s.pool).QueryRow(ctx,
        `SELECT id FROM player_character_bindings WHERE character_id = $1 AND ended_at IS NULL`,
        characterID,
    ).Scan(&bindingID)
    if err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return "", oops.Code("BINDING_NOT_FOUND").
                With("character_id", characterID).
                Errorf("no active binding for character %s", characterID)
        }
        return "", oops.Code("BINDING_STORE_QUERY_FAILED").Wrap(err)
    }
    return bindingID, nil
}

// Create inserts a new active binding for (playerID, characterID). The
// returned binding_id is a fresh ULID generated via crypto/rand-backed
// idgen.New() (per project's "always use crypto/rand" rule).
//
// Tx-composable: callers may pass a context carrying a pgx.Tx via the
// existing internal/world/postgres.Transactor pattern. The store uses
// execerFromCtx to honor the active Tx if present, falling back to the
// pool otherwise.
func (s *BindingRepository) Create(ctx context.Context, playerID, characterID, reason string) (string, error) {
    if playerID == "" || characterID == "" {
        return "", oops.Code("BINDING_STORE_INVALID_INPUT").
            Errorf("playerID and characterID required")
    }
    bindingID := idgen.New().String()
    _, err := execerFromCtx(ctx, s.pool).Exec(ctx,
        `INSERT INTO player_character_bindings (id, player_id, character_id, ended_reason)
         VALUES ($1, $2, $3, $4)`,
        bindingID, playerID, characterID, nilIfEmpty(reason),
    )
    if err != nil {
        return "", oops.Code("BINDING_STORE_INSERT_FAILED").
            With("character_id", characterID).
            With("player_id", playerID).
            Wrap(err)
    }
    return bindingID, nil
}

// End marks a binding as ended. Returns BINDING_NOT_FOUND if the
// binding doesn't exist; BINDING_ALREADY_ENDED if it's already ended.
func (s *BindingRepository) End(ctx context.Context, bindingID, reason string) error {
    cmdTag, err := execerFromCtx(ctx, s.pool).Exec(ctx,
        `UPDATE player_character_bindings
         SET ended_at = now(), ended_reason = $2
         WHERE id = $1 AND ended_at IS NULL`,
        bindingID, reason,
    )
    if err != nil {
        return oops.Code("BINDING_STORE_UPDATE_FAILED").Wrap(err)
    }
    if cmdTag.RowsAffected() == 0 {
        // Distinguish BINDING_NOT_FOUND from BINDING_ALREADY_ENDED
        // via a follow-up SELECT. Acceptable cost; End is rare.
        var alreadyEnded bool
        if scanErr := execerFromCtx(ctx, s.pool).QueryRow(ctx,
            `SELECT ended_at IS NOT NULL FROM player_character_bindings WHERE id = $1`,
            bindingID,
        ).Scan(&alreadyEnded); scanErr != nil {
            if errors.Is(scanErr, pgx.ErrNoRows) {
                return oops.Code("BINDING_NOT_FOUND").
                    With("binding_id", bindingID).
                    Errorf("binding %s not found", bindingID)
            }
            return oops.Code("BINDING_STORE_QUERY_FAILED").Wrap(scanErr)
        }
        if alreadyEnded {
            return oops.Code("BINDING_ALREADY_ENDED").
                With("binding_id", bindingID).
                Errorf("binding %s already ended", bindingID)
        }
    }
    return nil
}

func nilIfEmpty(s string) any {
    if s == "" {
        return nil
    }
    return s
}
```

The `execerFromCtx` helper used above is the existing one at `helpers.go:42` (package-private to `postgres`); placing `binding_repo.go` in the same package gives direct access. The `txKey{}` value used by `helpers.go:33` is the same one populated by `Transactor.InTransaction` at `transactor.go:34`, so a caller that opens a Tx via `transactor.InTransaction(ctx, fn)` will see `binding.Create(txCtx, ...)` participate in that same transaction transparently.

- [ ] **Step 6: Apply the migration on the test database.**

The existing test infrastructure auto-applies migrations on `FreshDatabase`. No explicit migration step needed in the test code.

- [ ] **Step 7: Run the integration tests to verify they pass.**

Run: `task test:int -- -tags=integration -run TestBindingRepository ./internal/world/postgres/...`

Expected: PASS.

- [ ] **Step 8: Modify `character_repo.go::Create` to use `execerFromCtx`.**

In `internal/world/postgres/character_repo.go:46-56`, change:

```go
func (r *CharacterRepository) Create(ctx context.Context, char *world.Character) error {
    _, err := r.pool.Exec(ctx, `
        INSERT INTO characters ...
    `, ...)
```

to:

```go
func (r *CharacterRepository) Create(ctx context.Context, char *world.Character) error {
    _, err := execerFromCtx(ctx, r.pool).Exec(ctx, `
        INSERT INTO characters (id, player_id, name, description, location_id, created_at)
        VALUES ($1, $2, $3, $4, $5, $6)
    `, char.ID.String(), char.PlayerID.String(), char.Name, char.Description,
        ulidToStringPtr(char.LocationID), char.CreatedAt)
```

One-line change; mirrors the existing `Delete` pattern at `character_repo.go:76`. Existing callers passing non-Tx contexts continue to work (`execerFromCtx` falls back to pool per `helpers.go:42`).

- [ ] **Step 9: Run the existing character-repo tests.**

Run: `task test -- ./internal/world/postgres/...`

Expected: PASS — the change is non-breaking.

- [ ] **Step 10: Wrap `internal/grpc/auth_handlers.go::CreateCharacter` (line 369) in a transaction.**

In `internal/grpc/auth_handlers.go::CreateCharacter`, locate the existing `characterService.Create` call. Wrap it plus a new `bindingStore.Create` call inside `transactor.InTransaction`:

```go
// Existing: char, err := s.characterService.Create(ctx, playerID, name)
// becomes:
var char *world.Character
err = s.transactor.InTransaction(ctx, func(txCtx context.Context) error {
    var createErr error
    char, createErr = s.characterService.Create(txCtx, playerID, name)
    if createErr != nil {
        return createErr
    }
    if _, bindErr := s.bindings.Create(txCtx, playerID.String(), char.ID.String(), "initial_bind"); bindErr != nil {
        return oops.Code("CHARACTER_CREATE_BINDING_FAILED").Wrap(bindErr)
    }
    return nil
})
if err != nil {
    return nil, /* existing error wrapping */
}
```

Add `transactor *postgres.Transactor` and `bindings *postgres.BindingRepository` fields to `CoreServer`; update the constructor.

- [ ] **Step 11: Wrap `internal/auth/guest_service.go:113` (`CreateGuest`) in a transaction.**

In `internal/auth/guest_service.go::CreateGuest`, the current shape (lines 96-121) is:

```go
char, err := world.NewCharacter(player.ID, charName)
// ... LocationID set ...
if err = s.chars.Create(ctx, char); err != nil {
    s.namer.ReleaseGuest(name)
    if delErr := s.players.Delete(ctx, player.ID); delErr != nil {
        // best-effort cleanup
    }
    return nil, oops.Code("GUEST_CREATE_FAILED")...
}
```

Replace with a transactional shape:

```go
char, err := world.NewCharacter(player.ID, charName)
if err != nil { /* existing handling */ }
char.LocationID = &startLoc

// Wrap players.Create + chars.Create + bindings.Create in one Tx.
// Best-effort cleanup at line 116 is replaced by Tx rollback.
err = s.transactor.InTransaction(ctx, func(txCtx context.Context) error {
    // Note: s.players.Create was called earlier (line 88-94 in current shape).
    // Re-arrange so that ALL three INSERTs land inside the same Tx:
    // move s.players.Create here too. The Tx scope is players + characters
    // + bindings; rollback on any failure is symmetric.
    if pErr := s.players.Create(txCtx, player); pErr != nil {
        return pErr
    }
    if cErr := s.chars.Create(txCtx, char); cErr != nil {
        return cErr
    }
    if _, bErr := s.bindings.Create(txCtx, player.ID.String(), char.ID.String(), "initial_bind_guest"); bErr != nil {
        return bErr
    }
    return nil
})
if err != nil {
    s.namer.ReleaseGuest(name)
    return nil, oops.Code("GUEST_CREATE_FAILED").
        With("name", name).
        Wrap(err)
}
```

(The exact placement depends on the current shape of `CreateGuest`; preserve existing semantics — the players.Create call may need to move into the Tx block. Read the full function before editing.)

Add `transactor *postgres.Transactor` and `bindings *postgres.BindingRepository` fields to `GuestService`; update its constructor.

- [ ] **Step 12: Wire BindingStore + Transactor into server construction.**

In `cmd/holomush/main.go` (or wherever `CoreServer` and `GuestService` are constructed), inject the new dependencies. The exact location depends on the existing wiring.

- [ ] **Step 13: Run the full test suite.**

Run: `task test && task test:int`

Expected: PASS — character-creation paths now atomically create the binding alongside the character; existing tests still pass because the change is additive.

- [ ] **Step 14: Commit.**

```shell
JJ_EDITOR=true jj --no-pager describe -m "feat(store): player_character_bindings substrate (Decision 7) (holomush-ojw1.2)

Phase 3b grounding doc Decision 7 / master spec §4.3a. Bindings are
long-lived player↔character tenures (weeks/months), distinct from
ephemeral sessions. binding_id is load-bearing in §7.2 Branch 1
AuthGuard decisions and crypto_keys.participants[].binding_id.

Migration 000015 creates player_character_bindings with a partial
UNIQUE index on (character_id) WHERE ended_at IS NULL, enforcing
exactly-one-active-binding-per-character. Back-population covers
existing characters with non-NULL player_id; orphans (NULL player_id)
are excluded and resolve to BINDING_MISSING under Phase 3d's flag flip.

internal/store/binding_store.go ships Current/Create/End methods using
execerFromCtx for Tx composition. Both production character-creation
paths (auth_handlers.go::CreateCharacter Path A; guest_service.go::CreateGuest
Path B from PR #181) wrap character + binding INSERTs in a transaction
via transactor.InTransaction. CharacterRepository.Create switches to
execerFromCtx (one-line change matching existing Delete pattern).

The orphan-character case (one row in characters, zero rows in
player_character_bindings) is structurally impossible for newly-created
characters." && jj --no-pager new
```

---

### Task T4: Add `dek.Manager.Participants` method (Decision 1 substrate prereq)

The data is already loaded — `internal/eventbus/crypto/dek/store.go:48-59` shows `row.Participants []Participant` is read on every `Resolve`. This task surfaces it as a `Manager` method without a second SELECT. Per plan-reviewer Round 1 Finding #4, the original "fake-based unit test" approach is dropped (because `dek.Store` is a concrete struct, not an interface, and inventing fakes is not viable); we rely on the integration test only.

**Files:**

- Modify: `internal/eventbus/crypto/dek/manager.go:25-35` (interface), `:38-65` (production impl)
- Modify: `internal/eventbus/crypto/dek/manager_integration_test.go` (integration test)

- [ ] **Step 1: Write a failing integration test for `Participants`.**

In `internal/eventbus/crypto/dek/manager_integration_test.go`, add:

```go
//go:build integration

func TestManagerParticipantsRoundTrip(t *testing.T) {
    pool := newTestPool(t) // existing helper
    cache := NewCache(CacheConfig{Capacity: 64})
    provider := newTestKEKProvider(t) // existing helper
    store := NewStore(pool)

    mgr, err := NewManager(provider, store, cache)
    require.NoError(t, err)

    initial := []Participant{
        {PlayerID: "01ABC", CharacterID: "01XYZ", BindingID: "01DEF", JoinedAt: time.Now()},
        {PlayerID: "01GHI", CharacterID: "01JKL", BindingID: "01MNO", JoinedAt: time.Now()},
    }
    key, err := mgr.GetOrCreate(t.Context(), ContextID{Type: "scene", ID: "01HXX"}, initial)
    require.NoError(t, err)

    parts, err := mgr.Participants(t.Context(), key.ID, key.Version)
    require.NoError(t, err)
    require.Len(t, parts, 2)
    assert.Equal(t, "01ABC", parts[0].PlayerID)
    assert.Equal(t, "01DEF", parts[0].BindingID)
    assert.Equal(t, "01GHI", parts[1].PlayerID)
}

func TestManagerParticipantsNotFound(t *testing.T) {
    pool := newTestPool(t)
    cache := NewCache(CacheConfig{Capacity: 64})
    provider := newTestKEKProvider(t)
    store := NewStore(pool)

    mgr, err := NewManager(provider, store, cache)
    require.NoError(t, err)

    _, err = mgr.Participants(t.Context(), codec.KeyID(99999), 1)
    errutil.AssertErrorCode(t, err, "DEK_NOT_FOUND")
}

func TestManagerParticipantsFromUnitTestStubReturnsNotConfigured(t *testing.T) {
    mgr := NewManagerForUnitTest()
    _, err := mgr.Participants(t.Context(), codec.KeyID(1), 1)
    errutil.AssertErrorCode(t, err, "DEK_MANAGER_NOT_CONFIGURED")
}
```

Use the existing `newTestPool` and `newTestKEKProvider` helpers in `manager_integration_test.go`. If they don't exist with those exact names, find the closest existing helpers in the same file.

- [ ] **Step 2: Run the test to verify it fails.**

Run: `task test:int -- -tags=integration -run TestManagerParticipants ./internal/eventbus/crypto/dek/...`

Expected: FAIL — `Manager.Participants` does not exist.

- [ ] **Step 3: Add `Participants` to the `Manager` interface.**

In `internal/eventbus/crypto/dek/manager.go:25-35`:

```go
type Manager interface {
    GetOrCreate(ctx context.Context, ctxID ContextID, initial []Participant) (codec.Key, error)
    Resolve(ctx context.Context, keyID codec.KeyID, version uint32) (codec.Key, error)

    // Participants returns the participant set for a (keyID, version) DEK.
    // Read by AuthGuard via the ParticipantLookup adapter (Phase 3b
    // grounding doc Decision 1). Phase 3b uses fetch-fresh-on-every-call;
    // caching lands in Phase 3c (DEK cache invalidation, holomush-ojw1.3).
    Participants(ctx context.Context, keyID codec.KeyID, version uint32) ([]Participant, error)

    // Phase 4 stub — see holomush-fi0n.
    Add(ctx context.Context, ctxID ContextID, p Participant) error
    Rotate(ctx context.Context, ctxID ContextID, newParticipants []Participant, reason string) error

    // Phase 5 stub — see holomush-jxo8.
    Rekey(ctx context.Context, ctxID ContextID, justification string, ops OperatorFactors) error
}
```

- [ ] **Step 4: Implement `Participants` on the production `manager`.**

After the existing `Resolve` method in `internal/eventbus/crypto/dek/manager.go`, add:

```go
// Participants returns the participant list for the row identified by
// (keyID, version). Reads via store.selectByID — same path as Resolve —
// but returns the Participants field rather than unwrapping the DEK.
// AuthGuard never holds DEK material; ParticipantLookup is the right
// boundary.
//
// Two-SELECT note: AuthGuard.Check calls Participants, then on permit
// calls Resolve. Resolve hits the cache; Participants does NOT.
// Phase 3b accepts this redundancy; Phase 3c (holomush-ojw1.3) revisits
// caching policy. TOCTOU concern: if Rotate happens between the two
// calls, AuthGuard checks new participants but Resolve returns the
// (now-stale) cached old key. Phase 3c's cache invalidation closes
// this; Phase 3b's Rotate is stubbed (lifecycle ops are Phase 4),
// so the TOCTOU is vacuous in 3b production.
func (m *manager) Participants(ctx context.Context, keyID codec.KeyID, version uint32) ([]Participant, error) {
    if err := m.configured(); err != nil {
        return nil, err
    }
    r, err := m.store.selectByID(ctx, keyID, version)
    if err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return nil, oops.Code("DEK_NOT_FOUND").
                With("key_id", uint64(keyID)).
                With("version", version).
                Errorf("crypto_keys row %d v%d not found", keyID, version)
        }
        return nil, oops.Code("DEK_STORE_SELECT_FAILED").Wrap(err)
    }
    return r.Participants, nil
}
```

- [ ] **Step 5: Verify `NewManagerForUnitTest` stub already returns `DEK_MANAGER_NOT_CONFIGURED`.**

The existing `configured()` guard at `manager.go:79-83` returns `DEK_MANAGER_NOT_CONFIGURED` when any of `provider`, `store`, `cache` is nil. The `NewManagerForUnitTest` helper at `manager.go:67-73` returns `&manager{}` with all collaborators nil. The new `Participants` method calls `m.configured()` first and propagates the error — no separate stub method needed.

- [ ] **Step 6: Run the integration test to verify it passes.**

Run: `task test:int -- -tags=integration -run TestManagerParticipants ./internal/eventbus/crypto/dek/...`

Expected: PASS.

- [ ] **Step 7: Run the full test suite.**

Run: `task test && task test:int`

Expected: PASS.

- [ ] **Step 8: Commit.**

```shell
JJ_EDITOR=true jj --no-pager describe -m "feat(crypto): dek.Manager.Participants for AuthGuard ParticipantLookup (holomush-ojw1.2)

Phase 3b grounding doc Decision 1 substrate prerequisite. AuthGuard's
character (Branch 1) and player (Branch 2) branches need the DEK
participant set to evaluate membership. The data is already loaded
on every Resolve via store.selectByID returning row.Participants;
this method surfaces it without a second SELECT.

Phase 3b uses fetch-fresh-on-every-call. Caching with cross-replica
invalidation lands in Phase 3c (holomush-ojw1.3, INV-28/INV-29);
participant-data staleness under concurrent Add/Rotate is Phase 4
(holomush-fi0n) territory.

dek.Store is a concrete struct (not interface), so unit tests with
fake stores are not viable; integration tests using the existing
newTestPool harness are the test approach (per plan-reviewer Round 1
Finding #4)." && jj --no-pager new
```

---

### Task T5: Add `EventFrame.metadata_only` proto + `Delivery.MetadataOnly()` interface (Decision 4)

`metadata_only` is a per-delivery flag. Master spec §4.1 mis-located it on `eventbusv1.Event`; the right home is `corev1.EventFrame` (per-delivery shape, sibling to `cursor`). The Go-side `eventbus.Delivery` interface gains `MetadataOnly() bool`; `jetStreamDelivery` carries the flag as a struct field.

**Files:**

- Modify: `api/proto/holomush/core/v1/core.proto:136-216` (`EventFrame`)
- Regenerate: `pkg/proto/holomush/core/v1/core.pb.go` via `task proto`
- Modify: `internal/eventbus/bus.go:43-52` (`Delivery` interface)
- Modify: `internal/eventbus/subscriber.go:341-352` (`jetStreamDelivery` struct + accessor)

- [ ] **Step 1: Write a failing test.**

In a new `internal/eventbus/delivery_metadata_only_test.go`:

```go
package eventbus_test

import (
    "testing"

    "github.com/stretchr/testify/assert"

    corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

func TestEventFrameCarriesMetadataOnlyFlag(t *testing.T) {
    frame := &corev1.EventFrame{
        Id:           "01HXX",
        Stream:       "events.scene.01ABC.ic",
        Type:         "core-comm:whisper",
        MetadataOnly: true,
    }
    assert.True(t, frame.GetMetadataOnly())
}
```

- [ ] **Step 2: Run the test to verify it fails.**

Run: `task test -- -run TestEventFrameCarriesMetadataOnlyFlag ./internal/eventbus/`

Expected: FAIL — `EventFrame.MetadataOnly` field does not exist.

- [ ] **Step 3: Add the proto field.**

In `api/proto/holomush/core/v1/core.proto:136-154`, after `RenderingMetadata rendering = 9;`, add:

```protobuf
  // metadata_only flags a delivery whose plaintext was withheld by the
  // host's AuthGuard (Phase 3b decrypt path). When true, payload is
  // empty bytes and the recipient was either not in the DEK's
  // participant set, lacked the requisite plugin manifest declaration /
  // ABAC grant, or hit the audit-emit backpressure throttle.
  // metadata_only=false on every legitimate delivery (including
  // legitimately-empty-payload events like a presence event with no content).
  //
  // Set by the host's Subscribe / QueryStreamHistory handler at fan-out
  // time (Phase 3b grounding doc Decision 4). NEVER set by emitters;
  // NEVER persisted to events_audit (storage rows always carry the
  // sender's payload, ciphertext or cleartext).
  bool metadata_only = 10;
```

- [ ] **Step 4: Regenerate Go bindings.**

Run: `task proto`

Expected: `pkg/proto/holomush/core/v1/core.pb.go` regenerated with `MetadataOnly bool` field.

- [ ] **Step 5: Run the test to verify it passes.**

Run: `task test -- -run TestEventFrameCarriesMetadataOnlyFlag ./internal/eventbus/`

Expected: PASS.

- [ ] **Step 6: Add `MetadataOnly() bool` to the `Delivery` interface.**

In `internal/eventbus/bus.go:43-52`:

```go
type Delivery interface {
    Event() Event
    // MetadataOnly reports whether the host's AuthGuard withheld plaintext
    // from this recipient. When true, Event().Payload is empty bytes.
    // The gRPC Subscribe handler reads this and stamps
    // EventFrame.metadata_only on the wire (Phase 3b grounding doc
    // Decision 4). False for identity-codec events and for legitimately
    // empty-payload sensitive events that were authorized.
    MetadataOnly() bool
    Ack() error
    Nack() error
    InProgress() error
}
```

- [ ] **Step 7: Add `metadataOnly` field + accessor on `jetStreamDelivery`.**

In `internal/eventbus/subscriber.go:341-352`:

```go
type jetStreamDelivery struct {
    msg          jetstream.Msg
    event        Event
    metadataOnly bool // stamped by decodeDelivery when AuthGuard denies (T9)
}

func (d *jetStreamDelivery) Event() Event       { return d.event }
func (d *jetStreamDelivery) MetadataOnly() bool { return d.metadataOnly }
func (d *jetStreamDelivery) Ack() error         { return oops.Wrap(d.msg.Ack()) }
func (d *jetStreamDelivery) Nack() error        { return oops.Wrap(d.msg.Nak()) }
func (d *jetStreamDelivery) InProgress() error {
    return oops.Wrap(d.msg.InProgress())
}
```

- [ ] **Step 8: Update existing Delivery mocks.**

Run: `rg -ln "Delivery interface|MockDelivery|fakeDelivery|stubDelivery" --type go`

For each match, add `MetadataOnly() bool { return false }` to satisfy the new interface.

- [ ] **Step 9: Run the full test suite.**

Run: `task test`

Expected: PASS — all callers see `MetadataOnly()` returning false (correct — T9 will start stamping true).

- [ ] **Step 10: Commit.**

```shell
JJ_EDITOR=true jj --no-pager describe -m "feat(eventbus): EventFrame.metadata_only + Delivery.MetadataOnly() (Decision 4) (holomush-ojw1.2)

Phase 3b grounding doc Decision 4. metadata_only is a per-delivery flag,
not a property of the event itself. Master spec §4.1 mis-located it on
eventbusv1.Event (storage shape) — fixed by placing it on
corev1.EventFrame (per-delivery wire shape, sibling to cursor; field 7
on Event is already taken by rendering anyway).

Go-side: Delivery interface gains MetadataOnly() bool; jetStreamDelivery
carries the flag as a struct field. T9's decode path will stamp it true
at the AuthGuard-denial point. The gRPC handler is a thin
proto-translation layer that reads delivery.MetadataOnly() and writes
EventFrame.metadata_only on the wire." && jj --no-pager new
```

---

### Task T6: Add `*plugin.Manager.PluginRequestsDecryption` substrate (Decision 1 prereq)

AuthGuard's plugin branch (Branch 3) needs a way to ask "does plugin P's manifest declare event_type E in `crypto.consumes.requests_decryption`?" Phase 1 shipped `LookupEmitSensitivity` for the `crypto.emits` side; the `crypto.consumes.requests_decryption` side has no registry-level lookup yet. This task adds it.

**Files:**

- Modify: `internal/plugin/manager.go` (add exported method)
- Create: `internal/plugin/crypto_manifest_lookup_test.go` (or extend existing manifest tests)

- [ ] **Step 1: Write a failing test.**

In `internal/plugin/crypto_manifest_lookup_test.go` (new file):

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    plugins "github.com/holomush/holomush/internal/plugin"
)

func TestManagerPluginRequestsDecryptionMatchesQualifiedRef(t *testing.T) {
    // mod-filter declares: consumes [{ requests_decryption: ["core-comm:whisper"] }]
    mgr := newTestManagerWithManifest(t, &plugins.Manifest{
        Name:         "mod-filter",
        Dependencies: map[string]string{"core-comm": "1.0.0"},
        Crypto: &plugins.CryptoSection{
            Consumes: []plugins.CryptoConsume{
                {
                    Subjects:           []string{"events.>"},
                    RequestsDecryption: []string{"core-comm:whisper"},
                },
            },
        },
    })

    assert.True(t, mgr.PluginRequestsDecryption("mod-filter", "core-comm:whisper"))
    assert.False(t, mgr.PluginRequestsDecryption("mod-filter", "core-comm:undeclared"))
    assert.False(t, mgr.PluginRequestsDecryption("nonexistent-plugin", "core-comm:whisper"))
}

func TestManagerPluginRequestsDecryptionFalseForNoCryptoSection(t *testing.T) {
    mgr := newTestManagerWithManifest(t, &plugins.Manifest{
        Name: "no-crypto-plugin",
        // No Crypto section
    })
    assert.False(t, mgr.PluginRequestsDecryption("no-crypto-plugin", "anything:event"))
}

// newTestManagerWithManifest returns a *plugins.Manager with the given
// manifest registered in its loaded map, suitable for unit-testing
// manifest-lookup methods. Uses whatever existing test pattern the
// plugin package uses for unit tests; check internal/plugin/manager_test.go
// for the convention.
func newTestManagerWithManifest(t *testing.T, m *plugins.Manifest) *plugins.Manager {
    t.Helper()
    // Implementation depends on existing Manager construction; check
    // internal/plugin/manager.go and existing tests for the pattern.
    return /* construct Manager with m in its loaded plugins */
}
```

The exact `newTestManagerWithManifest` helper depends on the existing `*plugins.Manager` construction; verify the actual pattern at `internal/plugin/manager.go` and existing tests before implementing.

- [ ] **Step 2: Run the test to verify it fails.**

Run: `task test -- -run TestManagerPluginRequestsDecryption ./internal/plugin/...`

Expected: FAIL — method doesn't exist.

- [ ] **Step 3: Implement the method.**

In `internal/plugin/manager.go`, add (near the existing `lookupManifest` private method around line 1205):

```go
// PluginRequestsDecryption returns true iff the plugin named pluginName
// has a manifest declaring `eventType` in its
// `crypto.consumes[].requests_decryption[]` list. The eventType MUST be
// in the qualified `<plugin>:<event_type>` form per crypto_validator.go:60-93.
//
// Read by AuthGuard via the ManifestLookup adapter (Phase 3b grounding
// doc Decision 1 / plan-reviewer Round 1 Finding #2).
func (m *Manager) PluginRequestsDecryption(pluginName, eventType string) bool {
    manifest := m.lookupManifest(pluginName)
    if manifest == nil || manifest.Crypto == nil {
        return false
    }
    for _, consume := range manifest.Crypto.Consumes {
        for _, ref := range consume.RequestsDecryption {
            if ref == eventType {
                return true
            }
        }
    }
    return false
}
```

(Reuse the existing `lookupManifest` pattern. If the field name `loaded` differs, adapt.)

- [ ] **Step 4: Run the test to verify it passes.**

Run: `task test -- -run TestManagerPluginRequestsDecryption ./internal/plugin/...`

Expected: PASS.

- [ ] **Step 5: Run the full test suite.**

Run: `task test`

Expected: PASS.

- [ ] **Step 6: Commit.**

```shell
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin): Manager.PluginRequestsDecryption substrate (Decision 1) (holomush-ojw1.2)

Phase 3b grounding doc Decision 1 substrate prerequisite per
plan-reviewer Round 1 Finding #2. AuthGuard's plugin branch consults
this method to check whether a plugin's manifest declares
requests_decryption for a given qualified event_type. Mirrors the
shape of the existing LookupEmitSensitivity helper (Phase 1) for the
emits side." && jj --no-pager new
```

---

### Task T7: Create `internal/eventbus/authguard/` package — types, interfaces, four-branch Guard impl (Decision 1)

Core authorization-policy package. Typed `Identity`, typed `Decision`, single `AuthGuard.Check(ctx, req)` interface, four small dependency interfaces, and the production `Guard` impl with the §7.2 four-branch decision tree.

**Files:**

- Create: `internal/eventbus/authguard/authguard.go`
- Create: `internal/eventbus/authguard/identity.go`
- Create: `internal/eventbus/authguard/identity_test.go`
- Create: `internal/eventbus/authguard/guard.go`
- Create: `internal/eventbus/authguard/guard_test.go`
- Create: `internal/eventbus/authguard/adapter_dek.go`
- Create: `internal/eventbus/authguard/adapter_dek_test.go`
- Create: `internal/eventbus/authguard/adapter_manifest.go`
- Create: `internal/eventbus/authguard/adapter_manifest_test.go`

- [ ] **Step 1: Write failing constructor tests.**

`internal/eventbus/authguard/identity_test.go`:

```go
package authguard_test

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/eventbus/authguard"
    "github.com/holomush/holomush/pkg/errutil"
)

func TestNewCharacterIdentityRequiresAllThreeIDs(t *testing.T) {
    cases := []struct {
        name        string
        playerID    string
        characterID string
        bindingID   string
    }{
        {"empty player rejected", "", "01XYZ", "01DEF"},
        {"empty character rejected", "01ABC", "", "01DEF"},
        {"empty binding rejected", "01ABC", "01XYZ", ""},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            _, err := authguard.NewCharacterIdentity(tc.playerID, tc.characterID, tc.bindingID)
            errutil.AssertErrorCode(t, err, "AUTHGUARD_IDENTITY_INVALID")
        })
    }
}

func TestNewCharacterIdentityHappyPath(t *testing.T) {
    id, err := authguard.NewCharacterIdentity("01ABC", "01XYZ", "01DEF")
    require.NoError(t, err)
    assert.Equal(t, authguard.IdentityKindCharacter, id.Kind)
    assert.Equal(t, "01ABC", id.PlayerID)
    assert.Equal(t, "01XYZ", id.CharacterID)
    assert.Equal(t, "01DEF", id.BindingID)
}

func TestNewPlayerIdentityRequiresPlayerID(t *testing.T) {
    _, err := authguard.NewPlayerIdentity("")
    errutil.AssertErrorCode(t, err, "AUTHGUARD_IDENTITY_INVALID")

    id, err := authguard.NewPlayerIdentity("01ABC")
    require.NoError(t, err)
    assert.Equal(t, authguard.IdentityKindPlayer, id.Kind)
    assert.Equal(t, "01ABC", id.PlayerID)
}

func TestNewPluginIdentityRequiresBoth(t *testing.T) {
    _, err := authguard.NewPluginIdentity("", "01INST")
    errutil.AssertErrorCode(t, err, "AUTHGUARD_IDENTITY_INVALID")
    _, err = authguard.NewPluginIdentity("mod-filter", "")
    errutil.AssertErrorCode(t, err, "AUTHGUARD_IDENTITY_INVALID")

    id, err := authguard.NewPluginIdentity("mod-filter", "01INST")
    require.NoError(t, err)
    assert.Equal(t, authguard.IdentityKindPlugin, id.Kind)
    assert.Equal(t, "mod-filter", id.PluginName)
    assert.Equal(t, "01INST", id.InstanceID)
}

func TestNewOperatorIdentityCarriesNoIDs(t *testing.T) {
    id := authguard.NewOperatorIdentity()
    assert.Equal(t, authguard.IdentityKindOperator, id.Kind)
    assert.Empty(t, id.PlayerID)
    assert.Empty(t, id.CharacterID)
}
```

- [ ] **Step 2: Run the constructor tests to verify they fail (package missing).**

Run: `task test -- -run TestNew.*Identity ./internal/eventbus/authguard/...`

Expected: FAIL — package not found.

- [ ] **Step 3: Create `internal/eventbus/authguard/authguard.go`.**

Full package types + interfaces. (Code identical to grounding doc Decision 1 sketch — refer to the spec for the exact `Identity`, `IdentityKind`, `CheckRequest`, `DecisionCode`, `Decision`, `AuthGuard`, `ParticipantLookup`, `ManifestLookup`, `ABACEngine`, `BackpressureChecker` shapes. Reproduced here for plan completeness.)

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package authguard provides the policy-evaluation seam for sensitive
// event delivery. AuthGuard combines DEK participant-set membership,
// plugin manifest declarations, and ABAC grants into a single typed
// Decision per Phase 3b grounding doc Decision 1.
package authguard

import (
    "context"

    "github.com/oklog/ulid/v2"

    "github.com/holomush/holomush/internal/eventbus/codec"
    "github.com/holomush/holomush/internal/eventbus/crypto/dek"
    accesstypes "github.com/holomush/holomush/internal/access/policy/types"
)

type IdentityKind int

const (
    IdentityKindUnknown IdentityKind = iota
    IdentityKindCharacter
    IdentityKindPlayer
    IdentityKindPlugin
    IdentityKindOperator
)

// Identity is the typed authenticated principal AuthGuard evaluates.
// Named "Identity" rather than "Subject" because eventbus.Subject already
// exists at internal/eventbus/types.go:16 as the JetStream subject filter
// type. binding_id semantics: see master spec §4.3a — long-lived
// player↔character tenure, NOT session.ID.
type Identity struct {
    Kind        IdentityKind
    PlayerID    string
    CharacterID string
    BindingID   string
    PluginName  string
    InstanceID  string
}

type CheckRequest struct {
    Identity   Identity
    KeyID      codec.KeyID
    KeyVersion uint32
    EventType  string
    EventID    ulid.ULID
}

type DecisionCode int

const (
    DecisionCodeUnknown DecisionCode = iota
    PermitParticipant
    PermitPlayerHistory
    PermitPluginGrant
    DenyNotParticipant
    DenyPlayerNeverParticipated
    DenyPlayerNoABACGrant
    DenyManifestDeclarationMissing
    DenyNoABACGrant
    DenyOperatorUseAdminRPC
    DenyAuditBackpressure
    DenyUnknownIdentityKind
)

type Decision struct {
    Permit       bool
    Code         DecisionCode
    GrantID      ulid.ULID
    Reason       string
    ABACDecision *accesstypes.Decision
}

func (d Decision) Permitted() bool { return d.Permit }

type AuthGuard interface {
    Check(ctx context.Context, req CheckRequest) (Decision, error)
}

type ParticipantLookup interface {
    Participants(ctx context.Context, keyID codec.KeyID, version uint32) ([]dek.Participant, error)
}

type ManifestLookup interface {
    PluginRequestsDecryption(pluginName, eventType string) bool
}

type ABACEngine interface {
    Evaluate(ctx context.Context, req accesstypes.AccessRequest) (accesstypes.Decision, error)
}

type BackpressureChecker interface {
    ShouldThrottle(pluginName string) bool
}
```

- [ ] **Step 4: Create `internal/eventbus/authguard/identity.go`.**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package authguard

import "github.com/samber/oops"

func NewCharacterIdentity(playerID, characterID, bindingID string) (Identity, error) {
    if playerID == "" {
        return Identity{}, oops.Code("AUTHGUARD_IDENTITY_INVALID").
            With("kind", "character").With("field", "playerID").
            Errorf("character identity requires non-empty playerID")
    }
    if characterID == "" {
        return Identity{}, oops.Code("AUTHGUARD_IDENTITY_INVALID").
            With("kind", "character").With("field", "characterID").
            Errorf("character identity requires non-empty characterID")
    }
    if bindingID == "" {
        return Identity{}, oops.Code("AUTHGUARD_IDENTITY_INVALID").
            With("kind", "character").With("field", "bindingID").
            Errorf("character identity requires non-empty bindingID")
    }
    return Identity{
        Kind:        IdentityKindCharacter,
        PlayerID:    playerID,
        CharacterID: characterID,
        BindingID:   bindingID,
    }, nil
}

func NewPlayerIdentity(playerID string) (Identity, error) {
    if playerID == "" {
        return Identity{}, oops.Code("AUTHGUARD_IDENTITY_INVALID").
            With("kind", "player").With("field", "playerID").
            Errorf("player identity requires non-empty playerID")
    }
    return Identity{Kind: IdentityKindPlayer, PlayerID: playerID}, nil
}

func NewPluginIdentity(pluginName, instanceID string) (Identity, error) {
    if pluginName == "" {
        return Identity{}, oops.Code("AUTHGUARD_IDENTITY_INVALID").
            With("kind", "plugin").With("field", "pluginName").
            Errorf("plugin identity requires non-empty pluginName")
    }
    if instanceID == "" {
        return Identity{}, oops.Code("AUTHGUARD_IDENTITY_INVALID").
            With("kind", "plugin").With("field", "instanceID").
            Errorf("plugin identity requires non-empty instanceID")
    }
    return Identity{Kind: IdentityKindPlugin, PluginName: pluginName, InstanceID: instanceID}, nil
}

func NewOperatorIdentity() Identity {
    return Identity{Kind: IdentityKindOperator}
}
```

- [ ] **Step 5: Run identity tests.**

Run: `task test -- -run TestNew.*Identity ./internal/eventbus/authguard/...`

Expected: PASS.

- [ ] **Step 6: Write failing Guard tests for each §7.2 branch.**

`internal/eventbus/authguard/guard_test.go` (uses existing `policytest` mocks where possible to avoid invented fixtures). Reproduce the four-branch test sketch from grounding doc Decision 1, with one critical correction: use `accesstypes.NewDecision` to construct test ABAC results, and use `idgen.New()` (not `ulid.Make()`) where the test asserts a non-zero ulid output. Reference `internal/access/policy/policytest/helpers.go` for `GrantEngine` / `NewErrorEngine` / `MockAccessPolicyEngine`.

```go
package authguard_test

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/eventbus/authguard"
    "github.com/holomush/holomush/internal/eventbus/codec"
    "github.com/holomush/holomush/internal/eventbus/crypto/dek"
    accesstypes "github.com/holomush/holomush/internal/access/policy/types"
    "github.com/holomush/holomush/internal/access/policy/policytest"
    "github.com/holomush/holomush/internal/idgen"
)

type fakeParticipants struct{ list []dek.Participant }

func (f *fakeParticipants) Participants(_ context.Context, _ codec.KeyID, _ uint32) ([]dek.Participant, error) {
    return f.list, nil
}

type fakeManifest struct{ allowed map[string]map[string]bool }

func (f *fakeManifest) PluginRequestsDecryption(plugin, eventType string) bool {
    if perPlugin := f.allowed[plugin]; perPlugin != nil {
        return perPlugin[eventType]
    }
    return false
}

type fakeBackpressure struct{ throttle bool }

func (f *fakeBackpressure) ShouldThrottle(_ string) bool { return f.throttle }

// newGuardWithFakes builds a Guard with the test fixtures. The
// abacEngine is a policytest.GrantEngine pre-configured to grant or
// deny based on `abacAllow`.
func newGuardWithFakes(t *testing.T, parts []dek.Participant, abacAllow bool) authguard.AuthGuard {
    t.Helper()
    p := &fakeParticipants{list: parts}
    m := &fakeManifest{allowed: map[string]map[string]bool{
        "mod-filter": {"core-comm:whisper": true},
    }}
    var abac authguard.ABACEngine
    if abacAllow {
        // policytest.AllowAllEngine() returns a *MockAccessPolicyEngine
        // pre-configured to allow every Evaluate (helpers.go:19).
        abac = policytest.AllowAllEngine()
    } else {
        // policytest.DenyAllEngine() returns a *MockAccessPolicyEngine
        // pre-configured to deny every Evaluate (helpers.go:29).
        abac = policytest.DenyAllEngine()
    }
    b := &fakeBackpressure{throttle: false}
    g, err := authguard.New(p, m, abac, b)
    require.NoError(t, err)
    return g
}

// Branch 1 — character is participant.
func TestGuardBranchCharacterParticipantPermits(t *testing.T) {
    parts := []dek.Participant{{PlayerID: "01ABC", CharacterID: "01XYZ", BindingID: "01DEF"}}
    g := newGuardWithFakes(t, parts, false)

    id, err := authguard.NewCharacterIdentity("01ABC", "01XYZ", "01DEF")
    require.NoError(t, err)
    decision, err := g.Check(t.Context(), authguard.CheckRequest{
        Identity: id, KeyID: codec.KeyID(42), KeyVersion: 1,
        EventType: "core-comm:whisper", EventID: idgen.New(),
    })
    require.NoError(t, err)
    assert.True(t, decision.Permit)
    assert.Equal(t, authguard.PermitParticipant, decision.Code)
}

func TestGuardBranchCharacterNonParticipantDenies(t *testing.T) {
    parts := []dek.Participant{{PlayerID: "01OTHER", CharacterID: "01OTHERCHAR", BindingID: "01OTHERBIND"}}
    g := newGuardWithFakes(t, parts, false)

    id, _ := authguard.NewCharacterIdentity("01ABC", "01XYZ", "01DEF")
    decision, err := g.Check(t.Context(), authguard.CheckRequest{
        Identity: id, KeyID: codec.KeyID(42), KeyVersion: 1,
    })
    require.NoError(t, err)
    assert.False(t, decision.Permit)
    assert.Equal(t, authguard.DenyNotParticipant, decision.Code)
}

// Branch 2 — player history read.
func TestGuardBranchPlayerHistoryReadPermitsWhenABACAllows(t *testing.T) {
    parts := []dek.Participant{{PlayerID: "01ABC", CharacterID: "01PRIORCHAR", BindingID: "01PRIORBIND"}}
    g := newGuardWithFakes(t, parts, true)

    id, _ := authguard.NewPlayerIdentity("01ABC")
    decision, err := g.Check(t.Context(), authguard.CheckRequest{
        Identity: id, KeyID: codec.KeyID(42), KeyVersion: 1,
    })
    require.NoError(t, err)
    assert.True(t, decision.Permit)
    assert.Equal(t, authguard.PermitPlayerHistory, decision.Code)
}

func TestGuardBranchPlayerNeverParticipatedDenies(t *testing.T) {
    parts := []dek.Participant{{PlayerID: "01OTHER", CharacterID: "01X", BindingID: "01Y"}}
    g := newGuardWithFakes(t, parts, true)

    id, _ := authguard.NewPlayerIdentity("01ABC")
    decision, err := g.Check(t.Context(), authguard.CheckRequest{Identity: id, KeyID: codec.KeyID(42), KeyVersion: 1})
    require.NoError(t, err)
    assert.False(t, decision.Permit)
    assert.Equal(t, authguard.DenyPlayerNeverParticipated, decision.Code)
}

// Branch 3 — plugin: manifest+ABAC permits.
func TestGuardBranchPluginPermits(t *testing.T) {
    g := newGuardWithFakes(t, nil, true)
    id, _ := authguard.NewPluginIdentity("mod-filter", "01INST")
    decision, err := g.Check(t.Context(), authguard.CheckRequest{
        Identity: id, KeyID: codec.KeyID(42), KeyVersion: 1,
        EventType: "core-comm:whisper",
    })
    require.NoError(t, err)
    assert.True(t, decision.Permit)
    assert.Equal(t, authguard.PermitPluginGrant, decision.Code)
}

// Branch 3 — plugin: manifest missing.
func TestGuardBranchPluginManifestMissingDenies(t *testing.T) {
    g := newGuardWithFakes(t, nil, true)
    id, _ := authguard.NewPluginIdentity("mod-filter", "01INST")
    decision, err := g.Check(t.Context(), authguard.CheckRequest{
        Identity: id, EventType: "core-comm:undeclared",
    })
    require.NoError(t, err)
    assert.False(t, decision.Permit)
    assert.Equal(t, authguard.DenyManifestDeclarationMissing, decision.Code)
}

// Branch 3 — backpressure pre-check.
func TestGuardBranchPluginBackpressureDeniesEarly(t *testing.T) {
    p := &fakeParticipants{}
    m := &fakeManifest{allowed: map[string]map[string]bool{"mod-filter": {"core-comm:whisper": true}}}
    a := policytest.AllowAllEngine() // helpers.go:19
    b := &fakeBackpressure{throttle: true}
    g, err := authguard.New(p, m, a, b)
    require.NoError(t, err)

    id, _ := authguard.NewPluginIdentity("mod-filter", "01INST")
    decision, err := g.Check(t.Context(), authguard.CheckRequest{
        Identity: id, EventType: "core-comm:whisper",
    })
    require.NoError(t, err)
    assert.False(t, decision.Permit)
    assert.Equal(t, authguard.DenyAuditBackpressure, decision.Code)
}

// Branch 4 — operator: INV-43.
func TestGuardBranchOperatorAlwaysDenies(t *testing.T) {
    g := newGuardWithFakes(t, nil, true)
    id := authguard.NewOperatorIdentity()
    decision, err := g.Check(t.Context(), authguard.CheckRequest{Identity: id})
    require.NoError(t, err)
    assert.False(t, decision.Permit)
    assert.Equal(t, authguard.DenyOperatorUseAdminRPC, decision.Code)
}

func TestGuardBranchUnknownKindDenies(t *testing.T) {
    g := newGuardWithFakes(t, nil, true)
    decision, err := g.Check(t.Context(), authguard.CheckRequest{
        Identity: authguard.Identity{Kind: authguard.IdentityKindUnknown},
    })
    require.NoError(t, err)
    assert.False(t, decision.Permit)
    assert.Equal(t, authguard.DenyUnknownIdentityKind, decision.Code)
}
```

Existing helpers verified against `internal/access/policy/policytest/helpers.go`: `AllowAllEngine()` (line 19, blanket-permit `*MockAccessPolicyEngine`); `DenyAllEngine()` (line 29, blanket-deny); `NewGrantEngine()` (line 45, configurable per-grant); `NewErrorEngine(err)` (line 96, fixed-error). Tests above use `AllowAllEngine` / `DenyAllEngine` for the simple positive/negative shapes; `NewGrantEngine` is available if a test needs per-grant precision.

- [ ] **Step 7: Run Guard tests to verify they fail.**

Run: `task test -- -run TestGuard ./internal/eventbus/authguard/...`

Expected: FAIL — `Guard` and `New` not implemented.

- [ ] **Step 8: Implement `internal/eventbus/authguard/guard.go`.**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package authguard

import (
    "context"
    "fmt"

    "github.com/oklog/ulid/v2"
    "github.com/samber/oops"

    accesstypes "github.com/holomush/holomush/internal/access/policy/types"
)

// Guard is the production AuthGuard impl. Stateless; safe for concurrent use.
type Guard struct {
    parts    ParticipantLookup
    manifest ManifestLookup
    abac     ABACEngine
    bp       BackpressureChecker
}

func New(p ParticipantLookup, m ManifestLookup, a ABACEngine, b BackpressureChecker) (*Guard, error) {
    switch {
    case p == nil:
        return nil, oops.Code("AUTHGUARD_DEPENDENCY_NIL").With("dependency", "ParticipantLookup").Errorf("nil ParticipantLookup")
    case m == nil:
        return nil, oops.Code("AUTHGUARD_DEPENDENCY_NIL").With("dependency", "ManifestLookup").Errorf("nil ManifestLookup")
    case a == nil:
        return nil, oops.Code("AUTHGUARD_DEPENDENCY_NIL").With("dependency", "ABACEngine").Errorf("nil ABACEngine")
    case b == nil:
        return nil, oops.Code("AUTHGUARD_DEPENDENCY_NIL").With("dependency", "BackpressureChecker").Errorf("nil BackpressureChecker")
    }
    return &Guard{parts: p, manifest: m, abac: a, bp: b}, nil
}

func (g *Guard) Check(ctx context.Context, req CheckRequest) (Decision, error) {
    switch req.Identity.Kind {
    case IdentityKindCharacter:
        return g.checkCharacter(ctx, req)
    case IdentityKindPlayer:
        return g.checkPlayer(ctx, req)
    case IdentityKindPlugin:
        return g.checkPlugin(ctx, req)
    case IdentityKindOperator:
        return Decision{
            Permit: false,
            Code:   DenyOperatorUseAdminRPC,
            Reason: "operator reads go through AdminReadStream (§7.5)",
        }, nil
    default:
        return Decision{
            Permit: false,
            Code:   DenyUnknownIdentityKind,
            Reason: fmt.Sprintf("unknown identity kind: %d", req.Identity.Kind),
        }, nil
    }
}

func (g *Guard) checkCharacter(ctx context.Context, req CheckRequest) (Decision, error) {
    parts, err := g.parts.Participants(ctx, req.KeyID, req.KeyVersion)
    if err != nil {
        return Decision{}, oops.Code("AUTHGUARD_PARTICIPANTS_FAILED").Wrap(err)
    }
    for _, p := range parts {
        if p.BindingID != "" && p.BindingID == req.Identity.BindingID {
            return Decision{Permit: true, Code: PermitParticipant, Reason: "character is current participant by binding_id"}, nil
        }
    }
    return Decision{Permit: false, Code: DenyNotParticipant, Reason: "character not in DEK participant set"}, nil
}

func (g *Guard) checkPlayer(ctx context.Context, req CheckRequest) (Decision, error) {
    parts, err := g.parts.Participants(ctx, req.KeyID, req.KeyVersion)
    if err != nil {
        return Decision{}, oops.Code("AUTHGUARD_PARTICIPANTS_FAILED").Wrap(err)
    }
    var matched bool
    for _, p := range parts {
        if p.PlayerID != "" && p.PlayerID == req.Identity.PlayerID {
            matched = true
            break
        }
    }
    if !matched {
        return Decision{Permit: false, Code: DenyPlayerNeverParticipated, Reason: "player never participated"}, nil
    }
    abacReq, err := accesstypes.NewAccessRequest(
        "player:"+req.Identity.PlayerID,
        "read_own_history",
        fmt.Sprintf("dek:%d:%d", req.KeyID, req.KeyVersion),
        nil,
    )
    if err != nil {
        return Decision{}, oops.Code("AUTHGUARD_ABAC_REQUEST_FAILED").Wrap(err)
    }
    abacDec, err := g.abac.Evaluate(ctx, abacReq)
    if err != nil {
        return Decision{}, oops.Code("AUTHGUARD_ABAC_EVAL_FAILED").Wrap(err)
    }
    if !abacDec.IsAllowed() {
        return Decision{Permit: false, Code: DenyPlayerNoABACGrant, Reason: "ABAC denied", ABACDecision: &abacDec}, nil
    }
    grantID := mustParseULID(abacDec.PolicyID()) // accesstypes.Decision.PolicyID() at types.go:171
    return Decision{
        Permit:       true,
        Code:         PermitPlayerHistory,
        GrantID:      grantID,
        ABACDecision: &abacDec,
    }, nil
}

func (g *Guard) checkPlugin(ctx context.Context, req CheckRequest) (Decision, error) {
    if g.bp.ShouldThrottle(req.Identity.PluginName) {
        return Decision{Permit: false, Code: DenyAuditBackpressure, Reason: "audit-emit queue throttled"}, nil
    }
    if !g.manifest.PluginRequestsDecryption(req.Identity.PluginName, req.EventType) {
        return Decision{Permit: false, Code: DenyManifestDeclarationMissing, Reason: "manifest does not declare requests_decryption"}, nil
    }
    abacReq, err := accesstypes.NewAccessRequest(
        "plugin:"+req.Identity.PluginName,
        "decrypt",
        fmt.Sprintf("dek:%d:%d", req.KeyID, req.KeyVersion),
        map[string]any{
            "event_type":  req.EventType,
            "plugin_name": req.Identity.PluginName,
            "plugin_inst": req.Identity.InstanceID,
        },
    )
    if err != nil {
        return Decision{}, oops.Code("AUTHGUARD_ABAC_REQUEST_FAILED").Wrap(err)
    }
    abacDec, err := g.abac.Evaluate(ctx, abacReq)
    if err != nil {
        return Decision{}, oops.Code("AUTHGUARD_ABAC_EVAL_FAILED").Wrap(err)
    }
    if !abacDec.IsAllowed() {
        return Decision{Permit: false, Code: DenyNoABACGrant, Reason: "ABAC denied", ABACDecision: &abacDec}, nil
    }
    grantID := mustParseULID(abacDec.PolicyID())
    return Decision{
        Permit:       true,
        Code:         PermitPluginGrant,
        GrantID:      grantID,
        ABACDecision: &abacDec,
    }, nil
}

// mustParseULID parses a ULID-format string into ulid.ULID. Falls back
// to zero ULID if the input doesn't parse (which can happen for
// non-ULID PolicyIDs in legacy seed policies). The caller logs the
// parse failure separately.
func mustParseULID(s string) ulid.ULID {
    if s == "" {
        return ulid.ULID{}
    }
    parsed, err := ulid.Parse(s)
    if err != nil {
        return ulid.ULID{}
    }
    return parsed
}
```

- [ ] **Step 9: Run Guard tests.**

Run: `task test -- -run TestGuard ./internal/eventbus/authguard/...`

Expected: PASS — including INV-43 operator-deny.

- [ ] **Step 10: Create `adapter_dek.go` and `adapter_manifest.go`.**

`internal/eventbus/authguard/adapter_dek.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package authguard

import (
    "context"

    "github.com/holomush/holomush/internal/eventbus/codec"
    "github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

type dekParticipantAdapter struct{ mgr dek.Manager }

func NewDEKParticipantLookup(mgr dek.Manager) ParticipantLookup {
    return &dekParticipantAdapter{mgr: mgr}
}

func (a *dekParticipantAdapter) Participants(ctx context.Context, keyID codec.KeyID, version uint32) ([]dek.Participant, error) {
    return a.mgr.Participants(ctx, keyID, version)
}
```

`internal/eventbus/authguard/adapter_manifest.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package authguard

import (
    plugins "github.com/holomush/holomush/internal/plugin"
)

type manifestAdapter struct{ mgr *plugins.Manager }

func NewPluginManifestLookup(mgr *plugins.Manager) ManifestLookup {
    return &manifestAdapter{mgr: mgr}
}

func (a *manifestAdapter) PluginRequestsDecryption(pluginName, eventType string) bool {
    return a.mgr.PluginRequestsDecryption(pluginName, eventType)
}
```

Plus minimal adapter tests (one happy-path each).

- [ ] **Step 11: Run all authguard tests.**

Run: `task test -- ./internal/eventbus/authguard/...`

Expected: PASS.

- [ ] **Step 12: Run the full test suite.**

Run: `task test`

Expected: PASS.

- [ ] **Step 13: Commit.**

```shell
JJ_EDITOR=true jj --no-pager describe -m "feat(authguard): typed Identity + AuthGuard four-branch decision tree (Decision 1) (holomush-ojw1.2)

Phase 3b grounding doc Decision 1. New package internal/eventbus/authguard/
holds the policy-evaluation seam for sensitive event delivery. Typed
Identity (Character/Player/Plugin/Operator) avoids the eventbus.Subject
naming collision; typed Decision carries the §7.2 outcome enum plus
*accesstypes.Decision for ABAC trace.

Guard concrete impl branches on Identity.Kind:
- Branch 1 (character): binding_id match → PermitParticipant
- Branch 2 (player): player_id match + ABAC permit → PermitPlayerHistory
- Branch 3 (plugin): backpressure pre-check + manifest declaration + ABAC permit → PermitPluginGrant
- Branch 4 (operator): always denies (INV-43)

GrantID populated from accesstypes.Decision.PolicyID(); ULIDs generated
via idgen.New() (project's crypto/rand convention)." && jj --no-pager new
```

---

### Task T8: Create `internal/eventbus/authguard/audit/` subpackage — `DecryptAuditEmitter` (Decision 3)

Per-plugin bounded queues, drain goroutines publishing to `audit.<game>.plugin_decrypt.<plugin>` subjects, and the queue-depth signal exposed as `BackpressureChecker.ShouldThrottle`. One concrete type satisfies both interfaces. **`WithGameID(string)` option replaces the hardcoded "holomush" game-id** (per plan-reviewer Round 1 Finding #14).

**Files:**

- Create: `internal/eventbus/authguard/audit/emitter.go`
- Create: `internal/eventbus/authguard/audit/emitter_test.go`

**Implementation reference:** identical structure to the grounding doc's Decision 3 sketch with one substantive change — the `eventToPublish` function takes `gameID string` (configured at `NewQueuedEmitter` time via `WithGameID(string)` option). Default game-id is "holomush" for backwards-compat with the spec's example wording, but production wiring (cmd/holomush) MUST set this from config.

**Step list (abbreviated; full TDD shape):**

- [ ] **Step 1: Write failing tests** (queue capacity, throttle, drain). Tests must use `WithGameID("test-game")` and assert subject is `audit.test-game.plugin_decrypt.<plugin>`.

- [ ] **Step 2: Run tests** — FAIL (package missing).

- [ ] **Step 3: Implement `emitter.go`** with the structure from grounding doc Decision 3, plus:

```go
type Option func(*queuedAuditEmitter)

func WithCapacity(n int) Option { ... }
func WithThreshold(r float64) Option { ... }
func WithGameID(gameID string) Option {
    return func(q *queuedAuditEmitter) {
        if gameID != "" {
            q.gameID = gameID
        }
    }
}

type queuedAuditEmitter struct {
    pub       eventbus.Publisher
    gameID    string  // default "holomush"; overridden via WithGameID
    capacity  int
    threshold float64
    // ... etc ...
}

func NewQueuedEmitter(pub eventbus.Publisher, opts ...Option) (*queuedAuditEmitter, error) {
    if pub == nil {
        return nil, oops.Code("AUDIT_EMITTER_DEPENDENCY_NIL").Errorf("nil Publisher")
    }
    q := &queuedAuditEmitter{
        pub: pub, gameID: "holomush",
        capacity: 10_000, threshold: 0.5,
        queues: make(map[string]*pluginQueue),
    }
    for _, opt := range opts { opt(q) }
    return q, nil
}

func (q *queuedAuditEmitter) eventToPublish(pluginName string, rec PluginDecryptRecord) eventbus.Event {
    payload := []byte(fmt.Sprintf(
        `{"plugin_name":%q,...,"emitted_at":%q}`,
        pluginName, ..., time.Now().UTC().Format(time.RFC3339Nano),
    ))
    return eventbus.Event{
        ID: idgen.New(), // crypto/rand-backed; not ulid.Make()
        Subject: eventbus.Subject(fmt.Sprintf("audit.%s.plugin_decrypt.%s", q.gameID, pluginName)),
        // ... etc ...
    }
}
```

- [ ] **Step 4: Run tests** — PASS.

- [ ] **Step 5: Run full test suite.** PASS.

- [ ] **Step 6: Commit.**

---

### Task T9: Wire AuthGuard + decrypt-on-fanout into subscriber and hot-tier history reader (Decision 5)

The core fan-out task. Both `JetStreamSubscriber.Next` and `hot_jetstream::decodeJetStreamMessage` gain the AuthGuard.Check + AAD reconstruct + conditional-decrypt + emit-audit + stamp-metadata_only flow per Decision 5's order-of-operations.

**Files:**

- Modify: `internal/eventbus/bus.go:18-25` (`Subscriber` interface gains `identity authguard.Identity`)
- Modify: `internal/eventbus/subscriber.go` (OpenSession, decodeDelivery, options)
- Modify: `internal/eventbus/history/hot_jetstream.go` (mirror)
- Modify: **all 35 OpenSession callsites across 13 files** (per plan-reviewer Round 1 Finding #6 corrected count):
  - `internal/eventbus/bus.go:22` (interface)
  - `internal/eventbus/subscriber.go:121` (impl)
  - `internal/eventbus/eventbustest/embedded_test.go:63,85`
  - `internal/eventbus/subscriber_round3_test.go:76,98`
  - `internal/eventbus/subscriber_test.go` (~17 occurrences)
  - `internal/eventbus/integration_test.go:88`
  - `internal/eventbus/publisher_test.go` (3 occurrences)
  - `internal/eventbus/bus_test.go` (`fakeBus.OpenSession`)
  - `internal/grpc/subscribe_server_test.go:73` (`fakeSubscriber.OpenSession`)
  - `internal/grpc/server_helpers_test.go` (`stubSubscriber.OpenSession`)
  - `test/integration/auth/auth_suite_test.go:358` (`unusedSubscriber.OpenSession`)
  - `test/integration/eventbus_e2e/multi_protocol_fanout_test.go`
  - `test/integration/eventbus_e2e/reconnect_resume_test.go`

**Step list (full TDD):**

- [ ] **Step 1: Write failing test for AuthGuard-deny → metadata_only stamping.**

In `internal/eventbus/subscriber_test.go`, add an integration test using the embedded JetStream + a fake AuthGuard + DEK manager to publish a sensitive event, open as a non-participant identity, and assert `delivery.MetadataOnly() == true` and `delivery.Event().Payload` is empty.

- [ ] **Step 2: Run test** — FAIL.

- [ ] **Step 3: Update `Subscriber` interface in `bus.go:22`.**

```go
type Subscriber interface {
    OpenSession(ctx context.Context, sessionID string, identity authguard.Identity, filters []Subject) (SessionStream, error)
}
```

- [ ] **Step 4: Add new options on `JetStreamSubscriber` in `subscriber.go`.**

`WithSubscriberAuthGuard(g authguard.AuthGuard)`, `WithSubscriberDEKManager(m dek.Manager)`, `WithSubscriberDecryptAuditEmitter(em audit.DecryptAuditEmitter)`. Add corresponding fields on `JetStreamSubscriber` struct + `jetStreamSessionStream` struct.

- [ ] **Step 5: Update `OpenSession` to take `identity` and propagate to session stream.**

- [ ] **Step 6: Implement `decodeAndAuthorize` in `subscriber.go`.**

Per grounding doc Decision 5's order-of-operations subsection: proto-unmarshal → identity-codec branch returns immediately; sensitive branch runs AuthGuard.Check → on permit, Resolve key + Build AAD + Decode → on plugin, EmitPluginDecrypt with TOCTOU plaintext-zeroing on `AUDIT_QUEUE_FULL`.

(Full code already provided in the original R1 plan's Task 7 Step 6 and the grounding doc Decision 5 pseudocode. Follow that shape exactly; the only correction from R1 is to use `idgen.New()` not `ulid.Make()` where ULIDs are minted, and to read `accesstypes.Decision.PolicyID()` for grant_id where AuthGuard returns trace data.)

- [ ] **Step 7: Replicate the same shape in `hot_jetstream.go::decodeJetStreamMessage`.**

- [ ] **Step 8: Update all 35 OpenSession callsites** to pass an `Identity` argument. Test callsites use:

```go
testIdentity, _ := authguard.NewCharacterIdentity("01TESTPLAYER", "01TESTCHAR", "01TESTBIND")
stream, err := sub.OpenSession(ctx, sessionID, testIdentity, []eventbus.Subject{subject})
```

For mocks/fakes (`fakeSubscriber`, `fakeBus`, `stubSubscriber`, `unusedSubscriber`), add the `identity authguard.Identity` parameter to the method signature.

- [ ] **Step 9: Run mockery** (bare `mockery`) to regenerate any generated mocks.

- [ ] **Step 10: Run test** — PASS.

- [ ] **Step 11: Run full suite.** PASS.

- [ ] **Step 12: Commit.**

---

### Task T10: gRPC handler Identity construction (Decision 2)

The gRPC handlers are the authentication boundary. They construct `authguard.NewCharacterIdentity(...)` from `info.PlayerID + info.CharacterID + bindings.Current(info.CharacterID)` and pass it to `Subscriber.OpenSession`. Same shape for `QueryStreamHistory`. They also read `delivery.MetadataOnly()` and stamp `EventFrame.metadata_only`.

**Files:**

- Modify: `internal/grpc/server.go:679-779` (Subscribe handler)
- Modify: `internal/grpc/query_stream_history.go` (same pattern)
- Modify: `cmd/holomush/...` (or wherever gRPC server is constructed) — inject `*postgres.BindingRepository`

- [ ] **Step 1: Write failing integration test for end-to-end metadata_only stamping over gRPC.**

In `test/integration/crypto/metadata_only_test.go` (will be implemented in T12 with full body; this Step writes the test scaffolding using the existing `testutil` pattern from `emit_test.go`).

- [ ] **Step 2: Run test** — FAIL.

- [ ] **Step 3: Update Subscribe handler.**

In `internal/grpc/server.go`, after `info, err := s.sessionStore.Get(ctx, req.SessionId)` (line 679), before `s.subscriber.OpenSession` (line 779), add:

```go
bindingID, err := s.bindings.Current(ctx, info.CharacterID.String())
if err != nil {
    return oops.Code("SUBSCRIBE_BINDING_LOOKUP_FAILED").
        With("character_id", info.CharacterID.String()).
        Wrap(err)
}
identity, err := authguard.NewCharacterIdentity(
    info.PlayerID.String(),
    info.CharacterID.String(),
    bindingID,
)
if err != nil {
    return oops.Code("SUBSCRIBE_IDENTITY_INVALID").Wrap(err)
}
```

Then change the OpenSession call to pass `identity`:

```go
busStream, subErr := s.subscriber.OpenSession(ctx, req.SessionId, identity, filters)
```

In the per-event Send loop, when constructing `EventFrame` from a `Delivery`, add:

```go
frame := &corev1.EventFrame{
    // ... existing fields ...
    MetadataOnly: delivery.MetadataOnly(),
}
```

- [ ] **Step 4: Update QueryStreamHistory handler.**

Same shape — read info, call `bindings.Current`, construct Identity, pass to history reader, stamp metadata_only.

- [ ] **Step 5: Wire BindingStore into the gRPC server.**

In `cmd/holomush/main.go` (or equivalent), add `BindingStore: postgres.NewBindingRepository(pool)` to the `CoreServer` constructor argument.

- [ ] **Step 6: Run integration test.** PASS.

- [ ] **Step 7: Run full suite.** PASS.

- [ ] **Step 8: Commit.**

---

### Task T11: Default-deny `audit.>` ABAC policy (Decision 3 supplement)

Per plan-reviewer Round 1 Finding #3, the original "fabricated `setup.RegisterDefaultPolicies`" approach is replaced with the real `policy.SeedPolicies()` mechanism plus a SQL migration for existing deployments.

**Files:**

- Modify: `internal/access/policy/seed.go` (add new SeedPolicy entry)
- Modify: `internal/access/policy/seed_test.go` (test the policy is in the seed list)
- Create: `internal/store/migrations/000016_seed_audit_subscribe_deny.up.sql`
- Create: `internal/store/migrations/000016_seed_audit_subscribe_deny.down.sql`

- [ ] **Step 1: Write failing test.**

In `internal/access/policy/seed_test.go`, add:

```go
func TestSeedPoliciesIncludesAuditSubscribeDeny(t *testing.T) {
    seeds := SeedPolicies()
    var found bool
    for _, s := range seeds {
        if s.Name == "seed:deny-audit-subscribe-plugin-character" {
            found = true
            assert.Contains(t, s.DSLText, "forbid")
            assert.Contains(t, s.DSLText, "audit.")
            break
        }
    }
    assert.True(t, found, "audit.> deny seed policy MUST be present")
}
```

- [ ] **Step 2: Run test** — FAIL.

- [ ] **Step 3: Add the seed policy.**

In `internal/access/policy/seed.go`, append to the returned `[]SeedPolicy`:

```go
{
    Name:        "seed:deny-audit-subscribe-plugin-character",
    Description: "Plugins and characters MUST NOT subscribe to audit.> subjects (§7.7 ABAC layer)",
    // The DSL parser supports principal kinds 'character' / 'plugin' /
    // 'system'; the resource type for stream subscriptions is 'stream';
    // the action is 'subscribe' (or 'read' depending on existing
    // verb conventions — verify against existing stream-related seed
    // policies before committing).
    DSLText:     `forbid(principal is character, action in ["subscribe"], resource is stream) when { resource.stream.name like "audit.*" };`,
    SeedVersion: 1,
},
{
    Name:        "seed:deny-audit-subscribe-plugin",
    Description: "Plugins MUST NOT subscribe to audit.> subjects (§7.7 ABAC layer)",
    DSLText:     `forbid(principal is plugin, action in ["subscribe"], resource is stream) when { resource.stream.name like "audit.*" };`,
    SeedVersion: 1,
},
```

(The exact action verb and resource type may need adjustment; verify against existing seed policies that handle stream subscriptions and existing DSL conventions.)

- [ ] **Step 4: Run test** — PASS.

- [ ] **Step 5: Create the migration up-script.**

`internal/store/migrations/000016_seed_audit_subscribe_deny.up.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Phase 3b grounding doc Decision 3 / §7.7 ABAC layer of the two-layer
-- audit-namespace isolation. SeedPolicies runs at fresh-bootstrap;
-- existing deployments need a migration to receive the new policies.

INSERT INTO policies (name, description, dsl_text, seed_version, created_at, updated_at)
VALUES (
    'seed:deny-audit-subscribe-plugin-character',
    'Characters MUST NOT subscribe to audit.> subjects (§7.7 ABAC layer)',
    'forbid(principal is character, action in ["subscribe"], resource is stream) when { resource.stream.name like "audit.*" };',
    1,
    NOW(),
    NOW()
)
ON CONFLICT (name) DO NOTHING;

INSERT INTO policies (name, description, dsl_text, seed_version, created_at, updated_at)
VALUES (
    'seed:deny-audit-subscribe-plugin',
    'Plugins MUST NOT subscribe to audit.> subjects (§7.7 ABAC layer)',
    'forbid(principal is plugin, action in ["subscribe"], resource is stream) when { resource.stream.name like "audit.*" };',
    1,
    NOW(),
    NOW()
)
ON CONFLICT (name) DO NOTHING;
```

(Exact column names and INSERT shape depend on the existing `policies` table schema; verify with `internal/store/migrations/000001_baseline.up.sql` or the relevant migration that creates the `policies` table.)

- [ ] **Step 6: Create the down-script.**

`internal/store/migrations/000016_seed_audit_subscribe_deny.down.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DELETE FROM policies WHERE name IN (
    'seed:deny-audit-subscribe-plugin-character',
    'seed:deny-audit-subscribe-plugin'
);
```

- [ ] **Step 7: Run full suite.** PASS.

- [ ] **Step 8: Commit.**

---

### Task T12: Integration tests for Phase 3b invariants

Comprehensive integration test suite covering INV-8, INV-9, INV-10, INV-17, INV-18, INV-19, INV-20, INV-22, INV-25, INV-26. **All tests use the existing `test/integration/crypto/emit_test.go` per-test-bring-up pattern (per plan-reviewer Round 1 Finding #10) — no fabricated `server.PrepDEKWithParticipants` / `SubscribeAs` / etc. helpers.** Per-test setup follows `emit_test.go:52-149`: SharedPostgres + FreshDatabase + StartEmbeddedJetStream + RandomKEKHex + LocalAEADProvider + dek.Store/Cache/Manager + audit.NewSubsystem.

**Files:**

- Create: `test/integration/crypto/decrypt_test.go` (INV-8, INV-9, INV-10)
- Create: `test/integration/crypto/plugin_decrypt_test.go` (INV-17, INV-18, INV-19, INV-20)
- Create: `test/integration/crypto/history_decrypt_test.go` (INV-22, INV-25)
- Create: `test/integration/crypto/metadata_only_test.go` (INV-26 — already drafted in T10 Step 1)

Each test starts with `// Verifies: INV-N` per master spec §11.1.

**Step list (per test, abbreviated):**

- [ ] **Steps 1-N for each invariant test:** Write failing test → Run → FAIL → confirm. (The implementations of T1-T11 ensure the tests pass once everything is wired.)

- [ ] **Final step: Run `task pr-prep` to mirror CI.**

```shell
task pr-prep
```

Expected: PASS — all CI jobs (lint, format, schema, license, unit, integration, E2E).

If failure: fix and re-run until green. Do NOT push to a PR branch with red `pr-prep`.

- [ ] **Commit.**

---

### Task T13 (optional): Meta-test for INV-N ↔ test-name binding

Per plan-reviewer Round 1 Finding #16. Defer to a follow-up bead if scoping bites.

**Files:**

- Create: `test/integration/crypto/inv_coverage_meta_test.go`

A meta-test that grep-checks every `INV-N` referenced in master spec §2 is covered by at least one `// Verifies: INV-N` test in the integration-test directory. Fails CI if a new invariant is added without a covering test.

(Implementation deferred — non-blocking; plan-reviewer marked as Low severity. If included, add full body. If deferred, file a follow-up bead.)

---

## Self-Review

1. **Spec coverage:**
   - Decision 0 → T1 ✓
   - Decision 1 (authguard package + interfaces + Guard impl) → T7 ✓
   - Decision 1 substrate (dek.Manager.Participants) → T4 ✓
   - Decision 1 substrate (ManifestLookup) → T6 ✓
   - Decision 2 (Identity construction at gRPC boundary + binding lookup) → T10 ✓
   - Decision 3 (DecryptAuditEmitter + backpressure + WithGameID) → T8 ✓
   - Decision 3 (audit.> default-deny ABAC policy) → T11 ✓
   - Decision 4 (EventFrame.metadata_only + Delivery.MetadataOnly()) → T5 ✓
   - Decision 5 (OpenSession Identity + dual-callsite + order-of-operations) → T9 ✓
   - Decision 6 (AccessRequest.Attributes Action-bag overlay + reserved key) → T2 ✓
   - Decision 7 (player_character_bindings substrate + Tx-wrap of both character-creation paths) → T3 ✓
   - INV invariant integration tests → T12 ✓
   - Meta-test enforcement → T13 (optional) ✓
   - Out-of-scope items (cold-tier, NATS deny rules, operator break-glass, plugin SDK helpers, wizard-transfer, character-deletion, scene-join callers) → not addressed, per spec ✓

2. **Plan-reviewer Round 1 findings:**
   - #1 (BindingID not in session pipeline) → addressed via Decision 7 substrate (T3)
   - #2 (PluginRequestsDecryption substrate missing) → addressed via T6
   - #3 (setup.RegisterDefaultPolicies fabricated) → addressed via T11 (real SeedPolicies + migration)
   - #4 (newFakeStoreWithRow fabricated; dek.Store concrete) → T4 drops fake-based unit tests; uses integration only
   - #5 (AttributeBags four-bag shape) → T2 uses Action-bag overlay convention
   - #6 (OpenSession callsite count) → T9 enumerates all 35 callsites across 13 files
   - #7 (two-SELECT redundancy / Rotate TOCTOU) → T4 documents
   - #8 (`task mockery` fabricated) → T2/T9 use bare `mockery`
   - #9 (Subscriber test fixture verification) → T9 uses verified `testutil` pattern
   - #10 (test harness fabricated) → T12 uses existing emit_test.go pattern, per-test bring-up
   - #11 (NewAccessRequest count undercount) → T2 enumerates correct 25 across 13 files
   - #12 (ulid.Make() vs idgen.New()) → T7/T8 use idgen.New()
   - #13 (WithSubscriberDEKManager naming) → T9 documents the choice
   - #14 (audit subject "holomush" hardcode) → T8 ships WithGameID option
   - #15 (test fixtures invented) → T2/T7 use existing policytest fixtures
   - #16 (INV-N meta-test) → T13 optional
   - #17 (TODO Phase 4 grant ID) → T7 uses accesstypes.Decision.PolicyID()

3. **Type consistency (cross-task verification):**
   - `authguard.Identity` consistent (T7 defines, T9/T10 consume)
   - `IdentityKind` enum consistent
   - `Decision` struct shape consistent (Permit/Code/GrantID/Reason/ABACDecision)
   - `OpenSession(ctx, sessionID, identity, filters)` consistent across T9, T10
   - `PluginDecryptRecord` consistent (T8 defines, T9 consumes)
   - `EventFrame.metadata_only = 10` consistent (T5 defines, T10 stamps)
   - `BindingStore.Current(ctx, characterID string) (string, error)` consistent (T3 defines, T10 calls with `.String()`)
   - `Bindings.Create` Tx-via-context contract consistent across T3 (Path A and Path B both use `transactor.InTransaction`)
   - `mockery` (bare) consistent across T2, T9

4. **Substrate citations (rg-verified at design-time):**
   - `internal/grpc/server.go:679` — `s.sessionStore.Get(ctx, req.SessionId)` ✓
   - `internal/auth/guest_service.go:113` — `s.chars.Create(ctx, char)` ✓
   - `internal/grpc/auth_handlers.go:369` — `func ... CreateCharacter(...)` ✓
   - `internal/world/postgres/character_repo.go:46-56` — `Create` method (currently uses `r.pool.Exec` directly) ✓
   - `internal/world/postgres/transactor.go:27` — `InTransaction(ctx, fn)` ✓
   - `internal/access/policy/seed.go:14-209` — `SeedPolicies()` mechanism ✓
   - `internal/access/policy/types/types.go:107-130` — `AccessRequest`/`NewAccessRequest` ✓
   - `internal/access/policy/engine.go:227` — `e.resolver.Resolve(ctx, req)` ✓
   - `internal/access/policy/attribute/resolver.go:170` — `bags.Action["name"] = req.Action` ✓
   - `internal/access/policy/dsl/evaluator.go:438` — DSL `action.<name>` resolution ✓ (per design-reviewer R3 verification)
   - `internal/eventbus/codec/codec.go` — Codec interface (Phase 3a, unchanged) ✓
   - `internal/eventbus/publisher.go:189-291` — Publish encrypt path ✓
   - `internal/eventbus/subscriber.go:354-435` — `decodeDelivery` ✓
   - `internal/eventbus/history/hot_jetstream.go:340-410` — `decodeJetStreamMessage` ✓
   - `internal/eventbus/crypto/dek/manager.go:25-35,38-65` — Manager interface + impl ✓
   - `internal/plugin/crypto_manifest.go:60` — `LookupEmitSensitivity` (template for T6's PluginRequestsDecryption) ✓
   - `internal/plugin/crypto_validator.go:60-93` — qualified `<plugin>:<event_type>` form ✓

5. **Placeholder scan:** No `TBD`/`TODO`/`FIXME` in production code blocks. Two intentional `accesstypes.Decision.PolicyID()` usages mark the grant-ID extraction (Decision 7's earlier R1 placeholder of `ulid.Make()` is replaced).

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-02-event-payload-crypto-phase3b-authguard-decrypt.md` (R2 — full revision against R4 grounding doc, addresses all 17 plan-reviewer Round 1 findings).

Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration. REQUIRED SUB-SKILL: `superpowers:subagent-driven-development`.

**2. Inline Execution** — Execute tasks in this session using `superpowers:executing-plans`, batch execution with checkpoints.

**Which approach?**
