<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 26. Per-Request Attribute Caching

> [Back to Decision Index](../README.md)

**Review finding:** Eager resolution without caching means repeated `Evaluate()`
calls within a single user action re-resolve the same attributes. At 200 users
with 5 auth checks per command, this creates unnecessary load.

**Decision:** Implement per-request attribute caching from the start using a
shared `AttributeCache` attached to the request context.

**Rationale:** The cache is scoped to a single request (no stale data risk) and
provides significant savings when a single user action triggers multiple
authorization checks. The implementation cost is low (a map with a mutex) and
avoids a predictable performance problem that would require retrofitting later.
