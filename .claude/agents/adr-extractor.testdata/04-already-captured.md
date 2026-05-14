# Fixture: Spec referencing an already-captured decision

## Background

This proposal extends the existing ABAC engine described in ADR 0009
(custom Go-native ABAC). We're adding eager attribute resolution per
ADR 0012.

## Decision

Use eager attribute resolution with per-request caching, as already
captured in ADR 0012.

Expected agent output: zero new candidates; dropped list cites the
existing ADRs.
