- **Registry-renumber series (.14.12–.14.27, ~9 legs, all READY except .14.23) — CONSOLIDATED.**
  Each leg renames a legacy invariant family (GW/ROPS/P7-split/P4/P5/P6/FS/SCENE/PLUGIN/ACCESS/
  CRYPTO/S*/M*/COMMAND) to canonical `INV-<SCOPE>-N`, dense non-contiguous ascending-by-position.
  Recurring checks that HOLD: (1) Per-file token scan of EVERY owned file → ONLY `INV-<SCOPE>-N`;
  residual walk `bareInvRE = \bINV-\d+\b` (invariant_registry_test.go:488) matches ONLY bare
  NUMERIC `INV-9` — NOT `INV-P7-1`/`INV-RB-1`/`INV-PLUGIN-22` (the `-XX-` segment breaks `\d+`),
  and `continue`s on shared files (~line 554). So foreign/deferred tokens survive ONLY in
  shared_files, and descriptive prose like `INV-P7-1..16` in an owned file is INERT. (2)
  checkProvenance greps CANONICAL `e.ID` at each ref site, NOT `r.Token` — refs recording legacy
  `token:"INV-15"` is the standing FROM-anchor convention; the canonical id must ALSO be
  physically present. (3) TestOwnedPathsPartition only forbids the SAME glob in two scopes'
  owned_paths; an unowned shared file is permissible (checkProvenance accepts shared-OR-owned).
  Genuinely multi-scope files carry BOTH tokens, listed in both scopes (one owned, one shared).
  (4) PER-SITE not per-number: same bare number → different outcome per file (bare INV-1 →
  PLUGIN-22 in plugin-evaluate files, LEFT bare in command-visibility files). (5) Coverage
  meta-tests: distinguish `testName`-EXISTENCE checks (go/parser Test* names; t.Run `inv` labels
  robust to rename, MIGRATE in lockstep) from fragile `// Verifies:`-grep scanners (DELETE —
  .14.9). (6) refs:[] spec-only/binding:pending is HONEST when the origin spec maps a mechanism
  to the PARENT token's code site, not its own (M4/5/6→parent PLUGIN-32; W9ML 1..6).
  TestEveryRegistryInvariantHasBinding tolerates pending+refs:[]. (7) Generated artifacts
  (.pb.go/_grpc.pb.go/.connect.go/_pb.ts/_connect.ts/grpc-api.md) must regen in sync when a proto
  INV-comment is renamed. (8) Deferred-bare-INV + foreign tokens → use file-path owned_paths NOT
  `dir/**` globs (else residual walk trips on left-behind tokens) — .14.14. (9) Trailing-comma
  `token:"...",}` noise recurs ~6×; renderer-inert, Low non-blocking. (10) Zero executable-line
  edits expected — every .go diff line is a comment or test-assertion string-literal token swap.
  Final counts: CRYPTO=67/PLUGIN=39/ACCESS=8/EVENTBUS=28/SCENE=60; COMMAND-scope created by LAYER
  split (Go-backend→COMMAND, web-composer TS LEFT exempt). Encountered .14.12–.14.27 (2026-06-02..04).

- **First NOT-READY in the series: registry guards are BLIND to unregistered files, so a green
  meta-test run does NOT prove a family migration is complete (hz0v4.14.23 INV-F NOT READY).**
  Two defects survived a fully-green run (lint:invariants, provenance, partition, binding, 617
  unit tests, int-compile): (A) **51 residual bare `INV-F*` token refs** in THREE cmd/holomush
  files NOT in the diff (admin_read_stream_e2e_test.go ×45, readstream_wiring_test.go ×4,
  admin_authenticate_e2e_test.go ×2) — same family migrated, but files neither owned_paths nor
  shared_files nor in any `refs[]`, so residual walk + provenance never look → green despite
  incomplete. 4 are STALE cross-file func-name cites (`TestINV_F2/F6/...`) pointing at funcs THIS
  diff RENAMED → dangling. (B) **Range-rewrite corruption**: `INV-P7-1..16` → `INV-CRYPTO-38..16`
  (tool rewrote left side of a `..N` range, left suffix); substring `INV-CRYPTO-38` keeps
  provenance green, CI blind. **Review pattern for family migrations: do NOT trust green guards
  as proof of completeness. ALWAYS `rg -c '\bINV-<OLD>[0-9]' --glob '!docs/**'` over the WHOLE
  tree (not just diffed files) — residual hits in unregistered files = incomplete. Also
  `rg 'INV-<SCOPE>-[0-9]+\.\.[0-9]+'` for range corruption, `rg 'TestINV_<OLD>[0-9]'` for
  dangling renamed-func cites.** Encountered: hz0v4.14.23 (2026-06-03) — NOT READY.

