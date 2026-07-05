- **Invariant-registry guards: renumber + binding-backfill + provenance + WRONG-GATE — CONSOLIDATED.**
  (1) Renumber legacy→canonical: guards BLIND to files in neither owned_paths/shared_files/refs[] (green≠done) —
  whole-tree `rg -c '\bINV-<OLD>[0-9]' --glob '!docs/**'` + `rg 'INV-<SCOPE>-[0-9]+\.\.[0-9]+'` (range corruption).
  (2) Backfill pending→bound+asserted_by: hand-verify asserted_by file CONTAINS `// Verifies:`; `pending` MUST NOT
  carry asserted_by. (3) TestProvenanceGuard reads each entry's refs[] from disk → deleted/renamed refs file turns
  it RED for ALL citing entries; a no-refs pending→bound flip neither causes nor worsens it (judge per-entry).
  (4) WRONG-GATE binding (eykuh.2.12 NOT READY, 2026-06-12): the meta-tests CANNOT detect a `// Verifies:` on a test
  asserting a DIFFERENT gate than the invariant names — only Skip-placeholders. ALWAYS read the origin spec to settle
  WHICH gate the invariant means, then confirm the bound test drives THAT gate. Case: INV-PLUGIN-45 "single
  least-privilege gate at broker/registry common path, no per-runtime fork" bound to hostcap.RegisterCapabilities
  (register.go:42) — but that registers the per-RUNTIME server SET by CapabilitySet, NOT the per-PLUGIN "declared X⇒
  gets X" gate. Real per-plugin gate is STILL split (luabridge.RegisterHostCaps Lua-only bridge.go:30 vs broker
  RequiredServiceNames binary manifest.go:150) = the exact spec-forbidden split, consolidation DEFERRED to sub-specs
  3-5. "Two BINARY endpoints, identical denial" never exercises the Lua gate → proves intra-runtime determinism, not
  cross-runtime single-source. Also "fail-closed without identity" (EMIT_TOKEN_MISSING/ACTOR_NOT_FOUND) ≠ "authorized
  as PluginSubject" — that's the transport-identity gap, engine never reached (DenyAll wired but unhit). Fix: revert
  to pending + coverage bug, OR re-target to a genuinely-shared per-plugin gate (none exists pre-consolidation).
  R2 READY (2026-06-12): chose REVERT — INV-45→pending (no asserted_by); INV-44 kept bound on a GENUINE
  cross-runtime reachability contrast (Lua SessionService.ListActive SUCCEEDS w/ WithSessionAccess backing vs binary
  Unimplemented; SessionService LuaDefaultSet-only register.go:53-55). The EvalService fail-closed test kept as
  SUPPORTING INV-44 with an explicit overclaim-guard comment scoping it to identity-absent only + tracked gap. LESSON:
  the clean revert pattern = pending+no-asserted_by + an INLINE NOTE in the binding test documenting why the sibling
  inv stays pending (deferral target). inv-render drift-check (empty post-render diff) + meta-tests are the proof.
- **Wire event-type qualification (bare scene_*→<plugin>:<verb>), aneim/r0kup READY, 2026-06-07.** THREE
  vocabularies, DIFFERENT rules — judge per-SITE: (1) registered-emit set + (2) crypto.emits[].event_type stay
  BARE (INV-PLUGIN-32 set-equality); (3) wire type + verbs[].type qualified. So bare main.go/crypto.emits/main_test
  assertions are CORRECT. Checklist: raw-bus synthetic int tests publish via bus.Publisher() (bypass verb registry,
  bare OK); scene_log INSERTs incl SQL type= literals (zero-row risk); audit dispatch row.GetType() qualified vs if.

- **Gate-removal (delete a runtime safety-gate, run check unconditionally) — dj95.3 READY, 2026-06-07.**
  Safe only once EVERY path the now-unconditional check could reject is compliant. (1) Completeness: whole-tree
  `rg <gateFlag>` — watch for a SEPARATE distinct gate that MUST remain (internal/grpc/ read-side ≠ plugins-pkg).
  (2) Crux is PRODUCTION-SAFETY not completeness: enumerate every `sensitivity: always` manifest entry and verify
  each emit SITE claims Sensitive=true. (3) Undeclared-event default must equal pre-gate behavior (LookupEmitSensitivity
  → Never → Sensitive=false); only NEW rejection is over-claim, which no prod path does. (4) Stale bead acceptance
  (`rg internal/ → zero hits`) when a legit gate remains = note-for-close, not a defect.

