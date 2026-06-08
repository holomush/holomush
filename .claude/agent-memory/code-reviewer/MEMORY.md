- **Invariant-registry family-renumber series (.14.12–.14.27, epic hz0v4) — CONSOLIDATED.** Each leg
  renames a legacy family (GW/ROPS/P7/P4-6/FS/SCENE/PLUGIN/ACCESS/CRYPTO/S*/M*/COMMAND) → canonical
  `INV-<SCOPE>-N`. Recurring HOLDS: (1) residual walk `bareInvRE=\bINV-\d+\b` matches ONLY bare NUMERIC
  `INV-9` — NOT `INV-P7-1`/`INV-PLUGIN-22` (the `-XX-` breaks `\d+`); `continue`s on shared files →
  foreign/deferred tokens survive ONLY in shared_files, descriptive `INV-P7-1..16` prose is inert.
  (2) checkProvenance greps CANONICAL `e.ID` at each ref site, NOT `r.Token`; legacy `token:"INV-15"`
  refs are the standing FROM-anchor convention (canonical id must ALSO be physically present).
  (3) PER-SITE not per-number: same bare number → different outcome per file. (4) Generated artifacts
  (.pb.go/_pb.ts/grpc-api.md) must regen when a proto INV-comment is renamed.
- **CRITICAL family-migration lesson (.14.23 NOT READY): registry guards are BLIND to UNREGISTERED
  files.** A fully-green run (lint:invariants, provenance, partition, binding, units, int-compile) does
  NOT prove a migration is complete: residual bare `INV-F*` tokens in files neither owned_paths nor
  shared_files nor any `refs[]` → never scanned. Also range-rewrite corruption (`INV-P7-1..16` →
  `INV-CRYPTO-38..16`, substring keeps provenance green). ALWAYS `rg -c '\bINV-<OLD>[0-9]' --glob
  '!docs/**'` over the WHOLE tree; `rg 'INV-<SCOPE>-[0-9]+\.\.[0-9]+'` for range corruption;
  `rg 'TestINV_<OLD>[0-9]'` for dangling renamed-func cites.
- **Verification-BINDING backfill (pending→bound flip) — hz0v4 READY.** Flips registry entries
  pending→bound + adds asserted_by ONLY where `// Verifies: INV-<id>` ALREADY exists. Meta-test does
  NOT cross-check asserted_by against annotation sites → typo'd/fabricated path passes. MUST hand-verify
  each asserted_by file genuinely contains the annotation. For bug-closing flips (false-green fixes),
  READ the cited test + confirm a REAL runtime assertion of EACH invariant clause. `pending` MUST NOT
  carry asserted_by.