- **Verification-BINDING backfill (pending→bound flip, NOT a renumber) — hz0v4 binding-backfill
  READY.** Flips registry entries pending→bound + adds asserted_by, ONLY where a `// Verifies:
  INV-<id>` annotation ALREADY exists; may add the comment to a pre-existing asserting test.
  TestEveryRegistryInvariantHasBinding walks ALL *_test.go (raw regex, build tags irrelevant —
  integration files ARE scanned); a `bound` entry needs ≥1 annotation ANYWHERE, and asserted_by
  is NOT cross-checked against annotation sites — so a typo'd/fabricated asserted_by path passes
  the gate. MUST hand-verify each asserted_by file genuinely contains the annotation. `pending`
  MUST NOT carry asserted_by. For bug-closing flips (0sh1k 'asserted only in comments'), the
  `// Verifies:` comment does NOT fix a false-green — READ the cited test and confirm a REAL
  runtime assertion of EACH invariant clause (CRYPTO-28: audit-emit = WaitForOneJetStreamMsg on
  AUDIT stream + header assert; fail-closed = audit=nil → res.Err set, no plaintext). Gates:
  `inv-render -check` exit 0; meta binding+provenance green. Encountered: 2026-06-05 — READY.

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

- **Frontend stream-loop port (alt sessions / scenes workspace, holomush-5rh.8.15) — NOT READY.**
  When a .svelte.ts module claims to "mirror the terminal hydrateAndStream pattern", DIFF it against
  terminal/+page.svelte — ports drop the hard parts. Concrete bugs found, all green-passing: (1)
  Backoff `let reconnectDelayMs=1000` declared INSIDE the recursive openStream → re-init to 1000 every
  reconnect; 1s→30s cap is dead code, recursion grows stack. Hoist backoff to session + loop, don't
  recurse. (2) connectionIdPromise resolved ONLY in STREAM_OPENED branch; error/close-before-open
  never reject → consumer `await awaitConnectionId` deadlocks. Terminal ref has waitForStreamReady w/
  10s timeout + rejectStreamReady on close — ports omit both. (3) `streamGeneration` field declared but
  NEVER incremented anywhere → the entire `gen !== session.streamGeneration` stale-frame guard is INERT
  (grep for the increment site; if absent the guard is theater). (4) Map keyed by characterId but
  delete called with sessionId → no-op delete, dead session cached forever. (5) Test-count claim is
  whole-suite ("241 pass") but commit adds N tests, ZERO exercising the async surface — no fake
  streamEvents async-iterator test → all the above survive green. ALWAYS `grep -c it(` the actual new
  test file and check a fake-iterator test exists. Encountered 2026-06-08 — NOT READY.
  ROUND 2 (2026-06-08, commit mmwsokrtpwpl) — READY. All 6 blockers + Medium genuinely fixed in
  CODE (not just test-greened): backoff hoisted to AltSession.reconnectDelayMs + while-loop +
  30s cap + reset on STREAM_OPENED; awaitConnectionId rejects on close/error + 10s timeout +
  fresh gate reinstalled; STREAM_CLOSED rejects+evicts; all delete sites use characterId;
  streamGeneration NOW incremented (line 183) + controller swapped/aborted + closeSession aborts;
  altSessions.test.ts (7 fake-iterator tests) pins it; ingestEvent routes by parsed scene_id.
  Caveat noted: generation guard's `localGen!==streamGeneration` branch is unreachable in current
  topology (single sequential loop, ensureSession dedups) — defensive not dead. Non-blocking.
  makeGate's promise.catch(()=>{}) defuses unhandled-rejection on no-consumer gate. (MEMORY now
  >200 lines — collapse this 5rh.8.15 two-part entry to one line next curation pass.)
