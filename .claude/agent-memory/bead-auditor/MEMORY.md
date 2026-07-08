# bead-auditor — accumulated audit memory

Concise notes about HoloMUSH-specific patterns the auditor has hit before.
Curate; keep under 200 lines. Stale entries should be deleted, not left
to drift.

## False-fix offenders (verified 2026-04-26)

These are beads where a sub-fix bead was closed and an in-bead `Closed:`
or `Fixed:` comment was added, but the actual code change never landed.
They prove the rule: in-bead closure comments are hypotheses, not
evidence.

- **holomush-wfza.21** — Closure claimed `infra:session-invalid` prefix
  was changed to `deny:session-invalid`. Code at
  `internal/access/policy/engine.go` still uses `infra:session-invalid`;
  `IsInfraFailure()` in `types.go` still does
  `strings.HasPrefix(d.policyID, "infra:")`. Sub-fix bead `wfza.26` was
  closed but the code change never landed.
- **holomush-wfza.62** — Closure claimed `ModeMinimal` and
  `ModeDenialsOnly` were differentiated. The two `case` bodies in
  `internal/audit/logger.go` `shouldLog` are byte-identical (both log
  `{EffectDeny, EffectDefaultDeny, EffectSystemBypass}`). Sub-fix bead
  `wfza.69` was closed.

## Architectural supersession themes

When auditing post-2026-04-26, treat any bead targeting these systems as
candidates for `MOOT` closure (with a `path:line` or `rg → 0 hits`
verification):

- **eventStore + LISTEN/NOTIFY + Broadcaster + cursors** → JetStream
  durable consumers. PR #252 merged 2026-04-21. Triggering symbols (now
  gone): `EventWriter`, `pgnotify`, `Broadcaster`, `EventCursors`,
  `cursor_lock`, `persistCursorAsync`, `pglistener`. The pre-cutover
  spec `docs/specs/2026-03-20-event-delivery-redesign.md` carries an
  explicit `**SUPERSEDED**` header.
- **WatchSession control plane** → `session_ended` events on
  `character:{ID}` streams. PR #233 merged 2026-04-19. Triggering
  symbols: `WatchSession`, `cursorLocks`, `Get+Watch under same mu`.
- **StaticAccessControl** → `AccessPolicyEngine` (Epic 7). All Epic 3
  (`holomush-ql5.*`) sub-tasks are MOOT. `rg "StaticAccessControl"` →
  0 hits.
- **WASM/Extism plugin framework** → `gopher-lua` + `hashicorp/go-plugin`.
  Epic 1.6 (`holomush-qmt`) and child WASM beads under
  `holomush-1hq.21`/`23` already removed remnants. `rg "wasm|extism|wazero"`
  hits only test fixtures.
- **Capability enforcer** → ABAC engine in hostfunc. PR #106 merged
  2026-03-15. `rg "capability.Enforcer"` → 0 hits in production code.
  `Manifest.Capabilities` is deprecated with a warning emit.
- **Phase 7.5 Locks & Admin** → DEFERRED per Decision #96; no
  replacement epic exists. All `holomush-5k1.7.*` and `holomush-5k1.48`
  /`holomush-5k1.600.4` are superseded by the design choice not to
  build them.

## High-yield queries (cheap, run early)

```
# Title duplicates (byte-exact)
bd list --status open --json | jq -r '.[].title' | sort | uniq -d

# Beads with in-bead Closed: comments still open (slow — only 24 hits in 2026-04-26)
jq -r '.[].id' /tmp/bead-audit/open.json | xargs -I {} sh -c '
  bd show {} 2>/dev/null | rg -l "Closed: " >/dev/null && echo {}
'

# Closed parent epics with open children
jq -r '.[].id' /tmp/bead-audit/open.json |
  sed -E 's/^(holomush-[a-z0-9]+)\..*/\1/' |
  sort -u |
  while read p; do
    bd show "$p" 2>&1 | head -1 | grep -q "✓" && {
      n=$(jq -r --arg p "$p" '.[] | select(.id|startswith($p+".")) | .id' /tmp/bead-audit/open.json | wc -l)
      [ "$n" -gt 0 ] && echo "$p: $n open children"
    }
  done
```

## Operational notes

- **`.beads/` lives in main repo, not worktrees.** Running `bd show`
  from a worktree cwd emits `no beads database found`. Always `cd` to
  the main repo root (resolve via `git rev-parse --path-format=absolute
  --git-common-dir`; its parent directory is the main checkout).
- **Closing children before parents.** `bd close <epic>` errors if any
  child is still open. Either close children first, or use `--force` on
  the epic (only after explicitly auditing why children remain open).
- **`bd close` reasons are public-facing.** Treat the `-r` argument as a
  comment that another contributor will read in a year. Cite
  `path:line`, the verifying detail, and "Closed via YYYY-MM-DD audit"
  for traceability.

## holomush-1tvn JetStream audit (2026-05-16, complete)

16 CLOSE, 1 KEEP. All M1-M7 + F1-F9 Phase A/B tasks shipped in PR #252 (2026-04-21);
bead chain was never closed (pure tracking-debt). Key verifications:
- `internal/eventbus/` full package: types.go, bus.go, errors.go, subsystem.go, codec/, audit/, history/, telemetry/ all present.
- `cursor_lock.go`, `replay.go`, `event_writer.go` all GONE; `rg EventWriter` → 0 production hits.
- `internal/store/migrations/000009` (create events_audit) + `000010` (drop events+cursors) present.
- E2E suite: `test/integration/eventbus_e2e/` 14 files + `.github/workflows/nightly-soak.yml`.
- Plugin EventType migration confirmed: `plugins/core-communication/events.go`, `plugins/core-objects/events.go`.
- CLAUDE.md has 0 hits for EventWriter/pgnotify; prior spec SUPERSEDED-tagged; event-store.md exists.
- **KEEP: 1tvn.17** — DLQ TODOs still live at `projection.go:74` and `subsystem.go:59`. No EVENTS_AUDIT_DLQ stream exists. Real operational gap.

