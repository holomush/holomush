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
  (.8.4 role-gates) still open; plan scheduled flip for final Task 20 — check WHICH task owns the registry flip.

- **5rh.8.21 service-dispatch token (READY, 2026-06-07).** Host.BeginServiceDispatch (host.go:968) mints a
  dispatch token for server-side-verified actor+ownerPlayerID; Revoke idempotent + 5min TTL sweeper. Binary-only
  via findOptional[ServiceDispatcher] (SERVICE_DISPATCH_UNSUPPORTED for Lua) = transport-specific, NOT a symmetry
  violation. WatchScene advisory-md check (service.go:878) is restriction-only: spoofed md can only restore baseline,
  never grant — correct defense-in-depth polarity. Caller MUST pass server-verified actor; token outlives unload (Low).
- **Harness event-path parity (5rh.8.4, 2026-06-07).** Production busEventAppender publishes via wrapPublisher's RenderingPublisher (sub_grpc.go:207) — a harness mirror using raw `bus.Bus.Publisher()` ships nil-Rendering frames (INV-EVENTBUS-6: gateway drops them); check publisher WRAPPING not just Append fidelity. harness.go is SHARED — a change un-dropping events affects EVERY suite → require full `task test:int`. Fake-level exclusion pins (re-implement role filter in Go) pin the fake not the SQL — demand a DB-level twin.

- **Gateway scene-RPC passthrough (5rh.8.12 READY, 2026-06-08).** RECURRING gateway-wide trap (NOT per-PR, non-blocking): `*grpc.Client` wrappers `oops.Code("RPC_FAILED").Wrap` a grpc-go status.Status err; web handler returns it to connect-go (server.go:69 NewWebServiceHandler, NO error interceptor) under a FALSE `//nolint:wrapcheck // pass through as-is`. connect-go CodeOf only sees `*connect.Error` via errors.As → oops-wrapped status collapses to CodeUnknown/HTTP500; facade PERMISSION_DENIED/NOT_FOUND all become Unknown at browser. IDENTICAL to WebListFocusPresence (handler.go:792) etc → track separately. `...PassesStatusErrorThroughAsIs` unit tests use the MOCK (no wrap) so CANNOT catch it. Token header-injected (headerInjectSessionToken), never body/logged.

- **Frontend/E2E ports + proto-vocab traps (5rh.8.15/.16/.17/.19, 2026-06-08) — CONSOLIDATED.** (a) .svelte.ts ports DROP hard parts: backoff inside recursive fn (hoist to while-loop), gate resolved only in STREAM_OPENED, counter declared-never-incremented=inert, Map keyed charId but delete(sessionId)=dead cache; "N pass" proves nothing — `grep -c 'it('` the NEW file. (b) Load-bearing blocker often in the CONSUMED store not the diff (refresh() hardcoded asCharacterId:'' → blank roster): trace each consumed field to its PRODUCER, verify POPULATED not just typed; pnpm-check blind to empty-but-valid. (c) proto STRING field vs literal: grep the proto/Go const set — UI authors cross state↔visibility↔role vocab (SceneInfo.state∈{active,paused,ended,archived}, 'open' is VISIBILITY → state==='open' never true). nav params DEAD unless +page reads searchParams; charId='' → RPC never fires. (d) re-check a prior NOT-READY blocker STILL holds in CURRENT source before failing a dependent PR (upstream may have fixed). Honest-degradation OK iff test title+comment scope it + cite a tracked bead. Flakiness: no sleeps; expect.poll/toPass; conn-pill gate; unique titles.
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

- **hostcap Lua adapter (eykuh.2.6) + parity fixes (eykuh.2.14 READY, 2026-06-12) — CONSOLIDATED.**
  luaHostCapAdapter wraps *hostfunc.Functions; symmetric to binary *goplugin.Host. DECISIVE FACT for 2.6:
  newLuaHostCapAdapter is TEST-ONLY; bufconn endpoint (T7) makes methods reachable → "no live caller →
  track" per eykuh.2.5. 2.14 FIXED the 2.6 tracked gaps; verified all:
  (A) FocusOps→Coordinator lossy-field DISCIPLINE: server-reachable methods (servers.go) must populate
  EVERY field the SERVER reads, lossy-OK only for fields confirmed-unread. VERIFY by reading the focusServer
  method: SetConnectionFocus discards result (`_, err = fc.SetConn`, servers.go:201)→zero-result safe;
  AutoFocusOnJoin reads ONLY total+3 slices+FocusFailure{ConnID,Reason} (servers.go:283-307)→adapter must
  map exactly those (SessionID/CharLocationID unread=lossy-OK). Confirm signatures identical for direct
  delegations (IsAnyConnFocused/GetConnectionFocus). RestoreFocus/RestoreConnectionFocus called ONLY from
  internal/grpc/ (list_session_streams.go:81, server.go:943) NOT hostcap → genuinely unreachable, UNSUPPORTED-stub OK.
  (B) Settings store-recovery via type-assertion = TRUE PARITY iff: lua.Host.SetSettingsStores (host.go:144-161)
  installs *settingsStoresOpsAdapter ONLY when all3 non-nil else SetSettingsOps(nil); type-assert comma-ok
  (no panic); the recovered seam is the SAME one Lua get/set_setting hostfuncs consume; settingsServer
  fails-closed Unimplemented on nil store (servers.go:667,712). No partial-wiring path.
  (C) sessionAdminServer nil-guard: single-capture local then nil-check (session.go:104-108) — no double-call;
  returns codes.Unimplemented (not Internal), no leak. SessionAdminService is LuaDefaultSet-ONLY (register.go:53-58,
  register_test asserts binary EXCLUDES it) → binary nil SessionAdmin never reached, no regression.
  OwnedResourceTypes: non-nil empty map at parity w/ evaluate.go:57 literal+comment. RECURRING: when adapter
  is dead-code-until-next-task, verdict hinges on live-caller; for ANY server-reachable method read the
  CONSUMER server body to classify each return field read-vs-unread before trusting a "lossy but unread" claim.

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
