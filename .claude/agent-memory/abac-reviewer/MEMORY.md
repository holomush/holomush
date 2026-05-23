# ABAC Reviewer Memory

Accumulated patterns from prior reviews. Read at the start of each review; update after.

## Architecture snapshots

- Phase 1 (current): static evaluator with role-based permissions in `internal/access/`
- `AccessPolicyEngine.Evaluate(ctx, AccessRequest) (Decision, error)` — errors are distinct from denials
- Legacy adapter `AccessControl.Check()` wraps new engine at ~28 call sites — all must map engine errors to `false` (deny)
- Default posture: default-deny, no policy match = access denied

## Known invariants to check

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
- **Audit assertion gap in integration property specs (rmsi.5 Low NIT)**:
  `seed_policies_test.go` S1-S13 reset `auditWriter` in BeforeEach but no spec in the
  property block reads back `env.auditWriter.Entries()` to verify the decision was
  recorded. Engine-level audit contract is covered by `evaluation_test.go:68-70` (via
  `Eventually`), but per-property-seed audit assertion is absent. When reviewing future
  integration suites that add new seed coverage blocks, check whether the block includes
  at least one audit-trail assertion — especially for FORBID seeds where audit capture
  is the primary defense against undetected denials.
