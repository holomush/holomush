<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 29. DSL `like` Pattern Validation at Parser Layer

> [Back to Decision Index](../README.md)

**Review finding:** The spec referenced `glob.Compile(pattern, ':',
glob.Simple)` but `gobwas/glob` has no `Simple` option. The library natively
supports character classes (`[abc]`), alternation (`{a,b}`), and `**` — these
cannot be disabled via API.

**Decision:** Move the restriction to the DSL parser layer. The parser MUST
reject `like` patterns containing `[`, `{`, or `**` syntax before passing them
to `glob.Compile(pattern, ':')`. This restricts `like` to simple `*` and `?`
wildcards only.

**Note on backslash escapes:** Backslash characters in patterns are treated as
literal (no escape mechanism). The parser validation MUST be tested against
`gobwas/glob` behavior during implementation. If `gobwas/glob` interprets
backslash as an escape character, the parser MUST reject patterns containing
backslash at parse time to maintain the "no escape" guarantee.

**Rationale:** Parser-level validation gives clear error messages at policy
creation time rather than unexpected matching behavior at evaluation time.
Restricting to simple wildcards keeps the lock syntax approachable for
non-technical game admins.
