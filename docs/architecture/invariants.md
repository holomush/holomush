<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# HoloMUSH Invariant Registry

Canonical registry of all named system invariants. Paired with
`invariants.yaml` (machine-readable source of truth). The meta-test at
`test/meta/invariant_registry_test.go` reads the YAML file directly.

**This document is generated** inside the `BEGIN GENERATED` / `END GENERATED`
regions below: `cmd/inv-render` renders them from `invariants.yaml`. Do not
hand-edit inside those regions — edit the YAML and run `task invariants:render`.
The prose outside the regions is hand-authored. CI runs `inv-render -check`
(generate-and-diff) and fails if the rendered regions drift from the YAML.

## Scope index

<!-- BEGIN GENERATED: scope-index (edit invariants.yaml + run `task invariants:render`) -->

| Scope | Description | Boundary |
|-------|-------------|----------|
| `INV-CRYPTO` | Event payload encryption, DEK lifecycle, key wrapping, decryption delivery, participant sets, AdminReadStream | Cryptographic operations on event payloads. Does NOT include: audit projection (→ INV-EVENTBUS), plugin manifest validation (→ INV-PLUGIN), cluster coordination (→ INV-CLUSTER). Crypto invariants that operate on in-process state (DEK cache, key material, envelope codec) belong here; invariants that govern wire-level coordination between replicas (invalidation pings, probe-and-pill, N-of-N ack contracts) belong under INV-CLUSTER. |
| `INV-PRIVACY` | Stream history temporal floors, scope gating, guest-session bounds, reattach/Idle arrival-timestamp semantics | Privacy-relevant gating on history reads. Does NOT include: ABAC policy evaluation (→ INV-ACCESS), subscribe authorization (→ INV-EVENTBUS). |
| `INV-PRESENCE` | Presence snapshot correctness, field enumeration, client-side dedup, ownership obscuration | Current-state presence queries. Does NOT include: session status lifecycle (→ INV-SESSION). |
| `INV-SCENE` | Scene lifecycle, board queries, content warnings, pose ordering, focus model, publish snapshot/state, IC isolation, history readability | All scene-domain behavior. Cross-cuts multiple Phase specs (P4–P8). |
| `INV-PLUGIN` | Runtime symmetry, manifest validation, hostfunc safety, emit gates, setting isolation, plugin authz | Plugin-system contracts applicable to both Lua and binary runtimes. Does NOT include: plugin crypto wiring (→ INV-CRYPTO). |
| `INV-EVENTBUS` | Subject naming, JetStream consumer config, audit projection, delivery contracts, tier routing, rendering completeness, colon eradication | Event infrastructure. Does NOT include: event payload encryption (→ INV-CRYPTO), history privacy gating (→ INV-PRIVACY). |
| `INV-CLUSTER` | Member identity, heartbeats, cache invalidation (cross-replica coordination path), probe-and-pill, clock independence | Multi-replica coordination. Includes cluster-scoped invalidation contracts (e.g., INV-28/INV-29 N-of-N ack pings, INV-56 Coordinator retry limits, INV-59 cache-invalidation correctness) that govern wire-level behavior between replicas. Does NOT include single-process DEK operations (→ INV-CRYPTO). |
| `INV-ACCESS` | ABAC policy evaluation, attribute provider invariants, seed policy shape, authorization decisions | Access control evaluation. Does NOT include: stream-access gating at gRPC boundary (→ INV-EVENTBUS). |
| `INV-SESSION` | Session status lifecycle, connection attachment, focus membership, idle detection | Session state machine. Does NOT include: presence snapshot (→ INV-PRESENCE). |
| `INV-STORE` | Migration discipline, no-DELETE enforcement, spec compliance scanning | Database invariants. |
| `INV-TELEMETRY` | Logging discipline, trace context, metric naming, sloglint policy | Observability contracts. |
| `INV-BRANDING` | Asset integrity, palette tokens, logo generation | Visual identity invariants. Does NOT include: docs quality (separate concern). |
| `INV-DOCS` | Proto doc comments, doc IA, contributor onboarding surface | Documentation quality invariants. |

<!-- END GENERATED: scope-index -->

A new scope is warranted when at least 3 invariants exist that don't fit an
existing scope's boundary, or when a new major subsystem ships with its own
invariants.

## Invariant tables

<!-- BEGIN GENERATED: invariant-tables (edit invariants.yaml + run `task invariants:render`) -->

### `INV-CRYPTO`

