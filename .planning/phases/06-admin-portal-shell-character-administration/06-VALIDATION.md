---
phase: 6
slug: admin-portal-shell-character-administration
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: validated
nyquist_compliant: false
wave_0_complete: true
created: 2026-08-13
validated: 2026-08-16
---

# Phase 6 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
>
> **Seeded by `/gsd-plan-phase 6`.** `status: draft` and the empty Per-Task
> Verification Map below are the seeded state, not coverage. `/gsd-validate-phase 6`
> fills the map and flips `status: validated`. A `produces:` hook is satisfied by this
> filename existing, so never read "the nyquist gate passed" as "coverage exists" —
> check `status:` and re-derive the row count.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` + testify (unit) · Ginkgo/Gomega (integration, `//go:build integration`) · Vitest + svelte-check (web unit) · Playwright (E2E) |
| **Config file** | `Taskfile.yaml` (targets verified: `test`:185, `test:int`:265, `test:e2e`:310, `web:test`:710) |
| **Quick run command** | `task test -- ./internal/admin/... ./internal/grpc/...` |
| **Full suite command** | `task pr-prep` (fast lane: schema/license/lint/fmt/unit/build/bats + `web:test`) |
| **Estimated runtime** | ~{TBD — measure at Wave 0} seconds |

> Tier selection is governed by `.claude/rules/testing.md`. `task test` does **NOT**
> compile `//go:build integration` files — any refactor of shared types MUST also run
> `task test:int`. Docker is required for `test:int` and `test:e2e`.

---

## Sampling Rate

