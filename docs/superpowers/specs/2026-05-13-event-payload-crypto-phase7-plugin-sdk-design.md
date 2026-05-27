<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Event Payload Cryptography — Phase 7: Plugin SDK + Plugin-Owned Audit Crypto Integration

## Status

DRAFT v4. v1 proposed a host-owned crypto-ref side table that
contradicted the master spec's deliberate §8.2 commitment; both gate
reviewers returned NOT READY. v2 reverted to the master spec's intent
(plugin tables mirror events_audit shape including crypto columns,
dispatcher widens to forward ciphertext); both gate reviewers returned
NOT READY again with substantially fewer findings — all tactical
patches, no architectural rethink. v3 applies the patches:

- D1: Corrected proto path `api/proto/holomush/plugin/v1/audit.proto`
  throughout.
- D2: `AuditRow` proto gains `schema_ver` field; `headers` map drop is
  enumerated with all callers + test-stub migration steps.
- D3: `PluginDowngradeFence` placed in `internal/eventbus/audit/`
  alongside `PluginHistoryRouter`; explicit import-direction note.
- D4: INV-P7-8 dropped (no manifest hot-reload exists today); follow-up
  bead `holomush-kl9w` files the future hot-reload work; new invariant
  asserts the no-hot-reload property so future regression is detectable.
- C1: `may`-elevated downgrade gap accepted and documented explicitly
  in §1 threat model + §8 failure modes. Two-layer fence reframed
  honestly: manifest-set layer is `always`-only by design; `may`-elevated
  - tampering on any AAD-bound field falls to the AEAD AAD-binding
  layer with a deliberately worse operator UX (decrypt-side error code,
  no `plugin_integrity_violation` emission).
- C2: New INV-P7-15 carries master spec INV-48 onto the plugin-routed
  read path.
- C3: Master spec polish list expanded — §8.2's `events_audit` fallback
  pseudo-code amended for plugin-owned subjects.
- Plus several WARN-tier clarifications (audit-emission backpressure
  cross-ref; qualified vs bare event-type form; operator-with-shell
  threat ack).
The v2 architectural direction is preserved verbatim.

### v4 patches (over v3)

v3 review found 5 BLOCKING findings (3 design + 2 crypto). Two were
errors I introduced by trusting unverified prior claims. v4 patches:

- **D1 (import cycle)**: v3 placed `PluginDowngradeFence` in
  `internal/eventbus/audit/` while wrapping the `history.PluginHistoryRouter`
  interface. `internal/eventbus/history/tier.go:39` already imports
  `internal/eventbus/audit` for `audit.OwnerMap` — v3's edge would form
  a cycle. Go has no "interface-only" import distinction. **v4 reverts
  the fence placement** to `internal/eventbus/history/plugin_downgrade_fence.go`
  (v2's location). The fence wraps the *interface* `history.PluginHistoryRouter`
  (defined at `tier.go:79-84`), not the concrete `audit.PluginHistoryRouter`
  type — no concrete-coupling, no cycle. Composition at wiring time:
  `NewPluginDowngradeFence(concreteRouter)`. v2's design-reviewer was
  wrong to reject this placement on the "concrete impl lives in audit"
  grounds — interface wrapping doesn't require co-location with the
  concrete.
- **D2 (companion bead `holomush-demb`)**: Verified by direct read of
  `plugins/core-scenes/audit.go:141` — the INSERT statement ALREADY has
  `ON CONFLICT (id) DO NOTHING`. The v2 design-reviewer's claim that
  it was omitted was wrong; v3 propagated the wrong claim into a bead.
  **v4 removes all `holomush-demb` references**; bead is closed as
  no-op. Memory `feedback_verify_prior_claims_before_propagating`
  records the meta-pattern.
- **D3 (master INV-39 contradiction)**: INV-P7-15 says "no fallback
  for plugin-owned subjects" while master INV-39 unqualifiedly mandates
  cold-tier fallback. **v4 adds polish item 5**: INV-39 scope
  clarification — host-owned subjects only; plugin-owned subjects
  terminate with `metadata_only=true` per INV-P7-15 (plugin row IS
  cold).
- **C1 (fence layer framing)**: INV-P7-15 is a PRE-decrypt check; v3's
  two-layer framing called only manifest-set + AEAD. **v4 restates the
  fence as two layers with clearer scope:** layer (1) is the
  **QueryStreamHistory pre-decrypt fence** covering both manifest-set
  downgrade (INV-P7-7) AND DEK existence (INV-P7-15); layer (2) is the
  AEAD AAD-binding at decrypt (master INV-25). Identical wording lands
  in §1, §3.4, §8 prose, and the master spec §8.2 amendment.
- **C2 (AAD reconstruction call-site)**: `aad.Build` (verified at
  `internal/eventbus/crypto/aad/aad.go:62`) takes `*eventbusv1.Event`,
  not `pluginauditpb.AuditRow`. Call sites at
  `internal/eventbus/history/dispatcher.go:179, 322` use the host's
  envelope proto today. v4 specifies in §5.4 the
  `AuditRow → *eventbusv1.Event` adapter (per-field copy: id, subject,
  type, timestamp, actor), pins the `aad.Build` input source for
  plugin-routed reads, and adds INV-P7-16 + integration test
  `plugin_aad_reconstruction_byte_equal_test.go` asserting byte-exact
  AAD reconstruction.
- **WARN-tier patches**:
  - Schema_ver routed through shared parser (crypto-reviewer WARN D).
  - TOCTOU on `crypto_keys.destroyed_at` accepted explicitly in §8
    (both error surfaces land at `metadata_only=true` per master
    INV-26).
  - INV-P7-15 wording clarified (crypto-reviewer NIT).
  - `plugin_integrity_violation` payload operator-correlation gap
    documented (operator forensic correlates via JetStream message
    store; events_audit has no row for plugin-owned subjects).

## Authors

Sean Brandt + Claude Opus 4.7

## Date

2026-05-13 (v4)

## Context

### Project context

Phase 7 of the Event Payload Cryptography epic (`holomush-1r0v`, parent
`holomush-e49r`). Phases 2–5 shipped the crypto substrate for
host-emitted events. The substrate stops at the plugin boundary:
`internal/eventbus/audit/plugin_consumer.go:332-335` documents this
explicitly —

> "Only the identity codec is supported here because
> PluginConsumerManager has no KeySelector wired **yet** — a
> non-identity codec returns an error so misconfigurations surface at
> dispatch time rather than forwarding ciphertext to the plugin."

Phase 7 closes that gap. After Phase 7, the per-plugin audit consumer
forwards ciphertext + crypto headers to the plugin's `AuditEvent` RPC;
plugin-owned audit tables mirror `events_audit`'s shape (payload +
codec + dek_ref + dek_version); the plugin's `QueryHistory` returns
ciphertext bytes byte-equal to what the bus carried; the host's
`QueryStreamHistory` decrypts and delivers (or refuses, per the new
read-side downgrade fence). Third-party plugin authors get the
encrypt/decrypt guarantees without owning crypto code.

### Out of scope (non-goals)

- Backfill of pre-Phase-7 plugin audit rows. Plugin migration ADDS the
  two crypto columns (`dek_ref BIGINT NULL`, `dek_version INTEGER
  NULL`); existing rows get NULL on both, which is correct for
  identity-codec rows. Not a clean break — the additive migration is
  the natural shape since the new columns are nullable.
- New sensitivity tiers beyond `always` / `may` / `never`. Existing
  taxonomy holds.
- Multi-tenant per-plugin key separation.
- Codec versioning / migration logic in the validator.
- Plugin domain tables (scenes, participants, ops_events) participating
  in crypto. Manifest `audit:` subjects only per master spec §8.2.
- Phase 8 site documentation. Tracked separately under `holomush-hd8h`.
  Phase 7 carries only PR-blocking inline docs:
  `site/docs/extending/binary-plugins.md` SDK pointer +
  `site/docs/reference/audit-subjects.md` registration of the new
  `plugin_integrity_violation` subject.
