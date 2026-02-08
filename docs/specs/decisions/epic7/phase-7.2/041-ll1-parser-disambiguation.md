<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 41. LL(1) Parser Disambiguation for Condition Grammar

> [Back to Decision Index](../README.md)

**Review finding (C1):** The condition grammar has an ambiguity when the
parser sees an identifier after an expression — it could be the start of a
`has` check or a binary operator. Without a disambiguation rule, the parser
would need unbounded lookahead.

**Decision:** Use one-token lookahead: after parsing a primary expression, if
the next token is `has`, parse a `has`-expression; if the next token is a
comparison or logical operator, parse a binary expression; otherwise, return
the primary expression.

**Rationale:** LL(1) lookahead is sufficient because `has` is a keyword that
cannot appear as an attribute name or operator. This keeps the parser simple
(no backtracking, no GLR) while handling the full grammar.

**Implementation note:** Phase 7.2 recommends participle (PEG-based parser)
which uses ordered-choice semantics rather than explicit lookahead. The LL(1)
specification describes the _logical grammar design_ (what ambiguities exist,
how to resolve them). Participle's PEG ordered-choice achieves the same
disambiguation effect — when multiple alternatives match, the first one in
source order is selected. Implementers MUST verify disambiguation behavior with
test cases covering ambiguous inputs (e.g., `principal.faction` alone vs
`principal.faction == "red"`) regardless of parser implementation choice.
