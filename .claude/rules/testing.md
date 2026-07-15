<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

---
paths:
  - "**/*_test.go"
  - "test/**/*.go"
  - "**/testutil/**/*.go"
  - "**/policytest/**/*.go"
  - "**/eventbustest/**/*.go"
  - "**/natstest/**/*.go"
  - "**/mocks/**/*.go"
---

# Go Testing Rules

This file auto-loads when editing test files. The high-level testing posture is in `CLAUDE.md`; the operational detail lives here.

## Coverage

codecov measures coverage at two granularities — **patch** (the lines your PR
changed) and **project** (the whole repo). It does **not** measure per-package,
so there is no per-package coverage floor to satisfy.

| Requirement | Description |
| ----------- | ----------- |
| **SHOULD** cover changed lines to 80% | codecov's `patch` status targets 80% coverage on changed lines (`threshold: 5%`). It POSTS a status on the PR but is **not** a required protect-main check today — it does not block merges. It becomes a merge gate only when added to the protect-main ruleset (`11923801`). |
| **MUST NOT** lower whole-repo coverage | codecov's `project` status is a rising-floor ratchet (`target: auto, threshold: 1%`) — a PR MUST NOT drop project coverage more than 1 percentage point below the base commit. Legacy code is not retroactively blocked. Also POSTED, not currently required. |
| **MUST** run `task test:cover` | Verify coverage locally before completing work. |

The required protect-main checks are [Build, Lint, Test, CodeRabbit, Integration
Test, E2E Test] plus the OPS-03 `Vuln` check (once the operator adds it). Classic
branch protection is off, so `gh api repos/holomush/holomush/branches/main/protection`
returning 404 is expected — the ruleset (`11923801`), not classic protection, is
authoritative.

## Integration Tests and Refactoring

`task test` does NOT compile `//go:build integration` files. When refactoring shared types, interfaces, or packages, always run `task test:int` to catch breakage that unit tests miss.

## Test Files

- Tests live next to implementation: `foo.go` → `foo_test.go`
- Integration tests in `*_integration_test.go`
- Use build tags for integration tests: `//go:build integration`

## Test Naming (ACE)

Test names MUST be sentences that communicate behavior. Follow the ACE framework: **Action** (what), **Condition** (when/given), **Expectation** (then/result).

