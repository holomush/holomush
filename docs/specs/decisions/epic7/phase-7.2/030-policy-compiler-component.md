<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 30. PolicyCompiler Component

> [Back to Decision Index](../README.md)

**Review finding:** The spec jumped from "DSL text stored in PostgreSQL" to
"engine evaluates conditions" without defining the compilation pipeline.
Without this, every `Evaluate()` would re-parse DSL text, violating the <25ms
p99 target (see Decision #97).

**Decision:** Add a `PolicyCompiler` component responsible for parsing DSL text
to AST, validating attribute references, pre-compiling glob patterns for `like`
expressions, and producing a `CompiledPolicy` struct. The compiled form is
stored alongside DSL text (as JSONB) and used by the in-memory policy cache.

**Rationale:** Compilation at store time (not evaluation time) ensures
`Evaluate()` only works with pre-parsed, pre-validated policies. The compiled
form also enables validation feedback with line/column error information at
`policy create`/`policy edit` time.
