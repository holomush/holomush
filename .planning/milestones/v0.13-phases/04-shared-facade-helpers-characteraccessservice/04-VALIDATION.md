---
phase: 4
slug: shared-facade-helpers-characteraccessservice
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: validated
nyquist_compliant: false
wave_0_complete: true
created: 2026-08-10
validated: 2026-08-16
---

# Phase 4 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` + `testify` (unit); Ginkgo/Gomega (full-stack integration, `//go:build integration`) |
| **Config file** | none (Go native) — harness at `internal/testsupport/integrationtest/`; ABAC engine builder at `internal/testsupport/abactest/abactest.go:68` |
| **Quick run command** | `task test -- ./internal/grpc/ ./internal/access/... ./internal/web/` |
| **Full suite command** | `task test` then `task test:int` (scoped: `task test:int -- ./test/integration/access/...`) |
| **Estimated runtime** | ~90 seconds quick; ~10-15 min full (`task test:int` needs Docker) |

---

## Sampling Rate

- **After every task commit:** Run `task test -- ./<touched-package>/` + `task lint`
- **After every plan wave:** Run `task test` (full unit) + `task test -- -run 'Census' ./test/meta/`
- **Before `/gsd-verify-work`:** Full suite must be green — `task pr-prep` green, then `task pr-prep:full` (integration + E2E) before push; `/holomush-dev:review-abac` READY
- **Max feedback latency:** 90 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 04-02-T1/T2 → 04-08-T1/T2 | 04-02, 04-08 | 1, 6 | IDENT-02 | — | Guest gate + ownership each defined once; every owner-audience RPC routes through them (set equality + fail-closed partition) | unit (meta) | `task test -- -run 'CharacterAccessRoutingCensus' ./test/meta/` | ✅ `characteraccess_routing_census_test.go:496,530,626,681,708,780` | ✅ green |
| 04-02-T1 | 04-02 | 1 | IDENT-02 | — | Guest denied with a per-facade message; unparseable ULID and non-owned character are **wire-identical** NotFound; repo failure → Internal | unit | `task test -- -run 'PlayerGate' ./internal/grpc/` | ✅ `internal/grpc/player_gate_test.go:39,65,101,124,151,188,232,278` | ✅ green |
| 04-04-T1/T2 | 04-04 | 2 | PROFILE-03 | T-4-EXIST-DISCLOSE | Withheld field absent from the **marshaled bytes** (sentinel scan + paired positive control); unreachable ≡ nonexistent | unit + integration | `task test -- -run 'GetCharacterProfile' ./internal/grpc/` ; `task test:int -- ./test/integration/access/...` | ✅ `characteraccess_profile_test.go:559,403`; `test/integration/access/character_profile_read_test.go:362` (binds INV-PRIVACY-9 at `:360`), `:452` | ✅ green |
| 04-01-T1, 04-07-T1/T2 | 04-01, 04-07 | 1, 5 | PROFILE-04 | T-4-TIER-BYPASS | Per-attribute tier floor governs every field; unknown tier denies **with no principal at all**; directory gate independent of reachability | unit | `task test -- -run 'GetCharacterProfile\|ViewerIdentity\|ListCharacterDirectory' ./internal/grpc/` | ✅ `characteraccess_profile_test.go:350,610,647,693`; `characteraccess_viewer_test.go:225`; `characteraccess_directory_test.go:266,310` | ✅ green |
| 04-06-T1/T2/T3, 04-09-T1/T2 | 04-06, 04-09 | 4, 2 | PROFILE-10, IDENT-02a | T-4-MASK-PASSTHRU | Owner edit via typed RPC; over-cap rejected server-side in **bytes, pre-store**; reaches `world.Service.UpdateCharacterDescription`; closed mask allowlist; CAS | unit + integration | `task test -- -run 'UpdateCharacter' ./internal/grpc/ ./internal/world/` ; `task test:int -- ./test/integration/access/...` | ✅ `characteraccess_write_test.go` (14 sites, `:286`–`:808`); `internal/world/service_test.go:434`; `service_profile_test.go:34,170,281`; `mutator_profile_test.go:199`; integration `character_write_test.go:201`–`:388` (W1–W13) | ✅ green |
| 04-01-T2 | 04-01 | 1 | PROFILE-05 | — | Profile built exclusively from the viewer-filtered slice; a direct property-repo call **does not compile** | compile-time + unit | `task build` ; `task test -- -run 'GetCharacterProfileCarriesTheStoredValue\|ReportsAVisibleRowMissing' ./internal/grpc/` | ✅ fence at `characteraccess_service.go:37,63,114,165` (narrow interfaces); `characteraccess_profile_test.go:247,286,445` | ✅ green (RED demonstrated manually — see Manual-Only) |
| 04-01-T1, 04-04-T2 | 04-01, 04-04 | 1, 2 | EXT-06 | — | Media proto present but ships **no entries**; gallery ordering ascending by slot | unit + integration | `task test:int -- ./test/integration/access/...` | ✅ `characteraccess_profile_test.go:380,504`; `character_profile_read_test.go:437`; `test/integration/access/media_schema_test.go:240,288` | ✅ green |
| 04-05-T1/T2/T3 | 04-05 | 3 | PROFILE-03 | T-4-ALT-LINKAGE | Off-location viewer reads description; a viewer grant **never widens to the player behind the character** | unit + integration | `task test -- -run 'GetMyCharacter\|OwnerAudience' ./internal/grpc/` ; `task test:int -- ./test/integration/access/...` | ✅ `characteraccess_owner_test.go:267,422,478,504`; `test/integration/access/viewer_alt_linkage_test.go:126` (binds INV-ACCESS-15 at `:125`), `:152` control, `:162` | ✅ green |
| 04-01-T1 | 04-01 | 1 | PROFILE-03 (structural half) | T-4-ALT-LINKAGE | The projection message carries **no** `player_id` / `location_id` field at all | — | none | ⚠️ true by schema construction (`PublicCharacter` = id, name, description, profile, primary_image, gallery) — **no descriptor-level guard**; nothing turns RED if a proto edit adds the field | ⚠️ **PARTIAL — #4994** |
| 04-03-T1/T2 | 04-03 | 1 | IDENT-02 (spec amendment) | — | 01-SPEC §9.3 carries no `RenameCharacter` row | docs-only (meta) | `task test -- -run 'CharacterReturningRPCCensus' ./test/meta/` | ✅ `test/meta/character_rpc_census_test.go:309,350,376,404` | ✅ green |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