| ID | Summary | Legacy | Binding |
|----|---------|--------|---------|
| `INV-CRYPTO-1` | WithHistoryAuth(g, m, em) MUST produce the same coldOpts as WithCryptoCold with the matching per-tier cold constructors. | `INV-1` | pending |
| `INV-CRYPTO-2` | WithHistoryAuth(g, m, em) MUST produce the same hotOpts as WithCryptoHot with the matching per-tier hot constructors. | `INV-2` | pending |
| `INV-CRYPTO-3` | NewReader MUST forward accumulated hotOpts to newJetStreamHotTier when building the default hot tier. | `INV-3` | pending |
| `INV-CRYPTO-4` | WithCryptoHot MUST be a no-op when WithHotTier is also supplied — crypto options are not forwarded to a custom tier. | `INV-4` | pending |
| `INV-CRYPTO-5` | newHistoryReader(nil, nil, nil) MUST preserve the existing nil-auth passthrough behavior (no auth option appended). | `INV-6` | pending |
| `INV-CRYPTO-6` | A subject NOT in a DEK's participant set MUST NOT receive plaintext via fan-out, even when subscribed to the matching subject. | `INV-9` | pending |
| `INV-CRYPTO-7` | Add(participant) MUST grant immediate read access to all existing DEK history without rotating the DEK. | `INV-12` | pending |
| `INV-CRYPTO-8` | Rotate(context) MUST preserve the old DEK ciphertext and old DEK record unchanged (holds under Phase 3c soft-delete). | `INV-13` | pending |
| `INV-CRYPTO-9` | A plugin without manifest requests_decryption for an event class MUST receive metadata-only delivery, regardless of subject subscription. | `INV-17` | pending |
| `INV-CRYPTO-10` | A plugin with manifest declaration but without an active ABAC grant MUST receive metadata-only delivery. | `INV-18` | pending |
| `INV-CRYPTO-11` | Every plugin decryption MUST emit an audit event on a subject the plugin cannot subscribe to. | `INV-19` | pending |
| `INV-CRYPTO-12` | A plugin authorization failure MUST NOT block fan-out to other recipients. | `INV-20` | pending |
| `INV-CRYPTO-13` | events_audit.envelope MUST be byte-equal to the marshaled Event proto envelope on the bus for sensitive events. | `INV-21` | pending |
| `INV-CRYPTO-14` | An event whose cleartext metadata, codec, or dek_ref has been altered MUST fail decryption with a tag-mismatch error and MUST NOT yield plaintext. | `INV-25` | pending |
| `INV-CRYPTO-15` | A recipient denied decryption MUST receive the event with metadata_only=true, empty payload bytes, populated cleartext metadata, and no ciphertext. | `INV-26` | pending |
| `INV-CRYPTO-16` | dek.Material MUST NOT be passed to any io.Writer, json/gob/proto marshaler, slog/log, fmt.Sprint/Print/Errorf, or any []byte-returning function other than the codec Encode/Decode (gocritic ruleguard-enforced). | `INV-27` | pending |
| `INV-CRYPTO-17` | Wrap followed by Unwrap with the returned keyID MUST recover the original DEK byte-for-byte. | `INV-30` | pending |
| `INV-CRYPTO-18` | A startup with provider.name=none MUST refuse if any crypto_keys row exists. | `INV-32` | pending |
| `INV-CRYPTO-19` | A startup with provider X MUST refuse if any crypto_keys row's wrap_provider is not unwrappable by X. | `INV-33` | pending |
| `INV-CRYPTO-20` | A NoneProvider MUST refuse Wrap and MUST cause emit-time failure for any event with Sensitive=true. | `INV-34` | pending |
| `INV-CRYPTO-21` | A crashed Rotate MUST be resolvable by the startup integrity check without manual intervention. | `INV-37` | pending |
| `INV-CRYPTO-22` | Reads of historical events whose dek_ref no longer exists in crypto_keys MUST automatically fall back to the cold tier (host-owned subjects only). | `INV-39` | pending |
| `INV-CRYPTO-23` | AdminReadStream MUST emit its audit event before delivering any plaintext data; if the audit emit fails, the call MUST refuse. | `INV-42` | pending |
| `INV-CRYPTO-24` | The runtime AuthGuard MUST NEVER return PERMIT for a subject of kind operator; legitimate operator reads go through AdminReadStream. | `INV-43` | pending |
| `INV-CRYPTO-25` | Envelope byte-equality across emit->audit->cold-read: the marshaled Event proto envelope on JetStream MUST be byte-equal to events_audit.envelope and to the cold-tier reader's recovered envelope bytes. | `INV-49` | pending |
| `INV-CRYPTO-26` | Read-back decryption MUST occur host-side; the plugin MUST NOT receive or hold a DEK — it receives only plaintext or a refusal. | `INV-RB-1` | pending |
| `INV-CRYPTO-27` | A plugin read-back decrypt MUST pass two gates evaluated once each (default-deny): (g1) host-side OwnerMap subject-ownership at primitive entry; (g2) manifest crypto.emits[].readback:true via PluginCanReadBack. | `INV-RB-2` | pending |
| `INV-CRYPTO-28` | Every read-back decrypt MUST emit an INV-19 plugin_decrypt audit event on a subject the plugin cannot subscribe to; the primitive MUST fail closed if the audit emitter is absent. | `INV-RB-3` | pending |
| `INV-CRYPTO-29` | AAD for read-back decrypt MUST be built by routing the row through AuditRowToEvent + aad.Build (delegating to decodeAuthorizeAndDispatch, not reimplementing decode); a row whose fields mismatch the bound AAD MUST fail decrypt. | `INV-RB-4` | pending |
| `INV-CRYPTO-30` | INV-P7-7 (downgrade refusal) and INV-P7-15 (DEK-existence) MUST apply on every read-back path — snapshot direct entry and routed fence — identically to the pre-existing fence behavior. | `INV-RB-5` | pending |
| `INV-CRYPTO-31` | The snapshot MUST read its IC events via the plugin's in-tx SQL read + the direct decrypt entry; it MUST NOT route through PluginAuditService.QueryHistory (no self-loop). | `INV-RB-6` | pending |
| `INV-CRYPTO-32` | The fence clean-row path MUST return decrypted plaintext to a routed reader authorized by AuthGuard DEK-participant-set membership; a non-member MUST receive a refused/metadata-only row. | `INV-RB-7` | pending |
| `INV-CRYPTO-33` | Snapshot read+decrypt+render MUST complete before the write-tx; the in-tx SELECT FOR UPDATE re-validation of COOLOFF + all-yes MUST be the serialization point; a vote-flip between read and write MUST yield a no-op commit. | `INV-RB-8` | pending |
| `INV-CRYPTO-34` | The capability, fence, and audit MUST apply identically to binary and Lua plugins (runtime symmetry). | `INV-RB-9` | pending |
| `INV-CRYPTO-35` | A snapshot decrypt failure MUST transition the attempt to ATTEMPT_FAILED with failure_reason = SNAPSHOT_DECRYPT_FAILED. | `INV-RB-10` | pending |
| `INV-CRYPTO-36` | The decrypt primitive and fence completion MUST be subject-agnostic: any plugin-owned sensitivity:always subject MUST flow through the identical primitive with no per-plugin special-casing. | `INV-RB-11` | pending |
| `INV-CRYPTO-37` | DecryptOwnAuditRows MUST return a per-row result (plaintext or typed refusal), never all-or-nothing, with ordering matching the input; any refusal/error is a publish failure. | `INV-RB-12` | pending |
| `INV-CRYPTO-38` | The per-plugin audit dispatcher MUST forward ciphertext bytes byte-equal to what arrived on JetStream when App-Codec is non-identity; no decode-to-plaintext step occurs before forwarding. | `INV-P7-1` | pending |
| `INV-CRYPTO-39` | The dispatcher MUST populate AuditRow.codec/dek_ref/dek_version from the JS App-Codec/App-Dek-Ref/App-Dek-Version headers using the shared parser also used by the events_audit projection writer. | `INV-P7-2` | pending |
| `INV-CRYPTO-40` | pluginsdk.StoreFromMessage round-tripped through pluginsdk.LoadForQuery MUST yield byte-equal payload, identical projection fields, and identical codec/dek_ref/dek_version typed values. | `INV-P7-5` | pending |
| `INV-CRYPTO-41` | A plugin's stored audit row MUST byte-equal the row received via the AuditEvent RPC: (payload, codec, dek_ref, dek_version) are written and returned verbatim (extends master INV-46). | `INV-P7-6` | pending |
| `INV-CRYPTO-42` | The host QueryStreamHistory handler MUST refuse a plugin-returned row where codec=identity AND type is in the manifest-derived always-sensitive set (keyed by qualified <plugin>:<type>), emitting AUDIT_ROW_DOWNGRADE_DETECTED + plugin_integrity_violation (re-scopes master INV-50). | `INV-P7-7` | pending |
| `INV-CRYPTO-43` | The downgrade-fence refusal MUST be per-row and NOT stream-fatal — the stream continues after a single-row refusal (corrected v3 design). | `INV-P7-7b` | pending |
| `INV-CRYPTO-44` | The always-sensitive set used by the fence MUST be built once at server boot and be immutable for the server's lifetime; a regression introducing hot-reload without atomicity MUST be caught. | `INV-P7-8` | pending |
| `INV-CRYPTO-45` | The dispatcher's KeySelector MUST be the SAME KeySelector instance the host's hot-tier reader uses — no second selector, no parallel cache. | `INV-P7-9` | pending |
| `INV-CRYPTO-46` | The dispatcher MUST NOT decrypt to plaintext before forwarding to the plugin; the plugin receives ciphertext and the host (only) decrypts. | `INV-P7-11` | pending |
| `INV-CRYPTO-47` | The plugin's stored row MUST NOT carry any cleartext content for sensitive events — the plugin sees codec=xchacha20poly1305-v1 + ciphertext bytes only. | `INV-P7-12` | pending |
| `INV-CRYPTO-48` | Plugin code MUST NOT have a path that writes directly to host-owned tables (events_audit, crypto_keys); the plugin Postgres role lacks USAGE on schema public. | `INV-P7-13` | pending |
| `INV-CRYPTO-49` | Phase 7 MUST NOT add a second emit-time sensitivity gate — the crypto.emits manifest declaration enforced by sensitivity_fence.go (INV-6/INV-7) is the sole emit-time gate. | `INV-P7-14` | pending |
| `INV-CRYPTO-50` | The host QueryStreamHistory plugin path MUST refuse any plugin-returned row where codec!=identity AND dek_ref is absent or not present in crypto_keys (destroyed_at IS NULL filter); refusal surfaces as metadata_only=true (carries master INV-48). | `INV-P7-15` | pending |
| `INV-CRYPTO-51` | The AuditRow -> *eventbusv1.Event adapter MUST produce a value whose AAD reconstruction is byte-equal to the AAD used at encrypt for the same event_id (superseded by INV-STORE-5 at full ns resolution; ADR holomush-f5h0). | `INV-P7-16` | pending |
| `INV-CRYPTO-52` | Phase C.0 substrate: the plugin audit router MUST stamp the AuditRow.of (origin) on each routed row and expose it via the accessor used by the dispatcher. | `INV-P7-C0` | pending |
| `INV-CRYPTO-53` | AdminReadStream MUST emit the crypto.system.operator_read audit event and observe a successful OperatorReadAuditEmitter.EmitStart ack BEFORE sending any ReadStarted or Event frame. | `INV-F1` | pending |
| `INV-CRYPTO-54` | If the pre-data audit publish fails, AdminReadStream MUST return DENY_AUDIT_PRE_DATA_PUBLISH and MUST NOT invoke HistoryReader.QueryHistory. | `INV-F2` | pending |
| `INV-CRYPTO-55` | AdminReadStream MUST reject with DENY_OPERATOR_CAPABILITY when the operator lacks crypto.operator, BEFORE any audit emit. | `INV-F3` | pending |
| `INV-CRYPTO-56` | (until - since) > MaxWindow MUST return DENY_OPERATOR_READ_WINDOW_TOO_LARGE BEFORE the pre-data audit emit. | `INV-F6` | pending |
| `INV-CRYPTO-57` | OperatorReadStartPayload MUST persist both the Requested-prefixed (nullable, capturing defaulting) and Resolved-prefixed (always populated) since/until/contexts fields. | `INV-F7` | pending |
| `INV-CRYPTO-58` | ReadStarted.request_id, OperatorReadStartPayload.RequestID, the start event ID, and OperatorReadCompletedPayload.RequestID MUST all be equal. | `INV-F8` | pending |
| `INV-CRYPTO-59` | The crypto.system.operator_read_completed event's prev_hash MUST equal the recomputed self_hash of its corresponding operator_read start event; both share NATS subject events.<game>.system.operator_read.<request_id>. | `INV-F9` | pending |
| `INV-CRYPTO-60` | Completion-audit publish failure MUST NOT raise an error (data already delivered; the pre-data audit is the integrity anchor); it MUST be logged at WARN and counted via holomush_admin_readstream_completed_audit_failures_total. | `INV-F10` | pending |
| `INV-CRYPTO-61` | Dual-control: when req.dual_control=true and GetByOpArgsHash returns NOT_FOUND, the handler MUST send exactly one PendingApproval frame and block via Repo.WaitForApproval; no in-process pending-approval registry. | `INV-F11` | pending |
| `INV-CRYPTO-62` | F's classifier (classify.go::Classify) MUST match its documented matrix; every branch corresponds to a production-verified error producer, and unknown errors MUST surface as NO_PLAINTEXT_REASON_INTERNAL with a WARN log. | `INV-F12` | pending |
| `INV-CRYPTO-63` | crypto.system.operator_read and crypto.system.operator_read_completed MUST be registered in internal/core/builtins.go with DisplayTarget == EVENT_CHANNEL_AUDIT_ONLY. | `INV-F13` | pending |
| `INV-CRYPTO-64` | A per-frame write deadline (WriteDeadline, default 30s) MUST be enforced via sendWithDeadline; total stream duration MUST NOT be capped. | `INV-F14` | pending |
| `INV-CRYPTO-65` | F MUST set HistoryQuery.SensitiveOnly=true on every cold-tier query (canonical server-side WHERE dek_ref IS NOT NULL filter); identity-codec rows MUST NOT reach the operator's stream. | `INV-F15` | pending |
| `INV-CRYPTO-66` | The NoPlaintextReason enum expansion (4→7) MUST preserve INV-GW-14 parity, and the new values (DEK_MISSING, DEK_BAD_COLUMNS, INTERNAL) MUST NOT be stamped by cold_postgres.go or history/dispatcher.go — F's classifier is the only producer. | `INV-F16` | pending |
| `INV-CRYPTO-67` | approval.Repo.GetByOpArgsHash MUST apply all filters server-side (op_kind, op_args_hash, expires_at>now(), approved_at IS NOT NULL, primary_player_id != excludePlayerID); tiebreaker = most recently approved. | `INV-F17` | pending |

