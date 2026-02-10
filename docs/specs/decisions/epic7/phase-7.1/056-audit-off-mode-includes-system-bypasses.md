<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 56. Audit Off Mode Includes System Bypasses

> [Back to Decision Index](../README.md)

**Review finding:** The spec required system_bypass logging in ALL audit modes
including the minimal mode (originally called `off`), but the `off` mode was
defined as "Nothing logged" in the configuration table. These statements
contradicted each other.

**Decision:** Update the minimal mode (originally called `off`) description from
"Nothing" to "System bypasses + denials." This preserves the security intent of
always logging when the `system` subject bypasses policy evaluation and when
denials occur, while making the table description accurate.

**Rationale:** System bypass events are security-significant — they indicate
operations running outside normal policy evaluation. Even in development
environments where the minimal mode is used for performance, operators should be
aware of bypass activity. The volume is negligible (system operations are rare),
so there is no performance impact.

**Updated by:**

- [Decision #86](../general/086-audit-off-mode-logs-denials.md) — minimal mode also logs denials
- [Decision #104](../general/104-rename-audit-off-to-minimal.md) — mode renamed from `off` to `minimal`

**Cross-reference:** Main spec, Audit Log Configuration section; bead
`holomush-75um`.