> Task IDs are assigned by `/gsd-plan-phase` when PLAN.md files are written; `/gsd-validate-phase` reconciles this map against the executed plans.

---

## Wave 0 Requirements

- [x] Generalize `serviceReceiverName` → a facade-aware receiver predicate (new helper in `test/meta/`). **Landed** — `meta_helpers_test.go:88` `facadeReceiverName(...)`; it generalizes rather than replaces `serviceReceiverName` (`world_envelope_census_test.go:145`).
- [x] A symmetric-difference diff helper for census failures (01-SPEC §2.6 `:222-224`) — `test/meta/meta_helpers_test.go` is the existing shared-helper home. **Landed** — `setSymmetricDifference` at `:162`, plus `bodyReferencesIdent` at `:124`.
- [x] Decide and implement the **web-half routing predicate** — without it, criterion 1's proxy half is vacuous (research Open Question 3). **Landed and NOT vacuous** — `characteraccess_routing_census_test.go:601`. The universe is scoped by the facade-client selector `h.characterAccess` (`:484`) rather than a name prefix, membership needs a **two-conjunct** predicate (`bodyReferencesIdent(body, "headerInjectSessionToken")` AND `bodyNamesMethod(body, paired)`, `:611`), compared by set equality against a checked-in 6-member literal (`:285`), with a `require.NotEmpty` universe guard at `:488`.
- [x] A `(*integrationtest.Server).NewCharacterAccessServer` harness constructor, mirroring `harness.go:1173`. **Landed** — `harness.go:1378`.
- [x] Framework install: **none** — all frameworks present

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions | Outcome |
|----------|-------------|------------|-------------------|---------|
| Criterion 3's config-side clause — a game configuration cannot raise `name`/`pronouns` above the profile's own reachability floor | PROFILE-04 | 01-SPEC §8.8 (`:1855-1867`) records that **v0.13 ships no mechanism** enforcing this against a deliberately violating configuration; `INV-PRIVACY-10` "is phrased as a system guarantee while in fact resting on operator discipline" | Operator review of the shipped tier configuration. **Do NOT bind `INV-PRIVACY-10` to a test that proves only the facade half** — `TestBoundInvariantsAreGenuinelyAsserted` cannot detect a partial binding (`.claude/rules/invariants.md`). | ✅ **Correctly discharged by REFUSING to bind.** `docs/architecture/invariants.yaml:2185-2192` carries `binding: pending` with **no** `asserted_by`, and no `// Verifies: INV-PRIVACY-10` exists anywhere in the tree. Rationale in `04-05-SUMMARY.md` D9 and `04-04-SUMMARY.md` D12. This is the registry rule working as intended — a partial binding here would have been a false green the meta-test cannot catch. |
| Compile-fence RED for criterion 5 | PROFILE-05 | A compile error cannot be asserted by a passing test; the fence's RED must be *demonstrated and recorded*, not asserted | Temporarily add `s.propertyRepo.ListByParent(...)` to the facade, run `task build`, record the compile error verbatim in the plan's RED evidence, then revert. | ✅ **Discharged.** `04-01-SUMMARY.md` §"Criterion 5: the compile fence" (from `:181`) records the diagnostic verbatim; `:164` confirms the temporary call was reverted before commit. |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 90s
- [ ] `nyquist_compliant: true` set in frontmatter — **withheld**, see Audit below

