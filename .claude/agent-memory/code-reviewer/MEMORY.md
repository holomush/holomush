- **Invariant-registry family-renumber series (.14.12–.14.27, hz0v4) + UNREGISTERED-FILE blindness — CONSOLIDATED.**
  Legacy family→canonical `INV-<SCOPE>-N`. HOLDS: (1) residual walk `bareInvRE=\bINV-\d+\b` matches ONLY
  bare NUMERIC `INV-9`, NOT `INV-P7-1`/`INV-PLUGIN-22`; `continue`s on shared files. (2) checkProvenance greps
  CANONICAL `e.ID` not `r.Token`. (3) PER-SITE not per-number. (4) regen generated artifacts (.pb.go/_pb.ts/
  grpc-api.md) on proto INV-comment rename. CRITICAL (.14.23 NOT READY): registry guards are BLIND to files in
  neither owned_paths/shared_files/refs[] — a fully-green run does NOT prove migration complete. ALWAYS whole-tree
  `rg -c '\bINV-<OLD>[0-9]' --glob '!docs/**'`; `rg 'INV-<SCOPE>-[0-9]+\.\.[0-9]+'` for range-rewrite
  corruption (`INV-P7-1..16`→`INV-CRYPTO-38..16` keeps provenance green); `rg 'TestINV_<OLD>[0-9]'` dangling cites.
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
  DeliverCommand:936) + release func (`Revoke` = map delete, idempotent; 5min TTL sweeper backstops
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

- **Gateway scene-RPC passthrough (5rh.8.12 READY, 2026-06-08).** 9 `Web*` RPCs proxying
  SceneAccessService facade. KEY recurring trap: `*grpc.Client` wrappers wrap with
  `oops.Code("RPC_FAILED").Wrap(err)` — and the web handler returns that to connect-go with
  `//nolint:wrapcheck // gRPC status errors pass through as-is`. That comment is FALSE: core is
  reached via plain grpc-go (status.Status err), browser side is connect-go with NO error
  interceptor (server.go:69 NewWebServiceHandler, no WithInterceptors). connect-go wrapIfUncoded/
  CodeOf only recognize `*connect.Error` via errors.As → an oops-wrapped status err becomes
  CodeUnknown/HTTP 500. So facade PERMISSION_DENIED/NOT_FOUND/UNAUTHENTICATED all collapse to
  Unknown at the browser. BUT this is the IDENTICAL pre-existing pattern of WebListFocusPresence
  (handler.go:792) / WebListContent / WebListSessionStreams — NOT a new defect → non-blocking,
  track separately. Unit `...PassesStatusErrorThroughAsIs` tests use the MOCK (no wrap) so they
  prove handler transparency but CANNOT catch this (mock substitutes for the wrapper). Boundary/
  seam side: interface-based SceneAccessClient option-wired in handler.go; nil-client guard returns
  CodeUnimplemented; token from headerInjectSessionToken (never body, never logged); proto requests
  OMIT player_session_token (header-injected). All clean. Whenever reviewing a web/gateway PR,
  re-check this status→connect code gap; it is gateway-wide accepted behavior, not per-PR.

- **alt-session stream-loop port (5rh.8.15): R1 NOT READY → R2 READY (2026-06-08).** ports-drop-the-hard-parts
  bugs when a .svelte.ts "mirrors terminal hydrateAndStream": backoff declared inside recursive fn (hoist to
  while-loop); connectionId gate resolved ONLY in STREAM_OPENED (reject on close/error/timeout + reinstall);
  streamGeneration declared-but-never-incremented = inert (grep increment site); Map keyed by characterId but
  delete(sessionId) = dead cache. "241 pass" whole-suite proves nothing — `grep -c 'it('` the NEW test file.
- **Frontend UI-consumes-store + STATE/VISIBILITY string confusion (5rh.8.16/.8.17, 2026-06-08) — both NOT READY,**
  CONSOLIDATED. (.8.16) Load-bearing blocker was in the CONSUMED store not the diff's .svelte: refresh() hardcoded
  `asCharacterId:''` (workspaceStore.svelte.ts:72-73); this UI was FIRST to make it load-bearing → ensureSession('')
  → blank roster. LESSON: trace each consumed field to its PRODUCER, verify POPULATED not just typed — pnpm-check +
  whole-suite-pass are blind to empty-but-valid data. (.8.17) `SceneInfo.state`∈{active,paused,ended,archived};
  `"open"` is a VISIBILITY value not a state — `scene.state==='open'` NEVER true → board actions never render.
  LESSON: when a .svelte compares a proto string field to a literal, grep the proto/Go const set — UI authors cross
  state↔visibility↔role vocabularies. Recurring: `?watch/?join` nav params DEAD unless target +page reads
  $page.url.searchParams; characterId="" hardcoded → RPC never fires. `data.playerId` as sessionId arg = inert type
  confusion today (header-token INV-SCENE-63), breaks if session_id binds.
- **E2E scenes-workspace suite (5rh.8.19, 2026-06-08) — READY.** Playwright reusing helpers. KEY: re-check whether
  a prior NOT-READY blocker (.8.16 asCharacterId, .8.17 dead nav) STILL holds in CURRENT source before failing a
  dependent test PR — both were fixed upstream. ACCEPTABLE honest-degradation: a test scoped by its OWN title+comment
  to a narrower assertion than the plan, citing a real tracked bead (≠ silently weakening). Watch: a test passing
  because a broken path resolves {success:false} silently (no throw) rather than asserting the real behavior.
  Flakiness checklist: no sleeps; expect.poll/waitForEvent/toPass; conn-pill data-status gate before commands;
  unique per-test titles.
- **Proto package decomposition / carve god-service into capability services (eykuh.1.2 READY, 2026-06-11).**
  Proto-only, additive, 14 services across 11 host/v1 files + generated pb.go/grpc/connect/_pb.ts. CHECKLIST
  that held: (1) RPC membership per-service vs plan table — count each service's rpcs. (2) Carved-message
  fidelity: source `PluginHostServiceX{...}` → `X{...}` MUST keep SAME field#+type+validate-constraint+doc
  (prefix drop only); compare against the ORIGINAL plugin.proto bodies, not just the plan. (3) Lua-derived
  msgs (world/property/session) ground in Go hostfunc: SessionInfo↔cap_session.go SetField keys, QueryObject
  9-field projection↔world.go queryObjectFn, CreateObject one-of↔world_write.go containmentCount==1. (4) THE
  DEVIATION: copying package-local shared types (FocusKey/FocusKind/FocusFailure*, Event/StreamReplayMode,
  SettingScope) into the new package while IMPORTING cross-file AuditRow/RowResult is CONSISTENT not arbitrary
  — discriminator is type LOCALITY: copy types defined INSIDE plugin.proto (no standalone file to import w/o
  the god-service), import types in their OWN domain file (audit.proto, which plugin.proto itself imports).
  proto packages are FLAT: each shared type defined exactly ONCE across the package's files = no codegen
  collision (verify w/ `rg '^(message|enum) <T> ' host/v1/`). Copies spawn distinct Go types (hostv1.X vs
  pluginv1.X) → Task 3 server move OWES explicit pluginv1↔hostv1 translation + round-trip equality test (Low
  follow-up; nothing pins copies equal). (5) generated .pb.go SPDX header is REPO CONVENTION (flows from proto
  leading comment; plugin/v1+eventbus/v1 pb.go carry it; .licenserc.yaml excludes **/*.pb.go from ENFORCEMENT
  only) — not a 'skip generated files' violation. (6) additivity: `jj show --stat | rg plugin/v1` must be EMPTY;
  buf.validate import carried verbatim from source = fidelity not deviation.
- **Capability-service CLIENT rewire (eykuh.1.9 READY, 2026-06-11) — pure transport swap, crypto-adjacent.**
  Swaps SDK decrypt client pluginv1.PluginHostServiceClient→hostv1.AuditServiceClient + envelope
  pluginv1.DecryptOwnAuditRowsRequest/Response→hostv1.* . CRYPTO-SAFETY GATE held because the SDK
  wrapper (audit.go DecryptOwnAuditRows) is transport-only: NO DEK/AAD/eligibility/content logic in
  pkg/plugin — all host-side. Verify the ENVELOPE still carries pluginv1 ROWS (host audit.pb.go:
  Rows []*v1.AuditRow / Results []*v1.RowResult; wire descriptor refs holomush.plugin.v1.AuditRow =
  imported not redefined) → no row-drop/mis-assign. Host endpoint crypto-equivalence: auditServer
  (host_capability_servers.go:321) DELEGATES to s.legacy().DecryptOwnAuditRows = same
  pluginHostServiceServer handler (host_service.go:953) → same ReadbackDecryptor.DecryptOwnRows
  (OwnerMap g1 gate, not_owner, DECRYPT_BATCH_TOO_LARGE) — that delegate is Task 3's work, confirms
  endpoint equivalence not this diff's. NIL-GATING preserved: decAware block runs only if provider is
  SnapshotDecryptorAware → wantsDecryptor=true → hostConn dialed before NewAuditServiceClient(hostConn).
  hostClient var removal (sdk.go) FORCED by last-consumer migration = overlaps Task 10 (pre-done); flag
  for bookkeeping not defect. pluginv1 STILL imported in sdk.go for PluginService (Init/HandleEvent/
  HandleCommand) = not dangling. Tests register hostv1.RegisterAuditServiceServer via bufconn, 3 cases
  non-vacuous (plaintext echo, typed refusal+nil-plaintext, status err). God-service still registered =
  independence holds.
