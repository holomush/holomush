# D6 — Reliability, Error Handling & Observability — Findings

**Agent:** golang-pro/Sonnet 5 · **Date:** 2026-07-11 · **Scope examined:** `internal/eventbus/audit/**` (DLQ, projection, replay, plugin consumer), `internal/lifecycle/**`, `cmd/holomush/core.go` (boot path, fail-closed KEK/NATS-scope checks), `internal/eventbus/{subsystem,natsdial,publisher,subscriber,subscriber_auth,config}.go`, `internal/eventbus/authguard/audit/emitter.go`, `internal/plugin/{subscriber,manager}.go`, `internal/world/grpc_server.go`, `internal/audit/{logger,postgres}.go`, `internal/access/{setup,policy}/*.go`, `internal/telemetry/*.go`, `internal/observability/server.go`, `cmd/holomush/cmd_audit.go`, `docs/architecture/invariants.yaml` (EVENTBUS/TELEMETRY scopes), operator docs under `site/src/content/docs/operating/`.

## Summary

The `oops` structured-error discipline is applied consistently at every boundary I sampled, and the boot path is genuinely fail-closed (KEK required, external-NATS scope self-check, JetStream `provision:false` verify-and-refuse). The audit DLQ never-drop guarantee (INV-EVENTBUS-29/30) for the **host-owned** projection is rigorously implemented and well-tested — idempotent replay, fail-loud (never silent) on corruption, bounded retention with an alerting counter. However, I found one **High** finding: the documented zero-config default deployment (auto-generated `game_id`) breaks the audit-DLQ **replay** CLI — the tool that exists specifically to make dead letters recoverable rather than lost — because two independently-defaulted `game_id` settings diverge and neither the CLI nor the docs bridge them. I also found concrete, verified silent-observability gaps (a plugin-decrypt audit emitter that drops records with zero log/metric despite its own doc comment promising both; a confirmed sloglint blind spot on the plugin-event-delivery hot path; no NATS connection-health signal post-boot) and a couple of already-tracked issues I re-confirmed rather than re-file.

**Counts:** 1 High, 4 Medium, 2 Low, plus 3 already-tracked items re-confirmed (1 Medium-equivalent, 2 Low-equivalent) and several Strengths.

## Findings

### HIGH-1 Audit-DLQ replay CLI cannot recover dead letters in the documented default deployment