### `INV-PRIVACY`

| ID | Summary | Legacy | Binding |
|----|---------|--------|---------|
| `INV-PRIVACY-1` | A session may read only events from the interval its session row has existed for that stream's scope (active/idle/detached-within-TTL); the session-row lifetime is the continuity unit. ABAC read_unrestricted_history grants a limited bypass (location hard-gate only; temporal floor still applies). | `I-PRIV-1` | pending |
| `INV-PRIVACY-2` | Guest sessions get a temporal floor of MAX(scope_floor, guest_character.CreatedAt) on all stream history reads. | `I-PRIV-2` | pending |
| `INV-PRIVACY-3` | Subscribe.ReattachCAS and SelectCharacter reattach leave LocationArrivedAt UNCHANGED and MUST NOT change the durable's DeliverPolicy/OptStartTime/OptStartSeq (FilterSubjects may change). | `I-PRIV-3` | pending |
| `INV-PRIVACY-4` | Idle status change and transport/SelectCharacter reattach MUST NOT advance LocationArrivedAt. | `I-PRIV-4` | pending |
| `INV-PRIVACY-5` | All denial paths (hard-gate, I-17, ABAC, expired/missing session) return the same wire code STREAM_ACCESS_DENIED; the internal denial_reason is slog-only and never crosses the wire. | `I-PRIV-5` | pending |
| `INV-PRIVACY-6` | ABAC staff override bypasses the hard-gate location-match only, NOT the temporal floor. | `I-PRIV-6` | pending |
| `INV-PRIVACY-7` | Plugin-owned subjects with divergent history-replay semantics MUST declare history_scope in the manifest and be exercised by a test; silent inheritance of permissive semantics is forbidden. | `I-PRIV-7` | pending |
| `INV-PRIVACY-8` | OpenSession (incl. reattach) and SetFilters query the existing durable before CreateOrUpdateConsumer; an existing durable's DeliverPolicy/OptStartTime/OptStartSeq are copied verbatim (only FilterSubjects mutates); NATS is the source of truth. | `I-PRIV-8` | pending |

