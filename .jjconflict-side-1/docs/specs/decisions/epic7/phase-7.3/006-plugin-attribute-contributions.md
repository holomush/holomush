<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 6. Plugin Attribute Contributions

> [Back to Decision Index](../README.md)

**Question:** How do plugins contribute attributes to the evaluation context?

**Options considered:**

| Option | Description                      | Pros                                                            | Cons                                  |
| ------ | -------------------------------- | --------------------------------------------------------------- | ------------------------------------- |
| A      | Registration-based providers     | Simple interface, synchronous, consistent with eager resolution | Every check calls every provider      |
| B      | Attribute hooks via event system | Lower latency per check, async                                  | Attributes can be stale, more complex |

**Decision:** **Option A — Registration-based providers.**

**Rationale:** Consistent with the eager resolution choice. Plugins implement a
simple `AttributeProvider` interface with `Namespace()` to prevent collisions.
At ~200 users the synchronous resolution cost is trivial. Caching can be added
later if profiling shows it's needed.
