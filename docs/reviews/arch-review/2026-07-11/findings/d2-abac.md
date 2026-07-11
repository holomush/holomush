# D2 ABAC — Findings

**Agent:** abac-reviewer / Opus 4.8 · **Date:** 2026-07-11 · **Scope examined:** `internal/access/` (engine, DSL evaluator, all attribute providers, resolver, circuit breaker, seed/bootstrap, policy store), `internal/command/` two-layer dispatch + dispatcher, `internal/world/service.go` instance-level chokepoint, `internal/plugin/hostcap/` capability interceptor + descriptors, `internal/plugin/policy_installer.go`, `internal/store/role_resolver.go`, `docs/architecture/invariants.yaml` (INV-ACCESS scope).

## Summary

The ABAC surface is **SOUND with material-but-latent gaps**. The core is well-engineered: a 10-step fail-closed evaluation algorithm where every error path returns `EffectDefaultDeny`; Cedar-style fail-safe DSL (missing attr / type mismatch → false, depth-bounded, glob-limited); deny-overrides that override even `admin-full-access`; and a plugin capability interceptor that stamps `plugin:<name>` subjects (no character forgery) and fails closed on missing dispatch/extractor/resource (INV-PLUGIN-52, bound). No exploitable auth-bypass or default-allow path was found. The gaps are latent fail-open hazards not reachable through current in-tree seeds but which violate documented MUST invariants and would bite plugin-installed policies: three attribute providers emit empty-string sentinels (ADR ti1b violation), and an open provider circuit breaker silently drops attributes with no error (fail-open for forbid policies). Counts: **Blocker 0 · High 0 · Medium 2 · Low 3 · Info/Strength recorded below.**

## Findings

### MED-1 Three attribute providers emit empty-string sentinels for optional attrs (ADR ti1b violation)
- **Severity:** Medium
- **Claim:** `LocationProvider`, `ObjectProvider`, and `PropertyProvider` emit `attrs["X"] = ""` + `attrs["has_X"] = false` for absent optional attributes — the exact forbidden form ADR holomush-ti1b (bug holomush-9gtl) mandates against — creating a latent fail-open `"" == ""` match.
- **Evidence:** `internal/access/policy/attribute/location.go:72,80` (`owner_id`, `shadows_id`); `object.go:117,125,133` (`owner_id`, `held_by_character_id`, `contained_in_object_id`); `property.go:93,102` (`value`, `owner`). Sentinels ARE reachable by DSL: `mergeAttributes` writes `bagKey = "<namespace>.<key>"` (`resolver.go:503`), so e.g. `resource.property.owner` resolves the sentinel. Contrast the compliant path: `character.go:141-148` correctly OMITS `location` when absent, and `object.go:137-144` correctly omits `location`.
- **Impact:** Not exploitable via current seeds — the two seeds comparing these (`seed:property-private-read`/`-owner-write`, `seed.go:119,131`) compare `resource.property.owner == principal.character.id`, and `character.id` is always a real non-empty ULID, so `"" != ULID`. The hazard is any future or **plugin-installed** policy comparing two optional attributes on the same object (e.g. `resource.object.owner_id == resource.object.held_by_character_id`, both `""` → permit) or comparing an optional attr against another empty-sentinel value.
- **Recommendation:** Replace the `else { attrs["X"] = "" }` branches with omission (drop the key; keep only the `has_X` witness), matching `character.go`/`stream.go`. Add tests asserting the value key is absent when the source is nil.
- **Dedup:** related to #4773 (registers INV-ACCESS for omit-optional-attrs + scene resolver), but that issue does not cover these three providers' ID/string sentinels.

### MED-2 Open provider circuit breaker silently drops attributes with no error → fail-open for forbid policies
- **Severity:** Medium
- **Claim:** When a provider's circuit breaker is open, `resolveEntity` skips the provider via `continue` **without appending an error**, so `Resolve` returns partial bags with `nil` error; the engine then evaluates policies against missing attributes. Missing-attr→false is fail-closed for permits but **fail-open for any forbid whose condition references the skipped provider** (the forbid silently does not fire, letting a co-applicable permit through under deny-overrides).
- **Evidence:** `internal/access/policy/attribute/resolver.go:360-362` (`if cb.ShouldSkip() { continue }`, no error path) vs. the engine's fail-closed-on-resolve-error contract at `engine.go:212-239`. Breaker trips on latency-budget utilization (`circuit_breaker.go:195-226`), independent of the request.
- **Impact:** Not currently exploitable: the only DB-backed provider referenced by a forbid is `property` (`seed.go:143`), whose matching permit (`seed:property-restricted-visible-to`) also depends on the property provider → net fail-closed; the audit-stream/crypto/rekey forbids (`seed.go:228-299`) reference `StreamProvider`, which is pure computation (`stream.go` — no I/O) and therefore never trips. Latent for plugin-installed forbids over DB-backed providers, and a robustness gap: under DB load a forbid carve-out can silently weaken for the 60s open window.
- **Recommendation:** Treat a skipped (circuit-open) provider whose namespace is referenced by any candidate **forbid** as a resolution failure (fail closed), or surface a per-namespace "unavailable" marker the evaluator treats as deny-preserving for forbids. At minimum, document the fail-open-for-forbid semantics at the skip site.
- **Dedup:** possibly related to #4616 (ABAC Phase 7.7 bug cluster names "circuit breaker"); this specific forbid-fail-open framing is not confirmed covered.

