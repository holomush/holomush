<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 98. Import Cycle Check for PropertyProvider (T4a)

> [Back to Decision Index](../README.md)

**Date:** 2026-02-10
**Status:** Accepted
**Phase:** 7.1
**Task:** T4a (EntityProperty type and PropertyRepository)

## Context

Task 4a (PropertyRepository) creates the `PropertyRepository` interface and PostgreSQL implementation in `internal/world/`. Task 16b (PropertyProvider) will define a `PropertyProvider` interface in `internal/access/` that calls `PropertyRepository.ListByParent()` to fetch entity properties for ABAC evaluation.

This creates a dependency chain:

```text
internal/access/ → internal/world/ → internal/store/
```

However, if `internal/world/` ever needs to import from `internal/access/` (for example, to evaluate ABAC policies during world operations), a circular dependency would result:

```text
internal/access/ → internal/world/ ⟲ internal/access/
```

This is a common architectural risk when adding a new access control layer to an existing domain model.

## Problem

The codebase must prevent import cycles that would cause compilation failures. While the current design (Task 4a through Task 7) introduces no cycles, PropertyProvider and related components in Task 16b and later phases could introduce cycles if not carefully designed.

**Specific risk:** PropertyProvider accepts a PropertyRepository interface, which lives in `internal/world/`. If PropertyRepository ever needs to import types or interfaces from `internal/access/`, a cycle would be introduced.

## Decision

Add an explicit acceptance criterion to Task 4a:

> `go vet` confirms no import cycles between `internal/access/` and `PropertyProvider` (or any provider packages that import from `internal/world/` or `internal/store/`)

This criterion is checked at code review time and must pass before the task is marked complete. The criterion applies not only to Task 4a itself, but also gates Tasks 16b and later (PropertyProvider), since PropertyProvider is the component that bridges `internal/access/` and `internal/world/`.

## Rationale

1. **Proactive Detection:** Go's `go vet ./...` includes cycle detection as part of the standard toolchain. Running this check enforces architectural boundaries.

2. **Early Failure:** By making the criterion explicit in Task 4a (the task that creates PropertyRepository), we catch any design decision that would create a cycle before later tasks compound the problem.

3. **Documentation:** Explicit acceptance criteria document the architectural constraint for future contributors. A developer cannot claim Task 4a or Task 16b is complete if `go vet` reports cycles.

4. **No Performance Cost:** `go vet` is part of the standard test suite (`task test`) and adds negligible overhead.

5. **Matches Existing Practice:** The codebase already uses `task lint` (which invokes `golangci-lint`, which includes cycle checking) in CI. This criterion makes that implicit constraint explicit in the implementation plan.

## Alternatives Considered

### Alternative 1: No explicit criterion, rely on CI

- Con: Developers might not realize cycles are a problem until code review
- Con: Less clear to Task 4a implementer what the architectural constraint is
- Rejected: Explicit criteria improve communication and reduce rework

### Alternative 2: Add a separate architectural review checklist

- Con: Orthogonal to the task itself; harder to enforce
- Rejected: Acceptance criteria are the right place for hard constraints

### Alternative 3: Use a code generation tool to prevent cycles

- Con: Over-engineering for a constraint already enforced by Go's toolchain
- Rejected: `go vet` is sufficient

## Implementation

When implementing Task 4a, after creating the PropertyRepository interface:

1. Create the PropertyRepository interface and implementation
2. Run `go vet ./...` to confirm no cycles
3. At code review time, re-run `go vet ./...` as part of acceptance criteria verification
4. For Task 16b (PropertyProvider), repeat this check after PropertyProvider is added

## Related Decisions

- **ADR 018** ([018-property-package-ownership.md](018-property-package-ownership.md)): Establishes that PropertyRepository belongs in `internal/world/`, not `internal/access/`
- **ADR 003** ([../general/003-attribute-resolution-strategy.md](../general/003-attribute-resolution-strategy.md)): Documents the overall attribute resolution design, which PropertyProvider is part of
- **ADR 027** ([../phase-7.3/027-unified-attribute-provider-interface.md](../phase-7.3/027-unified-attribute-provider-interface.md)): Defines the unified AttributeProvider interface that PropertyProvider will implement

## Acceptance Criteria

- [ ] T4a acceptance criteria in `docs/plans/2026-02-06-full-abac-phase-7.1.md` updated to include: "`go vet` confirms no import cycles between `internal/access/` and `PropertyProvider` (or any provider packages that import from `internal/world/` or `internal/store/`)"
- [ ] This ADR created and documented
- [ ] README index updated with ADR 98 entry
