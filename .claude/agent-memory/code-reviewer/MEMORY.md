- **Invariant-registry family-renumber (.14.x hz0v4) + UNREGISTERED-FILE blindness — CONSOLIDATED.**
  Legacy family→canonical INV-<SCOPE>-N. HOLDS: residual walk `bareInvRE=\bINV-\d+\b` matches ONLY bare
  numeric INV-9 (not INV-P7-1); checkProvenance greps canonical e.ID not r.Token; PER-SITE not per-number;
  regen generated artifacts on proto INV-comment rename. CRITICAL: registry guards BLIND to files in neither
  owned_paths/shared_files/refs[] — green run≠migration complete. ALWAYS whole-tree `rg -c '\bINV-<OLD>[0-9]'
  --glob '!docs/**'`; `rg 'INV-<SCOPE>-[0-9]+\.\.[0-9]+'` for range-rewrite corruption; `rg 'TestINV_<OLD>'`.
- **Verification-BINDING backfill (pending→bound) hz0v4 READY.** Flips pending→bound + asserted_by ONLY where
  `// Verifies: INV-<id>` ALREADY exists. Meta-test does NOT cross-check asserted_by vs annotation sites →
  typo'd path passes. Hand-verify each asserted_by file contains the annotation; for bug-closing flips READ
  the cited test + confirm REAL assertion of EACH clause. `pending` MUST NOT carry asserted_by.

- **Wire event-type qualification migration (bare scene_*→core-scenes:<verb>), aneim P1/r0kup READY.**
  Three vocabularies, DIFFERENT rules — judge per-SITE not per-token: (1) registered-emit set
  (main.go phaseN EmitTypes) + (2) crypto.emits[].event_type stay BARE (INV-PLUGIN-32 set-equality +
  splitQualifiedRef); (3) wire type + verbs[].type qualified `<plugin>:<verb>`. So bare main.go/
  crypto.emits/main_test assertions are CORRECT. Checklist: (a) whole-tree `rg scene_pose|...` ex
  core-scenes/docs — proto doc-comments, generated pb, and raw-bus mintEvent/eventbus.Type synthetic
  int tests (publish via bus.Publisher() NOT RenderingPublisher→bypass verb registry, bare OK) are
  expected. (b) ALL scene_log INSERTs incl SQL `type='...'` literals — silent zero-row risk. (c) audit
  dispatch row.GetType() (qualified stored) must match `if eventType=="core-scenes:scene_pose"`. (d)
  emitEntryMatchesWireType (crypto_manifest.go) bridges bare↔qualified for sensitivity/readback. (e)
  fence non-defeat: AlwaysSensitiveSet already prefixes bare crypto.emits → qualifying scene_log.type
  CLOSES a keying gap, no fail-open. (f) harness emit helper qualifies bare→qualified IDEMPOTENTLY.
  No false-green (INV-PLUGIN-40 loader-gate/meta-test deferred). 2026-06-07.

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