- **NEW shape — wire event-type qualification migration (bare scene_* → core-scenes:<verb>),
  aneim Phase 1 / holomush-r0kup READY.** Distinct from the .14.x renumbers. Three vocabularies,
  DIFFERENT rules: (1) registered-emit set (main.go phase4/phase6EmitTypes) + (2)
  crypto.emits[].event_type MUST stay BARE (INV-PLUGIN-32 set-equality + splitQualifiedRef);
  (3) wire type + verbs[].type MUST be qualified `<plugin>:<verb>`. So per-SITE judgement, not
  per-token: bare main.go/crypto.emits/main_test.go assertions are CORRECT, not misses. Checklist
  that held: (a) whole-tree `rg scene_pose|scene_say|...` ex-core-scenes/docs — only proto
  doc-comments, generated *.pb.go/_pb.ts, crypto-bridge unit tests, and raw-bus `mintEvent`/
  `eventbus.Type` synthetic integration tests (published via `bus.Bus.Publisher()` NOT
  RenderingPublisher → bypass verb registry, self-consistent bare). (b) ALL scene_log INSERTs incl
  SQL single-quote literals (seedScenePoseLog, poseorder `type='...'`, publish_store.go `WHERE
  type IN (...)`) — silent zero-row risk; both late-found literals qualified. (c) audit dispatch
  `eventType := row.GetType()` (qualified stored) vs `if eventType == "core-scenes:scene_pose"` —
  match. (d) handleEmit `strings.TrimPrefix(eventType,"core-scenes:scene_")` clean for
  pose/say/emit/ooc; `verb` only feeds user output, wire `Type` stays qualified. (e) DOWNGRADE
  FENCE NON-DEFEAT: `cryptowiring.AlwaysSensitiveSet` already PREFIXES bare crypto.emits with
  `<plugin>:`, so fence `alwaysSensitive` was always qualified — qualifying scene_log.type CLOSES
  a pre-existing keying gap (row.GetType() now matches), does not open fail-open; fenceCheckRow
  only consults alwaysSensitive on identity-codec rows (encrypted use DEK-exists). (f)
  `emitEntryMatchesWireType` (crypto_manifest.go:89) bridges bare entry ↔ qualified wire for
  LookupEmitSensitivity + PluginCanReadBack — emit/readback unaffected. (g) harness regression
  validity: SAME verbRegistry instance wrapped into crypto RenderingPublisher AND populated by
  plugin loader from manifest (harness.go:333→347 & →376→plugins.go:311); EmitSceneICContent's
  internal require.NoError surfaces EMIT_UNKNOWN_VERB so the spec genuinely fails w/o the verbs
  block. (h) harness emit helper qualifies bare→qualified IDEMPOTENTLY (`if !strings.Contains
  (wireType,":")`), so untouched tests passing bare verbs still work; readback/privacy tests
  already used `pluginName+":scene_pose"`. (i) manifest enums closed (manifest.go:176-187):
  category{communication,movement,state,system,command}, format{speech,action,narrative,
  notification,error,snapshot,delta}, speech needs label. Design: IC content (pose/say/emit/ooc)→
  communication; lifecycle/publish notices→system+notification. No false-green (no `// Verifies:
  INV-PLUGIN-40`; loader-gate aneim.10 + meta-test aneim.11 deferred). 2026-06-07 — READY.

