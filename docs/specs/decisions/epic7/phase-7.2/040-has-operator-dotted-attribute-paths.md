<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 40. `has` Operator Supports Dotted Attribute Paths

> [Back to Decision Index](../README.md)

**Review finding (C3):** The `has` operator only accepted simple identifiers
(`principal has faction`), but plugin attributes use dotted namespaces
(`reputation.score`). Without dotted path support, `has` couldn't check for
the existence of plugin-contributed attributes.

**Decision:** Extend the grammar to allow dotted paths after `has`:

```text
| attribute_root "has" identifier { "." identifier }
```

The parser joins segments with `.` and checks the resulting flat key against
the attribute bag. `principal has reputation.score` checks whether
`"reputation.score"` exists in the subject's attribute bag.

**Rationale:** Attribute providers register namespaced keys
(`reputation.score`, not nested maps). The `has` operator must match the same
flat-key model. Without this, admins couldn't write defensive patterns like
`principal has reputation.score && principal.reputation.score >= 50` for
plugin-contributed attributes.
