<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 15. Grammar: `in` Operator Extended to Attribute Expressions

> [Back to Decision Index](../README.md)

**Review finding:** The DSL grammar defined `expr "in" list` and
`expr "in" entity_ref` but example policies used `principal.id in
resource.visible_to` — an attribute-to-attribute membership check that was
unparseable under the original grammar.

**Decision:** Add `expr "in" expr` to the condition production. The right-hand
side MUST resolve to a `[]string` or `[]any` attribute at evaluation time. This
is distinct from `expr "in" list` where the list is a literal.

**Rationale:** Property access control requires checking character IDs against
`visible_to` and `excluded_from` lists, which are attributes, not literals.
Without this, the healer-wound scenario and all `visible_to`/`excluded_from`
policies would be unimplementable.