### LOW-1 `CanPerformAction` ignores its scope parameter — Layer-2 capability preflight does not enforce Scope
- **Severity:** Low
- **Claim:** The engine's Layer-2 capability check discards the scope argument (`_`), so a command capability declaring `ScopeSelf` vs `ScopeGlobal` for the same action/resource-type receives identical Layer-2 treatment; scope fencing relies entirely on policy conditions plus the world-service instance-level `Evaluate`.
- **Evidence:** `internal/access/policy/engine.go:406` (`CanPerformAction(ctx, subject, action, resourceType, _ string)`); callers pass `capability.EffectiveScope()` (`internal/command/access.go:72`) which is dropped.
- **Impact:** Mitigated in practice — real instance+scope enforcement happens at `internal/world/service.go:136` `checkAccess` (well-tested `_VerifiesAccessRequest` suite). But it is a footgun: a command author who believes declaring `ScopeSelf` constrains at Layer 2 is mistaken, and any handler that mutates state solely on the optimistic preflight (no instance-level `Evaluate`) would have no scope fence. `resetpassword.go:113-134` gates only via `CanPerformAction` (scope-dropped) — safe only because the underlying capability is admin-gated.
- **Recommendation:** Either enforce the scope in `CanPerformAction` (fold it into candidate filtering) or rename the param and document that command-capability scope is advisory, with a lint/test ensuring every mutating handler performs an instance-level `Evaluate`.
- **Dedup:** none.

### LOW-2 `RoleResolver.GetRoles` swallows store errors and defaults to `[player]`, bypassing engine fail-closed-on-provider-error
- **Severity:** Low
- **Claim:** On a role-store error `GetRoles` logs and returns `nil`; `CharacterProvider` then substitutes `[player]`. This does not propagate an error up the resolver, so the engine's fail-closed-on-provider-error protection never engages for role failures.
- **Evidence:** `internal/store/role_resolver.go:33-36` (`return nil` on error); `internal/access/policy/attribute/character.go:119-121` (`if len(roles)==0 { roles = []string{RolePlayer} }`).
- **Impact:** Fail-closed and safe for admin/builder permits (a role-lookup failure demotes to plain player → denied — a reasonable availability degradation). But it is fail-open for any role-gated **forbid**; none exist today (roles are only player/builder/admin), so this is latent/consistency-only.
- **Recommendation:** If a role-gated forbid is ever introduced, make role-store errors propagate as a resolver error (fail closed) rather than silently degrading to `[player]`. Document the current degrade-to-player choice.
- **Dedup:** none.

### LOW-3 INV-ACCESS-7 / INV-ACCESS-8 (deny char/plugin reads of system/audit/crypto streams) are `binding: pending`
- **Severity:** Low (Info-adjacent per ground-rule 5)
- **Claim:** Two load-bearing plaintext-leak guarantees — characters/plugins MUST NOT read `events.*.system.*`, `audit.*`, `crypto_totp.*`, `crypto_policy.*` streams — carry `binding: pending`, so the registry does not ratify a test as proving them.
- **Evidence:** `docs/architecture/invariants.yaml:2137-2158` (INV-ACCESS-7/-8, `binding: pending`). Referencing (but unratified) tests exist: refs cite `internal/access/policy/seed_test.go`, `seed_smoke_test.go` tokens `INV-15`/`INV-A16`; the seeds themselves are present and correct (`seed.go:225-299`, including the broad `events.*.system.*` forbid that closes the prior rekey gap).
- **Impact:** The seeds and referencing tests exist, so practical risk is low; a regression that deleted a forbid seed might not be caught by a ratified invariant binding until the backfill epic completes.
- **Recommendation:** Ratify the bindings (backfill epic holomush-hz0v4.14) — confirm `seed_smoke_test.go` genuinely asserts each forbid and flip to `binding: bound`.
- **Dedup:** known backfill gap per ground-rule 5; not a novel discovery.

