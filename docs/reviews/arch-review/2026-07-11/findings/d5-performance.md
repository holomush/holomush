<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# D5 Performance & Scalability — Findings

**Agent:** performance-engineer / Opus 4.8 · **Date:** 2026-07-11 · **Scope examined:** event publish hot path (`internal/eventbus/publisher.go`, `rendering_publisher.go`), subscriber fan-out + per-message decode/AuthGuard (`internal/eventbus/subscriber.go`, `authguard/guard.go`, `adapter_dek.go`), DEK/participant caches (`internal/eventbus/crypto/dek/{cache,participants_cache,manager,store}.go`), KEK provider (`kek/local_aead.go`), history query (`internal/eventbus/history/cold_postgres.go` + `events_audit` indexes), presence (`internal/grpc/list_focus_presence.go`, `internal/store/session_store.go`), session indexes (migrations 000001/000009/000011/000013/000016), Lua host state lifecycle (`internal/plugin/lua/{host,state}.go`), audit projection (`internal/eventbus/audit/projection.go`), pool config (`internal/store/postgres.go`). Cross-referenced `evidence/open-issues.json` (186 issues) and verb-registry/pool wiring in `cmd/holomush/sub_grpc.go`, `core.go`.

## Summary

The event architecture is soundly built for its hobbyist target: per-stream JetStream ordering, per-session durable consumers with bounded backpressure (MaxAckPending=256), embedded NATS by default (no network hop), an index-backed + LIMIT-bounded history query, batch name-resolution in presence (no N+1), an in-memory (not KMS) KEK provider, and a reused protovalidate validator. The bulk of events (movement, notices, public `say`/`pose`) are `sensitivity: never` → identity codec → a cheap decode-only fan-out with **no** AuthGuard, DEK, or DB touch.

The one material scalability characteristic is on the **encrypted-content path** (scene IC roleplay content, private page/whisper): two in-process caches that exist specifically to keep the crypto hot path off Postgres are **bypassed for the DB read** — `dek.Manager.GetOrCreate` (publish) and `dek.Manager.Participants` (per-subscriber AuthGuard) each do an unconditional `crypto_keys` SELECT. Net effect is O(participants) indexed point-reads per scene pose. It holds fine at the low hundreds but is the highest-ROI optimization and the fix is already described in the code's own comments.

