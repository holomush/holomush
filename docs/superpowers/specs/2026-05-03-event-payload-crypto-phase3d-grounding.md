<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Event Payload Cryptography — Phase 3d Final Synthesis Grounding

## Status

**REVISION 3 (DRAFT — pending design-reviewer re-run).** Final sub-phase of Phase 3 (`holomush-ojw1`). Synthesises the remaining Phase 3 capabilities into a single deliverable PR and flips `Crypto.Enabled` to its visible-behavior default. Companion document, not a replacement: the master spec at [`2026-04-25-event-payload-crypto-design.md`](2026-04-25-event-payload-crypto-design.md) remains authoritative; this document records the Phase-3d-specific decisions and structures the work for plan-writing.

**Revision history:**

- **Revision 1** (2026-05-03 initial) — caught NOT READY by `design-reviewer`. Four blocking findings: (1) cold-tier reconstruction omitted Actor.legacy_id and nanosecond Timestamp, breaking AAD for plugin-authored events; (2) NATS deny-install hook point assumed substrate that doesn't exist; (3) master-spec edit phrased "MAY" not "MUST"; (4) stale-DEK case conflated INV-39 with §8.4 terminal branch.
- **Revision 2** — addressed Revision 1 by retiring NATS deny scope (architectural realization: game-topic NATS is single-principal by design) and proposing full-envelope persistence as a new `envelope_proto BYTEA` column. Caught NOT READY by `design-reviewer` again with three blocking findings: (1) migration `000015` already taken; (2) `events_audit.payload` already stores the marshaled envelope (the new column would duplicate it); (3) hot-tier dispatcher reads `keyID`/`keyVersion` from `msg.Headers()`, so cold-path "convergence" requires a refactor.
- **Revision 3** (current) — addresses Revision 2 blockers: column rename (`payload` → `envelope`) replaces duplicate-column proposal; dispatcher refactor signature corrected to match the existing `decodeAndAuthorizeHistory` types and return shape; column-rename ripple extended in the architecture table to include all five test-fixture files; verbatim §7.7 master-spec replacement text locked in Appendix A; `holomush-u5bb` close rationale strengthened (intent closed; proposed dedicated column not needed); client-visible stale-DEK signaling filed as P3 follow-up bead. Decision 4 retirement holds (empirically verified).

Normative requirements use RFC2119 keywords (MUST, MUST NOT, SHOULD, SHOULD NOT, MAY) per the project's `CLAUDE.md` "RFC2119 Keywords" convention. Descriptive passages explaining decisions, alternatives, and future phases are not normative.

## Authors

- Sean Brandt
- Claude (collaborator)

## Date

2026-05-03

## Context

Phase 3a (`holomush-ojw1.1`, merged) shipped the `xchacha20poly1305-v1` codec, the `EventSink` encryption path, and the host-side downgrade fence at `internal/plugin/event_emitter.go::Emit`. Phase 3b (`holomush-ojw1.2`, merged) shipped `AuthGuard`, decrypt-on-fan-out, metadata-only delivery, and the binding substrate. Phase 3c (`holomush-ojw1.3`, merged) shipped the DEK cache, request-reply cache invalidation, and cluster substrate.

What remains for Phase 3 to deliver its visible-behavior contract per master spec §11.1 phase 3 row:

- Cold-tier `QueryStreamHistory` crypto path for host-owned audit
- Plugin SDK wire-format surface for `EmitIntent.Sensitive` (Lua + binary)
- Flip `Crypto.Enabled` default `false → true`
- End-to-end integration test of the sensitive flow
- Operator runbook (Phase-3-scoped subset of master spec §9.2)

The master-spec §11.1 phase 3 row also names "NATS account-level deny rules." Revision 2/3 retire that scope based on the architectural realization recorded in Decision 4 — game-topic NATS is single-principal by design, so deny rules have no target. The master spec receives an amendment to §7.7 in T9.

This document records the design decisions made during the Phase 3d brainstorming session on 2026-05-03 and structures the work for plan-writing.

---

## Decision 1 — Resolve `ojw1.3.22` and `ojw1.3.23` as a precursor PR

**Decision:** The two Phase 3c follow-up beads (`ojw1.3.22` oops.Code semantics; `ojw1.3.23` mockable NATSConn interface seam) MUST land in their own PR before the Phase 3d feature branch begins. They are bugfix and refactor work with no design-level relationship to Phase 3d, and folding them in would muddy the spec-compliance review surface of the Phase 3d PR.

**Effective priority change:** Both beads promote from P3 to P1 implicitly by being on the Phase 3d critical path (they are the hard prerequisite for `ojw1.4.1`). Update accordingly.

**Alternatives considered:**

- *Fold into Phase 3d branch as T0/T1.* Rejected: couples bugfix review with feature review; a CodeRabbit finding on the bugfix would block the feature.
- *Cut `ojw1.4.1` from Phase 3d entirely.* Rejected: without the SDK `Sensitive` surface, the flag flip in Decision 2 would crater every plugin emit for `manifest=always` event types.

---

## Decision 2 — `Crypto.Enabled` default flips in the Phase 3d PR

**Decision:** The `Crypto.Enabled` default in `internal/eventbus/config.go` MUST flip from `false` to `true` as part of the Phase 3d feature branch, in the same PR as the cold-tier read path, SDK surface, E2E tests, and runbook. No separate "ship dark, flip later" follow-up.

**Rationale:** HoloMUSH has no existing users, no existing deployments, and no backwards-compatibility constraints. The `Crypto.Enabled = false` default existed solely to keep `main` buildable during the phased landing of Phase 3a, 3b, and 3c. With Phase 3d completing the capability, the rationale for "ships dark" disappears.

**Test invariant flip (verified function name):** `internal/eventbus/config_test.go:14` defines `TestCryptoEnabledDefaultsToFalse`; line 16 asserts `assert.False(t, cfg.Crypto.Enabled, "Phase 3a ships dark — flag must default to off")`. This MUST flip:

