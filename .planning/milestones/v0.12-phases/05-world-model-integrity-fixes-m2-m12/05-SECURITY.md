---
phase: "05 — World-Model Integrity Fixes (MODEL-02/03/04)"
asvs_level: 1
block_on: high
threats_total: 63
threats_closed: 63
threats_open: 0
unregistered_flags: 0
verdict: SECURED
audited_at: 2026-07-13
audited_by: gsd-security-auditor
---

# Phase 05 Security Verification — World-Model Integrity Fixes

Retroactive threat-mitigation audit. Every declared mitigation was verified
against implemented code at `path:line` — documentation/intent was not accepted
as evidence. Verification depth is ASVS L1 (mitigation present at the cited
boundary), with L2-style boundary checks applied to the high-severity CAS,
outbox-atomicity, relay-ordering, and genesis/reaping-fence threats.

**Result: SECURED.** 63/63 threats CLOSED (59 mitigate verified in code, 4 accept
documented below). `threats_open = 0` — no high-severity (or any) mitigation is
absent. No unregistered attack surface: all 9 SUMMARY `## Threat Flags` report
"None beyond the plan's `<threat_model>`".

## High-Severity Threat Verification (33 — the block_on≥high gate)

| Threat ID | Category | Sev | Evidence (path:line) |
|-----------|----------|-----|----------------------|
| T-05-04 | Tampering | high | `internal/world/postgres/location_repo.go:90,97` version CAS `version=version+1 … AND version=$9`; classifier `helpers.go:81` |
| T-05-06 | Tampering | high | `internal/world/postgres/helpers.go:46` `execerFromCtx`; writes enroll via `withTx`+tx (`location_repo.go:103`); no escaping `r.pool.Exec` write in world/postgres |
| T-05-60 | Tampering | high | `internal/world/postgres/location_repo.go:155` parent `SELECT version … FOR UPDATE` FIRST, then `preselectCascadedExits` (both FK dirs) `:198`, then delete `:178` |
| T-05-07 | Tampering | high | `internal/world/postgres/character_repo.go:79,83` CAS; `object_repo.go:314` Move CAS; classifier `helpers.go:81` |
| T-05-08 | Tampering | high | `internal/world/postgres/object_repo.go:~312` Move via `withTx`+`tx.Exec` (ambient-tx enrolled), row locked FOR UPDATE |
| T-05-10 | Tampering | high | `internal/world/service.go:878,1028` read `char.Version` threaded into `updateCharacterPreferences`/`moveCharacter`; mutator `mutator.go:228,249` `expectedVersion` param |
| T-05-12 | Tampering | high | `internal/world/postgres/outbox_store.go:61` `execerFromCtx`; `feed_counter.go:65` fails closed (`WORLD_FEED_ALLOCATE_NO_TX`) without ambient tx |
| T-05-13 | Tampering | high | `internal/world/postgres/feed_counter.go:92` `SELECT next_position,epoch … FOR UPDATE`; migration `000050_world_outbox.up.sql:78` `UNIQUE(game_id,epoch,feed_position)` |
| T-05-16 | Tampering | high | `internal/world/mutator.go:197` `mutate(ctx, intent, write)` — both params non-optional; write repos private fields; `:208` `WriteIntent` in same `InTransaction` |
| T-05-19 | Tampering | high | `internal/world/outbox/relay.go:197` `Drain` strict `(epoch,feed_position)` order; `mu` held across publish `:82`; halt-on-poison `:252`, never advances past halt |
| T-05-20 | DoS | high | `internal/world/outbox/relay.go:325` `halt()` raises `relayHalts`+`relayHaltPosition`; `Halted()` probe `:116` |
| T-05-23 | Tampering | high | entire `internal/world/outbox/*.go` imports only `internal/eventbus`+`internal/world/wmodel` — no `internal/access`/crypto; `test/meta/world_import_graph_test.go:37` forbidden-edge guard |
| T-05-41 | Tampering | high | `internal/world/outbox/relay.go:306` `MarkPublished(…, Generation())`; `postgres/outbox_lease.go:188` fences vs stored `lease_generation` |
| T-05-42 | DoS | high | `internal/world/outbox/skip.go:54` `SkipService.Skip` owns store+publisher, publishes marker + resolves via lease; CLI `cmd/holomush/world_genesis.go`/outbox-skip |
| T-05-43 | Tampering | high | `internal/world/outbox/skip.go:145` marker at poison's own `FeedPosition`; `:114` `MarkSkipResolved` only after PubAck; `:78` position-mismatch refused |
| T-05-49 | Tampering | high | `internal/world/outbox/skip.go:87-96` stable `SkipMarkerID`/`PersistSkipMarkerID` BEFORE publish, reused on retry; column `000050_world_outbox.up.sql:76` |
| T-05-24 | DoS | high | `internal/world/outbox/relay.go:259` transient-outage path resumes in order; `sleepWithBackoff`/`backoffDelay` `:347,362` |
| T-05-25 | Tampering | high | `internal/world/postgres/outbox_lease.go:66` `AcquireLease` pins dedicated conn + bumps generation; `MarkPublished` `:188` generation-fenced ack rejection |
| T-05-27 | Tampering | high | `test/meta/world_sql_fence_test.go:188` AST scan of prod Go+migrations; firing negative fixture `:256`; allowlist = `internal/world/postgres` only |
| T-05-52 | Repudiation | high | `test/meta/world_sql_fence_test.go:62` fenced-table set (no scene_participants); `:293` scene_participants-not-flagged fixture; char_settings fold-in documented `:44-47` |
| T-05-32 | Tampering | high | `test/meta/world_envelope_census_test.go:64` bijection no-allowlist; `:187` go/ast `s.mutator` mutating-method cross-check |
| T-05-34 | EoP | high | `internal/auth/character_genesis.go:162` `Create` insert+binding+envelope in one `InTransaction`; all 3 paths route through it (`character_service.go:152`, `guest_service.go:151`, `bootstrap/admin.go:96`) |
| T-05-59 | Tampering | high | `internal/auth/character_reaping.go:239` `reapCharacter` per-char tombstone tx before player delete; census producer `world_envelope_census_test.go:324` |
| T-05-35 | Repudiation | high | `test/meta/invariant_registry_test.go` `TestBoundInvariantsAreGenuinelyAsserted`; INV-WORLD-2 DELTA-PARITY `invariants.yaml:4908` asserts manifest EQUALS delta |
| T-05-38 | Tampering | high | `internal/world/mutator.go:203` re-entrant `InTransaction` seam; writes via `execerFromCtx`; no surviving `r.pool.Begin/Exec` world write |
| T-05-39 | Repudiation | high | `internal/world/postgres/helpers.go:114` `withTx` — `if txFromContext(ctx)!=nil { return fn(ctx) }` reuses ambient tx (no second Begin) |
| T-05-45 | Tampering | high | `internal/auth/guest_service.go:151` `s.genesis.Create(…"initial_bind_guest")`; `mocks/mock_GuestCharacterRepository.go` has NO Create (compile fence) |
| T-05-46 | EoP | high | `internal/bootstrap/admin.go:96` `CharService.Create` genesis-backed; role assigned after via `RoleStore.AddRole` `:100` (ordered) |
| T-05-47 | Tampering | high | `internal/auth/mocks/mock_CharacterRepository.go` no Create + composition allowlist `world_import_graph_test.go:73` + census descriptor |
| T-05-54 | Tampering | high | `internal/auth/character_reaping.go:216-228` per-char guarded `Delete`+tombstone envelope, THEN player delete step 4 |
| T-05-55 | Tampering | high | `internal/auth/guest_service.go:207` `cleanupGuestPlayer` → `s.cleaner.DeleteGuestPlayer` (reaping service) |
| T-05-61 | Tampering | high | reaper `MarkReaping` first `character_reaping.go:198`; genesis guard `postgres/reaping_guard.go:64` `SELECT reaping_at … FOR UPDATE`; migration `000051_player_reaping.up.sql:18` |
| T-05-63 | Repudiation | high | `internal/world/postgres/character_repo.go:181` `ListByPlayer` SELECTs `version`; reaping uses `char.Version` in guarded Delete `character_reaping.go:263` |