**Approval:** validated 2026-08-16 as **PARTIAL** — 9 of 10 rows automated and
green; the PROFILE-03 structural half has no regression guard (#4994).

---

## Validation Audit 2026-08-16

| Metric | Count |
|--------|-------|
| Gaps found | 2 |
| Resolved | 1 |
| Escalated | 1 |

This map was **rebuilt, not reconciled** — every Task ID previously read `TBD`
and the commands were generic placeholders. Rebuilt against the 9 executed plans
at `e124617b4`. Unit rows green via `task test` PASS (11852 ok / 4 skipped);
integration rows via CI `success` at `07ca74c46`.

**Resolved — the seed's own command was vacuous.** The PROFILE-03 row invoked
`task test -- -run 'Profile.*Absent' ./internal/grpc/`, which matches **zero**
tests and therefore **exits 0 green forever**. Established with a positive
control: `^func Test.*Profile.*Absent` returns 0 against `^func Test.*Profile`
returning 38 in the same package. The real assertion is
`TestGetCharacterProfileWithholdsABelowFloorFieldFromTheMarshaledBytes`
(`characteraccess_profile_test.go:559`); the command is re-keyed above. Every
command in the rebuilt map was chosen against a symbol confirmed by reading its
`func` declaration.

**Escalated — #4994.** PROFILE-03's behavioural half is well covered, but its
**structural** half (the projection carries no `player_id`/`location_id`) holds
only by schema construction. Nothing turns RED if a future proto edit adds the
field, so the sole remaining defense is code review — on a privacy requirement
whose failure mode is silent. An in-tree guard shape already exists
(`internal/plugin/hostcap/hostv1_no_seq_test.go:43` walks
`ProtoReflect().Descriptor().Fields()` asserting a forbidden field is absent);
closing this is one test function, no new tooling.

**Note on census drift, not a defect.** The checked-in census literals have grown
past the plan's stated counts (guest gate 6 including `CreateCharacter` /
`SetDefaultCharacter`, ownership 4, web proxies 6, versus the plan's 4/3/4).
That is the census doing its job — later RPCs were forced into it. The plan text
is stale; the guards are not.

**Accepted by design.** The web half deliberately carries no audience partition
(only the facade half does, `:626`), so a net-new *ungated* web proxy that never
touches `h.characterAccess` falls outside the census universe. The facade is the
enforcement point, so this is a scoped universe rather than a hole.
