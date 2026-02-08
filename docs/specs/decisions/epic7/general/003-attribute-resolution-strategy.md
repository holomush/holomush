<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 3. Attribute Resolution Strategy

> [Back to Decision Index](../README.md)

**Question:** When should attributes be resolved — all up front, or on demand?

**Options considered:**

| Option | Description                   | Pros                                                      | Cons                                         |
| ------ | ----------------------------- | --------------------------------------------------------- | -------------------------------------------- |
| A      | Eager (collect-then-evaluate) | Simple, predictable, complete audit snapshot per decision | May fetch unused attributes                  |
| B      | Lazy (resolve-on-reference)   | More efficient, only fetches what policies need           | Harder to audit, ordering/caching complexity |

**Decision:** **Option A — Eager resolution.**

**Rationale:** At ~200 users and modest policy counts, the performance difference
is negligible. Eager resolution provides a complete attribute snapshot for every
decision, which powers both audit logging and the `policy test` debugging
command. The implementation is simpler and the mental model is clearer: every
check starts with "here's everything we know about the subject and resource."