- **Gate-removal (delete WithCryptoEnabled emit fence gate, run fence unconditionally) —
  holomush-dj95.3 READY.** Deleting a runtime safety-gate is only safe once EVERY emit path
  that the now-unconditional check could reject is already compliant. Review pattern that held:
  (1) Completeness: `rg WithCryptoEnabled|cryptoEnabled` whole-tree — plugins-package gate fully
  gone; SEPARATE `internal/grpc/` read-side gate (server.go:178, auth_handlers.go:156) is a
  DISTINCT Phase-3b binding-lookup gate that MUST remain (don't flag). (2) Production-safety is
  the crux, NOT completeness: enumerate ALL `sensitivity: always` manifest entries
  (`rg always plugins/*/plugin.yaml`) and verify each emit SITE claims Sensitive=true — core-scenes
  IC content commands.go:1325 (Sensitive:true), core-communication Lua page/whisper/pemit main.lua
  carry `sensitive = true` (guarded by lua/corecomm_sensitive_emit_test.go, the 50zqs regression).
  (3) Undeclared-event safety: LookupEmitSensitivity (crypto_manifest.go:66) defaults
  SensitivityNever for nil-manifest AND unmatched type → EnforceSensitivity(never,false)=never,nil →
  Sensitive=false, IDENTICAL to pre-gate behavior; only NEW rejection is over-claim (claim=true on
  never/undeclared) which no production path does. (4) Precursor dep (50zqs, SDK Sensitive plumbing)
  CLOSED; proto sensitive=4 + bidirectional event_marshal.go + Lua flush `sensitive` key all landed.
  (5) Bead acceptance criterion `rg ... internal/ → zero hits` is STALE (grpc gate legitimately
  remains) — note for close, not a code defect. (6) Minor: package-doc wire_crypto.go:6-10 retains
  cfg.Crypto-gated framing predating the change (DEK-wiring clause still accurate); function-doc
  37-43 is the authoritative+correct one. `_ eventbus.Config` unused param: no lint risk (unparam
  skips exported funcs; `_` signals intent). 746 unit tests green. 2026-06-07 — READY.

- **Docs-only ADR-capture branch: ALWAYS check `@` is empty before verdict (holomush-5rh.8
  NOT READY, 2026-06-07).** Two recurring traps: (1) `task pr-prep`/fmt runs AFTER the commits
  leave license-eye SPDX headers + yamlfmt normalization (docs/** IS in .licenserc.yaml paths;
  .yamlfmt has no docs/ exclude, Taskfile.yaml:528 `yamlfmt -lint .`) sitting UNCOMMITTED in `@`
  — pr-prep validates the jj SNAPSHOT (includes @), the push unit (@-) fails CI. `jj st` +
  `jj diff -r @` is mandatory; reading files with cat/Read shows the @-fixed state, NOT what
  ships — compare `jj file show -r @-` vs main sibling for header checks. (2) Spec revised
  during plan grounding (V-resolutions) leaves STALE pre-revision instructions in §4/§5.2
  tables contradicting the freshly captured ADR (spec:62,97 "Implement ReadSceneLog" vs ADR
  pc3bg "No plugin-side ReadSceneLog exists" vs spec's own D8/V6) — grep the spec for every
  mechanism an ADR says was REJECTED. Also: probe index missed `ReadSceneLogForSnapshot`
  (publish_store.go:632); confirm probe zero-results with rg before claiming absence.

- **Plugin migrations: `.down.sql` is NEVER executed** — `RunMigrationsFS` filters `.up.sql` only
  (pkg/plugin/storage/storage.go:68) and embed is `migrations/*.up.sql` (core-scenes store.go:50).
  Downs are manual-rollback docs; "reversible" is inspection-only. Also: `DROP CONSTRAINT IF EXISTS
  <wrong-name>` silently no-ops and `ADD` then stacks a 2nd constraint, leaving the old tight CHECK
  live — test:int can't catch this until something INSERTs the newly-admitted value. PG auto-name
  for inline column CHECK = `{table}_{column}_check` (verified: neon docs + ChooseConstraintName).
  Encountered: 5rh.8.1 (2026-06-07) — READY.

- **Store-layer observer support (5rh.8.2 NOT READY, 2026-06-07).** Recurring shapes: (1) When a
  diff WIDENS an emit guard (`result == A || B || NEW`), READ the emit helper body — payload
  mapping written for the old result set silently misreports the new one (emitSceneJoinIC
  `fromRole` only maps OpPromoted→"invited"; ParticipantUpgraded fell to "none"). (2)
  SELECT-then-INSERT inside a tx is NOT race-safe even with FOR SHARE on the parent row —
  FOR SHARE locks are mutually compatible and don't serialize child-table inserts; with
  PK(scene_id,character_id) (migration 000003:16) concurrent duplicates → PK violation instead
  of the documented idempotent result. AddParticipant's ON CONFLICT is the repo-correct shape.
  (3) Pre-upsert plain SELECT for prior-role classification races with concurrent inserts —
  needs FOR UPDATE or RETURNING-based classification. (4) Check bead acceptance verbatim against
  the diff: "SceneInfo.observers populated" unmet (rowToProto sets neither participants nor
  observers; new store method had zero production callers) — implementer claims of "deferred by
  design" need a recorded deferral in bead/plan, not just assertion.

- **WatchScene RPC (5rh.8.3 NOT READY, 2026-06-07).** (1) `focus.Coordinator.JoinFocus` is NOT
  idempotent — duplicate membership errors `FOCUS_ALREADY_MEMBER` (focus/join.go:38-42); repo
  precedent commands.go:832-845 special-cases it. Any handler claiming "JoinFocus is idempotent"
  is wrong; fakeFocusClient returns nil on duplicates → false-green idempotency tests. ALWAYS
  read the real coordinator + check joinErr code handling. (2) Premature partial binding:
  multi-clause invariant flipped bound when only one clause has tests and the proving task
  (.8.4 role-gates) is still open; plan scheduled flip for final Task 20 — check WHICH task owns
  the registry flip. (3) `hostEvaluateClient.Evaluate` needs the x-holomush-emit-token, minted
  ONLY in DeliverEvent/DeliverCommand (host.go:854,929); a plugin service-RPC handler calling
  Evaluate is unreachable from a plain registry-conn caller (facade) — EMIT_TOKEN_MISSING.
  Subject is token-derived dispatch actor, never bound to req.character_id. Cross-task risk to
  flag whenever a SceneService RPC consults s.evaluator.

- **5rh.8.21 RESOLVES the facade EMIT_TOKEN_MISSING gap (prev entry) — READY (2026-06-07).**
  `Host.BeginServiceDispatch` (goplugin/host.go:968) mints a dispatch token for caller-supplied
  actor+ownerPlayerID, returns ctx w/ token + advisory actor md (same coreActorKindToSDK as
  DeliverCommand:936) + release func (`Revoke` = map delete, idempotent; 5min TTL sweeper backs
  forgotten release; store terminal-on-close + Close clears items). Manager routes via
  `findOptional[ServiceDispatcher]` (Unwrap-chain capability, matches PluginAuditClientProvider
  shape) → typed SERVICE_DISPATCH_UNSUPPORTED for Lua host. Binary-only is transport-specific,
  NOT a symmetry violation (Lua serves no gRPC). WatchScene advisory check (service.go:878) is
  restriction-only: rejects PermissionDenied iff incoming md kind==character && id!=req.character_id;
  absent/non-character md proceeds (token-derived subject stays authoritative) — correct
  defense-in-depth polarity; spoofed md can only restore baseline, never grant. Pattern to
  re-check on .8.11 facade: caller MUST pass server-side-verified actor; token outlives plugin
  unload until release/TTL (Low, accepted).

- **Harness event-path parity (5rh.8.4, 2026-06-07): production busEventAppender publishes via
  wrapPublisher's RenderingPublisher (sub_grpc.go:207,219) — any harness mirror using raw
  `bus.Bus.Publisher()` ships nil-Rendering frames (INV-EVENTBUS-6: gateway drops those).
  Check publisher wrapping, not just Append-translation fidelity. Also: harness.go is a SHARED
  int helper — a change there that un-drops events (eventStore was nil → command_response/
  command_error now publish) affects EVERY harness suite (privacy's empty-registry SendCommand
  → unknown-command command_error now hits bus + events_audit); require full `task test:int`,
  not just the touched suites. Fake-level "exclusion pins" (fakeStore.GetWithMembership
  re-implements role filter in Go) pin the fake, not the SQL — demand a DB-level twin.

- **Gateway scene-RPC passthrough (5rh.8.12 READY, 2026-06-08).** 9 Web* RPCs proxying
  SceneAccessService facade. KEY recurring trap: `*grpc.Client` wrappers wrap with
  `oops.Code("RPC_FAILED").Wrap(err)` — and the web handler returns that to connect-go with
  `//nolint:wrapcheck // gRPC status errors pass through as-is`. That comment is FALSE: core is
  reached via plain grpc-go (status.Status err), browser side is connect-go with NO error
  interceptor (server.go:69 NewWebServiceHandler, no WithInterceptors). connect-go wrapIfUncoded/
  CodeOf only recognize *connect.Error via errors.As → an oops-wrapped status err becomes
  CodeUnknown/HTTP 500. So facade PERMISSION_DENIED/NOT_FOUND/UNAUTHENTICATED all collapse to
  Unknown at the browser. BUT this is the IDENTICAL pre-existing pattern of WebListFocusPresence
  (handler.go:792) / WebListContent / WebListSessionStreams — NOT a new defect → non-blocking,
  track separately. Unit `...PassesStatusErrorThroughAsIs` tests use the MOCK (no wrap) so they
  prove handler transparency but CANNOT catch this (mock substitutes for the wrapper). Boundary/
  seam side: interface-based SceneAccessClient option-wired in handler.go; nil-client guard returns
  CodeUnimplemented; token from headerInjectSessionToken (never body, never logged); proto requests
  OMIT player_session_token (header-injected). All clean. Whenever reviewing a web/gateway PR,
  re-check this status→connect code gap; it is gateway-wide accepted behavior, not per-PR.

- **alt-session stream-loop port (5rh.8.15): R1 NOT READY → R2 (mmwsokrtpwpl) READY.** Recurring
  ports-drop-the-hard-parts bugs to grep for when a .svelte.ts "mirrors terminal hydrateAndStream":
  backoff declared inside recursive fn (hoist to session+while-loop); connectionId gate resolved
  ONLY in STREAM_OPENED (must reject on close/error/10s-timeout + reinstall fresh gate);
  streamGeneration declared-but-never-incremented = inert guard (grep the increment site);
  Map keyed by characterId but delete(sessionId) = dead session cached. R2 fixed all in code +
  altSessions.test.ts 7 fake-iterator tests. "241 pass" is whole-suite, proves nothing about new
  async surface — `grep -c 'it('` the NEW test file. (2026-06-08)
- **Frontend UI consuming a runes store (5rh.8.16 scenes workspace, 2026-06-08) — NOT READY.**
  6 Svelte components + page wiring. Runes reactivity ($derived reading store getters), drafts (two
  $effects, no cross-scene clobber since draftKey is $derived), localStorage SSR guards, a11y, send-
  path error handling — all CORRECT. BUT the load-bearing BLOCKER was in the CONSUMED store, not the
  diff's own .svelte files: `workspaceStore.refresh()` hardcodes `asCharacterId:'' / asCharacterName:''`
  for every scene (workspaceStore.svelte.ts:72-73). This UI is the FIRST consumer to make those fields
  load-bearing → composer `ensureSession(scene.asCharacterId)` = `ensureSession('')`, select() opens an
  empty-character alt, roster/strip/"posing as" render blank. pnpm-check-0-errors + 241-pass prove
  TYPE-correctness only (`''` is a valid string) — ZERO tests render these components with real store
  state. LESSON: for UI-consumes-store PRs, trace each consumed field back to its PRODUCER and check it
  is actually populated, not just typed; a green type-check + whole-suite-pass is blind to empty-but-
  valid data. Also: `data.playerId` passed as the `sessionId` arg (refresh/select/queryStreamHistory) —
  type confusion, inert today (header-token auth INV-SCENE-63) but breaks if session_id ever binds;
  Medium non-blocking.
- **Scene board UI (5rh.8.17, 2026-06-08) — NOT READY. STATE vs VISIBILITY string confusion.**
  `SceneInfo.state` ∈ {active,paused,ended,archived} (core-scenes/types.go:23-26); `"open"` is a
  VISIBILITY value ({open,private}), NOT a state. SceneBoardRow gated Watch/Join on
  `scene.state === 'open'` → NEVER true → primary board actions never render, only "View"; stateColor
  green-dot likewise dead. pnpm-check 0-errors is BLIND (both are valid strings). LESSON: whenever a
  .svelte compares a proto string field to a literal, grep the proto/Go const set for that field — UI
  authors routinely cross state↔visibility↔role vocabularies. Also recurring: nav params (?watch/?join
  via goto) are DEAD unless the target +page.svelte reads $page.url.searchParams — workspace page
  doesn't; board passes characterId="" hardcoded so even the watchScene RPC never fires. JSONL export
  has no timestamp → timestampMs:0 → PoseCard renders 1970/12:00AM (Low). jsonlToLogEntries try-guarded,
  blank/trailing-line safe, matches backend `{"speaker","kind","content"}\n` render; downloadBlob
  SSR-safe (document only in click handler) + revokes — all sound.
- **E2E scenes-workspace suite (5rh.8.19, 2026-06-08) — READY.** 8 Playwright tests reusing
  existing helpers. KEY: the two prior NOT-READY app blockers WERE FIXED upstream — .8.16
  hardcoded `asCharacterId:''` (now refresh() fans out alts tagging real charId) and .8.17 dead
  `?watch`/`?join` nav (now +page.svelte:143-160 reads param + selects). Always re-check whether a
  prior blocker still holds in CURRENT source before failing a dependent test PR. Honest-degradation
  pattern that is ACCEPTABLE (not a hide-the-bug like the weakened S3 original): a test scoped by its
  OWN title+comment to a narrower assertion than the plan scenario, referencing a real tracked bead
  (.8.26 P1). S2 owner-sees-composer ≠ plan's watcher→Join-CTA (observer branch SceneComposer.svelte:99
  never exercised) — Medium non-blocking, recommend a follow-up bead so the observer E2E gap isn't lost.
  S3 asserts draft-clears only; passes because .8.26's broken path resolves {success:false} silently
  (no throw → success branch clears draft); catch does NOT clear draft, so a real throw WOULD fail it.
  Flakiness checklist that held: no sleeps; expect.poll/waitForEvent/toPass; conn-pill data-status
  =connected gate before commands; unique per-test titles. App-fix listScenes(...characterId) correct;
  char-less player → primaryCharId='' → facade NotFound caught into fetchError (Low, not in test scope).