Reference: [Test Names Should Be Sentences](https://bitfieldconsulting.com/posts/test-names)

| Requirement | Description |
| ----------- | ----------- |
| **MUST** follow ACE | Every test name communicates action, condition, and expectation |
| **MUST** use PascalCase | Top-level function names: `TestConfigDirFallsBackToHomeDotConfig` |
| **SHOULD NOT** use underscores | Exception: `TestType_Method` with subtests (e.g., `TestEngine_Evaluate`) |
| **MUST** use lowercase subtests | Subtest strings: `"returns ErrNotFound for missing character"` |
| **MUST NOT** use vague names | No `"success"`, `"error case"`, `"test 1"` |

## Test Quality

| Requirement | Description |
| ----------- | ----------- |
| **MUST** test both paths | Every exported function needs at least one positive and one negative test |
| **MUST** assert behavior | No zero-assertion "don't panic" tests |
| **MUST** focus each test | One behavior per test/subtest — if it needs "and," split it |
| **SHOULD** use error codes | Prefer `errutil.AssertErrorCode` or `assert.ErrorIs` over string matching |
| **MUST** use `require` for preconditions | `require.NoError` for setup, `assert.*` for the check under test |

## Invariant Bindings (`// Verifies:`)

When a test genuinely asserts a named system invariant from the registry
(`docs/architecture/invariants.yaml`), annotate it so the registry can bind it:

```go
// Verifies: INV-CRYPTO-28
func TestDecryptPluginRowFailClosedWithoutAuditEmitter(t *testing.T) { ... }
```

| Requirement | Rule |
| ----------- | ---- |
| **MUST** annotate invariant-asserting tests | A test that proves an `INV-<SCOPE>-N` carries `// Verifies: INV-<SCOPE>-N` immediately above the test (or the assertion block). This is what flips the registry entry from `binding: pending` to `bound`. |
| **MUST NOT** annotate a test that does not prove it | A `// Verifies:` on a test that only touches the code is a false-green (the INV-RB-3 bug, `holomush-0sh1k`). No genuine assertion ⇒ leave it pending and file a coverage bug. |
| **SHOULD** read the rule | Full workflow (define / respect / bind / regenerate) is in `.claude/rules/invariants.md`. |

## Test Engine Helpers (ABAC)

Test engines: policytest.GrantEngine / AllowAll / DenyAll / ErrorEngine — examples in the deep reference.

## Test Tiers

| Tier | Dependencies | Runner | Build tag |
| --- | --- | --- | --- |
| unit | none | `task test` | (none) |
| bus-integration | embedded NATS (`eventbustest`) | `task test:int` | `//go:build integration` |
| audit-integration | embedded NATS + Postgres testcontainer | `task test:int` | `//go:build integration` |
| full-stack integration | embedded NATS + Postgres + `CoreServer` (+ optional in-tree plugins via `WithInTreePlugins()`) | `task test:int` | `//go:build integration` |
| external-NATS integration | a **real** single-node NATS JetStream container + per-replica `*nats.Conn` (`internal/testsupport/natstest`), + Postgres/`CoreServer` as needed | `task test:int` | `//go:build integration` |
| **E2E** | full Docker stack driven through a real client (browser) | `task test:e2e` | (Playwright) |

"E2E" means the Playwright browser suite — a test that crosses the real user
boundary. The Ginkgo `test:int` suite is **integration** (it calls Go/gRPC APIs
in-process), regardless of how much of the stack it stands up. Use "E2E" only
for the Playwright suite; Go Ginkgo suites are "integration".

**Embedded NATS (`eventbustest`) is the correct harness at every tier EXCEPT
external-mode-specific behavior** — external dial / fail-closed boot, single-
principal account scoping, multi-node per-replica crypto invalidation, and DLQ
against a real broker. That behavior MUST be verified against a **real** NATS
container via `internal/testsupport/natstest`, because it needs N HoloMUSH
replicas each with an INDEPENDENT connection to one external broker — a shape
the shared in-process `eventbustest` connection cannot express. Embedded stays
correct for everything else.

Production code MUST NOT import `eventbustest`, `internal/core/coretest`, or
`internal/testsupport/natstest` — enforced by the depguard rule in `.golangci.yaml`.

### Quarantine

Known-flaky integration and E2E specs are quarantined so they self-skip in gating CI and in `task pr-prep:full`, running only nightly or locally with `HOLOMUSH_RUN_QUARANTINED=1`. Quarantine is for **flakiness with an open GitHub issue** — never for a real failure.

Quarantine is for flakiness with an open GitHub issue and no reproducible cause; if the root cause is known, fix it — do NOT quarantine it.

**Three marker idioms:**

| Stack | Marker |
| --- | --- |
| Go unit/integration | `quarantinetest.Skip(t, "holomush-xxxx")` (helper at `internal/testsupport/quarantinetest`) |
| Ginkgo | `if !quarantinetest.Enabled() { Skip("quarantined: holomush-xxxx") }` |
| Playwright | `{ tag: ['@quarantine', '@holomush-xxxx'] }` |

Every marker MUST have a corresponding row in `test/quarantine.yaml` with an `issue:` field citing an open GitHub issue; the marker token (`holomush-xxxx` for legacy rows, `holomush-i<issue#>` for new ones) is the bijection key (meta-test INV-2 at `test/meta/quarantine_registry_test.go`). `task quarantine:audit` flags rows whose cited issue is closed.

Production code MUST NOT import `quarantinetest` — enforced by depguard.

See [site/src/content/docs/contributing/how-to/quarantine.md](../../site/src/content/docs/contributing/how-to/quarantine.md) for the full contributor guide.

## Integration Tests (Ginkgo/Gomega BDD)

| Requirement | Description |
| ----------- | ----------- |
| **MUST** use Ginkgo/Gomega | All integration tests use BDD-style specs |
| **MUST** write feature specs | User stories become `Describe`/`It` blocks |
| **MUST** use `//go:build integration` | Tag all integration test files |
| **SHOULD** use testcontainers | For database integration tests |

**Run integration tests:**

```bash
task test:int
```

Deep reference: .claude/rules/references/testing-detail.md (read on demand).