- **Severity:** High
- **Claim:** In the officially documented zero-config deployment (leave `game_id` unset so it auto-generates), the server publishes DLQ entries under a subject keyed by the auto-generated `core.game_id`, but `holomush audit dlq replay` defaults to an unrelated, independently-defaulted `event_bus.game_id="main"` with no flag or documented config key to reconcile them — so every replay attempt fails with "DLQ subject does not carry the expected prefix" instead of restoring the dead letters.
- **Evidence:**
  - Two independent `game_id` settings exist. `coreConfig.GameID` (flag `--game-id`, YAML key `core.game_id`) auto-generates a ULID via `internal/store/postgres.go:96-113` (`InitGameID`) when unset — documented as the default at `site/src/content/docs/operating/reference/configuration.md:367-371` ("Empty string triggers auto-generation on first start... Default: `""` (auto-generated)"). Separately, `eventbus.Config.GameID` (YAML key `event_bus.game_id`) defaults to the literal string `"main"` (`internal/eventbus/config.go:28,42,153-154`) and is **not documented anywhere** — absent from both `configuration.md` and `external-nats-deployment.md`'s `event_bus:` key table (`site/src/content/docs/operating/how-to/external-nats-deployment.md:125-133`).
  - `cmd/holomush/core.go:300-304` resolves the real game id (`cfg.GameID`, else `dbSub.GameID()`) and uses it for the audit-DLQ subject: `cmd/holomush/core.go:561-570` — `Subject: fmt.Sprintf("internal.%s.audit.dlq", gameID)`. The surrounding comment (lines 563-566) explicitly acknowledges the split: *"Use the resolved gameID (cfg.GameID, else dbSub.GameID()) so the DLQ subject matches the rest of the process; `eventBusConfig.GameID` can still be the unresolved default. The replay CLI must target the same game_id — a mismatch fails loud (WR-06), never silently."*
  - The CLI never resolves the real game id. `cmd/holomush/cmd_audit.go:143-149` (`loadEventBusConfig`) loads only the `event_bus` section; `cmd/holomush/cmd_audit.go:333-343` (`dlqConfigForGame`) builds the replay's `DLQConfig.Subject` from that `event_bus.game_id` alone. `newAuditDLQReplayCmd` registers only `--all`/`--msg-id`/`--limit` (`cmd/holomush/cmd_audit.go:115-117`) — no `--game-id` flag exists on `replay`, `list`, or `show`.
  - The failure is at least *loud*, not silent: `internal/eventbus/audit/replay.go:198-218` (`replayOne`/`originalSubject`) detects the prefix mismatch, increments `result.Failed`, and emits `slog.WarnContext(ctx, "audit DLQ replay: DLQ subject does not carry the expected prefix; not persisted (replay game_id likely differs from capture-time game_id)", ...)` — an operator who reads the WARN log gets the right diagnosis, but nothing in the CLI or docs tells them what to *do* about it (no `--game-id` flag to set, and the value they'd need — `core.game_id` — lives in a different config section than the one the CLI reads).
  - None of the reference deployment artifacts set either key explicitly (`deploy/nats/*.conf`, `deploy/nats/README.md` grep clean for `game_id=`), so the reference/documented path reproduces this exactly.
  - `list` and `show` are unaffected (they scan `EVENTS_AUDIT_DLQ` by fixed stream name and match on the `Nats-Msg-Id` header, not on subject — `cmd/holomush/cmd_audit.go:170,207-268`) — only `replay`'s subject-recovery step is broken.
- **Impact:** An operator who follows the documented runbook (`site/src/content/docs/operating/how-to/external-nats-deployment.md:258` — `holomush audit dlq replay --all --config /etc/holomush/config.yaml`) after a real Postgres outage — precisely the scenario the DLQ exists for — gets `scanned: N, replayed: 0, failed: N` and no dead letters actually recovered, undermining the "recoverable, not data loss" promise that is the headline justification for Phase 3's audit-DLQ work. No data is actually lost (it stays safely in `EVENTS_AUDIT_DLQ`, unbounded by this bug), but the recovery tool doesn't work until the operator reverse-engineers the two-`game_id` split from source comments.
- **Recommendation:** Smallest fix: `runAuditDLQReplay` already opens a Postgres pool (`openAuditPool`, `cmd/holomush/cmd_audit.go:319`) before calling `ReplayDLQ` — have `dlqConfigForGame` fall back to `PostgresEventStore.GetSystemInfo(ctx, "game_id")` (mirroring `InitGameID`, `internal/store/postgres.go:70,97-101`) when `event_bus.game_id` is unset, so the CLI derives the same value the server did. Failing that, add an explicit `--game-id` flag to `dlq replay`/`list`/`show` and document `event_bus.game_id` in `configuration.md`. Either way, document the two-setting split in the external-NATS runbook.
- **Dedup:** none

### MEDIUM-1 Plugin-decrypt audit emitter drops records with zero log or metric, contradicting its own doc comment

- **Severity:** Medium
- **Claim:** `Emitter.drain` in `internal/eventbus/authguard/audit/emitter.go` silently discards a `PluginDecryptRecord` when the async `Publish` to the bus fails — no log line, no metric — even though the function's own comment says failures are "logged/metered."
- **Evidence:** `internal/eventbus/authguard/audit/emitter.go:196-199` documents: *"Best-effort: drain failures are logged/metered but never retry-block the subscriber path."* The actual body at lines 210-218:

  ```go
  if err := q.pub.Publish(q.drainCtx, event); err != nil {
      // TODO(metrics): increment authguard_audit_drain_failed_total
      _ = err
  }
  ```

  No `slog` call anywhere in the file (confirmed by search); the promised metric is a TODO, never implemented. Once a record is dequeued from `pq.ch` (line 210) it cannot be recovered — a failed publish is a permanent, unrecorded loss of that plugin-decrypt audit entry.
- **Impact:** `PluginDecryptRecord` is the audit trail of every time a plugin received decrypted (plaintext) sensitive payload — a compliance-relevant signal per INV-CRYPTO-28/19 (master spec §7.6). It is explicitly best-effort/fire-and-forget by design (spec §12 Q2), so occasional loss under backpressure is accepted — but *silent* loss (no operator signal at all) is a gap even within that design: an operator cannot tell the difference between "no plugin has decrypted anything" and "the decrypt-audit trail has been silently failing to publish for an hour."
- **Recommendation:** Add the counter the comment already promises (`authguard_audit_drain_failed_total`, labeled by plugin) and a `slog.ErrorContext(q.drainCtx, "plugin decrypt audit publish failed", "plugin", pluginName, "error", err)` at the existing `_ = err` site — small, in-scope, and closes the gap between documented and actual behavior.
- **Dedup:** none

### MEDIUM-2 EventBus connection health is invisible after boot — no reconnect/disconnect signal, no HealthReporter

- **Severity:** Medium
- **Claim:** Once the EventBus subsystem starts, nothing observes the underlying `*nats.Conn`'s connection state: no `DisconnectErrHandler`/`ReconnectHandler`/`ClosedHandler` is registered, nothing polls `conn.Status()`, `eventbus.Subsystem` does not implement `lifecycle.HealthReporter`, and the Prometheus NATS exporter is embedded-only (skipped entirely in external mode). An operator running Phase 3's new external/clustered NATS deployment gets zero HoloMUSH-side signal if the connection to the broker degrades or drops.
- **Evidence:**
  - `rg -n "DisconnectErrHandler|ReconnectHandler|ClosedHandler|conn.Status\(\)"` across `internal/` and `cmd/` returns zero hits (verified this session).
  - `rg -n "func.*HealthStatus\(\)"` across `internal/`/`cmd/` (excluding tests) returns only `internal/plugin/setup/subsystem.go:547` and the generic `internal/lifecycle/tracker.go:125` helper — `eventbus.Subsystem` (`internal/eventbus/subsystem.go`) has no `HealthStatus()` method and is never `Register`ed into `lifecycle.ReadinessRegistry` (`internal/lifecycle/registry.go:29-51`, whose `AllReady()` vacuously returns `true` for any subsystem that never registers).
  - `internal/eventbus/subsystem.go:103-109,221-227` (`exporterEnabled`): the Prometheus NATS exporter is gated to `Mode == ModeEmbedded` only — *"External mode has no embedded server to scrape (OQ-7)"* — by design the operator's own cluster monitoring is the intended substitute, but that only covers the broker side, not HoloMUSH's client-side connection health.
  - `internal/eventbus/natsdial.go:36-41` documents the deliberate choice not to set `RetryOnFailedConnect` at boot (fail-closed at boot is correct — see Strengths), but says nothing about what happens to visibility once a live connection later degrades; `nats.go`'s default reconnect (bounded, silent) is the only behavior in play post-boot.
- **Impact:** During an external-NATS outage or flapping connection, publishes/subscribes will start failing at the call sites (which do return errors, so callers aren't fooled locally), but there's no single "eventbus connectivity degraded" signal in logs, metrics, or `/readyz` — an operator has to infer it from a rise in scattered downstream errors rather than one clear indicator.
- **Recommendation:** Register `nats.DisconnectErrHandler`/`ReconnectHandler`/`ClosedHandler` on the connection in both `connectEmbedded`/`connectExternal` (`internal/eventbus/subsystem.go:143,208`) to log state transitions and increment a `holomush_eventbus_connection_state` gauge; have `eventbus.Subsystem` implement `lifecycle.HealthReporter` (degrade to `HealthDegraded`/`HealthStale` while disconnected) and register it with the `ReadinessRegistry` so `/readyz` reflects it.
- **Dedup:** none

### MEDIUM-3 EventBus core internals carry no OTel span instrumentation of their own

- **Severity:** Medium
- **Claim:** `internal/eventbus/{publisher,rendering_publisher,subscriber}.go`, `internal/eventbus/history/*.go`, `internal/eventbus/audit/{projection,plugin_consumer}.go`, and `internal/plugin/event_emitter.go` contain zero `tracer.Start(...)` calls — the only spans covering the EventBus's most important flows live at the outer gRPC boundary (`internal/grpc/server.go`'s `subscribe.*` spans) or in the command dispatcher; the actual publish/consume/history-query work inside the bus is an unbroken block in whichever parent span happens to be open.
- **Evidence:** `rg -n "otel\.|trace\.|Span|tracer"` against those files returns zero hits (verified this session). `internal/eventbus/telemetry/otel.go` provides `InjectHeaders`/`ExtractContext` (W3C trace-context propagation across the JetStream hop, correctly implemented) but neither function — nor any caller in the eventbus package — starts a span; propagation without a corresponding local span just re-links the *next* span (usually inside plugin delivery) to the *previous* one, with the JetStream round trip itself invisible in the waterfall. By contrast, `internal/grpc/server.go:86,824-1107` and `internal/command/dispatcher.go:208` and `internal/plugin/otel_middleware.go:111,152` do instrument their own boundaries.
- **Impact:** A trace waterfall for "why is this command slow" can show command-dispatch and plugin-delivery latency but not "how long did the actual JetStream publish/dupe-window take" or "how long did the audit-projection INSERT take relative to the consume loop" — a real diagnostic blind spot for the single largest subsystem in the codebase (19.9k LoC per the system map) when NATS-side latency (e.g., under external-cluster load) is the suspect.
- **Recommendation:** Add child spans at the JetStream boundary specifically — `eventbus.publish` around `js.PublishMsg` in `JetStreamPublisher.Publish` (`internal/eventbus/publisher.go`), and `eventbus.audit.persist` around the INSERT in `projection.persist` (`internal/eventbus/audit/projection.go:319-331`) — the two spots most likely to show real latency variance.
- **Dedup:** none

### MEDIUM-4 Confirmed sloglint blind spot: bare slog calls inside a goroutine closure on the plugin-delivery hot path

- **Severity:** Medium
- **Claim:** `internal/plugin/subscriber.go::deliverAsync` (the dispatch path for every event delivered to every plugin) uses bare `slog.Warn`/`slog.Debug`/`slog.Error` at lines 123, 130, 134 inside a `go func(){}()` closure that captures `ctx`/`tctx` — losing `trace_id`/`span_id` correlation on every plugin-delivery timeout, cancellation, or failure — and I confirmed via a scoped `task lint` run (`./bin/custom-gcl run ./internal/plugin/... ./internal/eventbus/...` → `0 issues`) that the repo's `sloglint` (`context: scope`, `.golangci.yaml:162-165`) does **not** catch this: its lexical-scope analysis apparently doesn't follow `ctx` through a goroutine closure boundary, so this class of violation is currently unenforced.
- **Evidence:**

  ```go
  // internal/plugin/subscriber.go:104-140
  func (s *Subscriber) deliverAsync(ctx context.Context, pluginName string, event pluginsdk.Event) {
      s.wg.Add(1)
      go func() {
          defer s.wg.Done()
          tctx, cancel := context.WithTimeout(ctx, 5*time.Second)
          defer cancel()
          dispatchCtx := core.WithActor(tctx, actorFromIncomingEvent(event))
          emits, err := s.host.DeliverEvent(dispatchCtx, pluginName, event)
          if err != nil {
              switch {
              case errors.Is(err, context.DeadlineExceeded):
                  slog.Warn("plugin event delivery timed out", ...)          // line 123 — bare
              case errors.Is(err, context.Canceled):
                  slog.Debug("plugin event delivery canceled", ...)          // line 130 — bare
              default:
                  slog.Error("failed to deliver event to plugin", ...)       // line 134 — bare
              }
              return
          }
  ```

  `ctx`, `tctx`, and `dispatchCtx` are all live, in-scope `context.Context` values at every one of these call sites — trivially `WarnContext(tctx, ...)` etc. Confirmed clean lint pass via a scoped `local-check` dispatch this session (transcript: `0 issues` from `./bin/custom-gcl run ./internal/plugin/... ./internal/eventbus/...`).
- **Impact:** Plugin event delivery is one of the highest-traffic flows in the system (per the container diagram: `PH --> EB`, every plugin subscription). Delivery timeouts/failures — exactly the events an operator most wants correlated to a trace — currently emit orphaned log lines with no `trace_id`/`span_id`, and the linter that's supposed to catch this class of bug (per `.claude/rules/logging.md`, a MUST) has a real, confirmed gap for goroutine-closure call sites.
- **Recommendation:** Fix the three call sites (cheap, ~3 lines). Separately, file a tooling gap: consider whether `sloglint`'s `context: scope` needs a golangci-lint version bump or a supplementary custom analyzer (the repo already ships several in `gorules/`) to catch ctx-in-enclosing-closure cases — this is likely not the only occurrence repo-wide, just the one I verified.
- **Dedup:** none

### LOW-1 Shared error-mapping helper drops ctx despite trivial availability at every call site

- **Severity:** Low
- **Claim:** `internal/world/grpc_server.go::mapWorldError(err error)` takes no `context.Context` and logs via bare `slog.Error` (lines 157, 163, 172), even though all four of its call sites (`GetLocation`, `GetCharacter`, `ListCharactersAtLocation`, and one more — `internal/world/grpc_server.go:37,55,73,+1`) are gRPC handlers with `ctx context.Context` as their first parameter, already threaded one line above into `s.svc.Get...(ctx, ...)`.
- **Evidence:** `internal/world/grpc_server.go:37-46` (`GetLocation`) — `loc, err := s.svc.GetLocation(ctx, subjectID, locID); if err != nil { return nil, mapWorldError(err) }` — `ctx` is live but not passed to `mapWorldError`. Same shape at lines 55-64 and 73-82. `mapWorldError` itself (line 154) never leaks internal error text to the client (correctly follows `.claude/rules/grpc-errors.md` — `status.Errorf(codes.Internal, "internal error")`, a static string) — the only defect is the orphaned server-side log.
- **Impact:** Every "internal error" on the WorldService gRPC boundary (a load-bearing flow — `GRPC --> WORLD` in the container diagram) logs without trace correlation, making it harder to link a client-visible `codes.Internal` back to the specific request/span in Loki/Sentry.
- **Recommendation:** Thread `ctx` into `mapWorldError(ctx, err)` and switch to `slog.ErrorContext`; this is a shared-helper pattern (see also MEDIUM-4) worth a one-time repo-wide sweep for other `func mapXError(err error)`-shaped helpers that drop ctx by construction rather than by an in-scope oversight.
- **Dedup:** none

### LOW-2 Read-modeled degrade-to-nil helpers mask transient infra errors as "unset"

- **Severity:** Low
- **Claim:** A few narrow, explicitly-documented helpers convert a lookup error into a bare `nil`/zero-value success rather than surfacing it, so a transient DB hiccup during a non-critical read is indistinguishable from "value was never set."
- **Evidence:** `internal/grpc/focus/player_prefs_adapter.go:30-38` (`SceneFocusReplayTail`) — doc comment: *"returns the player's configured value, or nil if unset or on lookup error (degrades to 'unset')"* — a `PlayerRepoReader.GetByID` failure (e.g., DB blip) is treated identically to "player never set a preference." `internal/plugin/hostcap/servers.go:1279-1296` and `internal/grpc/query_stream_history.go:546-561,563-579` (`encodeHostEventCursor`/`encodeEventCursor`/`encodePluginEventCursor`) are the same shape for pagination-cursor encoding, already tracked (see Dedup).
- **Impact:** Low — none of these are on a write/audit path, and each is explicitly documented as an accepted degradation (not an oversight). Worth a mention only because a DB blip could transiently look like "feature reset" for one request in `player_prefs_adapter.go`'s case, with no log line to distinguish the two.
- **Recommendation:** Optional: add a `slog.DebugContext` on the error path in `player_prefs_adapter.go` for diagnosability; not worth blocking on.
- **Dedup:** cursor-encode instances already-tracked:#4664 ("Code quality: magic values + silent cursor-encode failures"); `player_prefs_adapter.go` is a new observation but low enough value I'm folding it in rather than filing separately.

### Re-confirmed (already tracked, not re-filed as new)

- **Plugin-owned audit consumers have no DLQ wiring** (`internal/eventbus/audit/plugin_consumer.go::pluginConsumer.handle`, lines 317-328): unlike the host projection (`projection.go:254-298`), a plugin-audit RPC that exhausts `MaxDeliver` is never captured to `EVENTS_AUDIT_DLQ` — only a generic `slog.Default().Error(...)` (itself a bare, non-Context call) with no metric, and the message simply ages out of the source stream at `StreamMaxAge` with no durable record on the plugin side. The host-only DLQ's own doc comment anticipates this: *"The plugin audit consumer reuses this helper in a documented follow-up"* (`internal/eventbus/audit/dlq.go:100`). **Dedup: already-tracked:#4776** ("Wire audit DLQ into the plugin audit consumer", currently labeled `enhancement` — given this is a gap in the never-drop guarantee for plugin-owned audit subjects such as `plugin_core_scenes.scene_log`, I'd suggest the label undersells it relative to its host-side sibling.)
- **Nak-on-`msg.Metadata()`-error edge case in the host audit DLQ**: `internal/eventbus/audit/projection.go:269-291` — if `msg.Metadata()` itself errors on the message's final delivery attempt, the code falls through to the generic "below the cap" no-ack branch and never attempts DLQ capture for what may in fact be the last attempt. **Dedup: already-tracked:#4779** ("Audit DLQ never-drop: Nak on msg.Metadata() error on final delivery attempt", `priority::low`).
- **ABAC graceful-degradation nuance**: I independently verified the `HealthDead` tier is fail-closed (`internal/access/policy/engine.go:39-52,128-137` — `EnterDegradedMode` denies all requests with `types.EffectDefaultDeny`), which is the core safety property. The already-open #4761 ("Graceful degradation can silently mask ABAC policy failures", `security-finding`) likely concerns a more specific pre-`Dead`-tier nuance I did not independently re-derive — flagging for completeness, not as a new claim.

