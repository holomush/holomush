<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 10. Property Visibility Defaults

> [Back to Decision Index](../README.md)

**Question:** Should `visible_to`/`excluded_from` always have defaults?

**Sean's input:** Default to always visible to self and empty excluded list when
set to restricted. Prevents footguns.

**Decision:** When visibility is set to `restricted`, auto-populate
`visible_to = [parent_id]` (self) and `excluded_from = []`. For other visibility
levels, both fields are NULL (not applicable). Logic lives in Go property store,
not database triggers.