- Test function rename: `TestCryptoEnabledDefaultsToFalse` → `TestCryptoEnabledDefaultsToTrue`
- Assertion: `assert.True(t, cfg.Crypto.Enabled, "Phase 3d ships live")`

Both changes land in the same commit that flips the default.

**Rollback:** Master spec §11.4 stays as-written; with no deployments, rollback is a Git revert.

---

## Decision 3 — E2E integration test shape: hybrid happy-path + targeted invariant proof + ABAC denial coverage

**Decision:** Phase 3d's E2E test surface MUST consist of:

1. **One BDD happy-path narrative** at `test/integration/crypto/e2e_test.go` covering emit (both character-authored AND plugin-authored sensitive events) → JS publish → projection insert → fan-out for participant (decrypts plaintext) and non-participant (`metadata_only=true`) → JS retention rolls off → cold-tier `QueryStreamHistory` returns the same content under the same AuthGuard branches.
2. **One targeted invariant proof** at `test/integration/crypto/inv49_envelope_roundtrip_test.go`: `events_audit.envelope` (post-rename per Decision 5) byte-equality across emit→audit→cold-read→decrypt. Asserts that the envelope bytes persisted in PG equal the bytes that went onto the bus, and that decryption succeeds via the cold path for both character-authored AND plugin-authored sensitive events.

Both files MUST use Ginkgo/Gomega per `CLAUDE.md` integration-test convention with the `//go:build integration` tag.

**Plugin-authored sensitive coverage is mandatory.** Decision 5's full-envelope round-trip (via the existing column) eliminates the AAD-divergence risk the design-reviewer surfaced for plugin actors, but the E2E suite MUST exercise plugin-authored sensitive emits explicitly to lock in the invariant.

**ABAC subscribe-deny coverage:** INV-15 (post-Decision-4 reword: ABAC denies subscribe to `audit.>` for `kind={plugin|character}` subjects) MUST have a Phase-3d-touchable test. Plan-writing MUST verify whether an existing ABAC test in `internal/access/` covers this case explicitly; if not, T7 scope expands to add a unit test under `internal/access/` (not E2E — ABAC denial is enforced at the gRPC subscribe handler boundary, which is unit-testable). The grounding doc does not commit to "test file already exists vs. add new" — that's a plan-time verification.

**INV-39 (stale-DEK fallback) deferred.** Master spec §8.4 stale-DEK fallback requires `Rekey` (Phase 5) to set up the failure naturally. Synthesising the failure in 3d would require either a fault-injection seam in the `CryptoProvider` or destroying a `crypto_keys` row out-of-band. Deferred to a Phase 5 follow-up bead.

**INV-21 byte-equality (payload) NOT retested at E2E level.** Already proven at the component level by `internal/eventbus/audit/projection_test.go` — the projection has no decrypt code path, so byte-equality is mechanical. Decision 5 extends INV-21 to cover the renamed `envelope` column; that extension IS exercised by `inv49_envelope_roundtrip_test.go`.

**INV-52 (NATS-level deny) retires per Decision 4.** No test ships in 3d.

**INV-46 (plugin-owned audit byte-equality) is Phase 7** per master spec §11.1; out of scope here.

**Alternatives considered:**

- *Single BDD spec file containing all concerns.* Rejected: file grows too large and the invariant-to-test mapping fragments.
- *One file per invariant only (no happy-path narrative).* Rejected: loses the user-story readability that's the value of BDD.

---

## Decision 4 — Game-topic NATS is single-principal by architectural design; NATS-level deny rules retire

**Decision:** Phase 3d MUST NOT install NATS account-level deny rules. The master spec §7.7 NATS-level deny scope retires entirely. ABAC is the authoritative isolation gate for game-topic subscribe access.

**Architectural grounding (verified in repo):**

- Embedded mode: `internal/eventbus/subsystem.go:96` sets `DontListen: true`; the embedded NATS server has no network listener. The only client is `nats.Connect("", nats.InProcessServer(s.server), ...)` at `subsystem.go:120` — the holomush server itself.
- Plugins: `rg "nats\.Connect|nats\.Subscribe" plugins/ pkg/plugin/` returns no matches. Plugins emit via gRPC `PluginHostService` and receive via host-mediated gRPC streams. They do NOT open NATS connections for game-topic traffic.
- Characters: never had NATS connections; sessions are server-internal subscriptions multiplexed through the server's NATS connection.
- Empirical verification (Revision 3): `rg "nats.Connect|InProcessServer|nats.Subscribe"` across `internal/`, `pkg/`, `plugins/`, `cmd/` — only matches in `internal/eventbus/`. Architectural claim is empirically true today.

**Project intent (per architectural decision recorded 2026-05-03):**

In every HoloMUSH topology — embedded today, external NATS tomorrow — only the holomush server is a NATS-connecting principal on game topics (`events.>`, `audit.>`, `internal.>`). Plugins MAY use NATS separately for their own purposes (their own subjects, their own clusters, their own credentials), but that is a plugin-internal concern with no game-topic surface.

There are therefore no plugin or character NATS account principals in any planned topology; deny rules for `audit.>` or `internal.>` have no target. The master spec §7.7 draft (Revision 1 of this grounding doc carried it forward) presumed an architecture with multi-principal NATS connectivity that HoloMUSH does not implement.

**External NATS deploy** (`holomush-s5ts`): when the server connects to an external NATS cluster, it authenticates as a single account scoped to game topics:

```text
Account "holomush-server":
  publish:   events.>, audit.>, internal.>
  subscribe: events.>, audit.>, internal.>
```

