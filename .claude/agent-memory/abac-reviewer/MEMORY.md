# ABAC Reviewer Memory

Accumulated patterns from prior reviews. Read at the start of each review; update after.

## Architecture snapshots

- Phase 1 (current): static evaluator with role-based permissions in `internal/access/`
- `AccessPolicyEngine.Evaluate(ctx, AccessRequest) (Decision, error)` — errors are distinct from denials
- Legacy adapter `AccessControl.Check()` wraps new engine at ~28 call sites — all must map engine errors to `false` (deny)
- Default posture: default-deny, no policy match = access denied

## Known invariants to check

- **History-decrypt gate ≠ ABAC engine (y5inx.8, 2026-05-28)**: `authguard.checkCharacter`
  (`internal/eventbus/authguard/guard.go:67-79`) gates SENSITIVE history decrypt by
  binding_id membership in the DEK participant set — the ABACEngine is consulted ONLY on
  the player branch, NEVER character. So an allow-all policy engine in a test harness does
  NOT make the decrypt gate trivially pass; the gate is the real DEK-participant lookup.
  Scene `.ic` streams are private (membership-gated via `sessionHasMembership`, I-17,
  `stream_access.go:85-115`); ABAC never consulted for them. Two distinct layers: I-17
  membership (can SEE the frame) vs decrypt-gate AuthGuard (can decrypt). A scene member
  who is NOT a DEK participant correctly gets metadata-only (dispatcher.go:310-314), never
  plaintext, never error-fallthrough. When reviewing harness/test-support wiring that
  replicates this, confirm identity is built from session-record data (PlayerID/CharacterID
  + bindings.Current), gated on `cryptoEnabled && bindings != nil`, not client-supplied.

- `context.Background()` usage in access-critical paths loses auth context — always flag
- `TODO`/`FIXME` comments deferring security checks are blocking, not informational
- DSL attribute names must be validated against an allowlist; arbitrary strings are an injection surface
- Seed policies live in `docs/specs/2026-02-05-full-abac-design.md` — verify any new seed policy against it

## Recurring blind spots

- **Spec amendment vs. seed-policy drift**: master-spec amendments to INV-15 (the audit-stream
  denial enumeration) frequently land in the spec text + `spec_amendments_test.go` fingerprints
  but NOT in `internal/access/policy/seed.go`. Pattern: a new audit chain registers a subject
  prefix `events.<game>.system.<chain_name>` (e.g. `crypto_totp`, `crypto_policy`, `rekey`) and
  needs *two parallel forbid seeds* — one for `principal is character`, one for `principal is plugin`.
  Verify the seed list contains the new chain's namespace whenever a new audit chain is introduced.
  The `AUDIT_ONLY` dispatch filter at `internal/grpc/server.go:1019` masks the absence of the ABAC
  gate; per master §4.6/§7.7, ABAC is the authoritative gate and the filter is defense-in-depth.
- **Rekey-namespace gap (Phase 5 sub-epic E)**: shipped without a `seed:deny-events-system-rekey-read-*`
  seed despite the rekey audit chain emitting on `events.<game>.system.rekey.*` and A16 amending
  INV-15 to cover the broader `events.*.system.*` family. Caught in `2026-05-11` review.
