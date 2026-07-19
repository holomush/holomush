---
status: resolved
trigger: "E2E regression: scene-publish-vote and pose-in-scene-log tests fail after phase 07 (event-model-bootstrap-decomposition). Bisected to commit 255c46fa6."
created: 2026-07-18T21:25:04Z
updated: 2026-07-18T22:10:00Z
---

## Current Focus
<!-- OVERWRITE on each update - always reflects NOW -->

status: RESOLVED. Root cause confirmed via direct runtime instrumentation, fix
applied and verified end-to-end (both previously-failing E2E spec files now
pass 100%). Human verification received — independently re-verified via a
separate local-check dispatch (build/scoped test/lint all PASS) plus a fresh
E2E run of the exact failing combination (3 passed, 7.1s). Archiving.

## Symptoms
<!-- Written during gathering, then immutable -->

expected: In `web/e2e/scenes.spec.ts:452` ("workspace composer submits pose and pose card appears live in the scene log"), after a registered player creates a scene via terminal, navigates to `/scenes?watch=<sceneId>`, types a pose in the workspace composer, and clicks "Send pose", the pose text MUST appear as an `<article>` in the scene log (`role="log" aria-label="scene log"`) within 15s (JetStream fan-out + decrypt + WebSocket push budget). Same class of failure in `web/e2e/scene-publish-vote.spec.ts` (both its tests): the publish-vote panel / live tally MUST appear via `scene_publish_*` event-driven refetch.

actual: The composer's "Send pose" RPC succeeds client-side (button disables, no `p.text-destructive` error rendered — i.e. the mutation was accepted), but the pose `<article>` never appears in the scene log within the 15s timeout. Playwright times out on `expect(sceneLog.locator('article').filter({hasText: poseText})).toBeVisible()`. Same shape of failure for scene-publish-vote's tally/panel.

errors: Playwright: `Error: expect(locator).toBeVisible() failed / Timeout: 15000ms / Error: element(s) not found`. Server-side (core container logs, one clean isolated local repro of `scenes.spec.ts:452` alone): a `session disconnected` INFO log for the scene-creator's session fires ~6s after scene creation (this is plausibly the EXPECTED terminal-stream teardown on `page.goto('/scenes?watch=...')` navigation, not itself a bug), followed by two `scene.service.get_scene ok` INFO lines (the workspace's cold-start load), then TOTAL SILENCE (no further scene/pose/emit log lines of any kind) until container shutdown ~15s later when the test times out. No WARN/ERROR appears for the actual pose-submit RPC or its resulting event — consistent with either (a) a silent client-to-server delivery failure, or (b) a successful server-side pose command whose resulting event is emitted on a subject the subscriber never receives (the leading theory, since Go does not log successful RPCs by default, so "no log line" cannot by itself distinguish success-but-misdelivered from never-arrived).
  A DIFFERENT local run (2 workers, `scene-publish-vote.spec.ts` + `scenes.spec.ts:452` together) additionally surfaced `SCENE_AUDIT_ACCESS_DENIED — non-member` from `plugins/core-scenes/audit.go:551` for what looked like the scene owner's own character — but this run mixed in `scene-publish-vote.spec.ts`'s OBSERVER sub-test (which is SUPPOSED to get PermissionDenied on `GetPublishedScene` — that's the test's whole point), so this specific log line is very likely a false lead / correctly-denied-observer artifact of running two spec files with 2 parallel workers, not a real second bug. Confirmed as a false lead — root cause below is unrelated to this log line.

reproduction: |
  cd <repo-root-or-worktree-at-the-target-commit>
  task test:e2e -- e2e/scenes.spec.ts:452
  # or: task test:e2e -- e2e/scene-publish-vote.spec.ts
  # Reproduces 100% locally (2/2 runs) and in CI (2/2 runs) at branch HEAD
  # (gsd/phase-07-event-model-bootstrap-decomposition, PR #4823).
  # Passes 100% on origin/main (f063b8045) — confirmed via a throwaway
  # `git worktree add -f /tmp/holomush-main-baseline origin/main` + same command (1 passed, 6.9s).
  # To capture full server logs (they are lost once `task test:e2e`'s
  # `defer: docker compose ... down -v` fires): run
  #   docker compose -p holomush-e2e -f compose.yaml -f compose.e2e.yaml logs -f core gateway > /tmp/compose-logs.txt
  # concurrently in a second shell right after containers become healthy
  # (poll `docker ps --filter name=holomush-e2e-core-1` until non-empty),
  # since task test:e2e's own defer tears everything down within ~1s of the
  # test run finishing.