- Vault provider integration (`holomush-aub5` P4, not a dependency).

### Relationship to substrate spec

This design **fulfills** the master spec
(`docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`)
§8.2 and §8.3 as written, not as I (v1's author) initially re-proposed.
v1's substrate revision is abandoned.

### Master spec polish list (lands in the same PR)

1. **§4.6 audit-subjects catalog** — register
   `audit.<game>.system.plugin_integrity_violation` (new subject
   emitted by the Phase 7 downgrade fence).
2. **§8.2 pseudo-code amendment** — the current §8.2 paragraph (master
   spec lines 2107-2118) contains pseudo-code that says
   "fall back to events_audit (host-owned ground truth) if present"
   and asserts "the host's events_audit is the byte-equal mirror of
   what was actually published; if the plugin's response disagrees
   with the host's mirror, the host's mirror wins." Both statements
   are **structurally false for plugin-owned subjects**:
   `internal/eventbus/audit/projection.go:130-149` ack-and-skips
   plugin-owned messages; `events_audit` has no row for them. Phase 7
   amends §8.2 to strike the events_audit-fallback pseudo-code line
   and the "host's mirror wins" sentence for plugin-owned subjects,
   replacing with the v3 two-layer fence text from §1 verbatim
   (manifest-set heuristic on `always`-declared types + AEAD
   AAD-binding as the cryptographic ground truth; explicit
   `may`-elevated coverage boundary).
3. **Section 2 INV-50 wording** — cross-reference INV-P7-7 (which
   re-scopes the original wording onto the qualified event-type form
   and pins the `always`-only coverage boundary).
4. **§8.3 `AuditRow` Go-struct example** — update field set to mirror
   the proto definition committed in Phase 7 (id / Subject / Type /
   Timestamp / Actor / Codec / Payload / DEKRef pointer / DEKVersion
   pointer / SchemaVer).
5. **§2 INV-39 scope clarification** — master INV-39 (line 320) says
   "Reads of historical events whose `dek_ref` no longer exists in
   `crypto_keys` MUST automatically fall back to the cold tier" with no
   subject-owner qualification. Under Phase 7, plugin-owned subjects
   have no separate cold tier (the plugin row IS the cold record);
   INV-P7-15 mandates `metadata_only=true` termination instead of
   fallback. The polish amendment scopes INV-39 to host-owned subjects
   explicitly and cross-references INV-P7-15 for the plugin-owned
   contract. Other in-tree references to INV-39 (master lines 459, 473,
   1005, 1018, 1110, 1606, 1646, 2338, 2388) are already discussing
   host-tier mechanics; no other in-tree edits needed.

No invariant overturns. `crypto-reviewer` still fires because the
substrate touch is significant (plugin RPC proto reshape, audit
projection helper extraction, dispatcher contract change), but the
audit scope is narrower than v1.

## Section 1 — Threat model

Inherited from master spec §1. Phase 7 adds one explicit adversary:

| Adversary | Capability | Phase 7 defence |
|---|---|---|
| **Malicious or buggy plugin** | After Phase 7's dispatcher widening, plugins receive ciphertext for sensitive events. A misbehaving plugin's `QueryHistory` could return rows with `codec=identity` + a forged cleartext `payload`, attempting to bypass encryption for events that should have been sensitive. | **Two-layer fence with documented coverage boundary.** (1) **QueryStreamHistory pre-decrypt fence**, owned by `PluginDowngradeFence`, runs BEFORE the decrypt path is invoked. It enforces two checks per returned row: **(1a) manifest-set heuristic (INV-P7-7)**: host maintains an always-sensitive event-type set computed from `crypto.emits: sensitivity: always` declarations across loaded manifests; if `frame.type ∈ always_sensitive_types AND frame.codec = "identity"`, REFUSE with `AUDIT_ROW_DOWNGRADE_DETECTED` + emit `audit.<game>.system.plugin_integrity_violation`. **(1a) is `always`-only by design and does NOT cover `may`-declared events that were runtime-elevated to encrypted (via INV-6/INV-7 `sensitivity_fence.go::EnforceSensitivity` promoting `may + claim=true → SensitivityAlways` at emit time).** A malicious plugin returning `codec=identity` + cleartext for a `may`-elevated event_id WILL bypass (1a) because the manifest-set is built from declared `always`, not the runtime-elevation state which the read-side fence has no record of (no host shadow). **(1b) DEK existence check (INV-P7-15)**: if `frame.codec != "identity"` AND `frame.dek_ref` is absent OR not present in `crypto_keys` (applying the production `destroyed_at IS NULL` filter), REFUSE with `metadata_only=true` per master INV-26. (1b) does NOT emit `plugin_integrity_violation` — the failure mode is indistinguishable from a legitimate `Rekey`-destroyed-DEK case. (2) **AEAD AAD-binding at decrypt.** per master INV-25, any tampering of the AAD-bound projection fields (subject, type, actor, timestamp, event_id, codec, dek_ref, dek_version) causes AEAD tag-mismatch at decrypt and refuses to yield plaintext. **Layer (2) IS the catch for `may`-elevated downgrade**: the host's decrypt path reconstructs AAD via the §5.4 `AuditRow → *eventbusv1.Event` adapter and calls `aad.Build` (`internal/eventbus/crypto/aad/aad.go:62`); the bound `codec` value in AAD differs from what was used at encrypt; tag-check fails; the host surfaces a decrypt-side error and refuses plaintext. **Coverage trade explicitly documented**: layer (2) preserves the cryptographic guarantee (plaintext never leaks under any tamper), but for `may`-elevated downgrade specifically, the operator-visible signal is a generic decrypt error rather than the dedicated `AUDIT_ROW_DOWNGRADE_DETECTED` + `plugin_integrity_violation` audit. This is the cost of the no-host-shadow architecture. A future enhancement bead (e.g., per-event `was_encrypted` shadow flag) MAY close this UX gap; deliberately out of scope for Phase 7. |

The plugin role's Postgres credentials cannot tamper with
`events_audit`, `crypto_keys`, or the host-owned audit chain:
`schema_provisioner.go:163` REVOKEs all on schema `public` from the
plugin role, so the table names don't even resolve under the plugin's
connection.

**Operator-with-shell access (inherited threat).** After Phase 7,
plugin's `scene_log` (and analogous plugin-owned audit tables) hold
ciphertext + dek_ref + dek_version for sensitive events. An operator
with direct database shell access can pair a plugin row's ciphertext

- dek_ref/dek_version with a host `crypto_keys` read to attempt offline
plaintext recovery. This is the same offline-decryption surface as
operator-with-shell on `events_audit` (host-owned subjects) — Phase 7
does NOT introduce a new leak shape, it extends the existing one to
plugin-owned subjects. The `AdminReadStream` audit-chain + dual-control
controls (Phase 5, master spec §5.9 / §7.5) bound *server-mediated*
operator reads; they do not (and never claimed to) bound direct DB
access for either tier. Operational mitigation (DB read-role
separation, audit log on `crypto_keys` access) is master spec §9.2
operator-runbook territory, not Phase 7's scope.

## Section 2 — Invariants & testable proofs

Numbered RFC2119 invariants. Phase 7's invariants extend the master
spec's corpus rather than replacing entries; each maps to a named test
in the tree, and a meta-test asserts existence-by-name.

### What MUST be true

| ID | Invariant | Verification |
|---|---|---|
| **INV-P7-1** | The per-plugin audit dispatcher (`internal/eventbus/audit/plugin_consumer.go`) MUST forward ciphertext bytes byte-equal to what arrived on JetStream when the message's `App-Codec` header is non-identity. No decode-to-plaintext step occurs before forwarding. (Replaces the existing `AUDIT_PLUGIN_CODEC_UNSUPPORTED` rejection at `plugin_consumer.go:343`.) | Unit: `plugin_consumer_test.go::TestDispatchForwardsCiphertextByteEqual` drives the dispatcher with an xchacha20poly1305-v1 message; asserts the plugin RPC receives `AuditRow.payload == msg.Data()`. |
| **INV-P7-2** | The dispatcher MUST populate `AuditRow.codec`, `AuditRow.dek_ref`, `AuditRow.dek_version` from the JS message's `App-Codec` / `App-Dek-Ref` / `App-Dek-Version` headers using a **shared parser** (see Section 5.5). The same parser is used by the host audit projection's `events_audit` writer; byte-equality across the two branches is structural. | Unit: `header_parser_test.go::TestParserBindsCodecDekRefDekVersionTypes`. Integration: `dispatcher_projection_parity_test.go::TestPluginAndHostBranchesParseHeadersIdentically`. |
| **INV-P7-3** | Plugin audit tables MUST add `dek_ref BIGINT NULL` and `dek_version INTEGER NULL` columns (per master spec §8.2 mirror-events_audit contract). The columns are nullable; identity-codec rows store NULL on both. | Integration: `plugin_migration_test.go::TestSceneLogHasDekColumns`. |
| **INV-P7-4** | Plugin SDK Layer 2: `pluginsdk.AuditRow` Go struct fields MUST be 1:1 with `pluginauditpb.AuditRow` proto fields (id, subject, type, timestamp, actor, codec, payload, dek_ref, dek_version). | Compile-time + unit: `audit_test.go::TestAuditRowStructMirrorsProto`. |
| **INV-P7-5** | `pluginsdk.StoreFromMessage(msg)` round-tripped through `pluginsdk.LoadForQuery(row)` MUST yield byte-equal `payload`, identical projection fields, AND identical `codec` / `dek_ref` / `dek_version` typed values. | Unit: `audit_test.go::TestAuditRowRoundTripPreservesAllFields`. |
| **INV-P7-6** (extends master INV-46) | A plugin's stored audit row MUST byte-equal the row received via the `AuditEvent` RPC: `(payload, codec, dek_ref, dek_version)` are written verbatim and returned verbatim. Together with INV-P7-1 (dispatcher forwards bus-byte-equal), this gives the bus-to-plugin-stored-row byte-equality that master INV-46 asserts; INV-P7-6 holds the plugin side, INV-P7-1 holds the dispatcher side, master INV-46 is the composition. | Integration: `plugin_audit_round_trip_test.go::TestSceneLogPreservesCiphertextAndAuditHeaders`. |
| **INV-P7-7** (re-scopes master INV-50) | The host's `QueryStreamHistory` handler MUST refuse a plugin-returned row where `row.codec = "identity"` AND `row.type ∈ always_sensitive_types`. The set is keyed by the **qualified event-type form** `<plugin_name>:<event_type>` (matching `Event.type` as emitted by plugins per existing `event_emitter_crypto_test.go` fixtures). **Set-key construction**: for each loaded manifest's `crypto.emits` entry where `Sensitivity == Always`, the set key is built as `<plugin_name>:<emit.EventType>`. Manifests using already-qualified `EventType` (begins with `<plugin_name>:`) are accepted verbatim; otherwise the `<plugin_name>:` prefix is prepended. This normalization is necessary because in-tree manifests today store unqualified types (e.g. `"scene.whisper"` at `internal/plugin/crypto_manifest_test.go:58`) while runtime `Event.type` is qualified (e.g. `"test-plugin:secret"` at `internal/plugin/event_emitter_crypto_test.go:58`). The set is computed at initial-manifest-load time. Refusal emits `AUDIT_ROW_DOWNGRADE_DETECTED` AND `audit.<game>.system.plugin_integrity_violation` (single-event subject; no chain participation; see Section 7). **This invariant covers the named threat (`always`-declared events); the `may`-elevated downgrade class is caught by AEAD AAD-binding only — see §1 threat-model coverage boundary and §8 failure modes.** | Unit: `plugin_history_fence_test.go::TestRefusesIdentityForAlwaysSensitiveType` + `::TestQualifiedTypeFormKey`. Integration: `test_downgrade_attacker_e2e_test.go`. |
| **INV-P7-8** | Plugin manifests are **NOT hot-reloaded at runtime today**. The always-sensitive set used by INV-P7-7 is built once at server startup (during `internal/plugin/manager.go::Manager.Load`-equivalent path) and is immutable for the server's lifetime. A regression that introduces hot-reload without also adding atomicity guarantees on this set MUST be caught: the invariant test asserts the set's pointer is set exactly once during boot and never reassigned. Follow-up bead `holomush-kl9w` files the future hot-reload work, which MUST land with its own atomicity invariant. | Unit: `always_sensitive_set_test.go::TestSetBuiltOnceAtBoot`. |
| **INV-P7-9** | The dispatcher's KeySelector wiring (Section 5.2) MUST be the SAME KeySelector instance the host's hot-tier reader uses (`internal/eventbus/history/hot_jetstream.go`). No second selector, no parallel cache. | Integration: `dispatcher_selector_identity_test.go::TestDispatcherAndHotTierShareSelector`. |
| **INV-P7-10** | Plugin migrations MAY run before or after Phase 7's host migration (`event_crypto_refs` is NOT introduced — there is no host-side schema change beyond what Phases 2–5 already shipped). The two crypto columns added to plugin tables are nullable and don't require any new host-side support. | Integration: `plugin_migration_test.go::TestPhase7PluginMigrationStandalone`. |
| **INV-P7-15** (carries master INV-48 onto plugin-routed path) | The host's `QueryStreamHistory` plugin path MUST refuse any plugin-returned row where `row.codec != "identity"` AND `row.dek_ref` is absent OR `row.dek_ref` is not present in `crypto_keys` (the host's lookup applies the production `destroyed_at IS NULL` filter; a row not satisfying that filter is treated as absent for the purposes of this check). Refusal surfaces to the client as `metadata_only=true` per master INV-26 — NOT as a `plugin_integrity_violation`, since the failure mode is "host can't find the DEK," which is indistinguishable from a legitimate `Rekey`-destroyed-DEK case. There is **no fallback for plugin-owned subjects** (the plugin row IS the cold record; no host shadow exists); the read terminates with metadata-only, no loop. TOCTOU window between this validator check and the decrypt-path resolver is accepted: both surface `metadata_only=true` per master INV-26, so the operator-visible outcome is identical regardless of which call observed the race. Master INV-39 (cold-tier fallback) is scoped to host-owned subjects only — see master spec polish item 5. | Integration: `plugin_history_fence_test.go::TestRefusesUnknownDekRef` + `::TestNoColdFallbackForPluginOwnedSubjects`. |
| **INV-P7-16** (new in v4) | The `AuditRow → *eventbusv1.Event` adapter (§5.4) MUST produce a value whose AAD reconstruction is byte-equal to the AAD used at encrypt for the same `event_id`. Integration test encrypts a sensitive plugin-owned event, captures the encrypt-side AAD bytes from `aad.Build(envProto, codecName, dekRef, dekVer)` (`internal/eventbus/crypto/aad/aad.go:62`), stores+returns the row through the plugin path, reconstructs the `*eventbusv1.Event` via the §5.4 adapter, recomputes AAD, and asserts byte-equal. Failure mode is critical: an AAD reconstruction bug would manifest as EVERY sensitive plugin-stored event failing AEAD tag-check on decrypt and being indistinguishable from a deliberate downgrade attack. | Integration: `plugin_aad_reconstruction_test.go::TestRoundTripProducesByteEqualAAD`. |