- **Abort single-control intent**: `RekeyAbort` requires only `crypto.operator` (no admin role
  re-check, no dual-control) per INV-E17 — this is intentional, not a privilege downgrade.
  Fresh-start Rekey under a dual-control-required policy still requires dual-control;
  the asymmetry is documented in master §6.3.2 ("Abort is non-destructive; the destructive
  phase (DEK destroy) is part of rekey itself").
- **Force-destroy audit capture**: INV-E11 requires `force_destroy=true` and
  `final_missing_members` in the audit *payload* (chain row), not only in slog.
  Phase 5 sub-epic E correctly captures both in `RekeyAuditPayload.ForceDestroy` /
  `Phases.Phase5FinalMissingMembers` (`internal/eventbus/crypto/dek/rekey_phase7.go:158,161`).
- **Missing-provider class bug (holomush-g776, 2026-05-21)**: `BuildABACStack`
  in `internal/access/setup/setup.go` registers providers explicitly per
  namespace. If a seed's `when` clause references `resource.<ns>.<attr>` and
  no provider is registered for `<ns>`, the seed silently default-denies in
  production. Smoke tests at `internal/access/policy/seed_smoke_test.go`
  use hand-rolled mock providers (`locationProvider`, `streamProvider`)
  and pass despite the gap — they catch DSL/seed bugs, not wiring bugs.
  **Always audit `BuildABACStack` provider registrations vs every namespace
  referenced in `internal/access/policy/seed.go`'s `when` clauses.** Current
  known gaps after g776 fix: PropertyProvider is NOT registered (6 seeds
  depend at `seed.go:108-141`; production caller at
  `internal/world/service.go:1068` silently default-denies). ExitProvider
  and SceneProvider are also unregistered but happen to be NoOp because
  their seeds are target-only (no `when` clause).
- **xxel coverage-validator scope (2026-05-21)**: `warnOnMissingSeedCoverage`
  (`internal/access/setup/seed_coverage.go`) regex-scans `policy.SeedPolicies()`
  for `(principal|resource).<ns>.<attr>` and WARNs per unregistered namespace.
  Two structural gaps to remember on future reviews: (1) `productionRegistered`
  in `seed_coverage_test.go:132` is a hardcoded mirror, not live introspection
  of `BuildABACStack` — a refactor dropping a provider passes the test but
  fires the runtime WARN; (2) plugin-installed policies (via
  `PolicyInstaller.InstallPluginPolicies`) are NOT scanned — same silent-deny
  class can recur for plugin-declared namespaces. Symmetric stale-entry
  direction IS asserted at `seed_coverage_test.go:154-160`.
- **Co-location seed empty-string equality — FIXED (holomush-9gtl / ADR ti1b, 2026-05-22)**:
  three co-location seeds (`seed:player-character-colocation`,
  `seed:player-object-colocation`, `seed:property-public-read`) compared
  `resource.<ns>.location == principal.character.location` as raw strings.
  Pre-fix, Character/Object/Property providers emitted `""` sentinels for
  unresolved optional location attrs, and DSL `evalComparison` treats
  `"" == ""` as TRUE (only *missing* keys short-circuit to false per
  `evaluator.go:128`). Fix landed via option (b): providers OMIT the
  `location` / `parent_location` key when unresolved. The `has_X` boolean
  witness stays present. ADR `holomush-ti1b` codifies the invariant
  ("AttributeProviders MUST omit optional attrs, never emit empty-string
  sentinels"); rule file `.claude/rules/abac-providers.md` auto-loads on
  `internal/access/policy/attribute/**`. Follow-up `holomush-awb3` (P3)
  sweeps the remaining optional attrs (`owner_id`, `held_by_character_id`,
  `contained_in_object_id`, property `value` / `owner`) — those still emit
  `""` but are safe against the current seed set because the LHS in those
  comparisons (e.g. `principal.character.id`) is always a non-empty ULID.
  Regression locked at `test/integration/access/seed_policies_test.go:239-333`
  via real-engine fingerprints (NULL `location_id`, containment cycle
  constructed via raw SQL UPDATE that bypasses `ObjectRepository.Move`'s
  guard). When reviewing future AttributeProvider edits, flag any
  `attrs["X"] = ""` followed by `attrs["has_X"] = false` as a fail-open bug.
- **Substrate-side authorization (D4 / ADR x0ph) is a valid ABAC alternative**:
  Phase 5 scenes intentionally uses FocusMemberships (list maintained
  via JoinFocus/LeaveFocus) as the authorization gate, NOT
  `access.PolicyEngine.Evaluate`. The check fires inside the same
  store-lock acquisition as the write (`set_connection_focus.go:75-81`,
  `auto_focus_on_join.go:122-128`) — no TOCTOU window. When a branch
  touches focus/scene state and `internal/access/` is untouched, that's
  by design; do NOT flag missing engine calls. Verify the spec
  explicitly disclaims ABAC (Phase 5 spec has zero `ABAC|engine.Evaluate`
  hits) before accepting this pattern. The asymmetric gap: focus RPCs
  (SetConnectionFocus, AutoFocusOnJoin, JoinFocus, etc.) at
  `internal/plugin/goplugin/host_service.go` skip the
  `x-holomush-emit-token` check that EmitEvent uses — this is consistent
  across the entire focus-RPC family and bounded by substrate-membership
  gates, but worth a documentation comment citing ADR x0ph.
- **Wildcard resource IDs (`type:*`) bypass per-instance attrs**:
  `service.CreateLocation` / `service.FindLocationByName` /
  `service.CreateExit` / `service.CreateObject` all call `checkAccess`
  with `access.LocationResource("*")` etc. (`service.go:209,319,449,1033`).
  Engine matches these against seeds via `target.ResourceType`
  (`engine.go:401-405`, `parseEntityType("location:*") == "location"`), NOT
  via `when`-clause pattern matching. Providers MUST tolerate non-ULID IDs
  by returning `(nil, nil)` for wildcards — raising a parse error
  fail-closes the entire bootstrap chain. See
  `internal/access/policy/attribute/location.go:54-62` for the canonical
  shape (line-scoped `//nolint:nilerr` with rationale).
- **WARN-when-missing wiring pattern (g776 → k3ud → 72ou, 2026-05-22)**:
  three serial applications now in `setup.go` (`8b. Location`, `8c. Object`,
  `8d. Property`). The pattern: ABACConfig field for repo(+ optional helper);
  `if cfg.X != nil { register } else { slog.WarnContext(...) }`; production
  wiring always passes the dep; the WARN names affected seeds + bead ID;
  unit test mirrors the namespace into `productionRegistered`; integration
  drift-detector at `buildabacstack_seed_coverage_integ_test.go` exercises
  the REAL stack and asserts the actual-missing set equals
  `AcknowledgedMissingSeedNamespaces`. Property block (72ou) adds
  `property_repo_set` / `parent_location_resolver_set` boolean structured
  fields in the WARN — improvement over prior precedent for two-dep
  providers. When auditing future provider-wiring PRs, check that the WARN
  test EXISTS (precedent `setup_warn_integ_test.go`) and exercises the
  else-branch by INTENTIONALLY omitting deps. Note: drift-detector only
  catches if it's actually invoked with the production-shape config —
  verify the integration test passes BOTH new deps, not just the new
  ABACConfig field with nil values.
- **Property wiring (72ou) does NOT fix `service.go:1068` shape bug**:
  `Service.ListPropertiesByParent` emits `access.PropertyResource(parentType + ":" + parentID.String())`
  — a 3-segment composite (`property:location:01HXXX`) where the ID is
  non-ULID. PropertyProvider correctly returns `(nil, nil)` for non-ULID IDs
  (wildcard tolerance precedent at `location.go:54-62`), so the seeds still
  default-deny for that caller. Task 3 (`rmsi.3` / future) fixes the
  caller's resource shape. No fail-open risk; behavior is identical
  before/after this PR for that call site.
- **rmsi.3 per-property filter shape (2026-05-22)**: `ListPropertiesByParent`
  (`internal/world/service.go:1077-1104`) now fetches all properties then
  loops, issuing one `checkAccess(ctx, subject, "read", access.PropertyResource(prop.ID.String()), prefixProperty)`
  per property. INV-1 (per-property ULID resource shape, NOT parent-shaped
  composite) is pinned by the unit-test "mixed permit/deny" case at
  `service_test.go:7684-7693` via `strings.Contains(rid, p2ID.String())`.
  INV-2 (silent default-deny filter) implemented via
  `errors.Is(checkErr, ErrPermissionDenied)` no-op branch. INV-2b (infra
  failure propagation) implemented via `errors.Is(checkErr,
  ErrAccessEvaluationFailed)` early-return; defensive default case ALSO
  fail-closes on unrecognized errors (good belt-and-suspenders).
  `checkAccess` distinguishes infra-vs-deny via `decision.IsInfraFailure()`
  (PolicyID prefix "infra:") at `service.go:164`. Test covers both
  Evaluate-returns-error AND Evaluate-returns-InfraFailure-decision paths.
  Information-leak about property count: caller who can't read any
  property still triggers `propertyRepo.ListByParent`; trade-off is
  documented in spec §2 and is acceptable because pre-fix already
  default-denied uniformly (no info leak DELTA). Caller surface
  unchanged: `internal/command/types.go:71` & `internal/plugin/hostfunc/cap_property.go:28`
  return shape unchanged.
- **visible_to/excluded_from provider activation (rmsi.5, 2026-05-22)**:
  `seed:property-restricted-visible-to` and `seed:property-restricted-excluded` were dead
  code until this fix — PropertyProvider never emitted those attributes. Fix: emit ONLY
  when `len(prop.VisibleTo) > 0` / `len(prop.ExcludedFrom) > 0` (omit-when-empty, ti1b
  pattern). Schema correctly declares `AttrTypeStringList`. No default-deny integrity
  risk: the PERMIT seed only fires for listed principals, and the FORBID seed only fires
  for excluded ones. When reviewing future StringList provider additions, verify the `has`
  guard path: if the provider emits an empty list (`[]any{}`) rather than omitting the
  key, `resource has X` returns TRUE even when the list is logically absent — same
  fail-open shape as the empty-string sentinel bug (ti1b). The correct shape is:
  omit the key entirely, not emit an empty list.
- **pluginauthz shared core (8kkv5, 2026-05-25)**: `internal/plugin/pluginauthz/evaluate.go`
  is the runtime-neutral INV-5 single-source-of-truth for plugin per-action authorization.
  Key invariants confirmed: empty subject/action/resource each fail closed before engine;
  unentitled resource type fails closed before engine; engine error fails closed; zero-value
  `Decision{}` is `allowed=false` (`types.go` test line 198-203). Three recurring Low patterns
  to check in future pluginauthz edits: (1) `ActorSystem` collapses all system actors to bare
  `"system"` (drops `.ID`) — non-invertible, but safe because system actors don't enter
  plugin-eval paths in practice; flag if a new sentinel is added. NOTE (corrected
  2026-05-25 PR#4266 review): two earlier claims in this bullet were STALE/WRONG vs
  shipped code — (a) `splitResourceRef` (`evaluate.go:256-266`) REJECTS any id half
  containing a colon, so `"a:b:c"`/`"type:id:extra"` are rejected, NOT parsed; this IS
  tested (`evaluate_test.go:109` includes both); (b) nil `Engine` IS guarded and
  fails closed (`evaluate.go:142-147`, test `TestEvaluate_NilEngineFailsClosed:153`),
  it does NOT panic. nil auditor also guarded (line 213). The shared core fails closed
  on EVERY edge path with a non-nil error accompanying every non-allowing Decision.
- **Lua hostfunc nil-LState-context pattern (8kkv5.4, 2026-05-25)**: `evaluateFn`
  (and `listCommandsFn`, `getCommandHelpFn`, `checkKVAccess`, etc.) all follow the
  same pattern: `ctx := L.Context(); if ctx == nil { ctx = context.Background() }`.
  This is safe for auth (no-actor → fail-closed), but the nil branch is untested in
  every case. When reviewing future Lua hostfuncs, flag missing test coverage for the
  nil-LState-context path AND flag any bare `slog.Warn` before ctx is derived (the
  fix: hoist the ctx derivation above the nil-engine guard so `slog.WarnContext` can
  be used). The sloglint `context: scope` linter won't catch these because ctx is
  technically not yet in scope at the Warn call site.
- **GatedSubcommand SDK gate (8kkv5.6-7, 2026-05-25)**: `pkg/plugin.GatedSubcommand.Run`
  enforces structural ABAC: ResourceRef → Evaluate → Handler, three distinct early-return
  paths, no fallthrough to Handler without `Allowed:true`. Confirmed confirmed in 8kkv5.7:
  `handleExtend` has exactly ONE call site (as `Handler` field of `GatedSubcommand{}`);
  nil-evaluator guard fails closed to `CommandError`; action string in DSL matches code exactly.
  Recurring Low gap pattern for GatedSubcommand consumers: nil-evaluator branch and
  engine-error path often lack dedicated tests. When reviewing plugin subcommand
  code, always check: (1) test for nil evaluator → `CommandError`; (2) test for
  engine error → `CommandFailure`, `handlerRan == false`; (3) `rg "handleX"` across
  the repo to confirm no ungated call site exists.
- **Audit assertion gap / service-layer INV-S9 authz coexistence (rmsi.5 + 8kkv5.8)**: (1) `seed_policies_test.go` FORBID seeds lack audit-trail assertions — flag in integration suite reviews; (2) `GetPoseOrder` uses a direct `IsParticipant` store check (INV-S9, fail-closed) that is NOT replaced by the engine — intentional per spec. Dead policies in plugin.yaml (e.g., `join-open-scene`) are a Low doc risk — no fail-open.
- **Authz/business-state separation pattern (yznw, 2026-05-25)**: Removing `resource.scene.state` from ABAC `when` clauses is safe IFF the store layer enforces state via SQL `WHERE state IN (...)` + `classifyTransitionMiss`. Verified pattern for core-scenes: end/pause/resume/update/transfer all enforce via SQL; write-scene is safe because `ListScenesForCharacter` filters to active/paused (indirect, document the coupling). `InviteParticipant` and `KickParticipant` do NOT enforce state in SQL — their ABAC state clauses are the ONLY gate; retaining them is mandatory. When reviewing policy `when`-clause removals: (1) cite the store SQL guard; (2) flag if the only enforcement is indirect (membership filter, not explicit WHERE); (3) check real store, not fakeStore — fakes may omit state guards their real counterparts also omit.
- **Token-ferry pattern for PluginHostService clients (vqxkowlz, 2026-05-25)**: `hostEvaluateClient.Evaluate` and `pluginHostEventSink.Emit` both ferry the `x-holomush-emit-token` from `metadata.FromIncomingContext` → `metadata.AppendToOutgoingContext`. The ferry is safe: plugin cannot forge the incoming metadata; host validates via `tokenStore.Lookup(pluginName, token)`. Evaluate intentionally omits the self-token fallback (always command-gated). When reviewing future PluginHostService client methods, check: (1) outgoing-already-set guard present; (2) incoming→outgoing copy present; (3) self-token fallback absent (Evaluate) or present (EmitEvent) per spec; (4) missing-token path ends in `EMIT_TOKEN_MISSING`, not allow.
- **Plugin resolver wiring pattern (8kkv5.18, 2026-05-25)**: `ABACSubsystem.AttributeResolver()` returns `stack.Resolver` — the same `*attribute.Resolver` instance passed to `policy.NewEngine`. `PluginSubsystem.Start()` captures it via `resolver := s.cfg.ABAC.AttributeResolver()` (line 286) and closes over it in `WithAttributeProviderRegistrar`/`WithAttributeProviderUnregistrar` callbacks (lines 293-298). Registration happens synchronously inside `LoadAll` (line 318), before health registration (line 333) and before gRPC traffic — no TOCTOU window. Future reviewers: (1) confirm same-instance by tracing `BuildABACStack` → `ABACStack.Resolver` → `AttributeResolver()` return; (2) `Resolver` struct has NO mutex — concurrent `Evaluate` + `RegisterProvider` would data-race; safe under current single-threaded boot order but flag if startup concurrency changes; (3) plugin namespace collision fails closed (load aborts, rollback unregisters), not open; (4) `EngineProvider` interface widening is guarded by compile-time `var _ setup.EngineProvider = (*fakeEngineProvider)(nil)` in subsystem_test.go.
- **Scene publish split-gate (5rh.20.11 / Phase 6, 2026-05-24)**: `StartScenePublish`
  (`plugins/core-scenes/publish_service.go`) follows the spec §5-row-243 split: ABAC
  `publish` action is HOST-enforced at command dispatch; the handler does only
  state/budget preconditions and NEVER calls the engine (INV-P6-6 — `SceneServiceImpl`
  has no engine field; rg-asserted by `TestParticipantRPCsDoNotConsultABACEngine`).
  Same shape as EndScene/PauseScene. The `start-publish-as-participant` permit
  (`plugin.yaml`) is `permit(..., action in ["publish"], ...) when { principal.id in
  resource.scene.participants && resource.scene.state == "ended" }` — additive, no
  forbid/wildcard, default-deny preserved. `participants` = owner+member excluding
  invited (resolver.go:110, store.go GetWithMembership SQL `role IN ('owner','member')`).
  When reviewing a scene-write handler with no `engine.Evaluate` call, that is BY
  DESIGN — do NOT flag missing engine calls. DO confirm the command-dispatch task that
  invokes action `publish` actually lands (5rh.20.44 wires commands.go publish/log
  dispatch; pre-.44 only scene/scenes commands were declared), else the permit is dead
  code and the handler's preconditions are the only gate.
- **Admin-extend staged rollout (5rh.20.34 / Phase 6, 2026-05-25)**: `admin-extend-publish-attempts`
  policy (`plugin.yaml`) and `ExtendScenePublishVoteAttempts` handler (`publish_service.go`)
  ship in E1 without the `scene publish vote extend` command wiring (that lands in E2).
  During the E1→E2 window, direct gRPC callers can invoke the RPC without hitting
  the ABAC gate (handler has no in-plugin check by design). This is an accepted staged-rollout
  gap, NOT a blocking finding, provided E2 exists as a dependent bead. The pattern is
  identical to the 5rh.20.11 `start-publish-as-participant` gap above. When reviewing
  future Phase 6 plugin policy PRs: verify spec §6.1 command table against
  `commands.go` dispatch switch; flag missing command wiring as Low (not blocking) when
  the wiring bead is documented in the spec. Pin test at `publish_service_test.go:218`
  does not assert `resource is scene` — Low NIT for future reviewers to add.
- **Audit assertion gap in integration property specs (rmsi.5 Low NIT)**:
   `seed_policies_test.go` S1-S13 reset `auditWriter` in BeforeEach but no spec in the
   property block reads back `env.auditWriter.Entries()` to verify the decision was
   recorded. Engine-level audit contract is covered by `evaluation_test.go:68-70` (via
   `Eventually`), but per-property-seed audit assertion is absent. When reviewing future
   integration suites that add new seed coverage blocks, check whether the block includes
   at least one audit-trail assertion — especially for FORBID seeds where audit capture
   is the primary defense against undetected denials.
- **In-handler owner check acceptable when predicate = ABAC policy (5rh.20.13 / B4, 2026-05-25)**:
  `WithdrawScenePublish` adds `scene.OwnerID != callerID → SCENE_PUBLISH_NOT_OWNER` in-handler
  alongside the `withdraw-publish-as-owner` ABAC policy. This is CORRECT and DIFFERENT from
  the E1 admin-extend pattern (which has NO in-handler check). Acceptable when ALL of: (1) the
  spec explicitly documents the error code for that handler (spec §5.2 line 275 mandates
  `SCENE_PUBLISH_NOT_OWNER → PermissionDenied` for `WithdrawScenePublish`); (2) the plugin holds
  the owning-entity record at that code point (after `store.Get`); (3) the in-handler predicate
  is structurally identical to the ABAC `when`-clause. Closes direct-gRPC gap that E1 accepted.
  Missing `case "publish"` in `commands.go` is a Low staged-rollout gap (same as E1), not blocking.
- **E1→E2 admin-extend gap CLOSED (5rh.20.35 / E2, 2026-05-26)**: the `admin-extend-publish-attempts`
  staged-rollout gap (5rh.20.34 entry above) is now closed. E2 RETIRED the deviating top-level
  `scene extend` GatedSubcommand stub (`handleExtend` fully removed) and moved the command to
  spec §6.1's nested path `scene publish vote extend <count>` (under handlePublish→handleVote).
  Because it's nested under direct-routed sub-dispatchers, it gates via an IN-HANDLER evaluator
  (`handleVoteExtend` @commands.go:1563: `p.evaluator.Evaluate(ctx, "extend_publish_attempts",
  "scene:"+sceneID)` then `!dec.Allowed` reject BEFORE `ExtendScenePublishVoteAttempts`), the
  handleEmit/handleVote precedent — NOT the top-level GatedSubcommand. Fails closed on all three
  edges (nil eval→CommandError, engine err→CommandFailure via zero-value `EvaluateDecision{}`
  Allowed=false, deny→CommandError). resolve-before-gate ordering is accepted (resource ref needs
  resolved id; no mutation before gate). When reviewing future nested publish sub-commands, the
  in-handler gate is the correct shape and the top-level INV-7 backstop test (commands_test.go
  TestSceneGatedSubcommands_DenyWhenPolicyDenies) does NOT cover them — each needs its own
  dedicated deny-path + nil-eval + engine-err tests.
- **Settings host RPCs (iokti.7, 2026-05-30) — GetSetting/SetSetting owner-partition pattern**:
  `internal/plugin/goplugin/host_service.go`. Verified-good shape for plugin host
  RPCs touching owner-partitioned state. Trust anchor = `s.pluginName`, stamped at
  `newPluginHostServiceServer(h, manifest.Name)` (host.go:640, manifest name), NEVER from
  request/metadata. Owner bound via `base.Owner(s.pluginName)` — INV-11. The real
  `Owner(name)` (internal/settings/game.go:174) prefixes every key with `plugin/<name>/`
  (ReservedNamespace); `ValidateNamespace` (namespaces.go:41) rejects `plugin` as a host
  key, so a crafted `key` cannot escape the partition — INV-7 holds end-to-end. Single
  shared authz gate `resolveSettingScope`: nil-host→err; UNSPECIFIED→InvalidArgument;
  `actorFromToken(ctx)` (real x-holomush-emit-token → tokenStore.Lookup(pluginName,tok),
  same as Evaluate/EmitEvent) fail-closed on missing/rejected; `ActorSubject(actor)`
  (=`character:<id>`, evaluate.go:62); empty subject→PermissionDenied; nil per-scope
  store→Unimplemented; PLAYER/CHARACTER→`requirePrincipalOwnership(req.principalID, actor)`
  compares against TOKEN actor.ID (bare ULID) not a request field; default→InvalidArgument.
  GAME writes also gate `authorizeGameWrite`: nil engine→Unimplemented,
  `eng.Evaluate(write,"setting:game")`, `!dec.IsAllowed()`→PermissionDenied (IsAllowed
  types.go:228 = EffectAllow only), engine err→codes.Internal. GAME *reads* intentionally
  open (no engine) but owner-partitioned. `GrantEngine.Evaluate` (policytest/helpers.go:64)
  returns explicit EffectDeny+nil-err on no-match (deny path is real). `contextWithValidToken`
  (host_service_test.go:947) issues a REAL token into the real store — tests hit the genuine
  auth path, not a bypass. Two Low findings: (1) `setting:game` is one global resource for
  ALL plugins' game writes — coarse, but owner-partition contains blast radius; (2) GAME
  reads open — fine, owner-partitioned (TestGameSettingOwnerPartitionIsolatedAcrossPlugins).
- **Degraded-harness survival (2026-05-30)**: this worktree's bash/Read harness
  intermittently returns STALE/garbled stdout. Detection trick that works: `base64 < file`
  — if decoded bytes don't match what you wrote, the harness is replaying. Trust exit
  codes captured as the FIRST token (`TESTRC=$?`) over printed text; `task test` exit 0 is
  authoritative even when stdout is garbage. NOTE: trailing `echo ... rc=$?` can show a
  bogus `rc=2` because the PreToolUse hook appends advisory text after the command — the
  preceding printed value is still correct. The "MEMORY.md became a directory" theory from
  a prior degraded run was FALSE — the file is intact.
- **Optimistic resource-conditioned permit at class pre-flight (iokti.14, 2026-05-30)**:
  `Engine.CanPerformAction` (engine.go:406-540) resolves subject+env attrs ONLY (no
  resource provider call). A permit whose `when` references `resource.*` and fails under
  subject-only attrs is treated as `anyPermit=true` OPTIMISTICALLY (engine.go:517-520, via
  `dsl.ReferencesResourceAttrs`, refs.go:55-66). So a global class-capability conditioned on
  a per-resource attr (e.g. `scenes` cmd cap {read,scene,global} + permit
  `when{resource.scene.visibility=="open"}`) does NOT fail-safe-deny at pre-flight — board
  works in prod. The STRICT per-instance gate is `Evaluate` (gated_dispatch.go:52), which DOES
  resolve resource attrs; the optimistic branch is UNIQUE to CanPerformAction. Two recurring
  checks for this shape: (1) confirm a strict instance-level `Evaluate` consumer exists for the
  same action so private/non-matching resources are still gated (here `scene info`@commands.go:482);
  (2) watch for SILENT read-broadening: adding `permit(read,scene) when{visibility=="open"}`
  also widens any OTHER instance-level read consumer of that action (here `scene info` for
  non-members) — scope to a distinct action if board-only was intended. Board SQL also
  hardcodes `WHERE visibility='open'` (store.go:1393) as defense-in-depth. Integration tests
  calling `p.HandleCommand` directly with `allowEvaluator{}` BYPASS the host dispatcher pre-flight
  (dispatcher.go:234) — they don't cover CanPerformAction; add a real-engine pre-flight test.
- **Per-plugin GAME-write resource (iokti.15, 2026-05-30) — resolves iokti.7 Low#1**:
  `pluginauthz.SettingsGameWriteResource` const `"setting:game"` → func returning
  `"setting:game:"+pluginName` (principal.go:22). Both runtimes' authorizeGameWrite
  (host_service.go:820, stdlib_settings.go:126) call the single func — no drift. NARROWS,
  not widens: `parseEntityType` (engine.go:542, `SplitN(":",2)`) still yields type `setting`,
  the suffix only scopes the per-instance ID. NO orphaned grant: `rg "setting:game"` shows
  ZERO shipped policy/seed/yaml grants — new substrate, only tests grant it (plugin.yaml
  `setting:`/`type: setting` matches are the plugin-type field, NOT the ABAC resource).
  When auditing "const→per-entity resource" refactors: (1) rg the OLD resource string
  across seed.go + plugin.yaml `policies:` + migrations — a shipped grant on the old string
  = ORPHAN = NOT READY; (2) confirm parseEntityType still yields the same TYPE (SplitN keeps
  type stable regardless of suffix count); (3) confirm single-func source so both runtimes
  can't drift. Also iokti.15: CheckPrincipalOwnership (principal.go:58) now parses BOTH ids
  to ulid.ULID and compares values — case-insensitive (ULID encoding-independent), distinct
  ULIDs can't collide (no over-grant), empty-expectedOwnerID fail-closed branch PRESERVED
  before the parse (line 67 < 77), malformed-expected fails closed PRINCIPAL_NOT_OWNED.
- **owner→plugin nomenclature rename (iokti.17, 2026-05-30) — pure-vocabulary, INV-11 intact**:
  STRUCTURAL-ONLY reframe of the settings substrate: `Scoped.Owner(name)`→`Plugin(name)`,
  `gameOwnerSettings`→`gamePluginSettings`, `markOwnerDirty`→`markPluginDirty`,
  `dirtyTracker.owners`→`.plugins`, plus spec/ADR prose (`holomush-uvbyt`, `holomush-74ib4`).
  Verified NO substance change: (1) trust anchor untouched — partition still bound from
  `s.pluginName` (binary, struct field set at `newPluginHostServiceServer`, host_service.go:33/41)
  / registration-time `pluginName` (Lua, closed over in getSettingFn/setSettingFn; Lua sig is
  `(scope,principal_id,key)` — pluginName NOT an arg); (2) `Plugin(name)` indexes `v.plugins[name]`,
  distinct from `v.host` — `Plugin("")` keys an empty-string plugin partition, Host UNREACHABLE via
  Plugin; (3) `ReservedNamespace="plugin"` + GAME `plugin/<name>/` prefix byte-for-byte unchanged
  (namespaces.go), host-key `plugin.*` rejection intact; (4) INV-11 spec:170 + both ADRs read
  identically post-reframe, both add an explicit "word-only, invariants unchanged" footnote.
  Also iokti.17 .20: `resolveSettingScope` PLAYER/CHARACTER dedup into `principalScopedStore`
  helper (host_service.go:765) — helper takes `expectedOwnerID` as a PARAM, never chooses it;
  PLAYER passes `ownerPlayer` (token-vouched), CHARACTER passes `actor.ID`; nil-store→Unimplemented
  + PLAYER-no-owner→PRINCIPAL_NOT_OWNED both preserved. When reviewing rename/dedup "no-behavior"
  PRs: (a) rg the OLD method/identifier across non-test code to confirm zero stragglers in
  enforcement paths; (b) confirm the host/plugin partition namespace-validation asymmetry survives
  (Host validateNamespace:true vs Plugin:false); (c) confirm dedup helpers take the per-scope
  distinguisher as a param, not a hardcoded constant. Lone straggler found: spec mermaid node
  (spec:34) still says "owner BOUND to authenticated caller" — cosmetic Low, not blocking.
- **WatchScene spectate gate (5rh.8.3, 2026-06-07)**: INV-SCENE-61 pattern verified — plugin-code
  visibility/state gate BEFORE `HostEvaluator.Evaluate("spectate", "scene:"+id)`, store re-checks
  in-tx, nil-evaluator/nil-focusClient fail closed. Two recurring checks for HostEvaluator-in-service
  handlers: (1) the ABAC subject is the DISPATCH-TOKEN actor (host_service.go:565), NOT any
  request-supplied character_id — flag when the effect (row insert, focus join) uses req fields
  the gate never compared against the token actor; principal-conditioned policies on that action
  would gate the wrong subject. (2) The admin wildcard seed (seed.go:104, `action, resource` —
  no action list) permits ANY new action on ANY resource for admins — defense-in-depth on new
  actions exists ONLY if the code gate runs first. Also: observers excluded from the `participants`
  ABAC attr (store.go GetWithMembership role IN ('owner','member')) so observer auto-join grants
  no participant-clause permits. INV-SCENE-61 binding is PARTIAL (gate-order asserted; observer
  no-emit/no-pose/no-vote clause structural-only) — same shape as INV-PRIVACY-6.
- **BeginServiceDispatch token plumbing (5rh.8.21, 2026-06-07)**: host-internal mint-any-actor API
  (`goplugin/host.go:970`, `Manager.BeginServiceDispatch` via optional `ServiceDispatcher` capability)
  reuses `emitTokenStore`; per-plugin scoping enforced at `Lookup` (`emit_token_store.go:125`,
  `entry.pluginName != pluginName` → false). Identical semantics to DeliverCommand character tokens —
  no emit widening; `actor_kinds_claimable` gate at `event_emitter.go:125-134` unchanged. Reviewed
  READY. Two things to re-verify when bead .8.11 (SceneAccessService facade) lands: (1) facade actor
  MUST come from server-side-verified session state (contract is doc-comment-only); (2) the
  token+advisory-metadata pairing is convention, not enforced — WatchScene's mismatch deny
  (`plugins/core-scenes/service.go:878`) is skippable by omitting metadata; today all three minting
  paths (DeliverEvent host.go:860, DeliverCommand host.go:935, BeginServiceDispatch host.go:987)
  pair them, so token subject stays authoritative.
- **SceneAccessService facade (5rh.8.11, 2026-06-08) — READY**: host-side web scene
  authz seam (`internal/grpc/sceneaccess_service.go`, 9 RPCs). Closes the .8.21
  re-verify list. Uniform gate order on all 9: `resolveAndGate` (resolve→player
  load→IsGuest deny PermissionDenied, INV-SCENE-64) → `ownedCharacter`
  (ULID parse + ListByPlayer membership, NotFound on miss) → `beginDispatch`
  (actor=`core.Actor{ActorCharacter, verifiedChar.ID}`, INV-SCENE-63). INV-SCENE-63
  key check: token actor AND downstream `CharacterId` BOTH derive from the single
  `char` returned by `ownedCharacter` — `req.GetCharacterId()` is NEVER forwarded raw;
  they cannot diverge. Pinned by `TestSceneAccessDispatchActorEqualsVerifiedCharacter`
  (spoof-ID asserts capturedActor stays zero) + `...OverridesClientSuppliedCharacterWithOwnedAlt`
  (`// Verifies: INV-SCENE-63`). All 9 RPCs (incl. 3 public-archive + SetSceneFocus)
  enumerated in `TestSceneAccessDeniesGuestPlayersEverywhere` w/ AssertNotCalled.
  SetSceneFocus is the ONE RPC that doesn't mint a dispatch token — it delegates to
  `coordinator.SetConnectionFocus` (substrate auth, ADR x0ph); its gate is the
  connection-ownership trace: GetConnection→conn.SessionID→Store.Get→Info.CharacterID
  (session.go:203)→`ownedCharacter(playerID, CharacterID)`. Sound; can't pivot another
  player's connection. Fail-closed everywhere (NOT_CONFIGURED→Unimplemented,
  resolve-fail→Unauthenticated, dispatch-err→Internal-before-delegate). No seed/policy
  change. Two Lows (non-blocking): (1) SetSceneFocus collapses ownedCharacter's Internal
  err into PermissionDenied (:308 — fail-closed, but masks infra failure; slog still
  fires); (2) doc-only: SetSceneFocus has no token by design. `resolvePlayerSessionWithRepo`
  (auth_handlers.go:183) is the shared CoreServer+facade impl; nil-repo→NOT_CONFIGURED.
- **SetSceneFocus JoinFocus privacy gate (5rh.8.26, 2026-06-08) — READY**: SetSceneFocus
  now establishes scene FocusMembership via `coordinator.JoinFocus` (so comms_hub subscribes
  to scene streams). CRITICAL: `JoinFocus` (`internal/grpc/focus/join.go:17-59`) is
  UNCONDITIONAL — adds membership + `streamSender.Send(add=true)`, no participation re-check
  (FOCUS_ALREADY_MEMBER is just dedup). So the facade's pre-JoinFocus participation check
  (`sceneaccess_service.go:340-356`) is the SOLE authz barrier before private-scene stream
  subscription. Gate is airtight: (a) char from `ownedCharacter(ps.PlayerID, gameSession.CharacterID)`
  — verified-owned, never client-supplied (only `req.SceneId` target is client input);
  (b) oracle `ListCharacterScenes` (`plugins/core-scenes/store.go:1774`) is
  `JOIN scene_participants WHERE character_id=$1 AND archived_at IS NULL` — exactly the
  char's participant rows, ANY role (no role filter — owner/member/observer all count);
  (c) `listErr→Internal` deny (fail-closed, no fall-through to JoinFocus); (d) non-participant
  → PermissionDenied before JoinFocus; (e) clear-to-grid (`sceneId==""`) skips the whole
  block. Deny test asserts `joinFocusCalls==0` — genuinely pins no-leak. Pattern to remember:
  when a facade calls an UNCONDITIONAL substrate mutation that subscribes streams, the
  facade-side participation check is load-bearing — trace that it runs on EVERY path reaching
  the mutation, and that an oracle error fails closed (not skip).
- **SetSceneFocus DEK-participant seed (5rh.8.29.4, 2026-06-09) — READY**: extends the .8.26
  gate. After the participation gate → JoinFocus, seeds the focusing char into the scene DEK
  participant set via `s.dekAdder.Add` (`sceneaccess_service.go:411-426`) so AuthGuard
  `checkCharacter` (`guard.go:69-80`) permits decrypt. Order MUST be gate→JoinFocus→Add(fatal)
  →SetConnectionFocus. Fatal on Add err = `codes.Internal` + NO SetConnectionFocus (test pins
  `setConnFocusCall==0`). KEY INSIGHT: AuthGuard `checkCharacter` permits decrypt on
  DEK-participant-set membership ALONE (binding_id match) — NOT on focused connection or
  FocusMembership. So a dangling DEK-participant entry (after a fatal SetConnectionFocus) DOES
  confer standalone decrypt capability — but it's benign because the participation gate already
  authorized that char to decrypt the scene, and no-focus = no stream = no ciphertext to decrypt.
  Idempotent Add (`manager.go:357-359` added=false) self-heals on retry. `s.dekAdder==nil` is a
  legit degraded mode (T5 wires only when `RekeyManager != nil`, i.e. crypto stack present), NOT
  a bypass — no crypto stack = no encrypted events = nothing to gate. Identity: `gameSession.CharacterID`
  is `session.Info.CharacterID` (session.go:203, server-stored), validated by
  `ownedCharacter(ps.PlayerID, ...)`; only `req.SceneId`/`req.ConnectionId` are client input.
  ADR `holomush-mihtk` documents the fatal-refuse decision + benign-dangling rationale.
  Two Lows: (1) `// Verifies: INV-CRYPTO-116` references a registry id NOT YET in invariants.yaml
  — registered in plan Task 11 of the SAME epic (forward-ref within sequenced epic, NOT a
  fabricated binding; orphaned master INV-8 being registered); (2) dangling DEK-participant residue
  is broader than the plan's "dangling FocusMembership" framing but equally benign. Pattern: when
  a host facade seeds a crypto-participant set, the AuthGuard char-branch gates on set-membership
  alone — confirm the seed is preceded by an authz gate that ALREADY licenses decrypt, else the
  seed over-grants.
- **Exact-wildcard cap permit vs type-match own-location (kplrr, 2026-06-18) — READY**:
  hostcap interceptor now runs default-deny ABAC for EVERY non-exempt capability;
  non-scoped methods eval at type level `resource=md.Resource+":*"`
  (`interceptor.go:186`), scope-eligible keep dispatch/extract/own-location.
  11 new `seed:plugin-cap-*` permits match the sentinel EXACTLY via
  `resource == "<type>:*"` (ResourceExact) NOT `resource is <type>`. KEY engine
  fact: `findApplicablePolicies` ResourceExact requires `req.Resource ==
  *target.ResourceExact` string-equal (`engine.go:374-377`) — so `location:*`
  permit canNOT match scoped `location:<realid>` from CreateExit/CreateObject;
  those match ONLY `seed:plugin-world-mutation-own-location` (`resource is
  location` + `when{resource.location.id==action.dispatch_location}`). own-location
  also doesn't spuriously fire at `location:*` (LocationProvider returns nil for
  wildcard → missing attr → when=false). No path writes an arbitrary existing
  location (CreateLocation = new-only; the only existing-location mutations are
  scope-eligible). Exempt={emit,command-registry} (`descriptor.go:48`). Subject=
  `access.PluginSubject(d.PluginName)` host-derived; non-scoped scopeAttrs=nil +
  unconditional permits → no attr-influence escalation. Audit flows for both
  paths (Auditor passed to EvaluateCapabilityAccess). Completeness guard
  `TestEverySeededCapabilityResourceHasDefaultPermit` is total for non-scoped
  non-exempt only (skips scope-eligible — fail-closed if a scoped seed missing).
  ONE Medium (doc-only): seed.go:411 + interceptor.go:117 claim the system forbid
  `seed:deny-events-system-read-plugin` "still overrides" stream reads, but the
  plugin `stream.history` handler `QueryStreamHistory` (`servers.go:818-883`) runs
  NO instance-level ABAC on req.GetStream() — only gate is cap interceptor at
  `stream:*`, where the `events.*.system.*` glob can't match `*`. NOT a regression
  (pre-change the non-scoped short-circuit forwarded ungated entirely); the forbid
  only bites paths with a concrete-name check (CoreServer query_stream_history.go).
  Pattern: when a non-scoped capability handler lacks its own instance-level ABAC
  check, the type-level `<type>:*` cap permit is the ONLY gate — a `when`-clause
  forbid on concrete attrs is dead weight there. Always trace whether the served
  handler re-checks the concrete resource before trusting a forbid to protect it.