## Medium/Low Mitigate Threat Verification (26)

| Threat ID | Category | Sev | Evidence (path:line) |
|-----------|----------|-----|----------------------|
| T-05-01 | DoS | low | `000049_world_version_guard.up.sql:18-21` `ADD COLUMN IF NOT EXISTS … NOT NULL DEFAULT 1` |
| T-05-02 | Tampering | med | same migration — every row carries `version`; CAS lands in 05-02/03 |
| T-05-05 | Repudiation | med | `internal/world/postgres/helpers.go:81` `classifyCASZeroRow` two-outcome (conflict/not-found) |
| T-05-09 | Repudiation | med | `helpers.go:81` classifier reused across character/object CAS |
| T-05-14 | Spoofing | med | `internal/world/wmodel/envelope.go:66` `NewEnvelopeIntent` mints `core.NewULID()`; `000050:49` event_id PK/dedup |
| T-05-15 | Info Disc | low | `wmodel/envelope.go:40` intent-level new-values-only payload (erasure-safe) |
| T-05-17 | DoS | med | `emitWithRetry` ABSENT in production (deleted) |
| T-05-18 | Repudiation | low | M2 mechanism doc corrected in same change (plan 05-06) |
| T-05-58 | Repudiation | med | `internal/world/movement_hook.go:23` logs+metric+RETURNS SUCCESS (post-commit failure ≠ command failure) |
| T-05-21 | Spoofing | med | `internal/world/outbox/consumer.go:51` `ApplyOnce` durable `world_consumer_receipts` dedup; `000050:112` |
| T-05-22 | EoP | med | outbox package imports no `internal/access`; publishes already-authorized facts |
| T-05-26 | Spoofing | med | consumer `ApplyOnce` receipt dedup on event ULID (`consumer.go:47`) |
| T-05-28 | Tampering | med | `internal/world/outbox/taxonomy.go:19` `AppSchemaVersion=1`; `IsDeclared`/`Kinds` registry `:181,187` |
| T-05-29 | Tampering | med | `mutator.go:208` exactly one `WriteIntent` per `mutate()`; census proves no un-migrated command |
| T-05-30 | Tampering | med | `wmodel/envelope.go:126,133` `manifestFromDelta` builds manifest from delta (not inputs); INV-WORLD-2 |
| T-05-31 | Info Disc | low | intent-level new-values-only payloads |
| T-05-33 | Tampering | med | `000050:102-108` `world_genesis_checkpoint` PK `(game_id,epoch,aggregate_type,aggregate_id)`; genesis_store checkpoint-idempotent |
| T-05-36 | Tampering | low | `inv-render` generate-and-diff CI guard (`invariants.md` regenerated) |
| T-05-37 | Repudiation | low | `test/meta/world_model_doc_claim_test.go:80,120` doc-claim + legitimate-catch-up-preserved tests |
| T-05-40 | Tampering | med | `wmodel/envelope.go:112` `Finalize` manifest from `MutationDelta`; INV-WORLD-2 binding |
| T-05-44 | Tampering | med | `000050:92` `epoch` column; `genesis_store.go` epoch advance seeds `next_position` at 1 |
| T-05-50 | Tampering | med | `world_import_graph_test.go:50` forbidden edge `{outbox, postgres}` |
| T-05-51 | DoS | med | `cmd/holomush/world_genesis.go:36,58` `world genesis` / `epoch-reset` CLI operator entry |
| T-05-53 | Repudiation | med | `internal/bootstrap/admin.go:83-93` narrowed atomicity (char+envelope), role after, orphan compensation documented |
| T-05-56 | Repudiation | med | `internal/auth/character_reaping.go:108-116` two-pool boundary documented; characters tombstoned+deleted first, then player |
| T-05-62 | Tampering | med | `internal/auth/character_reaping.go:258` `props.DeleteByParent("character",id)` before delete (cascade parity) |

