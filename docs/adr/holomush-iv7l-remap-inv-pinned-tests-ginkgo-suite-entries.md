<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Remap INV-pinned Test* functions to Ginkgo suite entries on migration

**Date:** 2026-05-16
**Status:** Accepted
**Decision:** holomush-iv7l
**Deciders:** Sean Brandt

## Context

`internal/eventbus/history/phase7_boundary_meta_test.go` is a drift detector for the Phase 7 invariant table. For each `INV-P7-N` invariant, it asserts that a named top-level `Test*` function exists somewhere in the `*_test.go` corpus, via a `go/parser` AST walk that enumerates top-level `func TestXxx(t *testing.T)` declarations.

Ginkgo `Describe` and `It` blocks register specs via `var _ = Describe(...)` package-init expressions. Those expressions are invisible to the top-level-function AST walk: the meta-test sees a Ginkgo spec file as having zero `Test*` functions of the named form. When a Ginkgo conversion removes an INV-pinned `Test*` func and replaces it with a `Describe`/`It` registration, the drift detector silently loses coverage of that invariant — the meta-test fails with "named test X NOT FOUND", and the underlying invariant assertion goes from "pinned to a discoverable test" to "pinned to nothing the detector can see."

This was discovered during Phase A of the testify+ginkgo migration. Pull requests PR-3951 (INV-P7-3, INV-P7-13) and PR-4015 (INV-P7-6, INV-P7-9, INV-P7-10, INV-P7-12) each remove INV-pinned `Test*` funcs as part of a `Describe`/`It` conversion.

## Decision

Every Ginkgo migration PR that removes an INV-pinned `Test*` function MUST:

1. **Remap the meta-test mapping** in `internal/eventbus/history/phase7_boundary_meta_test.go` from the removed `TestXxx` name to the Ginkgo suite entry func name (e.g., `TestBinaryPlugin`, `TestEventbusE2E`, `TestStore`).
2. **Preserve the INV reference inside the spec's `Describe` name string**, e.g., `Describe("Plugin role cannot write host tables (INV-P7-13)", ...)`, so the INV-to-spec linkage stays grep-discoverable from code.
3. **Add a brief doc comment** above the remapped meta-test entry naming the original `Test*` function (now removed), the spec file, and the spec `Describe` string. This documents the trail for future readers.

## Rationale

- **Zero meta-test infrastructure changes.** Updating the `inv → testName` map to point at a suite entry preserves the drift detector's coverage semantics with no AST-walker rewrite.
- **Suite entry funcs are real `Test*` functions.** Every Ginkgo bootstrap has a `func TestX(t *testing.T) { RunSpecs(...) }` entry — this satisfies the AST walk by construction.
- **INV reference greppability.** Embedding the INV string in the spec's `Describe` name keeps `rg "INV-P7-13"` working as a way to locate the spec from the invariant, just as it located the named test func before.
- **Lightweight per-PR protocol.** No coordination is required across PRs; each conversion bundles its own remap. The worked-example PRs cited in References demonstrate the protocol is small enough to fit alongside the conversion itself.

## Alternatives Considered

### Keep individual `Test*` stub functions alongside Ginkgo specs (REJECTED)

**Strengths:** Meta-test AST walk continues to find the named function without modification.

**Weaknesses:** Defeats the purpose of migration — the named func becomes a dead stub or a thin wrapper that runs nothing. Adds confusion: contributors must learn that the "real" test lives in a `Describe` block elsewhere. Pure maintenance overhead with no semantic value.

### Update the meta-test AST walker to understand Ginkgo `Describe`/`It` naming (REJECTED)

**Strengths:** Removes the per-PR protocol; meta-test becomes Ginkgo-aware once and forever.

**Weaknesses:** Non-trivial AST rewrite. Ginkgo's `Describe`/`It` names are string literals passed as function arguments, not function declarations — the walker would need to interpret call expressions in package-init positions and parse `var _ = Describe(...)` chains, including nested `Context`/`When`/`Describe` blocks. Adds fragility: the meta-test would silently lose coverage if a spec used a non-literal name expression (variable or concatenation). Meta-test maintenance burden grows.

### Remap INV mapping to suite entry; INV reference greppable in `Describe` name (ACCEPTED)

**Strengths:** Zero meta-test code change; INV reference remains grep-discoverable; suite entry func IS a real `Test*` func visible to AST walk; minimal protocol overhead per migration PR.

**Weaknesses:** Mapping becomes one-to-many when multiple INVs share a suite (e.g., 4 INVs all map to `TestEventbusE2E` after PR-4015). Contributors MUST know the remap protocol or the drift detector silently loses coverage on future conversions. No automated enforcement.

## Consequences

### Positive

- Drift detector (`phase7_boundary_meta_test.go`) continues to enforce INV coverage after Ginkgo migration without infrastructure changes.
- INV references remain grep-discoverable in spec `Describe` strings.
- Pattern composes: a single suite can carry many INVs (PR-4015 maps four INVs to `TestEventbusE2E`).

### Negative

- Protocol must be followed explicitly on every Ginkgo migration PR that touches an INV-pinned file. No automated enforcement of the remap step — the meta-test fails loudly if missed, but only after the conversion PR is pushed.
- INV-to-suite-entry mapping becomes implicit documentation in the meta-test rather than a 1:1 function name.

### Neutral

- PR-3951 and PR-4015 serve as canonical worked examples (see References).

## References

- Drift detector: `internal/eventbus/history/phase7_boundary_meta_test.go`
- Worked examples:
  - <https://github.com/holomush/holomush/pull/3951> (INV-P7-3 → `TestBinaryPlugin`; INV-P7-13 → `TestBinaryPlugin`)
  - <https://github.com/holomush/holomush/pull/4015> (INV-P7-6, INV-P7-9, INV-P7-10, INV-P7-12 → `TestEventbusE2E`)
- Superseded design doc: `docs/superpowers/specs/2026-05-15-testify-ginkgo-migration-completion-design.md`
- Closed epic: holomush-rccc