## qve.16 session persistence (verified 2026-05-16)
8 CLOSE, 1 KEEP. `internal/session/` + `internal/store/session_store.go` fully present.
**KEEP: qve.16.9** — session_persistence_suite_test.go has zero Describe/It blocks.

## Plugin architecture migration (verified 2026-05-15)

`who`, `ooc`, `pemit`, `home`, `teleport`, `examine` and other system commands
were migrated from compiled-in Go handlers (`internal/command/handlers/*.go`) to
Lua plugins in the "Plugin Architecture Phase 2" effort. `internal/command/handlers/`
now contains ONLY: `quit`, `shutdown`, `output`, `register`, `plugin_admin`,
`resetpassword`. Any bead targeting `handlers/who.go` (or `WhoHandler`) is MOOT
by architectural supersession — verify with `ls internal/command/handlers/`.

## GetCharactersByLocation prefix design (verified 2026-05-15)

`world.Service.GetCharactersByLocation` intentionally uses `prefixLocation` with
`checkAccess`, producing `LOCATION_*` error codes. This is documented in ADR #76
(Compound Resource Decomposition, `service.go:671`). Findings claiming this is a
bug should be closed as design misread.

## RoleResolver interface evolution (verified 2026-05-15)

`RoleResolver.GetRole() string` was refactored to `GetRoles() []string`. Any bead
discussing "empty string for a key that exists" in RoleResolver is moot — the
return type changed. Current tests in `character_test.go:229` cover nil/empty slice.

## e55 Phase 1.5 audit (2026-05-15, complete)

All 49 open children audited across two sessions. All are CLOSE. Key patterns:
- `internal/control/socket.go` never existed in final form — design evolved to `grpc_server.go` (gRPC control with mTLS). e55.20 beads referencing Unix socket are moot by design evolution.
- Coverage beads (e55.28–38): all gaps filled; `internal/grpc/` and `internal/control/` have extensive test suites.
- A/B/C/D/E/F series (e55.12–19): all Phase 1.5 implementation tasks shipped; XDG, logging, TLS, proto, CLI, gRPC server all present.
- Bug beads (e55.47–49): gateway.go line 247 rewritten; runGateway() covered by DI injection; LoadControlServerTLS tests present.

## hqi6 PR #106 audit (2026-05-15, complete)

All 26 open children audited. 23 CLOSE, 3 KEEP. Key patterns:
- "Breaking change" beads (hqi6.14/17/19/24): all internal packages, pre-deployment; close as moot.
- Duplicate clusters: hqi6.5≡hqi6.1 (hadError); hqi6.6/15/23 (nil engine fail-open); hqi6.7/8/10 (LIKE escape); hqi6.3≡hqi6.18 (registry unused).
- `capabilities → policies` migration warning IS implemented at manifest.go:361-364 (produces clear error).
- `PluginProvider` two-phase init: `NewPluginProvider(nil)` at startup, `SetRegistry` called post-Manager; registered at setup.go:133-136.
- hqi6.11 (loadPlugin policy install rollback test): still open; duplicate of g8o9.7; jsna may cover.
- hqi6.13 (checkKVAccess engine error path untest): still open; commands path tested but KV not.
- hqi6.25 (golangci forbidigo for AccessControl.Check): still open; .golangci.yaml unchanged.

## gqjb audit (2026-05-15, complete)