## Strengths

- **Audit DLQ never-drop (host path) is rigorously implemented and tested.** `internal/eventbus/audit/projection.go:254-298` never Terms a message without a successful DLQ capture first, never Naks past `MaxDeliver` (avoiding a Nak's instant-redeliver storm), and the design is bound to real tests: INV-EVENTBUS-29 (`test/integration/eventbus_external/external_boot_test.go`) and INV-EVENTBUS-30 (`internal/eventbus/audit/dlq_capture_integration_test.go`, `dlq_neverdrop_integration_test.go`) — both `binding: bound` in `docs/architecture/invariants.yaml:2620-2637`.
- **DLQ replay is idempotent, ctx-aware, and fails loud rather than silently.** `internal/eventbus/audit/replay.go` reuses the exact same `writeAuditRow` the live path uses (byte-identical persistence), respects `ctx` cancellation mid-fetch, and on a subject-prefix mismatch or persist failure counts it in `Failed` and retains the message rather than acking-and-losing it (lines 198-238) — see HIGH-1 for the separate CLI-usability gap layered on top of this otherwise-sound mechanism.
- **Boot path is genuinely fail-closed.** KEK is mandatory (`BOOT_KEK_REQUIRED`, `cmd/holomush/core.go:792-801`, no degraded KEK-less mode); external-mode NATS self-verifies its own account isn't over-scoped and refuses to boot on mismatch (`cmd/holomush/core.go:467-480`, `EVENTBUS_SCOPE_CHECK_FAILED`); `provision:false` JetStream deployments verify-and-refuse rather than silently provisioning (`internal/eventbus/subsystem.go:291-315`, `EVENTBUS_STREAM_CONFIG_MISMATCH`); external dial has no `RetryOnFailedConnect` so a bad boot fails fast instead of retry-looping silently (`internal/eventbus/natsdial.go:36-41`).
- **`Orchestrator.StartAll` rolls back cleanly on partial-start failure**, stopping already-started subsystems in reverse order (`internal/lifecycle/orchestrator.go:58-65`), and `grpcSubsystem.Stop` implements a properly bounded graceful shutdown (`GracefulStop` with a ctx-deadline fallback to hard `Stop()`, `cmd/holomush/sub_grpc.go:742-756`) — in-flight requests drain rather than being cut off.
- **AuthGuard/DEK failure modes are fail-closed, not fail-open.** `guard.Check` erroring propagates as `EVENTBUS_AUTHGUARD_CHECK_FAILED` rather than defaulting to permit (`internal/eventbus/subscriber.go:628-633`); a `WithSubscriberAuthGuard` misconfiguration without a paired `DEKManager` explicitly fails closed rather than panicking (lines 645-648).
- **The ABAC audit logger's sync/async split is a well-reasoned tiered design**: denials/`default_deny`/`system_bypass` are written synchronously with a WAL fallback and a double-failure error surfaced to the caller (`internal/audit/logger.go:161-198`); only `allow` events in `ModeAll` are best-effort-async, and even that path logs + increments a metric at every loss point (`logger.go:282-300`, `postgres.go:116-119`) rather than dropping silently.
- **`oops` discipline and the gRPC error-opacity rule are consistently followed** in every file sampled — I did not find a `status.Errorf(..., "%v", err)` leak pattern anywhere in this dimension's scope; internal errors are logged server-side and a static "internal error" is returned to clients.
- **Postgres access is OTel-traced** via `otelpgx.NewTracer()` wired into every pool (`internal/store/postgres.go:41-51`), and Sentry integration is clean, optional (DSN-gated), and correctly drains buffered events/spans on shutdown (`internal/telemetry/sentry.go`).
- **The audit-DLQ has real Prometheus coverage**: `holomush_audit_dlq_messages_total`, `holomush_audit_projection_lag_seconds`, and `holomush_audit_projection_plugin_owned_skipped_total` are all registered and wired at boot (`internal/eventbus/audit/lag_metric.go`, `cmd/holomush/core.go:525`).

## Not examined

- Did not run the full `task test:int`/`task pr-prep` suite myself — relied on reading + one scoped `local-check` `task lint` dispatch (subscriber.go/publisher.go) for the sloglint claim in MEDIUM-4.
- Did not deep-audit `internal/session/`, `internal/cluster/` invalidation internals, `internal/telnet/` gateway reliability, or the web BFF's own error/retry handling in depth — sampled only where they intersected the eventbus/audit/boot flows above.
- Did not exhaustively review all 341 registered invariants or all ~180 `binding: pending` EVENTBUS entries individually — spot-checked the two the task explicitly named (INV-EVENTBUS-29/30, both `bound`) plus a handful of adjacent ones; the broad `pending` backlog is a known, transparently-tracked state (`holomush-hz0v4.11`), not a fresh finding.
- Did not independently re-derive the specific mechanism behind already-open #4761 (ABAC graceful-degradation nuance) — only verified the `HealthDead`-tier fail-closed behavior it's presumably adjacent to.
- Did not audit retry/backoff behavior in `internal/plugin/goplugin` (binary-plugin subprocess mTLS gRPC) beyond what surfaced incidentally.
- Of the ~112 bare (non-`*Context`) `slog.*` call sites found repo-wide, I verified a representative sample (roughly a dozen) rather than all of them; several checked were legitimate no-ctx-available cases (init/callback signatures) and are not reported as findings. MEDIUM-4 and LOW-1 are the two confirmed genuine violations from that sample; more likely exist unaudited.