### What MUST NOT be true

| ID | Invariant | Verification |
|---|---|---|
| **INV-P7-11** | The dispatcher MUST NOT decrypt to plaintext before forwarding to the plugin. The plugin receives ciphertext; the host (and only the host) decrypts. (This preserves master spec §8.2's pass-through guarantee — the plugin participates in storage, not in decryption.) | Unit: `plugin_consumer_test.go::TestDispatchDoesNotDecryptBeforeForward` asserts `AuditRow.payload` is byte-equal to `msg.Data()` even for xchacha20poly1305-v1 codec. |
| **INV-P7-12** | The plugin's stored row MUST NOT carry any cleartext content for sensitive events. The plugin sees `codec=xchacha20poly1305-v1` + ciphertext bytes; even an inspect-the-row plugin (or a leaked DB dump) reveals nothing. | Integration: `sensitive_event_storage_test.go::TestPluginRowIsCiphertextForSensitiveEvents`. |
| **INV-P7-13** | Plugin code MUST NOT have a path that writes directly to host-owned tables (`events_audit`, `crypto_keys`, etc.). Plugin's Postgres role lacks USAGE on schema `public` (per `internal/plugin/schema_provisioner.go:163`); any INSERT fails with `permission denied for schema public`. | Integration: `plugin_role_permissions_test.go::TestPluginRoleCannotWriteHostTables`. |
| **INV-P7-14** | Phase 7 MUST NOT add a second emit-time sensitivity gate. The `crypto.emits` manifest declaration enforced by `internal/plugin/sensitivity_fence.go` (INV-6 / INV-7) is the sole emit-time gate. | Static + meta-test: `phase7_boundary_meta_test.go::TestNoNewEmitTimeSensitivityGates` grep-asserts no new sensitivity check is added outside `sensitivity_fence.go`. |

### Meta-test

A single test file
`internal/eventbus/history/phase7_boundary_meta_test.go` walks every
`INV-P7-N` in this section's tables (INV-P7-1 through INV-P7-16,
excluding INV-P7-2 and INV-P7-14 which themselves contain meta
assertions) and asserts the named test exists in the tree
(`path:function` lookup). Drift between this table and the test
corpus fails CI.