### `INV-PRESENCE`

| ID | Summary | Legacy | Binding |
|----|---------|--------|---------|
| `INV-PRESENCE-1` | Snapshot returns only Active sessions; Detached/Expired excluded. | `I-PRES-1` | pending |
| `INV-PRESENCE-2` | Snapshot exempt from I-PRIV-1 temporal floor (timeless current state). | `I-PRES-2` | pending |
| `INV-PRESENCE-3` | Ownership failures collapse to SESSION_NOT_FOUND (enumeration-safe). | `I-PRES-3` | pending |
| `INV-PRESENCE-4` | RPC ABAC-gated by action=list_presence on resource=location:<id>. | `I-PRES-4` | pending |
| `INV-PRESENCE-5` | Non-empty FocusMemberships → UNIMPLEMENTED; no silent fallback. | `I-PRES-5` | pending |
| `INV-PRESENCE-6` | Caller's own session included when status+location qualify. | `I-PRES-6` | pending |
| `INV-PRESENCE-7` | PresenceEntry has exactly 3 fields: character_id, character_name, state. | `I-PRES-7` | pending |
| `INV-PRESENCE-8` | Client presence map keyed by character_id; idempotent add/remove. | `I-PRES-8` | pending |
| `INV-PRESENCE-9` | Response deduplicates by character_id (defense-in-depth). | `I-PRES-9` | pending |

### `INV-SCENE`

