# Event Payload Cryptography — Phase 7: Plugin SDK + Plugin-Owned Audit Crypto Integration

## Status

DRAFT — awaiting `design-reviewer` and `crypto-reviewer` gates.

## Authors

Sean Brandt + Claude Opus 4.7

## Date

2026-05-13

## Context

### Project context

Phase 7 of the Event Payload Cryptography epic (`holomush-1r0v`, parent
`holomush-e49r`). Phases 2–5 shipped the crypto substrate for host-emitted
events: encryption at emit, AuthGuard, decrypt-on-fanout, DEK lifecycle,
operator break-glass via `AdminReadStream`. The substrate stops at the host
boundary.

Phase 7 brings third-party plugin authors into the encryption story without
requiring them to reimplement crypto. Today, a plugin that wants to emit a
private event (DM, whisper, scene-private state change) would have to:

- Own its own audit table and store host-emitted event rows.
- Mirror the `events_audit` schema's crypto-metadata columns (`dek_ref`,
  `dek_version`, `codec`) byte-for-byte.
- Implement its own `PluginAuditService.QueryHistory` returning those
  columns unchanged.
- Trust the host to validate everything correctly on the read path.

Each of those is a footgun for plugin authors and a privilege gradient
the host has no way to enforce uniformly. The master spec
(`docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` §8.2,
§8.3) proposed mirroring `events_audit`'s crypto columns into plugin
tables and retrofitting INV-50 as a read-time manifest-set heuristic.
This design re-examines that contract and proposes a cleaner trust
boundary instead.

### Out of scope (non-goals)

- Backfill of pre-Phase-7 plugin audit rows. Clean break: Phase 7
  migration truncates `scene_log` and any other plugin-owned audit
  tables. No prod, no deployments
  ([feedback_no_prod_shape_for_undeployed]).
- New sensitivity tiers beyond `always` / `may` / `never`. Existing
  taxonomy holds.
- Multi-tenant per-plugin key separation. Single-game scope; no
  plugin-scoped KEKs.
- Codec versioning / migration logic in the validator. The validator's
  only job is disagreement detection; codec evolution is a separate
  cross-cutting concern.
- Plugin domain tables (scenes, participants, ops_events, etc.)
  participating in crypto. Manifest `audit:` subjects only.
- Phase 8 site documentation. Tracked separately under `holomush-hd8h`.
  Phase 7 carries only the PR-blocking inline docs (§8.2/§8.3 spec
  revision + `binary-plugins.md` SDK pointer).
- Vault provider integration. Detached to `holomush-aub5` P4; not a
  Phase 7 dependency.

### Relationship to substrate spec

This design **revises** the master spec in two specific places:

1. **§8.2** — the "plugin audit table mirrors `events_audit` shape"
   contract is replaced with "plugin tables hold projection-only fields;
   crypto metadata lives in a host-owned `event_crypto_refs` table."
2. **§8.3** — `AuditRow` struct drops the `Codec`, `DEKRef`, and
   `DEKVersion` fields. They were never plugin-domain knowledge.
3. **Section 2 invariants** — INV-46, INV-48, INV-50 wordings updated to
   reference concrete byte-level disagreement against `event_crypto_refs`
   rather than the manifest-set heuristic. New Phase-7-scoped invariants
   (INV-P7-1 through INV-P7-14) are introduced in this document.

The substrate change is significant enough that `crypto-reviewer`
(in addition to `design-reviewer`) MUST run before `writing-plans` is
invoked on this spec.

## Section 1 — Threat model

Inherited from the master spec (§1, "Threat model"). Phase 7 adds one
explicit adversary:

| Adversary | Capability | Phase 7 defence |
|---|---|---|
| **Malicious or buggy plugin** | Returns rows from `PluginAuditService.QueryHistory` that disagree with what the host originally encrypted — e.g., `codec=identity` plus cleartext `payload` for an event the host encrypted, hoping the host will deliver it. | Host-owned `event_crypto_refs` is the ground truth for every event's codec + DEK refs. The `PluginRowValidator` is the only path from plugin frame → bus delivery, and it cross-references every frame against `event_crypto_refs`. Any disagreement refuses the row and emits `plugin_integrity_violation` audit. |