## Section 3 — Architecture

### 3.1 Trust-boundary picture

```text
Plugin-emitted sensitive event flow (Phase 7)
─────────────────────────────────────────────
  Plugin code  ─emit→  HostPluginService.Emit  ─[INV-6/INV-7 fence]→  encrypt(host KEK/DEK)  →  JetStream
                                                                                                       │
                                                                                                       ▼
                                                                              headers: App-Codec, App-Dek-Ref, App-Dek-Version
                                                                                       (host-set)

Audit projection (host — UNCHANGED in Phase 7)
──────────────────────────────────────────────
  JetStream  ─main audit consumer→  HostAuditProjection
                                          │
                                          ├── owner = host    →  INSERT events_audit (full row, parses crypto headers)  [unchanged]
                                          └── owner = plugin  →  ack-and-skip                                            [unchanged]

Audit projection (plugin — CHANGED in Phase 7)
──────────────────────────────────────────────
  JetStream  ─per-plugin consumer→  PluginConsumer (one per plugin, FilterSubjects on plugin's audit subjects)
                                          │
                                          ▼
                            [PHASE 7] Parse codec/dek headers via shared parser (Section 5.5)
                            [PHASE 7] Remove identity-codec rejection at plugin_consumer.go:343
                                          │
                                          ▼
                            Build pluginauditpb.AuditRow{
                                payload  = msg.Data(),           // CIPHERTEXT byte-equal — INV-P7-1/11
                                codec    = parsed codec,
                                dek_ref  = parsed dek_ref,       // NULL on identity
                                dek_version = parsed dek_version,// NULL on identity
                                + projection fields (id, subject, type, timestamp, actor)
                            }
                                          │
                                          ▼
                              plugin.PluginAuditService.AuditEvent(AuditEventRequest{row})
                                          │
                                          ▼
                            Plugin stores row in scene_log (or analogous plugin-owned table)
                            with new dek_ref/dek_version columns (INV-P7-3)

Plugin-owned history read flow
──────────────────────────────
  Client  ─QueryStreamHistory→  CoreServer  →  history.Reader  ─owner=plugin→  PluginHistoryRouter
                                                                                       │
                                                                                       ▼
                                                                           plugin.QueryHistory (gRPC)
                                                                                       │
                                                                                       ▼
                                                                           Plugin reads its scene_log; returns
                                                                           QueryHistoryResponse{ AuditRow row } stream
                                                                                       │
                                                                                       ▼
                                                                  [PHASE 7] PluginDowngradeFence wraps the stream:
                                                                    For each AuditRow:
                                                                      if row.codec == "identity" AND
                                                                         row.type ∈ always_sensitive_types:
                                                                          REFUSE → AUDIT_ROW_DOWNGRADE_DETECTED
                                                                          emit audit.<game>.system.plugin_integrity_violation
                                                                          [INV-P7-7]
                                                                      else:
                                                                          pass through
                                                                                       │
                                                                                       ▼
                                                                  AuthGuard + DEK decrypt + delivery (existing path,
                                                                  uses AAD with subject/type/actor/timestamp/codec/dek_ref;
                                                                  AAD-tag-mismatch refuses with INV-25 — the second
                                                                  fence layer)
```

### 3.2 Components changed by Phase 7

