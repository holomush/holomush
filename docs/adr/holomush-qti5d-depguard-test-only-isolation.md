<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Enforce Test-Only Import Isolation via depguard, Not Build Tags

**Date:** 2026-05-25
**Status:** Accepted
**Decision:** holomush-qti5d
**Deciders:** HoloMUSH Contributors
**Related:** holomush-1eps2, holomush-bmtd, holomush-tcf7

## Context

The in-memory event store (`core.NewMemoryEventStore`) carried a
`//go:build !integration` tag to keep it out of production builds. It was the
*only* non-test file with that tag, and its side effect forced `task test:int`
to enumerate an explicit package list — `./...` fails to compile under
`-tags=integration` whenever a compiled file references the excluded symbol
(holomush-bmtd). The real safety property is "production packages must not
import test-only constructs" — a package-import constraint, not a
compilation-lane constraint. A precursor (holomush-tcf7) had already removed a
false claim in `testing.md` that a build tag "enforced" this.

## Decision

Test-only construct isolation is enforced by a **depguard** deny rule in
`.golangci.yaml`, not a build tag. `MemoryEventStore` is extracted into a new
test-only package `internal/core/coretest` so the rule can operate at package
granularity; the `//go:build !integration` tag on `store_memory.go` is removed,
unblocking `task test:int -- ./...` and deleting the explicit package list.

## Alternatives Considered

### A — depguard package-import rule (chosen)

Enforces the real invariant uniformly across every build lane, scoped to
production files via `files` patterns with a test-support allowlist. Eliminates
the fragile package list as a side effect. Requires extracting the store to its
own package (depguard cannot deny a symbol within a package production needs)
and enabling depguard (disabled today).

### B — Build-tag scheme (`busint` / `!e2e`)

Cannot separate bus-integration from full-stack integration without breaking
compilation, because both legitimately import `eventbustest`. Adds a tag
dimension requiring a full-tree migration, and still does not actually prevent
production imports — it only excludes files from build lanes.

### C — Pure documentation / status quo

Zero code, but leaves production import of test-only constructs silently
possible and the bmtd drift class (new integration tests silently not run)
unfixed.

## Rationale

- The safety property is a package boundary rule; depguard is the mechanism that
  matches it.
- Removing the sole non-test `!integration` file lets `./...` compile under
  `-tags=integration`, killing the silent-omission drift class.
- A meta-test (`TestDepguardTestOnlyConstructRulesPresent`) guards the config
  against silent deletion — the exact failure mode that produced the original
  false invariant.

## Consequences

**Positive:** INV-1/INV-2 (no production import of `eventbustest` /
`core/coretest`) are machine-enforced at `task lint`; `test:int -- ./...` works
without enumeration; the depguard allowlist self-documents the legitimate
harness packages.

**Negative:** requires extracting the store and updating 9 test-file imports;
adds depguard as an active linter contributors must learn.

**Neutral:** the `coretest` package name + doc-comment make the prohibition
visible without reading `.golangci.yaml`.

## Implementation

See `docs/superpowers/plans/2026-05-25-test-tier-taxonomy.md` Tasks 1, 2, 5, 6.

## References

- Spec: `docs/superpowers/specs/2026-05-25-test-tier-taxonomy-design.md` §5
- holomush-1eps2 (design), holomush-bmtd (absorbed), holomush-tcf7 (precursor)
