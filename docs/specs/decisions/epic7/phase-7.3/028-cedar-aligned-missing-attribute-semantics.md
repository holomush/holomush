<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 28. Cedar-Aligned Missing Attribute Semantics

> [Back to Decision Index](../README.md)

**Review finding:** The type system defined `!=` across types as returning
`true`, creating a security hazard. `principal.faction != "enemy"` would return
`true` when `faction` is missing, accidentally granting access to characters
without the attribute. The suggested workaround (`principal.reputation.score
!= 0`) was also broken — it would return `true` when the reputation plugin
was not loaded.

**Decision:** Align with Cedar: ALL comparisons involving a missing attribute
evaluate to `false`, regardless of operator (including `!=`). This matches
Cedar's behavior where a missing attribute produces an error value that causes
the entire condition to be unsatisfied.

**Example:** `principal.faction != "enemy"` with missing `faction` attribute:

1. Comparison evaluates to `false` (missing attribute → all comparisons false)
2. The `when` condition is unsatisfied
3. The `permit` policy does NOT match
4. No other policy matches → default deny
5. Outcome: Access denied (safe, conservative)

This is the desired behavior — missing attributes always fail-closed.

**Rationale:** The original `!=` semantics created a class of policies that
silently granted unintended access. Cedar's behavior is proven safe: missing
attributes are always conservative (deny). The `has` operator provides an
explicit existence check for cases where presence matters. For negation,
the defensive pattern is `principal has faction && principal.faction !=
"enemy"`.

**Updates [Decision #8](../phase-7.2/008-dsl-expression-language-scope.md):** Changes the type
system table from Decision #8's implied semantics.