- **After every task commit:** Run `task test -- ./<touched-package>/`
- **After every plan wave:** Run `task test` (+ `task test:int` when the wave touched shared types)
- **Before `/gsd-verify-work`:** Full suite must be green (`task pr-prep`)
- **Max feedback latency:** {TBD — measure at Wave 0} seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| T1, T2 | 06-01 | 1 | ADMIN-01 | — | Non-admin denied **at the wire** with `PermissionDenied` and a static message; admin positive control passes | integration | `task test:int -- ./test/integration/access/` | ✅ `admin_section_gate_test.go:60,103` | ✅ green |
| T1 | 06-01 | 1 | ADMIN-01 | — | Typed `DENY_ADMIN_SECTION` in-process; the server factory **refuses to build an ungated server** | unit | `task test -- -run 'TestTheInterceptorRefusesANonAdminWithTheTypedDenyCode\|TestTheServerFactoryRefusesToBuildAnUngatedServer' ./internal/grpc/` | ✅ `admin_interceptor_test.go:120`; `server_interceptor_test.go:24` | ✅ green |
| T1, T2 | 06-01 | 1 | ADMIN-02 | — | An undeclared method is refused **before subject resolution** (`ADMIN_SECTION_NOT_DECLARED`) | unit | `task test -- ./internal/grpc/ ./internal/admin/section/` | ✅ `admin_interceptor_test.go:191`; `descriptor_completeness_test.go:65` | ✅ green |
| T1–T3 | 06-04 | 3 | ADMIN-03 (server half) | — | List / search / detail: ordering, paging, LIKE-escaping, 12-field detail bound | unit + integration | `task test -- ./internal/grpc/` ; `task test:int -- ./internal/world/postgres/ ./test/integration/access/` | ✅ `admin_characters_read_test.go:109,143,434`; `character_repo_admin_integration_test.go:95,317,442`; `test/integration/access/admin_characters_read_test.go:56,239` | ✅ green (UI half is Phase 06.1 — the requirement straddles both) |
| T2, T3 | 06-05 | 4 | ADMIN-04 | — | 13-path exact-string mask, **both directions**; role-bearing field fence over the generated descriptors | unit | `task test -- ./internal/grpc/ ./test/meta/` | ✅ `admin_characters_write_test.go:64,258,280`; `test/meta/admin_character_message_role_fence_test.go:93` | ✅ green |
| T2, T3 | 06-05 | 4 | ADMIN-05 | — | Retire / unretire route through the canonical lifecycle command; idempotent; **no delete RPC exists** | unit + integration | `task test -- ./internal/grpc/` ; `task test:int -- ./test/integration/access/` | ✅ `admin_characters_write_test.go:531,553,566`; `test/integration/access/admin_characters_write_test.go:123,168,201` | ✅ green |
| T3 | 06-05 | 4 | ADMIN-06 | — | Envelope written in-transaction with the acting player; rollback leaves neither envelope nor audit row; the audit projection is the sole `events_audit` writer | integration + unit (meta) | `task test:int -- ./test/integration/access/` ; `task test -- ./test/meta/` | ⚠️ `admin_characters_write_test.go:123,415`; `test/meta/world_sql_fence_test.go:664,762,788,810` | ⚠️ **PARTIAL — #4971** (projection clause unreachable; see Audit) |
| T2, T3 | 06-02 | 2 | ADMIN-08 | — | `roles` forwarded verbatim and **never authoritative** — differential test: real roles say admin, ABAC still denies | integration + unit | `task test:int -- ./test/integration/access/` ; `task test -- ./internal/grpc/ ./internal/web/` | ✅ `admin_section_gate_test.go:284`; `auth_handlers_test.go:1504`; `internal/web/auth_handlers_test.go:833` | ✅ green |
| T1, T2 | 06-01, 06-02 | 1–2 | EXT-01 | — | Seven entries, one available / six planned; the admission probe id is immaterial | unit | `task test -- ./internal/admin/section/ ./internal/grpc/` | ✅ `gate_test.go:427,482`; `registry_test.go:44,56,249`; `admin_sections_test.go:125,181` | ✅ green |
| T1, T3 | 06-02 | 2 | EXT-02 | — | All seven reachable; `NOT_IMPLEMENTED` returned **only after the gate** (6/1 counts asserted) | integration + unit | `task test:int -- ./test/integration/access/` ; `task test -- ./internal/grpc/` | ✅ `admin_section_gate_test.go:180`; `admin_sections_test.go:96,212,232` | ✅ green |
| T2 | 06-01 | 1 | EXT-03 | — | Admin-prefixed RPCs fenced to admin packages, off the character facade | unit (meta) | `task test -- ./test/meta/` | ✅ `admin_rpc_placement_test.go:70`; `characteraccess_routing_census_test.go:905` | ✅ green |
| T1–T2 | 06-01, 06-02 | 1–2 | EXT-04 | — | Descriptor set-equal to the served set **both directions**; malformed / 0-or-2-shape entries abort boot; denying default arm | unit | `task test -- ./internal/admin/section/ ./internal/grpc/` | ✅ `descriptor_test.go:74,169,218,250`; `admin_interceptor_test.go:402,442` | ✅ green |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

None owed. Every guard this phase needed already existed, and the one extension
it made was owned in-plan rather than as Wave 0: `WithGatedGRPCListener` on
`internal/testsupport/integrationtest` — the harness's first real network
transport, built through the **production** server factory so the gate under test
is the shipped one.

*Existing infrastructure is expected to cover most of this phase: `internal/testsupport/integrationtest`
(full-stack tier), `test/meta/` (set-equality / census meta-tests), `web/e2e/` (Playwright),
and `task web:test` (Vitest + svelte-check, gated in `pr-prep` and the CI `Build` job).*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions | Outcome |
|----------|-------------|------------|-------------------|---------|
| Migration `000057` up/down/up round-trip on a scratch Postgres 17 container | ADMIN-03 | Down-migration reversibility is asserted by no in-tree test | Run the round-trip on a throwaway container; record exits, index counts, and `max(version_id)` | ✅ **Discharged** — table of exits / index counts / `max(version_id)` recorded in `06-04-SUMMARY.md` §"Migration round-trip on a scratch Postgres 17 container" |
| ~30 planted-mutation RED demonstrations (ordering clauses, allowlist directions, audit fence, empty-mask skip, roles precondition) | ADMIN-03/04/05/06, EXT-02/04 | The *falsifiability* of an assertion cannot itself be asserted in-tree — a test that would pass under the bug is invisible to the suite | Plant the mutation, observe RED, revert, verify the tree is clean | ✅ **Discharged** — `06-02-SUMMARY.md`, `06-04-SUMMARY.md`, and `06-05-SUMMARY.md` each carry a "Demonstrations Performed and Recorded" table (planted mutation / assertion / observed), each closing with "reverted, working tree verified clean" |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (none owed)
- [x] No watch-mode flags
- [x] Feedback latency < 90s (scoped unit) / < 1200s (integration tier)
- [ ] `nyquist_compliant: true` set in frontmatter — **withheld**, see Audit

