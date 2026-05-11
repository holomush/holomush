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
