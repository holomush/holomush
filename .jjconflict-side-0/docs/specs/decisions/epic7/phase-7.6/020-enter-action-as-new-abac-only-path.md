<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 20. `enter` Action as New ABAC-Only Path

> [Back to Decision Index](../README.md)

**Review finding:** The seed policies introduce an `enter` action for location
entry control, but the static system handles movement through
`write:character:$self` (changing character location). This semantic gap affects
shadow mode validation.

**Decision:** ~~The `enter` action is a new capability introduced by the ABAC
system with no static-system equivalent. Shadow mode validation MUST exclude
`enter` actions when comparing engine results against `StaticAccessControl`.~~

**Superseded by [Decision #37](037-no-shadow-mode.md).** Shadow mode was removed.
The `enter` action remains a new ABAC capability with no static-system
equivalent, but no shadow mode validation is performed.

**Rationale:** The static system conflates "move yourself" with "enter a
location" under the `write:character` permission. ABAC separates these concerns
so admins can write fine-grained location entry policies (faction gates, level
requirements) independent of character modification permissions.
