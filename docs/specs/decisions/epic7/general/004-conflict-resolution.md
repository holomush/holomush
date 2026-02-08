<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 4. Conflict Resolution

> [Back to Decision Index](../README.md)

**Question:** How should conflicting policies be resolved? Should policies have
priority ordering?

**Roadmap predefined:** Deny-overrides (any deny wins, then any allow, then
default deny).

**Additional question:** Should policies have numeric priority for override
scenarios?

**Options considered:**

| Option | Description                     | Pros                                  | Cons                                       |
| ------ | ------------------------------- | ------------------------------------- | ------------------------------------------ |
| A      | Deny-overrides without priority | Simple mental model, Cedar-proven     | Can't say "this allow overrides that deny" |
| B      | Priority-based ordering         | More flexible, supports VIP overrides | "Why was I denied?" debugging nightmares   |

**Decision:** **Deny-overrides without priority, with system subject bypass.**

**Rationale:** Cedar chose no priority and it works well. If an admin needs an
override, they write a more specific `allow` that avoids triggering the `deny`
condition, rather than using priority escalation. The `system` subject bypass
(already existing) handles the "ultimate override" case. Keeps the mental model
simple: deny always wins, period.
