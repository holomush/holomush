<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 8. DSL Expression Language Scope

> [Back to Decision Index](../README.md)

**Initial proposal:** Stripped-down operators (comparisons, AND/OR/NOT, `in`
lists). Excluded hierarchy traversal, `has`, set operations, and if-then-else.

**Sean's feedback:** Include the full set — hierarchy, `has`, set operations, and
if-then-else.

**Decision:** **Full Cedar-compatible expression language.**

**Rationale:** The healer-wound-visibility scenario demonstrated why the full
language is needed. Without `has`, every property would need every possible
attribute defined. Without `containsAny`/`in`, you'd need a separate policy per
healer character. The full expression language pays for itself in real MUSH
scenarios. Operators: `==`, `!=`, `>`, `>=`, `<`, `<=`, `in` (list and
hierarchy), `has`, `containsAll`, `containsAny`, `if-then-else`, `like`, `&&`,
`||`, `!`.