| Component | Status | Responsibility |
|---|---|---|
| `pkg/plugin/audit.go` | EXTENDED | Add `AuditRow` + `StoreFromMessage` + `LoadForQuery` Layer-2 helpers below the existing Layer-1 ABAC decision-hint recorder. `AuditRow` mirrors `pluginauditpb.AuditRow` 1:1. Top-of-file doc enumerates Layer 1 and Layer 2 surfaces. |
| `api/proto/holomush/plugin/v1/audit.proto` | RESHAPED | New `AuditRow` message (id, subject, type, timestamp, actor, codec, payload, dek_ref optional, dek_version optional, schema_ver). `AuditEventRequest`: `event` + `headers` fields BOTH replaced with `AuditRow row = 1`. `QueryHistoryResponse`: `event` replaced with `AuditRow row = 1`. Clean break (no `reserved` markers, no compat shims — per `[feedback_no_prod_shape_for_undeployed]`). Existing `headers` map callers MUST be migrated (enumerated below). Proto regen via `task proto`. **Affected callers** (verified by design-reviewer): `plugins/core-scenes/audit.go:160-237` (reads `auditHeaderCodec` / `auditHeaderSchemaVer` / `auditHeaderEventType` / `auditHeaderActorKind` / `auditHeaderActorID` from `req.GetHeaders()`) — rewrites to read from `req.GetRow().GetCodec()` etc.; `test/integration/eventbus_e2e/plugin_audit_isolation_test.go:175-220` (test stub mirroring core-scenes shape) — rewrites parallel to the real handler; `internal/eventbus/audit/plugin_consumer_unit_test.go:82` (`assert.NotEmpty(t, cli.gotReq.GetHeaders())`) — replaced with `assert.NotNil(t, cli.gotReq.GetRow())` + per-field assertions. |
| `internal/eventbus/audit/plugin_consumer.go` | MODIFIED | Remove `AUDIT_PLUGIN_CODEC_UNSUPPORTED` rejection at line 343. Wire KeySelector through `PluginConsumerManager` (same instance as `hot_jetstream` reader). Build `AuditRow` from raw msg bytes + parsed headers via shared parser (Section 5.5). Forward ciphertext byte-equal. |
| `internal/eventbus/audit/header_parser.go` | NEW | Extract ALL audit-header parsing (codec / `App-Dek-Ref` / `App-Dek-Version` / `App-Schema-Version`) from `projection.go:172-262` into a shared `ParseAuditHeaders` helper returning `HeaderAuditMetadata`. Both the projection (`events_audit` writer) AND `plugin_consumer.go` use it; byte-equality between the two branches is structural (INV-P7-2). `schema_ver` is co-located here despite not being a crypto field — single source of truth for header→typed-value conversion prevents the host-branch and plugin-branch from drifting on parse rules. **Error-code unification**: parser returns errors with codes `AUDIT_DEK_REF_PARSE_FAILED` / `AUDIT_DEK_VERSION_PARSE_FAILED` / `AUDIT_BAD_SCHEMA_VERSION` (matching `projection.go`'s current codes); both call sites surface them as-is without re-wrapping under `AUDIT_PLUGIN_*` prefixes. |
| `internal/eventbus/audit/projection.go` | TRIMMED | Use the shared parser. Behaviour unchanged. |
| `internal/eventbus/history/plugin_downgrade_fence.go` | NEW | Lives in `internal/eventbus/history/`. Implements AND wraps the `history.PluginHistoryRouter` interface (defined at `internal/eventbus/history/tier.go:79-84`). Composition at wiring time: `NewPluginDowngradeFence(audit.NewPluginHistoryRouter(...))` — the fence has zero compile-time coupling to `audit.PluginHistoryRouter`'s concrete type. No new import edges (the package already imports `eventbus/audit` via `tier.go:39`; the fence doesn't need that import at all). Owns two responsibilities: (a) **QueryStreamHistory pre-decrypt fence layer (1)** — for each returned `AuditRow`, refuse on the manifest-set check (INV-P7-7) OR the DEK existence check (INV-P7-15); (b) emits `audit.<game>.system.plugin_integrity_violation` on INV-P7-7 refusals (NOT on INV-P7-15 refusals — see §1 threat model). Owns the always-sensitive set (built once at boot per INV-P7-8). |
| `internal/eventbus/history/tier.go` | EXTENDED | `Reader.QueryHistory` plugin branch routes through `PluginDowngradeFence` before returning the stream. Single-line change at `tier.go:367`. |
| `plugins/core-scenes/migrations/0000XX_add_scene_log_dek_columns.{up,down}.sql` | NEW | `ALTER TABLE scene_log ADD COLUMN dek_ref BIGINT NULL, ADD COLUMN dek_version INTEGER NULL`. Down migration drops them. Additive; existing rows correctly NULL. |
| `plugins/core-scenes/audit.go` | EXTENDED | `SceneAuditStore.Insert` accepts dek_ref + dek_version params; `queryLog` returns them; `AuditEvent` RPC populates them from `AuditRow.dek_ref` / `.dek_version`; `QueryHistory` returns them on `AuditRow`. |
| `test/integration/plugin/testdata/test_downgrade_attacker/` | NEW | Test fixture: plugin manifest declares `crypto.emits: [{event_type: secret, sensitivity: always}]`. Happy path emits one sensitive event (host encrypts, dispatcher forwards ciphertext, plugin stores byte-equal). Malicious `QueryHistory` returns the same `event_id` with `codec = "identity"` + cleartext `payload`. Lives under `test/integration/plugin/testdata/` (not under `plugins/`) so `task plugin:build-all` does NOT see it and production builds are unaffected. Loaded by the e2e test harness directly. |
| `internal/eventbus/history/plugin_downgrade_fence_test.go` | NEW | Unit: truth-table tests against router fake. INV-P7-7, INV-P7-8, INV-P7-15 coverage. |
| `internal/eventbus/history/plugin_aad_reconstruction_test.go` | NEW | Integration: encrypts a plugin-owned sensitive event, captures encrypt-side AAD bytes, stores+returns the row through the plugin path, reconstructs `*eventbusv1.Event` from the returned `AuditRow` via the §5.4 adapter, calls `aad.Build` (`internal/eventbus/crypto/aad/aad.go:62`) and asserts byte-equal AAD. INV-P7-16 coverage. |
| `internal/eventbus/audit/header_parser_test.go` | NEW | Unit: typed parser coverage. INV-P7-2. |
| `internal/eventbus/audit/plugin_consumer_test.go` | EXTENDED | INV-P7-1, INV-P7-11. |
| `pkg/plugin/audit_test.go` | EXTENDED | INV-P7-4, INV-P7-5. |
| Integration tests (Section 6) | NEW | INV-P7-3, INV-P7-6, INV-P7-9, INV-P7-10, INV-P7-12, INV-P7-13. |
| Master spec polish | UPDATED | §4.6 audit-subjects table adds `plugin_integrity_violation`. Section 2 INV-50 wording cross-references INV-P7-7. §8.3 Go-struct example field-name capitalization. |
| `site/docs/extending/binary-plugins.md` | EXTENDED | Document the Layer 2 SDK helpers and the post-Phase-7 contract that plugin audit tables mirror `events_audit` shape including crypto columns. PR-blocking inline doc. |
| `site/docs/reference/audit-subjects.md` | EXTENDED | Register `audit.<game>.system.plugin_integrity_violation` subject (one-shot violation, no chain participation). |

### 3.3 Boundaries (what each component MUST / MUST NOT know)

- **Plugin code** MUST NOT decrypt. It receives `AuditRow` with
  ciphertext `payload` + codec/dek_ref/dek_version metadata, stores it
  verbatim, returns it verbatim on `QueryHistory`. Plugin is a
  pass-through store.