## Strengths

- **10-step fail-closed engine**: every abnormal path (ctx-cancel, degraded mode, invalid request, session-resolve error, attribute-resolve error, cache-unavailable, no-candidate, decision-validate) returns `EffectDefaultDeny`; no path returns allow on error (`internal/access/policy/engine.go:82-342`). System bypass is gated on `access.IsSystemContext(ctx)` (S1 defense, `engine.go:92-101`); degraded mode denies all non-system requests (`engine.go:128-154`).
- **Cedar-aligned fail-safe DSL**: missing attribute and type mismatch → false (`evaluator.go:130-164`); nesting depth bounded (`MaxNestingDepth`, `evaluator.go:81-84`); negation guarded so a depth-exceeded subtree cannot flip to `true` (`evaluator.go:87-92`); glob patterns validated (len ≤100, ≤5 wildcards, no `[`/`{`/`**`, `evaluator.go:294-315`).
- **Deny-overrides is absolute**: forbids override even `seed:admin-full-access`; `combineDecisions` scans all satisfied policies for a deny before any allow (`engine.go:591-611`).
- **Plugin capability interceptor is exemplary**: subject is always `access.PluginSubject(d.PluginName)` — a plugin can never evaluate as a character (`interceptor.go:301`); scope-eligible calls fail closed on missing dispatch (`SCOPE_NO_DISPATCH`), missing extractor (`SCOPE_NO_EXTRACTOR`, INV-PLUGIN-52 bound), or unresolved resource (`SCOPE_NO_RESOURCE`) (`interceptor.go:269-294`); unclassified methods, empty plugin name, and nil engine all fail closed (`interceptor.go:222-255`, `EVALUATE_NO_ENGINE`). A meta-test enforces every scope-eligible descriptor method wires an extractor (`descriptor_completeness_test.go:21`).
- **Two-layer command dispatch backed by a real instance-level chokepoint**: Layer 1 (`Evaluate execute command:<name>`) → Layer 2 (`CanPerformAction` per capability) → handler, all fail-closed (`internal/command/dispatcher.go:288-320`); focus redirects rewrite the command name BEFORE both layers (test `dispatcher_focus_test.go:309`); the optimistic Layer-2 permit is enforced concretely by `world.Service.checkAccess` which calls `engine.Evaluate` with the specific `<type>:<id>` instance (`internal/world/service.go:136-181`, extensively covered by `_VerifiesAccessRequest` tests).
- **Policy mutation is not a runtime attacker surface**: policies change only at bootstrap (seeds) and plugin-load (`policy_installer.go`); plugin policies are constrained to `principal is plugin` and to the installing plugin's own name (`policy_installer.go:78,171`), preventing cross-principal escalation; source/name coupling is validated (`store.go:70-109`); a policy-change version trail exists (`access_policy_versions`, `created_by`, `postgres.go:195`).
- **Character self-write cannot self-promote**: roles are not writable through the character-write path (only `UpdateCharacterDescription` exists, `world/service.go:656`); roles come from a separate `RoleResolver`.
- **Robustness**: provider panic recovery + per-provider circuit breaker (`resolver.go:422-457`), resolver re-entrance guard (`resolver.go:169-171`), and audit logging on every decision path (allow, deny, and infra-failure) with graceful audit-failure handling.

## Not examined

- Web BFF typed-RPC ABAC wiring and gRPC `Subscribe`/`QueryHistory` action-verb mapping (the enforcement points where INV-ACCESS-7 actually binds) — outside `internal/access`; verified the seeds exist but not the handler wiring.
- The `audit.Logger` tamper-resistance / whether an evaluated subject can influence its own audit records.
- Full DSL parser/compiler internals (`parser.go`, `compiler.go`, `validate.go`) beyond the evaluator's fail-safe semantics.
- The `crypto.operator` break-glass grant flow and TOTP-gated escalation (touched `grants.go` only).
- Scene/channel plugin-shipped policies in depth, and the `test-abac-widget` plugin.
- `seed_smoke_test.go` assertion strength for INV-ACCESS-7/8 (would confirm whether LOW-3 is bookkeeping-only or a real coverage gap).

## Verdict

**SOUND** — the ABAC surface enforces default-deny end-to-end with fail-closed error handling, an absolute deny-override, and a strong plugin isolation boundary; the two Medium findings are latent fail-open hazards (sentinel providers, circuit-breaker-open) that are not reachable through current in-tree seeds but violate documented MUST invariants and should be fixed before external plugin authors ship policies.