Other accounts in the cluster (any account that isn't `holomush-server`) have NO publish or subscribe permission on these subjects by default — enforced at the cluster admin layer, not inside the server. Optional future addition: a distinct read-only operator account (`holomush-operator-read` with `subscribe: events.>` only) for monitoring or debugging. Both belong to `holomush-s5ts`, not Phase 3d.

**ABAC is the authoritative gate (master spec §7.7 amended; T9 deliverable):**

The default ABAC policy denies `subject={kind: plugin|character}, action: subscribe, resource: subject:audit.>` (and `internal.>`). INV-15 (post-reword) verifies this denial at the gRPC subscribe handler boundary. Defense in depth is at the architectural level (gRPC mediation between plugins/characters and NATS) rather than the substrate level — removing the gRPC mediation would require a deliberate architectural change.

**Capabilities dropped from Revision 1:**

- `internal/eventbus/nats_deny.go` (new file): not written.
- `Crypto.AllowAuditSubscribe` config knob and `HOLOMUSH_TEST_ALLOW_AUDIT_SUBSCRIBE` env override: not added.
- Bootstrap fail-closed wiring for deny-install failure: not implemented.
- `holomush-ojw1.4.2` bead: closed with rationale "architecturally not applicable; game NATS is server-only by design."
- INV-52 invariant: retires; re-cite as architectural property in §7.7 amendment.

---

## Decision 5 — Cold-tier fidelity via existing envelope column (semantics clarification + dispatcher refactor)

**Decision:** Phase 3d MUST clarify and reuse the existing `events_audit.payload` column rather than add a new one. The column ALREADY contains the marshaled `Event` envelope bytes (verified: `internal/eventbus/publisher.go:295` marshals the envelope; `:302` assigns the bytes to `msg.Data`; `internal/eventbus/audit/projection.go:281` writes `msg.Data()` to the `payload` column). The pre-existing column name `payload` is a misnomer because the bytes are not "just the inner application payload" — they are the full marshaled `eventbusv1.Event` envelope, of which `Event.payload` is one nested field.

**Phase 3d deliverables for Decision 5:**

1. **Migration `000017_events_audit_envelope_rename`** (next free migration number; verified `000015` and `000016` already taken) MUST `ALTER TABLE events_audit RENAME COLUMN payload TO envelope`. No data migration required (PG `RENAME COLUMN` is metadata-only). Down migration renames back. With no users this is straightforward; the column rename clarifies semantics for future readers.
2. **Audit projection writer** (`internal/eventbus/audit/projection.go`) MUST update its INSERT to use the new column name `envelope`. No semantic change — still writes `msg.Data()` (the marshaled envelope bytes).
3. **Cold-tier reader** (`internal/eventbus/history/cold_postgres.go`) MUST expand its SELECT to include `envelope, codec, dek_ref, dek_version` columns and `proto.Unmarshal(row.envelope, &event)` to recover the full `*eventbusv1.Event`. The existing field-by-field column reconstruction (`cold_postgres.go:152`) MUST be replaced uniformly — including for `codec='identity'` rows. No two-path code; envelope unmarshal is the only path.
4. **Header-free dispatcher refactor in `internal/eventbus/history/hot_jetstream.go`** MUST extract a shared function. The existing `decodeAndAuthorizeHistory` at `hot_jetstream.go:484-493` takes `(ctx, msg jetstream.Msg, envelope, codecName, identity, guard, dekMgr, auditEm) → (eventbus.Event, bool, error)` and reaches into `msg.Headers()` at `:494` to parse `keyID`/`keyVersion`. The refactor MUST extract a header-free shared dispatcher with the existing return shape and existing crypto types preserved:

   ```go
   func decodeAuthorizeAndDispatch(
       ctx context.Context,
       envelope *eventbusv1.Event,
       codecName codec.Name,            // existing type, not string
       keyID codec.KeyID,                // existing type, not uint64
       keyVersion uint32,                // existing type, not uint64
       identity eventbus.SessionIdentity,
       guard eventbus.SessionAuthGuard,
       dekMgr eventbus.SessionDEKManager,
       auditEm eventbus.SessionAuditEmitter,
   ) (eventbus.Event, bool, error)
   ```

   The hot path's existing function `decodeAndAuthorizeHistory` becomes a thin wrapper that parses `App-Codec`, `App-Dek-Ref`, `App-Dek-Version` from `msg.Headers()` and calls the dispatcher. The cold path becomes a sibling thin wrapper that reads `row.codec`, `row.dek_ref`, `row.dek_version` from PG columns and calls the same dispatcher. Both paths share the AAD computation, codec dispatch, AuthGuard.Decide, and decrypt-or-metadata-only logic. Return shape `(eventbus.Event, bool, error)` is the existing `(event, metadataOnly, err)` triple — preserving the call site at `:440-445` which does `ev.MetadataOnly = metaOnly`.
5. **AAD inputs on cold read enumerated explicitly:** `aad.Build(unmarshaled_envelope, row.codec, row.dek_ref, row.dek_version)`. Identical AAD bytes to the hot path's `aad.Build(envelope, codecName, keyID, keyVersion)` because the inputs are byte-equal (envelope bytes byte-equal between bus and PG; column values byte-equal to the original header values per existing INV-49 forward direction wired in Phase 3a).
6. **Stale-DEK case (cold row references missing `crypto_keys` row, INV-39 deferred per Decision 3):** the placeholder behavior MUST be: deliver event with `metadata_only=true` and emit metric `crypto.cold_dek_miss`. **No wire-level header claim** — Revision 2's `App-Warning: stale-dek` proposal retired. The Event proto has no header bag for cold-path injection; adding one would expand scope unnecessarily. Operators detect via metric; clients detect via `metadata_only=true`. If a future phase needs client-visible stale-DEK signaling, a typed proto field can be added then.

**Rationale for column rename:**

The pre-existing `payload` name is actively misleading — it conflates the column contents (full envelope) with `Event.payload` (one nested field). Renaming clarifies semantics for the cold-path code and for future readers. With no users, the rename is straightforward; PG `RENAME COLUMN` is metadata-only, no row-level work. Alternative considered: keep the misleading name and add a code comment. Rejected: code comments don't propagate to SQL clients and the misnomer continues to confuse anyone reading PG directly.

**Closes `holomush-u5bb` (legacy_id persistence) intent.** The original `holomush-u5bb` description proposed a dedicated `actor_legacy_id TEXT` column plus updates to projection and `actorFromAuditRow`. Decision 5 satisfies the underlying intent — fidelity preservation through the cold tier — via envelope-unmarshal recovering all `Actor` proto fields including `legacy_id`. The originally-proposed dedicated column is NOT needed today: no current consumer queries `events_audit` by `actor_legacy_id`. If a future consumer requires queryable `legacy_id` for SQL-level filtering (e.g., an audit drift detector keyed on plugin name), that becomes a separate follow-up bead — not Phase 3d's concern. `u5bb` closes with this rationale; plan-writing MUST NOT relitigate.

**Properties preserved by full-envelope unmarshal:**

- **INV-21 byte-equality (extended):** `events_audit.envelope == bus envelope bytes`. The cold tier becomes a verbatim mirror at the proto level, not just at the inner-payload level.
- **AAD-binding fidelity:** `aad.Build` produces identical bytes whether called against the hot-tier unmarshaled event or the cold-tier unmarshaled event, because the input bytes are byte-equal AND the keyID/keyVersion values are byte-equal.
- **No tier-leak in dispatcher:** the shared `decodeAuthorizeAndDispatch` receives the same inputs regardless of caller; no signal distinguishing hot from cold. Master spec §3 *"this design adds nothing tier-specific"* remains true and is now structurally enforced by the shared function.
- **Future Actor proto evolution decoupled** (e.g., the `legacy_id` elimination tracked separately per Decision 6): no cold-reader changes required when the proto schema migrates, because cold-read is `proto.Unmarshal` of whatever-bytes-are-there.

**Identity-codec rows** continue to write `envelope` (with `payload` cleartext inside the envelope). Cold reads work mechanically — `codec.Lookup("identity")` is a passthrough; AuthGuard doesn't gate plaintext reads (existing pre-encryption behavior preserved).

**Alternatives considered:**

- *Add a separate `envelope_proto BYTEA NOT NULL` column.* Rejected (Revision 2 → 3 correction): duplicates the existing `payload` column verbatim. Wastes storage and code-path bookkeeping for no benefit.
- *Keep field-by-field column reconstruction for identity rows; envelope-unmarshal only for sensitive rows.* Rejected: two code paths to maintain, identity-row reconstruction has the same proto-evolution rot risk that envelope-unmarshal eliminates.
- *Keep `payload` column name; add code comment.* Rejected: comments don't propagate to SQL tooling; the misnomer continues to confuse.

---

## Decision 6 — `legacy_id` is tech debt; eliminate in a separate epic

**Decision:** The `Actor.legacy_id` field at `api/proto/holomush/eventbus/v1/eventbus.proto:23-30` is technical debt. Plugin actors should be assigned ULIDs at registration time (in the plugin registry), and `Actor.id` should carry a ULID uniformly across all `ActorKind` values. Plugin name becomes a display attribute, not an identity field.

**This elimination is OUT of scope for Phase 3d.** It's a cross-cutting refactor touching at minimum: Actor proto schema, plugin registry persistence, plugin emit paths (`publisher.go:316`, `publisher.go:431`), consumer paths (`subscriber.go:770`, `query_stream_history.go:516`, `server.go:604`, `history/actor_from_envelope.go`, etc.), ABAC policies that key off plugin name, and rendering layer ULID→name lookup.

**Phase 3d is decoupled from `legacy_id` evolution.** Decision 5's envelope-unmarshal cold path is agnostic to Actor proto schema — whatever bytes are in the envelope today round-trip unchanged. When `legacy_id` elimination lands later, Phase 3d's cold-tier read code is unchanged; only the proto schema migration carries the work.

**T9 deliverable:** file a top-level bead `legacy_id` elimination epic (P2 or P3, type=epic, parent=none) with description: "Tech debt: eliminate `Actor.legacy_id` in favor of uniform ULID identity for plugin actors. Assign ULIDs at plugin registration; migrate emit + consumer paths; update ABAC policies; add ULID→name display lookup at the rendering layer. Cross-cutting refactor; its own design + plan + execution cycle."

---

## Decision 7 — Operator runbook: Phase-3-scoped subset of master spec §9.2

**Decision:** Phase 3d MUST land two new operator-audience documents in `site/docs/operating/`:

1. `crypto-setup.md` — initial provisioning per master spec §11.3 checklist items 1-3: `KEKSource` choice, master key provisioning, backup discipline, `events_audit` + `crypto_keys` consistent backup.
2. `crypto-runbook.md` (Phase-3-scoped) — config knobs (`Crypto.Enabled` default), bootstrap fail cases relating to crypto provider initialization, the `crypto.cold_dek_miss` metric (per Decision 5 stale-DEK placeholder).

Deeper runbook content lands with later phases per master spec §11.1 *"docs ship with the code that introduces each capability"*:

- Rekey procedures, AdminReadStream usage, localhost UNIX admin socket → Phase 5.
- KEK rotation, Vault provider migration → Phase 6.
- Plugin-owned audit + INV-50 downgrade fence → Phase 7.

---

## Architecture & components touched

Phase 3d's surface is significantly smaller after Revisions 2/3: no new file in `internal/eventbus/`, no new config knob, no fail-closed bootstrap wiring, no new column on `events_audit`. The work is a column rename, a code refactor, expanded SELECT, and the SDK Sensitive surface plus flag flip.

| Area | File(s) | Change |
| ---- | ------- | ------ |
| `events_audit` schema | `internal/store/migrations/000017_events_audit_envelope_rename.up.sql` (NEW) + `.down.sql` (NEW) | `ALTER TABLE events_audit RENAME COLUMN payload TO envelope`; metadata-only operation |
| Audit projection writer | `internal/eventbus/audit/projection.go` | INSERT bind `envelope` instead of `payload`; same byte source (`msg.Data()`) |
| Cold-tier reader | `internal/eventbus/history/cold_postgres.go` | SELECT add `envelope, codec, dek_ref, dek_version`; replace field-by-field reconstruction with `proto.Unmarshal(row.envelope, &event)` uniformly across identity and sensitive codecs |
| History dispatcher refactor | `internal/eventbus/history/hot_jetstream.go` | Extract `decodeAuthorizeAndDispatch` from existing `decodeAndAuthorizeHistory` per Decision 5 item 4; hot path becomes header-parsing wrapper |
| Test fixture: crypto helper | `test/testutil/crypto.go` | SQL string at `:101` references `payload` — rename to `envelope`. Also rename Go struct field `EventsAuditRow.Payload` → `EventsAuditRow.Envelope` (struct at `:86-91`); update consumer `test/integration/crypto/emit_test.go:183` |
| Test fixture: projection unit | `internal/eventbus/audit/projection_test.go` | SQL at `:156` references `payload` — rename |
| Test fixture: cold reader integration | `internal/eventbus/history/cold_postgres_integration_test.go` | INSERT around `:72` references `payload` — rename |
| Test fixture: cross-tier E2E | `test/integration/eventbus_e2e/cross_tier_query_test.go` | INSERT at `:475` references `payload` — rename |
| Test fixture: store unit | `internal/store/events_audit_test.go` | INSERTs at `:69` and `:104` reference `payload` — rename |
| Plan-time scope verification | T2 plan-writing | Run `rg -n "\bpayload\b" --type=go --type=sql .` against the workspace and reconcile against this table; no surprises permitted before T2 starts |
| Config | `internal/eventbus/config.go` | `Crypto.Enabled` default `true` |
| Config test | `internal/eventbus/config_test.go` | Rename function `TestCryptoEnabledDefaultsToFalse` → `TestCryptoEnabledDefaultsToTrue`; flip assertion |
| Plugin SDK proto | `api/proto/holomush/plugin/v1/plugin.proto` | Add `bool sensitive = N;` to `PluginHostServiceEmitEventRequest`; regen Go + TS bindings |
| Plugin SDK Go (binary) | `internal/plugin/goplugin/host_service.go` (or actual path; verify in plan) | Copy `req.Sensitive` → `EmitIntent.Sensitive` |
| Plugin SDK Lua | Lua emit translator (path verify in plan) | Read `sensitive` boolean key with type checking; default `false`; `LUA_EMIT_SENSITIVE_TYPE` rejection |

**No new packages. No new Go source files in `internal/eventbus/`. One new SQL migration pair.**

**Trust boundary deltas vs Phase 3c:** One narrowing, no widenings.

Plugin SDK `Sensitive` becomes a wire-level claim. With Phase 3a's downgrade fence already in place at the shared `event_emitter.go::Emit` boundary (per `CLAUDE.md` "Plugin Runtime Symmetry" invariant), a plugin claiming `Sensitive=false` for a `manifest=always` event still hits `EVENT_SENSITIVITY_REQUIRED`. The SDK surface change does not widen the attack surface; it lets honest plugins emit honest sensitive events.

---

## Cold-tier read flow (data flow)

```text
Client → gRPC QueryStreamHistory(stream_id, range)
   │
   ▼
History reader (hot first, cold on JS retention miss — F4 crossover)
   │
   ├─[hot tier]──► JS message (headers carry App-Codec, App-Dek-Ref, App-Dek-Version)
   │                │
   │                ▼
   │              proto.Unmarshal(msg.Data()) → *eventbusv1.Event
   │                │
   │                ▼
   │              Parse codecName, keyID, keyVersion from msg.Headers()
   │                │   (existing wrapper around the dispatcher)
   │                ▼
   │
   └─[cold tier]──► PG events_audit row (envelope, codec, dek_ref, dek_version)
                    │
                    ▼
                  proto.Unmarshal(row.envelope) → *eventbusv1.Event
                    │
                    ▼
                  Parse codecName=row.codec, keyID=row.dek_ref, keyVersion=row.dek_version
                    │   (NEW thin wrapper around the dispatcher)
                    ▼
   ◄── Both paths converge on shared header-free dispatcher ────►
                    │
                    ▼
              decodeAuthorizeAndDispatch(
                  ctx, envelope, codecName, keyID, keyVersion,
                  identity, guard, dekMgr, auditEm
              )
                    │
                    ▼
              aad.Build(envelope, codecName, keyID, keyVersion)
                    │
                    ▼
              Codec dispatch (codec.Lookup)
                    │
                    ▼
              AuthGuard.Decide(actor, event, contextID)
                    │
              ┌─────┼──────┬──────────┬──────────────┐
              ▼     ▼      ▼          ▼              ▼
       participant host  operator   deny       (audit failure)
              │     │   (Phase 5)    │              │
              ▼     ▼     ▼          ▼              ▼
          Decrypt Decrypt AdminRS  metadata_only  metadata_only
                                                   + crypto.cold_dek_miss
                                                   metric on stale DEK
                                │
                                ▼
                        Stream Event back to client
```

**Key properties preserved by the shared dispatcher:**

- **INV-21 byte-equality (extended):** `events_audit.envelope == bus envelope bytes`. Cold reader reads the same bytes the projection wrote, which are the same bytes that were on the bus.
- **AAD-binding fidelity:** `aad.Build(envelope, codecName, keyID, keyVersion)` produces identical bytes for hot and cold reads of the same event because all four inputs are byte-equal.
- **No tier-leak:** the shared dispatcher receives identical inputs regardless of caller; no signal distinguishing hot from cold. Master spec §3 *"this design adds nothing tier-specific"* remains true and is structurally enforced.

**Identity-codec rows:** `codec='identity'` rows write the marshaled envelope (with cleartext `Event.payload` nested inside). Cold reads are mechanical — `codec.Lookup("identity")` is a passthrough; AuthGuard doesn't gate plaintext reads (pre-encryption behavior preserved).

---

## Error handling

Phase-3d-specific failure modes (additions to master spec §10):

| Condition | Detection | Response |
| --------- | --------- | -------- |
| Cold-tier row `envelope` fails `proto.Unmarshal` (corruption) | PG reader pre-dispatch | `oops.Code("AUDIT_ENVELOPE_UNMARSHAL_FAILED")` with row id; alert; no fallback path (envelope is sole source of truth in Decision 5) |
| Cold-tier row references missing `crypto_keys` (stale DEK, INV-39 deferred) | DEK lookup miss in `CryptoProvider` | Deliver `metadata_only=true`; metric `crypto.cold_dek_miss`. No wire-level warning header — Event proto has no header bag for cold-path injection. Master spec §8.4 amendment in T9: drop the wire-header claim; metric-only signaling. |
| Lua return-value `sensitive` wrong type | Lua translator type check | Reject with `oops.Code("LUA_EMIT_SENSITIVE_TYPE")`; do NOT coerce |
| Lua return-value `sensitive` absent | Lua translator | Default `false`; manifest fence at `event_emitter.go::Emit` catches `manifest=always` violations (existing Phase 3a fence) |
| Binary plugin proto `sensitive` field absent (older plugin) | Goplugin host service | Default `false` (proto3 zero); same fence applies — no new trust assumption |

**Bootstrap failure modes from Revision 1 retired:** `NATS_DENY_INSTALL_FAILED`, `Crypto.AllowAuditSubscribe=true` validation, deny-install ordering — none of these apply after Decision 4.

---

## Testing approach

**Layer 1 — Unit / package-level (no testcontainers):**

- Lua `sensitive` key reader: table-driven, all type variants, default-false, wrong-type rejection
- Goplugin host service `Sensitive` translation
- `decodeAuthorizeAndDispatch` extracted-function unit tests (table-driven across codec/AuthGuard branches; reproduces the existing hot-path test surface against the new shared function)
- Cold-reader column-mapping test: SELECT result row → dispatcher inputs (envelope unmarshal + codec/dek_ref/dek_version column reads)
- Audit projection writer test: assert `envelope` column matches `msg.Data()` byte-for-byte (existing test updated for column rename)
- `config_test.go` invariant flip + test name flip
- ABAC subscribe-deny coverage (verify in plan; reuse existing test or add new)

**Layer 2 — Component / bus-integration (`eventbustest.New` in-process NATS, no PG):**

- (Existing projection tests cover payload-via-`msg.Data()` byte-equality; column rename means test fixture updates only)

**Layer 3 — E2E integration (full stack, PG testcontainer, `//go:build integration`, Ginkgo BDD):**

Per Decision 3:

- `test/integration/crypto/e2e_test.go` — happy-path narrative covering character-authored AND plugin-authored sensitive events, end-to-end through cold-tier reads.
- `test/integration/crypto/inv49_envelope_roundtrip_test.go` — INV-49 reframed: `envelope` column byte-equality across emit→audit→cold-read→decrypt; both actor kinds.

**Tests dropped from Revision 1:**

- `inv52_nats_deny_test.go` — INV-52 retires per Decision 4.
- Cold-reader column-state validation tests — subsumed by envelope being the source of truth.
- Bootstrap fail-closed deny-install tests — not applicable.

---

## Decomposition & sequencing

**Pre-3d precursor PR (per Decision 1):** `holomush-ojw1.3.22` + `holomush-ojw1.3.23` land as their own PR. One `task pr-prep` cycle. Adversarial `code-reviewer` subagent gates push.

**Phase 3d feature branch — 9 tasks:**

| Task | Scope | Depends on | Parallel-safe with |
| ---- | ----- | ---------- | ------------------ |
| T0 | Plan-time pre-state verification (no code change) | — | — |
| T1 | Migration `000017_events_audit_envelope_rename`: `ALTER TABLE events_audit RENAME COLUMN payload TO envelope` (up + down) | T0 | T3-T5 |
| T2 | Audit projection writer (column rename in INSERT) + cold-reader expanded SELECT + `decodeAuthorizeAndDispatch` extraction in `hot_jetstream.go` + cold-path wrapper that supplies dispatcher inputs from PG columns + unit tests for the shared dispatcher and the cold wrapper | T1 | T3-T5 |
| T3 | SDK proto: `bool sensitive` on `PluginHostServiceEmitEventRequest`; regen Go + TS bindings | T0 | T1-T2 |
| T4 | Goplugin host service translation + unit tests | T3 | T1-T2 |
| T5 | Lua emit translator with type check + unit tests | T3 | T1-T2, T4 |
| T6 | `Crypto.Enabled` default flip + `config_test.go` function rename + assertion flip | T1-T5 | — |
| T7 | E2E BDD (`e2e_test.go`) + targeted invariant proof (`inv49_envelope_roundtrip_test.go`) + ABAC subscribe-deny test (verify existing or add new) | T1-T6 | T8 |
| T8 | Operator docs (`crypto-setup.md` + Phase-3-scoped `crypto-runbook.md`) | T1-T6 | T7 |
| T9 | Bead hygiene + master spec amendments (§7.7 NATS-architecture rewrite; INV-15 reword; INV-21 extension to `envelope`; INV-49 reframe; INV-52 retirement; §11.1 phase 3 row update; §8.4 stale-DEK header-claim drop; §4.7 column-rename note) | T1-T8 | — |

**Critical path properties:**

- T1-T2 (cold-tier infrastructure) || T3-T5 (SDK Sensitive surface) is the natural fan-out. `superpowers:subagent-driven-development` MAY dispatch sub-agents in parallel here per the worktree-isolation support landed in PR #2772.
- T6 is the synchronization barrier: flipping the default before T3-T5 land would crater plugin emits.
- T7 + T8 fan out after T6.
- T9 is closing hygiene.

---

## Bead chain structure

```text
holomush-ojw1                   (existing parent epic — Phase 3 overall)
└── holomush-ojw1.4             (existing child epic — Phase 3d)
    │   • Description updated to link this grounding doc + the impl plan
    │   • Acceptance: all task beads closed; PR merged
    │
    ├── ojw1.4.1                (existing — SDK Sensitive wire surface)
    │   • Splits into ojw1.4.1.1 / .2 / .3 (four-level numbering supported,
    │     e.g. holomush-ojw1.1.1 precedent); commit to split so each
    │     plan-task maps to a bead 1:1
    │   • .1 = T3 (proto), .2 = T4 (goplugin), .3 = T5 (Lua)
    │
    ├── ojw1.4.2                (existing — NATS deny rules)
    │   • CLOSED with rationale: "Architecturally not applicable; game NATS
    │     is server-only by design (Decision 4). NATS-level account scoping
    │     for the holomush server in external mode tracked under
    │     holomush-s5ts. ABAC layer 2 is the authoritative gate."
    │
    ├── ojw1.4.3 (NEW)          — Cold-tier envelope rename + dispatcher refactor (T1+T2)
    │   • Migration + projection writer column update + cold reader
    │     expanded SELECT + envelope-unmarshal + dispatcher extraction
    │   • Closes holomush-u5bb (legacy_id persistence) as a side effect
    │     (verify u5bb full scope is addressed in plan; if any non-crypto
    │     consumer still needs a dedicated column, that becomes a separate
    │     follow-up)
    │
    ├── ojw1.4.4 (NEW)          — Crypto.Enabled default flip (T6)
    ├── ojw1.4.5 (NEW)          — E2E + INV-49 envelope round-trip + ABAC subscribe-deny tests (T7)
    │   • Single bead covering all three test concerns (1:1 review surface
    │     for crypto E2E coverage)
    └── ojw1.4.6 (NEW)          — Operator docs (T8)
```

T0 (pre-state verification) and T9 (bead hygiene + spec amendments) are plan-housekeeping and DO NOT receive their own beads — they live in the impl plan's checklist.

Plus `bd dep add` edges matching the dependency column in the task table.

**Each task bead's description MUST include the following sections:**

| Section | Required content |
| ------- | ---------------- |
| **Goal** | One-sentence scope |
| **Design reference** | Link to this grounding doc (with section anchor for the relevant Decision) + the master spec at `2026-04-25-event-payload-crypto-design.md` (with §-anchors) |
| **Plan reference** | Link to `docs/superpowers/plans/2026-05-03-event-payload-crypto-phase3d.md` (the impl plan) with the task ID (T-N) anchor |
| **TDD acceptance criteria** | Named tests that MUST exist before the task is "done." For tests-as-deliverable beads (`ojw1.4.5`), the test IS the criterion. |
| **Verification steps** | Concrete commands: `task lint`, `task test -- ./<package>/`, `task test:int -- -run <pattern>`, `task pr-prep` for the closing bead |
| **Files touched** | Explicit list per the architecture table; reviewers verify no surprises |
| **Dependencies** | `bd dep add` edges matching the task graph |
| **Out of scope** | Explicit non-goals (INV-39, INV-50, lifecycle ops, NATS deny rules, `legacy_id` elimination) so reviewers don't ask "where's X?" |

---

## Master spec edits required

Master spec at `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` MUST be edited as part of T9 (bead hygiene + spec amendments). These are normative changes:

1. **§7.7 (Audit-stream isolation): substantial rewrite.** Replace the current "two-layer NATS-account-deny + ABAC" framing with the architectural realization from Decision 4. Verbatim replacement text locked in **Appendix A** of this grounding doc — plan-writing MUST apply it as written, not re-author from intent.
2. **INV-15 reword:** "ABAC denies subscribe to `audit.>` for `kind={plugin|character}` subjects" — drop the NATS-level deny clause; INV-15 is now ABAC-only.
3. **INV-52 retirement.** Remove from the invariants list with a one-line note pointing to the §7.7 rewrite.
4. **INV-21 extension:** byte-equality applies to `events_audit.envelope == bus envelope bytes`. Strengthen the existing INV-21 phrasing and update the column name from `payload` to `envelope`.
5. **INV-49 reframe:** the original "header-to-column round-trip" becomes "envelope byte-equality across emit→audit→cold-read." The forward direction (header→column) and reverse direction (column→header) collapse into a single envelope round-trip invariant.
6. **§4.7 (events_audit migration):** add a note that `payload` was renamed to `envelope` in migration `000017` to clarify pre-existing semantics; the column always carried the marshaled envelope bytes.
7. **§8.4 (stale-DEK fallback):** drop the wire-level header claim; metric-only signaling for the cold-itself-can't-decrypt terminal branch. INV-39 (hot→cold fallback loop) remains deferred to Phase 5.
8. **§11.1 phase 3 row:** mark phase 3 status complete; reference this grounding doc; remove the "NATS account-level deny rules" entry from the row.
9. **§11.4 rollback:** add note that no users / no deployments → rollback is Git revert; the operator-grade rollback discussion stays for future deployments.

---

## Bead updates required

T9 (bead hygiene) MUST:

1. **Promote `ojw1.3.22` and `ojw1.3.23`** P3 → P1; update parent linkage to the precursor PR landing.
2. **Update `holomush-ojw1.4`** description to link this grounding doc + the impl plan; refine acceptance criteria.
3. **Close `holomush-ojw1.4.2`** with rationale per Decision 4.
4. **Create new beads** `ojw1.4.3`, `ojw1.4.4`, `ojw1.4.5`, `ojw1.4.6` (and `ojw1.4.1.{1,2,3}` per the four-level split commitment) per the chain structure above, each with the required description sections.
5. **Add `bd dep add` edges** matching the task table dependencies.
6. **Close `holomush-u5bb`** (legacy_id persistence) as resolved-as-side-effect-of-`ojw1.4.3`. Plan-writing MUST verify u5bb's full description scope is addressed; if any non-crypto consumer still needs a dedicated column for `actor_legacy_id` lookup, file as a separate follow-up.
7. **File new follow-up beads:**
   - INV-39 stale-DEK fallback (P2, parent `holomush-ojw1`, references master spec §8.4 + Phase 5 Rekey setup)
   - `legacy_id` elimination tech-debt epic (top-level, type=epic, P2 or P3, see Decision 6)
   - Client-visible stale-DEK signaling (P3, parent `holomush-ojw1`): typed Event proto field or distinct `EventFrame` status to distinguish stale-DEK from non-participant denial. Decision 5 retired the wire-header claim in favor of `metadata_only=true` + `crypto.cold_dek_miss` metric, which conflates stale-DEK with other metadata-only causes. Acceptable for Phase 3d but file as future-debt so the diagnostic gap is owned.
8. **Update `holomush-s5ts` description:** drop the deny-rules scope; add the server-account scoping (`holomush-server` account allow-listed for game topics) and optional read-only operator account scope.

---

## Out of scope

The following are NOT addressed in Phase 3d and remain deferred to their cited phases:

- INV-39 stale-DEK fallback (deferred to Phase 5 / Rekey-prep follow-up)
- INV-50 plugin-owned audit downgrade fence (master spec §8.2 — Phase 7)
- Lifecycle ops `Add` / `Rotate` / `Rekey` (Phase 4 / 5)
- `OperatorAuthProvider` / `AdminReadStream` / localhost UNIX admin socket (Phase 5)
- `VaultTransitProvider` / KEK rotation / provider migration CLI (Phase 6)
- Plugin SDK helpers (`pkg/plugin/audit.go`) and plugin-owned audit table integration (Phase 7)
- Right-to-be-forgotten beyond `Rekey` — two-tier content-addressed model (master spec §12 Q3, future)
- External-NATS deploy: server-account scoping + lifecycle pivot (`holomush-s5ts`, P3)
- NATS-level deny rules of any kind (architecturally not applicable; see Decision 4)
- `legacy_id` elimination tech-debt epic (Decision 6 — its own design + plan + execution cycle)
- Site documentation deliverables (Section 9 of master spec) beyond the Phase-3-scoped subset called out in Decision 7 (Phase 8)
- Client-visible stale-DEK signaling (Decision 5 retires the wire-header claim; metric-only for now)

---

## References

- Master spec: [`2026-04-25-event-payload-crypto-design.md`](2026-04-25-event-payload-crypto-design.md)
- Phase 1 manifest grammar: [`2026-04-25-event-payload-crypto-phase1-manifest-grammar.md`](2026-04-25-event-payload-crypto-phase1-manifest-grammar.md)
- Phase 2 substrate design: [`2026-04-30-event-payload-crypto-phase2-substrate-design.md`](2026-04-30-event-payload-crypto-phase2-substrate-design.md)
- Phase 3a substrate grounding: [`2026-05-02-event-payload-crypto-phase3a-grounding.md`](2026-05-02-event-payload-crypto-phase3a-grounding.md)
- Phase 3a impl plan: [`../plans/2026-05-02-event-payload-crypto-phase3a-codec-emit.md`](../plans/2026-05-02-event-payload-crypto-phase3a-codec-emit.md)
- Phase 3d impl plan (forthcoming): `../plans/2026-05-03-event-payload-crypto-phase3d.md`
- External-NATS deploy epic: `holomush-s5ts`
- `legacy_id` elimination epic: filed as part of T9 hygiene

---

## Appendix A — Verbatim master-spec §7.7 replacement text

T9 MUST replace the current §7.7 *"Audit-stream isolation"* in the master spec at [`2026-04-25-event-payload-crypto-design.md`](2026-04-25-event-payload-crypto-design.md) with the following verbatim text. Plan-writing MUST NOT re-author from intent; this appendix is the source of truth for the §7.7 amendment.

---

**§7.7 Audit-stream isolation.**

Game-topic NATS subjects (`events.>`, `audit.>`, `internal.>`) are single-principal by architectural design: only the holomush server connects on these subjects in any planned topology. Plugins emit via gRPC `PluginHostService` and receive via host-mediated gRPC streams; characters' subscriptions are server-internal multiplexing through the server's NATS connection. There is no plugin or character NATS account, in embedded or external mode. Plugins MAY use NATS separately for their own purposes (their own subjects, their own clusters, their own credentials), but that is a plugin-internal concern with no game-topic surface.

**ABAC is the authoritative isolation gate.** The default ABAC policy MUST deny `subject={kind: plugin|character}, action: subscribe, resource: subject:audit.>` (and `subject:internal.>`). INV-15 verifies this denial at the gRPC subscribe handler boundary.

**Defense-in-depth is at the architectural level, not the substrate level.** The absence of plugin/character NATS principals is a structural property of HoloMUSH's gRPC-mediated plugin model. Removing the gRPC mediation between plugins/characters and NATS would require a deliberate architectural change subject to its own design review.

**NATS-level deny rules do not apply.** They have no target principal in any planned topology. Earlier drafts of this section (and INV-52) presumed an architecture where plugins or characters connected to NATS directly; that assumption was incorrect for HoloMUSH. INV-52 retires; cite this section as the architectural property it would have asserted.

**External NATS deploy.** When the server connects to an external NATS cluster (tracked under `holomush-s5ts`), it authenticates as a single account scoped to game topics:

```text
Account "holomush-server":
  publish:   events.>, audit.>, internal.>
  subscribe: events.>, audit.>, internal.>
```

Other accounts in the cluster have no publish or subscribe permission on these subjects by default — enforced at the cluster admin layer, not inside the server.

**Operator audit query.** A future operator-read account (`holomush-operator-read`, subscribe `events.>` only — NOT `audit.>` or `internal.>`) MAY be added under `holomush-s5ts` for monitoring and debugging use cases. Audit-table reads remain the localhost UNIX admin socket path (Phase 5), not NATS subscribe.

---