- **`plugin_consumer.go` dispatcher** MUST NOT decrypt before forwarding
  (INV-P7-11). The dispatcher forwards ciphertext byte-equal without
  using the KeySelector for the forward path itself. KeySelector wiring
  in Phase 7 is for substrate *symmetry* with the hot-tier reader: if a
  future phase adds a dispatcher-side validation (e.g., "refuse if the
  message's `dek_ref` doesn't resolve to a real `crypto_keys` row"), the
  substrate is ready and we don't have to revisit the wiring point.
  INV-P7-9 pins the symmetric-wiring invariant.
- **`PluginDowngradeFence`** is the ONLY READ-side enforcer of the
  downgrade attack. Master spec §8.2's "host's events_audit is the
  byte-equal mirror" applies only to host-owned subjects; for
  plugin-owned subjects, the plugin's own table IS the byte-equal
  mirror (INV-P7-6 enforces this; the host has no shadow copy).
- **Plugin SDK Layer 2** is convenience for plugin authors. Host has
  the AEAD AAD-binding as the cryptographic ground truth; SDK
  correctness is verified by `audit_test.go`, not relied on by the
  host's threat model.

### 3.4 Why this shape

- **Follows the master spec's deliberate design.** v1 of this design
  tried to "fix" §8.2 with a host-owned side table; that was
  overturning a prior architectural commitment without recognizing it
  was deliberate. v2 fulfills §8.2 as written.
- **The plugin's table IS the cold-tier mirror.** No host shadow copy
  exists for plugin-owned subjects. The plugin owns its cold tier
  end-to-end, with the host's per-plugin role isolation +
  Postgres-schema REVOKE providing the integrity boundary, and the
  host's `PluginDowngradeFence` + AAD-binding providing the read-side
  validation.
- **Two-layer fence with explicit boundary.** Layer (1) is the
  **QueryStreamHistory pre-decrypt fence**: cheap host-owned checks
  that refuse the row before any decrypt work runs. (1a) is the
  manifest-set heuristic catching the named `always`-declared
  downgrade attack with a dedicated error code + `plugin_integrity_violation`
  audit. (1b) is the DEK existence check (INV-P7-15) catching missing
  or destroyed `dek_ref` references with `metadata_only=true` per
  master INV-26. Layer (2) is **AEAD AAD-binding at decrypt** (master
  INV-25): the cryptographic ground truth that catches every other
  tamper shape (subject/type/actor/timestamp/codec/dek_ref/dek_version)
  with tag-mismatch refusal. The framing has the explicit `may`-elevated
  coverage boundary per §1 threat model: layer (1a) is `always`-only
  by design; `may`-elevated downgrade is caught by layer (2) at the
  cost of a less specific operator UX signal.
- **Shared header parser closes the byte-divergence risk
  (crypto-reviewer B3).** The two branches (host projection,
  plugin_consumer) that interpret the same JS-header crypto metadata
  use the same code path. Future header changes propagate to both
  branches by construction.
- **Clean proto reshape.** Per `[feedback_no_prod_shape_for_undeployed]`,
  no `reserved` markers, no compat shims, no deprecation period. The
  `AuditRow` proto message becomes the canonical shape for both
  ingest and query.

## Section 4 — Data model

### 4.1 Plugin schema migration

```sql
-- plugins/core-scenes/migrations/0000XX_add_scene_log_dek_columns.up.sql
ALTER TABLE scene_log
    ADD COLUMN IF NOT EXISTS dek_ref     BIGINT,
    ADD COLUMN IF NOT EXISTS dek_version INTEGER;

CREATE INDEX IF NOT EXISTS scene_log_dek_ref
    ON scene_log (dek_ref)
    WHERE dek_ref IS NOT NULL;
```

No CHECK constraint between codec column and dek columns — `scene_log`
already carries the `codec` column (existing schema); the agreement
between codec and dek columns is enforced at the **host's projection
emit** side (the JS publisher sets the headers consistently), not at the
plugin table. The plugin stores what it receives; if it ever stored an
inconsistent shape, INV-P7-6 round-trip and AAD validation surface the
break.

No host-side schema change in Phase 7. `events_audit` already has
dek_ref/dek_version columns (migration 000014). `crypto_keys` unchanged.

### 4.2 `pluginauditpb.AuditRow` proto

```proto
// api/proto/holomush/plugin/v1/audit.proto

import "holomush/eventbus/v1/actor.proto";
import "google/protobuf/timestamp.proto";

// AuditRow is the canonical wire shape for plugin-owned audit rows. Used
// in both directions: dispatcher → plugin (AuditEventRequest) and
// plugin → host (QueryHistoryResponse). Mirrors the events_audit row
// shape so the proto wire format and the storage shape are coupled.
message AuditRow {
    // Cleartext projection fields
    bytes id = 1;                                   // 16-byte ULID
    string subject = 2;
    string type = 3;
    google.protobuf.Timestamp timestamp = 4;
    holomush.eventbus.v1.Actor actor = 5;

    // Crypto envelope
    string codec = 6;                               // "identity" | "xchacha20poly1305-v1"
    bytes payload = 7;                              // ciphertext when codec != "identity"

    // DEK reference — absent on identity codec, required on others.
    // Host enforces the agreement (codec=identity ⇔ both absent).
    optional uint64 dek_ref = 8;
    optional uint32 dek_version = 9;

    // Audit schema version. Today's wire carries this via
    // App-Schema-Version header; v3 moves it onto the row so the proto
    // is the complete contract and the existing scene_log.schema_ver
    // NOT NULL column has a typed source.
    int32 schema_ver = 10;
}

message AuditEventRequest {
    AuditRow row = 1;
}

message QueryHistoryResponse {
    AuditRow row = 1;
}
```

The `eventbusv1.Event` reuse in today's proto is dropped from
`AuditEventRequest` and `QueryHistoryResponse`; both messages now
exclusively use `AuditRow`. Other RPCs continue to use `eventbusv1.Event`
where appropriate.

### 4.3 Plugin SDK Layer 2 — `pkg/plugin/audit.go`

```go
// AuditRow is the Go-side mirror of pluginauditpb.AuditRow. Fields and
// names match the proto-generated Go 1:1 for INV-P7-4.
type AuditRow struct {
    EventID    ulid.ULID
    Subject    string
    Type       string
    Timestamp  time.Time
    Actor      *eventbuspb.Actor

    Codec      string
    Payload    []byte                // ciphertext when Codec != "identity"

    // Pointers map to the proto's optional fields. nil ⇔ NULL ⇔
    // identity codec.
    DEKRef     *uint64
    DEKVersion *uint32

    // SchemaVer mirrors AuditRow.schema_ver from the proto. Plain int32
    // because the proto field is non-optional (every row has one).
    SchemaVer  int32
}

// StoreFromMessage extracts an AuditRow from a JetStream message,
// preserving payload bytes byte-equal and using the shared header
// parser (header_parser.go) so the typed values match what the host
// audit projection records for host-owned subjects.
func StoreFromMessage(msg jetstream.Msg) (AuditRow, error)

// LoadForQuery converts a stored AuditRow into the proto frame
// returned by PluginAuditService.QueryHistory. Round-trip stable with
// StoreFromMessage. Verified by INV-P7-5.
func LoadForQuery(row AuditRow) (*pluginauditpb.AuditRow, error)
```

### 4.4 Always-sensitive type set

`PluginDowngradeFence` holds an immutable-after-boot set keyed by
qualified event type (`<plugin_name>:<event_type>`), built once at
server startup from loaded manifests' `crypto.emits` declarations
per INV-P7-8. The set is NOT updated at runtime — manifests are not
hot-reloaded today, and the fence's set follows that constraint.

The set is fed from `internal/plugin/Manifest.crypto.emits` entries
where `Sensitivity == SensitivityAlways`. Future hot-reload support
(post-Phase-7) lands via bead `holomush-kl9w`, which will add a
manifest-registry reload callback and must extend INV-P7-8 to mandate
atomic refresh at that time.

## Section 5 — Implementation notes

### 5.1 Plugin dispatcher widening

`internal/eventbus/audit/plugin_consumer.go::decodeEnvelope`'s current
behaviour: parses `App-Codec` header; if non-identity, rejects with
`AUDIT_PLUGIN_CODEC_UNSUPPORTED`. Phase 7 removes the rejection. New
behaviour:

- Parse `App-Codec`, `App-Dek-Ref`, `App-Dek-Version` via the shared
  header parser (Section 5.5).
- Build `pluginauditpb.AuditRow` directly from `msg.Data()` (ciphertext
  payload byte-equal) + the parsed headers + the projection fields
  read from the proto envelope's cleartext metadata. For sensitive
  events, `payload` is ciphertext; for identity events, it's the
  plaintext-encoded envelope bytes (unchanged from today).
- Forward `AuditEventRequest{row: AuditRow}` to the plugin RPC.

The function signature and error-code surface change to reflect
ciphertext forwarding; tests at
`plugin_consumer_unit_test.go::TestPluginConsumerDispatchRejectsNonIdentityCodec`
are deleted (the test name itself encodes the deprecated behaviour).

### 5.2 KeySelector wiring

`PluginConsumerManager` gains a `keySelector codec.KeySelector` field.
At wiring time (`cmd/holomush/deps.go`), the same selector instance
that's passed to `history.NewReader` via `WithCodecSelector` is also
passed to `PluginConsumerManager`. INV-P7-9 verifies the identity.

In Phase 7, the dispatcher doesn't *use* the selector for the forward
path. It's wired for substrate symmetry; future work (e.g.,
dispatcher-side DEK existence check) lands without re-wiring.

### 5.3 PluginDowngradeFence

```go
// internal/eventbus/history/plugin_downgrade_fence.go

type PluginDowngradeFence struct {
    inner             PluginHistoryRouter
    sensitiveTypes    atomic.Pointer[map[string]struct{}]
    integrityEmitter  AuditEmitter   // for plugin_integrity_violation
}

func (f *PluginDowngradeFence) QueryHistory(ctx context.Context,
    pluginName string, q eventbus.HistoryQuery) (eventbus.HistoryStream, error) {
    inner, err := f.inner.QueryHistory(ctx, pluginName, q)
    if err != nil { return nil, err }
    return &fencedStream{inner: inner, fence: f}, nil
}

// fencedStream wraps the inner stream; Next() applies the
// manifest-set check per row.
```

Per-row check is cheap: one set lookup keyed by `row.type`. Fence emits
`plugin_integrity_violation` audit synchronously when refusing
(blocking the client's read until the audit emit completes — operator
visibility is the point).

### 5.4 `AuditRow → *eventbusv1.Event` AAD-reconstruction adapter

The host's AAD construction (`internal/eventbus/crypto/aad/aad.go:62`)
takes a `*eventbusv1.Event` plus codec name, dek_ref, dek_version:

```go
func Build(event *eventbusv1.Event, codecName string,
    dekRef uint64, dekVersion uint32) ([]byte, error)
```

Existing call sites at `internal/eventbus/history/dispatcher.go:179`
and `:322` pass the host's envelope proto directly. After Phase 7, the
plugin-routed read path returns `*pluginauditpb.AuditRow`; AAD
reconstruction MUST convert `AuditRow → *eventbusv1.Event` byte-exactly
before calling `aad.Build`.

