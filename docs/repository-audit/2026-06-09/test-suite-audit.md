<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Test-Suite Audit — All Tiers (holomush-8lqco)

- **Date:** 2026-06-09
- **Bead:** `holomush-8lqco` — *Audit test suite across all layers: collapse redundancy, cut bloat, raise coverage*
- **Baseline commit:** `f350ac37` (`origin/main` tip at audit time)
- **Scope:** read-only discovery/validation/audit. No refactors land under this audit; each actionable item is a follow-up bead (§6).
- **Conventions audited against:** `.claude/rules/testing.md` (tiers, ACE naming, table-driven, coverage targets, quarantine), `.claude/rules/invariants.md`, `docs/architecture/invariants.yaml`, `test/meta/` guards.

## Methodology and caveats

1. **Inventory and coverage** were computed directly (not sampled): `task test:cover` (gotestsum, `-race -covermode=atomic -coverpkg=./...`) ran the full unit tier — **9,156 tests, 3 skipped, exit 0, ~84 s**. Per-package percentages were derived from `coverage.out` by deduplicating each statement block (`file:start,end`) across test binaries and taking max count — necessary because `-coverpkg=./...` repeats every block once per test binary, which deflates naive aggregates.
2. **Coverage numbers are unit-tier only.** `task test:cover` does not compile `//go:build integration` files, so Postgres-backed packages (repositories, stores) legitimately show near-zero here while being integration-covered. Every below-target package was cross-checked for integration-tier supplementation before being called a gap.
3. **Qualitative sweeps** (COLLAPSE / CUT / COVER / APPROACH) were performed by five parallel read-only agents, with the highest-leverage claims re-verified in the parent session. Two agent claims **failed verification and were discarded**:
   - "`coretest.NewMemoryEventStore` is dead" — false; it has 66 references in `internal/grpc/auth_handlers_test.go` alone.
   - Specific "untested branch" line citations in `internal/eventbus/crypto/dek/manager.go` were wrong (e.g. `Resolve` is at `manager.go:301`, not `:27`); the functions exist but the negative-coverage claims are downgraded to a gap-analysis recommendation (holomush-psr40) rather than asserted.
4. Counts were re-verified mechanically with `rg`/`fd` at the baseline commit.

## Inventory baseline (re-verified 2026-06-09)

| Metric | Count |
| --- | --- |
| `_test.go` files | 878 (231 tagged `//go:build integration`) |
| Top-level unit `func Test` (non-integration) | 5,386 (+1,641 `t.Run` subtests repo-wide) |
| Integration `func Test` | 243 |
| Ginkgo `It`/`Entry` specs | 842 |
| Playwright e2e cases | 76 across 9 spec files / 16 describe blocks |
| Invariant registry | 311 invariants: **42 bound / 269 pending** |
| Quarantine registry | 6 rows (`test/quarantine.yaml`), all citing open beads |

Pending invariants by scope: CRYPTO 108, SCENE 60, PLUGIN 37, EVENTBUS 28, TELEMETRY 8, STORE 8, ACCESS 8, SESSION 4, COMMAND 3, PRIVACY 2, CLUSTER 2, PRESENCE 1.

## 1. Per-package unit coverage baseline (acceptance #1)

126 packages appear in the profile; **74 are below the 80% per-package target**, but classification matters:

**Not meaningful (generated / mocks / test-support / one-shot tooling)** — ~30 packages including all `pkg/proto/holomush/*` (generated), all `*/mocks/` and `*mocks` packages, `*/coretest`, `*/worldtest`, `*/policytest`, `*/clustertest`, `test/testutil`, `cmd/internal/fsmdiagram`, `internal/plugin/gen-schema`, `internal/access/policy/dsl/gen-ebnf`, `cmd/lint-plugin-manifests`, `cmd/inv-migrate`, `cmd/holomush-cutover`. Recommendation: exclude these classes from the 80% ratchet rather than chase them.

**Integration-covered at another tier (unit number misleading)**:

