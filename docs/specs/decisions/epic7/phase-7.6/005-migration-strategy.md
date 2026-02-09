<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 5. Migration Strategy

> [Back to Decision Index](../README.md)

**Question:** How do we migrate ~28 production call sites from the old
`AccessControl` interface to the new `AccessPolicyEngine`?

**Options considered:**

| Option | Description                                 | Pros                                                  | Cons                                                                             |
| ------ | ------------------------------------------- | ----------------------------------------------------- | -------------------------------------------------------------------------------- |
| A      | Big-bang interface change                   | Clean, one-time effort                                | Large blast radius, all ~28 production callers need error handling added at once |
| B      | New interface + adapter for backward compat | Incremental migration, preserves fail-closed behavior | Two interfaces exist temporarily                                                 |

**Decision:** ~~**Option B — New `AccessPolicyEngine` interface with adapter.**~~

**Superseded by [Decision #36](036-direct-replacement-no-adapter.md).** With no
production releases, the adapter adds unnecessary complexity. All call sites
switch to `AccessPolicyEngine.Evaluate()` directly.

**Naming:** The new interface is called `AccessPolicyEngine` (per Sean's
preference).