The plugin role's Postgres credentials cannot directly tamper with
`event_crypto_refs`: each plugin gets a dedicated role with `REVOKE ALL
ON SCHEMA public` (`internal/plugin/schema_provisioner.go:139-170`), so
the table name doesn't even resolve under the plugin's connection.

## Section 2 — Invariants & testable proofs

Numbered RFC2119 invariants, each mapped to a named test. A meta-test
walks this table at compile/test time and asserts every named test
exists in the tree.

### What MUST be true

| ID | Invariant | Verification |
|---|---|---|
| **INV-P7-1** | The host audit projection MUST write an `event_crypto_refs` row for every JetStream delivery whose subject resolves to a plugin owner, before acknowledging the delivery to JetStream. | Integration: `event_crypto_refs_projection_test.go::TestProjectionWritesCryptoRefRowBeforeAck`. |
| **INV-P7-2** | Every code path that delivers a plugin-supplied row to a client MUST cross `PluginRowValidator.Validate`. The type contract enforces this at the boundary: plugin routers return `*pluginauditpb.AuditEvent` (raw); bus delivery requires `*eventbus.HistoryFrame` (validated); the only legitimate conversion lives in the validator. A meta-test (`phase7_boundary_meta_test.go`) grep-asserts no other conversion site exists. | Static + Meta-test. |
| **INV-P7-3** (replaces master spec INV-50) | The validator MUST refuse a frame where `frame.codec = "identity"` AND the joined `event_crypto_refs.codec ≠ "identity"`. Error code `AUDIT_ROW_DOWNGRADE_DETECTED`. Audit event `audit.<game>.system.plugin_integrity_violation` emitted on refusal. | Unit (`plugin_validator_test.go::TestValidateRefusesDowngradeAttack`) + Integration (`plugins/test-downgrade-attacker/` binary fixture). |
| **INV-P7-4** (replaces master spec INV-48) | The validator MUST refuse a frame where `frame.codec ≠ event_crypto_refs.codec` for any disagreement (not only the identity-vs-non-identity case). Error code `AUDIT_ROW_CODEC_MISMATCH`. INV-P7-3 is the named special case of this rule where the disagreement is specifically the downgrade-attack shape (`frame.codec = "identity"` + `event_crypto_refs.codec ≠ "identity"`); it carries its own error code and triggers the `plugin_integrity_violation` audit emission because it's the threat-model adversary we explicitly defend against. INV-P7-4 covers all other disagreements (e.g., codec version drift). | Unit: `plugin_validator_test.go::TestValidateRefusesCodecMismatch`. |
| **INV-P7-5** | The validator MUST refuse a frame whose `event_id` has no matching `event_crypto_refs` row. Error `AUDIT_ROW_NOT_FOUND`. (Should be impossible after the clean-break truncation; rule exists to make the proceeds-or-refuses decision total.) | Unit: `plugin_validator_test.go::TestValidateRefusesMissingCryptoRef`. |
| **INV-P7-6** | `event_crypto_refs.codec = "identity"` MUST coincide with both `dek_ref` and `dek_version` being NULL; `codec ≠ "identity"` MUST coincide with both being non-NULL. | DB constraint (`event_crypto_refs_codec_dek_agreement` CHECK) + integration test. |
| **INV-P7-7** | `pluginsdk.StoreFromMessage(msg)` round-tripped through `pluginsdk.LoadForQuery(row)` MUST yield byte-equal `payload` and identical projection fields (event_id, subject, type, timestamp, actor). | Unit: `audit_test.go::TestAuditRowRoundTripPreservesProjectionFields`. |
| **INV-P7-8** | The audit projection's plugin-owned-subject branch MUST be idempotent under JetStream redelivery: the same `Nats-Msg-Id` delivered twice MUST result in exactly one `event_crypto_refs` row. | Integration: `event_crypto_refs_projection_test.go::TestProjectionIdempotentOnRedelivery`. |
| **INV-P7-9** | The Phase 7 plugin-schema migration (truncate + drop codec) MUST be runnable independently of host-schema state (plugin migrations run in plugin's own runner). | Integration: `plugin_migration_test.go::TestPhase7CleanBreakMigrationStandalone`. |

### What MUST NOT be true

| ID | Invariant | Verification |
|---|---|---|
| **INV-P7-10** | A plugin's `PluginAuditService.QueryHistory` response MUST NOT cause the host to deliver plaintext for any event whose `event_crypto_refs.codec` is non-identity, even if the plugin returns `codec=identity` with a cleartext payload. | End-to-end: `plugins/test-downgrade-attacker/` binary fixture exercises the full loader/gRPC/projection-consumer/validator chain. |
| **INV-P7-11** | The validator MUST NOT consult plugin-supplied `codec` or DEK metadata as ground truth — only as a candidate to verify against host-owned `event_crypto_refs`. | Unit: `plugin_validator_test.go::TestValidateIgnoresPluginSuppliedCryptoMetadataAsGroundTruth`. |
| **INV-P7-12** | The plugin SDK Layer 2 (`AuditRow` / `StoreFromMessage` / `LoadForQuery`) MUST NOT expose any crypto metadata field (codec, dek_ref, dek_version). The type system makes them unrepresentable. | Compile-time + unit: `audit_test.go::TestAuditRowHasNoCryptoFields`. |
| **INV-P7-13** | Plugin code MUST NOT have a path that writes to `event_crypto_refs`. The plugin's Postgres role lacks USAGE on schema `public` (per `internal/plugin/schema_provisioner.go:163`), so any direct INSERT fails with `permission denied for schema public`. | Integration: `plugin_role_permissions_test.go::TestPluginRoleCannotWriteEventCryptoRefs`. |
| **INV-P7-14** | The `crypto.emits` manifest declaration is the ONLY emit-time gate for plugin-emitted sensitive events (existing INV-6/INV-7 / `sensitivity_fence.go`). Phase 7 MUST NOT add a second emit-time gate. | Static review + meta-test: `phase7_boundary_meta_test.go::TestNoNewEmitTimeSensitivityGates`. |

### Meta-test

A single test file `internal/eventbus/history/phase7_boundary_meta_test.go`
walks every `INV-P7-N` in this section's tables and asserts the named
test exists in the tree (by `path:function` lookup). Drift between this
table and the test corpus fails CI.

## Section 3 — Architecture

### 3.1 Trust-boundary picture

```text
Plugin-emitted event flow (sensitive)
─────────────────────────────────────
  Plugin code  ─emit→  HostPluginService.Emit  ─[INV-6/INV-7 fence]→  encrypt(host KEK/DEK)  →  JetStream
                                                                              │
                                                                              ▼
                                                                  headers: dek_ref, dek_version, codec
                                                                              (host-set)

Audit projection (host)
───────────────────────
  JetStream  ─consumer→  HostAuditProjection
                              │
                              ├── owner = host    →  INSERT events_audit (full row)             [existing]
                              └── owner = plugin  →  INSERT event_crypto_refs (narrow row)      [NEW Phase 7]
                                                 →  forward to plugin.AuditEvent RPC            [existing, unchanged]

Plugin-owned history read flow
──────────────────────────────
  Client  ─QueryStreamHistory→  CoreServer  →  history.Reader  ─owner=plugin→  PluginHistoryRouter
                                                                                     │
                                                                                     ▼
                                                                         plugin.QueryHistory (gRPC)
                                                                                     │
                                                                            raw *pluginauditpb.AuditEvent
                                                                                     │
                                                                                     ▼
                                                                            PluginRowValidator   ←─ joins event_crypto_refs
                                                                            (INV-P7-3/4/5)             (host ground truth)
                                                                                     │
                                                                                     ▼
                                                                         validated *eventbus.HistoryFrame
                                                                                     │
                                                                                     ▼
                                                                         AuthGuard + DEK decrypt + delivery (existing)
```

### 3.2 Components added/changed

| Component | Status | Responsibility |
|---|---|---|
| `pkg/plugin/audit.go` | EXTENDED | Add `AuditRow` + `StoreFromMessage` + `LoadForQuery` Layer-2 helpers below the existing Layer-1 ABAC decision-hint recorder; package-level doc enumerates both surfaces. |
| `internal/store/migrations/0000XX_create_event_crypto_refs.{up,down}.sql` | NEW | `event_crypto_refs(event_id BYTEA PK, codec TEXT NOT NULL, dek_ref BIGINT NULL, dek_version INTEGER NULL, inserted_at TIMESTAMPTZ)` + CHECK constraint + partial index on `dek_ref`. |
| `internal/eventbus/audit/projection.go` | EXTENDED | Plugin-owned-subject branch: write narrow `event_crypto_refs` row from JS headers (today ack-and-skips). |
| `internal/eventbus/history/plugin_validator.go` | NEW | `PluginRowValidator` type; owns INV-P7-3 / 4 / 5 / 11 checks; queries `event_crypto_refs` for ground truth; emits `plugin_integrity_violation` audit on refusal. |
| `internal/eventbus/history/tier.go` | EXTENDED | `Reader.QueryHistory` plugin branch routes router output through validator before returning stream. |
| `plugins/core-scenes/migrations/0000XX_truncate_scene_log_drop_codec.{up,down}.sql` | NEW | Phase 7 clean-break: `TRUNCATE scene_log; ALTER TABLE scene_log DROP COLUMN codec`. |
| `plugins/core-scenes/audit.go` | TRIMMED | Remove `codec` column read/write (now host's domain); `payload` and other projection fields unchanged. |
| `plugins/test-downgrade-attacker/` | NEW | Binary plugin fixture for INV-P7-10 e2e test: manifest declares one `sensitivity: always` event type; happy path emits one event (host encrypts, projection writes crypto ref); malicious `QueryHistory` returns the same `event_id` with `codec=identity` + cleartext payload. |
| `internal/eventbus/history/plugin_validator_test.go` | NEW | Truth-table unit tests against router fake (INV-P7-3 / 4 / 5 / 6 / 11). |
| `internal/eventbus/history/event_crypto_refs_projection_test.go` | NEW | Integration: drives audit projection with both host- and plugin-owned subjects (INV-P7-1, INV-P7-6, INV-P7-8). |
| `internal/eventbus/history/plugin_role_permissions_test.go` | NEW | Integration: asserts plugin role cannot INSERT into `event_crypto_refs` (INV-P7-13). |
| `internal/eventbus/history/phase7_boundary_meta_test.go` | NEW | Meta-test for INV-P7-2 / INV-P7-14: grep-asserts no other plugin-frame → bus-frame conversion site, no new emit-time sensitivity gates. |
| `pkg/plugin/audit_test.go` | EXTENDED | Layer-2 round-trip (INV-P7-7), type shape (INV-P7-12). |
| `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` | REVISED | §8.2 contract update; §8.3 `AuditRow` field set update; §2 INV-46/48/50 wording. |
| `site/docs/extending/binary-plugins.md` | EXTENDED | Document the Layer-2 SDK helpers (`pluginsdk.AuditRow`, `StoreFromMessage`, `LoadForQuery`) and how plugin-owned audit tables relate to host-owned `event_crypto_refs`. PR-blocking inline doc; broader docs ramp under Phase 8. |

### 3.3 Boundaries (what each component MUST/MUST NOT know)

- **Plugin code** MUST NOT know the codec, the DEK ref, or anything
  cryptographic. It stores the bytes it receives via `AuditEvent` RPC and
  returns them on `QueryHistory`. Schema simplifies (codec column drops).
- **`PluginRowValidator`** is the sole reader of `event_crypto_refs` at
  query time. No other host code reads this table during reads; the
  validator owns the join + the agreement check.
- **Plugin SDK Layer 2 helpers** are convenience for plugin authors;
  they do NOT enforce anything host-side. Host has its own ground truth
  in `event_crypto_refs` and does not trust SDK output.

### 3.4 Why this shape

- **Ownership boundaries match knowledge boundaries.** Plugin owns its
  projection (subject, type, actor, timestamp, payload). Host owns
  crypto metadata (codec, dek_ref, dek_version). Neither lies because
  neither stores what it cannot authoritatively prove.
- **Type system enforces the trust boundary.** No code path can deliver
  a plugin-supplied row to a client without crossing
  `PluginRowValidator`. INV-P7-3, INV-P7-4, INV-P7-5 are co-located
  corollaries of one architectural commitment, not three independent
  retrofits.
- **Plugin migrations stay simple.** Phase 7 reduces plugin schema
  complexity (drops a column); it doesn't add new requirements plugin
  authors have to know about. Future crypto-metadata evolution
  (audit-chain hash, schema versions, replay tokens) lands in host
  migrations only.
- **Spec coherence improved.** §8.2's mirror-events_audit contract was a
  coupling that didn't match the trust model. Phase 7 fixes the spec
  along with the implementation.

## Section 4 — Data model

### 4.1 New host table: `event_crypto_refs`

```sql
CREATE TABLE event_crypto_refs (
    event_id     BYTEA       PRIMARY KEY,         -- 16-byte ULID, matches Event.id
    codec        TEXT        NOT NULL,            -- "identity" | "xchacha20poly1305-v1" | future
    dek_ref      BIGINT      NULL,                -- NULL for codec=identity
    dek_version  INTEGER     NULL,                -- NULL for codec=identity
    inserted_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX event_crypto_refs_dek_ref
    ON event_crypto_refs (dek_ref)
    WHERE dek_ref IS NOT NULL;

ALTER TABLE event_crypto_refs ADD CONSTRAINT event_crypto_refs_codec_dek_agreement
    CHECK ((codec = 'identity' AND dek_ref IS NULL AND dek_version IS NULL)
        OR (codec <> 'identity' AND dek_ref IS NOT NULL AND dek_version IS NOT NULL));
```

No foreign key to `crypto_keys`. `Rekey` destroys old `crypto_keys` rows
by design (master spec §4.7); the same rule applies here as in
`events_audit.dek_ref`.

Idempotency: `INSERT ... ON CONFLICT (event_id) DO NOTHING`.

### 4.2 Modified plugin table: `scene_log` (and any future plugin audit table)

```sql
TRUNCATE scene_log;
ALTER TABLE scene_log DROP COLUMN codec;
```

Plugin's audit table schema after Phase 7 holds projection-only fields:
`(id BYTEA PK, subject TEXT, type TEXT, timestamp TIMESTAMPTZ,
actor_kind TEXT, actor_id BYTEA, payload BYTEA, schema_ver SMALLINT,
js_seq BIGINT, inserted_at TIMESTAMPTZ)`.

### 4.3 Plugin SDK — Layer 2 type contracts

```go
// AuditRow is the canonical projection-only shape for plugin-owned audit
// rows. Crypto-metadata fields (codec, dek_ref, dek_version) are
// intentionally absent — those live in host-owned event_crypto_refs.
type AuditRow struct {
    EventID   ulid.ULID
    Subject   string
    Type      string                // "<plugin>:<event_type>"
    Timestamp time.Time
    Actor     *eventbuspb.Actor
    Payload   []byte                // opaque bytes — ciphertext when host encrypted; plaintext otherwise
}

// StoreFromMessage builds an AuditRow from a JetStream message. Discards
// crypto headers (dek_ref, dek_version, codec) — those flow separately
// to the host's audit projection consumer.
func StoreFromMessage(msg jetstream.Msg) (AuditRow, error)

// LoadForQuery converts a stored AuditRow into the proto frame returned
// by PluginAuditService.QueryHistory. Round-trip stable with StoreFromMessage.
func LoadForQuery(row AuditRow) (*pluginauditpb.AuditEvent, error)
```

### 4.4 Host-side validator

```go
// PluginRowValidator is the typed trust boundary between raw plugin
// frames and the host's bus-event delivery path. Every check the host
// enforces against plugin responses lives here.
type PluginRowValidator interface {
    // Validate converts an unchecked plugin frame into a validated bus
    // frame, cross-referencing the host-owned event_crypto_refs row.
    //
    // Typed errors:
    //   - AUDIT_ROW_DOWNGRADE_DETECTED (INV-P7-3)
    //   - AUDIT_ROW_CODEC_MISMATCH     (INV-P7-4)
    //   - AUDIT_ROW_NOT_FOUND          (INV-P7-5)
    Validate(ctx context.Context, frame *pluginauditpb.AuditEvent) (*eventbus.HistoryFrame, error)
}
```

The concrete implementation takes a host pool (for `event_crypto_refs`
lookup) and an audit emitter (for `plugin_integrity_violation` events on
refusal).

## Section 5 — Audit projection change

The existing `internal/eventbus/audit/projection.go` ack-and-skips
plugin-owned subjects today. Phase 7 routes those into a narrow
`event_crypto_refs` INSERT:

```text
For each JetStream delivery msg:
  subject_owner = owners.Resolve(msg.Subject)
  switch subject_owner:
    case Host:
      INSERT events_audit (full row + crypto cols)                       [unchanged]
    case Plugin:
      INSERT event_crypto_refs (event_id, codec, dek_ref, dek_version)   [NEW]
      forward msg to plugin.PluginAuditService.AuditEvent RPC            [unchanged]
```

Both branches use `ON CONFLICT (event_id) DO NOTHING` for redelivery
idempotency. The two INSERTs in the plugin branch (host `event_crypto_refs`
+ plugin `scene_log` via RPC) are NOT atomic; the host write commits first,
then the plugin RPC is invoked. JetStream redelivery handles partial
failures (host wrote, plugin RPC failed): redelivery re-runs both
INSERTs idempotently. INV-P7-8 covers this.

## Section 6 — Test strategy

| Layer | Tests | Build tag |
|---|---|---|
| **Unit** | `plugin_validator_test.go` — exhaustive truth table (INV-P7-3 / 4 / 5 / 6 / 11). Router fake feeds synthesized frames; constructed `event_crypto_refs` fixtures via testdb helper. `audit_test.go` Layer-2 round-trip (INV-P7-7). Layer-2 type shape (INV-P7-12). Meta-test (INV-P7-2, INV-P7-14). | none — runs in `task test` |
| **Integration** | `event_crypto_refs_projection_test.go` — drives audit projection with host- and plugin-owned subjects; asserts narrow-row write on plugin subjects (INV-P7-1, INV-P7-6, INV-P7-8). `plugin_role_permissions_test.go` — INV-P7-13 perm denial. `plugin_migration_test.go` — INV-P7-9 standalone migration runnability. | `//go:build integration` |
| **End-to-end** | `plugins/test-downgrade-attacker/` binary fixture: manifest declares `crypto.emits: [{event_type: secret, sensitivity: always}]`; emits one sensitive event (host encrypts, projection writes `event_crypto_refs` with `codec=xchacha20poly1305-v1`); deliberately-malicious `QueryHistory` returns the same `event_id` with `codec=identity` + cleartext `payload`. Host e2e test calls `QueryStreamHistory`, asserts refusal + `plugin_integrity_violation` audit emission (INV-P7-3, INV-P7-10). Plus one happy-path: same plugin with non-malicious `QueryHistory` delivers correctly. | `//go:build integration` |
| **Lint / static** | Existing `gorules/dek_no_serialize.go` continues. No new ruleguard rules. | `task lint` |
| **Coverage gate** | Per project rule: >80% per package. `internal/eventbus/history/` and `pkg/plugin/` MUST stay above; new files covered by their named unit tests. | `task test:cover` |

## Section 7 — Spec revision summary

The Phase 7 PR carries a single-section revision to
`docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`:

- **§8.2 table** — "Plugin's audit table schema" row changes from
  `Mirrors events_audit shape: payload BYTEA, codec TEXT, dek_ref BIGINT,
  dek_version INT` to `Projection-only fields: payload BYTEA. Crypto
  metadata (codec, dek_ref, dek_version) is host-owned in
  event_crypto_refs.`
- **§8.2 downgrade-attack-fence paragraph** — pseudo-code rewritten to
  express the concrete `event_crypto_refs.codec ≠ frame.codec`
  disagreement check, replacing the manifest-set heuristic.
- **§8.3** — `AuditRow` struct definition updated to omit `Codec`,
  `DEKRef`, `DEKVersion` fields. Surrounding prose updated.
- **Section 2 invariants** — INV-46 / INV-48 / INV-50 wording updated to
  reflect concrete-disagreement semantics; cross-references added to the
  new INV-P7-N corpus in this design doc.

This is a substrate-level spec change. `crypto-reviewer` gate fires on
the design doc + spec revision before `superpowers:writing-plans` can be
invoked.

## Section 8 — Failure modes

| Failure | Detected by | Behaviour |
|---|---|---|
| Plugin `QueryHistory` returns `codec=identity` for an event whose `event_crypto_refs.codec` is non-identity | `PluginRowValidator.Validate` | Refuse row with `AUDIT_ROW_DOWNGRADE_DETECTED`; emit `audit.<game>.system.plugin_integrity_violation`. |
| Plugin `QueryHistory` returns any other codec disagreement (e.g., `xchacha20poly1305-v1` when host stored `xchacha20poly1305-v2`) | `PluginRowValidator.Validate` | Refuse row with `AUDIT_ROW_CODEC_MISMATCH`; emit `plugin_integrity_violation`. |
| Plugin returns a frame whose `event_id` has no matching `event_crypto_refs` row | `PluginRowValidator.Validate` | Refuse row with `AUDIT_ROW_NOT_FOUND`. Logged at WARN — should be impossible after the clean-break truncation; the rule exists to keep the validator's decision total. |
| Audit projection's plugin-owned-subject branch crashes between writing `event_crypto_refs` and forwarding to the plugin RPC | JetStream redelivery + `ON CONFLICT DO NOTHING` | Redelivery re-runs both inserts idempotently. INV-P7-8 covers this. |
| Plugin role attempts direct `INSERT INTO event_crypto_refs` | PostgreSQL permission check | `permission denied for schema public` (schema-level USAGE revoked at provisioning). INV-P7-13 asserts. |
| Pre-Phase-7 plugin audit row exists at read time (e.g., dev DB) | Clean-break migration | Should be impossible after the migration runs. If it happens, validator returns `AUDIT_ROW_NOT_FOUND`. Operator remediation: re-run plugin migrations. |

## Section 9 — Migration / cutover

Phase 7 is a single PR. The PR contains:

1. Host migration adding `event_crypto_refs` table + CHECK + index.
2. Plugin migration truncating `scene_log` + dropping `codec` column.
3. Audit projection consumer change.
4. New `PluginRowValidator` + plugin-frame-to-bus-frame wiring at
   `Reader.QueryHistory`.
5. SDK Layer 2 additions to `pkg/plugin/audit.go`.
6. Test corpus per Section 6.
7. `plugins/test-downgrade-attacker/` binary fixture.
8. Spec revision to master event-payload-crypto-design doc.
9. `site/docs/extending/binary-plugins.md` SDK reference update.

No backfill. No rollback path: an environment that already ran the
plugin clean-break migration cannot return to the codec-column world
without restoring from backup. For an undeployed codebase this is
acceptable; documented in §1 out-of-scope.

## Prerequisites and dependencies

- Phase 3 (`holomush-ojw1`) — DONE. Provides the encryption substrate
  (codec, AuthGuard, DEK lifecycle, AAD discipline, decrypt-on-fanout).
- Phase 5 (`holomush-jxo8`) — DONE. Provides `AdminReadStream` and
  break-glass audit semantics referenced from §8.5 of the master spec.
- No new external dependencies; no schema changes to `crypto_keys` or
  `events_audit`.

## References

- Master spec: `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`
  (especially §2, §4.6, §7, §8, §11.1 row 7).
- Phase 3d grounding: `docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md`.
- Plugin schema isolation: `internal/plugin/schema_provisioner.go`.
- Existing plugin-owned audit pattern: `plugins/core-scenes/audit.go`,
  `plugins/core-scenes/migrations/000004_create_scene_log.up.sql`.
- Existing emit-time sensitivity fence: `internal/plugin/sensitivity_fence.go`.
- Existing host-side QueryStreamHistory: `internal/grpc/query_stream_history.go`.
- Existing tier reader with plugin routing: `internal/eventbus/history/tier.go:346-369`.