| Package | Unit cover | Integration supplementation (verified) |
| --- | --- | --- |
| `internal/auth/postgres` | 1.1% | `internal/auth/postgres/player_repo_test.go` (integration-tagged) covers Create/Get/Update/Delete/locking happy paths |
| `internal/world/postgres` | 1.3% | location/object/binding/character/property repo integration tests + `cascade_delete_test.go` |
| `internal/access/policy/store` | 14.2% | `postgres_integration_test.go` (Ginkgo) covers CRUD + unique-name constraint |

**Genuine below-target production packages (unit tier):**

| Package | Unit cover | Notes |
| --- | --- | --- |
| `internal/bootstrap/setup` | 4.6% | subsystem wiring |
| `internal/plugin/setup` | 11.4% | subsystem wiring |
| `internal/access/setup` | 18.9% | subsystem wiring |
| `pkg/plugin/storage` | 20.0% | plugin SDK surface |
| `internal/admin/approval` | 23.0% | |
| `internal/eventbus/crypto/dek` | 27.0% | heavily integration-supplemented (33 test files, 14 integration-tagged) — see holomush-psr40 |
| `internal/admin/policy` | 28.2% | |
| `plugins/core-scenes` | 51.0% | largest plugin |
| `cmd/holomush` | 56.7% | |
| `internal/audit` | 60.2% / `internal/eventbus/audit` 63.2% / `audit/chain` 66.3% | |
| `internal/settings` | 65.8%, `internal/content` 67.2%, `internal/admin/readstream` 68.9% | |
| `internal/store` | 69.1%, `internal/session` 69.6%, `internal/property` 72.3% | |
| `internal/grpc` | 72.9%, `internal/eventbus/history` 72.9%, `internal/eventbus/crypto/kek` 73.4% | |
| `internal/plugin/cryptowiring` | 75.0%, `pkg/plugin` 75.3%, `internal/cluster` 78.0%, `internal/bootstrap` 79.1%, `internal/telnet` 79.5% | near-target |

## 2. COLLAPSE — prioritized (acceptance #2)