The adapter lives in `internal/eventbus/history/plugin_aad_adapter.go`
(NEW). Per-field copy contract:

| `pluginauditpb.AuditRow` field | `*eventbusv1.Event` target | Notes |
|---|---|---|
| `id` (bytes, 16) | `Event.Id` (bytes) | Verbatim copy; INV-P7-16 asserts byte-equality. |
| `subject` (string) | `Event.Subject` (string) | Verbatim. |
| `type` (string) | `Event.Type` (string) | Qualified `<plugin>:<event>` form preserved verbatim per INV-P7-7. |
| `timestamp` (`google.protobuf.Timestamp`) | `Event.Timestamp` | Nanosecond-preserved (per `aad.go:74` `UnixNano()` extraction). |
| `actor` (`eventbusv1.Actor`) | `Event.Actor` | Verbatim; `aad.Build` deterministically-marshals this submessage internally. |
| `codec`, `dek_ref`, `dek_version` | passed to `aad.Build` as scalar args | NOT in the `*Event` (they're separate args to `Build`). |
| `payload` | NOT copied | Ciphertext payload is the AEAD input, not the AAD. |
| `schema_ver` | NOT copied | AAD canonical inputs per master §4.2 do NOT include schema_ver (verified at `aad.go:106-114`); the adapter ignores this field for AAD construction. |
| `rendering` (`Event.rendering` field 7, verified at `api/proto/holomush/eventbus/v1/eventbus.proto:35-46`) | NOT copied | Not in AAD canonical inputs per master §4.2 (verified at `aad.go:62-117`). Adapter ignores this field for AAD construction; if a future AAD revision starts binding `rendering`, this row + INV-P7-16 test corpus MUST extend to cover it. |

The adapter is a 6-field copy with two `nil`-safety guards (Actor /
Timestamp may be nil for some event shapes; `Build` already tolerates
nil Actor via `event.GetActor()` and zero Timestamp via the unconditional
`UnixNano()` path).

### 5.5 Shared header parser

```go
// internal/eventbus/audit/header_parser.go

// HeaderAuditMetadata is the typed projection of a JetStream
// message's audit-related headers. Both the host audit projection
// (events_audit writer) and the per-plugin dispatcher use this parser;
// byte-equality of typed values across the two branches is structural
// (INV-P7-2). schema_ver is included here despite not being a crypto
// field — co-locating ALL audit-header parsing in one helper prevents
// the host-branch and plugin-branch from drifting on the parse rules
// (default value, error code, header name spelling).
type HeaderAuditMetadata struct {
    Codec      string
    SchemaVer  int32      // SMALLINT-bounded; INVALID if outside [0, 32767]
    DEKRef     *int64     // matches pgx nullable int8 ⇔ proto uint64 conversion handled by caller
    DEKVersion *int32     // matches pgx nullable int4 ⇔ proto uint32 conversion handled by caller
}

func ParseAuditHeaders(h nats.Header) (HeaderAuditMetadata, error)
```

The parser is the single source of truth for header → typed-value
conversion. `projection.go` and `plugin_consumer.go` both call it.

## Section 6 — Test strategy

| Layer | Tests | Build tag |
|---|---|---|
| **Unit** | `plugin_consumer_test.go` — INV-P7-1, INV-P7-11 (dispatcher forwards ciphertext byte-equal; no decrypt). `header_parser_test.go` — INV-P7-2 (typed value coverage, error-code surface). `plugin_downgrade_fence_test.go` — INV-P7-7, INV-P7-8, INV-P7-15 (truth table, set-built-once-at-boot, dek_ref-not-in-crypto-keys refusal). `audit_test.go` — INV-P7-4, INV-P7-5 (struct↔proto mirroring including `SchemaVer`, round-trip). Meta-test — INV-P7-14. | none — `task test` |
| **Integration** | `plugin_migration_test.go` — INV-P7-3, INV-P7-10 (column add, standalone runnability). `plugin_audit_round_trip_test.go` — INV-P7-6 (byte-equal store+return for sensitive event). `sensitive_event_storage_test.go` — INV-P7-12 (plugin row is ciphertext). `plugin_role_permissions_test.go` — INV-P7-13. `dispatcher_projection_parity_test.go` — INV-P7-2 cross-branch. `dispatcher_selector_identity_test.go` — INV-P7-9. | `//go:build integration` |
| **End-to-end** | `test_downgrade_attacker_e2e_test.go` — uses `test/integration/plugin/testdata/test_downgrade_attacker/` fixture. Happy path: plugin emits one `sensitivity: always` event; host encrypts; dispatcher forwards ciphertext; plugin stores ciphertext byte-equal; plugin's `QueryHistory` returns ciphertext; host decrypts and delivers. Attack path: same plugin's malicious `QueryHistory` returns the same event with `codec=identity` + cleartext payload; `PluginDowngradeFence` refuses with `AUDIT_ROW_DOWNGRADE_DETECTED`; `plugin_integrity_violation` audit event is emitted. | `//go:build integration` |
| **Lint / static** | Existing `gorules/dek_no_serialize.go` continues; no new ruleguard rules. | `task lint` |
| **Coverage gate** | Per project rule: >80% per package. `internal/eventbus/audit/`, `internal/eventbus/history/`, `pkg/plugin/` MUST stay above. | `task test:cover` |

## Section 7 — `plugin_integrity_violation` audit subject

New audit subject introduced by Phase 7. Updates master spec §4.6.

- **Subject:** `audit.<game>.system.plugin_integrity_violation`
- **Emit trigger:** `PluginDowngradeFence` refuses a plugin-returned
  row per INV-P7-7.
- **Payload fields (cleartext):** `plugin_name`, `event_id`,
  `event_type`, `claimed_codec`, `expected_sensitivity`, `refusal_code`
  (`AUDIT_ROW_DOWNGRADE_DETECTED`).
- **Operator correlation gap (documented):** For plugin-owned subjects
  the host's `events_audit` has no mirror row (the audit projection
  ack-and-skips plugin-owned subjects per `projection.go:130-149`).
  Operator forensic investigation of `plugin_integrity_violation` MUST
  correlate via the JetStream message store (assuming the original
  message is still within retention) or via the plugin's own audit
  table (which the malicious plugin owns — limited trust). This is
  the cost of the no-host-shadow architecture; documented in
  `site/docs/operating/crypto-runbook.md` (Phase 8).
- **Sensitivity:** `never`. The violation report itself is operator
  diagnostic data; it carries no game-content.
- **Chain participation:** NONE. This is a one-shot violation report,
  not a hash-linked state machine. (Master spec §4.6.X registered
  chains: `policy_set`, `rekey`; Phase 7 does NOT add a chain.)
- **ABAC:** Inherits the existing `audit.*.system.*` ABAC deny rule
  (master spec §4.6); plugin / character subjects cannot subscribe.
  Operators read via the localhost UNIX admin socket
  (`holomush admin audit query`).
- **Backpressure:** `plugin_integrity_violation` is under
  `audit.<game>.system.*`, NOT under `audit.<game>.plugin_decrypt.*`.
  Master spec §7.6's "audit emission backpressure mechanism" applies
  to plugin-decrypt audit specifically; this subject does NOT
  participate. Cardinality is naturally bounded: fence-refusal
  short-circuits stream processing for the offending event_id (the
  client receives `metadata_only=true` and the validator emits at
  most one violation per fenced event), so a malicious-plugin
  streaming attack cannot inflate violation emissions beyond the
  query's own page-size cap.

`site/docs/reference/audit-subjects.md` gets the registration row.

## Section 8 — Failure modes