started: |
  Bisected via `git worktree add -f <path> <commit>` + `task test:e2e -- e2e/scenes.spec.ts:452` at each phase-07 wave boundary (each a full Docker rebuild + real E2E run, ~3-5 min per point):
    07-05 end   (0b114f5f1) -> PASS
    07-06 end   (b30526520) -> PASS
    07-07 end   (06a783358) -> PASS
    07-08 end   (ee987f6e6) -> PASS
    07-09 mid   (932390dd1, "build the TLS subsystem", before eager-starts removed) -> PASS
    07-09 end   (255c46fa6, "kill the five eager starts and register TLS via the named subsystem set") -> FAIL  <-- introduced here
  255c46fa6 is a single large atomic commit (45 files, +1769/-850) — could not
  bisect further with git (it's the actual unit of work, not further splittable
  without breaking compilation per the plan's own atomic-commit design). The
  commit's own message says: "gameIDProvider is the single game-id resolution +
  override closure. TLS, World, Plugin, the outbox relay, Cluster, the audit DLQ
  subject, and the EventBus itself all resolve through it at their own Start
  instead of a hand-sequenced pre-start" — replacing what used to be ONE
  eagerly-computed `gameID` string passed explicitly into every consumer at boot,
  with N independent lazy resolutions via `gameIDProvider()` / `bus.GameID()`
  calls scattered across each subsystem's own `Start()`.
  CONFIRMED (see Evidence below): the commit's own message is the key clue —
  "the EventBus itself" now ALSO resolves through gameIDProvider, meaning
  BEFORE 255c46fa6 the EventBus subsystem's GameID field was NEVER wired to the
  DB-resolved id at all; it stayed at `eventbus.Config.Defaults()`'s literal
  `"main"` for the whole process lifetime. This accidentally matched a
  pre-existing, unrelated piece of tech debt in two binary plugins
  (core-scenes, core-channels) that hardcode their OWN emitted-subject game id
  to the literal `"main"` (both have a comment: "ServiceConfig does not
  currently carry a game_id field; this hardcode is the documented expedient").
  255c46fa6 correctly fixes the EventBus's own GameID resolution (now a real
  per-database ULID from `store.InitGameID`) — but in doing so, it exposes the
  plugins' hardcode: plugins now publish on `events.main.scene.<id>...` while
  the host's CoreServer.Subscribe (and every other host-side gameID consumer)
  correctly filters on `events.<real-game-id>.scene.<id>...`. Permanent, silent
  subject mismatch — JetStream never routes non-matching-subject messages, so
  neither side logs an error.
  NOTE: 07-11 (ecbefb990, "split Start into Prepare/Activate across all 17
  subsystems") lands AFTER 255c46fa6 and is NOT implicated by this bisection —
  confirmed correct; see Eliminated.

## Eliminated
<!-- APPEND only - prevents re-investigating -->

- hypothesis: CI-only flakiness / testcontainer or Docker-under-load environment noise, not a real code defect (per the repo's own "check whether the parent commit's CI also failed the same way" convention for diagnosing CI-only integration failures).
  evidence: Reproduced 4/4 across two independent environments — 2x GitHub Actions CI runs (including a fresh re-run after the first) AND 2x local Docker runs on the developer machine — always the exact same 3 tests (scene-publish-vote x2, scenes.spec.ts:452), and the identical spec passed 1/1 on a clean `origin/main` checkout under the identical local Docker/task test:e2e harness. Environment-only flakiness would not produce this consistent, branch-correlated a/b split.
  timestamp: 2026-07-18T21:15:00Z

- hypothesis: The `internal/web/handler.go` heartbeat-interval rename (`session.DefaultLeaseRefreshInterval` -> `sessionlease.DefaultRefreshInterval`, landed in 07-03/07-01 leaf extractions) silently changed the session-lease timeout value, causing premature session eviction.
  evidence: `rg -n "DefaultLeaseRefreshInterval" origin/main` shows the original constant was `15 * time.Second` (`internal/session/reaper.go:25` on main); the new `internal/sessionlease/sessionlease.go:22` constant is also `15 * time.Second` — byte-identical value, pure rename. Also eliminated on bisection grounds: 07-01/07-02/07-03 boundaries all PASS.
  timestamp: 2026-07-18T21:05:00Z

- hypothesis: `internal/plugin/event_emitter.go`'s switch from a raw `eventbus.Event{}` struct literal to `eventbus.NewEvent(...)` (07-07, WR-02) changed the Timestamp precision (`time.Now()` vs `time.Now().UTC()`) or otherwise altered the wire-encoded AAD/subject in a way that broke delivery.
  evidence: crypto-reviewer (dispatched separately for this PR's security gate) verified this is AAD-neutral — `timestamppb.New` encodes Unix seconds+nanos, location-independent, no marshaled-byte change. Also eliminated on bisection grounds: 07-07 boundary (06a783358) PASSES — this exact commit is the LAST commit of the 07-07 wave and includes this exact change, and it still passes.
  timestamp: 2026-07-18T21:12:00Z

- hypothesis: The actor-bridge collapse (07-07, three duplicate `core.Actor -> eventbus.Actor` bridges collapsed to `plugins.coreActorToEventbusActor`) introduced a wrong Kind/ID mapping that breaks scene-membership checks (`SceneAuditServer.QueryHistory`'s caller-derived membership lookup) or actor stamping on emitted events.
  evidence: `internal/grpc/query_stream_history.go` (caller construction: `eventbus.Actor{Kind: ActorKindCharacter, ID: info.CharacterID}`), `internal/eventbus/publisher.go` (`ActorToProto`/`ActorKindToProto`), and `internal/eventbus/audit/plugin_router.go` (`eventbus.ActorToProto(q.Caller)`) are ALL completely unchanged in this PR's diff (`git diff --stat origin/main...HEAD` shows zero hits for these three files). Also eliminated on bisection grounds: 07-07 boundary PASSES.
  timestamp: 2026-07-18T21:10:00Z

- hypothesis: The 07-11 Prepare/Activate two-sweep barrier (`ecbefb990`) shifts request-handling timing (e.g. gRPC starts serving at a different relative moment) such that a request arrives before some dependency is fully warm.
  evidence: Bisection shows the failure is ALREADY present at 255c46fa6, which lands BEFORE 07-11/ecbefb990 in the commit sequence and still uses the old single-phase `Start()` model (Prepare/Activate doesn't exist yet at this commit). The regression cannot be caused by a mechanism that doesn't exist yet at the point it first reproduces. RE-CONFIRMED at HEAD: read `internal/eventbus/subsystem.go`'s current Prepare/Activate split — GameIDProvider resolution runs at the TOP of `Prepare()` (before `connect()`/`EnsureStream()`), and `internal/lifecycle/orchestrator.go`'s `StartAll` runs sweep 1 (ALL Prepares, in topological order) fully before sweep 2 (ALL Activates) — `TestStartAllRunsAllPreparesBeforeAnyActivate` pins this. `grpcSubsystem.DependsOn()` includes `SubsystemEventBus`, so topoSort orders EventBus.Prepare() before grpcSubsystem.Prepare() even within sweep 1. Every host-side gameID consumer (`s.cfg.EventBus.GameID`/`GameID()`, whether captured as a method value or called immediately at wiring time) therefore reads the fully-resolved value correctly, at every call site traced (`cmd/holomush/sub_grpc.go` lines 358, 372, 495, 543, 598, 609, 638, 671, 693; `internal/grpc/server.go` currentGameID/toSubject call sites). No timing/ordering divergence exists among HOST-side consumers — they all funnel through the identical resolved `eventbus.Subsystem.cfg.GameID` field. The real divergence (see Resolution) is NOT a host-side timing race at all — it's a plugin-side hardcoded literal that never consults gameIDProvider in the first place.
  timestamp: 2026-07-18T21:24:00Z / re-confirmed 2026-07-18T21:50:00Z

- hypothesis: `internal/grpc/server.go`'s `applyFilterCtrl` WARN ("plugin attempted to modify location stream — rejected", observed in fresh log capture at `session_id=<terminal session>, stream=location.<starting-location-id>`) indicates a plugin/host bug in the mid-session stream-filter-update path that drops the scene-focus ctrl update.
  evidence: `internal/grpc/server.go` (where `applyFilterCtrl` lives) is NOT in 255c46fa6's changed-file list — this code and its guard behavior predate the regression entirely. Direct instrumentation (see Evidence) shows the SCENE stream ctrl updates (`scene.<id>.ooc`, `scene.<id>.ic`) succeed and are correctly added to the session's filter set with the correct game id — this WARN is an unrelated, pre-existing, harmless artifact of the location-follow mechanism firing once per session teardown/reattach and is NOT on the critical path for pose/publish-vote delivery.
  timestamp: 2026-07-18T22:00:00Z

## Evidence
<!-- APPEND only - facts discovered during investigation -->

- timestamp: 2026-07-18T21:00:00Z
  checked: `origin/main` (f063b8045) vs branch HEAD (8581bdd3c, PR #4823) — ran `task test:e2e -- e2e/scenes.spec.ts:452` on both via disposable `git worktree add -f`.
  found: main = 1 passed (6.9s); branch HEAD = 1 failed (composer submits, pose never appears in log, 15s timeout).
  implication: Definitively a regression introduced somewhere in phase 07's 11-plan diff, not pre-existing flakiness.

- timestamp: 2026-07-18T21:20:00Z
  checked: Bisected via disposable worktrees at each of the 11 plans' wave-boundary commits, running the identical isolated E2E command at each point (see `started` field above for the full table).
  found: PASS through 07-08 end (ee987f6e6); PASS at 07-09's first commit (932390dd1, TLS subsystem scaffolded but not yet wired in); FAIL at 07-09's second/last commit (255c46fa6, "kill the five eager starts").
  implication: The regression is contained entirely within one large atomic commit's diff (45 files). Focus all further investigation there — do not re-examine 07-01 through 07-08 or 07-10/07-11.

- timestamp: 2026-07-18T21:22:00Z
  checked: `git show 255c46fa6 -- internal/eventbus/config.go internal/eventbus/subsystem.go` and the opening ~200 lines of `git show 255c46fa6 -- cmd/holomush/core.go`.
  found: New `Config.GameIDProvider func() string` (koanf-excluded) on `eventbus.Config`, resolved once at the top of `Subsystem.Start` — "An empty resolve keeps whatever Defaults()-substituted (or koanf-loaded) value is already in s.cfg.GameID". `EventBus.DependsOn()` changed `nil` -> `[SubsystemDatabase]`. In `cmd/holomush/core.go`, the old eager `gameID := cfg.GameID; if gameID == "" { gameID = dbSub.GameID() }` (computed ONCE, passed explicitly everywhere) is replaced by a `gameIDProvider := func() string { ... return dbSub.GameID() }` closure that "TLS, world, plugin, outbox relay, cluster, audit DLQ, the EventBus itself, and the cryptoWiring block" all now call independently, each at their own Start.
  implication: This is the mechanical shape of a classic "single source of truth split into N independent lazy resolvers" refactor. The commit message's own phrase "the EventBus itself" (implying it did NOT resolve through gameIDProvider before) turned out to be the load-bearing clue — see below.

- timestamp: 2026-07-18T21:50:00Z
  checked: Ran the isolated E2E repro (`task test:e2e -- e2e/scenes.spec.ts:452`) with `docker compose logs -f core gateway` captured concurrently, plus the Playwright trace.zip (`0-trace.network`) from the failed run — extracted the actual `SendCommand` request body and confirmed the "pose" command WAS sent successfully (`{"success":true}`) with the correct sessionId/connectionId. Confirms client-side delivery is NOT the failure — the RPC reaches the server and succeeds.
  found: `SendCommand` postData: `{"sessionId":"...","text":"pose PoseE2E-...","connectionId":"..."}` -> response `{"success":true}`. Server logs show a `WARN "plugin attempted to modify location stream — rejected"` around the same window, later eliminated as unrelated (see Eliminated). No further server-side log lines for the pose after acceptance (expected — Go doesn't log successful command handling by default).
  implication: Confirms the divergence must be server-internal (subject mismatch), not a client-side or RPC-level failure. Motivated direct runtime instrumentation as the next step.

- timestamp: 2026-07-18T22:00:00Z
  checked: Added temporary `slog.InfoContext` DEBUGTRACE lines at (a) `internal/plugin/event_emitter.go::Emit`, right after `eventbus.Qualify(gameID, subjectRaw)`, logging `raw_subject`/`game_id`/`qualified_subject`; (b) `internal/grpc/server.go::Subscribe`, right after `computeInitialFilters`, logging the initial filter set; (c) `internal/grpc/server.go::applyFilterCtrl`, on the success path, logging the qualified subject added to the filter set. Rebuilt (`task build`), re-ran the isolated E2E repro with fresh log capture.
  found: DECISIVE. Subscribe-side filter set (via `applyFilterCtrl`, scene-focus ctrl update) correctly contains `events.01KXVJHWPYXZ9NGPBC3V0C9WD0.scene.01KXVJJ566P4J3BCW4JVGER7B6.ic` — the REAL DB-resolved game id (`01KXVJHWPYXZ9NGPBC3V0C9WD0`). But the emit-side trace for the actual pose event (`event_type=core-scenes:scene_pose`) shows `raw_subject=events.main.scene.01KXVJJ566P4J3BCW4JVGER7B6.ic` -> `qualified_subject=events.main.scene.01KXVJJ566P4J3BCW4JVGER7B6.ic` — IDENTICAL to the raw subject, because `eventbus.Qualify()` (internal/eventbus/qualify.go:27-29) treats any string ALREADY starting with `"events."` as fully-qualified and returns it VERBATIM, ignoring the `gameID` parameter entirely. The plugin (core-scenes) is constructing its OWN fully-qualified subject string with a HARDCODED literal `"main"` as the game-id token — `eventbus.Qualify`'s correctly-resolved real game id (`01KXVJHWPYXZ9NGPBC3V0C9WD0`, confirmed in the same log line's `game_id=` field) never gets substituted in, because the plugin's subject already "looks" qualified.
  implication: ROOT CAUSE FOUND. Traced to `plugins/core-scenes/main.go:255`: `p.service.gameID = "main"` — a documented, pre-existing hardcode ("ServiceConfig does not currently carry a game_id field; this hardcode is the documented expedient until multi-tenant deployment is real"), used by `plugins/core-scenes/store.go:2030` and `plugins/core-scenes/service.go:641,670` to build subjects as `"events." + s.gameID + ".scene." + sceneID + ...`. Grep confirmed `plugins/core-channels/main.go:298` has the IDENTICAL hardcode + identical comment ("mirrors core-scenes main.go"). Before 255c46fa6, `eventbus.Config.Defaults()`'s `defaultGameID = "main"` (internal/eventbus/config.go:28) was NEVER overridden by the DB-resolved id anywhere in the EventBus subsystem itself (confirmed: the commit's own message — "the EventBus itself" now ALSO resolves through gameIDProvider — is literally documenting this exact gap being closed), so `s.cfg.EventBus.GameID()` returned `"main"` for the whole process, accidentally matching the plugins' hardcode. 255c46fa6 correctly fixes EventBus's own resolution (now a real per-database ULID via `store.InitGameID`, confirmed unchanged/pre-existing at `internal/store/postgres.go:97-113`), which is CORRECT and INTENDED (multi-tenant readiness) — but it exposes the plugins' stale hardcode as a permanent publish/subscribe subject mismatch.

- timestamp: 2026-07-18T22:15:00Z (post-checkpoint, human-verify)
  checked: User independently re-verified via a separate `local-check` dispatch (task build, scoped task test 3637/3637, task lint) and re-ran `task test:e2e -- e2e/scene-publish-vote.spec.ts e2e/scenes.spec.ts:452` themselves.
  found: 3 passed, 7.1s — matches the exact spec-file combination that failed in the original CI run.
  implication: Fix confirmed end-to-end by an independent verification pass, not just the debugger's own self-check. Checkpoint response: "confirmed fixed."

## Resolution
<!-- OVERWRITE as understanding evolves -->

root_cause: |
  Two binary plugins (`plugins/core-scenes`, `plugins/core-channels`) construct
  their own fully-qualified NATS/JetStream subjects directly (e.g.
  `"events." + s.gameID + ".scene." + sceneID + ".ic"`) using a game id that
  was HARDCODED to the literal `"main"` at Init, because `pluginv1.ServiceConfig`
  (the binary-plugin Init RPC payload) carried no `game_id` field for the
  plugin to read the host's real resolved value from — a pre-existing,
  explicitly-documented gap ("ServiceConfig does not currently carry a
  game_id field; this hardcode is the documented expedient").

  Before commit 255c46fa6 (07-09, "kill the five eager starts"), the EventBus
  subsystem's own `GameID` field was NEVER wired to the DB-resolved game id
  either — it stayed at `eventbus.Config.Defaults()`'s literal `"main"` for
  the whole process. This accidentally matched the plugins' hardcode, so
  publish and subscribe subjects always agreed by coincidence.

  255c46fa6 correctly wires the EventBus subsystem to resolve the REAL,
  per-database game id (a ULID from `store.InitGameID`) via the new
  `gameIDProvider` closure — this is the CORRECT, intended fix for a
  different, legitimate problem (multi-tenant readiness). But it broke the
  accidental agreement: the host side (CoreServer's Subscribe/OpenSession
  filters, and every other host-side gameID consumer) now correctly uses the
  real per-database game id, while the two plugins keep publishing on
  `events.main.scene.<id>...` / `events.main.channel.<id>...`. Because
  `eventbus.Qualify()` treats any subject already starting with `"events."`
  as pre-qualified and passes it through verbatim (no game-id substitution),
  this mismatch is permanent and silent — JetStream simply never routes a
  published message to a subscriber filter that doesn't match; neither
  publisher nor subscriber sees an error.

fix: |
  1. Added a `game_id` field to `pluginv1.ServiceConfig`
     (`api/proto/holomush/plugin/v1/plugin.proto`) — the host's resolved game
     id for this boot, for plugins that build fully-qualified subjects
     directly. Regenerated via `task proto`.
  2. `internal/plugin/goplugin/host.go`'s Init-request construction now
     populates `Config.GameId: h.gameID` — the SAME gameIDProvider-resolved
     value the binary host already threads through `goplugin.WithGameID(...)`
     (wired since 255c46fa6 itself, at `internal/plugin/setup/subsystem.go`)
     for mTLS cert SANs; it was simply never plumbed into the Init payload.
  3. Added `pluginsdk.ResolveGameID(config *pluginv1.ServiceConfig) string`
     (`pkg/plugin/config.go`) — returns `config.GetGameId()` when set, falling
     back to `"main"` (eventbus.Config's own default) when nil/unset, so test
     harnesses constructing `ServiceConfig` directly keep working unchanged.
  4. `plugins/core-scenes/main.go` and `plugins/core-channels/main.go`: replaced
     the hardcoded `p.service.gameID = "main"` with
     `p.service.gameID = pluginsdk.ResolveGameID(config)`.

  TDD: wrote `TestResolveGameIDReturnsHostSuppliedValue` /
  `TestResolveGameIDFallsBackToMainWhenUnset` /
  `TestResolveGameIDFallsBackToMainWhenConfigNil` in `pkg/plugin/config_test.go`
  BEFORE implementing `ResolveGameID` — confirmed RED (`undefined: ResolveGameID`,
  compile failure) via `task test -- -run TestResolveGameID ./pkg/plugin/`, then
  implemented the function and confirmed GREEN (3/3 passed).

verification: |
  1. Unit tests: `task test -- ./pkg/plugin/... ./internal/plugin/...
     ./internal/grpc/... ./plugins/core-scenes/... ./plugins/core-channels/...`
     — 3637 tests, all pass, no regressions.
  2. `task build` — compiles clean (host binary + embedded web build).
  3. `task plugin:build-all` — all three binary plugins (core-channels,
     core-scenes, test-abac-widget) compile clean for all three target
     platforms.
  4. `task lint` — full lint suite green (proto doc-comment gate
     `task lint:proto` specifically verified for the new `game_id` field).
  5. End-to-end (the actual regression repro): removed the temporary
     DEBUGTRACE instrumentation, rebuilt, and re-ran BOTH previously-failing
     isolated E2E specs against a fresh Docker stack:
       `task test:e2e -- e2e/scenes.spec.ts:452` -> 1 passed, 6.9s (matches
         the origin/main baseline timing exactly — was timing out at 15s+
         before the fix).
       `task test:e2e -- e2e/scene-publish-vote.spec.ts` -> 2 passed, 6.0s
         (both sub-tests, including the observer-denial sub-test).
  6. Independent human re-verification (post-checkpoint): separate
     `local-check` dispatch (build/scoped-test/lint all PASS) plus a fresh
     `task test:e2e -- e2e/scene-publish-vote.spec.ts e2e/scenes.spec.ts:452`
     run — 3 passed, 7.1s, the exact combination that failed in the original
     CI run. User confirmed: "confirmed fixed."

files_changed:
  - api/proto/holomush/plugin/v1/plugin.proto
  - pkg/proto/holomush/plugin/v1/plugin.pb.go (generated, task proto)
  - web/src/lib/connect/holomush/plugin/v1/plugin_pb.ts (generated, task web:generate)
  - pkg/plugin/config.go
  - pkg/plugin/config_test.go
  - internal/plugin/goplugin/host.go
  - plugins/core-scenes/main.go
  - plugins/core-channels/main.go
