<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 16. Entity References Explicitly Deferred

> [Back to Decision Index](../README.md)

**Review finding:** The grammar included `entity_ref` (`Type::"value"`) syntax
and the operator table listed `in (entity)`, but the spec simultaneously said
this was "reserved for future." This created a confusing situation where admins
could write policies the parser would accept but the evaluator couldn't execute.

**Decision:** Remove `entity_ref` from the grammar entirely. The parser MUST
reject `Type::"value"` syntax with a clear error message directing admins to use
attribute-based group checks (`principal.flags.containsAny(["admin"])`) instead.
Entity references MAY be added in a future phase when group/hierarchy features
are implemented.

**Rationale:** Including unimplemented syntax in the grammar invites runtime
errors. Better to reject at parse time with a helpful message than to accept
syntax that fails silently at evaluation time.

**Updates [Decision #8](008-dsl-expression-language-scope.md):** The full expression language still includes all
operators from Decision #8. Only `entity_ref` is deferred — `in` works with
lists and attribute expressions.