| Failure | Detected by | Behaviour |
|---|---|---|
| Plugin's `QueryHistory` returns a row where `codec=identity` for an event whose type is `sensitivity: always` in some plugin's manifest | `PluginDowngradeFence` (INV-P7-7) | Refuse the row with `AUDIT_ROW_DOWNGRADE_DETECTED`. Emit `plugin_integrity_violation`. Surface refusal to the client as `metadata_only=true` for that event_id (per master INV-26 contract). |
| Plugin's `QueryHistory` returns a row where AAD-bound fields (subject, type, actor, timestamp) are tampered but codec/dek_ref match the host's expectation | AEAD AAD-binding at decrypt (master INV-25) | Decrypt fails with tag-mismatch; existing error path. No `plugin_integrity_violation` emitted (the fence didn't trip); the decrypt-side error is the operator signal. Per the two-layer fence design (§1 threat model), this is the cryptographic ground truth catching what the heuristic doesn't. **Operator-UX rationale**: emitting `plugin_integrity_violation` from the decrypt path would couple two unrelated failure modes (key resolution failure vs intentional row tampering) under the same diagnostic label. Operators chasing a decrypt failure follow the existing crypto runbook; the malicious-plugin signal stays distinct in `plugin_integrity_violation` audits for the cases the heuristic catches. |
| **`may`-elevated downgrade**: plugin returns `codec="identity"` + cleartext payload for an event_id whose `event_type` is manifest-declared `may` AND the event was runtime-elevated to `SensitivityAlways` via INV-6/INV-7 (so the host encrypted at emit) | AEAD AAD-binding at decrypt (master INV-25) | Same as the AAD-tamper row above. The manifest-set fence misses this class by design (the set is `always`-only); the AAD-binding catches it. Operator-visible signal is the decrypt-side error, NOT `plugin_integrity_violation`. **Documented gap** (§1 threat-model coverage boundary); accepted as the cost of the no-host-shadow architecture. Future enhancement bead MAY close this UX gap with a per-event `was_encrypted` shadow flag if operational experience shows the dedicated signal is needed. |
| Plugin's `QueryHistory` returns a row with `codec != "identity"` AND `dek_ref` pointing at a non-existent or soft-deleted `crypto_keys` row | `PluginDowngradeFence` pre-decrypt check (INV-P7-15) | Refuse with `metadata_only=true` per master INV-26. **No fallback** — for plugin-owned subjects the plugin's row IS the cold record; no host shadow exists. The read terminates immediately with metadata-only. No `plugin_integrity_violation` audit emitted (the failure mode is indistinguishable from a legitimate `Rekey`-destroyed-DEK case). |
| Plugin dispatcher's KeySelector unwired | Boot-time check (INV-P7-9) | Panic at startup: `PLUGIN_DISPATCHER_NO_SELECTOR`. Pre-Phase-7 this dispatch path rejected any non-identity codec; post-Phase-7 the absence of a selector is a misconfiguration that MUST surface at boot, not at first sensitive event. |
| `plugin_integrity_violation` audit emit fails (NATS down, queue full) | Existing `AuditEmitter` error path | Operator-visible error logged; refusal of the offending row still proceeds (the client gets `metadata_only=true`). The audit gap is accepted here because the alternative — refusing reads — is worse than a missed diagnostic record. Documented in `site/docs/operating/crypto-runbook.md` (Phase 8). |
| Plugin migration runs without host migrations | Plugin runner | Succeeds; ALTER ADD COLUMN is idempotent and standalone (INV-P7-10). |
| Plugin role attempts INSERT into `events_audit` | PostgreSQL permission check | `permission denied for schema public` (INV-P7-13). |

## Section 9 — Migration / cutover

Phase 7 is a single PR containing:

1. New `internal/eventbus/audit/header_parser.go` (shared parser
   extracted from existing projection.go logic).
2. Modified `internal/eventbus/audit/plugin_consumer.go` (removes
   identity-codec rejection; wires KeySelector; forwards ciphertext).
3. New `internal/eventbus/history/plugin_downgrade_fence.go` — implements
   and wraps the `history.PluginHistoryRouter` interface (defined at
   `tier.go:79-84`). Composition at wiring time:
   `NewPluginDowngradeFence(audit.NewPluginHistoryRouter(...))`. No new
   import edges in the package graph.
4. New `internal/eventbus/history/plugin_aad_adapter.go` — implements
   the `pluginauditpb.AuditRow → *eventbusv1.Event` per-field copy
   used by the decrypt path to reconstruct AAD via `aad.Build`
   (`internal/eventbus/crypto/aad/aad.go:62`). Six fields copied; two
   nil-safety guards (Actor, Timestamp).
5. Reshaped `api/proto/holomush/plugin/v1/audit.proto` + regenerated proto code:
   new `AuditRow` message (id, subject, type, timestamp, actor, codec,
   payload, dek_ref optional, dek_version optional, schema_ver);
   `AuditEventRequest.event` + `headers` fields dropped, replaced with
   `AuditRow row = 1`; `QueryHistoryResponse.event` replaced with
   `AuditRow row = 1`.
6. Migrated all `pluginauditpb.AuditEventRequest.GetEvent()` / `.GetHeaders()`
   callers to the new shape:
   - `plugins/core-scenes/audit.go:160-237` — reads from `req.GetRow()` fields
     instead of the headers map.
   - `test/integration/eventbus_e2e/plugin_audit_isolation_test.go:175-220`
     test stub mirrors the real handler shape.
   - `internal/eventbus/audit/plugin_consumer_unit_test.go:82` —
     `cli.gotReq.GetHeaders()` assertion replaced with `.GetRow()` field
     assertions.
7. Extended `pkg/plugin/audit.go` (Layer 2 helpers with `SchemaVer`).
8. Plugin migration: `plugins/core-scenes/migrations/0000XX_add_scene_log_dek_columns.{up,down}.sql`.
9. Updated `plugins/core-scenes/audit.go`: handles new dek_ref/dek_version
   columns; constructs `AuditRow` proto on QueryHistory; reads from
   `AuditEventRequest.GetRow()` instead of headers map.
10. New test fixture under `test/integration/plugin/testdata/test_downgrade_attacker/`.
11. Test corpus per Section 6.
12. Master spec polish (per "Master spec polish list" in §Context):
    §4.6 audit-subjects registration; §8.2 pseudo-code amendment
    striking the events_audit-fallback line; §2 INV-50 cross-reference;
    §8.3 Go-struct example field-set match.
13. PR-blocking docs: `site/docs/extending/binary-plugins.md` SDK pointer,
    `site/docs/reference/audit-subjects.md` violation-subject registration.

No backfill required: existing `scene_log` rows have `codec=identity`;
the new `dek_ref`/`dek_version` columns default NULL, which is correct
for identity rows.

Rollback: drop the new columns + revert the dispatcher change. The
proto reshape is a clean break and isn't trivially reversible, which is
acceptable for the no-deployments codebase.

## Prerequisites and dependencies

- Phase 3 (`holomush-ojw1`) — DONE. Provides encryption substrate,
  codec, AuthGuard, DEK lifecycle, AAD discipline.
- Phase 5 (`holomush-jxo8`) — DONE. Provides AdminReadStream
  break-glass semantics referenced from master spec §7.5.
- No external dependency changes; no schema changes to `crypto_keys`
  or `events_audit`.

## References

- Master spec: `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`
  (especially §2, §4.6, §7, §8.2, §8.3, §11.1 row 7).
- Phase 3d grounding:
  `docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md`.
- Plugin schema isolation: `internal/plugin/schema_provisioner.go`.
- Existing plugin-owned audit pattern: `plugins/core-scenes/audit.go`,
  `plugins/core-scenes/migrations/000004_create_scene_log.up.sql`.
- Existing emit-time sensitivity fence: `internal/plugin/sensitivity_fence.go`.
- Existing host audit projection:
  `internal/eventbus/audit/projection.go` (especially line 240-262
  header parsing extracted to `header_parser.go` in Phase 7).
- Existing per-plugin audit dispatcher:
  `internal/eventbus/audit/plugin_consumer.go` (especially line 332-365
  documenting the parked KeySelector wiring).
- Existing tier reader with plugin routing:
  `internal/eventbus/history/tier.go:346-369`.
- Existing test fixture pattern:
  `test/integration/plugin/testdata/forgery_plugin/`.
