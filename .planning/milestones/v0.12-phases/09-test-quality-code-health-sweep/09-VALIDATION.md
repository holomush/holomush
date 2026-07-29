---
phase: 9
slug: test-quality-code-health-sweep
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-25
---

# Phase 9 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `09-RESEARCH.md` §Validation Architecture (:908-958).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + `testify` (unit); **Ginkgo/Gomega** (integration, `//go:build integration`); Playwright (E2E) |
| **Config file** | `Taskfile.yaml:150` (`test:cover`), `:165` (`test:int`); `.golangci.yaml`; `.codecov.yml` |
| **Quick run command** | `task test -- ./<package>/` |
| **Full suite command** | `task test:cover` then `task test:int`; final gate `task pr-prep` |
| **Estimated runtime** | ~80s unit (10,366 tests) · ~141s integration (10,786 tests, needs Docker) — both measured this session |

---

## Sampling Rate

- **After every task commit:** Run `task test -- ./<changed-package>/` + `task lint`
- **After every plan wave:** Run `task test:cover`; additionally `task test:int` for any wave touching shared types or the `integrationtest` harness
- **Before `/gsd-verify-work`:** `task pr-prep` green **inline in the parent session** (not delegated — schema-regeneration side-effects a subagent cannot surface)
- **Max feedback latency:** ~80 seconds (unit); ~141 seconds (integration)

> **Read the exit code, never the log tail.** go-task collapses failures to 201; the authoritative verdict is in the `▸ pr-prep result:` file. Never branch on a matched output string (`.claude/rules/search-tools.md`).

---

## Per-Task Verification Map

Task IDs are assigned at planning time; this map is keyed by requirement until then.

| Requirement | Behavior | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|-------------|----------|------------|-----------------|-----------|-------------------|-------------|--------|
| QUAL-02 | `cmd/holomush` ≥80% | — | N/A | coverage | `task test:cover` + codecov `project` status on the PR | ✅ | ⬜ pending |
| QUAL-02 | `internal/tls` ≥80% | — | N/A | coverage | `task test:cover` | ✅ | ⬜ pending |
| QUAL-02 | `codecov/patch` + `codecov/project` are **required** checks | — | N/A | ratchet (operator) | `gh api repos/holomush/holomush/rulesets/11923801 --jq '[.rules[]\|select(.type=="required_status_checks")\|.parameters.required_status_checks[].context]'` must contain both | ✅ | ⬜ pending |
| QUAL-02 | `threshold: 0%` landed as final plan | — | N/A | config | `rg -n 'threshold: 0%' .codecov.yml` | ✅ | ⬜ pending |
| QUAL-03 | ACE predicate returns zero hits | — | N/A | ratchet | `task test -- -run TestACENamingRegistry ./test/meta/` | ❌ W0 | ⬜ pending |
| QUAL-03 | Four skip files trimmed; each cites an **open** issue | — | N/A | ratchet | quarantine-style walker check | ❌ W0 | ⬜ pending |
| QUAL-04 | Suite runs ≥15 specs | — | N/A | integration | `task test:int -- ./test/integration/session/...` | ⚠ bootstrap only | ⬜ pending |
| QUAL-04 | Every matrix cell maps to a real spec or a cited pointer | — | N/A | ratchet (bijection) | `task test -- -run TestSessionMatrixRegistry ./test/meta/` | ❌ W0 | ⬜ pending |
| QUAL-04 | `TestPrivacy_ReattachWithinTTLPreservesFloor` passes | — | history floor preserved across reattach | integration | `task test:int -- ./test/integration/session/...` | ❌ W0 | ⬜ pending |
| QUAL-04 | `TestPrivacy_TTLExpiryEndsSessionFreshFloor` passes | — | expired session gets a fresh floor (no backfill leak) | integration | same | ❌ W0 | ⬜ pending |
| QUAL-04 | #4682 I-PRIV-6 floor arm passes | I-PRIV-6 | floor preserved; no pre-join history leak | integration | `task test:int -- ./test/integration/privacy/...` | ❌ W0 | ⬜ pending |
| QUAL-04 | D-15 timestamped emit exists | — | N/A | unit + integration | `rg -n 'EmitDirectEventAt\|WithEmitAt' internal/testsupport/integrationtest/` + `task test:int` | ❌ W0 | ⬜ pending |
| QUAL-05 | #4793 — zero `attrs[...] = ""` in `attribute/` | ASVS V4 | absent key → fail-safe deny (never `"" == ""` fail-open) | unit + ratchet | `task test -- ./internal/access/policy/attribute/` + key-**absence** assertions per `.claude/rules/abac-providers.md` | ⚠ partial | ⬜ pending |
| QUAL-05 | #4794 — secure defaults ON | ASVS V3/V14 | `Secure` cookie + HSTS + CSP on by default | unit | `task test -- ./cmd/holomush/ ./internal/web/` — must **invert** `cmd/holomush/gateway_test.go:543-545` which pins `false` | ✅ (must invert) | ⬜ pending |
| QUAL-05 | #4796 — index exists and migration reverses | — | N/A | integration | `task test:int` (migrations on fresh DB) + up/down round-trip | ✅ | ⬜ pending |
| QUAL-05 | #4797 — drop path logs/metrics | ASVS V7 | audit record loss is observable, never silent | unit | `task test -- ./internal/eventbus/history/` with a nil-emitter case asserting the log/metric | ⚠ partial | ⬜ pending |
| QUAL-05 | #4792 deferral recorded | — | N/A | manual-only | `gh issue view 4792 -R holomush/holomush --comments` shows the deferral rationale | n/a | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `test/meta/ace_naming_registry_test.go` — the productionized ACE predicate (QUAL-03). **Prerequisite for D-08/D-10.** Model: `test/meta/quarantine_registry_test.go:35,76`.
- [ ] `test/meta/session_matrix_registry_test.go` — D-13's matrix↔spec bijection (QUAL-04).
- [ ] The committed matrix table artifact the D-13 meta-test reads.
- [ ] D-15 timestamped emit in `internal/testsupport/integrationtest/session.go` — **blocks** the two `holomush-dqd1` specs and #4682.
- [ ] Four GitHub issues for `holomush-{ecbg,6nds,nko7,l4kx}` — **blocks** D-11's trim. Verified: no existing issue matches any of the four.
- [ ] Three GitHub issues for D-21's deferred `ec22.9` residue (argon2 dummy-hash entropy, `http.Server` write timeout, `addlicense` pin).
- [ ] Absence-assertion helper for #4793 (assert key **not present**, not `== ""`).

*No framework install needed — Ginkgo, testify, and gotestsum are all present and green.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `codecov/patch` + `codecov/project` become required checks | QUAL-02 (D-04) | GitHub repo-settings change on ruleset `11923801` — an operator action no PR can make or revert | Operator edits the ruleset; verify with the `gh api` command in the map above. **Reversibility: costly** (same operator round-trip to undo). |
| #4792 deferral rationale | QUAL-05 (D-17) | A comment on an external issue, not a code artifact | `gh issue comment 4792 -R holomush/holomush` with the D-17 rationale; verify with `gh issue view 4792 --comments` |
| #4794 release note | QUAL-05 (D-18) | Behaviour change for operators running plain HTTP; prose review | Release note states the secure-cookie/HSTS/CSP default inverted and names the opt-out flag |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 150s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
