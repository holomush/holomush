<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 27. Unified `AttributeProvider` Interface

> [Back to Decision Index](../README.md)

**Review finding:** The spec defined `AttributeProvider` twice with incompatible
signatures — `ResolveSubject`/`ResolveResource` in the Core Interfaces section
vs. a single `Resolve` with `LockTokens()` in the Lock section.

**Decision:** Unify into a single interface with `ResolveSubject`,
`ResolveResource`, and `LockTokens()`. Providers that contribute no lock
vocabulary return an empty slice from `LockTokens()`.

**Rationale:** The subject/resource distinction matters because providers may
resolve different attributes depending on whether the entity is the principal or
the target. A single `Resolve` method loses this context. Adding `LockTokens()`
to the same interface keeps the provider contract in one place.