- **Docs-only ADR-capture branch (holomush-5rh.8, 2026-06-07).** (1) ALWAYS `jj st`+`jj diff -r @` before
  verdict: task pr-prep/fmt leave SPDX+yamlfmt normalization UNCOMMITTED in @ (docs/** IS in .licenserc; yamlfmt
  no docs/ exclude) -- pr-prep validates the @ SNAPSHOT, push unit @- fails CI. cat/Read shows @-fixed state NOT
  what ships; compare `jj file show -r @-` vs main. (2) Spec revised in plan-grounding leaves STALE instructions
  contradicting the captured ADR -- grep spec for every mechanism the ADR REJECTED. probe missed
  ReadSceneLogForSnapshot; confirm probe zero-results with rg before claiming absence.

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

- **WatchScene RPC (5rh.8.3 NOT READY, 2026-06-07).** `focus.Coordinator.JoinFocus` is NOT idempotent
  — dup membership errors `FOCUS_ALREADY_MEMBER` (focus/join.go:38-42); fakeFocusClient nil-on-dup =
  false-green; read the REAL coordinator. Premature partial binding: multi-clause inv flipped bound when only one clause tested — check WHICH task owns the registry flip.

- **5rh.8.21 service-dispatch token (READY, 2026-06-07).** Host.BeginServiceDispatch (host.go:968) mints a
  dispatch token for server-side-verified actor+ownerPlayerID; Revoke idempotent + 5min TTL sweeper. Binary-only
  via findOptional[ServiceDispatcher] (SERVICE_DISPATCH_UNSUPPORTED for Lua) = transport-specific, NOT a symmetry
  violation. WatchScene advisory-md check (service.go:878) is restriction-only: spoofed md can only restore baseline,
  never grant — correct defense-in-depth polarity. Caller MUST pass server-verified actor; token outlives unload (Low).
- **Harness event-path parity (5rh.8.4, 2026-06-07).** Production busEventAppender publishes via wrapPublisher's RenderingPublisher (sub_grpc.go:207) — a harness mirror using raw `bus.Bus.Publisher()` ships nil-Rendering frames (INV-EVENTBUS-6: gateway drops them); check publisher WRAPPING not just Append fidelity. harness.go is SHARED — a change un-dropping events affects EVERY suite → require full `task test:int`. Fake-level exclusion pins (re-implement role filter in Go) pin the fake not the SQL — demand a DB-level twin.

- **Gateway scene-RPC passthrough — RESOLVED by interceptor (orig 5rh.8.12 2026-06-08; fixed by 5rh.24.13 2026-06-26).** The old RECURRING trap (`*grpc.Client` oops-wraps a grpc status.Status; web handler `return nil,err //nolint:wrapcheck // pass through as-is`; connect-go CodeOf collapsed oops-wrapped status to CodeUnknown/HTTP500) is now FIXED: `statusTranslationInterceptor()` is wired at server.go:76 (impl `internal/web/status_interceptor.go`). It `errors.As`-walks the oops chain for a `GRPCStatus()` and maps to the right connect code (unclassified→CodeInternal, NOT Unknown); roundtrip tests in status_interceptor_test.go prove PermissionDenied/NotFound/Internal reach the browser correctly + no oops-chain leak. So `//nolint:wrapcheck // gRPC status errors pass through as-is` on web BFF handlers is now LEGITIMATE — do NOT re-raise the old collapse finding. Token still header-injected, never body/logged.

- **Frontend/E2E ports + proto-vocab traps (5rh.8.15/.16/.17/.19, 2026-06-08) — CONSOLIDATED.** (a) .svelte.ts ports DROP hard parts: backoff inside recursive fn (hoist to while-loop), gate resolved only in STREAM_OPENED, counter declared-never-incremented=inert, Map keyed charId but delete(sessionId)=dead cache; "N pass" proves nothing — `grep -c 'it('` the NEW file. (b) Load-bearing blocker often in the CONSUMED store not the diff (refresh() hardcoded asCharacterId:'' → blank roster): trace each consumed field to its PRODUCER, verify POPULATED not just typed; pnpm-check blind to empty-but-valid. (c) proto STRING field vs literal: grep the proto/Go const set — UI authors cross state↔visibility↔role vocab (SceneInfo.state∈{active,paused,ended,archived}, 'open' is VISIBILITY → state==='open' never true). nav params DEAD unless +page reads searchParams; charId='' → RPC never fires. (d) re-check a prior NOT-READY blocker STILL holds in CURRENT source before failing a dependent PR (upstream may have fixed). Honest-degradation OK iff test title+comment scope it + cite a tracked bead. Flakiness: no sleeps; expect.poll/toPass; conn-pill gate; unique titles.
- **Guest/auth-flag gate via one-shot get(store) in PERSISTENT component (5rh.23 NOT READY, 2026-06-20).**
  Svelte 5: a gate reading `get(authState).isGuest` ONCE at `const`-init is stale if the component lives in the
  ROOT +layout.svelte (unauthed shell, mounts once, no {#if}/{#key}) — isGuest defaults false and is set LATER by
  route load fns ((authed)/+layout.ts, +page.ts, login). restoreSession restores sessionId/characterName but NOT
  isGuest. So palette froze isGuest:false → guest sees "Go to Scenes" dead-end (the exact thing the bead removed).
  Sibling Rail was CORRECT: $derived(visibleSections({isGuest})) inside (authed) layout. LESSON: for any
  auth/session-derived gate, trace the CONSUMER's mount point + lifetime vs WHEN the flag is set; `get()` one-shot
  is only safe if the instance is created AFTER the flag resolves AND never outlives a transition. Pure-fn unit test
  (sections.test.ts) passes — it CANNOT catch consumer staleness; demand a mount-then-flip-store test. Backend GAP
  was CLEAN: CharacterProvider WithCharacterKindLookup emits character.is_guest (omit-don't-sentinel ti1b), manifest
  `&& principal.character.is_guest == false` fail-closed (evalComparison !leftOK→false, evaluator.go:134), resolver
  merges to bags.Subject["character.is_guest"] + Schema() registers keys (else merge rejects). Prod wiring
  subsystem.go always supplies PlayerKindLookup. character: principal is what command dispatch evaluates (player:
  never reached) → gate MUST live on character bag not player bag.
- **Proto god-service→capability-service decomposition (eykuh.1.2 READY, 2026-06-11).** Proto-only/additive,
  14 svcs across 11 host/v1 files. CHECKLIST: (1) count each svc's RPCs vs plan table. (2) carved-msg fidelity:
  `PluginHostServiceX{}`→`X{}` keep SAME field#+type+validate-constraint+doc (prefix-drop only) vs ORIGINAL
  plugin.proto bodies. (3) Lua-derived msgs ground in Go hostfunc (SessionInfo↔cap_session keys, QueryObject
  9-field↔world.go, CreateObject one-of↔world_write containmentCount==1). (4) DEVIATION OK: copy types defined
  INSIDE plugin.proto (no standalone file), IMPORT types w/ own domain file (audit.proto) — discriminator=type
  LOCALITY. Proto pkgs FLAT: each type defined ONCE (verify `rg '^(message|enum) <T> ' host/v1/`). Copies spawn
  distinct Go types (hostv1.X vs pluginv1.X)→server-move OWES pluginv1↔hostv1 translation+round-trip test. (5)
  generated .pb.go SPDX header IS repo convention (not 'skip generated' violation). (6) additivity: `jj show
  --stat | rg plugin/v1` must be EMPTY.
- **Capability-service CLIENT rewire (eykuh.1.9 READY, 2026-06-11) — transport swap, crypto-adjacent.** SDK
  decrypt client pluginv1.PluginHostServiceClient→hostv1.AuditServiceClient + envelope→hostv1.*. CRYPTO GATE
  held: SDK wrapper is transport-only (NO DEK/AAD/eligibility in pkg/plugin — all host-side). Verify envelope
  STILL carries pluginv1 ROWS (Rows []*v1.AuditRow imported not redefined)→no row-drop. Host endpoint equiv:
  auditServer DELEGATES to legacy().DecryptOwnAuditRows→same ReadbackDecryptor.DecryptOwnRows (OwnerMap gate,
  not_owner, BATCH_TOO_LARGE). NIL-GATING preserved (decAware only if SnapshotDecryptorAware→hostConn dialed
  before client). pluginv1 still imported for PluginService = not dangling. Tests via bufconn, 3 non-vacuous.
- **hostcap capability-server impl (eykuh.2.x: Session Task 4 READY, 2026-06-12).** New
  per-capability gRPC server in internal/plugin/hostcap, registered LuaDefaultSet-only.
  CHECKLIST: (1) RPC→service completeness = read BOTH session.proto AND generated
  *_grpc.pb.go ServerServer interface — they're the truth, plan prose was WRONG
  (claimed SetLastWhispered+Disconnect→DeleteByCharacter on admin svc; proto puts
  SetLastWhispered on SessionService, Disconnect→DisconnectSession). Embedded
  Unimplemented*Server on each struct = future proto RPCs fail closed. (2) NOT-FOUND
  parity: session.Access.FindByCharacterName returns (nil,nil) on miss → server nil→
  empty resp (session.go:39-43) = faithful to Lua nil-return (cap_session.go:88,
  stdlib_session.go:75). (3) buf.validate min_len=1 is DECORATIVE on broker —
  grpc.NewServer has NO protovalidate interceptor; property.go validates explicitly.
  Error opacity clean (LogErrorContext + static "internal error", neg tests assert
  NotContains "secret").

- **hostcap WorldQueryService capstone (eykuh.2.5 READY w/ tracked gap, 2026-06-12).** Task 5 maps 4
  Query RPCs→WorldQuerier(pluginName); 5th proto RPC FindLocation left inheriting Unimplemented*Server.
  KEY: FindLocation backed by WorldMutator.FindLocationByName (NOT WorldQuerier; adapter has none). Whole
  WorldMutationService ALSO unimplemented/unregistered — neither scheduled in ANY plan task. LIVE consumers
  exist (core-building/main.lua:61 find_location, core-objects create_object/location) but Lua bufconn
  CONSUMPTION isn't wired until Phase 1 T7 → acceptable-and-track, MUST file bead before any plugin opts
  into host-brokered world consumption. WorldQuerier widen 2→4 SAFE (superset). Subject-stamp test asserts
  bare "core-scenes" (server passes bare; real adapter SubjectID() prefixes plugin: downstream).

- **hostcap Lua adapter (eykuh.2.6 + parity fixes 2.14 READY, 2026-06-12) — CONSOLIDATED.** luaHostCapAdapter
  wraps *hostfunc.Functions, symmetric to binary *goplugin.Host. RECURRING RULE: when adapter is dead-code-until-
  next-task, verdict hinges on live-caller; for ANY server-reachable method READ the CONSUMER server body
  (servers.go) and classify each return field read-vs-unread before trusting a "lossy but unread" claim — adapter
  must populate EVERY field the server READS (e.g. AutoFocusOnJoin reads total+3 slices+FocusFailure{ConnID,Reason}
  servers.go:283-307; SetConnectionFocus discards result→zero-result safe). Settings store-recovery via comma-ok
  type-assert = parity iff same seam Lua hostfuncs consume + fails-closed Unimplemented on nil. SessionAdminService
  LuaDefaultSet-ONLY (binary excludes → binary-nil never reached). OwnedResourceTypes non-nil empty = evaluate.go:57 parity.
- **luabridge plugin-service bridge T10 (eykuh.2.10 READY, 2026-06-12).** RegisterPluginService:
  descriptor→Lua table, dynamicpb marshal, conn.Invoke. DECISIVE for "real BrokerProxy" check:
  test brokerProxyLoopback does `factory:=goplugin.NewBrokerProxy(providerConn,name); srv:=factory(nil);
  srv.Serve(lis)` = EXACT binary path (host.go:819-820 NewBrokerProxy→AcceptAndServe calls factory then
  Serve). ProxyStreams (grpc_proxy.go:93) raw-byte RawMessage forward = genuine, not stub. Round-trip
  genuine BECAUSE reply field≠request field (message→reply): hardcoded passthrough would fail assertion.
  Path `/<FullName>/<Method>` = `/test.echo.v1.Echo/Echo` canonical. Fail-early order CORRECT: bound slice
  built+len-checked (pluginsvc.go:60-74) BEFORE tbl/SetGlobal (76-82) → no partial global leak. Namespace=
  strings.ToLower(short name) → collision risk if 2 providers' short names lowercase-collide (Low, flag for
  sub-spec5 migration). UNWIRED mechanism (only test+plan call it) = per-task-deferral pattern (spec §5
  fixture-only, sub-spec5 migrates) — consistent w/ 2.5/2.6. Streaming silently `continue`-skipped, doc'd in
  source not logged (Low). pushBridgeError opacity preserved (status.Convert msg only). 396 tests, lint 0.

- **INV-PLUGIN-49 cross-runtime parity binding (eykuh.2.11 R2 READY, 2026-06-12).** R1 flagged
  settings+interceptor scaffolding (modeled non-existent prod path); R2 reworked to `kv`. PATTERN for
  cross-runtime single-source binding: SAME hostcap.RegisterCapabilities behind BOTH newBinaryEndpoint
  (BinaryDefaultSet, real goplugin.NewHost) + newLuaEndpoint (LuaDefaultSet, REAL
  luaHost.HostCapabilitiesAdapter()), differ ONLY by CapabilitySet. KVService registered UNCONDITIONALLY
  (register.go:51, outside LuaDefaultSet block); kvServer = bare UnimplementedKVServiceServer (servers.go:1004)
  → genuine Unimplemented from REGISTERED svc. NON-VACUOUS GUARD: GetServiceInfo `require.Contains` KVService
  in BOTH servers distinguishes Unimplemented-from-registered (genuine) vs from-UNREGISTERED (false pass);
  equal-codes assert = single-source routing. Prod fidelity: test == newPluginEndpoint(bufconn_endpoint.go:30)
  / host_service.go:31 byte-for-byte. Doc-comment "no interceptor needed" is FINE, not scaffolding.
- **INV-PLUGIN-22 spec-sanctioned structural binding (eykuh.2.13 READY, 2026-06-12) — CLEAN case.**
  Pattern: annotating a PRE-EXISTING structural meta-test is a LEGITIMATE binding when (a) the test genuinely
  proves the invariant clause AND (b) the ORIGIN SPEC names that mechanism. Verified: spec
  2026-05-25-plugin-host-evaluate-design.md:275 "(meta-test: proto descriptor has no subject field)" +:350-351
  — so `TestINV1EvaluateRequestHasNoSubjectField` (descriptor field-name scan + `fields.Len()==2` lock) is
  non-vacuous (breaks build if subject/actor field added). REGISTRY MOVE refs→asserted_by: the proving test
  moves OUT of refs INTO asserted_by; other provenance (proto/impl files) stays in refs — correct shape, meta-test
  green. CLAUSE-2 STRUCTURAL-BUT-GENUINE nuance: luaHostCapAdapter.LookupActor returns (actor_from_ctx,
  PluginSubject(pluginName)) — subject built from host-established PARAM, ctx actor lands in the IGNORED first
  return. Forged-actor-on-ctx assertion `subject==plugin:<name>` CANNOT fail given code shape → pins correct
  property but regress-blind; ACCEPTABLE as SUPPORTING clause alongside spec-canonical clause 1, NOT load-bearing
  alone (regress-blind but supporting). NOT a T11 scaffolding-trap (own-attack, honestly-scoped Lua seam) nor a T12 wrong-gate.
- **SDK capability-declaration registry, Aware->token map (si3zs.2 READY, 2026-06-13) -- CLEAN.** pkg/plugin
  hostCapabilityRequirements maps each *Aware iface->tokens; validateDeclaredCapabilities fail-closes
  CAPABILITY_NOT_DECLARED. KEY FAIL-OPEN CHECK: rg '^type \w*Aware\b' pkg/plugin/ MUST cover every Aware iface
  (6 today) -- unmapped iface=silent fail-open. Cross-check each token 3 ways: facade injected (sdk.go;
  SnapshotDecryptor->NewAuditServiceClient=audit), vocab capability_vocab.go CapabilityServiceNames, exempt set=
  descriptor.go declarationExemptCapabilities {emit,command-registry}=nil tokens. FocusClientAware grants BOTH
  focus+stream.history (2 clients, focus_client.go:223). Fixtures embed ServiceProvider (RegisterServices+Init
  only, no Set*)->no spurious Aware match; verify embedded iface method set. Pending-entry + in-code // Verifies:
  FINE pre-flip: meta-tests bind from registry asserted_by not code-scan (ran 4 green); registry flip=later task.
- **Token re-anchor after gate deletion (si3zs.6 READY, 2026-06-13) -- HONEST repair.** When an earlier task
  DELETES the code/test carrying an invariant's canonical token (si3zs.5 deleted manifestNeedsInit +
  TestNeedsInitIncludesConfig carrying INV-PLUGIN-8), TestProvenanceGuard goes RED: checkProvenance
  (invariant_registry_test.go:546) matches the CANONICAL id INV-PLUGIN-8 against each refs[]/shared_files file
  CONTENT, NOT the yaml token: field (INV-PC-8=metadata only). Repair = move canonical-token comment onto
  SURVIVING genuinely-relevant code + rewrite summary to drop deleted mechanism. HONESTY: token on code that
  ACTUALLY provides the guarantee (not sprinkle), summary matches code (verify needsInit:=true unconditional), a
  pending invariant gets PLAIN token comment NOT // Verifies:. SHARED_FILES (yaml:414) skip residual-legacy
  check (checkFile:566) but refs canonical-token check still applies. Proof: inv-render -> zero drift.
- **Atomic plugin-capability cutover (eykuh.4.9 READY, 2026-06-14) — CLEAN flip.** Strip cap host-fn reg from
  hostfunc.Register (keep ambient: log/req-id/command-registry/stream.subscription/stream.history/audit); delete
  WithHostCapBridge+bridgeEnabled → RegisterHostCaps UNCONDITIONAL. NO FAIL-OPEN: declaredCaps=grantedSubset
  (host.go:148 len(granted)==0→nil) OR manifest-fallback when grants nil (host.go:720); empty→inject nothing.
  Migrated Lua checked FIELD-BY-FIELD vs bindings_gen.go + host/v1 protos: req keys snake_case; resp via
  ProtoToLuaTable where m.Range SKIPS unset (proto3 presence)→absent msg/empty repeated=nil → `resp and resp.session`
  /`or {}` handles it. Dotted globals via _G["world.query"]; single-word direct. "no prod backing" VERIFIABLE:
  subsystem.go:180 registers ONLY AuditService → legacy property/world_ext globals ALWAYS nil in prod → those
  branches dead → set "Unknown property"/describe fallbacks FAITHFUL not regressions. WEAK-GATE done right: census
  proves LOAD only; new corebuilding_brokered_command_test DELIVERS dig + asserts CreateExit fields thru real
  bufconn → catches wrong field name. Harness wires AllowAllEngine + fixed AttributeResolver = supply scope-fence
  INPUTS not stub the check. Entitlement test: AllowAll+foreign scene:+OwnedResourceTypes empty+gate-before-engine
  (evaluate.go:196<212)+errmsg has "scene"; deleted Lua subtest MOVED not dropped.
- **Binary dispatch-context propagation (ndtq1 READY, 2026-06-15) -- CLEAN extract+ferry.** Shared
  dispatchwire codec extracted from lua/dispatch_interceptor.go; binary path adds StampInterceptor
  before capability ic in newHostCapabilityServer, host.go DeliverEvent/Command marshal
  DispatchForHost->AttachOutgoing, SDK dispatch_ferry.go interceptor on the SINGLE shared
  dialPluginHost conn (sdk.go:189) ferries incoming->outgoing. FAIL-CLOSED verified every leg:
  DecodeFromIncoming denies len!=1/badJSON/empty-subject; ferryDispatch strips plugin-forged
  OUTGOING then forwards only host-delivered INCOMING (host-vouched wins, ambiguous dropped).
  LUA REFACTOR behavior-preservation crux: orig=unconditional strip + append-if(ok&&subj!=""),
  new=if-ok->AttachOutgoing(strip+append-if-subj!="") else->StripOutgoing. DispatchForHost
  (dispatch.go:42) returns ok regardless of subject, so empty-subject must be handled INSIDE
  AttachOutgoing -- it is. Regression guard = lua TestPluginForgedDispatchMetadataIsIgnored (real
  bufconn, forged+!ok->SCOPE_NO_DISPATCH). KEY-SYNC by twin literal-pinning tests (no shared symbol;
  internal/SDK module boundary forces it, mirrors x-holomush-emit-token). withDispatchToken keys on
  x-holomush-emit-token = disjoint from x-holomush-dispatch, no collision. Binary production path
  STILL latent (BinaryDefaultSet omits WorldMutationService) so StampInterceptor stamps a ctx value
  nothing reads = harmless. Test mock engines model real interceptor: scope half surfaces
  dc.Attributes["location"] as action attr "dispatch_location" (interceptor.go:208). Non-blocking:
  no single host->ferry->server e2e test (legs proven separately; cross-process needs subprocess).
- **Two-path stream-read gate, qualify-before-ABAC (xakba R2 READY, 2026-06-19) -- CLEAN dual-gate.**
  R1 NOT READY: ABAC built resource from UN-qualified req.GetStream() (system forbid never fired); DenyAll+pre-
  qualified test inputs couldn't catch. R2: pluginauthz.AuthorizeStreamRead Qualify(GameID,Stream) BEFORE
  EvaluateCapabilityAccess on "stream:<qual>", fail-closed on qualify err. SHARED by hostcap QueryStreamHistory
  (both runtimes) AND hostfunc authorizingHistoryReader decorator (functions.go:331, ambient Lua path) → symmetry
  closed. LESSON: gating a capability handler → rg the underlying read primitive (ReplayTail) across
  internal/plugin; ambient hostfuncs bypass the interceptor (decorator-wrap the reader, not just the broker handler).
  Tests have TEETH only if recordingEngine asserts the EXACT qualified resource + reader.called==false on deny.
  Latent non-blocking: binary Host.gameID set only via WithCA → no-mTLS binary plugins get gameID="" → fail closed.
- **Scene roster name-resolution (5rh.25/vdy2z READY, 2026-06-20) — CLEAN host-side fix.** GetSceneForViewer
  resolveRosterNames overwrites ParticipantInfo.CharacterName in-place via characterNameResolver (mirrors
  ListFocusPresence). VERIFIED NON-ALIASING: plugin GetScene builds Participants (service.go:484) + Observers
  (494) as SEPARATE &ParticipantInfo{} appends → combined-roster slice shares distinct pointers, in-place
  mutate safe. Best-effort: nil-resolver/nil-scene/parse-err/resolver-err/missing-id all keep ULID; resolver
  err LOGGED+return (DEGRADES, NOT INTERNAL — divergence from presence which hard-fails, doc'd in source).
  slog.ErrorContext (not errutil.LogErrorContext) is FINE here: matches sibling list_focus_presence.go:161, ctx
  carried (logging.md satisfied), err NOT returned to client (no opacity leak). Shared handleEmit (commands.go:1310)
  stamps character_name for IC+OOC both (ooc only swaps subject 1306-08) → R3 parity free. actor_id ULID stays
  (GameEvent.ActorId separate from Actor=character_name); translateEvent reads p.CharacterName→actor (translate.go:81).
  Harness: worldCharRepo raw *worldpg.CharacterRepository (==prod sub_grpc.go:312 type) added; NewSceneAccessServer
  helper == prod wiring; both files //go:build integration; NO prod import of integrationtest. Tests TEETH: facade
  unit asserts resolved+ULID-fallback(missingID); plugin include/omit; gateway actor; int uses REAL `scene join`
  cmd for plugin-DB roster row (JoinScene store-shortcut only touches focus_memberships, doc'd) + cross-loc
  not-ULID regex + ContainElement seeded names + decrypted pose author. int roster checks participants only (not
  observers) but unit covers observers — OK. All 5 unit tests pass; go vet -tags integration green.
- **BFF lifecycle mirror (5rh.24.4/.5 NOT READY, 2026-06-25) — build+test GREEN masks gofumpt drift.**
  Client wrappers + Web*Scene handlers that faithfully mirror CreateScene/WebCreateScene are
  trivially correct (gateway-boundary pure forwarder, oops With("method",<Verb>) per-method label,
  errutil.LogErrorContext + nil,err //nolint:wrapcheck, own req/resp types, 4 fields SessionId/
  PlayerSessionToken/CharacterId/SceneId). orchestrator "build 0 + N tests pass" CANNOT catch fmt
  drift — gofumpt is orthogonal to compile. ALWAYS run `task fmt:check` (NOT bare gofmt — hook blocks
  it) regardless of green build; fmt:check is in pr-prep fast lane = CI gate. Trap: a NEW contiguous
  struct-field block whose LONGEST name (resumeSceneReq, 14ch) forces a wider type-column than the
  earlier rows align to → first rows under-indented → drift. fmt:check ALSO surfaces PRE-EXISTING
  drift in out-of-scope sibling files (sceneaccess_service_test.go) — note separately, still gates
  branch. Logging-test docstring "all ten handlers" went stale (13 cases now) — implementer claimed
  to maintain "every handler" invariant but didn't bump count = Low non-blocking.
- **Widened repo interface → MISSED Nth implementor, harness compile break (5rh.24.10 NOT READY, 2026-06-26).**
  Adding a method to an interface (`auth.CharacterRepository.ListAll`) breaks EVERY type with a `var _ Iface = (*T)(nil)`
  assertion that wasn't updated. `auth.CharacterRepository` has THREE adapter impls — setup/adapters.go AND
  test/integration/auth/auth_suite_test.go AND a SECOND `authCharRepoAdapter` in internal/testsupport/integrationtest/
  harness.go (//go:build integration, assertion at :1282). Diff updated the first two, MISSED the harness → integration
  CI gate red. Local `task test:int -- ./internal/world/postgres/` is BLIND (postgres pkg doesn't import the harness).
  METHOD: enumerate all implementors via the interface's UNIQUE method (ListByPlayer→[]*world.Character distinguishes
  auth.CharacterRepository from world.CharacterRepository which lacks it AND from narrow ISP `CharacterLister`/
  `mockCharLister` which only has ListByPlayer→not full impl), then check each defines the new method. Narrow ISP test
  fakes are SAFE (don't implement the full iface). To prove harness compiles, run a test:int pkg that IMPORTS it
  (test/integration/...), never just the changed pkg's own test.
- **gofumpt const-block-comment regrouping (5rh.24.12 NOT READY, 2026-06-26) — same fmt-drift trap as .24.4/.5.**
  Inserting a `// Doc` comment line BEFORE the last entry of an aligned const block and aligning ALL entries to the
  new longest name is NOT gofumpt-clean: gofumpt breaks the alignment section at the comment, so entries above align
  among themselves (narrow) and the post-comment entry is its own group. `task fmt:check` (pr-prep fast lane / CI gate)
  goes RED; `task build`/unit tests stay GREEN (gofumpt orthogonal to compile). ALWAYS run `task fmt:check` on any diff
  touching a Go const/var/struct block regardless of green build — never trust "the realignment is benign". fmt:check
  is whole-repo: it also surfaces PRE-EXISTING drift in out-of-scope siblings (here plugins/core-scenes/service_test.go)
  — note those separately, they don't gate the bead but redden branch fmt. Rest of this bead was CLEAN: slog.ErrorContext
  (not errutil) FINE per file convention + vdy2z precedent (ctx carried, opaque "internal error" returned, no leak);
  deny-test binding genuine (mock leaves ListAll un-expected → fails if gate doesn't block before read). One real
  non-blocker: handler claims "mirrors list_focus_presence.go:116-137" but DROPPED the precedent's accessEngine==nil
  guard at :100-107 (panic-recovered-as-Internal = fail-closed not fail-open, so Medium not blocker).
- **E2E scene-membership: raw char-name in UI selectors + stale failing artifact (5rh.24.24 NOT READY, 2026-06-27).**
  TWO traps. (1) NAME NORMALIZATION: registerAndEnterTerminal returns the RAW input charName (fixtures uniqueSceneUser
  "Sc<Cap> <suffix>", e.g. "ScMbs aafdab") but server NormalizeCharacterName (internal/world/validation.go:114) Initial-
  Caps's it → stored/displayed "Scmbs Aafdab". Any UI selector keyed on the raw name (Join scene as {asCharacterName}
  SceneComposer:109; Manage {p.name}/<span>{p.name} SceneContextRail:129/140; getByText(name,exact)) CANNOT match →
  CI E2E red. Fix = derive display name via getClientCharacterName(page) (reads sessionStorage normalized) not the input.
  Spec's OWN comment used role-only DB asserts "name may be normalized" then violated it in UI selectors — read that
  caveat then check EVERY name selector honors it. (2) STALE-ARTIFACT GROUNDING: web/e2e/test-results/<spec>/error-
  context.md + summary.md are written per-run (summary-reporter.ts every run; error-context on FAILURE). Compare their
  mtime vs the spec mtime: if spec edited AFTER the last summary.md and summary shows FAILED, the pushed version was
  NEVER re-run = no green evidence (NOT READY for a "final E2E" deliverable). The a11y snapshot + console logs in error-
  context.md are FREE grounding (showed banner "Scmbs Aafdab" vs locator, 401s, empty workspace). test-results/ is
  gitignored (jj st won't list it) + task test:e2e rm -rf's it at start — ephemeral, not a push artifact. PRETTIER non-
  gate: web has NO prettier dep/config/format script; e2e .ts not in any pr-prep/CI format lane (Go fmt only) — `npx
  prettier@latest` flagging files (incl pre-existing db.ts) is NOT a repo gate, DROP it. CLEAN parts: getParticipants
  BySceneId SQL matches scene_participants(character_id,role) owner/member/invited/observer (migs 000003+000010);
  waitForURL(/\/scenes$/) workaround sound — ScenesShell deep-link $effect strips ?watch= via goto('/scenes',replaceState)
  after awaited watchScene (ScenesShell:204-215); kickAction auto-refetch (membershipFlow.ts:41); selectors otherwise
  all verified against components; create defaults active/open valid.
- **Svelte debounce+abort orchestration (5rh.24.41.3 READY, 2026-06-30) — CLEAN.** publishStore onEvent/scheduleTallyRefetch/
  reloadPointer + abort-aware refetchTally. Critical race-check: success-path abort guard `if(signal?.aborted)return` AFTER
  the await + scheduleTallyRefetch aborting prior controller BEFORE new = superseded success can't write stale tally (SOLID).
  Recurring NON-blocking shapes for fire-and-forget event orchestration: (1) `void asyncFn()` with NO try/catch = unhandled
  rejection on transient getScene fail (the SHAPE to watch — in this store reloadPointer + loadColdStart + refetchTally are now ALL try/catch-guarded: reloadPointer hardened in 5rh.24.41.8; loadColdStart guards + ensureSession-moved-inside-try in the #4562 autofix). (2) abort/late-
  response-suppression contracts UNTESTED — synchronous mock never reacts to abort(); 5 tests proved debounce-count/observer/
  cross-scene/lifecycle-pointer/transition but NOT supersede-no-write. (3) permission_denied catch branch unguarded by
  signal.aborted (narrow: real abort→Canceled not perm_denied, and transition runs on non-aborted latest). (4) concurrent
  pointer-load races getScene — RESOLVED via a reloadPointerSeq counter checked after every await (reloadPointer in
  5rh.24.41.8; loadColdStart in the #4562 autofix). All were NON-blocking for verbatim-from-plan+plan-READY+green. connect-es v2 CallOptions
  `(req,{signal})` validated by svelte-check (wrong shape=compile err). optional 3rd-arg signal keeps 2-arg client.test green.
  vi.advanceTimersByTimeAsync flushes microtasks between timers so async reloadPointer awaits resolve before its setTimeout fires.

- **protojson.Marshal panics-behind-"cannot fail" comment (kk1ot.2, 2026-07-04).** A builder that
  `panic`s on `protojson.Marshal` error with a comment "marshal of an in-hand proto cannot fail on
  valid fields" is a REACHABLE crash: protojson returns `errInvalidUTF8` for any proto3 string field
  holding invalid UTF-8 (`protobuf@v1.36.x encoding/protojson/encode.go:300-303`). If ANY string field
  traces to player/telnet input, the panic fires — telnet input path has NO UTF-8 validation
  (`internal/telnet/sanitize.go` is OUTPUT-only; `gateway_handler.go` reads raw via bufio). "Valid
  fields" is the load-bearing qualifier the comment glosses. Block unless input is `strings.ToValidUTF8`'d
  before marshal or the API returns `(_, error)`. Plan-sanctioned code with a false invariant comment is
  still a defect — the next-bead author trusts the comment.

- **Lua emitter→shared-builder migration (kk1ot.9 READY, 2026-07-04) — guard-vs-builder equivalence.**
  Slice pattern: hand-built JSON → `holo.comm.*` builders (pkg/plugin/comm; ParsePose/ParseOOC sigil grammar).
  KEY TRAP the PLAN ITSELF got wrong: builders NEVER return "" (build() always marshals ≥{text}), so the plan's
  Step-3 example `if payload=="" then error` is BROKEN — correct guards check INPUT (trim(msg)=="", action=="",
  text=="") BEFORE building. Verify each guard sits BEFORE the build call (no event on reject; error_response
  has no events field structurally). CRUX = trace guard's empty-decision ⟺ builder's empty-Text on EVERY sigil
  case (";"/":"/sigil-only/": "/prefix-alias+embedded-sigil): local guard strips sigil to compute `action`/`text`
  then passes RAW args to builder which re-parses via ParsePose/ParseOOC — a divergence would leak empty text or
  wrong-reject. All matched here (builder TrimSpace + guard trim idempotent). Field-fidelity: builders emit ONLY
  proto fields → no legacy character_name/message/action co-emitted (new-names-only holds for free); actor_id from
  ctx.character_id, emit actorless. Harness (mirror core-help runHelp): drive on_command GLOBAL not file-local
  handle_* (locals unreachable); stub holomush.{register_emit_type ×8, log}; RegisterStdlib gives holo.comm.
  Renderers (telnet gateway_handler stringFromPayload "actor_display_name"→"character_name", "text"→"message";
  web/translate) already prefer new fields w/ legacy fallback → field-name swap degrades safe. NON-BLOCK: rejection
  tests asserted only output string (not status/no-events); Lua success tests hit one grammar branch each (rest
  covered by pkg/plugin/comm/comm_test.go). NOTE: this MEMORY.md is 300+ lines — needs consolidation next pass.