1. **HIGH — Transport-layer duplication, gRPC vs web auth handlers.** `internal/grpc/auth_handlers_test.go` (2,797 lines, 62 funcs) and `internal/web/auth_handlers_test.go` (1,189 lines, 23 funcs) re-assert the same business scenarios across both transports (e.g. `TestAuthenticatePlayer_Success` at `internal/grpc/auth_handlers_test.go:62-110` vs `TestWebAuthenticatePlayer_Success` at `internal/web/auth_handlers_test.go:67-91`; same for SelectCharacter and CreatePlayer pairs). The web tests thin-wrap gRPC responses. Est. ~600–1,000 LOC. → **holomush-65a4l**
2. **HIGH — `TestSelectCharacter_*` sibling family.** 7 sibling funcs spanning `internal/grpc/auth_handlers_test.go:199-451` differing only in setup state; one table-driven test with ~10 rows. (Folded into holomush-65a4l.)
3. **MED — Lua hostfunc harness repetition.** `internal/plugin/hostfunc/world_test.go` (1,080 lines): the arrange block (mock querier → `hostfunc.New` → `lua.NewState` → `Register` → `DoString`) repeats ~26×; the four query functions carry structurally identical error-path families (`world_test.go:122-138` / `312-328` / `485-501` / `719-737`). Est. ~260 LOC. → **holomush-17rcu**
4. **MED — Repeated fixture wiring in heavy files.** `policytest.NewGrantEngine()` + session/store + core-engine construction repeated ~60× in `internal/grpc/auth_handlers_test.go` and ~80× of analogous repo-mock wiring in `internal/world/service_test.go`; a `newAuthTestServer(t)`-style helper would remove ~150–200 LOC and lower the cost of every future auth test. (Tracked inside holomush-65a4l's consolidation.)
5. **LOW — `internal/plugin/manifest_test.go`** is already well table-driven; adjacent clusters (invalid-name / valid-names / missing-fields, e.g. `:127-201`, `:222-324`) could merge under a `wantValid` flag, but payoff is marginal.
6. **LOW/observed — over-mocking.** `internal/command/dispatcher_test.go` and parts of `internal/grpc/auth_handlers_test.go` carry `.EXPECT()` chains that assert mock invocation rather than outcomes (e.g. `dispatcher_test.go:1822-1851`); `internal/telnet/gateway_handler_test.go` (3,027 lines) hand-codes a `mockCoreClient` (`:81-230`) for paths that `test/integration/` exercises for real. Recommendation: prefer stubs over expectation-mocks at boundaries; no dedicated bead — apply opportunistically during the collapses above.

Total realistic collapse estimate: **~1,300–1,900 test LOC** with no coverage loss.

## 3. CUT — waste/bloat (acceptance #3)

1. **34 zero-assertion interface-satisfaction tests** (e.g. `TestBootstrapSubsystemImplementsSubsystem` in `internal/bootstrap/setup/subsystem_test.go`, `TestPostgresPlayerSessionStore_CompileTimeCheck` at `internal/store/player_session_store_test.go:50`, plus the `internal/*/setup`, `internal/settings`, `pkg/plugin`, `cmd/holomush` clusters). These are compile-time facts; replace with `var _ Iface = (*Impl)(nil)` declarations. → **holomush-o9in7**
2. **3 tests of protobuf-generated code**: `pkg/proto/holomush/plugin/v1/plugin_test.go:16,38,63` (`TestEventMessageFields`, `TestHandleEventRequestResponse`, `TestEmitEventFields`) assert generated struct fields — framework-not-our-code. → **holomush-o9in7**
3. **1 dead test utility (verified):** `MatchOopsCode` at `internal/testsupport/integrationtest/matchers.go:19` — zero references outside its own file. → **holomush-o9in7**. (A second "dead helper" claim, `coretest.NewMemoryEventStore`, was **rejected on verification** — see Methodology.)
4. **Integration→unit downgrades: low confidence, no bead.** The sweep's concrete candidates were either already-quarantined flaky tests or migration tests that legitimately need Postgres. No grounded list of safely-downgradable Docker-bound tests emerged; revisit only if `task test:int` wall-time becomes a pain point.
5. **Quarantine fix-or-delete:** all 6 rows verified — markers present, cited beads open, `scripts/quarantine-audit.sh` clean. Verdict: **keep all 6 quarantined**; none are delete candidates (5 are infra/timing flakes with active beads since 2026-05-25; the 6th — `scenes.spec.ts` pose draft, `holomush-5rh.8.27` — is blocked on the in-flight crypto wiring and dated 2026-06-08). Watch item: the five 2026-05-25 rows are approaching three weeks quarantined; their beads should show progress or be re-prioritized.

## 4. COVER — gaps (acceptance #4)

1. **Invariant binding backlog is the single largest verification gap: 269/311 pending.** A verified subset is *category (a): test exists, annotation missing* — e.g. INV-CRYPTO-1 is genuinely asserted by `TestWithHistoryAuthProducesSameColdOptsAsCryptoCold` (`internal/eventbus/history/tier_crypto_options_test.go:48`) and INV-CRYPTO-2..5 by the sibling tests at `:83,:119,:148,:164`, none annotated. A full sweep classifying (a)/(b)/(c) is the cheapest large coverage-confidence win. → **holomush-60spm**
2. **Postgres repositories have no negative-path tests.** `internal/auth/postgres`, `internal/world/postgres`, `internal/access/policy/store` cover happy-path CRUD at the integration tier but nothing for unique-constraint violations (duplicate username/email), FK violations, concurrent-update conflicts, or `ReplaceBySource` rollback. → **holomush-ltv9j**
3. **E2E is happy-path-only.** All 76 cases across the 9 specs (`web/e2e/`) cover success journeys; there are no wrong-password/lockout, ABAC-denial-in-UI, command-parse-error, or deleted-character journeys. → **holomush-av6s1**
4. **Crypto/scenes unit-tier depth.** `internal/eventbus/crypto/dek` (27.0% unit) and `plugins/core-scenes` (51.0%) need a combined-tier profile to find branches covered at *no* tier (the unit-only number alone is not proof of a gap — see Methodology §3). → **holomush-psr40**
5. **ABAC deny-path depth.** `internal/access/policy/engine.go` has deny-override tests (`TestEngineDenyOverridesForbidWins`, `engine_test.go:1151`), but condition-evaluation-error propagation and explicit default-deny-on-empty-policy-set assertions were not found; fold into holomush-60spm's scope-ACCESS pass (8 ACCESS invariants pending).

## 5. Pyramid shape and tier-appropriateness (acceptance #5)

Ratio ≈ **6,900 unit : ~950 integration : 76 e2e** (≈ 91 : 12.5 : 1). This is a healthy pyramid — wide unit base, ~7:1 unit:integration, ~12:1 integration:e2e. The problem is not shape but **edge quality**: the unit tier carries duplication (§2) and assertion-free filler (§3), while the e2e tier is breadth-limited to happy paths (§4.3).

Tier placement is otherwise well maintained: no Ginkgo outside sanctioned locations, no testcontainers in untagged files except **one**: `internal/store/session_store_presence_test.go` uses `sessiontest.NewStoreWithPool(t)` (`:31-33`) with no `//go:build integration` tag, and `internal/store/` is not in the CLAUDE.md sanctioned-exception list (`internal/grpc/`, `internal/grpc/focus/`, `internal/command/handlers/`, `internal/session/`). Either tag it or amend the sanctioned list — it is arguably in the spirit of the exception since `PostgresSessionStore` lives there. → **holomush-sx2no**

Consistency posture (APPROACH sweep): ACE naming violations are rare (31 funcs ≈ 0.55%, clustered in `cmd/holomush/migrate_test.go`, `internal/web/auth_handlers_test.go`, `internal/grpc/auth_handlers_test.go`); table-driven style appears in 169/878 files with the biggest holdouts being `plugins/core-scenes/service_test.go` (77 funcs) and `internal/plugin/goplugin/host_test.go` (85 funcs, 2 tables); testify is the clear standard with raw-`if` outliers in `internal/tls/certs_test.go` (16) and `internal/cluster/payload_test.go` (14) (`test/meta/invariant_registry_test.go`'s raw style is deliberate — meta-test); `// Verifies:` hygiene is clean — zero stray bare `INV-<n>` tokens found, and spot-checked annotations (`internal/grpc/list_focus_presence_test.go`, `internal/grpc/sceneaccess_service_test.go`, `internal/auth/guest_service_test.go`) genuinely assert their invariants. → **holomush-sx2no**

## 6. Follow-up beads (acceptance #6)

| Bead | P | Dimension | Summary |
| --- | --- | --- | --- |
| `holomush-65a4l` | P2 | COLLAPSE | Consolidate gRPC-vs-web auth handler test duplication + SelectCharacter table + shared auth fixture |
| `holomush-17rcu` | P3 | COLLAPSE | hostfunc Lua harness helper + parameterized error-path families |
| `holomush-o9in7` | P3 | CUT | Zero-assertion interface tests → `var _` checks; delete proto-generated-code tests; delete `MatchOopsCode` |
| `holomush-60spm` | P2 | COVER | Sweep 269 pending invariants; bind category-(a) entries (INV-CRYPTO-1..5 verified bindable) |
| `holomush-ltv9j` | P2 | COVER | Postgres repo negative-path integration tests (constraints, FK, rollback) |
| `holomush-av6s1` | P2 | COVER | 5–8 negative-journey e2e specs |
| `holomush-psr40` | P2 | COVER | Combined-tier coverage gap analysis: crypto/dek + core-scenes |
| `holomush-sx2no` | P3 | APPROACH | ACE renames, raw-assert outliers, untagged Docker-bound store test |

## References

- `.claude/rules/testing.md` — tier taxonomy, ACE naming, coverage targets, quarantine idioms
- `.claude/rules/invariants.md` — binding ratchet rules (no fabricated bindings)
- `docs/architecture/invariants.yaml` — 311-entry registry audited at `f350ac37`
- `test/quarantine.yaml` + `scripts/quarantine-audit.sh` — quarantine bijection (clean at audit time)
- `task test:cover` output — 9,156 tests / 3 skips / exit 0; per-package table archived in bead notes (holomush-8lqco)
- Prior audit precedent: `docs/repository-audit/2026-05-13/`
