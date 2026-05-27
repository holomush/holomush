<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Test Suite Review Design

**Date:** 2026-04-04
**Status:** Draft
**Scope:** All unit and integration test files (~210 files, ~400+ test functions)

## Problem Statement

The HoloMUSH test suite has grown organically to ~210 test files across 37
packages. While overall quality is strong (table-driven tests, testify
assertions, proper helpers), the suite has accumulated inconsistencies:

- **Naming:** Mix of `TestType_Method_Condition`, bare `TestFoo`, and
  sentence-style subtests. No single convention enforced.
- **Coverage gaps:** Some packages lack negative/error-path tests. Several
  "don't panic" tests call functions with no assertions.
- **Duplication:** 14 attribute provider test files follow near-identical
  patterns. Some unit tests duplicate validation logic also tested at the
  integration layer.
- **Focus:** Some tests check multiple unrelated behaviors in a single
  function.

This spec defines the conventions, quality bar, deduplication strategy, and
execution plan for a comprehensive review.

## Naming Convention

### Reference

[Test Names Should Be Sentences](https://bitfieldconsulting.com/posts/test-names)
by John Arundel (Bitfield Consulting).

### ACE Framework

Every test name MUST communicate three elements:

1. **Action** -- what operation is being performed
2. **Condition** -- under what circumstances
3. **Expectation** -- what result SHOULD happen

### Rules

#### Top-Level Function Names

When the function has **no subtests**, the function name itself MUST be a
sentence following ACE:

```go
// Before
func TestConfigDir_EnvVar(t *testing.T) { ... }
func TestEnsureDir_Error(t *testing.T) { ... }

// After
func TestConfigDirUsesXDGEnvVarWhenSet(t *testing.T) { ... }
func TestEnsureDirFailsWhenParentIsAFile(t *testing.T) { ... }
```

When the function **uses subtests** (table-driven or explicit `t.Run`), the
parent name is the noun/verb being tested. The subtest names carry the ACE
sentence:

```go
// Parent name: what is being tested
func TestHashPassword(t *testing.T) {
    t.Run("produces valid argon2id hash", func(t *testing.T) { ... })
    t.Run("rejects empty password", func(t *testing.T) { ... })
    t.Run("produces unique hash for each call via salt", func(t *testing.T) { ... })
}
```

#### Subtest Names

Subtest name strings (in `t.Run` or table struct `name` fields) MUST read as
plain-English sentences when extracted. They SHOULD use lowercase except for
proper nouns and acronyms:

```go
// Good
"returns ErrNotFound for non-existent character"
"rejects empty password"
"produces unique hash for each call via salt"

// Bad
"success"
"error case"
"test 1"
```

#### Capitalization and Formatting

- Top-level function names use Go's required `Test` prefix followed by
  PascalCase: `TestConfigDirFallsBackToHomeDotConfig`.
- Top-level function names SHOULD NOT contain underscores. Underscores in Go
  test names become `/` separators in subtest paths, which creates confusing
  output. The one permitted exception: `TestType_Method` is acceptable when
  the function uses subtests and the parent name identifies the receiver type
  and method being tested (e.g., `TestEngine_Evaluate`). In this case the
  underscore acts as a visual separator between type and method, and the
  subtests carry the sentence-style names.
- Subtest name strings use natural English casing (lowercase start).

#### Validation Tool

[`gotestdox`](https://github.com/bitfield/gotestdox) converts camelCase test
names into readable sentences. It SHOULD be used locally during development and
MUST be added as a CI check after all phases complete (Phase 9).

```bash
go test -v ./internal/xdg/ 2>&1 | gotestdox
# Output:
# ConfigDir
#  - uses XDG env var when set
#  - falls back to home dot config when env unset
```

## Quality Standards

### Positive and Negative Coverage

- Every exported function in production code MUST have at least one happy-path
  test and one error/edge-case test.
- Table-driven tests MUST include both valid and invalid cases in the same
  table, not split across separate functions.

### Assertion Quality

- Use `require.NoError` / `require.NotNil` for preconditions (fail-fast).
  Use `assert.*` for the behavior under test.
- Error tests SHOULD assert on error codes (`errutil.AssertErrorCode`) or
  sentinel errors (`assert.ErrorIs`), not on error message strings.
- Exception: string matching is acceptable when the error originates from an
  external library with no structured error type.

### Test Focus

- Each test (or subtest) MUST test exactly one behavior.
- If a test name needs "and" in it, it SHOULD be split into two tests.
- A test with zero assertions (e.g., `_, _ = ConfigDir()` "don't panic" tests)
  MUST either gain meaningful assertions or be removed.

### Test Helpers

- Helpers MUST call `t.Helper()` so failure messages point to the caller.
- Helpers MUST register cleanup via `t.Cleanup()`, not defer in the test body.
- Shared test helpers that serve a package's tests live in the same package.
  Cross-package test helpers live in a `*test` support package
  (e.g., `policytest`).

## Deduplication Strategy

### Category 1: Attribute Provider Structural Duplication

The 14 files in `internal/access/policy/attribute/` each test the same
`AttributeProvider` interface shape with near-identical structure.

**Remedy:** Create a shared **provider contract test** helper that takes any
`AttributeProvider` and runs common structural assertions:

- Namespace is non-empty
- Schema returns non-nil definitions
- Non-matching resource IDs return nil
- Non-matching subject IDs return nil

Each provider's test file then only contains provider-specific cases (e.g.,
`command` provider parses `command:say` into `{type: "command", name: "say"}`).

### Category 2: Unit vs Integration Overlap

Service-layer unit tests (mocked repos) and postgres repo integration tests
sometimes cover identical validation logic.

**Remedy:** Validation tests belong where the validation code lives:

- If the service layer validates, the service test owns it.
- The postgres repo test MUST only cover database-level concerns (constraint
  violations, query correctness, serialization).
- During audit, identify overlapping cases and remove the test that is further
  from the validation source.

### Category 3: Bootstrap Test Overlap

`admin_bootstrap_test.go` and `admin_test.go` cover similar seed scenarios.

**Remedy:** Consolidate into whichever file owns the function being tested.

## Execution Plan

### Phase Overview

| Phase | Packages | Est. Files | PR Scope |
|-------|----------|-----------|----------|
| 1 | CLAUDE.md updates + xdg, tls, naming, observability, idgen, logging, telemetry | ~7 | Single PR -- establishes conventions |
| 2 | core, store, config, control | ~17 | Single PR |
| 3 | auth, auth/postgres | ~16 | Single PR |
| 4 | command, command/handlers | ~19 | Single PR |
| 5 | plugin, plugin/hostfunc, plugin/lua, plugin/goplugin | ~32 | 1--2 PRs |
| 6 | world, world/postgres | ~25 | Single PR |
| 7 | access, access/policy/* (all sub-packages) | ~47 | 2--3 PRs |
| 8 | Cross-cutting: unit/integration dedup, shared contract tests | Varies | Single PR |
| 9 | CI: add `gotestdox` check | Config only | Single PR |

### Per-File Checklist

For each test file in each phase:

1. Rename test functions to sentence style (ACE for standalone, noun/verb
   parent with sentence subtests for table-driven).
2. Check positive/negative balance -- add missing cases.
3. Remove duplicates or weak tests (zero-assertion "don't panic" tests).
4. Verify focus -- split multi-behavior tests if needed.

### Verification

Per-package during development:

- `task test -- ./the/package/...` -- unit tests for the package under review.
- `task test:int -- ./test/integration/the-domain/...` -- if the package has
  matching integration tests.
- `task test:e2e` -- for phases touching grpc, web, telnet, or cmd packages.

Before opening each PR:

- `task pr-prep` MUST pass with zero failures. This mirrors all CI jobs (lint,
  format, schema, license, unit, integration, E2E).

### Commit Strategy

Each phase produces two types of commits:

1. **Rename-only commits** -- mechanical function renames, no behavior changes.
   Easy to review (rubber-stamp).
2. **Quality change commits** -- test additions, removals, restructuring.
   Require actual review scrutiny.

These MUST be separate commits within the same PR to simplify review.

### CLAUDE.md Updates (Phase 1)

The project CLAUDE.md MUST be updated in Phase 1 to establish the naming rules
before any test changes land. This ensures all future test code (including
later phases of this effort) follows the convention from the start.

Updates to CLAUDE.md Testing section:

- Test naming rules (ACE framework, sentence-style subtests)
- Quality standards (positive/negative balance, assertion quality)
- "Don't panic" test prohibition
- Reference link to the Bitfield Consulting article

### gotestdox CI Check (Phase 9)

Added last because it would fail on every intermediate PR while the rename is
in progress. Once all phases complete, the CI check enforces the standard going
forward.

## Out of Scope

- **Integration test structure** (Ginkgo/Gomega BDD tests under
  `test/integration/`) -- these use a different framework and naming convention.
  They MAY be reviewed in a follow-up effort.
- **Generated code** (`*.pb.go`, mockery-generated mocks) -- not hand-written,
  not reviewed.
- **Benchmark and fuzz tests** -- different purpose, different naming needs.
  Left as-is.

## Success Criteria

- All unit test function names read as sentences when processed by `gotestdox`.
- Every exported function has at least one positive and one negative test.
- No zero-assertion tests remain.
- Attribute provider tests use a shared contract test helper.
- No unit tests duplicate validation logic that belongs to a different layer.
- `task pr-prep` passes at every phase boundary.
- CLAUDE.md documents the naming convention for all future test code.