| ID | Summary | Legacy | Binding |
|----|---------|--------|---------|
| `INV-SCENE-1` | All Phase-4 plugin-owned scene events MUST emit to dot-style NATS subjects (events.<game_id>.scene.<scene_id>.<facet>); legacy colon-style scene:<id>:* MUST NOT appear in any pub/sub topic context. | `INV-P4-1` | pending |
| `INV-SCENE-2` | The 8 scene event types MUST be declared in core-scenes plugin.yaml crypto.emits AND registered via EmitTypeRegistrar.RegisterEmitTypes; the two sets MUST be set-equal. | `INV-P4-2` | pending |
| `INV-SCENE-3` | Sensitivity classification MUST be: scene_pose/say/emit/ooc are always; scene_join_ic/leave_ic/pose_order_changed_ic/idle_nudge are never. No may-classified events in Phase 4. | `INV-P4-3` | pending |
| `INV-SCENE-4` | GetPoseOrder MUST gate non-participant callers via a direct scene_participants membership check before any computation; the ABAC engine MUST NOT be consulted for this gate. | `INV-P4-4` | pending |
| `INV-SCENE-5` | AttributeResolverService.ResolveResource MUST NOT expose pose-order data (last_pose_at/last_pose_seq/total_pose_count) as a scene attribute; pose-order is reachable only via the gated GetPoseOrder RPC. | `INV-P4-5` | pending |
| `INV-SCENE-6` | Non-participants in the same physical location MUST NOT receive scene IC events (closes audit-finding holomush-ac50). | `INV-P4-6` | pending |
| `INV-SCENE-7` | Pose-order computation MUST produce correct results for each of the 4 modes (strict, 3pr, 5pr, free) across empty/single/multi participants and turn-edge cases. | `INV-P4-7` | pending |
| `INV-SCENE-8` | Maintained pose-order metadata (scenes.total_pose_count, scene_participants.last_pose_at/last_pose_seq) MUST be a function of scene_log scene_pose rows; the recovery SQL MUST reproduce identical values. | `INV-P4-8` | pending |
| `INV-SCENE-9` | Late-joining participants MUST see only IC events from scene_participants.joined_at forward via QueryStreamHistory; pose-order computation remains scene-global. | `INV-P4-9` | pending |
| `INV-SCENE-10` | scene_pose audit-row insertion AND pose-metadata update MUST be transactional — either both commit or both roll back. | `INV-P4-10` | pending |
| `INV-SCENE-11` | scene pose/say/emit/ooc subcommands MUST require the actor to be a participant of the target scene (inherits the write-scene-as-participant ABAC policy via command-capability pre-flight). | `INV-P4-11` | pending |
| `INV-SCENE-12` | scene update with pose_order_mode in update_mask MUST require the actor to be the scene owner (inherits the update-own-scene ABAC policy). | `INV-P4-12` | pending |
| `INV-SCENE-13` | Meta: every numbered INV-P4-* MUST have at least one cited test file; the §12.1 coverage matrix MUST exist. | `INV-P4-13` | pending |
| `INV-SCENE-14` | Focus-without-membership MUST NOT be possible: substrate validates against info.FocusMemberships inside the SessionConnectionMutator callback before applying any non-nil Connection.FocusKey; validation and write are atomic under one Store-lock. | `INV-P5-1` | pending |
| `INV-SCENE-15` | Each Connection has exactly one FocusKey at all times (nil = grid; otherwise a single FocusKey) — no multiple focuses per connection. | `INV-P5-2` | pending |
| `INV-SCENE-16` | The focus-managed subset of Connection.Streams is a deterministic function of (FocusKey, character-level always-on streams); plugin-added streams co-exist additively. | `INV-P5-3` | pending |
| `INV-SCENE-17` | AutoFocusOnJoin terminal-only filter: ClientType in {terminal, telnet}; comms_hub connections are NEVER auto-focused. | `INV-P5-4` | pending |
| `INV-SCENE-18` | On reconnect, focus restoration validates info.PresentingFocus against info.FocusMemberships inside the SessionConnectionMutator callback under one Store-lock; falls back to grid on failure with no read-then-mutate race. | `INV-P5-5` | pending |
| `INV-SCENE-19` | The 3 new PluginHostService focus RPCs ship with Go SDK + Lua hostfunc bindings together (substrate-contract parity). | `INV-P5-6` | pending |
| `INV-SCENE-20` | Phase-5 multi-field focus mutations (Connection.FocusKey + Info.PresentingFocus) MUST be applied via a single SessionConnectionMutator invocation under one Store-lock — both fields atomic; no observer sees torn state. | `INV-P5-7` | pending |
| `INV-SCENE-21` | Meta: every numbered INV-P5-N declaration MUST cite at least one existing test path. | `INV-P5-8` | pending |
| `INV-SCENE-22` | ULID encoding boundary (D6): proto wire = 16-byte bytes; Lua hostfunc accepts 26-char base32; malformed input → INVALID_ULID; Go SDK + Lua round-trip a known ULID identically. | `INV-P5-9` | pending |
| `INV-SCENE-23` | SessionStreamRegistry.SendToConnection delivers an update to EXACTLY the named connection's channel; other connections in the same session do NOT receive it via this path. | `INV-P5-10` | pending |
| `INV-SCENE-24` | AutoFocusOnJoin MUST skip a connection whose FocusKey is already non-nil and different from the target; the skipped conn_id is reported and its FocusKey is unchanged. | `INV-P5-11` | pending |
| `INV-SCENE-25` | Reconnect restoration vs concurrent LeaveFocus serializes via the single Store-lock — either leave-first (grid fallback) or restore-first (leave's scene_leave_ic reaches the subscribed connection); no torn state. | `INV-P5-12` | pending |
| `INV-SCENE-26` | scene grid MUST NOT modify info.PresentingFocus; the per-Connection FocusKey is cleared to nil while the session-level reconnect target is preserved. | `INV-P5-13` | pending |
| `INV-SCENE-27` | Postgres UpdateSessionConnection MUST lock the sessions row FIRST via FOR UPDATE, then the session_connections row (canonical order) — pinned by a deadlock-detector regression test. | `INV-P5-14` | pending |
| `INV-SCENE-28` | Publication-vote rosters are frozen at attempt creation and immutable for the attempt's lifetime; owner+member roles only, invited rows excluded. | `INV-P6-1` | pending |
| `INV-SCENE-29` | A vote MAY be cast/changed any number of times during COLLECTING; once in COOLOFF, votes MAY change only by voting no (which transitions back to COLLECTING). | `INV-P6-2` | pending |
| `INV-SCENE-30` | Only the scene owner MAY withdraw an active attempt; opposed participants express their position by voting no, not via withdraw. | `INV-P6-3` | pending |
| `INV-SCENE-31` | A scene transitions to archived ONLY on PUBLISHED; ATTEMPT_FAILED does not advance scene state; attempts-exhausted scenes stay ended indefinitely. | `INV-P6-4` | pending |
| `INV-SCENE-32` | The IsParticipant gate at GetPublishedScene/DownloadPublishedScene/ListScenePublishAttempts MUST execute before any DB query against published_scenes.content_entries or published_scene_votes. | `INV-P6-5` | pending |
| `INV-SCENE-33` | The ABAC engine MUST NOT be called during participant-gated publication RPC handlers (the participant-only read path forbids ABAC). | `INV-P6-6` | pending |
| `INV-SCENE-34` | AttributeResolverService.ResolveResource MUST NOT return scene content (poses, says, emits, OOC, publication content_entries) under any attribute name. | `INV-P6-7` | pending |
| `INV-SCENE-35` | GetPublicSceneArchive/DownloadPublicSceneArchive MUST return opaque NOT_FOUND for any non-PUBLISHED publication; the wire shape is identical for nonexistent/COLLECTING/COOLOFF/ATTEMPT_FAILED. | `INV-P6-8` | pending |
| `INV-SCENE-36` | Hard-privacy-boundary blocks MUST emit a WARN log AND increment scene_publish_privacy_blocks_total AND mark the OTel span error with deny.reason; NO IC stream event is emitted (side-channel prevention). | `INV-P6-9` | pending |
| `INV-SCENE-37` | Snapshot at COOLOFF → PUBLISHED MUST be atomic (SELECT FOR UPDATE + content build + UPDATE + scene-state change in one transaction); failures transition to ATTEMPT_FAILED without partial state. | `INV-P6-10` | pending |
| `INV-SCENE-38` | Per-connection focus-delta delivery MUST be driven inside focus.Coordinator; no runtime-specific layer may be its sole driver. | `INV-FS-1` | pending |
| `INV-SCENE-39` | A character joining a binary-plugin scene MUST receive live IC/OOC poses via per-connection delivery under production-equivalent wiring (ex-ymgjs INV-FW-1). | `INV-FS-2` | pending |
| `INV-SCENE-40` | A character joining a scene via the Lua focus path MUST receive the same per-connection deltas as the binary path. | `INV-FS-3` | pending |
| `INV-SCENE-41` | Production and the integration-test harness MUST build the coordinator's focus-delivery wiring through holoGRPC.FocusStreamCoordinatorOptions; no hand-rolled parallel adapter assembly (ex-ymgjs INV-FW-2). | `INV-FS-4` | pending |
| `INV-SCENE-42` | The StreamSender and ConnectionSender produced for one coordinator MUST target the same SessionStreamRegistry (ex-ymgjs INV-FW-4). | `INV-FS-5` | pending |
| `INV-SCENE-43` | The session-level StreamSenderAdapter MUST continue to reject non-FromCursor replay modes with REPLAY_MODE_NOT_SUPPORTED (ex-ymgjs INV-FW-5). | `INV-FS-6` | pending |
| `INV-SCENE-44` | The Lua hostfunc stream-registry wiring (hostfunc.WithStreamRegistry) MUST be preserved (ex-ymgjs INV-FW-6). | `INV-FS-7` | pending |
| `INV-SCENE-45` | A SendToConnection failure MUST NOT fail the focus mutation or abort delivery to the remaining focused connections, and MUST be logged. | `INV-FS-8` | pending |
| `INV-SCENE-46` | newSceneID() returns a bare 26-character ULID with no scene- (or any) prefix. | `INV-Y5INX-1` | pending |
| `INV-SCENE-47` | A scene created via CreateScene is readable via the host CoreServer.QueryStreamHistory by a participant (scene log returns the events). | `INV-Y5INX-2` | pending |
| `INV-SCENE-48` | Joining a real scene opens a focus subscription: protoToFocusKey parses the bare join key and JoinFocus succeeds. | `INV-Y5INX-3` | pending |
| `INV-SCENE-49` | The history-scope temporal floor (streamScopeFloor) excludes pre-join events for a late participant of a scene. | `INV-Y5INX-4` | pending |
| `INV-SCENE-50` | No production code path strips a scene- prefix from an identifier or subject token (the masking strip lives only in the test harness). | `INV-Y5INX-5` | pending |
| `INV-SCENE-51` | WithPluginConfigOverrides reaches PluginSubsystemConfig.PluginConfigOverrides so core-scenes runs with the test's cooloff_window/scheduler_interval. | `INV-SH-1` | pending |
| `INV-SCENE-52` | Server.SceneServiceClient() resolves the loaded plugin's SceneService (read-back works). | `INV-SH-2` | pending |
| `INV-SCENE-53` | Session.CreateScene returns a valid scene ULID via the real RPC (no t.Fatalf). | `INV-SH-3` | pending |
| `INV-SCENE-54` | The driving layer adds zero production code: SceneServiceClient uses the existing ServiceRegistry() getter; all new symbols are integration-build-tagged. | `INV-SH-4` | pending |
| `INV-SCENE-55` | The happy-path lifecycle drives to PUBLISHED through SendCommand + reads back via the client (E6 acceptance). | `INV-SH-5` | pending |
| `INV-SCENE-56` | Every board row MUST display its content-warning labels regardless of active filters; display MUST NOT be suppressible by a filter. | `INV-2` | pending |
| `INV-SCENE-57` | With no game-configured taxonomy, core-scenes' DefaultCWTaxonomy MUST apply; a fresh game validates/filters correctly with zero operator configuration. | `INV-5` | pending |
| `INV-SCENE-58` | content.cw_block resolution MUST be the union of GAME, PLAYER, and CHARACTER scope lists (safety-accumulating), not first-match-wins. | `INV-6` | pending |
| `INV-SCENE-59` | Scene settings/sensitivity access MUST be ABAC-authorized and default-deny: a principal may read/write its own PLAYER/CHARACTER settings; GAME-scope writes require an operator action. | `INV-7` | pending |
| `INV-SCENE-60` | The hard privacy boundary for scene-log reads MUST remain plugin-code-enforced; ABAC MUST NOT be in the path for scene-log reads (a non-participant read fails before the ABAC engine is consulted). | `INV-S9` | pending |

### `INV-PLUGIN`

| ID | Summary | Legacy | Binding |
|----|---------|--------|---------|
| `INV-PLUGIN-1` | The host MUST NOT interpret plugin config-key meaning — only declared generic types (no plugin-config key literals in host packages). | `INV-PC-1` | pending |
| `INV-PLUGIN-2` | Effective config = manifest defaults overlaid by server override, per key; override wins. | `INV-PC-2` | pending |
| `INV-PLUGIN-3` | The host builds one merged value map per plugin; both binary (plugin_config) and Lua (holomush.config) delivery carry that same map — parity enforced at the host-merge layer, not re-derived per runtime. | `INV-PC-3` | pending |
| `INV-PLUGIN-4` | A required key absent from both manifest default and override → fail-fast at plugin load (PLUGIN_CONFIG_MISSING_REQUIRED). | `INV-PC-4` | pending |
| `INV-PLUGIN-5` | A value that does not parse to its declared type → fail-fast at load (PLUGIN_CONFIG_TYPE_INVALID). | `INV-PC-5` | pending |
| `INV-PLUGIN-6` | An override key not declared in the manifest schema → fail-fast at load (PLUGIN_CONFIG_UNKNOWN_KEY). | `INV-PC-6` | pending |
| `INV-PLUGIN-7` | With no override (production), core-scenes resolves vote_window=168h, cooloff_window=30m, scheduler_interval=30s (cfg-zero regression lock). | `INV-PC-7` | pending |
| `INV-PLUGIN-8` | A binary plugin declaring config: MUST receive Init (and its plugin_config) even with none of requires/provides/storage/crypto.emits — the needsInit gate MUST include len(manifest.Config) > 0. | `INV-PC-8` | pending |
| `INV-PLUGIN-9` | Every Actor at every layer and kind carries a ULID identity; system sentinels resolve via NameByID after Manager bootstrap. | `INV-W9ML-1` | pending |
| `INV-PLUGIN-10` | IdentityRegistry is the sole resolution path for actor identity (IDByName at stamp sites, NameByID at render sites). | `INV-W9ML-2` | pending |
| `INV-PLUGIN-11` | Plugin name uniqueness: two active plugins with the same name — the second load fails with a constraint violation. | `INV-W9ML-3` | pending |
| `INV-PLUGIN-12` | A plugin's ULID is stable across manifest updates, content updates, and unload/reload within the retention window. | `INV-W9ML-4` | pending |
| `INV-PLUGIN-13` | Plugin policies are lifecycle-coupled (installed/removed with the plugin). | `INV-W9ML-5` | pending |
| `INV-PLUGIN-14` | No production-shape-discipline regressions (documentary; verified by spec review). | `INV-W9ML-6` | pending |
| `INV-PLUGIN-15` | Clean wire format: no legacy-ID references in production code (grep gate over non-doc, non-generated files returns zero matches). | `INV-W9ML-7` | pending |
| `INV-PLUGIN-16` | Sweep ordering: the retention GC sweep MUST NOT collect a plugin loaded this cycle. | `INV-W9ML-8` | pending |
| `INV-PLUGIN-17` | No deletion: production code MUST NOT issue DELETE FROM plugins (grep gate returns zero matches outside explicit test fixtures). | `INV-W9ML-9` | pending |
| `INV-PLUGIN-18` | WithInTreePlugins() MUST reuse setup.PluginSubsystem and MUST NOT construct plugins.NewManager directly in the harness. | `INV-WS-1` | pending |
| `INV-PLUGIN-19` | The whole-system suite MUST assert >=1 cross-plugin-ABAC permit AND >=1 forbid against the real seeded engine. | `INV-WS-2` | pending |
| `INV-PLUGIN-20` | The whole-system suite MUST NOT be silently skipped in CI: with HOLOMUSH_REQUIRE_PLUGINS set, a missing binary artifact is a hard failure. | `INV-WS-3` | pending |
| `INV-PLUGIN-21` | WithInTreePlugins() MUST be opt-in: omitting it leaves the harness plugin-free and behaviorally unchanged. | `INV-WS-4` | pending |
| `INV-PLUGIN-22` | PluginHostService.Evaluate's subject is host-derived from the authenticated actor; there is no subject field on the wire (never sourced from plugin/Lua-supplied data). | `INV-1` | pending |
| `INV-PLUGIN-23` | No authenticated actor bound to the call → Evaluate returns deny + error (fail-closed). | `INV-2` | pending |
| `INV-PLUGIN-24` | A resource type the plugin does not own (outside its entitlement, no command carve-out) → rejected. | `INV-3` | pending |
| `INV-PLUGIN-25` | Each Evaluate emits exactly one host-stamped audit event; the audit logger MUST be wired on both the binary (gRPC) and Lua (hostfunc) surfaces. | `INV-4` | pending |
| `INV-PLUGIN-26` | The binary (gRPC) and Lua (hostfunc) surfaces reach identical host evaluation logic via a single shared mapping (runtime parity). | `INV-5` | pending |
| `INV-PLUGIN-27` | Each settings host RPC MUST ship a Go SDK method AND a Lua hostfunc together (runtime parity); the settings access gate is the single shared path for both runtimes. | `INV-8` | pending |
| `INV-PLUGIN-28` | Cross-plugin settings isolation MUST be structural: the host binds the plugin partition from the authenticated caller identity (stamped at server construction), never from caller-supplied input; a plugin MUST NOT address another plugin's owner partition. | `INV-11` | pending |
| `INV-PLUGIN-29` | Emitting an event with Sensitive=true whose type is not declared in the plugin's crypto.emits as 'may' or 'always' MUST fail at the emit-time fence with EVENT_SENSITIVITY_NOT_DECLARED (over-claim reject). | `INV-6` | pending |
| `INV-PLUGIN-30` | A plugin declaring an event type as sensitivity:always MUST NOT publish that event with Sensitive=false; the emit-time fence rejects with EVENT_SENSITIVITY_REQUIRED (under-claim reject). | `INV-7` | pending |
| `INV-PLUGIN-31` | Every Plugin Host RPC and SDK primitive MUST ship a Go SDK method AND a Lua hostfunc together; asymmetric capability between the binary and Lua runtimes is forbidden. | `INV-S3` | pending |
| `INV-PLUGIN-32` | Every plugin declaring crypto.emits MUST pass startup-time set-equality validation: the manifest-declared emit-type set MUST equal the code-registered emit-type set in both directions, or plugin startup fails closed. | `INV-S5` | pending |

### `INV-EVENTBUS`

| ID | Summary | Legacy | Binding |
|----|---------|--------|---------|
| `INV-EVENTBUS-1` | The gateway process MUST NOT import internal/world, internal/access, internal/store, internal/plugin, internal/eventbus, internal/auth/service, or internal/command. | `INV-GW-1` | pending |
| `INV-EVENTBUS-2` | RenderingPublisher.Publish MUST stamp event.Rendering from the verb registry before publishing. | `INV-GW-2` | pending |
| `INV-EVENTBUS-3` | RenderingPublisher.Publish MUST return EMIT_UNKNOWN_VERB when the verb registry has no entry for event.Type. | `INV-GW-3` | pending |
| `INV-EVENTBUS-4` | JetStreamPublisher.Publish MUST copy event.Rendering into the eventbusv1.Event.Rendering proto field before proto.Marshal; round-trip publish + JetStream consume MUST preserve Rendering byte-for-byte. | `INV-GW-3a` | pending |
| `INV-EVENTBUS-5` | RenderingPublisher.Publish MUST return EMIT_VALIDATION_FAILED when protovalidate.Validate(ev) fails on the stamped frame. | `INV-GW-4` | pending |
| `INV-EVENTBUS-6` | Gateway translation (web + telnet) MUST drop events with Rendering == nil, increment holomush_gateway_dropped_nil_rendering_total, and log an error; MUST NOT render fallback. | `INV-GW-5` | pending |
| `INV-EVENTBUS-7` | Every row in events_audit MUST have a non-nil rendering sub-message after a full E2E run. | `INV-GW-6` | pending |
| `INV-EVENTBUS-8` | RenderingMetadata.label MUST be set when format == "speech"; enforced at the proto layer (CEL) and at VerbRegistry.Register. | `INV-GW-7` | pending |
| `INV-EVENTBUS-9` | RenderingMetadata.display_target MUST NOT be EVENT_CHANNEL_UNSPECIFIED; enforced at the proto layer (enum.not_in: [0]). | `INV-GW-8` | pending |
| `INV-EVENTBUS-10` | RenderingMetadata.source_plugin and source_plugin_version MUST be populated. For builtins, source_plugin == "builtin" and source_plugin_version == "host-<binary version>". | `INV-GW-9` | pending |
| `INV-EVENTBUS-11` | The plugin manager MUST require a non-nil VerbRegistry at construction time; a nil registry returns ErrMissingVerbRegistry. | `INV-GW-10` | pending |
| `INV-EVENTBUS-12` | BootstrapVerbRegistry() MUST be the only public path that returns a registry seeded with host builtins; RegisterBuiltinTypes MUST be unexported. | `INV-GW-11` | pending |
| `INV-EVENTBUS-13` | The audit projection writer MUST read the App-Rendering NATS header and write its JSON value into events_audit.rendering (NOT NULL); a missing, empty, or malformed JSON header MUST fail the insert. | `INV-GW-13` | pending |
| `INV-EVENTBUS-14` | The Go-side eventbus.RenderingMetadata struct and proto-side corev1.RenderingMetadata MUST stay in sync — same field set, same names. | `INV-GW-14` | pending |
| `INV-EVENTBUS-15` | For every event published through RenderingPublisher, the JSON value of the App-Rendering NATS header MUST encode the same RenderingMetadata as the Rendering field inside the proto envelope — the two transports cannot drift. | `INV-GW-15` | pending |
| `INV-EVENTBUS-16` | corev1.EventChannel and webv1.EventChannel MUST stay in lockstep — same enum values, same names, same numeric assignments. | `INV-GW-16` | pending |
| `INV-EVENTBUS-17` | Colon-style subjects appear only as an ABAC policy-DSL identifier, never as a pub/sub stream name (enforced executably by INV-EVENTBUS-19 + INV-EVENTBUS-22). Spec-only — no standalone code annotation. | `INV-ROPS-1` | pending |
| `INV-EVENTBUS-18` | Unclassifiable stream names are rejected at handler entry with INVALID_ARGUMENT, never routed to a default authorization branch. | `INV-ROPS-2` | pending |
| `INV-EVENTBUS-19` | A CI meta-test asserts no production Go or Lua source contains a colon-style entity-prefix literal (location:, character:, scene:, plugin:, …) as a stream name (the eradication gate; ABAC builders are allowlisted). | `INV-ROPS-3` | pending |
| `INV-EVENTBUS-20` | Producer↔subscriber symmetry: an integration test (real embedded NATS) emits through the production producer path for each migrated stream type and asserts a subscriber built from the production filter constructor receives it. | `INV-ROPS-4` | pending |
| `INV-EVENTBUS-21` | Classifier non-collision: a table-driven unit test over the four internal/grpc classifiers asserts location is public-not-scene, character private-not-scene, scene private-and-scene, and unknown/malformed none. | `INV-ROPS-5` | pending |
| `INV-EVENTBUS-22` | Role split both directions: for the same character ULID, the stream is dot (events.<gid>.character.<id>) and the ABAC subject is colon (character:<id>) — guards against an over-eager sweep migrating the ABAC subject. | `INV-ROPS-6` | pending |
| `INV-EVENTBUS-23` | Temporal floor on every private stream: a late joiner cannot read pre-join history on each private stream type (scope floor applied, not zero-floor). StreamProvider populates resource.location + has_location for dot location streams; absent (not empty-sentinel) for non-location streams. | `INV-ROPS-7` | pending |
| `INV-EVENTBUS-24` | Location-seed authorization survives the dot-form flip: an integration test seeds the engine and asserts a co-located character can emit to and read its own dot-form location stream, and a non-co-located character cannot. | `INV-ROPS-8` | pending |
| `INV-EVENTBUS-25` | Plugin audit tables MUST add dek_ref BIGINT NULL and dek_version INTEGER NULL columns (mirror-events_audit contract); the columns are nullable, and identity-codec rows store NULL on both. | `INV-P7-3` | pending |
| `INV-EVENTBUS-26` | Plugin SDK Layer 2: pluginsdk.AuditRow Go struct fields MUST be 1:1 with pluginauditpb.AuditRow proto fields (id, subject, type, timestamp, actor, codec, payload, dek_ref, dek_version). | `INV-P7-4` | pending |
| `INV-EVENTBUS-27` | Plugin migrations MAY run before or after Phase 7's host migration (no host-side schema change beyond Phases 2–5); the two crypto columns added to plugin tables are nullable and require no new host-side support. | `INV-P7-10` | pending |
| `INV-EVENTBUS-28` | New event subjects MUST use the NATS dot-style form events.<game_id>.<domain>.<entity-id>[.<facet>...]; colon-style is legacy and translated at the EventSink boundary. | `INV-S4` | pending |

### `INV-CLUSTER`

| ID | Summary | Legacy | Binding |
|----|---------|--------|---------|
| `INV-CLUSTER-1` | KEK rotation issues a cluster-prefixed NATS request-reply cache-invalidate ping and MUST receive N-of-N replica acks (30s timeout; rollback on timeout). | `INV-28` | pending |
| `INV-CLUSTER-2` | Rotate/Rekey(context) issues a cluster-prefixed cache-invalidate ping and MUST receive N-of-N replica acks before returning (5s timeout; N=1 degenerates to local self-ack; rollback on timeout). | `INV-29` | pending |
| `INV-CLUSTER-3` | Every cluster.Registry member has a unique MemberID; colliding concurrent registration is rejected with CLUSTER_MEMBER_DUPLICATE_ID. | `INV-53` | pending |
| `INV-CLUSTER-4` | All Phase-3c internal coordination subjects are cluster_id-prefixed; members drop messages whose payload cluster_id disagrees with their configured cluster_id. | `INV-54` | pending |
| `INV-CLUSTER-5` | A pill on internal.<cluster_id>.member.poison.<self_id> triggers Pill.Trigger after flushing audit telemetry; the production Pill terminates the process with exit code 125. | `INV-55` | pending |
| `INV-CLUSTER-6` | invalidation.Coordinator attempts at most one probe-and-pill + retry cycle per RequestInvalidation; after the second timeout it returns INVALIDATION_PARTIAL_FAILURE with the missing-member set. | `INV-56` | pending |
| `INV-CLUSTER-7` | cluster.Registry.ProbeAndPill issues at most one attempt per (member_id, reason) per PillRateLimit window (claim-then-probe, closing the TOCTOU race); over-limit returns ErrPillRateLimited without reaching the wire. | `INV-57` | pending |
| `INV-CLUSTER-8` | No Phase-3c decision is conditioned on cross-host wall-clock comparison (enforced by the noremoteclockcompare analyzer; observability-only skew/latency metrics are the carved-out exceptions). | `INV-58` | pending |
| `INV-CLUSTER-9` | A successful RequestInvalidation(participants_changed) leaves every other live member's dek.ParticipantsCache with no entry for (ctxType, ctxId, version) on return (re-fetch from PG). | `INV-59` | pending |
| `INV-CLUSTER-10` | cluster.Registry.ProbeAndPill refuses id==Self() with ErrCannotPillSelf; the Coordinator filters Self() out of the missing-member set (prevents N=1 self-pill). | `INV-60` | pending |

### `INV-ACCESS`

| ID | Summary | Legacy | Binding |
|----|---------|--------|---------|
| `INV-ACCESS-1` | With WithRealABAC(), the CoreServer access engine is the setup.BuildABACStack engine, not allowAllPolicyEngine. | `INV-RA-1` | pending |
| `INV-ACCESS-2` | Without WithRealABAC(), the harness retains the allow-all default (no regression). | `INV-RA-2` | pending |
| `INV-ACCESS-3` | With WithRealABAC(), the seed:* policy set is installed before the engine's cache loads; the engine evaluates against a non-empty seeded policy set. | `INV-RA-3` | pending |
| `INV-ACCESS-4` | With WithRealABAC()+WithInTreePlugins(), the attribute.Resolver and attribute.PluginProvider the plugin subsystem registers on are the SAME instances (pointer identity) the engine evaluates against. | `INV-RA-4` | pending |
| `INV-ACCESS-5` | Every attribute namespace referenced by an installed seed policy has a registered provider under WithRealABAC (no silent default-deny from an unregistered provider). | `INV-RA-5` | pending |
| `INV-ACCESS-6` | Option order MUST NOT affect the resulting stack: Start(t,A,B) and Start(t,B,A) produce identical permit/deny behavior. | `INV-RA-6` | pending |
| `INV-ACCESS-7` | ABAC denies subscribe to events.*.system.* (and audit.>) streams for kind={plugin\|character} at the gRPC subscribe boundary; the Rekey system audit event lands on a subject those principals cannot read. | `INV-15` | pending |
| `INV-ACCESS-8` | Two ABAC seed forbid policies MUST deny character and plugin principals from reading events.*.system.crypto_totp.* streams (sub-epic A; A16 extends INV-15's system-namespace deny across crypto audit namespaces). | `INV-A16` | pending |

### `INV-SESSION`

| ID | Summary | Legacy | Binding |
|----|---------|--------|---------|
| `INV-SESSION-1` | session.Store has exactly one production implementation: store.PostgresSessionStore. | `INV-M-1` | pending |
| `INV-SESSION-2` | sessiontest.NewStore(t) returns a fresh, isolated store per invocation; cross-test state never leaks. | `INV-M-2` | pending |
| `INV-SESSION-3` | PostgresSessionStore.AddConnection rejects invalid client_type (accept terminal/comms_hub/telnet; reject others). | `INV-M-3` | pending |
| `INV-SESSION-4` | Memstore-removal preserves behavioral coverage: every pre-consolidation test is named in a surviving test's // replaces: chain. | `INV-M-4` | pending |

### `INV-STORE`

| ID | Summary | Legacy | Binding |
|----|---------|--------|---------|
| `INV-STORE-1` | All persistent time values stored as BIGINT epoch-ns (UTC); no new TIMESTAMPTZ/TIMESTAMP columns. | `INV-TS-1` | pending |
| `INV-STORE-2` | pgnanos.Time is the canonical scan/insert seam between time.Time and BIGINT epoch-ns; no int64<->time.Time arithmetic outside pgnanos. | `INV-TS-2` | pending |
| `INV-STORE-3` | Application code (production + tests) must not Truncate(time.Microsecond) on any time.Time round-tripping through PG. | `INV-TS-3` | pending |
| `INV-STORE-4` | publisher.Publish does not truncate event.Timestamp before AAD/envelope; the on-wire timestamp carries full nanosecond precision. | `INV-TS-4` | pending |
| `INV-STORE-5` | AAD round-trip publish->persist->read->reconstruct is byte-equal at full nanosecond resolution (strengthens former INV-P7-16). | `INV-TS-5` | pending |
| `INV-STORE-6` | Privacy/scope floor comparisons operate at nanosecond resolution; the dispatchDelivery Truncate(microsecond) is deleted, not stubbed. | `INV-TS-6` | pending |
| `INV-STORE-7` | Sub-microsecond timestamp ties resolve deterministically; the privacy floor uses >= so an event at the exact floor ns is included. | `INV-TS-7` | pending |
| `INV-STORE-9` | TIMESTAMPTZ->BIGINT conversion migrations saturate out-of-range / +/-infinity to int64 bounds, pass NULL through, and convert in-range values exactly (numeric arithmetic). | `INV-TS-9` | pending |

### `INV-TELEMETRY`

| ID | Summary | Legacy | Binding |
|----|---------|--------|---------|
| `INV-TELEMETRY-1` | Load harness drives the web tier over the Connect protocol (not gRPC/gRPC-Web). | `INV-LOAD-1` | pending |
| `INV-TELEMETRY-2` | Load harness drives the telnet tier over raw TCP through the real gateway telnet listener. | `INV-LOAD-2` | pending |
| `INV-TELEMETRY-3` | say->broadcast and page/whisper->delivery latency is computed from an in-payload emit-timestamp recorded by the recipient VU (never cross-VU shared state); generator/SUT clock skew <= 50ms. | `INV-LOAD-3` | pending |
| `INV-TELEMETRY-4` | Load pass/fail verdict is derived from k6's exit code (thresholds), never a substring match on k6 output. | `INV-LOAD-4` | pending |
| `INV-TELEMETRY-5` | SLO thresholds gate against .benchmarks/load-baseline.json (relative regression), not hard-coded absolutes, once a baseline exists. | `INV-LOAD-5` | pending |
| `INV-TELEMETRY-6` | Load scenario must not issue command verbs not registered in the running server (command-availability gating). | `INV-LOAD-6` | pending |
| `INV-TELEMETRY-7` | Load action selection is seeded deterministically so two runs of the same scenario config produce the same action sequence. | `INV-LOAD-7` | pending |
| `INV-TELEMETRY-8` | The load harness must not be wired into task pr-prep (fast lane). | `INV-LOAD-8` | pending |

<!-- END GENERATED: invariant-tables -->