## Accepted Risks Log (4 — disposition=accept)

Each acceptance was verified as a deliberate, documented decision in the plan
`<threat_model>` text, not an unmitigated gap. For T-05-48 the fail-closed
behavior was additionally confirmed in code.

| Threat ID | Sev | Acceptance rationale (verified) |
|-----------|-----|---------------------------------|
| T-05-03 | low | Conflict code `WORLD_CONCURRENT_EDIT` is intentionally generic (no row contents); `.With("id",…)` is server-side context, not wire payload. `internal/world/errors.go:16-26`. |
| T-05-11 | low | `WORLD_CONCURRENT_EDIT` propagates unchanged so callers distinguish conflict from not-found; UX mapping deferred (D-02). Integrity signal exists. |
| T-05-48 | med | Fail-closed character creation when genesis deps are missing is deliberate — envelope-less degraded mode is the exact drift Phase 5 closes. Confirmed: `internal/auth/character_genesis.go:114-135` constructor rejects nil deps (typed error at first creation attempt). |
| T-05-57 | low | Reaper halting on a `WORLD_CONCURRENT_EDIT` is retriable + resumable: per-character txs (`character_reaping.go:239`) let earlier tombstones survive; the character is never deleted without a tombstone. |

## Unregistered Flags

None. All 9 SUMMARY files carrying a `## Threat Flags` section report "None
beyond the plan's `<threat_model>`" and explicitly state no new network
endpoint, auth path, or trust-boundary schema change; relay/genesis/reaping
publish already-authorized, already-committed facts and import no
`internal/access` or crypto surface (verified independently against the
`internal/world/outbox` import set).

## Notes on the recent relay code-review fix pass

Commits `20c32ab14` / `58fa04f89` hardened `relay.go` (dead-LISTEN-conn
busy-loop CR-01; publish-path lock/backoff WR-01/WR-02). The ordering
mitigations still hold in the current code: `mu` is held across the network
publish inside `Drain` to preserve strict `(epoch, feed_position)` order and
one-event-per-position (`relay.go:81-84`), the halt-state signal moved to a
separate `haltMu` so `Halted()` never blocks behind an in-flight publish
(`:87-94`), and `MarkPublished` remains generation-fenced (`:306`). No
ordering/one-event-per-feed-position regression introduced.
