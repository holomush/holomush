- **Invariant-registry guards: renumber + binding-backfill + provenance — CONSOLIDATED.**
  (1) Renumber legacy→canonical: guards BLIND to files in neither owned_paths/shared_files/refs[] (green≠done) —
  whole-tree `rg -c '\bINV-<OLD>[0-9]' --glob '!docs/**'` + `rg 'INV-<SCOPE>-[0-9]+\.\.[0-9]+'` (range corruption).
  (2) Backfill pending→bound+asserted_by: hand-verify each asserted_by file CONTAINS `// Verifies:` (meta-test
  doesn't cross-check); bug-close flips → READ cited test, confirm REAL per-clause assertion; `pending` MUST NOT
  carry asserted_by. (3) TestProvenanceGuard reads each entry's refs[] from disk → a deleted/renamed refs file
  turns it RED for ALL citing entries; a no-refs pending→bound flip neither causes nor worsens it (judge per-entry).

- **Wire event-type qualification (bare scene_*→<plugin>:<verb>), aneim/r0kup READY, 2026-06-07.** THREE
  vocabularies, DIFFERENT rules — judge per-SITE: (1) registered-emit set + (2) crypto.emits[].event_type stay
  BARE (INV-PLUGIN-32 set-equality); (3) wire type + verbs[].type qualified. So bare main.go/crypto.emits/main_test
  assertions are CORRECT. Checklist: raw-bus synthetic int tests publish via bus.Publisher() (bypass verb registry,
  bare OK); ALL scene_log INSERTs incl SQL `type='...'` literals (silent zero-row risk); audit dispatch row.GetType()
  (qualified stored) must match qualified `if`; emitEntryMatchesWireType bridges bare↔qualified for sensitivity.

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

- **Gateway scene-RPC passthrough (5rh.8.12 READY, 2026-06-08).** RECURRING gateway-wide trap (NOT per-PR, non-blocking): `*grpc.Client` wrappers `oops.Code("RPC_FAILED").Wrap` a grpc-go status.Status err; web handler returns it to connect-go (server.go:69 NewWebServiceHandler, NO error interceptor) under a FALSE `//nolint:wrapcheck // pass through as-is`. connect-go CodeOf only sees `*connect.Error` via errors.As → oops-wrapped status collapses to CodeUnknown/HTTP500; facade PERMISSION_DENIED/NOT_FOUND all become Unknown at browser. IDENTICAL to WebListFocusPresence (handler.go:792) etc → track separately. `...PassesStatusErrorThroughAsIs` unit tests use the MOCK (no wrap) so CANNOT catch it. Token header-injected (headerInjectSessionToken), never body/logged.

- **alt-session stream-loop port (5rh.8.15): R1 NOT READY → R2 READY (2026-06-08).** ports-drop-the-hard-parts when a .svelte.ts mirrors terminal hydrateAndStream: backoff declared inside recursive fn (hoist to while-loop); connectionId gate resolved ONLY in STREAM_OPENED; streamGeneration declared-but-never-incremented = inert; Map keyed by characterId but delete(sessionId) = dead cache. "241 pass" proves nothing — `grep -c 'it('` the NEW test file.
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
