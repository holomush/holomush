<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 47. Fuzz Testing for DSL Parser

> [Back to Decision Index](../README.md)

**Review finding:** The DSL parser accepts untrusted admin input. Without fuzz
testing, edge cases in the parser (malformed Unicode, deeply nested
expressions, pathological patterns) could cause panics or infinite loops.

**Decision:** Add Go-native fuzz tests (`func FuzzParseDSL`) targeting the
DSL parser. The fuzzer exercises `parser.Parse()` with random byte sequences
and validates that it either returns a valid AST or a structured error —
never panics, never hangs.

**Note on lock expression parser:** The lock expression parser SHOULD also have
a fuzz test (`func FuzzParseLock`) since lock expressions accept player input
and must handle malformed input gracefully. Lock parsing is a separate code
path from full DSL policy parsing.

**Rationale:** Go 1.18+ includes built-in fuzz testing. The DSL parser is the
primary attack surface for crafted input. Fuzz testing catches classes of bugs
(buffer overflows in string handling, stack overflow from recursive descent,
infinite loops from ambiguous grammar) that unit tests rarely cover. CI runs
`go test -fuzz=FuzzParseDSL -fuzztime=30s` to catch regressions.