**Approval:** validated 2026-08-16 as **PARTIAL** by `/gsd-validate-phase 6` —
11 of 12 requirement rows automated and green; ADMIN-06's projection clause is
unreachable by construction (#4971).

---

## Validation Audit 2026-08-16

| Metric | Count |
|--------|-------|
| Gaps found | 1 |
| Resolved | 0 |
| Escalated | 1 |

This map was **built from scratch** — it shipped carrying an explicit
`SEEDED EMPTY by plan-phase` marker whose own comment warned that an empty map
"is NOT evidence of coverage." Rebuilt against the 4 executed plans at
`e124617b4`. Unit rows green via `task test` PASS (11852 ok / 4 skipped);
integration rows via CI `success` at `07ca74c46`.

This phase is **server-side only** — all four plans are `subsystem: api`, and no
SUMMARY references `web/e2e`, `.spec.ts`, Vitest, or `web:test`. The admin *web*
surface is Phase 06.1. Note also that there is no plan `06-03`; the plans are
01, 02, 04, 05 across waves 1–4.

**Test-name fidelity was total here: 22 of 22 SUMMARY-cited test names resolved
to a real `func` declaration.** No predicted-but-nonexistent names, in contrast
to Phases 02.2 and 04.

**Escalated — ADMIN-06's projection clause cannot fire (#4971).** The checkbox
reads `[x]` and the tests pass, but both `events_audit` assertions assert **zero
rows**, and they are correct to: the world outbox relay publishes through a bare
`JetStreamPublisher`, only `RenderingPublisher` writes the `App-Rendering`
header, and `audit.writeAuditRow` requires that header. So no admin mutation ever
produces an `events_audit` row. The **transactional** half of the requirement is
real and proven end to end; the **projection** half is unreachable in the current
wiring.

The phase handled this correctly rather than papering over it: `INV-WORLD-9` was
deliberately worded to claim only the transactional and single-writer properties,
and the two tests that would have asserted the false property were **not
written**. That is the same discipline the invariant registry demands — refusing
a binding beats fabricating one. It is now tracked at **#4971** (OPEN), filed
precisely because it had been recorded only as prose in `06-05-SUMMARY.md:194-204`
with no issue and no `WINDOWS.md` row.

**The fence is proven non-vacuous in both directions.**
`test/meta/world_sql_fence_test.go:762,788,810` are the fence's own falsifiability
tests — it fires on a production INSERT, ignores reads/comments/DLQ, and does not
fire on a `_test.go` file.

**Two caught-in-flight false greens worth preserving.** `06-04-SUMMARY.md`
deviation 3 records that the `normalized_name ASC` tiebreak demonstration
**passed under the bug** until the fixture was reseeded so insertion order and
name order disagree. Deviation 6 records an `assert.NotNil` on an empty proto map
— an always-true assertion — replaced with a discriminating end-to-end read.
Both are the "would this fail under the bug?" test applied successfully during
execution.

**Related open findings on this surface, already tracked:** #4972 (admin search
binds one charname-normalized parameter to two differently-normalized columns, so
non-ASCII usernames silently return empty — no test covers it) and #4990 (the
`d.SectionFromRequest` branch at `admin_interceptor.go:215-231` has no production
caller; its refusal semantics are exercised only by `admin_interceptor_test.go:402`,
`:442`, `admin_sections_test.go:212`, `:232`, and wire-level
`admin_section_gate_test.go:180`).

**Stale artifact note.** `deferred-items.md` §1 describes `test/integration/charname`
as RED from migration `000057`; `06-VERIFICATION.md` confirms it was fixed in
`313be9e22` and the lane is 24/24 green.
