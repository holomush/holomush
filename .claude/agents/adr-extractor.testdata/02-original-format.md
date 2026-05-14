# Fixture: Original-format spec

## Context

Session tokens stored client-side carry forgery risk.

## Decision

Use opaque session tokens validated server-side, not signed JWTs.

## Rationale

JWT validation logic adds complexity; revocation is hard. Opaque tokens
trade a lookup per request for trivial revocation.

## Alternatives Considered

JWTs were considered and rejected for the revocation issue.

Expected agent output: 1 candidate, worthiness_score=4.