Counts: **Medium 2 · Low 3 · Info 1** (0 Blocker, 0 High). Several adjacent items already tracked (#4693 pool, #4713 alias pool, #4697/#4628 ABAC benchmarks).

## Findings

### MED-1 Encrypted-event hot path bypasses its own DEK/participant caches for the DB read (O(participants) crypto_keys reads per scene pose)

- **Severity:** Medium (considered High for the compounding-architecture angle; hobbyist calibration + cheap index point-reads + in-memory KEK keep it Medium — but it becomes the first thing to bite if scenes get large or the pool stays default-sized)
- **Claim:** For a `sensitivity: always` event (scene IC content; private page/whisper), the publish path and every per-subscriber delivery each perform an unconditional `crypto_keys` SELECT even when the DEK/participant material is already in the in-process cache — so a scene pose to P participants costs ~(P+1) Postgres round-trips, none of which the existing caches prevent.
- **Evidence:**
  - Publish path never consults the DEK material cache: `internal/eventbus/crypto/dek/manager.go:221-230` — `GetOrCreate` opens with `m.store.selectActive(ctx, ctxID)` (a PG query, `store.go:95-121`) and only *writes* the cache via `unwrapAndCache` (`manager.go:539-566`). `m.cache.Get` is never called here. Every sensitive `Publish` (`publisher.go:216` `p.dekMgr.GetOrCreate(...)`) therefore = 1 `crypto_keys` SELECT + 1 KEK unwrap.
  - Subscribe/AuthGuard path reads PG before its cache: `manager.go:348-368` `Participants` calls `m.store.selectByID` (PG, `store.go:122-143`) **first**, then checks `m.partCache.Get`. The code comment concedes it (`manager.go:339-347`): *"this body reads PG first … A future '(keyID, version) → (ctxType, ctxID)' reverse index would lift the PG read; out of scope for T7."* On a cache hit it discards the row it just read — the participants cache saves only a slice copy, not the round-trip.
  - That `Participants` is on the per-message-per-subscriber path: `authguard/adapter_dek.go:22-27` → `authguard/guard.go:69-80` (`checkCharacter`) → invoked from `subscriber.go:628` `guard.Check` inside `decodeAndAuthorize`, which runs for every non-identity delivery (`subscriber.go:503-513`).
  - AuthGuard is wired in production whenever a KEK is configured: `cmd/holomush/sub_grpc.go:214-221`, `:425-432`.
  - Scene IC content is `sensitivity: always`: `plugins/core-scenes/plugin.yaml:155-172` (pose/say/emit/ooc). Public communication is `never` (`plugins/core-communication/plugin.yaml:277-305`) — so this is scoped to scenes + private messages, not all chat.
- **Impact:** During an active encrypted scene, each pose fans to every participant session; each delivery's `guard.Check` issues a `crypto_keys` point-read. A 10-person scene = ~11 `crypto_keys` reads per pose (10 fan-out + 1 publish) plus 1 in-memory KEK unwrap. Each read is a sub-ms index point-read on a small table (`crypto_keys` UNIQUE `(context_type,context_id,version)` + partial active index, migrations 000013/000016; `selectByID` is PK-backed), so it holds at the low hundreds. It compounds because all reads draw from one default-sized pool (see LOW-2 / #4693) alongside interactive commands, so at several concurrent large scenes it adds head-of-line latency to `look`/`move`/`say`.
- **Recommendation:** Implement the reverse index the code already names: cache `(keyID,version) → (contextType,contextID)` so `Participants` is cache-first (eliminates the per-subscriber read). For the publish side, add a small `contextID → activeDEK(keyID,version)` cache invalidated by the existing rekey/rotate cluster-invalidation coordinator (which already calls `InvalidateContext`), so `GetOrCreate` short-circuits the `selectActive` on the steady state. Both are in-process only (INV-CRYPTO-16 preserved). This is the single highest-ROI perf change; no infra needed.
- **Dedup:** none (related pool sizing: #4693)

### MED-2 Lua plugin source is re-lexed/parsed/compiled on every event delivery (no compiled-bytecode cache)

- **Severity:** Medium (scoped to Lua plugins only — the hot content plugins are binary; echo-bot is the one hot Lua subscriber)
- **Claim:** Each `DeliverEvent`/`DeliverCommand` compiles the plugin's entire Lua source string from scratch (`L.DoString(code)`) into a throwaway state, rather than executing pre-compiled bytecode — spending O(source-size) parse+compile CPU on every event a Lua plugin handles.
- **Evidence:** `internal/plugin/lua/host.go:478` snapshots `code := p.code` (raw source), `:499` `h.factory.NewState(ctx)` (fresh VM), `:533` `L.DoString(code)` — DoString lexes, parses, compiles to bytecode, and executes on every call; `:503` `defer L.Close()` discards it. Same pattern in `DeliverCommand` (`:605,:630`) and `:733,:752`. The fresh-state-per-event isolation is deliberate and correct (`state.go:67-96`); the avoidable cost is re-compilation, not the state.
- **Impact:** echo-bot subscribes to `say` and re-emits, so every `say` event triggers a full recompile of its source. echo-bot is tiny (compile ≈ tens of µs), so it is not a bottleneck today, but the cost is O(source bytes) per event and scales badly for any larger Lua plugin on a hot verb. Binary plugins (scenes/communication/channels — the real content path) are unaffected (out-of-process gRPC).
- **Recommendation:** Compile once at `Load` into a `*lua.FunctionProto` (`parse.Parse` → `lua.Compile`) and store it on the loaded-plugin struct; per delivery, `L.Push(L.NewFunctionFromProto(proto)); L.PCall(...)` instead of `DoString(code)`. This is the library-documented reuse pattern — gopher-lua explicitly supports sharing read-only bytecode across many LStates (https://github.com/yuin/gopher-lua, "Sharing Lua byte code between LStates"). Keeps per-event isolation; drops the parse/compile step.
- **Dedup:** none (plugin runtime hardening #4675 is adjacent but about goroutine/ctx-watchdog, not compilation)

### LOW-1 `sessions.location_id` has no index — presence query sequential-scans the sessions table

- **Severity:** Low (small table at target scale; trivial fix)
- **Claim:** `ListActiveByLocation` filters `WHERE location_id = $1 AND status='active' AND grid_present=true` but no index covers `location_id`, so each presence snapshot is a seq-scan of `sessions`.
- **Evidence:** query at `internal/store/session_store.go:745-753`; `sessions` indexes are `idx_sessions_active_character` (character_id, partial), `idx_sessions_status` (status='detached', partial), and `idx_sessions_player_session_id` — none on `location_id` (migration 000001 lines 221-223; migration 000008). The correlated `EXISTS` on `session_connections` is covered by `idx_session_connections_session` (000001), so only the outer scan is unindexed.
- **Impact:** `ListFocusPresence` (`internal/grpc/list_focus_presence.go:140`) runs on every "who's here"/location render. At hundreds–low-thousands of sessions a seq-scan is sub-ms, so no real pain at target scale; it degrades linearly as the sessions table grows.
- **Recommendation:** Add a partial index `CREATE INDEX idx_sessions_location_active ON sessions (location_id) WHERE status='active' AND grid_present=true;` (mirrors the partial-index style already used for `idx_sessions_status`). Cheap insurance; matches the query exactly.
- **Dedup:** same underlying gap as the **canonical D7 data-layer finding** (`findings/d7-data.md` MEDIUM-1); this is the perf lens. Both feed the single issue I16 (#4796); D7 carries the chosen severity.

### LOW-2 pgx pool left at library defaults (max(4, NumCPU)); one shared pool fronts all subsystems

- **Severity:** Low (already-tracked; recording the concrete observation for the record)
- **Claim:** The pool is built via `pgxpool.ParseConfig(dsn)` + `NewWithConfig` with no explicit `MaxConns`/`MinConns`, so it defaults to `max(4, runtime.NumCPU())` connections, and that single pool is shared by world, session, crypto (`crypto_keys`), audit-chain, totp and admin stores — the same pool the MED-1 per-event `crypto_keys` reads draw from.
- **Evidence:** `internal/store/postgres.go:41-51` (no MaxConns set; only the otel tracer is configured). Shared-pool wiring: `cmd/holomush/core.go` threads one `dbSub` pool into `NewCheckpointRepo`, `auditChainRepo`, `totpRepo`, `adminRoleStore`, world/session `Pool:` fields, etc.; `sub_grpc.go:237` `pool := s.cfg.DB.Pool()`.
- **Impact:** On a low-core host the effective ceiling is 4 concurrent DB ops; the MED-1 fan-out reads plus interactive command queries compete for those slots, so pool saturation (not query cost) becomes the latency ceiling under concurrent scene load.
- **Recommendation:** Surface `pool_max_conns` as an explicit config knob (or set a sane floor like 10–20 in `ParseConfig`), and land MED-1 so the pool isn't spent on avoidable `crypto_keys` reads. No change needed for tiny deployments.
- **Dedup:** already-tracked:#4693 (Review pgx pool and PostgreSQL connection settings); related #4713 (unify the separate plugin-alias pool into the shared pool — a second pool compounds the ceiling).

### LOW-3 Rendering header (protojson) is re-marshaled on every publish though it is fully determined by the verb type

- **Severity:** Low (micro-optimization)
- **Claim:** Every event, `RenderingPublisher.Publish` allocates a fresh protojson `App-Rendering` header from data that is identical for all events of the same verb type (the static verb registration).
- **Evidence:** `internal/eventbus/rendering_publisher.go:78` `renderingJSONOpts.Marshal(RenderingToProto(event.Rendering))` runs unconditionally; the `RenderingMetadata` fields (`:66-73`) come verbatim from `reg.Category/Format/Label/DisplayTarget/Source` — a per-type constant looked up at `:59` (`registry.Lookup`, an O(1) map read, `internal/core/registry.go:93-98`). `validateRendering` (`:106`, `:121-126`) also re-validates the same per-type proto each publish, though the validator itself is correctly built once (`:49-53`).
- **Impact:** One JSON marshal + one protovalidate eval of allocation/CPU per event that could be memoized per verb type. Negligible at hobbyist volume; pure waste at any volume.
- **Recommendation:** Memoize the marshaled `App-Rendering` bytes (and the validation result) per verb type on the `VerbRegistry` entry, computed at registration time. Optional; only worth it if a profiler later flags publish allocation.
- **Dedup:** none

### INFO-1 Audit projection is a single serial consumer with one INSERT per host-owned event (no batching)

- **Severity:** Info
- **Claim:** Host-owned events are persisted to `events_audit` by a single `Consume` worker doing one `INSERT` per message; there is no pgx `Batch`/`CopyFrom` amortization.
- **Evidence:** `internal/eventbus/audit/projection.go:203` single `p.consumer.Consume(p.handle)`; `:254` `p.persist(msg)` per message → `writeAuditRow` (`:353`, `:416` `INSERT INTO events_audit`). Plugin-owned subjects (scene content) are ack-and-skipped here (`:238-252`) and projected by a separate per-plugin consumer, so this path carries host-owned events only.
- **Impact:** A single-threaded INSERT loop tops out around ~1–2k inserts/s — comfortably above hobbyist peak, and JetStream MaxAckPending provides backpressure if it falls behind (no loss; redelivery + DLQ on max-deliver). Would need batching to scale to high write volume.
- **Recommendation:** None now. If write volume ever climbs, coalesce persist into a `pgx.Batch`/`CopyFrom` micro-batch (flush on N rows or a short timer). Recording as a known scaling seam, not a defect.
- **Dedup:** none

## Strengths

- **Bounded per-session backpressure.** The session inbox channel is sized to `MaxAckPending` (256) and `handle` blocks on send, so a slow client stalls its own consumer rather than leaking memory — `internal/eventbus/subscriber.go:216-239`, `:289-307`; defaults documented at `:28-39`.
- **History query is bounded and index-backed.** Cold-tier PG read always applies `LIMIT` and `ORDER BY js_seq`, with cursor/time bounds — `internal/eventbus/history/cold_postgres.go:186-198`; backed by `events_audit_subject_js_seq (subject, js_seq)`, `events_audit_subject_ts (subject, timestamp)`, and `events_audit_subject_pat (subject text_pattern_ops)` for prefix LIKE (migrations 000009/000011). No unbounded scan.
- **Presence avoids N+1.** `ListFocusPresence` dedups character IDs in memory then does one **batch** name resolution `characterNameResolver.Names(ctx, uniqueIDs)`, not a per-character lookup — `internal/grpc/list_focus_presence.go:147-164`.
- **DEK material `Resolve` IS cache-first.** The decrypt-key path checks `m.cache.Get` before PG (`manager.go:315-321`) — the counter-example that shows MED-1's `GetOrCreate`/`Participants` reads are an omission, not a policy.
- **In-memory KEK, not KMS.** Only a local XChaCha20-Poly1305 provider exists (`kek/local_aead.go:44`, Unwrap holds the KEK in `p.kekByID` and does an in-memory AEAD open) — no per-event network KMS call. Removes the worst-case publish-path cost.
- **Cheap identity-codec fan-out for the common case.** Non-sensitive events skip AuthGuard/DEK/PG entirely — `subscriber.go:499-502` (`codecName == identity → deliver as-is`); public communication verbs are `sensitivity: never` (`plugins/core-communication/plugin.yaml`).
- **Fresh Lua state per event is deliberate isolation, cheaply built.** `SkipOpenLibs` + only 4 safe libs (`internal/plugin/lua/state.go:73-95`) keeps VM construction minimal; the only waste is recompilation (MED-2), not the state.
- **Honest self-documentation of a scale seam.** `ListByFocus` explicitly notes the missing GIN index and states the acceptable cardinality bound — `internal/store/session_store.go:769-774`. Good engineering hygiene.
- **Well-built caches (once reached).** Both DEK and participant caches are LRU + TTL with a reverse `byContext` index for O(entries-for-context) invalidation and defensive slice copies — `crypto/dek/cache.go`, `participants_cache.go`.

## Not examined

- **NATS JetStream server-side consumer scaling** (N durable consumers per stream at high N). Verified the per-session-durable design in code; did not load-test NATS's own overhead at thousands of consumers — out of scope for a code review and above the hobbyist target.
- **Web/BFF and SvelteKit client performance** (Core Web Vitals, bundle size) — owned by the UI dimension.
- **Binary-plugin gRPC transport cost** (mTLS handshake reuse, proxy goroutine lifecycle) — touches reliability more than throughput; goroutine cleanup is already tracked in #4675.
- **Actual profiling / benchmarks.** This is a static analysis; all cost estimates are inferred from the code and labeled as such. The repo has ABAC benchmarks pending recalibration (#4628) and a benchmark-suite ask (#4697); no event-path benchmark exists to cite measured numbers.
- **World-model read caching** (`internal/world` location/character reads per command) — spot-checked query shapes but did not exhaustively trace every command's DB fan-out.