All 24 open children of `holomush-gqjb` (PR #106 capability.Enforcer removal) audited. 21 CLOSE, 3 KEEP.
Key patterns:
- KV ABAC enforcement landed: `checkKVAccess` at `functions.go:277-314`; deny tests at `functions_test.go:579-688`.
- `get_command_help` now engine-gated: `commands.go:246-281`; `TestGetCommandHelpAccessDenied` present.
- `Manifest.Capabilities` triggers hard validation error at `manifest.go:361-363` (not silent dead data). (Cross-confirmed hqi6 finding.)
- `PluginSubject()` prefix is `plugin:` NOT `system:plugin:` — gqjb.19 was a factual misread.
- gqjb.11 is NOT duplicate of `holomush-jsna` (jsna = unit engine-error branch; gqjb.11 = integration fixture wiring gap).
- gqjb.10/13 = spec compliance gaps (T28.5 ADR waiver; S8 CI check). No ADR filed as of 2026-05-15.

## c3ww PR #88 audit (2026-05-15, complete)

All 82 open children audited. All are CLOSE. 14 beads targeting `who.go`/`WhoHandler` MOOT
by Lua migration. 7 beads targeting `*_equivalence_test.go` MOOT — files never existed.
All code fixes landed. Zero false-fix entries.

## holomush-5k1 batch 01 audit (2026-05-15, .18–.47, complete)

All 30 open children audited. All 30 CLOSE. Key patterns:
- Every bead is a plan/spec review finding filed 2026-02-06/07 before implementation shipped.
- Plan `docs/plans/2026-02-06-full-abac-implementation.md` is now a historical artifact.
- Verified shipped: `access_audit_log`, `access_policies`, `entity_properties` tables in migrations;
  `internal/access/policy/` full package (engine, compiler, DSL, seed, metrics, audit);
  `PropertyRepository.DeleteByParent` at `service.go:603`; `abac_degraded_mode` gauge at `metrics.go:31`;
  sync denial writes + WAL fallback at `logger.go:155-197`; Decision #59 file exists.
- In 8 cases the code shipped correctly despite plan errors (.26, .34, .39, .40, .41, .42, .45, .47).
- Zero false-fix entries.

## holomush-5k1 ABAC Epic 7 audit (2026-05-15, batches 0, 3, 5 of 23)

30 beads audited: 11 CLOSE, 19 KEEP. Key patterns:
- **Phase epic containers (5k1.3/4/5/6/8) are tracking-debt** — implementation shipped (StaticAccessControl → 0 hits, full engine in internal/access/policy/) but plan-epoch bead chain never formally closed. Child task beads (5k1.3.x, 5k1.4.x, 5k1.5.x, 5k1.6.x, 5k1.8.x) need a fast-batch audit — most should CLOSE.
- **Audit logger sync/async dispatch**: `internal/audit/logger.go:Log()` calls `WriteSync` for deny/default_deny/system_bypass; `WriteAsync` for allows. WAL fallback present. `5k1.13` CLOSED.
- **Seed versioning implemented**: `bootstrap.go:90` uses `SeedVersion` field with upgrade path and `SkipSeedMigrations` option. `3c61` CLOSED.
- **Attribute LRU cache**: `attribute/cache.go:newAttributeCache(100)` — capacity=100, eviction at lines 82-91. `2i6c` CLOSED.
- **Session circuit breaker auto-invalidation gone**: no `ErrCodeSessionInvalidatedByCircuitBreaker` in codebase; engine session path (engine.go:184-228) has no session-kill behavior. `3kdz` CLOSED.
- **ADR 0016 moved** to `docs/adr/holomush-5z2y-postgresql-listennotify-policy-cache-invalidation.md`; fire-and-forget in Context section. `0v6c` CLOSED.
- **Doc-only KEEP cluster**: 14 of 19 KEEPs are spec prose / ADR text fixes — lower priority, should be batched as docs-only pass.
- **holomush-2jpm (real KEEP)**: `NewAccessRequest` constructor exists but action constants (ActionRead etc.) absent; world/service.go still uses raw strings.

Batch 6 (5k1.174–5k1.203, 30 beads): ALL 30 CLOSE. All plan-review findings (label: `review-finding`).
- Plan completely rewritten to 781 lines with modular phase files (7.1–7.7). Original had 4000+ lines.
- All Mermaid fixes (T27 phantom, T21a, T16c removal), Go version, SPDX note, task splits, line refs: resolved in rewrite.
- `CompiledPolicy.Effect` is `types.PolicyEffect` at compiler.go:43 — ADR #62 applied.
- All three Delete methods exist: `service.go` via cascade_delete_test.go:332/370/408.
- `rg "AccessControl\b" internal/ --include="*.go" (non-test) → 0 hits` — migration complete.

Batch 7 (5k1.204–5k1.233, 30 beads): ALL 30 CLOSE. All PR #69 review findings against plan doc.
- Task 0 AST spike in phase-7.1.md; dsl/ast.go shipped. Task 18 confirmed (9 refs).
- SystemBypass → sync audit path at audit/logger.go:239,248,257.

## holomush-5rh Scenes epic audit (2026-05-16, complete)

19 open children: 8 CLOSE, 2 REFRAME, 3 FRONTIER-KEEP, 6 DOWNSTREAM-KEEP.
- Phases 1+2 shipped PR #200 (2026-04-07). Phase 3 shipped PR #202 (2026-04-09). Focus adoption PR #230 (2026-04-17). Audit QueryHistory membership gate PR #267 (2026-04-25).
- **3z90 is a false audit finding**: bead claimed "rg JoinFocus → 0 hits" but commands.go:349-397/418-431/541-565 call JoinFocus/LeaveFocus/PresentFocus. Close 3z90, unblock 5rh.13.
- **True Phase 4 frontier (5rh.13)**: `HandleEvent` is a no-op (main.go:39-41); `crypto.emits: []` in plugin.yaml; no poseorder.go. IC/OOC event emission is the gate.
- **scene_log write path blocked by Phase 4**: audit read path (QueryHistory) is implemented but there are no IC events to read yet.
- **REFRAME 5rh.5**: visibility isolation mechanism changed — focus subscription gating (not event routing filter); who/where scoping still unimplemented (Phase 5 work).
- **REFRAME 5rh.7**: scene_log schema + QueryHistory present (PR #267); `scene log`/`scene export` commands absent; IC emission (Phase 4) prerequisite.
- Forums epic (holomush-djj) undesigned; 5rh.9 correctly blocked on it.

## holomush-wr9 Epic 6 Commands & Behaviors audit (2026-05-16, complete)

153 open beads audited. 136 CLOSE (~25 SUPERSEDED), 12 KEEP (real open work).

**Core navigation gap:** `look`, `move`, `who`, `boot` have no implementation — not in
`internal/command/handlers/` (only quit, shutdown, resetpassword, plugin_admin remain),
and no `plugins/core-navigation/` plugin exists. These are genuine future-work beads.

**Plugin migration supersession pattern:** ~25 beads targeted Go handlers that were designed
in early E6 spec but superseded when all game commands migrated to Lua plugins. Safe to
CLOSE as SUPERSEDED: any bead referencing `WhoHandler`, `CreateHandler`, `SetHandler`,
`MoveHandler`, `LookHandler`, `BroadcastSystemMessage`, or `handlers/objects.go`.

**Key shipped artifacts:**
- `internal/command/` — parser, registry, dispatcher (OTel metrics, rate limiting, two-layer ABAC)
- `internal/command/alias.go` — AliasCache with player/system/prefix tiers, PostgreSQL-backed
- `internal/plugin/hostfunc/world_write.go` — create_location, create_exit, create_object, find_location, set_property, get_property
- `internal/property/registry.go` — PropertyRegistry with sync.RWMutex
- ADRs 0006/0007/0008 exist under two naming schemes (sequential + holomush-slug)
- `plugins/core-aliases/` — alias/unalias/aliases/sysalias/sysunsalias/sysaliases (all 6)
- `plugins/core-communication/` — say/pose/ooc/page/whisper/pemit/emit/wall (all 8)
- `plugins/core-building/` — dig/link; `plugins/core-objects/` — describe/examine/create/set
- `plugins/core-help/` — help; `plugins/core-aliases/` — alias management

## mq5o PR #123 audit (2026-05-16, complete)

34 open children (+ parent epic). 22 CLOSE, 11 KEEP, 1 DUPLICATE-IN-EPIC. Key patterns:
- **P0 beads mq5o.2 and mq5o.19 are DUPLICATES and FIXED**: production routes through `internal/session/setup/subsystem.go:53-54` calling `store.NewPostgresSessionStore(pool)`. MemStore is test-only.
- **JetStream F3/F6/F7 migration killed ~8 beads**: `replayMissedEvents`, `forwardLiveEvents`, per-event goroutines, cursor persist goroutines, `goto drained` label all gone. Beads targeting these patterns are MOOT.
- **IDOR fixes landed (bd-jv7z)**: `GetCommandHistory`, `HandleCommand`, `Subscribe`, `Disconnect` all call `auth.ValidateSessionOwnership`. `web/handler.go` forwards player_session_token via header.
- **Token entropy fixed**: `internal/auth/player_session.go:145-157` — `GenerateSessionToken()` uses `crypto/rand` 256 bits. ULID is gone from bearer token generation.
- **`player_token_store.go` no longer exists** in `internal/store/`. Replaced by `player_session_store.go` which uses `safePrefix(tokenHash)`.
- **Reaper panic recovery implemented**: `reaper.go:59-70` wraps `OnExpired` in deferred recover. `runDisconnectHooks` extracted as shared helper at `server.go:570-585`.
- **Remaining real P1 KEEPs**: mq5o.5 (MemStore ListByPlayer still ignores playerID), mq5o.13 (Disconnect CountConnections race), mq5o.23 (AddConnection failure leaves untracked connection).
- **Test-gap beads largely filled** by qve.16 implementation work (mq5o.29/.30/.31/.33/.34 all CLOSE).

## holomush-4vrc PR #88 ABAC Phase 7.6 audit (2026-05-16, complete)

All 158 open children audited. ALL 158 CLOSE. Zero KEEP, zero FALSE-FIX. Key patterns:
- **Praise cluster (24 beads)**: reviewer filed positive affirmation beads as open; no bugs.
- **Fix() sub-beads (37 beads)**: all parent findings resolved; 5 exact duplicate Fix() pairs.
- **Dead-file findings**: cache_test_helpers.go and migration_equivalence_test.go do NOT exist.
- **Pre-deployment [api] breaking change cluster**: ErrNilAccessControl, WithAccessControl, ServicesConfig.Access, accesstest package — all 0 hits; internal-only, no external callers.
- **StaticAccessControl moot cluster**: 0 hits; migration complete.
- **DI wiring verified**: `access/setup/setup.go:76-219` BuildABACStack wires all components.
- **IsInfraFailure**: `types/types.go:284-285` uses strings.HasPrefix; hasBypass at `rate_limit_middleware.go:101-102` wired.
- **Decision fields**: ALL unexported at `types/types.go:206-213`.
- **G706**: scoped to path pattern in .golangci.yaml, not global suppression.

## holomush-dwk (Epic 5 Auth & Identity) audit (2026-05-16, complete)

37 CLOSE, 9 KEEP, 0 FALSE-FIX across 46 open children.
- All E5.1–E5.6 phase/task beads (dwk.3–dwk.24): tracking-debt. Full auth substrate in `internal/auth/`: Argon2id hasher, rate limiter, Player type, postgres repos, auth/character/reset services, mocks. Migrations in 000001_baseline.up.sql. All CLOSE.
- ADR beads (qr25/oyin/yjob): all migrated to ID-based format (`holomush-4x7x`, `holomush-ex40`, `holomush-8qbl`). CLOSE.
- Review-finding E5.7.x cluster (logged 2026-02-02): mostly fixed. 7 CLOSE, 3 KEEP.
- **KEEP snq5**: `gateway_handler.go:refreshCharacterList` (line 779-782) sends client error but does NOT slog — operators blind to ListCharacters failures.
- **KEEP qsoi**: `NeedsUpgrade()` on `Argon2idHasher` is never called in auth flow. Bcrypt→argon2id auto-upgrade described in ADR holomush-4x7x is NOT implemented. Legacy bcrypt hashes will fail auth (AUTH_INVALID_HASH), not upgrade.
- **KEEP r88a**: No test for duplicate `token_hash` unique constraint violation in PasswordResetRepository.Create.
- **KEEP cri1**: HMAC vs opaque-random-token decision was made implicitly; no ADR or spec update.
- **KEEP odos**: `docs/specs/2026-01-25-auth-identity-design.md` still has pre-implementation schema (web_sessions, token_signature column, etc.).

## sdno PR #88 ABAC Phase 7.6 audit (2026-05-16, complete)

All 69 open children audited. All 69 CLOSE. Zero KEEP, zero FALSE-FIX. Key patterns:
- **infra:session-invalid prefix intentional** (sdno.1/5/10/20): Decision #55 documents the `infra:` prefix choice. `prefix.go:35-39` has explanation comment. Cluster of 4 beads = design misread.
- **ModeMinimal EffectSystemBypass cluster** (sdno.3/7/21/30/52/60): one bug, six beads. Fixed at `logger.go:239`. Always check for identical-body duplicates before verifying.
- **Sub-fix bead pairs**: 25 of 69 beads are Fix() sub-beads. Verify the parent finding and the fix together; don't check sub-fix in isolation.
- **who.go migration**: `handlers/who.go` gone (Lua migration). Any sdno bead referencing `who.go` circuit breaker duplication is moot. `commands.go:101-102` comments note this explicitly.
- **Test-coverage gap beads**: all 12+ such beads closed — all claimed gaps filled. Pattern: PR review findings lag behind implementation; by audit time coverage is usually present.
- **Dispatcher option short-circuit** (sdno.53): `dispatcher.go:108-110` has `if d.optErr != nil { return nil, d.optErr }` per option. Fixed.
- Degraded mode writes audit event with reason=degraded_mode at engine.go:152-170.
- ParentID is ulid.ULID confirmed at property_repo.go:58.
- Poller design diverged from pg_notify plan — uses polling model (poller.go), not pg_notify LISTEN.
- SESSION_INVALID covered in session_resolver_test.go; store error in engine_test.go:264.
- Microbenchmark targets at phase-7.3.md:1501-1507.
- Spec Deviations table (5 rows) and Deferred Features table (7+ rows) both present at plan lines 754-779.

Batch 5 (5k1.144–5k1.173, 30 beads): ALL 30 CLOSE. All plan-review findings (label: `review-finding`).
- Plan updated post-review: Spec Deviations (lines 756-761), Deferred Features (line 772), Mermaid edges (lines 271, 321-324, 334, 336).
- Phase 7.5 deferral to Epic 8 makes ~6 beads moot (Tasks 24/25/27).
- T16c never existed (`rg "T16c" docs/plans/` → 0 hits); lock.go never a handler; `CompiledPolicy` absent from implementation.
- Implemented: `internal/access/policy/types/`, `session_resolver.go`, `internal/audit/logger.go:abac_audit_wal_entries`, `access.WithSystemSubject()` in context.go, `abac_provider_circuit_breaker_trips_total` in metrics.go.

Batch 2 (5k1.48–5k1.78, 30 beads): ALL 30 CLOSE. All plan-review findings.
- **Phase 7.5 cluster (6 beads)**: 5k1.48/61/62/64/69/76 — lock compilation, lock tokens, policy admin commands, orphan cleanup. All moot by Phase 7.5 deferral (Decision #96).
- **Implementation verified correct**: `store/store.go:38-39` StoredPolicy has CreatedAt/UpdatedAt; `audit/logger.go:25-32` Mode is string type; `attribute/circuit_breaker.go:26-32` budget-utilization CB; `audit/logger.go:81-84` abac_audit_wal_entries gauge; `audit/partition_creator.go`+`retention.go` exist; `store/errors.go:12` IsNotFound exists; `attribute/property.go:18-19` ParentLocationResolver handles all parent types.
- **Seed design**: `seed.go` SeedPolicies() has 34 seeds (25 permit, 9 forbid). ADR 087 documents deliberate choice of EffectDefaultDeny over explicit catch-all forbid.
- **seed:property-restricted-visible-to + seed:property-restricted-excluded** both exist in seed.go.

## 5k1 batch 20 (2026-05-15, top-level non-.N beads, complete)

3 CLOSE, 1 KEEP, 26 KEEP-UNVERIFIED. 26 of 30 provided IDs did not exist in database (after dolt pull).
- 8agz: decisions doc split to docs/specs/decisions/epic7/; D#50/51/52 exist in individual files → CLOSE.
- imzn: CTE depth inconsistency; old monolithic spec gone; all abac/ sections say 20 → CLOSE.
- j159: ADR 0010 → holomush-iv43; inline hazard note already at line 44-46 → CLOSE.
- dpsd: KEEP per brief (canonical S1 ingress tracker; engine guard exists but higher-level API layer guard still pending).
- **KEY PATTERN**: If given a batch of top-level bead IDs (no .N suffix), check existence first with `bd list --json --limit 10000 | jq -r '.[].id' | grep -E "id1|id2"` — many may not exist.

## 5k1 batch 14 (2026-05-15, .421–.452, complete)

30 open beads all CLOSE. All plan/spec review findings pre-implementation.
- seed:player-exit-use at seed_test.go:88; seed:property-system-forbid absent (0 hits). ADRs 86-88 in README. ADR 57 range already "001-110". T5→T8 edge at implementation.md:112,296. T22b task at phase-7.4.md:187. T16b→T23 at implementation.md:409. original_subject at migrations/000001_baseline.up.sql:291. System bypass precedence at 04-resolution-evaluation.md:326-329. ADR 89/90 relative links already use ../../../abac/. ADR 38 link already correct filename. T17 split at phase-7.3.md:498-511.
- 5k1.438/5k1.444 (SHA256 comment, rumdl CI caching): unimplemented minor CI enhancements from plan review; closed as moot (Phase 7.5 deferred, finding is on historical plan doc). No code/CI is broken.
- 5k1.441 (MD057): .markdownlint.yaml doesn't exist; project uses rumdl, not markdownlint-cli2. Rule is non-applicable.
- 5k1.429 (4-backtick fence): intentional design — whole grammar spec is in one 4-backtick block to allow nested triple-backtick examples.

## 5k1 batch 10 (2026-05-15, .298–.327, complete)

30 beads (PR #69 review findings) all CLOSE. Behavioral findings all shipped: ResourceExit/ResourceScene (prefix.go:29-30), glob validation (compiler.go:18-338), like `:` separator (evaluator.go:302), bootstrap compile errors (bootstrap_test.go:301). LISTEN/NOTIFY beads (315/318) moot by poller design. Already-fixed: ADR 024 link, ADR 066 created, README index, doc count to "93", ADR 057 range to "001-110", rumdl CI from v0.1.15 to v0.1.62 (ci.yaml:66). MD057 disabled = intentional trade-off.

## 5k1 batch 18 (2026-05-15, .546–.575, complete)

ALL 30 CLOSE. Key verified artifacts: dprint SHA256 at ci.yaml:77; Taskfile lint:markdown has `--exclude site`; `actions/cache@v4` replaced by nscloud-cache-action SHA-pinned; glob colon separator at compiler.go:302+evaluator.go:285; glob rejection (brackets/braces/globstar) at compiler.go:313-338 with tests; LRU 256-entry criterion at phase-7.3.md:154; ADRs 98-102 already use `# N. Title` format. Mermaid/diagram beads all moot by plan rewrite (batch 6).

## 5k1 batch 08 (2026-05-15, .234–.264, complete)

30 real beads + 1 ghost (.261 not in DB). All 30 CLOSE. Key:
- Plan split (5k1.259) done: phase-7.1 through phase-7.7 all exist.
- Per-phase corrections (.260/.262/.263/.264): 0-hit greps confirm fixed.
- TD1: `PolicyEffect string` at `types/types.go:50-54`. TD2: `Decision.allowed` unexported + `IsAllowed()`.
- "Off" mode = `ModeMinimal` (Decision #104). Bead .234: `TestAuditLoggerMinimalModeSystemBypassLoggedSync` at `logger_test.go:133`.
- S1 (panic recovery): `recover()` in `attribute/resolver.go` (2 sites). S2 (pg_notify): poller used; 0 pg_notify hits.

## 5k1 batch 09 (2026-05-15, .266–.297, complete)

29 beads all CLOSE. Plan/spec review findings 2026-02-08/09. Key:
- `docs/specs/abac/` split done (9 section files). `docs/specs/decisions/epic7/` done (all phase dirs + README).
- ADR 057: correct path/numbering/format. ADR 036: Supersedes #5 only. ADR 038: `off`→`minimal`. ADR 024: refs #36.
- README gaps 60-64/67-73 documented. ADR 077 covers Decision type mixed visibility.
- `seed:property-restricted-*` have `visibility=="restricted"` guards in DSL. 34 total seeds.
- `store.go`: both `hasSeedPrefix`+`hasLockPrefix` checks; `Effect *types.PolicyEffect` in ListOptions.
- `resolver.go:153-155` re-entrance guard (panic); `engine.go` calls `decision.Validate()` on all return paths.
- `service.go:667-685` compound resource decomposed (`list_characters` action per ADR #76).

## 5k1 batch 21 audit (2026-05-15, top-level ABAC review beads)

23 of 33 existed in DB; 10 returned "no issue found". Of 23: 20 CLOSE, 3 KEEP.
- Spec section-file rewrite resolves nearly all doc-fix beads (monolithic spec → `docs/specs/abac/00-08*.md`).
- 10 missing bead IDs: likely partial bulk-import. Orchestrator should investigate source list.
- **holomush-u2da (KEEP)**: Spec chose SESSION_INVALID = normal operation; bead asked for CRITICAL alerting.
- **holomush-t70y (KEEP)**: Deferred refactor — shared access helper. `rg "checkAccess"` → 0 hits.
- **holomush-k5c3 (KEEP)**: Admin bypass removal from staleness/degraded mode — needs engine.go code read.

## 5k1 batch 22 audit (2026-05-15, top-level ABAC beads — FINAL batch)

9 CLOSE, 1 KEEP. All doc-fix beads address `docs/specs/2026-02-05-full-abac-design.md` (now a 37-line index pointing to `docs/specs/abac/` split files). Verify fixes in refactored files, not original.
- vok6 (plugin collision → fatal) → `abac/01-core-types.md:407-414` MUST reject+continue.
- wheb (20% benchmark threshold) → `abac/04-resolution-evaluation.md:587-592` now 10%.
- wn9s (step numbering mismatch) → `abac/00-overview.md:154-156` defers to Evaluation Algorithm.
- x1rq (SHOULD→MUST session codes) → `abac/01-core-types.md:222-226` constants MUST.
- xd0q (corrupted forbid degraded mode) → `abac/04-resolution-evaluation.md:318-344`.
- y65i (Decision #42 assumption note) → `decisions/epic7/phase-7.3/042-sequential-provider-resolution.md:24-28`.
- yhg6 (CTE failure → nil,err) → `abac/03-property-model.md:198-204`.
- z42q (fuzz corpus dir) → `abac/08-testing-appendices.md:73-76`.
- znbl (ADR 0013 circular dep) → moved to `docs/adr/holomush-xx3e-properties-as-first-class-world-model-entities.md:63-86`.
- **ur6c (KEEP)**: noopSessionResolver hardcoded in setup.go:154-155; no real ResolveSession implementation; ABACConfig has no SessionResolver field. Wiring deferred pending web auth layer.

## Known-stale epics to watch

- `holomush-oy6e` — `oy6e.2` references dead LISTEN-based `EventStore.SubscribeSession`.
- `holomush-qve.16` — `qve.16.5` references dead broadcaster + cursor primitives.
- `holomush-ec22` (codebase review 2026-04-25) — recent, mostly still valid.

## qwom PR #116 telnet audit (2026-05-15, complete)

All 35 open children audited. All are CLOSE. `gateway_handler.go` fully rewritten; all
bugs fixed. `ReleaseGuest` at guest_auth.go:55-60. `CoreClient` interface 13 methods with
compile-time assertion at gateway_handler_test.go:70-75. High duplicate rate (~8 distinct
findings filed 35 times). Zero false-fix cases.

## 5k1 Epic 7 ABAC batch 03 (2026-05-15, complete)

30 beads (5k1.79–113) all CLOSE. All target `docs/plans/2026-02-06-full-abac-implementation.md`
(PR #69 review); implementation shipped PRs #88/#106/#114. Key verifications:
- char→character: `internal/access/prefix.go:14`; `ParseEntityRef` rejects `char:` as legacy.
- Circuit breaker: budget-utilization, `attribute/circuit_breaker.go:56-63` — spec-compliant.
- StoredPolicy timestamps: `store/store.go:38-39`.
- Metrics: `{source,effect}` labels at `metrics.go:24-27` — no cardinality issue.
- 34 seeds (25 permit+9 forbid); no `seed:catch-all-deny` — `seed_test.go:17`.
- Reserved prefix enforcement: `store/store.go:70-108`.
- LISTEN/NOTIFY → poller design evolution: `poller.go` used, not LISTEN reconnect.
- P1 plan errors (5k1.95/96/97) never propagated to code — all resolved correctly.

## 5k1 Epic 7 ABAC batch 11 (2026-05-15, complete)

30 beads (5k1.328–357) all CLOSE. Tooling + security design notes all implemented:
- S1 dual bypass: engine_test.go:109 `TestEngineSystemBypassRejectedWithoutSystemContext`.
- S2 re-entrance: resolver.go:154-155 context-scoped panic; cross-goroutine = MUST NOT (Decision #84).
- S3 'off'→'minimal': logger.go:236-241 all modes log denials (Decision #86).
- S4 lock TOCTOU: moot — Phase 7.5 Locks deferred to Epic 8 (Decision #96).
- S5 cache deadlock: setup.go:181-184 OnMutate detached-context invalidation (Decision #107).
- S6 namespace runtime validation: resolver.go:438-454 `mergeAttributes()` rejects unregistered keys.
- S7 WAL MUST: logger.go:161-183 WAL fallback unconditional for deny/default_deny/system_bypass.
- S8 migration complete: StaticAccessControl → 0 hits; no live call sites.
- CI SHA256 pinned literals: ci.yaml:69-71 for rumdl/dprint/buf (Decision #93).
- lefthook site/ split: lefthook.yaml:50-72 (`exclude: "site/**"` + separate site hooks).
- Canary ABAC integration test: test/integration/access/evaluation_test.go:22.
- markdownlint configs gone: .markdownlint.yaml / .markdownlint-cli2.yaml both deleted.

## 5k1 Phase 7.3 implementation task audit (2026-05-15, complete)

17 beads (5k1.5.1–5k1.5.16 + 5k1.3.12) all CLOSE. T13–T21b + T7b shipped PRs #88/#114.
- AttributeProvider + SchemaRegistry: `attribute/provider.go` + `attribute/schema.go`.
- Resolver per-request cache + re-entrance guard: `attribute/resolver.go:152-155`.
- Core providers: `attribute/{character,location,property,command,stream,exit,scene,environment}.go`.
- PropertyProvider recursive CTE via `ParentLocationResolver` at `property.go:18`.
- Engine Evaluate() Steps 1-10 complete: `engine.go:102-364`.
- Poller cache (not LISTEN/NOTIFY — design evolution): `poller.go` + `cache.go`. T18 title says LISTEN/NOTIFY; close as shipped-differently.
- Audit logger + WAL fallback: `internal/audit/logger.go:140-230`.
- Retention + partition: `internal/audit/retention.go` + `partition_creator.go`.
- Metrics: `metrics.go:17-66` (`abac_degraded_mode`, evaluations, circuit breaker, etc.).
- Benchmarks: `engine_bench_test.go` (6 funcs, targets lines 15-19).
- CI enforcement: `.github/workflows/benchmark-check.yml` + `scripts/check-benchmark-regression.sh`.
- T21a (@-prefix): seed policies use bare names; `command.go` docstring retains `@dig` as legacy-compat — intentional.
- T7b contract tests: `engine_contract_test.go` (8+ tests).

## 5k1 Phase 7.4+7.6 implementation task audit (2026-05-15, complete)

9 beads (5k1.6.1–6.4, 5k1.8.1–8.3, soor, 39vv) all CLOSE. All Phase 7.4 seed/bootstrap and Phase 7.6 migration tasks shipped PRs #88+#106.
- T22: `seed.go` has 34 seeds; `seed_test.go:14` confirms count; all with SeedVersion.
- T22b: G1–G4 at `seed.go:148-178`; G5+G6 intentional (documented at :31-32); `seed_validation_test.go:21` tests G5.
- T23: `bootstrap.go` ships SkipSeedMigrations/:24, partition creation/:40, UpdateSeed/:190, upgrade/:141.
- T23b: `--validate-seeds` CLI flag never implemented (0 hits in cmd/holomush/) — moot per ADR #92; smoke tests at `seed_smoke_test.go` + `test/integration/access/seed_policies_test.go`.
- T28/T29: `rg "StaticAccessControl|AccessControl.Check|capability.Enforcer|char:" internal/ → 0 hits`.
- T28.5 (`migration_equivalence_test.go`): never created, moot — old engine deleted before test could be written.
- soor: context cancellation tests at `dispatcher_test.go:926+991`.
- 39vv: MOOT — EventStore deleted by PR #252.
- Zero false-fix entries.

## 5k1 Phase 7.1+7.2 implementation task audit (2026-05-15, complete)

17 beads (5k1.3.1–3.11, o1jt, 5k1.4.1–4.5) all CLOSE. All Phase 7.1 schema + Phase 7.2 DSL/compiler tasks shipped PRs #88/#106/#114.
- ABAC tables in `000001_baseline.up.sql` (not planned `000015`/`000017` — baseline consolidation artefact).
- PropertyRepository at `internal/world/postgres/property_repo.go`; DeleteCharacter at `service.go:588`; cascade for all 3 entity types at `service.go:602-619`.
- Core types (unexported `allowed` field, Validate()) at `internal/access/policy/types/types.go`.
- Prefix constants + ParseEntityRef (char: rejected) at `internal/access/prefix.go:13-183`.
- PolicyStore: `store/store.go` + `store/postgres.go`.
- DSL package complete: `dsl/{ast,parser,evaluator,validate}.go` + `compiler.go` + `parser_fuzz_test.go`.
- Cascade integration tests: `internal/world/postgres/cascade_delete_test.go` (build tag `integration`).
- Zero false-fix entries.

## holomush-x2t Epic 4 World Model audit (2026-05-16, complete)

31 CLOSE, 1 SUPERSEDED, 2 KEEP. 33 open children (+ epic root).
- Full substrate shipped: `internal/world/` (location/exit/object/scene/character types + events + payloads), `internal/world/postgres/` (all 5 repos + testcontainers), `internal/world/worldtest/` (7 mocks), `test/integration/world/` (9 BDD files), `internal/plugin/hostfunc/` (query_object + nil-check + context inheritance).
- All tables in `000001_baseline.up.sql:92-165` (not separate migration file — baseline consolidation).
- `EventTypeMove` at `internal/core/event.go:54`; object event types in `plugins/core-objects/events.go` (plugin-boundary design).
- `withQueryContext` at `helpers.go:70-94` centralizes Lua context inheritance for ALL world query functions; `TestQueryObjectInheritsParentContext` at `world_test.go:992`.
- x2t.22 (LISTEN/NOTIFY): SUPERSEDED by JetStream PR #252; spec at `docs/specs/2026-03-20-event-delivery-redesign.md:10` has explicit `**SUPERSEDED**` header.
- x2t.25 (KEEP): doc task — spec acceptance checkboxes + architecture-overview update, not yet verified done.
- Zero false-fix entries.

## holomush-1hq (Epic 2: Plugin System) audit (2026-05-16, complete)

34 CLOSE, 3 KEEP (+ epic itself = 4 KEEP total). 0 FALSE-FIX.
- **Tracking-debt dominant pattern**: 29 of 34 CLOSEs are Jan 2026 TDD task chain beads against w77b (PR #192) work that shipped. Verify with ls on paths cited in bead descriptions.
- **Capability enforcer superseded**: 1hq.7 + 1hq.10 reference `internal/plugin/capability/enforcer.go` — directory never created; superseded by ABAC hostfunc (capability.go). `internal/plugin/capability/` → No such file or directory.
- **oops migration complete (25.x cluster)**: all 12 target files use `github.com/samber/oops`; pkg/errutil shipped. Two residual `fmt.Errorf` in cmd/holomush/core.go are new startup context wraps, not original 316 sites.
- **testify migration complete (51-57)**: all target packages verified using testify via import scan; hand-rolled mocks replaced by mockery (0 `type mock` structs in grpc/).
- **KEEP cluster**: 1hq.26 (parent epic for rccc), 1hq.60 (store integration still testing.T), 1hq.63 (docs/reference/testing-guide.md absent), 1hq.64 (blocked on 60+63).
- test:ginkgo target never created — ginkgo runs under test:int. 1hq.50 still closable (mockery + mocks:generate shipped).

## holomush-59zu PR #107 ABAC stack wiring audit (2026-05-16, complete)

10 CLOSE (6 SUPERSEDED + 4 CLOSE), 2 KEEP. Key patterns:
- **Implementation shape changed post-review**: PR #107 beads target the first draft (pglistener.go, hasPool type assertion, pluginManager.LoadAll in core.go). Final implementation uses Poller (poller.go) + subsystem lifecycle. `pglistener.go` MISSING, `hasPool` → 0 hits.
- **KEEP 59zu.7**: `setup.go:51` `&& firstErr == nil` guard silently drops sqlDB.Close() error when AuditLogger.Close() also fails. Suggested fix: `errors.Join` or log secondary at Warn.
- **KEEP 59zu.11**: `attribute/plugin_provider.go:31-33` plain field write `p.registry = r` without sync/atomic. ResolveSubject reads p.registry at line 44. Race detector will flag. Fix: `atomic.Pointer[PluginRegistry]`.
- **Duplicate conflict (59zu.3/4 vs 59zu.8)**: 3 and 4 recommend adding session ID to error; 8 says current code (NOT including it) is the secure pattern. All 3 CLOSE because prod is already correct.
- **WAL /tmp fallback (59zu.12)**: CLOSE — file created with 0o600 (owner-only); bead claimed 0644. Main security concern addressed.
