# Fixture: Clean current-format spec

## Decision

Use bd-decision IDs as ADR filename identifiers.

## Alternatives Considered

- **Sequential NNNN with auto-renumber:** simple but parallel branches
  collide on the next integer. — rejected
- **Reserve numbers via placeholder PRs:** complex; adds round-trip. — rejected
- **bd-decision ID as filename:** atomically allocated; no collision. — selected

## Rationale

Atomic allocation by bd eliminates the parallel-branch collision class
that the NNNN convention has.

Expected agent output: 1 candidate, worthiness_score=4.
