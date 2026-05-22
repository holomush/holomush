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
